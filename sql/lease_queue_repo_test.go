package sql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/resumeee-app/resumeee-core"
)

func setupIsolatedLeaseStores(t *testing.T) (*BranchStore, *PDFJobStore) {
	t.Helper()
	dsn := fmt.Sprintf("file:lease_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := New(dsn)
	if err != nil {
		t.Fatalf("failed to open isolated sqlite db: %v", err)
	}
	return NewBranchStore(db), NewPDFJobStore(db)
}

func TestBranchStoreSubmitPendingValidationAndAdmission(t *testing.T) {
	ctx := context.Background()
	branchRepo, _ := setupIsolatedLeaseStores(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	for _, b := range []*core.Branch{
		{ID: "br-a1", UserID: "user-a", Name: "A1", CreatedAt: now, UpdatedAt: now},
		{ID: "br-b1", UserID: "user-b", Name: "B1", CreatedAt: now, UpdatedAt: now},
	} {
		if err := branchRepo.CreateBranch(ctx, b); err != nil {
			t.Fatalf("CreateBranch %s: %v", b.ID, err)
		}
	}

	t.Run("validation rejects nil, non-pending status, missing branch, and missing user", func(t *testing.T) {
		cases := []struct {
			name string
			ver  *core.BranchVersion
		}{
			{name: "nil version", ver: nil},
			{name: "non-pending status", ver: &core.BranchVersion{ID: "v-bad-status", UserID: "user-a", ResumeID: "br-a1", Status: core.VersionStatusProcessing}},
			{name: "missing branch", ver: &core.BranchVersion{ID: "v-no-br", UserID: "user-a"}},
			{name: "missing user", ver: &core.BranchVersion{ID: "v-no-usr", ResumeID: "br-a1"}},
		}
		for _, tc := range cases {
			if _, err := branchRepo.SubmitPendingVersion(ctx, tc.ver, core.PendingJobOptions{Now: now}); !errors.Is(err, core.ErrValidation) {
				t.Errorf("%s: expected ErrValidation, got %v", tc.name, err)
			}
		}
	})

	t.Run("duplicate id, global cap, active lease conflict, and expired processing recovery", func(t *testing.T) {
		v1 := &core.BranchVersion{ID: "ver-a1", UserID: "user-a", ResumeID: "br-a1", ReservationID: "res-a1"}
		if _, err := branchRepo.SubmitPendingVersion(ctx, v1, core.PendingJobOptions{Now: now, MaxGlobalPending: 2}); err != nil {
			t.Fatalf("SubmitPendingVersion v1: %v", err)
		}
		if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{ID: "ver-a1", UserID: "user-a", ResumeID: "br-a1"}, core.PendingJobOptions{Now: now}); !errors.Is(err, core.ErrConflict) {
			t.Fatalf("expected ErrConflict on duplicate ID, got %v", err)
		}
		// Global cap = 1 blocks user-b while ver-a1 is pending
		if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{ID: "ver-b1", UserID: "user-b", ResumeID: "br-b1"}, core.PendingJobOptions{Now: now, MaxGlobalPending: 1}); !errors.Is(err, core.ErrPendingLimitExceeded) {
			t.Fatalf("expected ErrPendingLimitExceeded for global cap, got %v", err)
		}

		// Claim ver-a1 with 10s lease; active lease blocks same-branch submission
		claimed, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{WorkerID: "w1", Now: now, LeaseDuration: 10 * time.Second})
		if err != nil || claimed == nil {
			t.Fatalf("ClaimNextVersion: claimed=%+v err=%v", claimed, err)
		}
		if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{ID: "ver-a2", UserID: "user-a", ResumeID: "br-a1"}, core.PendingJobOptions{Now: now.Add(5 * time.Second)}); !errors.Is(err, core.ErrConflict) {
			t.Fatalf("expected ErrConflict with active lease, got %v", err)
		}

		// After lease expires, SubmitPendingVersion auto-fails expired processing version and returns its reservation
		rel, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{ID: "ver-a2", UserID: "user-a", ResumeID: "br-a1"}, core.PendingJobOptions{Now: now.Add(15 * time.Second)})
		if err != nil {
			t.Fatalf("SubmitPendingVersion after lease expiry: %v", err)
		}
		if len(rel) != 1 || rel[0].ReservationID != "res-a1" {
			t.Fatalf("expected expired res-a1 released, got %+v", rel)
		}
		oldV1, _ := branchRepo.Version(ctx, "br-a1", "ver-a1")
		if oldV1.Status != core.VersionStatusFailed {
			t.Fatalf("expected expired ver-a1 failed, got %s", oldV1.Status)
		}
	})
}

