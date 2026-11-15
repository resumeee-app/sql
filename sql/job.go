package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/resumeee-core"
)

// jobRecord maps unified core.Job instances to the relational jobs table.
type jobRecord struct {
	ID             string                 `gorm:"primaryKey;type:text"`
	Kind           core.JobKind           `gorm:"index;not null"`
	UserID         string                 `gorm:"index;not null"`
	Status         core.JobStatus         `gorm:"index;not null"`
	AttemptCount   int                    `gorm:"column:attempt_count;not null"`
	ErrorMessage   string                 `gorm:"column:error_message"`
	ReservationID  string                 `gorm:"column:reservation_id;index"`
	ExpiresAt      *time.Time             `gorm:"column:expires_at;index"`
	NextRetryAt    *time.Time             `gorm:"column:next_retry_at;index"`
	ClaimedBy      string                 `gorm:"column:claimed_by;index"`
	ClaimedAt      *time.Time             `gorm:"column:claimed_at"`
	LeaseExpiresAt *time.Time             `gorm:"column:lease_expires_at;index"`
	TailorBranchID string                 `gorm:"column:tailor_branch_id;index"`
	Tailor         *core.BranchTailorSpec `gorm:"serializer:json"`
	PDF            *core.PDFParseSpec     `gorm:"serializer:json"`
	CreatedAt      time.Time              `gorm:"index;autoCreateTime:false"`
	UpdatedAt      time.Time              `gorm:"autoUpdateTime:false"`
	CompletedAt    *time.Time             `gorm:"column:completed_at;index"`
}

// TableName overrides the GORM table name for jobRecord.
func (jobRecord) TableName() string {
	return "jobs"
}

// toJobRecord converts a domain Job into its relational record representation.
func toJobRecord(j *core.Job) *jobRecord {
	var tailorBranchID string
	var tailor *core.BranchTailorSpec
	if j.Tailor != nil {
		cp := *j.Tailor
		tailor = &cp
		tailorBranchID = j.Tailor.BranchID
	}
	var pdf *core.PDFParseSpec
	if j.PDF != nil {
		cp := *j.PDF
		pdf = &cp
	}
	return &jobRecord{
		ID:             j.ID,
		Kind:           j.Kind,
		UserID:         j.UserID,
		Status:         j.Status,
		AttemptCount:   j.AttemptCount,
		ErrorMessage:   j.ErrorMessage,
		ReservationID:  j.ReservationID,
		ExpiresAt:      normalizeUTCTimePtr(j.ExpiresAt),
		NextRetryAt:    normalizeUTCTimePtr(j.NextRetryAt),
		ClaimedBy:      j.ClaimedBy,
		ClaimedAt:      normalizeUTCTimePtr(j.ClaimedAt),
		LeaseExpiresAt: normalizeUTCTimePtr(j.LeaseExpiresAt),
		TailorBranchID: tailorBranchID,
		Tailor:         tailor,
		PDF:            pdf,
		CreatedAt:      normalizeUTCTime(j.CreatedAt),
		UpdatedAt:      normalizeUTCTime(j.UpdatedAt),
		CompletedAt:    normalizeUTCTimePtr(j.CompletedAt),
	}
}

// toCoreJob converts a persisted jobRecord into a domain Job.
func toCoreJob(r *jobRecord) *core.Job {
	if r == nil {
		return nil
	}
	var tailor *core.BranchTailorSpec
	if r.Tailor != nil {
		cp := *r.Tailor
		tailor = &cp
	}
	var pdf *core.PDFParseSpec
	if r.PDF != nil {
		cp := *r.PDF
		pdf = &cp
	}
	return &core.Job{
		ID:             r.ID,
		Kind:           r.Kind,
		UserID:         r.UserID,
		Status:         r.Status,
		AttemptCount:   r.AttemptCount,
		ErrorMessage:   r.ErrorMessage,
		ReservationID:  r.ReservationID,
		ExpiresAt:      normalizeUTCTimePtr(r.ExpiresAt),
		NextRetryAt:    normalizeUTCTimePtr(r.NextRetryAt),
		ClaimedBy:      r.ClaimedBy,
		ClaimedAt:      normalizeUTCTimePtr(r.ClaimedAt),
		LeaseExpiresAt: normalizeUTCTimePtr(r.LeaseExpiresAt),
		Tailor:         tailor,
		PDF:            pdf,
		CreatedAt:      normalizeUTCTime(r.CreatedAt),
		UpdatedAt:      normalizeUTCTime(r.UpdatedAt),
		CompletedAt:    normalizeUTCTimePtr(r.CompletedAt),
	}
}

// Job loads a queued background job by ID.
func (r *readTx) Job(ctx context.Context, id string) (*core.Job, error) {
	var rec jobRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreJob(&rec), nil
}

// ListActiveJobs returns all jobs whose Status is JobStatusPending or JobStatusProcessing.
func (r *readTx) ListActiveJobs(ctx context.Context) ([]core.Job, error) {
	var recs []jobRecord
	if err := r.session().
		Where("status IN ?", []core.JobStatus{core.JobStatusPending, core.JobStatusProcessing}).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Job, len(recs))
	for i := range recs {
		out[i] = *toCoreJob(&recs[i])
	}
	return out, nil
}

// ListJobsByBranch returns all branch-tailoring jobs targeting branchID.
func (r *readTx) ListJobsByBranch(ctx context.Context, branchID string) ([]core.Job, error) {
	var recs []jobRecord
	if err := r.session().
		Where("kind = ? AND tailor_branch_id = ?", core.JobKindBranchTailor, branchID).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Job, len(recs))
	for i := range recs {
		out[i] = *toCoreJob(&recs[i])
	}
	return out, nil
}

// ListJobsByUser returns jobs owned by userID matching any status in statuses, or all jobs owned by userID when statuses is empty.
func (r *readTx) ListJobsByUser(ctx context.Context, userID string, statuses []core.JobStatus) ([]core.Job, error) {
	q := r.session().Where("user_id = ?", userID)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var recs []jobRecord
	if err := q.Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Job, len(recs))
	for i := range recs {
		out[i] = *toCoreJob(&recs[i])
	}
	return out, nil
}

// PutJob inserts or replaces a background job record.
func (w *writeTx) PutJob(ctx context.Context, job *core.Job) error {
	if job == nil {
		return fmt.Errorf("%w: job cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toJobRecord(job)).Error)
}

// DeleteJob removes a background job record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeleteJob(ctx context.Context, id string) error {
	return deleteRecordByID[jobRecord](w.session(), id)
}
