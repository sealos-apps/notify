package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCredentialsYAML(t *testing.T) {
	credentials, err := parseCredentials([]byte(`
apps:
  - appId: app-a
    appSecret: secret-a
    name: App A
    enabled: true
  - appId: app-b
    appSecret: secret-b
    enabled: false
`))
	if err != nil {
		t.Fatalf("parseCredentials returned error: %v", err)
	}
	if len(credentials) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(credentials))
	}
	if credentials[0].AppID != "app-a" || credentials[0].AppSecret != "secret-a" {
		t.Fatalf("unexpected first credential: %#v", credentials[0])
	}
}

func TestParseCredentialsJSONWrapped(t *testing.T) {
	credentials, err := parseCredentials([]byte(`{
		"apps": [
			{"appId": "app-a", "appSecret": "secret-a", "enabled": true}
		]
	}`))
	if err != nil {
		t.Fatalf("parseCredentials returned error: %v", err)
	}
	if len(credentials) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(credentials))
	}
	if credentials[0].AppID != "app-a" || credentials[0].AppSecret != "secret-a" {
		t.Fatalf("unexpected credential: %#v", credentials[0])
	}
}

func TestManagerReloadFiltersDisabledAndTrimsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.yaml")
	if err := os.WriteFile(path, []byte(`
apps:
  - appId: " app-a "
    appSecret: " secret-a "
    name: App A
    enabled: true
  - appId: app-b
    appSecret: secret-b
    enabled: false
`), 0o600); err != nil {
		t.Fatalf("failed to write credentials: %v", err)
	}

	manager, err := NewManager(path, true, nil)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	principal, ok := manager.Authenticate("app-a", "secret-a")
	if !ok {
		t.Fatal("expected trimmed enabled credential to authenticate")
	}
	if principal.Name != "App A" {
		t.Fatalf("unexpected principal: %#v", principal)
	}
	if _, ok := manager.Authenticate("app-b", "secret-b"); ok {
		t.Fatal("expected disabled credential to be ignored")
	}
}

func TestManagerReloadKeepsPreviousSnapshotOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.yaml")
	if err := os.WriteFile(path, []byte(`
apps:
  - appId: app-a
    appSecret: secret-a
`), 0o600); err != nil {
		t.Fatalf("failed to write credentials: %v", err)
	}

	manager, err := NewManager(path, true, nil)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	if err := os.WriteFile(path, []byte(`
apps:
  - appId: app-b
    appSecret: ""
`), 0o600); err != nil {
		t.Fatalf("failed to write invalid credentials: %v", err)
	}
	if err := manager.Reload(); err == nil {
		t.Fatal("expected reload error")
	}

	if _, ok := manager.Authenticate("app-a", "secret-a"); !ok {
		t.Fatal("expected previous valid snapshot to remain available")
	}
	if _, ok := manager.Authenticate("app-b", ""); ok {
		t.Fatal("invalid credential should not be loaded")
	}
}

func TestManagerAuthenticate(t *testing.T) {
	enabled := true
	manager := &Manager{
		enabled: true,
		credentials: map[string]Credential{
			"app-a": {
				AppID:     "app-a",
				AppSecret: "secret-a",
				Name:      "App A",
				Enabled:   &enabled,
			},
		},
	}

	principal, ok := manager.Authenticate("app-a", "secret-a")
	if !ok {
		t.Fatal("expected valid credentials to authenticate")
	}
	if principal.AppID != "app-a" || principal.Name != "App A" {
		t.Fatalf("unexpected principal: %#v", principal)
	}

	if _, ok := manager.Authenticate("app-a", "wrong"); ok {
		t.Fatal("expected wrong appSecret to fail")
	}
	if _, ok := manager.Authenticate("missing", "secret-a"); ok {
		t.Fatal("expected missing appId to fail")
	}
}
