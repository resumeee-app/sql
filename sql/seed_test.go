package sql

import (
	"context"
	"testing"

	"github.com/resumeee-app/resumeee-core/domain"
)

func TestSeedDefaultData_FirstBoot(t *testing.T) {
	ctx := context.Background()
	db, err := New("file:seed_test_1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to initialize test database: %v", err)
	}

	if err := SeedDefaultDataDB(ctx, db); err != nil {
		t.Fatalf("SeedDefaultData failed: %v", err)
	}

	profileRepo := NewProfileRepository(db)
	templateRepo := NewTemplateRepository(db)
	presetRepo := NewPresetRepository(db)

	// 1. Verify default profile
	prof, err := profileRepo.GetProfile(ctx, "default")
	if err != nil {
		t.Fatalf("failed to get default profile: %v", err)
	}
	if prof.UserID != "default" {
		t.Errorf("expected UserID 'default', got %s", prof.UserID)
	}
	if prof.MainRevision != 1 {
		t.Errorf("expected MainRevision 1, got %d", prof.MainRevision)
	}

	// 2. Verify built-in template sets
	templates, err := templateRepo.ListTemplates(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list templates: %v", err)
	}
	expectedTemplateCount := len(domain.DefaultTemplateSets())
	if len(templates) != expectedTemplateCount || len(templates) != 5 {
		t.Fatalf("expected exactly %d built-in templates (count 5), got %d", expectedTemplateCount, len(templates))
	}

	tmplMap := make(map[string]domain.TemplateSet)
	for _, tmpl := range templates {
		tmplMap[tmpl.Name] = tmpl
		// Verify template syntax validity in domain
		if err := (domain.DefaultTemplateValidator{}).Validate(&tmpl); err != nil {
			t.Errorf("template %s has invalid syntax: %v", tmpl.Name, err)
		}
	}

	expectedTemplates := []struct {
		name string
		id   string
	}{
		{"Standard Clean", domain.TemplateStandardCleanID},
		{"Detailed Immigration / CV", domain.TemplateDetailedImmigrationCVID},
		{"ATS Minimal Plain-Text & Clean HTML", domain.TemplateATSMinimalTxtID},
		{"ATS Modern Tech", domain.TemplateATSHybridTechID},
		{"ATS Executive Clean", domain.TemplateATSExecutiveCleanID},
	}

	for _, exp := range expectedTemplates {
		tmpl, ok := tmplMap[exp.name]
		if !ok {
			t.Errorf("missing built-in template %q", exp.name)
			continue
		}
		if tmpl.ID != exp.id {
			t.Errorf("expected template %q to have ID %q, got %q", exp.name, exp.id, tmpl.ID)
		}
		if tmpl.HTMLTemplate == "" || tmpl.MarkdownTemplate == "" || tmpl.TextTemplate == "" {
			t.Errorf("expected %q to have HTML, Markdown, and PlainText templates", exp.name)
		}
	}

	// 3. Verify built-in presets
	presets, err := presetRepo.ListPresets(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list presets: %v", err)
	}
	if len(presets) < 2 {
		t.Errorf("expected at least 2 built-in presets, got %d", len(presets))
	}

	presetMap := make(map[string]domain.Preset)
	for _, p := range presets {
		presetMap[p.Name] = p
	}

	techPreset, ok := presetMap["1-Page Tech Resume"]
	if !ok {
		t.Errorf("missing built-in preset '1-Page Tech Resume'")
	} else if techPreset.SystemInstructions == "" || techPreset.TemplateSetID == "" {
		t.Errorf("'1-Page Tech Resume' preset missing instructions or template_set_id")
	}

	cvPreset, ok := presetMap["Immigration Full History CV"]
	if !ok {
		t.Errorf("missing built-in preset 'Immigration Full History CV'")
	} else if cvPreset.SystemInstructions == "" || cvPreset.TemplateSetID == "" {
		t.Errorf("'Immigration Full History CV' preset missing instructions or template_set_id")
	}

	// 4. Verify default branch seeded
	branchRepo := NewBranchRepository(db)
	branches, err := branchRepo.ListBranches(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list branches: %v", err)
	}
	if len(branches) == 0 {
		t.Fatalf("expected at least 1 branch (default branch), got 0")
	}
	defaultBranch := branches[0]
	if !defaultBranch.IsDefault {
		t.Errorf("expected first branch to have IsDefault=true, got %+v", defaultBranch)
	}
	if defaultBranch.ID != domain.DefaultBranchID("default") {
		t.Errorf("expected default branch ID %s, got %s", domain.DefaultBranchID("default"), defaultBranch.ID)
	}
	if defaultBranch.Name != domain.DefaultBranchName {
		t.Errorf("expected default branch name %s, got %s", domain.DefaultBranchName, defaultBranch.Name)
	}
	if defaultBranch.TemplateSetID != domain.TemplateStandardCleanID {
		t.Errorf("expected default branch template %s, got %s", domain.TemplateStandardCleanID, defaultBranch.TemplateSetID)
	}
	if defaultBranch.ActiveVersionID == nil || *defaultBranch.ActiveVersionID == "" {
		t.Fatalf("expected default branch to have an active version")
	}

	ver, err := branchRepo.GetVersion(ctx, defaultBranch.ID, *defaultBranch.ActiveVersionID)
	if err != nil {
		t.Fatalf("failed to get default branch active version: %v", err)
	}
	if ver.Status != domain.VersionStatusCompleted {
		t.Errorf("expected completed status, got %s", ver.Status)
	}
	if ver.SourceType != domain.SourceTypeMainProfile {
		t.Errorf("expected SourceType %s, got %s", domain.SourceTypeMainProfile, ver.SourceType)
	}
	if ver.ProviderUsed != "none" || ver.ModelUsed != "none" {
		t.Errorf("expected provider/model to be 'none', got provider=%s, model=%s", ver.ProviderUsed, ver.ModelUsed)
	}
	if ver.HTMLOutput == "" || ver.MarkdownOutput == "" || ver.PlainTextOutput == "" {
		t.Errorf("expected non-empty rendered outputs in initial default branch version")
	}
}

