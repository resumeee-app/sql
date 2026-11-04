package sqlite

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"resumeee/internal/domain"
)

type profileRepository struct {
	db *gorm.DB
}

// NewProfileRepository constructs a SQLite-backed ProfileRepository.
func NewProfileRepository(db *gorm.DB) domain.ProfileRepository {
	return &profileRepository{db: db}
}

// incrementRevisionTx atomically increments the user's master revision within an active transaction.
func (r *profileRepository) incrementRevisionTx(tx *gorm.DB, userID string) error {
	var prof domain.Profile
	err := tx.Where("user_id = ?", userID).First(&prof).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			prof = domain.Profile{
				ID:             "profile-" + userID,
				UserID:         userID,
				FullName:       userID,
				MasterRevision: 1,
				CreatedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
			}
			return tx.Create(&prof).Error
		}
		return err
	}
	return tx.Model(&domain.Profile{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"master_revision": gorm.Expr("master_revision + 1"),
			"updated_at":      time.Now().UTC(),
		}).Error
}

// GetProfile retrieves the master profile record for the specified user.
func (r *profileRepository) GetProfile(ctx context.Context, userID string) (*domain.Profile, error) {
	var prof domain.Profile
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&prof).Error; err != nil {
		return nil, mapError(err)
	}
	return &prof, nil
}

// UpsertProfile inserts or updates a profile record, incrementing the master revision.
func (r *profileRepository) UpsertProfile(ctx context.Context, profile *domain.Profile) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing domain.Profile
		err := tx.Where("user_id = ?", profile.UserID).First(&existing).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if profile.MasterRevision == 0 {
					profile.MasterRevision = 1
				}
				profile.CreatedAt = time.Now().UTC()
				profile.UpdatedAt = time.Now().UTC()
				return tx.Create(profile).Error
			}
			return err
		}

		profile.MasterRevision = existing.MasterRevision + 1
		profile.UpdatedAt = time.Now().UTC()
		return tx.Model(&existing).Updates(profile).Error
	})
}

// IncrementMasterRevision bumps the profile master revision counter and returns the new revision number.
func (r *profileRepository) IncrementMasterRevision(ctx context.Context, userID string) (int64, error) {
	var newRev int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.incrementRevisionTx(tx, userID); err != nil {
			return err
		}
		var prof domain.Profile
		if err := tx.Where("user_id = ?", userID).First(&prof).Error; err != nil {
			return err
		}
		newRev = prof.MasterRevision
		return nil
	})
	return newRev, err
}

// GetSnapshot retrieves the complete hydrated career graph including skills, experiences, projects, education, certifications, and facts.
func (r *profileRepository) GetSnapshot(ctx context.Context, userID string) (*domain.MasterProfileSnapshot, error) {
	var prof domain.Profile
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&prof).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, mapError(err)
	}

	var skills []domain.Skill
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&skills).Error; err != nil {
		return nil, mapError(err)
	}

	var exps []domain.WorkExperience
	if err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ?", userID).
		Find(&exps).Error; err != nil {
		return nil, mapError(err)
	}

	var projs []domain.Project
	if err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ?", userID).
		Find(&projs).Error; err != nil {
		return nil, mapError(err)
	}

	var edus []domain.Education
	if err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Where("user_id = ?", userID).
		Find(&edus).Error; err != nil {
		return nil, mapError(err)
	}

	var certs []domain.Certification
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&certs).Error; err != nil {
		return nil, mapError(err)
	}

	var facts []domain.Fact
	if err := r.db.WithContext(ctx).Preload("Skills").Where("user_id = ?", userID).Find(&facts).Error; err != nil {
		return nil, mapError(err)
	}

	snap := &domain.MasterProfileSnapshot{
		Profile:        prof,
		MasterRevision: prof.MasterRevision,
		Skills:         skills,
		Experiences:    exps,
		Projects:       projs,
		Education:      edus,
		Certifications: certs,
		Facts:          facts,
		ExportedAt:     time.Now().UTC(),
	}
	return snap.ConsolidateSkills(), nil
}

