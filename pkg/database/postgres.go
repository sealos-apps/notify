// Package database provides database connectivity for sealos-notify
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
)

// DB represents a database connection
type DB struct {
	pool   *pgxpool.Pool
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
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName, cfg.SSLMode,
	)

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Set connection pool settings
	poolConfig.MaxConns = int32(cfg.MaxOpenConns)
	poolConfig.MinConns = int32(cfg.MaxIdleConns)
	poolConfig.MaxConnLifetime = cfg.ConnMaxLifetime
	poolConfig.MaxConnIdleTime = 10 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info("Database connection established")

	return &DB{
		pool:   pool,
		logger: logger,
	}, nil
}

// Close closes the database connection
func (db *DB) Close() {
	db.pool.Close()
	db.logger.Info("Database connection closed")
}

// Pool returns the underlying connection pool
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Ping checks if the database is alive
func (db *DB) Ping(ctx context.Context) error {
	return db.pool.Ping(ctx)
}

// InitSchema initializes the database schema
func (db *DB) InitSchema(ctx context.Context) error {
	schema := `
-- Notifications table
CREATE TABLE IF NOT EXISTS notifications (
    id VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    template VARCHAR(255),
    variables_json JSONB,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_status ON notifications(status);
CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at);

-- Notification recipients table
CREATE TABLE IF NOT EXISTS notification_recipients (
    id VARCHAR(64) PRIMARY KEY,
    notification_id VARCHAR(64) NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    recipient_type VARCHAR(50) NOT NULL,
    recipient_value VARCHAR(255) NOT NULL,
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
DROP TRIGGER IF EXISTS update_notifications_updated_at ON notifications;
CREATE TRIGGER update_notifications_updated_at BEFORE UPDATE ON notifications
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_delivery_tasks_updated_at ON delivery_tasks;
CREATE TRIGGER update_delivery_tasks_updated_at BEFORE UPDATE ON delivery_tasks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
`

	_, err := db.pool.Exec(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	db.logger.Info("Database schema initialized")
	return nil
}

// BeginTx starts a new transaction
func (db *DB) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return db.pool.Begin(ctx)
}

// QueryRow executes a query that returns at most one row
func (db *DB) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	return db.pool.QueryRow(ctx, query, args...)
}

// Query executes a query that returns rows
func (db *DB) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	return db.pool.Query(ctx, query, args...)
}

// Exec executes a query that doesn't return rows
func (db *DB) Exec(ctx context.Context, query string, args ...interface{}) (pgx.CommandTag, error) {
	return db.pool.Exec(ctx, query, args...)
}

// IsNoRows checks if an error is a "no rows" error
func IsNoRows(err error) bool {
	return err == sql.ErrNoRows || err == pgx.ErrNoRows
}
