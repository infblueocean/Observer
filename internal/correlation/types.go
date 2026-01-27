// Package correlation provides entity extraction, story clustering,
// and relationship tracking for news items.
//
// Philosophy: "Don't curate. Illuminate."
//
// The correlation engine makes the shape of information visible
// without deciding what matters. It extracts structure from text,
// finds relationships between items, and surfaces patterns—all
// transparently, all optional, all user-controlled.
package correlation

import (
	"time"
)

// ClaimType describes the nature of a claim
type ClaimType string

const (
	ClaimStatement  ClaimType = "statement"  // Assertion of fact
	ClaimPrediction ClaimType = "prediction" // Future-oriented
	ClaimDenial     ClaimType = "denial"     // Refutation
)

// Claim represents an extracted statement or prediction
type Claim struct {
	ID                 string    `json:"id"`
	ItemID             string    `json:"item_id"`
	EntityID           string    `json:"entity_id,omitempty"` // Who made the claim
	ClaimText          string    `json:"claim_text"`
	ClaimType          ClaimType `json:"claim_type"`
	Sentiment          string    `json:"sentiment"` // positive, negative, neutral
	ExtractedAt        time.Time `json:"extracted_at"`
	PredictionDate     time.Time `json:"prediction_date,omitempty"` // When X is expected
	PredictionResolved bool      `json:"prediction_resolved,omitempty"`
	PredictionOutcome  string    `json:"prediction_outcome,omitempty"`
}

// DisagreementType categorizes conflicts between sources
type DisagreementType string

const (
	DisagreementFactual  DisagreementType = "factual"  // Different facts
	DisagreementFraming  DisagreementType = "framing"  // Same facts, different spin
	DisagreementOmission DisagreementType = "omission" // One covers what other ignores
)

// Disagreement records when sources conflict
type Disagreement struct {
	ID               string           `json:"id"`
	ClusterID        string           `json:"cluster_id,omitempty"`
	ClaimAID         string           `json:"claim_a_id"`
	ClaimBID         string           `json:"claim_b_id"`
	DisagreementType DisagreementType `json:"disagreement_type"`
	Description      string           `json:"description"`
	DetectedAt       time.Time        `json:"detected_at"`
}

// VelocityTrend indicates direction of activity
type VelocityTrend string

const (
	TrendSpiking VelocityTrend = "spiking" // Significant increase
	TrendSteady  VelocityTrend = "steady"  // Stable
	TrendFading  VelocityTrend = "fading"  // Decreasing
)

// DuplicateGroup represents items that are duplicates/near-duplicates
type DuplicateGroup struct {
	ID         string    `json:"id"`
	ItemIDs    []string  `json:"item_ids"`
	SimHash    uint64    `json:"simhash"` // Fingerprint for matching
	DetectedAt time.Time `json:"detected_at"`
}

// SourceAttribution tracks original reporting vs aggregation
type SourceAttribution struct {
	OriginalSource string `json:"original_source,omitempty"`
	IsAggregation  bool   `json:"is_aggregation"`
}

// ItemEntity links items to entities (used by cheap extractor)
type ItemEntity struct {
	ItemID   string
	EntityID string
	Context  string  // The sentence/phrase where entity appeared
	Salience float64 // How important is this entity to the item (0-1)
}

// ActivityType represents a type of correlation activity
type ActivityType string

const (
	ActivityExtract   ActivityType = "extract"
	ActivityCluster   ActivityType = "cluster"
	ActivityDuplicate ActivityType = "duplicate"
	ActivityDisagree  ActivityType = "disagree"
)

// Activity represents a single correlation engine action
type Activity struct {
	Type      ActivityType
	Time      time.Time
	ItemTitle string
	Details   string // e.g., "found: US, China, Trade" or "joined cluster with 5 items"
}

// Stats holds correlation engine statistics
type Stats struct {
	ItemsProcessed     int
	EntitiesFound      int
	ClustersFormed     int
	DuplicatesFound    int
	DisagreementsFound int
	StartTime          time.Time
}

// ClusterSummary holds summary info for radar display
type ClusterSummary struct {
	ID          string
	Summary     string
	ItemCount   int
	Velocity    float64
	Trend       VelocityTrend
	HasConflict bool
	FirstItemAt time.Time
}
