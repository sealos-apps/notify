// Package engine provides notification engine functionality
package engine

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/config"
	"github.com/labring/sealos-notify/pkg/database"
	"github.com/labring/sealos-notify/pkg/storage"
	log "github.com/sirupsen/logrus"
)

// Recipient represents a notification recipient
type Recipient struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// SendNotificationRequest represents a request to send a notification
type SendNotificationRequest struct {
	IdempotencyKey string                 `json:"idempotencyKey"` //nolint:gofmt
	Title          string                 `json:"title"`
	Content        string                 `json:"content"`
	Template       string                 `json:"template,omitempty"`
	Channels       []string               `json:"channels"`
	Recipients     []Recipient            `json:"recipients"`
	Variables      map[string]interface{} `json:"variables,omitempty"`
}

// SendNotificationResponse represents the response after sending a notification
type SendNotificationResponse struct {
	NotificationID string `json:"notificationId"`
	Status         string `json:"status"`
}

// Engine handles notification creation and task generation
type Engine struct {
	config            *config.GlobalConfig //nolint:golines,gofumpt,gofmt,gci
	notificationStore *storage.NotificationStore
	recipientStore    *storage.RecipientStore
	deliveryTaskStore *storage.DeliveryTaskStore
	logger            *log.Entry
}

// New creates a new notification engine
func New(
	cfg *config.GlobalConfig,
	notificationStore *storage.NotificationStore,
	recipientStore *storage.RecipientStore,
	deliveryTaskStore *storage.DeliveryTaskStore,
	logger *log.Entry,
) *Engine {
	if logger == nil {
		logger = log.WithField("component", "engine")
	}

	return &Engine{
		config:            cfg,
		notificationStore: notificationStore,
		recipientStore:    recipientStore,
		deliveryTaskStore: deliveryTaskStore,
		logger:            logger,
	}
}

// SendNotification creates a notification and generates delivery tasks
func (e *Engine) SendNotification(ctx context.Context, req *SendNotificationRequest) (*SendNotificationResponse, error) { //nolint:golines
	// Validate request
	if err := e.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Create notification
	notification := &database.Notification{
		ID:             uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		Title:          req.Title,
		Content:        req.Content,
		Template:       req.Template,
		VariablesJSON:  req.Variables,
		Status:         database.NotificationStatusPending,
	}

	if err := e.notificationStore.Create(ctx, notification); err != nil {
		return nil, fmt.Errorf("failed to create notification: %w", err)
	}

	// Create recipients
	recipients := make([]*database.NotificationRecipient, 0, len(req.Recipients))
	for _, r := range req.Recipients {
		recipient := &database.NotificationRecipient{
			ID:             uuid.New().String(),
			NotificationID: notification.ID,
			RecipientType:  r.Type,
			RecipientValue: r.Value,
		}
		recipients = append(recipients, recipient)
	}

	if err := e.recipientStore.Create(ctx, recipients); err != nil {
		return nil, fmt.Errorf("failed to create recipients: %w", err)
	}

	// Generate delivery tasks
	tasks, err := e.generateDeliveryTasks(notification.ID, recipients, req.Channels)
	if err != nil {
		return nil, fmt.Errorf("failed to generate delivery tasks: %w", err)
	}

	if err := e.deliveryTaskStore.Create(ctx, tasks); err != nil {
		return nil, fmt.Errorf("failed to create delivery tasks: %w", err)
	}

	e.logger.WithFields(log.Fields{
		"notification_id": notification.ID,
		"recipients":      len(recipients),
		"tasks":           len(tasks),
	}).Info("Notification created")

	return &SendNotificationResponse{
		NotificationID: notification.ID,
		Status:         "accepted",
	}, nil
}

// validateRequest validates the send notification request
func (e *Engine) validateRequest(req *SendNotificationRequest) error {
	if req.IdempotencyKey == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if req.Title == "" {
		return fmt.Errorf("title is required")
	}
	if req.Content == "" {
		return fmt.Errorf("content is required")
	}
	if len(req.Recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}
	if len(req.Channels) == 0 {
		return fmt.Errorf("at least one channel is required")
	}

	// Validate channels are enabled
	for _, channelName := range req.Channels {
		channel, ok := e.config.Channels[channelName]
		if !ok {
			return fmt.Errorf("channel not found: %s", channelName)
		}
		if !channel.Enabled {
			return fmt.Errorf("channel is disabled: %s", channelName)
		}
	}

	return nil
}

// generateDeliveryTasks generates delivery tasks for each recipient and channel combination
func (e *Engine) generateDeliveryTasks(
	notificationID string,
	recipients []*database.NotificationRecipient,
	channels []string,
) ([]*database.DeliveryTask, error) {
	var tasks []*database.DeliveryTask

	for _, recipient := range recipients {
		for _, channelName := range channels {
			channel := e.config.Channels[channelName]
			if !channel.Enabled {
				continue
			}

			// Match recipient type with channel
			if !e.isRecipientCompatibleWithChannel(recipient.RecipientType, channelName) {
				continue
			}

			task := &database.DeliveryTask{
				ID:             uuid.New().String(),
				NotificationID: notificationID,
				RecipientID:    recipient.ID,
				Channel:        channelName,
				Provider:       channel.Provider,
				Status:         database.DeliveryTaskStatusPending,
				RetryCount:     0,
				MaxRetry:       e.config.Defaults.MaxRetry,
			}
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

// isRecipientCompatibleWithChannel checks if a recipient type is compatible with a channel
func (e *Engine) isRecipientCompatibleWithChannel(recipientType string, channelName string) bool { //nolint:gofumpt
	switch channelName {
	case "email":
		return recipientType == "email"
	case "sms", "voice":
		return recipientType == "phone"
	case "inapp":
		return recipientType == "user_id"
	case "feishu_webhook", "feishu_app":
		return recipientType == "feishu_user_id" || recipientType == "email"
	default:
		return false
	}
}

// GetNotificationStatus retrieves notification status
func (e *Engine) GetNotificationStatus(ctx context.Context, notificationID string) (map[string]interface{}, error) { //nolint:gofmt
	notification, err := e.notificationStore.GetByID(ctx, notificationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification: %w", err)
	}

	tasks, err := e.deliveryTaskStore.GetByNotificationID(ctx, notificationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get delivery tasks: %w", err)
	}

	return map[string]interface{}{ //nolint:gofmt
		"id":         notification.ID,
		"status":     notification.Status,
		"title":      notification.Title,
		"created_at": notification.CreatedAt,
		"updated_at": notification.UpdatedAt,
		"tasks":      tasks,
	}, nil
}
