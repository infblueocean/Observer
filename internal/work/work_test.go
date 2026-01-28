package work

import (
	"container/heap"
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPriorityQueueDirect(t *testing.T) {
	// Test the priority queue directly without pool concurrency
	pq := make(priorityQueue, 0)
	heap.Init(&pq)

	// Push items with different priorities
	now := time.Now()
	items := []*Item{
		{ID: "low", Priority: PriorityLow, CreatedAt: now},
		{ID: "high", Priority: PriorityHigh, CreatedAt: now.Add(time.Millisecond)},
		{ID: "normal", Priority: PriorityNormal, CreatedAt: now.Add(2 * time.Millisecond)},
	}

	for _, item := range items {
		heap.Push(&pq, item)
	}

	// Pop should return in priority order: high, normal, low
	expected := []string{"high", "normal", "low"}
	for i, exp := range expected {
		if pq.Len() == 0 {
			t.Fatalf("queue empty at index %d", i)
		}
		item := heap.Pop(&pq).(*Item)
		if item.ID != exp {
			t.Errorf("pop[%d] = %s (priority %d), expected %s", i, item.ID, item.Priority, exp)
		}
	}
}

func TestPriorityQueueFIFODirect(t *testing.T) {
	// Test FIFO ordering for same priority
	pq := make(priorityQueue, 0)
	heap.Init(&pq)

	// Push items with same priority but different creation times
	now := time.Now()
	items := []*Item{
		{ID: "first", Priority: PriorityNormal, CreatedAt: now},
		{ID: "second", Priority: PriorityNormal, CreatedAt: now.Add(time.Millisecond)},
		{ID: "third", Priority: PriorityNormal, CreatedAt: now.Add(2 * time.Millisecond)},
	}

	for _, item := range items {
		heap.Push(&pq, item)
	}

	// Pop should return in FIFO order: first, second, third
	expected := []string{"first", "second", "third"}
	for i, exp := range expected {
		if pq.Len() == 0 {
			t.Fatalf("queue empty at index %d", i)
		}
		item := heap.Pop(&pq).(*Item)
		if item.ID != exp {
			t.Errorf("pop[%d] = %s, expected %s", i, item.ID, exp)
		}
	}
}

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

func TestPriorityQueue(t *testing.T) {
	// Test that higher priority items are dispatched first
	// We check dispatch order via "started" events, not execution results
	// (execution results can arrive out of order due to goroutine scheduling)
	pool := NewPool(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)

	// Subscribe to events to track dispatch order
	events := pool.Subscribe()

	// Use a blocker to ensure items queue up before being dispatched
	blockerStarted := make(chan struct{})
	blocker := make(chan struct{})
	pool.SubmitFuncWithPriority(TypeOther, "blocker", PriorityCritical, func() (string, error) {
		close(blockerStarted)
		<-blocker
		return "blocker done", nil
	})

	// Wait for blocker to start
	select {
	case <-blockerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("blocker did not start in time")
	}

	// Drain events up to blocker starting
	drainEvents(events, 50*time.Millisecond)

	// Submit items - they'll all queue up since blocker occupies the worker
	pool.SubmitFuncWithPriority(TypeOther, "low", PriorityLow, func() (string, error) {
		return "low done", nil
	})
	pool.SubmitFuncWithPriority(TypeOther, "high", PriorityHigh, func() (string, error) {
		return "high done", nil
	})
	pool.SubmitFuncWithPriority(TypeOther, "normal", PriorityNormal, func() (string, error) {
		return "normal done", nil
	})

	// Wait for items to be queued
	time.Sleep(50 * time.Millisecond)
	if pool.PendingCount() != 3 {
		t.Fatalf("expected 3 pending items, got %d", pool.PendingCount())
	}

	// Drain the "created" events
	drainEvents(events, 50*time.Millisecond)

	// Release blocker - items should now be dispatched in priority order
	close(blocker)

	// Collect "started" events to check dispatch order
	var dispatchOrder []string
	timeout := time.After(2 * time.Second)
	startedCount := 0
	for startedCount < 3 {
		select {
		case evt := <-events:
			if evt.Change == "started" && evt.Item.Description != "blocker" {
				dispatchOrder = append(dispatchOrder, evt.Item.Description)
				startedCount++
			}
		case <-timeout:
			t.Fatalf("timed out waiting for dispatch events, got %v", dispatchOrder)
		}
	}

	pool.Stop()

	expected := []string{"high", "normal", "low"}
	for i, exp := range expected {
		if i >= len(dispatchOrder) {
			t.Errorf("missing dispatch at index %d, expected %s", i, exp)
			continue
		}
		if dispatchOrder[i] != exp {
			t.Errorf("dispatch[%d] = %s, expected %s", i, dispatchOrder[i], exp)
		}
	}
}

// drainEvents reads events from the channel until timeout
func drainEvents(events <-chan Event, timeout time.Duration) {
	for {
		select {
		case <-events:
			// discard
		case <-time.After(timeout):
			return
		}
	}
}

func TestPriorityQueueFIFO(t *testing.T) {
	// Test that items with same priority are processed in FIFO order
	pool := NewPool(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	results := make(chan string, 3)

	// Use a blocker
	blocker := make(chan struct{})
	pool.SubmitFuncWithPriority(TypeOther, "blocker", PriorityCritical, func() (string, error) {
		<-blocker
		return "blocker done", nil
	})

	time.Sleep(20 * time.Millisecond)

	// Submit 3 items with same priority - should be FIFO
	pool.SubmitFuncWithPriority(TypeOther, "first", PriorityNormal, func() (string, error) {
		results <- "first"
		return "first done", nil
	})
	pool.SubmitFuncWithPriority(TypeOther, "second", PriorityNormal, func() (string, error) {
		results <- "second"
		return "second done", nil
	})
	pool.SubmitFuncWithPriority(TypeOther, "third", PriorityNormal, func() (string, error) {
		results <- "third"
		return "third done", nil
	})

	time.Sleep(20 * time.Millisecond)
	close(blocker)

	var order []string
	timeout := time.After(2 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case r := <-results:
			order = append(order, r)
		case <-timeout:
			t.Fatalf("timed out waiting for results, got %v", order)
		}
	}

	expected := []string{"first", "second", "third"}
	for i, exp := range expected {
		if i >= len(order) {
			t.Errorf("missing result at index %d, expected %s", i, exp)
			continue
		}
		if order[i] != exp {
			t.Errorf("result[%d] = %s, expected %s (FIFO order)", i, order[i], exp)
		}
	}
}

func TestSubmitWithDataAndPriority(t *testing.T) {
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	done := make(chan struct{})
	pool.SubmitWithDataAndPriority(TypeFetch, "test data", "source1", "cat1", PriorityHigh, func() (string, any, error) {
		close(done)
		return "done", map[string]int{"count": 42}, nil
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("work did not complete in time")
	}

	// Verify the work completed
	snap := pool.Snapshot()
	if snap.Stats.TotalCompleted != 1 {
		t.Errorf("expected 1 completed, got %d", snap.Stats.TotalCompleted)
	}
}

func TestDoubleStop(t *testing.T) {
	// Test that calling Stop() twice doesn't panic
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)

	// Submit some work to ensure pool is active
	done := make(chan struct{})
	pool.SubmitFunc(TypeFetch, "test", func() (string, error) {
		close(done)
		return "ok", nil
	})
	<-done

	// First stop should work
	pool.Stop()

	// Second stop should be a no-op (not panic)
	pool.Stop()

	// Third stop should also be a no-op
	pool.Stop()
}

func TestDoubleStart(t *testing.T) {
	// Test that calling Start() twice doesn't create duplicate goroutines
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First start
	pool.Start(ctx)

	// Second start should be ignored
	pool.Start(ctx)

	// Submit work to verify pool works correctly
	done := make(chan struct{})
	pool.SubmitFunc(TypeFetch, "test", func() (string, error) {
		close(done)
		return "ok", nil
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("work did not complete in time")
	}

	pool.Stop()
}

func TestUpdatePriorityO1Lookup(t *testing.T) {
	// Test that UpdatePriority uses O(1) lookup
	pool := NewPool(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	// Use a blocker to keep items pending
	blocker := make(chan struct{})
	pool.SubmitFuncWithPriority(TypeOther, "blocker", PriorityCritical, func() (string, error) {
		<-blocker
		return "blocker done", nil
	})

	time.Sleep(20 * time.Millisecond)

	// Submit items with low priority
	id1 := pool.SubmitFuncWithPriority(TypeOther, "item1", PriorityLow, func() (string, error) {
		return "item1 done", nil
	})
	id2 := pool.SubmitFuncWithPriority(TypeOther, "item2", PriorityLow, func() (string, error) {
		return "item2 done", nil
	})

	time.Sleep(20 * time.Millisecond)

	// Update priority of second item to high
	if !pool.UpdatePriority(id2, PriorityHigh) {
		t.Fatal("UpdatePriority should have succeeded for pending item")
	}

	// UpdatePriority should fail for non-existent ID
	if pool.UpdatePriority("nonexistent", PriorityHigh) {
		t.Error("UpdatePriority should have failed for non-existent ID")
	}

	// Verify item1 is still in the queue
	if pool.UpdatePriority(id1, PriorityNormal) == false {
		t.Error("UpdatePriority should have succeeded for id1")
	}

	close(blocker)
}

func TestSnapshotReturnsCopies(t *testing.T) {
	// Test that Snapshot returns copies, not live pointers
	pool := NewPool(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	defer pool.Stop()

	// Submit and complete some work
	done := make(chan struct{})
	pool.SubmitFunc(TypeFetch, "test item", func() (string, error) {
		close(done)
		return "completed", nil
	})
	<-done
	time.Sleep(20 * time.Millisecond)

	// Get snapshot
	snap := pool.Snapshot()

	// Verify we have completed items
	if len(snap.Completed) == 0 {
		t.Fatal("expected completed items in snapshot")
	}

	// Modify the snapshot item
	originalDesc := snap.Completed[0].Description
	snap.Completed[0].Description = "modified"

	// Get another snapshot and verify original is unchanged
	snap2 := pool.Snapshot()
	if snap2.Completed[0].Description != originalDesc {
		t.Errorf("snapshot returned live pointers: expected %q, got %q",
			originalDesc, snap2.Completed[0].Description)
	}
}
