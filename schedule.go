package pending

import (
	"errors"
	"time"
)

// ErrInvalidScheduleOptions is returned when mutually exclusive schedule options are both set.
var ErrInvalidScheduleOptions = errors.New("pending: delay and at cannot both be set")

// ScheduleOptions configures advanced scheduling behavior.
type ScheduleOptions struct {
	// Delay schedules execution after the given duration.
	Delay time.Duration
	// At schedules execution at an absolute time.
	// Delay and At are mutually exclusive.
	At time.Time
	// IfAbsent only schedules when no task with the same ID exists.
	IfAbsent bool
	// Group is reserved for future grouped task operations.
	Group string
	// Retry is reserved for future retry support.
	Retry RetryPolicy
}

// RetryPolicy defines retry behavior for error-returning tasks.
// The zero value disables retries.
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Jitter         float64
}

type scheduleConfig struct {
	delay    time.Duration
	ifAbsent bool
}

func newConfig() scheduleConfig {
	return scheduleConfig{}
}

func (c scheduleConfig) validate(opt ScheduleOptions) (scheduleConfig, error) {
	c.ifAbsent = opt.IfAbsent

	hasDelay := opt.Delay != 0
	hasAt := !opt.At.IsZero()

	if hasDelay && hasAt {
		return c, ErrInvalidScheduleOptions
	}
	if hasAt {
		d := time.Until(opt.At)
		d = max(d, 0)
		c.delay = d
		return c, nil
	}

	c.delay = opt.Delay
	return c, nil
}
