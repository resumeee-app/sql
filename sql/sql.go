// Package sql implements a relational persistence backend for resumeee-core/store
// backed by GORM across SQLite, PostgreSQL, and MySQL.
//
// This package provides durable CRUD persistence, atomic transaction boundaries
// (including SQLite BEGIN IMMEDIATE write serialization and SQL SERIALIZABLE
// retry handling), schema migration, connection pooling, DSN redaction, and
// idempotent baseline store bootstrapping. All domain logic lives in resumeee-core.
package sql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/resumeee-app/resumeee-core"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// New opens an auto-migrated SQLite *gorm.DB for the given DSN with dialect tuning applied.
func New(dsn string) (*gorm.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("sql: dsn cannot be empty")
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("sql: open sqlite: %w", err)
	}
	if err := ConfigureDialect(db, DriverSQLite, dsn); err != nil {
		return nil, err
	}
	if sqlDB, dbErr := db.DB(); dbErr == nil && sqlDB != nil {
		ConfigureConnectionPool(sqlDB, DriverSQLite, dsn, poolConfig(DefaultConfig()))
	}
	if err := AutoMigrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

// poolConfig extracts ConnectionPoolConfig settings from cfg.
func poolConfig(cfg Config) ConnectionPoolConfig {
	return ConnectionPoolConfig{
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
	}
}

// AutoMigrate reconciles relational tables, unique constraints, and secondary/partial indexes.
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("sql: db cannot be nil")
	}
	models := []any{
		&userRecord{},
		&presetRecord{},
		&templateSetRecord{},
		&tenantQuotaRecord{},
		&llmUsageRecord{},
		&profileRecord{},
		&skillRecord{},
		&workExperienceRecord{},
		&projectRecord{},
		&educationRecord{},
		&certificationRecord{},
		&factRecord{},
		&branchRecord{},
		&branchVersionRecord{},
		&jobRecord{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("sql: auto-migrate schema: %w", err)
	}
	return ensureIndexes(db)
}

// ensureIndexes creates secondary join-table and active-job indexes not covered by struct tags.
func ensureIndexes(db *gorm.DB) error {
	joinIndexes := []struct {
		name   string
		table  string
		column string
	}{
		{name: "idx_experience_skills_skill_id", table: "experience_skills", column: "skill_id"},
		{name: "idx_project_skills_skill_id", table: "project_skills", column: "skill_id"},
		{name: "idx_fact_skills_skill_id", table: "fact_skills", column: "skill_id"},
	}
	for _, idx := range joinIndexes {
		if err := createIndex(db, idx.name, idx.table, idx.column, ""); err != nil {
			return err
		}
	}

	if db.Dialector != nil && db.Name() == string(DriverMySQL) {
		return createIndex(db, "idx_jobs_active", "jobs", "status, next_retry_at, created_at", "")
	}
	return createIndex(
		db,
		"idx_jobs_active",
		"jobs",
		"status, next_retry_at, created_at",
		"status IN ('pending', 'processing')",
	)
}

// createIndex idempotently creates an index using dialect-appropriate DDL.
func createIndex(db *gorm.DB, name, table, columns, whereClause string) error {
	if db.Dialector != nil && db.Name() == string(DriverMySQL) {
		var count int64
		if err := db.Raw(
			"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?",
			table, name,
		).Scan(&count).Error; err != nil {
			return fmt.Errorf("sql: check index %s on %s: %w", name, table, err)
		}
		if count > 0 {
			return nil
		}
		stmt := fmt.Sprintf("CREATE INDEX %s ON %s (%s)", name, table, columns)
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("sql: create index %s: %w", name, err)
		}
		return nil
	}

	stmt := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)", name, table, columns)
	if whereClause != "" {
		stmt += " WHERE " + whereClause
	}
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("sql: create index %s: %w", name, err)
	}
	return nil
}

// mapError translates GORM and SQL driver errors into canonical core domain sentinels.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, core.ErrNotFound) ||
		errors.Is(err, core.ErrConflict) ||
		errors.Is(err, core.ErrValidation) ||
		errors.Is(err, core.ErrForbidden) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return core.ErrNotFound
	}
	if isDuplicateKeyError(err) {
		return fmt.Errorf("%w: %v", core.ErrConflict, err)
	}
	return err
}

// isDuplicateKeyError reports whether err represents a primary key or unique index constraint failure.
func isDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "sqlite_constraint_unique") ||
		strings.Contains(msg, "sqlite_constraint_primarykey") ||
		strings.Contains(msg, "sqlstate 23505")
}

// isSQLiteLockError reports whether err is a transient SQLite lock or busy contention failure.
func isSQLiteLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "sqlite_locked")
}

// isSerializationError reports whether err represents a transient SQL serialization or deadlock abort.
func isSerializationError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "40001") ||
		strings.Contains(msg, "40p01") ||
		strings.Contains(msg, "could not serialize access") ||
		strings.Contains(msg, "deadlock detected") ||
		strings.Contains(msg, "deadlock found") ||
		strings.Contains(msg, "error 1213")
}

// normalizeUTCTime converts non-zero timestamps to UTC while preserving zero time.Time values.
func normalizeUTCTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC()
}

// normalizeUTCTimePtr returns a UTC copy of non-nil time pointers.
func normalizeUTCTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
