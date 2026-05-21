package config

import (
	"testing"
	"time"
)

func TestLoadGlobalConfigFromYAMLAndEnvironment(t *testing.T) {
	t.Setenv("FEISHU_APP_SECRET", "expanded-secret")
	t.Setenv("SERVER_ADDRESS", ":9090")

	cfg, err := LoadGlobalConfig(LoadOptions{
		Args: []string{"--auth-enabled=false"},
		ConfigContent: []byte(`
server:
  address: ":8080"
database:
  host: db
  dbname: notify
auth:
  enabled: false
dispatcher:
  interval: 2s
defaults:
  maxRetry: 4
  retryBackoffSeconds: [1, 2, 3]
channels:
  feishu_app:
    enabled: true
    provider: feishu-provider
providers:
  feishu-provider:
    type: feishu_app
    appSecret: ${FEISHU_APP_SECRET}
`),
	})
	if err != nil {
		t.Fatalf("LoadGlobalConfig returned error: %v", err)
	}

	if cfg.Server.Address != ":9090" {
		t.Fatalf("Server.Address = %q, want env override :9090", cfg.Server.Address)
	}
	if cfg.Dispatcher.Interval != 2*time.Second {
		t.Fatalf("Dispatcher.Interval = %v, want 2s", cfg.Dispatcher.Interval)
	}
	if cfg.Providers["feishu-provider"].Data["appSecret"] != "expanded-secret" {
		t.Fatalf("provider env was not expanded: %#v", cfg.Providers["feishu-provider"].Data)
	}
}

func TestValidateAuthRequiresCredentialPathWhenEnabled(t *testing.T) {
	cfg := &GlobalConfig{
		Database: DatabaseConfig{Host: "db", DBName: "notify"},
		Auth:     AuthConfig{Enabled: true},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected auth credential path validation error")
	}
}

func TestValidateEnabledChannelRequiresExistingProvider(t *testing.T) {
	cfg := &GlobalConfig{
		Database: DatabaseConfig{Host: "db", DBName: "notify"},
		Auth:     AuthConfig{Enabled: false},
		Channels: map[string]ChannelConfig{
			"email": {Enabled: true, Provider: "missing"},
		},
		Providers: map[string]ProviderConfig{},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing provider validation error")
	}
}

func TestGetDSN(t *testing.T) {
	cfg := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "secret",
		DBName:   "notify",
		SSLMode:  "disable",
	}

	want := "host=localhost port=5432 user=postgres password=secret dbname=notify sslmode=disable"
	if got := cfg.GetDSN(); got != want {
		t.Fatalf("GetDSN() = %q, want %q", got, want)
	}
}
