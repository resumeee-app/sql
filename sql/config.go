package sql

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DriverType represents the supported relational database engine.
type DriverType string

const (
	DriverSQLite   DriverType = "sqlite"
	DriverPostgres DriverType = "postgres"
	DriverMySQL    DriverType = "mysql"
)

// SSLMode represents the transport security mode for database connections.
type SSLMode string

const (
	SSLModeVerifyFull SSLMode = "verify-full"
	SSLModeVerifyCA   SSLMode = "verify-ca"
	SSLModeRequire    SSLMode = "require"
	SSLModeDisable    SSLMode = "disable"
)

// ConnectionPoolConfig encapsulates connection pool lifecycle parameters.
type ConnectionPoolConfig struct {
	MaxOpenConns    int           `json:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time"`
}

// Config defines the dialect-agnostic configuration contract for database persistence.
type Config struct {
	Driver          DriverType    `json:"driver"`
	DSN             string        `json:"dsn"`
	SSLMode         SSLMode       `json:"ssl_mode"`
	MaxOpenConns    int           `json:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time"`
	AutoMigrate     bool          `json:"auto_migrate"`
	DefaultUserID   string        `json:"default_user_id"`
}

// Validate validates database driver, DSN, and security mode constraints.
func (c *Config) Validate() error {
	driver := c.Driver
	if driver == "" {
		driver = DriverSQLite
	}

	switch driver {
	case DriverSQLite:
		return nil

	case DriverPostgres, DriverMySQL:
		if c.DSN == "" {
			return errors.New("dsn cannot be empty for remote database driver")
		}

		// Security enforcement: require TLS encryption for remote database engines
		if c.SSLMode == SSLModeDisable {
			host := extractHostFromDSN(c.DSN)
			if host == "" {
				return errors.New("security violation: cannot verify host for unencrypted database connection (sslmode=disable)")
			}
			if !isLocalHost(host) {
				return fmt.Errorf("security violation: unencrypted database connections (sslmode=disable) are prohibited for remote hosts (%s)", host)
			}
		}
		return nil

	default:
		return fmt.Errorf("unsupported database driver: %s", driver)
	}
}

var (
	// mysqlTCPRegex matches Go MySQL DSN TCP address syntax: user:pass@tcp(host:port)/dbname or tcp(host)/dbname
	mysqlTCPRegex = regexp.MustCompile(`@tcp\(([^)]+)\)`)
	// mysqlUnixRegex matches Go MySQL DSN Unix address syntax: user:pass@unix(/path/to/socket)/dbname or @/dbname
	mysqlUnixRegex = regexp.MustCompile(`@(unix\([^)]+\)|/)`)
)

func extractHostFromDSN(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return ""
	}

	// 1. URL schemes (postgres://, postgresql://, mysql://)
	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") || strings.HasPrefix(trimmed, "mysql://") {
		if u, err := url.Parse(trimmed); err == nil {
			return u.Hostname()
		}
	}

	// 2. Go MySQL DSN format: user:pass@tcp(host:port)/dbname
	if matches := mysqlTCPRegex.FindStringSubmatch(trimmed); len(matches) > 1 {
		addr := matches[1]
		if h, _, err := net.SplitHostPort(addr); err == nil {
			return h
		}
		return addr
	}

	// 3. Go MySQL DSN unix socket or default local socket: user:pass@unix(/path)/dbname or user:pass@/dbname
	if mysqlUnixRegex.MatchString(trimmed) {
		return "localhost"
	}

	// 4. Key-value pairs (e.g. "host=db.example.com ...")
	parts := strings.Fields(trimmed)
	for _, p := range parts {
		if strings.HasPrefix(p, "host=") {
			val := strings.TrimPrefix(p, "host=")
			val = strings.Trim(val, "'\"")
			return val
		}
	}

	return ""
}

func isLocalHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false // Fail closed: unresolvable or empty host is never assumed local
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}
