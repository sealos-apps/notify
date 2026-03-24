// Package adapter defines channel adapter interfaces for sealos-notify
package adapter

import (
	"context"
)

// ChannelType represents a notification channel type
type ChannelType string

const (
	ChannelTypeInApp         ChannelType = "inapp"
	ChannelTypeEmail         ChannelType = "email"
	ChannelTypeSMS           ChannelType = "sms"
	ChannelTypeVoice         ChannelType = "voice"
	ChannelTypeFeishuWebhook ChannelType = "feishu_webhook"
	ChannelTypeFeishuApp     ChannelType = "feishu_app"
)

// SendRequest contains the information needed to send a notification
type SendRequest struct {
	// Recipient information
	RecipientType  string
	RecipientValue string

	// Message content
	Title   string
	Content string
	Subject string

	// Template information
	TemplateCode string
	Variables    map[string]interface{}

	// Additional metadata
	Metadata map[string]string
}

// SendResponse contains the result of sending a notification
type SendResponse struct {
	Success bool
	Error   error
	Details map[string]interface{}
}

// Adapter defines the interface for notification channel adapters
type Adapter interface {
	// Send sends a notification through this channel
	Send(ctx context.Context, request *SendRequest) (*SendResponse, error)

	// Name returns the name of this adapter
	Name() string

	// ChannelType returns the channel type
	ChannelType() ChannelType

	// Validate validates the adapter configuration
	Validate() error
}

// Factory creates adapters based on configuration
type Factory interface {
	// CreateAdapter creates an adapter instance
	CreateAdapter(providerType string, config map[string]interface{}) (Adapter, error)
}
