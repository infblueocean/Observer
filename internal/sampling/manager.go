package sampling

import (
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/logging"
)

// QueueManager coordinates all source queues and sampling
type QueueManager struct {
	queues  map[string]*SourceQueue // source name -> queue
	mu      sync.RWMutex
	sampler Sampler

	// Configuration
	maxItemAge    time.Duration // prune items older than this
	pruneInterval time.Duration // how often to prune
	lastPrune     time.Time
}

// NewQueueManager creates a new queue manager with the given sampler
func NewQueueManager(sampler Sampler) *QueueManager {
	if sampler == nil {
		sampler = NewRoundRobinSampler()
	}
	return &QueueManager{
		queues:        make(map[string]*SourceQueue),
		sampler:       sampler,
		maxItemAge:    48 * time.Hour, // keep 2 days of items
		pruneInterval: 1 * time.Hour,  // prune every hour
		lastPrune:     time.Now(),     // don't prune immediately on startup
	}
}

// SetSampler changes the sampling strategy
func (m *QueueManager) SetSampler(sampler Sampler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sampler = sampler
}

// RegisterSource creates a queue for a new source
func (m *QueueManager) RegisterSource(name string, sourceType feeds.SourceType, basePoll time.Duration) *SourceQueue {
	m.mu.Lock()
	defer m.mu.Unlock()

	if q, exists := m.queues[name]; exists {
		return q
	}

	q := NewSourceQueue(name, sourceType, basePoll)
	m.queues[name] = q
	logging.Debug("Registered source queue", "source", name, "basePoll", basePoll)
	return q
}

// GetQueue returns the queue for a source, or nil if not found
func (m *QueueManager) GetQueue(name string) *SourceQueue {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.queues[name]
}

// AllQueues returns all registered queues
func (m *QueueManager) AllQueues() []*SourceQueue {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*SourceQueue, 0, len(m.queues))
	for _, q := range m.queues {
		result = append(result, q)
	}
	return result
}

// AddItems adds items to the appropriate source queue
// Returns the number of new (non-duplicate) items added
func (m *QueueManager) AddItems(sourceName string, items []feeds.Item) int {
	m.mu.RLock()
	q := m.queues[sourceName]
	m.mu.RUnlock()

	if q == nil {
		logging.Warn("No queue for source", "source", sourceName)
		return 0
	}

	newCount := q.Add(items)
	q.AdjustInterval() // adapt polling based on new content

	if newCount > 0 {
		logging.Debug("Added items to queue",
			"source", sourceName,
			"new", newCount,
			"total", q.Len(),
			"nextPoll", q.PollInterval())
	}

	return newCount
}

// Sample returns items using the configured sampler
func (m *QueueManager) Sample(n int) []feeds.Item {
	m.mu.RLock()
	sampler := m.sampler
	queues := make([]*SourceQueue, 0, len(m.queues))
	for _, q := range m.queues {
		queues = append(queues, q)
	}
	m.mu.RUnlock()

	// Periodically prune old items
	m.maybePrune()

	return sampler.Sample(queues, n)
}

// GetSourcesDuePoll returns sources that should be polled now
func (m *QueueManager) GetSourcesDuePoll() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var due []string
	for name, q := range m.queues {
		if q.ShouldPoll() {
			due = append(due, name)
		}
	}
	return due
}

// MarkPolled records that a source was just polled
func (m *QueueManager) MarkPolled(sourceName string) {
	m.mu.RLock()
	q := m.queues[sourceName]
	m.mu.RUnlock()

	if q != nil {
		q.MarkPolled()
	}
}

// TotalItems returns the total number of items across all queues
func (m *QueueManager) TotalItems() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0
	for _, q := range m.queues {
		total += q.Len()
	}
	return total
}

// Stats returns statistics about the queues
func (m *QueueManager) Stats() QueueStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := QueueStats{
		SourceCount: len(m.queues),
		BySource:    make(map[string]SourceStats),
	}

	for name, q := range m.queues {
		stats.TotalItems += q.Len()
		stats.BySource[name] = SourceStats{
			ItemCount:    q.Len(),
			PollInterval: q.PollInterval(),
			LastPoll:     q.LastPoll(),
			LastNewCount: q.LastNewCount(),
		}
	}

	return stats
}

// maybePrune removes old items if enough time has passed
func (m *QueueManager) maybePrune() {
	m.mu.Lock()
	if time.Since(m.lastPrune) < m.pruneInterval {
		m.mu.Unlock()
		return
	}
	m.lastPrune = time.Now()
	queues := make([]*SourceQueue, 0, len(m.queues))
	for _, q := range m.queues {
		queues = append(queues, q)
	}
	maxAge := m.maxItemAge
	m.mu.Unlock()

	// Prune outside the lock
	totalPruned := 0
	for _, q := range queues {
		pruned := q.Prune(maxAge)
		totalPruned += pruned
	}

	if totalPruned > 0 {
		logging.Info("Pruned old items", "count", totalPruned, "maxAge", maxAge)
	}
}

// QueueStats holds statistics about all queues
type QueueStats struct {
	SourceCount int
	TotalItems  int
	BySource    map[string]SourceStats
}

// SourceStats holds statistics for a single source
type SourceStats struct {
	ItemCount    int
	PollInterval time.Duration
	LastPoll     time.Time
	LastNewCount int
}