// ReplaceCareerGraph atomically purges and re-inserts the full career graph from a snapshot.
func (r *profileRepository) ReplaceCareerGraph(ctx context.Context, userID string, snapshot *domain.MasterProfileSnapshot) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Clean up join tables first
		if err := tx.Exec("DELETE FROM fact_skills WHERE fact_id IN (SELECT id FROM facts WHERE user_id = ?)", userID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM experience_skills WHERE work_experience_id IN (SELECT id FROM work_experiences WHERE user_id = ?)", userID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM project_skills WHERE project_id IN (SELECT id FROM projects WHERE user_id = ?)", userID).Error; err != nil {
			return err
		}

		// Clean up entity records
		if err := tx.Where("user_id = ?", userID).Delete(&domain.Fact{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.WorkExperience{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.Project{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.Education{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.Certification{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&domain.Skill{}).Error; err != nil {
			return err
		}

		// Re-insert skills in batch using all consolidated skills across the snapshot
		allSkills := snapshot.AllSkills()
		if len(allSkills) > 0 {
			for i := range allSkills {
				allSkills[i].UserID = userID
			}
			if err := tx.CreateInBatches(allSkills, 100).Error; err != nil {
				return err
			}
		}

		// Re-insert experiences
		for i := range snapshot.Experiences {
			snapshot.Experiences[i].UserID = userID
			for j := range snapshot.Experiences[i].Facts {
				snapshot.Experiences[i].Facts[j].UserID = userID
			}
			if err := tx.Create(&snapshot.Experiences[i]).Error; err != nil {
				return err
			}
		}

		// Re-insert projects
		for i := range snapshot.Projects {
			snapshot.Projects[i].UserID = userID
			for j := range snapshot.Projects[i].Facts {
				snapshot.Projects[i].Facts[j].UserID = userID
			}
			if err := tx.Create(&snapshot.Projects[i]).Error; err != nil {
				return err
			}
		}

		// Re-insert education
		for i := range snapshot.Education {
			snapshot.Education[i].UserID = userID
			for j := range snapshot.Education[i].Facts {
				snapshot.Education[i].Facts[j].UserID = userID
			}
			if err := tx.Create(&snapshot.Education[i]).Error; err != nil {
				return err
			}
		}

		// Re-insert certs in batch
		if len(snapshot.Certifications) > 0 {
			for i := range snapshot.Certifications {
				snapshot.Certifications[i].UserID = userID
			}
			if err := tx.CreateInBatches(snapshot.Certifications, 100).Error; err != nil {
				return err
			}
		}

		// Re-insert standalone facts or facts not yet inserted, and link fact skills
		for i := range snapshot.Facts {
			snapshot.Facts[i].UserID = userID
			var count int64
			if err := tx.Model(&domain.Fact{}).Where("id = ?", snapshot.Facts[i].ID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := tx.Create(&snapshot.Facts[i]).Error; err != nil {
					return err
				}
			} else if len(snapshot.Facts[i].Skills) > 0 {
				if err := tx.Model(&domain.Fact{ID: snapshot.Facts[i].ID}).Association("Skills").Replace(snapshot.Facts[i].Skills); err != nil {
					return err
				}
			}
		}

		// Upsert profile
		snapProf := snapshot.Profile
		snapProf.UserID = userID
		snapProf.MasterRevision = snapshot.MasterRevision
		snapProf.UpdatedAt = time.Now().UTC()
		return tx.Save(&snapProf).Error
	})
}

// Skills CRUD
// ListSkills returns all skills registered for the specified user.
func (r *profileRepository) ListSkills(ctx context.Context, userID string) ([]domain.Skill, error) {
	var skills []domain.Skill
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&skills).Error
	return skills, mapError(err)
}

// GetSkill retrieves a skill record by either its canonical UUID or its compact short_id.
// It scopes the lookup strictly to the authenticated userID to enforce multi-tenant isolation.
//
// Parameters:
//   - ctx: Context for database cancellation and timeout propagation.
//   - userID: Tenant identifier scoping the query.
//   - id: Canonical UUID or compact short_id of the skill.
//
// Returns:
//   - Pointer to domain.Skill if found, or domain.ErrNotFound if no matching record exists.
func (r *profileRepository) GetSkill(ctx context.Context, userID, id string) (*domain.Skill, error) {
	var skill domain.Skill
	err := r.db.WithContext(ctx).Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).First(&skill).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &skill, nil
}

