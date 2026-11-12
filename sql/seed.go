package sql

import (
	"context"

	"gorm.io/gorm"
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
