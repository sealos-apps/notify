// Package storage provides delivery task operations
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/database"
	log "github.com/sirupsen/logrus"
)

// DeliveryTaskStore handles delivery task operations
type DeliveryTaskStore struct {
	db     *database.DB
	logger *log.Entry
}

// NewDeliveryTaskStore creates a new delivery task store
func NewDeliveryTaskStore(db *database.DB, logger *log.Entry) *DeliveryTaskStore {
	if logger == nil {
		logger = log.WithField("component", "delivery_task_store")
	}

	return &DeliveryTaskStore{
		db:     db,
		logger: logger,
	}
}

// Create creates delivery tasks
func (s *DeliveryTaskStore) Create(ctx context.Context, tasks []*database.DeliveryTask) error {
	if len(tasks) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO delivery_tasks (
			id, notification_id, recipient_id, channel, provider,
			status, retry_count, max_retry
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	for _, task := range tasks {
		if task.ID == "" {
			task.ID = uuid.New().String()
		}

		_, err := tx.Exec(ctx, query,
			task.ID,
			task.NotificationID,
			task.RecipientID,
			task.Channel,
			task.Provider,
			task.Status,
			task.RetryCount,
			task.MaxRetry,
		)
		if err != nil {
			return fmt.Errorf("failed to create delivery task: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// AcquirePendingTasks acquires pending tasks for processing
func (s *DeliveryTaskStore) AcquirePendingTasks(ctx context.Context, leaseOwner string, leaseTimeout time.Duration, limit int) ([]*database.DeliveryTask, error) {
	query := `
		UPDATE delivery_tasks
		SET status = $1,
		    lease_owner = $2,
		    lease_expire_at = $3
		WHERE id IN (
			SELECT id FROM delivery_tasks
			WHERE status = $4
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $5
		)
		RETURNING id, notification_id, recipient_id, channel, provider,
		          status, retry_count, max_retry, next_retry_at, last_error,
		          lease_owner, lease_expire_at, created_at, updated_at
	`

	rows, err := s.db.Query(ctx, query,
		database.DeliveryTaskStatusProcessing,
		leaseOwner,
		time.Now().Add(leaseTimeout),
		database.DeliveryTaskStatusPending,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire pending tasks: %w", err)
	}
	defer rows.Close()

	return s.scanTasks(rows)
}

// AcquireRetryTasks acquires failed tasks that are ready for retry
func (s *DeliveryTaskStore) AcquireRetryTasks(ctx context.Context, leaseOwner string, leaseTimeout time.Duration, limit int) ([]*database.DeliveryTask, error) {
	query := `
		UPDATE delivery_tasks
		SET status = $1,
		    lease_owner = $2,
		    lease_expire_at = $3
		WHERE id IN (
			SELECT id FROM delivery_tasks
			WHERE status = $4
			  AND next_retry_at IS NOT NULL
			  AND next_retry_at <= $5
			ORDER BY next_retry_at
			FOR UPDATE SKIP LOCKED
			LIMIT $6
		)
		RETURNING id, notification_id, recipient_id, channel, provider,
		          status, retry_count, max_retry, next_retry_at, last_error,
		          lease_owner, lease_expire_at, created_at, updated_at
	`

	rows, err := s.db.Query(ctx, query,
		database.DeliveryTaskStatusProcessing,
		leaseOwner,
		time.Now().Add(leaseTimeout),
		database.DeliveryTaskStatusFailed,
		time.Now(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire retry tasks: %w", err)
	}
	defer rows.Close()

	return s.scanTasks(rows)
}

// UpdateSuccess marks a task as successfully completed
func (s *DeliveryTaskStore) UpdateSuccess(ctx context.Context, taskID string) error {
	query := `
		UPDATE delivery_tasks
		SET status = $1,
		    lease_owner = NULL,
		    lease_expire_at = NULL,
		    last_error = NULL
		WHERE id = $2
	`

	_, err := s.db.Exec(ctx, query, database.DeliveryTaskStatusSuccess, taskID)
	if err != nil {
		return fmt.Errorf("failed to update task success: %w", err)
	}

	return nil
}

// UpdateFailure marks a task as failed and schedules retry if applicable
func (s *DeliveryTaskStore) UpdateFailure(ctx context.Context, taskID string, errorMsg string, retryAfter *time.Duration) error {
	var query string
	var args []interface{}

	if retryAfter != nil {
		// Schedule retry
		nextRetryAt := time.Now().Add(*retryAfter)
		query = `
			UPDATE delivery_tasks
			SET status = $1,
			    retry_count = retry_count + 1,
			    last_error = $2,
			    next_retry_at = $3,
			    lease_owner = NULL,
			    lease_expire_at = NULL
			WHERE id = $4
		`
		args = []interface{}{database.DeliveryTaskStatusFailed, errorMsg, nextRetryAt, taskID}
	} else {
		// Mark as dead (no more retries)
		query = `
			UPDATE delivery_tasks
			SET status = $1,
			    retry_count = retry_count + 1,
			    last_error = $2,
			    lease_owner = NULL,
			    lease_expire_at = NULL
			WHERE id = $3
		`
		args = []interface{}{database.DeliveryTaskStatusDead, errorMsg, taskID}
	}

	_, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update task failure: %w", err)
	}

	return nil
}

// GetByNotificationID retrieves all delivery tasks for a notification
func (s *DeliveryTaskStore) GetByNotificationID(ctx context.Context, notificationID string) ([]*database.DeliveryTask, error) {
	query := `
		SELECT id, notification_id, recipient_id, channel, provider,
		       status, retry_count, max_retry, next_retry_at, last_error,
		       lease_owner, lease_expire_at, created_at, updated_at
		FROM delivery_tasks
		WHERE notification_id = $1
		ORDER BY created_at
	`

	rows, err := s.db.Query(ctx, query, notificationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get delivery tasks: %w", err)
	}
	defer rows.Close()

	return s.scanTasks(rows)
}

// scanTasks scans delivery tasks from query results
func (s *DeliveryTaskStore) scanTasks(rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close()
}) ([]*database.DeliveryTask, error) {
	var tasks []*database.DeliveryTask
	for rows.Next() {
		task := &database.DeliveryTask{}
		err := rows.Scan(
			&task.ID,
			&task.NotificationID,
			&task.RecipientID,
			&task.Channel,
			&task.Provider,
			&task.Status,
			&task.RetryCount,
			&task.MaxRetry,
			&task.NextRetryAt,
			&task.LastError,
			&task.LeaseOwner,
			&task.LeaseExpireAt,
			&task.CreatedAt,
			&task.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan delivery task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// DeliveryAttemptStore handles delivery attempt operations
type DeliveryAttemptStore struct {
	db     *database.DB
	logger *log.Entry
}

// NewDeliveryAttemptStore creates a new delivery attempt store
func NewDeliveryAttemptStore(db *database.DB, logger *log.Entry) *DeliveryAttemptStore {
	if logger == nil {
		logger = log.WithField("component", "delivery_attempt_store")
	}

	return &DeliveryAttemptStore{
		db:     db,
		logger: logger,
	}
}

// Create creates a delivery attempt record
func (s *DeliveryAttemptStore) Create(ctx context.Context, attempt *database.DeliveryAttempt) error {
	if attempt.ID == "" {
		attempt.ID = uuid.New().String()
	}

	query := `
		INSERT INTO delivery_attempts (
			id, task_id, attempt_no, request_payload, response_payload,
			result, error_message, started_at, finished_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := s.db.Exec(ctx, query,
		attempt.ID,
		attempt.TaskID,
		attempt.AttemptNo,
		attempt.RequestPayload,
		attempt.ResponsePayload,
		attempt.Result,
		attempt.ErrorMessage,
		attempt.StartedAt,
		attempt.FinishedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create delivery attempt: %w", err)
	}

	return nil
}

// GetByTaskID retrieves all attempts for a task
func (s *DeliveryAttemptStore) GetByTaskID(ctx context.Context, taskID string) ([]*database.DeliveryAttempt, error) {
	query := `
		SELECT id, task_id, attempt_no, request_payload, response_payload,
		       result, error_message, started_at, finished_at
		FROM delivery_attempts
		WHERE task_id = $1
		ORDER BY attempt_no
	`

	rows, err := s.db.Query(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get delivery attempts: %w", err)
	}
	defer rows.Close()

	var attempts []*database.DeliveryAttempt
	for rows.Next() {
		attempt := &database.DeliveryAttempt{}
		err := rows.Scan(
			&attempt.ID,
			&attempt.TaskID,
			&attempt.AttemptNo,
			&attempt.RequestPayload,
			&attempt.ResponsePayload,
			&attempt.Result,
			&attempt.ErrorMessage,
			&attempt.StartedAt,
			&attempt.FinishedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan delivery attempt: %w", err)
		}
		attempts = append(attempts, attempt)
	}

	return attempts, nil
}