// CreateSkill creates a new skill and increments the master revision.
func (r *profileRepository) CreateSkill(ctx context.Context, skill *domain.Skill) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(skill).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, skill.UserID)
	})
}

// UpdateSkill modifies a skill and increments the master revision.
func (r *profileRepository) UpdateSkill(ctx context.Context, skill *domain.Skill) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(skill).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, skill.UserID)
	})
}

// DeleteSkill deletes a skill and cleanups join tables before incrementing master revision.
func (r *profileRepository) DeleteSkill(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Clean up join tables where skill is referenced
		if err := tx.Exec("DELETE FROM experience_skills WHERE skill_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM project_skills WHERE skill_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM fact_skills WHERE skill_id = ?", id).Error; err != nil {
			return err
		}

		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&domain.Skill{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return r.incrementRevisionTx(tx, userID)
	})
}

// ListExperiences retrieves all work experience records with associations.
func (r *profileRepository) ListExperiences(ctx context.Context, userID string) ([]domain.WorkExperience, error) {
	var exps []domain.WorkExperience
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ?", userID).
		Find(&exps).Error
	return exps, mapError(err)
}

// GetExperience retrieves a work experience record by either its canonical UUID or its compact short_id,
// preloading all associated facts and skills within the authenticated user's tenant boundary.
//
// Parameters:
//   - ctx: Context for database cancellation and timeout propagation.
//   - userID: Tenant identifier scoping the query.
//   - id: Canonical UUID or compact short_id of the experience.
//
// Returns:
//   - Pointer to domain.WorkExperience with preloaded relations, or domain.ErrNotFound.
func (r *profileRepository) GetExperience(ctx context.Context, userID, id string) (*domain.WorkExperience, error) {
	var exp domain.WorkExperience
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).
		First(&exp).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &exp, nil
}

// isUUID checks whether a string represents a UUID (with or without a "skill-" or "sk-" prefix).
func isUUID(s string) bool {
	s = strings.TrimPrefix(s, "skill-")
	s = strings.TrimPrefix(s, "sk-")
	_, err := uuid.Parse(s)
	return err == nil
}

// resolveSkillsTx resolves raw skill identifiers or names against the user's skills within an active transaction.
// If an existing skill matches by ID, ShortID, or case-insensitive Name for the specified user, that skill is used.
// If no existing skill matches and a valid skill name is provided, a new canonical Skill record is created
// for the user and persisted in the database.
func (r *profileRepository) resolveSkillsTx(tx *gorm.DB, userID string, rawSkills []domain.Skill) ([]domain.Skill, error) {
	if len(rawSkills) == 0 {
		return []domain.Skill{}, nil
	}

	resolved := make([]domain.Skill, 0, len(rawSkills))
	seen := make(map[string]struct{}, len(rawSkills))

	for _, raw := range rawSkills {
		rawID := strings.TrimSpace(raw.ID)
		rawShortID := strings.TrimSpace(raw.ShortID)
		rawName := strings.TrimSpace(raw.Name)

		// Determine skill name candidate if name was passed directly or passed in ID
		nameCandidate := rawName
		if nameCandidate == "" && rawID != "" && !isUUID(rawID) {
			nameCandidate = rawID
		}

		var found domain.Skill

		// 1. Match existing skill by ID
		if rawID != "" {
			err := tx.Where("user_id = ? AND id = ?", userID, rawID).First(&found).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}

		// 2. Match existing skill by ShortID
		if found.ID == "" && rawShortID != "" {
			err := tx.Where("user_id = ? AND short_id = ?", userID, rawShortID).First(&found).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}

		// 3. Match existing skill by case-insensitive Name
		if found.ID == "" && nameCandidate != "" {
			err := tx.Where("user_id = ? AND LOWER(name) = LOWER(?)", userID, nameCandidate).First(&found).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}

		// If found, deduplicate and attach
		if found.ID != "" {
			if _, exists := seen[found.ID]; !exists {
				seen[found.ID] = struct{}{}
				resolved = append(resolved, found)
			}
			continue
		}

		// If not found and name candidate is empty or is an unknown UUID, do not create a stub skill
		if nameCandidate == "" || isUUID(nameCandidate) {
			continue
		}

		// 4. Create new canonical Skill for this user
		newCategory := strings.TrimSpace(raw.Category)
		if newCategory == "" {
			newCategory = "General Skills"
		}

		newSkill := domain.Skill{
			ID:              domain.GenerateID("skill"),
			ShortID:         domain.GenerateShortID("skill"),
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
			resolved = append(resolved, newSkill)
		}
	}

	return resolved, nil
}

