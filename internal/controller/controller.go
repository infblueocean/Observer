// Package controller implements the Controller layer of Observer's MVC architecture.
//
// Controllers sit between Model (data) and View (UI), deciding what data flows through.
// Each view has its own controller that runs a filter pipeline to transform items.
//
// # Architecture
//
//	┌─────────┐     ┌────────────┐     ┌──────┐
//	│  Model  │ ──> │ Controller │ ──> │ View │
//	│ (Store) │     │ (Pipeline) │     │ (UI) │
//	└─────────┘     └────────────┘     └──────┘
//
// # Controllers
//
// Each view has a dedicated controller:
//   - MainFeedController: Manages the main stream view
//   - FetchController: Manages periodic feed fetching
//
// Controllers communicate with views via event channels. When a Refresh()
// completes, the controller sends a EventCompleted with the filtered items.
//
// # Filter Pipeline
//
// Controllers use filter pipelines to transform items:
//
//	items := store.GetItemsSince(cutoff)
//	filtered := pipeline.Run(ctx, items, pool)
//	// filtered items sent to view
//
// Each filter in the pipeline receives the output of the previous filter.
// Filters are composable, testable, and respect context cancellation.
//
// # Concurrency
//
// Controllers are safe for concurrent use. Multiple goroutines can call
// Refresh() concurrently - the controller serializes these calls internally.
//
// Event channels have buffers to prevent blocking. If a subscriber doesn't
// consume events fast enough, old events are dropped.
package controller

import (
	"context"

	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// Controller manages a filter pipeline for a specific view.
// Each view has its own controller that decides what items to display.
type Controller interface {
	// ID uniquely identifies this controller.
	ID() string

	// Refresh triggers the controller to re-run its pipeline.
	// Results come back via the Subscribe() channel.
	Refresh(ctx context.Context, store *model.Store, pool *work.Pool)

	// Subscribe returns a channel of controller events.
	Subscribe() <-chan Event
}

// EventType categorizes controller events.
type EventType string

const (
	EventStarted   EventType = "started"
	EventProgress  EventType = "progress"
	EventCompleted EventType = "completed"
	EventError     EventType = "error"
)

// Event is sent to subscribers when controller state changes.
type Event struct {
	Type       EventType
	Items      []model.Item // Populated on EventCompleted
	Err        error        // Populated on EventError
	FilterName string       // Current filter (for progress)
	Progress   float64      // 0.0 to 1.0 (for progress)
}
