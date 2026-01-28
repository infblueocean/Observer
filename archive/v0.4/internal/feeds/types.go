package feeds

import "time"

// SourceType identifies the origin of a feed item
type SourceType string

const (
	SourceRSS        SourceType = "rss"
	SourceReddit     SourceType = "reddit"    // Public subreddit RSS
	SourceHN         SourceType = "hn"        // Hacker News API
	SourceTwitter    SourceType = "twitter"   // (Deprecated - requires auth)
	SourceUSGS       SourceType = "usgs"      // Earthquake data
	SourceMastodon   SourceType = "mastodon"  // Public timeline RSS
	SourceBluesky    SourceType = "bluesky"   // Native RSS feeds
	SourceArXiv      SourceType = "arxiv"     // Academic preprints
	SourceSEC        SourceType = "sec"       // SEC EDGAR filings
	SourceAggregator SourceType = "aggregator" // Third-party aggregators
	SourcePolymarket SourceType = "polymarket" // Prediction market
	SourceManifold   SourceType = "manifold"   // Prediction market
)

// Item represents a single piece of content from any source
// This is the unified type that flows through the stream
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
	Read       bool      // User has seen this
	Saved      bool      // User bookmarked this
}

// Source is the interface all feed sources implement
type Source interface {
	// Name returns human-readable source name
	Name() string

	// Type returns the source type
	Type() SourceType

	// Fetch retrieves latest items from this source
	Fetch() ([]Item, error)
}
