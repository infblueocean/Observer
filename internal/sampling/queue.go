package sampling

import (
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

// SourceQueue holds items for a single source with adaptive polling
type SourceQueue struct {
	Name       string
	SourceType feeds.SourceType
	Weight     float64 // importance weight (default 1.0)

	// Queue state - items are soft-ordered by recency (newest first)
	items []feeds.Item
	mu    sync.RWMutex

	// Adaptive polling configuration
	pollInterval time.Duration // current interval
	basePoll     time.Duration // configured base interval
	minPoll      time.Duration // floor (won't poll faster than this)
	maxPoll      time.Duration // ceiling (won't poll slower than this)
	lastPoll     time.Time
	lastNewCount int // items found in last poll

	// Stats
	totalItems  int
	itemsPerDay float64 // rolling average
}

// NewSourceQueue creates a new source queue with default settings
func NewSourceQueue(name string, sourceType feeds.SourceType, basePoll time.Duration) *SourceQueue {
	return &SourceQueue{
		Name:         name,
		SourceType:   sourceType,
		Weight:       1.0,
		items:        make([]feeds.Item, 0),
		pollInterval: basePoll,
		basePoll:     basePoll,
		minPoll:      30 * time.Second,  // never poll faster than 30s
		maxPoll:      15 * time.Minute,  // never poll slower than 15min
		lastPoll:     time.Time{},       // zero time = never polled
	}
}

// SetWeight sets the importance weight for this source
func (q *SourceQueue) SetWeight(w float64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.Weight = w
}

// SetPollLimits configures the adaptive polling bounds
func (q *SourceQueue) SetPollLimits(min, max time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.minPoll = min
	q.maxPoll = max
}

// Add adds items to the queue, maintaining soft recency order
// Returns the number of new items added (not duplicates)
func (q *SourceQueue) Add(items []feeds.Item) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Build set of existing IDs for deduplication
	existing := make(map[string]bool, len(q.items))
	for _, item := range q.items {
		existing[item.ID] = true
	}

	// Add new items at the front (newest first)
	newCount := 0
	var newItems []feeds.Item
	for _, item := range items {
		if !existing[item.ID] {
			newItems = append(newItems, item)
			newCount++
		}
	}

	// Prepend new items (they're the freshest)
	q.items = append(newItems, q.items...)
	q.totalItems += newCount
	q.lastNewCount = newCount

	return newCount
}

// Len returns the number of items in the queue
func (q *SourceQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.items)
}

// Peek returns the nth item without removing it (0 = newest)
func (q *SourceQueue) Peek(n int) *feeds.Item {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if n < 0 || n >= len(q.items) {
		return nil
	}
	item := q.items[n]
	return &item
}

// Take returns up to n items from the front of the queue (newest first)
// Does not remove items - queues are persistent
func (q *SourceQueue) Take(n int) []feeds.Item {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if n > len(q.items) {
		n = len(q.items)
	}
	result := make([]feeds.Item, n)
	copy(result, q.items[:n])
	return result
}

// All returns all items in the queue
func (q *SourceQueue) All() []feeds.Item {
	q.mu.RLock()
	defer q.mu.RUnlock()
	result := make([]feeds.Item, len(q.items))
	copy(result, q.items)
	return result
}

// Clear removes all items from the queue
func (q *SourceQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = q.items[:0]
}

// Prune removes items older than the given duration
func (q *SourceQueue) Prune(maxAge time.Duration) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)

	// Items might not be perfectly ordered, so scan all
	var remaining []feeds.Item
	for _, item := range q.items {
		if item.Published.After(cutoff) {
			remaining = append(remaining, item)
		}
	}

	pruned := len(q.items) - len(remaining)
	q.items = remaining
	return pruned
}

// --- Adaptive Polling ---

// ShouldPoll returns true if it's time to poll this source
func (q *SourceQueue) ShouldPoll() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.lastPoll.IsZero() {
		return true // never polled
	}
	return time.Since(q.lastPoll) >= q.pollInterval
}

// MarkPolled records that a poll just happened
func (q *SourceQueue) MarkPolled() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.lastPoll = time.Now()
}

// AdjustInterval adapts the polling interval based on recent activity
// Call this after adding items from a poll
func (q *SourceQueue) AdjustInterval() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.lastNewCount > 0 {
		// Found new content - speed up polling
		q.pollInterval = time.Duration(float64(q.pollInterval) * 0.7)
		if q.pollInterval < q.minPoll {
			q.pollInterval = q.minPoll
		}
	} else {
		// No new content - slow down polling
		q.pollInterval = time.Duration(float64(q.pollInterval) * 1.5)
		if q.pollInterval > q.maxPoll {
			q.pollInterval = q.maxPoll
		}
	}
}

// PollInterval returns the current polling interval
func (q *SourceQueue) PollInterval() time.Duration {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.pollInterval
}

// LastPoll returns when this source was last polled
func (q *SourceQueue) LastPoll() time.Time {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.lastPoll
}

// LastNewCount returns how many new items were found in the last poll
func (q *SourceQueue) LastNewCount() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.lastNewCount
}

// --- Sampler Interface ---

// Sampler defines how to select items from multiple source queues
type Sampler interface {
	// Sample selects up to n items from the given queues
	// The implementation decides the selection strategy
	Sample(queues []*SourceQueue, n int) []feeds.Item
}
