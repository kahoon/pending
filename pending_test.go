package pending

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type spyLogger struct {
	nopLogger
	mu        sync.Mutex
	dropped   bool
	failedSig chan struct{}
}

func (s *spyLogger) OnFailed(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropped = true
	if s.failedSig != nil {
		select {
		case s.failedSig <- struct{}{}:
		default:
		}
	}
}

func TestManager_StrategyDrop(t *testing.T) {
	spy := &spyLogger{failedSig: make(chan struct{}, 1)}
	mgr := NewManager(WithLimit(1, StrategyDrop), WithLogger(spy))

	running := make(chan struct{})
	release := make(chan struct{})

	mgr.Schedule("t1", 1*time.Millisecond, func(ctx context.Context) {
		close(running)
		<-release
	})

	<-running

	mgr.Schedule("t2", 1*time.Millisecond, func(ctx context.Context) {})

	select {
	case <-spy.failedSig:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected task 2 to be dropped")
	}

	close(release)
}

func TestManager_Shutdown(t *testing.T) {
	mgr := NewManager()
	mgr.Schedule("slow", 1*time.Hour, func(ctx context.Context) {})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := mgr.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestManager_StrategyBlock(t *testing.T) {
	mgr := NewManager(WithLimit(1, StrategyBlock))

	firstRunning := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondRan := make(chan struct{})

	mgr.Schedule("t1", 1*time.Millisecond, func(ctx context.Context) {
		close(firstRunning)
		<-releaseFirst
	})

	<-firstRunning

	mgr.Schedule("t2", 1*time.Millisecond, func(ctx context.Context) {
		close(secondRan)
	})

	select {
	case <-secondRan:
		t.Fatal("task 2 should not run before first task releases slot")
	case <-time.After(30 * time.Millisecond):
	}

	close(releaseFirst)

	select {
	case <-secondRan:
	case <-time.After(200 * time.Millisecond):
		t.Error("task 2 blocked for too long or deadlocked")
	}
}

func TestManager_StrategyBlockCancelWhileWaiting(t *testing.T) {
	mgr := NewManager(WithLimit(1, StrategyBlock))

	firstRunning := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondRan := make(chan struct{})

	mgr.Schedule("t1", 1*time.Millisecond, func(ctx context.Context) {
		close(firstRunning)
		<-releaseFirst
	})
	<-firstRunning

	// This task should wait for capacity and must not run if canceled while waiting.
	mgr.Schedule("t2", 0, func(ctx context.Context) {
		close(secondRan)
	})

	// Keep t1 holding the slot long enough to ensure t2 attempts to wait for it.
	select {
	case <-secondRan:
		t.Fatal("task 2 should not run while first task holds the slot")
	case <-time.After(20 * time.Millisecond):
	}

	mgr.Cancel("t2")
	close(releaseFirst)

	select {
	case <-secondRan:
		t.Fatal("canceled blocking task should not execute")
	case <-time.After(80 * time.Millisecond):
	}
}

func TestManager_ShutdownTimeout(t *testing.T) {
	mgr := NewManager()
	start := make(chan struct{})
	release := make(chan struct{})

	mgr.Schedule("stubborn-task", 1*time.Millisecond, func(ctx context.Context) {
		close(start)
		<-release
	})

	<-start

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := mgr.Shutdown(ctx)
	if err == nil {
		t.Error("expected timeout error from shutdown, got nil")
	}
	if s := mgr.Stats(); s.Status != StatusDraining {
		t.Fatalf("expected status to be draining after shutdown timeout: %+v", s)
	}

	close(release)
	longCtx, longCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer longCancel()
	if err := mgr.Shutdown(longCtx); err != nil {
		t.Fatalf("expected shutdown to eventually complete after release: %v", err)
	}
	if s := mgr.Stats(); s.Status != StatusClosed {
		t.Fatalf("expected status to be closed after shutdown completion: %+v", s)
	}
}

func TestManager_RescheduleKeepsNewestEntry(t *testing.T) {
	spy := &spyLogger{}
	mgr := NewManager(WithLogger(spy))

	started := make(chan struct{})
	release := make(chan struct{})
	secondRan := make(chan struct{})

	mgr.Schedule("same-id", 1*time.Millisecond, func(ctx context.Context) {
		close(started)
		<-release
	})

	<-started

	mgr.Schedule("same-id", 100*time.Millisecond, func(ctx context.Context) {
		close(secondRan)
	})

	close(release)

	select {
	case <-secondRan:
		t.Fatal("second task ran too early")
	case <-time.After(20 * time.Millisecond):
	}

	mgr.Cancel("same-id")

	select {
	case <-secondRan:
		t.Fatal("cancel should prevent the newest task from running")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestManager_ShutdownCanRetryAfterTimeout(t *testing.T) {
	mgr := NewManager()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	mgr.Schedule("retry", 1*time.Millisecond, func(ctx context.Context) {
		close(started)
		<-release
		close(done)
	})

	<-started

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer shortCancel()
	if err := mgr.Shutdown(shortCtx); err == nil {
		t.Fatal("expected first shutdown call to time out")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for retry task to finish")
	}

	longCtx, longCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer longCancel()
	if err := mgr.Shutdown(longCtx); err != nil {
		t.Fatalf("expected second shutdown call to succeed: %v", err)
	}
}

func TestManager_ScheduleAfterShutdownIsNoOp(t *testing.T) {
	mgr := NewManager()

	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	ran := make(chan struct{}, 1)
	mgr.Schedule("late-task", 0, func(ctx context.Context) {
		ran <- struct{}{}
	})

	select {
	case <-ran:
		t.Fatal("task should not run after shutdown")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestManager_ScheduleWith_ReturnsErrWhenNotAccepting(t *testing.T) {
	mgr := NewManager()

	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	scheduled, err := mgr.ScheduleWith("late-task", func(ctx context.Context) error {
		return nil
	}, ScheduleOptions{Delay: 0})

	if scheduled {
		t.Fatal("expected scheduling to be rejected after shutdown")
	}
	if !errors.Is(err, ErrManagerNotAccepting) {
		t.Fatalf("expected ErrManagerNotAccepting, got %v", err)
	}
}

func TestManager_ScheduleWith_RejectsDelayAndAt(t *testing.T) {
	mgr := NewManager()

	scheduled, err := mgr.ScheduleWith("invalid", func(ctx context.Context) error {
		return nil
	}, ScheduleOptions{
		Delay: time.Second,
		At:    time.Now().Add(time.Second),
	})

	if scheduled {
		t.Fatal("expected scheduling to fail for invalid options")
	}
	if !errors.Is(err, ErrInvalidScheduleOptions) {
		t.Fatalf("expected ErrInvalidScheduleOptions, got %v", err)
	}
}

func TestManager_ScheduleWith_AtInPastRunsImmediately(t *testing.T) {
	mgr := NewManager()
	ran := make(chan struct{}, 1)

	scheduled, err := mgr.ScheduleWith("past-at", func(ctx context.Context) error {
		ran <- struct{}{}
		return nil
	}, ScheduleOptions{
		At: time.Now().Add(-1 * time.Second),
	})
	if err != nil {
		t.Fatalf("unexpected schedule error: %v", err)
	}
	if !scheduled {
		t.Fatal("expected scheduling to succeed")
	}

	select {
	case <-ran:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected task to run immediately for past At")
	}
}

func TestManager_ScheduleWith_AtInFutureDelaysExecution(t *testing.T) {
	mgr := NewManager()
	ran := make(chan struct{}, 1)
	start := time.Now()

	scheduled, err := mgr.ScheduleWith("future-at", func(ctx context.Context) error {
		ran <- struct{}{}
		return nil
	}, ScheduleOptions{
		At: time.Now().Add(35 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("unexpected schedule error: %v", err)
	}
	if !scheduled {
		t.Fatal("expected scheduling to succeed")
	}

	select {
	case <-ran:
		if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
			t.Fatalf("task ran too early for future At: %v", elapsed)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected task to run for future At")
	}
}

func TestManager_ScheduleWith_IfAbsentDoesNotReplace(t *testing.T) {
	mgr := NewManager()
	firstRan := make(chan struct{}, 1)
	secondRan := make(chan struct{}, 1)

	scheduled, err := mgr.ScheduleWith("same-id", func(ctx context.Context) error {
		firstRan <- struct{}{}
		return nil
	}, ScheduleOptions{Delay: 40 * time.Millisecond})
	if err != nil || !scheduled {
		t.Fatalf("expected first schedule to succeed: scheduled=%v err=%v", scheduled, err)
	}

	scheduled, err = mgr.ScheduleWith("same-id", func(ctx context.Context) error {
		secondRan <- struct{}{}
		return nil
	}, ScheduleOptions{Delay: 0, IfAbsent: true})
	if err != nil {
		t.Fatalf("expected no error for IfAbsent path, got %v", err)
	}
	if scheduled {
		t.Fatal("expected IfAbsent schedule to be skipped")
	}

	select {
	case <-secondRan:
		t.Fatal("replacement task should not run when IfAbsent is true")
	case <-time.After(20 * time.Millisecond):
	}

	select {
	case <-firstRan:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected original task to run")
	}
}

func TestManager_ScheduleWith_TaskErrorReported(t *testing.T) {
	wantErr := errors.New("boom")
	spy := &spyLogger{failedSig: make(chan struct{}, 1)}
	mgr := NewManager(WithLogger(spy))

	scheduled, err := mgr.ScheduleWith("err-task", func(ctx context.Context) error {
		return wantErr
	}, ScheduleOptions{Delay: 0})
	if err != nil || !scheduled {
		t.Fatalf("expected schedule to succeed: scheduled=%v err=%v", scheduled, err)
	}

	select {
	case <-spy.failedSig:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected OnFailed to be called for task error")
	}
}

func TestManager_StatsSnapshotLifecycle(t *testing.T) {
	mgr := NewManager()

	s := mgr.Stats()
	if s.Pending != 0 || s.Running != 0 || s.Status != StatusAccepting {
		t.Fatalf("unexpected initial stats: %+v", s)
	}

	mgr.Schedule("stats-pending", time.Hour, func(ctx context.Context) {})
	s = mgr.Stats()
	if s.Pending != 1 || s.Running != 0 || s.Status != StatusAccepting {
		t.Fatalf("unexpected stats after schedule: %+v", s)
	}

	mgr.Cancel("stats-pending")
	s = mgr.Stats()
	if s.Pending != 0 || s.Running != 0 || s.Status != StatusAccepting {
		t.Fatalf("unexpected stats after cancel: %+v", s)
	}

	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	s = mgr.Stats()
	if s.Pending != 0 || s.Running != 0 || s.Status != StatusClosed {
		t.Fatalf("unexpected stats after shutdown: %+v", s)
	}
}

func TestManager_StatsPendingAndRunning(t *testing.T) {
	mgr := NewManager(WithLimit(1, StrategyBlock))

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	secondDone := make(chan struct{})

	mgr.Schedule("stats-running", 0, func(ctx context.Context) {
		close(firstStarted)
		<-releaseFirst
	})
	<-firstStarted

	mgr.Schedule("stats-waiting", 0, func(ctx context.Context) {
		close(secondStarted)
		close(secondDone)
	})

	waitFor(t, 200*time.Millisecond, func() bool {
		s := mgr.Stats()
		return s.Running == 1 && s.Pending == 1 && s.Status == StatusAccepting
	}, "expected one running and one pending task")

	select {
	case <-secondStarted:
		t.Fatal("waiting task should not start while first task holds the slot")
	default:
	}

	close(releaseFirst)

	select {
	case <-secondDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for second task to complete")
	}

	waitFor(t, 200*time.Millisecond, func() bool {
		s := mgr.Stats()
		return s.Running == 0 && s.Pending == 0
	}, "expected no running or pending tasks")
}

func TestManager_StatsPendingClampWhenCanceledWhileRunning(t *testing.T) {
	mgr := NewManager()

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	mgr.Schedule("stats-clamp", 0, func(ctx context.Context) {
		close(started)
		<-release
		close(done)
	})

	<-started
	mgr.Cancel("stats-clamp")

	s := mgr.Stats()
	if s.Running != 1 {
		t.Fatalf("expected one running task after cancel, got %+v", s)
	}
	if s.Pending != 0 {
		t.Fatalf("expected pending to clamp to zero, got %+v", s)
	}
	if s.Status != StatusAccepting {
		t.Fatalf("expected status to remain accepting while task runs: %+v", s)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for running task to finish")
	}

	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestManager_StatsConcurrentAccess(t *testing.T) {
	mgr := NewManager()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var readers sync.WaitGroup
	errs := make(chan error, 1)
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				s := mgr.Stats()
				if s.Pending < 0 || s.Running < 0 {
					select {
					case errs <- fmt.Errorf("invalid stats: %+v", s):
					default:
					}
					return
				}
			}
		})
	}

	for i := range 200 {
		id := fmt.Sprintf("stats-concurrent-%d", i)
		mgr.Schedule(id, time.Hour, func(ctx context.Context) {})
		mgr.Cancel(id)
	}

	cancel()
	readers.Wait()

	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}

func TestManager_IsPendingLifecycle(t *testing.T) {
	mgr := NewManager()
	const id = "is-pending"

	if mgr.IsPending("missing") {
		t.Fatal("expected missing task to not be pending")
	}

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	mgr.Schedule(id, 80*time.Millisecond, func(ctx context.Context) {
		close(started)
		<-release
		close(done)
	})

	if !mgr.IsPending(id) {
		t.Fatal("expected task to be pending before timer fires")
	}

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for task to start")
	}

	if mgr.IsPending(id) {
		t.Fatal("expected task to no longer be pending once started")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for task completion")
	}

	if mgr.IsPending(id) {
		t.Fatal("expected completed task to not be pending")
	}
}

func TestManager_TimeRemaining(t *testing.T) {
	mgr := NewManager()
	const id = "remaining"

	if got := mgr.TimeRemaining("missing"); got != 0 {
		t.Fatalf("expected zero remaining for missing task, got %v", got)
	}

	mgr.Schedule(id, 120*time.Millisecond, func(ctx context.Context) {})

	first := mgr.TimeRemaining(id)
	if first <= 0 {
		t.Fatalf("expected positive remaining duration, got %v", first)
	}
	if first > 200*time.Millisecond {
		t.Fatalf("unexpectedly large remaining duration: %v", first)
	}

	time.Sleep(30 * time.Millisecond)
	second := mgr.TimeRemaining(id)
	if second <= 0 {
		t.Fatalf("expected positive remaining duration after sleep, got %v", second)
	}
	if second >= first {
		t.Fatalf("expected remaining duration to decrease: first=%v second=%v", first, second)
	}

	mgr.Cancel(id)
	if got := mgr.TimeRemaining(id); got != 0 {
		t.Fatalf("expected zero remaining after cancel, got %v", got)
	}
}

func TestManager_TimeRemainingZeroAfterStart(t *testing.T) {
	mgr := NewManager()
	const id = "remaining-started"
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	mgr.Schedule(id, 0, func(ctx context.Context) {
		close(started)
		<-release
		close(done)
	})

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for task start")
	}

	if got := mgr.TimeRemaining(id); got != 0 {
		t.Fatalf("expected zero remaining once task has started, got %v", got)
	}
	if mgr.IsPending(id) {
		t.Fatal("expected started task to not be pending")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for task completion")
	}
}

func TestManager_TimeRemainingPastDeadlineClampsToZero(t *testing.T) {
	mgr := NewManager()

	mgr.mu.Lock()
	mgr.pending["overdue"] = &entry{deadline: time.Now().Add(-1 * time.Second)}
	mgr.mu.Unlock()

	if got := mgr.TimeRemaining("overdue"); got != 0 {
		t.Fatalf("expected zero remaining for overdue task, got %v", got)
	}
}

func TestStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   string
	}{
		{name: "accepting", status: StatusAccepting, want: "accepting"},
		{name: "draining", status: StatusDraining, want: "draining"},
		{name: "closed", status: StatusClosed, want: "closed"},
		{name: "unknown", status: Status(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Fatalf("unexpected status string for %v: got %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()

	deadline := time.After(timeout)
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal(msg)
		case <-ticker.C:
		}
	}
}

func TestCoverageBooster(t *testing.T) {
	_ = NewManager(
		WithLimit(1, StrategyBlock),
		WithLogger(nil),
	)

	n := nopLogger{}
	n.OnScheduled("t", time.Second)
	n.OnRescheduled("t")
	n.OnExecuted("t", time.Second)
	n.OnCancelled("t")
	n.OnFailed("t", fmt.Errorf("err"))
}

func TestCoverage_TimerRaceGuard(t *testing.T) {
	mgr := NewManager()

	ran := make(chan struct{}, 1)
	mgr.Schedule("race-trigger", 20*time.Millisecond, func(ctx context.Context) {
		ran <- struct{}{}
	})

	mgr.mu.Lock()
	mgr.status = StatusDraining
	mgr.mu.Unlock()

	// Wait until the timer callback has started (or fail).
	waitFor(t, 200*time.Millisecond, func() bool {
		return !mgr.IsPending("race-trigger")
	}, "expected timer callback to run while manager is draining")

	select {
	case <-ran:
		t.Fatal("task should not execute when manager is draining")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestManager_ManualCancel(t *testing.T) {
	mgr := NewManager()

	mgr.Schedule("cancel-me", 1*time.Hour, func(ctx context.Context) {
		t.Error("this task should have been cancelled and never run")
	})

	mgr.Cancel("cancel-me")
}
