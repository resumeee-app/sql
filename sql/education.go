package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/resumeee-core"
)

// educationRecord maps core.Education to the relational educations table.
type educationRecord struct {
	ID          string         `gorm:"primaryKey;type:text"`
	ShortID     string         `gorm:"column:short_id;index"`
	UserID      string         `gorm:"index;not null"`
	Institution string         `gorm:"not null"`
	Degree      string         `gorm:"column:degree"`
	Field       string         `gorm:"column:field"`
	StartDate   *core.Date     `gorm:"column:start_date;type:text"`
	EndDate     *core.Date     `gorm:"column:end_date;type:text"`
	Score       string         `gorm:"column:score"`
	Location    core.Location  `gorm:"serializer:json"`
	Metadata    map[string]any `gorm:"serializer:json"`
	CreatedAt   time.Time      `gorm:"autoCreateTime:false"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for educationRecord.
func (educationRecord) TableName() string {
	return "educations"
}

// toEducationRecord converts a domain Education into its relational record representation.
func toEducationRecord(e *core.Education) *educationRecord {
	var startDate, endDate *core.Date
	if e.StartDate != nil {
		cp := *e.StartDate
		startDate = &cp
	}
	if e.EndDate != nil {
		cp := *e.EndDate
		endDate = &cp
	}
	return &educationRecord{
		ID:          e.ID,
		ShortID:     e.ShortID,
		UserID:      e.UserID,
		Institution: e.Institution,
		Degree:      e.Degree,
		Field:       e.Field,
		StartDate:   startDate,
		EndDate:     endDate,
		Score:       e.Score,
		Location:    e.Location,
		Metadata:    e.Metadata,
		CreatedAt:   normalizeUTCTime(e.CreatedAt),
		UpdatedAt:   normalizeUTCTime(e.UpdatedAt),
	}
}

// toCoreEducation converts a persisted educationRecord into a domain Education.
func toCoreEducation(r *educationRecord) *core.Education {
	if r == nil {
		return nil
	}
	var startDate, endDate *core.Date
	if r.StartDate != nil {
		cp := *r.StartDate
		startDate = &cp
	}
	if r.EndDate != nil {
		cp := *r.EndDate
		endDate = &cp
	}
	return &core.Education{
		ID:          r.ID,
		ShortID:     r.ShortID,
		UserID:      r.UserID,
		Institution: r.Institution,
		Degree:      r.Degree,
		Field:       r.Field,
		StartDate:   startDate,
		EndDate:     endDate,
		Score:       r.Score,
		Location:    r.Location,
		Metadata:    r.Metadata,
		CreatedAt:   normalizeUTCTime(r.CreatedAt),
		UpdatedAt:   normalizeUTCTime(r.UpdatedAt),
	}
}

// ListEducations returns all education records owned by userID.
func (r *readTx) ListEducations(ctx context.Context, userID string) ([]core.Education, error) {
	var recs []educationRecord
	if err := r.session().Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Education, len(recs))
	for i := range recs {
		out[i] = *toCoreEducation(&recs[i])
	}
	return out, nil
}

// Education loads an education record by ID.
func (r *readTx) Education(ctx context.Context, id string) (*core.Education, error) {
	var rec educationRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreEducation(&rec), nil
}

// PutEducation inserts or replaces an education record.
func (w *writeTx) PutEducation(ctx context.Context, edu *core.Education) error {
	if edu == nil {
		return fmt.Errorf("%w: education cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toEducationRecord(edu)).Error)
}

// DeleteEducation deletes an education record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeleteEducation(ctx context.Context, id string) error {
	return deleteRecordByID[educationRecord](w.session(), id)
}
