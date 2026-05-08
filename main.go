// Package main provides the entry point for sealos-notify
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labring/sealos-notify/pkg/config"
	"github.com/labring/sealos-notify/pkg/logger"
	"github.com/labring/sealos-notify/server"
	log "github.com/sirupsen/logrus"
)

var (
	// Version is set by build flags
	Version = "dev"
	// BuildTime is set by build flags
	BuildTime = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Store CLI args for config reload (skip program name)
	cliArgs := os.Args[1:]

	// Load configuration: CLI args (defaults) → YAML → env vars
	cfg, err := config.LoadGlobalConfig(config.LoadOptions{
		Args: cliArgs,
	})
	if err != nil {
		log.WithError(err).Error("Failed to load configuration")
		return 1
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.WithError(err).Error("Configuration validation failed")
		return 1
	}

	// Initialize logger
	appLogger := logger.InitLog(
		nil,
		logger.WithDebug(cfg.Logging.Debug),
		logger.WithLevel(cfg.Logging.Level),
		logger.WithFormat(cfg.Logging.Format),
	)
	appLog := log.NewEntry(appLogger)

	appLog.WithFields(log.Fields{
		"version":            Version,
		"build_time":         BuildTime,
		"server_address":     cfg.Server.Address,
		"database_host":      cfg.Database.Host,
		"dispatcher_enabled": cfg.Dispatcher.Enabled,
	}).Info("Starting sealos-notify")

	// Read config file content if provided
	var configContent []byte
	if cfg.ConfigPath != "" {
		configContent, err = os.ReadFile(cfg.ConfigPath)
		if err != nil {
			log.WithError(err).Error("Failed to read config file")
			return 1
		}
	}

	// Create server
	srv := server.New(cfg, configContent, appLog)

	// Setup config reloader if config path is provided
	var reloader *config.Reloader
	if cfg.ConfigPath != "" {
		reloader, err = config.NewReloader(cfg.ConfigPath, func(newConfigContent []byte) error {
			return handleReload(cliArgs, newConfigContent, appLogger, srv)
		})
		if err != nil {
			log.WithError(err).Error("Failed to create config reloader")
			return 1
		}
	}

	// Setup signal handling with cancellable context
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize server first (this may take several seconds)
	if err := srv.Init(ctx); err != nil {
		log.WithError(err).Error("Failed to initialize server")
		return 1
	}

	// Start config reloader AFTER server is fully initialized
	if reloader != nil {
		if err := reloader.Start(ctx); err != nil {
			log.WithError(err).Error("Failed to start config reloader")
			return 1
		}
		defer func() {
			if err := reloader.Stop(); err != nil {
				log.WithError(err).Error("Failed to stop config reloader")
			}
		}()

		appLog.WithField("config_path", cfg.ConfigPath).Info("Configuration hot reload enabled")
	}

	// Start HTTP server in a goroutine
	go func() {
		if err := srv.Serve(); err != nil {
			appLog.WithError(err).Error("Server exited with error")
		}
	}()

	// Wait for interrupt signal
	<-ctx.Done()

	appLog.Info("Shutting down gracefully...")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	// Shutdown server
	if err := srv.Shutdown(shutdownCtx); err != nil {
		appLog.WithError(err).Error("Failed to shutdown server gracefully")
		cancel()
		return 1
	}
	cancel()

	appLog.Info("Server exited successfully")
	return 0
}

// handleReload handles configuration reload
func handleReload(
	cliArgs []string,
	newConfigContent []byte,
	appLogger *log.Logger,
	srv *server.Server,
) error {
	// Load and validate new configuration
	newConfig, err := loadAndValidateConfig(cliArgs, newConfigContent)
	if err != nil {
		return err
	}

	// Reload logger
	reloadLogger(appLogger, newConfig)

	// Reload server
	return srv.Reload(newConfigContent, newConfig)
}

// loadAndValidateConfig loads and validates configuration
func loadAndValidateConfig(cliArgs []string, configContent []byte) (*config.GlobalConfig, error) {
	newConfig, err := config.LoadGlobalConfig(config.LoadOptions{
		Args:          cliArgs,
		ConfigContent: configContent,
		DisableExit:   true,
	})
	if err != nil {
		return nil, err
	}

	if err := newConfig.Validate(); err != nil {
		return nil, err
	}

	return newConfig, nil
}

// reloadLogger reloads logger configuration
func reloadLogger(appLogger *log.Logger, cfg *config.GlobalConfig) {
	logger.InitLog(
		appLogger,
		logger.WithDebug(cfg.Logging.Debug),
		logger.WithLevel(cfg.Logging.Level),
		logger.WithFormat(cfg.Logging.Format),
	)

	log.NewEntry(appLogger).Info("Logger reloaded")
}
