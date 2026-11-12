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

type quotaRepository struct {
	db *gorm.DB
}

// NewQuotaRepository constructs a SQLite-backed QuotaRepository.
func NewQuotaRepository(db *gorm.DB) core.QuotaRepository {
	return &quotaRepository{db: db}
}

// GetQuota retrieves the quota for a tenant, initializing defaults and performing lazy window resets.
func (r *quotaRepository) GetQuota(ctx context.Context, userID string) (*core.TenantQuota, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}

	var quota core.TenantQuota
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&quota).Error
	now := time.Now().UTC()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		quota = core.TenantQuota{
			UserID:             userID,
			DailyRequestsUsed:  0,
			DailyRequestLimit:  50,
			DailyWindowStart:   now,
			MonthlyTokensUsed:  0,
			MonthlyTokenLimit:  1000000,
			MonthlyWindowStart: now,
			UpdatedAt:          now,
		}
		if createErr := r.db.WithContext(ctx).Create(&quota).Error; createErr != nil {
			return nil, mapError(createErr)
		}
		return &quota, nil
	}
	if err != nil {
		return nil, mapError(err)
	}

	if quota.ResetWindowsIfExpired(now) {
		quota.UpdatedAt = now
		if saveErr := r.db.WithContext(ctx).Save(&quota).Error; saveErr != nil {
			return nil, mapError(saveErr)
		}
	}

	return &quota, nil
}

// ReserveQuota atomically verifies limits and reserves daily requests and estimated tokens.
func (r *quotaRepository) ReserveQuota(
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

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var quota core.TenantQuota
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&quota).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			dLimit := 50
			if dailyLimit > 0 {
				dLimit = dailyLimit
			}
			mLimit := 1000000
			if monthlyLimit > 0 {
				mLimit = monthlyLimit
			}
			quota = core.TenantQuota{
				UserID:             userID,
				DailyRequestsUsed:  0,
				DailyRequestLimit:  dLimit,
				DailyWindowStart:   now,
				MonthlyTokensUsed:  0,
				MonthlyTokenLimit:  mLimit,
				MonthlyWindowStart: now,
				UpdatedAt:          now,
			}
			if createErr := tx.Create(&quota).Error; createErr != nil {
				return createErr
			}
		} else if err != nil {
			return err
		}

		// Check window resets
		quota.ResetWindowsIfExpired(now)

		// Update limits if customized and not already set
		if dailyLimit > 0 && quota.DailyRequestLimit != dailyLimit {
			quota.DailyRequestLimit = dailyLimit
		}
		if monthlyLimit > 0 && quota.MonthlyTokenLimit != monthlyLimit {
			quota.MonthlyTokenLimit = monthlyLimit
		}

		// Check capacity
		if quota.DailyRequestsUsed+1 > quota.DailyRequestLimit {
			return core.ErrQuotaExceeded
		}
		if quota.MonthlyTokensUsed+estimatedTokens > quota.MonthlyTokenLimit {
			return core.ErrQuotaExceeded
		}

		quota.DailyRequestsUsed++
		quota.MonthlyTokensUsed += estimatedTokens
		quota.UpdatedAt = now

		if saveErr := tx.Save(&quota).Error; saveErr != nil {
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

		return tx.Create(&record).Error
	})

	if err != nil {
		if errors.Is(err, core.ErrQuotaExceeded) {
			return nil, core.ErrQuotaExceeded
		}
		return nil, mapError(err)
	}

	return &record, nil
}

// CommitUsage reconciles token usage and marks the reservation committed.
func (r *quotaRepository) CommitUsage(ctx context.Context, reservationID string, usage core.TokenUsage) error {
	if reservationID == "" {
		return fmt.Errorf("%w: reservation id cannot be empty", core.ErrValidation)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record core.LLMUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", reservationID).First(&record).Error; err != nil {
			return mapError(err)
		}

		if record.Status != core.ReservationStatusPending {
			return nil
		}

		var quota core.TenantQuota
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", record.UserID).First(&quota).Error; err == nil {
			delta := usage.TotalTokens - record.EstimatedTokens
			quota.AdjustMonthlyTokens(delta)
			quota.UpdatedAt = time.Now().UTC()
			if saveErr := tx.Save(&quota).Error; saveErr != nil {
				return saveErr
			}
		}

		now := time.Now().UTC()
		record.Status = core.ReservationStatusCommitted
		record.PromptTokens = usage.PromptTokens
		record.CompletionTokens = usage.CompletionTokens
		record.TotalTokens = usage.TotalTokens
		record.CommittedAt = &now

		return tx.Save(&record).Error
	})
}

// ReleaseQuota refunds the reservation and marks it released.
func (r *quotaRepository) ReleaseQuota(ctx context.Context, reservationID string, errMsg string) error {
	if reservationID == "" {
		return fmt.Errorf("%w: reservation id cannot be empty", core.ErrValidation)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record core.LLMUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", reservationID).First(&record).Error; err != nil {
			return mapError(err)
		}

		if record.Status != core.ReservationStatusPending {
			return nil
		}

		var quota core.TenantQuota
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", record.UserID).First(&quota).Error; err == nil {
			quota.ReleaseReservation(record.EstimatedTokens)
			quota.UpdatedAt = time.Now().UTC()
			if saveErr := tx.Save(&quota).Error; saveErr != nil {
				return saveErr
			}
		}

		record.Status = core.ReservationStatusReleased
		record.ErrorMessage = errMsg

		return tx.Save(&record).Error
	})
}

// ListUsageRecords returns recent LLM usage records for a tenant.
func (r *quotaRepository) ListUsageRecords(ctx context.Context, userID string, limit int) ([]core.LLMUsageRecord, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id cannot be empty", core.ErrValidation)
	}
	if limit <= 0 {
		limit = 50
	}

	var records []core.LLMUsageRecord
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&records).Error

	return records, mapError(err)
}
