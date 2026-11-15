package sql

import (
	"strings"

	"github.com/resumeee-app/resumeee-core"
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

// deleteRecordByID removes the row matching id from table T or returns core.ErrNotFound when absent.
func deleteRecordByID[T any](tx *gorm.DB, id string) error {
	var model T
	res := tx.Where("id = ?", id).Delete(&model)
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}

// escapeLikePrefix escapes SQL LIKE wildcards in prefix using '!' and appends a trailing '%'.
func escapeLikePrefix(prefix string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(prefix) + "%"
}
