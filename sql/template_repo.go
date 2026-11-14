package sql

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

type templateRepository struct {
	db *gorm.DB
}

// NewTemplateRepository constructs a SQLite-backed TemplateRepository.
func NewTemplateRepository(db *gorm.DB) core.TemplateRepository {
	return &templateRepository{db: db}
}

// ListTemplates returns user-owned and global built-in templates.
func (r *templateRepository) ListTemplates(ctx context.Context, userID string) ([]core.TemplateSet, error) {
	var templates []core.TemplateSet
	// Allow retrieving user-owned templates OR global built-in templates
	err := r.db.WithContext(ctx).
		Where("user_id = ? OR is_built_in = ? OR user_id = ?", userID, true, core.SystemUserID).
		Find(&templates).Error
	return templates, mapError(err)
}

// GetTemplate fetches a template by user ID or built-in status and template ID.
func (r *templateRepository) GetTemplate(ctx context.Context, userID, id string) (*core.TemplateSet, error) {
	var tmpl core.TemplateSet
	err := r.db.WithContext(ctx).
		Where("(user_id = ? OR is_built_in = ? OR user_id = ?) AND id = ?", userID, true, core.SystemUserID, id).
		First(&tmpl).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &tmpl, nil
}

// CreateTemplate inserts a new template set without rendering engine validation.
func (r *templateRepository) CreateTemplate(ctx context.Context, tmpl *core.TemplateSet) error {
	if tmpl == nil {
		return fmt.Errorf("%w: template set cannot be nil", core.ErrValidation)
	}
	return mapError(r.db.WithContext(ctx).Create(tmpl).Error)
}

// UpdateTemplate saves modifications for an existing template set without rendering engine validation.
func (r *templateRepository) UpdateTemplate(ctx context.Context, tmpl *core.TemplateSet) error {
	if tmpl == nil {
		return fmt.Errorf("%w: template set cannot be nil", core.ErrValidation)
	}
	if tmpl.IsBuiltIn || strings.HasPrefix(tmpl.ID, "builtin-") || tmpl.UserID == core.SystemUserID {
		return core.ErrStrategyImmutable
	}
	var existing core.TemplateSet
	if err := r.db.WithContext(ctx).Where("id = ?", tmpl.ID).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if existing.IsBuiltIn || strings.HasPrefix(existing.ID, "builtin-") || existing.UserID == core.SystemUserID {
		return core.ErrStrategyImmutable
	}
	if existing.UserID != tmpl.UserID {
		return core.ErrForbidden
	}
	return mapError(r.db.WithContext(ctx).Save(tmpl).Error)
}

// DeleteTemplate removes a custom template set.
func (r *templateRepository) DeleteTemplate(ctx context.Context, userID, id string) error {
	var existing core.TemplateSet
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if existing.IsBuiltIn || strings.HasPrefix(existing.ID, "builtin-") || existing.UserID == core.SystemUserID {
		return core.ErrStrategyImmutable
	}
	if existing.UserID != userID {
		return core.ErrForbidden
	}
	res := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).Delete(&core.TemplateSet{})
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}
