// Package auth provides app credential based API authentication.
package auth

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Credential represents one application allowed to call the notify center API.
type Credential struct {
	AppID     string `json:"appId" yaml:"appId"`
	AppSecret string `json:"appSecret" yaml:"appSecret"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// Principal is the authenticated caller identity.
type Principal struct {
	AppID string
	Name  string
}

// Manager keeps credentials in memory and reloads them from a mounted Secret file.
type Manager struct {
	path        string
	enabled     bool
	credentials map[string]Credential
	watcher     *fsnotify.Watcher
	stopCh      chan struct{}
	logger      *log.Entry
	mu          sync.RWMutex
}

// NewManager creates a credential manager and loads the initial credential file.
func NewManager(path string, enabled bool, logger *log.Entry) (*Manager, error) {
	if logger == nil {
		logger = log.WithField("component", "auth")
	}

	m := &Manager{
		path:        path,
		enabled:     enabled,
		credentials: make(map[string]Credential),
		stopCh:      make(chan struct{}),
		logger:      logger,
	}

	if !enabled {
		logger.Warn("API authentication is disabled")
		return m, nil
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("auth credential file path is required when authentication is enabled")
	}
	if err := m.Reload(); err != nil {
		return nil, err
	}
	return m, nil
}

// Enabled reports whether authentication should be enforced.
func (m *Manager) Enabled() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// Update applies a new auth configuration and reloads credentials from the new path.
func (m *Manager) Update(path string, enabled bool) error {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	oldPath := m.path
	oldEnabled := m.enabled
	m.path = path
	m.enabled = enabled
	m.mu.Unlock()

	if !enabled {
		if oldEnabled {
			m.logger.Warn("API authentication disabled by configuration reload")
		}
		return nil
	}
	if strings.TrimSpace(path) == "" {
		m.mu.Lock()
		m.path = oldPath
		m.enabled = oldEnabled
		m.mu.Unlock()
		return errors.New("auth credential file path is required when authentication is enabled")
	}
	if err := m.Reload(); err != nil {
		m.mu.Lock()
		m.path = oldPath
		m.enabled = oldEnabled
		m.mu.Unlock()
		return err
	}

	if path != oldPath && m.watcher != nil {
		_ = m.watcher.Remove(oldPath)
		_ = m.watcher.Remove(filepath.Dir(oldPath))
		for _, watchPath := range []string{path, filepath.Dir(path)} {
			if err := m.watcher.Add(watchPath); err != nil {
				return fmt.Errorf("failed to watch auth credential path %q: %w", watchPath, err)
			}
		}
	}
	return nil
}

// Authenticate validates appId/appSecret against the current in-memory snapshot.
func (m *Manager) Authenticate(appID, appSecret string) (*Principal, bool) {
	if m == nil || !m.Enabled() {
		return &Principal{}, true
	}

	m.mu.RLock()
	cred, ok := m.credentials[appID]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if subtle.ConstantTimeCompare([]byte(cred.AppSecret), []byte(appSecret)) != 1 {
		return nil, false
	}
	return &Principal{AppID: cred.AppID, Name: cred.Name}, true
}

// Reload reloads credentials from disk. The previous valid snapshot is kept on error.
func (m *Manager) Reload() error {
	m.mu.RLock()
	path := m.path
	m.mu.RUnlock()

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read auth credentials file: %w", err)
	}

	credentials, err := parseCredentials(content)
	if err != nil {
		return err
	}

	next := make(map[string]Credential, len(credentials))
	for _, credential := range credentials {
		credential.AppID = strings.TrimSpace(credential.AppID)
		credential.AppSecret = strings.TrimSpace(credential.AppSecret)
		if credential.AppID == "" {
			return errors.New("auth credential appId must not be empty")
		}
		if credential.AppSecret == "" {
			return fmt.Errorf("auth credential %q appSecret must not be empty", credential.AppID)
		}
		if credential.Enabled != nil && !*credential.Enabled {
			continue
		}
		if _, exists := next[credential.AppID]; exists {
			return fmt.Errorf("duplicate auth credential appId %q", credential.AppID)
		}
		next[credential.AppID] = credential
	}
	if len(next) == 0 {
		return errors.New("at least one enabled auth credential is required")
	}

	m.mu.Lock()
	m.credentials = next
	m.mu.Unlock()

	m.logger.WithField("credential_count", len(next)).Info("API auth credentials loaded")
	return nil
}

// Start begins watching the credential file and its parent directory for Secret updates.
func (m *Manager) Start() error {
	if m == nil || !m.Enabled() {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create auth credential watcher: %w", err)
	}
	m.watcher = watcher

	watchPaths := []string{m.path, filepath.Dir(m.path)}
	for _, path := range watchPaths {
		if err := watcher.Add(path); err != nil {
			_ = watcher.Close()
			return fmt.Errorf("failed to watch auth credential path %q: %w", path, err)
		}
	}

	go m.watchLoop()
	m.logger.WithField("path", m.path).Info("API auth credential watcher started")
	return nil
}

// Stop stops the credential watcher.
func (m *Manager) Stop() error {
	if m == nil || m.watcher == nil {
		return nil
	}
	close(m.stopCh)
	if err := m.watcher.Close(); err != nil {
		return fmt.Errorf("failed to close auth credential watcher: %w", err)
	}
	return nil
}

func (m *Manager) watchLoop() {
	for {
		select {
		case <-m.stopCh:
			return
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			if !m.shouldReload(event) {
				continue
			}
			m.logger.WithFields(log.Fields{
				"file": event.Name,
				"op":   event.Op.String(),
			}).Info("API auth credential file changed, reloading")
			if err := m.Reload(); err != nil {
				m.logger.WithError(err).Error("Failed to reload API auth credentials")
			}
		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			m.logger.WithError(err).Error("API auth credential watcher error")
		}
	}
}

func (m *Manager) shouldReload(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
		return false
	}
	return event.Name == m.path ||
		filepath.Dir(event.Name) == filepath.Dir(m.path) ||
		filepath.Base(event.Name) == "..data"
}

func parseCredentials(content []byte) ([]Credential, error) {
	var list []Credential
	if err := json.Unmarshal(content, &list); err == nil {
		return list, nil
	}

	var wrapped struct {
		Apps []Credential `json:"apps" yaml:"apps"`
	}
	if err := json.Unmarshal(content, &wrapped); err == nil && len(wrapped.Apps) > 0 {
		return wrapped.Apps, nil
	}

	if err := yaml.Unmarshal(content, &wrapped); err != nil {
		return nil, fmt.Errorf("failed to parse auth credentials file: %w", err)
	}
	if len(wrapped.Apps) == 0 {
		return nil, errors.New("auth credentials file must contain apps")
	}
	return wrapped.Apps, nil
}
