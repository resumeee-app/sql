package sql

import (
	"context"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core/domain"
)

const (
	// Built-in Template Set IDs
	TemplateStandardCleanID         = domain.TemplateStandardCleanID
	TemplateDetailedImmigrationCVID = domain.TemplateDetailedImmigrationCVID
	TemplateATSMinimalTxtID         = domain.TemplateATSMinimalTxtID
	TemplateATSHybridTechID         = domain.TemplateATSHybridTechID
	TemplateATSExecutiveCleanID     = domain.TemplateATSExecutiveCleanID

	// Built-in Preset IDs
	Preset1PageTechResumeID          = domain.Preset1PageTechResumeID
	PresetImmigrationFullHistoryCVID = domain.PresetImmigrationFullHistoryCVID
)

// SeedDefaultDataDB idempotently populates default user profile, built-in template sets,
// and built-in presets into the relational database on first boot.
//
// Parameters:
//   - ctx: Context controlling cancellation signals.
//   - db: The GORM database connection.
//   - defaultUserID: Optional target user ID (defaults to "default").
//
// Returns:
//   - error: Non-nil if seeding fails.
func SeedDefaultDataDB(ctx context.Context, db *gorm.DB, defaultUserID ...string) error {
	targetUserID := "default"
	if len(defaultUserID) > 0 && defaultUserID[0] != "" {
		targetUserID = defaultUserID[0]
	}
	return SeedBaseline(ctx, db, Config{DefaultUserID: targetUserID})
}
