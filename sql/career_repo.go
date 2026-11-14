package sql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// skillRecord is the GORM persistence model for core.Skill.
type skillRecord struct {
	ID              string         `gorm:"primaryKey;type:text"`
	ShortID         string         `gorm:"index"`
	UserID          string         `gorm:"index;not null"`
	Name            string         `gorm:"not null;index"`
	Slug            string         `gorm:"index"`
	IsGlobal        bool           `gorm:"index;default:false"`
	Category        string         `gorm:"column:category"`
	Proficiency     string         `gorm:"column:proficiency"`
	YearsExperience float64        `gorm:"column:years_experience"`
	Keywords        []string       `gorm:"serializer:json"`
	Metadata        map[string]any `gorm:"serializer:json"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName overrides the GORM table name to skills.
func (skillRecord) TableName() string {
	return "skills"
}

func toSkillRecord(sk *core.Skill) *skillRecord {
	if sk == nil {
		return nil
	}
	return &skillRecord{
		ID:              sk.ID,
		ShortID:         sk.ShortID,
		UserID:          sk.UserID,
		Name:            sk.Name,
		Slug:            sk.Slug,
		IsGlobal:        sk.IsGlobal,
		Category:        sk.Category,
		Proficiency:     sk.Proficiency,
		YearsExperience: sk.YearsExperience,
		Keywords:        sk.Keywords,
		Metadata:        sk.Metadata,
		CreatedAt:       sk.CreatedAt,
		UpdatedAt:       sk.UpdatedAt,
	}
}

func toSkillRecords(skills []core.Skill) []skillRecord {
	if skills == nil {
		return nil
	}
	recs := make([]skillRecord, len(skills))
	for i := range skills {
		recs[i] = *toSkillRecord(&skills[i])
	}
	return recs
}

func toCoreSkill(r *skillRecord) *core.Skill {
	if r == nil {
		return nil
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
		Keywords:        r.Keywords,
		Metadata:        r.Metadata,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

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

// workExperienceRecord is the GORM persistence model for core.WorkExperience.
type workExperienceRecord struct {
	ID                string              `gorm:"primaryKey;type:text"`
	ShortID           string              `gorm:"index"`
	UserID            string              `gorm:"index;not null"`
	Company           string              `gorm:"not null"`
	RoleTitle         string              `gorm:"not null"`
	EmploymentType    core.EmploymentType `gorm:"column:employment_type"`
	StartDate         core.Date           `gorm:"not null"`
	EndDate           *core.Date          `gorm:"column:end_date"`
	IsCurrent         bool                `gorm:"column:is_current"`
	Location          core.Location       `gorm:"serializer:json"`
	CompanyLocation   core.Location       `gorm:"serializer:json"`
	CompanyContact    *core.ContactDetail `gorm:"serializer:json"`
	HoursPerWeek      *int                `gorm:"column:hours_per_week"`
	SupervisorName    string              `gorm:"column:supervisor_name"`
	SupervisorContact string              `gorm:"column:supervisor_contact"`
	DutiesSummary     string              `gorm:"column:duties_summary"`
	Facts             []factRecord        `gorm:"polymorphic:Owner;polymorphicValue:work_experience"`
	Skills            []skillRecord       `gorm:"many2many:experience_skills;foreignKey:ID;joinForeignKey:work_experience_id;References:ID;joinReferences:skill_id"`
	Metadata          map[string]any      `gorm:"serializer:json"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// TableName overrides the GORM table name to work_experiences.
func (workExperienceRecord) TableName() string {
	return "work_experiences"
}

func toWorkExperienceRecord(e *core.WorkExperience) *workExperienceRecord {
	if e == nil {
		return nil
	}
	return &workExperienceRecord{
		ID:                e.ID,
		ShortID:           e.ShortID,
		UserID:            e.UserID,
		Company:           e.Company,
		RoleTitle:         e.RoleTitle,
		EmploymentType:    e.EmploymentType,
		StartDate:         e.StartDate,
		EndDate:           e.EndDate,
		IsCurrent:         e.IsCurrent,
		Location:          e.Location,
		CompanyLocation:   e.CompanyLocation,
		CompanyContact:    e.CompanyContact,
		HoursPerWeek:      e.HoursPerWeek,
		SupervisorName:    e.SupervisorName,
		SupervisorContact: e.SupervisorContact,
		DutiesSummary:     e.DutiesSummary,
		Facts:             toFactRecords(e.Facts),
		Skills:            toSkillRecords(e.Skills),
		Metadata:          e.Metadata,
		CreatedAt:         e.CreatedAt,
		UpdatedAt:         e.UpdatedAt,
	}
}

func toCoreExperience(r *workExperienceRecord) *core.WorkExperience {
	if r == nil {
		return nil
	}
	return &core.WorkExperience{
		ID:                r.ID,
		ShortID:           r.ShortID,
		UserID:            r.UserID,
		Company:           r.Company,
		RoleTitle:         r.RoleTitle,
		EmploymentType:    r.EmploymentType,
		StartDate:         r.StartDate,
		EndDate:           r.EndDate,
		IsCurrent:         r.IsCurrent,
		Location:          r.Location,
		CompanyLocation:   r.CompanyLocation,
		CompanyContact:    r.CompanyContact,
		HoursPerWeek:      r.HoursPerWeek,
		SupervisorName:    r.SupervisorName,
		SupervisorContact: r.SupervisorContact,
		DutiesSummary:     r.DutiesSummary,
		Facts:             toCoreFacts(r.Facts),
		Skills:            toCoreSkills(r.Skills),
		Metadata:          r.Metadata,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}

func toCoreExperiences(recs []workExperienceRecord) []core.WorkExperience {
	if recs == nil {
		return nil
	}
	out := make([]core.WorkExperience, len(recs))
	for i := range recs {
		out[i] = *toCoreExperience(&recs[i])
	}
	return out
}

// projectRecord is the GORM persistence model for core.Project.
type projectRecord struct {
	ID        string         `gorm:"primaryKey;type:text"`
	ShortID   string         `gorm:"index"`
	UserID    string         `gorm:"index;not null"`
	Name      string         `gorm:"not null"`
	Role      string         `gorm:"column:role"`
	URL       string         `gorm:"column:url"`
	StartDate *core.Date     `gorm:"column:start_date"`
	EndDate   *core.Date     `gorm:"column:end_date"`
	Summary   string         `gorm:"column:summary"`
	Facts     []factRecord   `gorm:"polymorphic:Owner;polymorphicValue:project"`
	Skills    []skillRecord  `gorm:"many2many:project_skills;foreignKey:ID;joinForeignKey:project_id;References:ID;joinReferences:skill_id"`
	Metadata  map[string]any `gorm:"serializer:json"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName overrides the GORM table name to projects.
func (projectRecord) TableName() string {
	return "projects"
}

func toProjectRecord(p *core.Project) *projectRecord {
	if p == nil {
		return nil
	}
	return &projectRecord{
		ID:        p.ID,
		ShortID:   p.ShortID,
		UserID:    p.UserID,
		Name:      p.Name,
		Role:      p.Role,
		URL:       p.URL,
		StartDate: p.StartDate,
		EndDate:   p.EndDate,
		Summary:   p.Summary,
		Facts:     toFactRecords(p.Facts),
		Skills:    toSkillRecords(p.Skills),
		Metadata:  p.Metadata,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func toCoreProject(r *projectRecord) *core.Project {
	if r == nil {
		return nil
	}
	return &core.Project{
		ID:        r.ID,
		ShortID:   r.ShortID,
		UserID:    r.UserID,
		Name:      r.Name,
		Role:      r.Role,
		URL:       r.URL,
		StartDate: r.StartDate,
		EndDate:   r.EndDate,
		Summary:   r.Summary,
		Facts:     toCoreFacts(r.Facts),
		Skills:    toCoreSkills(r.Skills),
		Metadata:  r.Metadata,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func toCoreProjects(recs []projectRecord) []core.Project {
	if recs == nil {
		return nil
	}
	out := make([]core.Project, len(recs))
	for i := range recs {
		out[i] = *toCoreProject(&recs[i])
	}
	return out
}

// educationRecord is the GORM persistence model for core.Education.
type educationRecord struct {
	ID          string         `gorm:"primaryKey;type:text"`
	ShortID     string         `gorm:"index"`
	UserID      string         `gorm:"index;not null"`
	Institution string         `gorm:"not null"`
	Degree      string         `gorm:"column:degree"`
	Field       string         `gorm:"column:field"`
	StartDate   *core.Date     `gorm:"column:start_date"`
	EndDate     *core.Date     `gorm:"column:end_date"`
	Score       string         `gorm:"column:score"`
	Location    core.Location  `gorm:"serializer:json"`
	Facts       []factRecord   `gorm:"polymorphic:Owner;polymorphicValue:education"`
	Metadata    map[string]any `gorm:"serializer:json"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableName overrides the GORM table name to educations.
func (educationRecord) TableName() string {
	return "educations"
}

func toEducationRecord(e *core.Education) *educationRecord {
	if e == nil {
		return nil
	}
	return &educationRecord{
		ID:          e.ID,
		ShortID:     e.ShortID,
		UserID:      e.UserID,
		Institution: e.Institution,
		Degree:      e.Degree,
		Field:       e.Field,
		StartDate:   e.StartDate,
		EndDate:     e.EndDate,
		Score:       e.Score,
		Location:    e.Location,
		Facts:       toFactRecords(e.Facts),
		Metadata:    e.Metadata,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

func toCoreEducation(r *educationRecord) *core.Education {
	if r == nil {
		return nil
	}
	return &core.Education{
		ID:          r.ID,
		ShortID:     r.ShortID,
		UserID:      r.UserID,
		Institution: r.Institution,
		Degree:      r.Degree,
		Field:       r.Field,
		StartDate:   r.StartDate,
		EndDate:     r.EndDate,
		Score:       r.Score,
		Location:    r.Location,
		Facts:       toCoreFacts(r.Facts),
		Metadata:    r.Metadata,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func toCoreEducationList(recs []educationRecord) []core.Education {
	if recs == nil {
		return nil
	}
	out := make([]core.Education, len(recs))
	for i := range recs {
		out[i] = *toCoreEducation(&recs[i])
	}
	return out
}

// certificationRecord is the GORM persistence model for core.Certification.
type certificationRecord struct {
	ID        string         `gorm:"primaryKey;type:text"`
	ShortID   string         `gorm:"index"`
	UserID    string         `gorm:"index;not null"`
	Name      string         `gorm:"not null"`
	Issuer    string         `gorm:"column:issuer"`
	Date      *core.Date     `gorm:"column:date"`
	URL       string         `gorm:"column:url"`
	Summary   string         `gorm:"column:summary"`
	Metadata  map[string]any `gorm:"serializer:json"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName overrides the GORM table name to certifications.
func (certificationRecord) TableName() string {
	return "certifications"
}

func toCertificationRecord(c *core.Certification) *certificationRecord {
	if c == nil {
		return nil
	}
	return &certificationRecord{
		ID:        c.ID,
		ShortID:   c.ShortID,
		UserID:    c.UserID,
		Name:      c.Name,
		Issuer:    c.Issuer,
		Date:      c.Date,
		URL:       c.URL,
		Summary:   c.Summary,
		Metadata:  c.Metadata,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func toCoreCertification(r *certificationRecord) *core.Certification {
	if r == nil {
		return nil
	}
	return &core.Certification{
		ID:        r.ID,
		ShortID:   r.ShortID,
		UserID:    r.UserID,
		Name:      r.Name,
		Issuer:    r.Issuer,
		Date:      r.Date,
		URL:       r.URL,
		Summary:   r.Summary,
		Metadata:  r.Metadata,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func toCoreCertifications(recs []certificationRecord) []core.Certification {
	if recs == nil {
		return nil
	}
	out := make([]core.Certification, len(recs))
	for i := range recs {
		out[i] = *toCoreCertification(&recs[i])
	}
	return out
}

// factRecord is the GORM persistence model for core.Fact.
type factRecord struct {
	ID        string         `gorm:"primaryKey;type:text"`
	ShortID   string         `gorm:"index"`
	UserID    string         `gorm:"index;not null"`
	OwnerID   string         `gorm:"index;index:idx_fact_owner;not null"`
	OwnerType core.OwnerType `gorm:"index;index:idx_fact_owner;not null"`
	Statement string         `gorm:"not null"`
	Impact    string         `gorm:"column:impact"`
	Skills    []skillRecord  `gorm:"many2many:fact_skills;foreignKey:ID;joinForeignKey:fact_id;References:ID;joinReferences:skill_id"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName overrides the GORM table name to facts.
func (factRecord) TableName() string {
	return "facts"
}

func toFactRecord(f *core.Fact) *factRecord {
	if f == nil {
		return nil
	}
	return &factRecord{
		ID:        f.ID,
		ShortID:   f.ShortID,
		UserID:    f.UserID,
		OwnerID:   f.OwnerID,
		OwnerType: f.OwnerType,
		Statement: f.Statement,
		Impact:    f.Impact,
		Skills:    toSkillRecords(f.Skills),
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
}

func toFactRecords(facts []core.Fact) []factRecord {
	if facts == nil {
		return nil
	}
	recs := make([]factRecord, len(facts))
	for i := range facts {
		recs[i] = *toFactRecord(&facts[i])
	}
	return recs
}

func toCoreFact(r *factRecord) *core.Fact {
	if r == nil {
		return nil
	}
	return &core.Fact{
		ID:        r.ID,
		ShortID:   r.ShortID,
		UserID:    r.UserID,
		OwnerID:   r.OwnerID,
		OwnerType: r.OwnerType,
		Statement: r.Statement,
		Impact:    r.Impact,
		Skills:    toCoreSkills(r.Skills),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func toCoreFacts(recs []factRecord) []core.Fact {
	if recs == nil {
		return nil
	}
	out := make([]core.Fact, len(recs))
	for i := range recs {
		out[i] = *toCoreFact(&recs[i])
	}
	return out
}

// CareerStore implements core.CareerStore backed by GORM.
type CareerStore struct {
	db *gorm.DB
}

var _ core.CareerStore = (*CareerStore)(nil)

// NewCareerStore constructs a SQL-backed CareerStore.
func NewCareerStore(db *gorm.DB) *CareerStore {
	return &CareerStore{db: db}
}

// ListSkills returns all skills registered for the specified user plus system global skills.
func (s *CareerStore) ListSkills(ctx context.Context, userID string) ([]core.Skill, error) {
	var recs []skillRecord
	if err := s.db.WithContext(ctx).Where("user_id = ? OR is_global = ?", userID, true).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreSkills(recs), nil
}

// ListGlobalSkills returns all system-seeded global skills.
func (s *CareerStore) ListGlobalSkills(ctx context.Context) ([]core.Skill, error) {
	var recs []skillRecord
	if err := s.db.WithContext(ctx).Where("is_global = ?", true).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreSkills(recs), nil
}

// Skill retrieves a skill record by either its canonical UUID or its compact short_id,
// scoped to the authenticated userID or system global skills.
func (s *CareerStore) Skill(ctx context.Context, userID, id string) (*core.Skill, error) {
	var rec skillRecord
	err := s.db.WithContext(ctx).Where("(user_id = ? OR is_global = ?) AND (id = ? OR short_id = ?)", userID, true, id, id).First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreSkill(&rec), nil
}

// CreateSkill creates a new skill and increments the main revision.
func (s *CareerStore) CreateSkill(ctx context.Context, skill *core.Skill) error {
	if skill == nil {
		return core.ErrValidation
	}
	if skill.IsGlobal && skill.UserID != core.SystemUserID {
		return fmt.Errorf("%w: only system may create global skills", core.ErrForbidden)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(toSkillRecord(skill)).Error; err != nil {
			return err
		}
		if skill.UserID != core.SystemUserID {
			return incrementRevisionTx(tx, skill.UserID)
		}
		return nil
	})
}

// UpdateSkill modifies a skill and increments the main revision.
// System-seeded global skills cannot be modified.
func (s *CareerStore) UpdateSkill(ctx context.Context, skill *core.Skill) error {
	if skill == nil {
		return core.ErrValidation
	}
	var existing skillRecord
	if err := s.db.WithContext(ctx).Where("id = ?", skill.ID).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if existing.IsGlobal {
		return core.ErrGlobalSkillImmutable
	}
	if existing.UserID != skill.UserID {
		return core.ErrForbidden
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(toSkillRecord(skill)).Error; err != nil {
			return err
		}
		return incrementRevisionTx(tx, skill.UserID)
	})
}

// DeleteSkill deletes a skill and cleans up join tables before incrementing main revision.
// System-seeded global skills cannot be deleted.
func (s *CareerStore) DeleteSkill(ctx context.Context, userID, id string) error {
	var existing skillRecord
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
		return mapError(err)
	}
	if existing.IsGlobal {
		return core.ErrGlobalSkillImmutable
	}
	if existing.UserID != userID {
		return core.ErrForbidden
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM experience_skills WHERE skill_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM project_skills WHERE skill_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM fact_skills WHERE skill_id = ?", id).Error; err != nil {
			return err
		}

		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&skillRecord{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return core.ErrNotFound
		}
		return incrementRevisionTx(tx, userID)
	})
}

// ListExperiences retrieves all work experience records with associations.
func (s *CareerStore) ListExperiences(ctx context.Context, userID string) ([]core.WorkExperience, error) {
	var recs []workExperienceRecord
	if err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ?", userID).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreExperiences(recs), nil
}

// Experience retrieves a work experience record by either its canonical UUID or its compact short_id,
// preloading all associated facts and skills within the authenticated user's tenant boundary.
func (s *CareerStore) Experience(ctx context.Context, userID, id string) (*core.WorkExperience, error) {
	var rec workExperienceRecord
	err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).
		First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreExperience(&rec), nil
}

// isUUID checks whether a string represents a UUID (with or without a "skill-" or "sk-" prefix).
func isUUID(s string) bool {
	s = strings.TrimPrefix(s, "skill-")
	s = strings.TrimPrefix(s, "sk-")
	_, err := uuid.Parse(s)
	return err == nil
}

// resolveSkillsTx resolves raw skill identifiers or names against the user's skills within an active transaction.
func resolveSkillsTx(tx *gorm.DB, userID string, rawSkills []core.Skill) ([]core.Skill, error) {
	if len(rawSkills) == 0 {
		return []core.Skill{}, nil
	}

	resolved := make([]core.Skill, 0, len(rawSkills))
	seen := make(map[string]struct{}, len(rawSkills))

	for _, raw := range rawSkills {
		rawID := strings.TrimSpace(raw.ID)
		rawShortID := strings.TrimSpace(raw.ShortID)
		rawName := strings.TrimSpace(raw.Name)

		nameCandidate := rawName
		if nameCandidate == "" && rawID != "" && !isUUID(rawID) {
			nameCandidate = rawID
		}

		var found skillRecord

		if rawID != "" {
			err := tx.Where("(user_id = ? OR is_global = ?) AND id = ?", userID, true, rawID).First(&found).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}

		if found.ID == "" && rawShortID != "" {
			err := tx.Where("(user_id = ? OR is_global = ?) AND short_id = ?", userID, true, rawShortID).First(&found).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}

		if found.ID == "" && nameCandidate != "" {
			err := tx.Where("(user_id = ? OR is_global = ?) AND (LOWER(name) = LOWER(?) OR slug = LOWER(?))", userID, true, nameCandidate, nameCandidate).First(&found).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}

		if found.ID != "" {
			if _, exists := seen[found.ID]; !exists {
				seen[found.ID] = struct{}{}
				resolved = append(resolved, *toCoreSkill(&found))
			}
			continue
		}

		if nameCandidate == "" || isUUID(nameCandidate) {
			continue
		}

		newCategory := strings.TrimSpace(raw.Category)
		if newCategory == "" {
			newCategory = "General Skills"
		}

		newSkill := skillRecord{
			ID:              core.GenerateID("skill"),
			ShortID:         core.GenerateShortID("skill"),
			UserID:          userID,
			Name:            nameCandidate,
			Category:        newCategory,
			Proficiency:     raw.Proficiency,
			YearsExperience: raw.YearsExperience,
			Keywords:        raw.Keywords,
			Metadata:        raw.Metadata,
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		}

		if err := tx.Create(&newSkill).Error; err != nil {
			return nil, err
		}

		if _, exists := seen[newSkill.ID]; !exists {
			seen[newSkill.ID] = struct{}{}
			resolved = append(resolved, *toCoreSkill(&newSkill))
		}
	}

	return resolved, nil
}

// CreateExperience creates a work experience entry and increments main revision.
func (s *CareerStore) CreateExperience(ctx context.Context, exp *core.WorkExperience) error {
	if exp == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveSkillsTx(tx, exp.UserID, exp.Skills)
		if err != nil {
			return err
		}
		exp.Skills = resolved
		if err := tx.Create(toWorkExperienceRecord(exp)).Error; err != nil {
			return err
		}
		return incrementRevisionTx(tx, exp.UserID)
	})
}

// UpdateExperience saves modified experience fields and replaces skill associations.
func (s *CareerStore) UpdateExperience(ctx context.Context, exp *core.WorkExperience) error {
	if exp == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveSkillsTx(tx, exp.UserID, exp.Skills)
		if err != nil {
			return err
		}
		exp.Skills = resolved
		rec := toWorkExperienceRecord(exp)
		if err := tx.Session(&gorm.Session{FullSaveAssociations: false}).Save(rec).Error; err != nil {
			return err
		}
		if err := tx.Model(rec).Association("Skills").Replace(rec.Skills); err != nil {
			return err
		}
		return incrementRevisionTx(tx, exp.UserID)
	})
}

// DeleteExperience removes an experience, associated skills join rows, and owned facts.
func (s *CareerStore) DeleteExperience(ctx context.Context, userID, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&workExperienceRecord{ID: id}).Association("Skills").Clear(); err != nil {
			return err
		}
		if err := deleteOwnerFactsTx(tx, userID, id, core.OwnerTypeWorkExperience); err != nil {
			return err
		}

		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&workExperienceRecord{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return core.ErrNotFound
		}
		return incrementRevisionTx(tx, userID)
	})
}

