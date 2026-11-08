package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

// DefaultGlobalSkills returns the canonical, system-seeded global skills available to all tenants.
func DefaultGlobalSkills() []domain.Skill {
	now := time.Now().UTC()
	canonical := []struct {
		Name     string
		Slug     string
		Category string
	}{
		// Languages
		{"Go", "go", "Languages"},
		{"Python", "python", "Languages"},
		{"TypeScript", "typescript", "Languages"},
		{"JavaScript", "javascript", "Languages"},
		{"Rust", "rust", "Languages"},
		{"Java", "java", "Languages"},
		{"C++", "c-plus-plus", "Languages"},
		{"SQL", "sql", "Languages"},
		// Cloud & Infrastructure
		{"Docker", "docker", "Cloud & Infrastructure"},
		{"Kubernetes", "kubernetes", "Cloud & Infrastructure"},
		{"AWS", "aws", "Cloud & Infrastructure"},
		{"Google Cloud", "google-cloud", "Cloud & Infrastructure"},
		{"Terraform", "terraform", "Cloud & Infrastructure"},
		{"Linux", "linux", "Cloud & Infrastructure"},
		// Databases
		{"PostgreSQL", "postgresql", "Databases"},
		{"MySQL", "mysql", "Databases"},
		{"SQLite", "sqlite", "Databases"},
		{"Redis", "redis", "Databases"},
		{"MongoDB", "mongodb", "Databases"},
		// Web & Frameworks
		{"React", "react", "Web Technologies"},
		{"Next.js", "nextjs", "Web Technologies"},
		{"Node.js", "nodejs", "Web Technologies"},
		{"GraphQL", "graphql", "Web Technologies"},
		{"REST APIs", "rest-apis", "Web Technologies"},
		// AI & Machine Learning
		{"PyTorch", "pytorch", "AI & Machine Learning"},
		{"TensorFlow", "tensorflow", "AI & Machine Learning"},
		{"LLM Integration", "llm-integration", "AI & Machine Learning"},
		{"LangChain", "langchain", "AI & Machine Learning"},
	}

	skills := make([]domain.Skill, len(canonical))
	for i, c := range canonical {
		skills[i] = domain.Skill{
			ID:          "global-skill-" + c.Slug,
			ShortID:     "g-" + c.Slug,
			UserID:      "system",
			Name:        c.Name,
			Slug:        c.Slug,
			IsGlobal:    true,
			Category:    c.Category,
			Proficiency: "Proficient",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	return skills
}

// DefaultPresets returns the system default built-in presets customized for the specified user.
//
// Parameters:
//   - userID: The target user identifier (defaults to domain.SystemUserID if empty).
//
// Returns:
//   - []domain.Preset: The list of system built-in presets.
func DefaultPresets(userID string) []domain.Preset {
	if userID == "" {
		userID = domain.SystemUserID
	}
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
			Language:           domain.DefaultLanguage,
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
			Language:           domain.DefaultLanguage,
			IncludeAdminFields: true,
			CreatedAt:          now,
			UpdatedAt:          now,
		},
	}
}

const tenantStripeCount = 64

var tenantProvisionStripes [tenantStripeCount]sync.Mutex

func getTenantProvisionLock(userID string) *sync.Mutex {
	var h uint32 = 2166136261 // FNV-1a 32-bit offset basis
	for i := 0; i < len(userID); i++ {
		h ^= uint32(userID[i])
		h *= 16777619 // FNV-1a 32-bit prime
	}
	return &tenantProvisionStripes[h%tenantStripeCount]
}

// EnsureTenantProvisioned idempotently ensures a tenant has an upserted user record,
// default profile, and default resume branch with an initial active rendered version.
func EnsureTenantProvisioned(
	ctx context.Context,
	store Store,
	userID string,
	userInfo ...*domain.User,
) error {
	if store == nil {
		return errors.New("store cannot be nil")
	}
	return EnsureTenantProvisionedRepos(
		ctx,
		store.Profile(),
		store.Branch(),
		store.Template(),
		store.User(),
		userID,
		userInfo...,
	)
}

