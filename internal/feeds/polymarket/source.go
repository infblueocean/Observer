package polymarket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

const (
	gammaAPI = "https://gamma-api.polymarket.com"
)

// Market represents a Polymarket prediction market
type Market struct {
	ID                 string      `json:"id"`
	Question           string      `json:"question"`
	Description        string      `json:"description"`
	Slug               string      `json:"slug"`
	Active             bool        `json:"active"`
	Closed             bool        `json:"closed"`
	Volume             json.Number `json:"volume"`      // API returns string or number
	Volume24hr         json.Number `json:"volume24hr"`  // API returns string or number
	OutcomePrices      string      `json:"outcomePrices"` // JSON string "[0.65, 0.35]"
	Outcomes           string      `json:"outcomes"`       // JSON string "[\"Yes\", \"No\"]"
	CreatedAt          time.Time   `json:"createdAt"`
	EndDate            time.Time   `json:"endDate"`
	Category           string      `json:"category"`
	Liquidity          json.Number `json:"liquidity"` // API returns string or number
}

// MarketResponse is the API response
type MarketResponse []Market

// Source fetches prediction markets from Polymarket
type Source struct {
	name   string
	client *http.Client
}

// New creates a new Polymarket source
func New() *Source {
	return &Source{
		name: "Polymarket",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *Source) Name() string {
	return s.name
}

func (s *Source) Type() feeds.SourceType {
	return feeds.SourcePolymarket
}

func (s *Source) Fetch() ([]feeds.Item, error) {
	// Fetch active markets sorted by volume
	url := fmt.Sprintf("%s/markets?active=true&closed=false&limit=50&order=volume24hr&ascending=false", gammaAPI)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch polymarket: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("polymarket API error: %d", resp.StatusCode)
	}

	var markets MarketResponse
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil, fmt.Errorf("failed to decode polymarket response: %w", err)
	}

	// Sort by 24h volume
	sort.Slice(markets, func(i, j int) bool {
		vi, _ := markets[i].Volume24hr.Float64()
		vj, _ := markets[j].Volume24hr.Float64()
		return vi > vj
	})

	items := make([]feeds.Item, 0, len(markets))
	now := time.Now()

	for _, m := range markets {
		if m.Question == "" {
			continue
		}

		// Parse outcome prices
		prob := parseFirstPrice(m.OutcomePrices)

		// Format summary with probability and volume
		summary := fmt.Sprintf("%.0f%% YES", prob*100)
		vol24hr, _ := m.Volume24hr.Float64()
		if vol24hr > 0 {
			summary += fmt.Sprintf(" · $%.0fK 24h volume", vol24hr/1000)
		}
		if m.Description != "" {
			desc := m.Description
			if len(desc) > 150 {
				desc = desc[:150] + "..."
			}
			summary += "\n" + desc
		}

		items = append(items, feeds.Item{
			ID:         fmt.Sprintf("poly-%s", m.ID),
			Source:     feeds.SourcePolymarket,
			SourceName: s.name,
			SourceURL:  fmt.Sprintf("https://polymarket.com/event/%s", m.Slug),
			Title:      fmt.Sprintf("📊 %s", m.Question),
			Summary:    summary,
			URL:        fmt.Sprintf("https://polymarket.com/event/%s", m.Slug),
			Published:  m.CreatedAt,
			Fetched:    now,
		})
	}

	return items, nil
}

func parseFirstPrice(pricesJSON string) float64 {
	// Try parsing as []float64 first
	var floatPrices []float64
	if err := json.Unmarshal([]byte(pricesJSON), &floatPrices); err == nil && len(floatPrices) > 0 {
		return floatPrices[0]
	}

	// Try parsing as []string (API sometimes returns string values)
	var stringPrices []string
	if err := json.Unmarshal([]byte(pricesJSON), &stringPrices); err == nil && len(stringPrices) > 0 {
		var price float64
		if _, err := fmt.Sscanf(stringPrices[0], "%f", &price); err == nil {
			return price
		}
	}

	return 0.5
}
