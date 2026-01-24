package feeds

import (
	"sync"
	"time"
)

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
	mu      sync.RWMutex
	sources map[string]*SourceState
	items   []Item
	filter  *Filter
	blocked int // Count of blocked items
}

// NewAggregator creates a new aggregator
func NewAggregator() *Aggregator {
	return &Aggregator{
		sources: make(map[string]*SourceState),
		items:   make([]Item, 0),
		filter:  DefaultFilter(),
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
func (a *Aggregator) GetSourceStates() []*SourceState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	states := make([]*SourceState, 0, len(a.sources))
	for _, s := range a.sources {
		states = append(states, s)
	}
	return states
}

// GetSourcesDueForRefresh returns sources that need refreshing
func (a *Aggregator) GetSourcesDueForRefresh() []*SourceState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var due []*SourceState
	for _, s := range a.sources {
		if s.ShouldRefresh() {
			due = append(due, s)
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
	} else {
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

	return added
}

// BlockedCount returns number of items blocked by filter
func (a *Aggregator) BlockedCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.blocked
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
