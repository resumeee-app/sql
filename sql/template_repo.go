package sqlite

import (
	"context"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	"gorm.io/gorm"

	"resumeee/internal/domain"
)

type templateRepository struct {
	db *gorm.DB
}

// SafeFuncMap provides whitelisted helper functions for template rendering without filesystem or network access.
var SafeFuncMap = map[string]any{
	"lower":   strings.ToLower,
	"upper":   strings.ToUpper,
	"trim":    strings.TrimSpace,
	"join":    strings.Join,
	"split":   strings.Split,
	"replace": strings.ReplaceAll,
}

// ValidateTemplateSet parses HTML, Markdown, and PlainText templates to ensure syntactic validity,
// verify that only whitelisted functions are called, and enforce Go's html/template contextual auto-escaping.
func ValidateTemplateSet(tmpl *domain.TemplateSet) error {
	if tmpl.HTMLTemplate != "" {
		if _, err := htmltemplate.New("html").Funcs(SafeFuncMap).Parse(tmpl.HTMLTemplate); err != nil {
			return fmt.Errorf("%w in html_template: %v", domain.ErrTemplateSyntax, err)
		}
	}
	if tmpl.MarkdownTemplate != "" {
		if _, err := texttemplate.New("md").Funcs(SafeFuncMap).Parse(tmpl.MarkdownTemplate); err != nil {
			return fmt.Errorf("%w in markdown_template: %v", domain.ErrTemplateSyntax, err)
		}
	}
	if tmpl.TextTemplate != "" {
		if _, err := texttemplate.New("text").Funcs(SafeFuncMap).Parse(tmpl.TextTemplate); err != nil {
			return fmt.Errorf("%w in text_template: %v", domain.ErrTemplateSyntax, err)
		}
	}
	return nil
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
	tmpl, err := htmltemplate.New("render").Funcs(SafeFuncMap).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
