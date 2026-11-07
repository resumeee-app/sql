package memory_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"resumeee/internal/domain"
	"resumeee/internal/renderer"
	"resumeee/internal/repository"
	"resumeee/internal/repository/memory"
)

func setupTestStore(t *testing.T) *memory.Store {
	t.Helper()
	return memory.New()
}

// TestMemoryStore_Interfaces verifies that memory.Store satisfies domain and repository interfaces.
func TestMemoryStore_Interfaces(t *testing.T) {
	store := setupTestStore(t)
	var _ repository.Store = store
	var _ domain.ProfileRepository = store
	var _ domain.BranchRepository = store
	var _ domain.TemplateRepository = store
	var _ domain.PresetRepository = store
	var _ domain.PDFParseJobRepository = store

	if store.Profile() == nil || store.Branch() == nil || store.Preset() == nil || store.Template() == nil || store.PDFJob() == nil {
		t.Fatal("expected non-nil repositories from store getters")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("expected clean close: %v", err)
	}
}

// TestProfileRepository_MasterRevisionIncrements asserts that profile mutations increment master revision.
func TestProfileRepository_MasterRevisionIncrements(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-rev-test"

	prof := &domain.Profile{
		ID:       "prof-1",
		UserID:   userID,
		FullName: "Initial User",
	}
	if err := store.UpsertProfile(ctx, prof); err != nil {
		t.Fatalf("failed to insert profile: %v", err)
	}
	if prof.MasterRevision != 1 {
		t.Errorf("expected initial master revision 1, got %d", prof.MasterRevision)
	}

	exp := &domain.WorkExperience{
		ID:        "exp-1",
		UserID:    userID,
		Company:   "Acme Corp",
		RoleTitle: "Staff Engineer",
		StartDate: domain.NewDate(2024, 1, 1),
	}
	if err := store.CreateExperience(ctx, exp); err != nil {
		t.Fatalf("failed to create experience: %v", err)
	}

	p, err := store.GetProfile(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get profile: %v", err)
	}
	if p.MasterRevision != 2 {
		t.Errorf("expected master revision 2 after experience creation, got %d", p.MasterRevision)
	}

	rev, err := store.IncrementMasterRevision(ctx, userID)
	if err != nil {
		t.Fatalf("failed manual increment: %v", err)
	}
	if rev != 3 {
		t.Errorf("expected revision 3, got %d", rev)
	}
}

