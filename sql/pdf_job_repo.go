package sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// pdfParseJobRepository implements core.PDFParseJobRepository backed by SQLite and GORM.
type pdfParseJobRepository struct {
	db *gorm.DB
}

// NewPDFParseJobRepository constructs a new SQLite-backed PDFParseJobRepository.
func NewPDFParseJobRepository(db *gorm.DB) core.PDFParseJobRepository {
	return &pdfParseJobRepository{db: db}
}

// CreateJob persists a new PDFParseJob entity in the database.
func (r *pdfParseJobRepository) CreateJob(ctx context.Context, job *core.PDFParseJob) error {
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

	if err := r.db.WithContext(ctx).Create(job).Error; err != nil {
		return mapError(err)
	}
	return nil
}

// GetJob retrieves a PDFParseJob by its unique ID scoped to the authenticated user ID.
func (r *pdfParseJobRepository) GetJob(ctx context.Context, userID, jobID string) (*core.PDFParseJob, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}
	if jobID == "" {
		return nil, fmt.Errorf("%w: job id cannot be empty", core.ErrValidation)
	}

	var job core.PDFParseJob
	query := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", jobID, userID)

	if err := query.First(&job).Error; err != nil {
		return nil, mapError(err)
	}
	return &job, nil
}

// UpdateJobStatus updates the lifecycle state, result payload, and error message of a parse job.
func (r *pdfParseJobRepository) UpdateJobStatus(ctx context.Context, jobID string, status core.PDFParseJobStatus, result any, errMsg string) error {
	if jobID == "" {
		return fmt.Errorf("%w: job id cannot be empty", core.ErrValidation)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job core.PDFParseJob
		if err := tx.Where("id = ?", jobID).First(&job).Error; err != nil {
			return mapError(err)
		}

		now := time.Now().UTC()
		job.Status = status
		job.UpdatedAt = now
		if errMsg != "" {
			job.ErrorMessage = errMsg
		}
		if result != nil {
			job.Result = result
		}
		if status == core.PDFParseJobStatusCompleted || status == core.PDFParseJobStatusFailed {
			job.CompletedAt = &now
		}

		if err := tx.Save(&job).Error; err != nil {
			return mapError(err)
		}
		return nil
	})
}

// FailInterruptedJobs marks all pending and processing jobs as failed with the given reason upon server restart.
func (r *pdfParseJobRepository) FailInterruptedJobs(ctx context.Context, reason string) error {
	now := time.Now()
	updates := map[string]any{
		"status":        core.PDFParseJobStatusFailed,
		"error_message": reason,
		"updated_at":    now,
		"completed_at":  &now,
	}

	err := r.db.WithContext(ctx).
		Model(&core.PDFParseJob{}).
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
func (r *pdfParseJobRepository) CleanupCompletedJobs(ctx context.Context, userID string, olderThan time.Duration) error {
	if userID == "" {
		return fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND status IN ? AND (completed_at < ? OR (completed_at IS NULL AND updated_at < ?))",
			userID,
			[]core.PDFParseJobStatus{core.PDFParseJobStatusCompleted, core.PDFParseJobStatusFailed},
			cutoff, cutoff,
		).
		Delete(&core.PDFParseJob{}).Error
	return mapError(err)
}
