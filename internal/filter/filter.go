// Package filter provides pure filter functions for items.
// All functions are simple: []Item in, []Item out. No side effects.
package filter

import (
	"sort"
	"strings"
	"time"

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
