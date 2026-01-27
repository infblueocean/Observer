package rss

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/logging"
	"github.com/mmcdole/gofeed"
)

// Source fetches items from an RSS/Atom feed
type Source struct {
	name   string
	url    string
	parser *gofeed.Parser
	client *http.Client
}

// UserAgent is the User-Agent string used for RSS fetches
// Using a browser-like UA because some sites (Reddit, etc.) block generic bots
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// New creates a new RSS source
func New(name, url string) *Source {
	return &Source{
		name:   name,
		url:    url,
		parser: gofeed.NewParser(),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *Source) Name() string {
	return s.name
}

func (s *Source) Type() feeds.SourceType {
	return feeds.SourceRSS
}

func (s *Source) Fetch() ([]feeds.Item, error) {
	// Create request with proper User-Agent (some sites block generic bots)
	req, err := http.NewRequest("GET", s.url, nil)
	if err != nil {
		logging.Error("RSS fetch request error", "source", s.name, "url", s.url, "error", err)
		return nil, fmt.Errorf("request error fetching %s: %w", s.name, err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := s.client.Do(req)
	if err != nil {
		logging.Error("RSS fetch network error", "source", s.name, "url", s.url, "error", err)
		return nil, fmt.Errorf("network error fetching %s: %w", s.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logging.Error("RSS fetch HTTP error", "source", s.name, "url", s.url, "status", resp.StatusCode)
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, s.name)
	}

	feed, err := s.parser.Parse(resp.Body)
	if err != nil {
		logging.Error("RSS parse error", "source", s.name, "url", s.url, "error", err)
		return nil, fmt.Errorf("failed to parse %s: %w", s.name, err)
	}

	items := make([]feeds.Item, 0, len(feed.Items))
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
			// Truncate content for summary
			summary = truncate(entry.Content, 200)
		}

		author := ""
		if entry.Author != nil {
			author = entry.Author.Name
		}

		items = append(items, feeds.Item{
			ID:         id,
			Source:     feeds.SourceRSS,
			SourceName: s.name,
			SourceURL:  s.url,
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
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