// ListProjects returns all projects belonging to the specified user.
func (s *CareerStore) ListProjects(ctx context.Context, userID string) ([]core.Project, error) {
	var recs []projectRecord
	if err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ?", userID).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreProjects(recs), nil
}

// Project retrieves a project record by either its canonical UUID or its compact short_id,
// preloading all associated facts and skills within the authenticated user's tenant boundary.
func (s *CareerStore) Project(ctx context.Context, userID, id string) (*core.Project, error) {
	var rec projectRecord
	err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).
		First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreProject(&rec), nil
}

// CreateProject inserts a new project record and bumps main revision.
func (s *CareerStore) CreateProject(ctx context.Context, proj *core.Project) error {
	if proj == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveSkillsTx(tx, proj.UserID, proj.Skills)
		if err != nil {
			return err
		}
		proj.Skills = resolved
		if err := tx.Create(toProjectRecord(proj)).Error; err != nil {
			return err
		}
		return incrementRevisionTx(tx, proj.UserID)
	})
}

// UpdateProject saves modified project fields and updates skill associations.
func (s *CareerStore) UpdateProject(ctx context.Context, proj *core.Project) error {
	if proj == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveSkillsTx(tx, proj.UserID, proj.Skills)
		if err != nil {
			return err
		}
		proj.Skills = resolved
		rec := toProjectRecord(proj)
		if err := tx.Session(&gorm.Session{FullSaveAssociations: false}).Save(rec).Error; err != nil {
			return err
		}
		if err := tx.Model(rec).Association("Skills").Replace(rec.Skills); err != nil {
			return err
		}
		return incrementRevisionTx(tx, proj.UserID)
	})
}