// CreateExperience creates a work experience entry and increments master revision.
func (r *profileRepository) CreateExperience(ctx context.Context, exp *domain.WorkExperience) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := r.resolveSkillsTx(tx, exp.UserID, exp.Skills)
		if err != nil {
			return err
		}
		exp.Skills = resolved
		if err := tx.Create(exp).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, exp.UserID)
	})
}

// UpdateExperience saves modified experience fields and replaces skill associations.
func (r *profileRepository) UpdateExperience(ctx context.Context, exp *domain.WorkExperience) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := r.resolveSkillsTx(tx, exp.UserID, exp.Skills)
		if err != nil {
			return err
		}
		exp.Skills = resolved
		if err := tx.Session(&gorm.Session{FullSaveAssociations: false}).Save(exp).Error; err != nil {
			return err
		}
		if err := tx.Model(exp).Association("Skills").Replace(exp.Skills); err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, exp.UserID)
	})
}

// DeleteExperience removes an experience, associated skills join rows, and owned facts.
func (r *profileRepository) DeleteExperience(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Clear many2many skills join table
		if err := tx.Model(&domain.WorkExperience{ID: id}).Association("Skills").Clear(); err != nil {
			return err
		}
		// Clear fact associations and delete facts
		var factIDs []string
		if err := tx.Model(&domain.Fact{}).Where("user_id = ? AND owner_id = ? AND owner_type = ?", userID, id, domain.OwnerTypeWorkExperience).Pluck("id", &factIDs).Error; err != nil {
			return err
		}
		for _, fID := range factIDs {
			if err := tx.Model(&domain.Fact{ID: fID}).Association("Skills").Clear(); err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ? AND owner_id = ? AND owner_type = ?", userID, id, domain.OwnerTypeWorkExperience).Delete(&domain.Fact{}).Error; err != nil {
			return err
		}

		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&domain.WorkExperience{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return r.incrementRevisionTx(tx, userID)
	})
}

// ListProjects returns all projects belonging to the specified user.
func (r *profileRepository) ListProjects(ctx context.Context, userID string) ([]domain.Project, error) {
	var projs []domain.Project
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ?", userID).
		Find(&projs).Error
	return projs, mapError(err)
}

// GetProject retrieves a project record by either its canonical UUID or its compact short_id,
// preloading all associated facts and skills within the authenticated user's tenant boundary.
//
// Parameters:
//   - ctx: Context for database cancellation and timeout propagation.
//   - userID: Tenant identifier scoping the query.
//   - id: Canonical UUID or compact short_id of the project.
//
// Returns:
//   - Pointer to domain.Project with preloaded relations, or domain.ErrNotFound.
func (r *profileRepository) GetProject(ctx context.Context, userID, id string) (*domain.Project, error) {
	var proj domain.Project
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).
		First(&proj).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &proj, nil
}

// CreateProject inserts a new project record and bumps master revision.
func (r *profileRepository) CreateProject(ctx context.Context, proj *domain.Project) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := r.resolveSkillsTx(tx, proj.UserID, proj.Skills)
		if err != nil {
			return err
		}
		proj.Skills = resolved
		if err := tx.Create(proj).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, proj.UserID)
	})
}

// UpdateProject saves modified project fields and updates skill associations.
func (r *profileRepository) UpdateProject(ctx context.Context, proj *domain.Project) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := r.resolveSkillsTx(tx, proj.UserID, proj.Skills)
		if err != nil {
			return err
		}
		proj.Skills = resolved
		if err := tx.Session(&gorm.Session{FullSaveAssociations: false}).Save(proj).Error; err != nil {
			return err
		}
		if err := tx.Model(proj).Association("Skills").Replace(proj.Skills); err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, proj.UserID)
	})
}

