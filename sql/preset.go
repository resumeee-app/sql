package sql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/resumeee-app/resumeee-core"
)

// presetRecord maps core.Preset to the relational tailoring_strategies table.
type presetRecord struct {
	ID                 string    `gorm:"primaryKey;type:text"`
	UserID             string    `gorm:"index;not null"`
	Name               string    `gorm:"not null"`
	Description        string    `gorm:"column:description"`
	SystemInstructions string    `gorm:"not null"`
	SuggestedThemeID   *string   `gorm:"index"`
	Language           string    `gorm:"column:language;not null"`
	CreatedAt          time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for presetRecord.
func (presetRecord) TableName() string {
	return "tailoring_strategies"
}

// toPresetRecord converts a domain Preset into its relational record representation.
func toPresetRecord(p *core.Preset) *presetRecord {
	var theme *string
	if p.SuggestedThemeID != nil {
		cp := *p.SuggestedThemeID
		theme = &cp
	}
	return &presetRecord{
		ID:                 p.ID,
		UserID:             p.UserID,
		Name:               p.Name,
		Description:        p.Description,
		SystemInstructions: p.SystemInstructions,
		SuggestedThemeID:   theme,
		Language:           p.Language,
		CreatedAt:          normalizeUTCTime(p.CreatedAt),
		UpdatedAt:          normalizeUTCTime(p.UpdatedAt),
	}
}

// toCorePreset converts a persisted presetRecord into a domain Preset.
func toCorePreset(r *presetRecord) *core.Preset {
	if r == nil {
		return nil
	}
	var theme *string
	if r.SuggestedThemeID != nil {
		cp := *r.SuggestedThemeID
		theme = &cp
	}
	return &core.Preset{
		ID:                 r.ID,
		UserID:             r.UserID,
		Name:               r.Name,
		Description:        r.Description,
		SystemInstructions: r.SystemInstructions,
		SuggestedThemeID:   theme,
		Language:           r.Language,
		CreatedAt:          normalizeUTCTime(r.CreatedAt),
		UpdatedAt:          normalizeUTCTime(r.UpdatedAt),
	}
}

// ListPresets returns presets whose UserID matches userIDs or whose ID starts with any prefix in idPrefixes, deduplicated.
func (r *readTx) ListPresets(ctx context.Context, userIDs []string, idPrefixes []string) ([]core.Preset, error) {
	var (
		clauses []string
		args    []any
	)
	if len(userIDs) > 0 {
		clauses = append(clauses, "user_id IN ?")
		args = append(args, userIDs)
	}
	for _, prefix := range idPrefixes {
		if prefix == "" {
			continue
		}
		clauses = append(clauses, "id LIKE ? ESCAPE '!'")
		args = append(args, escapeLikePrefix(prefix))
	}
	if len(clauses) == 0 {
		return []core.Preset{}, nil
	}

	var recs []presetRecord
	if err := r.session().
		Where("("+strings.Join(clauses, " OR ")+")", args...).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Preset, len(recs))
	for i := range recs {
		out[i] = *toCorePreset(&recs[i])
	}
	return out, nil
}

// Preset loads a preset record by ID.
func (r *readTx) Preset(ctx context.Context, id string) (*core.Preset, error) {
	var rec presetRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCorePreset(&rec), nil
}

// PutPreset inserts or replaces a preset record.
func (w *writeTx) PutPreset(ctx context.Context, preset *core.Preset) error {
	if preset == nil {
		return fmt.Errorf("%w: preset cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toPresetRecord(preset)).Error)
}

// DeletePreset removes a preset record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeletePreset(ctx context.Context, id string) error {
	res := w.session().Where("id = ?", id).Delete(&presetRecord{})
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}
