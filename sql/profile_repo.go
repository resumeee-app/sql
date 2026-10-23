package sqlite

import (
	"context"
	"errors"
	"time"

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

func (r *profileRepository) GetProfile(ctx context.Context, userID string) (*domain.Profile, error) {
	var prof domain.Profile
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&prof).Error; err != nil {
		return nil, mapError(err)
	}
	return &prof, nil
}

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

	return &domain.MasterProfileSnapshot{
		Profile:        prof,
		MasterRevision: prof.MasterRevision,
		Skills:         skills,
		Experiences:    exps,
		Projects:       projs,
		Education:      edus,
		Certifications: certs,
		Facts:          facts,
		ExportedAt:     time.Now().UTC(),
	}, nil
}

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

		// Re-insert skills
		for i := range snapshot.Skills {
			snapshot.Skills[i].UserID = userID
			if err := tx.Create(&snapshot.Skills[i]).Error; err != nil {
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

		// Re-insert certs
		for i := range snapshot.Certifications {
			snapshot.Certifications[i].UserID = userID
			if err := tx.Create(&snapshot.Certifications[i]).Error; err != nil {
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
func (r *profileRepository) ListSkills(ctx context.Context, userID string) ([]domain.Skill, error) {
	var skills []domain.Skill
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&skills).Error
	return skills, mapError(err)
}

func (r *profileRepository) GetSkill(ctx context.Context, userID, id string) (*domain.Skill, error) {
	var skill domain.Skill
	err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).First(&skill).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &skill, nil
}

func (r *profileRepository) CreateSkill(ctx context.Context, skill *domain.Skill) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(skill).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, skill.UserID)
	})
}

func (r *profileRepository) UpdateSkill(ctx context.Context, skill *domain.Skill) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(skill).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, skill.UserID)
	})
}

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

// Work Experience CRUD
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

func (r *profileRepository) GetExperience(ctx context.Context, userID, id string) (*domain.WorkExperience, error) {
	var exp domain.WorkExperience
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ? AND id = ?", userID, id).
		First(&exp).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &exp, nil
}

func (r *profileRepository) CreateExperience(ctx context.Context, exp *domain.WorkExperience) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(exp).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, exp.UserID)
	})
}

func (r *profileRepository) UpdateExperience(ctx context.Context, exp *domain.WorkExperience) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{FullSaveAssociations: true}).Save(exp).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, exp.UserID)
	})
}

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

// Projects CRUD
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

func (r *profileRepository) GetProject(ctx context.Context, userID, id string) (*domain.Project, error) {
	var proj domain.Project
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ? AND id = ?", userID, id).
		First(&proj).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &proj, nil
}

func (r *profileRepository) CreateProject(ctx context.Context, proj *domain.Project) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(proj).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, proj.UserID)
	})
}

func (r *profileRepository) UpdateProject(ctx context.Context, proj *domain.Project) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{FullSaveAssociations: true}).Save(proj).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, proj.UserID)
	})
}

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

// Education CRUD
func (r *profileRepository) ListEducation(ctx context.Context, userID string) ([]domain.Education, error) {
	var edus []domain.Education
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Where("user_id = ?", userID).
		Find(&edus).Error
	return edus, mapError(err)
}

func (r *profileRepository) GetEducation(ctx context.Context, userID, id string) (*domain.Education, error) {
	var edu domain.Education
	err := r.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Where("user_id = ? AND id = ?", userID, id).
		First(&edu).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &edu, nil
}

func (r *profileRepository) CreateEducation(ctx context.Context, edu *domain.Education) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(edu).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, edu.UserID)
	})
}

func (r *profileRepository) UpdateEducation(ctx context.Context, edu *domain.Education) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{FullSaveAssociations: true}).Save(edu).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, edu.UserID)
	})
}

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

// Certifications CRUD
func (r *profileRepository) ListCertifications(ctx context.Context, userID string) ([]domain.Certification, error) {
	var certs []domain.Certification
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&certs).Error
	return certs, mapError(err)
}

func (r *profileRepository) GetCertification(ctx context.Context, userID, id string) (*domain.Certification, error) {
	var cert domain.Certification
	err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).First(&cert).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &cert, nil
}

func (r *profileRepository) CreateCertification(ctx context.Context, cert *domain.Certification) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(cert).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, cert.UserID)
	})
}

func (r *profileRepository) UpdateCertification(ctx context.Context, cert *domain.Certification) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(cert).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, cert.UserID)
	})
}

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
func (r *profileRepository) ListFactsByOwner(ctx context.Context, userID, ownerType, ownerID string) ([]domain.Fact, error) {
	var facts []domain.Fact
	err := r.db.WithContext(ctx).
		Preload("Skills").
		Where("user_id = ? AND owner_type = ? AND owner_id = ?", userID, ownerType, ownerID).
		Find(&facts).Error
	return facts, mapError(err)
}

func (r *profileRepository) GetFact(ctx context.Context, userID, id string) (*domain.Fact, error) {
	var fact domain.Fact
	err := r.db.WithContext(ctx).
		Preload("Skills").
		Where("user_id = ? AND id = ?", userID, id).
		First(&fact).Error
	if err != nil {
		return nil, mapError(err)
	}
	return &fact, nil
}

func (r *profileRepository) CreateFact(ctx context.Context, fact *domain.Fact) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(fact).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, fact.UserID)
	})
}

func (r *profileRepository) UpdateFact(ctx context.Context, fact *domain.Fact) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{FullSaveAssociations: true}).Save(fact).Error; err != nil {
			return err
		}
		return r.incrementRevisionTx(tx, fact.UserID)
	})
}

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
