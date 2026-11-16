package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/core"
)

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

// Fact loads a fact record by ID with associated skills preloaded.
func (r *readTx) Fact(ctx context.Context, id string) (*core.Fact, error) {
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
