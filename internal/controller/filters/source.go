package filters

import (
	"context"

	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// SourceBalanceFilter ensures variety by limiting items per source.
//
// This prevents a single prolific source from dominating the feed.
// Items are processed in order, so earlier items from a source are kept.
//
// This filter is stateless and safe for concurrent use.
type SourceBalanceFilter struct {
	MaxPerSource int
}

// NewSourceBalanceFilter creates a source balance filter.
//
// maxPerSource must be positive. If <= 0, defaults to 10.
func NewSourceBalanceFilter(maxPerSource int) *SourceBalanceFilter {
	if maxPerSource <= 0 {
		maxPerSource = 10
	}
	return &SourceBalanceFilter{MaxPerSource: maxPerSource}
}

// Name returns "source-balance".
func (f *SourceBalanceFilter) Name() string {
	return "source-balance"
}

// Run limits items per source.
//
// Respects context cancellation - checks ctx every 1000 items.
func (f *SourceBalanceFilter) Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Track counts per source
	sourceCounts := make(map[string]int)
	filtered := make([]model.Item, 0, len(items))

	for i, item := range items {
		// Check context periodically
		if i > 0 && i%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		count := sourceCounts[item.SourceName]
		if count < f.MaxPerSource {
			filtered = append(filtered, item)
			sourceCounts[item.SourceName] = count + 1
		}
	}

	return filtered, nil
}

// SourceFilter includes only items from specific sources.
//
// This filter is stateless and safe for concurrent use.
type SourceFilter struct {
	Sources map[string]bool
}

// NewSourceFilter creates a filter for specific sources.
//
// Only items from the named sources will be included.
func NewSourceFilter(sources []string) *SourceFilter {
	m := make(map[string]bool, len(sources))
	for _, s := range sources {
		m[s] = true
	}
	return &SourceFilter{Sources: m}
}

// Name returns "source".
func (f *SourceFilter) Name() string {
	return "source"
}

// Run filters to only include items from the configured sources.
//
// Respects context cancellation.
func (f *SourceFilter) Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	filtered := make([]model.Item, 0, len(items))

	for i, item := range items {
		// Check context periodically
		if i > 0 && i%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		if f.Sources[item.SourceName] {
			filtered = append(filtered, item)
		}
	}

	return filtered, nil
}
