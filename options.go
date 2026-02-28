package pending

// Strategy defines how the manager behaves when the concurrency limit is reached.
type Strategy int

const (
	// StrategyBlock waits for a concurrency slot to become available.
	StrategyBlock Strategy = iota
	// StrategyDrop ignores the execution if the concurrency limit is reached.
	StrategyDrop
)

type Option func(*Manager)

// WithLimit sets the maximum number of concurrent tasks.
func WithLimit(limit int, strategy Strategy) Option {
	return func(m *Manager) {
		if limit > 0 {
			m.semaphore = make(chan struct{}, limit)
			m.strategy = strategy
		}
	}
}

// WithLogger attaches a custom TelemetryHandler.
func WithLogger(logger TelemetryHandler) Option {
	return func(m *Manager) {
		if logger != nil {
			m.logger = logger
		}
	}
}