// TestProfileRepository_FactsAndSnapshot verifies facts association and snapshot generation.
func TestProfileRepository_FactsAndSnapshot(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-facts-test"

	prof := &domain.Profile{ID: "prof-facts", UserID: userID, FullName: "Facts Engineer"}
	if err := store.UpsertProfile(ctx, prof); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	skill1 := &domain.Skill{ID: "sk-go", UserID: userID, Name: "Go"}
	if err := store.CreateSkill(ctx, skill1); err != nil {
		t.Fatalf("create skill: %v", err)
	}

	exp := &domain.WorkExperience{
		ID:        "exp-poly",
		UserID:    userID,
		Company:   "Cloud Scale Inc",
		RoleTitle: "Backend Lead",
		StartDate: domain.NewDate(2023, 1, 1),
		Skills:    []domain.Skill{*skill1},
	}
	if err := store.CreateExperience(ctx, exp); err != nil {
		t.Fatalf("create exp: %v", err)
	}

	fact := &domain.Fact{
		ID:        "fact-poly-1",
		UserID:    userID,
		OwnerID:   exp.ID,
		OwnerType: domain.OwnerTypeWorkExperience,
		Statement: "Architected real-time microservices handling 50k QPS",
		Skills:    []domain.Skill{*skill1},
	}
	if err := store.CreateFact(ctx, fact); err != nil {
		t.Fatalf("create fact: %v", err)
	}

	facts, err := store.ListFactsByOwner(ctx, userID, string(domain.OwnerTypeWorkExperience), exp.ID)
	if err != nil {
		t.Fatalf("list facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].Statement != fact.Statement {
		t.Errorf("expected statement %q, got %q", fact.Statement, facts[0].Statement)
	}

	snap, err := store.GetSnapshot(ctx, userID)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if len(snap.Experiences) != 1 {
		t.Fatalf("expected 1 experience in snapshot, got %d", len(snap.Experiences))
	}
	if len(snap.Experiences[0].Facts) != 1 {
		t.Errorf("expected 1 preloaded fact on experience, got %d", len(snap.Experiences[0].Facts))
	}
}

// TestProfileRepository_ReplaceCareerGraph verifies atomic replacement of career records.
func TestProfileRepository_ReplaceCareerGraph(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-replace-test"

	prof := &domain.Profile{ID: "prof-rep", UserID: userID, FullName: "Graph Engineer"}
	if err := store.UpsertProfile(ctx, prof); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	sk1 := &domain.Skill{ID: "sk-old", UserID: userID, Name: "OldSkill"}
	if err := store.CreateSkill(ctx, sk1); err != nil {
		t.Fatalf("create old skill: %v", err)
	}

	newSkill := domain.Skill{ID: "sk-new", UserID: userID, Name: "NewSkill"}
	newExp := domain.WorkExperience{
		ID:        "exp-new",
		UserID:    userID,
		Company:   "NewCo",
		RoleTitle: "Director",
		StartDate: domain.NewDate(2024, 6, 1),
		Skills:    []domain.Skill{newSkill},
	}
	newFact := domain.Fact{
		ID:        "fact-new",
		UserID:    userID,
		OwnerID:   newExp.ID,
		OwnerType: domain.OwnerTypeWorkExperience,
		Statement: "Expanded engineering team from 10 to 50",
		Skills:    []domain.Skill{newSkill},
	}

	snap := &domain.MasterProfileSnapshot{
		Profile:        *prof,
		Skills:         []domain.Skill{newSkill},
		Experiences:    []domain.WorkExperience{newExp},
		Facts:          []domain.Fact{newFact},
		MasterRevision: 10,
	}

	if err := store.ReplaceCareerGraph(ctx, userID, snap); err != nil {
		t.Fatalf("replace career graph: %v", err)
	}

	skills, err := store.ListSkills(ctx, userID)
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(skills) != 1 || skills[0].ID != newSkill.ID {
		t.Errorf("expected only newSkill, got %+v", skills)
	}

	exps, err := store.ListExperiences(ctx, userID)
	if err != nil {
		t.Fatalf("list experiences: %v", err)
	}
	if len(exps) != 1 || exps[0].ID != newExp.ID {
		t.Errorf("expected only newExp, got %+v", exps)
	}
}

// TestPresetRepository_Immutability verifies built-in preset protection.
func TestPresetRepository_Immutability(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-preset-test"

	builtin := &domain.Preset{
		ID:                 domain.Preset1PageTechResumeID,
		UserID:             userID,
		Name:               "Builtin Tech",
		SystemInstructions: "Focus on tech metrics",
	}
	if err := store.CreatePreset(ctx, builtin); err != nil {
		t.Fatalf("create builtin preset: %v", err)
	}

	builtin.Name = "Attempted Mutation"
	err := store.UpdatePreset(ctx, builtin)
	if !errors.Is(err, domain.ErrStrategyImmutable) {
		t.Errorf("expected ErrStrategyImmutable on update, got: %v", err)
	}

	err = store.DeletePreset(ctx, userID, builtin.ID)
	if !errors.Is(err, domain.ErrStrategyImmutable) {
		t.Errorf("expected ErrStrategyImmutable on delete, got: %v", err)
	}

	custom := &domain.Preset{
		ID:                 "custom-preset-1",
		UserID:             userID,
		Name:               "Custom Preset",
		SystemInstructions: "Custom rules",
	}
	if err := store.CreatePreset(ctx, custom); err != nil {
		t.Fatalf("create custom preset: %v", err)
	}
	custom.Name = "Updated Custom Preset"
	if err := store.UpdatePreset(ctx, custom); err != nil {
		t.Fatalf("update custom preset: %v", err)
	}
	if err := store.DeletePreset(ctx, userID, custom.ID); err != nil {
		t.Fatalf("delete custom preset: %v", err)
	}
}

// TestTemplateRepository_Validation verifies template validation on persistence.
func TestTemplateRepository_Validation(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-tmpl-test"

	invalidTmpl := &domain.TemplateSet{
		ID:               "invalid-tmpl",
		UserID:           userID,
		Name:             "Invalid Syntax",
		HTMLTemplate:     "<div>{{ unclosed tag",
		MarkdownTemplate: "# Resume",
		TextTemplate:     "Plain text",
	}
	err := store.CreateTemplate(ctx, invalidTmpl)
	if err == nil {
		t.Fatal("expected validation error on invalid template syntax, got nil")
	}

	validTmpl := &domain.TemplateSet{
		ID:               "valid-tmpl",
		UserID:           userID,
		Name:             "Valid Template",
		HTMLTemplate:     "<div>{{ .Profile.FullName }}</div>",
		MarkdownTemplate: "# {{ .Profile.FullName }}",
		TextTemplate:     "Name: {{ .Profile.FullName }}",
	}
	if err := store.CreateTemplate(ctx, validTmpl); err != nil {
		t.Fatalf("create valid template: %v", err)
	}

	fetched, err := store.GetTemplate(ctx, userID, validTmpl.ID)
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if fetched.Name != validTmpl.Name {
		t.Errorf("expected template name %s, got %s", validTmpl.Name, fetched.Name)
	}
}

// TestBranchRepository_LifecycleAndVersions verifies version sequencing and completion logic.
func TestBranchRepository_LifecycleAndVersions(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-branch-test"

	p := &domain.Profile{ID: "prof-br", UserID: userID, FullName: "Branch Engineer", MasterRevision: 3}
	if err := store.UpsertProfile(ctx, p); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	branch := &domain.Branch{
		ID:                       "br-test-1",
		UserID:                   userID,
		Name:                     "Tech Resume Branch",
		TemplateSetID:            renderer.TemplateStandardCleanID,
		LastSyncedMasterRevision: 1,
	}
	if err := store.CreateBranch(ctx, branch); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	fetchedBranch, err := store.GetBranch(ctx, userID, branch.ID)
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if !fetchedBranch.IsStale {
		t.Errorf("expected branch to be stale because last synced (1) < master revision (3)")
	}

	v1 := &domain.BranchVersion{
		ID:                 "ver-1",
		BranchID:           branch.ID,
		Status:             domain.VersionStatusProcessing,
		MasterRevisionUsed: 3,
	}
	if err := store.CreateVersion(ctx, v1); err != nil {
		t.Fatalf("create version 1: %v", err)
	}
	if v1.VersionNumber != 1 {
		t.Errorf("expected version number 1, got %d", v1.VersionNumber)
	}

	v2 := &domain.BranchVersion{
		ID:                 "ver-2",
		BranchID:           branch.ID,
		Status:             domain.VersionStatusPending,
		MasterRevisionUsed: 3,
	}
	if err := store.CreateVersion(ctx, v2); err != nil {
		t.Fatalf("create version 2: %v", err)
	}
	if v2.VersionNumber != 2 {
		t.Errorf("expected sequential version number 2, got %d", v2.VersionNumber)
	}

	v2.HTMLOutput = "<html>Rendered</html>"
	if err := store.CompleteVersion(ctx, v2); err != nil {
		t.Fatalf("complete version 2: %v", err)
	}

	updatedBranch, err := store.GetBranch(ctx, userID, branch.ID)
	if err != nil {
		t.Fatalf("get branch after completion: %v", err)
	}
	if updatedBranch.ActiveVersionID == nil || *updatedBranch.ActiveVersionID != v2.ID {
		t.Fatalf("expected active version ID %s, got %+v", v2.ID, updatedBranch.ActiveVersionID)
	}
	if updatedBranch.LastSyncedMasterRevision != 3 {
		t.Errorf("expected last synced master revision 3, got %d", updatedBranch.LastSyncedMasterRevision)
	}
	if updatedBranch.IsStale {
		t.Errorf("expected branch to no longer be stale")
	}

	if err := store.FailInterruptedVersions(ctx, "server reboot"); err != nil {
		t.Fatalf("fail interrupted: %v", err)
	}
	interruptedV1, err := store.GetVersion(ctx, branch.ID, v1.ID)
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}
	if interruptedV1.Status != domain.VersionStatusFailed {
		t.Errorf("expected v1 to fail after interruption, got %s", interruptedV1.Status)
	}
	if !strings.Contains(interruptedV1.ErrorMessage, "server reboot") {
		t.Errorf("expected error message to contain reason, got %q", interruptedV1.ErrorMessage)
	}
}

