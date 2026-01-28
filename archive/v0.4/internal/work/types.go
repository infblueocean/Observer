// Package work provides a unified system for async work processing.
//
// All async operations (fetching, dedup, reranking, analysis) flow through
// a central work pool, making the system observable and debuggable.
//
// # Priority System
//
// Work items have a Priority field that controls execution order. Higher priority
// items are executed before lower priority items. Use the predefined constants:
//
//	PriorityBackground - Batch operations (embeds, cleanup)
//	PriorityLow        - Deferred work
//	PriorityNormal     - Standard operations (default)
//	PriorityHigh       - Important operations (breaking news)
//	PriorityUrgent     - User-initiated actions
//	PriorityCritical   - System-critical operations
//
// Press /w to see the work queue in action.
//
// Logging: All state changes are logged via internal/logging for debugging
// since the UI may not be visible during development.
package work

import (
	"fmt"
	"time"

	"github.com/abelbrown/observer/internal/logging"
)

// Priority levels for work items.
// Higher values = more urgent, executed first.
const (
	PriorityBackground = -10 // Batch work: embeds, dedup, cleanup
	PriorityLow        = 0   // Deferred: non-urgent background tasks
	PriorityNormal     = 10  // Standard: scheduled fetches (default)
	PriorityHigh       = 50  // Important: breaking news sources
	PriorityUrgent     = 100 // User-initiated: manual refresh, analysis
	PriorityCritical   = 200 // System-critical: shutdown tasks
)

// LogEvent logs a work event for debugging.
func LogEvent(event Event) {
	item := event.Item
	switch event.Change {
	case "created":
		logging.Info("Work created",
			"id", item.ID,
			"type", item.Type,
			"priority", item.Priority,
			"desc", item.Description)
	case "started":
		logging.Info("Work started",
			"id", item.ID,
			"type", item.Type,
			"desc", item.Description)
	case "progress":
		logging.Debug("Work progress",
			"id", item.ID,
			"pct", fmt.Sprintf("%.0f%%", item.Progress*100),
			"msg", item.ProgressMsg)
	case "completed":
		logging.Info("Work completed",
			"id", item.ID,
			"type", item.Type,
			"desc", item.Description,
			"result", item.Result,
			"duration", item.Duration())
	case "failed":
		logging.Error("Work failed",
			"id", item.ID,
			"type", item.Type,
			"desc", item.Description,
			"error", item.Error,
			"duration", item.Duration())
	}
}

// Type categorizes work items for filtering and display.
type Type string

const (
	TypeFetch   Type = "fetch"   // Fetching RSS/API sources
	TypeDedup   Type = "dedup"   // Duplicate detection
	TypeEmbed   Type = "embed"   // Embedding generation
	TypeRerank  Type = "rerank"  // ML reranking
	TypeFilter  Type = "filter"  // Pattern/semantic filtering
	TypeAnalyze Type = "analyze" // AI analysis
	TypeIntake  Type = "intake"  // Intake pipeline processing
	TypeOther   Type = "other"   // Catch-all
)

// TypeIcon returns a display icon for the work type.
func (t Type) Icon() string {
	switch t {
	case TypeFetch:
		return "↓"
	case TypeDedup:
		return "◇"
	case TypeEmbed:
		return "◈"
	case TypeRerank:
		return "▲"
	case TypeFilter:
		return "◌"
	case TypeAnalyze:
		return "◉"
	case TypeIntake:
		return "⇒"
	default:
		return "○"
	}
}

// Status represents the lifecycle state of a work item.
type Status string

const (
	StatusPending  Status = "pending"  // Queued, waiting for worker
	StatusActive   Status = "active"   // Currently being processed
	StatusComplete Status = "complete" // Finished successfully
	StatusFailed   Status = "failed"   // Finished with error
)

// Item represents a unit of async work.
type Item struct {
	ID          string
	Type        Type
	Status      Status
	Description string // Human-readable: "Fetching Reuters"

	// Timing
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time

	// Progress (for long-running work)
	Progress    float64 // 0.0 to 1.0
	ProgressMsg string  // "1,234 of 7,234"

	// Result
	Result string // "12 new items", "blocked 3"
	Error  error
	Data   any // Arbitrary result data (e.g., []feeds.Item for fetch)

	// Context
	Source   string // Source name, item ID, or other context
	Category string // Category for filtering (e.g., feed category)
	Priority int    // Higher = more urgent (default PriorityNormal)

	// Internal: the work function to execute
	workFn func() (string, error)

	// Internal: progress callback for long-running work
	progressFn func(pct float64, msg string)

	// Internal: heap index for priority queue
	heapIndex int
}

// Duration returns how long the work took (or has been running).
func (i *Item) Duration() time.Duration {
	if i.FinishedAt.IsZero() {
		if i.StartedAt.IsZero() {
			return 0
		}
		return time.Since(i.StartedAt)
	}
	return i.FinishedAt.Sub(i.StartedAt)
}

// Age returns how long since the work completed.
func (i *Item) Age() time.Duration {
	if i.FinishedAt.IsZero() {
		return 0
	}
	return time.Since(i.FinishedAt)
}

// StatusIcon returns a display icon for the current status.
func (i *Item) StatusIcon() string {
	switch i.Status {
	case StatusPending:
		return "○"
	case StatusActive:
		return "●"
	case StatusComplete:
		return "✓"
	case StatusFailed:
		return "✗"
	default:
		return "?"
	}
}

// PriorityName returns a human-readable name for the priority level.
func (i *Item) PriorityName() string {
	switch {
	case i.Priority >= PriorityCritical:
		return "critical"
	case i.Priority >= PriorityUrgent:
		return "urgent"
	case i.Priority >= PriorityHigh:
		return "high"
	case i.Priority >= PriorityNormal:
		return "normal"
	case i.Priority >= PriorityLow:
		return "low"
	default:
		return "background"
	}
}

// Event is sent to subscribers when work state changes.
type Event struct {
	Item   *Item
	Change string // "created", "started", "progress", "completed", "failed"
}

// Snapshot represents the current state of the work pool.
type Snapshot struct {
	Pending   []*Item
	Active    []*Item
	Completed []*Item // Recent completed (success + failure), newest first
	Stats     Stats
}

// Stats tracks work pool metrics.
type Stats struct {
	TotalCreated   int64
	TotalCompleted int64
	TotalFailed    int64
	WorkersActive  int
	WorkersTotal   int
	PendingCount   int
}

// String returns a summary string for stats.
func (s Stats) String() string {
	return fmt.Sprintf("Active: %d  Pending: %d  Done: %d  Failed: %d",
		s.WorkersActive, s.PendingCount, s.TotalCompleted, s.TotalFailed)
}
