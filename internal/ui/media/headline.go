package media

import (
	"time"
)

// Weights defines the influence of different factors on the final score.
// All values are expected to be in the range [0, 1].
type Weights struct {
	Arousal  float64 // Importance of emotional intensity
	Negavity float64 // Importance of negative sentiment
	Curiosity float64 // Importance of novelty/uniqueness
	Recency  float64 // Importance of how new the item is
}

// DefaultWeights returns the baseline weights for the media view.
func DefaultWeights() Weights {
	return Weights{
		Arousal:   0.5,
		Negavity:  0.3,
		Curiosity: 0.4,
		Recency:   0.8,
	}
}

// Breakdown contains the granular scores that make up a Headline's final ranking.
// All scores are in the range [0, 1].
type Breakdown struct {
	Semantic     float64 // Stage 1: Vector similarity score
	Rerank       float64 // Stage 2: Cross-encoder score
	Arousal      float64 // Emotional intensity (from LLM/Analysis)
	RecencyDecay float64 // Multiplier based on age
	Diversity    float64 // Penalty/Bonus for source/topic diversity
	NegBoost     float64 // Extra weight for negative/conflict news
	FinalScore   float64 // The final calculated rank
}

// Headline represents a single news item adapted for the "Engineered" Media View.
type Headline struct {
	ID         string
	Title      string
	Source     string
	Published  time.Time
	Breakdown  Breakdown
	TitleHash  uint32 // Pre-computed for the Glitch Engine
}

// EnsureHash populates the TitleHash if it hasn't been set.
// This avoids re-computing the hash during the 120ms glitch tick.
func (h *Headline) EnsureHash() {
	if h.TitleHash != 0 {
		return
	}
	h.TitleHash = fnv32a(h.Title)
}

// fnv32a is a simple, fast non-cryptographic hash used for glitch determinism.
func fnv32a(s string) uint32 {
	hash := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		hash ^= uint32(s[i])
		hash *= 16777619
	}
	return hash
}
