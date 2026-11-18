package sql

import (
	"context"
	"fmt"
	"strings"

	"github.com/resumeee-app/core/seed"
	"github.com/resumeee-app/core/store"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open bootstraps a complete SQL-backed *store.Store using the provided configuration:
// validating connection settings, running auto-migration when enabled, seeding baseline
// templates/presets/global skills and default tenant resources, and returning *store.Store.
func Open(ctx context.Context, cfg Config) (*store.Store, error) {
	if cfg.Driver == "" {
		cfg.Driver = DriverSQLite
	}
	if cfg.DefaultUserID == "" {
		cfg.DefaultUserID = "default"
	}

	db, err := ResolveConnection(cfg)
	if err != nil {
		return nil, err
	}

	if cfg.AutoMigrate {
		if err := MigrateSchema(db); err != nil {
			closeUnderlyingDB(db)
			return nil, err
		}
	}

	backend, err := NewBackend(db)
	if err != nil {
		closeUnderlyingDB(db)
		return nil, err
	}

	st := store.New(backend)
	if err := SeedBaseline(ctx, st, cfg); err != nil {
		_ = st.Close()
		return nil, err
	}

	return st, nil
}

// OpenWithOptions constructs a Config from DefaultConfig() overridden by functional options
// and calls Open.
func OpenWithOptions(ctx context.Context, opts ...ConfigOption) (*store.Store, error) {
	cfg := DefaultConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, fmt.Errorf("sql: apply config option: %w", err)
		}
	}
	return Open(ctx, cfg)
}

// OpenStore provides a convenience constructor for opening an auto-migrated and seeded
// SQLite *store.Store at dbPath with optional ConfigOption overrides.
func OpenStore(ctx context.Context, dbPath string, opts ...ConfigOption) (*store.Store, error) {
	baseOpts := make([]ConfigOption, 0, 2+len(opts))
	baseOpts = append(baseOpts, WithDriver(DriverSQLite), WithDBPath(dbPath))
	baseOpts = append(baseOpts, opts...)
	return OpenWithOptions(ctx, baseOpts...)
}

// ResolveConnection validates cfg, opens the underlying SQL driver dialector, applies dialect
// tuning and connection pooling, and returns the initialized *gorm.DB handle.
func ResolveConnection(cfg Config) (*gorm.DB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	driver := cfg.Driver
	if driver == "" {
		driver = DriverSQLite
	}

	dsn := strings.TrimSpace(cfg.DSN)
	if driver == DriverSQLite && dsn == "" {
		dsn = "file::memory:?cache=shared"
	}

	var dialector gorm.Dialector
	switch driver {
	case DriverSQLite:
		dialector = sqlite.Open(dsn)
	default:
		return nil, fmt.Errorf("sql: driver %s is not registered in this build", driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("sql: connect (%s) dsn=%s: %w", driver, SanitizeDSN(dsn), err)
	}

	if err := ConfigureDialect(db, driver, dsn); err != nil {
		closeUnderlyingDB(db)
		return nil, err
	}

	if sqlDB, dbErr := db.DB(); dbErr == nil && sqlDB != nil {
		ConfigureConnectionPool(sqlDB, driver, dsn, poolConfig(cfg))
	}

	return db, nil
}

// MigrateSchema executes database schema migration and secondary/partial index creation.
func MigrateSchema(db *gorm.DB) error {
	return AutoMigrate(db)
}

// SeedBaseline idempotently seeds system templates, presets, global skills, and the configured
// default tenant into st.
func SeedBaseline(ctx context.Context, st *store.Store, cfg Config) error {
	userID := cfg.DefaultUserID
	if userID == "" {
		userID = "default"
	}
	if err := seed.DefaultData(ctx, st, userID); err != nil {
		return fmt.Errorf("sql: seed baseline data: %w", err)
	}
	return nil
}

// closeUnderlyingDB closes the raw database handle backing db when open initialization fails.
func closeUnderlyingDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil && sqlDB != nil {
		_ = sqlDB.Close()
	}
}
