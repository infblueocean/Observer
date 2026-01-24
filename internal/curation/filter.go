package curation

import (
	"regexp"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

// FilterType indicates how a filter evaluates items
type FilterType string

const (
	FilterTypePattern  FilterType = "pattern"  // Regex/keyword matching
	FilterTypeSemantic FilterType = "semantic" // AI-evaluated natural language
)

// FilterAction is what happens when a filter matches
type FilterAction string

const (
	ActionHide    FilterAction = "hide"    // Don't show in stream
	ActionDim     FilterAction = "dim"     // Show but dimmed
	ActionBoost   FilterAction = "boost"   // Increase prominence
	ActionTag     FilterAction = "tag"     // Add a tag/label
	ActionFlag    FilterAction = "flag"    // Flag for review
)

// Filter represents a user-defined content filter
type Filter struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`         // "No Shopping Content"
	Description string       `json:"description"`  // Natural language: "Hide product reviews, deals, shopping guides, affiliate content"
	Type        FilterType   `json:"type"`
	Action      FilterAction `json:"action"`
	Enabled     bool         `json:"enabled"`
	CreatedAt   time.Time    `json:"created_at"`

	// For pattern filters
	Patterns []string `json:"patterns,omitempty"` // Compiled at runtime

	// For semantic filters - evaluated by AI
	Criteria string `json:"criteria,omitempty"` // "Is this item promotional, sponsored, or primarily about shopping/deals?"

	// Sharing - for shared sessions
	Shared    bool   `json:"shared"`     // Visible to all session members
	CreatedBy string `json:"created_by"` // User who created (empty = system default)
	SessionID string `json:"session_id"` // Which session this belongs to (empty = local)

	// Stats
	MatchCount int       `json:"match_count"`
	LastMatch  time.Time `json:"last_match,omitempty"`
}

// FilterResult is the outcome of evaluating an item against filters
type FilterResult struct {
	Item       feeds.Item
	Action     FilterAction
	MatchedBy  []string // Filter names that matched
	Reason     string   // Why it matched (for transparency)
	Confidence float64  // For AI filters, 0-1
}

// FilterEngine manages and applies filters
type FilterEngine struct {
	filters         map[string]*Filter
	compiledPattern map[string][]*regexp.Regexp
	aiEvaluator     AIEvaluator // Interface to AI for semantic filters
}

// AIEvaluator is the interface for AI-powered filter evaluation
type AIEvaluator interface {
	// EvaluateItems checks items against semantic filter criteria
	// Returns map of item ID -> (matches bool, confidence float64, reason string)
	EvaluateItems(items []feeds.Item, criteria string) (map[string]SemanticMatch, error)
}

// SemanticMatch is the result of AI evaluation
type SemanticMatch struct {
	Matches    bool
	Confidence float64
	Reason     string
}

// NewFilterEngine creates a new filter engine
func NewFilterEngine(aiEval AIEvaluator) *FilterEngine {
	fe := &FilterEngine{
		filters:         make(map[string]*Filter),
		compiledPattern: make(map[string][]*regexp.Regexp),
		aiEvaluator:     aiEval,
	}

	// Add default filters
	fe.AddFilter(DefaultFilters()...)

	return fe
}

// DefaultFilters returns sensible defaults users can toggle
func DefaultFilters() []*Filter {
	return []*Filter{
		{
			ID:          "ads-sponsored",
			Name:        "Ads & Sponsored",
			Description: "Hide sponsored content, advertisements, and paid posts",
			Type:        FilterTypePattern,
			Action:      ActionHide,
			Enabled:     true,
			Patterns: []string{
				`(?i)/sponsored/`,
				`(?i)/branded-content/`,
				`(?i)/partner/`,
				`(?i)/paid-post/`,
				`(?i)/advertisement/`,
				`(?i)\[sponsored\]`,
				`(?i)\[ad\]`,
				`(?i)paid content`,
				`(?i)partner content`,
			},
		},
		{
			ID:          "shopping-deals",
			Name:        "Shopping & Deals",
			Description: "Hide product reviews, deals, coupons, and shopping guides",
			Type:        FilterTypePattern,
			Action:      ActionHide,
			Enabled:     true,
			Patterns: []string{
				`(?i)/deals/`,
				`(?i)/coupons/`,
				`(?i)/shopping/`,
				`(?i)/reviewed/`,
				`(?i)/underscored/`,
				`(?i)/select/`,
				`(?i)/blueprint/`,
				`(?i)best.*deals`,
				`(?i)price drop`,
			},
		},
		{
			ID:          "clickbait",
			Name:        "Clickbait Titles",
			Description: "Dim articles with clickbait-style titles",
			Type:        FilterTypeSemantic,
			Action:      ActionDim,
			Enabled:     false, // Off by default, user can enable
			Criteria:    "Does this title use clickbait tactics like: numbered lists promising secrets, emotional manipulation, vague teasers, 'you won't believe', 'shocking', excessive punctuation, or misleading implications?",
		},
		{
			ID:          "breaking-boost",
			Name:        "Breaking News Boost",
			Description: "Boost breaking news and urgent updates",
			Type:        FilterTypeSemantic,
			Action:      ActionBoost,
			Enabled:     false,
			Criteria:    "Is this breaking news about a significant ongoing event, emergency, or major development that just happened in the last few hours?",
		},
		{
			ID:          "opinion-tag",
			Name:        "Opinion Pieces",
			Description: "Tag opinion and editorial content",
			Type:        FilterTypeSemantic,
			Action:      ActionTag,
			Enabled:     false,
			Criteria:    "Is this primarily an opinion piece, editorial, commentary, or analysis rather than factual news reporting?",
		},
	}
}

// AddFilter adds one or more filters
func (fe *FilterEngine) AddFilter(filters ...*Filter) {
	for _, f := range filters {
		if f.CreatedAt.IsZero() {
			f.CreatedAt = time.Now()
		}
		fe.filters[f.ID] = f

		// Compile patterns
		if f.Type == FilterTypePattern {
			var compiled []*regexp.Regexp
			for _, p := range f.Patterns {
				if re, err := regexp.Compile(p); err == nil {
					compiled = append(compiled, re)
				}
			}
			fe.compiledPattern[f.ID] = compiled
		}
	}
}

// CreateFilter creates a new user-defined filter from natural language
func (fe *FilterEngine) CreateFilter(name, description string, action FilterAction) *Filter {
	f := &Filter{
		ID:          strings.ToLower(strings.ReplaceAll(name, " ", "-")),
		Name:        name,
		Description: description,
		Type:        FilterTypeSemantic,
		Action:      action,
		Enabled:     true,
		CreatedAt:   time.Now(),
		Criteria:    description, // Use description as AI criteria
		Shared:      false,       // Private by default
	}
	fe.AddFilter(f)
	return f
}

// CreateSharedFilter creates a filter visible to all session members
func (fe *FilterEngine) CreateSharedFilter(name, description string, action FilterAction, sessionID, createdBy string) *Filter {
	f := &Filter{
		ID:          strings.ToLower(strings.ReplaceAll(name, " ", "-")),
		Name:        name,
		Description: description,
		Type:        FilterTypeSemantic,
		Action:      action,
		Enabled:     true,
		CreatedAt:   time.Now(),
		Criteria:    description,
		Shared:      true,
		SessionID:   sessionID,
		CreatedBy:   createdBy,
	}
	fe.AddFilter(f)
	return f
}

// ShareFilter makes a filter visible to session members
func (fe *FilterEngine) ShareFilter(id string) bool {
	if f, ok := fe.filters[id]; ok {
		f.Shared = true
		return true
	}
	return false
}

// UnshareFilter makes a filter private
func (fe *FilterEngine) UnshareFilter(id string) bool {
	if f, ok := fe.filters[id]; ok {
		f.Shared = false
		return true
	}
	return false
}

// GetSharedFilters returns filters shared in a session
func (fe *FilterEngine) GetSharedFilters(sessionID string) []*Filter {
	var result []*Filter
	for _, f := range fe.filters {
		if f.Shared && (sessionID == "" || f.SessionID == sessionID) {
			result = append(result, f)
		}
	}
	return result
}

// ImportFilter imports a filter from another session/user
func (fe *FilterEngine) ImportFilter(f *Filter) {
	// Create a copy with new ID to avoid conflicts
	imported := *f
	imported.ID = f.ID + "-imported"
	imported.Shared = false // Imported filters start as private
	fe.AddFilter(&imported)
}

// ToggleFilter enables or disables a filter
func (fe *FilterEngine) ToggleFilter(id string) bool {
	if f, ok := fe.filters[id]; ok {
		f.Enabled = !f.Enabled
		return f.Enabled
	}
	return false
}

// GetFilters returns all filters
func (fe *FilterEngine) GetFilters() []*Filter {
	result := make([]*Filter, 0, len(fe.filters))
	for _, f := range fe.filters {
		result = append(result, f)
	}
	return result
}

// GetEnabledFilters returns only enabled filters
func (fe *FilterEngine) GetEnabledFilters() []*Filter {
	var result []*Filter
	for _, f := range fe.filters {
		if f.Enabled {
			result = append(result, f)
		}
	}
	return result
}

// EvaluateItem checks an item against all enabled pattern filters
// Returns the action to take and which filters matched
func (fe *FilterEngine) EvaluateItem(item feeds.Item) FilterResult {
	result := FilterResult{
		Item:   item,
		Action: "", // No action by default
	}

	for _, f := range fe.filters {
		if !f.Enabled || f.Type != FilterTypePattern {
			continue
		}

		patterns := fe.compiledPattern[f.ID]
		for _, re := range patterns {
			if re.MatchString(item.URL) || re.MatchString(item.Title) || re.MatchString(item.Summary) {
				result.MatchedBy = append(result.MatchedBy, f.Name)
				result.Reason = "Matched pattern: " + re.String()
				f.MatchCount++
				f.LastMatch = time.Now()

				// Take the most restrictive action
				if f.Action == ActionHide || result.Action == "" {
					result.Action = f.Action
				}
				break
			}
		}
	}

	return result
}

// EvaluateItemsSemantic runs AI evaluation on items for semantic filters
// This should be called periodically in the background, not on every item
func (fe *FilterEngine) EvaluateItemsSemantic(items []feeds.Item) ([]FilterResult, error) {
	if fe.aiEvaluator == nil {
		return nil, nil
	}

	var results []FilterResult

	// Get enabled semantic filters
	var semanticFilters []*Filter
	for _, f := range fe.filters {
		if f.Enabled && f.Type == FilterTypeSemantic {
			semanticFilters = append(semanticFilters, f)
		}
	}

	if len(semanticFilters) == 0 {
		return nil, nil
	}

	// Evaluate each filter
	for _, f := range semanticFilters {
		matches, err := fe.aiEvaluator.EvaluateItems(items, f.Criteria)
		if err != nil {
			continue
		}

		for _, item := range items {
			if match, ok := matches[item.ID]; ok && match.Matches {
				f.MatchCount++
				f.LastMatch = time.Now()

				results = append(results, FilterResult{
					Item:       item,
					Action:     f.Action,
					MatchedBy:  []string{f.Name},
					Reason:     match.Reason,
					Confidence: match.Confidence,
				})
			}
		}
	}

	return results, nil
}

// Stats returns filtering statistics
func (fe *FilterEngine) Stats() map[string]int {
	stats := make(map[string]int)
	for _, f := range fe.filters {
		stats[f.Name] = f.MatchCount
	}
	return stats
}
