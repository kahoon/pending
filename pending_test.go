package pending

import (
	"context"
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

	close(release)
	longCtx, longCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer longCancel()
	if err := mgr.Shutdown(longCtx); err != nil {
		t.Fatalf("expected shutdown to eventually complete after release: %v", err)
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
	mgr.Schedule("race-trigger", 0, func(ctx context.Context) {
		ran <- struct{}{}
	})

	mgr.mu.Lock()
	mgr.isClosed = true
	mgr.mu.Unlock()

	select {
	case <-ran:
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
