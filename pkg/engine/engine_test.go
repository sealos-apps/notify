package engine

import (
	"testing"

	"github.com/labring/sealos-notify/pkg/config"
	"github.com/labring/sealos-notify/pkg/database"
)

func TestGenerateDeliveryTasksMatchesRecipientTypesToChannels(t *testing.T) {
	e := &Engine{
		config: &config.GlobalConfig{
			Defaults: config.DefaultsConfig{MaxRetry: 5},
			Channels: map[string]config.ChannelConfig{
				"email":      {Enabled: true, Provider: "smtp"},
				"sms":        {Enabled: true, Provider: "sms-provider"},
				"feishu_app": {Enabled: true, Provider: "feishu"},
				"voice":      {Enabled: false, Provider: "voice-provider"},
			},
		},
	}
	recipients := []*database.NotificationRecipient{
		{ID: "recipient-email", Params: database.JSONMap{"type": "email", "value": "alice@example.com"}},
		{ID: "recipient-phone", Params: database.JSONMap{"type": "phone", "value": "+12025550123"}},
		{ID: "recipient-feishu", Params: database.JSONMap{"type": "feishu_user_id", "value": "ou_xxx"}},
		{ID: "recipient-user", Params: database.JSONMap{"type": "user_id", "value": "user-1"}},
	}
	channels := map[string]ChannelRequest{
		"email":      {Template: "email-template", Params: map[string]string{"incident": "database"}},
		"sms":        {Template: "sms-template", Params: map[string]string{"incident": "database"}},
		"feishu_app": {Template: "feishu-template", Params: map[string]string{"incident": "database"}},
		"voice":      {Template: "voice-template", Params: map[string]string{"incident": "database"}},
	}

	tasks, err := e.generateDeliveryTasks("notification-1", recipients, channels)
	if err != nil {
		t.Fatalf("generateDeliveryTasks returned error: %v", err)
	}

	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d: %#v", len(tasks), tasks)
	}

	assertTask := func(recipientID, channel, provider, template string) {
		t.Helper()
		for _, task := range tasks {
			if task.RecipientID == recipientID && task.Channel == channel {
				if task.Provider != provider {
					t.Fatalf("task provider = %q, want %q", task.Provider, provider)
				}
				if task.TemplateName != template {
					t.Fatalf("task template = %q, want %q", task.TemplateName, template)
				}
				if task.Status != database.DeliveryTaskStatusPending {
					t.Fatalf("task status = %q, want pending", task.Status)
				}
				if task.MaxRetry != 5 {
					t.Fatalf("task MaxRetry = %d, want 5", task.MaxRetry)
				}
				if task.TemplateParams["incident"] != "database" {
					t.Fatalf("task TemplateParams = %#v", task.TemplateParams)
				}
				return
			}
		}
		t.Fatalf("missing task for recipient %q channel %q", recipientID, channel)
	}

	assertTask("recipient-email", "email", "smtp", "email-template")
	assertTask("recipient-email", "feishu_app", "feishu", "feishu-template")
	assertTask("recipient-phone", "sms", "sms-provider", "sms-template")
	assertTask("recipient-feishu", "feishu_app", "feishu", "feishu-template")
}

func TestGenerateDeliveryTasksSkipsUnsupportedRecipientTypes(t *testing.T) {
	e := &Engine{
		config: &config.GlobalConfig{
			Channels: map[string]config.ChannelConfig{
				"email": {Enabled: true, Provider: "smtp"},
			},
		},
	}

	tasks, err := e.generateDeliveryTasks(
		"notification-1",
		[]*database.NotificationRecipient{
			{ID: "recipient-phone", Params: database.JSONMap{"type": "phone", "value": "+12025550123"}},
		},
		map[string]ChannelRequest{"email": {Template: "email-template"}},
	)
	if err != nil {
		t.Fatalf("generateDeliveryTasks returned error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks, got %#v", tasks)
	}
}
