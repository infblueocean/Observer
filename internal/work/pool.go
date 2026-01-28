package work

import (
	"container/heap"
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
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
	pending      priorityQueue    // Heap-based priority queue (higher priority first)
	pendingIndex map[string]*Item // ID -> pending item for O(1) lookup
	active       map[string]*Item // ID -> active work
	completed    *RingBuffer      // Recent completed (success + failure)

	// Channels
	workSignal chan struct{} // Signal-only channel to wake up pending processor
	stopChan   chan struct{}
	stopOnce   sync.Once // Protects against double-stop panic

	// Event subscribers (for UI updates)
	subscribers   []chan Event
	subscribersMu sync.RWMutex

	// Stats (protected by mu, not atomic)
	totalCreated   int64
	totalCompleted int64
	totalFailed    int64

	// ID generator (atomic, no mutex needed)
	nextID int64

	// Lifecycle
	started bool // Protected by mu, prevents double-start
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewPool creates a work pool with the specified number of workers.
// If workers <= 0, uses runtime.NumCPU().
func NewPool(workers int) *Pool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	p := &Pool{
		workers:      workers,
		pending:      make(priorityQueue, 0),
		pendingIndex: make(map[string]*Item),
		active:       make(map[string]*Item),
		completed:    NewRingBuffer(100),
		workSignal:   make(chan struct{}, 1), // Buffered signal channel (1 is enough)
		stopChan:     make(chan struct{}),
	}
	heap.Init(&p.pending)
	return p
}

// Start launches the worker goroutines.
// Safe to call multiple times; subsequent calls are no-ops.
func (p *Pool) Start(ctx context.Context) {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		logging.Debug("Work pool already started, ignoring Start() call")
		return
	}
	p.started = true
	p.mu.Unlock()

	p.ctx, p.cancel = context.WithCancel(ctx)

	logging.Info("Work pool starting", "workers", p.workers)

	// Start pending queue processor (the only goroutine we need)
	p.wg.Add(1)
	go p.processPending()

	logging.Info("Work pool started", "workers", p.workers)
}

// Stop gracefully shuts down the pool.
// Safe to call multiple times; subsequent calls are no-ops.
func (p *Pool) Stop() {
	p.stopOnce.Do(func() {
		logging.Info("Work pool stopping")
		if p.cancel != nil {
			p.cancel()
		}
		close(p.stopChan)
		p.wg.Wait()
		p.mu.RLock()
		created := p.totalCreated
		completed := p.totalCompleted
		failed := p.totalFailed
		p.mu.RUnlock()
		logging.Info("Work pool stopped",
			"created", created,
			"completed", completed,
			"failed", failed)
	})
}

// Submit adds a work item to the queue.
// The item's Priority field is used as-is (no defaulting).
// Use SubmitFunc for the common case with automatic PriorityNormal.
func (p *Pool) Submit(item *Item) string {
	p.mu.Lock()
	// Reject submissions after pool is stopped
	if p.ctx != nil && p.ctx.Err() != nil {
		p.mu.Unlock()
		return "" // Pool is stopped, reject submission
	}

	item.ID = p.generateID()
	item.Status = StatusPending
	item.CreatedAt = time.Now()

	heap.Push(&p.pending, item)
	p.pendingIndex[item.ID] = item
	p.totalCreated++
	p.mu.Unlock()

	p.notify(Event{Item: copyItem(item), Change: "created"})

	// Signal pending processor (non-blocking)
	select {
	case p.workSignal <- struct{}{}:
	default:
		// Signal already pending, no need to send another
	}

	return item.ID
}

// SubmitFunc is a convenience for simple work without progress tracking.
// Uses PriorityNormal by default.
func (p *Pool) SubmitFunc(typ Type, desc string, fn func() (string, error)) string {
	item := &Item{
		Type:        typ,
		Description: desc,
		Priority:    PriorityNormal,
		workFn:      fn,
	}
	return p.Submit(item)
}

// SubmitWithProgress submits work that reports progress.
// Uses PriorityNormal by default.
func (p *Pool) SubmitWithProgress(typ Type, desc string, fn func(progress func(pct float64, msg string)) (string, error)) string {
	item := &Item{
		Type:        typ,
		Description: desc,
		Priority:    PriorityNormal,
	}

	// Wrap the function to inject progress callback
	item.workFn = func() (string, error) {
		return fn(func(pct float64, msg string) {
			p.mu.Lock()
			item.Progress = pct
			item.ProgressMsg = msg
			itemCopy := copyItem(item)
			p.mu.Unlock()
			p.notify(Event{Item: itemCopy, Change: "progress"})
		})
	}

	return p.Submit(item)
}

// SubmitWithData submits work that returns arbitrary data.
// The dataFn should return (result string, data any, error).
// Uses PriorityNormal by default.
func (p *Pool) SubmitWithData(typ Type, desc string, source string, category string, dataFn func() (string, any, error)) string {
	item := &Item{
		Type:        typ,
		Description: desc,
		Source:      source,
		Category:    category,
		Priority:    PriorityNormal,
	}

	// Wrap the function to capture data
	item.workFn = func() (string, error) {
		result, data, err := dataFn()
		// Synchronize write to item.Data - Snapshot() reads this concurrently
		p.mu.Lock()
		item.Data = data
		p.mu.Unlock()
		return result, err
	}

	return p.Submit(item)
}

