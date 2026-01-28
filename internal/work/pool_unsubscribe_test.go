package work

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnsubscribePreventsLeak(t *testing.T) {
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	// Record baseline goroutine count
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	startGoroutines := runtime.NumGoroutine()

	// Subscribe
	ch := pool.Subscribe()

	// Do some work that generates events
	var completed atomic.Int32
	for i := 0; i < 10; i++ {
		pool.SubmitFunc(TypeFetch, "test work", func() (string, error) {
			completed.Add(1)
			return "ok", nil
		})
	}

	// Wait for work to complete
	timeout := time.After(5 * time.Second)
	for completed.Load() < 10 {
		select {
		case <-timeout:
			t.Fatalf("work did not complete in time, only %d/10 done", completed.Load())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Drain any pending events
	draining := true
	for draining {
		select {
		case <-ch:
		default:
			draining = false
		}
	}

	// Unsubscribe
	pool.Unsubscribe(ch)

	// Give time for cleanup
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	endGoroutines := runtime.NumGoroutine()

	// Should not have leaked more than a couple goroutines
	// (some variance is expected due to GC, timers, etc.)
	if endGoroutines > startGoroutines+5 {
		t.Errorf("possible goroutine leak: started with %d, ended with %d (diff: %d)",
			startGoroutines, endGoroutines, endGoroutines-startGoroutines)
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	ch := pool.Subscribe()

	// Unsubscribe should close the channel
	pool.Unsubscribe(ch)

	// Reading from closed channel should return immediately with ok=false
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed (ok=false)")
		}
		// ok=false means channel is closed - this is expected
	case <-time.After(100 * time.Millisecond):
		t.Error("channel not closed after Unsubscribe")
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	ch := pool.Subscribe()

	// First unsubscribe
	pool.Unsubscribe(ch)

	// Second unsubscribe should not panic
	pool.Unsubscribe(ch)

	// Third unsubscribe should also not panic
	pool.Unsubscribe(ch)
}

func TestStopWaitsForActiveWorkers(t *testing.T) {
	// Fixed in Phase 1.1: Stop() now waits for active worker goroutines
	// via wg.Add(1) before go p.execute(item) and defer wg.Done() in execute().

	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)

	// Track work completion
	var completed atomic.Int32
	var started atomic.Int32

	// Submit slow work
	for i := 0; i < 5; i++ {
		pool.SubmitFunc(TypeFetch, "slow work", func() (string, error) {
			started.Add(1)
			time.Sleep(100 * time.Millisecond)
			completed.Add(1)
			return "done", nil
		})
	}

	// Wait for at least one work to start
	for started.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	// Stop should wait for active work
	pool.Stop()

	// After fix, all started work should complete
	finalCount := completed.Load()
	t.Logf("Completed %d/5 work items after Stop()", finalCount)

	// All work that was started should complete
	if finalCount < started.Load() {
		t.Errorf("expected all %d started work items to complete, got %d", started.Load(), finalCount)
	}
}
