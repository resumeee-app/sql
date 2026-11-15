package sql

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/resumeee-app/resumeee-core"
)

// pendingDates holds normalized UTC pending-queue timestamps.
type pendingDates struct {
	createdAt   time.Time
	updatedAt   time.Time
	expiresAt   *time.Time
	nextRetryAt *time.Time
}

// claimCandidate represents an eligible pending job or version considered during fair scheduling.
type claimCandidate struct {
	id        string
	tenantID  string
	createdAt time.Time
}

// runQueueTx executes fn inside an atomic write-serialized transaction.
// On SQLite it pins a connection with per-connection busy_timeout and BEGIN IMMEDIATE so concurrent
// connections wait instead of deadlocking on SHARED-to-RESERVED upgrades.
func runQueueTx(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error) error {
	if db.Name() == "sqlite" {
		return db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
			// Pin one connection, force fresh statement clones per query (NewDB), and skip nested GORM BEGIN/COMMIT wrappers.
			tx := conn.Session(&gorm.Session{Context: ctx, NewDB: true, SkipDefaultTransaction: true})
			if err := tx.Exec("PRAGMA busy_timeout = 5000;").Error; err != nil {
				return mapError(err)
			}
			if err := tx.Exec("PRAGMA foreign_keys = ON;").Error; err != nil {
				return mapError(err)
			}
			if err := tx.Exec("BEGIN IMMEDIATE").Error; err != nil {
				return mapError(err)
			}
			committed := false
			defer func() {
				if !committed {
					_ = tx.Session(&gorm.Session{Context: context.Background(), NewDB: true}).Exec("ROLLBACK").Error
				}
			}()
			if err := fn(tx); err != nil {
				return err
			}
			if err := tx.Exec("COMMIT").Error; err != nil {
				return mapError(err)
			}
			committed = true
			return nil
		})
	}
	return db.WithContext(ctx).Transaction(fn)
}

// withQueueLock attaches FOR UPDATE row locking on dialects that support pessimistic row locks.
func withQueueLock(tx *gorm.DB) *gorm.DB {
	if tx.Name() == "sqlite" {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

// normalizeUTCTimePtr returns a copy of t converted to UTC, or nil when t is nil.
func normalizeUTCTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// resolvePendingDates normalizes createdAt, expiresAt, and nextRetryAt to UTC and applies default TTL when expiresAt is nil.
func resolvePendingDates(createdAt time.Time, expiresAt, nextRetryAt *time.Time, opts core.PendingJobOptions, now time.Time) pendingDates {
	_, _, ttl := resolvePendingLimits(opts)
	c := now
	if !createdAt.IsZero() {
		c = createdAt.UTC()
	}
	exp := normalizeUTCTimePtr(expiresAt)
	if exp == nil {
		e := now.Add(ttl)
		exp = &e
	}
	return pendingDates{
		createdAt:   c,
		updatedAt:   now,
		expiresAt:   exp,
		nextRetryAt: normalizeUTCTimePtr(nextRetryAt),
	}
}

// ensureRecordIDAvailableTx returns ErrConflict if a record of type T with id already exists.
func ensureRecordIDAvailableTx[T any](tx *gorm.DB, id string) error {
	var model T
	var count int64
	if err := tx.Model(&model).Where("id = ?", id).Count(&count).Error; err != nil {
		return mapError(err)
	}
	if count > 0 {
		return core.ErrConflict
	}
	return nil
}

// resolveNowTime returns t normalized to UTC, or the current UTC time when t is zero.
func resolveNowTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}

// isPendingUnexpired reports whether a pending item has no expiration deadline or expires strictly after now.
func isPendingUnexpired(expiresAt *time.Time, now time.Time) bool {
	return expiresAt == nil || expiresAt.After(now)
}

// isLeaseUnexpired reports whether a claimed lease has no expiry or expires strictly after now.
func isLeaseUnexpired(leaseExpiresAt *time.Time, now time.Time) bool {
	return leaseExpiresAt == nil || leaseExpiresAt.After(now)
}

// isRetryReady reports whether a pending item has no retry backoff or its NextRetryAt is at or before now.
func isRetryReady(nextRetryAt *time.Time, now time.Time) bool {
	return nextRetryAt == nil || !nextRetryAt.After(now)
}

// resolvePendingLimits extracts effective per-tenant cap, global cap, and pending TTL from opts.
func resolvePendingLimits(opts core.PendingJobOptions) (int, int, time.Duration) {
	maxPerTenant := opts.MaxPendingPerTenant
	if maxPerTenant <= 0 {
		maxPerTenant = core.DefaultMaxPendingPerTenant
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = core.DefaultJobTTL
	}
	return maxPerTenant, opts.MaxGlobalPending, ttl
}

// resolveLeaseDuration returns d when positive, or DefaultLeaseDuration otherwise.
func resolveLeaseDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return core.DefaultLeaseDuration
	}
	return d
}

