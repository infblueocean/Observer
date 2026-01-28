package feeds

import (
	"sort"
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/logging"
)

// DefaultMaxItems is the default maximum number of items to keep in memory
const DefaultMaxItems = 10000

// SourceState tracks the state of a single source
type SourceState struct {
	Source         Source
	Config         RSSFeedConfig
	LastFetched    time.Time
	NextRefresh    time.Time
	LastError      error
	ItemCount      int
	ConsecErrors   int
	Fetching       bool
}

// RefreshProgress returns 0.0-1.0 indicating progress toward next refresh
func (s *SourceState) RefreshProgress() float64 {
	if s.Fetching {
		return 1.0
	}
	if s.LastFetched.IsZero() {
		return 1.0 // Never fetched, should fetch now
	}

	interval := time.Duration(s.Config.RefreshMinutes) * time.Minute
	elapsed := time.Since(s.LastFetched)

	progress := float64(elapsed) / float64(interval)
	if progress > 1.0 {
		return 1.0
	}
	return progress
}

// TimeUntilRefresh returns duration until next refresh
func (s *SourceState) TimeUntilRefresh() time.Duration {
	if s.LastFetched.IsZero() {
		return 0
	}
	interval := time.Duration(s.Config.RefreshMinutes) * time.Minute
	return interval - time.Since(s.LastFetched)
}

// ShouldRefresh returns true if this source needs refreshing
func (s *SourceState) ShouldRefresh() bool {
	if s.Fetching {
		return false
	}
	if s.LastFetched.IsZero() {
		return true
	}
	interval := time.Duration(s.Config.RefreshMinutes) * time.Minute

	// Back off on errors
	if s.ConsecErrors > 0 {
		backoff := time.Duration(s.ConsecErrors*s.ConsecErrors) * time.Minute
		if backoff > 30*time.Minute {
			backoff = 30 * time.Minute
		}
		interval += backoff
	}

	return time.Since(s.LastFetched) >= interval
}

// Aggregator manages multiple sources with independent refresh intervals
type Aggregator struct {
	mu       sync.RWMutex
	sources  map[string]*SourceState
	items    []Item
	filter   *Filter
	blocked  int // Count of blocked items
	maxItems int // Maximum items to keep in memory (0 = unlimited)
	evicted  int // Count of items evicted due to memory cap
}

// NewAggregator creates a new aggregator with default max items cap
func NewAggregator() *Aggregator {
	return NewAggregatorWithCap(DefaultMaxItems)
}

// NewAggregatorWithCap creates a new aggregator with a custom max items cap
// Set maxItems to 0 for unlimited (not recommended for production)
func NewAggregatorWithCap(maxItems int) *Aggregator {
	return &Aggregator{
		sources:  make(map[string]*SourceState),
		items:    make([]Item, 0),
		filter:   DefaultFilter(),
		maxItems: maxItems,
	}
}

// AddSource registers a source with the aggregator
func (a *Aggregator) AddSource(source Source, config RSSFeedConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Default refresh interval if not set
	if config.RefreshMinutes == 0 {
		config.RefreshMinutes = RefreshNormal
	}
	// Default weight if not set
	if config.Weight == 0 {
		config.Weight = 1.0
	}

	a.sources[config.Name] = &SourceState{
		Source: source,
		Config: config,
	}
}

// GetSourceStates returns all source states for UI display
// Returns copies of the states to avoid data races after the lock is released
func (a *Aggregator) GetSourceStates() []SourceState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	states := make([]SourceState, 0, len(a.sources))
	for _, s := range a.sources {
		states = append(states, *s) // Copy the struct
	}
	return states
}

// GetSourcesDueForRefresh returns sources that need refreshing
// Returns copies of the states to avoid data races after the lock is released
func (a *Aggregator) GetSourcesDueForRefresh() []SourceState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var due []SourceState
	for _, s := range a.sources {
		if s.ShouldRefresh() {
			due = append(due, *s) // Copy the struct
		}
	}
	return due
}

// MarkFetching marks a source as currently fetching
func (a *Aggregator) MarkFetching(name string, fetching bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if s, ok := a.sources[name]; ok {
		s.Fetching = fetching
	}
}

// UpdateSourceState updates state after a fetch attempt
func (a *Aggregator) UpdateSourceState(name string, itemCount int, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	s, ok := a.sources[name]
	if !ok {
		return
	}

	s.LastFetched = time.Now()
	s.Fetching = false
	s.ItemCount = itemCount
	s.LastError = err

	if err != nil {
		s.ConsecErrors++
		if s.ConsecErrors >= 3 {
			logging.Warn("Source experiencing repeated failures", "source", name, "consecutive_errors", s.ConsecErrors, "error", err)
		}
	} else {
		if s.ConsecErrors > 0 {
			logging.Info("Source recovered", "source", name, "previous_errors", s.ConsecErrors)
		}
		s.ConsecErrors = 0
	}

	// Calculate next refresh
	interval := time.Duration(s.Config.RefreshMinutes) * time.Minute
	s.NextRefresh = s.LastFetched.Add(interval)
}

