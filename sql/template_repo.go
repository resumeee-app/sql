package sql

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core/domain"
)

type templateRepository struct {
	db *gorm.DB
}

// NewTemplateRepository constructs a SQLite-backed TemplateRepository.
func NewTemplateRepository(db *gorm.DB) domain.TemplateRepository {
	return &templateRepository{db: db}
}

// ListTemplates returns user-owned and global built-in templates.
func (r *templateRepository) ListTemplates(ctx context.Context, userID string) ([]domain.TemplateSet, error) {
	var templates []domain.TemplateSet
	// Allow retrieving user-owned templates OR global built-in templates
	err := r.db.WithContext(ctx).
		Where("user_id = ? OR is_built_in = ? OR user_id = ?", userID, true, domain.SystemUserID).
		Find(&templates).Error
	return templates, mapError(err)
}

// GetTemplate fetches a template by user ID or built-in status and template ID.
func (r *templateRepository) GetTemplate(ctx context.Context, userID, id string) (*domain.TemplateSet, error) {
	var tmpl domain.TemplateSet
	err := r.db.WithContext(ctx).
		Where("(user_id = ? OR is_built_in = ? OR user_id = ?) AND id = ?", userID, true, domain.SystemUserID, id).
		First(&tmpl).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &tmpl, nil
}

// CreateTemplate inserts a new template set without rendering engine validation.
func (r *templateRepository) CreateTemplate(ctx context.Context, tmpl *domain.TemplateSet) error {
	if tmpl == nil {
		return fmt.Errorf("%w: template set cannot be nil", domain.ErrValidation)
	}
	return mapError(r.db.WithContext(ctx).Create(tmpl).Error)
}

// UpdateTemplate saves modifications for an existing template set without rendering engine validation.
func (r *templateRepository) UpdateTemplate(ctx context.Context, tmpl *domain.TemplateSet) error {
	if tmpl == nil {
		return fmt.Errorf("%w: template set cannot be nil", domain.ErrValidation)
	}
	if tmpl.IsBuiltIn || strings.HasPrefix(tmpl.ID, "builtin-") || tmpl.UserID == domain.SystemUserID {
		return domain.ErrStrategyImmutable
	}
	var existing domain.TemplateSet
	if err := r.db.WithContext(ctx).Where("id = ?", tmpl.ID).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if existing.IsBuiltIn || strings.HasPrefix(existing.ID, "builtin-") || existing.UserID == domain.SystemUserID {
		return domain.ErrStrategyImmutable
	}
	if existing.UserID != tmpl.UserID {
		return domain.ErrForbidden
	}
	return mapError(r.db.WithContext(ctx).Save(tmpl).Error)
}

// DeleteTemplate removes a custom template set.
func (r *templateRepository) DeleteTemplate(ctx context.Context, userID, id string) error {
	var existing domain.TemplateSet
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if existing.IsBuiltIn || strings.HasPrefix(existing.ID, "builtin-") || existing.UserID == domain.SystemUserID {
		return domain.ErrStrategyImmutable
	}
	if existing.UserID != userID {
		return domain.ErrForbidden
	}
	res := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).Delete(&domain.TemplateSet{})
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