// TestPDFParseJobRepository_Lifecycle verifies PDF parse job status updates and crash recovery.
func TestPDFParseJobRepository_Lifecycle(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-pdf-test"

	job := &domain.PDFParseJob{
		ID:       "pdf-job-1",
		UserID:   userID,
		Status:   domain.PDFParseJobStatusPending,
		Filename: "resume.pdf",
		Provider: "gemini",
		Model:    "gemini-3.8-flash",
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, err := store.GetJob(ctx, userID, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != domain.PDFParseJobStatusPending {
		t.Errorf("expected status pending, got %s", got.Status)
	}

	if err := store.UpdateJobStatus(ctx, job.ID, domain.PDFParseJobStatusProcessing, nil, ""); err != nil {
		t.Fatalf("update to processing: %v", err)
	}

	if err := store.FailInterruptedJobs(ctx, "worker crash"); err != nil {
		t.Fatalf("fail interrupted jobs: %v", err)
	}

	crashed, err := store.GetJob(ctx, userID, job.ID)
	if err != nil {
		t.Fatalf("get job after crash: %v", err)
	}
	if crashed.Status != domain.PDFParseJobStatusFailed {
		t.Errorf("expected failed status, got %s", crashed.Status)
	}
	if !strings.Contains(crashed.ErrorMessage, "worker crash") {
		t.Errorf("expected error message to contain reason, got %q", crashed.ErrorMessage)
	}
}

// TestNewSeeded_DefaultData verifies that NewSeeded initializes built-ins, default profile, and default branch.
func TestNewSeeded_DefaultData(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewSeeded(ctx, "test-user")
	if err != nil {
		t.Fatalf("NewSeeded failed: %v", err)
	}

	prof, err := store.GetProfile(ctx, "test-user")
	if err != nil {
		t.Fatalf("get seeded profile: %v", err)
	}
	if prof.UserID != "test-user" {
		t.Errorf("expected user test-user, got %s", prof.UserID)
	}

	presets, err := store.ListPresets(ctx, "test-user")
	if err != nil {
		t.Fatalf("list presets: %v", err)
	}
	if len(presets) != 2 {
		t.Errorf("expected 2 seeded presets, got %d", len(presets))
	}

	templates, err := store.ListTemplates(ctx, "test-user")
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}
	if len(templates) != 5 {
		t.Errorf("expected 5 seeded templates, got %d", len(templates))
	}

	branches, err := store.ListBranches(ctx, "test-user")
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	if len(branches) != 1 || !branches[0].IsDefault {
		t.Fatalf("expected 1 default branch, got %+v", branches)
	}
	if branches[0].ActiveVersionID == nil {
		t.Fatal("expected default branch to have rendered active version")
	}
}

// TestMemoryStore_ConcurrentAccess verifies thread safety and state validity under parallel read/write workloads.
func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-race"

	prof := &domain.Profile{ID: "prof-race", UserID: userID, FullName: "Race Tester"}
	if err := store.UpsertProfile(ctx, prof); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	var wg sync.WaitGroup
	workers := 20
	iterations := 50
	errCh := make(chan error, workers*iterations*3)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				expID := fmt.Sprintf("exp-%d-%d", w, j)
				exp := &domain.WorkExperience{
					ID:        expID,
					UserID:    userID,
					Company:   fmt.Sprintf("Company-%d", w),
					RoleTitle: "Dev",
					StartDate: domain.NewDate(2024, 1, 1),
				}
				if err := store.CreateExperience(ctx, exp); err != nil {
					errCh <- fmt.Errorf("create experience error: %w", err)
				}
				if _, err := store.GetSnapshot(ctx, userID); err != nil {
					errCh <- fmt.Errorf("get snapshot error: %w", err)
				}
				if err := store.DeleteExperience(ctx, userID, expID); err != nil {
					errCh <- fmt.Errorf("delete experience error: %w", err)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("unexpected concurrent error: %v", err)
	}

	snap, err := store.GetSnapshot(ctx, userID)
	if err != nil {
		t.Fatalf("failed to retrieve snapshot after concurrent operations: %v", err)
	}
	if len(snap.Experiences) != 0 {
		t.Errorf("expected 0 experiences remaining after deletes, got %d", len(snap.Experiences))
	}
}

