// Package storage provides data access layer for sealos-notify
package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/database"
	log "github.com/sirupsen/logrus"
)

// NotificationStore handles notification data operations
type NotificationStore struct {
	db     *database.DB
	logger *log.Entry
}

// NewNotificationStore creates a new notification store
func NewNotificationStore(db *database.DB, logger *log.Entry) *NotificationStore {
	if logger == nil {
		logger = log.WithField("component", "notification_store")
	}

	return &NotificationStore{
		db:     db,
		logger: logger,
	}
}

// Create creates a new notification
func (s *NotificationStore) Create(ctx context.Context, notification *database.Notification) error {
	if notification.ID == "" {
		notification.ID = uuid.New().String()
	}

	query := `
		INSERT INTO notifications (id, idempotency_key, title, content, template, variables_json, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id
	`

	err := s.db.QueryRow(ctx, query,
		notification.ID,
		notification.IdempotencyKey,
		notification.Title,
		notification.Content,
		notification.Template,
		notification.VariablesJSON,
		notification.Status,
	).Scan(&notification.ID)

	if err != nil {
		if database.IsNoRows(err) {
			// Idempotency key conflict, fetch existing notification
			return s.GetByIdempotencyKey(ctx, notification.IdempotencyKey, notification)
		}
		return fmt.Errorf("failed to create notification: %w", err)
	}

	return nil
}

// GetByID retrieves a notification by ID
func (s *NotificationStore) GetByID(ctx context.Context, id string) (*database.Notification, error) {
	notification := &database.Notification{}
	query := `
		SELECT id, idempotency_key, title, content, template, variables_json, status, created_at, updated_at
		FROM notifications
		WHERE id = $1
	`

	err := s.db.QueryRow(ctx, query, id).Scan(
		&notification.ID,
		&notification.IdempotencyKey,
		&notification.Title,
		&notification.Content,
		&notification.Template,
		&notification.VariablesJSON,
		&notification.Status,
		&notification.CreatedAt,
		&notification.UpdatedAt,
	)

	if err != nil {
		if database.IsNoRows(err) {
			return nil, fmt.Errorf("notification not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get notification: %w", err)
	}

	return notification, nil
}

// GetByIdempotencyKey retrieves a notification by idempotency key
func (s *NotificationStore) GetByIdempotencyKey(ctx context.Context, key string, notification *database.Notification) error {
	query := `
		SELECT id, idempotency_key, title, content, template, variables_json, status, created_at, updated_at
		FROM notifications
		WHERE idempotency_key = $1
	`

	err := s.db.QueryRow(ctx, query, key).Scan(
		&notification.ID,
		&notification.IdempotencyKey,
		&notification.Title,
		&notification.Content,
		&notification.Template,
		&notification.VariablesJSON,
		&notification.Status,
		&notification.CreatedAt,
		&notification.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to get notification by idempotency key: %w", err)
	}

	return nil
}

// UpdateStatus updates the status of a notification
func (s *NotificationStore) UpdateStatus(ctx context.Context, id string, status database.NotificationStatus) error {
	query := `
		UPDATE notifications
		SET status = $1
		WHERE id = $2
	`

	_, err := s.db.Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update notification status: %w", err)
	}

	return nil
}

// RecipientStore handles notification recipient operations
type RecipientStore struct {
	db     *database.DB
	logger *log.Entry
}

// NewRecipientStore creates a new recipient store
func NewRecipientStore(db *database.DB, logger *log.Entry) *RecipientStore {
	if logger == nil {
		logger = log.WithField("component", "recipient_store")
	}

	return &RecipientStore{
		db:     db,
		logger: logger,
	}
}

// Create creates notification recipients
func (s *RecipientStore) Create(ctx context.Context, recipients []*database.NotificationRecipient) error {
	if len(recipients) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO notification_recipients (id, notification_id, recipient_type, recipient_value)
		VALUES ($1, $2, $3, $4)
	`

	for _, recipient := range recipients {
		if recipient.ID == "" {
			recipient.ID = uuid.New().String()
		}

		_, err := tx.Exec(ctx, query,
			recipient.ID,
			recipient.NotificationID,
			recipient.RecipientType,
			recipient.RecipientValue,
		)
		if err != nil {
			return fmt.Errorf("failed to create recipient: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetByNotificationID retrieves all recipients for a notification
func (s *RecipientStore) GetByNotificationID(ctx context.Context, notificationID string) ([]*database.NotificationRecipient, error) {
	query := `
		SELECT id, notification_id, recipient_type, recipient_value, created_at
		FROM notification_recipients
		WHERE notification_id = $1
	`

	rows, err := s.db.Query(ctx, query, notificationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recipients: %w", err)
	}
	defer rows.Close()

	var recipients []*database.NotificationRecipient
	for rows.Next() {
		recipient := &database.NotificationRecipient{}
		err := rows.Scan(
			&recipient.ID,
			&recipient.NotificationID,
			&recipient.RecipientType,
			&recipient.RecipientValue,
			&recipient.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}

	return recipients, nil
}
