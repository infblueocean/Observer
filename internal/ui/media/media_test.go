package media

import (
	"testing"
	"time"
)

func TestGlitchEngine(t *testing.T) {
	ge := GlitchEngine{Styles: DefaultStyles()}
	title := "Test Title"
	
	// Intensity 0 should return original
	if got := ge.Glitchify(title, 0.0, 1); got != title {
		t.Errorf("Glitchify(0.0) = %q, want %q", got, title)
	}

	// Intensity 1 should return something different (with ANSI codes)
	got := ge.Glitchify(title, 1.0, 1)
	if got == title {
		t.Errorf("Glitchify(1.0) should distort text")
	}
	// It should be longer due to ANSI codes
	if len(got) <= len(title) {
		t.Errorf("Glitchify(1.0) should contain ANSI codes")
	}
}

func TestFeedModel_Recompute(t *testing.T) {
	now := time.Now()
	base := Breakdown{Semantic: 0.5, Rerank: 0.5, Arousal: 0.5}
	
	headlines := []Headline{
		{ID: "1", Title: "Old", Published: now.Add(-24 * time.Hour), Breakdown: base},
		{ID: "2", Title: "New", Published: now, Breakdown: base},
	}
	
	m := NewFeedModel(headlines)
	
	// High recency weight -> New should be first
	w := DefaultWeights()
	w.Recency = 1.0
	w.Arousal = 0.0
	
	m.Recompute(w, now)
	
	if m.Headlines[0].ID != "2" {
		t.Errorf("Expected New item first with high recency weight")
	}
	
	// Reset for second test: High arousal weight
	// Re-initialize slice to ensure ID "1" is first
	headlines = []Headline{
		{ID: "1", Title: "Old", Published: now.Add(-24 * time.Hour), Breakdown: base},
		{ID: "2", Title: "New", Published: now, Breakdown: base},
	}
	headlines[0].Breakdown.Arousal = 1.0
	headlines[1].Breakdown.Arousal = 0.0
	m = NewFeedModel(headlines)
	
	w.Recency = 0.0
	w.Arousal = 1.0
	
	m.Recompute(w, now)
	
	if m.Headlines[0].ID != "1" {
		t.Errorf("Expected Old item (High Arousal) first with high arousal weight")
	}
}
