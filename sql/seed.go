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
					DefaultModel:       "gemini-3.8-flash",
					GlobalWritingRules: "Prioritize measurable outcomes, active voice, and clear technical context.",
					CreatedAt:          time.Now().UTC(),
					UpdatedAt:          time.Now().UTC(),
				}
				if err := tx.Create(&defaultProfile).Error; err != nil {
					return fmt.Errorf("seeding default profile: %w", err)
				}
				existingProf = defaultProfile
			} else {
				return err
			}
		}

		// 4. Seed Default Branch (Untailored Master Profile directly rendered)
		var existingBranch domain.Branch
		defaultBranchID := domain.DefaultBranchID(targetUserID)
		err = tx.Where("user_id = ? AND (id = ? OR is_default = ?)", targetUserID, defaultBranchID, true).First(&existingBranch).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				var tmpl domain.TemplateSet
				if err := tx.Where("id = ?", TemplateStandardCleanID).First(&tmpl).Error; err != nil {
					return fmt.Errorf("finding standard template for default branch: %w", err)
				}

				now := time.Now().UTC()
				defaultBranch := domain.Branch{
					ID:                       defaultBranchID,
					UserID:                   targetUserID,
					Name:                     domain.DefaultBranchName,
					IsDefault:                true,
					TemplateSetID:            TemplateStandardCleanID,
					LastSyncedMasterRevision: existingProf.MasterRevision,
					CreatedAt:                now,
					UpdatedAt:                now,
				}
				if err := tx.Create(&defaultBranch).Error; err != nil {
					return fmt.Errorf("seeding default branch: %w", err)
				}

				profileRepo := NewProfileRepository(tx)
				snapshot, err := profileRepo.GetSnapshot(ctx, targetUserID)
				if err != nil {
					return fmt.Errorf("getting snapshot for default branch: %w", err)
				}

				tailoredContent := domain.SnapshotToTailoredContent(snapshot)
				rendered, err := renderer.Render(ctx, &tmpl, &existingProf, tailoredContent)
				if err != nil {
					return fmt.Errorf("rendering default branch initial version: %w", err)
				}

				verID := "ver-default-" + targetUserID
				ver := domain.BranchVersion{
					ID:                 verID,
					BranchID:           defaultBranch.ID,
					VersionNumber:      1,
					Status:             domain.VersionStatusCompleted,
					MasterRevisionUsed: existingProf.MasterRevision,
					TemplateSetIDUsed:  TemplateStandardCleanID,
					ProviderUsed:       "none",
					ModelUsed:          "none",
					SourceType:         domain.SourceTypeMasterProfile,
					TailoredContent:    tailoredContent,
					HTMLOutput:         rendered.HTML,
					MarkdownOutput:     rendered.Markdown,
					PlainTextOutput:    rendered.PlainText,
					CreatedAt:          now,
					CompletedAt:        &now,
				}
				if err := tx.Create(&ver).Error; err != nil {
					return fmt.Errorf("seeding default branch version: %w", err)
				}

				if err := tx.Model(&domain.Branch{}).Where("id = ?", defaultBranch.ID).Update("active_version_id", ver.ID).Error; err != nil {
					return fmt.Errorf("setting default branch active version: %w", err)
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
