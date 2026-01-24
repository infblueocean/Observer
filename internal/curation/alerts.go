package curation

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

// AlertType indicates how an alert triggers
type AlertType string

const (
	AlertTypePattern    AlertType = "pattern"    // Keyword/regex match
	AlertTypeSemantic   AlertType = "semantic"   // AI-evaluated criteria
	AlertTypeThreshold  AlertType = "threshold"  // Numeric threshold (e.g., earthquake magnitude)
	AlertTypeCorrelation AlertType = "correlation" // Multiple sources covering same story
)

// AlertPriority indicates urgency
type AlertPriority int

const (
	PriorityLow    AlertPriority = 1
	PriorityMedium AlertPriority = 2
	PriorityHigh   AlertPriority = 3
	PriorityUrgent AlertPriority = 4
)

// Alert represents a user-defined semantic alert
type Alert struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`        // "AI Regulation News"
	Description string        `json:"description"` // Natural language: "Alert when there's news about AI regulation, safety, or governance"
	Type        AlertType     `json:"type"`
	Priority    AlertPriority `json:"priority"`
	Enabled     bool          `json:"enabled"`
	CreatedAt   time.Time     `json:"created_at"`

	// For pattern alerts
	Patterns []string `json:"patterns,omitempty"`

	// For semantic alerts - AI criteria
	Criteria string `json:"criteria,omitempty"`

	// For threshold alerts
	Field     string  `json:"field,omitempty"`     // e.g., "magnitude", "probability_change"
	Operator  string  `json:"operator,omitempty"`  // ">", "<", ">=", "<=", "="
	Threshold float64 `json:"threshold,omitempty"` // e.g., 6.0 for earthquake magnitude

	// For correlation alerts
	MinSources int `json:"min_sources,omitempty"` // Minimum sources covering same story

	// Notification settings
	Sound    bool `json:"sound"`    // Play sound on match
	Sticky   bool `json:"sticky"`   // Keep at top of stream
	Highlight bool `json:"highlight"` // Visual highlight

	// Stats
	TriggerCount int       `json:"trigger_count"`
	LastTrigger  time.Time `json:"last_trigger,omitempty"`
}

// AlertMatch represents an item that triggered an alert
type AlertMatch struct {
	Item       feeds.Item
	Alert      *Alert
	MatchedAt  time.Time
	Reason     string
	Confidence float64 // For semantic alerts
	Dismissed  bool
}

// AlertEngine manages semantic alerts
type AlertEngine struct {
	alerts      map[string]*Alert
	matches     []AlertMatch
	aiEvaluator AIEvaluator // Reuse from filters
}

// NewAlertEngine creates a new alert engine
func NewAlertEngine(aiEval AIEvaluator) *AlertEngine {
	ae := &AlertEngine{
		alerts:      make(map[string]*Alert),
		matches:     make([]AlertMatch, 0),
		aiEvaluator: aiEval,
	}

	// Add some default alerts (disabled by default)
	ae.AddAlert(DefaultAlerts()...)

	return ae
}

// DefaultAlerts returns useful preset alerts users can enable
func DefaultAlerts() []*Alert {
	return []*Alert{
		{
			ID:          "breaking-news",
			Name:        "Breaking News",
			Description: "Major breaking news stories across multiple sources",
			Type:        AlertTypeCorrelation,
			Priority:    PriorityUrgent,
			Enabled:     false,
			MinSources:  3,
			Highlight:   true,
			Sticky:      true,
		},
		{
			ID:          "major-earthquake",
			Name:        "Major Earthquake",
			Description: "Earthquakes magnitude 6.0 or higher",
			Type:        AlertTypeThreshold,
			Priority:    PriorityUrgent,
			Enabled:     true, // On by default - this is important
			Field:       "magnitude",
			Operator:    ">=",
			Threshold:   6.0,
			Sound:       true,
			Highlight:   true,
		},
		{
			ID:          "market-move",
			Name:        "Big Market Move",
			Description: "Prediction market probability changed significantly",
			Type:        AlertTypeThreshold,
			Priority:    PriorityHigh,
			Enabled:     false,
			Field:       "probability_change_24h",
			Operator:    ">=",
			Threshold:   0.15, // 15% move
			Highlight:   true,
		},
		{
			ID:          "ai-news",
			Name:        "AI/ML News",
			Description: "Significant news about artificial intelligence, machine learning, or large language models",
			Type:        AlertTypeSemantic,
			Priority:    PriorityMedium,
			Enabled:     false,
			Criteria:    "Is this a significant announcement, breakthrough, or regulatory development in AI, machine learning, or large language models? Not just minor product updates.",
		},
		{
			ID:          "security-breach",
			Name:        "Security Breach",
			Description: "Major cybersecurity incidents, data breaches, or vulnerabilities",
			Type:        AlertTypeSemantic,
			Priority:    PriorityHigh,
			Enabled:     false,
			Criteria:    "Is this about a significant cybersecurity incident, data breach affecting many users, critical vulnerability, or major hack?",
			Highlight:   true,
		},
		{
			ID:          "political-breaking",
			Name:        "Political Breaking",
			Description: "Major political developments, elections, policy changes",
			Type:        AlertTypeSemantic,
			Priority:    PriorityHigh,
			Enabled:     false,
			Criteria:    "Is this a major political development such as: election results, significant legislation passing, leadership changes, major policy announcements, or geopolitical events?",
		},
		{
			ID:          "space-launch",
			Name:        "Space Launch",
			Description: "Rocket launches, space mission updates",
			Type:        AlertTypePattern,
			Priority:    PriorityLow,
			Enabled:     false,
			Patterns:    []string{`(?i)launch`, `(?i)rocket`, `(?i)spacex`, `(?i)nasa.*mission`},
		},
	}
}

// AddAlert adds alerts to the engine
func (ae *AlertEngine) AddAlert(alerts ...*Alert) {
	for _, a := range alerts {
		if a.CreatedAt.IsZero() {
			a.CreatedAt = time.Now()
		}
		ae.alerts[a.ID] = a
	}
}

// CreateAlert creates a new user-defined alert
func (ae *AlertEngine) CreateAlert(name, description string, priority AlertPriority) *Alert {
	a := &Alert{
		ID:          strings.ToLower(strings.ReplaceAll(name, " ", "-")),
		Name:        name,
		Description: description,
		Type:        AlertTypeSemantic,
		Priority:    priority,
		Enabled:     true,
		CreatedAt:   time.Now(),
		Criteria:    description,
		Highlight:   priority >= PriorityHigh,
	}
	ae.AddAlert(a)
	return a
}

// ToggleAlert enables or disables an alert
func (ae *AlertEngine) ToggleAlert(id string) bool {
	if a, ok := ae.alerts[id]; ok {
		a.Enabled = !a.Enabled
		return a.Enabled
	}
	return false
}

// GetAlerts returns all alerts
func (ae *AlertEngine) GetAlerts() []*Alert {
	result := make([]*Alert, 0, len(ae.alerts))
	for _, a := range ae.alerts {
		result = append(result, a)
	}
	return result
}

// GetEnabledAlerts returns only enabled alerts
func (ae *AlertEngine) GetEnabledAlerts() []*Alert {
	var result []*Alert
	for _, a := range ae.alerts {
		if a.Enabled {
			result = append(result, a)
		}
	}
	return result
}

// CheckItem evaluates an item against all enabled alerts
// Returns any alerts that were triggered
func (ae *AlertEngine) CheckItem(item feeds.Item) []AlertMatch {
	var matches []AlertMatch

	for _, alert := range ae.alerts {
		if !alert.Enabled {
			continue
		}

		var matched bool
		var reason string
		var confidence float64 = 1.0

		switch alert.Type {
		case AlertTypePattern:
			matched, reason = ae.checkPatternAlert(alert, item)

		case AlertTypeThreshold:
			matched, reason = ae.checkThresholdAlert(alert, item)

		case AlertTypeCorrelation:
			// Handled separately in batch processing
			continue

		case AlertTypeSemantic:
			// Handled separately with AI evaluation
			continue
		}

		if matched {
			alert.TriggerCount++
			alert.LastTrigger = time.Now()

			matches = append(matches, AlertMatch{
				Item:       item,
				Alert:      alert,
				MatchedAt:  time.Now(),
				Reason:     reason,
				Confidence: confidence,
			})
		}
	}

	// Store matches
	ae.matches = append(ae.matches, matches...)

	return matches
}

func (ae *AlertEngine) checkPatternAlert(alert *Alert, item feeds.Item) (bool, string) {
	text := strings.ToLower(item.Title + " " + item.Summary)

	for _, pattern := range alert.Patterns {
		if strings.Contains(text, strings.ToLower(pattern)) {
			return true, "Matched pattern: " + pattern
		}
	}
	return false, ""
}

func (ae *AlertEngine) checkThresholdAlert(alert *Alert, item feeds.Item) (bool, string) {
	// For earthquake magnitude
	if alert.Field == "magnitude" && item.Source == feeds.SourceUSGS {
		// Parse magnitude from title (e.g., "M 6.2 - 10 km SW of...")
		if mag := parseMagnitude(item.Title); mag > 0 {
			if checkThreshold(mag, alert.Operator, alert.Threshold) {
				return true, fmt.Sprintf("Magnitude %.1f %s %.1f", mag, alert.Operator, alert.Threshold)
			}
		}
	}

	// For prediction market probability changes
	if alert.Field == "probability_change_24h" {
		// Would need to track historical probabilities
		// TODO: Implement with probability history tracking
	}

	return false, ""
}

func parseMagnitude(title string) float64 {
	// Parse "M 6.2 - location" format
	if len(title) < 3 {
		return 0
	}
	if title[0] == 'M' && title[1] == ' ' {
		var mag float64
		_, err := fmt.Sscanf(title[2:], "%f", &mag)
		if err == nil {
			return mag
		}
	}
	return 0
}

func checkThreshold(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "=", "==":
		return value == threshold
	}
	return false
}

// GetRecentMatches returns recent alert matches
func (ae *AlertEngine) GetRecentMatches(limit int) []AlertMatch {
	if len(ae.matches) <= limit {
		return ae.matches
	}
	return ae.matches[len(ae.matches)-limit:]
}

// GetUndismissedMatches returns matches that haven't been dismissed
func (ae *AlertEngine) GetUndismissedMatches() []AlertMatch {
	var result []AlertMatch
	for _, m := range ae.matches {
		if !m.Dismissed {
			result = append(result, m)
		}
	}
	return result
}

// DismissMatch marks a match as dismissed
func (ae *AlertEngine) DismissMatch(itemID string) {
	for i := range ae.matches {
		if ae.matches[i].Item.ID == itemID {
			ae.matches[i].Dismissed = true
		}
	}
}

// Import is at top of file, using fmt.Sprintf directly
