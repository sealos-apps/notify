package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/labring/sealos-notify/pkg/auth"
)

const authPrincipalKey = "authPrincipal"

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.authManager == nil || !s.authManager.Enabled() {
			c.Next()
			return
		}

		appID, appSecret := credentialsFromRequest(c)
		if appID == "" || appSecret == "" {
			abortWithError(c, http.StatusUnauthorized, errorCodeUnauthorized, "missing app credentials")
			return
		}

		principal, ok := s.authManager.Authenticate(appID, appSecret)
		if !ok {
			abortWithError(c, http.StatusUnauthorized, errorCodeUnauthorized, "invalid app credentials")
			return
		}

		c.Set(authPrincipalKey, principal)
		c.Next()
	}
}

func credentialsFromRequest(c *gin.Context) (string, string) {
	appID := strings.TrimSpace(c.GetHeader("X-App-Id"))
	appSecret := strings.TrimSpace(c.GetHeader("X-App-Secret"))
	if appID != "" || appSecret != "" {
		return appID, appSecret
	}

	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader == "" {
		return "", ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", ""
	}

	token := strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func principalFromContext(c *gin.Context) *auth.Principal {
	value, ok := c.Get(authPrincipalKey)
	if !ok {
		return nil
	}
	principal, _ := value.(*auth.Principal)
	return principal
}
