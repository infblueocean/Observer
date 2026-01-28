package model

import (
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

func TestFromFeedsItem(t *testing.T) {
	now := time.Now()
	fetched := now.Add(-5 * time.Minute)

	f := feeds.Item{
		ID:         "test-123",
		Source:     feeds.SourceRSS,
		SourceName: "Test Source",
		SourceURL:  "https://example.com/feed.xml",
		Title:      "Test Title",
		Summary:    "Test Summary",
		Content:    "Full content here",
		URL:        "https://example.com/article",
		Author:     "Test Author",
		Published:  now,
		Fetched:    fetched,
		Read:       true,
		Saved:      false,
	}

	m := FromFeedsItem(f)

	if m.ID != f.ID {
		t.Errorf("ID mismatch: got %s, want %s", m.ID, f.ID)
	}
	if m.Source != SourceRSS {
		t.Errorf("Source mismatch: got %s, want %s", m.Source, SourceRSS)
	}
	if m.SourceName != f.SourceName {
		t.Errorf("SourceName mismatch: got %s, want %s", m.SourceName, f.SourceName)
	}
	if m.SourceURL != f.SourceURL {
		t.Errorf("SourceURL mismatch: got %s, want %s", m.SourceURL, f.SourceURL)
	}
	if m.Title != f.Title {
		t.Errorf("Title mismatch: got %s, want %s", m.Title, f.Title)
	}
	if m.Summary != f.Summary {
		t.Errorf("Summary mismatch: got %s, want %s", m.Summary, f.Summary)
	}
	if m.Content != f.Content {
		t.Errorf("Content mismatch: got %s, want %s", m.Content, f.Content)
	}
	if m.URL != f.URL {
		t.Errorf("URL mismatch: got %s, want %s", m.URL, f.URL)
	}
	if m.Author != f.Author {
		t.Errorf("Author mismatch: got %s, want %s", m.Author, f.Author)
	}
	if !m.Published.Equal(f.Published) {
		t.Errorf("Published mismatch: got %v, want %v", m.Published, f.Published)
	}
	if !m.Fetched.Equal(f.Fetched) {
		t.Errorf("Fetched mismatch: got %v, want %v", m.Fetched, f.Fetched)
	}
	if m.Read != f.Read {
		t.Errorf("Read mismatch: got %v, want %v", m.Read, f.Read)
	}
	if m.Saved != f.Saved {
		t.Errorf("Saved mismatch: got %v, want %v", m.Saved, f.Saved)
	}
	if m.Embedding != nil {
		t.Errorf("Embedding should be nil, got %v", m.Embedding)
	}
}

func TestToFeedsItem(t *testing.T) {
	now := time.Now()
	fetched := now.Add(-5 * time.Minute)

	m := Item{
		ID:         "model-456",
		Source:     SourceHN,
		SourceName: "Hacker News",
		SourceURL:  "https://news.ycombinator.com",
		Title:      "Model Title",
		Summary:    "Model Summary",
		Content:    "Model Content",
		URL:        "https://example.com/story",
		Author:     "Model Author",
		Published:  now,
		Fetched:    fetched,
		Read:       false,
		Saved:      true,
		Embedding:  []float32{1.0, 2.0, 3.0},
	}

	f := m.ToFeedsItem()

	if f.ID != m.ID {
		t.Errorf("ID mismatch: got %s, want %s", f.ID, m.ID)
	}
	if f.Source != feeds.SourceHN {
		t.Errorf("Source mismatch: got %s, want %s", f.Source, feeds.SourceHN)
	}
	if f.SourceName != m.SourceName {
		t.Errorf("SourceName mismatch: got %s, want %s", f.SourceName, m.SourceName)
	}
	if f.SourceURL != m.SourceURL {
		t.Errorf("SourceURL mismatch: got %s, want %s", f.SourceURL, m.SourceURL)
	}
	if f.Title != m.Title {
		t.Errorf("Title mismatch: got %s, want %s", f.Title, m.Title)
	}
	if f.Summary != m.Summary {
		t.Errorf("Summary mismatch: got %s, want %s", f.Summary, m.Summary)
	}
	if f.Content != m.Content {
		t.Errorf("Content mismatch: got %s, want %s", f.Content, m.Content)
	}
	if f.URL != m.URL {
		t.Errorf("URL mismatch: got %s, want %s", f.URL, m.URL)
	}
	if f.Author != m.Author {
		t.Errorf("Author mismatch: got %s, want %s", f.Author, m.Author)
	}
	if !f.Published.Equal(m.Published) {
		t.Errorf("Published mismatch: got %v, want %v", f.Published, m.Published)
	}
	if !f.Fetched.Equal(m.Fetched) {
		t.Errorf("Fetched mismatch: got %v, want %v", f.Fetched, m.Fetched)
	}
	if f.Read != m.Read {
		t.Errorf("Read mismatch: got %v, want %v", f.Read, m.Read)
	}
	if f.Saved != m.Saved {
		t.Errorf("Saved mismatch: got %v, want %v", f.Saved, m.Saved)
	}
}

func TestRoundTrip(t *testing.T) {
	// Test feeds → model → feeds preserves all data (except Embedding)
	now := time.Now()
	fetched := now.Add(-10 * time.Minute)

	original := feeds.Item{
		ID:         "roundtrip-789",
		Source:     feeds.SourceReddit,
		SourceName: "r/golang",
		SourceURL:  "https://reddit.com/r/golang",
		Title:      "Round Trip Title",
		Summary:    "Round Trip Summary",
		Content:    "Round Trip Content",
		URL:        "https://reddit.com/r/golang/comments/abc123",
		Author:     "reddit_user",
		Published:  now,
		Fetched:    fetched,
		Read:       true,
		Saved:      true,
	}

	// Convert feeds → model → feeds
	modelItem := FromFeedsItem(original)
	result := modelItem.ToFeedsItem()

	// Verify all fields are preserved
	if result.ID != original.ID {
		t.Errorf("ID not preserved: got %s, want %s", result.ID, original.ID)
	}
	if result.Source != original.Source {
		t.Errorf("Source not preserved: got %s, want %s", result.Source, original.Source)
	}
	if result.SourceName != original.SourceName {
		t.Errorf("SourceName not preserved: got %s, want %s", result.SourceName, original.SourceName)
	}
	if result.SourceURL != original.SourceURL {
		t.Errorf("SourceURL not preserved: got %s, want %s", result.SourceURL, original.SourceURL)
	}
	if result.Title != original.Title {
		t.Errorf("Title not preserved: got %s, want %s", result.Title, original.Title)
	}
	if result.Summary != original.Summary {
		t.Errorf("Summary not preserved: got %s, want %s", result.Summary, original.Summary)
	}
	if result.Content != original.Content {
		t.Errorf("Content not preserved: got %s, want %s", result.Content, original.Content)
	}
	if result.URL != original.URL {
		t.Errorf("URL not preserved: got %s, want %s", result.URL, original.URL)
	}
	if result.Author != original.Author {
		t.Errorf("Author not preserved: got %s, want %s", result.Author, original.Author)
	}
	if !result.Published.Equal(original.Published) {
		t.Errorf("Published not preserved: got %v, want %v", result.Published, original.Published)
	}
	if !result.Fetched.Equal(original.Fetched) {
		t.Errorf("Fetched not preserved: got %v, want %v", result.Fetched, original.Fetched)
	}
	if result.Read != original.Read {
		t.Errorf("Read not preserved: got %v, want %v", result.Read, original.Read)
	}
	if result.Saved != original.Saved {
		t.Errorf("Saved not preserved: got %v, want %v", result.Saved, original.Saved)
	}
}

func TestFromFeedsItems(t *testing.T) {
	now := time.Now()

	feedItems := []feeds.Item{
		{
			ID:         "item-1",
			Source:     feeds.SourceRSS,
			SourceName: "Source 1",
			Title:      "Title 1",
			Published:  now,
		},
		{
			ID:         "item-2",
			Source:     feeds.SourceHN,
			SourceName: "Hacker News",
			Title:      "Title 2",
			Published:  now.Add(-time.Hour),
		},
		{
			ID:         "item-3",
			Source:     feeds.SourceUSGS,
			SourceName: "USGS Earthquakes",
			Title:      "Title 3",
			Published:  now.Add(-2 * time.Hour),
		},
	}

	modelItems := FromFeedsItems(feedItems)

	if len(modelItems) != len(feedItems) {
		t.Fatalf("Length mismatch: got %d, want %d", len(modelItems), len(feedItems))
	}

	for i, mi := range modelItems {
		fi := feedItems[i]
		if mi.ID != fi.ID {
			t.Errorf("Item %d ID mismatch: got %s, want %s", i, mi.ID, fi.ID)
		}
		if mi.Title != fi.Title {
			t.Errorf("Item %d Title mismatch: got %s, want %s", i, mi.Title, fi.Title)
		}
		if mi.SourceName != fi.SourceName {
			t.Errorf("Item %d SourceName mismatch: got %s, want %s", i, mi.SourceName, fi.SourceName)
		}
		if mi.Embedding != nil {
			t.Errorf("Item %d Embedding should be nil", i)
		}
	}
}

func TestToFeedsItems(t *testing.T) {
	now := time.Now()

	modelItems := []Item{
		{
			ID:         "model-1",
			Source:     SourceRSS,
			SourceName: "Source 1",
			Title:      "Title 1",
			Published:  now,
			Embedding:  []float32{1.0, 2.0},
		},
		{
			ID:         "model-2",
			Source:     SourceHN,
			SourceName: "Hacker News",
			Title:      "Title 2",
			Published:  now.Add(-time.Hour),
			Embedding:  []float32{3.0, 4.0},
		},
	}

	feedItems := ToFeedsItems(modelItems)

	if len(feedItems) != len(modelItems) {
		t.Fatalf("Length mismatch: got %d, want %d", len(feedItems), len(modelItems))
	}

	for i, fi := range feedItems {
		mi := modelItems[i]
		if fi.ID != mi.ID {
			t.Errorf("Item %d ID mismatch: got %s, want %s", i, fi.ID, mi.ID)
		}
		if fi.Title != mi.Title {
			t.Errorf("Item %d Title mismatch: got %s, want %s", i, fi.Title, mi.Title)
		}
		if fi.SourceName != mi.SourceName {
			t.Errorf("Item %d SourceName mismatch: got %s, want %s", i, fi.SourceName, mi.SourceName)
		}
	}
}

func TestFromFeedsItemsEmpty(t *testing.T) {
	result := FromFeedsItems([]feeds.Item{})

	if len(result) != 0 {
		t.Errorf("Expected empty slice, got length %d", len(result))
	}
	if result == nil {
		t.Error("Expected non-nil empty slice, got nil")
	}
}

func TestToFeedsItemsEmpty(t *testing.T) {
	result := ToFeedsItems([]Item{})

	if len(result) != 0 {
		t.Errorf("Expected empty slice, got length %d", len(result))
	}
	if result == nil {
		t.Error("Expected non-nil empty slice, got nil")
	}
}

func TestEmbeddingLostOnRoundTrip(t *testing.T) {
	// Document that embedding is nil after round-trip conversion.
	// This is expected behavior since feeds.Item doesn't have an Embedding field.
	m := Item{
		ID:        "embed-test",
		Source:    SourceRSS,
		Title:     "Test with Embedding",
		Embedding: []float32{1.0, 2.0, 3.0, 4.0, 5.0},
	}

	// Verify original has embedding
	if len(m.Embedding) != 5 {
		t.Fatalf("Expected embedding length 5, got %d", len(m.Embedding))
	}

	// Convert to feeds.Item and back
	f := m.ToFeedsItem()
	m2 := FromFeedsItem(f)

	// Embedding should be nil after round-trip
	if m2.Embedding != nil {
		t.Errorf("Expected Embedding to be nil after round-trip, got %v", m2.Embedding)
	}

	// Other fields should be preserved
	if m2.ID != m.ID {
		t.Errorf("ID not preserved: got %s, want %s", m2.ID, m.ID)
	}
	if m2.Title != m.Title {
		t.Errorf("Title not preserved: got %s, want %s", m2.Title, m.Title)
	}
}

func TestSourceTypeConversion(t *testing.T) {
	// Test that all source types convert correctly between feeds and model
	testCases := []struct {
		feedsType feeds.SourceType
		modelType SourceType
	}{
		{feeds.SourceRSS, SourceRSS},
		{feeds.SourceReddit, SourceReddit},
		{feeds.SourceHN, SourceHN},
		{feeds.SourceUSGS, SourceUSGS},
		{feeds.SourceMastodon, SourceMastodon},
		{feeds.SourceBluesky, SourceBluesky},
		{feeds.SourceArXiv, SourceArXiv},
		{feeds.SourceSEC, SourceSEC},
		{feeds.SourceAggregator, SourceAggregator},
		{feeds.SourcePolymarket, SourcePolymarket},
		{feeds.SourceManifold, SourceManifold},
	}

	for _, tc := range testCases {
		t.Run(string(tc.feedsType), func(t *testing.T) {
			// feeds → model
			fi := feeds.Item{ID: "test", Source: tc.feedsType}
			mi := FromFeedsItem(fi)
			if mi.Source != tc.modelType {
				t.Errorf("feeds→model: got %s, want %s", mi.Source, tc.modelType)
			}

			// model → feeds
			mi2 := Item{ID: "test", Source: tc.modelType}
			fi2 := mi2.ToFeedsItem()
			if fi2.Source != tc.feedsType {
				t.Errorf("model→feeds: got %s, want %s", fi2.Source, tc.feedsType)
			}
		})
	}
}
