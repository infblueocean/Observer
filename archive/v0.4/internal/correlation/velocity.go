package correlation

import (
	"sync"
	"time"
)

// VelocitySnapshot tracks activity at a point in time.
type VelocitySnapshot struct {
	Timestamp time.Time
	Rate15m   float64
	Rate1h    float64
	Rate6h    float64
	Sources   int
}

// VelocitySpike is emitted when activity exceeds threshold.
type VelocitySpike struct {
	ClusterID string
	Window    string  // "15m", "1h", "6h"
	Rate      float64
}

// VelocityTracker tracks activity velocity across multiple time windows.
type VelocityTracker struct {
	mu        sync.RWMutex
	snapshots map[string]*RingBuffer // clusterID → velocity history

	// Spike detection
	spikeThreshold float64
}

// NewVelocityTracker creates a new velocity tracker.
func NewVelocityTracker() *VelocityTracker {
	return &VelocityTracker{
		snapshots:      make(map[string]*RingBuffer),
		spikeThreshold: 2.0, // 2x baseline = spike
	}
}

// Record records a new item and checks for spikes.
// Returns a spike event if 2-of-3 windows are elevated.
func (v *VelocityTracker) Record(clusterID string, itemCount, sourceCount int) *VelocitySpike {
	v.mu.Lock()
	defer v.mu.Unlock()

	if _, ok := v.snapshots[clusterID]; !ok {
		v.snapshots[clusterID] = NewRingBuffer(288) // 24h at 5min intervals
	}

	buf := v.snapshots[clusterID]
	now := time.Now()

	// Calculate rates based on recent history
	rate15m := v.calculateRate(buf, 15*time.Minute, itemCount)
	rate1h := v.calculateRate(buf, time.Hour, itemCount)
	rate6h := v.calculateRate(buf, 6*time.Hour, itemCount)

	snapshot := VelocitySnapshot{
		Timestamp: now,
		Rate15m:   rate15m,
		Rate1h:    rate1h,
		Rate6h:    rate6h,
		Sources:   sourceCount,
	}
	buf.Add(snapshot)

	// Check for spike (require 2-of-3 windows elevated)
	elevated := 0
	if rate15m > v.spikeThreshold*v.getBaseline(buf, 15*time.Minute) {
		elevated++
	}
	if rate1h > v.spikeThreshold*v.getBaseline(buf, time.Hour) {
		elevated++
	}
	if rate6h > v.spikeThreshold*v.getBaseline(buf, 6*time.Hour) {
		elevated++
	}

	if elevated >= 2 {
		window := "1h"
		if rate15m > rate1h {
			window = "15m"
		}
		return &VelocitySpike{
			ClusterID: clusterID,
			Window:    window,
			Rate:      rate1h,
		}
	}

	return nil
}

// calculateRate calculates the rate of items in a time window.
func (v *VelocityTracker) calculateRate(buf *RingBuffer, window time.Duration, currentCount int) float64 {
	cutoff := time.Now().Add(-window)
	recent := buf.Since(cutoff)
	if len(recent) == 0 {
		return float64(currentCount) / window.Hours()
	}

	// Count items in window
	count := 0
	for range recent {
		count++
	}
	return float64(count) / window.Hours()
}

// getBaseline calculates the baseline rate for comparison.
func (v *VelocityTracker) getBaseline(buf *RingBuffer, window time.Duration) float64 {
	// Use older data as baseline (skip recent spike period)
	oldStart := time.Now().Add(-24 * time.Hour)
	oldEnd := time.Now().Add(-window)

	older := buf.Between(oldStart, oldEnd)
	if len(older) == 0 {
		return 1.0 // Default baseline
	}

	hours := oldEnd.Sub(oldStart).Hours()
	return float64(len(older)) / hours
}

// GetSparkline returns normalized velocity values for sparkline rendering.
func (v *VelocityTracker) GetSparkline(clusterID string, points int) []float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	buf, ok := v.snapshots[clusterID]
	if !ok {
		return nil
	}

	recent := buf.Last(points)
	if len(recent) == 0 {
		return nil
	}

	// Normalize to 0-1 range
	var maxRate float64
	data := make([]float64, len(recent))
	for i, item := range recent {
		s := item.(VelocitySnapshot)
		data[i] = s.Rate1h
		if s.Rate1h > maxRate {
			maxRate = s.Rate1h
		}
	}

	if maxRate > 0 {
		for i := range data {
			data[i] /= maxRate
		}
	}

	return data
}

// GetVelocity returns the current velocity for a cluster.
func (v *VelocityTracker) GetVelocity(clusterID string) float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	buf, ok := v.snapshots[clusterID]
	if !ok {
		return 0
	}

	recent := buf.Last(1)
	if len(recent) == 0 {
		return 0
	}

	return recent[0].(VelocitySnapshot).Rate1h
}

// RingBuffer is a fixed-size circular buffer for velocity snapshots.
type RingBuffer struct {
	data  []interface{}
	size  int
	head  int
	count int
}

// NewRingBuffer creates a new ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]interface{}, size),
		size: size,
	}
}

// Add adds an item to the buffer, overwriting oldest if full.
func (r *RingBuffer) Add(item interface{}) {
	r.data[r.head] = item
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// Last returns the most recent n items.
func (r *RingBuffer) Last(n int) []interface{} {
	if n > r.count {
		n = r.count
	}
	result := make([]interface{}, n)
	for i := 0; i < n; i++ {
		idx := (r.head - n + i + r.size) % r.size
		result[i] = r.data[idx]
	}
	return result
}

// Since returns all items since the given time.
func (r *RingBuffer) Since(t time.Time) []interface{} {
	var result []interface{}
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + r.size) % r.size
		item := r.data[idx]
		if s, ok := item.(VelocitySnapshot); ok && s.Timestamp.After(t) {
			result = append(result, item)
		}
	}
	return result
}

// Between returns items between two times.
func (r *RingBuffer) Between(start, end time.Time) []interface{} {
	var result []interface{}
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + r.size) % r.size
		item := r.data[idx]
		if s, ok := item.(VelocitySnapshot); ok {
			if s.Timestamp.After(start) && s.Timestamp.Before(end) {
				result = append(result, item)
			}
		}
	}
	return result
}
