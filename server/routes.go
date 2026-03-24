// Package server provides HTTP routes
package server

import (
	"github.com/gin-gonic/gin"
)

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes(router *gin.Engine) {
	// Health check
	router.GET("/health", s.handleHealth)

	// API v1
	v1 := router.Group("/api/v1")
	{
		// Notifications
		v1.POST("/notifications", s.handleSendNotification)
		v1.GET("/notifications/:id", s.handleGetNotification)
		v1.GET("/notifications/:id/deliveries", s.handleGetDeliveries)

		// Configuration (future implementation)
		// v1.GET("/config", s.handleGetConfig)
		// v1.PUT("/config", s.handleUpdateConfig)
		// v1.PATCH("/config", s.handlePatchConfig)
		// v1.POST("/config/validate", s.handleValidateConfig)

		// Test (future implementation)
		// v1.POST("/test/send", s.handleTestSend)
	}
}
