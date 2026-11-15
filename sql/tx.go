package sql

import (
	"github.com/resumeee-app/resumeee-core/store"
	"gorm.io/gorm"
)

// readTx implements store.ReadTx over an active GORM transaction handle.
type readTx struct {
	db *gorm.DB
}

// writeTx implements store.Tx by extending readTx with GORM persistence mutations.
type writeTx struct {
	readTx
}

var (
	_ store.ReadTx = (*readTx)(nil)
	_ store.Tx     = (*writeTx)(nil)
)

// session returns an isolated statement session bound to the transaction connection and context.
func (r *readTx) session() *gorm.DB {
	return r.db.Session(&gorm.Session{Context: r.db.Statement.Context})
}
