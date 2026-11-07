package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"resumeee/internal/domain"
	"resumeee/internal/repository"
)

// New initializes a GORM SQLite database connection using the pure-Go SQLite driver,
// enables WAL mode, turns on foreign keys, and executes automatic schema migrations.
func New(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:      logger.Default.LogMode(logger.Silent),
		PrepareStmt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", dsn, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve generic sql.DB: %w", err)
	}

	// For in-memory databases with shared cache, pool connections appropriately
	if strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory") {
		sqlDB.SetMaxOpenConns(1)
	} else {
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetConnMaxLifetime(1 * time.Hour)
		sqlDB.SetConnMaxIdleTime(15 * time.Minute)
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
	if err := db.WithContext(ctx).AutoMigrate(
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
		&domain.PDFParseJob{},
		&domain.User{},
		&domain.TenantQuota{},
		&domain.LLMUsageRecord{},
	); err != nil {
		return err
	}

	// Ensure secondary indexes on join tables for reverse lookups by skill_id
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_experience_skills_skill_id ON experience_skills (skill_id);",
		"CREATE INDEX IF NOT EXISTS idx_project_skills_skill_id ON project_skills (skill_id);",
		"CREATE INDEX IF NOT EXISTS idx_fact_skills_skill_id ON fact_skills (skill_id);",
		"CREATE INDEX IF NOT EXISTS idx_skills_global ON skills (is_global);",
		"CREATE INDEX IF NOT EXISTS idx_resume_versions_user_id ON resume_versions (user_id);",
		"CREATE INDEX IF NOT EXISTS idx_skills_slug ON skills (slug);",
	}
	for _, idx := range indexes {
		if err := db.WithContext(ctx).Exec(idx).Error; err != nil {
			return fmt.Errorf("creating join table index: %w", err)
		}
	}

	return nil
}

// mapError converts GORM errors into canonical domain errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %v", domain.ErrNotFound, err)
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fmt.Errorf("%w: %v", domain.ErrConflict, err)
	}
	return err
}

// OpenStore initializes a SQLite database connection at dbPath, ensures parent directories exist,
// seeds default data idempotently, and bundles all SQLite repository implementations into a repository.Store.
//
// Parameters:
//   - ctx: Context controlling cancellation during startup and seeding.
//   - dbPath: Filesystem path to SQLite database or memory URI (":memory:").
//   - defaultUserID: Optional target user identifier for initial seed records (defaults to "default").
//
// Returns:
//   - repository.Store: Initialized SQLite store bundle.
//   - error: Non-nil if directory creation, database initialization, or seeding fails.
func OpenStore(ctx context.Context, dbPath string, defaultUserID ...string) (repository.Store, error) {
	if dbPath != ":memory:" && !strings.Contains(dbPath, "mode=memory") {
		dir := filepath.Dir(dbPath)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0750); err != nil {
				return nil, fmt.Errorf("creating db directory: %w", err)
			}
		}
	}

	db, err := New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("initializing database: %w", err)
	}

	targetUserID := "default"
	if len(defaultUserID) > 0 && defaultUserID[0] != "" {
		targetUserID = defaultUserID[0]
	}

	if err := SeedDefaultData(ctx, db, targetUserID); err != nil {
		return nil, fmt.Errorf("seeding default data: %w", err)
	}

	sqlDB, err := db.DB()
	closer := func() error {
		if err == nil && sqlDB != nil {
			return sqlDB.Close()
		}
		return nil
	}

	return &repository.StoreBundle{
		ProfileRepo:  NewProfileRepository(db),
		PresetRepo:   NewPresetRepository(db),
		TemplateRepo: NewTemplateRepository(db),
		BranchRepo:   NewBranchRepository(db),
		PDFJobRepo:   NewPDFParseJobRepository(db),
		QuotaRepo:    NewQuotaRepository(db),
		Closer:       closer,
	}, nil
}
