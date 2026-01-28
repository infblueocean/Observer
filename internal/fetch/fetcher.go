// Package fetch provides feed fetching capabilities for Observer v0.5.
//
// This package handles retrieving content from various feed sources (RSS, Atom)
// and converting them to store.Item structs for persistence.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/abelbrown/observer/internal/store"
	"github.com/mmcdole/gofeed"
)

// Source represents a feed source configuration.
// NOTE: No Interval field - all sources fetched on the same global interval in v0.5.
type Source struct {
	Type string // "rss", "hn", "reddit"
	Name string // Display name
	URL  string // Feed URL
}

// Fetcher retrieves items from feed sources.
type Fetcher struct {
	client *http.Client
}

// NewFetcher creates a Fetcher with the given HTTP client timeout.
func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Fetch retrieves items from a source. Returns items and any error.
// Does NOT store items - caller decides what to do with them.
//
// The function respects context cancellation and will return early
// if the context is cancelled.
func (f *Fetcher) Fetch(ctx context.Context, src Source) ([]store.Item, error) {
	// Check context before starting
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set a user agent to be a good citizen
	req.Header.Set("User-Agent", "Observer/0.5 (https://github.com/abelbrown/observer)")

	// Perform the request
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feed: %w", err)
	}
	defer resp.Body.Close()

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	// Parse the feed
	parser := gofeed.NewParser()
	feed, err := parser.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse feed: %w", err)
	}

	// Convert feed items to store.Item
	now := time.Now()
	items := make([]store.Item, 0, len(feed.Items))

	for _, feedItem := range feed.Items {
		item := convertFeedItem(feedItem, src, now)
		items = append(items, item)
	}

	return items, nil
}

// convertFeedItem converts a gofeed.Item to a store.Item.
func convertFeedItem(feedItem *gofeed.Item, src Source, fetchTime time.Time) store.Item {
	// Generate deterministic ID
	id := generateID(feedItem)

	// Get published time, fallback to fetch time if not available
	published := fetchTime
	if feedItem.PublishedParsed != nil {
		published = *feedItem.PublishedParsed
	} else if feedItem.UpdatedParsed != nil {
		published = *feedItem.UpdatedParsed
	}

	// Get author
	author := ""
	if feedItem.Author != nil {
		author = feedItem.Author.Name
	}

	// Get summary - prefer Description, fallback to Content snippet
	summary := feedItem.Description
	if summary == "" && feedItem.Content != "" {
		// Truncate content to a reasonable summary length
		summary = truncate(feedItem.Content, 500)
	}

	return store.Item{
		ID:         id,
		SourceType: src.Type,
		SourceName: src.Name,
		Title:      feedItem.Title,
		Summary:    summary,
		URL:        feedItem.Link,
		Author:     author,
		Published:  published,
		Fetched:    fetchTime,
		Read:       false,
		Saved:      false,
	}
}

// generateID creates a deterministic ID for a feed item.
// Uses the GUID if available, otherwise hashes the URL.
func generateID(feedItem *gofeed.Item) string {
	// Prefer GUID if available
	if feedItem.GUID != "" {
		return hashString(feedItem.GUID)
	}

	// Fallback to URL
	if feedItem.Link != "" {
		return hashString(feedItem.Link)
	}

	// Last resort: hash title + published time
	key := feedItem.Title
	if feedItem.PublishedParsed != nil {
		key += feedItem.PublishedParsed.String()
	}
	return hashString(key)
}

// hashString creates a short hash of a string for use as an ID.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8]) // 16 character hex string
}

// truncate shortens a string to maxLen runes, adding "..." if truncated.
// Uses rune-aware slicing to avoid breaking UTF-8 characters.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
