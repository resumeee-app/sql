package sql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/resumeee-app/core/store"
	"gorm.io/gorm"
)

// Transaction retry and SQLite locking configuration parameters.
const (
	// sqliteBusyTimeoutMS configures per-connection SQLite lock wait duration in milliseconds.
	sqliteBusyTimeoutMS = 5000

	// maxTxRetries bounds automatic retries on transient SQLite lock and SQL SERIALIZABLE conflicts.
	maxTxRetries = 10

	// baseTxRetryDelay defines the initial backoff duration before retrying a contended transaction.
	baseTxRetryDelay = 4 * time.Millisecond
)

// Backend implements core/store.Backend using GORM with dialect-specific
// read/write transaction isolation and retry guarantees.
type Backend struct {
	db        *gorm.DB
	dialector string
}

var _ store.Backend = (*Backend)(nil)

// NewBackend constructs a store.Backend wrapping an initialized *gorm.DB connection.
func NewBackend(db *gorm.DB) (*Backend, error) {
	if db == nil {
		return nil, errors.New("sql: db cannot be nil")
	}
	dialector := ""
	if db.Dialector != nil {
		dialector = db.Name()
	}
	return &Backend{
		db:        db,
		dialector: dialector,
	}, nil
}

// DB exposes the underlying *gorm.DB handle for schema inspection and diagnostics.
func (b *Backend) DB() *gorm.DB {
	if b == nil {
		return nil
	}
	return b.db
}

// Close closes the underlying database connection pool.
func (b *Backend) Close() error {
	if b == nil || b.db == nil {
		return nil
	}
	sqlDB, err := b.db.DB()
	if err != nil {
		return err
	}
	if sqlDB == nil {
		return nil
	}
	return sqlDB.Close()
}

// Read executes fn within a side-effect-free read transaction, retrying transient lock
// or serialization conflicts cleanly.
func (b *Backend) Read(ctx context.Context, fn func(tx store.ReadTx) error) error {
	if b == nil || b.db == nil {
		return errors.New("sql: backend not initialized")
	}
	if fn == nil {
		return errors.New("sql: read callback cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return retryTx(ctx, func() error {
		var txOpts []*stdsql.TxOptions
		switch b.dialector {
		case string(DriverPostgres):
			txOpts = append(txOpts, &stdsql.TxOptions{
				Isolation: stdsql.LevelSerializable,
				ReadOnly:  true,
			})
		case string(DriverMySQL):
			txOpts = append(txOpts, &stdsql.TxOptions{
				Isolation: stdsql.LevelRepeatableRead,
				ReadOnly:  true,
			})
		}

		err := b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return fn(&readTx{db: tx})
		}, txOpts...)
		return mapError(err)
	})
}

// Write executes fn within an atomic write-serialized transaction.
// On SQLite it acquires an immediate write lock via BEGIN IMMEDIATE on a pinned connection;
// on PostgreSQL and MySQL it runs under SERIALIZABLE isolation with retry on transient aborts.
func (b *Backend) Write(ctx context.Context, fn func(tx store.Tx) error) error {
	if b == nil || b.db == nil {
		return errors.New("sql: backend not initialized")
	}
	if fn == nil {
		return errors.New("sql: write callback cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return retryTx(ctx, func() error {
		if b.dialector == string(DriverSQLite) {
			return b.writeSQLite(ctx, fn)
		}
		err := b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return fn(&writeTx{readTx: readTx{db: tx}})
		}, &stdsql.TxOptions{
			Isolation: stdsql.LevelSerializable,
		})
		return mapError(err)
	})
}

// writeSQLite pins a single database connection, configures busy/FK pragmas,
// starts a BEGIN IMMEDIATE write transaction, and guarantees detached rollback on failure.
func (b *Backend) writeSQLite(ctx context.Context, fn func(tx store.Tx) error) error {
	return b.db.WithContext(ctx).Connection(func(connDB *gorm.DB) error {
		bg := connDB.Session(&gorm.Session{Context: context.Background()})
		if err := bg.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", sqliteBusyTimeoutMS)).Error; err != nil {
			return mapError(err)
		}
		if err := bg.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
			return mapError(err)
		}

		scoped := connDB.Session(&gorm.Session{
			Context:                ctx,
			SkipDefaultTransaction: true,
		})
		if err := scoped.Exec("BEGIN IMMEDIATE").Error; err != nil {
			return mapError(err)
		}

		committed := false
		defer func() {
			if !committed {
				_ = bg.Exec("ROLLBACK").Error
			}
		}()

		wtx := &writeTx{readTx: readTx{db: scoped}}
		if err := fn(wtx); err != nil {
			return mapError(err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := scoped.Exec("COMMIT").Error; err != nil {
			return mapError(err)
		}
		committed = true
		return nil
	})
}

// retryTx executes attempt with bounded exponential backoff and jitter when a transient
// SQLite lock or SQL serialization/deadlock failure occurs and ctx has not been cancelled.
func retryTx(ctx context.Context, attempt func() error) error {
	var err error
	for i := 0; i < maxTxRetries; i++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		err = attempt()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		if !isSQLiteLockError(err) && !isSerializationError(err) {
			return err
		}
		if i == maxTxRetries-1 {
			break
		}
		delay := baseTxRetryDelay << min(i, 5)
		jitter := time.Duration(rand.Int63n(int64(baseTxRetryDelay) + 1))
		timer := time.NewTimer(delay + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}
