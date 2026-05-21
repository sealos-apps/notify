// Package storage provides delivery task operations
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/database"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeliveryTaskStore handles delivery task operations
type DeliveryTaskStore struct {
	db     *gorm.DB
	logger *log.Entry
}

// NewDeliveryTaskStore creates a new delivery task store
func NewDeliveryTaskStore(db *gorm.DB, logger *log.Entry) *DeliveryTaskStore {
	if logger == nil {
		logger = log.WithField("component", "delivery_task_store")
	}
	return &DeliveryTaskStore{db: db, logger: logger}
}

// Create creates delivery tasks in a batch
func (s *DeliveryTaskStore) Create(ctx context.Context, tasks []*database.DeliveryTask) error {
	if len(tasks) == 0 {
		return nil
	}

	for _, task := range tasks {
		if task.ID == "" {
			task.ID = uuid.New().String()
		}
	}

	result := s.db.WithContext(ctx).Create(tasks)
	if result.Error != nil {
		return fmt.Errorf("failed to create delivery tasks: %w", result.Error)
	}
	return nil
}

// AcquirePendingTasks locks and claims pending tasks for processing
func (s *DeliveryTaskStore) AcquirePendingTasks(ctx context.Context, leaseOwner string, leaseTimeout time.Duration, limit int) ([]*database.DeliveryTask, error) {
	var tasks []*database.DeliveryTask

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ?", string(database.DeliveryTaskStatusPending)).
			Order("created_at ASC").
			Limit(limit).
			Find(&tasks)
		if result.Error != nil {
			return result.Error
		}
		if len(tasks) == 0 {
			return nil
		}

		ids := make([]string, len(tasks))
		for i, t := range tasks {
			ids[i] = t.ID
		}

		leaseExpireAt := time.Now().Add(leaseTimeout)
		return tx.Model(&database.DeliveryTask{}).
			Where("id IN ?", ids).
			Updates(map[string]interface{}{
				"status":          string(database.DeliveryTaskStatusProcessing),
				"lease_owner":     leaseOwner,
				"lease_expire_at": leaseExpireAt,
			}).Error
	})
	if err != nil {
		return nil, fmt.Errorf("failed to acquire pending tasks: %w", err)
	}
	return tasks, nil
}

// AcquireRetryTasks locks and claims failed tasks that are ready for retry
func (s *DeliveryTaskStore) AcquireRetryTasks(ctx context.Context, leaseOwner string, leaseTimeout time.Duration, limit int) ([]*database.DeliveryTask, error) {
	var tasks []*database.DeliveryTask

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?",
				string(database.DeliveryTaskStatusFailed), time.Now()).
			Order("next_retry_at ASC").
			Limit(limit).
			Find(&tasks)
		if result.Error != nil {
			return result.Error
		}
		if len(tasks) == 0 {
			return nil
		}

		ids := make([]string, len(tasks))
		for i, t := range tasks {
			ids[i] = t.ID
		}

		leaseExpireAt := time.Now().Add(leaseTimeout)
		return tx.Model(&database.DeliveryTask{}).
			Where("id IN ?", ids).
			Updates(map[string]interface{}{
				"status":          string(database.DeliveryTaskStatusProcessing),
				"lease_owner":     leaseOwner,
				"lease_expire_at": leaseExpireAt,
			}).Error
	})
	if err != nil {
		return nil, fmt.Errorf("failed to acquire retry tasks: %w", err)
	}
	return tasks, nil
}

// ExpireProcessingTasks marks expired processing tasks as failed or dead so they can flow through retry handling.
func (s *DeliveryTaskStore) ExpireProcessingTasks(ctx context.Context, retryBackoffSeconds []int, limit int) ([]string, int64, error) {
	if limit <= 0 {
		return nil, 0, nil
	}

	var tasks []*database.DeliveryTask
	notificationIDs := make(map[string]struct{})
	now := time.Now()
	var affected int64

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND lease_expire_at IS NOT NULL AND lease_expire_at <= ?",
				string(database.DeliveryTaskStatusProcessing), now).
			Order("lease_expire_at ASC").
			Limit(limit).
			Find(&tasks)
		if result.Error != nil {
			return result.Error
		}
		if len(tasks) == 0 {
			return nil
		}

		for _, task := range tasks {
			updates := map[string]interface{}{
				"retry_count":     gorm.Expr("retry_count + 1"),
				"last_error":      "processing lease expired",
				"lease_owner":     nil,
				"lease_expire_at": nil,
			}
			if task.RetryCount < task.MaxRetry {
				retryAfter := retryBackoffDuration(task.RetryCount, retryBackoffSeconds)
				updates["status"] = string(database.DeliveryTaskStatusFailed)
				updates["next_retry_at"] = now.Add(retryAfter)
			} else {
				updates["status"] = string(database.DeliveryTaskStatusDead)
				updates["next_retry_at"] = nil
			}

			result := tx.Model(&database.DeliveryTask{}).
				Where("id = ? AND status = ?", task.ID, string(database.DeliveryTaskStatusProcessing)).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				affected += result.RowsAffected
				notificationIDs[task.NotificationID] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to expire processing tasks: %w", err)
	}

	ids := make([]string, 0, len(notificationIDs))
	for id := range notificationIDs {
		ids = append(ids, id)
	}
	return ids, affected, nil
}

