package sql

import (
	"context"

	"github.com/resumeee-app/resumeee-core/domain"
)

// DefaultGlobalSkills returns the canonical, system-seeded global skills available to all tenants.
func DefaultGlobalSkills() []domain.Skill {
	return domain.DefaultGlobalSkills()
}

// DefaultPresets returns the system default built-in presets customized for the specified user.
func DefaultPresets(userID string) []domain.Preset {
	return domain.DefaultPresets(userID)
}

// EnsureTenantProvisioned idempotently ensures a tenant has an upserted user record,
// default profile, and default resume branch with an initial active rendered version.
func EnsureTenantProvisioned(
	ctx context.Context,
	store domain.Store,
	userID string,
	userInfo ...*domain.User,
) error {
	return domain.EnsureTenantProvisioned(ctx, store, userID, userInfo...)
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
	return domain.EnsureTenantProvisionedRepos(ctx, profileRepo, branchRepo, templateRepo, userRepo, userID, userInfo...)
}

// SeedDefaultData idempotently populates built-in templates, presets, default user profile,
// and default resume branch across any storage backend implementing the domain repository interfaces.
func SeedDefaultData(
	ctx context.Context,
	profileRepo domain.ProfileRepository,
	branchRepo domain.BranchRepository,
	templateRepo domain.TemplateRepository,
	presetRepo domain.PresetRepository,
	defaultUserID ...string,
) error {
	return domain.SeedDefaultData(ctx, profileRepo, branchRepo, templateRepo, presetRepo, defaultUserID...)
}
