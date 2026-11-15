package sql

import (
	"context"
	"encoding/json"
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
	return runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		if err := ensureRecordIDAvailableTx[pdfParseJobRecord](tx, job.ID); err != nil {
			return err
		}
		if err := checkPendingAdmissionTx(tx, job.UserID, "", opts, now); err != nil {
			return err
		}

		dates := resolvePendingDates(job.CreatedAt, job.ExpiresAt, job.NextRetryAt, opts, now)
		job.Status = core.PDFParseJobStatusPending
		job.CreatedAt = dates.createdAt
		job.UpdatedAt = dates.updatedAt
		job.ExpiresAt = dates.expiresAt
		job.NextRetryAt = dates.nextRetryAt
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
	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var rec pdfParseJobRecord
		if err := withQueueLock(tx).Where("id = ?", jobID).First(&rec).Error; err != nil {
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
	leaseDur := resolveLeaseDuration(req.LeaseDuration)

	var out *core.PDFParseJob
	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		skipped := make(map[string]bool)
		for {
			candidates, err := findEligiblePDFCandidatesTx(tx, req, now, skipped)
			if err != nil {
				return err
			}
			winnerID, ok := selectFairestCandidate(candidates, req.LastServedSeq)
			if !ok {
				return nil
			}
			out, err = acquirePDFJobLeaseTx(tx, winnerID, req.WorkerID, now, leaseDur)
			if err != nil || out != nil {
				return err
			}
			skipped[winnerID] = true
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// findEligiblePDFCandidatesTx gathers claimable pending PDF jobs enforcing expiry, retry readiness, and cross-queue concurrency.
func findEligiblePDFCandidatesTx(tx *gorm.DB, req core.ClaimJobRequest, now time.Time, skipped map[string]bool) ([]claimCandidate, error) {
	otherVersionByTenant, _, activeByTenant, err := activeProcessingByTenantTx(tx, now)
	if err != nil {
		return nil, err
	}
	// Use persisted branch lease counts unless the caller supplies an explicit sibling override.
	otherByTenant := req.OtherActiveByTenant
	if otherByTenant == nil {
		otherByTenant = otherVersionByTenant
	}

	var pending []pdfParseJobRecord
	if err := withQueueLock(tx).Select("id, user_id, created_at, expires_at, next_retry_at").
		Where("status = ?", core.PDFParseJobStatusPending).
		Find(&pending).Error; err != nil {
		return nil, mapError(err)
	}

	var candidates []claimCandidate
	for i := range pending {
		job := &pending[i]
		if skipped[job.ID] || !isPendingUnexpired(job.ExpiresAt, now) || !isRetryReady(job.NextRetryAt, now) {
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
	}
	return candidates, nil
}

// acquirePDFJobLeaseTx atomically transitions winnerID from pending to processing under workerID's lease.
func acquirePDFJobLeaseTx(tx *gorm.DB, winnerID, workerID string, now time.Time, leaseDur time.Duration) (*core.PDFParseJob, error) {
	leaseExp := now.Add(leaseDur)
	res := tx.Model(&pdfParseJobRecord{}).
		Where("id = ? AND status = ?", winnerID, core.PDFParseJobStatusPending).
		Updates(leaseAcquireUpdates(core.PDFParseJobStatusProcessing, workerID, now, leaseExp))
	if res.Error != nil {
		return nil, mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}

	var winner pdfParseJobRecord
	if err := tx.Where("id = ?", winnerID).First(&winner).Error; err != nil {
		return nil, mapError(err)
	}
	return toCorePDFParseJob(&winner), nil
}

// RenewJobLease extends an active processing job lease owned by claimedBy.
func (s *PDFJobStore) RenewJobLease(ctx context.Context, jobID, claimedBy string, leaseExpiresAt time.Time) error {
	if jobID == "" || claimedBy == "" || leaseExpiresAt.IsZero() {
		return fmt.Errorf("%w: job id, claimed_by, and lease_expires_at are required", core.ErrValidation)
	}

	return runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		if _, err := claimedPDFJobTx(tx, jobID, claimedBy); err != nil {
			return err
		}
		return mapError(tx.Model(&pdfParseJobRecord{}).
			Where("id = ?", jobID).
			Updates(leaseRenewUpdates(leaseExpiresAt, time.Now().UTC())).Error)
	})
}

// RequeueJobForRetry returns a transiently failed claimed job back to pending for future retry prior to expiration.
func (s *PDFJobStore) RequeueJobForRetry(ctx context.Context, jobID, claimedBy string, nextRetryAt time.Time, errMsg string) error {
	if jobID == "" || claimedBy == "" || nextRetryAt.IsZero() {
		return fmt.Errorf("%w: job id, claimed_by, and next_retry_at are required", core.ErrValidation)
	}

	return runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
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
			Updates(leaseRequeueUpdates(core.PDFParseJobStatusPending, errMsg, retryAt, time.Now().UTC())).Error)
	})
}

