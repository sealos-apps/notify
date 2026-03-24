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
	adapters             map[string]adapter.Adapter
	leaseOwner           string
	stopCh               chan struct{}
	wg                   sync.WaitGroup
	logger               *log.Entry
}

// New creates a new dispatcher
func New(
	cfg *config.GlobalConfig,
	deliveryTaskStore *storage.DeliveryTaskStore,
	deliveryAttemptStore *storage.DeliveryAttemptStore,
	notificationStore *storage.NotificationStore,
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
		adapters:             adapters,
		leaseOwner:           uuid.New().String(),
		stopCh:               make(chan struct{}),
		logger:               logger,
	}
}

// Start begins the dispatcher loop
func (d *Dispatcher) Start(ctx context.Context) error {
	if !d.config.Dispatcher.Enabled {
		d.logger.Info("Dispatcher is disabled")
		return nil
	}

	d.logger.WithFields(log.Fields{
		"lease_owner": d.leaseOwner,
		"interval":    d.config.Dispatcher.Interval,
		"batch_size":  d.config.Dispatcher.BatchSize,
	}).Info("Dispatcher started")

	d.wg.Add(1)
	go d.dispatchLoop(ctx)

	return nil
}

// Stop stops the dispatcher
func (d *Dispatcher) Stop() error {
	close(d.stopCh)
	d.wg.Wait()
	d.logger.Info("Dispatcher stopped")
	return nil
}

// dispatchLoop is the main dispatch loop
func (d *Dispatcher) dispatchLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.Dispatcher.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.dispatchBatch(ctx)
		}
	}
}

// dispatchBatch dispatches a batch of tasks
func (d *Dispatcher) dispatchBatch(ctx context.Context) {
	// Acquire pending tasks
	pendingTasks, err := d.deliveryTaskStore.AcquirePendingTasks(
		ctx,
		d.leaseOwner,
		d.config.Dispatcher.LeaseTimeout,
		d.config.Dispatcher.BatchSize,
	)
	if err != nil {
		d.logger.WithError(err).Error("Failed to acquire pending tasks")
	} else if len(pendingTasks) > 0 {
		d.logger.WithField("count", len(pendingTasks)).Debug("Acquired pending tasks")
		d.processTasks(ctx, pendingTasks)
	}

	// Acquire retry tasks
	retryTasks, err := d.deliveryTaskStore.AcquireRetryTasks(
		ctx,
		d.leaseOwner,
		d.config.Dispatcher.LeaseTimeout,
		d.config.Dispatcher.BatchSize,
	)
	if err != nil {
		d.logger.WithError(err).Error("Failed to acquire retry tasks")
	} else if len(retryTasks) > 0 {
		d.logger.WithField("count", len(retryTasks)).Debug("Acquired retry tasks")
		d.processTasks(ctx, retryTasks)
	}
}

// processTasks processes a batch of tasks
func (d *Dispatcher) processTasks(ctx context.Context, tasks []*database.DeliveryTask) {
	for _, task := range tasks {
		// Process each task in a goroutine
		d.wg.Add(1)
		go func(t *database.DeliveryTask) {
			defer d.wg.Done()
			d.processTask(ctx, t)
		}(task)
	}
}

// processTask processes a single delivery task
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
	adapter, ok := d.adapters[task.Provider]
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

	// Create send request
	sendReq := &adapter.SendRequest{
		Title:     notification.Title,
		Content:   notification.Content,
		Variables: notification.VariablesJSON,
		Metadata:  map[string]string{},
	}

	// Record attempt start time
	startTime := time.Now()

	// Send notification
	response, err := adapter.Send(ctx, sendReq)

	// Record attempt
	attempt := &database.DeliveryAttempt{
		TaskID:     task.ID,
		AttemptNo:  task.RetryCount + 1,
		Result:     "success",
		StartedAt:  startTime,
		FinishedAt: time.Now(),
	}

	if err != nil || !response.Success {
		attempt.Result = "failed"
		if err != nil {
			attempt.ErrorMessage = err.Error()
		} else if response.Error != nil {
			attempt.ErrorMessage = response.Error.Error()
		}
	}

	if err := d.deliveryAttemptStore.Create(ctx, attempt); err != nil {
		logger.WithError(err).Error("Failed to record delivery attempt")
	}

	// Handle result
	if err != nil || !response.Success {
		errorMsg := "unknown error"
		if err != nil {
			errorMsg = err.Error()
		} else if response.Error != nil {
			errorMsg = response.Error.Error()
		}

		d.handleTaskFailure(ctx, task, errorMsg, nil)
		logger.WithError(err).Error("Task failed")
	} else {
		d.handleTaskSuccess(ctx, task)
		logger.Info("Task completed successfully")
	}
}

// handleTaskSuccess handles successful task completion
func (d *Dispatcher) handleTaskSuccess(ctx context.Context, task *database.DeliveryTask) {
	if err := d.deliveryTaskStore.UpdateSuccess(ctx, task.ID); err != nil {
		d.logger.WithError(err).WithField("task_id", task.ID).Error("Failed to update task success")
	}
}

// handleTaskFailure handles task failure and retry logic
func (d *Dispatcher) handleTaskFailure(ctx context.Context, task *database.DeliveryTask, errorMsg string, retryAfter *time.Duration) {
	// Determine if we should retry
	var calculatedRetryAfter *time.Duration
	if task.RetryCount < task.MaxRetry {
		// Calculate backoff duration
		backoffIndex := task.RetryCount
		if backoffIndex >= len(d.config.Defaults.RetryBackoffSeconds) {
			backoffIndex = len(d.config.Defaults.RetryBackoffSeconds) - 1
		}

		duration := time.Duration(d.config.Defaults.RetryBackoffSeconds[backoffIndex]) * time.Second
		calculatedRetryAfter = &duration
	}

	if err := d.deliveryTaskStore.UpdateFailure(ctx, task.ID, errorMsg, calculatedRetryAfter); err != nil {
		d.logger.WithError(err).WithField("task_id", task.ID).Error("Failed to update task failure")
	}
}
