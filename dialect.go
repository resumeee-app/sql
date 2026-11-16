package sql

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ConfigureDialect executes engine-specific pragmas and session parameters.
// SQLite-specific PRAGMAs (WAL mode, foreign keys, busy timeouts) are strictly isolated here.
func ConfigureDialect(db *gorm.DB, driver DriverType, dsn string) error {
	if db == nil {
		return errors.New("database instance cannot be nil")
	}

	switch driver {
	case DriverSQLite, "":
		// Enforce SQLite pragmas
		if err := db.Exec("PRAGMA foreign_keys = ON;").Error; err != nil {
			return fmt.Errorf("failed to enable foreign keys pragma: %w", err)
		}
		if err := db.Exec("PRAGMA busy_timeout = 5000;").Error; err != nil {
			return fmt.Errorf("failed to set busy_timeout pragma: %w", err)
		}

		// WAL mode only applies to file-backed SQLite databases
		if !strings.Contains(dsn, ":memory:") && !strings.Contains(dsn, "mode=memory") {
			if err := db.Exec("PRAGMA journal_mode = WAL;").Error; err != nil {
				return fmt.Errorf("failed to enable WAL journal mode: %w", err)
			}
		}
		return nil

	case DriverPostgres, DriverMySQL:
		// Non-SQLite drivers do not support SQLite PRAGMAs; isolated for future dialect session tuning
		return nil

	default:
		return fmt.Errorf("unsupported driver type for dialect tuning: %s", driver)
	}
}

// ConfigureConnectionPool configures standard library *sql.DB connection pooling lifecycle parameters,
// falling back to safe defaults when unconfigured.
func ConfigureConnectionPool(sqlDB *sql.DB, driver DriverType, dsn string, pool ConnectionPoolConfig) {
	if sqlDB == nil {
		return
	}
	isMem := strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory")

	// MaxOpenConns
	maxOpen := pool.MaxOpenConns
	if maxOpen <= 0 {
		if (driver == DriverSQLite || driver == "") && isMem {
			maxOpen = 1
		} else {
			maxOpen = 25
		}
	}
	sqlDB.SetMaxOpenConns(maxOpen)

	// MaxIdleConns
	maxIdle := pool.MaxIdleConns
	if maxIdle <= 0 {
		if (driver == DriverSQLite || driver == "") && isMem {
			maxIdle = 1
		} else {
			maxIdle = 5
		}
	}
	sqlDB.SetMaxIdleConns(maxIdle)

	// ConnMaxLifetime
	maxLifetime := pool.ConnMaxLifetime
	if maxLifetime <= 0 {
		maxLifetime = 1 * time.Hour
	}
	sqlDB.SetConnMaxLifetime(maxLifetime)

	// ConnMaxIdleTime
	maxIdleTime := pool.ConnMaxIdleTime
	if maxIdleTime <= 0 {
		maxIdleTime = 15 * time.Minute
	}
	sqlDB.SetConnMaxIdleTime(maxIdleTime)
}
