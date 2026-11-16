package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/core"
)

// workExperienceRecord maps core.WorkExperience to the relational work_experiences table.
type workExperienceRecord struct {
	ID                string              `gorm:"primaryKey;type:text"`
	ShortID           string              `gorm:"column:short_id;index"`
	UserID            string              `gorm:"index;not null"`
	Company           string              `gorm:"not null"`
	RoleTitle         string              `gorm:"column:role_title;not null"`
	EmploymentType    core.EmploymentType `gorm:"column:employment_type"`
	StartDate         core.Date           `gorm:"column:start_date;type:text"`
	EndDate           *core.Date          `gorm:"column:end_date;type:text"`
	IsCurrent         bool                `gorm:"column:is_current;not null;default:false"`
	Location          core.Location       `gorm:"serializer:json"`
	CompanyLocation   core.Location       `gorm:"serializer:json"`
	CompanyContact    *core.ContactDetail `gorm:"serializer:json"`
	HoursPerWeek      *int                `gorm:"column:hours_per_week"`
	SupervisorName    string              `gorm:"column:supervisor_name"`
	SupervisorContact string              `gorm:"column:supervisor_contact"`
	DutiesSummary     string              `gorm:"column:duties_summary"`
	Skills            []skillRecord       `gorm:"many2many:experience_skills;foreignKey:ID;joinForeignKey:work_experience_id;References:ID;joinReferences:skill_id"`
	Metadata          map[string]any      `gorm:"serializer:json"`
	CreatedAt         time.Time           `gorm:"autoCreateTime:false"`
	UpdatedAt         time.Time           `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for workExperienceRecord.
func (workExperienceRecord) TableName() string {
	return "work_experiences"
}

// toWorkExperienceRecord converts a domain WorkExperience into its relational record representation.
func toWorkExperienceRecord(e *core.WorkExperience) *workExperienceRecord {
	var endDate *core.Date
	if e.EndDate != nil {
		cp := *e.EndDate
		endDate = &cp
	}
	var companyContact *core.ContactDetail
	if e.CompanyContact != nil {
		cp := *e.CompanyContact
		companyContact = &cp
	}
	var hours *int
	if e.HoursPerWeek != nil {
		cp := *e.HoursPerWeek
		hours = &cp
	}
	return &workExperienceRecord{
		ID:                e.ID,
		ShortID:           e.ShortID,
		UserID:            e.UserID,
		Company:           e.Company,
		RoleTitle:         e.RoleTitle,
		EmploymentType:    e.EmploymentType,
		StartDate:         e.StartDate,
		EndDate:           endDate,
		IsCurrent:         e.IsCurrent,
		Location:          e.Location,
		CompanyLocation:   e.CompanyLocation,
		CompanyContact:    companyContact,
		HoursPerWeek:      hours,
		SupervisorName:    e.SupervisorName,
		SupervisorContact: e.SupervisorContact,
		DutiesSummary:     e.DutiesSummary,
		Skills:            toSkillRecords(e.Skills),
		Metadata:          e.Metadata,
		CreatedAt:         normalizeUTCTime(e.CreatedAt),
		UpdatedAt:         normalizeUTCTime(e.UpdatedAt),
	}
}

// toCoreWorkExperience converts a persisted workExperienceRecord into a domain WorkExperience.
func toCoreWorkExperience(r *workExperienceRecord) *core.WorkExperience {
	if r == nil {
		return nil
	}
	var endDate *core.Date
	if r.EndDate != nil {
		cp := *r.EndDate
		endDate = &cp
	}
	var companyContact *core.ContactDetail
	if r.CompanyContact != nil {
		cp := *r.CompanyContact
		companyContact = &cp
	}
	var hours *int
	if r.HoursPerWeek != nil {
		cp := *r.HoursPerWeek
		hours = &cp
	}
	var skills []core.Skill
	if len(r.Skills) > 0 {
		skills = toCoreSkills(r.Skills)
	}
	return &core.WorkExperience{
		ID:                r.ID,
		ShortID:           r.ShortID,
		UserID:            r.UserID,
		Company:           r.Company,
		RoleTitle:         r.RoleTitle,
		EmploymentType:    r.EmploymentType,
		StartDate:         r.StartDate,
		EndDate:           endDate,
		IsCurrent:         r.IsCurrent,
		Location:          r.Location,
		CompanyLocation:   r.CompanyLocation,
		CompanyContact:    companyContact,
		HoursPerWeek:      hours,
		SupervisorName:    r.SupervisorName,
		SupervisorContact: r.SupervisorContact,
		DutiesSummary:     r.DutiesSummary,
		Skills:            skills,
		Metadata:          r.Metadata,
		CreatedAt:         normalizeUTCTime(r.CreatedAt),
		UpdatedAt:         normalizeUTCTime(r.UpdatedAt),
	}
}

// ListExperiences returns all work experiences owned by userID with associated skills preloaded.
func (r *readTx) ListExperiences(ctx context.Context, userID string) ([]core.WorkExperience, error) {
	var recs []workExperienceRecord
	if err := r.session().Preload("Skills").Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.WorkExperience, len(recs))
	for i := range recs {
		out[i] = *toCoreWorkExperience(&recs[i])
	}
	return out, nil
}

// Experience loads a work experience record by ID with associated skills preloaded.
func (r *readTx) Experience(ctx context.Context, id string) (*core.WorkExperience, error) {
	var rec workExperienceRecord
	if err := r.session().Preload("Skills").Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreWorkExperience(&rec), nil
}

// PutExperience inserts or replaces a work experience record and synchronizes experience_skills.
func (w *writeTx) PutExperience(ctx context.Context, exp *core.WorkExperience) error {
	if exp == nil {
		return fmt.Errorf("%w: experience cannot be nil", core.ErrValidation)
	}
	rec := toWorkExperienceRecord(exp)
	if err := w.session().Omit("Skills").Save(rec).Error; err != nil {
		return mapError(err)
	}
	return mapError(w.session().Model(rec).Association("Skills").Replace(rec.Skills))
}

// DeleteExperience clears experience_skills links and deletes the work experience record by ID.
func (w *writeTx) DeleteExperience(ctx context.Context, id string) error {
	var existing workExperienceRecord
	if err := w.session().Select("id").Where("id = ?", id).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if err := w.session().Exec("DELETE FROM experience_skills WHERE work_experience_id = ?", id).Error; err != nil {
		return mapError(err)
	}
	return deleteRecordByID[workExperienceRecord](w.session(), id)
}
