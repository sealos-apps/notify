// Package database provides database connectivity for sealos-notify
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB represents a database connection
type DB struct {
	gormDB *gorm.DB
	logger *log.Entry
}

// Config contains database configuration
type Config struct {
	Host            string
	Port            int
	User            string
	Password        string
	DBName          string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// New creates a new database connection
func New(ctx context.Context, cfg Config, logger *log.Entry) (*DB, error) {
	if logger == nil {
		logger = log.WithField("component", "database")
	}

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s",
		cfg.Host, cfg.User, cfg.Password, cfg.DBName, cfg.Port, cfg.SSLMode,
	)

	gormDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// Test connection
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info("Database connection established")

	return &DB{
		gormDB: gormDB,
		logger: logger,
	}, nil
}

// GORM returns the underlying *gorm.DB instance
func (db *DB) GORM() *gorm.DB {
	return db.gormDB
}

// Close closes the database connection
func (db *DB) Close() {
	if sqlDB, err := db.gormDB.DB(); err == nil {
		sqlDB.Close()
	}
	db.logger.Info("Database connection closed")
}

// Ping checks if the database is alive
func (db *DB) Ping(ctx context.Context) error {
	sqlDB, err := db.gormDB.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// InitSchema initializes the database schema
func (db *DB) InitSchema(ctx context.Context) error {
	result := db.gormDB.WithContext(ctx).Exec(schema)
	if result.Error != nil {
		return fmt.Errorf("failed to initialize schema: %w", result.Error)
	}
	db.logger.Info("Database schema initialized")
	return nil
}

// IsNoRows checks if an error is a "no rows" error
func IsNoRows(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, sql.ErrNoRows)
}

// schema is the SQL to initialize the database tables, indexes, and triggers
const schema = `
-- Templates table (must exist before notifications for FK safety; no FK dependency)
CREATE TABLE IF NOT EXISTS templates (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    channel VARCHAR(50) NOT NULL,
    description TEXT,
    subject TEXT,
    body TEXT,
    template_code VARCHAR(255),
    msg_type VARCHAR(50),
    params JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_templates_channel ON templates(channel);

-- Notifications table (content-free; all data lives in recipients/templates)
CREATE TABLE IF NOT EXISTS notifications (
    id VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_status ON notifications(status);
CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at);

-- Notification recipients table (params = all KVs: identifiers + template variables)
CREATE TABLE IF NOT EXISTS notification_recipients (
    id VARCHAR(64) PRIMARY KEY,
    notification_id VARCHAR(64) NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    params JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recipients_notification_id ON notification_recipients(notification_id);

-- Delivery tasks table
CREATE TABLE IF NOT EXISTS delivery_tasks (
    id VARCHAR(64) PRIMARY KEY,
    notification_id VARCHAR(64) NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    recipient_id VARCHAR(64) NOT NULL REFERENCES notification_recipients(id) ON DELETE CASCADE,
    channel VARCHAR(50) NOT NULL,
    provider VARCHAR(100) NOT NULL,
    template_name VARCHAR(255) NOT NULL,
    template_params JSONB,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retry INTEGER NOT NULL DEFAULT 3,
    next_retry_at TIMESTAMP,
    last_error TEXT,
    lease_owner VARCHAR(255),
    lease_expire_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(notification_id, recipient_id, channel)
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON delivery_tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_next_retry ON delivery_tasks(next_retry_at) WHERE status = 'failed' AND next_retry_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_lease ON delivery_tasks(lease_owner, lease_expire_at);

-- Delivery attempts table
CREATE TABLE IF NOT EXISTS delivery_attempts (
    id VARCHAR(64) PRIMARY KEY,
    task_id VARCHAR(64) NOT NULL REFERENCES delivery_tasks(id) ON DELETE CASCADE,
    attempt_no INTEGER NOT NULL,
    request_payload TEXT,
    response_payload TEXT,
    result VARCHAR(50) NOT NULL,
    error_message TEXT,
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_attempts_task_id ON delivery_attempts(task_id);
CREATE INDEX IF NOT EXISTS idx_attempts_started_at ON delivery_attempts(started_at);

-- Config change audits table
CREATE TABLE IF NOT EXISTS config_change_audits (
    id VARCHAR(64) PRIMARY KEY,
    operator VARCHAR(255) NOT NULL,
    action VARCHAR(50) NOT NULL,
    resource_version_before VARCHAR(100),
    resource_version_after VARCHAR(100),
    config_snapshot TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audits_created_at ON config_change_audits(created_at);
CREATE INDEX IF NOT EXISTS idx_audits_operator ON config_change_audits(operator);

-- Trigger function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create triggers
DROP TRIGGER IF EXISTS update_templates_updated_at ON templates;
CREATE TRIGGER update_templates_updated_at BEFORE UPDATE ON templates
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_notifications_updated_at ON notifications;
CREATE TRIGGER update_notifications_updated_at BEFORE UPDATE ON notifications
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_delivery_tasks_updated_at ON delivery_tasks;
CREATE TRIGGER update_delivery_tasks_updated_at BEFORE UPDATE ON delivery_tasks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
`