// DeleteProject deletes a project, unlinks associated skills, and purges child facts.
func (s *CareerStore) DeleteProject(ctx context.Context, userID, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&projectRecord{ID: id}).Association("Skills").Clear(); err != nil {
			return err
		}
		if err := deleteOwnerFactsTx(tx, userID, id, core.OwnerTypeProject); err != nil {
			return err
		}

		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&projectRecord{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return core.ErrNotFound
		}
		return incrementRevisionTx(tx, userID)
	})
}

// ListEducation retrieves all education entries for the specified user.
func (s *CareerStore) ListEducation(ctx context.Context, userID string) ([]core.Education, error) {
	var recs []educationRecord
	if err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Where("user_id = ?", userID).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreEducationList(recs), nil
}

// Education retrieves an education record by either its canonical UUID or its compact short_id,
// preloading all associated facts within the authenticated user's tenant boundary.
func (s *CareerStore) Education(ctx context.Context, userID, id string) (*core.Education, error) {
	var rec educationRecord
	err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).
		First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreEducation(&rec), nil
}

// CreateEducation creates an education entry and increments main revision.
func (s *CareerStore) CreateEducation(ctx context.Context, edu *core.Education) error {
	if edu == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(toEducationRecord(edu)).Error; err != nil {
			return err
		}
		return incrementRevisionTx(tx, edu.UserID)
	})
}

