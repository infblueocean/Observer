// Package filters provides built-in filter implementations for Observer v0.5.
//
// All filters in this package are safe for concurrent use and respect context cancellation.
package filters

import (
	"context"
	"time"

	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// TimeFilter filters items by age.
//
// Items older than MaxAge (relative to time.Now()) are excluded.
// This filter is stateless and safe for concurrent use.
type TimeFilter struct {
	MaxAge time.Duration
}

// NewTimeFilter creates a time filter with the given max age.
//
// MaxAge must be positive. Items with Published time older than
// time.Now().Add(-MaxAge) will be filtered out.
func NewTimeFilter(maxAge time.Duration) *TimeFilter {
	if maxAge < 0 {
		maxAge = 0
	}
	return &TimeFilter{MaxAge: maxAge}
}

// Name returns "time".
func (f *TimeFilter) Name() string {
	return "time"
}

// Run filters items to only include those within the time window.
//
// Respects context cancellation - checks ctx every 1000 items.
func (f *TimeFilter) Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-f.MaxAge)
	filtered := make([]model.Item, 0, len(items))

	for i, item := range items {
		// Check context periodically for large item sets
		if i > 0 && i%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		if item.Published.After(cutoff) {
			filtered = append(filtered, item)
		}
	}

	return filtered, nil
}

// Since returns the cutoff time for this filter (current time minus MaxAge).
func (f *TimeFilter) Since() time.Time {
	return time.Now().Add(-f.MaxAge)
}
