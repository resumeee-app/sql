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

// branchRecord is the GORM persistence model for core.Branch.
type branchRecord struct {
	ID                     string          `gorm:"primaryKey;type:text"`
	UserID                 string          `gorm:"index;not null"`
	Name                   string          `gorm:"not null"`
	IsDefault              bool            `gorm:"default:false;index"`
	TargetRole             string          `gorm:"column:target_role"`
	TargetCompany          string          `gorm:"column:target_company"`
	TargetRequirements     string          `gorm:"column:target_requirements"`
	CustomDirectives       string          `gorm:"column:custom_directives"`
	BranchInstructions     string          `gorm:"column:branch_instructions"`
	TailoringStrategyID    *string         `gorm:"index"`
	PresetID               *string         `gorm:"column:preset_id;index"`
	TemplateSetID          string          `gorm:"index;not null"`
	ProviderOverride       string          `gorm:"column:provider_override"`
	ModelOverride          string          `gorm:"column:model_override"`
	ReasoningLevel         string          `gorm:"column:reasoning_level"`
	PinnedEntityIDs        []string        `gorm:"serializer:json"`
	ExcludedEntityIDs      []string        `gorm:"serializer:json"`
	ActiveVersionID        *string         `gorm:"index"`
	GapReport              *core.GapReport `gorm:"serializer:json"`
	ATSScore               *core.ATSScore  `gorm:"serializer:json"`
	LastSyncedMainRevision int64           `gorm:"column:last_synced_main_revision"`
	IsStale                bool            `gorm:"-"`
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// TableName overrides the GORM table name to target_resumes.
func (branchRecord) TableName() string {
	return "target_resumes"
}

// Normalize synchronizes aliases prior to persistence.
func (r *branchRecord) Normalize() error {
	b := toCoreBranch(r)
	if err := b.Normalize(); err != nil {
		return err
	}
	r.TailoringStrategyID = b.TailoringStrategyID
	r.PresetID = b.PresetID
	r.CustomDirectives = b.CustomDirectives
	r.BranchInstructions = b.BranchInstructions
	return nil
}

func toBranchRecord(b *core.Branch) *branchRecord {
	if b == nil {
		return nil
	}
	return &branchRecord{
		ID:                     b.ID,
		UserID:                 b.UserID,
		Name:                   b.Name,
		IsDefault:              b.IsDefault,
		TargetRole:             b.TargetRole,
		TargetCompany:          b.TargetCompany,
		TargetRequirements:     b.TargetRequirements,
		CustomDirectives:       b.CustomDirectives,
		BranchInstructions:     b.BranchInstructions,
		TailoringStrategyID:    b.TailoringStrategyID,
		PresetID:               b.PresetID,
		TemplateSetID:          b.TemplateSetID,
		ProviderOverride:       b.ProviderOverride,
		ModelOverride:          b.ModelOverride,
		ReasoningLevel:         b.ReasoningLevel,
		PinnedEntityIDs:        b.PinnedEntityIDs,
		ExcludedEntityIDs:      b.ExcludedEntityIDs,
		ActiveVersionID:        b.ActiveVersionID,
		GapReport:              b.GapReport,
		ATSScore:               b.ATSScore,
		LastSyncedMainRevision: b.LastSyncedMainRevision,
		IsStale:                b.IsStale,
		CreatedAt:              b.CreatedAt,
		UpdatedAt:              b.UpdatedAt,
	}
}

func toCoreBranch(r *branchRecord) *core.Branch {
	if r == nil {
		return nil
	}
	return &core.Branch{
		ID:                     r.ID,
		UserID:                 r.UserID,
		Name:                   r.Name,
		IsDefault:              r.IsDefault,
		TargetRole:             r.TargetRole,
		TargetCompany:          r.TargetCompany,
		TargetRequirements:     r.TargetRequirements,
		CustomDirectives:       r.CustomDirectives,
		BranchInstructions:     r.BranchInstructions,
		TailoringStrategyID:    r.TailoringStrategyID,
		PresetID:               r.PresetID,
		TemplateSetID:          r.TemplateSetID,
		ProviderOverride:       r.ProviderOverride,
		ModelOverride:          r.ModelOverride,
		ReasoningLevel:         r.ReasoningLevel,
		PinnedEntityIDs:        r.PinnedEntityIDs,
		ExcludedEntityIDs:      r.ExcludedEntityIDs,
		ActiveVersionID:        r.ActiveVersionID,
		GapReport:              r.GapReport,
		ATSScore:               r.ATSScore,
		LastSyncedMainRevision: r.LastSyncedMainRevision,
		IsStale:                r.IsStale,
		CreatedAt:              r.CreatedAt,
		UpdatedAt:              r.UpdatedAt,
	}
}

// branchVersionRecord is the GORM persistence model for core.BranchVersion.
type branchVersionRecord struct {
	ID                 string                `gorm:"primaryKey;type:text"`
	UserID             string                `gorm:"index;not null"`
	ResumeID           string                `gorm:"uniqueIndex:idx_resume_version,priority:1;column:resume_id;not null"`
	BranchID           string                `gorm:"column:branch_id;index"`
	VersionNumber      int                   `gorm:"uniqueIndex:idx_resume_version,priority:2;column:version_number;not null"`
	Status             core.VersionStatus    `gorm:"index;not null"`
	MainRevisionUsed   int64                 `gorm:"column:main_revision_used"`
	TemplateSetIDUsed  string                `gorm:"column:template_set_id_used"`
	StrategyIDUsed     *string               `gorm:"column:strategy_id_used"`
	ProviderUsed       string                `gorm:"column:provider_used"`
	ModelUsed          string                `gorm:"column:model_used"`
	SourceType         core.SourceType       `gorm:"column:source_type"`
	ParentVersionID    *string               `gorm:"index"`
	TailoredContent    *core.TailoredContent `gorm:"serializer:json"`
	GapReport          *core.GapReport       `gorm:"serializer:json"`
	ATSScore           *core.ATSScore        `gorm:"serializer:json"`
	ValidationWarnings []string              `gorm:"serializer:json"`
	ErrorMessage       string                `gorm:"column:error_message"`
	HTMLOutput         string                `gorm:"column:html_output"`
	MarkdownOutput     string                `gorm:"column:markdown_output"`
	PlainTextOutput    string                `gorm:"column:plain_text_output"`
	CreatedAt          time.Time
	CompletedAt        *time.Time
}

// TableName overrides the GORM table name to resume_versions.
func (branchVersionRecord) TableName() string {
	return "resume_versions"
}

// Normalize synchronizes resume and branch aliases prior to persistence.
func (r *branchVersionRecord) Normalize() error {
	v := toCoreBranchVersion(r)
	if err := v.Normalize(); err != nil {
		return err
	}
	r.ResumeID = v.ResumeID
	r.BranchID = v.BranchID
	return nil
}

func toBranchVersionRecord(v *core.BranchVersion) *branchVersionRecord {
	if v == nil {
		return nil
	}
	return &branchVersionRecord{
		ID:                 v.ID,
		UserID:             v.UserID,
		ResumeID:           v.ResumeID,
		BranchID:           v.BranchID,
		VersionNumber:      v.VersionNumber,
		Status:             v.Status,
		MainRevisionUsed:   v.MainRevisionUsed,
		TemplateSetIDUsed:  v.TemplateSetIDUsed,
		StrategyIDUsed:     v.StrategyIDUsed,
		ProviderUsed:       v.ProviderUsed,
		ModelUsed:          v.ModelUsed,
		SourceType:         v.SourceType,
		ParentVersionID:    v.ParentVersionID,
		TailoredContent:    v.TailoredContent,
		GapReport:          v.GapReport,
		ATSScore:           v.ATSScore,
		ValidationWarnings: v.ValidationWarnings,
		ErrorMessage:       v.ErrorMessage,
		HTMLOutput:         v.HTMLOutput,
		MarkdownOutput:     v.MarkdownOutput,
		PlainTextOutput:    v.PlainTextOutput,
		CreatedAt:          v.CreatedAt,
		CompletedAt:        v.CompletedAt,
	}
}

func toCoreBranchVersion(r *branchVersionRecord) *core.BranchVersion {
	if r == nil {
		return nil
	}
	v := &core.BranchVersion{
		ID:                 r.ID,
		UserID:             r.UserID,
		ResumeID:           r.ResumeID,
		BranchID:           r.BranchID,
		VersionNumber:      r.VersionNumber,
		Status:             r.Status,
		MainRevisionUsed:   r.MainRevisionUsed,
		TemplateSetIDUsed:  r.TemplateSetIDUsed,
		StrategyIDUsed:     r.StrategyIDUsed,
		ProviderUsed:       r.ProviderUsed,
		ModelUsed:          r.ModelUsed,
		SourceType:         r.SourceType,
		ParentVersionID:    r.ParentVersionID,
		TailoredContent:    r.TailoredContent,
		GapReport:          r.GapReport,
		ATSScore:           r.ATSScore,
		ValidationWarnings: r.ValidationWarnings,
		ErrorMessage:       r.ErrorMessage,
		HTMLOutput:         r.HTMLOutput,
		MarkdownOutput:     r.MarkdownOutput,
		PlainTextOutput:    r.PlainTextOutput,
		CreatedAt:          r.CreatedAt,
		CompletedAt:        r.CompletedAt,
	}
	_ = v.Normalize()
	return v
}

// BranchStore implements core.BranchStore backed by GORM.
type BranchStore struct {
	db *gorm.DB
}

var _ core.BranchStore = (*BranchStore)(nil)

// NewBranchStore constructs a SQL-backed BranchStore.
func NewBranchStore(db *gorm.DB) *BranchStore {
	return &BranchStore{db: db}
}

// ListBranches retrieves all branches for a user and calculates staleness against the current main revision.
func (s *BranchStore) ListBranches(ctx context.Context, userID string) ([]core.Branch, error) {
	var records []branchRecord
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("is_default DESC, created_at DESC").Find(&records).Error; err != nil {
		return nil, mapError(err)
	}

	var currentMainRev int64
	if err := s.db.WithContext(ctx).
		Model(&profileRecord{}).
		Where("user_id = ?", userID).
		Select("main_revision").
		Scan(&currentMainRev).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, mapError(err)
	}

	branches := make([]core.Branch, len(records))
	for i := range records {
		b := *toCoreBranch(&records[i])
		b.IsStale = b.IsStaleRevision(currentMainRev)
		branches[i] = b
	}

	return branches, nil
}

