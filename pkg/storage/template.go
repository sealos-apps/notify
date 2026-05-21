// Package storage provides template data operations
package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/database"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// TemplateStore handles template CRUD operations
type TemplateStore struct {
	db     *gorm.DB
	logger *log.Entry
}

// NewTemplateStore creates a new template store
func NewTemplateStore(db *gorm.DB, logger *log.Entry) *TemplateStore {
	if logger == nil {
		logger = log.WithField("component", "template_store")
	}
	return &TemplateStore{db: db, logger: logger}
}

// Create persists a new template. Name must be unique.
func (s *TemplateStore) Create(ctx context.Context, tpl *database.Template) error {
	if tpl.ID == "" {
		tpl.ID = uuid.New().String()
	}

	result := s.db.WithContext(ctx).Create(tpl)
	if result.Error != nil {
		return fmt.Errorf("failed to create template: %w", result.Error)
	}
	return nil
}

// GetByName retrieves a template by its unique name
func (s *TemplateStore) GetByName(ctx context.Context, name string) (*database.Template, error) {
	tpl := &database.Template{}
	result := s.db.WithContext(ctx).Where("name = ?", name).First(tpl)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: template %s", ErrNotFound, name)
		}
		return nil, fmt.Errorf("failed to get template: %w", result.Error)
	}
	return tpl, nil
}

// List returns all templates, optionally filtered by channel
func (s *TemplateStore) List(ctx context.Context, channel string) ([]*database.Template, error) {
	var templates []*database.Template
	q := s.db.WithContext(ctx).Order("name ASC")
	if channel != "" {
		q = q.Where("channel = ?", channel)
	}
	if err := q.Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}
	return templates, nil
}

// Update replaces updatable fields on the template identified by name.
// Only non-zero fields in updates are applied.
func (s *TemplateStore) Update(ctx context.Context, name string, updates *database.Template) error {
	result := s.db.WithContext(ctx).
		Model(&database.Template{}).
		Where("name = ?", name).
		Updates(map[string]interface{}{
			"description":   updates.Description,
			"subject":       updates.Subject,
			"body":          updates.Body,
			"template_code": updates.TemplateCode,
			"msg_type":      updates.MsgType,
			"params":        updates.Params,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to update template: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: template %s", ErrNotFound, name)
	}
	return nil
}

// Delete removes a template by name
func (s *TemplateStore) Delete(ctx context.Context, name string) error {
	result := s.db.WithContext(ctx).Where("name = ?", name).Delete(&database.Template{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete template: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: template %s", ErrNotFound, name)
	}
	return nil
}
