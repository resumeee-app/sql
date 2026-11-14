package sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// pdfParseJobRecord is the GORM persistence model for core.PDFParseJob.
type pdfParseJobRecord struct {
	ID           string                 `gorm:"primaryKey;type:text"`
	UserID       string                 `gorm:"index;not null"`
	Status       core.PDFParseJobStatus `gorm:"index;not null"`
	Filename     string
	Provider     string
	Model        string
	Result       *core.JSONResume `gorm:"serializer:json"`
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CompletedAt  *time.Time
}

// TableName overrides the GORM table name to pdf_parse_jobs.
func (pdfParseJobRecord) TableName() string {
	return "pdf_parse_jobs"
}

func toPDFParseJobRecord(j *core.PDFParseJob) *pdfParseJobRecord {
	if j == nil {
		return nil
	}
	return &pdfParseJobRecord{
		ID:           j.ID,
		UserID:       j.UserID,
		Status:       j.Status,
		Filename:     j.Filename,
		Provider:     j.Provider,
		Model:        j.Model,
		Result:       j.Result,
		ErrorMessage: j.ErrorMessage,
		CreatedAt:    j.CreatedAt,
		UpdatedAt:    j.UpdatedAt,
		CompletedAt:  j.CompletedAt,
	}
}

func toCorePDFParseJob(r *pdfParseJobRecord) *core.PDFParseJob {
	if r == nil {
		return nil
	}
	return &core.PDFParseJob{
		ID:           r.ID,
		UserID:       r.UserID,
		Status:       r.Status,
		Filename:     r.Filename,
		Provider:     r.Provider,
		Model:        r.Model,
		Result:       r.Result,
		ErrorMessage: r.ErrorMessage,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		CompletedAt:  r.CompletedAt,
	}
}

// PDFJobStore implements core.PDFJobStore backed by GORM.
type PDFJobStore struct {
	db *gorm.DB
}

var _ core.PDFJobStore = (*PDFJobStore)(nil)

// NewPDFJobStore constructs a new SQL-backed PDFJobStore.
func NewPDFJobStore(db *gorm.DB) *PDFJobStore {
	return &PDFJobStore{db: db}
}

// CreateJob persists a new PDFParseJob entity in the database.
func (s *PDFJobStore) CreateJob(ctx context.Context, job *core.PDFParseJob) error {
	if job == nil {
		return fmt.Errorf("%w: job cannot be nil", core.ErrValidation)
	}
	if job.ID == "" || job.UserID == "" {
		return fmt.Errorf("%w: job id and user_id are required", core.ErrValidation)
	}

	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	if err := s.db.WithContext(ctx).Create(toPDFParseJobRecord(job)).Error; err != nil {
		return mapError(err)
	}
	return nil
}

// Job retrieves a PDFParseJob by its unique ID scoped to the authenticated user ID.
func (s *PDFJobStore) Job(ctx context.Context, userID, jobID string) (*core.PDFParseJob, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}
	if jobID == "" {
		return nil, fmt.Errorf("%w: job id cannot be empty", core.ErrValidation)
	}

	var rec pdfParseJobRecord
	query := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", jobID, userID)

	if err := query.First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCorePDFParseJob(&rec), nil
}

// UpdateJobStatus updates the lifecycle state, result payload, and error message of a parse job.
func (s *PDFJobStore) UpdateJobStatus(ctx context.Context, jobID string, status core.PDFParseJobStatus, result *core.JSONResume, errMsg string) error {
	if jobID == "" {
		return fmt.Errorf("%w: job id cannot be empty", core.ErrValidation)
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec pdfParseJobRecord
		if err := tx.Where("id = ?", jobID).First(&rec).Error; err != nil {
			return mapError(err)
		}

		now := time.Now().UTC()
		rec.Status = status
		rec.UpdatedAt = now
		if errMsg != "" {
			rec.ErrorMessage = errMsg
		}
		if result != nil {
			rec.Result = result
		}
		if status == core.PDFParseJobStatusCompleted || status == core.PDFParseJobStatusFailed {
			rec.CompletedAt = &now
		}

		if err := tx.Save(&rec).Error; err != nil {
			return mapError(err)
		}
		return nil
	})
}

// FailInterruptedJobs marks all pending and processing jobs as failed with the given reason upon server restart.
func (s *PDFJobStore) FailInterruptedJobs(ctx context.Context, reason string) error {
	now := time.Now()
	updates := map[string]any{
		"status":        core.PDFParseJobStatusFailed,
		"error_message": reason,
		"updated_at":    now,
		"completed_at":  &now,
	}

	err := s.db.WithContext(ctx).
		Model(&pdfParseJobRecord{}).
		Where("status IN ?", []core.PDFParseJobStatus{
			core.PDFParseJobStatusPending,
			core.PDFParseJobStatusProcessing,
		}).
		Updates(updates).Error

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return mapError(err)
	}
	return nil
}

// CleanupCompletedJobs purges completed or failed PDF parse jobs older than the retention threshold.
func (s *PDFJobStore) CleanupCompletedJobs(ctx context.Context, userID string, olderThan time.Duration) error {
	if userID == "" {
		return fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND status IN ? AND (completed_at < ? OR (completed_at IS NULL AND updated_at < ?))",
			userID,
			[]core.PDFParseJobStatus{core.PDFParseJobStatusCompleted, core.PDFParseJobStatusFailed},
			cutoff, cutoff,
		).
		Delete(&pdfParseJobRecord{}).Error
	return mapError(err)
}
