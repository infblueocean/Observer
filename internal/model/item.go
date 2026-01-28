// Package model provides the data layer for Observer v0.5.
//
// This package implements the Model layer of the MVC architecture.
// It is the source of truth for all application data, backed by SQLite.
//
// # Architecture Role
//
// The model layer provides:
//   - Persistent storage (SQLite)
//   - Data types (Item, Source, TimeBand)
//   - CRUD operations on items and sources
//   - Session tracking
//
// The model is "dumb" - it doesn't make decisions about what to display.
// That's the controller's job.
//
// # Thread Safety
//
// All Store methods are safe for concurrent use. The underlying sql.DB
// handles connection pooling and serialization.
//
// # Data Flow
//
//	Fetch → Store.SaveItems() → items persisted
//	Controller.Refresh() → Store.GetItemsSince() → items loaded
//	View → renders items
package model

import "time"

// SourceType identifies the origin of a feed item
type SourceType string

const (
	SourceRSS        SourceType = "rss"
	SourceReddit     SourceType = "reddit"
	SourceHN         SourceType = "hn"
	SourceUSGS       SourceType = "usgs"
	SourceMastodon   SourceType = "mastodon"
	SourceBluesky    SourceType = "bluesky"
	SourceArXiv      SourceType = "arxiv"
	SourceSEC        SourceType = "sec"
	SourceAggregator SourceType = "aggregator"
	SourcePolymarket SourceType = "polymarket"
	SourceManifold   SourceType = "manifold"
)

// Item represents a single piece of content from any source.
// This is the unified type that flows through the entire system.
type Item struct {
	ID         string
	Source     SourceType
	SourceName string    // "Hacker News", "r/golang", "BBC News"
	SourceURL  string    // Feed URL or subreddit URL
	Title      string
	Summary    string    // Brief description/excerpt
	Content    string    // Full content if available
	URL        string    // Link to original
	Author     string
	Published  time.Time
	Fetched    time.Time
	Read       bool
	Saved      bool

	// Embedding vector (for semantic operations)
	Embedding []float32
}

// Age returns how old this item is since publication.
func (i *Item) Age() time.Duration {
	return time.Since(i.Published)
}

// TimeBand returns which time band this item belongs to.
func (i *Item) TimeBand() TimeBand {
	age := i.Age()
	switch {
	case age < 15*time.Minute:
		return TimeBandJustNow
	case age < time.Hour:
		return TimeBandPastHour
	case age < 24*time.Hour:
		return TimeBandToday
	case age < 48*time.Hour:
		return TimeBandYesterday
	default:
		return TimeBandOlder
	}
}

// TimeBand represents a time grouping for UI display.
type TimeBand int

const (
	TimeBandJustNow TimeBand = iota
	TimeBandPastHour
	TimeBandToday
	TimeBandYesterday
	TimeBandOlder
)

// String returns the display name for a time band.
func (tb TimeBand) String() string {
	switch tb {
	case TimeBandJustNow:
		return "Just Now"
	case TimeBandPastHour:
		return "Past Hour"
	case TimeBandToday:
		return "Today"
	case TimeBandYesterday:
		return "Yesterday"
	default:
		return "Older"
	}
}
