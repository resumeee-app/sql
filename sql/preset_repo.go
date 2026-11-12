package sql

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

type presetRepository struct {
	db *gorm.DB
}

// NewPresetRepository constructs a SQLite-backed PresetRepository.
func NewPresetRepository(db *gorm.DB) core.PresetRepository {
	return &presetRepository{db: db}
}

// ListPresets retrieves all presets registered for the user plus system built-in presets.
func (r *presetRepository) ListPresets(ctx context.Context, userID string) ([]core.Preset, error) {
	var presets []core.Preset
	err := r.db.WithContext(ctx).Where("user_id = ? OR user_id = ? OR id LIKE 'builtin-%'", userID, core.SystemUserID).Find(&presets).Error
	return presets, mapError(err)
}

// GetPreset retrieves a preset by user and preset ID (or system built-in preset).
func (r *presetRepository) GetPreset(ctx context.Context, userID, id string) (*core.Preset, error) {
	var preset core.Preset
	err := r.db.WithContext(ctx).Where("(user_id = ? OR user_id = ? OR id LIKE 'builtin-%') AND id = ?", userID, core.SystemUserID, id).First(&preset).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &preset, nil
}

// CreatePreset inserts a new preset record.
func (r *presetRepository) CreatePreset(ctx context.Context, preset *core.Preset) error {
	return mapError(r.db.WithContext(ctx).Create(preset).Error)
}

// UpdatePreset saves changes to an existing preset record.
func (r *presetRepository) UpdatePreset(ctx context.Context, preset *core.Preset) error {
	if preset != nil && (preset.IsBuiltIn() || strings.HasPrefix(preset.ID, "builtin-") || preset.UserID == core.SystemUserID) {
		return core.ErrStrategyImmutable
	}
	return mapError(r.db.WithContext(ctx).Save(preset).Error)
}

// DeletePreset removes a preset record by user and preset ID.
func (r *presetRepository) DeletePreset(ctx context.Context, userID, id string) error {
	if strings.HasPrefix(id, "builtin-") || userID == core.SystemUserID {
		return core.ErrStrategyImmutable
	}
	existing, err := r.GetPreset(ctx, userID, id)
	if err == nil && existing != nil && (existing.IsBuiltIn() || strings.HasPrefix(existing.ID, "builtin-") || existing.UserID == core.SystemUserID) {
		return core.ErrStrategyImmutable
	}
	res := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).Delete(&core.Preset{})
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}
