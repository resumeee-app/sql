package sql

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core/domain"
)

type userRepository struct {
	db *gorm.DB
}

// NewUserRepository constructs a SQLite-backed UserRepository.
func NewUserRepository(db *gorm.DB) domain.UserRepository {
	return &userRepository{db: db}
}

// GetUser retrieves a user record by ID.
func (r *userRepository) GetUser(ctx context.Context, id string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &user, nil
}

// UpsertUser inserts or updates a user record, preserving an existing admin role.
func (r *userRepository) UpsertUser(ctx context.Context, user *domain.User) error {
	if user == nil || user.ID == "" {
		return domain.ErrValidation
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing domain.User
		err := tx.Where("id = ?", user.ID).First(&existing).Error
		if err == nil {
			if existing.Role == domain.UserRoleAdmin && user.Role != domain.UserRoleAdmin {
				user.Role = domain.UserRoleAdmin
			}
			if !existing.CreatedAt.IsZero() {
				user.CreatedAt = existing.CreatedAt
			}
			return mapError(tx.Save(user).Error)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return mapError(err)
		}
		return mapError(tx.Create(user).Error)
	})
}
