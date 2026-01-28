package work

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abelbrown/observer/internal/logging"
)

// Pool manages a pool of workers that process work items.
// All async work flows through this central hub.
type Pool struct {
	mu      sync.RWMutex
	workers int

	// Queues
	pending   []*Item           // Priority queue (higher priority first)
	active    map[string]*Item  // ID -> active work
	completed *RingBuffer       // Recent completed (success + failure)

	// Channels
	workChan chan *Item
	stopChan chan struct{}

	// Event subscribers (for UI updates)
	subscribers   []chan Event
	subscribersMu sync.RWMutex

	// Stats
	totalCreated   int64
	totalCompleted int64
	totalFailed    int64

	// ID generator
	nextID int64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewPool creates a work pool with the specified number of workers.
// If workers <= 0, uses runtime.NumCPU().
func NewPool(workers int) *Pool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	return &Pool{
		workers:   workers,
		active:    make(map[string]*Item),
		completed: NewRingBuffer(100),
		workChan:  make(chan *Item, 1000),
		stopChan:  make(chan struct{}),
	}
}

// Start launches the worker goroutines.
func (p *Pool) Start(ctx context.Context) {
	p.ctx, p.cancel = context.WithCancel(ctx)

	logging.Info("Work pool starting", "workers", p.workers)

	// Start workers
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	// Start pending queue processor
	p.wg.Add(1)
	go p.processPending()

	logging.Info("Work pool started", "workers", p.workers)
}

// Stop gracefully shuts down the pool.
func (p *Pool) Stop() {
	logging.Info("Work pool stopping")
	if p.cancel != nil {
		p.cancel()
	}
	close(p.stopChan)
	p.wg.Wait()
	logging.Info("Work pool stopped",
		"created", p.totalCreated,
		"completed", p.totalCompleted,
		"failed", p.totalFailed)
}

// Submit adds a work item to the queue.
func (p *Pool) Submit(item *Item) string {
	item.ID = p.generateID()
	item.Status = StatusPending
	item.CreatedAt = time.Now()

	p.mu.Lock()
	p.pending = append(p.pending, item)
	atomic.AddInt64(&p.totalCreated, 1)
	p.mu.Unlock()

	p.notify(Event{Item: item, Change: "created"})

	// Signal pending processor
	select {
	case p.workChan <- item:
	default:
		// Channel full, item stays in pending queue
		logging.Debug("Work channel full, item queued", "id", item.ID)
	}

	return item.ID
}

// SubmitFunc is a convenience for simple work without progress tracking.
func (p *Pool) SubmitFunc(typ Type, desc string, fn func() (string, error)) string {
	item := &Item{
		Type:        typ,
		Description: desc,
		workFn:      fn,
	}
	return p.Submit(item)
}

// SubmitWithProgress submits work that reports progress.
func (p *Pool) SubmitWithProgress(typ Type, desc string, fn func(progress func(pct float64, msg string)) (string, error)) string {
	item := &Item{
		Type:        typ,
		Description: desc,
	}

	// Wrap the function to inject progress callback
	item.workFn = func() (string, error) {
		return fn(func(pct float64, msg string) {
			p.mu.Lock()
			item.Progress = pct
			item.ProgressMsg = msg
			p.mu.Unlock()
			p.notify(Event{Item: item, Change: "progress"})
		})
	}

	return p.Submit(item)
}

// SubmitWithData submits work that returns arbitrary data.
// The dataFn should return (result string, data any, error).
func (p *Pool) SubmitWithData(typ Type, desc string, source string, category string, dataFn func() (string, any, error)) string {
	item := &Item{
		Type:        typ,
		Description: desc,
		Source:      source,
		Category:    category,
	}

	// Wrap the function to capture data
	item.workFn = func() (string, error) {
		result, data, err := dataFn()
		item.Data = data
		return result, err
	}

	return p.Submit(item)
}

// processPending moves items from pending queue to workers.
func (p *Pool) processPending() {
	defer p.wg.Done()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.dispatchPending()
		case <-p.workChan:
			// Signal received, try to dispatch
			p.dispatchPending()
		}
	}
}

// dispatchPending sends pending items to workers if capacity available.
func (p *Pool) dispatchPending() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Sort by priority (simple bubble sort, pending queue is small)
	for i := 0; i < len(p.pending)-1; i++ {
		for j := i + 1; j < len(p.pending); j++ {
			if p.pending[j].Priority > p.pending[i].Priority {
				p.pending[i], p.pending[j] = p.pending[j], p.pending[i]
			}
		}
	}

	// Dispatch items while we have worker capacity
	for len(p.pending) > 0 && len(p.active) < p.workers {
		item := p.pending[0]
		p.pending = p.pending[1:]

		item.Status = StatusActive
		item.StartedAt = time.Now()
		p.active[item.ID] = item

		p.notify(Event{Item: item, Change: "started"})

		// Execute in goroutine (worker pool is really more of a concurrency limiter)
		go p.execute(item)
	}
}

