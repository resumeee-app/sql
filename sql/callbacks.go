package sql

import (
	"errors"
	"reflect"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core/domain"
)

// RegisterNormalizationCallbacks registers GORM lifecycle callbacks that automatically invoke
// domain.Normalizable prior to record creation and updates, restoring domain entity purity.
func RegisterNormalizationCallbacks(db *gorm.DB) error {
	if db == nil {
		return errors.New("database instance cannot be nil")
	}

	callback := func(db *gorm.DB) {
		if db == nil || db.Statement == nil {
			return
		}
		normalizeValue(db, db.Statement.Dest)
		if db.Statement.Model != nil {
			normalizeValue(db, db.Statement.Model)
		}
	}

	if err := db.Callback().Create().Before("gorm:create").Register("resumeee:normalize_create", callback); err != nil {
		return err
	}
	if err := db.Callback().Update().Before("gorm:update").Register("resumeee:normalize_update", callback); err != nil {
		return err
	}
	return nil
}

func normalizeValue(db *gorm.DB, v any) {
	if v == nil || db == nil {
		return
	}

	// Direct interface check
	if n, ok := v.(domain.Normalizable); ok {
		if err := n.Normalize(); err != nil {
			_ = db.AddError(err)
		}
		return
	}

	// Reflection check for slices or pointer-to-structs
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}

	if val.Kind() == reflect.Slice {
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			if elem.CanAddr() {
				if n, ok := elem.Addr().Interface().(domain.Normalizable); ok {
					if err := n.Normalize(); err != nil {
						_ = db.AddError(err)
					}
				}
			} else if n, ok := elem.Interface().(domain.Normalizable); ok {
				if err := n.Normalize(); err != nil {
					_ = db.AddError(err)
				}
			}
		}
	}
}
