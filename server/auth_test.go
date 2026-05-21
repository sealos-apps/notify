package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/labring/sealos-notify/pkg/auth"
)

func TestCredentialsFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		wantAppID  string
		wantSecret string
	}{
		{
			name: "headers",
			headers: map[string]string{
				"X-App-Id":     " notify-console ",
				"X-App-Secret": " dev-secret ",
			},
			wantAppID:  "notify-console",
			wantSecret: "dev-secret",
		},
		{
			name: "bearer token",
			headers: map[string]string{
				"Authorization": "Bearer notify-console:dev-secret",
			},
			wantAppID:  "notify-console",
			wantSecret: "dev-secret",
		},
		{
			name: "unsupported authorization scheme",
			headers: map[string]string{
				"Authorization": "Basic notify-console:dev-secret",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testContextWithHeaders(tt.headers)
			appID, secret := credentialsFromRequest(c)
			if appID != tt.wantAppID || secret != tt.wantSecret {
				t.Fatalf("credentialsFromRequest() = (%q, %q), want (%q, %q)", appID, secret, tt.wantAppID, tt.wantSecret)
			}
		})
	}
}

func TestPrincipalFromContext(t *testing.T) {
	c := testContextWithHeaders(nil)
	if principalFromContext(c) != nil {
		t.Fatal("expected nil principal before context is populated")
	}

	want := &auth.Principal{AppID: "notify-console", Name: "Notify Console"}
	c.Set(authPrincipalKey, want)

	if got := principalFromContext(c); got != want {
		t.Fatalf("principalFromContext() = %#v, want %#v", got, want)
	}
}

func TestAuthMiddlewareRejectsMissingCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, err := auth.NewManager(writeCredentialFile(t), true, nil)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	s := &Server{authManager: manager}

	router := gin.New()
	router.Use(s.authMiddleware())
	router.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewareAcceptsValidCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, err := auth.NewManager(writeCredentialFile(t), true, nil)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	s := &Server{authManager: manager}

	router := gin.New()
	router.Use(s.authMiddleware())
	router.GET("/protected", func(c *gin.Context) {
		principal := principalFromContext(c)
		if principal == nil || principal.AppID != "notify-console" {
			t.Fatalf("unexpected principal: %#v", principal)
		}
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-App-Id", "notify-console")
	req.Header.Set("X-App-Secret", "dev-secret")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func testContextWithHeaders(headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	c.Request = req
	return c
}

func writeCredentialFile(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/apps.yaml"
	content := []byte(`
apps:
  - appId: notify-console
    appSecret: dev-secret
    name: Notify Console
    enabled: true
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("failed to write credential file: %v", err)
	}
	return path
}
