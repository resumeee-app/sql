package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/core"
)

// userRecord maps core.User to the relational users table.
type userRecord struct {
	ID        string        `gorm:"primaryKey;type:text"`
	Email     string        `gorm:"index"`
	FullName  string        `gorm:"column:full_name"`
	Role      core.UserRole `gorm:"column:role;not null"`
	Tier      string        `gorm:"column:tier"`
	IsActive  bool          `gorm:"column:is_active;not null;default:false"`
	CreatedAt time.Time     `gorm:"autoCreateTime:false"`
	UpdatedAt time.Time     `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for userRecord.
func (userRecord) TableName() string {
	return "users"
}

// toUserRecord converts a domain User into its relational record representation.
func toUserRecord(u *core.User) *userRecord {
	return &userRecord{
		ID:        u.ID,
		Email:     u.Email,
		FullName:  u.FullName,
		Role:      u.Role,
		Tier:      u.Tier,
		IsActive:  u.IsActive,
		CreatedAt: normalizeUTCTime(u.CreatedAt),
		UpdatedAt: normalizeUTCTime(u.UpdatedAt),
	}
}

// toCoreUser converts a persisted userRecord into a domain User.
func toCoreUser(r *userRecord) *core.User {
	if r == nil {
		return nil
	}
	return &core.User{
		ID:        r.ID,
		Email:     r.Email,
		FullName:  r.FullName,
		Role:      r.Role,
		Tier:      r.Tier,
		IsActive:  r.IsActive,
		CreatedAt: normalizeUTCTime(r.CreatedAt),
		UpdatedAt: normalizeUTCTime(r.UpdatedAt),
	}
}

// User loads a user record by ID.
func (r *readTx) User(ctx context.Context, id string) (*core.User, error) {
	var rec userRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreUser(&rec), nil
}

// PutUser inserts or replaces a user record.
func (w *writeTx) PutUser(ctx context.Context, user *core.User) error {
	if user == nil {
		return fmt.Errorf("%w: user cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toUserRecord(user)).Error)
}
