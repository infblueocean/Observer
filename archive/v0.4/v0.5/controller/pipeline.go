//go:build ignore

package controller

import (
	"context"
	"fmt"

	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// Pipeline runs a sequence of filters on items.
//
// Filters execute in order, with each filter receiving the output of the previous one.
// If any filter returns an error, the pipeline stops and returns that error.
//
// # Context Cancellation
//
// The pipeline checks for context cancellation between each filter stage.
// Individual filters are also expected to check ctx and return early if cancelled.
//
// # Thread Safety
//
// Pipeline is NOT safe for concurrent use. Each Refresh() call on a controller
// should complete before starting another. The controller is responsible for
// serializing access if needed.
type Pipeline struct {
	filters []Filter
}

// NewPipeline creates a new empty pipeline.
func NewPipeline() *Pipeline {
	return &Pipeline{}
}

// Add adds a filter to the pipeline. Returns the pipeline for chaining.
//
// Filters execute in the order they are added.
func (p *Pipeline) Add(f Filter) *Pipeline {
	p.filters = append(p.filters, f)
	return p
}

// Run executes all filters in sequence.
//
// Each filter receives the output of the previous filter. The pipeline stops
// and returns an error if any filter fails or the context is cancelled.
//
// Returns the final filtered items, or an error if any stage failed.
func (p *Pipeline) Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error) {
	current := items

	for _, filter := range p.filters {
		// Check context before each filter
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("pipeline cancelled before %s: %w", filter.Name(), err)
		}

		result, err := filter.Run(ctx, current, pool)
		if err != nil {
			return nil, fmt.Errorf("filter %s failed: %w", filter.Name(), err)
		}

		current = result
	}

	return current, nil
}

// Filters returns the filters in this pipeline.
func (p *Pipeline) Filters() []Filter {
	return p.filters
}

// Len returns the number of filters in the pipeline.
func (p *Pipeline) Len() int {
	return len(p.filters)
}
