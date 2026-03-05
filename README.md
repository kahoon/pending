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
- **Runtime Stats**: Read pending/running/status state via `Stats()`.
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

### Debouncing User Events

```go
mgr := pending.NewManager()

func onSearchInput(userID, query string) {
    key := "search:" + userID
    mgr.Schedule(key, 300*time.Millisecond, func(ctx context.Context) {
        if ctx.Err() != nil {
            return
        }
        runSearch(query)
    })
}
```

### Resettable State Timeout

```go
mgr := pending.NewManager()

func onSessionActivity(sessionID string) {
    key := "session-timeout:" + sessionID
    mgr.Schedule(key, 15*time.Minute, func(ctx context.Context) {
        if ctx.Err() != nil {
            return
        }
        expireSession(sessionID)
    })
}
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

### Delayed Retry with Cancellation

```go
mgr := pending.NewManager()

func scheduleRetry(jobID string, attempt int) {
    key := "retry:" + jobID
    delay := time.Duration(attempt) * time.Second

    mgr.Schedule(key, delay, func(ctx context.Context) {
        if ctx.Err() != nil {
            return
        }
        if err := sendWebhook(jobID); err != nil {
            scheduleRetry(jobID, attempt+1)
        }
    })
}

// Stop any pending retry if the job succeeds elsewhere.
func onJobSucceeded(jobID string) {
    mgr.Cancel("retry:" + jobID)
}
```

### Safe Service Shutdown Wiring in `main()`

```go
func main() {
    mgr := pending.NewManager()
    defer func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        _ = mgr.Shutdown(ctx)
    }()

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    defer signal.Stop(sig)

    go runHTTPServer(mgr)

    <-sig
    log.Println("signal received, draining pending tasks")
}
```

### Manual Cancellation

```go
mgr := pending.NewManager()

mgr.Schedule("user_123_unlock", 30*time.Minute, unlockTask)

// User was unlocked manually, no need to run delayed task.
mgr.Cancel("user_123_unlock")
```

### Runtime Stats

```go
s := mgr.Stats()
log.Printf("pending=%d running=%d status=%s", s.Pending, s.Running, s.Status)
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

- Contributing guide: [CONTRIBUTING](CONTRIBUTING.md)
- Security policy: [SECURITY](SECURITY.md)
- Code of Conduct: [CODE_OF_CONDUCT](CODE_OF_CONDUCT.md)
- Changelog: [CHANGELOG](CHANGELOG.md)

## License

MIT. See [LICENSE](LICENSE).