// UpdateEducation saves updated education fields and increments main revision.
func (s *CareerStore) UpdateEducation(ctx context.Context, edu *core.Education) error {
	if edu == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{FullSaveAssociations: true}).Save(toEducationRecord(edu)).Error; err != nil {
			return err
		}
		return incrementRevisionTx(tx, edu.UserID)
	})
}

// DeleteEducation removes an education entry and its associated facts.
func (s *CareerStore) DeleteEducation(ctx context.Context, userID, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := deleteOwnerFactsTx(tx, userID, id, core.OwnerTypeEducation); err != nil {
			return err
		}

		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&educationRecord{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return core.ErrNotFound
		}
		return incrementRevisionTx(tx, userID)
	})
}

// ListCertifications returns all certifications registered under the user.
func (s *CareerStore) ListCertifications(ctx context.Context, userID string) ([]core.Certification, error) {
	var recs []certificationRecord
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreCertifications(recs), nil
}

// Certification retrieves a certification record by either its canonical UUID or its compact short_id
// within the authenticated user's tenant boundary.
func (s *CareerStore) Certification(ctx context.Context, userID, id string) (*core.Certification, error) {
	var rec certificationRecord
	err := s.db.WithContext(ctx).Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreCertification(&rec), nil
}

// CreateCertification inserts a certification entry and increments main revision.
func (s *CareerStore) CreateCertification(ctx context.Context, cert *core.Certification) error {
	if cert == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(toCertificationRecord(cert)).Error; err != nil {
			return err
		}
		return incrementRevisionTx(tx, cert.UserID)
	})
}

