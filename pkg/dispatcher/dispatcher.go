// Package dispatcher provides task dispatching functionality
package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/adapter"
	"github.com/labring/sealos-notify/pkg/config"
	"github.com/labring/sealos-notify/pkg/database"
	"github.com/labring/sealos-notify/pkg/storage"
	log "github.com/sirupsen/logrus"
)

// Dispatcher handles task polling and execution
type Dispatcher struct {
	config               *config.GlobalConfig
	deliveryTaskStore    *storage.DeliveryTaskStore
	deliveryAttemptStore *storage.DeliveryAttemptStore
	notificationStore    *storage.NotificationStore
	recipientStore       *storage.RecipientStore
	adapters             map[string]adapter.Adapter
	leaseOwner           string
	cancel               context.CancelFunc
	wg                   sync.WaitGroup
	logger               *log.Entry
}

// New creates a new dispatcher
func New(
	cfg *config.GlobalConfig,
	deliveryTaskStore *storage.DeliveryTaskStore,
	deliveryAttemptStore *storage.DeliveryAttemptStore,
	notificationStore *storage.NotificationStore,
	recipientStore *storage.RecipientStore,
	adapters map[string]adapter.Adapter,
	logger *log.Entry,
) *Dispatcher {
	if logger == nil {
		logger = log.WithField("component", "dispatcher")
	}

	return &Dispatcher{
		config:               cfg,
		deliveryTaskStore:    deliveryTaskStore,
		deliveryAttemptStore: deliveryAttemptStore,
		notificationStore:    notificationStore,
		recipientStore:       recipientStore,
		adapters:             adapters,
		leaseOwner:           uuid.New().String(),
		logger:               logger,
	}
}

// Start begins the dispatcher loop
func (d *Dispatcher) Start(ctx context.Context) error {
	if !d.config.Dispatcher.Enabled {
		d.logger.Info("Dispatcher is disabled")
		return nil
	}

	ctx, d.cancel = context.WithCancel(ctx)

	d.logger.WithFields(log.Fields{
		"lease_owner": d.leaseOwner,
		"interval":    d.config.Dispatcher.Interval,
		"batch_size":  d.config.Dispatcher.BatchSize,
	}).Info("Dispatcher started")

	d.wg.Add(1)
	go d.dispatchLoop(ctx)

	return nil
}

// Stop stops the dispatcher and waits for all in-flight tasks to complete
func (d *Dispatcher) Stop() error {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
	d.logger.Info("Dispatcher stopped")
	return nil
}

// dispatchLoop is the main dispatch loop, driven by a ticker and context cancellation
func (d *Dispatcher) dispatchLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.Dispatcher.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.dispatchBatch(ctx)
		}
	}
}

// dispatchBatch acquires and dispatches pending and retry tasks concurrently
func (d *Dispatcher) dispatchBatch(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		pendingTasks, err := d.deliveryTaskStore.AcquirePendingTasks(
			ctx,
			d.leaseOwner,
			d.config.Dispatcher.LeaseTimeout,
			d.config.Dispatcher.BatchSize,
		)
		if err != nil {
			d.logger.WithError(err).Error("Failed to acquire pending tasks")
			return
		}
		if len(pendingTasks) > 0 {
			d.logger.WithField("count", len(pendingTasks)).Debug("Acquired pending tasks")
			d.processTasks(ctx, pendingTasks)
		}
	}()

	go func() {
		defer wg.Done()
		retryTasks, err := d.deliveryTaskStore.AcquireRetryTasks(
			ctx,
			d.leaseOwner,
			d.config.Dispatcher.LeaseTimeout,
			d.config.Dispatcher.BatchSize,
		)
		if err != nil {
			d.logger.WithError(err).Error("Failed to acquire retry tasks")
			return
		}
		if len(retryTasks) > 0 {
			d.logger.WithField("count", len(retryTasks)).Debug("Acquired retry tasks")
			d.processTasks(ctx, retryTasks)
		}
	}()

	wg.Wait()
}

// processTasks spawns a goroutine for each task
func (d *Dispatcher) processTasks(ctx context.Context, tasks []*database.DeliveryTask) {
	for _, task := range tasks {
		d.wg.Add(1)
		go func(t *database.DeliveryTask) {
			defer d.wg.Done()
			d.processTask(ctx, t)
		}(task)
	}
}

