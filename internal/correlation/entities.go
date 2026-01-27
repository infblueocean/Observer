package correlation

import (
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

// EntityResult is the output of entity extraction.
type EntityResult struct {
	ItemID   string
	Item     *feeds.Item
	Entities []Entity
}

// Entity represents an extracted entity from text.
type Entity struct {
	ID       string    // normalized: "ticker:AAPL", "country:usa", "source:reuters"
	Name     string    // display: "$AAPL", "United States", "Reuters"
	Type     string    // "ticker", "country", "person", "org", "source"
	Salience float64   // 0.0-1.0
	Mentions int       // mention count (for top entities)
	LastSeen time.Time // last seen timestamp
}

// NewEntityWorker creates a worker pool for entity extraction.
// Uses cheap regex-based extraction (no LLM).
func NewEntityWorker(workers, buffer int) *Worker[*feeds.Item, *EntityResult] {
	return NewWorker("entities", workers, buffer, extractEntities)
}

// extractEntities performs cheap regex-based entity extraction.
// Budget: <5ms per item.
func extractEntities(item *feeds.Item) *EntityResult {
	var entities []Entity
	text := item.Title + " " + item.Summary

	// Tickers ($AAPL) - high salience
	for _, t := range ExtractTickers(text) {
		entities = append(entities, Entity{
			ID:       "ticker:" + t,
			Name:     "$" + t,
			Type:     "ticker",
			Salience: 0.9,
		})
	}

	// Countries - medium salience
	for _, c := range ExtractCountries(text) {
		entities = append(entities, Entity{
			ID:       "country:" + c,
			Name:     formatCountryName(c),
			Type:     "country",
			Salience: 0.7,
		})
	}

	// Source attributions - lower salience
	if attr := ExtractSourceAttribution(text); attr != nil && attr.OriginalSource != "" {
		entities = append(entities, Entity{
			ID:       "source:" + strings.ToLower(attr.OriginalSource),
			Name:     attr.OriginalSource,
			Type:     "source",
			Salience: 0.5,
		})
	}

	return &EntityResult{
		ItemID:   item.ID,
		Item:     item,
		Entities: entities,
	}
}

// formatCountryName converts normalized country ID to display name.
func formatCountryName(normalized string) string {
	names := map[string]string{
		"united_states":  "United States",
		"united_kingdom": "United Kingdom",
		"north_korea":    "North Korea",
		"south_korea":    "South Korea",
		"south_africa":   "South Africa",
		"saudi_arabia":   "Saudi Arabia",
		"hong_kong":      "Hong Kong",
		"european_union": "European Union",
	}
	if name, ok := names[normalized]; ok {
		return name
	}
	// Capitalize first letter
	if len(normalized) > 0 {
		return strings.ToUpper(normalized[:1]) + normalized[1:]
	}
	return normalized
}