// leaseAcquireUpdates builds the shared column map for transitioning a pending item to processing under a worker lease.
func leaseAcquireUpdates[S ~string](status S, workerID string, now, leaseExp time.Time) map[string]any {
	return map[string]any{
		"status":           status,
		"claimed_by":       workerID,
		"claimed_at":       &now,
		"lease_expires_at": &leaseExp,
		"next_retry_at":    nil,
		"attempt_count":    gorm.Expr("attempt_count + 1"),
		"updated_at":       now,
	}
}

// leaseRenewUpdates builds the column map for extending an active worker lease deadline.
func leaseRenewUpdates(leaseExpiresAt, now time.Time) map[string]any {
	exp := leaseExpiresAt.UTC()
	return map[string]any{
		"lease_expires_at": &exp,
		"updated_at":       now,
	}
}

// leaseRequeueUpdates builds the column map for returning a claimed item to pending with retry backoff.
func leaseRequeueUpdates[S ~string](status S, errMsg string, retryAt, now time.Time) map[string]any {
	return map[string]any{
		"status":           status,
		"error_message":    errMsg,
		"next_retry_at":    &retryAt,
		"claimed_by":       "",
		"claimed_at":       nil,
		"lease_expires_at": nil,
		"completed_at":     nil,
		"updated_at":       now,
	}
}

// leaseTerminalUpdates builds the column map for transitioning a queued or claimed item into a terminal state.
func leaseTerminalUpdates[S ~string](status S, reason string, now time.Time, clearClaim bool, extra map[string]any) map[string]any {
	updates := map[string]any{
		"status":           status,
		"error_message":    reason,
		"updated_at":       now,
		"completed_at":     &now,
		"lease_expires_at": nil,
		"next_retry_at":    nil,
	}
	if clearClaim {
		updates["claimed_by"] = ""
		updates["claimed_at"] = nil
	}
	for k, v := range extra {
		updates[k] = v
	}
	return updates
}

// buildReleasedReservation constructs a ReleasedReservation when reservationID is non-empty.
func buildReleasedReservation(jobID, userID, reservationID, reason string) (core.ReleasedReservation, bool) {
	if reservationID == "" {
		return core.ReleasedReservation{}, false
	}
	return core.ReleasedReservation{
		JobID:         jobID,
		UserID:        userID,
		ReservationID: reservationID,
		Reason:        reason,
	}, true
}

// collectReleased applies fn to each element of records and gathers any non-empty ReleasedReservation results.
func collectReleased[T any](tx *gorm.DB, records []T, fn func(*gorm.DB, *T) (core.ReleasedReservation, bool, error)) ([]core.ReleasedReservation, error) {
	var released []core.ReleasedReservation
	for i := range records {
		rel, ok, err := fn(tx, &records[i])
		if err != nil {
			return nil, err
		}
		if ok {
			released = append(released, rel)
		}
	}
	return released, nil
}

// selectFairestCandidate picks the winning candidate ordered by LastServedSeq LRU, oldest CreatedAt, then ID.
func selectFairestCandidate(candidates []claimCandidate, lastServedSeq map[string]uint64) (string, bool) {
	if len(candidates) == 0 {
		return "", false
	}
	best := candidates[0]
	bestSeq := lastServedSeq[best.tenantID]
	for _, c := range candidates[1:] {
		cSeq := lastServedSeq[c.tenantID]
		if candidatePrecedes(c, cSeq, best, bestSeq) {
			best = c
			bestSeq = cSeq
		}
	}
	return best.id, true
}

// candidatePrecedes reports whether a should be scheduled strictly before b.
func candidatePrecedes(a claimCandidate, aSeq uint64, b claimCandidate, bSeq uint64) bool {
	if aSeq != bSeq {
		return aSeq < bSeq
	}
	if !a.createdAt.Equal(b.createdAt) {
		return a.createdAt.Before(b.createdAt)
	}
	return a.id < b.id
}