// DeleteProject deletes a project, unlinks associated skills, and purges child facts.
func (r *profileRepository) DeleteProject(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.Project{ID: id}).Association("Skills").Clear(); err != nil {
			return err
		}
		var factIDs []string
		if err := tx.Model(&domain.Fact{}).Where("user_id = ? AND owner_id = ? AND owner_type = ?", userID, id, domain.OwnerTypeProject).Pluck("id", &factIDs).Error; err != nil {
			return err
		}
		for _, fID := range factIDs {
			if err := tx.Model(&domain.Fact{ID: fID}).Association("Skills").Clear(); err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ? AND owner_id = ? AND owner_type = ?", userID, id, domain.OwnerTypeProject).Delete(&domain.Fact{}).Error; err != nil {
			return err
		}

		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&domain.Project{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return r.incrementRevisionTx(tx, userID)
	})
}

// ListEducation retrieves all education entries for the specified user.
func (r *profileRepository) ListEducation(ctx context.Context, userID string) ([]domain.Education, error) {
	var edus []domain.Education
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Where("user_id = ?", userID).
		Find(&edus).Error
	return edus, mapError(err)
}

// GetEducation retrieves an education record by either its canonical UUID or its compact short_id,
// preloading all associated facts within the authenticated user's tenant boundary.
//
// Parameters:
//   - ctx: Context for database cancellation and timeout propagation.
//   - userID: Tenant identifier scoping the query.
//   - id: Canonical UUID or compact short_id of the education credential.
//
// Returns:
//   - Pointer to domain.Education with preloaded facts, or domain.ErrNotFound.
func (r *profileRepository) GetEducation(ctx context.Context, userID, id string) (*domain.Education, error) {
	var edu domain.Education
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).
		First(&edu).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &edu, nil
}

// CreateEducation creates an education entry and increments master revision.
func (r *profileRepository) CreateEducation(ctx context.Context, edu *domain.Education) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(edu).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, edu.UserID)
	})
}

// UpdateEducation saves updated education fields and increments master revision.
func (r *profileRepository) UpdateEducation(ctx context.Context, edu *domain.Education) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{FullSaveAssociations: true}).Save(edu).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, edu.UserID)
	})
}

// DeleteEducation removes an education entry and its associated facts.
func (r *profileRepository) DeleteEducation(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var factIDs []string
		if err := tx.Model(&domain.Fact{}).Where("user_id = ? AND owner_id = ? AND owner_type = ?", userID, id, domain.OwnerTypeEducation).Pluck("id", &factIDs).Error; err != nil {
			return err
		}
		for _, fID := range factIDs {
			if err := tx.Model(&domain.Fact{ID: fID}).Association("Skills").Clear(); err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ? AND owner_id = ? AND owner_type = ?", userID, id, domain.OwnerTypeEducation).Delete(&domain.Fact{}).Error; err != nil {
			return err
		}

		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&domain.Education{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return r.incrementRevisionTx(tx, userID)
	})
}

// ListCertifications returns all certifications registered under the user.
func (r *profileRepository) ListCertifications(ctx context.Context, userID string) ([]domain.Certification, error) {
	var certs []domain.Certification
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&certs).Error
	return certs, mapError(err)
}

// GetCertification retrieves a certification record by either its canonical UUID or its compact short_id
// within the authenticated user's tenant boundary.
//
// Parameters:
//   - ctx: Context for database cancellation and timeout propagation.
//   - userID: Tenant identifier scoping the query.
//   - id: Canonical UUID or compact short_id of the certification.
//
// Returns:
//   - Pointer to domain.Certification if found, or domain.ErrNotFound.
func (r *profileRepository) GetCertification(ctx context.Context, userID, id string) (*domain.Certification, error) {
	var cert domain.Certification
	err := r.db.WithContext(ctx).Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).First(&cert).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &cert, nil
}

// CreateCertification inserts a certification entry and increments master revision.
func (r *profileRepository) CreateCertification(ctx context.Context, cert *domain.Certification) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(cert).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, cert.UserID)
	})
}

