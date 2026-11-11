package sql

import (
	"context"
	"errors"
	"testing"

	"github.com/resumeee-app/resumeee-core/domain"
)

func TestUserRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	db, err := New("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}

	repo := NewUserRepository(db)

	t.Run("Validation failure on nil user or empty ID", func(t *testing.T) {
		if err := repo.UpsertUser(ctx, nil); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("expected ErrValidation for nil user, got %v", err)
		}
		if err := repo.UpsertUser(ctx, &domain.User{ID: ""}); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("expected ErrValidation for empty ID, got %v", err)
		}
	})

	t.Run("GetUser not found returns ErrNotFound", func(t *testing.T) {
		_, err := repo.GetUser(ctx, "non-existent-user-id")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Create user and retrieve", func(t *testing.T) {
		u := &domain.User{
			ID:       "user-alpha",
			Email:    "alpha@example.com",
			FullName: "Alpha User",
			Role:     domain.UserRoleUser,
			Tier:     "free",
			IsActive: true,
		}
		if err := repo.UpsertUser(ctx, u); err != nil {
			t.Fatalf("failed to upsert user: %v", err)
		}

		retrieved, err := repo.GetUser(ctx, "user-alpha")
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if retrieved.Email != "alpha@example.com" {
			t.Errorf("expected alpha@example.com, got %q", retrieved.Email)
		}
		if retrieved.FullName != "Alpha User" {
			t.Errorf("expected Alpha User, got %q", retrieved.FullName)
		}
		if retrieved.Role != domain.UserRoleUser {
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
		u := &domain.User{
			ID:       "user-alpha",
			Email:    "alpha.updated@example.com",
			FullName: "Alpha Updated",
			Role:     domain.UserRoleUser,
			Tier:     "pro",
			IsActive: false,
		}
		if err := repo.UpsertUser(ctx, u); err != nil {
			t.Fatalf("failed to update user: %v", err)
		}

		retrieved, err := repo.GetUser(ctx, "user-alpha")
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
		adminUser := &domain.User{
			ID:       "admin-user-1",
			Email:    "admin@resumeee.io",
			FullName: "Root Administrator",
			Role:     domain.UserRoleAdmin,
			Tier:     "enterprise",
			IsActive: true,
		}
		if err := repo.UpsertUser(ctx, adminUser); err != nil {
			t.Fatalf("failed to create admin user: %v", err)
		}

		// Subsequent upsert attempting to downgrade to standard user
		demotionAttempt := &domain.User{
			ID:       "admin-user-1",
			Email:    "admin.newemail@resumeee.io",
			FullName: "Root Administrator Updated",
			Role:     domain.UserRoleUser,
			Tier:     "free",
			IsActive: true,
		}
		if err := repo.UpsertUser(ctx, demotionAttempt); err != nil {
			t.Fatalf("failed to upsert user with attempted demotion: %v", err)
		}

		retrieved, err := repo.GetUser(ctx, "admin-user-1")
		if err != nil {
			t.Fatalf("failed to retrieve admin user: %v", err)
		}
		if retrieved.Role != domain.UserRoleAdmin {
			t.Errorf("expected role to remain %q, got %q", domain.UserRoleAdmin, retrieved.Role)
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

	if store.User() == nil {
		t.Fatal("expected store.User() to not be nil")
	}

	newTenantID := "tenant-sqlite-99"
	err = EnsureTenantProvisioned(ctx, store, newTenantID, &domain.User{
		ID:       newTenantID,
		Email:    "tenant99@example.com",
		FullName: "Tenant Ninety-Nine",
		Role:     domain.UserRoleUser,
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("failed to ensure tenant provisioned: %v", err)
	}

	// Verify User record was created
	u, err := store.User().GetUser(ctx, newTenantID)
	if err != nil {
		t.Fatalf("failed to get provisioned user: %v", err)
	}
	if u.Email != "tenant99@example.com" {
		t.Errorf("expected email tenant99@example.com, got %q", u.Email)
	}

	// Verify Profile was created
	prof, err := store.Profile().GetProfile(ctx, newTenantID)
	if err != nil {
		t.Fatalf("failed to get provisioned profile: %v", err)
	}
	if prof.UserID != newTenantID {
		t.Errorf("expected profile UserID %q, got %q", newTenantID, prof.UserID)
	}

	// Verify Default Branch was created
	defaultBranchID := domain.DefaultBranchID(newTenantID)
	branch, err := store.Branch().GetBranch(ctx, newTenantID, defaultBranchID)
	if err != nil {
		t.Fatalf("failed to get provisioned branch: %v", err)
	}
	if !branch.IsDefault {
		t.Errorf("expected branch to be default")
	}

	// Run second time idempotently with attempted role demotion
	err = EnsureTenantProvisioned(ctx, store, newTenantID, &domain.User{
		ID:       newTenantID,
		Email:    "tenant99.modified@example.com",
		FullName: "Tenant Ninety-Nine Modified",
		Role:     domain.UserRoleUser,
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("second call to EnsureTenantProvisioned failed: %v", err)
	}

	u2, err := store.User().GetUser(ctx, newTenantID)
	if err != nil {
		t.Fatalf("failed to get user after second call: %v", err)
	}
	if u2.Email != "tenant99.modified@example.com" {
		t.Errorf("expected updated email, got %q", u2.Email)
	}
}
