package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/resumeee-core"
	"gorm.io/gorm"
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
	IsCurrent         bool                `gorm:"column:is_current;not null"`
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

// educationRecord maps core.Education to the relational educations table.
type educationRecord struct {
	ID          string         `gorm:"primaryKey;type:text"`
	ShortID     string         `gorm:"column:short_id;index"`
	UserID      string         `gorm:"index;not null"`
	Institution string         `gorm:"not null"`
	Degree      string         `gorm:"column:degree"`
	Field       string         `gorm:"column:field"`
	StartDate   *core.Date     `gorm:"column:start_date;type:text"`
	EndDate     *core.Date     `gorm:"column:end_date;type:text"`
	Score       string         `gorm:"column:score"`
	Location    core.Location  `gorm:"serializer:json"`
	Metadata    map[string]any `gorm:"serializer:json"`
	CreatedAt   time.Time      `gorm:"autoCreateTime:false"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for educationRecord.
func (educationRecord) TableName() string {
	return "educations"
}

// toEducationRecord converts a domain Education into its relational record representation.
func toEducationRecord(e *core.Education) *educationRecord {
	var startDate, endDate *core.Date
	if e.StartDate != nil {
		cp := *e.StartDate
		startDate = &cp
	}
	if e.EndDate != nil {
		cp := *e.EndDate
		endDate = &cp
	}
	return &educationRecord{
		ID:          e.ID,
		ShortID:     e.ShortID,
		UserID:      e.UserID,
		Institution: e.Institution,
		Degree:      e.Degree,
		Field:       e.Field,
		StartDate:   startDate,
		EndDate:     endDate,
		Score:       e.Score,
		Location:    e.Location,
		Metadata:    e.Metadata,
		CreatedAt:   normalizeUTCTime(e.CreatedAt),
		UpdatedAt:   normalizeUTCTime(e.UpdatedAt),
	}
}

// toCoreEducation converts a persisted educationRecord into a domain Education.
func toCoreEducation(r *educationRecord) *core.Education {
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
	return &core.Education{
		ID:          r.ID,
		ShortID:     r.ShortID,
		UserID:      r.UserID,
		Institution: r.Institution,
		Degree:      r.Degree,
		Field:       r.Field,
		StartDate:   startDate,
		EndDate:     endDate,
		Score:       r.Score,
		Location:    r.Location,
		Metadata:    r.Metadata,
		CreatedAt:   normalizeUTCTime(r.CreatedAt),
		UpdatedAt:   normalizeUTCTime(r.UpdatedAt),
	}
}

// certificationRecord maps core.Certification to the relational certifications table.
type certificationRecord struct {
	ID        string         `gorm:"primaryKey;type:text"`
	ShortID   string         `gorm:"column:short_id;index"`
	UserID    string         `gorm:"index;not null"`
	Name      string         `gorm:"not null"`
	Issuer    string         `gorm:"column:issuer"`
	Date      *core.Date     `gorm:"column:date;type:text"`
	URL       string         `gorm:"column:url"`
	Summary   string         `gorm:"column:summary"`
	Metadata  map[string]any `gorm:"serializer:json"`
	CreatedAt time.Time      `gorm:"autoCreateTime:false"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for certificationRecord.
func (certificationRecord) TableName() string {
	return "certifications"
}

// toCertificationRecord converts a domain Certification into its relational record representation.
func toCertificationRecord(c *core.Certification) *certificationRecord {
	var date *core.Date
	if c.Date != nil {
		cp := *c.Date
		date = &cp
	}
	return &certificationRecord{
		ID:        c.ID,
		ShortID:   c.ShortID,
		UserID:    c.UserID,
		Name:      c.Name,
		Issuer:    c.Issuer,
		Date:      date,
		URL:       c.URL,
		Summary:   c.Summary,
		Metadata:  c.Metadata,
		CreatedAt: normalizeUTCTime(c.CreatedAt),
		UpdatedAt: normalizeUTCTime(c.UpdatedAt),
	}
}

// toCoreCertification converts a persisted certificationRecord into a domain Certification.
func toCoreCertification(r *certificationRecord) *core.Certification {
	if r == nil {
		return nil
	}
	var date *core.Date
	if r.Date != nil {
		cp := *r.Date
		date = &cp
	}
	return &core.Certification{
		ID:        r.ID,
		ShortID:   r.ShortID,
		UserID:    r.UserID,
		Name:      r.Name,
		Issuer:    r.Issuer,
		Date:      date,
		URL:       r.URL,
		Summary:   r.Summary,
		Metadata:  r.Metadata,
		CreatedAt: normalizeUTCTime(r.CreatedAt),
		UpdatedAt: normalizeUTCTime(r.UpdatedAt),
	}
}

// factRecord maps core.Fact to the relational facts table.
type factRecord struct {
	ID        string         `gorm:"primaryKey;type:text"`
	ShortID   string         `gorm:"column:short_id;index"`
	UserID    string         `gorm:"index;not null"`
	OwnerID   string         `gorm:"column:owner_id;index;not null"`
	OwnerType core.OwnerType `gorm:"column:owner_type;index;not null"`
	Statement string         `gorm:"not null"`
	Impact    string         `gorm:"column:impact"`
	Skills    []skillRecord  `gorm:"many2many:fact_skills;foreignKey:ID;joinForeignKey:fact_id;References:ID;joinReferences:skill_id"`
	CreatedAt time.Time      `gorm:"autoCreateTime:false"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for factRecord.
func (factRecord) TableName() string {
	return "facts"
}

// toFactRecord converts a domain Fact into its relational record representation.
func toFactRecord(f *core.Fact) *factRecord {
	return &factRecord{
		ID:        f.ID,
		ShortID:   f.ShortID,
		UserID:    f.UserID,
		OwnerID:   f.OwnerID,
		OwnerType: f.OwnerType,
		Statement: f.Statement,
		Impact:    f.Impact,
		Skills:    toSkillRecords(f.Skills),
		CreatedAt: normalizeUTCTime(f.CreatedAt),
		UpdatedAt: normalizeUTCTime(f.UpdatedAt),
	}
}

// toCoreFact converts a persisted factRecord into a domain Fact.
func toCoreFact(r *factRecord) *core.Fact {
	if r == nil {
		return nil
	}
	var skills []core.Skill
	if len(r.Skills) > 0 {
		skills = toCoreSkills(r.Skills)
	}
	return &core.Fact{
		ID:        r.ID,
		ShortID:   r.ShortID,
		UserID:    r.UserID,
		OwnerID:   r.OwnerID,
		OwnerType: r.OwnerType,
		Statement: r.Statement,
		Impact:    r.Impact,
		Skills:    skills,
		CreatedAt: normalizeUTCTime(r.CreatedAt),
		UpdatedAt: normalizeUTCTime(r.UpdatedAt),
	}
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

// GetSkill loads a skill record by ID.
func (r *readTx) GetSkill(ctx context.Context, id string) (*core.Skill, error) {
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

// GetExperience loads a work experience record by ID with associated skills preloaded.
func (r *readTx) GetExperience(ctx context.Context, id string) (*core.WorkExperience, error) {
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

// GetProject loads a project record by ID with associated skills preloaded.
func (r *readTx) GetProject(ctx context.Context, id string) (*core.Project, error) {
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

// ListEducations returns all education records owned by userID.
func (r *readTx) ListEducations(ctx context.Context, userID string) ([]core.Education, error) {
	var recs []educationRecord
	if err := r.session().Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Education, len(recs))
	for i := range recs {
		out[i] = *toCoreEducation(&recs[i])
	}
	return out, nil
}

// GetEducation loads an education record by ID.
func (r *readTx) GetEducation(ctx context.Context, id string) (*core.Education, error) {
	var rec educationRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreEducation(&rec), nil
}

// PutEducation inserts or replaces an education record.
func (w *writeTx) PutEducation(ctx context.Context, edu *core.Education) error {
	if edu == nil {
		return fmt.Errorf("%w: education cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toEducationRecord(edu)).Error)
}

// DeleteEducation deletes an education record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeleteEducation(ctx context.Context, id string) error {
	return deleteRecordByID[educationRecord](w.session(), id)
}

// ListCertifications returns all certification records owned by userID.
func (r *readTx) ListCertifications(ctx context.Context, userID string) ([]core.Certification, error) {
	var recs []certificationRecord
	if err := r.session().Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Certification, len(recs))
	for i := range recs {
		out[i] = *toCoreCertification(&recs[i])
	}
	return out, nil
}

// GetCertification loads a certification record by ID.
func (r *readTx) GetCertification(ctx context.Context, id string) (*core.Certification, error) {
	var rec certificationRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreCertification(&rec), nil
}

// PutCertification inserts or replaces a certification record.
func (w *writeTx) PutCertification(ctx context.Context, cert *core.Certification) error {
	if cert == nil {
		return fmt.Errorf("%w: certification cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toCertificationRecord(cert)).Error)
}

// DeleteCertification deletes a certification record by ID or returns core.ErrNotFound when absent.
func (w *writeTx) DeleteCertification(ctx context.Context, id string) error {
	return deleteRecordByID[certificationRecord](w.session(), id)
}

// ListFacts returns all facts owned by userID with associated skills preloaded.
func (r *readTx) ListFacts(ctx context.Context, userID string) ([]core.Fact, error) {
	var recs []factRecord
	if err := r.session().Preload("Skills").Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Fact, len(recs))
	for i := range recs {
		out[i] = *toCoreFact(&recs[i])
	}
	return out, nil
}

// ListFactsByOwner returns facts owned by userID scoped to ownerType and ownerID.
func (r *readTx) ListFactsByOwner(ctx context.Context, userID string, ownerType core.OwnerType, ownerID string) ([]core.Fact, error) {
	var recs []factRecord
	if err := r.session().
		Preload("Skills").
		Where("user_id = ? AND owner_type = ? AND owner_id = ?", userID, ownerType, ownerID).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.Fact, len(recs))
	for i := range recs {
		out[i] = *toCoreFact(&recs[i])
	}
	return out, nil
}

// GetFact loads a fact record by ID with associated skills preloaded.
func (r *readTx) GetFact(ctx context.Context, id string) (*core.Fact, error) {
	var rec factRecord
	if err := r.session().Preload("Skills").Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreFact(&rec), nil
}

// PutFact inserts or replaces a fact record and synchronizes fact_skills.
func (w *writeTx) PutFact(ctx context.Context, fact *core.Fact) error {
	if fact == nil {
		return fmt.Errorf("%w: fact cannot be nil", core.ErrValidation)
	}
	rec := toFactRecord(fact)
	if err := w.session().Omit("Skills").Save(rec).Error; err != nil {
		return mapError(err)
	}
	return mapError(w.session().Model(rec).Association("Skills").Replace(rec.Skills))
}

// DeleteFact clears fact_skills links and deletes the fact record by ID.
func (w *writeTx) DeleteFact(ctx context.Context, id string) error {
	var existing factRecord
	if err := w.session().Select("id").Where("id = ?", id).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if err := w.session().Exec("DELETE FROM fact_skills WHERE fact_id = ?", id).Error; err != nil {
		return mapError(err)
	}
	return deleteRecordByID[factRecord](w.session(), id)
}

// deleteRecordByID removes the row matching id from table T or returns core.ErrNotFound when absent.
func deleteRecordByID[T any](tx *gorm.DB, id string) error {
	var model T
	res := tx.Where("id = ?", id).Delete(&model)
	if res.Error != nil {
		return mapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return core.ErrNotFound
	}
	return nil
}
