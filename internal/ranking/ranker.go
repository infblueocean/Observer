// Package ranking provides composable item ranking for feed curation.
// Rankers score items; the highest-scored items fill each time band slot.
//
// Pipeline: feed -> filter -> rank -> render
//
// Design principles:
// - Rankers are stateless functions: (item, context) -> score
// - Multiple rankers can be active simultaneously
// - Scores are combined via weighted average or other strategies
// - Rankers don't mutate items; they just score them
package ranking

import (
	"time"

	"github.com/abelbrown/observer/internal/correlation"
	"github.com/abelbrown/observer/internal/feeds"
)

// Ranker scores items for ranking decisions.
// Implementations should be stateless and thread-safe.
type Ranker interface {
	// Name returns a unique identifier for this ranker
	Name() string

	// Score returns a score for the item (higher = more important)
	// Scores should be normalized to [0, 1] range for combinability
	Score(item *feeds.Item, ctx *Context) float64
}

// Context provides data rankers may need for scoring decisions.
// Not all rankers use all fields - take what you need.
type Context struct {
	// Time context
	Now time.Time // Current time for freshness calculations

	// Correlation context (may be nil if correlation engine not available)
	Correlation *correlation.Engine

	// Source context
	SourceWeights map[string]float64 // Source name -> weight multiplier

	// Diversity context (for penalizing over-represented sources)
	SourceCounts map[string]int // How many items from each source already selected

	// User context (future: personalization)
	// UserPreferences *UserPrefs

	// Batch context (all items being ranked, for relative scoring)
	AllItems []feeds.Item
}

// NewContext creates a context with sensible defaults
func NewContext() *Context {
	return &Context{
		Now:           time.Now(),
		SourceWeights: make(map[string]float64),
		SourceCounts:  make(map[string]int),
	}
}

// WithCorrelation adds correlation engine to context
func (c *Context) WithCorrelation(engine *correlation.Engine) *Context {
	c.Correlation = engine
	return c
}

// WithSourceWeights adds source weights to context
func (c *Context) WithSourceWeights(weights map[string]float64) *Context {
	c.SourceWeights = weights
	return c
}

// WithItems adds the full item batch for relative scoring
func (c *Context) WithItems(items []feeds.Item) *Context {
	c.AllItems = items
	return c
}

// Result holds a scored item
type Result struct {
	Item  feeds.Item
	Score float64
}

// Rank scores all items and returns them sorted by score (highest first)
func Rank(items []feeds.Item, ranker Ranker, ctx *Context) []Result {
	results := make([]Result, len(items))
	for i, item := range items {
		results[i] = Result{
			Item:  item,
			Score: ranker.Score(&item, ctx),
		}
	}

	// Sort by score descending
	sortByScore(results)
	return results
}

// TopN returns the top N items after ranking
func TopN(items []feeds.Item, n int, ranker Ranker, ctx *Context) []feeds.Item {
	if len(items) <= n {
		return items
	}

	results := Rank(items, ranker, ctx)
	top := make([]feeds.Item, n)
	for i := 0; i < n; i++ {
		top[i] = results[i].Item
	}
	return top
}

// sortByScore sorts results by score descending (in-place)
func sortByScore(results []Result) {
	// Simple insertion sort for now (items lists are small)
	for i := 1; i < len(results); i++ {
		j := i
		for j > 0 && results[j].Score > results[j-1].Score {
			results[j], results[j-1] = results[j-1], results[j]
			j--
		}
	}
}
