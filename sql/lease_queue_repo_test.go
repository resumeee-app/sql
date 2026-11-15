package sql

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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

	t.Run("validation rejects nil, non-pending status, and missing branch", func(t *testing.T) {
		cases := []struct {
			name string
			ver  *core.BranchVersion
		}{
			{name: "nil version", ver: nil},
			{name: "non-pending status", ver: &core.BranchVersion{ID: "v-bad-status", UserID: "user-a", ResumeID: "br-a1", Status: core.VersionStatusProcessing}},
			{name: "missing branch", ver: &core.BranchVersion{ID: "v-no-br", UserID: "user-a"}},
		}
		for _, tc := range cases {
			if _, err := branchRepo.SubmitPendingVersion(ctx, tc.ver, core.PendingJobOptions{Now: now}); !errors.Is(err, core.ErrValidation) {
				t.Errorf("%s: expected ErrValidation, got %v", tc.name, err)
			}
		}
	})

	t.Run("nonexistent branch, foreign-owner isolation, and UserID inheritance", func(t *testing.T) {
		for _, uid := range []string{"user-a", ""} {
			vMissing := &core.BranchVersion{ID: "ver-missing-br-" + uid, UserID: uid, ResumeID: "br-does-not-exist"}
			if _, err := branchRepo.SubmitPendingVersion(ctx, vMissing, core.PendingJobOptions{Now: now}); !errors.Is(err, core.ErrNotFound) {
				t.Fatalf("uid=%q: expected ErrNotFound on nonexistent branch, got %v", uid, err)
			}
			if _, lookupErr := branchRepo.Version(ctx, "br-does-not-exist", vMissing.ID); !errors.Is(lookupErr, core.ErrNotFound) {
				t.Fatalf("uid=%q: expected version not persisted for nonexistent branch, got %v", uid, lookupErr)
			}
		}

		// Victim pending version on br-a1 must remain intact if user-b tries submitting to br-a1.
		if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
			ID: "ver-victim", UserID: "user-a", ResumeID: "br-a1", ReservationID: "res-victim",
		}, core.PendingJobOptions{Now: now}); err != nil {
			t.Fatalf("SubmitPendingVersion ver-victim: %v", err)
		}
		released, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
			ID: "ver-foreign", UserID: "user-b", ResumeID: "br-a1",
		}, core.PendingJobOptions{Now: now.Add(time.Second)})
		if !errors.Is(err, core.ErrNotFound) || len(released) != 0 {
			t.Fatalf("expected ErrNotFound and 0 released reservations for foreign branch submit, got rel=%+v err=%v", released, err)
		}
		stillPending, err := branchRepo.Version(ctx, "br-a1", "ver-victim")
		if err != nil || stillPending.Status != core.VersionStatusPending || stillPending.ReservationID != "res-victim" {
			t.Fatalf("expected victim pending version untouched, got %+v err=%v", stillPending, err)
		}

		// Empty UserID inherits parent branch UserID and supersedes victim version legitimately.
		inherited := &core.BranchVersion{ID: "ver-inherited", ResumeID: "br-a1"}
		rel, err := branchRepo.SubmitPendingVersion(ctx, inherited, core.PendingJobOptions{Now: now.Add(2 * time.Second)})
		if err != nil || len(rel) != 1 || rel[0].ReservationID != "res-victim" {
			t.Fatalf("expected inherited submit to succeed and supersede res-victim, got rel=%+v err=%v", rel, err)
		}
		gotInherited, err := branchRepo.Version(ctx, "br-a1", "ver-inherited")
		if err != nil || gotInherited.UserID != "user-a" {
			t.Fatalf("expected inherited UserID user-a, got %+v err=%v", gotInherited, err)
		}
		// Branch with empty UserID and version with empty UserID -> ErrValidation.
		if err := branchRepo.CreateBranch(ctx, &core.Branch{ID: "br-empty-usr", Name: "EmptyUser", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("CreateBranch br-empty-usr: %v", err)
		}
		if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
			ID: "ver-empty-usr", ResumeID: "br-empty-usr",
		}, core.PendingJobOptions{Now: now}); !errors.Is(err, core.ErrValidation) {
			t.Fatalf("expected ErrValidation when both branch and version UserID are empty, got %v", err)
		}

		// Clean up ver-inherited so subsequent global cap checks start from empty queue.
		if _, err := branchRepo.CancelVersion(ctx, "user-a", "br-a1", "ver-inherited", "cleanup"); err != nil {
			t.Fatalf("cleanup ver-inherited: %v", err)
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

	// ClaimNextVersion skips ver-4 while NextRetryAt > Now even if user-b has lower LastServedSeq, claiming ver-2 instead.
	claimed2, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{
		WorkerID:      "worker-1",
		Now:           now.Add(5 * time.Second),
		LeaseDuration: time.Minute,
		LastServedSeq: map[string]uint64{"user-a": 100, "user-b": 0},
	})
	if err != nil || claimed2 == nil || claimed2.ID != "ver-2" {
		t.Fatalf("expected ver-2 claimed while ver-4 is in retry backoff, got %+v err=%v", claimed2, err)
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

	// Only ver-4 remains pending: ClaimNextVersion returns nil before retryAt and claims ver-4 at retryAt with AttemptCount=2.
	earlyClaim, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{
		WorkerID: "worker-2", Now: retryAt.Add(-time.Second), LeaseDuration: time.Minute,
	})
	if err != nil || earlyClaim != nil {
		t.Fatalf("expected nil claim before NextRetryAt, got %+v err=%v", earlyClaim, err)
	}
	reclaimed, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{
		WorkerID: "worker-2", Now: retryAt, LeaseDuration: time.Minute,
	})
	if err != nil || reclaimed == nil || reclaimed.ID != "ver-4" || reclaimed.AttemptCount != 2 {
		t.Fatalf("expected ver-4 reclaimed at NextRetryAt with AttemptCount=2, got %+v err=%v", reclaimed, err)
	}
	if err := branchRepo.FailClaimedVersion(ctx, "ver-4", "worker-2", "final failure"); err != nil {
		t.Fatalf("FailClaimedVersion: %v", err)
	}

	// CancelVersion succeeds on an owned pending version.
	if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-cancel", UserID: "user-b", ResumeID: "br-3", ReservationID: "res-cancel",
	}, core.PendingJobOptions{Now: retryAt}); err != nil {
		t.Fatalf("SubmitPendingVersion ver-cancel: %v", err)
	}
	cancelled, err := branchRepo.CancelVersion(ctx, "user-b", "br-3", "ver-cancel", "user aborted")
	if err != nil || cancelled.Status != core.VersionStatusCancelled {
		t.Fatalf("expected cancelled ver-cancel, got %+v err=%v", cancelled, err)
	}
}

