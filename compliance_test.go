package sql

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/resumeee-app/core"
	"github.com/resumeee-app/core/store"
	"github.com/resumeee-app/core/storetest"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// memorySeq generates unique SQLite shared-cache database names across compliance subtests.
var memorySeq atomic.Int64

// newMemoryStoreFactory builds a storetest.Factory backed by an isolated SQLite in-memory database.
func newMemoryStoreFactory(t *testing.T, clock func() time.Time) *store.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:storetest_mem_%d?mode=memory&cache=shared", memorySeq.Add(1))
	return openStoreForTest(t, dsn, clock)
}

// newFileStoreFactory builds a storetest.Factory backed by a file-system WAL SQLite database.
func newFileStoreFactory(t *testing.T, clock func() time.Time) *store.Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "storetest.db")
	return openStoreForTest(t, dsn, clock)
}

// openStoreForTest opens an auto-migrated SQLite Backend and wraps it in *store.Store with clock.
func openStoreForTest(t *testing.T, dsn string, clock func() time.Time) *store.Store {
	t.Helper()
	db, err := New(dsn)
	if err != nil {
		t.Fatalf("New(%q): %v", dsn, err)
	}
	backend, err := NewBackend(db)
	if err != nil {
		closeUnderlyingDB(db)
		t.Fatalf("NewBackend: %v", err)
	}
	var opts []store.Option
	if clock != nil {
		opts = append(opts, store.WithClock(clock))
	}
	st := store.New(backend, opts...)
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func TestStoreCompliance_SQLiteMemory(t *testing.T) {
	storetest.RunAll(t, newMemoryStoreFactory)
}

func TestStoreCompliance_SQLiteFileWAL(t *testing.T) {
	storetest.RunAll(t, newFileStoreFactory)
}

func TestDB_SchemaIndexesAndTransactions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "schema.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New(%q): %v", dbPath, err)
	}
	backend, err := NewBackend(db)
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}
	t.Cleanup(func() {
		_ = backend.Close()
	})

	requiredIndexes := []string{
		"idx_experience_skills_skill_id",
		"idx_project_skills_skill_id",
		"idx_fact_skills_skill_id",
		"idx_jobs_active",
		"idx_branch_version",
	}
	for _, idxName := range requiredIndexes {
		var count int
		if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", idxName).Scan(&count).Error; err != nil {
			t.Fatalf("query index %s: %v", idxName, err)
		}
		if count != 1 {
			t.Errorf("expected index %s to exist, count=%d", idxName, count)
		}
	}

	// Legacy pdf_parse_jobs table must not exist in greenfield schema.
	var legacyTableCount int
	if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='pdf_parse_jobs'").Scan(&legacyTableCount).Error; err != nil {
		t.Fatalf("query legacy table: %v", err)
	}
	if legacyTableCount != 0 {
		t.Errorf("expected legacy pdf_parse_jobs table to be absent, got count=%d", legacyTableCount)
	}

	// Composite unique index idx_branch_version rejects duplicate (branch_id, version_number).
	st := store.New(backend)
	if err := st.Branches().CreateBranch(ctx, &core.Branch{
		ID: "br-uniq", UserID: "u1", Name: "Unique", TemplateSetID: core.TemplateStandardCleanID,
	}); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	now := time.Now().UTC()
	if err := backend.Write(ctx, func(tx store.Tx) error {
		if err := tx.PutVersion(ctx, &core.BranchVersion{
			ID: "v1", UserID: "u1", BranchID: "br-uniq", VersionNumber: 1, BranchEpoch: 1,
			Status: core.VersionStatusCompleted, SourceType: core.SourceTypeManualEdit,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return tx.PutVersion(ctx, &core.BranchVersion{
			ID: "v2", UserID: "u1", BranchID: "br-uniq", VersionNumber: 1, BranchEpoch: 1,
			Status: core.VersionStatusCompleted, SourceType: core.SourceTypeManualEdit,
			CreatedAt: now, UpdatedAt: now,
		})
	}); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate (branch_id, version_number), got %v", err)
	}

	// Verify Write rolled back v1 cleanly as well.
	if _, err := st.Branches().Version(ctx, "br-uniq", "v1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected rolled-back v1 to be absent (ErrNotFound), got %v", err)
	}

	// Explicit Delete* missing returns ErrNotFound.
	if err := backend.Write(ctx, func(tx store.Tx) error {
		return tx.DeleteJob(ctx, "nonexistent-job")
	}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting missing job, got %v", err)
	}
}

