package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"resumeee/internal/domain"
	"resumeee/internal/renderer"
)

const (
	// Built-in Template Set IDs
	TemplateStandardCleanID         = renderer.TemplateStandardCleanID
	TemplateDetailedImmigrationCVID = renderer.TemplateDetailedImmigrationCVID

	// Built-in Preset IDs
	Preset1PageTechResumeID          = "builtin-1-page-tech-resume"
	PresetImmigrationFullHistoryCVID = "builtin-immigration-full-history-cv"
)

// SeedDefaultData idempotently populates default user profile, built-in template sets,
// and built-in presets into SQLite on first boot.
func SeedDefaultData(ctx context.Context, db *gorm.DB, defaultUserID ...string) error {
	targetUserID := "default"
	if len(defaultUserID) > 0 && defaultUserID[0] != "" {
		targetUserID = defaultUserID[0]
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Seed Built-in Template Sets
		templates := getBuiltinTemplateSets()
		for _, tmpl := range templates {
			var existing domain.TemplateSet
			err := tx.Where("id = ?", tmpl.ID).First(&existing).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if err := ValidateTemplateSet(&tmpl); err != nil {
						return fmt.Errorf("validating built-in template %s: %w", tmpl.Name, err)
					}
					if err := tx.Create(&tmpl).Error; err != nil {
						return fmt.Errorf("seeding template %s: %w", tmpl.Name, err)
					}
				} else {
					return err
				}
			}
		}

		// 2. Seed Built-in Presets
		presets := getBuiltinPresets(targetUserID)
		for _, preset := range presets {
			var existing domain.Preset
			err := tx.Where("id = ?", preset.ID).First(&existing).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if err := tx.Create(&preset).Error; err != nil {
						return fmt.Errorf("seeding preset %s: %w", preset.Name, err)
					}
				} else {
					return err
				}
			}
		}

		// 3. Seed Default Profile
		var existingProf domain.Profile
		err := tx.Where("user_id = ?", targetUserID).First(&existingProf).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				defaultProfile := domain.Profile{
					ID:                 "profile-" + targetUserID,
					UserID:             targetUserID,
					FullName:           "Default User",
					Headline:           "Software Engineer",
					MasterRevision:     1,
					DefaultProvider:    "gemini",
					DefaultModel:       "gemini-2.5-pro",
					GlobalWritingRules: "Prioritize measurable outcomes, active voice, and clear technical context.",
					CreatedAt:          time.Now().UTC(),
					UpdatedAt:          time.Now().UTC(),
				}
				if err := tx.Create(&defaultProfile).Error; err != nil {
					return fmt.Errorf("seeding default profile: %w", err)
				}
			} else {
				return err
			}
		}

		return nil
	})
}

func getBuiltinTemplateSets() []domain.TemplateSet {
	return renderer.DefaultTemplateSets()
}

func getBuiltinPresets(userID string) []domain.Preset {
	now := time.Now().UTC()
	return []domain.Preset{
		{
			ID:                 Preset1PageTechResumeID,
			UserID:             userID,
			Name:               "1-Page Tech Resume",
			Description:        "Concise tech resume format emphasizing recent high-impact metrics and key skills.",
			SystemInstructions: "Focus strictly on high-impact engineering accomplishments, quantitative outcomes, and relevant technical skill groups. Ensure concise bullet formatting suitable for a 1-page tech resume.",
			TemplateSetID:      TemplateStandardCleanID,
			IncludeAdminFields: false,
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		{
			ID:                 PresetImmigrationFullHistoryCVID,
			UserID:             userID,
			Name:               "Immigration Full History CV",
			Description:        "Unabridged career history format for audit, visa, and immigration compliance.",
			SystemInstructions: "Include full, unabridged employment and academic history. Maintain detailed hours per week, company location and verification contacts, supervisor contact information, and full explanatory duty summaries.",
			TemplateSetID:      TemplateDetailedImmigrationCVID,
			IncludeAdminFields: true,
			CreatedAt:          now,
			UpdatedAt:          now,
		},
	}
}
