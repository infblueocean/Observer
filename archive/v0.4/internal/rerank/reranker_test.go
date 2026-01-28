package rerank

import (
	"testing"
)

func TestSortedResults(t *testing.T) {
	scores := []float64{0.3, 0.9, 0.1, 0.7, 0.5}
	results := SortedResults(scores)

	// Should be sorted by score descending
	expected := []int{1, 3, 4, 0, 2} // indices of 0.9, 0.7, 0.5, 0.3, 0.1
	for i, r := range results {
		if r.Index != expected[i] {
			t.Errorf("position %d: expected index %d, got %d", i, expected[i], r.Index)
		}
	}
}

func TestTopN(t *testing.T) {
	scores := []float64{0.3, 0.9, 0.1, 0.7, 0.5}

	top3 := TopN(scores, 3)
	expected := []int{1, 3, 4} // indices of top 3 scores
	if len(top3) != 3 {
		t.Fatalf("expected 3 results, got %d", len(top3))
	}
	for i, idx := range top3 {
		if idx != expected[i] {
			t.Errorf("position %d: expected index %d, got %d", i, expected[i], idx)
		}
	}

	// Test with n > len
	topAll := TopN(scores, 100)
	if len(topAll) != 5 {
		t.Errorf("expected 5 results, got %d", len(topAll))
	}
}

func TestParseScore(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     float64
	}{
		{"plain number", "8", 0.8},
		{"with newline", "7\n", 0.7},
		{"with spaces", "  9  ", 0.9},
		{"decimal", "8.5", 0.85},
		{"fraction format", "8/10", 0.8},
		{"with text", "Score: 6", 0.6},
		{"zero", "0", 0.0},
		{"ten", "10", 1.0},
		{"already normalized", "0.7", 0.7},
		{"empty response", "", 0.0},
		{"garbage", "not a number", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScore(tt.response)
			if got != tt.want {
				t.Errorf("parseScore(%q) = %.2f, want %.2f", tt.response, got, tt.want)
			}
		})
	}
}

func TestNormalizeScore(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{8, 0.8},    // 0-10 scale
		{10, 1.0},   // max
		{0, 0.0},    // min
		{0.5, 0.5},  // already normalized
		{1.0, 1.0},  // boundary
		{15, 1.0},   // clamped to 1
		{-5, 0.0},   // clamped to 0
		{100, 1.0},  // large number clamped
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := normalizeScore(tt.input)
			if got != tt.want {
				t.Errorf("normalizeScore(%.1f) = %.2f, want %.2f", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetRubric(t *testing.T) {
	// Known rubric
	rubric := GetRubric("breaking")
	if rubric == "" {
		t.Error("expected non-empty rubric for 'breaking'")
	}

	// Unknown rubric returns default
	unknown := GetRubric("nonexistent")
	defaultRubric := GetRubric(DefaultRubric)
	if unknown != defaultRubric {
		t.Error("expected unknown rubric to return default")
	}
}

func TestListRubrics(t *testing.T) {
	rubrics := ListRubrics()
	if len(rubrics) == 0 {
		t.Error("expected at least one rubric")
	}

	// Check that known rubrics are in the list
	found := false
	for _, r := range rubrics {
		if r == "topstories" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'topstories' in rubric list")
	}
}
