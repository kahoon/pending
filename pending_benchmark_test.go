package pending

import (
	"context"
	"strconv"
	"testing"
	"time"
)

var benchmarkTask = func(ctx context.Context) {}

func BenchmarkManager_Schedule(b *testing.B) {
	mgr := NewManager()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mgr.Schedule(strconv.Itoa(i), time.Hour, benchmarkTask)
	}

	b.StopTimer()
	_ = mgr.Shutdown(context.Background())
}

func BenchmarkManager_RescheduleSameID(b *testing.B) {
	mgr := NewManager()
	id := "same"
	mgr.Schedule(id, time.Hour, benchmarkTask)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mgr.Schedule(id, time.Hour, benchmarkTask)
	}

	b.StopTimer()
	_ = mgr.Shutdown(context.Background())
}

func BenchmarkManager_Cancel(b *testing.B) {
	mgr := NewManager()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		id := strconv.Itoa(i)
		mgr.Schedule(id, time.Hour, benchmarkTask)
		mgr.Cancel(id)
	}

	b.StopTimer()
	_ = mgr.Shutdown(context.Background())
}

func BenchmarkManager_Shutdown_NoRunningTasks(b *testing.B) {
	const pendingTasks = 32
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mgr := NewManager()
		for j := 0; j < pendingTasks; j++ {
			mgr.Schedule(strconv.Itoa(i*pendingTasks+j), time.Hour, benchmarkTask)
		}

		b.StartTimer()
		_ = mgr.Shutdown(context.Background())
		b.StopTimer()
	}
}

func BenchmarkManager_Shutdown_WithRunningTasks(b *testing.B) {
	const runningTasks = 16
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mgr := NewManager(WithLimit(runningTasks, StrategyBlock))
		started := make(chan struct{}, runningTasks)

		for j := 0; j < runningTasks; j++ {
			id := strconv.Itoa(j)
			mgr.Schedule(id, 0, func(ctx context.Context) {
				started <- struct{}{}
				<-ctx.Done()
			})
		}

		for j := 0; j < runningTasks; j++ {
			select {
			case <-started:
			case <-time.After(time.Second):
				b.Fatal("timed out waiting for tasks to start")
			}
		}

		b.StartTimer()
		_ = mgr.Shutdown(context.Background())
		b.StopTimer()
	}
}