func TestSeedDefaultData_Idempotency(t *testing.T) {
	ctx := context.Background()
	db, err := New("file:seed_test_idempotent?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to initialize test database: %v", err)
	}

	// First seed
	if err := SeedDefaultDataDB(ctx, db); err != nil {
		t.Fatalf("first SeedDefaultData failed: %v", err)
	}

	profileRepo := NewProfileRepository(db)
	templateRepo := NewTemplateRepository(db)
	presetRepo := NewPresetRepository(db)

	// User modifies profile
	prof, err := profileRepo.GetProfile(ctx, "default")
	if err != nil {
		t.Fatalf("failed to fetch profile: %v", err)
	}
	prof.FullName = "Alice Custom Name"
	prof.Headline = "Distinguished Architect"
	prof.MainRevision = 42
	if err := db.Save(prof).Error; err != nil {
		t.Fatalf("failed to update profile directly: %v", err)
	}

	// Run seeder multiple consecutive times
	for i := 0; i < 3; i++ {
		if err := SeedDefaultDataDB(ctx, db); err != nil {
			t.Fatalf("subsequent SeedDefaultData (%d) failed: %v", i, err)
		}
	}

	// Verify profile content was NOT overwritten
	profAfter, err := profileRepo.GetProfile(ctx, "default")
	if err != nil {
		t.Fatalf("failed to fetch profile after re-seed: %v", err)
	}
	if profAfter.FullName != "Alice Custom Name" {
		t.Errorf("expected FullName 'Alice Custom Name' to be preserved, got %s", profAfter.FullName)
	}
	if profAfter.MainRevision != 42 {
		t.Errorf("expected MainRevision 42 to be preserved, got %d", profAfter.MainRevision)
	}

	// Verify records were not duplicated
	templates, err := templateRepo.ListTemplates(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list templates: %v", err)
	}
	expectedTemplateCount := len(domain.DefaultTemplateSets())
	if len(templates) != expectedTemplateCount {
		t.Errorf("expected exactly %d templates after multiple seeds, got %d", expectedTemplateCount, len(templates))
	}

	presets, err := presetRepo.ListPresets(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list presets: %v", err)
	}
	if len(presets) != 2 {
		t.Errorf("expected exactly 2 presets after multiple seeds, got %d", len(presets))
	}

	branchRepo := NewBranchRepository(db)
	branches, err := branchRepo.ListBranches(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list branches: %v", err)
	}
	if len(branches) != 1 {
		t.Errorf("expected exactly 1 default branch after multiple seeds, got %d", len(branches))
	}
}

func TestBranchRepository_DefaultBranchImmutability(t *testing.T) {
	ctx := context.Background()
	db, err := New("file:seed_test_immutability?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to initialize test database: %v", err)
	}

	if err := SeedDefaultDataDB(ctx, db); err != nil {
		t.Fatalf("SeedDefaultData failed: %v", err)
	}

	branchRepo := NewBranchRepository(db)
	branches, err := branchRepo.ListBranches(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list branches: %v", err)
	}
	if len(branches) == 0 {
		t.Fatalf("expected seeded default branch")
	}

	defaultBranch := branches[0]

	// 1. Attempting to delete default branch should fail
	err = branchRepo.DeleteBranch(ctx, "default", defaultBranch.ID)
	if err == nil {
		t.Fatalf("expected error deleting default branch, got nil")
	}

	// 2. Creating a regular branch and deleting it should succeed
	customBranch := &domain.Branch{
		ID:            "branch-custom-1",
		UserID:        "default",
		Name:          "Custom Branch",
		TemplateSetID: domain.TemplateStandardCleanID,
		IsDefault:     false,
	}
	if err := branchRepo.CreateBranch(ctx, customBranch); err != nil {
		t.Fatalf("failed to create custom branch: %v", err)
	}

	// Verify ListBranches sorts default branch first
	allBranches, err := branchRepo.ListBranches(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list branches: %v", err)
	}
	if len(allBranches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(allBranches))
	}
	if !allBranches[0].IsDefault {
		t.Errorf("expected first branch to be default branch, got %s (IsDefault=%v)", allBranches[0].Name, allBranches[0].IsDefault)
	}

	// Deleting custom branch should succeed
	if err := branchRepo.DeleteBranch(ctx, "default", customBranch.ID); err != nil {
		t.Fatalf("failed to delete custom branch: %v", err)
	}
}