func TestBackend_FileBackedConcurrentSubmitAndClaim(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 15, 14, 0, 0, 0, time.UTC)
	st := newFileStoreFactory(t, func() time.Time { return now })

	// Concurrent SubmitBranchTailor enforcing MaxPendingPerTenant=2 across distinct branches.
	for i := 0; i < 6; i++ {
		if err := st.Branches().CreateBranch(ctx, &core.Branch{
			ID: fmt.Sprintf("br-cap-%d", i), UserID: "u-cap", Name: "Cap", TemplateSetID: core.TemplateStandardCleanID,
		}); err != nil {
			t.Fatalf("CreateBranch cap %d: %v", i, err)
		}
	}

	var subWG sync.WaitGroup
	subErrs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		subWG.Add(1)
		go func(idx int) {
			defer subWG.Done()
			_, err := st.Jobs().SubmitBranchTailor(ctx,
				&core.Job{ID: fmt.Sprintf("job-cap-%d", idx), UserID: "u-cap"},
				&core.BranchVersion{ID: fmt.Sprintf("ver-cap-%d", idx), BranchID: fmt.Sprintf("br-cap-%d", idx), UserID: "u-cap"},
				core.PendingJobOptions{Now: now, MaxPendingPerTenant: 2},
			)
			subErrs <- err
		}(i)
	}
	subWG.Wait()
	close(subErrs)

	admitted := 0
	for err := range subErrs {
		if err == nil {
			admitted++
		} else if !errors.Is(err, core.ErrPendingLimitExceeded) {
			t.Fatalf("unexpected concurrent SubmitBranchTailor error: %v", err)
		}
	}
	if admitted != 2 {
		t.Fatalf("expected exactly 2 admitted jobs under MaxPendingPerTenant=2, got %d", admitted)
	}

	// Concurrent SubmitPDFParse enforcing MaxGlobalPending=4 (2 already pending above).
	var pdfWG sync.WaitGroup
	pdfErrs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		pdfWG.Add(1)
		go func(idx int) {
			defer pdfWG.Done()
			pdfErrs <- st.Jobs().SubmitPDFParse(ctx, &core.Job{
				ID:     fmt.Sprintf("pdf-cap-%d", idx),
				UserID: fmt.Sprintf("u-pcap-%d", idx),
				PDF:    &core.PDFParseSpec{RawText: "raw text"},
			}, core.PendingJobOptions{Now: now, MaxPendingPerTenant: 5, MaxGlobalPending: 4})
		}(i)
	}
	pdfWG.Wait()
	close(pdfErrs)

	admittedPDF := 0
	for err := range pdfErrs {
		if err == nil {
			admittedPDF++
		} else if !errors.Is(err, core.ErrPendingLimitExceeded) {
			t.Fatalf("unexpected concurrent SubmitPDFParse error: %v", err)
		}
	}
	if admittedPDF != 2 {
		t.Fatalf("expected exactly 2 admitted PDF jobs under MaxGlobalPending=4, got %d", admittedPDF)
	}

	// Claim and complete all 4 queued jobs across competing workers simultaneously.
	var claimWG sync.WaitGroup
	claimedIDs := make(chan string, 4)
	claimErrs := make(chan error, 8)
	for w := 0; w < 8; w++ {
		claimWG.Add(1)
		go func(workerIdx int) {
			defer claimWG.Done()
			workerID := fmt.Sprintf("worker-%d", workerIdx)
			job, err := st.Jobs().ClaimNext(ctx, core.ClaimJobRequest{
				WorkerID:      workerID,
				Now:           now,
				LeaseDuration: 2 * time.Minute,
			})
			if err != nil {
				claimErrs <- err
				return
			}
			if job == nil {
				return
			}
			claimedIDs <- job.ID
			switch job.Kind {
			case core.JobKindBranchTailor:
				claimErrs <- st.Jobs().CompleteBranchTailor(ctx, job.ID, workerID, &core.BranchVersion{
					Status:     core.VersionStatusCompleted,
					HTMLOutput: "<p>ok</p>",
				})
			case core.JobKindPDFParse:
				claimErrs <- st.Jobs().CompletePDFParse(ctx, job.ID, workerID, &core.JSONResume{})
			}
		}(w)
	}
	claimWG.Wait()
	close(claimedIDs)
	close(claimErrs)

	for err := range claimErrs {
		if err != nil {
			t.Fatalf("concurrent claim/complete error: %v", err)
		}
	}
	seen := make(map[string]struct{})
	for id := range claimedIDs {
		if _, dup := seen[id]; dup {
			t.Fatalf("job %s claimed twice by competing workers", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != 4 {
		t.Fatalf("expected 4 unique jobs claimed and completed, got %d", len(seen))
	}
}

func TestBackend_MultiInstanceFileConcurrency(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "multi_instance.db")

	st1, err := OpenStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenStore 1: %v", err)
	}
	defer func() { _ = st1.Close() }()

	st2, err := OpenStore(ctx, dbPath, WithAutoMigrate(false))
	if err != nil {
		t.Fatalf("OpenStore 2: %v", err)
	}
	defer func() { _ = st2.Close() }()

	const incrementsPerStore = 10
	var wg sync.WaitGroup
	errs := make(chan error, incrementsPerStore*2)
	for i := 0; i < incrementsPerStore; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, e := st1.Profiles().IncrementMainRevision(ctx, "default")
			errs <- e
		}()
		go func() {
			defer wg.Done()
			_, e := st2.Profiles().IncrementMainRevision(ctx, "default")
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent IncrementMainRevision error: %v", e)
		}
	}
	prof, err := st1.Profiles().Profile(ctx, "default")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	want := int64(1 + incrementsPerStore*2)
	if prof.MainRevision != want {
		t.Fatalf("expected MainRevision=%d after %d concurrent increments across 2 stores, got %d", want, incrementsPerStore*2, prof.MainRevision)
	}
}

