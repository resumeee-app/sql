package sqlite

import (
	"context"

	"gorm.io/gorm"

	"resumeee/internal/domain"
	"resumeee/internal/renderer"
)

type templateRepository struct {
	db *gorm.DB
}

// SafeFuncMap delegates to renderer.SafeFuncMap for safe, sandboxed template helper functions.
var SafeFuncMap = renderer.SafeFuncMap

// ValidateTemplateSet delegates pre-persistence dry-run validation to internal/renderer.
func ValidateTemplateSet(tmpl *domain.TemplateSet) error {
	return renderer.Validate(tmpl)
}

// NewTemplateRepository constructs a SQLite-backed TemplateRepository.
func NewTemplateRepository(db *gorm.DB) domain.TemplateRepository {
	return &templateRepository{db: db}
}

func (r *templateRepository) ListTemplates(ctx context.Context, userID string) ([]domain.TemplateSet, error) {
	var templates []domain.TemplateSet
	// Allow retrieving user-owned templates OR global built-in templates
	err := r.db.WithContext(ctx).
		Where("user_id = ? OR is_built_in = ?", userID, true).
		Find(&templates).Error
	return templates, mapError(err)
}

func (r *templateRepository) GetTemplate(ctx context.Context, userID, id string) (*domain.TemplateSet, error) {
	var tmpl domain.TemplateSet
	err := r.db.WithContext(ctx).
		Where("(user_id = ? OR is_built_in = ?) AND id = ?", userID, true, id).
		First(&tmpl).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &tmpl, nil
}

func (r *templateRepository) CreateTemplate(ctx context.Context, tmpl *domain.TemplateSet) error {
	if err := ValidateTemplateSet(tmpl); err != nil {
		return err
	}
	return mapError(r.db.WithContext(ctx).Create(tmpl).Error)
}

func (r *templateRepository) UpdateTemplate(ctx context.Context, tmpl *domain.TemplateSet) error {
	if err := ValidateTemplateSet(tmpl); err != nil {
		return err
	}
	return mapError(r.db.WithContext(ctx).Save(tmpl).Error)
}

func (r *templateRepository) DeleteTemplate(ctx context.Context, userID, id string) error {
	res := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).Delete(&domain.TemplateSet{})
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SafeFuncMapRender parses and executes an HTML template string against data with contextual auto-escaping.
func SafeFuncMapRender(tmplStr string, data any) (string, error) {
	return renderer.RenderFormat(domain.OutputFormatHTML, tmplStr, data)
}
