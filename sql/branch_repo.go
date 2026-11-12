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

type branchRepository struct {
	db *gorm.DB
}

// NewBranchRepository constructs a SQLite-backed BranchRepository.
func NewBranchRepository(db *gorm.DB) core.BranchRepository {
	return &branchRepository{db: db}
}

// ListBranches retrieves all branches for a user and calculates staleness against the current main revision.
func (r *branchRepository) ListBranches(ctx context.Context, userID string) ([]core.Branch, error) {
	var branches []core.Branch
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("is_default DESC, created_at DESC").Find(&branches).Error; err != nil {
		return nil, mapError(err)
	}

	var currentMainRev int64
	if err := r.db.WithContext(ctx).
		Model(&core.Profile{}).
		Where("user_id = ?", userID).
		Select("main_revision").
		Scan(&currentMainRev).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, mapError(err)
	}

	for i := range branches {
		branches[i].IsStale = branches[i].IsStaleRevision(currentMainRev)
	}

	return branches, nil
}

// GetBranch retrieves a specific branch record and calculates its staleness.
func (r *branchRepository) GetBranch(ctx context.Context, userID, id string) (*core.Branch, error) {
	var branch core.Branch
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

	var currentMainRev int64
	if err := r.db.WithContext(ctx).
		Model(&core.Profile{}).
		Where("user_id = ?", targetUserID).
		Select("main_revision").
		Scan(&currentMainRev).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, mapError(err)
	}
	branch.IsStale = branch.IsStaleRevision(currentMainRev)

	return &branch, nil
}

// CreateBranch inserts a new resume branch record.
func (r *branchRepository) CreateBranch(ctx context.Context, branch *core.Branch) error {
	if branch.CreatedAt.IsZero() {
		branch.CreatedAt = time.Now().UTC()
	}
	branch.UpdatedAt = time.Now().UTC()
	return mapError(r.db.WithContext(ctx).Create(branch).Error)
}

// UpdateBranch updates mutable branch fields and refreshes the updated timestamp.
func (r *branchRepository) UpdateBranch(ctx context.Context, branch *core.Branch) error {
	branch.UpdatedAt = time.Now().UTC()
	return mapError(r.db.WithContext(ctx).Save(branch).Error)
}

// DeleteBranch removes a non-default branch and cascades deletion to all associated versions.
func (r *branchRepository) DeleteBranch(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var branch core.Branch
		if err := tx.Where("user_id = ? AND id = ?", userID, id).First(&branch).Error; err != nil {
			return mapError(err)
		}
		if branch.IsDefault {
			return fmt.Errorf("%w: cannot delete default branch", core.ErrDefaultBranchImmutable)
		}

		// Clean up branch versions
		if err := tx.Where("resume_id = ? OR branch_id = ?", id, id).Delete(&core.BranchVersion{}).Error; err != nil {
			return err
		}
		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&core.Branch{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return core.ErrNotFound
		}
		return nil
	})
}

// CreateVersion atomically assigns a sequential version number and creates a new branch version.
func (r *branchRepository) CreateVersion(ctx context.Context, version *core.BranchVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		targetID := version.ResumeID
		if targetID == "" {
			targetID = version.BranchID
		}
		if version.ResumeID == "" {
			version.ResumeID = targetID
		}
		if version.BranchID == "" {
			version.BranchID = targetID
		}
		if version.UserID == "" && targetID != "" {
			var b core.Branch
			if err := tx.Where("id = ?", targetID).Select("user_id").First(&b).Error; err == nil {
				version.UserID = b.UserID
			}
		}

		var maxVer int
		row := tx.Raw("SELECT COALESCE(MAX(version_number), 0) FROM resume_versions WHERE resume_id = ? OR branch_id = ?", targetID, targetID).Row()
		if err := row.Scan(&maxVer); err != nil {
			return err
		}

		version.VersionNumber = maxVer + 1
		if version.Status == "" {
			version.Status = core.VersionStatusPending
		}
		if version.CreatedAt.IsZero() {
			version.CreatedAt = time.Now().UTC()
		}

		return tx.Create(version).Error
	})
}

func syncVersionIDs(v *core.BranchVersion) {
	if v == nil {
		return
	}
	if v.ResumeID == "" && v.BranchID != "" {
		v.ResumeID = v.BranchID
	}
	if v.BranchID == "" && v.ResumeID != "" {
		v.BranchID = v.ResumeID
	}
}

// GetVersion retrieves a branch version by version UUID, optionally restricted to a specific branch.
func (r *branchRepository) GetVersion(ctx context.Context, branchID, versionID string) (*core.BranchVersion, error) {
	var version core.BranchVersion
	query := r.db.WithContext(ctx).Where("id = ?", versionID)
	if branchID != "" {
		query = query.Where("resume_id = ? OR branch_id = ?", branchID, branchID)
	}
	err := query.First(&version).Error
	if err != nil {
		return nil, mapError(err)
	}
	syncVersionIDs(&version)
	return &version, nil
}

// GetVersionByNumber retrieves a branch version by its sequential number within the branch.
func (r *branchRepository) GetVersionByNumber(ctx context.Context, branchID string, versionNumber int) (*core.BranchVersion, error) {
	var version core.BranchVersion
	err := r.db.WithContext(ctx).
		Where("(resume_id = ? OR branch_id = ?) AND version_number = ?", branchID, branchID, versionNumber).
		First(&version).Error
	if err != nil {
		return nil, mapError(err)
	}
	syncVersionIDs(&version)
	return &version, nil
}

