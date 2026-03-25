// Package feishu_app provides a Feishu (Lark) App notification adapter with urgent message support
package feishu_app

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/labring/sealos-notify/pkg/adapter"
)

// Config holds the configuration for the Feishu App adapter
type Config struct {
	// AppID is the Feishu/Lark application ID
	AppID string
	// AppSecret is the Feishu/Lark application secret
	AppSecret string
	// ReceiveIDType specifies how recipients are identified: open_id, user_id, union_id, email, chat_id
	ReceiveIDType string
	// MsgType is the message type: text, post, interactive
	MsgType string
	// UrgentType sets the urgent notification mode: app, sms, phone (empty = no urgent)
	UrgentType string
}

// Adapter implements the Feishu App notification channel with optional urgent delivery
type Adapter struct {
	config Config
	client *lark.Client
}

// New creates a new Feishu App adapter from a provider data map.
// Expected keys: appId, appSecret, receiveIdType, msgType, urgentType
func New(data map[string]interface{}) (*Adapter, error) {
	cfg := Config{
		AppID:         getString(data, "appId"),
		AppSecret:     getString(data, "appSecret"),
		ReceiveIDType: getString(data, "receiveIdType"),
		MsgType:       getString(data, "msgType"),
		UrgentType:    getString(data, "urgentType"),
	}

	if cfg.AppID == "" {
		return nil, fmt.Errorf("feishu_app: appId is required")
	}
	if cfg.AppSecret == "" {
		return nil, fmt.Errorf("feishu_app: appSecret is required")
	}
	if cfg.ReceiveIDType == "" {
		cfg.ReceiveIDType = "open_id"
	}
	if cfg.MsgType == "" {
		cfg.MsgType = "text"
	}

	client := lark.NewClient(cfg.AppID, cfg.AppSecret)
	return &Adapter{config: cfg, client: client}, nil
}

// Send sends a notification via the Feishu App channel.
// If UrgentType is configured, it also sends an urgent follow-up after the message is created.
func (a *Adapter) Send(ctx context.Context, req *adapter.SendRequest) (*adapter.SendResponse, error) {
	content, err := a.buildContent(req)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}

	msgReq := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(a.config.ReceiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(req.RecipientValue).
			MsgType(a.config.MsgType).
			Content(content).
			Build()).
		Build()

	resp, err := a.client.Im.V1.Message.Create(ctx, msgReq)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	if !resp.Success() {
		return &adapter.SendResponse{
			Success: false,
			Error:   fmt.Errorf("feishu create message error [%d]: %s", resp.Code, resp.Msg),
		}, nil
	}

	details := map[string]interface{}{}
	if resp.Data != nil && resp.Data.MessageId != nil {
		details["message_id"] = *resp.Data.MessageId

		// Send urgent notification if configured
		if a.config.UrgentType != "" {
			if urgentErr := a.sendUrgent(ctx, *resp.Data.MessageId, req.RecipientValue); urgentErr != nil {
				// Urgent notification failure is non-fatal — the message was already delivered
				details["urgent_error"] = urgentErr.Error()
			}
		}
	}

	return &adapter.SendResponse{Success: true, Details: details}, nil
}

// sendUrgent sends an urgent follow-up for an existing message
func (a *Adapter) sendUrgent(ctx context.Context, messageID string, userID string) error {
	receivers := larkim.NewUrgentReceiversBuilder().
		UserIdList([]string{userID}).
		Build()

	switch a.config.UrgentType {
	case "app":
		req := larkim.NewUrgentAppMessageReqBuilder().
			MessageId(messageID).
			UserIdType(a.config.ReceiveIDType).
			UrgentReceivers(receivers).
			Build()
		resp, err := a.client.Im.V1.Message.UrgentApp(ctx, req)
		if err != nil {
			return fmt.Errorf("urgent app request failed: %w", err)
		}
		if !resp.Success() {
			return fmt.Errorf("urgent app error [%d]: %s", resp.Code, resp.Msg)
		}

	case "sms":
		req := larkim.NewUrgentSmsMessageReqBuilder().
			MessageId(messageID).
			UserIdType(a.config.ReceiveIDType).
			UrgentReceivers(receivers).
			Build()
		resp, err := a.client.Im.V1.Message.UrgentSms(ctx, req)
		if err != nil {
			return fmt.Errorf("urgent sms request failed: %w", err)
		}
		if !resp.Success() {
			return fmt.Errorf("urgent sms error [%d]: %s", resp.Code, resp.Msg)
		}

	case "phone":
		req := larkim.NewUrgentPhoneMessageReqBuilder().
			MessageId(messageID).
			UserIdType(a.config.ReceiveIDType).
			UrgentReceivers(receivers).
			Build()
		resp, err := a.client.Im.V1.Message.UrgentPhone(ctx, req)
		if err != nil {
			return fmt.Errorf("urgent phone request failed: %w", err)
		}
		if !resp.Success() {
			return fmt.Errorf("urgent phone error [%d]: %s", resp.Code, resp.Msg)
		}

	default:
		return fmt.Errorf("unsupported urgentType: %s (supported: app, sms, phone)", a.config.UrgentType)
	}
	return nil
}

// buildContent builds the JSON message content for the configured message type
func (a *Adapter) buildContent(req *adapter.SendRequest) (string, error) {
	switch a.config.MsgType {
	case "text":
		b, err := json.Marshal(map[string]string{"text": req.Content})
		if err != nil {
			return "", fmt.Errorf("failed to marshal text content: %w", err)
		}
		return string(b), nil
	default:
		// For post/interactive/etc., Content is expected to already be valid JSON
		return req.Content, nil
	}
}

// Name returns the adapter name
func (a *Adapter) Name() string { return "feishu_app" }

// ChannelType returns the channel type
func (a *Adapter) ChannelType() adapter.ChannelType { return adapter.ChannelTypeFeishuApp }

// Validate validates the adapter configuration
func (a *Adapter) Validate() error {
	if a.config.AppID == "" {
		return fmt.Errorf("appId is required")
	}
	if a.config.AppSecret == "" {
		return fmt.Errorf("appSecret is required")
	}
	return nil
}

// getString safely extracts a string value from a map
func getString(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
