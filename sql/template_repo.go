package sql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// templateSetRecord is the GORM persistence model for core.TemplateSet.
type templateSetRecord struct {
	ID               string `gorm:"primaryKey;type:text"`
	UserID           string `gorm:"index;not null"`
	Name             string `gorm:"not null"`
	Description      string
	IsBuiltIn        bool   `gorm:"column:is_built_in"`
	HTMLTemplate     string `gorm:"not null"`
	CSSStyles        string
	MarkdownTemplate string `gorm:"not null"`
	TextTemplate     string `gorm:"not null"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TableName overrides the GORM table name to template_sets.
func (templateSetRecord) TableName() string {
	return "template_sets"
}

func toTemplateSetRecord(t *core.TemplateSet) *templateSetRecord {
	if t == nil {
		return nil
	}
	return &templateSetRecord{
		ID:               t.ID,
		UserID:           t.UserID,
		Name:             t.Name,
		Description:      t.Description,
		IsBuiltIn:        t.IsBuiltIn,
		HTMLTemplate:     t.HTMLTemplate,
		CSSStyles:        t.CSSStyles,
		MarkdownTemplate: t.MarkdownTemplate,
		TextTemplate:     t.TextTemplate,
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
	}
}

func toCoreTemplateSet(r *templateSetRecord) *core.TemplateSet {
	if r == nil {
		return nil
	}
	return &core.TemplateSet{
		ID:               r.ID,
		UserID:           r.UserID,
		Name:             r.Name,
		Description:      r.Description,
		IsBuiltIn:        r.IsBuiltIn,
		HTMLTemplate:     r.HTMLTemplate,
		CSSStyles:        r.CSSStyles,
		MarkdownTemplate: r.MarkdownTemplate,
		TextTemplate:     r.TextTemplate,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

// TemplateStore implements core.TemplateStore backed by GORM.
type TemplateStore struct {
	db *gorm.DB
}

var _ core.TemplateStore = (*TemplateStore)(nil)

// NewTemplateStore constructs a SQL-backed TemplateStore.
func NewTemplateStore(db *gorm.DB) *TemplateStore {
	return &TemplateStore{db: db}
}

// ListTemplates returns user-owned and global built-in templates.
func (s *TemplateStore) ListTemplates(ctx context.Context, userID string) ([]core.TemplateSet, error) {
	var records []templateSetRecord
	if err := s.db.WithContext(ctx).
		Where("user_id = ? OR is_built_in = ? OR user_id = ?", userID, true, core.SystemUserID).
		Find(&records).Error; err != nil {
		return nil, mapError(err)
	}
	templates := make([]core.TemplateSet, len(records))
	for i := range records {
		templates[i] = *toCoreTemplateSet(&records[i])
	}
	return templates, nil
}

// Template fetches a template by user ID or built-in status and template ID.
func (s *TemplateStore) Template(ctx context.Context, userID, id string) (*core.TemplateSet, error) {
	var rec templateSetRecord
	err := s.db.WithContext(ctx).
		Where("(user_id = ? OR is_built_in = ? OR user_id = ?) AND id = ?", userID, true, core.SystemUserID, id).
		First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreTemplateSet(&rec), nil
}

// CreateTemplate inserts a new template set without rendering engine validation.
func (s *TemplateStore) CreateTemplate(ctx context.Context, tmpl *core.TemplateSet) error {
	if tmpl == nil {
		return fmt.Errorf("%w: template set cannot be nil", core.ErrValidation)
	}
	return mapError(s.db.WithContext(ctx).Create(toTemplateSetRecord(tmpl)).Error)
}

// UpdateTemplate saves modifications for an existing template set without rendering engine validation.
func (s *TemplateStore) UpdateTemplate(ctx context.Context, tmpl *core.TemplateSet) error {
	if tmpl == nil {
		return fmt.Errorf("%w: template set cannot be nil", core.ErrValidation)
	}
	if tmpl.IsBuiltIn || strings.HasPrefix(tmpl.ID, "builtin-") || tmpl.UserID == core.SystemUserID {
		return core.ErrStrategyImmutable
	}
	var existing templateSetRecord
	if err := s.db.WithContext(ctx).Where("id = ?", tmpl.ID).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if existing.IsBuiltIn || strings.HasPrefix(existing.ID, "builtin-") || existing.UserID == core.SystemUserID {
		return core.ErrStrategyImmutable
	}
	if existing.UserID != tmpl.UserID {
		return core.ErrForbidden
	}
	return mapError(s.db.WithContext(ctx).Save(toTemplateSetRecord(tmpl)).Error)
}

// DeleteTemplate removes a custom template set.
func (s *TemplateStore) DeleteTemplate(ctx context.Context, userID, id string) error {
	var existing templateSetRecord
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if existing.IsBuiltIn || strings.HasPrefix(existing.ID, "builtin-") || existing.UserID == core.SystemUserID {
		return core.ErrStrategyImmutable
	}
	if existing.UserID != userID {
		return core.ErrForbidden
	}
	res := s.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).Delete(&templateSetRecord{})
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}
