package sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/resumeee-app/resumeee-core"
)

// tenantQuotaRecord is the GORM persistence model for core.TenantQuota.
type tenantQuotaRecord struct {
	UserID             string    `gorm:"primaryKey;type:text"`
	DailyRequestsUsed  int       `gorm:"not null;default:0"`
	DailyRequestLimit  int       `gorm:"not null;default:50"`
	DailyWindowStart   time.Time `gorm:"not null"`
	MonthlyTokensUsed  int       `gorm:"not null;default:0"`
	MonthlyTokenLimit  int       `gorm:"not null;default:1000000"`
	MonthlyWindowStart time.Time `gorm:"not null"`
	UpdatedAt          time.Time
}

// TableName overrides the GORM table name to tenant_quotas.
func (tenantQuotaRecord) TableName() string {
	return "tenant_quotas"
}

func toTenantQuotaRecord(q *core.TenantQuota) *tenantQuotaRecord {
	if q == nil {
		return nil
	}
	return &tenantQuotaRecord{
		UserID:             q.UserID,
		DailyRequestsUsed:  q.DailyRequestsUsed,
		DailyRequestLimit:  q.DailyRequestLimit,
		DailyWindowStart:   q.DailyWindowStart,
		MonthlyTokensUsed:  q.MonthlyTokensUsed,
		MonthlyTokenLimit:  q.MonthlyTokenLimit,
		MonthlyWindowStart: q.MonthlyWindowStart,
		UpdatedAt:          q.UpdatedAt,
	}
}

func toCoreTenantQuota(r *tenantQuotaRecord) *core.TenantQuota {
	if r == nil {
		return nil
	}
	return &core.TenantQuota{
		UserID:             r.UserID,
		DailyRequestsUsed:  r.DailyRequestsUsed,
		DailyRequestLimit:  r.DailyRequestLimit,
		DailyWindowStart:   r.DailyWindowStart,
		MonthlyTokensUsed:  r.MonthlyTokensUsed,
		MonthlyTokenLimit:  r.MonthlyTokenLimit,
		MonthlyWindowStart: r.MonthlyWindowStart,
		UpdatedAt:          r.UpdatedAt,
	}
}

// llmUsageRecord is the GORM persistence model for core.LLMUsageRecord.
type llmUsageRecord struct {
	ID         string                `gorm:"primaryKey;type:text"`
	UserID     string                `gorm:"index;not null"`
	Operation  core.LLMOperationType `gorm:"not null"`
	Provider   string                `gorm:"not null;default:''"`
	Model      string                `gorm:"not null;default:''"`
	StatusCode int                   `gorm:"not null;default:0"`
	// Latency persists the upstream round-trip duration as integer nanoseconds.
	Latency          time.Duration          `gorm:"column:latency_ns;not null;default:0"`
	EstimatedTokens  int                    `gorm:"not null"`
	PromptTokens     int                    `gorm:"not null;default:0"`
	CompletionTokens int                    `gorm:"not null;default:0"`
	TotalTokens      int                    `gorm:"not null;default:0"`
	Status           core.ReservationStatus `gorm:"index;not null;default:'pending'"`
	ErrorMessage     string
	CreatedAt        time.Time `gorm:"index"`
	CommittedAt      *time.Time
}

// TableName overrides the GORM table name to llm_usage_records.
func (llmUsageRecord) TableName() string {
	return "llm_usage_records"
}

func toLLMUsageRecord(u *core.LLMUsageRecord) *llmUsageRecord {
	if u == nil {
		return nil
	}
	return &llmUsageRecord{
		ID:               u.ID,
		UserID:           u.UserID,
		Operation:        u.Operation,
		Provider:         u.Provider,
		Model:            u.Model,
		StatusCode:       u.StatusCode,
		Latency:          u.Latency,
		EstimatedTokens:  u.EstimatedTokens,
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		Status:           u.Status,
		ErrorMessage:     u.ErrorMessage,
		CreatedAt:        u.CreatedAt,
		CommittedAt:      u.CommittedAt,
	}
}

