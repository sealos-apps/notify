// Package server provides HTTP handlers
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/labring/sealos-notify/pkg/database"
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

	if principal := principalFromContext(c); principal != nil {
		req.SenderAppID = principal.AppID
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
		s.logger.WithError(err).WithField("notification_id", notificationID).Error("Failed to get delivery tasks")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to get delivery tasks,internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deliveries": tasks,
	})
}

// handleCreateTemplate creates a new template
func (s *Server) handleCreateTemplate(c *gin.Context) {
	var tpl database.Template
	if err := c.ShouldBindJSON(&tpl); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
		return
	}
	if tpl.Name == "" || tpl.Channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and channel are required"})
		return
	}

	if err := s.templateStore.Create(c.Request.Context(), &tpl); err != nil {
		s.logger.WithError(err).Error("Failed to create template")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create template", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, tpl)
}

// handleListTemplates lists all templates, optionally filtered by channel query param
func (s *Server) handleListTemplates(c *gin.Context) {
	channel := c.Query("channel")

	templates, err := s.templateStore.List(c.Request.Context(), channel)
	if err != nil {
		s.logger.WithError(err).Error("Failed to list templates")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list templates", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

// handleGetTemplate retrieves a template by name
func (s *Server) handleGetTemplate(c *gin.Context) {
	name := c.Param("name")

	tpl, err := s.templateStore.GetByName(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tpl)
}

// handleUpdateTemplate updates an existing template by name
func (s *Server) handleUpdateTemplate(c *gin.Context) {
	name := c.Param("name")

	var updates database.Template
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
		return
	}

	if err := s.templateStore.Update(c.Request.Context(), name, &updates); err != nil {
		s.logger.WithError(err).Error("Failed to update template")
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	tpl, err := s.templateStore.GetByName(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch updated template"})
		return
	}

	c.JSON(http.StatusOK, tpl)
}

// handleDeleteTemplate deletes a template by name
func (s *Server) handleDeleteTemplate(c *gin.Context) {
	name := c.Param("name")

	if err := s.templateStore.Delete(c.Request.Context(), name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