// EnsureTenantProvisionedRepos idempotently provisions tenant entities using explicit repository handles.
func EnsureTenantProvisionedRepos(
	ctx context.Context,
	profileRepo domain.ProfileRepository,
	branchRepo domain.BranchRepository,
	templateRepo domain.TemplateRepository,
	userRepo domain.UserRepository,
	userID string,
	userInfo ...*domain.User,
) error {
	if userID == "" {
		return fmt.Errorf("%w: user id cannot be empty", domain.ErrValidation)
	}

	mu := getTenantProvisionLock(userID)
	mu.Lock()
	defer mu.Unlock()

	// 1. Upsert User record if UserRepository is provided
	if userRepo != nil {
		var u *domain.User
		if len(userInfo) > 0 && userInfo[0] != nil {
			u = userInfo[0]
			if u.ID == "" {
				u.ID = userID
			}
		} else {
			u = &domain.User{
				ID:       userID,
				Role:     domain.UserRoleUser,
				IsActive: true,
			}
		}
		if existing, err := userRepo.GetUser(ctx, userID); err == nil && existing != nil {
			if existing.Role == domain.UserRoleAdmin && u.Role != domain.UserRoleAdmin {
				u.Role = domain.UserRoleAdmin
			}
		}
		if err := userRepo.UpsertUser(ctx, u); err != nil {
			return fmt.Errorf("upserting tenant user: %w", err)
		}
	}

	// 2. Seed Default Profile if missing
	existingProf, err := profileRepo.GetProfile(ctx, userID)
	if err != nil || existingProf == nil {
		fullName := "Default User"
		email := ""
		if len(userInfo) > 0 && userInfo[0] != nil {
			if userInfo[0].FullName != "" {
				fullName = userInfo[0].FullName
			}
			email = userInfo[0].Email
		}
		defaultProfile := domain.Profile{
			ID:                 "profile-" + userID,
			UserID:             userID,
			FullName:           fullName,
			Email:              email,
			Headline:           "Software Engineer",
			MasterRevision:     1,
			DefaultProvider:    "gemini",
			DefaultModel:       "gemini-3.8-flash",
			GlobalWritingRules: "Prioritize measurable outcomes, active voice, and clear technical context.",
			CreatedAt:          time.Now().UTC(),
			UpdatedAt:          time.Now().UTC(),
		}
		if err := profileRepo.UpsertProfile(ctx, &defaultProfile); err != nil {
			return fmt.Errorf("seeding tenant default profile: %w", err)
		}
		existingProf = &defaultProfile
	}

	// 3. Seed Default Branch if missing
	defaultBranchID := domain.DefaultBranchID(userID)
	existingBranch, err := branchRepo.GetBranch(ctx, userID, defaultBranchID)
	if err != nil || existingBranch == nil {
		tmpl, err := templateRepo.GetTemplate(ctx, userID, renderer.TemplateStandardCleanID)
		if err != nil {
			tmpl, err = templateRepo.GetTemplate(ctx, domain.SystemUserID, renderer.TemplateStandardCleanID)
			if err != nil {
				return fmt.Errorf("finding standard template for default branch: %w", err)
			}
		}

		now := time.Now().UTC()
		defaultBranch := domain.Branch{
			ID:                       defaultBranchID,
			UserID:                   userID,
			Name:                     domain.DefaultBranchName,
			IsDefault:                true,
			TemplateSetID:            renderer.TemplateStandardCleanID,
			LastSyncedMasterRevision: existingProf.MasterRevision,
			CreatedAt:                now,
			UpdatedAt:                now,
		}
		if err := branchRepo.CreateBranch(ctx, &defaultBranch); err != nil && !errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("seeding default branch: %w", err)
		}

		snapshot, err := profileRepo.GetSnapshot(ctx, userID)
		if err != nil {
			return fmt.Errorf("getting snapshot for default branch: %w", err)
		}

		tailoredContent := domain.SnapshotToTailoredContent(snapshot)
		rendered, err := renderer.Render(ctx, tmpl, existingProf, tailoredContent)
		if err != nil {
			return fmt.Errorf("rendering default branch initial version: %w", err)
		}

		verID := "ver-default-" + userID
		ver := domain.BranchVersion{
			ID:                 verID,
			UserID:             userID,
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
		if err := branchRepo.CreateVersion(ctx, &ver); err != nil && !errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("seeding default branch version: %w", err)
		}

		defaultBranch.ActiveVersionID = &ver.ID
		_ = branchRepo.UpdateBranch(ctx, &defaultBranch)
	}

	return nil
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

	// 1. Seed Built-in Template Sets under system
	templates := renderer.DefaultTemplateSets()
	for _, tmpl := range templates {
		tmpl.UserID = domain.SystemUserID
		existing, err := templateRepo.GetTemplate(ctx, domain.SystemUserID, tmpl.ID)
		if err != nil || existing == nil {
			if err := renderer.Validate(&tmpl); err != nil {
				return fmt.Errorf("validating built-in template %s: %w", tmpl.Name, err)
			}
			if err := templateRepo.CreateTemplate(ctx, &tmpl); err != nil && !errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("seeding template %s: %w", tmpl.Name, err)
			}
		}
	}

	// 2. Seed Built-in Presets under system
	presets := DefaultPresets(domain.SystemUserID)
	for _, preset := range presets {
		existing, err := presetRepo.GetPreset(ctx, domain.SystemUserID, preset.ID)
		if err != nil || existing == nil {
			if err := presetRepo.CreatePreset(ctx, &preset); err != nil && !errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("seeding preset %s: %w", preset.Name, err)
			}
		}
	}

	// 2.5 Seed Global Skills under system
	globalSkills := DefaultGlobalSkills()
	for _, skill := range globalSkills {
		existing, err := profileRepo.GetSkill(ctx, domain.SystemUserID, skill.ID)
		if err != nil || existing == nil {
			if err := profileRepo.CreateSkill(ctx, &skill); err != nil && !errors.Is(err, domain.ErrConflict) {
				return fmt.Errorf("seeding global skill %s: %w", skill.Name, err)
			}
		}
	}

	// 3. Ensure Default Tenant is provisioned
	var userRepo domain.UserRepository
	if ur, ok := profileRepo.(domain.UserRepository); ok {
		userRepo = ur
	}
	return EnsureTenantProvisionedRepos(ctx, profileRepo, branchRepo, templateRepo, userRepo, targetUserID)
}

