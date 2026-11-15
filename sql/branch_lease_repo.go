package sql

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// SubmitPendingVersion atomically enforces admission caps, supersedes prior pending versions on the branch, and stores a pending version.
func (s *BranchStore) SubmitPendingVersion(ctx context.Context, version *core.BranchVersion, opts core.PendingJobOptions) ([]core.ReleasedReservation, error) {
	branchID, err := validatePendingVersionInput(version)
	if err != nil {
		return nil, err
	}

	now := resolveNowTime(opts.Now)
	var released []core.ReleasedReservation

	err = runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		if err := ensureRecordIDAvailableTx[branchVersionRecord](tx, version.ID); err != nil {
			return err
		}
		if err := loadBranchForSubmissionTx(tx, branchID, version); err != nil {
			return err
		}
		existing, err := loadActiveBranchVersionsTx(tx, branchID)
		if err != nil {
			return err
		}
		if err := ensureNoActiveBranchLease(existing, branchID, now); err != nil {
			return err
		}
		if err := checkPendingAdmissionTx(tx, version.UserID, branchID, opts, now); err != nil {
			return err
		}
		expiredRel, err := failExpiredBranchProcessingTx(tx, existing, now)
		if err != nil {
			return err
		}
		released = append(released, expiredRel...)

		superseded, err := supersedePendingVersionsTx(tx, branchID, "superseded by newer pending version", now)
		if err != nil {
			return err
		}
		released = append(released, superseded...)

		return insertPendingVersionTx(tx, version, branchID, opts, now)
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// validatePendingVersionInput normalizes version and verifies required submission fields prior to parent branch lookup.
func validatePendingVersionInput(version *core.BranchVersion) (string, error) {
	if version == nil {
		return "", fmt.Errorf("%w: version cannot be nil", core.ErrValidation)
	}
	if version.Status != "" && version.Status != core.VersionStatusPending {
		return "", fmt.Errorf("%w: pending version cannot start in %s state", core.ErrValidation, version.Status)
	}
	_ = version.Normalize()
	branchID := version.ResumeID
	if branchID == "" {
		return "", fmt.Errorf("%w: branch id is required", core.ErrValidation)
	}
	if version.ID == "" {
		version.ID = core.GenerateID("ver")
	}
	return branchID, nil
}

// loadBranchForSubmissionTx verifies parent branch existence and ownership, inheriting empty UserID from the branch.
func loadBranchForSubmissionTx(tx *gorm.DB, branchID string, version *core.BranchVersion) error {
	var br branchRecord
	if err := withQueueLock(tx).Select("id, user_id").Where("id = ?", branchID).First(&br).Error; err != nil {
		return mapError(err)
	}
	if version.UserID == "" {
		version.UserID = br.UserID
	} else if br.UserID != "" && br.UserID != version.UserID {
		return core.ErrNotFound
	}
	if version.UserID == "" {
		return fmt.Errorf("%w: user id is required", core.ErrValidation)
	}
	return nil
}

// loadActiveBranchVersionsTx fetches pending and processing versions belonging to branchID.
func loadActiveBranchVersionsTx(tx *gorm.DB, branchID string) ([]branchVersionRecord, error) {
	var existing []branchVersionRecord
	err := withQueueLock(tx).Where("(resume_id = ? OR branch_id = ?) AND status IN ?",
		branchID, branchID, []core.VersionStatus{core.VersionStatusProcessing, core.VersionStatusPending},
	).Find(&existing).Error
	return existing, mapError(err)
}

// ensureNoActiveBranchLease rejects submission when branchID already has an unexpired processing version.
func ensureNoActiveBranchLease(existing []branchVersionRecord, branchID string, now time.Time) error {
	for _, ex := range existing {
		if ex.Status == core.VersionStatusProcessing && isLeaseUnexpired(ex.LeaseExpiresAt, now) {
			return fmt.Errorf("%w: branch %s already has an active processing version", core.ErrConflict, branchID)
		}
	}
	return nil
}

// failExpiredBranchProcessingTx transitions expired processing versions on a branch to failed and collects their reservations.
func failExpiredBranchProcessingTx(tx *gorm.DB, existing []branchVersionRecord, now time.Time) ([]core.ReleasedReservation, error) {
	var expired []branchVersionRecord
	for _, ex := range existing {
		if ex.Status == core.VersionStatusProcessing && !isLeaseUnexpired(ex.LeaseExpiresAt, now) {
			expired = append(expired, ex)
		}
	}
	return collectReleased(tx, expired, func(tx *gorm.DB, v *branchVersionRecord) (core.ReleasedReservation, bool, error) {
		return transitionVersionTerminalTx(tx, v, core.VersionStatusFailed, "worker lease expired", now, false)
	})
}

// insertPendingVersionTx assigns the next monotonic version number, applies default TTL, and inserts the pending record.
func insertPendingVersionTx(tx *gorm.DB, version *core.BranchVersion, branchID string, opts core.PendingJobOptions, now time.Time) error {
	// Query MAX(version_number) inside the write transaction to keep per-branch numbering monotonic.
	var maxVer int
	if err := tx.Model(&branchVersionRecord{}).
		Select("COALESCE(MAX(version_number), 0)").
		Where("resume_id = ? OR branch_id = ?", branchID, branchID).
		Scan(&maxVer).Error; err != nil {
		return mapError(err)
	}

	dates := resolvePendingDates(version.CreatedAt, version.ExpiresAt, version.NextRetryAt, opts, now)
	version.VersionNumber = maxVer + 1
	version.Status = core.VersionStatusPending
	version.CreatedAt = dates.createdAt
	version.UpdatedAt = dates.updatedAt
	version.ExpiresAt = dates.expiresAt
	version.NextRetryAt = dates.nextRetryAt
	version.ClaimedBy = ""
	version.ClaimedAt = nil
	version.LeaseExpiresAt = nil
	version.CompletedAt = nil

	return mapError(tx.Create(toBranchVersionRecord(version)).Error)
}

// SupersedePendingVersions cancels all pending versions on branchID and returns released quota reservations.
func (s *BranchStore) SupersedePendingVersions(ctx context.Context, branchID, reason string) ([]core.ReleasedReservation, error) {
	if branchID == "" {
		return nil, fmt.Errorf("%w: branch id cannot be empty", core.ErrValidation)
	}
	var released []core.ReleasedReservation
	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var err error
		released, err = supersedePendingVersionsTx(tx, branchID, reason, time.Now().UTC())
		return err
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// AbortBranchVersions cancels all pending and processing versions on branchID and returns released reservations.
func (s *BranchStore) AbortBranchVersions(ctx context.Context, branchID, reason string) ([]core.ReleasedReservation, error) {
	if branchID == "" {
		return nil, fmt.Errorf("%w: branch id cannot be empty", core.ErrValidation)
	}

	now := time.Now().UTC()
	var released []core.ReleasedReservation
	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var records []branchVersionRecord
		if err := withQueueLock(tx).Select("id, user_id, reservation_id").
			Where("(resume_id = ? OR branch_id = ?) AND status IN ?",
				branchID, branchID, []core.VersionStatus{core.VersionStatusPending, core.VersionStatusProcessing},
			).Find(&records).Error; err != nil {
			return mapError(err)
		}

		var txErr error
		released, txErr = collectReleased(tx, records, func(tx *gorm.DB, v *branchVersionRecord) (core.ReleasedReservation, bool, error) {
			return transitionVersionTerminalTx(tx, v, core.VersionStatusCancelled, reason, now, true)
		})
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// CancelVersion cancels an owned pending branch version or returns ErrNotFound/ErrConflict.
func (s *BranchStore) CancelVersion(ctx context.Context, userID, branchID, versionID, reason string) (*core.BranchVersion, error) {
	if userID == "" || versionID == "" {
		return nil, fmt.Errorf("%w: user id and version id are required", core.ErrValidation)
	}

	var out *core.BranchVersion
	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var rec branchVersionRecord
		if err := withQueueLock(tx).Where("id = ?", versionID).First(&rec).Error; err != nil {
			return mapError(err)
		}
		matchesBranch := branchID == "" || rec.ResumeID == branchID || rec.BranchID == branchID
		if !matchesBranch || rec.UserID != userID {
			return core.ErrNotFound
		}
		if rec.Status != core.VersionStatusPending {
			return fmt.Errorf("%w: cannot cancel version in %s state", core.ErrConflict, rec.Status)
		}

		if _, _, err := transitionVersionTerminalTx(tx, &rec, core.VersionStatusCancelled, reason, time.Now().UTC(), true); err != nil {
			return err
		}
		out = toCoreBranchVersion(&rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ClaimNextVersion atomically claims the fairest eligible pending branch version for a worker lease.
func (s *BranchStore) ClaimNextVersion(ctx context.Context, req core.ClaimJobRequest) (*core.BranchVersion, error) {
	if req.WorkerID == "" {
		return nil, fmt.Errorf("%w: worker id cannot be empty", core.ErrValidation)
	}

	now := resolveNowTime(req.Now)
	leaseDur := resolveLeaseDuration(req.LeaseDuration)

	var out *core.BranchVersion
	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		skipped := make(map[string]bool)
		for {
			candidates, err := findEligibleVersionCandidatesTx(tx, req, now, skipped)
			if err != nil {
				return err
			}
			winnerID, ok := selectFairestCandidate(candidates, req.LastServedSeq)
			if !ok {
				return nil
			}
			out, err = acquireVersionLeaseTx(tx, winnerID, req.WorkerID, now, leaseDur)
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

// findEligibleVersionCandidatesTx gathers claimable pending versions enforcing expiry, backoff, per-branch exclusivity, and tenant concurrency.
func findEligibleVersionCandidatesTx(tx *gorm.DB, req core.ClaimJobRequest, now time.Time, skipped map[string]bool) ([]claimCandidate, error) {
	byTenant, activeBranches, otherPDFByTenant, err := activeProcessingByTenantTx(tx, now)
	if err != nil {
		return nil, err
	}
	// Use persisted PDF lease counts unless the caller supplies an explicit sibling override.
	otherByTenant := req.OtherActiveByTenant
	if otherByTenant == nil {
		otherByTenant = otherPDFByTenant
	}

	var pending []branchVersionRecord
	if err := withQueueLock(tx).Select("id, user_id, resume_id, branch_id, created_at, expires_at, next_retry_at").
		Where("status = ?", core.VersionStatusPending).
		Find(&pending).Error; err != nil {
		return nil, mapError(err)
	}

	var candidates []claimCandidate
	for i := range pending {
		v := &pending[i]
		if skipped[v.ID] || !isPendingUnexpired(v.ExpiresAt, now) || !isRetryReady(v.NextRetryAt, now) {
			continue
		}
		if activeBranches[v.effectiveBranchID()] {
			continue
		}
		if req.MaxConcurrentPerTenant > 0 && byTenant[v.UserID]+otherByTenant[v.UserID] >= req.MaxConcurrentPerTenant {
			continue
		}
		candidates = append(candidates, claimCandidate{
			id:        v.ID,
			tenantID:  v.UserID,
			createdAt: v.CreatedAt,
		})
	}
	return candidates, nil
}

// acquireVersionLeaseTx atomically transitions winnerID from pending to processing under workerID's lease.
func acquireVersionLeaseTx(tx *gorm.DB, winnerID, workerID string, now time.Time, leaseDur time.Duration) (*core.BranchVersion, error) {
	leaseExp := now.Add(leaseDur)
	res := tx.Model(&branchVersionRecord{}).
		Where("id = ? AND status = ?", winnerID, core.VersionStatusPending).
		Updates(leaseAcquireUpdates(core.VersionStatusProcessing, workerID, now, leaseExp))
	if res.Error != nil {
		return nil, mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}

	var winner branchVersionRecord
	if err := tx.Where("id = ?", winnerID).First(&winner).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreBranchVersion(&winner), nil
}

// RenewVersionLease extends an active processing version lease owned by claimedBy.
func (s *BranchStore) RenewVersionLease(ctx context.Context, versionID, claimedBy string, leaseExpiresAt time.Time) error {
	if versionID == "" || claimedBy == "" || leaseExpiresAt.IsZero() {
		return fmt.Errorf("%w: version id, claimed_by, and lease_expires_at are required", core.ErrValidation)
	}

	return runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		if _, err := claimedVersionTx(tx, versionID, claimedBy); err != nil {
			return err
		}
		return mapError(tx.Model(&branchVersionRecord{}).
			Where("id = ?", versionID).
			Updates(leaseRenewUpdates(leaseExpiresAt, time.Now().UTC())).Error)
	})
}

// RequeueVersionForRetry returns a claimed processing version to pending for future retry prior to expiration.
func (s *BranchStore) RequeueVersionForRetry(ctx context.Context, versionID, claimedBy string, nextRetryAt time.Time, errMsg string) error {
	if versionID == "" || claimedBy == "" || nextRetryAt.IsZero() {
		return fmt.Errorf("%w: version id, claimed_by, and next_retry_at are required", core.ErrValidation)
	}

	return runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		v, err := claimedVersionTx(tx, versionID, claimedBy)
		if err != nil {
			return err
		}
		retryAt := nextRetryAt.UTC()
		if v.ExpiresAt != nil && !retryAt.Before(*v.ExpiresAt) {
			return core.ErrJobExpired
		}

		return mapError(tx.Model(&branchVersionRecord{}).
			Where("id = ?", versionID).
			Updates(leaseRequeueUpdates(core.VersionStatusPending, errMsg, retryAt, time.Now().UTC())).Error)
	})
}

// FailClaimedVersion transitions an active claimed version owned by claimedBy to failed.
func (s *BranchStore) FailClaimedVersion(ctx context.Context, versionID, claimedBy, errMsg string) error {
	if versionID == "" || claimedBy == "" {
		return fmt.Errorf("%w: version id and claimed_by are required", core.ErrValidation)
	}

	return runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		v, err := claimedVersionTx(tx, versionID, claimedBy)
		if err != nil {
			return err
		}
		_, _, err = transitionVersionTerminalTx(tx, v, core.VersionStatusFailed, errMsg, time.Now().UTC(), false)
		return err
	})
}

// ExpirePendingVersions fails pending versions whose ExpiresAt has passed and returns released reservations.
func (s *BranchStore) ExpirePendingVersions(ctx context.Context, now time.Time, reason string) ([]core.ReleasedReservation, error) {
	ts := resolveNowTime(now)
	var released []core.ReleasedReservation

	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var pending []branchVersionRecord
		if err := withQueueLock(tx).Select("id, user_id, reservation_id").
			Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?", core.VersionStatusPending, ts).
			Find(&pending).Error; err != nil {
			return mapError(err)
		}
		var txErr error
		released, txErr = collectReleased(tx, pending, func(tx *gorm.DB, v *branchVersionRecord) (core.ReleasedReservation, bool, error) {
			return transitionVersionTerminalTx(tx, v, core.VersionStatusFailed, reason, ts, true)
		})
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// FailExpiredVersionLeases fails processing versions whose worker lease has expired and returns released reservations.
func (s *BranchStore) FailExpiredVersionLeases(ctx context.Context, now time.Time, reason string) ([]core.ReleasedReservation, error) {
	ts := resolveNowTime(now)
	var released []core.ReleasedReservation

	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var processing []branchVersionRecord
		if err := withQueueLock(tx).Select("id, user_id, reservation_id").
			Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", core.VersionStatusProcessing, ts).
			Find(&processing).Error; err != nil {
			return mapError(err)
		}
		var txErr error
		released, txErr = collectReleased(tx, processing, func(tx *gorm.DB, v *branchVersionRecord) (core.ReleasedReservation, bool, error) {
			return transitionVersionTerminalTx(tx, v, core.VersionStatusFailed, reason, ts, false)
		})
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// RecoverInterruptedVersions fails interrupted processing versions and expired pending versions while preserving unexpired pending work.
func (s *BranchStore) RecoverInterruptedVersions(ctx context.Context, reason string) ([]core.ReleasedReservation, error) {
	ts := time.Now().UTC()
	var released []core.ReleasedReservation

	err := runQueueTx(ctx, s.db, func(tx *gorm.DB) error {
		var records []branchVersionRecord
		if err := withQueueLock(tx).Select("id, user_id, reservation_id").
			Where("status = ? OR (status = ? AND expires_at IS NOT NULL AND expires_at <= ?)",
				core.VersionStatusProcessing, core.VersionStatusPending, ts,
			).Find(&records).Error; err != nil {
			return mapError(err)
		}
		var txErr error
		released, txErr = collectReleased(tx, records, func(tx *gorm.DB, v *branchVersionRecord) (core.ReleasedReservation, bool, error) {
			return transitionVersionTerminalTx(tx, v, core.VersionStatusFailed, reason, ts, false)
		})
		return txErr
	})
	if err != nil {
		return nil, err
	}
	return released, nil
}

// supersedePendingVersionsTx cancels all pending versions on branchID inside tx and returns any released reservations.
func supersedePendingVersionsTx(tx *gorm.DB, branchID, reason string, now time.Time) ([]core.ReleasedReservation, error) {
	var pending []branchVersionRecord
	if err := withQueueLock(tx).Select("id, user_id, reservation_id").
		Where("(resume_id = ? OR branch_id = ?) AND status = ?", branchID, branchID, core.VersionStatusPending).
		Find(&pending).Error; err != nil {
		return nil, mapError(err)
	}
	return collectReleased(tx, pending, func(tx *gorm.DB, v *branchVersionRecord) (core.ReleasedReservation, bool, error) {
		return transitionVersionTerminalTx(tx, v, core.VersionStatusCancelled, reason, now, true)
	})
}

// claimedVersionTx loads versionID inside tx and verifies active processing lease ownership by claimedBy.
func claimedVersionTx(tx *gorm.DB, versionID, claimedBy string) (*branchVersionRecord, error) {
	var v branchVersionRecord
	if err := withQueueLock(tx).Where("id = ?", versionID).First(&v).Error; err != nil {
		return nil, mapError(err)
	}
	if v.Status != core.VersionStatusProcessing || v.ClaimedBy != claimedBy {
		return nil, fmt.Errorf("%w: version %s is not actively claimed by %q", core.ErrLeaseExpired, versionID, claimedBy)
	}
	return &v, nil
}

// transitionVersionTerminalTx marks v terminal, clears active lease fields, and emits a ReleasedReservation when present.
func transitionVersionTerminalTx(tx *gorm.DB, v *branchVersionRecord, status core.VersionStatus, reason string, now time.Time, clearClaim bool) (core.ReleasedReservation, bool, error) {
	updates := leaseTerminalUpdates(status, reason, now, clearClaim, nil)
	if err := tx.Model(&branchVersionRecord{}).Where("id = ?", v.ID).Updates(updates).Error; err != nil {
		return core.ReleasedReservation{}, false, mapError(err)
	}

	v.Status = status
	v.ErrorMessage = reason
	v.UpdatedAt = now
	completedAt := now
	v.CompletedAt = &completedAt
	v.LeaseExpiresAt = nil
	v.NextRetryAt = nil
	if clearClaim {
		v.ClaimedBy = ""
		v.ClaimedAt = nil
	}

	rel, ok := buildReleasedReservation(v.ID, v.UserID, v.ReservationID, reason)
	return rel, ok, nil
}
