package sql

import (
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// claimCandidate represents an eligible pending job or version considered during fair scheduling.
type claimCandidate struct {
	id        string
	tenantID  string
	createdAt time.Time
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
	if err := tx.Where("status = ?", core.VersionStatusPending).Find(&pendingVersions).Error; err != nil {
		return 0, 0, mapError(err)
	}
	for _, v := range pendingVersions {
		if !isPendingUnexpired(v.ExpiresAt, now) {
			continue
		}
		branchID := v.ResumeID
		if branchID == "" {
			branchID = v.BranchID
		}
		if excludeBranchID != "" && branchID == excludeBranchID {
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
	if err := tx.Where("status = ?", core.PDFParseJobStatusPending).Find(&pendingPDFs).Error; err != nil {
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
	if err := tx.Where("status = ?", core.VersionStatusProcessing).Find(&procVersions).Error; err != nil {
		return nil, nil, nil, mapError(err)
	}
	var procPDFs []pdfParseJobRecord
	if err := tx.Where("status = ?", core.PDFParseJobStatusProcessing).Find(&procPDFs).Error; err != nil {
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
		branchID := v.ResumeID
		if branchID == "" {
			branchID = v.BranchID
		}
		if branchID != "" {
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
