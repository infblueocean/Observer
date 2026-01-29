// Package filter provides pure filter functions for items.
// All functions are simple: []Item in, []Item out. No side effects.
package filter

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/store"
)

// commonPrefixes are prefixes commonly used in news titles that should be
// ignored when comparing titles for deduplication.
var commonPrefixes = []string{
	"breaking:",
	"update:",
	"updated:",
	"exclusive:",
	"just in:",
	"developing:",
	"watch:",
	"live:",
	"opinion:",
	"analysis:",
	"review:",
}

// ByAge removes items older than maxAge based on Published time.
func ByAge(items []store.Item, maxAge time.Duration) []store.Item {
	if len(items) == 0 {
		return []store.Item{}
	}

	cutoff := time.Now().Add(-maxAge)
	result := make([]store.Item, 0, len(items))

	for _, item := range items {
		if item.Published.After(cutoff) {
			result = append(result, item)
		}
	}

	return result
}

// BySource keeps only items from the specified source names.
func BySource(items []store.Item, sources []string) []store.Item {
	if len(items) == 0 || len(sources) == 0 {
		return []store.Item{}
	}

	// Build a set of allowed sources for O(1) lookup
	allowed := make(map[string]bool, len(sources))
	for _, s := range sources {
		allowed[s] = true
	}

	result := make([]store.Item, 0, len(items))
	for _, item := range items {
		if allowed[item.SourceName] {
			result = append(result, item)
		}
	}

	return result
}

// normalizeTitle normalizes a title for comparison by lowercasing and
// removing common news prefixes.
func normalizeTitle(title string) string {
	normalized := strings.ToLower(strings.TrimSpace(title))

	// Remove common prefixes
	for _, prefix := range commonPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			normalized = strings.TrimSpace(strings.TrimPrefix(normalized, prefix))
			break // Only remove one prefix
		}
	}

	return normalized
}

// Dedup removes items with duplicate URLs. First occurrence wins.
// Also removes items with very similar titles (case-insensitive, ignoring
// common prefixes like "Breaking:", "Update:", etc.)
func Dedup(items []store.Item) []store.Item {
	if len(items) == 0 {
		return []store.Item{}
	}

	seenURLs := make(map[string]bool)
	seenTitles := make(map[string]bool)
	result := make([]store.Item, 0, len(items))

	for _, item := range items {
		// Check URL deduplication
		if item.URL != "" && seenURLs[item.URL] {
			continue
		}

		// Check title deduplication
		normalizedTitle := normalizeTitle(item.Title)
		if normalizedTitle != "" && seenTitles[normalizedTitle] {
			continue
		}

		// Mark as seen
		if item.URL != "" {
			seenURLs[item.URL] = true
		}
		if normalizedTitle != "" {
			seenTitles[normalizedTitle] = true
		}

		result = append(result, item)
	}

	return result
}

// LimitPerSource caps the number of items per source.
// Keeps the most recent items (by Published time) for each source.
// The result is sorted by Published DESC to ensure deterministic order.
func LimitPerSource(items []store.Item, maxPerSource int) []store.Item {
	if len(items) == 0 || maxPerSource <= 0 {
		return []store.Item{}
	}

	// Group items by source
	bySource := make(map[string][]store.Item)
	for _, item := range items {
		bySource[item.SourceName] = append(bySource[item.SourceName], item)
	}

	// Sort each source's items by Published DESC and take top N
	result := make([]store.Item, 0, len(items))
	for _, sourceItems := range bySource {
		// Sort by Published DESC (most recent first)
		sort.Slice(sourceItems, func(i, j int) bool {
			return sourceItems[i].Published.After(sourceItems[j].Published)
		})

		// Take up to maxPerSource items
		limit := maxPerSource
		if limit > len(sourceItems) {
			limit = len(sourceItems)
		}
		result = append(result, sourceItems[:limit]...)
	}

	// Sort final result by Published DESC for deterministic order
	// (map iteration order is random, so we need this final sort)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Published.After(result[j].Published)
	})

	return result
}

