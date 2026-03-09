package state

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DefaultAutoSaveInterval is the default interval between automatic checkpoint saves.
const DefaultAutoSaveInterval = 5 * time.Minute

// AutoSaver periodically saves checkpoints for a running task.
type AutoSaver struct {
	store    *StateStore
	interval time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewAutoSaver creates a new AutoSaver with the given store and interval.
// If interval is zero, DefaultAutoSaveInterval is used.
func NewAutoSaver(store *StateStore, interval time.Duration) *AutoSaver {
	if interval <= 0 {
		interval = DefaultAutoSaveInterval
	}
	return &AutoSaver{
		store:    store,
		interval: interval,
	}
}

// Start begins periodic checkpoint saving in a background goroutine.
// It saves a live checkpoint (without stopping containers) at each tick.
// Calling Start while already running is a no-op.
func (a *AutoSaver) Start(ctx context.Context, taskID string, opts SaveOpts) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		return // already running
	}

	autoCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.done = make(chan struct{})

	go a.run(autoCtx, taskID, opts)
}

// Stop stops the auto-save goroutine and waits for it to finish.
func (a *AutoSaver) Stop() {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	a.cancel = nil
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// run is the auto-save loop that ticks at the configured interval.
func (a *AutoSaver) run(ctx context.Context, taskID string, opts SaveOpts) {
	defer close(a.done)

	logger := slog.With("task_id", taskID, "component", "autosaver")
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	logger.Info("auto-save started", "interval", a.interval)

	for {
		select {
		case <-ticker.C:
			logger.Debug("auto-save tick")
			if _, err := a.store.SaveCheckpointLive(ctx, taskID, opts); err != nil {
				logger.Warn("auto-save checkpoint failed", "error", err)
			} else {
				logger.Info("auto-save checkpoint created")
			}
		case <-ctx.Done():
			logger.Info("auto-save stopped")
			return
		}
	}
}
