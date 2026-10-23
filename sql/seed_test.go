package sqlite_test

import (
	"context"
	"testing"

	"resumeee/internal/domain"
	"resumeee/internal/repository/sqlite"
)

func TestSeedDefaultData_FirstBoot(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.New("file:seed_test_1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to initialize test database: %v", err)
	}

	if err := sqlite.SeedDefaultData(ctx, db); err != nil {
		t.Fatalf("SeedDefaultData failed: %v", err)
	}

	profileRepo := sqlite.NewProfileRepository(db)
	templateRepo := sqlite.NewTemplateRepository(db)
	presetRepo := sqlite.NewPresetRepository(db)

	// 1. Verify default profile
	prof, err := profileRepo.GetProfile(ctx, "default")
	if err != nil {
		t.Fatalf("failed to get default profile: %v", err)
	}
	if prof.UserID != "default" {
		t.Errorf("expected UserID 'default', got %s", prof.UserID)
	}
	if prof.MasterRevision != 1 {
		t.Errorf("expected MasterRevision 1, got %d", prof.MasterRevision)
	}

	// 2. Verify built-in template sets
	templates, err := templateRepo.ListTemplates(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list templates: %v", err)
	}
	if len(templates) < 2 {
		t.Errorf("expected at least 2 built-in templates, got %d", len(templates))
	}

	tmplMap := make(map[string]domain.TemplateSet)
	for _, tmpl := range templates {
		tmplMap[tmpl.Name] = tmpl
		// Verify template syntax validity
		if err := sqlite.ValidateTemplateSet(&tmpl); err != nil {
			t.Errorf("template %s has invalid syntax: %v", tmpl.Name, err)
		}
	}

	cleanTmpl, ok := tmplMap["Standard Clean"]
	if !ok {
		t.Errorf("missing built-in template 'Standard Clean'")
	} else if cleanTmpl.HTMLTemplate == "" || cleanTmpl.MarkdownTemplate == "" || cleanTmpl.TextTemplate == "" {
		t.Errorf("expected 'Standard Clean' to have HTML, Markdown, and PlainText templates")
	}

	cvTmpl, ok := tmplMap["Detailed Immigration / CV"]
	if !ok {
		t.Errorf("missing built-in template 'Detailed Immigration / CV'")
	} else if cvTmpl.HTMLTemplate == "" || cvTmpl.MarkdownTemplate == "" || cvTmpl.TextTemplate == "" {
		t.Errorf("expected 'Detailed Immigration / CV' to have HTML, Markdown, and PlainText templates")
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
	} else if !cvPreset.IncludeAdminFields {
		t.Errorf("'Immigration Full History CV' preset should have IncludeAdminFields=true")
	}
}

func TestSeedDefaultData_Idempotency(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.New("file:seed_test_idempotent?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to initialize test database: %v", err)
	}

	// First seed
	if err := sqlite.SeedDefaultData(ctx, db); err != nil {
		t.Fatalf("first SeedDefaultData failed: %v", err)
	}

	profileRepo := sqlite.NewProfileRepository(db)
	templateRepo := sqlite.NewTemplateRepository(db)
	presetRepo := sqlite.NewPresetRepository(db)

	// User modifies profile
	prof, err := profileRepo.GetProfile(ctx, "default")
	if err != nil {
		t.Fatalf("failed to fetch profile: %v", err)
	}
	prof.FullName = "Alice Custom Name"
	prof.Headline = "Distinguished Architect"
	prof.MasterRevision = 42
	if err := db.Save(prof).Error; err != nil {
		t.Fatalf("failed to update profile directly: %v", err)
	}

	// Run seeder multiple consecutive times
	for i := 0; i < 3; i++ {
		if err := sqlite.SeedDefaultData(ctx, db); err != nil {
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
	if profAfter.MasterRevision != 42 {
		t.Errorf("expected MasterRevision 42 to be preserved, got %d", profAfter.MasterRevision)
	}

	// Verify records were not duplicated
	templates, err := templateRepo.ListTemplates(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list templates: %v", err)
	}
	if len(templates) != 2 {
		t.Errorf("expected exactly 2 templates after multiple seeds, got %d", len(templates))
	}

	presets, err := presetRepo.ListPresets(ctx, "default")
	if err != nil {
		t.Fatalf("failed to list presets: %v", err)
	}
	if len(presets) != 2 {
		t.Errorf("expected exactly 2 presets after multiple seeds, got %d", len(presets))
	}
}
