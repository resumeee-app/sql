package sqlite

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"resumeee/internal/domain"
)

type branchRepository struct {
	db *gorm.DB
}

// NewBranchRepository constructs a SQLite-backed BranchRepository.
func NewBranchRepository(db *gorm.DB) domain.BranchRepository {
	return &branchRepository{db: db}
}

// ListBranches retrieves all branches for a user and calculates staleness against the current master revision.
func (r *branchRepository) ListBranches(ctx context.Context, userID string) ([]domain.Branch, error) {
	var branches []domain.Branch
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("is_default DESC, created_at DESC").Find(&branches).Error; err != nil {
		return nil, mapError(err)
	}

	var currentMasterRev int64
	_ = r.db.WithContext(ctx).
		Model(&domain.Profile{}).
		Where("user_id = ?", userID).
		Select("master_revision").
		Scan(&currentMasterRev).Error

	for i := range branches {
		branches[i].IsStale = branches[i].IsStaleRevision(currentMasterRev)
	}

	return branches, nil
}

// GetBranch retrieves a specific branch record and calculates its staleness.
func (r *branchRepository) GetBranch(ctx context.Context, userID, id string) (*domain.Branch, error) {
	var branch domain.Branch
	query := r.db.WithContext(ctx).Where("id = ?", id)
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&branch).Error; err != nil {
		return nil, mapError(err)
	}

	targetUserID := userID
	if targetUserID == "" {
		targetUserID = branch.UserID
	}

	var currentMasterRev int64
	_ = r.db.WithContext(ctx).
		Model(&domain.Profile{}).
		Where("user_id = ?", targetUserID).
		Select("master_revision").
		Scan(&currentMasterRev).Error
	branch.IsStale = branch.IsStaleRevision(currentMasterRev)

	return &branch, nil
}

// CreateBranch inserts a new resume branch record.
func (r *branchRepository) CreateBranch(ctx context.Context, branch *domain.Branch) error {
	if branch.CreatedAt.IsZero() {
		branch.CreatedAt = time.Now().UTC()
	}
	branch.UpdatedAt = time.Now().UTC()
	return mapError(r.db.WithContext(ctx).Create(branch).Error)
}

// UpdateBranch updates mutable branch fields and refreshes the updated timestamp.
func (r *branchRepository) UpdateBranch(ctx context.Context, branch *domain.Branch) error {
	branch.UpdatedAt = time.Now().UTC()
	return mapError(r.db.WithContext(ctx).Save(branch).Error)
}

// DeleteBranch removes a non-default branch and cascades deletion to all associated versions.
func (r *branchRepository) DeleteBranch(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var branch domain.Branch
		if err := tx.Where("user_id = ? AND id = ?", userID, id).First(&branch).Error; err != nil {
			return mapError(err)
		}
		if branch.IsDefault {
			return fmt.Errorf("%w: cannot delete default branch", domain.ErrDefaultBranchImmutable)
		}

		// Clean up branch versions
		if err := tx.Where("branch_id = ?", id).Delete(&domain.BranchVersion{}).Error; err != nil {
			return err
		}
		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&domain.Branch{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// CreateVersion atomically assigns a sequential version number and creates a new branch version.
func (r *branchRepository) CreateVersion(ctx context.Context, version *domain.BranchVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var maxVer int
		row := tx.Raw("SELECT COALESCE(MAX(version_number), 0) FROM branch_versions WHERE branch_id = ?", version.BranchID).Row()
		if err := row.Scan(&maxVer); err != nil {
			return err
		}

		version.VersionNumber = maxVer + 1
		if version.Status == "" {
			version.Status = domain.VersionStatusPending
		}
		if version.CreatedAt.IsZero() {
			version.CreatedAt = time.Now().UTC()
		}

		return tx.Create(version).Error
	})
}

// GetVersion retrieves a branch version by version UUID, optionally restricted to a specific branch.
func (r *branchRepository) GetVersion(ctx context.Context, branchID, versionID string) (*domain.BranchVersion, error) {
	var version domain.BranchVersion
	query := r.db.WithContext(ctx).Where("id = ?", versionID)
	if branchID != "" {
		query = query.Where("branch_id = ?", branchID)
	}
	err := query.First(&version).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &version, nil
}

// GetVersionByNumber retrieves a branch version by its sequential number within the branch.
func (r *branchRepository) GetVersionByNumber(ctx context.Context, branchID string, versionNumber int) (*domain.BranchVersion, error) {
	var version domain.BranchVersion
	err := r.db.WithContext(ctx).
		Where("branch_id = ? AND version_number = ?", branchID, versionNumber).
		First(&version).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &version, nil
}

// ListVersions returns all versions belonging to a branch ordered by version number ascending.
func (r *branchRepository) ListVersions(ctx context.Context, branchID string) ([]domain.BranchVersion, error) {
	var versions []domain.BranchVersion
	err := r.db.WithContext(ctx).
		Where("branch_id = ?", branchID).
		Order("version_number ASC").
		Find(&versions).Error
	return versions, mapError(err)
}

// UpdateVersionStatus updates status and error diagnostic message for a version.
func (r *branchRepository) UpdateVersionStatus(ctx context.Context, versionID string, status domain.VersionStatus, errMsg string) error {
	updates := map[string]any{
		"status":        status,
		"error_message": errMsg,
	}
	if status == domain.VersionStatusCompleted || status == domain.VersionStatusCompletedWithWarnings || status == domain.VersionStatusFailed {
		now := time.Now().UTC()
		updates["completed_at"] = &now
	}
	res := r.db.WithContext(ctx).Model(&domain.BranchVersion{}).Where("id = ?", versionID).Updates(updates)
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CompleteVersion saves completed outputs and advances the parent branch active_version_id and last_synced_master_revision.
func (r *branchRepository) CompleteVersion(ctx context.Context, version *domain.BranchVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		version.CompletedAt = &now
		if version.Status == "" {
			version.Status = domain.VersionStatusCompleted
		}
		if err := tx.Save(version).Error; err != nil {
			return err
		}

		// Update active version pointer and last synced revision on parent branch
		return tx.Model(&domain.Branch{}).
			Where("id = ?", version.BranchID).
			Updates(map[string]any{
				"active_version_id":            version.ID,
				"last_synced_master_revision":  version.MasterRevisionUsed,
				"updated_at":                   now,
			}).Error
	})
}

// FailInterruptedVersions transitions in-flight pending and processing versions to failed status on recovery.
func (r *branchRepository) FailInterruptedVersions(ctx context.Context, reason string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&domain.BranchVersion{}).
		Where("status IN ?", []domain.VersionStatus{domain.VersionStatusPending, domain.VersionStatusProcessing}).
		Updates(map[string]any{
			"status":        domain.VersionStatusFailed,
			"error_message": reason,
			"completed_at":  now,
		}).Error
}
