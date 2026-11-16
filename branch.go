package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/core"
)

// branchRecord maps core.Branch to the relational target_resumes table.
type branchRecord struct {
	ID                     string          `gorm:"primaryKey;type:text"`
	UserID                 string          `gorm:"index;not null"`
	Name                   string          `gorm:"not null"`
	IsDefault              bool            `gorm:"column:is_default;index;not null;default:false"`
	TargetRole             string          `gorm:"column:target_role"`
	TargetCompany          string          `gorm:"column:target_company"`
	TargetRequirements     string          `gorm:"column:target_requirements"`
	BranchInstructions     string          `gorm:"column:branch_instructions"`
	PresetID               *string         `gorm:"column:preset_id;index"`
	TemplateSetID          string          `gorm:"column:template_set_id;index;not null"`
	ProviderOverride       string          `gorm:"column:provider_override"`
	ModelOverride          string          `gorm:"column:model_override"`
	ReasoningLevel         string          `gorm:"column:reasoning_level"`
	PinnedEntityIDs        []string        `gorm:"serializer:json"`
	ExcludedEntityIDs      []string        `gorm:"serializer:json"`
	ActiveVersionID        *string         `gorm:"column:active_version_id;index"`
	GapReport              *core.GapReport `gorm:"serializer:json"`
	ATSScore               *core.ATSScore  `gorm:"serializer:json"`
	Epoch                  int64           `gorm:"column:epoch;not null;default:1"`
	LastSyncedMainRevision int64           `gorm:"column:last_synced_main_revision;not null;default:0"`
	CreatedAt              time.Time       `gorm:"autoCreateTime:false"`
	UpdatedAt              time.Time       `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for branchRecord.
func (branchRecord) TableName() string {
	return "target_resumes"
}

// toBranchRecord converts a domain Branch into its relational record representation.
func toBranchRecord(b *core.Branch) *branchRecord {
	var presetID, activeVer *string
	if b.PresetID != nil {
		cp := *b.PresetID
		presetID = &cp
	}
	if b.ActiveVersionID != nil {
		cp := *b.ActiveVersionID
		activeVer = &cp
	}
	var pinned, excluded []string
	if b.PinnedEntityIDs != nil {
		pinned = append([]string{}, b.PinnedEntityIDs...)
	}
	if b.ExcludedEntityIDs != nil {
		excluded = append([]string{}, b.ExcludedEntityIDs...)
	}
	return &branchRecord{
		ID:                     b.ID,
		UserID:                 b.UserID,
		Name:                   b.Name,
		IsDefault:              b.IsDefault,
		TargetRole:             b.TargetRole,
		TargetCompany:          b.TargetCompany,
		TargetRequirements:     b.TargetRequirements,
		BranchInstructions:     b.BranchInstructions,
		PresetID:               presetID,
		TemplateSetID:          b.TemplateSetID,
		ProviderOverride:       b.ProviderOverride,
		ModelOverride:          b.ModelOverride,
		ReasoningLevel:         b.ReasoningLevel,
		PinnedEntityIDs:        pinned,
		ExcludedEntityIDs:      excluded,
		ActiveVersionID:        activeVer,
		GapReport:              b.GapReport,
		ATSScore:               b.ATSScore,
		Epoch:                  b.Epoch,
		LastSyncedMainRevision: b.LastSyncedMainRevision,
		CreatedAt:              normalizeUTCTime(b.CreatedAt),
		UpdatedAt:              normalizeUTCTime(b.UpdatedAt),
	}
}

// toCoreBranch converts a persisted branchRecord into a domain Branch.
func toCoreBranch(r *branchRecord) *core.Branch {
	if r == nil {
		return nil
	}
	var presetID, activeVer *string
	if r.PresetID != nil {
		cp := *r.PresetID
		presetID = &cp
	}
	if r.ActiveVersionID != nil {
		cp := *r.ActiveVersionID
		activeVer = &cp
	}
	var pinned, excluded []string
	if r.PinnedEntityIDs != nil {
		pinned = append([]string{}, r.PinnedEntityIDs...)
	}
	if r.ExcludedEntityIDs != nil {
		excluded = append([]string{}, r.ExcludedEntityIDs...)
	}
	return &core.Branch{
		ID:                     r.ID,
		UserID:                 r.UserID,
		Name:                   r.Name,
		IsDefault:              r.IsDefault,
		TargetRole:             r.TargetRole,
		TargetCompany:          r.TargetCompany,
		TargetRequirements:     r.TargetRequirements,
		BranchInstructions:     r.BranchInstructions,
		PresetID:               presetID,
		TemplateSetID:          r.TemplateSetID,
		ProviderOverride:       r.ProviderOverride,
		ModelOverride:          r.ModelOverride,
		ReasoningLevel:         r.ReasoningLevel,
		PinnedEntityIDs:        pinned,
		ExcludedEntityIDs:      excluded,
		ActiveVersionID:        activeVer,
		GapReport:              r.GapReport,
		ATSScore:               r.ATSScore,
		Epoch:                  r.Epoch,
		LastSyncedMainRevision: r.LastSyncedMainRevision,
		CreatedAt:              normalizeUTCTime(r.CreatedAt),
		UpdatedAt:              normalizeUTCTime(r.UpdatedAt),
	}
}

// branchVersionRecord maps core.BranchVersion to the relational resume_versions table.
type branchVersionRecord struct {
	ID                 string                `gorm:"primaryKey;type:text"`
	UserID             string                `gorm:"index;not null"`
	BranchID           string                `gorm:"uniqueIndex:idx_branch_version,priority:1;column:branch_id;index;not null"`
	JobID              string                `gorm:"column:job_id;index"`
	VersionNumber      int                   `gorm:"uniqueIndex:idx_branch_version,priority:2;column:version_number;not null"`
	BranchEpoch        int64                 `gorm:"column:branch_epoch;not null;default:1"`
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
	CreatedAt          time.Time             `gorm:"autoCreateTime:false"`
	UpdatedAt          time.Time             `gorm:"autoUpdateTime:false"`
	CompletedAt        *time.Time
}

// TableName overrides the GORM table name for branchVersionRecord.
func (branchVersionRecord) TableName() string {
	return "resume_versions"
}

// toBranchVersionRecord converts a domain BranchVersion into its relational record representation.
func toBranchVersionRecord(v *core.BranchVersion) *branchVersionRecord {
	var strategyID, parentID *string
	if v.StrategyIDUsed != nil {
		cp := *v.StrategyIDUsed
		strategyID = &cp
	}
	if v.ParentVersionID != nil {
		cp := *v.ParentVersionID
		parentID = &cp
	}
	var warnings []string
	if v.ValidationWarnings != nil {
		warnings = append([]string{}, v.ValidationWarnings...)
	}
	return &branchVersionRecord{
		ID:                 v.ID,
		UserID:             v.UserID,
		BranchID:           v.BranchID,
		JobID:              v.JobID,
		VersionNumber:      v.VersionNumber,
		BranchEpoch:        v.BranchEpoch,
		Status:             v.Status,
		MainRevisionUsed:   v.MainRevisionUsed,
		TemplateSetIDUsed:  v.TemplateSetIDUsed,
		StrategyIDUsed:     strategyID,
		ProviderUsed:       v.ProviderUsed,
		ModelUsed:          v.ModelUsed,
		SourceType:         v.SourceType,
		ParentVersionID:    parentID,
		TailoredContent:    v.TailoredContent,
		GapReport:          v.GapReport,
		ATSScore:           v.ATSScore,
		ValidationWarnings: warnings,
		ErrorMessage:       v.ErrorMessage,
		HTMLOutput:         v.HTMLOutput,
		MarkdownOutput:     v.MarkdownOutput,
		PlainTextOutput:    v.PlainTextOutput,
		CreatedAt:          normalizeUTCTime(v.CreatedAt),
		UpdatedAt:          normalizeUTCTime(v.UpdatedAt),
		CompletedAt:        normalizeUTCTimePtr(v.CompletedAt),
	}
}

// toCoreBranchVersion converts a persisted branchVersionRecord into a domain BranchVersion.
func toCoreBranchVersion(r *branchVersionRecord) *core.BranchVersion {
	if r == nil {
		return nil
	}
	var strategyID, parentID *string
	if r.StrategyIDUsed != nil {
		cp := *r.StrategyIDUsed
		strategyID = &cp
	}
	if r.ParentVersionID != nil {
		cp := *r.ParentVersionID
		parentID = &cp
	}
	var warnings []string
	if r.ValidationWarnings != nil {
		warnings = append([]string{}, r.ValidationWarnings...)
	}
	return &core.BranchVersion{
		ID:                 r.ID,
		UserID:             r.UserID,
		BranchID:           r.BranchID,
		JobID:              r.JobID,
		VersionNumber:      r.VersionNumber,
		BranchEpoch:        r.BranchEpoch,
		Status:             r.Status,
		MainRevisionUsed:   r.MainRevisionUsed,
		TemplateSetIDUsed:  r.TemplateSetIDUsed,
		StrategyIDUsed:     strategyID,
		ProviderUsed:       r.ProviderUsed,
		ModelUsed:          r.ModelUsed,
		SourceType:         r.SourceType,
		ParentVersionID:    parentID,
		TailoredContent:    r.TailoredContent,
		GapReport:          r.GapReport,
		ATSScore:           r.ATSScore,
		ValidationWarnings: warnings,
		ErrorMessage:       r.ErrorMessage,
		HTMLOutput:         r.HTMLOutput,
		MarkdownOutput:     r.MarkdownOutput,
		PlainTextOutput:    r.PlainTextOutput,
		CreatedAt:          normalizeUTCTime(r.CreatedAt),
		UpdatedAt:          normalizeUTCTime(r.UpdatedAt),
		CompletedAt:        normalizeUTCTimePtr(r.CompletedAt),
	}
}

// ListBranches returns all branch records owned by userID.
func (r *readTx) ListBranches(ctx context.Context, userID string) ([]core.Branch, error) {
	var recs []branchRecord
	if err := r.session().Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Branch, len(recs))
	for i := range recs {
		out[i] = *toCoreBranch(&recs[i])
	}
	return out, nil
}

// Branch loads a branch record by ID.
func (r *readTx) Branch(ctx context.Context, id string) (*core.Branch, error) {
	var rec branchRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreBranch(&rec), nil
}

// PutBranch inserts or replaces a branch record.
func (w *writeTx) PutBranch(ctx context.Context, branch *core.Branch) error {
	if branch == nil {
		return fmt.Errorf("%w: branch cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toBranchRecord(branch)).Error)
}

// DeleteBranch deletes a branch record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeleteBranch(ctx context.Context, id string) error {
	return deleteRecordByID[branchRecord](w.session(), id)
}

// ListVersionsByBranch returns all branch versions belonging to branchID.
func (r *readTx) ListVersionsByBranch(ctx context.Context, branchID string) ([]core.BranchVersion, error) {
	var recs []branchVersionRecord
	if err := r.session().Where("branch_id = ?", branchID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.BranchVersion, len(recs))
	for i := range recs {
		out[i] = *toCoreBranchVersion(&recs[i])
	}
	return out, nil
}

// ListVersionsByIDs returns branch versions matching any ID in ids.
func (r *readTx) ListVersionsByIDs(ctx context.Context, ids []string) ([]core.BranchVersion, error) {
	if len(ids) == 0 {
		return []core.BranchVersion{}, nil
	}
	var recs []branchVersionRecord
	if err := r.session().Where("id IN ?", ids).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.BranchVersion, len(recs))
	for i := range recs {
		out[i] = *toCoreBranchVersion(&recs[i])
	}
	return out, nil
}

// Version loads a branch version record by ID.
func (r *readTx) Version(ctx context.Context, id string) (*core.BranchVersion, error) {
	var rec branchVersionRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreBranchVersion(&rec), nil
}

// VersionByNumber loads a branch version record by branchID and versionNumber.
func (r *readTx) VersionByNumber(ctx context.Context, branchID string, versionNumber int) (*core.BranchVersion, error) {
	var rec branchVersionRecord
	if err := r.session().Where("branch_id = ? AND version_number = ?", branchID, versionNumber).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreBranchVersion(&rec), nil
}

// PutVersion inserts or replaces a branch version record.
func (w *writeTx) PutVersion(ctx context.Context, version *core.BranchVersion) error {
	if version == nil {
		return fmt.Errorf("%w: version cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toBranchVersionRecord(version)).Error)
}

// DeleteVersion deletes a branch version record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeleteVersion(ctx context.Context, id string) error {
	return deleteRecordByID[branchVersionRecord](w.session(), id)
}
