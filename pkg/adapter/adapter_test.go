package adapter

import (
	"reflect"
	"testing"
)

func TestRecipientIdentifierKeys(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		want    []string
	}{
		{name: "email", channel: "email", want: []string{"email"}},
		{name: "sms", channel: "sms", want: []string{"phone"}},
		{name: "voice", channel: "voice", want: []string{"phone"}},
		{name: "inapp", channel: "inapp", want: []string{"user_id"}},
		{name: "feishu app", channel: "feishu_app", want: []string{"feishu_user_id", "email"}},
		{name: "feishu webhook", channel: "feishu_webhook", want: []string{"feishu_user_id", "email"}},
		{name: "unknown", channel: "unknown", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RecipientIdentifierKeys(tt.channel); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("RecipientIdentifierKeys(%q) = %#v, want %#v", tt.channel, got, tt.want)
			}
		})
	}
}
