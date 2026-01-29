// Package embed provides text embedding generation and similarity computation.
package embed

import (
	"context"
	"math"
)

// Embedder generates vector embeddings from text.
type Embedder interface {
	// Available returns true if the embedding service is accessible.
	Available() bool
	// Embed generates a vector embedding for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
}

// BatchEmbedder extends Embedder with batch embedding support.
// Implementations can embed multiple texts in a single API call for efficiency.
// When EmbedBatch returns nil error, the result slice must have the same length
// as the input texts slice, with result[i] corresponding to texts[i].
type BatchEmbedder interface {
	Embedder
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// CosineSimilarity computes similarity between two embeddings.
// Returns 1.0 for identical vectors, 0.0 for orthogonal vectors.
// Returns 0.0 if vectors have different lengths or either is zero-length.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	// Handle zero vectors
	if normA == 0 || normB == 0 {
		return 0.0
	}

	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}
