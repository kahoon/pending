package pending

import (
	"context"
	"errors"
	"time"
)

type exampleTelemetry struct{}

func (exampleTelemetry) OnScheduled(id string, d time.Duration)       {}
func (exampleTelemetry) OnRescheduled(id string)                      {}
func (exampleTelemetry) OnExecuted(id string, duration time.Duration) {}
func (exampleTelemetry) OnCancelled(id string)                        {}
func (exampleTelemetry) OnFailed(id string, err error) {
	_ = errors.Is(err, ErrTaskDropped)
}

func ExampleNewManager() {
	mgr := NewManager(
		WithLimit(1, StrategyDrop),
		WithLogger(exampleTelemetry{}),
	)
	defer mgr.Shutdown(context.Background())

	mgr.Schedule("email:user-42", 2*time.Second, func(ctx context.Context) {})
}
