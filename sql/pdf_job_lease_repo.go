package sql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// SubmitPendingJob atomically enforces admission caps, applies default expiration metadata, and inserts a pending PDF parse job.
func (s *PDFJobStore) SubmitPendingJob(ctx context.Context, job *core.PDFParseJob, opts core.PendingJobOptions) error {
	if err := validatePendingPDFJobInput(job, opts); err != nil {
		return err
	}

	now := resolveNowTime(opts.Now)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&pdfParseJobRecord{}).Where("id = ?", job.ID).Count(&count).Error; err != nil {
			return mapError(err)
		}
		if count > 0 {
			return core.ErrConflict
		}
		if err := checkPendingAdmissionTx(tx, job.UserID, "", opts, now); err != nil {
			return err
		}

		_, _, ttl := resolvePendingLimits(opts)
		job.Status = core.PDFParseJobStatusPending
		if job.CreatedAt.IsZero() {
			job.CreatedAt = now
		}
		job.UpdatedAt = now
		if job.ExpiresAt == nil {
			exp := now.Add(ttl)
			job.ExpiresAt = &exp
		}
		job.ClaimedBy = ""
		job.ClaimedAt = nil
		job.LeaseExpiresAt = nil
		job.CompletedAt = nil

		return mapError(tx.Create(toPDFParseJobRecord(job)).Error)
	})
}

// validatePendingPDFJobInput checks required job fields, raw-text byte bounds, and assigns a default ID when empty.
func validatePendingPDFJobInput(job *core.PDFParseJob, opts core.PendingJobOptions) error {
	if job == nil {
		return fmt.Errorf("%w: job cannot be nil", core.ErrValidation)
	}
	if job.Status != "" && job.Status != core.PDFParseJobStatusPending {
		return fmt.Errorf("%w: pending job cannot start in %s state", core.ErrValidation, job.Status)
	}
	if job.UserID == "" {
		return fmt.Errorf("%w: user id is required", core.ErrValidation)
	}
	maxRawBytes := opts.MaxRawTextBytes
	if maxRawBytes <= 0 {
		maxRawBytes = core.DefaultMaxPDFRawTextBytes
	}
	if len(job.RawText) > maxRawBytes {
		return fmt.Errorf("%w: raw text exceeds maximum size of %d bytes", core.ErrValidation, maxRawBytes)
	}
	if job.ID == "" {
		job.ID = core.GenerateID("pdf")
	}
	return nil
}