func TestBranchStoreLeaseAndQueueLifecycle(t *testing.T) {
	ctx := context.Background()
	branchRepo, pdfRepo := setupIsolatedLeaseStores(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	for _, b := range []*core.Branch{
		{ID: "br-1", UserID: "user-a", Name: "Backend", CreatedAt: now, UpdatedAt: now},
		{ID: "br-2", UserID: "user-a", Name: "Platform", CreatedAt: now, UpdatedAt: now},
		{ID: "br-3", UserID: "user-b", Name: "Staff", CreatedAt: now, UpdatedAt: now},
	} {
		if err := branchRepo.CreateBranch(ctx, b); err != nil {
			t.Fatalf("CreateBranch %s: %v", b.ID, err)
		}
	}

	// SubmitPendingVersion supersedes prior pending on same branch and returns released reservation
	v1 := &core.BranchVersion{ID: "ver-1", UserID: "user-a", ResumeID: "br-1", ReservationID: "res-1"}
	if _, err := branchRepo.SubmitPendingVersion(ctx, v1, core.PendingJobOptions{Now: now, MaxPendingPerTenant: 2}); err != nil {
		t.Fatalf("SubmitPendingVersion v1: %v", err)
	}
	v2 := &core.BranchVersion{ID: "ver-2", UserID: "user-a", ResumeID: "br-1", ReservationID: "res-2"}
	rel, err := branchRepo.SubmitPendingVersion(ctx, v2, core.PendingJobOptions{Now: now.Add(time.Second), MaxPendingPerTenant: 2})
	if err != nil {
		t.Fatalf("SubmitPendingVersion v2: %v", err)
	}
	if len(rel) != 1 || rel[0].ReservationID != "res-1" || v2.VersionNumber != 2 {
		t.Fatalf("expected res-1 superseded and v2 num=2, got rel=%v num=%d", rel, v2.VersionNumber)
	}

	// Enforce per-tenant pending limit across queues
	v3 := &core.BranchVersion{ID: "ver-3", UserID: "user-a", ResumeID: "br-2", ReservationID: "res-3"}
	if _, err := branchRepo.SubmitPendingVersion(ctx, v3, core.PendingJobOptions{Now: now.Add(2 * time.Second), MaxPendingPerTenant: 2}); err != nil {
		t.Fatalf("SubmitPendingVersion v3: %v", err)
	}
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{ID: "pdf-cap", UserID: "user-a", RawText: "resume"}, core.PendingJobOptions{Now: now.Add(2 * time.Second), MaxPendingPerTenant: 2}); !errors.Is(err, core.ErrPendingLimitExceeded) {
		t.Fatalf("expected ErrPendingLimitExceeded, got %v", err)
	}

	v4 := &core.BranchVersion{ID: "ver-4", UserID: "user-b", ResumeID: "br-3", ReservationID: "res-4"}
	if _, err := branchRepo.SubmitPendingVersion(ctx, v4, core.PendingJobOptions{Now: now.Add(3 * time.Second), MaxPendingPerTenant: 2}); err != nil {
		t.Fatalf("SubmitPendingVersion v4: %v", err)
	}

	// CancelVersion validation and ownership checks
	if _, err := branchRepo.CancelVersion(ctx, "", "br-3", "ver-4", "nope"); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty userID, got %v", err)
	}
	if _, err := branchRepo.CancelVersion(ctx, "wrong-user", "br-3", "ver-4", "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-owner cancel, got %v", err)
	}

	// ClaimNextVersion validation and fair tenant scheduling (user-b wins over user-a)
	if _, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{}); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty WorkerID, got %v", err)
	}
	claimed, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{
		WorkerID:               "worker-1",
		Now:                    now.Add(4 * time.Second),
		LeaseDuration:          time.Minute,
		MaxConcurrentPerTenant: 1,
		LastServedSeq:          map[string]uint64{"user-a": 10, "user-b": 1},
	})
	if err != nil || claimed == nil || claimed.ID != "ver-4" || claimed.AttemptCount != 1 {
		t.Fatalf("expected ver-4 claimed with AttemptCount=1, got %+v err=%v", claimed, err)
	}
	if _, err := branchRepo.CancelVersion(ctx, "user-b", "br-3", "ver-4", "too late"); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("expected ErrConflict cancelling processing version, got %v", err)
	}

	// RenewVersionLease validation, not-found, wrong worker, and valid renewal
	if err := branchRepo.RenewVersionLease(ctx, "", "worker-1", now); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("expected ErrValidation on empty versionID, got %v", err)
	}
	if err := branchRepo.RenewVersionLease(ctx, "missing", "worker-1", now.Add(time.Minute)); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing version, got %v", err)
	}
	if err := branchRepo.RenewVersionLease(ctx, "ver-4", "worker-2", now.Add(2*time.Minute)); !errors.Is(err, core.ErrLeaseExpired) {
		t.Fatalf("expected ErrLeaseExpired on wrong worker renew, got %v", err)
	}
	if err := branchRepo.RenewVersionLease(ctx, "ver-4", "worker-1", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("RenewVersionLease: %v", err)
	}

	// RequeueVersionForRetry past ExpiresAt -> ErrJobExpired; before ExpiresAt -> ok & respects NextRetryAt
	if err := branchRepo.RequeueVersionForRetry(ctx, "ver-4", "worker-1", v4.ExpiresAt.Add(time.Minute), "transient"); !errors.Is(err, core.ErrJobExpired) {
		t.Fatalf("expected ErrJobExpired on retry past ExpiresAt, got %v", err)
	}
	retryAt := now.Add(30 * time.Second)
	if err := branchRepo.RequeueVersionForRetry(ctx, "ver-4", "worker-1", retryAt, "transient"); err != nil {
		t.Fatalf("RequeueVersionForRetry: %v", err)
	}
	cancelled, err := branchRepo.CancelVersion(ctx, "user-b", "br-3", "ver-4", "user aborted")
	if err != nil || cancelled.Status != core.VersionStatusCancelled {
		t.Fatalf("expected cancelled ver-4, got %+v err=%v", cancelled, err)
	}

	// Claim ver-2 (user-a), then AbortBranchVersions cancels both processing ver-2 and pending ver-3 if on same branch
	claimed2, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{WorkerID: "worker-1", Now: now.Add(5 * time.Second), LeaseDuration: time.Minute})
	if err != nil || claimed2 == nil || claimed2.ID != "ver-2" {
		t.Fatalf("expected ver-2 claimed, got %+v err=%v", claimed2, err)
	}
	aborted1, err := branchRepo.AbortBranchVersions(ctx, "br-1", "branch deleted")
	if err != nil || len(aborted1) != 1 || aborted1[0].ReservationID != "res-2" {
		t.Fatalf("expected active ver-2 aborted with res-2, got %+v err=%v", aborted1, err)
	}

	// SupersedePendingVersions directly cancels ver-3 on br-2 and returns res-3
	if _, err := branchRepo.SupersedePendingVersions(ctx, "", "nope"); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("expected ErrValidation on empty branchID, got %v", err)
	}
	sup, err := branchRepo.SupersedePendingVersions(ctx, "br-2", "manual supersede")
	if err != nil || len(sup) != 1 || sup[0].ReservationID != "res-3" {
		t.Fatalf("expected res-3 superseded, got %+v err=%v", sup, err)
	}
}

