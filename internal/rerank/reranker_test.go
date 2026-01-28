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

func TestParseScores(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected int
		want     []float64
	}{
		{
			name:     "standard format",
			response: "1. 8\n2. 5\n3. 9",
			expected: 3,
			want:     []float64{0.8, 0.5, 0.9},
		},
		{
			name:     "colon format",
			response: "1: 7\n2: 3\n3: 10",
			expected: 3,
			want:     []float64{0.7, 0.3, 1.0},
		},
		{
			name:     "with extra text",
			response: "Here are the scores:\n1. 6\n2. 8\nDone.",
			expected: 2,
			want:     []float64{0.6, 0.8},
		},
		{
			name:     "fraction format",
			response: "1. 8/10\n2. 5/10",
			expected: 2,
			want:     []float64{0.8, 0.5},
		},
		{
			name:     "missing scores default to 0.5",
			response: "1. 9",
			expected: 3,
			want:     []float64{0.9, 0.5, 0.5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScores(tt.response, tt.expected)
			if len(got) != len(tt.want) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.want))
			}
			for i, score := range got {
				if score != tt.want[i] {
					t.Errorf("score[%d]: got %.1f, want %.1f", i, score, tt.want[i])
				}
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