// TestMemoryStore_DeepCopyIsolation verifies mutations after writes or reads do not mutate internal store state.
func TestMemoryStore_DeepCopyIsolation(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-deep-copy"

	// 1. Profile metadata isolation
	prof := &domain.Profile{
		ID:       "prof-dc",
		UserID:   userID,
		FullName: "Original Name",
		Metadata: map[string]any{"role": "staff"},
	}
	if err := store.UpsertProfile(ctx, prof); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	prof.Metadata["role"] = "tampered"
	fetchedProf, err := store.GetProfile(ctx, userID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if fetchedProf.Metadata["role"] != "staff" {
		t.Errorf("store metadata was mutated by external caller mutation: got %v", fetchedProf.Metadata["role"])
	}
	fetchedProf.Metadata["role"] = "tampered-again"
	fetchedProf2, _ := store.GetProfile(ctx, userID)
	if fetchedProf2.Metadata["role"] != "staff" {
		t.Errorf("store metadata was mutated by caller mutating return value: got %v", fetchedProf2.Metadata["role"])
	}

	// 2. Skill keywords isolation
	skill := &domain.Skill{
		ID:       "sk-dc",
		UserID:   userID,
		Name:     "Go",
		Keywords: []string{"backend", "concurrency"},
	}
	if err := store.CreateSkill(ctx, skill); err != nil {
		t.Fatalf("create skill: %v", err)
	}
	skill.Keywords[0] = "mutated"
	fetchedSkill, err := store.GetSkill(ctx, userID, skill.ID)
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if fetchedSkill.Keywords[0] != "backend" {
		t.Errorf("skill keywords mutated after write: got %s", fetchedSkill.Keywords[0])
	}
	fetchedSkill.Keywords[0] = "mutated-2"
	fetchedSkill2, _ := store.GetSkill(ctx, userID, skill.ID)
	if fetchedSkill2.Keywords[0] != "backend" {
		t.Errorf("skill keywords mutated after read: got %s", fetchedSkill2.Keywords[0])
	}

	// 3. Branch pinned entity IDs isolation
	branch := &domain.Branch{
		ID:              "br-dc",
		UserID:          userID,
		Name:            "Deep Copy Branch",
		PinnedEntityIDs: []string{"ent-1", "ent-2"},
	}
	if err := store.CreateBranch(ctx, branch); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	branch.PinnedEntityIDs[0] = "tampered"
	fetchedBranch, err := store.GetBranch(ctx, userID, branch.ID)
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if fetchedBranch.PinnedEntityIDs[0] != "ent-1" {
		t.Errorf("branch pinned IDs mutated: got %s", fetchedBranch.PinnedEntityIDs[0])
	}

	// 4. PDF parse job result isolation
	job := &domain.PDFParseJob{
		ID:     "job-dc",
		UserID: userID,
		Status: domain.PDFParseJobStatusPending,
		Result: map[string]any{"summary": "original text"},
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	job.Result.(map[string]any)["summary"] = "tampered text"
	fetchedJob, err := store.GetJob(ctx, userID, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if fetchedJob.Result.(map[string]any)["summary"] != "original text" {
		t.Errorf("pdf job result mutated: got %v", fetchedJob.Result)
	}
}

// TestMemoryStore_ErrorSentinels verifies domain error sentinels returned across operations.
func TestMemoryStore_ErrorSentinels(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "user-sentinels"

	t.Run("ErrNotFound", func(t *testing.T) {
		tests := []struct {
			name string
			call func() error
		}{
			{"GetProfile", func() error { _, err := store.GetProfile(ctx, "nonexistent"); return err }},
			{"GetSkill", func() error { _, err := store.GetSkill(ctx, userID, "nonexistent"); return err }},
			{"UpdateSkill", func() error { return store.UpdateSkill(ctx, &domain.Skill{ID: "none", UserID: userID}) }},
			{"DeleteSkill", func() error { return store.DeleteSkill(ctx, userID, "none") }},
			{"GetExperience", func() error { _, err := store.GetExperience(ctx, userID, "none"); return err }},
			{"UpdateExperience", func() error { return store.UpdateExperience(ctx, &domain.WorkExperience{ID: "none", UserID: userID}) }},
			{"DeleteExperience", func() error { return store.DeleteExperience(ctx, userID, "none") }},
			{"GetProject", func() error { _, err := store.GetProject(ctx, userID, "none"); return err }},
			{"UpdateProject", func() error { return store.UpdateProject(ctx, &domain.Project{ID: "none", UserID: userID}) }},
			{"DeleteProject", func() error { return store.DeleteProject(ctx, userID, "none") }},
			{"GetEducation", func() error { _, err := store.GetEducation(ctx, userID, "none"); return err }},
			{"UpdateEducation", func() error { return store.UpdateEducation(ctx, &domain.Education{ID: "none", UserID: userID}) }},
			{"DeleteEducation", func() error { return store.DeleteEducation(ctx, userID, "none") }},
			{"GetCertification", func() error { _, err := store.GetCertification(ctx, userID, "none"); return err }},
			{"UpdateCertification", func() error { return store.UpdateCertification(ctx, &domain.Certification{ID: "none", UserID: userID}) }},
			{"DeleteCertification", func() error { return store.DeleteCertification(ctx, userID, "none") }},
			{"GetFact", func() error { _, err := store.GetFact(ctx, userID, "none"); return err }},
			{"UpdateFact", func() error { return store.UpdateFact(ctx, &domain.Fact{ID: "none", UserID: userID}) }},
			{"DeleteFact", func() error { return store.DeleteFact(ctx, userID, "none") }},
			{"GetPreset", func() error { _, err := store.GetPreset(ctx, userID, "none"); return err }},
			{"UpdatePreset", func() error { return store.UpdatePreset(ctx, &domain.Preset{ID: "none", UserID: userID}) }},
			{"DeletePreset", func() error { return store.DeletePreset(ctx, userID, "none") }},
			{"GetTemplate", func() error { _, err := store.GetTemplate(ctx, userID, "none"); return err }},
			{"DeleteTemplate", func() error { return store.DeleteTemplate(ctx, userID, "none") }},
			{"GetBranch", func() error { _, err := store.GetBranch(ctx, userID, "none"); return err }},
			{"UpdateBranch", func() error { return store.UpdateBranch(ctx, &domain.Branch{ID: "none", UserID: userID}) }},
			{"DeleteBranch", func() error { return store.DeleteBranch(ctx, userID, "none") }},
			{"GetVersion", func() error { _, err := store.GetVersion(ctx, "none", "none"); return err }},
			{"GetVersionByNumber", func() error { _, err := store.GetVersionByNumber(ctx, "none", 1); return err }},
			{"UpdateVersionStatus", func() error { return store.UpdateVersionStatus(ctx, "none", domain.VersionStatusCompleted, "") }},
			{"CompleteVersion", func() error { return store.CompleteVersion(ctx, &domain.BranchVersion{ID: "none", BranchID: "none"}) }},
			{"GetJob", func() error { _, err := store.GetJob(ctx, userID, "none"); return err }},
			{"UpdateJobStatus", func() error { return store.UpdateJobStatus(ctx, "none", domain.PDFParseJobStatusCompleted, nil, "") }},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.call()
				if !errors.Is(err, domain.ErrNotFound) {
					t.Errorf("%s: expected ErrNotFound, got: %v", tc.name, err)
				}
			})
		}
	})

	t.Run("ErrConflict", func(t *testing.T) {
		sk := &domain.Skill{ID: "sk-dup", UserID: userID, Name: "Duplicate"}
		if err := store.CreateSkill(ctx, sk); err != nil {
			t.Fatalf("initial create: %v", err)
		}
		if err := store.CreateSkill(ctx, sk); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("expected ErrConflict on duplicate skill, got: %v", err)
		}
	})

	t.Run("ErrDefaultBranchImmutable", func(t *testing.T) {
		defBranch := &domain.Branch{ID: "br-def", UserID: userID, IsDefault: true}
		if err := store.CreateBranch(ctx, defBranch); err != nil {
			t.Fatalf("create branch: %v", err)
		}
		err := store.DeleteBranch(ctx, userID, defBranch.ID)
		if !errors.Is(err, domain.ErrDefaultBranchImmutable) {
			t.Errorf("expected ErrDefaultBranchImmutable on deleting default branch, got: %v", err)
		}
	})

	t.Run("ErrValidation", func(t *testing.T) {
		if err := store.CreateJob(ctx, nil); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("expected ErrValidation on nil job, got: %v", err)
		}
		if err := store.CreatePreset(ctx, nil); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("expected ErrValidation on nil preset, got: %v", err)
		}
	})
}

