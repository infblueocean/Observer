// Package controllers provides built-in controller implementations.
package controllers

import (
	"context"
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/controller"
	"github.com/abelbrown/observer/internal/controller/filters"
	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// MainFeedController manages the main stream view.
//
// It applies time, dedup, and source balance filters to items from the store.
//
// # Thread Safety
//
// MainFeedController is safe for concurrent use. Multiple goroutines can call
// Refresh() concurrently, but only one refresh runs at a time (subsequent calls
// wait for the current refresh to complete).
//
// # Event Channel
//
// Subscribe() returns a buffered channel that receives events. The channel has
// a buffer of 10 events. If the subscriber doesn't consume events fast enough,
// old events will be dropped (non-blocking send).
type MainFeedController struct {
	id       string
	pipeline *controller.Pipeline
	events   chan controller.Event

	// Config (protected by mu)
	mu           sync.RWMutex
	maxAge       time.Duration
	maxPerSource int

	// Refresh serialization
	refreshMu sync.Mutex
}

// MainFeedConfig configures the main feed controller.
type MainFeedConfig struct {
	MaxAge       time.Duration // Items older than this are excluded (default: 6h)
	MaxPerSource int           // Max items per source (default: 10)
}

// DefaultMainFeedConfig returns sensible defaults.
func DefaultMainFeedConfig() MainFeedConfig {
	return MainFeedConfig{
		MaxAge:       6 * time.Hour,
		MaxPerSource: 10,
	}
}

// NewMainFeedController creates the main feed controller.
func NewMainFeedController(cfg MainFeedConfig) *MainFeedController {
	// Validate config
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 6 * time.Hour
	}
	if cfg.MaxPerSource <= 0 {
		cfg.MaxPerSource = 10
	}

	c := &MainFeedController{
		id:           "main-feed",
		events:       make(chan controller.Event, 10),
		maxAge:       cfg.MaxAge,
		maxPerSource: cfg.MaxPerSource,
	}

	c.rebuildPipeline()
	return c
}

// ID returns "main-feed".
func (c *MainFeedController) ID() string {
	return c.id
}

// Refresh runs the filter pipeline on items from the store.
//
// This method blocks until the refresh completes or the context is cancelled.
// Events are sent to subscribers as the refresh progresses.
//
// Only one refresh runs at a time. If called while a refresh is in progress,
// this call blocks until the previous refresh completes, then starts a new one.
func (c *MainFeedController) Refresh(ctx context.Context, store *model.Store, pool *work.Pool) {
	// Serialize refreshes - only one at a time
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	// Check if already cancelled
	if ctx.Err() != nil {
		c.sendEvent(controller.Event{Type: controller.EventError, Err: ctx.Err()})
		return
	}

	// Signal start
	c.sendEvent(controller.Event{Type: controller.EventStarted})

	// Get current config (thread-safe read)
	c.mu.RLock()
	maxAge := c.maxAge
	pipeline := c.pipeline
	c.mu.RUnlock()

	// Get items from the time window (using SQL for efficiency)
	cutoff := time.Now().Add(-maxAge)
	items, err := store.GetItemsSince(cutoff)
	if err != nil {
		c.sendEvent(controller.Event{Type: controller.EventError, Err: err})
		return
	}

	// If no items, still send completed
	if len(items) == 0 {
		c.sendEvent(controller.Event{Type: controller.EventCompleted, Items: items})
		return
	}

	// Run through pipeline (respects context cancellation)
	filtered, err := pipeline.Run(ctx, items, pool)
	if err != nil {
		c.sendEvent(controller.Event{Type: controller.EventError, Err: err})
		return
	}

	c.sendEvent(controller.Event{Type: controller.EventCompleted, Items: filtered})
}

// sendEvent sends an event to subscribers without blocking.
// If the channel is full, the event is dropped.
func (c *MainFeedController) sendEvent(event controller.Event) {
	select {
	case c.events <- event:
	default:
		// Channel full, drop event (subscriber not keeping up)
	}
}

// Subscribe returns the event channel.
//
// The returned channel has a buffer of 10 events. Subscribers should consume
// events promptly to avoid dropped events.
//
// The channel is never closed - it lives for the lifetime of the controller.
func (c *MainFeedController) Subscribe() <-chan controller.Event {
	return c.events
}

// SetMaxAge updates the time filter max age.
//
// Must be positive. Values <= 0 are ignored.
// The new value takes effect on the next Refresh() call.
func (c *MainFeedController) SetMaxAge(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	c.maxAge = d
	c.rebuildPipelineLocked()
	c.mu.Unlock()
}

// SetMaxPerSource updates the source balance limit.
//
// Must be positive. Values <= 0 are ignored.
// The new value takes effect on the next Refresh() call.
func (c *MainFeedController) SetMaxPerSource(n int) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	c.maxPerSource = n
	c.rebuildPipelineLocked()
	c.mu.Unlock()
}

func (c *MainFeedController) rebuildPipeline() {
	c.mu.Lock()
	c.rebuildPipelineLocked()
	c.mu.Unlock()
}

// rebuildPipelineLocked rebuilds the pipeline. Caller must hold c.mu.
func (c *MainFeedController) rebuildPipelineLocked() {
	c.pipeline = controller.NewPipeline().
		Add(filters.NewTimeFilter(c.maxAge)).
		Add(filters.NewDedupFilter()).
		Add(filters.NewSourceBalanceFilter(c.maxPerSource))
}
