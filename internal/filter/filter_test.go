package filter

import (
	"context"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/store"
)

func TestByAge(t *testing.T) {
	now := time.Now()
	items := []store.Item{
		{ID: "1", Title: "Recent", Published: now.Add(-1 * time.Hour)},
		{ID: "2", Title: "Old", Published: now.Add(-48 * time.Hour)},
		{ID: "3", Title: "Also Recent", Published: now.Add(-12 * time.Hour)},
	}

	result := ByAge(items, 24*time.Hour)

	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}

	// Verify we kept the right items
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["1"] || !ids["3"] {
		t.Error("expected items 1 and 3 to be kept")
	}
	if ids["2"] {
		t.Error("expected item 2 to be filtered out")
	}
}

func TestByAgeEmpty(t *testing.T) {
	result := ByAge(nil, 24*time.Hour)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	result = ByAge([]store.Item{}, 24*time.Hour)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestBySource(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Item 1", SourceName: "TechNews"},
		{ID: "2", Title: "Item 2", SourceName: "SportsFeed"},
		{ID: "3", Title: "Item 3", SourceName: "TechNews"},
		{ID: "4", Title: "Item 4", SourceName: "Weather"},
	}

	result := BySource(items, []string{"TechNews", "Weather"})

	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}

	// Verify we kept the right items
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["1"] || !ids["3"] || !ids["4"] {
		t.Error("expected items 1, 3, and 4 to be kept")
	}
	if ids["2"] {
		t.Error("expected item 2 to be filtered out")
	}
}

func TestBySourceEmpty(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Item 1", SourceName: "TechNews"},
	}

	// Empty sources list
	result := BySource(items, []string{})
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	// Nil sources list
	result = BySource(items, nil)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	// Empty items list
	result = BySource([]store.Item{}, []string{"TechNews"})
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	// Nil items list
	result = BySource(nil, []string{"TechNews"})
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestDedup(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "First Article", URL: "https://example.com/article1"},
		{ID: "2", Title: "Second Article", URL: "https://example.com/article2"},
		{ID: "3", Title: "Duplicate URL", URL: "https://example.com/article1"},
		{ID: "4", Title: "Third Article", URL: "https://example.com/article3"},
	}

	result := Dedup(items)

	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}

	// Verify first occurrence wins
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["1"] {
		t.Error("expected item 1 (first occurrence) to be kept")
	}
	if ids["3"] {
		t.Error("expected item 3 (duplicate URL) to be filtered out")
	}
}

func TestDedupEmptyURL(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "No URL", URL: ""},
		{ID: "2", Title: "Also No URL", URL: ""},
		{ID: "3", Title: "Has URL", URL: "https://example.com/article1"},
	}

	result := Dedup(items)

	// Items with different titles but empty URLs should both be kept
	// (empty URL doesn't count as duplicate)
	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}
}

func TestDedupSimilarTitles(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Major Event Happens Today", URL: "https://site1.com/event"},
		{ID: "2", Title: "Breaking: Major Event Happens Today", URL: "https://site2.com/event"},
		{ID: "3", Title: "UPDATE: Major Event Happens Today", URL: "https://site3.com/event"},
		{ID: "4", Title: "Different Story", URL: "https://site4.com/other"},
		{ID: "5", Title: "major event happens today", URL: "https://site5.com/event2"},
	}

	result := Dedup(items)

	if len(result) != 2 {
		t.Errorf("expected 2 items (first occurrence + different story), got %d", len(result))
	}

	// Verify first occurrence wins and different story is kept
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["1"] {
		t.Error("expected item 1 (first occurrence) to be kept")
	}
	if !ids["4"] {
		t.Error("expected item 4 (different story) to be kept")
	}
	if ids["2"] || ids["3"] || ids["5"] {
		t.Error("expected similar titles to be filtered out")
	}
}

