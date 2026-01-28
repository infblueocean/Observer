// Package fetch provides feed fetching capabilities for Observer v0.5.
//
// This package handles retrieving items from external sources (RSS feeds, APIs, etc.)
// and converting them to the unified model.Item type.
//
// # Supported Sources
//
// Currently supported:
//   - RSS/Atom feeds via [RSSFetcher]
//
// Planned:
//   - Hacker News API
//   - Reddit (via public RSS)
//   - USGS Earthquake data
//
// # Usage
//
//	cfg := fetch.SourceConfig{
//	    Name: "Example Feed",
//	    URL:  "https://example.com/feed.xml",
//	    Type: model.SourceRSS,
//	}
//	fetcher := fetch.CreateFetcher(cfg)
//	items, err := fetcher.Fetch()
//
// # Item IDs
//
// Item IDs are generated deterministically from the item URL using a truncated
// SHA256 hash. This ensures:
//   - Same URL always generates same ID
//   - IDs are stable across fetches
//   - Duplicate detection works correctly
//
// # Error Handling
//
// Fetchers return errors for:
//   - Network failures
//   - HTTP errors (non-200 status)
//   - Parse errors (invalid XML/JSON)
//
// Transient errors should be retried by the caller (FetchController handles this).
package fetch

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/abelbrown/observer/internal/model"
	"github.com/mmcdole/gofeed"
)

// Fetcher can retrieve items from a feed source.
type Fetcher interface {
	// Name returns the source name
	Name() string

	// Fetch retrieves items from the source
	Fetch() ([]model.Item, error)
}

// RSSFetcher fetches items from RSS/Atom feeds.
type RSSFetcher struct {
	name   string
	url    string
	parser *gofeed.Parser
	client *http.Client
}

// UserAgent is the User-Agent string used for RSS fetches.
// Using a browser-like UA because some sites block generic bots.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// NewRSSFetcher creates a new RSS fetcher.
func NewRSSFetcher(name, url string) *RSSFetcher {
	return &RSSFetcher{
		name:   name,
		url:    url,
		parser: gofeed.NewParser(),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (f *RSSFetcher) Name() string {
	return f.name
}

func (f *RSSFetcher) Fetch() ([]model.Item, error) {
	req, err := http.NewRequest("GET", f.url, nil)
	if err != nil {
		return nil, fmt.Errorf("request error fetching %s: %w", f.name, err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error fetching %s: %w", f.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, f.name)
	}

	feed, err := f.parser.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", f.name, err)
	}

	items := make([]model.Item, 0, len(feed.Items))
	now := time.Now()

	for _, entry := range feed.Items {
		// Generate stable ID from URL
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(entry.Link)))[:16]

		// Parse published time
		published := now
		if entry.PublishedParsed != nil {
			published = *entry.PublishedParsed
		} else if entry.UpdatedParsed != nil {
			published = *entry.UpdatedParsed
		}

		// Get summary
		summary := entry.Description
		if summary == "" && entry.Content != "" {
			summary = truncate(entry.Content, 200)
		}

		author := ""
		if entry.Author != nil {
			author = entry.Author.Name
		}

		items = append(items, model.Item{
			ID:         id,
			Source:     model.SourceRSS,
			SourceName: f.name,
			SourceURL:  f.url,
			Title:      entry.Title,
			Summary:    summary,
			Content:    entry.Content,
			URL:        entry.Link,
			Author:     author,
			Published:  published,
			Fetched:    now,
		})
	}

	return items, nil
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
