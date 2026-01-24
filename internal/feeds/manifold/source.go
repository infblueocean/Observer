package manifold

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

const (
	apiBase = "https://api.manifold.markets"
)

// Market represents a Manifold prediction market
type Market struct {
	ID               string    `json:"id"`
	Question         string    `json:"question"`
	Slug             string    `json:"slug"`
	URL              string    `json:"url"`
	Pool             Pool      `json:"pool"`
	Probability      float64   `json:"probability"`
	Volume           float64   `json:"volume"`
	Volume24Hours    float64   `json:"volume24Hours"`
	IsResolved       bool      `json:"isResolved"`
	CreatedTime      int64     `json:"createdTime"`
	CloseTime        int64     `json:"closeTime"`
	CreatorUsername  string    `json:"creatorUsername"`
	CreatorName      string    `json:"creatorName"`
	TextDescription  string    `json:"textDescription"`
	UniqueTraderCount int      `json:"uniqueBettorCount"`
}

// Pool represents the liquidity pool
type Pool struct {
	YES float64 `json:"YES"`
	NO  float64 `json:"NO"`
}

// Source fetches prediction markets from Manifold
type Source struct {
	name   string
	client *http.Client
}

// New creates a new Manifold source
func New() *Source {
	return &Source{
		name: "Manifold",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *Source) Name() string {
	return s.name
}

func (s *Source) Type() feeds.SourceType {
	return feeds.SourceManifold
}

func (s *Source) Fetch() ([]feeds.Item, error) {
	// Fetch markets sorted by 24h volume
	url := fmt.Sprintf("%s/v0/search-markets?limit=50&sort=24-hour-vol&filter=open", apiBase)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifold: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifold API error: %d", resp.StatusCode)
	}

	var markets []Market
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil, fmt.Errorf("failed to decode manifold response: %w", err)
	}

	items := make([]feeds.Item, 0, len(markets))
	now := time.Now()

	for _, m := range markets {
		if m.Question == "" || m.IsResolved {
			continue
		}

		// Format summary with probability and stats
		summary := fmt.Sprintf("%.0f%% YES", m.Probability*100)
		if m.UniqueTraderCount > 0 {
			summary += fmt.Sprintf(" · %d traders", m.UniqueTraderCount)
		}
		if m.Volume24Hours > 0 {
			summary += fmt.Sprintf(" · M$%.0f 24h", m.Volume24Hours)
		}
		if m.TextDescription != "" {
			desc := m.TextDescription
			if len(desc) > 150 {
				desc = desc[:150] + "..."
			}
			summary += "\n" + desc
		}

		// Parse created time
		createdAt := time.UnixMilli(m.CreatedTime)

		marketURL := m.URL
		if marketURL == "" {
			marketURL = fmt.Sprintf("https://manifold.markets/%s/%s", m.CreatorUsername, m.Slug)
		}

		items = append(items, feeds.Item{
			ID:         fmt.Sprintf("manifold-%s", m.ID),
			Source:     feeds.SourceManifold,
			SourceName: s.name,
			SourceURL:  marketURL,
			Title:      fmt.Sprintf("📈 %s", m.Question),
			Summary:    summary,
			URL:        marketURL,
			Author:     m.CreatorName,
			Published:  createdAt,
			Fetched:    now,
		})
	}

	return items, nil
}
