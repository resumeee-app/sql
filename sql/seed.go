package sqlite

import (
	"context"

	"gorm.io/gorm"

	"resumeee/internal/domain"
	"resumeee/internal/renderer"
	"resumeee/internal/repository"
)

const (
	// Built-in Template Set IDs
	TemplateStandardCleanID         = renderer.TemplateStandardCleanID
	TemplateDetailedImmigrationCVID = renderer.TemplateDetailedImmigrationCVID
	TemplateATSMinimalTxtID         = renderer.TemplateATSMinimalTxtID
	TemplateATSHybridTechID         = renderer.TemplateATSHybridTechID
	TemplateATSExecutiveCleanID     = renderer.TemplateATSExecutiveCleanID

	// Built-in Preset IDs
	Preset1PageTechResumeID          = domain.Preset1PageTechResumeID
	PresetImmigrationFullHistoryCVID = domain.PresetImmigrationFullHistoryCVID
)

// SeedDefaultData idempotently populates default user profile, built-in template sets,
// and built-in presets into SQLite on first boot.
//
// Parameters:
//   - ctx: Context controlling cancellation signals.
//   - db: The GORM database connection.
//   - defaultUserID: Optional target user ID (defaults to "default").
//
// Returns:
//   - error: Non-nil if seeding fails.
func SeedDefaultData(ctx context.Context, db *gorm.DB, defaultUserID ...string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		profileRepo := NewProfileRepository(tx)
		branchRepo := NewBranchRepository(tx)
		templateRepo := NewTemplateRepository(tx)
		presetRepo := NewPresetRepository(tx)
		return repository.SeedDefaultData(ctx, profileRepo, branchRepo, templateRepo, presetRepo, defaultUserID...)
	})
}
