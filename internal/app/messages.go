package app

import (
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
