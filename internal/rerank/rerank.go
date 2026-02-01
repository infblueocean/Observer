// Package rerank provides query-document relevance scoring using cross-encoder models.
// Unlike embedding-based similarity, rerankers directly model query-document pairs
// for more accurate relevance judgments.
package rerank

import (
	"context"
	"sort"
)

// Reranker scores documents against a query using cross-encoder models.
// Higher scores indicate higher relevance.
type Reranker interface {
	// Available returns true if the reranker service is accessible.
	Available() bool

	// Rerank scores documents against the query, returning scores for each document.
	// Scores are in the range [0, 1] where 1 is highly relevant.
	// The returned slice has the same length as documents, in corresponding order.
	Rerank(ctx context.Context, query string, documents []string) ([]Score, error)

	// Name returns the reranker identifier for logging.
	Name() string
}

// AutoReranker is an optional extension of Reranker. Backends that support
// fast batch reranking (e.g., Jina) return true. Slow per-item backends
// (e.g., Ollama) return false. Used by main.go to set AppConfig.AutoReranks.
type AutoReranker interface {
	AutoReranks() bool
}

// Score represents a document's relevance score.
type Score struct {
	Index int     // Original index in the documents slice
	Score float32 // Relevance score in [0, 1]
}

// SortByScore returns scores sorted by relevance (highest first).
// Does not modify the input slice.
func SortByScore(scores []Score) []Score {
	result := make([]Score, len(scores))
	copy(result, scores)

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

// TopN returns the indices of the top N highest-scoring documents.
// If n > len(scores), returns all indices.
func TopN(scores []Score, n int) []int {
	sorted := SortByScore(scores)
	if n > len(sorted) {
		n = len(sorted)
	}

	indices := make([]int, n)
	for i := 0; i < n; i++ {
		indices[i] = sorted[i].Index
	}
	return indices
}

// FilterAboveThreshold returns scores above the given threshold, sorted by score.
func FilterAboveThreshold(scores []Score, threshold float32) []Score {
	var result []Score
	for _, s := range scores {
		if s.Score >= threshold {
			result = append(result, s)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}
