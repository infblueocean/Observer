package work

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRingBuffer(t *testing.T) {
	r := NewRingBuffer(5)

	// Push items
	for i := 0; i < 3; i++ {
		r.Push(&Item{ID: string(rune('a' + i))})
	}

	if r.Len() != 3 {
		t.Errorf("expected len 3, got %d", r.Len())
	}

	// Check order (newest first)
	all := r.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 items, got %d", len(all))
	}
	if all[0].ID != "c" {
		t.Errorf("expected newest first, got %s", all[0].ID)
	}

	// Overflow
	for i := 0; i < 5; i++ {
		r.Push(&Item{ID: string(rune('x' + i))})
	}

	if r.Len() != 5 {
		t.Errorf("expected len 5 (capacity), got %d", r.Len())
	}

	// Should have evicted oldest
	recent := r.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent, got %d", len(recent))
	}
}

func TestPoolSubmit(t *testing.T) {
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	var counter int64
	done := make(chan struct{})

	// Submit work
	pool.SubmitFunc(TypeFetch, "test work", func() (string, error) {
		atomic.AddInt64(&counter, 1)
		close(done)
		return "done", nil
	})

	// Wait for completion
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("work did not complete in time")
	}

	if atomic.LoadInt64(&counter) != 1 {
		t.Errorf("expected counter 1, got %d", counter)
	}
}

func TestPoolProgress(t *testing.T) {
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	progressSeen := make(chan float64, 10)
	done := make(chan struct{})

	pool.SubmitWithProgress(TypeRerank, "progress test", func(progress func(pct float64, msg string)) (string, error) {
		for i := 1; i <= 5; i++ {
			pct := float64(i) / 5.0
			progress(pct, "step")
			progressSeen <- pct
		}
		close(done)
		return "complete", nil
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("work did not complete in time")
	}

	close(progressSeen)
	count := 0
	for range progressSeen {
		count++
	}
	if count != 5 {
		t.Errorf("expected 5 progress updates, got %d", count)
	}
}

func TestPoolSnapshot(t *testing.T) {
	pool := NewPool(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	// Submit some work
	done := make(chan struct{})
	pool.SubmitFunc(TypeFetch, "test", func() (string, error) {
		close(done)
		return "ok", nil
	})

	<-done
	time.Sleep(10 * time.Millisecond) // Let it complete

	snap := pool.Snapshot()
	if snap.Stats.TotalCreated != 1 {
		t.Errorf("expected 1 created, got %d", snap.Stats.TotalCreated)
	}
	if snap.Stats.TotalCompleted != 1 {
		t.Errorf("expected 1 completed, got %d", snap.Stats.TotalCompleted)
	}
}