// Branch retrieves a specific branch record and calculates its staleness.
func (s *BranchStore) Branch(ctx context.Context, userID, id string) (*core.Branch, error) {
	var rec branchRecord
	query := s.db.WithContext(ctx).Where("id = ?", id)
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&rec).Error; err != nil {
		return nil, mapError(err)
	}

	targetUserID := userID
	if targetUserID == "" {
		targetUserID = rec.UserID
	}

	var currentMainRev int64
	if err := s.db.WithContext(ctx).
		Model(&profileRecord{}).
		Where("user_id = ?", targetUserID).
		Select("main_revision").
		Scan(&currentMainRev).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, mapError(err)
	}
	branch := toCoreBranch(&rec)
	branch.IsStale = branch.IsStaleRevision(currentMainRev)

	return branch, nil
}

// CreateBranch inserts a new resume branch record.
func (s *BranchStore) CreateBranch(ctx context.Context, branch *core.Branch) error {
	if branch == nil {
		return core.ErrValidation
	}
	if branch.CreatedAt.IsZero() {
		branch.CreatedAt = time.Now().UTC()
	}
	branch.UpdatedAt = time.Now().UTC()
	return mapError(s.db.WithContext(ctx).Create(toBranchRecord(branch)).Error)
}

// UpdateBranch updates mutable branch fields and refreshes the updated timestamp.
func (s *BranchStore) UpdateBranch(ctx context.Context, branch *core.Branch) error {
	if branch == nil {
		return core.ErrValidation
	}
	branch.UpdatedAt = time.Now().UTC()
	return mapError(s.db.WithContext(ctx).Save(toBranchRecord(branch)).Error)
}