// UpdateCertification modifies certification attributes and bumps main revision.
func (s *CareerStore) UpdateCertification(ctx context.Context, cert *core.Certification) error {
	if cert == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(toCertificationRecord(cert)).Error; err != nil {
			return err
		}
		return incrementRevisionTx(tx, cert.UserID)
	})
}

// DeleteCertification removes a certification entry and increments main revision.
func (s *CareerStore) DeleteCertification(ctx context.Context, userID, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&certificationRecord{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return core.ErrNotFound
		}
		return incrementRevisionTx(tx, userID)
	})
}

// ListFactsByOwner retrieves all atomic facts belonging to a specific parent entity (work experience,
// project, or education).
func (s *CareerStore) ListFactsByOwner(ctx context.Context, userID, ownerType, ownerID string) ([]core.Fact, error) {
	var recs []factRecord
	targetOwnerID := ownerID
	switch ownerType {
	case string(core.OwnerTypeWorkExperience):
		if exp, err := s.Experience(ctx, userID, ownerID); err == nil && exp != nil {
			targetOwnerID = exp.ID
		}
	case string(core.OwnerTypeProject):
		if proj, err := s.Project(ctx, userID, ownerID); err == nil && proj != nil {
			targetOwnerID = proj.ID
		}
	case string(core.OwnerTypeEducation):
		if edu, err := s.Education(ctx, userID, ownerID); err == nil && edu != nil {
			targetOwnerID = edu.ID
		}
	}
	if err := s.db.WithContext(ctx).
		Preload("Skills").
		Where("user_id = ? AND owner_type = ? AND (owner_id = ? OR owner_id = ?)", userID, ownerType, targetOwnerID, ownerID).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreFacts(recs), nil
}

