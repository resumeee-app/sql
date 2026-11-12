package sql

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenWithOptions_SQLite(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "custom.db")

	store, err := OpenWithOptions(ctx,
		WithDBPath(dbPath),
		WithAutoMigrate(true),
		WithDefaultUserID("custom-user"),
		WithConnectionPool(5, 2, 5*time.Minute, time.Minute),
	)
	if err != nil {
		t.Fatalf("expected OpenWithOptions to succeed, got: %v", err)
	}
	defer func() { _ = store.Close() }()

	prof, err := store.Profile().GetProfile(ctx, "custom-user")
	if err != nil {
		t.Fatalf("expected seeded profile for 'custom-user', got: %v", err)
	}
	if prof.UserID != "custom-user" {
		t.Errorf("expected UserID 'custom-user', got %q", prof.UserID)
	}
}

func TestConfigOptions_Validation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		opt  ConfigOption
	}{
		{"empty driver", WithDriver("")},
		{"empty dsn", WithDSN("   ")},
		{"empty dbPath", WithDBPath("")},
		{"empty defaultUserID", WithDefaultUserID("")},
		{"negative maxOpen", WithConnectionPool(-1, 1, 0, 0)},
		{"negative maxIdle", WithConnectionPool(1, -1, 0, 0)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := OpenWithOptions(ctx, tc.opt); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
