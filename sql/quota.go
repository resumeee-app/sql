package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/resumeee-app/resumeee-core"
)

// tenantQuotaRecord maps core.TenantQuota to the relational tenant_quotas table.
type tenantQuotaRecord struct {
	UserID             string    `gorm:"primaryKey;type:text"`
	DailyRequestsUsed  int       `gorm:"not null"`
	DailyRequestLimit  int       `gorm:"not null"`
	DailyWindowStart   time.Time `gorm:"not null"`
	MonthlyTokensUsed  int       `gorm:"not null"`
	MonthlyTokenLimit  int       `gorm:"not null"`
	MonthlyWindowStart time.Time `gorm:"not null"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime:false"`
}

// TableName overrides the GORM table name for tenantQuotaRecord.
func (tenantQuotaRecord) TableName() string {
	return "tenant_quotas"
}

// toTenantQuotaRecord converts a domain TenantQuota into its relational record representation.
func toTenantQuotaRecord(q *core.TenantQuota) *tenantQuotaRecord {
	return &tenantQuotaRecord{
		UserID:             q.UserID,
		DailyRequestsUsed:  q.DailyRequestsUsed,
		DailyRequestLimit:  q.DailyRequestLimit,
		DailyWindowStart:   normalizeUTCTime(q.DailyWindowStart),
		MonthlyTokensUsed:  q.MonthlyTokensUsed,
		MonthlyTokenLimit:  q.MonthlyTokenLimit,
		MonthlyWindowStart: normalizeUTCTime(q.MonthlyWindowStart),
		UpdatedAt:          normalizeUTCTime(q.UpdatedAt),
	}
}

// toCoreTenantQuota converts a persisted tenantQuotaRecord into a domain TenantQuota.
func toCoreTenantQuota(r *tenantQuotaRecord) *core.TenantQuota {
	if r == nil {
		return nil
	}
	return &core.TenantQuota{
		UserID:             r.UserID,
		DailyRequestsUsed:  r.DailyRequestsUsed,
		DailyRequestLimit:  r.DailyRequestLimit,
		DailyWindowStart:   normalizeUTCTime(r.DailyWindowStart),
		MonthlyTokensUsed:  r.MonthlyTokensUsed,
		MonthlyTokenLimit:  r.MonthlyTokenLimit,
		MonthlyWindowStart: normalizeUTCTime(r.MonthlyWindowStart),
		UpdatedAt:          normalizeUTCTime(r.UpdatedAt),
	}
}

// llmUsageRecord maps core.LLMUsageRecord to the relational llm_usage_records table.
type llmUsageRecord struct {
	ID                 string                 `gorm:"primaryKey;type:text"`
	UserID             string                 `gorm:"index;not null"`
	Operation          core.LLMOperationType  `gorm:"not null"`
	Provider           string                 `gorm:"not null"`
	Model              string                 `gorm:"not null"`
	StatusCode         int                    `gorm:"not null"`
	Latency            int64                  `gorm:"column:latency_ns;not null"`
	EstimatedTokens    int                    `gorm:"not null"`
	PromptTokens       int                    `gorm:"not null"`
	CompletionTokens   int                    `gorm:"not null"`
	TotalTokens        int                    `gorm:"not null"`
	Status             core.ReservationStatus `gorm:"index;not null"`
	ErrorMessage       string                 `gorm:"column:error_message"`
	DailyWindowStart   time.Time              `gorm:"not null"`
	MonthlyWindowStart time.Time              `gorm:"not null"`
	CreatedAt          time.Time              `gorm:"index;autoCreateTime:false"`
	CommittedAt        *time.Time
}

// TableName overrides the GORM table name for llmUsageRecord.
func (llmUsageRecord) TableName() string {
	return "llm_usage_records"
}

