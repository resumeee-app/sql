package sql

import (
	"context"
	"errors"
	"testing"

	"github.com/resumeee-app/resumeee-core"
	"github.com/resumeee-app/resumeee-core/seed"
)

func TestUserStore_CRUD(t *testing.T) {
	ctx := context.Background()
	db, err := New("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}

	repo := NewUserStore(db)

	t.Run("Validation failure on nil user or empty ID", func(t *testing.T) {
		if err := repo.UpsertUser(ctx, nil); !errors.Is(err, core.ErrValidation) {
			t.Errorf("expected ErrValidation for nil user, got %v", err)
		}
		if err := repo.UpsertUser(ctx, &core.User{ID: ""}); !errors.Is(err, core.ErrValidation) {
			t.Errorf("expected ErrValidation for empty ID, got %v", err)
		}
	})

	t.Run("User not found returns ErrNotFound", func(t *testing.T) {
		_, err := repo.User(ctx, "non-existent-user-id")
		if !errors.Is(err, core.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Create user and retrieve", func(t *testing.T) {
		u := &core.User{
			ID:       "user-alpha",
			Email:    "alpha@example.com",
			FullName: "Alpha User",
			Role:     core.UserRoleUser,
			Tier:     "free",
			IsActive: true,
		}
		if err := repo.UpsertUser(ctx, u); err != nil {
			t.Fatalf("failed to upsert user: %v", err)
		}

		retrieved, err := repo.User(ctx, "user-alpha")
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if retrieved.Email != "alpha@example.com" {
			t.Errorf("expected alpha@example.com, got %q", retrieved.Email)
		}
		if retrieved.FullName != "Alpha User" {
			t.Errorf("expected Alpha User, got %q", retrieved.FullName)
		}
		if retrieved.Role != core.UserRoleUser {
			t.Errorf("expected user role, got %q", retrieved.Role)
		}
		if !retrieved.IsActive {
			t.Errorf("expected active user")
		}
		if retrieved.CreatedAt.IsZero() {
			t.Errorf("expected non-zero CreatedAt")
		}
	})

	t.Run("Update user details on subsequent upsert", func(t *testing.T) {
		u := &core.User{
			ID:       "user-alpha",
			Email:    "alpha.updated@example.com",
			FullName: "Alpha Updated",
			Role:     core.UserRoleUser,
			Tier:     "pro",
			IsActive: false,
		}
		if err := repo.UpsertUser(ctx, u); err != nil {
			t.Fatalf("failed to update user: %v", err)
		}

		retrieved, err := repo.User(ctx, "user-alpha")
		if err != nil {
			t.Fatalf("failed to get updated user: %v", err)
		}
		if retrieved.Email != "alpha.updated@example.com" {
			t.Errorf("expected updated email, got %q", retrieved.Email)
		}
		if retrieved.FullName != "Alpha Updated" {
			t.Errorf("expected updated full name, got %q", retrieved.FullName)
		}
		if retrieved.Tier != "pro" {
			t.Errorf("expected tier pro, got %q", retrieved.Tier)
		}
		if retrieved.IsActive {
			t.Errorf("expected inactive user")
		}
	})

	t.Run("Preserve existing admin role on conflict", func(t *testing.T) {
		adminUser := &core.User{
			ID:       "admin-user-1",
			Email:    "admin@resumeee.io",
			FullName: "Root Administrator",
			Role:     core.UserRoleAdmin,
			Tier:     "enterprise",
			IsActive: true,
		}
		if err := repo.UpsertUser(ctx, adminUser); err != nil {
			t.Fatalf("failed to create admin user: %v", err)
		}

		// Subsequent upsert attempting to downgrade to standard user
		demotionAttempt := &core.User{
			ID:       "admin-user-1",
			Email:    "admin.newemail@resumeee.io",
			FullName: "Root Administrator Updated",
			Role:     core.UserRoleUser,
			Tier:     "free",
			IsActive: true,
		}
		if err := repo.UpsertUser(ctx, demotionAttempt); err != nil {
			t.Fatalf("failed to upsert user with attempted demotion: %v", err)
		}

		retrieved, err := repo.User(ctx, "admin-user-1")
		if err != nil {
			t.Fatalf("failed to retrieve admin user: %v", err)
		}
		if retrieved.Role != core.UserRoleAdmin {
			t.Errorf("expected role to remain %q, got %q", core.UserRoleAdmin, retrieved.Role)
		}
		if retrieved.Email != "admin.newemail@resumeee.io" {
			t.Errorf("expected email to be updated to admin.newemail@resumeee.io, got %q", retrieved.Email)
		}
	})
}

func TestStore_UserAccessorAndEnsureTenantProvisioned(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, "file::memory:?cache=shared", "default")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	if store.Users() == nil {
		t.Fatal("expected store.Users() to not be nil")
	}

	newTenantID := "tenant-sqlite-99"
	err = seed.EnsureTenantProvisioned(ctx, store, newTenantID, &core.User{
		ID:       newTenantID,
		Email:    "tenant99@example.com",
		FullName: "Tenant Ninety-Nine",
		Role:     core.UserRoleUser,
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("failed to ensure tenant provisioned: %v", err)
	}

	// Verify User record was created
	u, err := store.Users().User(ctx, newTenantID)
	if err != nil {
		t.Fatalf("failed to get provisioned user: %v", err)
	}
	if u.Email != "tenant99@example.com" {
		t.Errorf("expected email tenant99@example.com, got %q", u.Email)
	}

	// Verify Profile was created
	prof, err := store.Profiles().Profile(ctx, newTenantID)
	if err != nil {
		t.Fatalf("failed to get provisioned profile: %v", err)
	}
	if prof.UserID != newTenantID {
		t.Errorf("expected profile UserID %q, got %q", newTenantID, prof.UserID)
	}

	// Verify Default Branch was created
	defaultBranchID := core.DefaultBranchID(newTenantID)
	branch, err := store.Branches().Branch(ctx, newTenantID, defaultBranchID)
	if err != nil {
		t.Fatalf("failed to get provisioned branch: %v", err)
	}
	if !branch.IsDefault {
		t.Errorf("expected branch to be default")
	}

	// Run second time idempotently with attempted role demotion
	err = seed.EnsureTenantProvisioned(ctx, store, newTenantID, &core.User{
		ID:       newTenantID,
		Email:    "tenant99.modified@example.com",
		FullName: "Tenant Ninety-Nine Modified",
		Role:     core.UserRoleUser,
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("second call to EnsureTenantProvisioned failed: %v", err)
	}

	u2, err := store.Users().User(ctx, newTenantID)
	if err != nil {
		t.Fatalf("failed to get user after second call: %v", err)
	}
	if u2.Email != "tenant99.modified@example.com" {
		t.Errorf("expected updated email, got %q", u2.Email)
	}
}
