package usgs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/httpclient"
)

const (
	// USGS GeoJSON feeds - different time windows and magnitudes
	allHour     = "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/all_hour.geojson"
	allDay      = "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/all_day.geojson"
	significant = "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/significant_week.geojson"
	m45Week     = "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_week.geojson"
)

// GeoJSON structures for USGS earthquake data
type FeatureCollection struct {
	Type     string    `json:"type"`
	Metadata Metadata  `json:"metadata"`
	Features []Feature `json:"features"`
}

type Metadata struct {
	Generated int64  `json:"generated"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Count     int    `json:"count"`
}

type Feature struct {
	Type       string     `json:"type"`
	Properties Properties `json:"properties"`
	Geometry   Geometry   `json:"geometry"`
	ID         string     `json:"id"`
}

type Properties struct {
	Mag     float64 `json:"mag"`     // Magnitude
	Place   string  `json:"place"`   // Location description
	Time    int64   `json:"time"`    // Unix timestamp (ms)
	Updated int64   `json:"updated"` // Last updated (ms)
	URL     string  `json:"url"`     // USGS event page
	Detail  string  `json:"detail"`  // Detail JSON URL
	Alert   string  `json:"alert"`   // PAGER alert level (green/yellow/orange/red)
	Status  string  `json:"status"`  // reviewed, automatic
	Tsunami int     `json:"tsunami"` // 1 if tsunami warning
	Sig     int     `json:"sig"`     // Significance (0-1000)
	Title   string  `json:"title"`   // Full title
	Type    string  `json:"type"`    // earthquake, quarry blast, etc.
}

type Geometry struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"` // [longitude, latitude, depth]
}

// Source fetches earthquake data from USGS
type Source struct {
	name   string
	url    string
	client *http.Client
}

// NewSignificant creates a source for significant earthquakes (past week)
func NewSignificant() *Source {
	return &Source{
		name:   "USGS Significant",
		url:    significant,
		client: httpclient.Default(),
	}
}

// NewM45Week creates a source for M4.5+ earthquakes (past week)
func NewM45Week() *Source {
	return &Source{
		name:   "USGS M4.5+",
		url:    m45Week,
		client: httpclient.Default(),
	}
}

// NewAllDay creates a source for all earthquakes in past day
func NewAllDay() *Source {
	return &Source{
		name:   "USGS Today",
		url:    allDay,
		client: httpclient.Default(),
	}
}

func (s *Source) Name() string {
	return s.name
}

func (s *Source) Type() feeds.SourceType {
	return feeds.SourceUSGS
}

func (s *Source) Fetch() ([]feeds.Item, error) {
	resp, err := s.client.Get(s.url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch earthquakes: %w", err)
	}
	defer resp.Body.Close()

	var fc FeatureCollection
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		return nil, fmt.Errorf("failed to decode earthquakes: %w", err)
	}

	items := make([]feeds.Item, 0, len(fc.Features))
	now := time.Now()

	for _, f := range fc.Features {
		// Skip non-earthquakes unless significant
		if f.Properties.Type != "earthquake" && f.Properties.Sig < 100 {
			continue
		}

		// Build summary with useful info
		summary := fmt.Sprintf("Magnitude %.1f earthquake %s",
			f.Properties.Mag, f.Properties.Place)

		if f.Properties.Alert != "" {
			summary += fmt.Sprintf(" [ALERT: %s]", f.Properties.Alert)
		}
		if f.Properties.Tsunami == 1 {
			summary += " [TSUNAMI WARNING]"
		}

		// Depth info
		depth := 0.0
		if len(f.Geometry.Coordinates) >= 3 {
			depth = f.Geometry.Coordinates[2]
		}
		summary += fmt.Sprintf(" Depth: %.1fkm", depth)

		items = append(items, feeds.Item{
			ID:         fmt.Sprintf("usgs-%s", f.ID),
			Source:     feeds.SourceUSGS,
			SourceName: s.name,
			SourceURL:  f.Properties.URL,
			Title:      f.Properties.Title,
			Summary:    summary,
			URL:        f.Properties.URL,
			Published:  time.UnixMilli(f.Properties.Time),
			Fetched:    now,
		})
	}

	return items, nil
}
