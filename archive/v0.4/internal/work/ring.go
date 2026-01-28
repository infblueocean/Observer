package work

import "sync"

// RingBuffer is a fixed-capacity circular buffer for storing completed work items.
// Thread-safe. Oldest items are evicted when capacity is reached.
type RingBuffer struct {
	mu       sync.RWMutex
	items    []*Item
	capacity int
	head     int  // Next write position
	count    int  // Current number of items
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 100
	}
	return &RingBuffer{
		items:    make([]*Item, capacity),
		capacity: capacity,
	}
}

// Push adds an item to the buffer, evicting the oldest if full.
func (r *RingBuffer) Push(item *Item) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.items[r.head] = item
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
}

// All returns all items in the buffer, newest first.
func (r *RingBuffer) All() []*Item {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.count == 0 {
		return nil
	}

	result := make([]*Item, r.count)
	for i := 0; i < r.count; i++ {
		// Start from most recent (head-1) and go backwards
		idx := (r.head - 1 - i + r.capacity) % r.capacity
		result[i] = r.items[idx]
	}
	return result
}

// Recent returns the n most recent items, newest first.
func (r *RingBuffer) Recent(n int) []*Item {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.count == 0 {
		return nil
	}

	if n > r.count {
		n = r.count
	}

	result := make([]*Item, n)
	for i := 0; i < n; i++ {
		idx := (r.head - 1 - i + r.capacity) % r.capacity
		result[i] = r.items[idx]
	}
	return result
}

// Len returns the current number of items in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

// Clear removes all items from the buffer.
func (r *RingBuffer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = make([]*Item, r.capacity)
	r.head = 0
	r.count = 0
}

// Filter returns items matching the predicate, newest first.
func (r *RingBuffer) Filter(pred func(*Item) bool) []*Item {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Item
	for i := 0; i < r.count; i++ {
		idx := (r.head - 1 - i + r.capacity) % r.capacity
		if pred(r.items[idx]) {
			result = append(result, r.items[idx])
		}
	}
	return result
}
