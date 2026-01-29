package embed

import (
	"math"
	"testing"
)

func TestCosineSimilarityIdentical(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{1.0, 2.0, 3.0}

	result := CosineSimilarity(a, b)

	// Identical vectors should have similarity of 1.0
	if math.Abs(float64(result-1.0)) > 1e-6 {
		t.Errorf("CosineSimilarity(identical) = %v, want 1.0", result)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	// Two orthogonal vectors in 2D
	a := []float32{1.0, 0.0}
	b := []float32{0.0, 1.0}

	result := CosineSimilarity(a, b)

	// Orthogonal vectors should have similarity of 0.0
	if math.Abs(float64(result)) > 1e-6 {
		t.Errorf("CosineSimilarity(orthogonal) = %v, want 0.0", result)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
	}{
		{
			name: "first vector zero",
			a:    []float32{0.0, 0.0, 0.0},
			b:    []float32{1.0, 2.0, 3.0},
		},
		{
			name: "second vector zero",
			a:    []float32{1.0, 2.0, 3.0},
			b:    []float32{0.0, 0.0, 0.0},
		},
		{
			name: "both vectors zero",
			a:    []float32{0.0, 0.0, 0.0},
			b:    []float32{0.0, 0.0, 0.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineSimilarity(tt.a, tt.b)
			if result != 0.0 {
				t.Errorf("CosineSimilarity(%s) = %v, want 0.0", tt.name, result)
			}
		})
	}
}

func TestCosineSimilarityDifferentLengths(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
	}{
		{
			name: "first longer",
			a:    []float32{1.0, 2.0, 3.0, 4.0},
			b:    []float32{1.0, 2.0, 3.0},
		},
		{
			name: "second longer",
			a:    []float32{1.0, 2.0},
			b:    []float32{1.0, 2.0, 3.0},
		},
		{
			name: "first empty",
			a:    []float32{},
			b:    []float32{1.0, 2.0, 3.0},
		},
		{
			name: "second empty",
			a:    []float32{1.0, 2.0, 3.0},
			b:    []float32{},
		},
		{
			name: "both empty",
			a:    []float32{},
			b:    []float32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineSimilarity(tt.a, tt.b)
			if result != 0.0 {
				t.Errorf("CosineSimilarity(%s) = %v, want 0.0", tt.name, result)
			}
		})
	}
}

func TestCosineSimilaritySimilarVectors(t *testing.T) {
	// Two similar but not identical vectors
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{2.0, 4.0, 6.0} // Same direction, different magnitude

	result := CosineSimilarity(a, b)

	// Parallel vectors should have similarity of 1.0
	if math.Abs(float64(result-1.0)) > 1e-6 {
		t.Errorf("CosineSimilarity(parallel) = %v, want 1.0", result)
	}
}

func TestCosineSimilarityOppositeVectors(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{-1.0, -2.0, -3.0}

	result := CosineSimilarity(a, b)

	// Opposite vectors should have similarity of -1.0
	if math.Abs(float64(result+1.0)) > 1e-6 {
		t.Errorf("CosineSimilarity(opposite) = %v, want -1.0", result)
	}
}