func TestBranchStoreCompleteVersionGuards(t *testing.T) {
	ctx := context.Background()
	branchRepo, _ := setupIsolatedLeaseStores(t)
	now := time.Date(2026, 11, 15, 12, 30, 0, 0, time.UTC)

	if err := branchRepo.CreateBranch(ctx, &core.Branch{ID: "br-comp", UserID: "u1", Name: "Comp", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	_, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{ID: "ver-c1", UserID: "u1", ResumeID: "br-comp"}, core.PendingJobOptions{Now: now})
	if err != nil {
		t.Fatalf("SubmitPendingVersion ver-c1: %v", err)
	}
	if _, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{WorkerID: "w1", Now: now, LeaseDuration: time.Minute}); err != nil {
		t.Fatalf("ClaimNextVersion: %v", err)
	}

	// Wrong ClaimedBy -> ErrLeaseExpired; empty ClaimedBy on claimed version -> ErrConflict
	if err := branchRepo.CompleteVersion(ctx, &core.BranchVersion{ID: "ver-c1", ResumeID: "br-comp", ClaimedBy: "w-other"}); !errors.Is(err, core.ErrLeaseExpired) {
		t.Fatalf("expected ErrLeaseExpired for wrong worker completion, got %v", err)
	}
	if err := branchRepo.CompleteVersion(ctx, &core.BranchVersion{ID: "ver-c1", ResumeID: "br-comp"}); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("expected ErrConflict for unclaimed completion of active lease, got %v", err)
	}

	// Valid claimed completion clears lease metadata and promotes branch ActiveVersionID
	if err := branchRepo.CompleteVersion(ctx, &core.BranchVersion{ID: "ver-c1", ResumeID: "br-comp", ClaimedBy: "w1", HTMLOutput: "<p>ok</p>"}); err != nil {
		t.Fatalf("CompleteVersion valid: %v", err)
	}
	got, _ := branchRepo.Version(ctx, "br-comp", "ver-c1")
	if got.ClaimedBy != "" || got.LeaseExpiresAt != nil || got.Status != core.VersionStatusCompleted {
		t.Fatalf("expected lease fields cleared on completed version, got %+v", got)
	}

	// Cannot transition terminal version via UpdateVersionStatus
	if err := branchRepo.UpdateVersionStatus(ctx, "ver-c1", core.VersionStatusFailed, "late fail"); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("expected ErrConflict transitioning terminal version, got %v", err)
	}
}