// MergeItems merges new items into the aggregate, deduplicating by URL and filtering ads
func (a *Aggregator) MergeItems(newItems []Item) int {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Build URL index of existing items
	urlIndex := make(map[string]bool)
	for _, item := range a.items {
		if item.URL != "" {
			urlIndex[item.URL] = true
		}
	}

	// Add new unique items, filtering out ads
	added := 0
	for _, item := range newItems {
		// Skip duplicates
		if item.URL != "" && urlIndex[item.URL] {
			continue
		}

		// Filter ads/sponsored content
		if a.filter != nil && a.filter.ShouldBlock(item) {
			a.blocked++
			continue
		}

		if item.URL != "" {
			urlIndex[item.URL] = true
		}
		a.items = append(a.items, item)
		added++
	}

	// Enforce memory cap by evicting oldest items
	a.enforceCapLocked()

	return added
}

// enforceCapLocked evicts oldest items if we exceed maxItems
// Must be called with mu held
func (a *Aggregator) enforceCapLocked() {
	if a.maxItems <= 0 || len(a.items) <= a.maxItems {
		return
	}

	overflow := len(a.items) - a.maxItems

	// Sort by timestamp (oldest first) to identify items to evict
	// We use a copy of indices to avoid modifying the original slice during sort
	type indexedItem struct {
		index int
		ts    time.Time
	}
	indexed := make([]indexedItem, len(a.items))
	for i, item := range a.items {
		indexed[i] = indexedItem{index: i, ts: itemTimestamp(item)}
	}

	// Sort by timestamp ascending (oldest first)
	sort.Slice(indexed, func(i, j int) bool {
		return indexed[i].ts.Before(indexed[j].ts)
	})

	// Mark oldest items for removal
	toRemove := make(map[int]bool, overflow)
	for i := 0; i < overflow; i++ {
		toRemove[indexed[i].index] = true
	}

	// Build new slice without evicted items
	newItems := make([]Item, 0, a.maxItems)
	for i, item := range a.items {
		if !toRemove[i] {
			newItems = append(newItems, item)
		}
	}

	a.evicted += overflow
	a.items = newItems

	logging.Info("Memory cap enforced, evicted oldest items",
		"evicted", overflow,
		"total_evicted", a.evicted,
		"remaining", len(a.items),
		"cap", a.maxItems)
}

// itemTimestamp returns the effective timestamp for an item
// Uses Published if available, falls back to Fetched
func itemTimestamp(item Item) time.Time {
	if !item.Published.IsZero() {
		return item.Published
	}
	if !item.Fetched.IsZero() {
		return item.Fetched
	}
	// Fallback to epoch if both are zero (shouldn't happen in practice)
	return time.Time{}
}

// BlockedCount returns number of items blocked by filter
func (a *Aggregator) BlockedCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.blocked
}

// EvictedCount returns number of items evicted due to memory cap
func (a *Aggregator) EvictedCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.evicted
}

// MaxItems returns the configured maximum items cap
func (a *Aggregator) MaxItems() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.maxItems
}

// SetMaxItems updates the maximum items cap and enforces it immediately
func (a *Aggregator) SetMaxItems(max int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.maxItems = max
	a.enforceCapLocked()
}

// GetItems returns all items (caller should sort/filter)
func (a *Aggregator) GetItems() []Item {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Return a copy
	items := make([]Item, len(a.items))
	copy(items, a.items)
	return items
}

// SourceCount returns the number of registered sources
func (a *Aggregator) SourceCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.sources)
}

// ItemCount returns the total number of items
func (a *Aggregator) ItemCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.items)
}

// CategoryStats returns item counts by category
func (a *Aggregator) CategoryStats() map[string]int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stats := make(map[string]int)
	for _, s := range a.sources {
		stats[s.Config.Category] += s.ItemCount
	}
	return stats
}

// SourceHealth represents source status counts
type SourceHealth struct {
	Total   int // Total registered sources
	Healthy int // Sources with no recent errors (ConsecErrors == 0)
	Failing int // Sources with repeated errors (ConsecErrors >= 3)
	Warning int // Sources with some errors (ConsecErrors 1-2)
}

// GetSourceHealth returns counts of healthy vs failing sources
func (a *Aggregator) GetSourceHealth() SourceHealth {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var health SourceHealth
	health.Total = len(a.sources)

	for _, s := range a.sources {
		// Only count sources that have been fetched at least once
		if s.LastFetched.IsZero() {
			continue
		}

		switch {
		case s.ConsecErrors == 0:
			health.Healthy++
		case s.ConsecErrors >= 3:
			health.Failing++
		default:
			health.Warning++
		}
	}

	return health
}