func toCoreLLMUsageRecord(r *llmUsageRecord) *core.LLMUsageRecord {
	if r == nil {
		return nil
	}
	return &core.LLMUsageRecord{
		ID:               r.ID,
		UserID:           r.UserID,
		Operation:        r.Operation,
		Provider:         r.Provider,
		Model:            r.Model,
		StatusCode:       r.StatusCode,
		Latency:          r.Latency,
		EstimatedTokens:  r.EstimatedTokens,
		PromptTokens:     r.PromptTokens,
		CompletionTokens: r.CompletionTokens,
		TotalTokens:      r.TotalTokens,
		Status:           r.Status,
		ErrorMessage:     r.ErrorMessage,
		CreatedAt:        r.CreatedAt,
		CommittedAt:      r.CommittedAt,
	}
}

// QuotaStore implements core.QuotaStore backed by GORM.
type QuotaStore struct {
	db *gorm.DB
}

var _ core.QuotaStore = (*QuotaStore)(nil)

// NewQuotaStore constructs a SQL-backed QuotaStore.
func NewQuotaStore(db *gorm.DB) *QuotaStore {
	return &QuotaStore{db: db}
}

// Quota retrieves the quota for a tenant, initializing defaults and performing lazy window resets.
func (s *QuotaStore) Quota(ctx context.Context, userID string) (*core.TenantQuota, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}

	var rec tenantQuotaRecord
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&rec).Error
	now := time.Now().UTC()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		quota := &core.TenantQuota{
			UserID:             userID,
			DailyRequestsUsed:  0,
			DailyRequestLimit:  50,
			DailyWindowStart:   now,
			MonthlyTokensUsed:  0,
			MonthlyTokenLimit:  1000000,
			MonthlyWindowStart: now,
			UpdatedAt:          now,
		}
		if createErr := s.db.WithContext(ctx).Create(toTenantQuotaRecord(quota)).Error; createErr != nil {
			return nil, mapError(createErr)
		}
		return quota, nil
	}
	if err != nil {
		return nil, mapError(err)
	}

	quota := toCoreTenantQuota(&rec)
	if quota.ResetWindowsIfExpired(now) {
		quota.UpdatedAt = now
		if saveErr := s.db.WithContext(ctx).Save(toTenantQuotaRecord(quota)).Error; saveErr != nil {
			return nil, mapError(saveErr)
		}
	}

	return quota, nil
}

// ReserveQuota atomically verifies limits and reserves daily requests and estimated tokens.
func (s *QuotaStore) ReserveQuota(
	ctx context.Context,
	userID string,
	op core.LLMOperationType,
	estimatedTokens int,
	dailyLimit int,
	monthlyLimit int,
) (*core.LLMUsageRecord, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}

	var record core.LLMUsageRecord
	now := time.Now().UTC()

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var quotaRec tenantQuotaRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&quotaRec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			dLimit := 50
			if dailyLimit > 0 {
				dLimit = dailyLimit
			}
			mLimit := 1000000
			if monthlyLimit > 0 {
				mLimit = monthlyLimit
			}
			quotaRec = tenantQuotaRecord{
				UserID:             userID,
				DailyRequestsUsed:  0,
				DailyRequestLimit:  dLimit,
				DailyWindowStart:   now,
				MonthlyTokensUsed:  0,
				MonthlyTokenLimit:  mLimit,
				MonthlyWindowStart: now,
				UpdatedAt:          now,
			}
			if createErr := tx.Create(&quotaRec).Error; createErr != nil {
				return createErr
			}
		} else if err != nil {
			return err
		}

		quota := toCoreTenantQuota(&quotaRec)
		quota.ResetWindowsIfExpired(now)

		if dailyLimit > 0 && quota.DailyRequestLimit != dailyLimit {
			quota.DailyRequestLimit = dailyLimit
		}
		if monthlyLimit > 0 && quota.MonthlyTokenLimit != monthlyLimit {
			quota.MonthlyTokenLimit = monthlyLimit
		}

		if quota.DailyRequestsUsed+1 > quota.DailyRequestLimit {
			return core.ErrQuotaExceeded
		}
		if quota.MonthlyTokensUsed+estimatedTokens > quota.MonthlyTokenLimit {
			return core.ErrQuotaExceeded
		}

		quota.DailyRequestsUsed++
		quota.MonthlyTokensUsed += estimatedTokens
		quota.UpdatedAt = now

		if saveErr := tx.Save(toTenantQuotaRecord(quota)).Error; saveErr != nil {
			return saveErr
		}

		record = core.LLMUsageRecord{
			ID:              "res-" + uuid.New().String(),
			UserID:          userID,
			Operation:       op,
			EstimatedTokens: estimatedTokens,
			Status:          core.ReservationStatusPending,
			CreatedAt:       now,
		}

		return tx.Create(toLLMUsageRecord(&record)).Error
	})

	if err != nil {
		if errors.Is(err, core.ErrQuotaExceeded) {
			return nil, core.ErrQuotaExceeded
		}
		return nil, mapError(err)
	}

	return &record, nil
}

