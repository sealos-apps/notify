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

// RecipientIdentifierKeys returns the params map keys that identify a recipient
// for the given channel. The first key found in a recipient's params is used as
// the delivery address.
func RecipientIdentifierKeys(channelName string) []string {
	switch channelName {
	case "email":
		return []string{"email"}
	case "sms", "voice":
		return []string{"phone"}
	case "inapp":
		return []string{"user_id"}
	case "feishu_app", "feishu_webhook":
		return []string{"feishu_user_id", "email"}
	default:
		return nil
	}
}

// SendRequest contains all information needed by an adapter to send one notification.
// Content fields are pre-rendered by the dispatcher before calling Send.
type SendRequest struct {
	// RecipientValue is the resolved delivery address (email, phone, open_id, etc.)
	RecipientValue string

	// Subject is the rendered email subject (empty for non-email channels)
	Subject string

	// Body is the rendered message body
	Body string

	// TemplateCode is the provider-side template identifier used by SMS / voice channels
	TemplateCode string

	// Variables contains the raw recipient params as strings, passed to SMS/voice providers
	Variables map[string]string

	// MsgType is the message format hint (e.g. "text", "post", "interactive" for Feishu)
	MsgType string

	// Metadata holds any extra per-send key-value data
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
