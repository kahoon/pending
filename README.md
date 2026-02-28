# pending

[![Go Reference](https://pkg.go.dev/badge/github.com/kahoon/pending.svg)](https://pkg.go.dev/github.com/kahoon/pending)
[![Go Report Card](https://goreportcard.com/badge/github.com/kahoon/pending)](https://goreportcard.com/report/github.com/kahoon/pending)
[![codecov](https://codecov.io/gh/kahoon/pending/branch/main/graph/badge.svg)](https://codecov.io/gh/kahoon/pending)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**pending** is a minimalist, context-aware deferred task scheduler for Go.

`pending` is designed for in-memory, ID-based deferred actions. It fits use cases like debouncing user input, handling hardware delays, or managing state-dependent timeouts.

## Why pending?

- **Pure Go**: Built entirely on the standard library.
- **Simple API**: `Schedule`, `Cancel`, and `Shutdown`.
- **Debouncing by ID**: Scheduling the same ID replaces the previous task.
- **Concurrency Limits**: Choose blocking or dropping behavior when at capacity.
- **Graceful Shutdown**: Cancel timers and wait for active tasks to finish.
- **Pluggable Telemetry**: Attach your own metrics/logging hooks.

## Installation

```bash
go get github.com/kahoon/pending
```

## Quick Start

```go
mgr := pending.NewManager(
    pending.WithLimit(5, pending.StrategyDrop),
)

defer mgr.Shutdown(context.Background())

mgr.Schedule("user:42:email", 2*time.Second, func(ctx context.Context) {
    // send email reminder
})

// reschedule with same ID (debounce)
mgr.Schedule("user:42:email", 2*time.Second, func(ctx context.Context) {
    // send latest reminder payload
})
```

## Cookbook

### Debouncing by ID

```go
mgr := pending.NewManager()

func onSensorDataReceived(sensorID string) {
    mgr.Schedule(sensorID, 10*time.Second, func(ctx context.Context) {
        fmt.Printf("Alert: Sensor %s went offline!\n", sensorID)
    })
}
```

### Manual Cancellation

```go
mgr := pending.NewManager()

mgr.Schedule("user_123_unlock", 30*time.Minute, unlockTask)

// User was unlocked manually, no need to run delayed task.
mgr.Cancel("user_123_unlock")
```

### Concurrency Limits (Drop)

```go
mgr := pending.NewManager(
    pending.WithLimit(5, pending.StrategyDrop),
)

for i := 0; i < 100; i++ {
    id := fmt.Sprintf("task_%d", i)
    mgr.Schedule(id, 1*time.Second, heavyDatabaseQuery)
}
```

When a task is dropped under `StrategyDrop`, your telemetry handler receives
`pending.ErrTaskDropped` via `OnFailed`, so you can match it with `errors.Is`.

### Graceful Shutdown

```go
mgr := pending.NewManager()

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := mgr.Shutdown(ctx); err != nil {
    log.Printf("shutdown timed out: %v", err)
}
```

## Benchmarks

Run benchmarks:

```bash
go test -run ^$ -bench BenchmarkManager_ -benchmem ./...
```

Sample output (darwin/arm64, Apple M4):

```text
BenchmarkManager_Schedule-10                     	  969188	       293.8 ns/op	     473 B/op	       6 allocs/op
BenchmarkManager_RescheduleSameID-10             	 1502884	       158.6 ns/op	     304 B/op	       5 allocs/op
BenchmarkManager_Cancel-10                       	 1280494	       188.9 ns/op	     311 B/op	       5 allocs/op
BenchmarkManager_Shutdown_NoRunningTasks-10      	   75316	      3226 ns/op	      16 B/op	       1 allocs/op
BenchmarkManager_Shutdown_WithRunningTasks-10    	   44101	      5451 ns/op	      16 B/op	       1 allocs/op
```

Results will vary by hardware, OS, and Go version.

## Scope

`pending` is not a cron replacement. It is intentionally focused on in-process deferred work with ID-based replacement and cancellation.

## Community

- Contributing guide: CONTRIBUTING.md
- Security policy: SECURITY.md
- Code of Conduct: CODE_OF_CONDUCT.md
- Changelog: CHANGELOG.md

## License

MIT. See LICENSE.