func TestBranchStoreCompleteVersionGuards(t *testing.T) {
	ctx := context.Background()
	branchRepo, _ := setupIsolatedLeaseStores(t)
	now := time.Date(2026, 11, 15, 12, 30, 0, 0, time.UTC)

	if err := branchRepo.CreateBranch(ctx, &core.Branch{ID: "br-comp", UserID: "u1", Name: "Comp", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	_, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID:                "ver-c1",
		UserID:            "u1",
		ResumeID:          "br-comp",
		ReservationID:     "res-c1",
		MainRevisionUsed:  7,
		TemplateSetIDUsed: "tmpl-clean",
		ProviderUsed:      "gemini",
		ModelUsed:         "gemini-pro",
		SourceType:        core.SourceTypeLLMGeneration,
	}, core.PendingJobOptions{Now: now})
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

	// Valid partial claimed completion clears lease metadata, promotes branch ActiveVersionID, and preserves existing metadata columns.
	if err := branchRepo.CompleteVersion(ctx, &core.BranchVersion{ID: "ver-c1", ResumeID: "br-comp", ClaimedBy: "w1", HTMLOutput: "<p>ok</p>"}); err != nil {
		t.Fatalf("CompleteVersion valid: %v", err)
	}
	got, _ := branchRepo.Version(ctx, "br-comp", "ver-c1")
	if got.ClaimedBy != "" || got.LeaseExpiresAt != nil || got.Status != core.VersionStatusCompleted {
		t.Fatalf("expected lease fields cleared on completed version, got %+v", got)
	}
	if got.AttemptCount != 1 || got.ReservationID != "res-c1" || got.MainRevisionUsed != 7 ||
		got.TemplateSetIDUsed != "tmpl-clean" || got.ProviderUsed != "gemini" || got.ModelUsed != "gemini-pro" ||
		got.SourceType != core.SourceTypeLLMGeneration || got.ClaimedAt == nil || got.ExpiresAt == nil {
		t.Fatalf("expected existing metadata columns preserved on partial CompleteVersion, got %+v", got)
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

	// RecoverInterruptedVersions fails interrupted processing and expired pending while preserving unexpired pending
	realNow := time.Now().UTC()
	pastExp = realNow.Add(-time.Minute)
	futureRecoveryExp := realNow.Add(time.Hour)
	for _, bID := range []string{"br-int-proc", "br-int-exp"} {
		if err := branchRepo.CreateBranch(ctx, &core.Branch{ID: bID, UserID: "u1", Name: bID}); err != nil {
			t.Fatalf("CreateBranch %s: %v", bID, err)
		}
	}
	if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-int-proc", UserID: "u1", ResumeID: "br-int-proc", ReservationID: "res-int-proc", ExpiresAt: &futureRecoveryExp,
	}, core.PendingJobOptions{Now: realNow}); err != nil {
		t.Fatalf("SubmitPendingVersion ver-int-proc: %v", err)
	}
	if claimedInt, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{
		WorkerID: "w-int", Now: realNow, LeaseDuration: time.Hour,
	}); err != nil || claimedInt == nil || claimedInt.ID != "ver-int-proc" {
		t.Fatalf("expected ver-int-proc claimed, got %+v err=%v", claimedInt, err)
	}
	if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-int-exp", UserID: "u1", ResumeID: "br-int-exp", ReservationID: "res-int-exp", ExpiresAt: &pastExp,
	}, core.PendingJobOptions{Now: realNow.Add(-2 * time.Minute)}); err != nil {
		t.Fatalf("SubmitPendingVersion ver-int-exp: %v", err)
	}
	if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-valid-pending", UserID: "u1", ResumeID: "br-exp", ExpiresAt: &futureRecoveryExp,
	}, core.PendingJobOptions{Now: realNow}); err != nil {
		t.Fatalf("SubmitPendingVersion ver-valid-pending: %v", err)
	}

	recRel, err := branchRepo.RecoverInterruptedVersions(ctx, "restart")
	if err != nil {
		t.Fatalf("RecoverInterruptedVersions: %v", err)
	}
	if len(recRel) != 2 {
		t.Fatalf("expected 2 released reservations from recovery, got %+v", recRel)
	}
	for _, idCheck := range []struct {
		branchID  string
		versionID string
		want      core.VersionStatus
	}{
		{"br-int-proc", "ver-int-proc", core.VersionStatusFailed},
		{"br-int-exp", "ver-int-exp", core.VersionStatusFailed},
		{"br-exp", "ver-valid-pending", core.VersionStatusPending},
	} {
		got, err := branchRepo.Version(ctx, idCheck.branchID, idCheck.versionID)
		if err != nil || got.Status != idCheck.want {
			t.Fatalf("version %s: got status %+v err=%v, want %s", idCheck.versionID, got, err, idCheck.want)
		}
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

	realNow := time.Now().UTC()
	pastExp = realNow.Add(-time.Minute)
	futureExp := realNow.Add(time.Hour)
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-int-proc", UserID: "user-2", RawText: "secret", ReservationID: "res-pdf-int-proc", ExpiresAt: &futureExp,
	}, core.PendingJobOptions{Now: realNow}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-int-proc: %v", err)
	}
	if claimedInt, err := pdfRepo.ClaimNextJob(ctx, core.ClaimJobRequest{
		WorkerID: "w-pdf-int", Now: realNow, LeaseDuration: time.Hour,
	}); err != nil || claimedInt == nil || claimedInt.ID != "pdf-int-proc" {
		t.Fatalf("expected pdf-int-proc claimed, got %+v err=%v", claimedInt, err)
	}
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-int-exp", UserID: "user-2", RawText: "expired", ReservationID: "res-pdf-int-exp", ExpiresAt: &pastExp,
	}, core.PendingJobOptions{Now: realNow.Add(-2 * time.Minute)}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-int-exp: %v", err)
	}
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-valid", UserID: "user-2", RawText: "keep me", ExpiresAt: &futureExp,
	}, core.PendingJobOptions{Now: realNow}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-valid: %v", err)
	}
	recRel, err := pdfRepo.RecoverInterruptedJobs(ctx, "reboot")
	if err != nil {
		t.Fatalf("RecoverInterruptedJobs: %v", err)
	}
	if len(recRel) != 2 {
		t.Fatalf("expected 2 released reservations from pdf recovery, got %+v", recRel)
	}
	for _, tc := range []struct {
		jobID    string
		want     core.PDFParseJobStatus
		wantText string
	}{
		{"pdf-int-proc", core.PDFParseJobStatusFailed, ""},
		{"pdf-int-exp", core.PDFParseJobStatusFailed, ""},
		{"pdf-valid", core.PDFParseJobStatusPending, "keep me"},
	} {
		got, err := pdfRepo.Job(ctx, "user-2", tc.jobID)
		if err != nil || got.Status != tc.want || got.RawText != tc.wantText {
			t.Fatalf("job %s: got %+v err=%v, want status=%s rawText=%q", tc.jobID, got, err, tc.want, tc.wantText)
		}
	}
}

