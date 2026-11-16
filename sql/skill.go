package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/core"
)

// skillRecord maps core.Skill to the relational skills table.
type skillRecord struct {
	ID              string         `gorm:"primaryKey;type:text"`
	ShortID         string         `gorm:"column:short_id;index"`
	UserID          string         `gorm:"index;not null"`
	Name            string         `gorm:"not null"`
	Slug            string         `gorm:"index"`
	IsGlobal        bool           `gorm:"column:is_global;index;not null"`
	Category        string         `gorm:"column:category"`
	Proficiency     string         `gorm:"column:proficiency"`
	YearsExperience float64        `gorm:"column:years_experience"`
	Keywords        []string       `gorm:"serializer:json"`
	Metadata        map[string]any `gorm:"serializer:json"`
	CreatedAt       time.Time      `gorm:"autoCreateTime:false"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for skillRecord.
func (skillRecord) TableName() string {
	return "skills"
}

// toSkillRecord converts a domain Skill into its relational record representation.
func toSkillRecord(s *core.Skill) *skillRecord {
	var keywords []string
	if s.Keywords != nil {
		keywords = append([]string{}, s.Keywords...)
	}
	return &skillRecord{
		ID:              s.ID,
		ShortID:         s.ShortID,
		UserID:          s.UserID,
		Name:            s.Name,
		Slug:            s.Slug,
		IsGlobal:        s.IsGlobal,
		Category:        s.Category,
		Proficiency:     s.Proficiency,
		YearsExperience: s.YearsExperience,
		Keywords:        keywords,
		Metadata:        s.Metadata,
		CreatedAt:       normalizeUTCTime(s.CreatedAt),
		UpdatedAt:       normalizeUTCTime(s.UpdatedAt),
	}
}

// toCoreSkill converts a persisted skillRecord into a domain Skill.
func toCoreSkill(r *skillRecord) *core.Skill {
	if r == nil {
		return nil
	}
	var keywords []string
	if r.Keywords != nil {
		keywords = append([]string{}, r.Keywords...)
	}
	return &core.Skill{
		ID:              r.ID,
		ShortID:         r.ShortID,
		UserID:          r.UserID,
		Name:            r.Name,
		Slug:            r.Slug,
		IsGlobal:        r.IsGlobal,
		Category:        r.Category,
		Proficiency:     r.Proficiency,
		YearsExperience: r.YearsExperience,
		Keywords:        keywords,
		Metadata:        r.Metadata,
		CreatedAt:       normalizeUTCTime(r.CreatedAt),
		UpdatedAt:       normalizeUTCTime(r.UpdatedAt),
	}
}

// toSkillRecords converts a slice of domain skills to relational skill records.
func toSkillRecords(skills []core.Skill) []skillRecord {
	recs := make([]skillRecord, 0, len(skills))
	for i := range skills {
		recs = append(recs, *toSkillRecord(&skills[i]))
	}
	return recs
}

// toCoreSkills converts a slice of relational skill records to domain skills.
func toCoreSkills(recs []skillRecord) []core.Skill {
	if recs == nil {
		return nil
	}
	out := make([]core.Skill, len(recs))
	for i := range recs {
		out[i] = *toCoreSkill(&recs[i])
	}
	return out
}

// ListSkills returns skills owned by userID, optionally including curated global skills.
func (r *readTx) ListSkills(ctx context.Context, userID string, includeGlobal bool) ([]core.Skill, error) {
	var recs []skillRecord
	q := r.session()
	if includeGlobal {
		q = q.Where("user_id = ? OR is_global = ? OR user_id = ?", userID, true, core.SystemUserID)
	} else {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreSkills(recs), nil
}

// Skill loads a skill record by ID.
func (r *readTx) Skill(ctx context.Context, id string) (*core.Skill, error) {
	var rec skillRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreSkill(&rec), nil
}

// PutSkill inserts or replaces a skill record.
func (w *writeTx) PutSkill(ctx context.Context, skill *core.Skill) error {
	if skill == nil {
		return fmt.Errorf("%w: skill cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toSkillRecord(skill)).Error)
}

// DeleteSkill removes join associations referencing id and deletes the skill row.
func (w *writeTx) DeleteSkill(ctx context.Context, id string) error {
	var existing skillRecord
	if err := w.session().Select("id").Where("id = ?", id).First(&existing).Error; err != nil {
		return mapError(err)
	}
	for _, stmt := range []string{
		"DELETE FROM experience_skills WHERE skill_id = ?",
		"DELETE FROM project_skills WHERE skill_id = ?",
		"DELETE FROM fact_skills WHERE skill_id = ?",
	} {
		if err := w.session().Exec(stmt, id).Error; err != nil {
			return mapError(err)
		}
	}
	return deleteRecordByID[skillRecord](w.session(), id)
}
