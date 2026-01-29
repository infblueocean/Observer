package rerank

import (
	"testing"
)

func TestSortByScore(t *testing.T) {
	scores := []Score{
		{Index: 0, Score: 0.3},
		{Index: 1, Score: 0.9},
		{Index: 2, Score: 0.5},
		{Index: 3, Score: 0.1},
	}

	sorted := SortByScore(scores)

	// Check sorted order (highest first)
	expected := []int{1, 2, 0, 3}
	for i, s := range sorted {
		if s.Index != expected[i] {
			t.Errorf("position %d: got index %d, want %d", i, s.Index, expected[i])
		}
	}

	// Original slice should be unchanged
	if scores[0].Index != 0 {
		t.Error("SortByScore modified original slice")
	}
}

func TestTopN(t *testing.T) {
	scores := []Score{
		{Index: 0, Score: 0.3},
		{Index: 1, Score: 0.9},
		{Index: 2, Score: 0.5},
		{Index: 3, Score: 0.1},
	}

	// Get top 2
	top := TopN(scores, 2)
	if len(top) != 2 {
		t.Fatalf("got %d results, want 2", len(top))
	}
	if top[0] != 1 || top[1] != 2 {
		t.Errorf("got indices %v, want [1, 2]", top)
	}

	// Request more than available
	all := TopN(scores, 10)
	if len(all) != 4 {
		t.Errorf("got %d results, want 4", len(all))
	}
}

func TestFilterAboveThreshold(t *testing.T) {
	scores := []Score{
		{Index: 0, Score: 0.3},
		{Index: 1, Score: 0.9},
		{Index: 2, Score: 0.5},
		{Index: 3, Score: 0.1},
	}

	// Filter >= 0.5
	filtered := FilterAboveThreshold(scores, 0.5)
	if len(filtered) != 2 {
		t.Fatalf("got %d results, want 2", len(filtered))
	}

	// Should be sorted by score
	if filtered[0].Index != 1 || filtered[1].Index != 2 {
		t.Errorf("got indices [%d, %d], want [1, 2]", filtered[0].Index, filtered[1].Index)
	}
}

func TestFilterAboveThreshold_Empty(t *testing.T) {
	scores := []Score{
		{Index: 0, Score: 0.3},
		{Index: 1, Score: 0.2},
	}

	filtered := FilterAboveThreshold(scores, 0.9)
	if len(filtered) != 0 {
		t.Errorf("got %d results, want 0", len(filtered))
	}
}
