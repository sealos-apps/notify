// Package config provides configuration reloading functionality
package config

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
)

// ReloadFunc is called when configuration changes
type ReloadFunc func(newConfigContent []byte) error

// Reloader watches configuration file for changes and triggers reload
type Reloader struct {
	configPath string
	reloadFunc ReloadFunc
	watcher    *fsnotify.Watcher
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewReloader creates a new configuration reloader
func NewReloader(configPath string, reloadFunc ReloadFunc) (*Reloader, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	return &Reloader{
		configPath: configPath,
		reloadFunc: reloadFunc,
		watcher:    watcher,
		stopCh:     make(chan struct{}),
	}, nil
}

// Start begins watching the configuration file
func (r *Reloader) Start(ctx context.Context) error {
	if err := r.watcher.Add(r.configPath); err != nil {
		return fmt.Errorf("failed to watch config file: %w", err)
	}

	r.wg.Add(1)
	go r.watchLoop(ctx)

	log.WithField("config_path", r.configPath).Info("Config reloader started")
	return nil
}

// Stop stops the reloader
func (r *Reloader) Stop() error {
	close(r.stopCh)
	r.wg.Wait()

	if err := r.watcher.Close(); err != nil {
		return fmt.Errorf("failed to close watcher: %w", err)
	}

	log.Info("Config reloader stopped")
	return nil
}

// watchLoop watches for file changes
func (r *Reloader) watchLoop(ctx context.Context) {
	defer r.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case event, ok := <-r.watcher.Events:
			if !ok {
				return
			}
			r.handleEvent(event)
		case err, ok := <-r.watcher.Errors:
			if !ok {
				return
			}
			log.WithError(err).Error("Config watcher error")
		}
	}
}

// handleEvent processes file system events
func (r *Reloader) handleEvent(event fsnotify.Event) {
	// Only care about write and create events
	if event.Op&fsnotify.Write != fsnotify.Write && event.Op&fsnotify.Create != fsnotify.Create {
		return
	}

	log.WithFields(log.Fields{
		"file": event.Name,
		"op":   event.Op.String(),
	}).Info("Config file changed, reloading...")

	// Read new config content
	newContent, err := os.ReadFile(r.configPath)
	if err != nil {
		log.WithError(err).Error("Failed to read config file")
		return
	}

	// Call reload function
	if err := r.reloadFunc(newContent); err != nil {
		log.WithError(err).Error("Failed to reload configuration")
		return
	}

	log.Info("Configuration reloaded successfully")
}