func TestMemoryStore_QuotaLifecycle(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	userID := "quota-user-1"

	// 1. Initial GetQuota creates default quota
	q, err := store.GetQuota(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get quota: %v", err)
	}
	if q.DailyRequestsUsed != 0 || q.MonthlyTokensUsed != 0 {
		t.Fatalf("expected 0 usage, got daily=%d monthly=%d", q.DailyRequestsUsed, q.MonthlyTokensUsed)
	}

	// 2. ReserveQuota success
	rec, err := store.ReserveQuota(ctx, userID, domain.OperationResumeTailoring, 1500, 2, 5000)
	if err != nil {
		t.Fatalf("failed to reserve quota: %v", err)
	}
	if rec.Status != domain.ReservationStatusPending || rec.EstimatedTokens != 1500 {
		t.Fatalf("unexpected record: %+v", rec)
	}

	// 3. CommitUsage
	usage := domain.TokenUsage{
		PromptTokens:     500,
		CompletionTokens: 1200,
		TotalTokens:      1700,
	}
	if err := store.CommitUsage(ctx, rec.ID, usage); err != nil {
		t.Fatalf("failed to commit usage: %v", err)
	}

	// Verify committed record
	records, err := store.ListUsageRecords(ctx, userID, 10)
	if err != nil {
		t.Fatalf("failed to list usage records: %v", err)
	}
	if len(records) != 1 || records[0].Status != domain.ReservationStatusCommitted || records[0].TotalTokens != 1700 {
		t.Fatalf("unexpected records: %+v", records)
	}

	// 4. Reserve second quota reaching daily limit
	rec2, err := store.ReserveQuota(ctx, userID, domain.OperationGapAnalysis, 1000, 2, 5000)
	if err != nil {
		t.Fatalf("failed to reserve second quota: %v", err)
	}

	// 5. Third reservation fails with ErrQuotaExceeded
	_, err = store.ReserveQuota(ctx, userID, domain.OperationPDFParse, 500, 2, 5000)
	if !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded on exceeding daily request limit, got %v", err)
	}

	// 6. Release second quota
	if err := store.ReleaseQuota(ctx, rec2.ID, "cancelled"); err != nil {
		t.Fatalf("failed to release quota: %v", err)
	}

	// 7. Verify reservation can now succeed after release
	rec3, err := store.ReserveQuota(ctx, userID, domain.OperationPDFParse, 500, 2, 5000)
	if err != nil {
		t.Fatalf("expected reservation to succeed after release, got %v", err)
	}
	if rec3.ID == "" {
		t.Fatal("expected non-empty reservation ID")
	}

	// 8. Releasing non-existent reservation returns ErrNotFound
	if err := store.ReleaseQuota(ctx, "non-existent-id", "err"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown reservation, got %v", err)
	}
}

