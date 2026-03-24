// Package database provides database models for sealos-notify
package database

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// NotificationStatus represents the status of a notification
type NotificationStatus string

const (
	NotificationStatusPending    NotificationStatus = "pending"
	NotificationStatusProcessing NotificationStatus = "processing"
	NotificationStatusSuccess    NotificationStatus = "success"
	NotificationStatusFailed     NotificationStatus = "failed"
)

// DeliveryTaskStatus represents the status of a delivery task
type DeliveryTaskStatus string

const (
	DeliveryTaskStatusPending    DeliveryTaskStatus = "pending"
	DeliveryTaskStatusProcessing DeliveryTaskStatus = "processing"
	DeliveryTaskStatusSuccess    DeliveryTaskStatus = "success"
	DeliveryTaskStatusFailed     DeliveryTaskStatus = "failed"
	DeliveryTaskStatusDead       DeliveryTaskStatus = "dead"
)

// Notification represents a notification request
type Notification struct {
	ID             string             `db:"id"`
	IdempotencyKey string             `db:"idempotency_key"`
	Title          string             `db:"title"`
	Content        string             `db:"content"`
	Template       string             `db:"template"`
	VariablesJSON  JSONMap            `db:"variables_json"`
	Status         NotificationStatus `db:"status"`
	CreatedAt      time.Time          `db:"created_at"`
	UpdatedAt      time.Time          `db:"updated_at"`
}

// NotificationRecipient represents a recipient of a notification
type NotificationRecipient struct {
	ID             string    `db:"id"`
	NotificationID string    `db:"notification_id"`
	RecipientType  string    `db:"recipient_type"`
	RecipientValue string    `db:"recipient_value"`
	CreatedAt      time.Time `db:"created_at"`
}

// DeliveryTask represents a task to deliver a notification to a recipient via a channel
type DeliveryTask struct {
	ID             string             `db:"id"`
	NotificationID string             `db:"notification_id"`
	RecipientID    string             `db:"recipient_id"`
	Channel        string             `db:"channel"`
	Provider       string             `db:"provider"`
	Status         DeliveryTaskStatus `db:"status"`
	RetryCount     int                `db:"retry_count"`
	MaxRetry       int                `db:"max_retry"`
	NextRetryAt    *time.Time         `db:"next_retry_at"`
	LastError      string             `db:"last_error"`
	LeaseOwner     string             `db:"lease_owner"`
	LeaseExpireAt  *time.Time         `db:"lease_expire_at"`
	CreatedAt      time.Time          `db:"created_at"`
	UpdatedAt      time.Time          `db:"updated_at"`
}

// DeliveryAttempt represents a single attempt to deliver a notification
type DeliveryAttempt struct {
	ID              string    `db:"id"`
	TaskID          string    `db:"task_id"`
	AttemptNo       int       `db:"attempt_no"`
	RequestPayload  string    `db:"request_payload"`
	ResponsePayload string    `db:"response_payload"`
	Result          string    `db:"result"`
	ErrorMessage    string    `db:"error_message"`
	StartedAt       time.Time `db:"started_at"`
	FinishedAt      time.Time `db:"finished_at"`
}

// ConfigChangeAudit represents a configuration change audit log
type ConfigChangeAudit struct {
	ID                    string    `db:"id"`
	Operator              string    `db:"operator"`
	Action                string    `db:"action"`
	ResourceVersionBefore string    `db:"resource_version_before"`
	ResourceVersionAfter  string    `db:"resource_version_after"`
	ConfigSnapshot        string    `db:"config_snapshot"`
	CreatedAt             time.Time `db:"created_at"`
}

// JSONMap is a helper type for storing JSON data in the database
type JSONMap map[string]interface{}

// Value implements driver.Valuer interface
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements sql.Scanner interface
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}

	return json.Unmarshal(bytes, j)
}
