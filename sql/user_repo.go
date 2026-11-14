package sql

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// userRecord is the GORM persistence model for core.User.
type userRecord struct {
	ID        string        `gorm:"primaryKey;type:text"`
	Email     string        `gorm:"index"`
	FullName  string        `gorm:"column:full_name"`
	Role      core.UserRole `gorm:"default:'user'"`
	Tier      string        `gorm:"default:'free'"`
	IsActive  bool          `gorm:"default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName overrides the GORM table name to users.
func (userRecord) TableName() string {
	return "users"
}

func toUserRecord(u *core.User) *userRecord {
	if u == nil {
		return nil
	}
	return &userRecord{
		ID:        u.ID,
		Email:     u.Email,
		FullName:  u.FullName,
		Role:      u.Role,
		Tier:      u.Tier,
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

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
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

// UserStore implements core.UserStore backed by GORM.
type UserStore struct {
	db *gorm.DB
}

var _ core.UserStore = (*UserStore)(nil)

// NewUserStore constructs a SQL-backed UserStore.
func NewUserStore(db *gorm.DB) *UserStore {
	return &UserStore{db: db}
}

// User retrieves a user record by ID.
func (s *UserStore) User(ctx context.Context, id string) (*core.User, error) {
	var rec userRecord
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreUser(&rec), nil
}

// UpsertUser inserts or updates a user record, preserving an existing admin role.
func (s *UserStore) UpsertUser(ctx context.Context, user *core.User) error {
	if user == nil || user.ID == "" {
		return core.ErrValidation
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing userRecord
		err := tx.Where("id = ?", user.ID).First(&existing).Error
		if err == nil {
			if existing.Role == core.UserRoleAdmin && user.Role != core.UserRoleAdmin {
				user.Role = core.UserRoleAdmin
			}
			if !existing.CreatedAt.IsZero() {
				user.CreatedAt = existing.CreatedAt
			}
			return mapError(tx.Save(toUserRecord(user)).Error)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return mapError(err)
		}
		return mapError(tx.Create(toUserRecord(user)).Error)
	})
}