// SubmitFuncWithPriority submits simple work with a specific priority.
func (p *Pool) SubmitFuncWithPriority(typ Type, desc string, priority int, fn func() (string, error)) string {
	item := &Item{
		Type:        typ,
		Description: desc,
		Priority:    priority,
		workFn:      fn,
	}
	return p.Submit(item)
}

// SubmitWithDataAndPriority submits work with data and a specific priority.
func (p *Pool) SubmitWithDataAndPriority(typ Type, desc string, source string, category string, priority int, dataFn func() (string, any, error)) string {
	item := &Item{
		Type:        typ,
		Description: desc,
		Source:      source,
		Category:    category,
		Priority:    priority,
	}

	// Wrap the function to capture data
	item.workFn = func() (string, error) {
		result, data, err := dataFn()
		// Synchronize write to item.Data - Snapshot() reads this concurrently
		p.mu.Lock()
		item.Data = data
		p.mu.Unlock()
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
		case <-p.workSignal:
			// Signal received, try to dispatch
			p.dispatchPending()
		}
	}
}

// dispatchPending sends pending items to workers if capacity available.
// Uses heap.Pop for O(log n) extraction of highest priority item.
func (p *Pool) dispatchPending() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Dispatch items while we have worker capacity
	for p.pending.Len() > 0 && len(p.active) < p.workers {
		item := heap.Pop(&p.pending).(*Item)
		delete(p.pendingIndex, item.ID)

		item.Status = StatusActive
		item.StartedAt = time.Now()
		p.active[item.ID] = item

		logging.Debug("Dispatching work",
			"id", item.ID,
			"desc", item.Description,
			"priority", item.Priority,
			"pending_remaining", p.pending.Len())

		p.notify(Event{Item: copyItem(item), Change: "started"})

		// Execute in goroutine (concurrency is limited by active map size)
		p.wg.Add(1)
		go p.execute(item)
	}
}

// execute runs a single work item.
func (p *Pool) execute(item *Item) {
	defer p.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack() // Auto-sizes, no truncation
			logging.Error("Work panicked",
				"id", item.ID,
				"type", item.Type,
				"desc", item.Description,
				"panic", r,
				"stack", string(stack))
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
		p.totalFailed++
	} else {
		item.Status = StatusComplete
		p.totalCompleted++
	}

	delete(p.active, item.ID)
	p.completed.Push(item)
	itemCopy := copyItem(item)
	p.mu.Unlock()

	change := "completed"
	if err != nil {
		change = "failed"
	}
	p.notify(Event{Item: itemCopy, Change: change})
}

// Snapshot returns the current state for UI display.
// Returns deep copies of items to prevent data races.
func (p *Pool) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Deep copy pending items
	pending := make([]*Item, p.pending.Len())
	for i, item := range p.pending {
		pending[i] = copyItem(item)
	}

	// Deep copy active items
	active := make([]*Item, 0, len(p.active))
	for _, item := range p.active {
		active = append(active, copyItem(item))
	}

	// Deep copy completed items
	completedRaw := p.completed.All()
	completed := make([]*Item, len(completedRaw))
	for i, item := range completedRaw {
		completed[i] = copyItem(item)
	}

	return Snapshot{
		Pending:   pending,
		Active:    active,
		Completed: completed,
		Stats: Stats{
			TotalCreated:   p.totalCreated,
			TotalCompleted: p.totalCompleted,
			TotalFailed:    p.totalFailed,
			WorkersActive:  len(p.active),
			WorkersTotal:   p.workers,
			PendingCount:   len(p.pending),
		},
	}
}

// copyItem creates a shallow copy of an Item (sufficient for UI display).
// Does not copy workFn, progressFn, or heapIndex as they are internal.
func copyItem(item *Item) *Item {
	if item == nil {
		return nil
	}
	return &Item{
		ID:          item.ID,
		Type:        item.Type,
		Status:      item.Status,
		Description: item.Description,
		CreatedAt:   item.CreatedAt,
		StartedAt:   item.StartedAt,
		FinishedAt:  item.FinishedAt,
		Progress:    item.Progress,
		ProgressMsg: item.ProgressMsg,
		Result:      item.Result,
		Error:       item.Error,
		Data:        item.Data, // Note: Data itself is not deep copied
		Source:      item.Source,
		Category:    item.Category,
		Priority:    item.Priority,
		// heapIndex intentionally not copied - internal heap state, concurrent access unsafe
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
		TotalCreated:   p.totalCreated,
		TotalCompleted: p.totalCompleted,
		TotalFailed:    p.totalFailed,
		WorkersActive:  len(p.active),
		WorkersTotal:   p.workers,
		PendingCount:   p.pending.Len(),
	}
}

// PendingCount returns the number of pending items.
func (p *Pool) PendingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pending.Len()
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

// UpdatePriority changes the priority of a pending work item.
// Returns false if the item is not found or not pending.
// Uses O(1) lookup via pendingIndex.
func (p *Pool) UpdatePriority(id string, priority int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	item, ok := p.pendingIndex[id]
	if !ok {
		return false
	}

	if p.pending.update(item, priority) {
		logging.Debug("Work priority updated", "id", id, "priority", priority)
		return true
	}
	return false
}