// DeleteBranch removes a non-default branch and cascades deletion to all associated versions.
func (s *BranchStore) DeleteBranch(ctx context.Context, userID, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var branch branchRecord
		if err := tx.Where("user_id = ? AND id = ?", userID, id).First(&branch).Error; err != nil {
			return mapError(err)
		}
		if branch.IsDefault {
			return fmt.Errorf("%w: cannot delete default branch", core.ErrDefaultBranchImmutable)
		}

		// Clean up branch versions
		if err := tx.Where("resume_id = ? OR branch_id = ?", id, id).Delete(&branchVersionRecord{}).Error; err != nil {
			return err
		}
		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&branchRecord{})
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
func (s *BranchStore) CreateVersion(ctx context.Context, version *core.BranchVersion) error {
	if version == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_ = version.Normalize()
		targetID := version.ResumeID
		if version.UserID == "" && targetID != "" {
			var b branchRecord
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

		return tx.Create(toBranchVersionRecord(version)).Error
	})
}

// Version retrieves a branch version by version UUID, optionally restricted to a specific branch.
func (s *BranchStore) Version(ctx context.Context, branchID, versionID string) (*core.BranchVersion, error) {
	var rec branchVersionRecord
	query := s.db.WithContext(ctx).Where("id = ?", versionID)
	if branchID != "" {
		query = query.Where("resume_id = ? OR branch_id = ?", branchID, branchID)
	}
	if err := query.First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreBranchVersion(&rec), nil
}

