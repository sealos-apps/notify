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

		// Templates
		v1.POST("/templates", s.handleCreateTemplate)
		v1.GET("/templates", s.handleListTemplates)
		v1.GET("/templates/:name", s.handleGetTemplate)
		v1.PUT("/templates/:name", s.handleUpdateTemplate)
		v1.DELETE("/templates/:name", s.handleDeleteTemplate)
	}
}
