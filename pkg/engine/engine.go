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

// ChannelRequest specifies the template and render params for one channel.
// All recipients within a single notification receive identical rendered content.
//
// Example:
//
//	{
//	  "template": "feishu-incident",
//	  "params":   {"incident": "DB down", "severity": "P0"}
//	}
type ChannelRequest struct {
	// Template is the name of the template stored in the database (required)
	Template string `json:"template"`
	// Params are the key-value pairs injected into the template at render time
	Params map[string]string `json:"params,omitempty"`
}

// RecipientRequest represents a single notification recipient.
// Type identifies the address kind (e.g. "email", "phone", "feishu_user_id", "user_id").
// Value is the actual address for that type.
type RecipientRequest struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// SendNotificationRequest is the API request payload for sending a notification.
//
// channels maps each channel name to a ChannelRequest (template + render params).
// recipients is a list of {type, value} address entries. Each entry is matched to
// channels by type: a recipient is assigned to a channel when its type appears in
// that channel's valid identifier types.
//
// Example:
//
//	{
//	  "idempotencyKey": "incident-001",
//	  "channels": {
//	    "feishu_app": {"template": "feishu-incident", "params": {"incident": "DB down", "severity": "P0"}},
//	    "email":      {"template": "email-incident",  "params": {"incident": "DB down", "severity": "P0"}}
//	  },
//	  "recipients": [
//	    {"type": "feishu_user_id", "value": "ou_xxx"},
//	    {"type": "email",          "value": "alice@example.com"}
//	  ]
//	}
type SendNotificationRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	// Channels maps channel name → template + params
	Channels map[string]ChannelRequest `json:"channels"`
	// Recipients is a list of {type, value} address entries
	Recipients []RecipientRequest `json:"recipients"`
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
	notificationID := uuid.New().String()
	notification := &database.Notification{
		ID:             notificationID,
		IdempotencyKey: req.IdempotencyKey,
		Status:         database.NotificationStatusPending,
	}
	if err := e.notificationStore.Create(ctx, notification); err != nil {
		return nil, fmt.Errorf("failed to create notification: %w", err)
	}
	if notification.ID != notificationID {
		return &SendNotificationResponse{
			NotificationID: notification.ID,
			Status:         "accepted",
		}, nil
	}

	// Create one recipient record per address entry
	recipients := make([]*database.NotificationRecipient, 0, len(req.Recipients))
	for _, r := range req.Recipients {
		recipients = append(recipients, &database.NotificationRecipient{
			ID:             uuid.New().String(),
			NotificationID: notification.ID,
			Params:         database.JSONMap{"type": r.Type, "value": r.Value},
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
	for channelName, channelReq := range req.Channels {
		ch, ok := e.config.Channels[channelName]
		if !ok {
			return fmt.Errorf("channel %q not found in configuration", channelName)
		}
		if !ch.Enabled {
			return fmt.Errorf("channel %q is disabled", channelName)
		}
		if channelReq.Template == "" {
			return fmt.Errorf("channel %q: template name must not be empty", channelName)
		}

		// Verify template exists in DB and belongs to the correct channel
		tpl, err := e.templateStore.GetByName(ctx, channelReq.Template)
		if err != nil {
			return fmt.Errorf("channel %q: template %q not found", channelName, channelReq.Template)
		}
		if tpl.Channel != channelName {
			return fmt.Errorf("channel %q: template %q belongs to channel %q", channelName, channelReq.Template, tpl.Channel)
		}
	}

	return nil
}

// generateDeliveryTasks creates one task per (recipient, channel) pair where
// the recipient's params contain an identifier key compatible with the channel.
// Template params from the channel request are stored on the task itself.
func (e *Engine) generateDeliveryTasks(
	notificationID string,
	recipients []*database.NotificationRecipient,
	channels map[string]ChannelRequest,
) ([]*database.DeliveryTask, error) {
	var tasks []*database.DeliveryTask

	for _, recipient := range recipients {
		for channelName, channelReq := range channels {
			ch := e.config.Channels[channelName]
			if !ch.Enabled {
				continue
			}

			// Check if recipient type is valid for this channel
			recipientType, _ := recipient.Params["type"].(string)
			validTypes := adapter.RecipientIdentifierKeys(channelName)
			matched := false
			for _, t := range validTypes {
				if t == recipientType {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}

			// Convert template params to JSONMap
			tplParams := make(database.JSONMap, len(channelReq.Params))
			for k, v := range channelReq.Params {
				tplParams[k] = v
			}

			tasks = append(tasks, &database.DeliveryTask{
				ID:             uuid.New().String(),
				NotificationID: notificationID,
				RecipientID:    recipient.ID,
				Channel:        channelName,
				Provider:       ch.Provider,
				TemplateName:   channelReq.Template,
				TemplateParams: tplParams,
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
