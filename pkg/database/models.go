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

// Template is a reusable notification template stored in the database.
// Templates are channel-specific and use Go text/template syntax for body/subject rendering.
type Template struct {
	ID          string `gorm:"primaryKey;column:id;type:varchar(64)"              json:"id"`
	Name        string `gorm:"uniqueIndex;column:name;type:varchar(255);not null"  json:"name"`
	Channel     string `gorm:"column:channel;type:varchar(50);not null;index"      json:"channel"`
	Description string `gorm:"column:description;type:text"                        json:"description,omitempty"`
	// Subject is rendered as a Go template (for email).
	Subject string `gorm:"column:subject;type:text"                            json:"subject,omitempty"`
	// Body is rendered as a Go template. For SMS/voice leave empty and set TemplateCode.
	Body string `gorm:"column:body;type:text"                               json:"body,omitempty"`
	// TemplateCode is the provider-side template identifier (SMS/voice).
	TemplateCode string `gorm:"column:template_code;type:varchar(255)"              json:"templateCode,omitempty"`
	// MsgType controls rendering for Feishu (text, post, interactive).
	MsgType string `gorm:"column:msg_type;type:varchar(50)"                    json:"msgType,omitempty"`
	// Params declares expected parameter keys and their descriptions for documentation.
	Params    JSONMap   `gorm:"column:params;type:jsonb"                            json:"params,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"                    json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"                    json:"updatedAt"`
}

// TableName sets the table name for GORM
func (Template) TableName() string { return "templates" }

// Notification represents a notification request.
// Content lives in the template; all per-notification data lives in recipients.
type Notification struct {
	ID             string             `gorm:"primaryKey;column:id;type:varchar(64)"                        json:"id"`
	IdempotencyKey string             `gorm:"uniqueIndex;column:idempotency_key;type:varchar(255);not null" json:"idempotencyKey"`
	Status         NotificationStatus `gorm:"column:status;type:varchar(50);not null;default:pending"       json:"status"`
	CreatedAt      time.Time          `gorm:"column:created_at;autoCreateTime"                              json:"createdAt"`
	UpdatedAt      time.Time          `gorm:"column:updated_at;autoUpdateTime"                              json:"updatedAt"`
}

// TableName sets the table name for GORM
func (Notification) TableName() string { return "notifications" }

// NotificationRecipient stores all KV data for one recipient.
// Params contains both channel identifier keys (email, phone, feishu_user_id, user_id)
// and template variable keys used during rendering.
type NotificationRecipient struct {
	ID             string    `gorm:"primaryKey;column:id;type:varchar(64)"              json:"id"`
	NotificationID string    `gorm:"column:notification_id;type:varchar(64);not null;index" json:"notificationId"`
	Params         JSONMap   `gorm:"column:params;type:jsonb;not null"                   json:"params"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"                    json:"createdAt"`
}

// TableName sets the table name for GORM
func (NotificationRecipient) TableName() string { return "notification_recipients" }

// DeliveryTask represents a task to deliver a notification to one recipient via one channel.
type DeliveryTask struct {
	ID             string             `gorm:"primaryKey;column:id;type:varchar(64)"              json:"id"`
	NotificationID string             `gorm:"column:notification_id;type:varchar(64);not null;index" json:"notificationId"`
	RecipientID    string             `gorm:"column:recipient_id;type:varchar(64);not null"       json:"recipientId"`
	Channel        string             `gorm:"column:channel;type:varchar(50);not null"            json:"channel"`
	Provider       string             `gorm:"column:provider;type:varchar(100);not null"          json:"provider"`
	TemplateName   string             `gorm:"column:template_name;type:varchar(255);not null"     json:"templateName"`
	Status         DeliveryTaskStatus `gorm:"column:status;type:varchar(50);not null;default:pending;index" json:"status"`
	RetryCount     int                `gorm:"column:retry_count;not null;default:0"               json:"retryCount"`
	MaxRetry       int                `gorm:"column:max_retry;not null;default:3"                 json:"maxRetry"`
	NextRetryAt    *time.Time         `gorm:"column:next_retry_at"                                json:"nextRetryAt,omitempty"`
	LastError      string             `gorm:"column:last_error;type:text"                         json:"lastError,omitempty"`
	LeaseOwner     string             `gorm:"column:lease_owner;type:varchar(255)"                json:"-"`
	LeaseExpireAt  *time.Time         `gorm:"column:lease_expire_at"                              json:"-"`
	CreatedAt      time.Time          `gorm:"column:created_at;autoCreateTime"                    json:"createdAt"`
	UpdatedAt      time.Time          `gorm:"column:updated_at;autoUpdateTime"                    json:"updatedAt"`
}

// TableName sets the table name for GORM
func (DeliveryTask) TableName() string { return "delivery_tasks" }

// DeliveryAttempt represents a single attempt to deliver a notification
type DeliveryAttempt struct {
	ID              string    `gorm:"primaryKey;column:id;type:varchar(64)"        json:"id"`
	TaskID          string    `gorm:"column:task_id;type:varchar(64);not null;index" json:"taskId"`
	AttemptNo       int       `gorm:"column:attempt_no;not null"                   json:"attemptNo"`
	RequestPayload  string    `gorm:"column:request_payload;type:text"             json:"requestPayload,omitempty"`
	ResponsePayload string    `gorm:"column:response_payload;type:text"            json:"responsePayload,omitempty"`
	Result          string    `gorm:"column:result;type:varchar(50);not null"      json:"result"`
	ErrorMessage    string    `gorm:"column:error_message;type:text"               json:"errorMessage,omitempty"`
	StartedAt       time.Time `gorm:"column:started_at;autoCreateTime"             json:"startedAt"`
	FinishedAt      time.Time `gorm:"column:finished_at"                           json:"finishedAt"`
}

// TableName sets the table name for GORM
func (DeliveryAttempt) TableName() string { return "delivery_attempts" }

// ConfigChangeAudit represents a configuration change audit log
type ConfigChangeAudit struct {
	ID                    string    `gorm:"primaryKey;column:id;type:varchar(64)"                    json:"id"`
	Operator              string    `gorm:"column:operator;type:varchar(255);not null;index"         json:"operator"`
	Action                string    `gorm:"column:action;type:varchar(50);not null"                  json:"action"`
	ResourceVersionBefore string    `gorm:"column:resource_version_before;type:varchar(100)"         json:"resourceVersionBefore,omitempty"`
	ResourceVersionAfter  string    `gorm:"column:resource_version_after;type:varchar(100)"          json:"resourceVersionAfter,omitempty"`
	ConfigSnapshot        string    `gorm:"column:config_snapshot;type:text"                         json:"configSnapshot,omitempty"`
	CreatedAt             time.Time `gorm:"column:created_at;autoCreateTime;index"                   json:"createdAt"`
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

// Scan implements sql.Scanner interface — handles both []byte and string from various drivers
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
