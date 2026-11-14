package sql

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// presetRecord is the GORM persistence model for core.Preset.
type presetRecord struct {
	ID                 string  `gorm:"primaryKey;type:text"`
	UserID             string  `gorm:"index;not null"`
	Name               string  `gorm:"not null"`
	Description        string  `gorm:"column:description"`
	SystemInstructions string  `gorm:"not null"`
	SuggestedThemeID   *string `gorm:"index"`
	TemplateSetID      string  `gorm:"column:template_set_id"`
	Language           string  `gorm:"default:'English';not null"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TableName overrides the GORM table name to tailoring_strategies.
func (presetRecord) TableName() string {
	return "tailoring_strategies"
}

// Normalize synchronizes language defaults and template/theme aliases prior to persistence.
func (r *presetRecord) Normalize() error {
	p := toCorePreset(r)
	if err := p.Normalize(); err != nil {
		return err
	}
	r.Language = p.Language
	r.SuggestedThemeID = p.SuggestedThemeID
	r.TemplateSetID = p.TemplateSetID
	return nil
}

func toPresetRecord(p *core.Preset) *presetRecord {
	if p == nil {
		return nil
	}
	return &presetRecord{
		ID:                 p.ID,
		UserID:             p.UserID,
		Name:               p.Name,
		Description:        p.Description,
		SystemInstructions: p.SystemInstructions,
		SuggestedThemeID:   p.SuggestedThemeID,
		TemplateSetID:      p.TemplateSetID,
		Language:           p.Language,
		CreatedAt:          p.CreatedAt,
		UpdatedAt:          p.UpdatedAt,
	}
}

func toCorePreset(r *presetRecord) *core.Preset {
	if r == nil {
		return nil
	}
	return &core.Preset{
		ID:                 r.ID,
		UserID:             r.UserID,
		Name:               r.Name,
		Description:        r.Description,
		SystemInstructions: r.SystemInstructions,
		SuggestedThemeID:   r.SuggestedThemeID,
		TemplateSetID:      r.TemplateSetID,
		Language:           r.Language,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

// PresetStore implements core.PresetStore backed by GORM.
type PresetStore struct {
	db *gorm.DB
}

var _ core.PresetStore = (*PresetStore)(nil)

// NewPresetStore constructs a SQL-backed PresetStore.
func NewPresetStore(db *gorm.DB) *PresetStore {
	return &PresetStore{db: db}
}

// ListPresets retrieves all presets registered for the user plus system built-in presets.
func (s *PresetStore) ListPresets(ctx context.Context, userID string) ([]core.Preset, error) {
	var records []presetRecord
	if err := s.db.WithContext(ctx).Where("user_id = ? OR user_id = ? OR id LIKE 'builtin-%'", userID, core.SystemUserID).Find(&records).Error; err != nil {
		return nil, mapError(err)
	}
	presets := make([]core.Preset, len(records))
	for i := range records {
		presets[i] = *toCorePreset(&records[i])
	}
	return presets, nil
}

// Preset retrieves a preset by user and preset ID (or system built-in preset).
func (s *PresetStore) Preset(ctx context.Context, userID, id string) (*core.Preset, error) {
	var rec presetRecord
	err := s.db.WithContext(ctx).Where("(user_id = ? OR user_id = ? OR id LIKE 'builtin-%') AND id = ?", userID, core.SystemUserID, id).First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCorePreset(&rec), nil
}

// CreatePreset inserts a new preset record.
func (s *PresetStore) CreatePreset(ctx context.Context, preset *core.Preset) error {
	if preset == nil {
		return core.ErrValidation
	}
	return mapError(s.db.WithContext(ctx).Create(toPresetRecord(preset)).Error)
}

// UpdatePreset saves changes to an existing preset record.
func (s *PresetStore) UpdatePreset(ctx context.Context, preset *core.Preset) error {
	if preset == nil {
		return core.ErrValidation
	}
	if preset.IsBuiltIn() {
		return core.ErrStrategyImmutable
	}
	return mapError(s.db.WithContext(ctx).Save(toPresetRecord(preset)).Error)
}

// DeletePreset removes a preset record by user and preset ID.
func (s *PresetStore) DeletePreset(ctx context.Context, userID, id string) error {
	if strings.HasPrefix(id, "builtin-") || userID == core.SystemUserID {
		return core.ErrStrategyImmutable
	}
	existing, err := s.Preset(ctx, userID, id)
	if err == nil && existing != nil && existing.IsBuiltIn() {
		return core.ErrStrategyImmutable
	}
	res := s.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).Delete(&presetRecord{})
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}
