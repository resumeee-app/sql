package sql

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/resumeee-app/resumeee-core"
)

// profileRecord is the GORM persistence model for core.Profile.
type profileRecord struct {
	ID                 string            `gorm:"primaryKey;type:text"`
	UserID             string            `gorm:"index;not null"`
	FullName           string            `gorm:"not null"`
	Headline           string            `gorm:"column:headline"`
	Email              string            `gorm:"column:email"`
	Phone              string            `gorm:"column:phone"`
	Location           core.Location     `gorm:"serializer:json"`
	Profiles           []core.SocialLink `gorm:"serializer:json"`
	Summary            string            `gorm:"column:summary"`
	GlobalWritingRules string            `gorm:"column:global_writing_rules"`
	MainRevision       int64             `gorm:"not null;default:1"`
	Metadata           map[string]any    `gorm:"serializer:json"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TableName overrides the GORM table name to profiles.
func (profileRecord) TableName() string {
	return "profiles"
}

func toProfileRecord(p *core.Profile) *profileRecord {
	if p == nil {
		return nil
	}
	return &profileRecord{
		ID:                 p.ID,
		UserID:             p.UserID,
		FullName:           p.FullName,
		Headline:           p.Headline,
		Email:              p.Email,
		Phone:              p.Phone,
		Location:           p.Location,
		Profiles:           p.Profiles,
		Summary:            p.Summary,
		GlobalWritingRules: p.GlobalWritingRules,
		MainRevision:       p.MainRevision,
		Metadata:           p.Metadata,
		CreatedAt:          p.CreatedAt,
		UpdatedAt:          p.UpdatedAt,
	}
}

func toCoreProfile(r *profileRecord) *core.Profile {
	if r == nil {
		return nil
	}
	return &core.Profile{
		ID:                 r.ID,
		UserID:             r.UserID,
		FullName:           r.FullName,
		Headline:           r.Headline,
		Email:              r.Email,
		Phone:              r.Phone,
		Location:           r.Location,
		Profiles:           r.Profiles,
		Summary:            r.Summary,
		GlobalWritingRules: r.GlobalWritingRules,
		MainRevision:       r.MainRevision,
		Metadata:           r.Metadata,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

// ProfileStore implements core.ProfileStore backed by GORM.
type ProfileStore struct {
	db *gorm.DB
}

var _ core.ProfileStore = (*ProfileStore)(nil)

// NewProfileStore constructs a SQL-backed ProfileStore.
func NewProfileStore(db *gorm.DB) *ProfileStore {
	return &ProfileStore{db: db}
}

// incrementRevisionTx atomically increments the user's main revision within an active transaction.
func incrementRevisionTx(tx *gorm.DB, userID string) error {
	var prof profileRecord
	err := tx.Where("user_id = ?", userID).First(&prof).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			prof = profileRecord{
				ID:           "profile-" + userID,
				UserID:       userID,
				FullName:     userID,
				MainRevision: 1,
				CreatedAt:    time.Now().UTC(),
				UpdatedAt:    time.Now().UTC(),
			}
			return tx.Create(&prof).Error
		}
		return err
	}
	return tx.Model(&profileRecord{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"main_revision": gorm.Expr("main_revision + 1"),
			"updated_at":    time.Now().UTC(),
		}).Error
}

// Profile retrieves the main profile record for the specified user.
func (s *ProfileStore) Profile(ctx context.Context, userID string) (*core.Profile, error) {
	var rec profileRecord
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreProfile(&rec), nil
}

// UpsertProfile inserts or updates a profile record, incrementing the main revision.
func (s *ProfileStore) UpsertProfile(ctx context.Context, profile *core.Profile) error {
	if profile == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing profileRecord
		err := tx.Where("user_id = ?", profile.UserID).First(&existing).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if profile.MainRevision == 0 {
					profile.MainRevision = 1
				}
				profile.CreatedAt = time.Now().UTC()
				profile.UpdatedAt = time.Now().UTC()
				return tx.Create(toProfileRecord(profile)).Error
			}
			return err
		}

		profile.MainRevision = existing.MainRevision + 1
		profile.UpdatedAt = time.Now().UTC()
		return tx.Model(&existing).Updates(toProfileRecord(profile)).Error
	})
}

// IncrementMainRevision bumps the profile main revision counter and returns the new revision number.
func (s *ProfileStore) IncrementMainRevision(ctx context.Context, userID string) (int64, error) {
	var newRev int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := incrementRevisionTx(tx, userID); err != nil {
			return err
		}
		var prof profileRecord
		if err := tx.Where("user_id = ?", userID).First(&prof).Error; err != nil {
			return err
		}
		newRev = prof.MainRevision
		return nil
	})
	return newRev, err
}

// Snapshot retrieves the complete hydrated career graph including skills, experiences, projects, education, certifications, and facts.
func (s *ProfileStore) Snapshot(ctx context.Context, userID string) (*core.MainProfileSnapshot, error) {
	var profRec profileRecord
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&profRec).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, mapError(err)
	}

	var skillRecs []skillRecord
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Find(&skillRecs).Error; err != nil {
		return nil, mapError(err)
	}

	var expRecs []workExperienceRecord
	if err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ?", userID).
		Find(&expRecs).Error; err != nil {
		return nil, mapError(err)
	}

	var projRecs []projectRecord
	if err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Preload("Skills").
		Where("user_id = ?", userID).
		Find(&projRecs).Error; err != nil {
		return nil, mapError(err)
	}

	var eduRecs []educationRecord
	if err := s.db.WithContext(ctx).
		Preload("Facts", "user_id = ?", userID).
		Preload("Facts.Skills").
		Where("user_id = ?", userID).
		Find(&eduRecs).Error; err != nil {
		return nil, mapError(err)
	}

	var certRecs []certificationRecord
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Find(&certRecs).Error; err != nil {
		return nil, mapError(err)
	}

	var factRecs []factRecord
	if err := s.db.WithContext(ctx).Preload("Skills").Where("user_id = ?", userID).Find(&factRecs).Error; err != nil {
		return nil, mapError(err)
	}

	snap := &core.MainProfileSnapshot{
		Profile:        *toCoreProfile(&profRec),
		MainRevision:   profRec.MainRevision,
		Skills:         toCoreSkills(skillRecs),
		Experiences:    toCoreExperiences(expRecs),
		Projects:       toCoreProjects(projRecs),
		Education:      toCoreEducationList(eduRecs),
		Certifications: toCoreCertifications(certRecs),
		Facts:          toCoreFacts(factRecs),
		ExportedAt:     time.Now().UTC(),
	}
	return snap.ConsolidateSkills(), nil
}

// ReplaceCareerGraph atomically purges and re-inserts the full career graph from a snapshot.
func (s *ProfileStore) ReplaceCareerGraph(ctx context.Context, userID string, snapshot *core.MainProfileSnapshot) error {
	if snapshot == nil {
		return core.ErrValidation
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if err := tx.Where("user_id = ?", userID).Delete(&factRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&workExperienceRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&projectRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&educationRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&certificationRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND is_global = ?", userID, false).Delete(&skillRecord{}).Error; err != nil {
			return err
		}

		// Re-insert custom skills in batch (exclude global skills)
		allSkills := snapshot.AllSkills()
		var customSkills []skillRecord
		for _, sk := range allSkills {
			if !sk.IsGlobal && sk.UserID != core.SystemUserID {
				sk.UserID = userID
				customSkills = append(customSkills, *toSkillRecord(&sk))
			}
		}
		if len(customSkills) > 0 {
			if err := tx.CreateInBatches(customSkills, 100).Error; err != nil {
				return err
			}
		}

		// Re-insert experiences
		for i := range snapshot.Experiences {
			snapshot.Experiences[i].UserID = userID
			for j := range snapshot.Experiences[i].Facts {
				snapshot.Experiences[i].Facts[j].UserID = userID
			}
			if err := tx.Create(toWorkExperienceRecord(&snapshot.Experiences[i])).Error; err != nil {
				return err
			}
		}

		// Re-insert projects
		for i := range snapshot.Projects {
			snapshot.Projects[i].UserID = userID
			for j := range snapshot.Projects[i].Facts {
				snapshot.Projects[i].Facts[j].UserID = userID
			}
			if err := tx.Create(toProjectRecord(&snapshot.Projects[i])).Error; err != nil {
				return err
			}
		}

		// Re-insert education
		for i := range snapshot.Education {
			snapshot.Education[i].UserID = userID
			for j := range snapshot.Education[i].Facts {
				snapshot.Education[i].Facts[j].UserID = userID
			}
			if err := tx.Create(toEducationRecord(&snapshot.Education[i])).Error; err != nil {
				return err
			}
		}

		// Re-insert certs in batch
		if len(snapshot.Certifications) > 0 {
			certRecs := make([]certificationRecord, len(snapshot.Certifications))
			for i := range snapshot.Certifications {
				snapshot.Certifications[i].UserID = userID
				certRecs[i] = *toCertificationRecord(&snapshot.Certifications[i])
			}
			if err := tx.CreateInBatches(certRecs, 100).Error; err != nil {
				return err
			}
		}

		// Re-insert standalone facts or facts not yet inserted, and link fact skills
		for i := range snapshot.Facts {
			snapshot.Facts[i].UserID = userID
			var count int64
			if err := tx.Model(&factRecord{}).Where("id = ?", snapshot.Facts[i].ID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if err := tx.Create(toFactRecord(&snapshot.Facts[i])).Error; err != nil {
					return err
				}
			} else if len(snapshot.Facts[i].Skills) > 0 {
				skillRecs := toSkillRecords(snapshot.Facts[i].Skills)
				if err := tx.Model(&factRecord{ID: snapshot.Facts[i].ID}).Association("Skills").Replace(skillRecs); err != nil {
					return err
				}
			}
		}

		// Upsert profile
		snapProf := snapshot.Profile
		snapProf.UserID = userID
		snapProf.MainRevision = snapshot.MainRevision
		snapProf.UpdatedAt = time.Now().UTC()
		return tx.Save(toProfileRecord(&snapProf)).Error
	})
}