// processTask executes a single delivery task
func (d *Dispatcher) processTask(ctx context.Context, task *database.DeliveryTask) {
	logger := d.logger.WithFields(log.Fields{
		"task_id":         task.ID,
		"notification_id": task.NotificationID,
		"channel":         task.Channel,
		"provider":        task.Provider,
		"retry_count":     task.RetryCount,
	})

	logger.Debug("Processing task")

	// Get adapter for this channel/provider
	adapterInstance, ok := d.adapters[task.Provider]
	if !ok {
		logger.Errorf("Adapter not found for provider: %s", task.Provider)
		d.handleTaskFailure(ctx, task, fmt.Sprintf("adapter not found: %s", task.Provider), nil)
		return
	}

	// Get notification details
	notification, err := d.notificationStore.GetByID(ctx, task.NotificationID)
	if err != nil {
		logger.WithError(err).Error("Failed to get notification")
		d.handleTaskFailure(ctx, task, fmt.Sprintf("failed to get notification: %v", err), nil)
		return
	}

	// Get recipient details
	recipient, err := d.recipientStore.GetByID(ctx, task.RecipientID)
	if err != nil {
		logger.WithError(err).Error("Failed to get recipient")
		d.handleTaskFailure(ctx, task, fmt.Sprintf("failed to get recipient: %v", err), nil)
		return
	}

	// Build send request
	sendReq := &adapter.SendRequest{
		RecipientType:  recipient.RecipientType,
		RecipientValue: recipient.RecipientValue,
		Title:          notification.Title,
		Content:        notification.Content,
		Variables:      notification.VariablesJSON,
		Metadata:       map[string]string{},
	}

	startTime := time.Now()

	// Send notification
	response, sendErr := adapterInstance.Send(ctx, sendReq)

	// Record attempt
	attempt := &database.DeliveryAttempt{
		TaskID:     task.ID,
		AttemptNo:  task.RetryCount + 1,
		Result:     "success",
		StartedAt:  startTime,
		FinishedAt: time.Now(),
	}

	if sendErr != nil || (response != nil && !response.Success) {
		attempt.Result = "failed"
		if sendErr != nil {
			attempt.ErrorMessage = sendErr.Error()
		} else if response.Error != nil {
			attempt.ErrorMessage = response.Error.Error()
		}
	}

	if err := d.deliveryAttemptStore.Create(ctx, attempt); err != nil {
		logger.WithError(err).Error("Failed to record delivery attempt")
	}

	// Handle result
	if sendErr != nil || (response != nil && !response.Success) {
		errorMsg := "unknown error"
		if sendErr != nil {
			errorMsg = sendErr.Error()
		} else if response != nil && response.Error != nil {
			errorMsg = response.Error.Error()
		}
		d.handleTaskFailure(ctx, task, errorMsg, nil)
		logger.WithError(sendErr).Error("Task failed")
	} else {
		d.handleTaskSuccess(ctx, task)
		logger.Info("Task completed successfully")
	}
}

// handleTaskSuccess marks the task as successfully completed
func (d *Dispatcher) handleTaskSuccess(ctx context.Context, task *database.DeliveryTask) {
	if err := d.deliveryTaskStore.UpdateSuccess(ctx, task.ID); err != nil {
		d.logger.WithError(err).WithField("task_id", task.ID).Error("Failed to update task success")
	}
}

// handleTaskFailure handles task failure and retry scheduling
func (d *Dispatcher) handleTaskFailure(ctx context.Context, task *database.DeliveryTask, errorMsg string, _ *time.Duration) {
	var retryAfter *time.Duration
	if task.RetryCount < task.MaxRetry {
		backoffIndex := task.RetryCount
		if backoffIndex >= len(d.config.Defaults.RetryBackoffSeconds) {
			backoffIndex = len(d.config.Defaults.RetryBackoffSeconds) - 1
		}
		duration := time.Duration(d.config.Defaults.RetryBackoffSeconds[backoffIndex]) * time.Second
		retryAfter = &duration
	}

	if err := d.deliveryTaskStore.UpdateFailure(ctx, task.ID, errorMsg, retryAfter); err != nil {
		d.logger.WithError(err).WithField("task_id", task.ID).Error("Failed to update task failure")
	}
}
