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
	ID             string                 `gorm:"primaryKey;type:text"`
	UserID         string                 `gorm:"index;not null"`
	Status         core.PDFParseJobStatus `gorm:"index;not null"`
	Filename       string
	Provider       string
	Model          string
	RawText        string
	ReservationID  string
	Result         *core.JSONResume `gorm:"serializer:json"`
	ErrorMessage   string
	AttemptCount   int
	ClaimedBy      string `gorm:"index"`
	ClaimedAt      *time.Time
	LeaseExpiresAt *time.Time `gorm:"index"`
	NextRetryAt    *time.Time `gorm:"index"`
	ExpiresAt      *time.Time `gorm:"index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
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
		ID:             j.ID,
		UserID:         j.UserID,
		Status:         j.Status,
		Filename:       j.Filename,
		Provider:       j.Provider,
		Model:          j.Model,
		RawText:        j.RawText,
		ReservationID:  j.ReservationID,
		Result:         j.Result,
		ErrorMessage:   j.ErrorMessage,
		AttemptCount:   j.AttemptCount,
		ClaimedBy:      j.ClaimedBy,
		ClaimedAt:      j.ClaimedAt,
		LeaseExpiresAt: j.LeaseExpiresAt,
		NextRetryAt:    j.NextRetryAt,
		ExpiresAt:      j.ExpiresAt,
		CreatedAt:      j.CreatedAt,
		UpdatedAt:      j.UpdatedAt,
		CompletedAt:    j.CompletedAt,
	}
}

func toCorePDFParseJob(r *pdfParseJobRecord) *core.PDFParseJob {
	if r == nil {
		return nil
	}
	return &core.PDFParseJob{
		ID:             r.ID,
		UserID:         r.UserID,
		Status:         r.Status,
		Filename:       r.Filename,
		Provider:       r.Provider,
		Model:          r.Model,
		RawText:        r.RawText,
		ReservationID:  r.ReservationID,
		Result:         r.Result,
		ErrorMessage:   r.ErrorMessage,
		AttemptCount:   r.AttemptCount,
		ClaimedBy:      r.ClaimedBy,
		ClaimedAt:      r.ClaimedAt,
		LeaseExpiresAt: r.LeaseExpiresAt,
		NextRetryAt:    r.NextRetryAt,
		ExpiresAt:      r.ExpiresAt,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		CompletedAt:    r.CompletedAt,
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

	now := time.Now().UTC()
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
		if rec.Status.IsTerminal() {
			return fmt.Errorf("%w: cannot transition terminal job %s from %s to %s", core.ErrConflict, jobID, rec.Status, status)
		}

		now := time.Now().UTC().UTC()
		rec.Status = status
		rec.UpdatedAt = now
		if errMsg != "" {
			rec.ErrorMessage = errMsg
		}
		if result != nil {
			rec.Result = result
		}
		if status.IsTerminal() {
			rec.RawText = ""
			rec.CompletedAt = &now
			rec.LeaseExpiresAt = nil
			rec.NextRetryAt = nil
		}

		if err := tx.Save(&rec).Error; err != nil {
			return mapError(err)
		}
		return nil
	})
}

// FailInterruptedJobs marks all pending and processing jobs as failed with the given reason upon server restart.
func (s *PDFJobStore) FailInterruptedJobs(ctx context.Context, reason string) error {
	now := time.Now().UTC().UTC()
	updates := map[string]any{
		"status":           core.PDFParseJobStatusFailed,
		"error_message":    reason,
		"raw_text":         "",
		"updated_at":       now,
		"completed_at":     &now,
		"lease_expires_at": nil,
		"next_retry_at":    nil,
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

// CleanupCompletedJobs purges completed, failed, or cancelled PDF parse jobs older than the retention threshold.
func (s *PDFJobStore) CleanupCompletedJobs(ctx context.Context, userID string, olderThan time.Duration) error {
	if userID == "" {
		return fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND status IN ? AND (completed_at < ? OR (completed_at IS NULL AND updated_at < ?))",
			userID,
			[]core.PDFParseJobStatus{core.PDFParseJobStatusCompleted, core.PDFParseJobStatusFailed, core.PDFParseJobStatusCancelled},
			cutoff, cutoff,
		).
		Delete(&pdfParseJobRecord{}).Error
	return mapError(err)
}
