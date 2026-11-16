package sql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/resumeee-app/core"
)

// templateSetRecord maps core.TemplateSet to the relational template_sets table.
type templateSetRecord struct {
	ID               string    `gorm:"primaryKey;type:text"`
	UserID           string    `gorm:"index;not null"`
	Name             string    `gorm:"not null"`
	Description      string    `gorm:"column:description"`
	IsBuiltIn        bool      `gorm:"column:is_built_in;not null;default:false"`
	HTMLTemplate     string    `gorm:"not null"`
	CSSStyles        string    `gorm:"column:css_styles"`
	MarkdownTemplate string    `gorm:"not null"`
	TextTemplate     string    `gorm:"not null"`
	CreatedAt        time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for templateSetRecord.
func (templateSetRecord) TableName() string {
	return "template_sets"
}

// toTemplateSetRecord converts a domain TemplateSet into its relational record representation.
func toTemplateSetRecord(t *core.TemplateSet) *templateSetRecord {
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
		CreatedAt:        normalizeUTCTime(t.CreatedAt),
		UpdatedAt:        normalizeUTCTime(t.UpdatedAt),
	}
}

// toCoreTemplateSet converts a persisted templateSetRecord into a domain TemplateSet.
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
		CreatedAt:        normalizeUTCTime(r.CreatedAt),
		UpdatedAt:        normalizeUTCTime(r.UpdatedAt),
	}
}

// ListTemplates returns templates whose UserID matches userIDs, whose ID starts with any prefix in idPrefixes, or whose IsBuiltIn flag is true when includeBuiltIn is true, deduplicated.
func (r *readTx) ListTemplates(ctx context.Context, userIDs []string, idPrefixes []string, includeBuiltIn bool) ([]core.TemplateSet, error) {
	var (
		clauses []string
		args    []any
	)
	if len(userIDs) > 0 {
		clauses = append(clauses, "user_id IN ?")
		args = append(args, userIDs)
	}
	if includeBuiltIn {
		clauses = append(clauses, "is_built_in = ?")
		args = append(args, true)
	}
	for _, prefix := range idPrefixes {
		if prefix == "" {
			continue
		}
		clauses = append(clauses, "id LIKE ? ESCAPE '!'")
		args = append(args, escapeLikePrefix(prefix))
	}
	if len(clauses) == 0 {
		return []core.TemplateSet{}, nil
	}

	var recs []templateSetRecord
	if err := r.session().
		Where("("+strings.Join(clauses, " OR ")+")", args...).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.TemplateSet, len(recs))
	for i := range recs {
		out[i] = *toCoreTemplateSet(&recs[i])
	}
	return out, nil
}

// Template loads a template set record by ID.
func (r *readTx) Template(ctx context.Context, id string) (*core.TemplateSet, error) {
	var rec templateSetRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreTemplateSet(&rec), nil
}

// PutTemplate inserts or replaces a template set record.
func (w *writeTx) PutTemplate(ctx context.Context, tmpl *core.TemplateSet) error {
	if tmpl == nil {
		return fmt.Errorf("%w: template cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toTemplateSetRecord(tmpl)).Error)
}

// DeleteTemplate removes a template set record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeleteTemplate(ctx context.Context, id string) error {
	res := w.session().Where("id = ?", id).Delete(&templateSetRecord{})
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}
