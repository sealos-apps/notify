// Package server provides HTTP handlers
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/labring/sealos-notify/pkg/engine"
)

// handleHealth handles health check requests
func (s *Server) handleHealth(c *gin.Context) {
	// Check database connectivity
	if err := s.db.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

// handleSendNotification handles notification sending requests
func (s *Server) handleSendNotification(c *gin.Context) {
	var req engine.SendNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Send notification
	response, err := s.engine.SendNotification(c.Request.Context(), &req)
	if err != nil {
		s.logger.WithError(err).Error("Failed to send notification")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to send notification",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// handleGetNotification handles notification status query requests
func (s *Server) handleGetNotification(c *gin.Context) {
	notificationID := c.Param("id")
	if notificationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "notification ID is required",
		})
		return
	}

	// Get notification status
	status, err := s.engine.GetNotificationStatus(c.Request.Context(), notificationID)
	if err != nil {
		s.logger.WithError(err).Error("Failed to get notification status")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to get notification status",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, status)
}

// handleGetDeliveries handles delivery records query requests
func (s *Server) handleGetDeliveries(c *gin.Context) {
	notificationID := c.Param("id")
	if notificationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "notification ID is required",
		})
		return
	}

	// Get delivery tasks
	tasks, err := s.deliveryTaskStore.GetByNotificationID(c.Request.Context(), notificationID)
	if err != nil {
		s.logger.WithError(err).Error("Failed to get delivery tasks")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to get delivery tasks",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deliveries": tasks,
	})
}