func TestSanitizeDSN(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "postgres uri password and query token",
			input: "postgres://dbuser:s3cr3t@db.example.com:5432/resumeee?sslmode=require&token=abc123",
			want:  "postgres://dbuser:*****@db.example.com:5432/resumeee?sslmode=require&token=*****",
		},
		{
			name:  "mysql tcp dsn",
			input: "admin:hunter2@tcp(127.0.0.1:3306)/resumeee",
			want:  "admin:*****@tcp(127.0.0.1:3306)/resumeee",
		},
		{
			name:  "key value dsn",
			input: "host=localhost user=app password=mysecretpass dbname=app",
			want:  "host=localhost user=app password=***** dbname=app",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeDSN(tc.input)
			if got != tc.want {
				t.Errorf("SanitizeDSN(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestOpenStore_MigratesLegacyBranchAndVersionSchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	legacyDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}

	legacyDDL := []string{
		`CREATE TABLE "target_resumes" (` +
			`"id" text,"user_id" text NOT NULL,"name" text NOT NULL,"is_default" numeric NOT NULL,` +
			`"target_role" text,"target_company" text,"target_requirements" text,"branch_instructions" text,` +
			`"preset_id" text,"template_set_id" text NOT NULL,"provider_override" text,"model_override" text,` +
			`"reasoning_level" text,"pinned_entity_ids" text,"excluded_entity_ids" text,"active_version_id" text,` +
			`"gap_report" text,"ats_score" text,"last_synced_main_revision" integer,"created_at" datetime,"updated_at" datetime,` +
			`PRIMARY KEY ("id"))`,
		`INSERT INTO "target_resumes" ("id","user_id","name","is_default","template_set_id","active_version_id","last_synced_main_revision") ` +
			`VALUES ('branch-default-default','default','Main Profile (Default)',1,'builtin-standard-clean','ver-default-default',1)`,
		`CREATE TABLE "resume_versions" (` +
			`"id" text,"user_id" text NOT NULL,"branch_id" text,"version_number" integer NOT NULL,"status" text NOT NULL,` +
			`"main_revision_used" integer,"template_set_id_used" text,"strategy_id_used" text,"provider_used" text,"model_used" text,` +
			`"source_type" text,"parent_version_id" text,"tailored_content" text,"gap_report" text,"ats_score" text,` +
			`"validation_warnings" text,"error_message" text,"html_output" text,"markdown_output" text,"plain_text_output" text,` +
			`"created_at" datetime,"completed_at" datetime,PRIMARY KEY ("id"))`,
		`INSERT INTO "resume_versions" ("id","user_id","branch_id","version_number","status") ` +
			`VALUES ('ver-default-default','default','branch-default-default',1,'completed')`,
	}
	for _, stmt := range legacyDDL {
		if err := legacyDB.Exec(stmt).Error; err != nil {
			t.Fatalf("exec legacy ddl %q: %v", stmt, err)
		}
	}
	closeUnderlyingDB(legacyDB)

	st, err := OpenStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenStore on legacy DB failed: %v", err)
	}
	defer func() { _ = st.Close() }()

	br, err := st.Branches().Branch(ctx, "default", "branch-default-default")
	if err != nil {
		t.Fatalf("Branch after migration: %v", err)
	}
	if br.Epoch != 1 {
		t.Errorf("migrated Branch.Epoch = %d, want 1", br.Epoch)
	}

	ver, err := st.Branches().Version(ctx, "branch-default-default", "ver-default-default")
	if err != nil {
		t.Fatalf("Version after migration: %v", err)
	}
	if ver.BranchEpoch != 1 {
		t.Errorf("migrated BranchVersion.BranchEpoch = %d, want 1", ver.BranchEpoch)
	}
}
