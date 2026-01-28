//go:build ignore

package filters

import (
	"context"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/model"
)

func TestTimeFilter(t *testing.T) {
	now := time.Now()
	items := []model.Item{
		{ID: "old", Title: "Old", Published: now.Add(-48 * time.Hour)},
		{ID: "recent", Title: "Recent", Published: now.Add(-30 * time.Minute)},
		{ID: "new", Title: "New", Published: now.Add(-5 * time.Minute)},
	}

	filter := NewTimeFilter(1 * time.Hour)

	result, err := filter.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("TimeFilter.Run failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 items within 1 hour, got %d", len(result))
	}

	// Verify the correct items were kept
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["recent"] || !ids["new"] {
		t.Error("expected recent and new items to be kept")
	}
	if ids["old"] {
		t.Error("old item should have been filtered out")
	}
}

func TestTimeFilterNegativeAge(t *testing.T) {
	// Negative age should be treated as 0 (filter everything)
	filter := NewTimeFilter(-1 * time.Hour)
	if filter.MaxAge != 0 {
		t.Errorf("expected MaxAge to be 0 for negative input, got %v", filter.MaxAge)
	}
}

func TestTimeFilterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	filter := NewTimeFilter(1 * time.Hour)
	items := make([]model.Item, 100)

	_, err := filter.Run(ctx, items, nil)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestDedupFilter(t *testing.T) {
	items := []model.Item{
		{ID: "1", Title: "First Article", URL: "https://example.com/1"},
		{ID: "2", Title: "Second Article", URL: "https://example.com/2"},
		{ID: "3", Title: "First Article", URL: "https://other.com/1"}, // Same title, different URL
		{ID: "4", Title: "Third Article", URL: "https://example.com/1"}, // Different title, same URL
	}

	filter := NewDedupFilter()

	result, err := filter.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("DedupFilter.Run failed: %v", err)
	}

	// Should keep: 1 (first occurrence), 2 (unique)
	// Should drop: 3 (same title as 1), 4 (same URL as 1)
	if len(result) != 2 {
		t.Errorf("expected 2 unique items, got %d", len(result))
	}
}

func TestDedupFilterNormalization(t *testing.T) {
	items := []model.Item{
		{ID: "1", Title: "Breaking: Stock Market Crashes", URL: "https://example.com/1"},
		{ID: "2", Title: "Stock Market Crashes", URL: "https://other.com/2"}, // Same after normalization
	}

	filter := NewDedupFilter()

	result, err := filter.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("DedupFilter.Run failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 item after normalization, got %d", len(result))
	}
}

func TestDedupFilterEmptyURL(t *testing.T) {
	// Items with empty URLs should only be deduped by title
	items := []model.Item{
		{ID: "1", Title: "Article One", URL: ""},
		{ID: "2", Title: "Article Two", URL: ""},
		{ID: "3", Title: "Article One", URL: ""}, // Duplicate title
	}

	filter := NewDedupFilter()

	result, err := filter.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("DedupFilter.Run failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestSourceBalanceFilter(t *testing.T) {
	items := []model.Item{
		{ID: "1", SourceName: "Source A"},
		{ID: "2", SourceName: "Source A"},
		{ID: "3", SourceName: "Source A"},
		{ID: "4", SourceName: "Source B"},
		{ID: "5", SourceName: "Source B"},
	}

	filter := NewSourceBalanceFilter(2) // Max 2 per source

	result, err := filter.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("SourceBalanceFilter.Run failed: %v", err)
	}

	// Should keep: 1, 2 (first 2 from A), 4, 5 (first 2 from B)
	if len(result) != 4 {
		t.Errorf("expected 4 items with max 2 per source, got %d", len(result))
	}

	// Verify balance
	sourceCounts := make(map[string]int)
	for _, item := range result {
		sourceCounts[item.SourceName]++
	}
	if sourceCounts["Source A"] > 2 {
		t.Error("Source A has more than 2 items")
	}
	if sourceCounts["Source B"] > 2 {
		t.Error("Source B has more than 2 items")
	}
}

func TestSourceBalanceFilterZeroMax(t *testing.T) {
	// Zero or negative should default to 10
	filter := NewSourceBalanceFilter(0)
	if filter.MaxPerSource != 10 {
		t.Errorf("expected default of 10, got %d", filter.MaxPerSource)
	}

	filter = NewSourceBalanceFilter(-5)
	if filter.MaxPerSource != 10 {
		t.Errorf("expected default of 10, got %d", filter.MaxPerSource)
	}
}

func TestSourceFilter(t *testing.T) {
	items := []model.Item{
		{ID: "1", SourceName: "Include Me"},
		{ID: "2", SourceName: "Exclude Me"},
		{ID: "3", SourceName: "Include Me"},
		{ID: "4", SourceName: "Also Exclude"},
	}

	filter := NewSourceFilter([]string{"Include Me"})

	result, err := filter.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("SourceFilter.Run failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 items from 'Include Me', got %d", len(result))
	}

	for _, item := range result {
		if item.SourceName != "Include Me" {
			t.Errorf("unexpected source: %s", item.SourceName)
		}
	}
}

func TestSourceFilterEmpty(t *testing.T) {
	items := []model.Item{
		{ID: "1", SourceName: "Any Source"},
	}

	filter := NewSourceFilter([]string{}) // Empty = nothing passes

	result, err := filter.Run(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("SourceFilter.Run failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 items with empty source filter, got %d", len(result))
	}
}

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Breaking: Big News", "big news"},
		{"UPDATE: Something Happened", "something happened"},
		{"  Extra   Spaces  ", "extra spaces"},
		{"WATCH: Video Content", "video content"},
		{"", ""},
		{"Normal Title", "normal title"},
	}

	for _, tc := range tests {
		result := normalizeTitle(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}
