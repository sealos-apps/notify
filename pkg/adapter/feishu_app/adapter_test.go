package feishu_app

import (
	"encoding/json"
	"testing"
)

func TestNewDefaultsAndValidation(t *testing.T) {
	a, err := New(map[string]interface{}{
		"appId":     "cli_test",
		"appSecret": "secret",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if a.config.ReceiveIDType != "open_id" {
		t.Fatalf("ReceiveIDType = %q, want open_id", a.config.ReceiveIDType)
	}
	if a.config.UrgentUserIDType != "open_id" {
		t.Fatalf("UrgentUserIDType = %q, want open_id", a.config.UrgentUserIDType)
	}
	if a.config.MsgType != "text" {
		t.Fatalf("MsgType = %q, want text", a.config.MsgType)
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestNewRequiresCredentials(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
	}{
		{name: "missing app id", data: map[string]interface{}{"appSecret": "secret"}},
		{name: "missing app secret", data: map[string]interface{}{"appId": "cli_test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.data); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestBuildContentForText(t *testing.T) {
	a := &Adapter{}

	got, err := a.buildContent("hello", "text")
	if err != nil {
		t.Fatalf("buildContent returned error: %v", err)
	}

	var decoded map[string]string
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if decoded["text"] != "hello" {
		t.Fatalf("unexpected text content: %#v", decoded)
	}
}

func TestBuildContentPassesThroughNonTextMessages(t *testing.T) {
	a := &Adapter{}
	body := `{"elements":[]}`

	got, err := a.buildContent(body, "interactive")
	if err != nil {
		t.Fatalf("buildContent returned error: %v", err)
	}
	if got != body {
		t.Fatalf("buildContent = %q, want %q", got, body)
	}
}
