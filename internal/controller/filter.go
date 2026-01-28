// Package controller provides the filter pipeline architecture for Observer v0.5.
//
// Controllers sit between Model (SQLite) and View (UI), deciding what data flows through.
// Each view has its own controller that runs a filter pipeline to transform items.
//
// # Concurrency
//
// Filters may be called concurrently from different goroutines. Implementations must
// be safe for concurrent use. The pipeline itself is not safe for concurrent use -
// each Refresh() call should complete before the next one starts.
//
// # Context Cancellation
//
// All filter operations respect context cancellation. When ctx is cancelled,
// filters should return promptly with ctx.Err().
package controller

import (
	"context"

	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// Filter transforms a set of items.
//
// Filters are the building blocks of a pipeline. They receive items, transform them
// (filter, sort, deduplicate, etc.), and return the result.
//
// # Implementation Guidelines
//
//   - Filters MUST respect context cancellation and return ctx.Err() when cancelled
//   - Filters MUST be safe for concurrent use (stateless or properly synchronized)
//   - Filters SHOULD NOT modify the input slice; create a new slice for output
//   - Filters MAY use the work pool for expensive async operations
//
// # Sync vs Async
//
// Most filters are synchronous - they process items and return immediately.
// Use [SyncFilter] for these cases.
//
// Some filters need async work (e.g., calling external services). These should
// submit work to the pool and block until completion, respecting ctx cancellation.
type Filter interface {
	// Name returns the filter name for logging and debugging.
	Name() string

	// Run executes the filter on the given items.
	//
	// The context should be checked periodically for cancellation.
	// The pool may be nil if no async work is needed.
	//
	// Returns the filtered items or an error. On context cancellation,
	// returns nil and ctx.Err().
	Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error)
}

// FilterResult contains the output of a filter operation.
// Kept for backward compatibility but Run() now returns directly.
type FilterResult struct {
	Items []model.Item
	Err   error
}

// SyncFilter is a helper for filters that don't need async work or the work pool.
//
// Example:
//
//	filter := NewSyncFilter("my-filter", func(ctx context.Context, items []model.Item) ([]model.Item, error) {
//	    var result []model.Item
//	    for _, item := range items {
//	        if ctx.Err() != nil {
//	            return nil, ctx.Err()
//	        }
//	        if shouldKeep(item) {
//	            result = append(result, item)
//	        }
//	    }
//	    return result, nil
//	})
type SyncFilter struct {
	name string
	fn   func(ctx context.Context, items []model.Item) ([]model.Item, error)
}

// NewSyncFilter creates a synchronous filter from a function.
//
// The function receives a context that should be checked for cancellation,
// especially in loops over large item sets.
func NewSyncFilter(name string, fn func(ctx context.Context, items []model.Item) ([]model.Item, error)) *SyncFilter {
	return &SyncFilter{name: name, fn: fn}
}

// Name returns the filter name.
func (f *SyncFilter) Name() string {
	return f.name
}

// Run executes the filter synchronously.
func (f *SyncFilter) Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error) {
	// Check context before starting
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return f.fn(ctx, items)
}