func TestBranchStoreExpiryAndRecovery(t *testing.T) {
	ctx := context.Background()
	branchRepo, _ := setupIsolatedLeaseStores(t)
	now := time.Now().UTC()

	if err := branchRepo.CreateBranch(ctx, &core.Branch{ID: "br-exp", UserID: "u1", Name: "Exp", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	pastExp := now.Add(-time.Minute)
	futureExp := now.Add(time.Hour)
	_, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-expired", UserID: "u1", ResumeID: "br-exp", ReservationID: "res-exp", ExpiresAt: &pastExp,
	}, core.PendingJobOptions{Now: now})
	if err != nil {
		t.Fatalf("SubmitPendingVersion ver-expired: %v", err)
	}

	rel, err := branchRepo.ExpirePendingVersions(ctx, now, "ttl expired")
	if err != nil || len(rel) != 1 || rel[0].ReservationID != "res-exp" {
		t.Fatalf("expected 1 expired reservation, got rel=%v err=%v", rel, err)
	}

	_, err = branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-proc", UserID: "u1", ResumeID: "br-exp", ReservationID: "res-proc", ExpiresAt: &futureExp,
	}, core.PendingJobOptions{Now: now})
	if err != nil {
		t.Fatalf("SubmitPendingVersion ver-proc: %v", err)
	}
	if _, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{WorkerID: "w1", Now: now, LeaseDuration: time.Second}); err != nil {
		t.Fatalf("ClaimNextVersion: %v", err)
	}

	rel, err = branchRepo.FailExpiredVersionLeases(ctx, now.Add(5*time.Second), "lease timed out")
	if err != nil || len(rel) != 1 || rel[0].ReservationID != "res-proc" {
		t.Fatalf("expected res-proc released on lease expiry, got rel=%v err=%v", rel, err)
	}

	// RecoverInterruptedVersions preserves valid pending while failing processing
	_, err = branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-valid-pending", UserID: "u1", ResumeID: "br-exp", ExpiresAt: &futureExp,
	}, core.PendingJobOptions{Now: now})
	if err != nil {
		t.Fatalf("SubmitPendingVersion ver-valid-pending: %v", err)
	}
	if _, err := branchRepo.RecoverInterruptedVersions(ctx, "restart"); err != nil {
		t.Fatalf("RecoverInterruptedVersions: %v", err)
	}
	gotPending, err := branchRepo.Version(ctx, "br-exp", "ver-valid-pending")
	if err != nil || gotPending.Status != core.VersionStatusPending {
		t.Fatalf("expected unexpired pending preserved, got %+v err=%v", gotPending, err)
	}
}