// countPendingVersionsTx counts unexpired pending branch versions globally and for userID, excluding excludeBranchID
// because a newer submission on the same branch supersedes its existing pending versions.
func countPendingVersionsTx(tx *gorm.DB, userID, excludeBranchID string, now time.Time) (tenantCount, globalCount int, err error) {
	var pendingVersions []branchVersionRecord
	if err := withQueueLock(tx).Select("user_id, resume_id, branch_id, expires_at").
		Where("status = ?", core.VersionStatusPending).
		Find(&pendingVersions).Error; err != nil {
		return 0, 0, mapError(err)
	}
	for _, v := range pendingVersions {
		if !isPendingUnexpired(v.ExpiresAt, now) {
			continue
		}
		if excludeBranchID != "" && v.effectiveBranchID() == excludeBranchID {
			continue
		}
		globalCount++
		if v.UserID == userID {
			tenantCount++
		}
	}
	return tenantCount, globalCount, nil
}

// countPendingPDFJobsTx counts unexpired pending PDF parse jobs globally and for userID.
func countPendingPDFJobsTx(tx *gorm.DB, userID string, now time.Time) (tenantCount, globalCount int, err error) {
	var pendingPDFs []pdfParseJobRecord
	if err := withQueueLock(tx).Select("user_id, expires_at").
		Where("status = ?", core.PDFParseJobStatusPending).
		Find(&pendingPDFs).Error; err != nil {
		return 0, 0, mapError(err)
	}
	for _, job := range pendingPDFs {
		if !isPendingUnexpired(job.ExpiresAt, now) {
			continue
		}
		globalCount++
		if job.UserID == userID {
			tenantCount++
		}
	}
	return tenantCount, globalCount, nil
}

// checkPendingAdmissionTx enforces MaxPendingPerTenant and MaxGlobalPending across both queues inside a transaction.
func checkPendingAdmissionTx(tx *gorm.DB, userID, excludeBranchID string, opts core.PendingJobOptions, now time.Time) error {
	if tx.Name() != "sqlite" && userID != "" {
		// Serialize concurrent submissions for the same tenant on Postgres/MySQL when the tenant row exists.
		var users []userRecord
		if err := withQueueLock(tx).Select("id").Where("id = ?", userID).Find(&users).Error; err != nil {
			return mapError(err)
		}
	}
	vTenant, vGlobal, err := countPendingVersionsTx(tx, userID, excludeBranchID, now)
	if err != nil {
		return err
	}
	pTenant, pGlobal, err := countPendingPDFJobsTx(tx, userID, now)
	if err != nil {
		return err
	}

	maxPerTenant, maxGlobal, _ := resolvePendingLimits(opts)
	if vTenant+pTenant+1 > maxPerTenant {
		return core.ErrPendingLimitExceeded
	}
	if maxGlobal > 0 && vGlobal+pGlobal+1 > maxGlobal {
		return core.ErrPendingLimitExceeded
	}
	return nil
}

// activeProcessingByTenantTx computes active unexpired processing counts per tenant across both queues
// and tracks branches with an active processing version so at most one version runs per branch.
func activeProcessingByTenantTx(tx *gorm.DB, now time.Time) (map[string]int, map[string]bool, map[string]int, error) {
	var procVersions []branchVersionRecord
	if err := withQueueLock(tx).Select("user_id, resume_id, branch_id, lease_expires_at").
		Where("status = ?", core.VersionStatusProcessing).
		Find(&procVersions).Error; err != nil {
		return nil, nil, nil, mapError(err)
	}
	var procPDFs []pdfParseJobRecord
	if err := withQueueLock(tx).Select("user_id, lease_expires_at").
		Where("status = ?", core.PDFParseJobStatusProcessing).
		Find(&procPDFs).Error; err != nil {
		return nil, nil, nil, mapError(err)
	}

	byVersionTenant := make(map[string]int)
	activeBranches := make(map[string]bool)
	for _, v := range procVersions {
		if !isLeaseUnexpired(v.LeaseExpiresAt, now) {
			continue
		}
		if v.UserID != "" {
			byVersionTenant[v.UserID]++
		}
		if branchID := v.effectiveBranchID(); branchID != "" {
			activeBranches[branchID] = true
		}
	}

	byPDFTenant := make(map[string]int)
	for _, job := range procPDFs {
		if !isLeaseUnexpired(job.LeaseExpiresAt, now) {
			continue
		}
		if job.UserID != "" {
			byPDFTenant[job.UserID]++
		}
	}
	return byVersionTenant, activeBranches, byPDFTenant, nil
}