// RerankByQuery reranks items by cosine similarity to a query embedding.
// Items with embeddings are sorted by similarity (highest first).
// Items without embeddings are placed at the end, maintaining their original order.
func RerankByQuery(items []store.Item, embeddings map[string][]float32, queryEmbedding []float32) []store.Item {
	if len(items) == 0 || len(queryEmbedding) == 0 || embeddings == nil {
		return items
	}

	type scored struct {
		item       store.Item
		similarity float32
		hasEmbed   bool
	}

	scoredItems := make([]scored, 0, len(items))
	for _, item := range items {
		s := scored{item: item}
		if emb, ok := embeddings[item.ID]; ok && len(emb) > 0 {
			s.similarity = embed.CosineSimilarity(emb, queryEmbedding)
			s.hasEmbed = true
		}
		scoredItems = append(scoredItems, s)
	}

	// Sort: items with embeddings first (by similarity desc), then items without
	// Use SliceStable to maintain original order for items without embeddings
	sort.SliceStable(scoredItems, func(i, j int) bool {
		// Both have embeddings: sort by similarity desc
		if scoredItems[i].hasEmbed && scoredItems[j].hasEmbed {
			return scoredItems[i].similarity > scoredItems[j].similarity
		}
		// Only one has embedding: that one goes first
		if scoredItems[i].hasEmbed != scoredItems[j].hasEmbed {
			return scoredItems[i].hasEmbed
		}
		// Neither has embedding: maintain original order (stable sort handles this)
		return false
	})

	result := make([]store.Item, len(scoredItems))
	for i, s := range scoredItems {
		result[i] = s.item
	}
	return result
}

// RerankByCrossEncoder reranks items using a cross-encoder reranker model.
// Takes the top N candidates and scores them against the query using the reranker.
// Returns items sorted by relevance score (highest first).
// If reranker fails or is unavailable, returns items unchanged.
func RerankByCrossEncoder(ctx context.Context, items []store.Item, query string, reranker Reranker) []store.Item {
	if len(items) == 0 || reranker == nil || !reranker.Available() {
		return items
	}

	// Extract titles for reranking
	docs := make([]string, len(items))
	for i, item := range items {
		docs[i] = item.Title
		if item.Summary != "" && len(item.Summary) < 200 {
			docs[i] += " - " + item.Summary
		}
	}

	// Score documents
	scores, err := reranker.Rerank(ctx, query, docs)
	if err != nil || len(scores) != len(items) {
		return items // Return unchanged on error
	}

	// Create scored items for sorting
	type scored struct {
		item  store.Item
		score float32
	}
	scoredItems := make([]scored, len(items))
	for i, item := range items {
		scoredItems[i] = scored{item: item, score: scores[i].Score}
	}

	// Sort by score descending
	sort.SliceStable(scoredItems, func(i, j int) bool {
		return scoredItems[i].score > scoredItems[j].score
	})

	// Extract sorted items
	result := make([]store.Item, len(scoredItems))
	for i, s := range scoredItems {
		result[i] = s.item
	}

	return result
}

// Reranker is the interface for cross-encoder reranking models.
// This is a subset of the rerank.Reranker interface for decoupling.
type Reranker interface {
	Available() bool
	Rerank(ctx context.Context, query string, documents []string) ([]Score, error)
}

// Score represents a document's relevance score.
type Score struct {
	Index int
	Score float32
}

// SemanticDedup removes semantically similar items using embeddings.
// Uses cosine similarity with threshold (e.g., 0.85).
// Falls back to URL dedup if embeddings unavailable for an item.
// First occurrence wins.
func SemanticDedup(items []store.Item, embeddings map[string][]float32, threshold float32) []store.Item {
	if len(items) == 0 {
		return []store.Item{}
	}

	seenURLs := make(map[string]bool)
	var seenEmbeddings [][]float32

	result := make([]store.Item, 0, len(items))

	for _, item := range items {
		// URL dedup (always)
		if item.URL != "" && seenURLs[item.URL] {
			continue
		}

		// Semantic dedup (if embedding available)
		if emb, ok := embeddings[item.ID]; ok {
			isDup := false
			for _, seen := range seenEmbeddings {
				if embed.CosineSimilarity(emb, seen) > threshold {
					isDup = true
					break
				}
			}
			if isDup {
				continue
			}
			seenEmbeddings = append(seenEmbeddings, emb)
		}

		if item.URL != "" {
			seenURLs[item.URL] = true
		}
		result = append(result, item)
	}

	return result
}