func TestLeaseQueueTimezoneNormalization(t *testing.T) {
	ctx := context.Background()
	branchRepo, pdfRepo := setupIsolatedLeaseStores(t)
	pst := time.FixedZone("PST", -8*3600)
	eet := time.FixedZone("EET", 2*3600)

	baseUTC := time.Date(2026, 11, 15, 19, 0, 0, 0, time.UTC)
	// 12:00 PST == 20:00 UTC (> 19:00 UTC); without UTC normalization "-08:00" sorts earlier than "+00:00" on same clock hour.
	pstNoon := time.Date(2026, 11, 15, 12, 0, 0, 0, pst)
	// 20:30 EET == 18:30 UTC (< 19:00 UTC); without UTC normalization "20:30:00+02:00" sorts after "19:00:00+00:00".
	eetLate := time.Date(2026, 11, 15, 20, 30, 0, 0, eet)

	for _, bID := range []string{"br-pst", "br-eet"} {
		if err := branchRepo.CreateBranch(ctx, &core.Branch{ID: bID, UserID: "u-tz", Name: bID}); err != nil {
			t.Fatalf("CreateBranch %s: %v", bID, err)
		}
	}
	if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-pst", UserID: "u-tz", ResumeID: "br-pst", ReservationID: "res-ver-pst", ExpiresAt: &pstNoon,
	}, core.PendingJobOptions{Now: baseUTC}); err != nil {
		t.Fatalf("SubmitPendingVersion ver-pst: %v", err)
	}
	if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
		ID: "ver-eet", UserID: "u-tz", ResumeID: "br-eet", ReservationID: "res-ver-eet", ExpiresAt: &eetLate,
	}, core.PendingJobOptions{Now: baseUTC.Add(-time.Hour)}); err != nil {
		t.Fatalf("SubmitPendingVersion ver-eet: %v", err)
	}

	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-pst", UserID: "u-tz", RawText: "raw-pst", ReservationID: "res-pdf-pst", ExpiresAt: &pstNoon,
	}, core.PendingJobOptions{Now: baseUTC}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-pst: %v", err)
	}
	if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
		ID: "pdf-eet", UserID: "u-tz", RawText: "raw-eet", ReservationID: "res-pdf-eet", ExpiresAt: &eetLate,
	}, core.PendingJobOptions{Now: baseUTC.Add(-time.Hour)}); err != nil {
		t.Fatalf("SubmitPendingJob pdf-eet: %v", err)
	}

	verRel, err := branchRepo.ExpirePendingVersions(ctx, baseUTC, "tz expiry")
	if err != nil || len(verRel) != 1 || verRel[0].ReservationID != "res-ver-eet" {
		t.Fatalf("expected only ver-eet expired at 19:00 UTC, got rel=%+v err=%v", verRel, err)
	}
	pdfRel, err := pdfRepo.ExpirePendingJobs(ctx, baseUTC, "tz expiry")
	if err != nil || len(pdfRel) != 1 || pdfRel[0].ReservationID != "res-pdf-eet" {
		t.Fatalf("expected only pdf-eet expired at 19:00 UTC, got rel=%+v err=%v", pdfRel, err)
	}

	// Verify RenewVersionLease normalizes non-UTC lease_expires_at so FailExpiredVersionLeases compares instants accurately.
	claimedTZ, err := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{
		WorkerID: "w-tz", Now: baseUTC, LeaseDuration: time.Minute,
	})
	if err != nil || claimedTZ == nil || claimedTZ.ID != "ver-pst" {
		t.Fatalf("expected ver-pst claimed, got %+v err=%v", claimedTZ, err)
	}
	if err := branchRepo.RenewVersionLease(ctx, "ver-pst", "w-tz", pstNoon); err != nil {
		t.Fatalf("RenewVersionLease non-UTC: %v", err)
	}
	if earlyRel, err := branchRepo.FailExpiredVersionLeases(ctx, baseUTC, "too early"); err != nil || len(earlyRel) != 0 {
		t.Fatalf("expected 0 expired leases at 19:00 UTC before 20:00 UTC (12:00 PST), got rel=%+v err=%v", earlyRel, err)
	}
	if err := branchRepo.RenewVersionLease(ctx, "ver-pst", "w-tz", eetLate); err != nil {
		t.Fatalf("RenewVersionLease EET: %v", err)
	}
	if lateRel, err := branchRepo.FailExpiredVersionLeases(ctx, baseUTC, "expired eet"); err != nil || len(lateRel) != 1 {
		t.Fatalf("expected ver-pst lease expired at 19:00 UTC after 18:30 UTC (20:30 EET), got rel=%+v err=%v", lateRel, err)
	}
}