// applyUsageTelemetry copies pre-normalized provider telemetry and token counters onto rec.
func applyUsageTelemetry(rec *llmUsageRecord, normalized core.LLMTelemetry) {
	rec.Provider = normalized.Provider
	rec.Model = normalized.Model
	rec.StatusCode = normalized.StatusCode
	rec.Latency = normalized.Latency
	rec.PromptTokens = normalized.Usage.PromptTokens
	rec.CompletionTokens = normalized.Usage.CompletionTokens
	rec.TotalTokens = normalized.Usage.TotalTokens
}

// CommitUsage reconciles reported token usage against the reservation estimate,
// persists call telemetry, and marks the reservation committed.
func (s *QuotaStore) CommitUsage(ctx context.Context, reservationID string, telemetry core.LLMTelemetry) error {
	if reservationID == "" {
		return fmt.Errorf("%w: reservation id cannot be empty", core.ErrValidation)
	}
	normalized := telemetry.Normalize()

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec llmUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", reservationID).First(&rec).Error; err != nil {
			return mapError(err)
		}

		if rec.Status != core.ReservationStatusPending {
			return nil
		}

		var quotaRec tenantQuotaRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", rec.UserID).First(&quotaRec).Error; err == nil {
			quota := toCoreTenantQuota(&quotaRec)
			delta := normalized.Usage.TotalTokens - rec.EstimatedTokens
			quota.AdjustMonthlyTokens(delta)
			quota.UpdatedAt = time.Now().UTC()
			if saveErr := tx.Save(toTenantQuotaRecord(quota)).Error; saveErr != nil {
				return saveErr
			}
		}

		now := time.Now().UTC()
		rec.Status = core.ReservationStatusCommitted
		applyUsageTelemetry(&rec, normalized)
		rec.CommittedAt = &now

		return tx.Save(&rec).Error
	})
}

// ReleaseQuota refunds the reservation, records any upstream call telemetry, and marks it released.
func (s *QuotaStore) ReleaseQuota(ctx context.Context, reservationID string, errMsg string, telemetry core.LLMTelemetry) error {
	if reservationID == "" {
		return fmt.Errorf("%w: reservation id cannot be empty", core.ErrValidation)
	}
	normalized := telemetry.Normalize()

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec llmUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", reservationID).First(&rec).Error; err != nil {
			return mapError(err)
		}

		if rec.Status != core.ReservationStatusPending {
			return nil
		}

		var quotaRec tenantQuotaRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", rec.UserID).First(&quotaRec).Error; err == nil {
			quota := toCoreTenantQuota(&quotaRec)
			quota.ReleaseReservation(rec.EstimatedTokens)
			quota.UpdatedAt = time.Now().UTC()
			if saveErr := tx.Save(toTenantQuotaRecord(quota)).Error; saveErr != nil {
				return saveErr
			}
		}

		rec.Status = core.ReservationStatusReleased
		applyUsageTelemetry(&rec, normalized)
		rec.ErrorMessage = errMsg

		return tx.Save(&rec).Error
	})
}

// ListUsageRecords returns recent LLM usage records for a tenant.
func (s *QuotaStore) ListUsageRecords(ctx context.Context, userID string, limit int) ([]core.LLMUsageRecord, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}
	if limit <= 0 {
		limit = 50
	}

	var recs []llmUsageRecord
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}

	records := make([]core.LLMUsageRecord, len(recs))
	for i := range recs {
		records[i] = *toCoreLLMUsageRecord(&recs[i])
	}
	return records, nil
}
