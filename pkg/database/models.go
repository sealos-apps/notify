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
	ID             string             `gorm:"primaryKey;column:id;type:varchar(64)"`
	IdempotencyKey string             `gorm:"uniqueIndex;column:idempotency_key;type:varchar(255);not null"`
	Title          string             `gorm:"column:title;not null"`
	Content        string             `gorm:"column:content;not null"`
	Template       string             `gorm:"column:template;type:varchar(255)"`
	VariablesJSON  JSONMap            `gorm:"column:variables_json;type:jsonb"`
	Status         NotificationStatus `gorm:"column:status;type:varchar(50);not null;default:pending"`
	CreatedAt      time.Time          `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time          `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName sets the table name for GORM
func (Notification) TableName() string { return "notifications" }

// NotificationRecipient represents a recipient of a notification
type NotificationRecipient struct {
	ID             string    `gorm:"primaryKey;column:id;type:varchar(64)"`
	NotificationID string    `gorm:"column:notification_id;type:varchar(64);not null;index"`
	RecipientType  string    `gorm:"column:recipient_type;type:varchar(50);not null"`
	RecipientValue string    `gorm:"column:recipient_value;type:varchar(255);not null"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
}

// TableName sets the table name for GORM
func (NotificationRecipient) TableName() string { return "notification_recipients" }

// DeliveryTask represents a task to deliver a notification to a recipient via a channel
type DeliveryTask struct {
	ID             string             `gorm:"primaryKey;column:id;type:varchar(64)"`
	NotificationID string             `gorm:"column:notification_id;type:varchar(64);not null;index"`
	RecipientID    string             `gorm:"column:recipient_id;type:varchar(64);not null"`
	Channel        string             `gorm:"column:channel;type:varchar(50);not null"`
	Provider       string             `gorm:"column:provider;type:varchar(100);not null"`
	Status         DeliveryTaskStatus `gorm:"column:status;type:varchar(50);not null;default:pending;index"`
	RetryCount     int                `gorm:"column:retry_count;not null;default:0"`
	MaxRetry       int                `gorm:"column:max_retry;not null;default:3"`
	NextRetryAt    *time.Time         `gorm:"column:next_retry_at"`
	LastError      string             `gorm:"column:last_error;type:text"`
	LeaseOwner     string             `gorm:"column:lease_owner;type:varchar(255)"`
	LeaseExpireAt  *time.Time         `gorm:"column:lease_expire_at"`
	CreatedAt      time.Time          `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time          `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName sets the table name for GORM
func (DeliveryTask) TableName() string { return "delivery_tasks" }

// DeliveryAttempt represents a single attempt to deliver a notification
type DeliveryAttempt struct {
	ID              string    `gorm:"primaryKey;column:id;type:varchar(64)"`
	TaskID          string    `gorm:"column:task_id;type:varchar(64);not null;index"`
	AttemptNo       int       `gorm:"column:attempt_no;not null"`
	RequestPayload  string    `gorm:"column:request_payload;type:text"`
	ResponsePayload string    `gorm:"column:response_payload;type:text"`
	Result          string    `gorm:"column:result;type:varchar(50);not null"`
	ErrorMessage    string    `gorm:"column:error_message;type:text"`
	StartedAt       time.Time `gorm:"column:started_at;autoCreateTime"`
	FinishedAt      time.Time `gorm:"column:finished_at"`
}

// TableName sets the table name for GORM
func (DeliveryAttempt) TableName() string { return "delivery_attempts" }

// ConfigChangeAudit represents a configuration change audit log
type ConfigChangeAudit struct {
	ID                    string    `gorm:"primaryKey;column:id;type:varchar(64)"`
	Operator              string    `gorm:"column:operator;type:varchar(255);not null;index"`
	Action                string    `gorm:"column:action;type:varchar(50);not null"`
	ResourceVersionBefore string    `gorm:"column:resource_version_before;type:varchar(100)"`
	ResourceVersionAfter  string    `gorm:"column:resource_version_after;type:varchar(100)"`
	ConfigSnapshot        string    `gorm:"column:config_snapshot;type:text"`
	CreatedAt             time.Time `gorm:"column:created_at;autoCreateTime;index"`
}

// TableName sets the table name for GORM
func (ConfigChangeAudit) TableName() string { return "config_change_audits" }

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

	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return nil
	}

	return json.Unmarshal(b, j)
}