func TestMemoryStore_GlobalSkillsAndImmutability(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// 1. Seed global skill
	globalSkill := &domain.Skill{
		ID:       "global-golang",
		UserID:   "system",
		Name:     "Go",
		Slug:     "go",
		IsGlobal: true,
	}
	if err := store.CreateSkill(ctx, globalSkill); err != nil {
		t.Fatalf("failed to create global skill: %v", err)
	}

	// 2. User skill
	userSkill := &domain.Skill{
		ID:       "user-skill-1",
		UserID:   "user-a",
		Name:     "Docker",
		Slug:     "docker",
		IsGlobal: false,
	}
	if err := store.CreateSkill(ctx, userSkill); err != nil {
		t.Fatalf("failed to create user skill: %v", err)
	}

	// 3. ListGlobalSkills returns global skills
	globals, err := store.ListGlobalSkills(ctx)
	if err != nil {
		t.Fatalf("failed to list global skills: %v", err)
	}
	if len(globals) != 1 || globals[0].ID != "global-golang" {
		t.Fatalf("expected 1 global skill, got %d (%+v)", len(globals), globals)
	}

	// 4. Deleting global skill returns ErrGlobalSkillImmutable
	if err := store.DeleteSkill(ctx, "user-a", "global-golang"); !errors.Is(err, domain.ErrGlobalSkillImmutable) {
		t.Fatalf("expected ErrGlobalSkillImmutable when deleting global skill, got %v", err)
	}

	// 5. Deleting another user's skill returns ErrForbidden
	if err := store.DeleteSkill(ctx, "user-b", "user-skill-1"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when deleting another tenant's skill, got %v", err)
	}

	// 6. Deleting non-existent skill returns ErrNotFound
	if err := store.DeleteSkill(ctx, "user-a", "unknown-skill"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown skill, got %v", err)
	}

	// 7. Legitimate owner can delete user skill
	if err := store.DeleteSkill(ctx, "user-a", "user-skill-1"); err != nil {
		t.Fatalf("expected success deleting own skill, got %v", err)
	}
}

