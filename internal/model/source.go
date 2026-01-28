package model

import "time"

// Source represents a feed source configuration.
type Source struct {
	Name           string
	URL            string
	Type           SourceType
	Category       string
	RefreshMinutes int
	Weight         float64
	Enabled        bool

	// Runtime state
	LastFetched time.Time
	ItemCount   int
	ErrorCount  int
	LastError   string
}

// IsDue returns true if this source should be fetched.
func (s *Source) IsDue() bool {
	if s.LastFetched.IsZero() {
		return true
	}
	interval := time.Duration(s.RefreshMinutes) * time.Minute
	return time.Since(s.LastFetched) >= interval
}

// RefreshInterval presets
const (
	RefreshRealtime = 1  // 1 minute - earthquakes, breaking
	RefreshFast     = 2  // 2 minutes - HN, fast-moving
	RefreshNormal   = 5  // 5 minutes - wire services
	RefreshSlow     = 15 // 15 minutes - blogs, tech
	RefreshLazy     = 30 // 30 minutes - newspapers, longform
	RefreshHourly   = 60 // 1 hour - very slow sources
)

// SourceStatus tracks the runtime state of a source.
type SourceStatus struct {
	Name        string
	LastFetched time.Time
	ItemCount   int
	ErrorCount  int
	LastError   string
}