// Fact retrieves an atomic fact record by either its canonical UUID or its compact short_id,
// preloading all associated skills within the authenticated user's tenant boundary.
func (s *CareerStore) Fact(ctx context.Context, userID, id string) (*core.Fact, error) {
	var rec factRecord
	err := s.db.WithContext(ctx).
		Preload("Skills").
		Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).
		First(&rec).Error
	if err != nil {
		return nil, mapError(err)
	}
	return toCoreFact(&rec), nil
}

// CreateFact inserts a fact bullet point and increments main revision.
func (s *CareerStore) CreateFact(ctx context.Context, fact *core.Fact) error {
	if fact == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveSkillsTx(tx, fact.UserID, fact.Skills)
		if err != nil {
			return err
		}
		fact.Skills = resolved
		if err := tx.Create(toFactRecord(fact)).Error; err != nil {
			return err
		}
		return incrementRevisionTx(tx, fact.UserID)
	})
}

// UpdateFact updates fact statements, impact, or skill associations and increments main revision.
func (s *CareerStore) UpdateFact(ctx context.Context, fact *core.Fact) error {
	if fact == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := resolveSkillsTx(tx, fact.UserID, fact.Skills)
		if err != nil {
			return err
		}
		fact.Skills = resolved
		rec := toFactRecord(fact)
		if err := tx.Session(&gorm.Session{FullSaveAssociations: false}).Save(rec).Error; err != nil {
			return err
		}
		if err := tx.Model(rec).Association("Skills").Replace(rec.Skills); err != nil {
			return err
		}
		return incrementRevisionTx(tx, fact.UserID)
	})
}

// DeleteFact deletes a fact bullet, unlinks associated skills, and increments main revision.
func (s *CareerStore) DeleteFact(ctx context.Context, userID, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&factRecord{ID: id}).Association("Skills").Clear(); err != nil {
			return err
		}
		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&factRecord{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return core.ErrNotFound
		}
		return incrementRevisionTx(tx, userID)
	})
}

func deleteOwnerFactsTx(tx *gorm.DB, userID, ownerID string, ownerType core.OwnerType) error {
	var factIDs []string
	if err := tx.Model(&factRecord{}).Where("user_id = ? AND owner_id = ? AND owner_type = ?", userID, ownerID, ownerType).Pluck("id", &factIDs).Error; err != nil {
		return err
	}
	for _, fID := range factIDs {
		if err := tx.Model(&factRecord{ID: fID}).Association("Skills").Clear(); err != nil {
			return err
		}
	}
	return tx.Where("user_id = ? AND owner_id = ? AND owner_type = ?", userID, ownerID, ownerType).Delete(&factRecord{}).Error
}
