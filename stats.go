package pending

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

// Stats returns a lock-safe snapshot of manager state.
func (m *Manager) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := max(len(m.pending)-int(m.running.Load()), 0)

	return Stats{
		Pending: pending,
		Running: int(m.running.Load()),
		Status:  m.status,
	}
}