// worker processes work items.
func (p *Pool) worker(id int) {
	defer p.wg.Done()
	logging.Debug("Worker started", "worker", id)

	<-p.ctx.Done()
	logging.Debug("Worker stopped", "worker", id)
}

// execute runs a single work item.
func (p *Pool) execute(item *Item) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Work panicked",
				"id", item.ID,
				"panic", r)
			p.complete(item, "", fmt.Errorf("panic: %v", r))
		}
	}()

	if item.workFn == nil {
		p.complete(item, "", fmt.Errorf("no work function"))
		return
	}

	result, err := item.workFn()
	p.complete(item, result, err)
}

// complete marks a work item as finished.
func (p *Pool) complete(item *Item, result string, err error) {
	p.mu.Lock()
	item.FinishedAt = time.Now()
	item.Result = result
	item.Error = err

	if err != nil {
		item.Status = StatusFailed
		atomic.AddInt64(&p.totalFailed, 1)
	} else {
		item.Status = StatusComplete
		atomic.AddInt64(&p.totalCompleted, 1)
	}

	delete(p.active, item.ID)
	p.completed.Push(item)
	p.mu.Unlock()

	change := "completed"
	if err != nil {
		change = "failed"
	}
	p.notify(Event{Item: item, Change: change})
}

// Snapshot returns the current state for UI display.
func (p *Pool) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Copy pending
	pending := make([]*Item, len(p.pending))
	copy(pending, p.pending)

	// Copy active
	active := make([]*Item, 0, len(p.active))
	for _, item := range p.active {
		active = append(active, item)
	}

	return Snapshot{
		Pending:   pending,
		Active:    active,
		Completed: p.completed.All(),
		Stats: Stats{
			TotalCreated:   atomic.LoadInt64(&p.totalCreated),
			TotalCompleted: atomic.LoadInt64(&p.totalCompleted),
			TotalFailed:    atomic.LoadInt64(&p.totalFailed),
			WorkersActive:  len(p.active),
			WorkersTotal:   p.workers,
			PendingCount:   len(p.pending),
		},
	}
}

// Subscribe returns a channel that receives work events.
// The channel should be drained to avoid blocking the pool.
func (p *Pool) Subscribe() <-chan Event {
	ch := make(chan Event, 100)
	p.subscribersMu.Lock()
	p.subscribers = append(p.subscribers, ch)
	p.subscribersMu.Unlock()
	logging.Debug("Work pool subscriber added", "total", len(p.subscribers))
	return ch
}

// Unsubscribe removes a subscriber channel.
func (p *Pool) Unsubscribe(ch <-chan Event) {
	p.subscribersMu.Lock()
	defer p.subscribersMu.Unlock()

	for i, sub := range p.subscribers {
		if sub == ch {
			p.subscribers = append(p.subscribers[:i], p.subscribers[i+1:]...)
			close(sub)
			logging.Debug("Work pool subscriber removed", "total", len(p.subscribers))
			return
		}
	}
}

// notify sends an event to all subscribers.
func (p *Pool) notify(event Event) {
	// Log the event for debugging
	LogEvent(event)

	p.subscribersMu.RLock()
	defer p.subscribersMu.RUnlock()

	for _, ch := range p.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber not keeping up, drop event
			logging.Debug("Work event dropped (subscriber full)",
				"id", event.Item.ID,
				"change", event.Change)
		}
	}
}

// generateID creates a unique work item ID.
func (p *Pool) generateID() string {
	id := atomic.AddInt64(&p.nextID, 1)
	return fmt.Sprintf("w%d", id)
}

// Stats returns current statistics.
func (p *Pool) Stats() Stats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return Stats{
		TotalCreated:   atomic.LoadInt64(&p.totalCreated),
		TotalCompleted: atomic.LoadInt64(&p.totalCompleted),
		TotalFailed:    atomic.LoadInt64(&p.totalFailed),
		WorkersActive:  len(p.active),
		WorkersTotal:   p.workers,
		PendingCount:   len(p.pending),
	}
}

// PendingCount returns the number of pending items.
func (p *Pool) PendingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.pending)
}

// ActiveCount returns the number of active items.
func (p *Pool) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.active)
}

// ClearHistory clears the completed work history.
func (p *Pool) ClearHistory() {
	p.completed.Clear()
	logging.Info("Work history cleared")
}

