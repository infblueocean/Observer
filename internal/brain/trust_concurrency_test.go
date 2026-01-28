package brain

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAnalysisSemaphoreLimit verifies that only 6 concurrent analyses can run.
// It starts more goroutines than the semaphore capacity and confirms that
// at most 6 are active at any given time.
func TestAnalysisSemaphoreLimit(t *testing.T) {
	const (
		semaphoreCapacity = 6  // Expected capacity of analysisSemaphore
		totalGoroutines   = 20 // More than capacity to test limiting
		holdDuration      = 50 * time.Millisecond
	)

	var (
		activeCount   atomic.Int32 // Current number of goroutines holding semaphore
		maxObserved   atomic.Int32 // Peak concurrency observed
		completedCount atomic.Int32
		wg            sync.WaitGroup
	)

	ctx := context.Background()

	for i := 0; i < totalGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Acquire semaphore (same pattern as in trust.go)
			select {
			case analysisSemaphore <- struct{}{}:
				defer func() { <-analysisSemaphore }()
			case <-ctx.Done():
				return
			}

			// Track active goroutines
			current := activeCount.Add(1)

			// Update max observed concurrency
			for {
				old := maxObserved.Load()
				if current <= old || maxObserved.CompareAndSwap(old, current) {
					break
				}
			}

			// Simulate work while holding semaphore
			time.Sleep(holdDuration)

			activeCount.Add(-1)
			completedCount.Add(1)
		}()
	}

	wg.Wait()

	// Verify all goroutines completed
	if completed := completedCount.Load(); completed != totalGoroutines {
		t.Errorf("Expected %d goroutines to complete, got %d", totalGoroutines, completed)
	}

	// Verify max concurrency never exceeded capacity
	if max := maxObserved.Load(); max > semaphoreCapacity {
		t.Errorf("Max concurrent goroutines (%d) exceeded semaphore capacity (%d)", max, semaphoreCapacity)
	}

	// Verify semaphore actually limited concurrency (should have hit capacity)
	if max := maxObserved.Load(); max < semaphoreCapacity {
		t.Errorf("Expected to reach semaphore capacity (%d), but max was %d", semaphoreCapacity, max)
	}
}

// TestAnalysisSemaphoreContextCancellation verifies that goroutines waiting
// for the semaphore exit promptly when their context is cancelled.
func TestAnalysisSemaphoreContextCancellation(t *testing.T) {
	const (
		semaphoreCapacity = 6
		holdDuration      = 200 * time.Millisecond
	)

	// First, fill the semaphore to capacity with goroutines that hold it
	var holdersWg sync.WaitGroup
	holderCtx, cancelHolders := context.WithCancel(context.Background())
	holdersReady := make(chan struct{}, semaphoreCapacity)

	for i := 0; i < semaphoreCapacity; i++ {
		holdersWg.Add(1)
		go func() {
			defer holdersWg.Done()

			select {
			case analysisSemaphore <- struct{}{}:
				defer func() { <-analysisSemaphore }()
			case <-holderCtx.Done():
				return
			}

			// Signal that we're holding the semaphore
			holdersReady <- struct{}{}

			// Hold until told to release
			<-holderCtx.Done()
		}()
	}

	// Wait for all holders to acquire the semaphore
	for i := 0; i < semaphoreCapacity; i++ {
		select {
		case <-holdersReady:
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for holders to acquire semaphore")
		}
	}

	// Now try to acquire with a cancelled context - should return immediately
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	start := time.Now()
	exitedChan := make(chan bool, 1)

	go func() {
		select {
		case analysisSemaphore <- struct{}{}:
			// Should not reach here - context was cancelled
			<-analysisSemaphore
			exitedChan <- false
		case <-cancelledCtx.Done():
			exitedChan <- true
		}
	}()

	select {
	case exitedViaContext := <-exitedChan:
		elapsed := time.Since(start)
		if !exitedViaContext {
			t.Error("Goroutine acquired semaphore instead of exiting via context")
		}
		// Should exit very quickly since context is already cancelled
		if elapsed > 50*time.Millisecond {
			t.Errorf("Goroutine took too long to exit (%v), expected near-instant exit", elapsed)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for goroutine to exit via cancelled context")
	}

	// Test with context that gets cancelled while waiting
	waitingCtx, cancelWaiting := context.WithCancel(context.Background())
	var waiterExited atomic.Bool
	var waiterWg sync.WaitGroup
	waiterWg.Add(1)

	go func() {
		defer waiterWg.Done()
		select {
		case analysisSemaphore <- struct{}{}:
			<-analysisSemaphore
		case <-waitingCtx.Done():
			waiterExited.Store(true)
		}
	}()

	// Give the goroutine time to start waiting
	time.Sleep(10 * time.Millisecond)

	// Cancel the context
	start = time.Now()
	cancelWaiting()

	// Wait for waiter to exit
	done := make(chan struct{})
	go func() {
		waiterWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(start)
		if !waiterExited.Load() {
			t.Error("Waiting goroutine did not exit via context cancellation")
		}
		if elapsed > 50*time.Millisecond {
			t.Errorf("Waiting goroutine took too long to exit after cancel (%v)", elapsed)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for waiting goroutine to exit")
	}

	// Clean up: release the holders
	cancelHolders()
	holdersWg.Wait()
}

// TestAnalysisSemaphoreNoDeadlock verifies the semaphore doesn't cause deadlock
// when multiple goroutines acquire and release in different orders.
func TestAnalysisSemaphoreNoDeadlock(t *testing.T) {
	const (
		totalGoroutines = 50
		timeout         = 5 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup
	completed := make(chan struct{}, totalGoroutines)

	for i := 0; i < totalGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			select {
			case analysisSemaphore <- struct{}{}:
				defer func() { <-analysisSemaphore }()
			case <-ctx.Done():
				return
			}

			// Vary hold duration to create different release orderings
			holdTime := time.Duration((id%5)+1) * time.Millisecond
			time.Sleep(holdTime)

			completed <- struct{}{}
		}(i)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
		if len(completed) != totalGoroutines {
			t.Errorf("Expected %d completions, got %d", totalGoroutines, len(completed))
		}
	case <-ctx.Done():
		t.Fatal("Test timed out - possible deadlock in semaphore handling")
	}
}