// CompleteClaimedJob finalizes a claimed processing PDF job and prunes transient raw input text.
func (s *PDFJobStore) CompleteClaimedJob(ctx context.Context, jobID, claimedBy string, result *core.JSONResume) error {
	if jobID == "" || claimedBy == "" {
		return fmt.Errorf("%w: job id and claimed_by are required", core.ErrValidation)
	}

	return runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
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
		return fmt.Errorf("%w: job id and claimed_by are required", core.ErrValidation)
	}

	return runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
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

	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var pending []pdfParseJobRecord
		if err := withQueueLock(tx).Select("id, user_id, reservation_id").
			Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?", core.PDFParseJobStatusPending, ts).
			Find(&pending).Error; err != nil {
			return mapError(err)
		}
		var txErr error
		released, txErr = collectReleased(tx, pending, func(tx *gorm.DB, job *pdfParseJobRecord) (core.ReleasedReservation, bool, error) {
			return transitionPDFJobTerminalTx(tx, job, core.PDFParseJobStatusFailed, reason, ts, true)
		})
		return txErr
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

	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var processing []pdfParseJobRecord
		if err := withQueueLock(tx).Select("id, user_id, reservation_id").
			Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", core.PDFParseJobStatusProcessing, ts).
			Find(&processing).Error; err != nil {
			return mapError(err)
		}
		var txErr error
		released, txErr = collectReleased(tx, processing, func(tx *gorm.DB, job *pdfParseJobRecord) (core.ReleasedReservation, bool, error) {
			return transitionPDFJobTerminalTx(tx, job, core.PDFParseJobStatusFailed, reason, ts, false)
		})
		return txErr
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

	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var records []pdfParseJobRecord
		if err := withQueueLock(tx).Select("id, user_id, reservation_id").
			Where("status = ? OR (status = ? AND expires_at IS NOT NULL AND expires_at <= ?)",
				core.PDFParseJobStatusProcessing, core.PDFParseJobStatusPending, ts,
			).Find(&records).Error; err != nil {
			return mapError(err)
		}
		var txErr error
		released, txErr = collectReleased(tx, records, func(tx *gorm.DB, job *pdfParseJobRecord) (core.ReleasedReservation, bool, error) {
			return transitionPDFJobTerminalTx(tx, job, core.PDFParseJobStatusFailed, reason, ts, false)
		})
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// claimedPDFJobTx loads jobID inside tx and verifies active processing lease ownership by claimedBy.
func claimedPDFJobTx(tx *gorm.DB, jobID, claimedBy string) (*pdfParseJobRecord, error) {
	var job pdfParseJobRecord
	if err := withQueueLock(tx).Where("id = ?", jobID).First(&job).Error; err != nil {
		return nil, mapError(err)
	}
	if job.Status != core.PDFParseJobStatusProcessing || job.ClaimedBy != claimedBy {
		return nil, fmt.Errorf("%w: job %s is not actively claimed by %q", core.ErrLeaseExpired, jobID, claimedBy)
	}
	return &job, nil
}

// transitionPDFJobTerminalTx marks job terminal, prunes transient RawText, clears lease metadata, and returns any ReleasedReservation.
func transitionPDFJobTerminalTx(tx *gorm.DB, job *pdfParseJobRecord, status core.PDFParseJobStatus, reason string, ts time.Time, clearClaim bool) (core.ReleasedReservation, bool, error) {
	updates := leaseTerminalUpdates(status, reason, ts, clearClaim, map[string]any{"raw_text": ""})
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
	if clearClaim {
		job.ClaimedBy = ""
		job.ClaimedAt = nil
	}

	rel, ok := buildReleasedReservation(job.ID, job.UserID, job.ReservationID, reason)
	return rel, ok, nil
}