// UpdateCertification modifies certification attributes and bumps master revision.
func (r *profileRepository) UpdateCertification(ctx context.Context, cert *domain.Certification) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(cert).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, cert.UserID)
	})
}

// DeleteCertification removes a certification entry and increments master revision.
func (r *profileRepository) DeleteCertification(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&domain.Certification{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return r.incrementRevisionTx(tx, userID)
	})
}

// Facts CRUD
// ListFactsByOwner retrieves all atomic facts belonging to a specific parent entity (work experience,
// project, or education). If ownerID is provided as a short_id, it resolves the owner's canonical UUID
// to query the underlying foreign key relation reliably.
//
// Parameters:
//   - ctx: Context for database cancellation and timeout propagation.
//   - userID: Tenant identifier scoping the query.
//   - ownerType: Entity discriminator string ("work_experience", "project", "education").
//   - ownerID: Canonical UUID or short_id of the parent entity.
//
// Returns:
//   - Slice of domain.Fact records with preloaded skills, or an error.
func (r *profileRepository) ListFactsByOwner(ctx context.Context, userID, ownerType, ownerID string) ([]domain.Fact, error) {
	var facts []domain.Fact
	targetOwnerID := ownerID
	switch ownerType {
	case string(domain.OwnerTypeWorkExperience):
		if exp, err := r.GetExperience(ctx, userID, ownerID); err == nil && exp != nil {
			targetOwnerID = exp.ID
		}
	case string(domain.OwnerTypeProject):
		if proj, err := r.GetProject(ctx, userID, ownerID); err == nil && proj != nil {
			targetOwnerID = proj.ID
		}
	case string(domain.OwnerTypeEducation):
		if edu, err := r.GetEducation(ctx, userID, ownerID); err == nil && edu != nil {
			targetOwnerID = edu.ID
		}
	}
	err := r.db.WithContext(ctx).
		Preload("Skills").
		Where("user_id = ? AND owner_type = ? AND (owner_id = ? OR owner_id = ?)", userID, ownerType, targetOwnerID, ownerID).
		Find(&facts).Error
	return facts, mapError(err)
}

// GetFact retrieves an atomic fact record by either its canonical UUID or its compact short_id,
// preloading all associated skills within the authenticated user's tenant boundary.
//
// Parameters:
//   - ctx: Context for database cancellation and timeout propagation.
//   - userID: Tenant identifier scoping the query.
//   - id: Canonical UUID or compact short_id of the fact.
//
// Returns:
//   - Pointer to domain.Fact with preloaded skills, or domain.ErrNotFound.
func (r *profileRepository) GetFact(ctx context.Context, userID, id string) (*domain.Fact, error) {
	var fact domain.Fact
	err := r.db.WithContext(ctx).
		Preload("Skills").
		Where("user_id = ? AND (id = ? OR short_id = ?)", userID, id, id).
		First(&fact).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &fact, nil
}

// CreateFact inserts a fact bullet point and increments master revision.
func (r *profileRepository) CreateFact(ctx context.Context, fact *domain.Fact) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := r.resolveSkillsTx(tx, fact.UserID, fact.Skills)
		if err != nil {
			return err
		}
		fact.Skills = resolved
		if err := tx.Create(fact).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, fact.UserID)
	})
}

// UpdateFact updates fact statements, impact, or skill associations and increments master revision.
func (r *profileRepository) UpdateFact(ctx context.Context, fact *domain.Fact) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		resolved, err := r.resolveSkillsTx(tx, fact.UserID, fact.Skills)
		if err != nil {
			return err
		}
		fact.Skills = resolved
		if err := tx.Session(&gorm.Session{FullSaveAssociations: false}).Save(fact).Error; err != nil {
			return err
		}
		if err := tx.Model(fact).Association("Skills").Replace(fact.Skills); err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, fact.UserID)
	})
}

// DeleteFact deletes a fact bullet, unlinks associated skills, and increments master revision.
func (r *profileRepository) DeleteFact(ctx context.Context, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.Fact{ID: id}).Association("Skills").Clear(); err != nil {
			return err
		}
		res := tx.Where("user_id = ? AND id = ?", userID, id).Delete(&domain.Fact{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return r.incrementRevisionTx(tx, userID)
	})
}
