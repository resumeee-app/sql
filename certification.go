package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/core"
)

// certificationRecord maps core.Certification to the relational certifications table.
type certificationRecord struct {
	ID        string         `gorm:"primaryKey;type:text"`
	ShortID   string         `gorm:"column:short_id;index"`
	UserID    string         `gorm:"index;not null"`
	Name      string         `gorm:"not null"`
	Issuer    string         `gorm:"column:issuer"`
	Date      *core.Date     `gorm:"column:date;type:text"`
	URL       string         `gorm:"column:url"`
	Summary   string         `gorm:"column:summary"`
	Metadata  map[string]any `gorm:"serializer:json"`
	CreatedAt time.Time      `gorm:"autoCreateTime:false"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for certificationRecord.
func (certificationRecord) TableName() string {
	return "certifications"
}

// toCertificationRecord converts a domain Certification into its relational record representation.
func toCertificationRecord(c *core.Certification) *certificationRecord {
	var date *core.Date
	if c.Date != nil {
		cp := *c.Date
		date = &cp
	}
	return &certificationRecord{
		ID:        c.ID,
		ShortID:   c.ShortID,
		UserID:    c.UserID,
		Name:      c.Name,
		Issuer:    c.Issuer,
		Date:      date,
		URL:       c.URL,
		Summary:   c.Summary,
		Metadata:  c.Metadata,
		CreatedAt: normalizeUTCTime(c.CreatedAt),
		UpdatedAt: normalizeUTCTime(c.UpdatedAt),
	}
}

// toCoreCertification converts a persisted certificationRecord into a domain Certification.
func toCoreCertification(r *certificationRecord) *core.Certification {
	if r == nil {
		return nil
	}
	var date *core.Date
	if r.Date != nil {
		cp := *r.Date
		date = &cp
	}
	return &core.Certification{
		ID:        r.ID,
		ShortID:   r.ShortID,
		UserID:    r.UserID,
		Name:      r.Name,
		Issuer:    r.Issuer,
		Date:      date,
		URL:       r.URL,
		Summary:   r.Summary,
		Metadata:  r.Metadata,
		CreatedAt: normalizeUTCTime(r.CreatedAt),
		UpdatedAt: normalizeUTCTime(r.UpdatedAt),
	}
}

// ListCertifications returns all certification records owned by userID.
func (r *readTx) ListCertifications(ctx context.Context, userID string) ([]core.Certification, error) {
	var recs []certificationRecord
	if err := r.session().Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Certification, len(recs))
	for i := range recs {
		out[i] = *toCoreCertification(&recs[i])
	}
	return out, nil
}

// Certification loads a certification record by ID.
func (r *readTx) Certification(ctx context.Context, id string) (*core.Certification, error) {
	var rec certificationRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreCertification(&rec), nil
}

// PutCertification inserts or replaces a certification record.
func (w *writeTx) PutCertification(ctx context.Context, cert *core.Certification) error {
	if cert == nil {
		return fmt.Errorf("%w: certification cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toCertificationRecord(cert)).Error)
}

// DeleteCertification deletes a certification record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeleteCertification(ctx context.Context, id string) error {
	return deleteRecordByID[certificationRecord](w.session(), id)
}