func TestLeaseQueueFileBackedConcurrency(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "concurrent_queue.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New file-backed sqlite db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
	})

	branchRepo := NewBranchStore(db)
	pdfRepo := NewPDFJobStore(db)
	now := time.Date(2026, 11, 15, 14, 0, 0, 0, time.UTC)

	const totalBranches = 12
	for i := 0; i < totalBranches; i++ {
		if err := branchRepo.CreateBranch(ctx, &core.Branch{
			ID: fmt.Sprintf("br-c-%d", i), UserID: fmt.Sprintf("u-%d", i%3), Name: "Conc",
		}); err != nil {
			t.Fatalf("CreateBranch %d: %v", i, err)
		}
	}

	// Concurrent SubmitPendingVersion against MaxPendingPerTenant=2 on distinct branches for one tenant.
	for i := 0; i < 6; i++ {
		if err := branchRepo.CreateBranch(ctx, &core.Branch{ID: fmt.Sprintf("br-cap-%d", i), UserID: "u-cap", Name: "Cap"}); err != nil {
			t.Fatalf("CreateBranch cap %d: %v", i, err)
		}
	}
	var subWG sync.WaitGroup
	subResults := make(chan error, 6)
	for i := 0; i < 6; i++ {
		subWG.Add(1)
		go func(idx int) {
			defer subWG.Done()
			_, subErr := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
				ID: fmt.Sprintf("ver-cap-%d", idx), UserID: "u-cap", ResumeID: fmt.Sprintf("br-cap-%d", idx),
			}, core.PendingJobOptions{Now: now, MaxPendingPerTenant: 2})
			subResults <- subErr
		}(i)
	}
	subWG.Wait()
	close(subResults)

	admittedCap := 0
	for resErr := range subResults {
		if resErr == nil {
			admittedCap++
		} else if !errors.Is(resErr, core.ErrPendingLimitExceeded) {
			t.Fatalf("unexpected concurrent SubmitPendingVersion error: %v", resErr)
		}
	}
	if admittedCap != 2 {
		t.Fatalf("expected exactly 2 admitted versions under MaxPendingPerTenant=2, got %d", admittedCap)
	}

	// Concurrent SubmitPendingJob against MaxGlobalPending=2 across tenants.
	var pdfSubWG sync.WaitGroup
	pdfSubErrs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		pdfSubWG.Add(1)
		go func(idx int) {
			defer pdfSubWG.Done()
			pdfSubErrs <- pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
				ID: fmt.Sprintf("pdf-cap-%d", idx), UserID: fmt.Sprintf("u-pcap-%d", idx), RawText: "cap raw",
			}, core.PendingJobOptions{Now: now, MaxPendingPerTenant: 5, MaxGlobalPending: 4})
		}(i)
	}
	pdfSubWG.Wait()
	close(pdfSubErrs)

	admittedPDFCap := 0
	for resErr := range pdfSubErrs {
		if resErr == nil {
			admittedPDFCap++
		} else if !errors.Is(resErr, core.ErrPendingLimitExceeded) {
			t.Fatalf("unexpected concurrent SubmitPendingJob error: %v", resErr)
		}
	}
	// Two branch versions from u-cap remain pending, so MaxGlobalPending=4 permits at most 2 PDF jobs.
	if admittedPDFCap != 2 {
		t.Fatalf("expected exactly 2 admitted pdf jobs under MaxGlobalPending=4 with 2 pending versions, got %d", admittedPDFCap)
	}

	// Clear cap jobs and versions before claim storm.
	for i := 0; i < 6; i++ {
		_, _ = branchRepo.AbortBranchVersions(ctx, fmt.Sprintf("br-cap-%d", i), "reset")
		_, _ = pdfRepo.CancelJob(ctx, fmt.Sprintf("u-pcap-%d", i), fmt.Sprintf("pdf-cap-%d", i), "reset")
	}

	for i := 0; i < totalBranches; i++ {
		if _, err := branchRepo.SubmitPendingVersion(ctx, &core.BranchVersion{
			ID: fmt.Sprintf("ver-c-%d", i), UserID: fmt.Sprintf("u-%d", i%3), ResumeID: fmt.Sprintf("br-c-%d", i),
		}, core.PendingJobOptions{Now: now, MaxPendingPerTenant: 20}); err != nil {
			t.Fatalf("SubmitPendingVersion %d: %v", i, err)
		}
		if err := pdfRepo.SubmitPendingJob(ctx, &core.PDFParseJob{
			ID: fmt.Sprintf("pdf-c-%d", i), UserID: fmt.Sprintf("u-%d", i%3), RawText: "concurrent raw",
		}, core.PendingJobOptions{Now: now, MaxPendingPerTenant: 20}); err != nil {
			t.Fatalf("SubmitPendingJob %d: %v", i, err)
		}
	}

	// 12 concurrent workers race to claim all 12 versions and 12 PDF jobs without SQLITE_BUSY or duplicates.
	var claimWG sync.WaitGroup
	verIDs := make(chan string, totalBranches)
	pdfIDs := make(chan string, totalBranches)
	claimErrs := make(chan error, totalBranches*2)

	for w := 0; w < totalBranches; w++ {
		claimWG.Add(1)
		go func(workerIdx int) {
			defer claimWG.Done()
			workerID := fmt.Sprintf("worker-%d", workerIdx)
			v, cErr := branchRepo.ClaimNextVersion(ctx, core.ClaimJobRequest{
				WorkerID: workerID, Now: now, LeaseDuration: time.Minute,
			})
			if cErr != nil {
				claimErrs <- cErr
			} else if v != nil {
				verIDs <- v.ID
			}
			p, pErr := pdfRepo.ClaimNextJob(ctx, core.ClaimJobRequest{
				WorkerID: workerID, Now: now, LeaseDuration: time.Minute,
			})
			if pErr != nil {
				claimErrs <- pErr
			} else if p != nil {
				pdfIDs <- p.ID
			}
		}(w)
	}
	claimWG.Wait()
	close(claimErrs)
	close(verIDs)
	close(pdfIDs)

	for cErr := range claimErrs {
		t.Fatalf("concurrent ClaimNext error: %v", cErr)
	}
	seenVer := make(map[string]bool)
	for id := range verIDs {
		if seenVer[id] {
			t.Fatalf("duplicate claimed version ID %s", id)
		}
		seenVer[id] = true
	}
	seenPDF := make(map[string]bool)
	for id := range pdfIDs {
		if seenPDF[id] {
			t.Fatalf("duplicate claimed pdf job ID %s", id)
		}
		seenPDF[id] = true
	}
	if len(seenVer) != totalBranches || len(seenPDF) != totalBranches {
		t.Fatalf("expected all %d versions and %d pdf jobs claimed cleanly, got ver=%d pdf=%d", totalBranches, totalBranches, len(seenVer), len(seenPDF))
	}
}
