package app

import (
	"github.com/abelbrown/observer/internal/brain"
	"github.com/abelbrown/observer/internal/feeds"
)

// Messages for Bubble Tea

// ItemsLoadedMsg is sent when feed items have been fetched
type ItemsLoadedMsg struct {
	Items      []feeds.Item
	SourceName string
	Category   string
	Err        error
}

// RefreshMsg triggers a feed refresh
type RefreshMsg struct{}

// TickMsg is sent periodically for auto-refresh
type TickMsg struct{}

// BrainTrustAnalysisMsg is sent when a Brain Trust analysis completes
type BrainTrustAnalysisMsg struct {
	ItemID    string
	ItemTitle string // Stored at request time to survive feed updates
	Analysis  brain.Analysis
}

// BrainTrustStreamChunkMsg is sent for each chunk during streaming analysis
type BrainTrustStreamChunkMsg struct {
	ItemID  string
	Content string // Incremental content to append
	Done    bool   // True when stream is complete
	Model   string // Model name
	Error   error
}

// BrainTrustStreamStartMsg is sent when streaming begins, carries the channel
type BrainTrustStreamStartMsg struct {
	ItemID       string
	ItemTitle    string
	ProviderName string // Name of the model/provider being used
	Chunks       <-chan brain.StreamChunk
}

// TopStoriesMsg is sent when AI top stories analysis completes
type TopStoriesMsg struct {
	Stories []brain.TopStoryResult
	Err     error
}

// CorrelationProcessedMsg is sent when correlation processing completes for items
type CorrelationProcessedMsg struct {
	ItemCount      int
	DuplicateCount int
	ClusterCount   int
}

// ShowBriefingMsg is sent on startup if user needs a briefing
type ShowBriefingMsg struct{}

// streamTickMsg is used to add a render delay between streaming chunks
type streamTickMsg struct {
	itemID string
}
