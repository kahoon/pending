package pending

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrTaskDropped is reported to TelemetryHandler.OnFailed when StrategyDrop
	// rejects a task because no concurrency slot is available.
	ErrTaskDropped = errors.New("pending: task dropped due to concurrency limit")
	// ErrManagerNotAccepting is returned by ScheduleWith when shutdown has started.
	ErrManagerNotAccepting = errors.New("pending: manager is not accepting new tasks")
)

// Task defines the function signature for a scheduled action.
// The provided context is cancelled if the manager shuts down or the task is replaced.
type Task func(ctx context.Context)

// TaskWithError defines an error-returning task callback.
//
// Returning a non-nil error causes TelemetryHandler.OnFailed to be called.
type TaskWithError func(ctx context.Context) error

// Manager coordinates the lifecycle of delayed tasks, ensuring thread-safety
// and providing concurrency control via semaphores.
type Manager struct {
	mu      sync.RWMutex
	pending map[string]*entry
	running atomic.Int32
	status  Status

	semaphore chan struct{}
	strategy  Strategy
	logger    TelemetryHandler

	wg           sync.WaitGroup
	shutdownOnce sync.Once
	shutdownDone chan struct{}
}

type entry struct {
	timer    *time.Timer
	deadline time.Time
	started  atomic.Bool
	cancel   context.CancelFunc
}

// NewManager initializes a new Manager with the provided options.
func NewManager(opts ...Option) *Manager {
	m := &Manager{
		pending:      make(map[string]*entry),
		status:       StatusAccepting,
		logger:       nopLogger{},
		shutdownDone: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Schedule plans a task for execution after duration d.
// If a task with the same id already exists, the previous one is cancelled
// and replaced (debouncing). If the manager is not accepting new tasks, Schedule does nothing.
func (m *Manager) Schedule(id string, d time.Duration, task Task) {
	_, _ = m.ScheduleWith(id, func() TaskWithError {
		return func(ctx context.Context) error {
			task(ctx)
			return nil
		}
	}(), ScheduleOptions{Delay: d})
}

// ScheduleWith schedules an error-returning task with advanced options.
//
// It returns scheduled=false when IfAbsent is true and an existing task with the
// same ID is already present.
func (m *Manager) ScheduleWith(id string, task TaskWithError, opt ScheduleOptions) (scheduled bool, err error) {
	cfg, err := newConfig().validate(opt)
	if err != nil {
		return false, err
	}
	return m.schedule(id, task, cfg)
}

// Cancel immediately stops a pending task by its ID and prevents it from running.
// If the task is already running, its context is cancelled.
func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.pending[id]; ok {
		e.timer.Stop()
		e.cancel()
		delete(m.pending, id)
		m.logger.OnCancelled(id)
	}
}

// TimeRemaining returns the remaining time until a pending task's timer fires.
// If the task does not exist or is no longer pending, it returns zero.
func (m *Manager) TimeRemaining(id string) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	task, ok := m.pending[id]
	if !ok || task.started.Load() {
		return 0
	}

	remaining := time.Until(task.deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// IsPending reports whether a task exists and has not started execution yet.
func (m *Manager) IsPending(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.pending[id]
	return ok && !task.started.Load()
}

// Shutdown stops the manager, cancels all pending timers, and waits for
// currently executing tasks to complete or for the context to time out.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.shutdownOnce.Do(func() {
		m.mu.Lock()
		m.status = StatusDraining

		for id, e := range m.pending {
			e.timer.Stop()
			e.cancel()
			delete(m.pending, id)
			m.logger.OnCancelled(id)
		}
		m.mu.Unlock()

		go func() {
			m.wg.Wait()
			m.mu.Lock()
			m.status = StatusClosed
			m.mu.Unlock()
			close(m.shutdownDone)
		}()
	})

	select {
	case <-m.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) schedule(id string, task TaskWithError, cfg scheduleConfig) (scheduled bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isClosed() {
		return false, ErrManagerNotAccepting
	}

	// Replace existing task if found.
	if old, exists := m.pending[id]; exists {
		if cfg.ifAbsent {
			return false, nil
		}
		if cfg.skipIfRunning && old.started.Load() {
			return false, nil
		}
		old.timer.Stop()
		old.cancel()
		m.logger.OnRescheduled(id)
	} else {
		m.logger.OnScheduled(id, cfg.delay)
	}

	ctx, cancel := context.WithCancel(context.Background())
	e := &entry{cancel: cancel, deadline: time.Now().Add(cfg.delay)}

	// Schedule the execution.
	e.timer = time.AfterFunc(cfg.delay, func() {
		e.started.Store(true)

		// Hold a read lock so Shutdown cannot reach wg.Wait before wg.Go is called.
		m.mu.RLock()
		defer m.mu.RUnlock()

		if m.isClosed() {
			cancel()
			return
		}

		m.wg.Go(func() {
			defer cancel()

			if !m.acquireSlot(ctx, id, e) {
				return
			}
			defer m.releaseSlot()
			m.running.Add(1)
			defer m.running.Add(-1)

			m.logger.OnExecuting(id)
			start := time.Now()
			err := task(ctx)
			duration := time.Since(start)

			m.deleteIfCurrent(id, e)
			m.logger.OnExecuted(id, duration)
			if err != nil {
				m.logger.OnFailed(id, err)
			}
		})
	})

	m.pending[id] = e
	return true, nil
}

func (m *Manager) isClosed() bool {
	return m.status != StatusAccepting
}

func (m *Manager) acquireSlot(ctx context.Context, id string, e *entry) bool {
	if m.semaphore == nil {
		return true
	}

	if m.strategy == StrategyDrop {
		select {
		case m.semaphore <- struct{}{}:
			return true
		default:
			m.logger.OnFailed(id, ErrTaskDropped)
			m.deleteIfCurrent(id, e)
			return false
		}
	}

	select {
	case m.semaphore <- struct{}{}:
		return true
	case <-ctx.Done():
		// The task was canceled while waiting for capacity (Cancel/Shutdown/reschedule).
		m.deleteIfCurrent(id, e)
		return false
	}
}

func (m *Manager) releaseSlot() {
	if m.semaphore != nil {
		<-m.semaphore
	}
}

func (m *Manager) deleteIfCurrent(id string, target *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if current, ok := m.pending[id]; ok && current == target {
		delete(m.pending, id)
	}
}
