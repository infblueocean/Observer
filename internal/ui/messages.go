// Package ui provides the Bubble Tea TUI for Observer.
package ui

import "github.com/abelbrown/observer/internal/store"

// ItemsLoaded is sent when items are fetched from the store.
type ItemsLoaded struct {
	Items      []store.Item
	Embeddings map[string][]float32
	Err        error
}

// ItemMarkedRead is sent when an item is marked as read.
type ItemMarkedRead struct {
	ID string
}

// FetchComplete is sent when background fetch finishes.
type FetchComplete struct {
	Source   string
	NewItems int
	Err      error
}

// RefreshTick triggers periodic refresh.
type RefreshTick struct{}

// QueryEmbedded is sent when a filter query has been embedded.
type QueryEmbedded struct {
	Query     string
	Embedding []float32
	QueryID   string // search correlation ID
	Err       error
}

// EntryReranked is sent when a single entry has been scored by the cross-encoder.
// Used for package-manager style progress feedback.
type EntryReranked struct {
	ItemID  string  // Item ID for lookup in rerankEntries
	Score   float32 // Relevance score in [0, 1]
	QueryID string  // search correlation ID
	Err     error
}

// RerankComplete is sent when batch reranking finishes (Jina API path).
// Contains all scores at once, unlike EntryReranked which arrives one at a time.
type RerankComplete struct {
	Query   string    // query that was reranked (for stale-check)
	Scores  []float32 // score per entry, indexed by rerankEntries position
	QueryID string    // search correlation ID
	Err     error
}

// SearchPoolLoaded is sent when the full item pool for search is ready.
type SearchPoolLoaded struct {
	Items      []store.Item
	Embeddings map[string][]float32
	QueryID    string // search correlation ID
	Err        error
}
