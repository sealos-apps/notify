// Package engine provides notification engine functionality
package engine

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/adapter"
	"github.com/labring/sealos-notify/pkg/config"
	"github.com/labring/sealos-notify/pkg/database"
	"github.com/labring/sealos-notify/pkg/storage"
	log "github.com/sirupsen/logrus"
)

// SendNotificationRequest is the API request payload for sending a notification.
//
// channels maps each channel name to the template name to use for that channel.
// recipients is a list of per-user KV maps. Each map must contain at least one
// channel identifier key (e.g. "email", "phone", "feishu_user_id", "user_id")
// and may contain any number of additional template variable keys.
//
// Example:
//
//	{
//	  "idempotencyKey": "incident-001",
//	  "channels": {"feishu_app": "feishu-incident", "email": "email-incident"},
//	  "recipients": [
//	    {"feishu_user_id": "ou_xxx", "email": "alice@example.com", "name": "Alice", "incident": "DB down"},
//	    {"feishu_user_id": "ou_yyy", "email": "bob@example.com",   "name": "Bob",   "incident": "DB down"}
//	  ]
//	}
type SendNotificationRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	// Channels maps channel name → template name
	Channels map[string]string `json:"channels"`
	// Recipients is a list of per-user param maps (identifiers + template variables)
	Recipients []map[string]string `json:"recipients"`
}

// SendNotificationResponse is the response returned after a notification is accepted
type SendNotificationResponse struct {
	NotificationID string `json:"notificationId"`
	Status         string `json:"status"`
}

// Engine handles notification creation and delivery task generation
type Engine struct {
	config            *config.GlobalConfig
	notificationStore *storage.NotificationStore
	recipientStore    *storage.RecipientStore
	deliveryTaskStore *storage.DeliveryTaskStore
	templateStore     *storage.TemplateStore
	logger            *log.Entry
}

// New creates a new notification engine
func New(
	cfg *config.GlobalConfig,
	notificationStore *storage.NotificationStore,
	recipientStore *storage.RecipientStore,
	deliveryTaskStore *storage.DeliveryTaskStore,
	templateStore *storage.TemplateStore,
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
		templateStore:     templateStore,
		logger:            logger,
	}
}

// SendNotification validates the request, creates notification records, recipients, and delivery tasks
func (e *Engine) SendNotification(ctx context.Context, req *SendNotificationRequest) (*SendNotificationResponse, error) {
	if err := e.validateRequest(ctx, req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Create notification record
	notification := &database.Notification{
		ID:             uuid.New().String(),
		IdempotencyKey: req.IdempotencyKey,
		Status:         database.NotificationStatusPending,
	}
	if err := e.notificationStore.Create(ctx, notification); err != nil {
		return nil, fmt.Errorf("failed to create notification: %w", err)
	}

	// Create one recipient record per user entry
	recipients := make([]*database.NotificationRecipient, 0, len(req.Recipients))
	for _, params := range req.Recipients {
		// Convert map[string]string → JSONMap
		jsonParams := make(database.JSONMap, len(params))
		for k, v := range params {
			jsonParams[k] = v
		}
		recipients = append(recipients, &database.NotificationRecipient{
			ID:             uuid.New().String(),
			NotificationID: notification.ID,
			Params:         jsonParams,
		})
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
func (e *Engine) validateRequest(ctx context.Context, req *SendNotificationRequest) error {
	if req.IdempotencyKey == "" {
		return fmt.Errorf("idempotencyKey is required")
	}
	if len(req.Channels) == 0 {
		return fmt.Errorf("at least one channel is required")
	}
	if len(req.Recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	// Validate each channel and its template
	for channelName, templateName := range req.Channels {
		ch, ok := e.config.Channels[channelName]
		if !ok {
			return fmt.Errorf("channel %q not found in configuration", channelName)
		}
		if !ch.Enabled {
			return fmt.Errorf("channel %q is disabled", channelName)
		}
		if templateName == "" {
			return fmt.Errorf("channel %q: template name must not be empty", channelName)
		}

		// Verify template exists in DB and belongs to the correct channel
		tpl, err := e.templateStore.GetByName(ctx, templateName)
		if err != nil {
			return fmt.Errorf("channel %q: template %q not found", channelName, templateName)
		}
		if tpl.Channel != channelName {
			return fmt.Errorf("channel %q: template %q belongs to channel %q", channelName, templateName, tpl.Channel)
		}
	}

	return nil
}

// generateDeliveryTasks creates one task per (recipient, channel) pair where
// the recipient's params contain an identifier key compatible with the channel.
func (e *Engine) generateDeliveryTasks(
	notificationID string,
	recipients []*database.NotificationRecipient,
	channels map[string]string,
) ([]*database.DeliveryTask, error) {
	var tasks []*database.DeliveryTask

	for _, recipient := range recipients {
		for channelName, templateName := range channels {
			ch := e.config.Channels[channelName]
			if !ch.Enabled {
				continue
			}

			// Check if the recipient has an identifier for this channel
			identifierKeys := adapter.RecipientIdentifierKeys(channelName)
			hasIdentifier := false
			for _, key := range identifierKeys {
				if _, ok := recipient.Params[key]; ok {
					hasIdentifier = true
					break
				}
			}
			if !hasIdentifier {
				continue
			}

			tasks = append(tasks, &database.DeliveryTask{
				ID:             uuid.New().String(),
				NotificationID: notificationID,
				RecipientID:    recipient.ID,
				Channel:        channelName,
				Provider:       ch.Provider,
				TemplateName:   templateName,
				Status:         database.DeliveryTaskStatusPending,
				RetryCount:     0,
				MaxRetry:       e.config.Defaults.MaxRetry,
			})
		}
	}

	return tasks, nil
}

// GetNotificationStatus retrieves notification status and its delivery tasks
func (e *Engine) GetNotificationStatus(ctx context.Context, notificationID string) (map[string]interface{}, error) {
	notification, err := e.notificationStore.GetByID(ctx, notificationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification: %w", err)
	}

	tasks, err := e.deliveryTaskStore.GetByNotificationID(ctx, notificationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get delivery tasks: %w", err)
	}

	return map[string]interface{}{
		"id":         notification.ID,
		"status":     notification.Status,
		"created_at": notification.CreatedAt,
		"updated_at": notification.UpdatedAt,
		"tasks":      tasks,
	}, nil
}