func TestMemoryStore_CleanupCompletedJobs(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	completedAt := now.Add(-48 * time.Hour)
	oldCompleted := &domain.PDFParseJob{
		ID:          "job-old-completed",
		UserID:      "user-1",
		Status:      domain.PDFParseJobStatusCompleted,
		CreatedAt:   now.Add(-48 * time.Hour),
		UpdatedAt:   now.Add(-48 * time.Hour),
		CompletedAt: &completedAt,
	}
	oldFailed := &domain.PDFParseJob{
		ID:          "job-old-failed",
		UserID:      "user-1",
		Status:      domain.PDFParseJobStatusFailed,
		CreatedAt:   now.Add(-48 * time.Hour),
		UpdatedAt:   now.Add(-48 * time.Hour),
		CompletedAt: &completedAt,
	}
	recentCompleted := &domain.PDFParseJob{
		ID:        "job-recent-completed",
		UserID:    "user-1",
		Status:    domain.PDFParseJobStatusCompleted,
		CreatedAt: now.Add(-1 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
	}
	oldPending := &domain.PDFParseJob{
		ID:        "job-old-pending",
		UserID:    "user-1",
		Status:    domain.PDFParseJobStatusPending,
		CreatedAt: now.Add(-48 * time.Hour),
		UpdatedAt: now.Add(-48 * time.Hour),
	}

	for _, j := range []*domain.PDFParseJob{oldCompleted, oldFailed, recentCompleted, oldPending} {
		if err := store.CreateJob(ctx, j); err != nil {
			t.Fatalf("failed to create job %s: %v", j.ID, err)
		}
	}

	// Cleanup jobs older than 24 hours
	if err := store.CleanupCompletedJobs(ctx, "user-1", 24*time.Hour); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// Old pending and recent completed must remain
	if _, err := store.GetJob(ctx, "user-1", "job-old-pending"); err != nil {
		t.Errorf("expected old pending job to be preserved, got error: %v", err)
	}
	if _, err := store.GetJob(ctx, "user-1", "job-recent-completed"); err != nil {
		t.Errorf("expected recent completed job to be preserved, got error: %v", err)
	}
	// Old completed and failed must be gone
	if _, err := store.GetJob(ctx, "user-1", "job-old-completed"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected old completed job to be purged, got %v", err)
	}
	if _, err := store.GetJob(ctx, "user-1", "job-old-failed"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected old failed job to be purged, got %v", err)
	}
}

func TestMemoryStore_TemplateImmutability(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Seed built-in template
	builtIn := &domain.TemplateSet{
		ID:               "builtin-standard",
		UserID:           "system",
		Name:             "Builtin Standard",
		IsBuiltIn:        true,
		HTMLTemplate:     "<div>{{.Profile.FullName}}</div>",
		MarkdownTemplate: "# {{.Profile.FullName}}",
		TextTemplate:     "{{.Profile.FullName}}",
	}
	if err := store.CreateTemplate(ctx, builtIn); err != nil {
		t.Fatalf("failed to create built-in template: %v", err)
	}

	// User custom template
	custom := &domain.TemplateSet{
		ID:               "custom-tmpl-1",
		UserID:           "user-a",
		Name:             "Custom Template",
		IsBuiltIn:        false,
		HTMLTemplate:     "<div>{{.Profile.FullName}}</div>",
		MarkdownTemplate: "# {{.Profile.FullName}}",
		TextTemplate:     "{{.Profile.FullName}}",
	}
	if err := store.CreateTemplate(ctx, custom); err != nil {
		t.Fatalf("failed to create custom template: %v", err)
	}

	t.Run("Update built-in template returns ErrStrategyImmutable", func(t *testing.T) {
		mutation := *builtIn
		mutation.Name = "Poisoned Name"
		mutation.UserID = "user-a"
		if err := store.UpdateTemplate(ctx, &mutation); !errors.Is(err, domain.ErrStrategyImmutable) {
			t.Fatalf("expected ErrStrategyImmutable when updating built-in template, got %v", err)
		}
	})

	t.Run("Delete built-in template returns ErrStrategyImmutable", func(t *testing.T) {
		if err := store.DeleteTemplate(ctx, "user-a", "builtin-standard"); !errors.Is(err, domain.ErrStrategyImmutable) {
			t.Fatalf("expected ErrStrategyImmutable when deleting built-in template, got %v", err)
		}
	})

	t.Run("Update another user's template returns ErrForbidden", func(t *testing.T) {
		mutation := *custom
		mutation.UserID = "user-b"
		if err := store.UpdateTemplate(ctx, &mutation); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("expected ErrForbidden when updating another user's template, got %v", err)
		}
	})

	t.Run("Delete another user's template returns ErrForbidden", func(t *testing.T) {
		if err := store.DeleteTemplate(ctx, "user-b", "custom-tmpl-1"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("expected ErrForbidden when deleting another user's template, got %v", err)
		}
	})
}