func TestDedupEmpty(t *testing.T) {
	result := Dedup(nil)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	result = Dedup([]store.Item{})
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestLimitPerSource(t *testing.T) {
	now := time.Now()
	items := []store.Item{
		{ID: "1", SourceName: "TechNews", Published: now.Add(-3 * time.Hour)},
		{ID: "2", SourceName: "TechNews", Published: now.Add(-1 * time.Hour)}, // Most recent TechNews
		{ID: "3", SourceName: "TechNews", Published: now.Add(-2 * time.Hour)},
		{ID: "4", SourceName: "SportsFeed", Published: now.Add(-1 * time.Hour)},
		{ID: "5", SourceName: "SportsFeed", Published: now.Add(-2 * time.Hour)},
	}

	result := LimitPerSource(items, 2)

	if len(result) != 4 {
		t.Errorf("expected 4 items (2 per source), got %d", len(result))
	}

	// Count items per source
	countBySource := make(map[string]int)
	for _, item := range result {
		countBySource[item.SourceName]++
	}

	if countBySource["TechNews"] != 2 {
		t.Errorf("expected 2 TechNews items, got %d", countBySource["TechNews"])
	}
	if countBySource["SportsFeed"] != 2 {
		t.Errorf("expected 2 SportsFeed items, got %d", countBySource["SportsFeed"])
	}

	// Verify we kept the most recent items for TechNews
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["2"] || !ids["3"] {
		t.Error("expected most recent TechNews items (2 and 3) to be kept")
	}
	if ids["1"] {
		t.Error("expected oldest TechNews item (1) to be filtered out")
	}
}

func TestLimitPerSourceLessThanLimit(t *testing.T) {
	now := time.Now()
	items := []store.Item{
		{ID: "1", SourceName: "TechNews", Published: now.Add(-1 * time.Hour)},
		{ID: "2", SourceName: "SportsFeed", Published: now.Add(-1 * time.Hour)},
	}

	result := LimitPerSource(items, 5)

	if len(result) != 2 {
		t.Errorf("expected 2 items (all items, since under limit), got %d", len(result))
	}

	// Verify all items are kept
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["1"] || !ids["2"] {
		t.Error("expected all items to be kept when under limit")
	}
}

func TestLimitPerSourceEmpty(t *testing.T) {
	result := LimitPerSource(nil, 5)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	result = LimitPerSource([]store.Item{}, 5)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestLimitPerSourceZeroLimit(t *testing.T) {
	items := []store.Item{
		{ID: "1", SourceName: "TechNews", Published: time.Now()},
	}

	result := LimitPerSource(items, 0)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items with limit 0, got %d", len(result))
	}

	result = LimitPerSource(items, -1)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items with negative limit, got %d", len(result))
	}
}

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello world"},
		{"BREAKING: Major News", "major news"},
		{"Update: Story Develops", "story develops"},
		{"  Whitespace  ", "whitespace"},
		{"breaking: lowercased prefix", "lowercased prefix"},
		{"EXCLUSIVE: Big Story", "big story"},
		{"", ""},
	}

	for _, tc := range tests {
		result := normalizeTitle(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeTitle(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestSemanticDedupWithEmbeddings(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Go Programming Tutorial", URL: "http://example.com/1"},
		{ID: "2", Title: "Python Programming Guide", URL: "http://example.com/2"},
		{ID: "3", Title: "Go Programming Guide", URL: "http://example.com/3"}, // Similar to 1
	}

	// Create embeddings where 1 and 3 are very similar
	embeddings := map[string][]float32{
		"1": {1.0, 0.0, 0.0},
		"2": {0.0, 1.0, 0.0}, // Orthogonal to 1 and 3
		"3": {0.99, 0.1, 0.0}, // Very similar to 1
	}

	result := SemanticDedup(items, embeddings, 0.85)

	if len(result) != 2 {
		t.Errorf("expected 2 items (3 is duplicate of 1), got %d", len(result))
	}

	// Verify items 1 and 2 are kept, 3 is removed
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["1"] || !ids["2"] {
		t.Error("expected items 1 and 2 to be kept")
	}
	if ids["3"] {
		t.Error("expected item 3 to be filtered as semantic duplicate of 1")
	}
}

func TestSemanticDedupWithoutEmbeddings(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "First Article", URL: "http://example.com/1"},
		{ID: "2", Title: "Second Article", URL: "http://example.com/2"},
		{ID: "3", Title: "Third Article", URL: "http://example.com/1"}, // Same URL as 1
	}

	// No embeddings - should fall back to URL dedup
	embeddings := map[string][]float32{}

	result := SemanticDedup(items, embeddings, 0.85)

	if len(result) != 2 {
		t.Errorf("expected 2 items (URL dedup), got %d", len(result))
	}

	// First occurrence wins
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["1"] || !ids["2"] {
		t.Error("expected items 1 and 2 to be kept")
	}
	if ids["3"] {
		t.Error("expected item 3 to be filtered as URL duplicate")
	}
}

func TestSemanticDedupMixed(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Article A", URL: "http://example.com/1"},
		{ID: "2", Title: "Article B", URL: "http://example.com/2"},
		{ID: "3", Title: "Article C", URL: "http://example.com/3"}, // No embedding
		{ID: "4", Title: "Article D", URL: "http://example.com/4"},
	}

	// Only some items have embeddings
	embeddings := map[string][]float32{
		"1": {1.0, 0.0, 0.0},
		"2": {0.0, 1.0, 0.0},
		// "3" has no embedding
		"4": {0.98, 0.1, 0.0}, // Very similar to 1
	}

	result := SemanticDedup(items, embeddings, 0.85)

	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}

	// Items 1, 2, 3 should be kept; 4 removed as semantic dup of 1
	ids := make(map[string]bool)
	for _, item := range result {
		ids[item.ID] = true
	}
	if !ids["1"] || !ids["2"] || !ids["3"] {
		t.Error("expected items 1, 2, 3 to be kept")
	}
	if ids["4"] {
		t.Error("expected item 4 to be filtered as semantic duplicate")
	}
}

