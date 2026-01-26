// Package correlation provides entity extraction, story clustering,
// and relationship tracking for news items.
//
// Philosophy: "Don't curate. Illuminate."
//
// The correlation engine makes the shape of information visible
// without deciding what matters. It extracts structure from text,
// finds relationships between items, and surfaces patterns—all
// transparently, all optional, all user-controlled.
//
// See CORRELATION_ENGINE.md for full design documentation.
package correlation

import (
	"time"
)

// ClusterStatus indicates the lifecycle state of a story cluster
type ClusterStatus string

const (
	ClusterActive   ClusterStatus = "active"   // Still developing
	ClusterStale    ClusterStatus = "stale"    // No updates in a while
	ClusterResolved ClusterStatus = "resolved" // Story concluded
)

// Cluster represents a group of items about the same event/story
type Cluster struct {
	ID           string        `json:"id"`
	EventSummary string        `json:"event_summary"` // "Boeing 737 MAX grounding extends"
	EventType    string        `json:"event_type"`    // announcement, incident, statement
	FirstItemAt  time.Time     `json:"first_item_at"`
	LastItemAt   time.Time     `json:"last_item_at"`
	ItemCount    int           `json:"item_count"`
	SourceCount  int           `json:"source_count"`
	Status       ClusterStatus `json:"status"`
	Velocity     float64       `json:"velocity"` // Items per hour
}

// ClusterItem links an item to a cluster
type ClusterItem struct {
	ClusterID  string    `json:"cluster_id"`
	ItemID     string    `json:"item_id"`
	AddedAt    time.Time `json:"added_at"`
	Confidence float64   `json:"confidence"`
}

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

// VelocitySnapshot tracks activity over time
type VelocitySnapshot struct {
	EntityID    string    `json:"entity_id,omitempty"`
	ClusterID   string    `json:"cluster_id,omitempty"`
	SnapshotAt  time.Time `json:"snapshot_at"`
	Mentions1h  int       `json:"mentions_1h"`
	Mentions24h int       `json:"mentions_24h"`
	Velocity    float64   `json:"velocity"` // Trend direction
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
