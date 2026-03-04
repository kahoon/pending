package pending

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrTaskDropped is reported to TelemetryHandler.OnFailed when StrategyDrop
// rejects a task because no concurrency slot is available.
var ErrTaskDropped = errors.New("pending: task dropped due to concurrency limit")

// Status represents the current lifecycle state of a Manager.
type Status int

const (
	// StatusAccepting means the manager accepts new schedules.
	StatusAccepting Status = iota
	// StatusDraining means shutdown has started and running tasks are draining.
	StatusDraining
	// StatusClosed means shutdown has completed.
	StatusClosed
)

func (s Status) String() string {
	switch s {
	case StatusAccepting:
		return "accepting"
	case StatusDraining:
		return "draining"
	case StatusClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Stats is a point-in-time snapshot of manager state.
type Stats struct {
	// Pending is the number of scheduled tasks that are not currently executing.
	Pending int
	// Running is the number of tasks currently executing.
	Running int
	// Status indicates whether the manager is accepting new tasks, draining existing ones, or fully closed.
	Status Status
}

// Task defines the function signature for a scheduled action.
// The provided context is cancelled if the manager shuts down or the task is replaced.
type Task func(ctx context.Context)

// Manager coordinates the lifecycle of delayed tasks, ensuring thread-safety
// and providing concurrency control via semaphores.
type Manager struct {
	mu      sync.RWMutex
	pending map[string]*entry
	running int
	status  Status

	semaphore chan struct{}
	strategy  Strategy
	logger    TelemetryHandler

	wg           sync.WaitGroup
	shutdownOnce sync.Once
	shutdownDone chan struct{}
}

type entry struct {
	timer  *time.Timer
	cancel context.CancelFunc
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
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isClosed() {
		return
	}

	// Replace existing task if found.
	if old, exists := m.pending[id]; exists {
		old.timer.Stop()
		old.cancel()
		m.logger.OnRescheduled(id)
	} else {
		m.logger.OnScheduled(id, d)
	}

	ctx, cancel := context.WithCancel(context.Background())
	e := &entry{cancel: cancel}

	// Schedule the execution.
	e.timer = time.AfterFunc(d, func() {
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
			m.updateRunning(1)
			defer m.updateRunning(-1)

			start := time.Now()
			task(ctx)
			duration := time.Since(start)

			m.deleteIfCurrent(id, e)
			m.logger.OnExecuted(id, duration)
		})
	})

	m.pending[id] = e
}

// Stats returns a lock-safe snapshot of manager state.
func (m *Manager) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := len(m.pending) - m.running
	if pending < 0 {
		pending = 0
	}

	return Stats{
		Pending: pending,
		Running: m.running,
		Status:  m.status,
	}
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

func (m *Manager) updateRunning(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running += delta
}

func (m *Manager) deleteIfCurrent(id string, target *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if current, ok := m.pending[id]; ok && current == target {
		delete(m.pending, id)
	}
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
