package sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/resumeee-app/resumeee-core"
	"gorm.io/gorm"
)

// profileRecord maps core.Profile to the relational profiles table.
type profileRecord struct {
	ID                 string            `gorm:"primaryKey;type:text"`
	UserID             string            `gorm:"uniqueIndex;not null"`
	FullName           string            `gorm:"not null"`
	Headline           string            `gorm:"column:headline"`
	Email              string            `gorm:"column:email"`
	Phone              string            `gorm:"column:phone"`
	Location           core.Location     `gorm:"serializer:json"`
	Profiles           []core.SocialLink `gorm:"serializer:json"`
	Summary            string            `gorm:"column:summary"`
	GlobalWritingRules string            `gorm:"column:global_writing_rules"`
	MainRevision       int64             `gorm:"column:main_revision;not null"`
	Metadata           map[string]any    `gorm:"serializer:json"`
	CreatedAt          time.Time         `gorm:"autoCreateTime:false"`
	UpdatedAt          time.Time         `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for profileRecord.
func (profileRecord) TableName() string {
	return "profiles"
}

// toProfileRecord converts a domain Profile into its relational record representation.
func toProfileRecord(p *core.Profile) *profileRecord {
	var links []core.SocialLink
	if p.Profiles != nil {
		links = append([]core.SocialLink{}, p.Profiles...)
	}
	return &profileRecord{
		ID:                 p.ID,
		UserID:             p.UserID,
		FullName:           p.FullName,
		Headline:           p.Headline,
		Email:              p.Email,
		Phone:              p.Phone,
		Location:           p.Location,
		Profiles:           links,
		Summary:            p.Summary,
		GlobalWritingRules: p.GlobalWritingRules,
		MainRevision:       p.MainRevision,
		Metadata:           p.Metadata,
		CreatedAt:          normalizeUTCTime(p.CreatedAt),
		UpdatedAt:          normalizeUTCTime(p.UpdatedAt),
	}
}

// toCoreProfile converts a persisted profileRecord into a domain Profile.
func toCoreProfile(r *profileRecord) *core.Profile {
	if r == nil {
		return nil
	}
	var links []core.SocialLink
	if r.Profiles != nil {
		links = append([]core.SocialLink{}, r.Profiles...)
	}
	return &core.Profile{
		ID:                 r.ID,
		UserID:             r.UserID,
		FullName:           r.FullName,
		Headline:           r.Headline,
		Email:              r.Email,
		Phone:              r.Phone,
		Location:           r.Location,
		Profiles:           links,
		Summary:            r.Summary,
		GlobalWritingRules: r.GlobalWritingRules,
		MainRevision:       r.MainRevision,
		Metadata:           r.Metadata,
		CreatedAt:          normalizeUTCTime(r.CreatedAt),
		UpdatedAt:          normalizeUTCTime(r.UpdatedAt),
	}
}

// GetProfile loads a candidate profile root by userID.
func (r *readTx) GetProfile(ctx context.Context, userID string) (*core.Profile, error) {
	var rec profileRecord
	if err := r.session().Where("user_id = ?", userID).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreProfile(&rec), nil
}

// PutProfile inserts or replaces a candidate profile root keyed by UserID.
func (w *writeTx) PutProfile(ctx context.Context, profile *core.Profile) error {
	if profile == nil {
		return fmt.Errorf("%w: profile cannot be nil", core.ErrValidation)
	}
	var existing profileRecord
	err := w.session().Select("id").Where("user_id = ?", profile.UserID).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return mapError(err)
	}
	// PutProfile keys ownership by user_id; remove any prior row if the replacement uses a new ID.
	if err == nil && existing.ID != profile.ID {
		if delErr := w.session().Where("id = ?", existing.ID).Delete(&profileRecord{}).Error; delErr != nil {
			return mapError(delErr)
		}
	}
	return mapError(w.session().Save(toProfileRecord(profile)).Error)
}
