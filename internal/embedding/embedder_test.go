package embedding

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a, b     Vector
		expected float64
		epsilon  float64
	}{
		{
			name:     "identical vectors",
			a:        Vector{1, 0, 0},
			b:        Vector{1, 0, 0},
			expected: 1.0,
			epsilon:  0.001,
		},
		{
			name:     "orthogonal vectors",
			a:        Vector{1, 0, 0},
			b:        Vector{0, 1, 0},
			expected: 0.0,
			epsilon:  0.001,
		},
		{
			name:     "opposite vectors",
			a:        Vector{1, 0, 0},
			b:        Vector{-1, 0, 0},
			expected: -1.0,
			epsilon:  0.001,
		},
		{
			name:     "similar vectors",
			a:        Vector{1, 1, 0},
			b:        Vector{1, 0.9, 0.1},
			expected: 0.989, // approximately
			epsilon:  0.01,
		},
		{
			name:     "empty vectors",
			a:        Vector{},
			b:        Vector{},
			expected: 0.0,
			epsilon:  0.001,
		},
		{
			name:     "different length",
			a:        Vector{1, 0},
			b:        Vector{1, 0, 0},
			expected: 0.0,
			epsilon:  0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.expected) > tt.epsilon {
				t.Errorf("CosineSimilarity(%v, %v) = %v, want %v (±%v)",
					tt.a, tt.b, got, tt.expected, tt.epsilon)
			}
		})
	}
}

func TestDedupIndex_InMemory(t *testing.T) {
	// Test without actual embedder (nil embedder = dedup disabled)
	idx := NewDedupIndex(nil, 0.85)

	// Without embedder, all items should be considered primary
	if !idx.IsPrimary("item1") {
		t.Error("expected item to be primary when embedder is nil")
	}

	if idx.GetGroupSize("item1") != 1 {
		t.Error("expected group size of 1 for unknown item")
	}
}
