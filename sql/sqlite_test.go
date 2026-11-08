package sqlite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"strings"
	"sync"
	"testing"
	"time"

	"resumeee/internal/domain"
)

func setupTestDB(t *testing.T) (
	domain.ProfileRepository,
	domain.PresetRepository,
	domain.TemplateRepository,
	domain.BranchRepository,
) {
	t.Helper()
	// Using shared cache memory database for tests
	db, err := New("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}

	profileRepo := NewProfileRepository(db)
	presetRepo := NewPresetRepository(db)
	templateRepo := NewTemplateRepository(db)
	branchRepo := NewBranchRepository(db)

	return profileRepo, presetRepo, templateRepo, branchRepo
}

func TestDB_Initialization(t *testing.T) {
	db, err := New("file:init_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	var fkEnabled int
	if err := db.Raw("PRAGMA foreign_keys;").Scan(&fkEnabled).Error; err != nil {
		t.Fatalf("failed to check foreign_keys pragma: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("expected foreign_keys=1, got %d", fkEnabled)
	}
}

func TestProfileRepository_MainRevisionIncrements(t *testing.T) {
	ctx := context.Background()
	profileRepo, _, _, _ := setupTestDB(t)

	userID := fmt.Sprintf("user-%d", time.Now().UnixNano())

	// 1. Initial Profile Upsert
	profile := &domain.Profile{
		ID:       "prof-1",
		UserID:   userID,
		FullName: "Jane Doe",
		Email:    "jane@example.com",
	}
	if err := profileRepo.UpsertProfile(ctx, profile); err != nil {
		t.Fatalf("failed to upsert profile: %v", err)
	}

	p, err := profileRepo.GetProfile(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get profile: %v", err)
	}
	if p.MainRevision != 1 {
		t.Errorf("expected initial MainRevision=1, got %d", p.MainRevision)
	}

	// 2. Profile Update increments revision
	p.Headline = "Staff Engineer"
	if err := profileRepo.UpsertProfile(ctx, p); err != nil {
		t.Fatalf("failed to update profile: %v", err)
	}
	p, _ = profileRepo.GetProfile(ctx, userID)
	if p.MainRevision != 2 {
		t.Errorf("expected MainRevision=2 after profile update, got %d", p.MainRevision)
	}

	// 3. Create Skill increments revision
	skill := &domain.Skill{
		ID:       "skill-go",
		UserID:   userID,
		Name:     "Go",
		Category: "Languages",
	}
	if err := profileRepo.CreateSkill(ctx, skill); err != nil {
		t.Fatalf("failed to create skill: %v", err)
	}
	p, _ = profileRepo.GetProfile(ctx, userID)
	if p.MainRevision != 3 {
		t.Errorf("expected MainRevision=3 after CreateSkill, got %d", p.MainRevision)
	}

	// 4. Create WorkExperience increments revision
	exp := &domain.WorkExperience{
		ID:             "exp-1",
		UserID:         userID,
		Company:        "Acme Corp",
		RoleTitle:      "Lead Architect",
		EmploymentType: domain.EmploymentTypeFullTime,
		StartDate:      domain.NewDate(2022, 1, 1),
		Skills:         []domain.Skill{*skill},
		CompanyContact: &domain.ContactDetail{Type: domain.ContactTypeEmail, Value: "hr@acme.com"},
	}
	if err := profileRepo.CreateExperience(ctx, exp); err != nil {
		t.Fatalf("failed to create experience: %v", err)
	}
	p, _ = profileRepo.GetProfile(ctx, userID)
	if p.MainRevision != 4 {
		t.Errorf("expected MainRevision=4 after CreateExperience, got %d", p.MainRevision)
	}

	// 5. Create Fact increments revision
	fact := &domain.Fact{
		ID:        "fact-1",
		UserID:    userID,
		OwnerID:   exp.ID,
		OwnerType: domain.OwnerTypeWorkExperience,
		Statement: "Reduced latency by 45%",
		Impact:    "45% p99 latency reduction across 12 services",
		Skills:    []domain.Skill{*skill},
	}
	if err := profileRepo.CreateFact(ctx, fact); err != nil {
		t.Fatalf("failed to create fact: %v", err)
	}
	p, _ = profileRepo.GetProfile(ctx, userID)
	if p.MainRevision != 5 {
		t.Errorf("expected MainRevision=5 after CreateFact, got %d", p.MainRevision)
	}

	// 6. Delete Experience increments revision
	if err := profileRepo.DeleteExperience(ctx, userID, exp.ID); err != nil {
		t.Fatalf("failed to delete experience: %v", err)
	}
	p, _ = profileRepo.GetProfile(ctx, userID)
	if p.MainRevision != 6 {
		t.Errorf("expected MainRevision=6 after DeleteExperience, got %d", p.MainRevision)
	}
}

func TestProfileRepository_PolymorphicFactsAndSnapshot(t *testing.T) {
	ctx := context.Background()
	profileRepo, _, _, _ := setupTestDB(t)

	userID := fmt.Sprintf("user-snapshot-%d", time.Now().UnixNano())

	// Profile
	if err := profileRepo.UpsertProfile(ctx, &domain.Profile{
		ID:       "prof-snap",
		UserID:   userID,
		FullName: "Alex Developer",
	}); err != nil {
		t.Fatalf("failed to upsert profile: %v", err)
	}

	// Skill
	skillGo := domain.Skill{ID: "sk-go", UserID: userID, Name: "Go"}
	skillGorm := domain.Skill{ID: "sk-gorm", UserID: userID, Name: "GORM"}
	if err := profileRepo.CreateSkill(ctx, &skillGo); err != nil {
		t.Fatalf("failed to create skillGo: %v", err)
	}
	if err := profileRepo.CreateSkill(ctx, &skillGorm); err != nil {
		t.Fatalf("failed to create skillGorm: %v", err)
	}

	// Experience with polymorphic fact and skills
	exp := &domain.WorkExperience{
		ID:        "exp-snap",
		UserID:    userID,
		Company:   "Tech Co",
		RoleTitle: "Senior Engineer",
		StartDate: domain.NewDate(2023, 1, 1),
		Skills:    []domain.Skill{skillGo, skillGorm},
	}
	if err := profileRepo.CreateExperience(ctx, exp); err != nil {
		t.Fatalf("failed to create experience: %v", err)
	}

	expFact := &domain.Fact{
		ID:        "fact-exp",
		UserID:    userID,
		OwnerID:   exp.ID,
		OwnerType: domain.OwnerTypeWorkExperience,
		Statement: "Built high throughput pipeline",
		Skills:    []domain.Skill{skillGo},
	}
	if err := profileRepo.CreateFact(ctx, expFact); err != nil {
		t.Fatalf("failed to create expFact: %v", err)
	}

	// Project with polymorphic fact
	proj := &domain.Project{
		ID:        "proj-snap",
		UserID:    userID,
		Name:      "Resumeee Engine",
		StartDate: domain.DatePtr(domain.NewDate(2024, 1, 1)),
		Skills:    []domain.Skill{skillGo},
	}
	if err := profileRepo.CreateProject(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	projFact := &domain.Fact{
		ID:        "fact-proj",
		UserID:    userID,
		OwnerID:   proj.ID,
		OwnerType: domain.OwnerTypeProject,
		Statement: "Created zero-dependency SQLite backend",
		Skills:    []domain.Skill{skillGo, skillGorm},
	}
	if err := profileRepo.CreateFact(ctx, projFact); err != nil {
		t.Fatalf("failed to create projFact: %v", err)
	}

	// Education with polymorphic fact
	edu := &domain.Education{
		ID:          "edu-snap",
		UserID:      userID,
		Institution: "MIT",
		Degree:      "B.S.",
		Field:       "Computer Science",
	}
	if err := profileRepo.CreateEducation(ctx, edu); err != nil {
		t.Fatalf("failed to create education: %v", err)
	}

	eduFact := &domain.Fact{
		ID:        "fact-edu",
		UserID:    userID,
		OwnerID:   edu.ID,
		OwnerType: domain.OwnerTypeEducation,
		Statement: "Graduated with honors",
	}
	if err := profileRepo.CreateFact(ctx, eduFact); err != nil {
		t.Fatalf("failed to create eduFact: %v", err)
	}

	// Verify Aggregate Snapshot
	snap, err := profileRepo.GetSnapshot(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get snapshot: %v", err)
	}

	if snap.Profile.FullName != "Alex Developer" {
		t.Errorf("expected full name Alex Developer, got %s", snap.Profile.FullName)
	}
	if len(snap.Skills) != 2 {
		t.Errorf("expected 2 skills in snapshot, got %d", len(snap.Skills))
	}
	if len(snap.Experiences) != 1 || len(snap.Experiences[0].Facts) != 1 {
		t.Errorf("expected 1 experience with 1 fact, got %d exps and %d facts", len(snap.Experiences), len(snap.Experiences[0].Facts))
	}
	if len(snap.Projects) != 1 || len(snap.Projects[0].Facts) != 1 {
		t.Errorf("expected 1 project with 1 fact, got %d projs and %d facts", len(snap.Projects), len(snap.Projects[0].Facts))
	}
	if len(snap.Education) != 1 || len(snap.Education[0].Facts) != 1 {
		t.Errorf("expected 1 education with 1 fact, got %d edus and %d facts", len(snap.Education), len(snap.Education[0].Facts))
	}
}

func TestBranchRepository_SequentialVersionsAndRecovery(t *testing.T) {
	ctx := context.Background()
	_, _, _, branchRepo := setupTestDB(t)

	userID := fmt.Sprintf("user-branch-%d", time.Now().UnixNano())

	// Create Branch
	branch := &domain.Branch{
		ID:            "branch-1",
		UserID:        userID,
		Name:          "Backend Staff Role",
		TemplateSetID: "tmpl-1",
	}
	if err := branchRepo.CreateBranch(ctx, branch); err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	// Create sequential versions
	v1 := &domain.BranchVersion{
		ID:               "v-1",
		BranchID:         branch.ID,
		MainRevisionUsed: 1,
	}
	if err := branchRepo.CreateVersion(ctx, v1); err != nil {
		t.Fatalf("failed to create version 1: %v", err)
	}
	if v1.VersionNumber != 1 {
		t.Errorf("expected VersionNumber=1, got %d", v1.VersionNumber)
	}
	if v1.Status != domain.VersionStatusPending {
		t.Errorf("expected default Status=pending, got %s", v1.Status)
	}

	v2 := &domain.BranchVersion{
		ID:               "v-2",
		BranchID:         branch.ID,
		MainRevisionUsed: 2,
	}
	if err := branchRepo.CreateVersion(ctx, v2); err != nil {
		t.Fatalf("failed to create version 2: %v", err)
	}
	if v2.VersionNumber != 2 {
		t.Errorf("expected VersionNumber=2, got %d", v2.VersionNumber)
	}

	// Concurrent version creation preserves sequential integrity
	const concurrentWorkers = 5
	var wg sync.WaitGroup
	errCh := make(chan error, concurrentWorkers)

	for i := 0; i < concurrentWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			v := &domain.BranchVersion{
				ID:               fmt.Sprintf("v-conc-%d", idx),
				BranchID:         branch.ID,
				MainRevisionUsed: 2,
			}
			if err := branchRepo.CreateVersion(ctx, v); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent CreateVersion error: %v", err)
	}

	versions, err := branchRepo.ListVersions(ctx, branch.ID)
	if err != nil {
		t.Fatalf("failed to list versions: %v", err)
	}
	// Total versions: v1, v2 + 5 concurrent = 7
	if len(versions) != 7 {
		t.Errorf("expected 7 versions, got %d", len(versions))
	}

	// Verify numbers are unique and 1..7
	seen := make(map[int]bool)
	for _, v := range versions {
		if seen[v.VersionNumber] {
			t.Errorf("duplicate VersionNumber observed: %d", v.VersionNumber)
		}
		seen[v.VersionNumber] = true
	}

	// Test FailInterruptedVersions
	if err := branchRepo.FailInterruptedVersions(ctx, "server restarted"); err != nil {
		t.Fatalf("failed to execute FailInterruptedVersions: %v", err)
	}

	updatedV1, err := branchRepo.GetVersion(ctx, branch.ID, "v-1")
	if err != nil {
		t.Fatalf("failed to get v-1: %v", err)
	}
	if updatedV1.Status != domain.VersionStatusFailed {
		t.Errorf("expected v-1 status to be failed, got %s", updatedV1.Status)
	}
	if updatedV1.ErrorMessage != "server restarted" {
		t.Errorf("expected error message 'server restarted', got '%s'", updatedV1.ErrorMessage)
	}
}

func TestPresetAndTemplateRepositories(t *testing.T) {
	ctx := context.Background()
	_, presetRepo, templateRepo, _ := setupTestDB(t)

	userID := fmt.Sprintf("user-config-%d", time.Now().UnixNano())

	// Template Set CRUD
	tmpl := &domain.TemplateSet{
		ID:               "tmpl-clean",
		UserID:           userID,
		Name:             "Standard Clean",
		HTMLTemplate:     "<h1>{{.FullName}}</h1>",
		MarkdownTemplate: "# {{.FullName}}",
		TextTemplate:     "{{.FullName}}",
	}
	if err := templateRepo.CreateTemplate(ctx, tmpl); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	gotTmpl, err := templateRepo.GetTemplate(ctx, userID, tmpl.ID)
	if err != nil {
		t.Fatalf("failed to get template: %v", err)
	}
	if gotTmpl.Name != "Standard Clean" {
		t.Errorf("expected template name 'Standard Clean', got %s", gotTmpl.Name)
	}

	// Preset CRUD
	preset := &domain.Preset{
		ID:                 "preset-tech",
		UserID:             userID,
		Name:               "1-Page Tech Resume",
		SystemInstructions: "Focus on distributed systems and quantitative impact.",
		TemplateSetID:      tmpl.ID,
	}
	if err := presetRepo.CreatePreset(ctx, preset); err != nil {
		t.Fatalf("failed to create preset: %v", err)
	}

	gotPreset, err := presetRepo.GetPreset(ctx, userID, preset.ID)
	if err != nil {
		t.Fatalf("failed to get preset: %v", err)
	}
	if gotPreset.Name != "1-Page Tech Resume" {
		t.Errorf("expected preset name '1-Page Tech Resume', got %s", gotPreset.Name)
	}
	if gotPreset.Language != domain.DefaultLanguage {
		t.Errorf("expected default preset language %q, got %q", domain.DefaultLanguage, gotPreset.Language)
	}

	// Preset with explicit language
	presetES := &domain.Preset{
		ID:                 "preset-tech-es",
		UserID:             userID,
		Name:               "1-Page Tech Resume (Spanish)",
		SystemInstructions: "Focus on distributed systems in Spanish.",
		TemplateSetID:      tmpl.ID,
		Language:           "Spanish",
	}
	if err := presetRepo.CreatePreset(ctx, presetES); err != nil {
		t.Fatalf("failed to create Spanish preset: %v", err)
	}
	gotPresetES, err := presetRepo.GetPreset(ctx, userID, presetES.ID)
	if err != nil {
		t.Fatalf("failed to get Spanish preset: %v", err)
	}
	if gotPresetES.Language != "Spanish" {
		t.Errorf("expected preset language 'Spanish', got %s", gotPresetES.Language)
	}

	// Verify NotFound mapping
	_, err = presetRepo.GetPreset(ctx, userID, "non-existent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-existent preset, got %v", err)
	}
}

func TestProfileRepository_ReplaceCareerGraph(t *testing.T) {
	ctx := context.Background()
	profileRepo, _, _, _ := setupTestDB(t)

	userID := fmt.Sprintf("user-replace-%d", time.Now().UnixNano())

	snapshot := &domain.MainProfileSnapshot{
		Profile: domain.Profile{
			ID:           "prof-repl",
			UserID:       userID,
			FullName:     "Original Name",
			MainRevision: 10,
		},
		MainRevision: 10,
		Skills: []domain.Skill{
			{ID: "sk-1", UserID: userID, Name: "Python"},
			{ID: "sk-2", UserID: userID, Name: "Rust"},
		},
		Experiences: []domain.WorkExperience{
			{
				ID:        "exp-1",
				UserID:    userID,
				Company:   "Startup Inc",
				RoleTitle: "Founding Engineer",
				StartDate: domain.NewDate(2020, 1, 1),
			},
		},
		Projects: []domain.Project{
			{
				ID:        "proj-1",
				UserID:    userID,
				Name:      "Distributed Store",
				StartDate: domain.DatePtr(domain.NewDate(2021, 1, 1)),
			},
		},
		Education: []domain.Education{
			{
				ID:          "edu-1",
				UserID:      userID,
				Institution: "Stanford",
				Degree:      "M.S.",
				Field:       "Computer Science",
			},
		},
		Certifications: []domain.Certification{
			{
				ID:     "cert-1",
				UserID: userID,
				Name:   "CKA",
			},
		},
	}

	if err := profileRepo.ReplaceCareerGraph(ctx, userID, snapshot); err != nil {
		t.Fatalf("failed to replace career graph: %v", err)
	}

	snapAfter, err := profileRepo.GetSnapshot(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get snapshot after replace: %v", err)
	}

	if snapAfter.MainRevision != 10 {
		t.Errorf("expected MainRevision 10, got %d", snapAfter.MainRevision)
	}
	if len(snapAfter.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(snapAfter.Skills))
	}
	if len(snapAfter.Experiences) != 1 {
		t.Errorf("expected 1 experience, got %d", len(snapAfter.Experiences))
	}
}

func TestProfileRepository_ReplaceCareerGraph_WithNestedSkills(t *testing.T) {
	ctx := context.Background()
	profileRepo, _, _, _ := setupTestDB(t)

	userID := fmt.Sprintf("user-replace-nested-%d", time.Now().UnixNano())

	skillTop := domain.Skill{ID: "sk-top", UserID: userID, Name: "Go", Category: "Languages"}
	skillExp := domain.Skill{ID: "sk-exp", UserID: userID, Name: "PostgreSQL", Category: "Databases"}
	skillFact := domain.Skill{ID: "sk-fact", UserID: userID, Name: "Kafka", Category: "Messaging"}
	skillProj := domain.Skill{ID: "sk-proj", UserID: userID, Name: "React", Category: "Frontend"}

	snapshot := &domain.MainProfileSnapshot{
		Profile: domain.Profile{
			ID:           "prof-nested",
			UserID:       userID,
			FullName:     "Nested Tester",
			MainRevision: 1,
		},
		MainRevision: 1,
		Skills:         []domain.Skill{skillTop}, // Notice: only sk-top is in snapshot.Skills initially
		Experiences: []domain.WorkExperience{
			{
				ID:        "exp-nested-1",
				UserID:    userID,
				Company:   "Tech Corp",
				RoleTitle: "Backend Lead",
				StartDate: domain.NewDate(2020, 1, 1),
				Skills:    []domain.Skill{skillTop, skillExp},
				Facts: []domain.Fact{
					{
						ID:        "fact-nested-1",
						UserID:    userID,
						OwnerID:   "exp-nested-1",
						OwnerType: domain.OwnerTypeWorkExperience,
						Statement: "Scaled message queue pipeline",
						Skills:    []domain.Skill{skillFact},
					},
				},
			},
		},
		Projects: []domain.Project{
			{
				ID:        "proj-nested-1",
				UserID:    userID,
				Name:      "Web Console",
				StartDate: domain.DatePtr(domain.NewDate(2021, 1, 1)),
				Skills:    []domain.Skill{skillProj},
			},
		},
	}

	if err := profileRepo.ReplaceCareerGraph(ctx, userID, snapshot); err != nil {
		t.Fatalf("failed to replace career graph with nested skills: %v", err)
	}

	snapAfter, err := profileRepo.GetSnapshot(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get snapshot: %v", err)
	}

	// All 4 skills should now be consolidated and present in the user's snapshot.Skills
	if len(snapAfter.Skills) != 4 {
		t.Fatalf("expected 4 consolidated skills in snapAfter.Skills, got %d", len(snapAfter.Skills))
	}

	// Verify experience has both skills linked
	if len(snapAfter.Experiences) != 1 {
		t.Fatalf("expected 1 experience, got %d", len(snapAfter.Experiences))
	}
	if len(snapAfter.Experiences[0].Skills) != 2 {
		t.Errorf("expected 2 skills on experience, got %d", len(snapAfter.Experiences[0].Skills))
	}

	// Verify fact has its skill linked
	if len(snapAfter.Experiences[0].Facts) != 1 {
		t.Fatalf("expected 1 fact on experience, got %d", len(snapAfter.Experiences[0].Facts))
	}
	if len(snapAfter.Experiences[0].Facts[0].Skills) != 1 || snapAfter.Experiences[0].Facts[0].Skills[0].ID != "sk-fact" {
		t.Errorf("expected skill sk-fact on fact, got %+v", snapAfter.Experiences[0].Facts[0].Skills)
	}

	// Verify project has skill linked
	if len(snapAfter.Projects) != 1 || len(snapAfter.Projects[0].Skills) != 1 || snapAfter.Projects[0].Skills[0].ID != "sk-proj" {
		t.Errorf("expected skill sk-proj on project, got %+v", snapAfter.Projects[0].Skills)
	}
}

func TestBranchRepository_LifecycleAndActiveVersion(t *testing.T) {
	ctx := context.Background()
	profileRepo, _, _, branchRepo := setupTestDB(t)

	userID := fmt.Sprintf("user-branch-life-%d", time.Now().UnixNano())

	// Profile with revision 5
	_ = profileRepo.UpsertProfile(ctx, &domain.Profile{
		ID:           "p-life",
		UserID:       userID,
		FullName:     "Branch Tester",
		MainRevision: 5,
	})

	// Create Branch
	b := &domain.Branch{
		ID:            "br-life",
		UserID:        userID,
		Name:          "Frontend Staff Engineer",
		TemplateSetID: "tmpl-1",
	}
	if err := branchRepo.CreateBranch(ctx, b); err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	// Verify staleness is true since LastSyncedMainRevision=0 < Profile.MainRevision=5
	fetchedBranch, err := branchRepo.GetBranch(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("failed to get branch: %v", err)
	}
	if !fetchedBranch.IsStale {
		t.Errorf("expected branch to be stale when LastSyncedMainRevision=0 and Profile=5")
	}

	// Create Version
	v := &domain.BranchVersion{
		ID:               "ver-life-1",
		BranchID:         b.ID,
		MainRevisionUsed: 5,
		Status:           domain.VersionStatusProcessing,
	}
	if err := branchRepo.CreateVersion(ctx, v); err != nil {
		t.Fatalf("failed to create version: %v", err)
	}

	// Complete Version
	v.Status = domain.VersionStatusCompleted
	v.HTMLOutput = "<h1>Resume</h1>"
	if err := branchRepo.CompleteVersion(ctx, v); err != nil {
		t.Fatalf("failed to complete version: %v", err)
	}

	// Check Branch updated
	updatedBranch, err := branchRepo.GetBranch(ctx, userID, b.ID)
	if err != nil {
		t.Fatalf("failed to get updated branch: %v", err)
	}
	if updatedBranch.ActiveVersionID == nil || *updatedBranch.ActiveVersionID != v.ID {
		t.Errorf("expected ActiveVersionID to be %s", v.ID)
	}
	if updatedBranch.LastSyncedMainRevision != 5 {
		t.Errorf("expected LastSyncedMainRevision to be 5, got %d", updatedBranch.LastSyncedMainRevision)
	}
	if updatedBranch.IsStale {
		t.Errorf("expected branch to NOT be stale once synced with revision 5")
	}

	// Test GetVersionByNumber
	vByNum, err := branchRepo.GetVersionByNumber(ctx, b.ID, 1)
	if err != nil {
		t.Fatalf("failed to get version by number: %v", err)
	}
	if vByNum.ID != v.ID {
		t.Errorf("expected version ID %s, got %s", v.ID, vByNum.ID)
	}

	// Delete branch
	if err := branchRepo.DeleteBranch(ctx, userID, b.ID); err != nil {
		t.Fatalf("failed to delete branch: %v", err)
	}
	_, err = branchRepo.GetBranch(ctx, userID, b.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound after branch deletion, got %v", err)
	}
}

func TestProfileRepository_FullEntityCRUD(t *testing.T) {
	ctx := context.Background()
	profileRepo, _, _, _ := setupTestDB(t)
	userID := fmt.Sprintf("user-crud-%d", time.Now().UnixNano())

	// Initialize Profile
	_ = profileRepo.UpsertProfile(ctx, &domain.Profile{
		ID:       "prof-crud",
		UserID:   userID,
		FullName: "CRUD User",
	})

	// 1. IncrementMainRevision directly
	rev, err := profileRepo.IncrementMainRevision(ctx, userID)
	if err != nil {
		t.Fatalf("failed to direct increment main revision: %v", err)
	}
	if rev != 2 {
		t.Errorf("expected revision 2, got %d", rev)
	}

	// 2. Skill CRUD
	sk := &domain.Skill{ID: "sk-c", UserID: userID, Name: "Docker"}
	if err := profileRepo.CreateSkill(ctx, sk); err != nil {
		t.Fatalf("create skill failed: %v", err)
	}
	skills, err := profileRepo.ListSkills(ctx, userID)
	if err != nil || len(skills) != 1 {
		t.Fatalf("list skills failed: %v, len=%d", err, len(skills))
	}
	fetchedSk, err := profileRepo.GetSkill(ctx, userID, sk.ID)
	if err != nil || fetchedSk.Name != "Docker" {
		t.Fatalf("get skill failed: %v", err)
	}
	sk.Name = "Docker & Containers"
	if err := profileRepo.UpdateSkill(ctx, sk); err != nil {
		t.Fatalf("update skill failed: %v", err)
	}
	if err := profileRepo.DeleteSkill(ctx, userID, sk.ID); err != nil {
		t.Fatalf("delete skill failed: %v", err)
	}
	if err := profileRepo.DeleteSkill(ctx, userID, "non-existent"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound deleting non-existent skill, got %v", err)
	}

	// 3. Project CRUD
	proj := &domain.Project{ID: "proj-c", UserID: userID, Name: "Project C"}
	if err := profileRepo.CreateProject(ctx, proj); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projs, err := profileRepo.ListProjects(ctx, userID)
	if err != nil || len(projs) != 1 {
		t.Fatalf("list projects failed: %v, len=%d", err, len(projs))
	}
	fetchedProj, err := profileRepo.GetProject(ctx, userID, proj.ID)
	if err != nil || fetchedProj.Name != "Project C" {
		t.Fatalf("get project failed: %v", err)
	}
	proj.Name = "Project C Updated"
	if err := profileRepo.UpdateProject(ctx, proj); err != nil {
		t.Fatalf("update project failed: %v", err)
	}
	if err := profileRepo.DeleteProject(ctx, userID, proj.ID); err != nil {
		t.Fatalf("delete project failed: %v", err)
	}
	if err := profileRepo.DeleteProject(ctx, userID, "non-existent"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound deleting non-existent project, got %v", err)
	}

	// 4. Education CRUD
	edu := &domain.Education{ID: "edu-c", UserID: userID, Institution: "State College"}
	if err := profileRepo.CreateEducation(ctx, edu); err != nil {
		t.Fatalf("create edu failed: %v", err)
	}
	edus, err := profileRepo.ListEducation(ctx, userID)
	if err != nil || len(edus) != 1 {
		t.Fatalf("list edus failed: %v, len=%d", err, len(edus))
	}
	fetchedEdu, err := profileRepo.GetEducation(ctx, userID, edu.ID)
	if err != nil || fetchedEdu.Institution != "State College" {
		t.Fatalf("get edu failed: %v", err)
	}
	edu.Institution = "State University"
	if err := profileRepo.UpdateEducation(ctx, edu); err != nil {
		t.Fatalf("update edu failed: %v", err)
	}
	if err := profileRepo.DeleteEducation(ctx, userID, edu.ID); err != nil {
		t.Fatalf("delete edu failed: %v", err)
	}
	if err := profileRepo.DeleteEducation(ctx, userID, "non-existent"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound deleting non-existent edu, got %v", err)
	}

	// 5. Certification CRUD
	cert := &domain.Certification{ID: "cert-c", UserID: userID, Name: "AWS SAA"}
	if err := profileRepo.CreateCertification(ctx, cert); err != nil {
		t.Fatalf("create cert failed: %v", err)
	}
	certs, err := profileRepo.ListCertifications(ctx, userID)
	if err != nil || len(certs) != 1 {
		t.Fatalf("list certs failed: %v, len=%d", err, len(certs))
	}
	fetchedCert, err := profileRepo.GetCertification(ctx, userID, cert.ID)
	if err != nil || fetchedCert.Name != "AWS SAA" {
		t.Fatalf("get cert failed: %v", err)
	}
	cert.Name = "AWS Solutions Architect Associate"
	if err := profileRepo.UpdateCertification(ctx, cert); err != nil {
		t.Fatalf("update cert failed: %v", err)
	}
	if err := profileRepo.DeleteCertification(ctx, userID, cert.ID); err != nil {
		t.Fatalf("delete cert failed: %v", err)
	}
	if err := profileRepo.DeleteCertification(ctx, userID, "non-existent"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound deleting non-existent cert, got %v", err)
	}

	// 6. Experience & Facts CRUD
	exp := &domain.WorkExperience{
		ID:        "exp-c",
		UserID:    userID,
		Company:   "Startup Labs",
		RoleTitle: "Engineer",
		StartDate: domain.NewDate(2021, 1, 1),
	}
	if err := profileRepo.CreateExperience(ctx, exp); err != nil {
		t.Fatalf("create exp failed: %v", err)
	}
	exps, err := profileRepo.ListExperiences(ctx, userID)
	if err != nil || len(exps) != 1 {
		t.Fatalf("list exps failed: %v, len=%d", err, len(exps))
	}
	fetchedExp, err := profileRepo.GetExperience(ctx, userID, exp.ID)
	if err != nil || fetchedExp.Company != "Startup Labs" {
		t.Fatalf("get exp failed: %v", err)
	}
	exp.RoleTitle = "Senior Engineer"
	if err := profileRepo.UpdateExperience(ctx, exp); err != nil {
		t.Fatalf("update exp failed: %v", err)
	}

	fact := &domain.Fact{
		ID:        "fact-c",
		UserID:    userID,
		OwnerID:   exp.ID,
		OwnerType: domain.OwnerTypeWorkExperience,
		Statement: "Optimized DB queries",
	}
	if err := profileRepo.CreateFact(ctx, fact); err != nil {
		t.Fatalf("create fact failed: %v", err)
	}
	facts, err := profileRepo.ListFactsByOwner(ctx, userID, string(domain.OwnerTypeWorkExperience), exp.ID)
	if err != nil || len(facts) != 1 {
		t.Fatalf("list facts failed: %v, len=%d", err, len(facts))
	}
	fetchedFact, err := profileRepo.GetFact(ctx, userID, fact.ID)
	if err != nil || fetchedFact.Statement != "Optimized DB queries" {
		t.Fatalf("get fact failed: %v", err)
	}
	fact.Statement = "Optimized SQLite indexes"
	if err := profileRepo.UpdateFact(ctx, fact); err != nil {
		t.Fatalf("update fact failed: %v", err)
	}
	if err := profileRepo.DeleteFact(ctx, userID, fact.ID); err != nil {
		t.Fatalf("delete fact failed: %v", err)
	}
	if err := profileRepo.DeleteFact(ctx, userID, "non-existent"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound deleting non-existent fact, got %v", err)
	}
	_ = profileRepo.DeleteExperience(ctx, userID, exp.ID)
}

func TestPresetTemplateBranch_ExtendedCRUD(t *testing.T) {
	ctx := context.Background()
	_, presetRepo, templateRepo, branchRepo := setupTestDB(t)
	userID := fmt.Sprintf("user-ext-%d", time.Now().UnixNano())

	// Preset
	p := &domain.Preset{
		ID:                 "p-ext",
		UserID:             userID,
		Name:               "Preset Ext",
		SystemInstructions: "Instructions",
		TemplateSetID:      "tmpl-ext",
	}
	_ = presetRepo.CreatePreset(ctx, p)
	presets, err := presetRepo.ListPresets(ctx, userID)
	if err != nil || len(presets) != 1 {
		t.Fatalf("list presets failed: %v", err)
	}
	p.Name = "Preset Ext Updated"
	if err := presetRepo.UpdatePreset(ctx, p); err != nil {
		t.Fatalf("update preset failed: %v", err)
	}
	if err := presetRepo.DeletePreset(ctx, userID, p.ID); err != nil {
		t.Fatalf("delete preset failed: %v", err)
	}
	if err := presetRepo.DeletePreset(ctx, userID, "non-existent"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-existent preset, got %v", err)
	}

	// Template
	tmpl := &domain.TemplateSet{
		ID:               "tmpl-ext",
		UserID:           userID,
		Name:             "Tmpl Ext",
		HTMLTemplate:     "<div>{{.FullName}}</div>",
		MarkdownTemplate: "{{.FullName}}",
		TextTemplate:     "{{.FullName}}",
	}
	_ = templateRepo.CreateTemplate(ctx, tmpl)
	tmpls, err := templateRepo.ListTemplates(ctx, userID)
	if err != nil || len(tmpls) != 1 {
		t.Fatalf("list templates failed: %v", err)
	}
	tmpl.Name = "Tmpl Ext Updated"
	if err := templateRepo.UpdateTemplate(ctx, tmpl); err != nil {
		t.Fatalf("update template failed: %v", err)
	}
	if err := templateRepo.DeleteTemplate(ctx, userID, tmpl.ID); err != nil {
		t.Fatalf("delete template failed: %v", err)
	}
	if err := templateRepo.DeleteTemplate(ctx, userID, "non-existent"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-existent template, got %v", err)
	}

	// Branch
	br := &domain.Branch{
		ID:            "br-ext",
		UserID:        userID,
		Name:          "Branch Ext",
		TemplateSetID: "tmpl-ext",
	}
	_ = branchRepo.CreateBranch(ctx, br)
	branches, err := branchRepo.ListBranches(ctx, userID)
	if err != nil || len(branches) != 1 {
		t.Fatalf("list branches failed: %v", err)
	}
	br.Name = "Branch Ext Updated"
	if err := branchRepo.UpdateBranch(ctx, br); err != nil {
		t.Fatalf("update branch failed: %v", err)
	}

	// Version status
	v := &domain.BranchVersion{
		ID:       "v-ext",
		BranchID: br.ID,
	}
	_ = branchRepo.CreateVersion(ctx, v)
	if err := branchRepo.UpdateVersionStatus(ctx, v.ID, domain.VersionStatusCompleted, ""); err != nil {
		t.Fatalf("update version status failed: %v", err)
	}
	if err := branchRepo.UpdateVersionStatus(ctx, "non-existent", domain.VersionStatusFailed, "err"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-existent version status, got %v", err)
	}
}

func TestTemplateRepository_SecurityAndSanitization(t *testing.T) {
	ctx := context.Background()
	_, _, templateRepo, _ := setupTestDB(t)
	userID := fmt.Sprintf("user-sec-%d", time.Now().UnixNano())

	// 1. Template with invalid / unclosed syntax must fail with ErrTemplateSyntax
	invalidTmpl := &domain.TemplateSet{
		ID:           "tmpl-malformed",
		UserID:       userID,
		Name:         "Malformed Template",
		HTMLTemplate: "<h1>{{.FullName", // Unclosed action tag
	}
	err := templateRepo.CreateTemplate(ctx, invalidTmpl)
	if !errors.Is(err, domain.ErrTemplateSyntax) {
		t.Errorf("expected ErrTemplateSyntax for unclosed HTML action tag, got %v", err)
	}

	// 2. Template calling non-whitelisted / dangerous functions must fail with ErrTemplateSyntax
	dangerousTmpl := &domain.TemplateSet{
		ID:           "tmpl-dangerous",
		UserID:       userID,
		Name:         "Dangerous Template",
		HTMLTemplate: "<h1>{{exec .Command}}</h1>",
	}
	err = templateRepo.CreateTemplate(ctx, dangerousTmpl)
	if !errors.Is(err, domain.ErrTemplateSyntax) {
		t.Errorf("expected ErrTemplateSyntax for non-whitelisted function call, got %v", err)
	}

	// 3. Valid template with context-aware auto-escaping of user-supplied content
	safeTmpl := &domain.TemplateSet{
		ID:           "tmpl-safe",
		UserID:       userID,
		Name:         "Safe Escaped Template",
		HTMLTemplate: `<div class="user"><span id="name">{{.FullName}}</span><a href="{{.Website}}">Profile</a></div>`,
	}
	if err := templateRepo.CreateTemplate(ctx, safeTmpl); err != nil {
		t.Fatalf("failed to create safe template: %v", err)
	}

	// Verify rendering with html/template context auto-escapes XSS payloads
	parsed, err := testRenderHTML(safeTmpl.HTMLTemplate, struct {
		FullName string
		Website  string
	}{
		FullName: `<script>alert("xss")</script>`,
		Website:  `javascript:alert('xss')`,
	})
	if err != nil {
		t.Fatalf("failed to test-render safe template: %v", err)
	}

	// html/template converts <script> in HTML context to &lt;script&gt; and javascript: in URL context to #ZgotmplZ
	if strings.Contains(parsed, "<script>") {
		t.Errorf("expected HTML context to escape raw <script>, got raw string in: %s", parsed)
	}
	if strings.Contains(parsed, "javascript:") {
		t.Errorf("expected URL context to sanitize javascript: scheme, got: %s", parsed)
	}
}

func testRenderHTML(tmplStr string, data any) (string, error) {
	t, err := htmltemplate.New("html").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func TestBranchRepository_VersionCompositeUniqueConstraint(t *testing.T) {
	ctx := context.Background()
	_, _, _, branchRepo := setupTestDB(t)

	db, err := New("file:unique_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	branchID := "branch-unique-1"
	b := &domain.Branch{
		ID:            branchID,
		UserID:        "user-u1",
		Name:          "Unique Test Branch",
		TemplateSetID: "tmpl-1",
	}
	if err := db.WithContext(ctx).Create(b).Error; err != nil {
		t.Fatalf("failed to create branch: %v", err)
	}

	v1 := &domain.BranchVersion{
		ID:            "ver-u1-1",
		BranchID:      branchID,
		VersionNumber: 1,
		Status:        domain.VersionStatusCompleted,
	}
	if err := db.WithContext(ctx).Create(v1).Error; err != nil {
		t.Fatalf("failed to create first version: %v", err)
	}

	// Attempting to insert a duplicate version_number for the same branch must fail
	vDuplicate := &domain.BranchVersion{
		ID:            "ver-u1-2",
		BranchID:      branchID,
		VersionNumber: 1,
		Status:        domain.VersionStatusCompleted,
	}
	if err := db.WithContext(ctx).Create(vDuplicate).Error; err == nil {
		t.Fatalf("expected unique constraint violation on duplicate (branch_id, version_number), got nil")
	}

	// Inserting version_number 1 for a DIFFERENT branch must succeed (composite uniqueness)
	branchID2 := "branch-unique-2"
	b2 := &domain.Branch{
		ID:            branchID2,
		UserID:        "user-u1",
		Name:          "Unique Test Branch 2",
		TemplateSetID: "tmpl-1",
	}
	if err := db.WithContext(ctx).Create(b2).Error; err != nil {
		t.Fatalf("failed to create branch 2: %v", err)
	}

	vDiffBranch := &domain.BranchVersion{
		ID:            "ver-u2-1",
		BranchID:      branchID2,
		VersionNumber: 1,
		Status:        domain.VersionStatusCompleted,
	}
	if err := db.WithContext(ctx).Create(vDiffBranch).Error; err != nil {
		t.Fatalf("expected version_number 1 on different branch to succeed, got: %v", err)
	}
	_ = branchRepo
}

func TestProfileRepository_AssociationSkillsPurge(t *testing.T) {
	ctx := context.Background()
	profileRepo, _, _, _ := setupTestDB(t)
	userID := fmt.Sprintf("user-purge-%d", time.Now().UnixNano())

	// Create 3 skills
	s1 := &domain.Skill{ID: fmt.Sprintf("s1-%d", time.Now().UnixNano()), UserID: userID, Name: "Go"}
	s2 := &domain.Skill{ID: fmt.Sprintf("s2-%d", time.Now().UnixNano()), UserID: userID, Name: "Rust"}
	s3 := &domain.Skill{ID: fmt.Sprintf("s3-%d", time.Now().UnixNano()), UserID: userID, Name: "Python"}
	_ = profileRepo.CreateSkill(ctx, s1)
	_ = profileRepo.CreateSkill(ctx, s2)
	_ = profileRepo.CreateSkill(ctx, s3)

	// Create Experience with 3 skills
	exp := &domain.WorkExperience{
		ID:        fmt.Sprintf("exp-purge-%d", time.Now().UnixNano()),
		UserID:    userID,
		Company:   "Acme",
		RoleTitle: "Engineer",
		StartDate: domain.NewDate(2020, 1, 1),
		Skills:    []domain.Skill{*s1, *s2, *s3},
	}
	if err := profileRepo.CreateExperience(ctx, exp); err != nil {
		t.Fatalf("failed to create experience: %v", err)
	}

	fetched, err := profileRepo.GetExperience(ctx, userID, exp.ID)
	if err != nil {
		t.Fatalf("failed to get experience: %v", err)
	}
	if len(fetched.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(fetched.Skills))
	}

	// Update Experience to only have 1 skill (s1)
	fetched.Skills = []domain.Skill{*s1}
	if err := profileRepo.UpdateExperience(ctx, fetched); err != nil {
		t.Fatalf("failed to update experience: %v", err)
	}

	refetched, err := profileRepo.GetExperience(ctx, userID, exp.ID)
	if err != nil {
		t.Fatalf("failed to refetch experience: %v", err)
	}
	if len(refetched.Skills) != 1 {
		t.Errorf("expected 1 skill remaining after update, got %d", len(refetched.Skills))
	}
	if len(refetched.Skills) > 0 && refetched.Skills[0].ID != s1.ID {
		t.Errorf("expected skill %s, got %s", s1.ID, refetched.Skills[0].ID)
	}

	// Create Project with 2 skills
	proj := &domain.Project{
		ID:     fmt.Sprintf("proj-purge-%d", time.Now().UnixNano()),
		UserID: userID,
		Name:   "P1",
		Skills: []domain.Skill{*s1, *s2},
	}
	if err := profileRepo.CreateProject(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}
	fetchedProj, _ := profileRepo.GetProject(ctx, userID, proj.ID)
	if len(fetchedProj.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(fetchedProj.Skills))
	}

	// Update Project to have 0 skills
	fetchedProj.Skills = []domain.Skill{}
	if err := profileRepo.UpdateProject(ctx, fetchedProj); err != nil {
		t.Fatalf("failed to update project: %v", err)
	}
	refetchedProj, _ := profileRepo.GetProject(ctx, userID, proj.ID)
	if len(refetchedProj.Skills) != 0 {
		t.Errorf("expected 0 skills remaining on project, got %d", len(refetchedProj.Skills))
	}
}

func TestProfileRepository_SkillResolution(t *testing.T) {
	ctx := context.Background()
	profileRepo, _, _, _ := setupTestDB(t)

	userID := fmt.Sprintf("user-skills-%d", time.Now().UnixNano())
	prof := &domain.Profile{
		ID:       fmt.Sprintf("prof-%d", time.Now().UnixNano()),
		UserID:   userID,
		FullName: "Skill Tester",
	}
	if err := profileRepo.UpsertProfile(ctx, prof); err != nil {
		t.Fatalf("failed to create main profile: %v", err)
	}

	// 1. Create Experience with raw skill names
	exp := &domain.WorkExperience{
		ID:        domain.GenerateID("exp"),
		ShortID:   domain.GenerateShortID("exp"),
		UserID:    userID,
		Company:   "Tech Innovators",
		RoleTitle: "Staff Engineer",
		StartDate: domain.NewDate(2023, 1, 1),
		Skills: []domain.Skill{
			{Name: "PostgreSQL"},
			{Name: "Go"},
		},
	}
	if err := profileRepo.CreateExperience(ctx, exp); err != nil {
		t.Fatalf("failed to create experience with raw skill names: %v", err)
	}

	fetchedExp, err := profileRepo.GetExperience(ctx, userID, exp.ID)
	if err != nil {
		t.Fatalf("failed to get experience: %v", err)
	}
	if len(fetchedExp.Skills) != 2 {
		t.Fatalf("expected 2 resolved skills, got %d", len(fetchedExp.Skills))
	}
	for _, s := range fetchedExp.Skills {
		if s.UserID != userID {
			t.Errorf("expected skill UserID %q, got %q", userID, s.UserID)
		}
		if s.Name == "" || strings.HasPrefix(s.Name, "skill-") {
			t.Errorf("expected human skill name, got %q", s.Name)
		}
		if s.Category != "General Skills" {
			t.Errorf("expected default category 'General Skills', got %q", s.Category)
		}
	}

	// 2. Create Project referencing one existing skill by case-insensitive name and one new skill
	proj := &domain.Project{
		ID:        domain.GenerateID("proj"),
		ShortID:   domain.GenerateShortID("proj"),
		UserID:    userID,
		Name:      "Cloud Pipeline",
		Skills: []domain.Skill{
			{Name: "go"}, // case-insensitive match with existing "Go"
			{Name: "Terraform"}, // new skill
		},
	}
	if err := profileRepo.CreateProject(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	fetchedProj, err := profileRepo.GetProject(ctx, userID, proj.ID)
	if err != nil {
		t.Fatalf("failed to get project: %v", err)
	}
	if len(fetchedProj.Skills) != 2 {
		t.Fatalf("expected 2 skills on project, got %d", len(fetchedProj.Skills))
	}

	// 3. Verify GetSnapshot consolidates skills and deduplicates
	snap, err := profileRepo.GetSnapshot(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get snapshot: %v", err)
	}
	// Total unique skills created: PostgreSQL, Go, Terraform = 3
	if len(snap.Skills) != 3 {
		t.Fatalf("expected 3 consolidated skills in snapshot, got %d", len(snap.Skills))
	}

	skillNames := make(map[string]bool)
	for _, s := range snap.Skills {
		skillNames[s.Name] = true
	}
	if !skillNames["PostgreSQL"] || !skillNames["Go"] || !skillNames["Terraform"] {
		t.Errorf("expected PostgreSQL, Go, and Terraform in snapshot, got: %+v", snap.Skills)
	}
}

func TestDB_SecondaryAndJoinTableIndexes(t *testing.T) {
	db, err := New("file:indexes_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	requiredIndexes := []string{
		"idx_experience_skills_skill_id",
		"idx_project_skills_skill_id",
		"idx_fact_skills_skill_id",
	}

	for _, idxName := range requiredIndexes {
		var count int
		query := "SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?"
		if err := db.Raw(query, idxName).Scan(&count).Error; err != nil {
			t.Fatalf("failed to query index %s: %v", idxName, err)
		}
		if count != 1 {
			t.Errorf("expected index %s to exist, count=%d", idxName, count)
		}
	}
}

func TestQuotaRepository_ReserveCommitRelease(t *testing.T) {
	ctx := context.Background()
	db, err := New("file:quota_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	quotaRepo := NewQuotaRepository(db)
	userID := "user-quota-1"

	// 1. Initial GetQuota returns default limits
	quota, err := quotaRepo.GetQuota(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get quota: %v", err)
	}
	if quota.DailyRequestsUsed != 0 || quota.MonthlyTokensUsed != 0 {
		t.Errorf("expected 0 used, got %+v", quota)
	}

	// 2. Reserve quota
	rec, err := quotaRepo.ReserveQuota(ctx, userID, domain.OperationResumeTailoring, 2000, 2, 5000)
	if err != nil {
		t.Fatalf("failed to reserve quota: %v", err)
	}
	if rec.EstimatedTokens != 2000 || rec.Status != domain.ReservationStatusPending {
		t.Errorf("unexpected reservation record: %+v", rec)
	}

	// 3. Second reservation succeeds
	rec2, err := quotaRepo.ReserveQuota(ctx, userID, domain.OperationGapAnalysis, 1000, 2, 5000)
	if err != nil {
		t.Fatalf("failed to reserve second quota: %v", err)
	}

	// 4. Third reservation exceeds daily limit (limit is 2)
	_, err = quotaRepo.ReserveQuota(ctx, userID, domain.OperationResumeTailoring, 500, 2, 5000)
	if !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got: %v", err)
	}

	// 5. Commit usage for rec1 with reconciliation
	usage := domain.TokenUsage{PromptTokens: 800, CompletionTokens: 1400, TotalTokens: 2200}
	if err := quotaRepo.CommitUsage(ctx, rec.ID, usage); err != nil {
		t.Fatalf("failed to commit usage: %v", err)
	}

	// 6. Release reservation for rec2 (e.g. failure)
	if err := quotaRepo.ReleaseQuota(ctx, rec2.ID, "llm timed out"); err != nil {
		t.Fatalf("failed to release quota: %v", err)
	}

	// 7. Verify updated quota after release: 1 daily request used, tokens reconciled
	quotaAfter, err := quotaRepo.GetQuota(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get quota: %v", err)
	}
	if quotaAfter.DailyRequestsUsed != 1 {
		t.Errorf("expected 1 daily request used after release, got %d", quotaAfter.DailyRequestsUsed)
	}
	if quotaAfter.MonthlyTokensUsed != 2200 {
		t.Errorf("expected 2200 tokens used after commit, got %d", quotaAfter.MonthlyTokensUsed)
	}
}

func TestGlobalSkills_ImmutabilityAndAccess(t *testing.T) {
	ctx := context.Background()
	db, err := New("file:globalskill_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	profileRepo := NewProfileRepository(db)
	systemUserID := "system"
	tenantUserID := "user-tenant-1"

	// 1. Create global skill as system
	globalSkill := &domain.Skill{
		ID:       "global-skill-rust",
		ShortID:  "g-rust",
		UserID:   systemUserID,
		Name:     "Rust",
		Slug:     "rust",
		IsGlobal: true,
	}
	if err := profileRepo.CreateSkill(ctx, globalSkill); err != nil {
		t.Fatalf("failed to create global skill: %v", err)
	}

	// 2. Tenant cannot create a skill with IsGlobal=true
	fakeGlobal := &domain.Skill{
		ID:       "fake-global-py",
		UserID:   tenantUserID,
		Name:     "Python",
		IsGlobal: true,
	}
	if err := profileRepo.CreateSkill(ctx, fakeGlobal); err == nil {
		t.Fatal("expected error when tenant attempts to create global skill, got nil")
	}

	// 3. Tenant can see global skill in ListSkills and GetSkill
	skills, err := profileRepo.ListSkills(ctx, tenantUserID)
	if err != nil {
		t.Fatalf("failed to list skills: %v", err)
	}
	found := false
	for _, s := range skills {
		if s.ID == globalSkill.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected global skill to be listed for tenant")
	}

	fetched, err := profileRepo.GetSkill(ctx, tenantUserID, globalSkill.ID)
	if err != nil {
		t.Fatalf("failed to get global skill as tenant: %v", err)
	}
	if fetched.Name != "Rust" {
		t.Errorf("expected 'Rust', got %q", fetched.Name)
	}

	// 4. Tenant cannot update global skill
	fetched.Name = "Rust 2024"
	if err := profileRepo.UpdateSkill(ctx, fetched); !errors.Is(err, domain.ErrGlobalSkillImmutable) {
		t.Fatalf("expected ErrGlobalSkillImmutable on update, got: %v", err)
	}

	// 5. Tenant cannot delete global skill
	if err := profileRepo.DeleteSkill(ctx, tenantUserID, globalSkill.ID); !errors.Is(err, domain.ErrGlobalSkillImmutable) {
		t.Fatalf("expected ErrGlobalSkillImmutable on delete, got: %v", err)
	}
}

func TestPDFParseJobRepository_CleanupCompletedJobs(t *testing.T) {
	ctx := context.Background()
	db, err := New("file:cleanup_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	repo := NewPDFParseJobRepository(db)
	userID := "user-cleanup"

	now := time.Now().UTC()
	oldTime := now.Add(-48 * time.Hour)

	oldJob := &domain.PDFParseJob{
		ID:          "job-old",
		UserID:      userID,
		Status:      domain.PDFParseJobStatusCompleted,
		CreatedAt:   oldTime,
		UpdatedAt:   oldTime,
		CompletedAt: &oldTime,
	}
	if err := repo.CreateJob(ctx, oldJob); err != nil {
		t.Fatalf("failed to create old job: %v", err)
	}

	newJob := &domain.PDFParseJob{
		ID:          "job-new",
		UserID:      userID,
		Status:      domain.PDFParseJobStatusCompleted,
		CreatedAt:   now,
		UpdatedAt:   now,
		CompletedAt: &now,
	}
	if err := repo.CreateJob(ctx, newJob); err != nil {
		t.Fatalf("failed to create new job: %v", err)
	}

	// Cleanup jobs older than 24 hours
	if err := repo.CleanupCompletedJobs(ctx, userID, 24*time.Hour); err != nil {
		t.Fatalf("failed to cleanup jobs: %v", err)
	}

	// Old job should be gone
	_, err = repo.GetJob(ctx, userID, "job-old")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for cleaned up job, got: %v", err)
	}

	// New job should still exist
	_, err = repo.GetJob(ctx, userID, "job-new")
	if err != nil {
		t.Errorf("expected new job to remain, got: %v", err)
	}
}

func TestBranchRepository_GapReportPropagation(t *testing.T) {
	ctx := context.Background()
	_, _, _, branchRepo := setupTestDB(t)

	userID := fmt.Sprintf("user-gap-%d", time.Now().UnixNano())
	score85 := 85.0

	t.Run("CompleteVersion propagates GapReport and match score to parent branch", func(t *testing.T) {
		b := &domain.Branch{
			ID:            "br-gap-prop",
			UserID:        userID,
			Name:          "Backend Staff Engineer",
			TemplateSetID: "tmpl-1",
		}
		if err := branchRepo.CreateBranch(ctx, b); err != nil {
			t.Fatalf("failed to create branch: %v", err)
		}

		v := &domain.BranchVersion{
			ID:               "ver-gap-1",
			BranchID:         b.ID,
			MainRevisionUsed: 1,
			Status:           domain.VersionStatusProcessing,
		}
		if err := branchRepo.CreateVersion(ctx, v); err != nil {
			t.Fatalf("failed to create version: %v", err)
		}

		v.Status = domain.VersionStatusCompleted
		v.GapReport = &domain.GapReport{
			OverallMatchSummary: "Strong candidate match with minor gap",
			MatchScore:          &score85,
			MissingRequirements: []domain.GapItem{
				{Requirement: "Kubernetes operator development", Importance: domain.GapImportanceCritical},
			},
		}

		if err := branchRepo.CompleteVersion(ctx, v); err != nil {
			t.Fatalf("failed to complete version: %v", err)
		}

		fetched, err := branchRepo.GetBranch(ctx, userID, b.ID)
		if err != nil {
			t.Fatalf("failed to get branch: %v", err)
		}
		if fetched.GapReport == nil {
			t.Fatalf("expected parent branch GapReport to be populated after CompleteVersion, got nil")
		}
		if fetched.GapReport.MatchScore == nil || *fetched.GapReport.MatchScore != score85 {
			t.Errorf("expected match score %f, got %v", score85, fetched.GapReport.MatchScore)
		}
		if len(fetched.GapReport.MissingRequirements) != 1 {
			t.Errorf("expected 1 missing requirement, got %d", len(fetched.GapReport.MissingRequirements))
		}
	})

	t.Run("CompleteVersion persists nil when version.GapReport is nil", func(t *testing.T) {
		b := &domain.Branch{
			ID:            "br-gap-nil",
			UserID:        userID,
			Name:          "Untailored Variant",
			TemplateSetID: "tmpl-1",
		}
		if err := branchRepo.CreateBranch(ctx, b); err != nil {
			t.Fatalf("failed to create branch: %v", err)
		}

		v := &domain.BranchVersion{
			ID:               "ver-gap-nil-1",
			BranchID:         b.ID,
			MainRevisionUsed: 1,
			Status:           domain.VersionStatusCompleted,
			GapReport:        nil,
		}
		if err := branchRepo.CreateVersion(ctx, v); err != nil {
			t.Fatalf("failed to create version: %v", err)
		}
		if err := branchRepo.CompleteVersion(ctx, v); err != nil {
			t.Fatalf("failed to complete version: %v", err)
		}

		fetched, err := branchRepo.GetBranch(ctx, userID, b.ID)
		if err != nil {
			t.Fatalf("failed to get branch: %v", err)
		}
		if fetched.GapReport != nil {
			t.Errorf("expected parent branch GapReport to be nil when version.GapReport is nil, got %+v", fetched.GapReport)
		}
		if fetched.ActiveVersionID == nil || *fetched.ActiveVersionID != v.ID {
			t.Errorf("expected active version ID %s, got %v", v.ID, fetched.ActiveVersionID)
		}
	})
}