// toLLMUsageRecord converts a domain LLMUsageRecord into its relational record representation.
func toLLMUsageRecord(rec *core.LLMUsageRecord) *llmUsageRecord {
	return &llmUsageRecord{
		ID:                 rec.ID,
		UserID:             rec.UserID,
		Operation:          rec.Operation,
		Provider:           rec.Provider,
		Model:              rec.Model,
		StatusCode:         rec.StatusCode,
		Latency:            int64(rec.Latency),
		EstimatedTokens:    rec.EstimatedTokens,
		PromptTokens:       rec.PromptTokens,
		CompletionTokens:   rec.CompletionTokens,
		TotalTokens:        rec.TotalTokens,
		Status:             rec.Status,
		ErrorMessage:       rec.ErrorMessage,
		DailyWindowStart:   normalizeUTCTime(rec.DailyWindowStart),
		MonthlyWindowStart: normalizeUTCTime(rec.MonthlyWindowStart),
		CreatedAt:          normalizeUTCTime(rec.CreatedAt),
		CommittedAt:        normalizeUTCTimePtr(rec.CommittedAt),
	}
}

// toCoreLLMUsageRecord converts a persisted llmUsageRecord into a domain LLMUsageRecord.
func toCoreLLMUsageRecord(r *llmUsageRecord) *core.LLMUsageRecord {
	if r == nil {
		return nil
	}
	return &core.LLMUsageRecord{
		ID:                 r.ID,
		UserID:             r.UserID,
		Operation:          r.Operation,
		Provider:           r.Provider,
		Model:              r.Model,
		StatusCode:         r.StatusCode,
		Latency:            time.Duration(r.Latency),
		EstimatedTokens:    r.EstimatedTokens,
		PromptTokens:       r.PromptTokens,
		CompletionTokens:   r.CompletionTokens,
		TotalTokens:        r.TotalTokens,
		Status:             r.Status,
		ErrorMessage:       r.ErrorMessage,
		DailyWindowStart:   normalizeUTCTime(r.DailyWindowStart),
		MonthlyWindowStart: normalizeUTCTime(r.MonthlyWindowStart),
		CreatedAt:          normalizeUTCTime(r.CreatedAt),
		CommittedAt:        normalizeUTCTimePtr(r.CommittedAt),
	}
}

// Quota loads quota state for userID.
func (r *readTx) Quota(ctx context.Context, userID string) (*core.TenantQuota, error) {
	var rec tenantQuotaRecord
	if err := r.session().Where("user_id = ?", userID).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreTenantQuota(&rec), nil
}

// UsageRecord loads an LLM usage record by ID.
func (r *readTx) UsageRecord(ctx context.Context, id string) (*core.LLMUsageRecord, error) {
	var rec llmUsageRecord
	if err := r.session().Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, mapError(err)
	}
	return toCoreLLMUsageRecord(&rec), nil
}

// ListUsageRecords returns usage records for userID ordered newest-first up to limit when limit > 0.
func (r *readTx) ListUsageRecords(ctx context.Context, userID string, limit int) ([]core.LLMUsageRecord, error) {
	q := r.session().Where("user_id = ?", userID).Order("created_at DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var recs []llmUsageRecord
	if err := q.Find(&recs).Error; err != nil {
		return nil, mapError(err)
	}
	out := make([]core.LLMUsageRecord, len(recs))
	for i := range recs {
		out[i] = *toCoreLLMUsageRecord(&recs[i])
	}
	return out, nil
}

// PutQuota inserts or replaces a tenant quota record.
func (w *writeTx) PutQuota(ctx context.Context, quota *core.TenantQuota) error {
	if quota == nil {
		return fmt.Errorf("%w: quota cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toTenantQuotaRecord(quota)).Error)
}

// PutUsageRecord inserts or replaces an LLM usage reservation/telemetry record.
func (w *writeTx) PutUsageRecord(ctx context.Context, rec *core.LLMUsageRecord) error {
	if rec == nil {
		return fmt.Errorf("%w: usage record cannot be nil", core.ErrValidation)
	}
	return mapError(w.session().Save(toLLMUsageRecord(rec)).Error)
}