func TestPDFJobStoreLeaseAndQueueLifecycle(t *testing.T) {
	ctx := context.Background()
	branchRepo, pdfRepo := setupIsolatedLeaseStores(t)
	now := time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC)

	// SubmitPendingJob validation & max raw text bytes
	if err := pdfRepo.SubmitPendingJob(ctx, nil, core.PendingJobOptions{Now: now}); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("expected ErrValidation for nil job, got %v", err)
	}
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-big", UserID: "user-1", RawText: strings.Repeat("a", 64),
	}, core.PendingJobOptions{Now: now, MaxRawTextBytes: 32}); !errors.Is(err, core.ErrValidation) {
		t.Fatalf("expected ErrValidation for oversized RawText, got %v", err)
	}

	// Cross-queue concurrency: active processing branch version blocks PDF job claim for same tenant
	if err := branchRepo.CreateBranch(ctx, &core.Branch{ID: "br-x", UserID: "user-1", Name: "X", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{ID: "ver-x", UserID: "user-1", ResumeID: "br-x"}, core.PendingJobOptions{Now: now}); err != nil {
		t.Fatalf("SubmitPendingVersion: %v", err)
	}
	if _, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{WorkerID: "w-branch", Now: now, LeaseDuration: time.Minute}); err != nil {
		t.Fatalf("ClaimNextVersion: %v", err)
	}

	job1 := &core.PDFParseJob{ID: "pdf-1", UserID: "user-1", RawText: "secret pdf text", ReservationID: "res-pdf-1"}
	if err := pdfRepo.SubmitPendingJob(ctx, job1, core.PendingJobOptions{Now: now}); err != nil {
		t.Fatalf("SubmitPendingJob: %v", err)
	}
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{ID: "pdf-1", UserID: "user-1", RawText: "dup"}, core.PendingJobOptions{Now: now}); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate job ID, got %v", err)
	}

	// MaxConcurrentPerTenant=1 should skip user-1 because ver-x is processing
	claimed, err := pdfRepo.ClaimNextJob(ctx, core.ClaimJobRequest{WorkerID: "w-pdf", Now: now, LeaseDuration: time.Minute, MaxConcurrentPerTenant: 1})
	if err != nil || claimed != nil {
		t.Fatalf("expected nil claim due to cross-queue active version, got %+v err=%v", claimed, err)
	}

	if err := branchRepo.FailClaimedVersion(ctx, "ver-x", "w-branch", "done"); err != nil {
		t.Fatalf("FailClaimedVersion: %v", err)
	}

	claimed, err = pdfRepo.ClaimNextJob(ctx, core.ClaimJobRequest{WorkerID: "w-pdf", Now: now, LeaseDuration: time.Minute, MaxConcurrentPerTenant: 1})
	if err != nil || claimed == nil || claimed.ID != "pdf-1" || claimed.AttemptCount != 1 {
		t.Fatalf("expected pdf-1 claimed, got %+v err=%v", claimed, err)
	}

	// Renew, requeue (error + valid), reclaim, and CompleteClaimedJob (prunes RawText)
	if err := pdfRepo.RenewJobLease(ctx, "pdf-1", "wrong-worker", now.Add(2*time.Minute)); !errors.Is(err, core.ErrLeaseExpired) {
		t.Fatalf("expected ErrLeaseExpired on wrong worker renew, got %v", err)
	}
	if err := pdfRepo.RenewJobLease(ctx, "pdf-1", "w-pdf", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("RenewJobLease: %v", err)
	}
	if err := pdfRepo.RequeueJobForRetry(ctx, "pdf-1", "w-pdf", job1.ExpiresAt.Add(time.Minute), "past ttl"); !errors.Is(err, core.ErrJobExpired) {
		t.Fatalf("expected ErrJobExpired when retry past ExpiresAt, got %v", err)
	}
	if err := pdfRepo.RequeueJobForRetry(ctx, "pdf-1", "w-pdf", now.Add(10*time.Second), "retry"); err != nil {
		t.Fatalf("RequeueJobForRetry: %v", err)
	}
	early, err := pdfRepo.ClaimNextJob(ctx, core.ClaimJobRequest{WorkerID: "w-pdf", Now: now.Add(5 * time.Second)})
	if err != nil || early != nil {
		t.Fatalf("expected nil before NextRetryAt, got %+v err=%v", early, err)
	}
	reclaimed, err := pdfRepo.ClaimNextJob(ctx, core.ClaimJobRequest{WorkerID: "w-pdf", Now: now.Add(10 * time.Second)})
	if err != nil || reclaimed == nil || reclaimed.AttemptCount != 2 {
		t.Fatalf("expected AttemptCount=2 on reclaim, got %+v err=%v", reclaimed, err)
	}

	res := &core.JSONResume{Basics: core.JSONResumeBasics{Name: "Grace Hopper"}}
	if err := pdfRepo.CompleteClaimedJob(ctx, "pdf-1", "w-pdf", res); err != nil {
		t.Fatalf("CompleteClaimedJob: %v", err)
	}
	finalJob, err := pdfRepo.Job(ctx, "user-1", "pdf-1")
	if err != nil || finalJob.Status != core.PDFParseJobStatusCompleted || finalJob.RawText != "" || finalJob.Result == nil || finalJob.Result.Basics.Name != "Grace Hopper" {
		t.Fatalf("unexpected completed job: %+v err=%v", finalJob, err)
	}
	if err := pdfRepo.UpdateJobStatus(ctx, "pdf-1", core.PDFParseJobStatusFailed, nil, "late fail"); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("expected ErrConflict transitioning terminal PDF job, got %v", err)
	}

	// FailClaimedJob prunes RawText and sets ErrorMessage
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{ID: "pdf-fail", UserID: "user-1", RawText: "sensitive"}, core.PendingJobOptions{Now: now}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-fail: %v", err)
	}
	if _, err := pdfRepo.ClaimNextJob(ctx, core.ClaimJobRequest{WorkerID: "w-pdf", Now: now}); err != nil {
		t.Fatalf("ClaimNextJob pdf-fail: %v", err)
	}
	if err := pdfRepo.FailClaimedJob(ctx, "pdf-fail", "w-pdf", "llm timeout"); err != nil {
		t.Fatalf("FailClaimedJob: %v", err)
	}
	failedJob, _ := pdfRepo.Job(ctx, "user-1", "pdf-fail")
	if failedJob.Status != core.PDFParseJobStatusFailed || failedJob.RawText != "" || failedJob.ErrorMessage != "llm timeout" {
		t.Fatalf("unexpected failed job state: %+v", failedJob)
	}

	// CancelJob prunes RawText
	job2 := &core.PDFParseJob{ID: "pdf-2", UserID: "user-1", RawText: "sensitive raw text", ReservationID: "res-pdf-2"}
	if err := pdfRepo.SubmitPendingJob(ctx, job2, core.PendingJobOptions{Now: now}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-2: %v", err)
	}
	if _, err := pdfRepo.CancelJob(ctx, "other-user", "pdf-2", "wrong user"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on non-owner CancelJob, got %v", err)
	}
	cancelled, err := pdfRepo.CancelJob(ctx, "user-1", "pdf-2", "user cancelled")
	if err != nil || cancelled.Status != core.PDFParseJobStatusCancelled || cancelled.RawText != "" {
		t.Fatalf("expected cancelled pdf-2 with pruned RawText, got %+v err=%v", cancelled, err)
	}

	// ExpirePendingJobs, FailExpiredJobLeases, RecoverInterruptedJobs
	pastExp := now.Add(-time.Minute)
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-exp", UserID: "user-1", RawText: "exp", ReservationID: "res-exp", ExpiresAt: &pastExp,
	}, core.PendingJobOptions{Now: now}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-exp: %v", err)
	}
	rel, err := pdfRepo.ExpirePendingJobs(ctx, now, "ttl expired")
	if err != nil || len(rel) != 1 || rel[0].ReservationID != "res-exp" {
		t.Fatalf("expected res-exp released, got rel=%v err=%v", rel, err)
	}

	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-lease-exp", UserID: "user-1", RawText: "lease raw", ReservationID: "res-lease-exp",
	}, core.PendingJobOptions{Now: now}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-lease-exp: %v", err)
	}
	if _, err := pdfRepo.ClaimNextJob(ctx, core.ClaimJobRequest{WorkerID: "w-pdf", Now: now, LeaseDuration: time.Second}); err != nil {
		t.Fatalf("ClaimNextJob pdf-lease-exp: %v", err)
	}
	rel, err = pdfRepo.FailExpiredJobLeases(ctx, now.Add(5*time.Second), "lease expired")
	if err != nil || len(rel) != 1 || rel[0].ReservationID != "res-lease-exp" {
		t.Fatalf("expected res-lease-exp released on FailExpiredJobLeases, got rel=%v err=%v", rel, err)
	}

	futureExp := time.Now().UTC().Add(time.Hour)
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-valid", UserID: "user-2", RawText: "keep me", ExpiresAt: &futureExp,
	}, core.PendingJobOptions{Now: time.Now().UTC()}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-valid: %v", err)
	}
	if _, err := pdfRepo.RecoverInterruptedJobs(ctx, "reboot"); err != nil {
		t.Fatalf("RecoverInterruptedJobs: %v", err)
	}
	kept, err := pdfRepo.Job(ctx, "user-2", "pdf-valid")
	if err != nil || kept.Status != core.PDFParseJobStatusPending || kept.RawText != "keep me" {
		t.Fatalf("expected valid pending job preserved on RecoverInterruptedJobs, got %+v err=%v", kept, err)
	}
}
