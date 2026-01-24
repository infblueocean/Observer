package rss

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/mmcdole/gofeed"
)

// Source fetches items from an RSS/Atom feed
type Source struct {
	name   string
	url    string
	parser *gofeed.Parser
}

// New creates a new RSS source
func New(name, url string) *Source {
	return &Source{
		name:   name,
		url:    url,
		parser: gofeed.NewParser(),
	}
}

func (s *Source) Name() string {
	return s.name
}

func (s *Source) Type() feeds.SourceType {
	return feeds.SourceRSS
}

func (s *Source) Fetch() ([]feeds.Item, error) {
	feed, err := s.parser.ParseURL(s.url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", s.url, err)
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