// GetVersionsByIDs retrieves multiple branch versions by their IDs in a single batch query.
func (r *branchRepository) GetVersionsByIDs(ctx context.Context, versionIDs []string) ([]core.BranchVersion, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}
	var versions []core.BranchVersion
	err := r.db.WithContext(ctx).
		Where("id IN ?", versionIDs).
		Find(&versions).Error
	if err != nil {
		return nil, mapError(err)
	}
	for i := range versions {
		syncVersionIDs(&versions[i])
	}
	return versions, nil
}

// ListVersions returns all versions belonging to a branch ordered by version number ascending.
func (r *branchRepository) ListVersions(ctx context.Context, branchID string) ([]core.BranchVersion, error) {
	var versions []core.BranchVersion
	err := r.db.WithContext(ctx).
		Where("resume_id = ? OR branch_id = ?", branchID, branchID).
		Order("version_number ASC").
		Find(&versions).Error
	if err == nil {
		for i := range versions {
			syncVersionIDs(&versions[i])
		}
	}
	return versions, mapError(err)
}

// UpdateVersionStatus updates status and error diagnostic message for a version.
func (r *branchRepository) UpdateVersionStatus(ctx context.Context, versionID string, status core.VersionStatus, errMsg string) error {
	updates := map[string]any{
		"status":        status,
		"error_message": errMsg,
	}
	if status == core.VersionStatusCompleted || status == core.VersionStatusCompletedWithWarnings || status == core.VersionStatusFailed {
		now := time.Now().UTC()
		updates["completed_at"] = &now
	}
	res := r.db.WithContext(ctx).Model(&core.BranchVersion{}).Where("id = ?", versionID).Updates(updates)
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}

// CompleteVersion saves completed outputs and advances the parent branch active_version_id and last_synced_main_revision.
func (r *branchRepository) CompleteVersion(ctx context.Context, version *core.BranchVersion) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		parentID := version.ResumeID
		if parentID == "" {
			parentID = version.BranchID
		}
		if version.ResumeID == "" {
			version.ResumeID = parentID
		}
		if version.BranchID == "" {
			version.BranchID = parentID
		}

		now := time.Now().UTC()
		version.CompletedAt = &now
		if version.Status == "" {
			version.Status = core.VersionStatusCompleted
		}
		if err := tx.Save(version).Error; err != nil {
			return err
		}

		// Update active version pointer, last synced revision, and gap report on parent branch
		updates := map[string]any{
			"active_version_id":         version.ID,
			"last_synced_main_revision": version.MainRevisionUsed,
			"updated_at":                now,
		}
		if version.GapReport != nil {
			raw, err := json.Marshal(version.GapReport)
			if err != nil {
				return fmt.Errorf("failed to marshal gap report: %w", err)
			}
			updates["gap_report"] = string(raw)
		} else {
			updates["gap_report"] = nil
		}

		if version.ATSScore != nil {
			rawATS, err := json.Marshal(version.ATSScore)
			if err != nil {
				return fmt.Errorf("failed to marshal ats score: %w", err)
			}
			updates["ats_score"] = string(rawATS)
		} else {
			updates["ats_score"] = nil
		}

		return tx.Model(&core.Branch{}).
			Where("id = ?", parentID).
			Updates(updates).Error
	})
}

// UpdateVersionATSScore persists an ATS score evaluation on a branch version and propagates it to the parent branch if that version is currently active.
func (r *branchRepository) UpdateVersionATSScore(ctx context.Context, versionID string, atsScore *core.ATSScore) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version core.BranchVersion
		if err := tx.Where("id = ?", versionID).First(&version).Error; err != nil {
			return mapError(err)
		}
		if atsScore != nil && version.ATSScore != nil {
			return fmt.Errorf("%w: ATS score already exists for version %s", core.ErrConflict, versionID)
		}

		var serialized any
		if atsScore != nil {
			atsScore.Normalize()
			raw, err := json.Marshal(atsScore)
			if err != nil {
				return fmt.Errorf("failed to marshal ats score: %w", err)
			}
			serialized = string(raw)
			res := tx.Model(&core.BranchVersion{}).
				Where("id = ? AND ats_score IS NULL", versionID).
				Update("ats_score", serialized)
			if res.Error != nil {
				return mapError(res.Error)
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("%w: ATS score already exists for version %s", core.ErrConflict, versionID)
			}
		} else {
			if err := tx.Model(&core.BranchVersion{}).Where("id = ?", versionID).Update("ats_score", nil).Error; err != nil {
				return mapError(err)
			}
		}

		parentID := version.ResumeID
		if parentID == "" {
			parentID = version.BranchID
		}
		if parentID != "" {
			if err := tx.Model(&core.Branch{}).
				Where("id = ? AND active_version_id = ?", parentID, versionID).
				Updates(map[string]any{
					"ats_score":  serialized,
					"updated_at": time.Now().UTC(),
				}).Error; err != nil {
				return mapError(err)
			}
		}

		return nil
	})
}

// FailInterruptedVersions transitions in-flight pending and processing versions to failed status on recovery.
func (r *branchRepository) FailInterruptedVersions(ctx context.Context, reason string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&core.BranchVersion{}).
		Where("status IN ?", []core.VersionStatus{core.VersionStatusPending, core.VersionStatusProcessing}).
		Updates(map[string]any{
			"status":        core.VersionStatusFailed,
			"error_message": reason,
			"completed_at":  now,
		}).Error
}
