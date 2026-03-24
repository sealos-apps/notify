// Package server provides HTTP server for sealos-notify
package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/labring/sealos-notify/pkg/adapter"
	"github.com/labring/sealos-notify/pkg/config"
	"github.com/labring/sealos-notify/pkg/database"
	"github.com/labring/sealos-notify/pkg/dispatcher"
	"github.com/labring/sealos-notify/pkg/engine"
	"github.com/labring/sealos-notify/pkg/storage"
	log "github.com/sirupsen/logrus"
)

// Server represents the HTTP server
type Server struct {
	config               *config.GlobalConfig
	configContent        []byte
	httpServer           *http.Server
	db                   *database.DB
	engine               *engine.Engine
	dispatcher           *dispatcher.Dispatcher
	adapters             map[string]adapter.Adapter
	notificationStore    *storage.NotificationStore
	recipientStore       *storage.RecipientStore
	deliveryTaskStore    *storage.DeliveryTaskStore
	deliveryAttemptStore *storage.DeliveryAttemptStore
	logger               *log.Entry
	mu                   sync.RWMutex
	serverCtx            context.Context
}

// New creates a new server instance
func New(cfg *config.GlobalConfig, configContent []byte, logger *log.Entry) *Server {
	if logger == nil {
		logger = log.WithField("component", "server")
	}

	return &Server{
		config:        cfg,
		configContent: configContent,
		logger:        logger,
		adapters:      make(map[string]adapter.Adapter),
	}
}

// Init initializes the server
func (s *Server) Init(ctx context.Context) error {
	s.serverCtx = ctx

	// Initialize database
	db, err := database.New(ctx, database.Config{
		Host:            s.config.Database.Host,
		Port:            s.config.Database.Port,
		User:            s.config.Database.User,
		Password:        s.config.Database.Password,
		DBName:          s.config.Database.DBName,
		SSLMode:         s.config.Database.SSLMode,
		MaxOpenConns:    s.config.Database.MaxOpenConns,
		MaxIdleConns:    s.config.Database.MaxIdleConns,
		ConnMaxLifetime: s.config.Database.ConnMaxLifetime,
	}, s.logger.WithField("subcomponent", "database"))
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	s.db = db

	// Initialize schema
	if err := s.db.InitSchema(ctx); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Initialize stores
	s.notificationStore = storage.NewNotificationStore(s.db, s.logger.WithField("subcomponent", "notification_store"))
	s.recipientStore = storage.NewRecipientStore(s.db, s.logger.WithField("subcomponent", "recipient_store"))
	s.deliveryTaskStore = storage.NewDeliveryTaskStore(s.db, s.logger.WithField("subcomponent", "delivery_task_store"))
	s.deliveryAttemptStore = storage.NewDeliveryAttemptStore(s.db, s.logger.WithField("subcomponent", "delivery_attempt_store"))

	// Initialize adapters
	if err := s.initAdapters(); err != nil {
		return fmt.Errorf("failed to initialize adapters: %w", err)
	}

	// Initialize engine
	s.engine = engine.New(
		s.config,
		s.notificationStore,
		s.recipientStore,
		s.deliveryTaskStore,
		s.logger.WithField("subcomponent", "engine"),
	)

	// Initialize dispatcher
	s.dispatcher = dispatcher.New(
		s.config,
		s.deliveryTaskStore,
		s.deliveryAttemptStore,
		s.notificationStore,
		s.adapters,
		s.logger.WithField("subcomponent", "dispatcher"),
	)

	// Start dispatcher
	if err := s.dispatcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start dispatcher: %w", err)
	}

	// Setup HTTP server
	s.setupHTTPServer()

	s.logger.Info("Server initialized successfully")
	return nil
}

// initAdapters initializes channel adapters
func (s *Server) initAdapters() error {
	// TODO: Initialize actual adapters based on configuration
	// For now, we just log the configured providers
	for providerName, providerConfig := range s.config.Providers {
		s.logger.WithFields(log.Fields{
			"provider": providerName,
			"type":     providerConfig.Type,
		}).Debug("Provider configured")
	}

	return nil
}

// setupHTTPServer sets up the HTTP server with routes
func (s *Server) setupHTTPServer() {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(s.loggingMiddleware())

	// Setup routes
	s.setupRoutes(router)

	s.httpServer = &http.Server{
		Addr:         s.config.Server.Address,
		Handler:      router,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
		IdleTimeout:  s.config.Server.IdleTimeout,
	}
}

// loggingMiddleware creates a logging middleware
func (s *Server) loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		s.logger.WithFields(log.Fields{
			"method":  method,
			"path":    path,
			"status":  status,
			"latency": latency,
		}).Info("HTTP request")
	}
}

// Serve starts the HTTP server
func (s *Server) Serve() error {
	s.logger.WithField("address", s.config.Server.Address).Info("Starting HTTP server")

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server")

	// Stop dispatcher
	if s.dispatcher != nil {
		if err := s.dispatcher.Stop(); err != nil {
			s.logger.WithError(err).Error("Failed to stop dispatcher")
		}
	}

	// Stop HTTP server
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.logger.WithError(err).Error("Failed to stop HTTP server")
		}
	}

	// Close database
	if s.db != nil {
		s.db.Close()
	}

	s.logger.Info("Server shutdown complete")
	return nil
}

// Reload reloads the server configuration
func (s *Server) Reload(newConfigContent []byte, newConfig *config.GlobalConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Reloading server configuration")

	// Apply hot-reloadable configuration
	s.config.ApplyHotReload(newConfig)
	s.configContent = newConfigContent

	// Reinitialize adapters if needed
	if err := s.initAdapters(); err != nil {
		s.logger.WithError(err).Error("Failed to reinitialize adapters")
		return err
	}

	s.logger.Info("Server configuration reloaded")
	return nil
}
