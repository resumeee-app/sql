// Package sqlite provides SQLite persistence implementations for Resumeee repositories.
package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"resumeee/internal/domain"
)

// pdfParseJobRepository implements domain.PDFParseJobRepository backed by SQLite and GORM.
type pdfParseJobRepository struct {
	db *gorm.DB
}

// NewPDFParseJobRepository constructs a new SQLite-backed PDFParseJobRepository.
func NewPDFParseJobRepository(db *gorm.DB) domain.PDFParseJobRepository {
	return &pdfParseJobRepository{db: db}
}

// CreateJob persists a new PDFParseJob entity in the database.
func (r *pdfParseJobRepository) CreateJob(ctx context.Context, job *domain.PDFParseJob) error {
	if job == nil {
		return fmt.Errorf("%w: job cannot be nil", domain.ErrValidation)
	}
	if job.ID == "" || job.UserID == "" {
		return fmt.Errorf("%w: job id and user_id are required", domain.ErrValidation)
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
func (r *pdfParseJobRepository) GetJob(ctx context.Context, userID, jobID string) (*domain.PDFParseJob, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id cannot be empty", domain.ErrValidation)
	}
	if jobID == "" {
		return nil, fmt.Errorf("%w: job id cannot be empty", domain.ErrValidation)
	}

	var job domain.PDFParseJob
	query := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", jobID, userID)

	if err := query.First(&job).Error; err != nil {
		return nil, mapError(err)
	}
	return &job, nil
}

// UpdateJobStatus updates the lifecycle state, result payload, and error message of a parse job.
func (r *pdfParseJobRepository) UpdateJobStatus(ctx context.Context, jobID string, status domain.PDFParseJobStatus, result any, errMsg string) error {
	if jobID == "" {
		return fmt.Errorf("%w: job id cannot be empty", domain.ErrValidation)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job domain.PDFParseJob
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
		if status == domain.PDFParseJobStatusCompleted || status == domain.PDFParseJobStatusFailed {
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
		"status":        domain.PDFParseJobStatusFailed,
		"error_message": reason,
		"updated_at":    now,
		"completed_at":  &now,
	}

	err := r.db.WithContext(ctx).
		Model(&domain.PDFParseJob{}).
		Where("status IN ?", []domain.PDFParseJobStatus{
			domain.PDFParseJobStatusPending,
			domain.PDFParseJobStatusProcessing,
		}).
		Updates(updates).Error

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return mapError(err)
	}
	return nil
}
