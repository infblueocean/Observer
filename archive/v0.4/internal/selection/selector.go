// Package selection provides composable predicates for narrowing item views.
// Selectors are generic functions that filter items without knowing about rendering.
package selection

import (
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

// Selector narrows the view to matching items.
// Selectors compose: TimeSelector + SourceSelector = "past hour from Reuters"
type Selector interface {
	Name() string
	Match(item *feeds.Item) bool
}

// TimeSelector filters items by age.
type TimeSelector struct {
	name   string
	maxAge time.Duration // Items older than this are excluded (0 = no max)
	minAge time.Duration // Items newer than this are excluded (0 = no min)
}

// NewTimeSelector creates a time-based selector.
func NewTimeSelector(name string, maxAge, minAge time.Duration) TimeSelector {
	return TimeSelector{name: name, maxAge: maxAge, minAge: minAge}
}

func (s TimeSelector) Name() string { return s.name }

func (s TimeSelector) Match(item *feeds.Item) bool {
	age := time.Since(item.Published)
	if s.minAge > 0 && age < s.minAge {
		return false
	}
	if s.maxAge > 0 && age > s.maxAge {
		return false
	}
	return true
}

// Built-in time selectors
var (
	JustNow   = NewTimeSelector("Just Now", 15*time.Minute, 0)
	PastHour  = NewTimeSelector("Past Hour", time.Hour, 0)
	Today     = NewTimeSelector("Today", 24*time.Hour, 0)
	Yesterday = NewTimeSelector("Yesterday", 48*time.Hour, 24*time.Hour)
	ThisWeek  = NewTimeSelector("This Week", 7*24*time.Hour, 0)
)

// TimeSelectors returns the built-in time selector presets in order.
func TimeSelectors() []Selector {
	return []Selector{JustNow, PastHour, Today, Yesterday, ThisWeek}
}

// SourceSelector filters items by source name.
type SourceSelector struct {
	name    string
	sources map[string]bool
}

// NewSourceSelector creates a source-based selector.
func NewSourceSelector(name string, sources ...string) SourceSelector {
	m := make(map[string]bool)
	for _, s := range sources {
		m[s] = true
	}
	return SourceSelector{name: name, sources: m}
}

func (s SourceSelector) Name() string { return s.name }

func (s SourceSelector) Match(item *feeds.Item) bool {
	return s.sources[item.SourceName]
}

// AndSelector requires all child selectors to match.
type AndSelector struct {
	name      string
	selectors []Selector
}

// And combines selectors with AND logic.
func And(name string, selectors ...Selector) AndSelector {
	return AndSelector{name: name, selectors: selectors}
}

func (s AndSelector) Name() string { return s.name }

func (s AndSelector) Match(item *feeds.Item) bool {
	for _, sel := range s.selectors {
		if !sel.Match(item) {
			return false
		}
	}
	return true
}

// OrSelector requires any child selector to match.
type OrSelector struct {
	name      string
	selectors []Selector
}

// Or combines selectors with OR logic.
func Or(name string, selectors ...Selector) OrSelector {
	return OrSelector{name: name, selectors: selectors}
}

func (s OrSelector) Name() string { return s.name }

func (s OrSelector) Match(item *feeds.Item) bool {
	for _, sel := range s.selectors {
		if sel.Match(item) {
			return true
		}
	}
	return false
}

// NotSelector inverts a selector.
type NotSelector struct {
	selector Selector
}

// Not inverts a selector's match result.
func Not(selector Selector) NotSelector {
	return NotSelector{selector: selector}
}

func (s NotSelector) Name() string { return "Not " + s.selector.Name() }

func (s NotSelector) Match(item *feeds.Item) bool {
	return !s.selector.Match(item)
}

// Apply filters items through a selector, returning only matches.
// If selector is nil, returns all items.
func Apply(items []feeds.Item, selector Selector) []feeds.Item {
	if selector == nil {
		return items
	}
	result := make([]feeds.Item, 0, len(items)/2)
	for i := range items {
		if selector.Match(&items[i]) {
			result = append(result, items[i])
		}
	}
	return result
}
