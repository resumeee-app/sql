package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"resumeee/internal/domain"
)

// New initializes a GORM SQLite database connection using the pure-Go SQLite driver,
// enables WAL mode, turns on foreign keys, and executes automatic schema migrations.
func New(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", dsn, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve generic sql.DB: %w", err)
	}

	// For in-memory databases with shared cache, pool connections appropriately
	if strings.Contains(dsn, ":memory:") {
		sqlDB.SetMaxOpenConns(1)
	}

	// Enforce SQLite pragmas
	if err := db.Exec("PRAGMA foreign_keys = ON;").Error; err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys pragma: %w", err)
	}
	if err := db.Exec("PRAGMA busy_timeout = 5000;").Error; err != nil {
		return nil, fmt.Errorf("failed to set busy_timeout pragma: %w", err)
	}
	// WAL mode only applies to file-backed databases
	if !strings.Contains(dsn, ":memory:") {
		if err := db.Exec("PRAGMA journal_mode = WAL;").Error; err != nil {
			return nil, fmt.Errorf("failed to enable WAL journal mode: %w", err)
		}
	}

	if err := AutoMigrate(context.Background(), db); err != nil {
		return nil, fmt.Errorf("failed to auto-migrate schema: %w", err)
	}

	return db, nil
}

// AutoMigrate migrates all entity tables and join tables defined by domain models.
func AutoMigrate(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).AutoMigrate(
		&domain.Profile{},
		&domain.Skill{},
		&domain.WorkExperience{},
		&domain.Project{},
		&domain.Education{},
		&domain.Certification{},
		&domain.Fact{},
		&domain.Preset{},
		&domain.TemplateSet{},
		&domain.Branch{},
		&domain.BranchVersion{},
	)
}

// mapError converts GORM errors into canonical domain errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %v", domain.ErrNotFound, err)
	}
	return err
}
