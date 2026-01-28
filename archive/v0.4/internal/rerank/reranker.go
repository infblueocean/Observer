// Package rerank provides ML-based reranking of news headlines.
// Rerankers score documents against a query/rubric, enabling
// "top stories" to be determined by relevance scores rather than
// LLM classification.
package rerank

import (
	"context"
)

// Reranker scores documents against a query.
// Higher scores = more relevant to the query.
type Reranker interface {
	// Rerank scores documents against the query, returning scores in same order as docs.
	// Scores are typically 0.0-1.0 but may vary by implementation.
	Rerank(ctx context.Context, query string, docs []string) ([]float64, error)

	// RerankWithProgress is like Rerank but reports progress for long operations.
	RerankWithProgress(ctx context.Context, query string, docs []string, progress func(pct float64, msg string)) ([]float64, error)

	// Name returns the reranker name for logging/display.
	Name() string

	// Available returns true if the reranker is ready to use.
	Available() bool
}

// Result pairs a document index with its relevance score.
type Result struct {
	Index int
	Score float64
}

// SortedResults returns results sorted by score descending.
func SortedResults(scores []float64) []Result {
	results := make([]Result, len(scores))
	for i, score := range scores {
		results[i] = Result{Index: i, Score: score}
	}

	// Simple sort by score descending
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results
}

// TopN returns the indices of the top N scoring documents.
func TopN(scores []float64, n int) []int {
	sorted := SortedResults(scores)
	if n > len(sorted) {
		n = len(sorted)
	}

	indices := make([]int, n)
	for i := 0; i < n; i++ {
		indices[i] = sorted[i].Index
	}
	return indices
}
