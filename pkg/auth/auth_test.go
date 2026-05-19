package auth

import "testing"

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