// CancelJob transitions an owned pending PDF parse job to cancelled, prunes transient input text, and returns the record.
func (s *PDFJobStore) CancelJob(ctx context.Context, userID, jobID, reason string) (*core.PDFParseJob, error) {
	if userID == "" || jobID == "" {
		return nil, fmt.Errorf("%w: user id and job id cannot be empty", core.ErrValidation)
	}

	var out *core.PDFParseJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec pdfParseJobRecord
		if err := tx.Where("id = ?", jobID).First(&rec).Error; err != nil {
			return mapError(err)
		}
		if rec.UserID != userID {
			return core.ErrNotFound
		}
		if rec.Status != core.PDFParseJobStatusPending {
			return fmt.Errorf("%w: cannot cancel job in %s state", core.ErrConflict, rec.Status)
		}

		if _, _, err := transitionPDFJobTerminalTx(tx, &rec, core.PDFParseJobStatusCancelled, reason, time.Now().UTC(), true); err != nil {
			return err
		}
		out = toCorePDFParseJob(&rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ClaimNextJob atomically claims the fairest eligible pending PDF parse job for a worker lease.
func (s *PDFJobStore) ClaimNextJob(ctx context.Context, req core.ClaimJobRequest) (*core.PDFParseJob, error) {
	if req.WorkerID == "" {
		return nil, fmt.Errorf("%w: worker id cannot be empty", core.ErrValidation)
	}

	now := resolveNowTime(req.Now)
	leaseDur := req.LeaseDuration
	if leaseDur <= 0 {
		leaseDur = core.DefaultLeaseDuration
	}

	var out *core.PDFParseJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		candidates, byID, err := findEligiblePDFCandidatesTx(tx, req, now)
		if err != nil {
			return err
		}
		winnerID, ok := selectFairestCandidate(candidates, req.LastServedSeq)
		if !ok {
			return nil
		}
		out, err = acquirePDFJobLeaseTx(tx, byID[winnerID], req.WorkerID, now, leaseDur)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// findEligiblePDFCandidatesTx gathers claimable pending PDF jobs enforcing expiry, retry readiness, and cross-queue concurrency.
func findEligiblePDFCandidatesTx(tx *gorm.DB, req core.ClaimJobRequest, now time.Time) ([]claimCandidate, map[string]*pdfParseJobRecord, error) {
	otherVersionByTenant, _, activeByTenant, err := activeProcessingByTenantTx(tx, now)
	if err != nil {
		return nil, nil, err
	}
	// Use persisted branch lease counts unless the caller supplies an explicit sibling override.
	otherByTenant := req.OtherActiveByTenant
	if otherByTenant == nil {
		otherByTenant = otherVersionByTenant
	}

	var pending []pdfParseJobRecord
	if err := tx.Where("status = ?", core.PDFParseJobStatusPending).Find(&pending).Error; err != nil {
		return nil, nil, mapError(err)
	}

	var candidates []claimCandidate
	byID := make(map[string]*pdfParseJobRecord, len(pending))
	for i := range pending {
		job := &pending[i]
		if !isPendingUnexpired(job.ExpiresAt, now) || !isRetryReady(job.NextRetryAt, now) {
			continue
		}
		if req.MaxConcurrentPerTenant > 0 && activeByTenant[job.UserID]+otherByTenant[job.UserID] >= req.MaxConcurrentPerTenant {
			continue
		}
		candidates = append(candidates, claimCandidate{
			id:        job.ID,
			tenantID:  job.UserID,
			createdAt: job.CreatedAt,
		})
		byID[job.ID] = job
	}
	return candidates, byID, nil
}

// acquirePDFJobLeaseTx atomically transitions winner from pending to processing under workerID's lease.
func acquirePDFJobLeaseTx(tx *gorm.DB, winner *pdfParseJobRecord, workerID string, now time.Time, leaseDur time.Duration) (*core.PDFParseJob, error) {
	leaseExp := now.Add(leaseDur)
	res := tx.Model(&pdfParseJobRecord{}).
		Where("id = ? AND status = ?", winner.ID, core.PDFParseJobStatusPending).
		Updates(map[string]any{
			"status":           core.PDFParseJobStatusProcessing,
			"claimed_by":       workerID,
			"claimed_at":       &now,
			"lease_expires_at": &leaseExp,
			"next_retry_at":    nil,
			"attempt_count":    gorm.Expr("attempt_count + 1"),
			"updated_at":       now,
		})
	if res.Error != nil {
		return nil, mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}

	winner.Status = core.PDFParseJobStatusProcessing
	winner.ClaimedBy = workerID
	winner.ClaimedAt = &now
	winner.LeaseExpiresAt = &leaseExp
	winner.NextRetryAt = nil
	winner.AttemptCount++
	winner.UpdatedAt = now
	return toCorePDFParseJob(winner), nil
}

// RenewJobLease extends an active processing job lease owned by claimedBy.
func (s *PDFJobStore) RenewJobLease(ctx context.Context, jobID, claimedBy string, leaseExpiresAt time.Time) error {
	if jobID == "" || claimedBy == "" || leaseExpiresAt.IsZero() {
		return core.ErrValidation
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := claimedPDFJobTx(tx, jobID, claimedBy); err != nil {
			return err
		}
		exp := leaseExpiresAt.UTC()
		return mapError(tx.Model(&pdfParseJobRecord{}).
			Where("id = ?", jobID).
			Updates(map[string]any{
				"lease_expires_at": &exp,
				"updated_at":       time.Now().UTC(),
			}).Error)
	})
}

// RequeueJobForRetry returns a transiently failed claimed job back to pending for future retry prior to expiration.
func (s *PDFJobStore) RequeueJobForRetry(ctx context.Context, jobID, claimedBy string, nextRetryAt time.Time, errMsg string) error {
	if jobID == "" || claimedBy == "" || nextRetryAt.IsZero() {
		return core.ErrValidation
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := claimedPDFJobTx(tx, jobID, claimedBy)
		if err != nil {
			return err
		}
		retryAt := nextRetryAt.UTC()
		if job.ExpiresAt != nil && !retryAt.Before(*job.ExpiresAt) {
			return core.ErrJobExpired
		}

		return mapError(tx.Model(&pdfParseJobRecord{}).
			Where("id = ?", jobID).
			Updates(map[string]any{
				"status":           core.PDFParseJobStatusPending,
				"error_message":    errMsg,
				"next_retry_at":    &retryAt,
				"claimed_by":       "",
				"claimed_at":       nil,
				"lease_expires_at": nil,
				"completed_at":     nil,
				"updated_at":       time.Now().UTC(),
			}).Error)
	})
}

// CompleteClaimedJob finalizes a claimed processing PDF job and prunes transient raw input text.
func (s *PDFJobStore) CompleteClaimedJob(ctx context.Context, jobID, claimedBy string, result *core.JSONResume) error {
	if jobID == "" || claimedBy == "" {
		return core.ErrValidation
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := claimedPDFJobTx(tx, jobID, claimedBy)
		if err != nil {
			return err
		}
		if result != nil {
			// Marshal explicitly because map/column Update bypasses GORM's struct serializer tag.
			raw, err := json.Marshal(result)
			if err != nil {
				return fmt.Errorf("failed to marshal pdf result: %w", err)
			}
			job.Result = result
			if err := tx.Model(&pdfParseJobRecord{}).Where("id = ?", jobID).Update("result", string(raw)).Error; err != nil {
				return mapError(err)
			}
		}
		_, _, err = transitionPDFJobTerminalTx(tx, job, core.PDFParseJobStatusCompleted, "", time.Now().UTC(), false)
		return err
	})
}

