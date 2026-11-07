package repository

import (
	"context"
	"fmt"
	"time"

	"resumeee/internal/domain"
	"resumeee/internal/renderer"
)

// ValidateTemplateSet delegates template syntax and integrity validation to the template renderer.
//
// Parameters:
//   - tmpl: The template set to validate.
//
// Returns:
//   - error: Non-nil if any template within the set fails validation.
func ValidateTemplateSet(tmpl *domain.TemplateSet) error {
	return renderer.Validate(tmpl)
}

// DefaultPresets returns the system default built-in presets customized for the specified user.
//
// Parameters:
//   - userID: The target user identifier.
//
// Returns:
//   - []domain.Preset: The list of system built-in presets.
func DefaultPresets(userID string) []domain.Preset {
	now := time.Now().UTC()
	themeStd := renderer.TemplateStandardCleanID
	themeCV := renderer.TemplateDetailedImmigrationCVID
	return []domain.Preset{
		{
			ID:                 domain.Preset1PageTechResumeID,
			UserID:             userID,
			Name:               "1-Page Tech Resume",
			Description:        "Concise tech resume format emphasizing recent high-impact metrics and key skills.",
			SystemInstructions: "Focus strictly on high-impact engineering accomplishments, quantitative outcomes, and relevant technical skill groups. Ensure concise bullet formatting suitable for a 1-page tech resume.",
			SuggestedThemeID:   &themeStd,
			TemplateSetID:      renderer.TemplateStandardCleanID,
			IncludeAdminFields: false,
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		{
			ID:                 domain.PresetImmigrationFullHistoryCVID,
			UserID:             userID,
			Name:               "Immigration Full History CV",
			Description:        "Unabridged career history format for audit, visa, and immigration compliance.",
			SystemInstructions: "Include full, unabridged employment and academic history. Maintain detailed hours per week, company location and verification contacts, supervisor contact information, and full explanatory duty summaries.",
			SuggestedThemeID:   &themeCV,
			TemplateSetID:      renderer.TemplateDetailedImmigrationCVID,
			IncludeAdminFields: true,
			CreatedAt:          now,
			UpdatedAt:          now,
		},
	}
}

// SeedDefaultData idempotently populates built-in templates, presets, default user profile,
// and default resume branch across any storage backend implementing the domain repository interfaces.
//
// Parameters:
//   - ctx: The execution context.
//   - profileRepo: The profile repository instance.
//   - branchRepo: The branch repository instance.
//   - templateRepo: The template repository instance.
//   - presetRepo: The preset repository instance.
//   - defaultUserID: Optional target user ID (defaults to "default").
//
// Returns:
//   - error: Non-nil if seeding fails at any step.
func SeedDefaultData(
	ctx context.Context,
	profileRepo domain.ProfileRepository,
	branchRepo domain.BranchRepository,
	templateRepo domain.TemplateRepository,
	presetRepo domain.PresetRepository,
	defaultUserID ...string,
) error {
	targetUserID := "default"
	if len(defaultUserID) > 0 && defaultUserID[0] != "" {
		targetUserID = defaultUserID[0]
	}

	// 1. Seed Built-in Template Sets
	templates := renderer.DefaultTemplateSets()
	for _, tmpl := range templates {
		existing, err := templateRepo.GetTemplate(ctx, targetUserID, tmpl.ID)
		if err != nil || existing == nil {
			if err := renderer.Validate(&tmpl); err != nil {
				return fmt.Errorf("validating built-in template %s: %w", tmpl.Name, err)
			}
			if err := templateRepo.CreateTemplate(ctx, &tmpl); err != nil {
				return fmt.Errorf("seeding template %s: %w", tmpl.Name, err)
			}
		}
	}

	// 2. Seed Built-in Presets
	presets := DefaultPresets(targetUserID)
	for _, preset := range presets {
		existing, err := presetRepo.GetPreset(ctx, targetUserID, preset.ID)
		if err != nil || existing == nil {
			if err := presetRepo.CreatePreset(ctx, &preset); err != nil {
				return fmt.Errorf("seeding preset %s: %w", preset.Name, err)
			}
		}
	}

	// 3. Seed Default Profile
	existingProf, err := profileRepo.GetProfile(ctx, targetUserID)
	if err != nil || existingProf == nil {
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
		if err := profileRepo.UpsertProfile(ctx, &defaultProfile); err != nil {
			return fmt.Errorf("seeding default profile: %w", err)
		}
		existingProf = &defaultProfile
	}

	// 4. Seed Default Branch
	defaultBranchID := domain.DefaultBranchID(targetUserID)
	existingBranch, err := branchRepo.GetBranch(ctx, targetUserID, defaultBranchID)
	if err != nil || existingBranch == nil {
		tmpl, err := templateRepo.GetTemplate(ctx, targetUserID, renderer.TemplateStandardCleanID)
		if err != nil {
			return fmt.Errorf("finding standard template for default branch: %w", err)
		}

		now := time.Now().UTC()
		defaultBranch := domain.Branch{
			ID:                       defaultBranchID,
			UserID:                   targetUserID,
			Name:                     domain.DefaultBranchName,
			IsDefault:                true,
			TemplateSetID:            renderer.TemplateStandardCleanID,
			LastSyncedMasterRevision: existingProf.MasterRevision,
			CreatedAt:                now,
			UpdatedAt:                now,
		}
		if err := branchRepo.CreateBranch(ctx, &defaultBranch); err != nil {
			return fmt.Errorf("seeding default branch: %w", err)
		}

		snapshot, err := profileRepo.GetSnapshot(ctx, targetUserID)
		if err != nil {
			return fmt.Errorf("getting snapshot for default branch: %w", err)
		}

		tailoredContent := domain.SnapshotToTailoredContent(snapshot)
		rendered, err := renderer.Render(ctx, tmpl, existingProf, tailoredContent)
		if err != nil {
			return fmt.Errorf("rendering default branch initial version: %w", err)
		}

		verID := "ver-default-" + targetUserID
		ver := domain.BranchVersion{
			ID:                 verID,
			ResumeID:           defaultBranch.ID,
			BranchID:           defaultBranch.ID,
			VersionNumber:      1,
			Status:             domain.VersionStatusCompleted,
			MasterRevisionUsed: existingProf.MasterRevision,
			TemplateSetIDUsed:  renderer.TemplateStandardCleanID,
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
		if err := branchRepo.CreateVersion(ctx, &ver); err != nil {
			return fmt.Errorf("seeding default branch version: %w", err)
		}

		defaultBranch.ActiveVersionID = &ver.ID
		if err := branchRepo.UpdateBranch(ctx, &defaultBranch); err != nil {
			return fmt.Errorf("setting default branch active version: %w", err)
		}
	}

	return nil
}
