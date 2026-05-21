// Package storage provides data access layer for sealos-notify
package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/database"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NotificationStore handles notification data operations
type NotificationStore struct {
	db     *gorm.DB
	logger *log.Entry
}

// NewNotificationStore creates a new notification store
func NewNotificationStore(db *gorm.DB, logger *log.Entry) *NotificationStore {
	if logger == nil {
		logger = log.WithField("component", "notification_store")
	}
	return &NotificationStore{db: db, logger: logger}
}

// Create creates a new notification. On idempotency_key conflict the existing record is loaded.
func (s *NotificationStore) Create(ctx context.Context, notification *database.Notification) error {
	if notification.ID == "" {
		notification.ID = uuid.New().String()
	}

	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(notification)

	if result.Error != nil {
		return fmt.Errorf("failed to create notification: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return s.GetByIdempotencyKey(ctx, notification.IdempotencyKey, notification)
	}

	return nil
}

// GetByID retrieves a notification by ID
func (s *NotificationStore) GetByID(ctx context.Context, id string) (*database.Notification, error) {
	notification := &database.Notification{}
	result := s.db.WithContext(ctx).First(notification, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("notification not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get notification: %w", result.Error)
	}
	return notification, nil
}

// GetByIdempotencyKey fills the provided notification struct with data matched by key
func (s *NotificationStore) GetByIdempotencyKey(ctx context.Context, key string, notification *database.Notification) error {
	result := s.db.WithContext(ctx).
		Where("idempotency_key = ?", key).
		First(notification)
	if result.Error != nil {
		return fmt.Errorf("failed to get notification by idempotency key: %w", result.Error)
	}
	return nil
}

// UpdateStatus updates the status of a notification
func (s *NotificationStore) UpdateStatus(ctx context.Context, id string, status database.NotificationStatus) error {
	result := s.db.WithContext(ctx).
		Model(&database.Notification{}).
		Where("id = ?", id).
		Update("status", string(status))
	if result.Error != nil {
		return fmt.Errorf("failed to update notification status: %w", result.Error)
	}
	return nil
}

// RefreshStatusFromDeliveryTasks recalculates a notification's aggregate status
// from its delivery tasks.
func (s *NotificationStore) RefreshStatusFromDeliveryTasks(ctx context.Context, notificationID string) error {
	var rows []struct {
		Status database.DeliveryTaskStatus `gorm:"column:status"`
		Count  int64                       `gorm:"column:count"`
	}

	result := s.db.WithContext(ctx).
		Model(&database.DeliveryTask{}).
		Select("status, COUNT(*) as count").
		Where("notification_id = ?", notificationID).
		Group("status").
		Scan(&rows)
	if result.Error != nil {
		return fmt.Errorf("failed to count delivery task statuses: %w", result.Error)
	}

	var total, active, dead, success int64
	for _, row := range rows {
		total += row.Count
		switch row.Status {
		case database.DeliveryTaskStatusPending, database.DeliveryTaskStatusProcessing, database.DeliveryTaskStatusFailed:
			active += row.Count
		case database.DeliveryTaskStatusDead:
			dead += row.Count
		case database.DeliveryTaskStatusSuccess:
			success += row.Count
		}
	}

	var status database.NotificationStatus
	switch {
	case total == 0:
		status = database.NotificationStatusPending
	case active > 0:
		status = database.NotificationStatusProcessing
	case dead > 0:
		status = database.NotificationStatusFailed
	case success == total:
		status = database.NotificationStatusSuccess
	default:
		status = database.NotificationStatusFailed
	}

	if err := s.UpdateStatus(ctx, notificationID, status); err != nil {
		return fmt.Errorf("failed to refresh notification status: %w", err)
	}
	return nil
}

// RecipientStore handles notification recipient operations
type RecipientStore struct {
	db     *gorm.DB
	logger *log.Entry
}

// NewRecipientStore creates a new recipient store
func NewRecipientStore(db *gorm.DB, logger *log.Entry) *RecipientStore {
	if logger == nil {
		logger = log.WithField("component", "recipient_store")
	}
	return &RecipientStore{db: db, logger: logger}
}

// Create creates notification recipients in a batch
func (s *RecipientStore) Create(ctx context.Context, recipients []*database.NotificationRecipient) error {
	if len(recipients) == 0 {
		return nil
	}

	for _, r := range recipients {
		if r.ID == "" {
			r.ID = uuid.New().String()
		}
	}

	result := s.db.WithContext(ctx).Create(recipients)
	if result.Error != nil {
		return fmt.Errorf("failed to create recipients: %w", result.Error)
	}
	return nil
}

// GetByID retrieves a recipient by ID
func (s *RecipientStore) GetByID(ctx context.Context, id string) (*database.NotificationRecipient, error) {
	recipient := &database.NotificationRecipient{}
	result := s.db.WithContext(ctx).First(recipient, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("recipient not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get recipient: %w", result.Error)
	}
	return recipient, nil
}

// GetByNotificationID retrieves all recipients for a notification
func (s *RecipientStore) GetByNotificationID(ctx context.Context, notificationID string) ([]*database.NotificationRecipient, error) {
	var recipients []*database.NotificationRecipient
	result := s.db.WithContext(ctx).
		Where("notification_id = ?", notificationID).
		Find(&recipients)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get recipients: %w", result.Error)
	}
	return recipients, nil
}
