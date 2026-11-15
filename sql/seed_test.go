package sql

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/resumeee-app/resumeee-core"
	"github.com/resumeee-app/resumeee-core/seed"
)

func TestSeedDefaultData_FirstBootAndIdempotency(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "seed.db")

	st, err := OpenStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = st.Close() }()

	// Re-running SeedDefaultData must be strictly idempotent without bumping revisions or duplicating entities.
	if err := SeedDefaultData(ctx, st, "default"); err != nil {
		t.Fatalf("second SeedDefaultData: %v", err)
	}

	prof, err := st.Profiles().Profile(ctx, "default")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if prof.UserID != "default" || prof.MainRevision != 1 {
		t.Fatalf("unexpected default profile: %+v", prof)
	}

	templates, err := st.Templates().ListTemplates(ctx, "default")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) != len(seed.DefaultTemplateSets()) {
		t.Fatalf("expected %d built-in templates, got %d", len(seed.DefaultTemplateSets()), len(templates))
	}
	for _, tmpl := range templates {
		if err := (core.DefaultTemplateValidator{}).Validate(&tmpl); err != nil {
			t.Errorf("template %s syntax invalid: %v", tmpl.Name, err)
		}
	}

	presets, err := st.Presets().ListPresets(ctx, "default")
	if err != nil {
		t.Fatalf("ListPresets: %v", err)
	}
	if len(presets) < len(seed.DefaultPresets(core.SystemUserID)) {
		t.Fatalf("expected at least %d presets, got %d", len(seed.DefaultPresets(core.SystemUserID)), len(presets))
	}

	globals, err := st.Career().ListGlobalSkills(ctx)
	if err != nil {
		t.Fatalf("ListGlobalSkills: %v", err)
	}
	if len(globals) != len(seed.DefaultGlobalSkills()) {
		t.Fatalf("expected %d global skills, got %d", len(seed.DefaultGlobalSkills()), len(globals))
	}

	branches, err := st.Branches().ListBranches(ctx, "default")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 1 || !branches[0].IsDefault || branches[0].Epoch != 1 {
		t.Fatalf("expected single default branch with Epoch=1, got %+v", branches)
	}
	if branches[0].ActiveVersionID == nil || *branches[0].ActiveVersionID == "" {
		t.Fatalf("expected default branch to have ActiveVersionID")
	}

	// Default branch cannot be mutated or deleted.
	defBranch := branches[0]
	defBranch.Name = "Renamed"
	if err := st.Branches().UpdateBranch(ctx, &defBranch); !errors.Is(err, core.ErrDefaultBranchImmutable) {
		t.Errorf("expected ErrDefaultBranchImmutable on UpdateBranch, got %v", err)
	}
	if err := st.Branches().DeleteBranch(ctx, "default", defBranch.ID); !errors.Is(err, core.ErrDefaultBranchImmutable) {
		t.Errorf("expected ErrDefaultBranchImmutable on DeleteBranch, got %v", err)
	}
}
