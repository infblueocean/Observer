package fetch

import "github.com/abelbrown/observer/internal/model"

// SourceConfig represents a configured feed source.
type SourceConfig struct {
	Name           string
	URL            string
	Type           model.SourceType
	Category       string
	RefreshMinutes int
	Weight         float64
}

// DefaultSources returns a curated list of feed sources for the MVP.
// This is a subset of the full source list for initial testing.
func DefaultSources() []SourceConfig {
	return []SourceConfig{
		// Wire services (high signal, fast refresh)
		{Name: "AP News", URL: "https://feedx.net/rss/ap.xml", Type: model.SourceRSS, Category: "wire", RefreshMinutes: model.RefreshNormal, Weight: 1.5},
		{Name: "BBC World", URL: "https://feeds.bbci.co.uk/news/world/rss.xml", Type: model.SourceRSS, Category: "wire", RefreshMinutes: model.RefreshNormal, Weight: 1.3},
		{Name: "BBC Top", URL: "https://feeds.bbci.co.uk/news/rss.xml", Type: model.SourceRSS, Category: "wire", RefreshMinutes: model.RefreshNormal, Weight: 1.3},

		// Tech news (medium priority)
		{Name: "Hacker News", URL: "https://news.ycombinator.com/rss", Type: model.SourceRSS, Category: "tech", RefreshMinutes: model.RefreshFast, Weight: 1.3},
		{Name: "Lobsters", URL: "https://lobste.rs/rss", Type: model.SourceRSS, Category: "tech", RefreshMinutes: model.RefreshNormal, Weight: 1.2},
		{Name: "Ars Technica", URL: "https://feeds.arstechnica.com/arstechnica/index", Type: model.SourceRSS, Category: "tech", RefreshMinutes: model.RefreshSlow, Weight: 1.2},
		{Name: "The Verge", URL: "https://www.theverge.com/rss/index.xml", Type: model.SourceRSS, Category: "tech", RefreshMinutes: model.RefreshSlow, Weight: 1.1},

		// Aggregators
		{Name: "Techmeme", URL: "https://www.techmeme.com/feed.xml", Type: model.SourceRSS, Category: "aggregator", RefreshMinutes: model.RefreshNormal, Weight: 1.4},

		// Science
		{Name: "Nature", URL: "https://www.nature.com/nature.rss", Type: model.SourceRSS, Category: "science", RefreshMinutes: model.RefreshHourly, Weight: 1.4},
		{Name: "Quanta Magazine", URL: "https://api.quantamagazine.org/feed/", Type: model.SourceRSS, Category: "science", RefreshMinutes: model.RefreshHourly, Weight: 1.3},

		// Finance
		{Name: "Bloomberg", URL: "https://feeds.bloomberg.com/markets/news.rss", Type: model.SourceRSS, Category: "finance", RefreshMinutes: model.RefreshNormal, Weight: 1.3},

		// Security
		{Name: "Krebs on Security", URL: "https://krebsonsecurity.com/feed/", Type: model.SourceRSS, Category: "security", RefreshMinutes: model.RefreshHourly, Weight: 1.4},
	}
}

// CreateFetcher creates a Fetcher from a SourceConfig.
func CreateFetcher(cfg SourceConfig) Fetcher {
	switch cfg.Type {
	case model.SourceRSS:
		return NewRSSFetcher(cfg.Name, cfg.URL)
	default:
		// Default to RSS for now
		return NewRSSFetcher(cfg.Name, cfg.URL)
	}
}

// Categories returns all unique categories in the default sources.
func Categories() []string {
	return []string{
		"wire",
		"tech",
		"aggregator",
		"science",
		"finance",
		"security",
	}
}
