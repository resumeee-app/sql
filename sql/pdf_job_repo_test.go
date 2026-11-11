package sql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/resumeee-app/resumeee-core/domain"
)

func TestPDFParseJobRepository_Lifecycle(t *testing.T) {
	db, err := New("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to initialize sqlite: %v", err)
	}

	repo := NewPDFParseJobRepository(db)
	ctx := context.Background()
	userID := "user-pdf-test-1"

	t.Run("Create and Get Job", func(t *testing.T) {
		job := &domain.PDFParseJob{
			ID:        "job-101",
			UserID:    userID,
			Status:    domain.PDFParseJobStatusPending,
			Filename:  "sample_resume.pdf",
			Provider:  "mock",
			Model:     "mock-model",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := repo.CreateJob(ctx, job); err != nil {
			t.Fatalf("failed to create job: %v", err)
		}

		retrieved, err := repo.GetJob(ctx, userID, "job-101")
		if err != nil {
			t.Fatalf("failed to get job: %v", err)
		}

		if retrieved.ID != "job-101" {
			t.Errorf("expected ID 'job-101', got %q", retrieved.ID)
		}
		if retrieved.Status != domain.PDFParseJobStatusPending {
			t.Errorf("expected status 'pending', got %q", retrieved.Status)
		}
		if retrieved.Filename != "sample_resume.pdf" {
			t.Errorf("expected filename 'sample_resume.pdf', got %q", retrieved.Filename)
		}
	})

	t.Run("GetJob returns ErrNotFound for non-existent job or wrong user", func(t *testing.T) {
		_, err := repo.GetJob(ctx, userID, "non-existent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound for non-existent job, got: %v", err)
		}

		_, err = repo.GetJob(ctx, "wrong-user", "job-101")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("expected ErrNotFound for wrong user, got: %v", err)
		}
	})

	t.Run("UpdateJobStatus to Processing and Completed with Result", func(t *testing.T) {
		jobID := "job-102"
		job := &domain.PDFParseJob{
			ID:        jobID,
			UserID:    userID,
			Status:    domain.PDFParseJobStatusPending,
			Filename:  "career.pdf",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := repo.CreateJob(ctx, job); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		// Transition to processing
		if err := repo.UpdateJobStatus(ctx, jobID, domain.PDFParseJobStatusProcessing, nil, ""); err != nil {
			t.Fatalf("update to processing failed: %v", err)
		}

		proc, err := repo.GetJob(ctx, userID, jobID)
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if proc.Status != domain.PDFParseJobStatusProcessing {
			t.Errorf("expected status 'processing', got %q", proc.Status)
		}

		// Transition to completed with structured JSON result
		resultPayload := map[string]any{
			"basics": map[string]any{
				"name":  "Ada Lovelace",
				"email": "ada@analytical.org",
			},
			"skills": []any{
				map[string]any{"name": "Mechanical Computation"},
			},
		}

		if err := repo.UpdateJobStatus(ctx, jobID, domain.PDFParseJobStatusCompleted, resultPayload, ""); err != nil {
			t.Fatalf("update to completed failed: %v", err)
		}

		comp, err := repo.GetJob(ctx, userID, jobID)
		if err != nil {
			t.Fatalf("get completed failed: %v", err)
		}
		if comp.Status != domain.PDFParseJobStatusCompleted {
			t.Errorf("expected status 'completed', got %q", comp.Status)
		}
		if comp.CompletedAt == nil {
			t.Errorf("expected non-nil CompletedAt")
		}

		resMap, ok := comp.Result.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any result, got %T: %+v", comp.Result, comp.Result)
		}
		basics, ok := resMap["basics"].(map[string]any)
		if !ok || basics["name"] != "Ada Lovelace" {
			t.Errorf("expected basics.name == 'Ada Lovelace', got %+v", resMap)
		}
	})

	t.Run("UpdateJobStatus to Failed with Error Message", func(t *testing.T) {
		jobID := "job-103"
		job := &domain.PDFParseJob{
			ID:        jobID,
			UserID:    userID,
			Status:    domain.PDFParseJobStatusPending,
			Filename:  "damaged.pdf",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := repo.CreateJob(ctx, job); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		errMsg := "LLM token rate limit exceeded"
		if err := repo.UpdateJobStatus(ctx, jobID, domain.PDFParseJobStatusFailed, nil, errMsg); err != nil {
			t.Fatalf("update to failed failed: %v", err)
		}

		failed, err := repo.GetJob(ctx, userID, jobID)
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if failed.Status != domain.PDFParseJobStatusFailed {
			t.Errorf("expected status 'failed', got %q", failed.Status)
		}
		if failed.ErrorMessage != errMsg {
			t.Errorf("expected error message %q, got %q", errMsg, failed.ErrorMessage)
		}
		if failed.CompletedAt == nil {
			t.Errorf("expected non-nil CompletedAt on failure")
		}
	})

	t.Run("FailInterruptedJobs marks in-flight jobs as failed on reboot", func(t *testing.T) {
		pendingJob := &domain.PDFParseJob{
			ID:        "job-interrupted-1",
			UserID:    userID,
			Status:    domain.PDFParseJobStatusPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		procJob := &domain.PDFParseJob{
			ID:        "job-interrupted-2",
			UserID:    userID,
			Status:    domain.PDFParseJobStatusProcessing,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := repo.CreateJob(ctx, pendingJob); err != nil {
			t.Fatalf("create pending failed: %v", err)
		}
		if err := repo.CreateJob(ctx, procJob); err != nil {
			t.Fatalf("create proc failed: %v", err)
		}

		reason := "server restarted while parsing PDF"
		if err := repo.FailInterruptedJobs(ctx, reason); err != nil {
			t.Fatalf("FailInterruptedJobs failed: %v", err)
		}

		j1, _ := repo.GetJob(ctx, userID, "job-interrupted-1")
		if j1.Status != domain.PDFParseJobStatusFailed || j1.ErrorMessage != reason {
			t.Errorf("expected j1 to be failed with %q, got status %q msg %q", reason, j1.Status, j1.ErrorMessage)
		}

		j2, _ := repo.GetJob(ctx, userID, "job-interrupted-2")
		if j2.Status != domain.PDFParseJobStatusFailed || j2.ErrorMessage != reason {
			t.Errorf("expected j2 to be failed with %q, got status %q msg %q", reason, j2.Status, j2.ErrorMessage)
		}
	})
}