// FailClaimedJob transitions an active claimed job to failed and prunes transient raw input text.
func (s *PDFJobStore) FailClaimedJob(ctx context.Context, jobID, claimedBy, errMsg string) error {
	if jobID == "" || claimedBy == "" {
		return core.ErrValidation
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := claimedPDFJobTx(tx, jobID, claimedBy)
		if err != nil {
			return err
		}
		_, _, err = transitionPDFJobTerminalTx(tx, job, core.PDFParseJobStatusFailed, errMsg, time.Now().UTC(), false)
		return err
	})
}

// ExpirePendingJobs fails pending PDF parse jobs whose ExpiresAt deadline has passed and returns released reservations.
func (s *PDFJobStore) ExpirePendingJobs(ctx context.Context, now time.Time, reason string) ([]core.ReleasedReservation, error) {
	ts := resolveNowTime(now)
	var released []core.ReleasedReservation

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var pending []pdfParseJobRecord
		if err := tx.Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?", core.PDFParseJobStatusPending, ts).
			Find(&pending).Error; err != nil {
			return mapError(err)
		}
		for i := range pending {
			rel, ok, err := transitionPDFJobTerminalTx(tx, &pending[i], core.PDFParseJobStatusFailed, reason, ts, true)
			if err != nil {
				return err
			}
			if ok {
				released = append(released, rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// FailExpiredJobLeases fails processing PDF parse jobs whose lease has expired and returns released reservations.
func (s *PDFJobStore) FailExpiredJobLeases(ctx context.Context, now time.Time, reason string) ([]core.ReleasedReservation, error) {
	ts := resolveNowTime(now)
	var released []core.ReleasedReservation

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var processing []pdfParseJobRecord
		if err := tx.Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", core.PDFParseJobStatusProcessing, ts).
			Find(&processing).Error; err != nil {
			return mapError(err)
		}
		for i := range processing {
			rel, ok, err := transitionPDFJobTerminalTx(tx, &processing[i], core.PDFParseJobStatusFailed, reason, ts, false)
			if err != nil {
				return err
			}
			if ok {
				released = append(released, rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// RecoverInterruptedJobs fails interrupted processing jobs and expired pending jobs, preserving valid pending work.
func (s *PDFJobStore) RecoverInterruptedJobs(ctx context.Context, reason string) ([]core.ReleasedReservation, error) {
	ts := time.Now().UTC()
	var released []core.ReleasedReservation

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var records []pdfParseJobRecord
		if err := tx.Where("status = ? OR (status = ? AND expires_at IS NOT NULL AND expires_at <= ?)",
			core.PDFParseJobStatusProcessing, core.PDFParseJobStatusPending, ts,
		).Find(&records).Error; err != nil {
			return mapError(err)
		}
		for i := range records {
			rel, ok, err := transitionPDFJobTerminalTx(tx, &records[i], core.PDFParseJobStatusFailed, reason, ts, false)
			if err != nil {
				return err
			}
			if ok {
				released = append(released, rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// claimedPDFJobTx loads jobID inside tx and verifies active processing lease ownership by claimedBy.
func claimedPDFJobTx(tx *gorm.DB, jobID, claimedBy string) (*pdfParseJobRecord, error) {
	var job pdfParseJobRecord
	if err := tx.Where("id = ?", jobID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, mapError(err)
	}
	if job.Status != core.PDFParseJobStatusProcessing || job.ClaimedBy != claimedBy {
		return nil, fmt.Errorf("%w: job %s is not actively claimed by %q", core.ErrLeaseExpired, jobID, claimedBy)
	}
	return &job, nil
}

// transitionPDFJobTerminalTx marks job terminal, prunes transient RawText, clears lease metadata, and returns any ReleasedReservation.
func transitionPDFJobTerminalTx(tx *gorm.DB, job *pdfParseJobRecord, status core.PDFParseJobStatus, reason string, ts time.Time, clearClaim bool) (core.ReleasedReservation, bool, error) {
	updates := map[string]any{
		"status":           status,
		"error_message":    reason,
		"raw_text":         "",
		"updated_at":       ts,
		"completed_at":     &ts,
		"lease_expires_at": nil,
		"next_retry_at":    nil,
	}
	if clearClaim {
		updates["claimed_by"] = ""
		updates["claimed_at"] = nil
		job.ClaimedBy = ""
		job.ClaimedAt = nil
	}
	if err := tx.Model(&pdfParseJobRecord{}).Where("id = ?", job.ID).Updates(updates).Error; err != nil {
		return core.ReleasedReservation{}, false, mapError(err)
	}

	job.Status = status
	job.ErrorMessage = reason
	job.RawText = ""
	job.UpdatedAt = ts
	completedAt := ts
	job.CompletedAt = &completedAt
	job.LeaseExpiresAt = nil
	job.NextRetryAt = nil

	if job.ReservationID == "" {
		return core.ReleasedReservation{}, false, nil
	}
	return core.ReleasedReservation{
		JobID:         job.ID,
		UserID:        job.UserID,
		ReservationID: job.ReservationID,
		Reason:        reason,
	}, true, nil
}
