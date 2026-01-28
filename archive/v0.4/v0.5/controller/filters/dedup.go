//go:build ignore

package filters

import (
	"context"
	"strings"

	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// DedupFilter removes duplicate items based on URL or title similarity.
//
// For MVP, this uses exact URL matching and normalized title matching.
// Future versions will use embedding-based semantic dedup.
//
// Items are processed in order - the first occurrence of a duplicate is kept.
//
// This filter is stateless and safe for concurrent use.
type DedupFilter struct{}

// NewDedupFilter creates a dedup filter.
func NewDedupFilter() *DedupFilter {
	return &DedupFilter{}
}

// Name returns "dedup".
func (f *DedupFilter) Name() string {
	return "dedup"
}

// Run removes duplicate items.
//
// Duplicates are identified by:
//  1. Exact URL match
//  2. Normalized title match (lowercase, common prefixes removed)
//
// The first occurrence of each item is kept.
//
// Respects context cancellation.
func (f *DedupFilter) Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Track seen URLs and normalized titles
	seenURLs := make(map[string]bool, len(items))
	seenTitles := make(map[string]bool, len(items))
	filtered := make([]model.Item, 0, len(items))

	for i, item := range items {
		// Check context periodically
		if i > 0 && i%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		// Check URL (primary dedup key)
		if item.URL != "" && seenURLs[item.URL] {
			continue
		}

		// Check normalized title (catch same story, different URL)
		normTitle := normalizeTitle(item.Title)
		if normTitle != "" && seenTitles[normTitle] {
			continue
		}

		if item.URL != "" {
			seenURLs[item.URL] = true
		}
		if normTitle != "" {
			seenTitles[normTitle] = true
		}
		filtered = append(filtered, item)
	}

	return filtered, nil
}

// normalizeTitle removes common prefixes/suffixes and normalizes whitespace.
//
// This helps catch duplicate stories with slightly different titles like:
//   - "Breaking: Stock Market Crashes"
//   - "Stock Market Crashes"
func normalizeTitle(title string) string {
	if title == "" {
		return ""
	}

	// Lowercase
	t := strings.ToLower(title)

	// Remove common prefixes
	prefixes := []string{
		"breaking:",
		"update:",
		"updated:",
		"exclusive:",
		"just in:",
		"developing:",
		"watch:",
		"video:",
		"live:",
	}
	for _, p := range prefixes {
		t = strings.TrimPrefix(t, p)
	}

	// Normalize whitespace
	t = strings.TrimSpace(t)
	t = strings.Join(strings.Fields(t), " ")

	return t
}