func TestSemanticDedupThreshold(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Article A", URL: "http://example.com/1"},
		{ID: "2", Title: "Article B", URL: "http://example.com/2"},
	}

	// Embeddings with moderate similarity (cosine ~0.8)
	embeddings := map[string][]float32{
		"1": {1.0, 0.0, 0.0},
		"2": {0.8, 0.6, 0.0}, // Cosine similarity ≈ 0.8
	}

	// With high threshold (0.9), both should be kept
	result := SemanticDedup(items, embeddings, 0.9)
	if len(result) != 2 {
		t.Errorf("with threshold 0.9, expected 2 items, got %d", len(result))
	}

	// With low threshold (0.7), item 2 should be removed
	result = SemanticDedup(items, embeddings, 0.7)
	if len(result) != 1 {
		t.Errorf("with threshold 0.7, expected 1 item, got %d", len(result))
	}
}

func TestSemanticDedupPreservesOrder(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "First", URL: "http://example.com/1"},
		{ID: "2", Title: "Second", URL: "http://example.com/2"},
		{ID: "3", Title: "Third", URL: "http://example.com/3"},
	}

	embeddings := map[string][]float32{
		"1": {1.0, 0.0, 0.0},
		"2": {0.0, 1.0, 0.0},
		"3": {0.0, 0.0, 1.0},
	}

	result := SemanticDedup(items, embeddings, 0.85)

	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}

	// Verify order is preserved
	expectedOrder := []string{"1", "2", "3"}
	for i, id := range expectedOrder {
		if result[i].ID != id {
			t.Errorf("item %d: expected ID %s, got %s", i, id, result[i].ID)
		}
	}
}

func TestSemanticDedupEmpty(t *testing.T) {
	result := SemanticDedup(nil, nil, 0.85)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	result = SemanticDedup([]store.Item{}, map[string][]float32{}, 0.85)
	if result == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestRerankByQuery(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Go Programming Tutorial"},
		{ID: "2", Title: "Python Basics"},
		{ID: "3", Title: "JavaScript Guide"},
		{ID: "4", Title: "Advanced Go Patterns"},
	}

	// Embeddings that simulate "Go" items being more similar to the query
	embeddings := map[string][]float32{
		"1": {1.0, 0.0, 0.0},   // Very similar to query
		"2": {0.1, 0.9, 0.3},   // Low similarity
		"3": {0.3, 0.3, 0.9},   // Medium similarity
		"4": {0.95, 0.1, 0.0},  // Very similar to query
	}

	// Query embedding close to items 1 and 4
	queryEmbedding := []float32{1.0, 0.0, 0.0}

	result := RerankByQuery(items, embeddings, queryEmbedding)

	if len(result) != 4 {
		t.Fatalf("expected 4 items (all items returned), got %d", len(result))
	}

	// Items 1 and 4 should be at the top (highest similarity)
	// Item 1 has similarity 1.0, Item 4 has similarity ~0.995
	if result[0].ID != "1" {
		t.Errorf("expected item 1 first (highest similarity), got %s", result[0].ID)
	}
	if result[1].ID != "4" {
		t.Errorf("expected item 4 second (second highest similarity), got %s", result[1].ID)
	}
}

func TestRerankByQueryNoEmbeddings(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "First"},
		{ID: "2", Title: "Second"},
		{ID: "3", Title: "Third"},
	}

	// No embeddings available
	embeddings := map[string][]float32{}

	queryEmbedding := []float32{1.0, 0.0, 0.0}

	result := RerankByQuery(items, embeddings, queryEmbedding)

	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}

	// Without embeddings, items should maintain original order
	if result[0].ID != "1" || result[1].ID != "2" || result[2].ID != "3" {
		t.Error("expected original order when no embeddings available")
	}
}

