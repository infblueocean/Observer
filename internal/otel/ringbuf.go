package otel

import "sync"

// DefaultRingSize is the default ring buffer capacity.
// Power of two allows modulo to compile to bitwise AND.
const DefaultRingSize = 1024

// RingBuffer is a fixed-size circular buffer of Events.
// Goroutine-safe for concurrent Push and read operations.
type RingBuffer struct {
	mu    sync.Mutex
	buf   []Event
	size  int
	head  int // next write position
	count int // number of valid entries (0..size)
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = DefaultRingSize
	}
	return &RingBuffer{
		buf:  make([]Event, size),
		size: size,
	}
}

// Push adds an event, overwriting the oldest if full. Goroutine-safe.
// Copies the Extra map (shallow copy of values) to prevent aliasing bugs.
func (r *RingBuffer) Push(e Event) {
	if e.Extra != nil {
		cp := make(map[string]any, len(e.Extra))
		for k, v := range e.Extra {
			cp[k] = v
		}
		e.Extra = cp
	}
	r.mu.Lock()
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}
	r.mu.Unlock()
}

// Snapshot returns a copy of all events in chronological order (oldest first).
// The returned slice is safe to use without locks.
func (r *RingBuffer) Snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.count == 0 {
		return nil
	}

	result := make([]Event, r.count)
	if r.count < r.size {
		copy(result, r.buf[:r.count])
	} else {
		n := copy(result, r.buf[r.head:])
		copy(result[n:], r.buf[:r.head])
	}
	return result
}

// Last returns the N most recent events in chronological order.
// If n > count, returns all events. If n <= 0, returns nil.
func (r *RingBuffer) Last(n int) []Event {
	if n <= 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.count == 0 {
		return nil
	}
	if n > r.count {
		n = r.count
	}

	result := make([]Event, n)
	start := (r.head - n + r.size) % r.size
	if start+n <= r.size {
		copy(result, r.buf[start:start+n])
	} else {
		first := r.size - start
		copy(result, r.buf[start:])
		copy(result[first:], r.buf[:n-first])
	}
	return result
}

// Len returns the number of events currently in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// Cap returns the buffer capacity.
func (r *RingBuffer) Cap() int {
	return r.size
}

// Stats returns aggregated counts by EventKind over all buffered events.
func (r *RingBuffer) Stats() map[EventKind]int {
	r.mu.Lock()
	defer r.mu.Unlock()

	counts := make(map[EventKind]int)
	start := 0
	if r.count >= r.size {
		start = r.head
	}
	for i := 0; i < r.count; i++ {
		idx := (start + i) % r.size
		counts[r.buf[idx].Kind]++
	}
	return counts
}
