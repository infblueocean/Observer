// Package ui provides the Bubble Tea TUI for Observer.
package ui

import "github.com/abelbrown/observer/internal/store"

// ItemsLoaded is sent when items are fetched from the store.
type ItemsLoaded struct {
	Items []store.Item
	Err   error
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