func TestRerankByQueryMixedEmbeddings(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Has Embedding"},
		{ID: "2", Title: "No Embedding"},
		{ID: "3", Title: "Also Has Embedding"},
		{ID: "4", Title: "Also No Embedding"},
	}

	// Only some items have embeddings
	embeddings := map[string][]float32{
		"1": {0.5, 0.5, 0.0},   // Medium similarity
		"3": {1.0, 0.0, 0.0},   // High similarity
	}

	queryEmbedding := []float32{1.0, 0.0, 0.0}

	result := RerankByQuery(items, embeddings, queryEmbedding)

	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}

	// Items with embeddings should come first, sorted by similarity
	// Item 3 has highest similarity (1.0), then Item 1 (~0.71)
	if result[0].ID != "3" {
		t.Errorf("expected item 3 first (highest similarity), got %s", result[0].ID)
	}
	if result[1].ID != "1" {
		t.Errorf("expected item 1 second (second highest similarity), got %s", result[1].ID)
	}

	// Items without embeddings should be at the end
	// They should maintain their relative original order
	noEmbedItems := result[2:]
	hasItem2 := false
	hasItem4 := false
	for _, item := range noEmbedItems {
		if item.ID == "2" {
			hasItem2 = true
		}
		if item.ID == "4" {
			hasItem4 = true
		}
	}
	if !hasItem2 || !hasItem4 {
		t.Error("items without embeddings should be at the end")
	}
}

func TestRerankByQueryEmptyQuery(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "First"},
		{ID: "2", Title: "Second"},
	}

	embeddings := map[string][]float32{
		"1": {1.0, 0.0},
		"2": {0.0, 1.0},
	}

	// Empty query embedding
	result := RerankByQuery(items, embeddings, []float32{})

	// Should return items unchanged
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].ID != "1" || result[1].ID != "2" {
		t.Error("expected original order with empty query embedding")
	}

	// Nil query embedding
	result = RerankByQuery(items, embeddings, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
}

func TestRerankByQueryEmptyItems(t *testing.T) {
	embeddings := map[string][]float32{}
	queryEmbedding := []float32{1.0, 0.0, 0.0}

	result := RerankByQuery([]store.Item{}, embeddings, queryEmbedding)
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	result = RerankByQuery(nil, embeddings, queryEmbedding)
	if result != nil {
		t.Error("expected nil for nil input")
	}
}

// mockReranker implements Reranker for testing.
type mockReranker struct {
	available bool
	scores    []Score
	err       error
}

func (m *mockReranker) Available() bool {
	return m.available
}

func (m *mockReranker) Rerank(ctx context.Context, query string, documents []string) ([]Score, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.scores, nil
}

func TestRerankByCrossEncoder(t *testing.T) {
	now := time.Now()
	items := []store.Item{
		{ID: "1", Title: "Football game today", Published: now},
		{ID: "2", Title: "Stock market rally", Published: now},
		{ID: "3", Title: "Super Bowl preview", Published: now},
		{ID: "4", Title: "Weather forecast", Published: now},
	}

	reranker := &mockReranker{
		available: true,
		scores: []Score{
			{Index: 0, Score: 0.3},  // Football - medium relevant
			{Index: 1, Score: 0.1},  // Stock - not relevant
			{Index: 2, Score: 0.9},  // Super Bowl - highly relevant
			{Index: 3, Score: 0.05}, // Weather - not relevant
		},
	}

	ctx := context.Background()
	result := RerankByCrossEncoder(ctx, items, "super bowl", reranker)

	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}

	// Super Bowl should be first (highest score)
	if result[0].ID != "3" {
		t.Errorf("expected Super Bowl (id=3) first, got %s", result[0].ID)
	}

	// Football should be second
	if result[1].ID != "1" {
		t.Errorf("expected Football (id=1) second, got %s", result[1].ID)
	}
}

func TestRerankByCrossEncoderUnavailable(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}

	reranker := &mockReranker{
		available: false,
	}

	ctx := context.Background()
	result := RerankByCrossEncoder(ctx, items, "query", reranker)

	// Should return items unchanged
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].ID != "1" || result[1].ID != "2" {
		t.Error("expected items to be returned unchanged")
	}
}

func TestRerankByCrossEncoderNilReranker(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Item 1"},
	}

	ctx := context.Background()
	result := RerankByCrossEncoder(ctx, items, "query", nil)

	// Should return items unchanged
	if len(result) != 1 || result[0].ID != "1" {
		t.Error("expected items to be returned unchanged with nil reranker")
	}
}

func TestRerankByCrossEncoderEmpty(t *testing.T) {
	reranker := &mockReranker{available: true}
	ctx := context.Background()

	result := RerankByCrossEncoder(ctx, []store.Item{}, "query", reranker)
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}

	result = RerankByCrossEncoder(ctx, nil, "query", reranker)
	if result != nil {
		t.Error("expected nil for nil input")
	}
}

func TestRerankByCrossEncoderError(t *testing.T) {
	items := []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}

	reranker := &mockReranker{
		available: true,
		err:       context.DeadlineExceeded,
	}

	ctx := context.Background()
	result := RerankByCrossEncoder(ctx, items, "query", reranker)

	// Should return items unchanged on error
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0].ID != "1" || result[1].ID != "2" {
		t.Error("expected items to be returned unchanged on error")
	}
}
