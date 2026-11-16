package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/core"
)

// projectRecord maps core.Project to the relational projects table.
type projectRecord struct {
	ID        string         `gorm:"primaryKey;type:text"`
	ShortID   string         `gorm:"column:short_id;index"`
	UserID    string         `gorm:"index;not null"`
	Name      string         `gorm:"not null"`
	Role      string         `gorm:"column:role"`
	URL       string         `gorm:"column:url"`
	StartDate *core.Date     `gorm:"column:start_date;type:text"`
	EndDate   *core.Date     `gorm:"column:end_date;type:text"`
	Summary   string         `gorm:"column:summary"`
	Skills    []skillRecord  `gorm:"many2many:project_skills;foreignKey:ID;joinForeignKey:project_id;References:ID;joinReferences:skill_id"`
	Metadata  map[string]any `gorm:"serializer:json"`
	CreatedAt time.Time      `gorm:"autoCreateTime:false"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for projectRecord.
func (projectRecord) TableName() string {
	return "projects"
}

// toProjectRecord converts a domain Project into its relational record representation.
func toProjectRecord(p *core.Project) *projectRecord {
	var startDate, endDate *core.Date
	if p.StartDate != nil {
		cp := *p.StartDate
		startDate = &cp
	}
	if p.EndDate != nil {
		cp := *p.EndDate
		endDate = &cp
	}
	return &projectRecord{
		ID:        p.ID,
		ShortID:   p.ShortID,
		UserID:    p.UserID,
		Name:      p.Name,
		Role:      p.Role,
		URL:       p.URL,
		StartDate: startDate,
		EndDate:   endDate,
		Summary:   p.Summary,
		Skills:    toSkillRecords(p.Skills),
		Metadata:  p.Metadata,
		CreatedAt: normalizeUTCTime(p.CreatedAt),
		UpdatedAt: normalizeUTCTime(p.UpdatedAt),
	}
}

// toCoreProject converts a persisted projectRecord into a domain Project.
func toCoreProject(r *projectRecord) *core.Project {
	if r == nil {
		return nil
	}
	var startDate, endDate *core.Date
	if r.StartDate != nil {
		cp := *r.StartDate
		startDate = &cp
	}
	if r.EndDate != nil {
		cp := *r.EndDate
		endDate = &cp
	}
	var skills []core.Skill
	if len(r.Skills) > 0 {
		skills = toCoreSkills(r.Skills)
	}
	return &core.Project{
		ID:        r.ID,
		ShortID:   r.ShortID,
		UserID:    r.UserID,
		Name:      r.Name,
		Role:      r.Role,
		URL:       r.URL,
		StartDate: startDate,
		EndDate:   endDate,
		Summary:   r.Summary,
		Skills:    skills,
		Metadata:  r.Metadata,
		CreatedAt: normalizeUTCTime(r.CreatedAt),
		UpdatedAt: normalizeUTCTime(r.UpdatedAt),
	}
}

// ListProjects returns all projects owned by userID with associated skills preloaded.
func (r *readTx) ListProjects(ctx context.Context, userID string) ([]core.Project, error) {
	var recs []projectRecord
	if err := r.session().Preload("Skills").Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Project, len(recs))
	for i := range recs {
		out[i] = *toCoreProject(&recs[i])
	}
	return out, nil
}

// Project loads a project record by ID with associated skills preloaded.
func (r *readTx) Project(ctx context.Context, id string) (*core.Project, error) {
	var rec projectRecord
	if err := r.session().Preload("Skills").Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreProject(&rec), nil
}

// PutProject inserts or replaces a project record and synchronizes project_skills.
func (w *writeTx) PutProject(ctx context.Context, proj *core.Project) error {
	if proj == nil {
		return fmt.Errorf("%w: project cannot be nil", core.ErrValidation)
	}
	rec := toProjectRecord(proj)
	if err := w.session().Omit("Skills").Save(rec).Error; err != nil {
		return mapError(err)
	}
	return mapError(w.session().Model(rec).Association("Skills").Replace(rec.Skills))
}

// DeleteProject clears project_skills links and deletes the project record by ID.
func (w *writeTx) DeleteProject(ctx context.Context, id string) error {
	var existing projectRecord
	if err := w.session().Select("id").Where("id = ?", id).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if err := w.session().Exec("DELETE FROM project_skills WHERE project_id = ?", id).Error; err != nil {
		return mapError(err)
	}
	return deleteRecordByID[projectRecord](w.session(), id)
}
