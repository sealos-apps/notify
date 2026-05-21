// Package server provides HTTP handlers
package server

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/labring/sealos-notify/pkg/database"
	"github.com/labring/sealos-notify/pkg/engine"
	"github.com/labring/sealos-notify/pkg/storage"
)

// handleHealth handles health check requests
func (s *Server) handleHealth(c *gin.Context) {
	// Check database connectivity
	if err := s.db.Ping(c.Request.Context()); err != nil {
		s.logger.WithError(err).Error("Health check failed")
		respondError(c, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "service unavailable")
		return
	}

	respondOK(c, http.StatusOK, gin.H{"status": "healthy"})
}

// handleSendNotification handles notification sending requests
func (s *Server) handleSendNotification(c *gin.Context) {
	var req engine.SendNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.WithError(err).Warn("Invalid send notification request body")
		respondError(c, http.StatusBadRequest, errorCodeBadRequest, "invalid request body")
		return
	}

	if principal := principalFromContext(c); principal != nil {
		req.SenderAppID = principal.AppID
	}

	// Send notification
	response, err := s.engine.SendNotification(c.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, engine.ErrInvalidRequest) {
			s.logger.WithError(err).Warn("Invalid send notification request")
			respondError(c, http.StatusBadRequest, errorCodeBadRequest, "invalid notification request")
			return
		}
		s.logger.WithError(err).Error("Failed to send notification")
		internalServerError(c)
		return
	}

	respondOK(c, http.StatusOK, response)
}

// handleGetNotification handles notification status query requests
func (s *Server) handleGetNotification(c *gin.Context) {
	notificationID := c.Param("id")
	if notificationID == "" {
		respondError(c, http.StatusBadRequest, errorCodeBadRequest, "notification ID is required")
		return
	}

	// Get notification status
	status, err := s.engine.GetNotificationStatus(c.Request.Context(), notificationID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.logger.WithError(err).WithField("notification_id", notificationID).Warn("Notification not found")
			respondError(c, http.StatusNotFound, errorCodeNotFound, "notification not found")
			return
		}
		s.logger.WithError(err).WithField("notification_id", notificationID).Error("Failed to get notification status")
		internalServerError(c)
		return
	}

	respondOK(c, http.StatusOK, status)
}

// handleGetDeliveries handles delivery records query requests
func (s *Server) handleGetDeliveries(c *gin.Context) {
	notificationID := c.Param("id")
	if notificationID == "" {
		respondError(c, http.StatusBadRequest, errorCodeBadRequest, "notification ID is required")
		return
	}

	// Get delivery tasks
	tasks, err := s.deliveryTaskStore.GetByNotificationID(c.Request.Context(), notificationID)
	if err != nil {
		s.logger.WithError(err).WithField("notification_id", notificationID).Error("Failed to get delivery tasks")
		internalServerError(c)
		return
	}

	respondOK(c, http.StatusOK, gin.H{"deliveries": tasks})
}

// handleCreateTemplate creates a new template
func (s *Server) handleCreateTemplate(c *gin.Context) {
	var tpl database.Template
	if err := c.ShouldBindJSON(&tpl); err != nil {
		s.logger.WithError(err).Warn("Invalid create template request body")
		respondError(c, http.StatusBadRequest, errorCodeBadRequest, "invalid request body")
		return
	}
	if tpl.Name == "" || tpl.Channel == "" {
		respondError(c, http.StatusBadRequest, errorCodeBadRequest, "name and channel are required")
		return
	}

	if err := s.templateStore.Create(c.Request.Context(), &tpl); err != nil {
		s.logger.WithError(err).Error("Failed to create template")
		internalServerError(c)
		return
	}

	respondOK(c, http.StatusCreated, tpl)
}

// handleListTemplates lists all templates, optionally filtered by channel query param
func (s *Server) handleListTemplates(c *gin.Context) {
	channel := c.Query("channel")

	templates, err := s.templateStore.List(c.Request.Context(), channel)
	if err != nil {
		s.logger.WithError(err).Error("Failed to list templates")
		internalServerError(c)
		return
	}

	respondOK(c, http.StatusOK, gin.H{"templates": templates})
}

// handleGetTemplate retrieves a template by name
func (s *Server) handleGetTemplate(c *gin.Context) {
	name := c.Param("name")

	tpl, err := s.templateStore.GetByName(c.Request.Context(), name)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.logger.WithError(err).WithField("template_name", name).Warn("Template not found")
			respondError(c, http.StatusNotFound, errorCodeNotFound, "template not found")
			return
		}
		s.logger.WithError(err).WithField("template_name", name).Error("Failed to get template")
		internalServerError(c)
		return
	}

	respondOK(c, http.StatusOK, tpl)
}

// handleUpdateTemplate updates an existing template by name
func (s *Server) handleUpdateTemplate(c *gin.Context) {
	name := c.Param("name")

	var updates database.Template
	if err := c.ShouldBindJSON(&updates); err != nil {
		s.logger.WithError(err).WithField("template_name", name).Warn("Invalid update template request body")
		respondError(c, http.StatusBadRequest, errorCodeBadRequest, "invalid request body")
		return
	}

	if err := s.templateStore.Update(c.Request.Context(), name, &updates); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.logger.WithError(err).WithField("template_name", name).Warn("Template not found")
			respondError(c, http.StatusNotFound, errorCodeNotFound, "template not found")
			return
		}
		s.logger.WithError(err).WithField("template_name", name).Error("Failed to update template")
		internalServerError(c)
		return
	}

	tpl, err := s.templateStore.GetByName(c.Request.Context(), name)
	if err != nil {
		s.logger.WithError(err).WithField("template_name", name).Error("Failed to fetch updated template")
		internalServerError(c)
		return
	}

	respondOK(c, http.StatusOK, tpl)
}

// handleDeleteTemplate deletes a template by name
func (s *Server) handleDeleteTemplate(c *gin.Context) {
	name := c.Param("name")

	if err := s.templateStore.Delete(c.Request.Context(), name); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.logger.WithError(err).WithField("template_name", name).Warn("Template not found")
			respondError(c, http.StatusNotFound, errorCodeNotFound, "template not found")
			return
		}
		s.logger.WithError(err).WithField("template_name", name).Error("Failed to delete template")
		internalServerError(c)
		return
	}

	respondNoData(c, http.StatusOK)
}
