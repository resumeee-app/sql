package sql

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/resumeee-app/resumeee-core"
)

// Option defines a functional configuration option for Store bundles.
type Option func(*bundleOptions)

type bundleOptions struct {
	closer func() error
}

// WithCloser registers an explicit closer cleanup callback on the store bundle.
func WithCloser(closer func() error) Option {
	return func(o *bundleOptions) {
		o.closer = closer
	}
}

// ResolveConnection executes Phase 1 of store initialization:
// connection resolution, DSN credential sanitization, GORM lifecycle callbacks,
// connection pooling configuration, and dialect tuning.
func ResolveConnection(cfg Config) (*gorm.DB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid storage configuration: %w", err)
	}

	driver := cfg.Driver
	if driver == "" {
		driver = DriverSQLite
	}

	var db *gorm.DB
	var err error

	switch driver {
	case DriverSQLite:
		dsn := cfg.DSN
		if dsn == "" {
			dsn = ":memory:"
		}

		// Ensure parent directory exists for file-backed SQLite
		if dsn != ":memory:" && !strings.Contains(dsn, "mode=memory") {
			dir := filepath.Dir(dsn)
			if dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0750); err != nil {
					return nil, fmt.Errorf("creating database directory %s: %w", dir, err)
				}
			}
		}

		db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
			Logger:      logger.Default.LogMode(logger.Silent),
			PrepareStmt: true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to open sqlite database at %s: %w", SanitizeDSN(dsn), err)
		}

	case DriverPostgres, DriverMySQL:
		return nil, fmt.Errorf("driver %s requires external dialect driver (RFC-006 Phase 2)", driver)

	default:
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}

	// Register pure domain Normalizable lifecycle callbacks on GORM engine
	if err := RegisterNormalizationCallbacks(db); err != nil {
		if sqlDB, dbErr := db.DB(); dbErr == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
		return nil, fmt.Errorf("failed to register normalization callbacks: %w", err)
	}

	// Configure connection pooling on *sql.DB
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve sql.DB: %w", err)
	}
	if sqlDB == nil {
		return nil, errors.New("sql.DB instance is nil")
	}

	poolCfg := ConnectionPoolConfig{
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
	}
	ConfigureConnectionPool(sqlDB, driver, cfg.DSN, poolCfg)

	// Configure engine-specific pragmas and dialect tuning
	if err := ConfigureDialect(db, driver, cfg.DSN); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("failed to configure dialect: %w", err)
	}

	return db, nil
}

// MigrateSchema executes Phase 2 of store initialization:
// dialect-aware table migration and join index creation.
func MigrateSchema(ctx context.Context, db *gorm.DB, _ DriverType) error {
	if db == nil {
		return errors.New("database instance cannot be nil")
	}
	return AutoMigrate(ctx, db)
}

// SeedBaseline executes Phase 3 of store initialization:
// idempotent baseline seeding of system templates, presets, global skills, and default tenant.
func SeedBaseline(ctx context.Context, db *gorm.DB, cfg Config) error {
	if db == nil {
		return errors.New("database instance cannot be nil")
	}
	targetUserID := cfg.DefaultUserID
	if targetUserID == "" {
		targetUserID = "default"
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		profileRepo := NewProfileRepository(tx)
		branchRepo := NewBranchRepository(tx)
		templateRepo := NewTemplateRepository(tx)
		presetRepo := NewPresetRepository(tx)
		return core.SeedDefaultData(ctx, profileRepo, branchRepo, templateRepo, presetRepo, targetUserID)
	})
}

// NewStoreBundle executes Phase 4 of store initialization:
// instantiating repository handles with injected dependencies.
func NewStoreBundle(db *gorm.DB, opts ...Option) (core.Store, error) {
	if db == nil {
		return nil, errors.New("database instance cannot be nil")
	}

	var options bundleOptions
	for _, opt := range opts {
		opt(&options)
	}

	sqlDB, err := db.DB()
	closer := func() error {
		if options.closer != nil {
			return options.closer()
		}
		if err == nil && sqlDB != nil {
			return sqlDB.Close()
		}
		return nil
	}

	return &core.StoreBundle{
		ProfileRepo:  NewProfileRepository(db),
		PresetRepo:   NewPresetRepository(db),
		TemplateRepo: NewTemplateRepository(db),
		BranchRepo:   NewBranchRepository(db),
		PDFJobRepo:   NewPDFParseJobRepository(db),
		QuotaRepo:    NewQuotaRepository(db),
		UserRepo:     NewUserRepository(db),
		Closer:       closer,
	}, nil
}

// Open orchestrates the unified 4-phase store initialization pipeline:
// Phase 1: Connection resolution, credential masking, callbacks, and pooling.
// Phase 2: Schema auto-migration (if enabled).
// Phase 3: Idempotent baseline data seeding.
// Phase 4: Repository bundle instantiation.
func Open(ctx context.Context, cfg Config) (core.Store, error) {
	// Phase 1: Connection & Pool Resolution
	db, err := ResolveConnection(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolving database connection: %w", err)
	}

	closeOnErr := func(cause error) (core.Store, error) {
		if sqlDB, dbErr := db.DB(); dbErr == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
		return nil, cause
	}

	// Phase 2: Schema Migration
	if cfg.AutoMigrate {
		if err := MigrateSchema(ctx, db, cfg.Driver); err != nil {
			return closeOnErr(fmt.Errorf("migrating schema: %w", err))
		}
	}

	// Phase 3: Baseline Seeding
	if err := SeedBaseline(ctx, db, cfg); err != nil {
		return closeOnErr(fmt.Errorf("seeding baseline data: %w", err))
	}

	// Phase 4: Repository Bundle Instantiation
	store, err := NewStoreBundle(db)
	if err != nil {
		return closeOnErr(fmt.Errorf("instantiating store bundle: %w", err))
	}
	return store, nil
}

// OpenWithOptions initializes a core.Store applying functional ConfigOptions on top of DefaultConfig().
func OpenWithOptions(ctx context.Context, opts ...ConfigOption) (core.Store, error) {
	cfg := DefaultConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	return Open(ctx, cfg)
}