// VersionByNumber retrieves a branch version by its sequential number within the branch.
func (s *BranchStore) VersionByNumber(ctx context.Context, branchID string, versionNumber int) (*core.BranchVersion, error) {
	var rec branchVersionRecord
	err := s.db.WithContext(ctx).
		Where("(resume_id = ? OR branch_id = ?) AND version_number = ?", branchID, branchID, versionNumber).
		First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreBranchVersion(&rec), nil
}

// VersionsByIDs retrieves multiple branch versions by their IDs in a single batch query.
func (s *BranchStore) VersionsByIDs(ctx context.Context, versionIDs []string) ([]core.BranchVersion, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}
	var records []branchVersionRecord
	if err := s.db.WithContext(ctx).
		Where("id IN ?", versionIDs).
		Find(&records).Error; err != nil {
		return nil, mapError(err)
	}
	versions := make([]core.BranchVersion, len(records))
	for i := range records {
		versions[i] = *toCoreBranchVersion(&records[i])
	}
	return versions, nil
}

// ListVersions returns all versions belonging to a branch ordered by version number ascending.
func (s *BranchStore) ListVersions(ctx context.Context, branchID string) ([]core.BranchVersion, error) {
	var records []branchVersionRecord
	if err := s.db.WithContext(ctx).
		Where("resume_id = ? OR branch_id = ?", branchID, branchID).
		Order("version_number ASC").
		Find(&records).Error; err != nil {
		return nil, mapError(err)
	}
	versions := make([]core.BranchVersion, len(records))
	for i := range records {
		versions[i] = *toCoreBranchVersion(&records[i])
	}
	return versions, nil
}

// UpdateVersionStatus updates status and error diagnostic message for a version.
func (s *BranchStore) UpdateVersionStatus(ctx context.Context, versionID string, status core.VersionStatus, errMsg string) error {
	updates := map[string]any{
		"status":        status,
		"error_message": errMsg,
	}
	if status == core.VersionStatusCompleted || status == core.VersionStatusCompletedWithWarnings || status == core.VersionStatusFailed {
		now := time.Now().UTC()
		updates["completed_at"] = &now
	}
	res := s.db.WithContext(ctx).Model(&branchVersionRecord{}).Where("id = ?", versionID).Updates(updates)
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}

// CompleteVersion saves completed outputs and advances the parent branch active_version_id and last_synced_main_revision.
func (s *BranchStore) CompleteVersion(ctx context.Context, version *core.BranchVersion) error {
	if version == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_ = version.Normalize()
		parentID := version.ResumeID

		now := time.Now().UTC()
		version.CompletedAt = &now
		if version.Status == "" {
			version.Status = core.VersionStatusCompleted
		}
		if err := tx.Save(toBranchVersionRecord(version)).Error; err != nil {
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

		return tx.Model(&branchRecord{}).
			Where("id = ?", parentID).
			Updates(updates).Error
	})
}

// UpdateVersionATSScore persists an ATS score evaluation on a branch version and propagates it to the parent branch if that version is currently active.
func (s *BranchStore) UpdateVersionATSScore(ctx context.Context, versionID string, atsScore *core.ATSScore) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version branchVersionRecord
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
			res := tx.Model(&branchVersionRecord{}).
				Where("id = ? AND ats_score IS NULL", versionID).
				Update("ats_score", serialized)
			if res.Error != nil {
				return mapError(res.Error)
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("%w: ATS score already exists for version %s", core.ErrConflict, versionID)
			}
		} else {
			if err := tx.Model(&branchVersionRecord{}).Where("id = ?", versionID).Update("ats_score", nil).Error; err != nil {
				return mapError(err)
			}
		}

		parentID := version.ResumeID
		if parentID == "" {
			parentID = version.BranchID
		}
		if parentID != "" {
			if err := tx.Model(&branchRecord{}).
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
func (s *BranchStore) FailInterruptedVersions(ctx context.Context, reason string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).
		Model(&branchVersionRecord{}).
		Where("status IN ?", []core.VersionStatus{core.VersionStatusPending, core.VersionStatusProcessing}).
		Updates(map[string]any{
			"status":        core.VersionStatusFailed,
			"error_message": reason,
			"completed_at":  now,
		}).Error
}
