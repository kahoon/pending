package pending

import "time"

// TelemetryHandler receives lifecycle events for scheduled tasks.
//
// Implementations can be used to emit logs, metrics, or traces.
type TelemetryHandler interface {
	OnScheduled(id string, d time.Duration)
	OnRescheduled(id string)
	OnExecuted(id string, duration time.Duration)
	OnCancelled(id string)
	OnFailed(id string, err error)
}

type nopLogger struct{}

func (n nopLogger) OnScheduled(id string, d time.Duration)  {}
func (n nopLogger) OnRescheduled(id string)                 {}
func (n nopLogger) OnExecuted(id string, dur time.Duration) {}
func (n nopLogger) OnCancelled(id string)                   {}
func (n nopLogger) OnFailed(id string, err error)           {}