// UpdateSuccess marks a task as successfully completed and clears the lease
func (s *DeliveryTaskStore) UpdateSuccess(ctx context.Context, taskID string, leaseOwner string) error {
	result := s.db.WithContext(ctx).
		Model(&database.DeliveryTask{}).
		Where("id = ? AND lease_owner = ?", taskID, leaseOwner).
		Updates(map[string]interface{}{
			"status":          string(database.DeliveryTaskStatusSuccess),
			"lease_owner":     nil,
			"lease_expire_at": nil,
			"last_error":      nil,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to update task success: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("failed to update task success: %w", ErrLeaseNotHeld)
	}
	return nil
}

// UpdateFailure marks a task as failed, increments retry count, and schedules retry if applicable
func (s *DeliveryTaskStore) UpdateFailure(ctx context.Context, taskID string, leaseOwner string, errorMsg string, retryAfter *time.Duration) error {
	updates := map[string]interface{}{
		"retry_count":     gorm.Expr("retry_count + 1"),
		"last_error":      errorMsg,
		"lease_owner":     nil,
		"lease_expire_at": nil,
	}

	if retryAfter != nil {
		updates["status"] = string(database.DeliveryTaskStatusFailed)
		updates["next_retry_at"] = time.Now().Add(*retryAfter)
	} else {
		updates["status"] = string(database.DeliveryTaskStatusDead)
	}

	result := s.db.WithContext(ctx).
		Model(&database.DeliveryTask{}).
		Where("id = ? AND lease_owner = ?", taskID, leaseOwner).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to update task failure: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("failed to update task failure: %w", ErrLeaseNotHeld)
	}
	return nil
}

func retryBackoffDuration(retryCount int, retryBackoffSeconds []int) time.Duration {
	if len(retryBackoffSeconds) == 0 {
		return 0
	}
	backoffIndex := retryCount
	if backoffIndex >= len(retryBackoffSeconds) {
		backoffIndex = len(retryBackoffSeconds) - 1
	}
	return time.Duration(retryBackoffSeconds[backoffIndex]) * time.Second
}

// GetByNotificationID retrieves all delivery tasks for a notification
func (s *DeliveryTaskStore) GetByNotificationID(ctx context.Context, notificationID string) ([]*database.DeliveryTask, error) {
	var tasks []*database.DeliveryTask
	result := s.db.WithContext(ctx).
		Where("notification_id = ?", notificationID).
		Order("created_at ASC").
		Find(&tasks)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get delivery tasks: %w", result.Error)
	}
	return tasks, nil
}

// DeliveryAttemptStore handles delivery attempt operations
type DeliveryAttemptStore struct {
	db     *gorm.DB
	logger *log.Entry
}

// NewDeliveryAttemptStore creates a new delivery attempt store
func NewDeliveryAttemptStore(db *gorm.DB, logger *log.Entry) *DeliveryAttemptStore {
	if logger == nil {
		logger = log.WithField("component", "delivery_attempt_store")
	}
	return &DeliveryAttemptStore{db: db, logger: logger}
}

// Create creates a delivery attempt record
func (s *DeliveryAttemptStore) Create(ctx context.Context, attempt *database.DeliveryAttempt) error {
	if attempt.ID == "" {
		attempt.ID = uuid.New().String()
	}

	result := s.db.WithContext(ctx).Create(attempt)
	if result.Error != nil {
		return fmt.Errorf("failed to create delivery attempt: %w", result.Error)
	}
	return nil
}

// GetByTaskID retrieves all attempts for a task
func (s *DeliveryAttemptStore) GetByTaskID(ctx context.Context, taskID string) ([]*database.DeliveryAttempt, error) {
	var attempts []*database.DeliveryAttempt
	result := s.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("attempt_no ASC").
		Find(&attempts)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get delivery attempts: %w", result.Error)
	}
	return attempts, nil
}
