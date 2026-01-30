package fetch

import (
	"testing"
	"time"

	"github.com/infblueocean/clarion"
)

func TestConvertItem_AllFields(t *testing.T) {
	now := time.Now().UTC()
	ci := clarion.Item{
		ID:         "guid-123",
		Source:     "test",
		SourceName: "Test Source",
		SourceType: clarion.SourceTypeRSS,
		Title:      "Test Article",
		Summary:    "A summary",
		URL:        "https://example.com/article",
		Author:     "Jane Doe",
		Published:  now,
		Fetched:    now,
	}

	item := convertItem(ci)

	if item.ID == "" {
		t.Error("expected non-empty ID")
	}
	if item.SourceType != "rss" {
		t.Errorf("expected SourceType 'rss', got %q", item.SourceType)
	}
	if item.SourceName != "Test Source" {
		t.Errorf("expected SourceName 'Test Source', got %q", item.SourceName)
	}
	if item.Title != "Test Article" {
		t.Errorf("expected Title 'Test Article', got %q", item.Title)
	}
	if item.Summary != "A summary" {
		t.Errorf("expected Summary 'A summary', got %q", item.Summary)
	}
	if item.URL != "https://example.com/article" {
		t.Errorf("expected URL, got %q", item.URL)
	}
	if item.Author != "Jane Doe" {
		t.Errorf("expected Author 'Jane Doe', got %q", item.Author)
	}
	if !item.Published.Equal(now) {
		t.Errorf("expected Published %v, got %v", now, item.Published)
	}
}

func TestConvertItem_EmptyIDFallback(t *testing.T) {
	// Falls back to URL when ID is empty
	ci := clarion.Item{
		ID:      "",
		URL:     "https://example.com/article",
		Title:   "Test",
		Fetched: time.Now(),
	}
	item := convertItem(ci)
	expected := hashString("https://example.com/article")
	if item.ID != expected {
		t.Errorf("expected ID from URL hash %q, got %q", expected, item.ID)
	}

	// Falls back to Title when both ID and URL are empty
	ci2 := clarion.Item{
		ID:      "",
		URL:     "",
		Title:   "Some Title",
		Fetched: time.Now(),
	}
	item2 := convertItem(ci2)
	expected2 := hashString("Some Title")
	if item2.ID != expected2 {
		t.Errorf("expected ID from Title hash %q, got %q", expected2, item2.ID)
	}
}

func TestConvertItem_SummaryFallback(t *testing.T) {
	ci := clarion.Item{
		ID:      "test",
		Summary: "",
		Content: "This is the full content of the article that should be truncated to a summary.",
		Fetched: time.Now(),
	}

	item := convertItem(ci)
	if item.Summary == "" {
		t.Error("expected summary to be populated from Content")
	}
	if item.Summary != ci.Content {
		// Content is short enough that it shouldn't be truncated
		t.Errorf("expected summary to match content, got %q", item.Summary)
	}
}

func TestConvertItem_AuthorsFallback(t *testing.T) {
	ci := clarion.Item{
		ID:      "test",
		Author:  "",
		Authors: []string{"Alice", "Bob"},
		Fetched: time.Now(),
	}

	item := convertItem(ci)
	if item.Author != "Alice" {
		t.Errorf("expected Author 'Alice' from Authors fallback, got %q", item.Author)
	}
}

func TestConvertItem_HashDeterminism(t *testing.T) {
	ci := clarion.Item{
		ID:      "guid-abc",
		Fetched: time.Now(),
	}

	item1 := convertItem(ci)
	item2 := convertItem(ci)

	if item1.ID != item2.ID {
		t.Errorf("expected deterministic ID: %q != %q", item1.ID, item2.ID)
	}
}

// --- truncate() tests ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short_string", "hello", 10, "hello"},
		{"exact_length", "hello", 5, "hello"},
		{"needs_truncation", "hello world", 8, "hello..."},
		{"maxLen_3", "hello", 3, "hel"},
		{"maxLen_1", "hello", 1, "h"},
		{"maxLen_4", "hello", 4, "h..."},
		{"empty_string", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// --- NewClarionProvider tests ---

func TestNewClarionProvider_NilSources(t *testing.T) {
	p := NewClarionProvider(nil, clarion.FetchOptions{}, nil)
	if len(p.sources) == 0 {
		t.Fatal("expected non-empty sources when nil passed (should use AllSources)")
	}
	allSources := clarion.AllSources()
	if len(p.sources) != len(allSources) {
		t.Errorf("sources count = %d, want %d (AllSources)", len(p.sources), len(allSources))
	}
}

func TestNewClarionProvider_ExplicitSources(t *testing.T) {
	src := clarion.AllSources()
	if len(src) == 0 {
		t.Skip("no clarion sources available")
	}
	p := NewClarionProvider(src[:1], clarion.FetchOptions{}, nil)
	if len(p.sources) != 1 {
		t.Errorf("sources count = %d, want 1", len(p.sources))
	}
}

func TestNewClarionProvider_NilLogger(t *testing.T) {
	p := NewClarionProvider(nil, clarion.FetchOptions{}, nil)
	if p.logger == nil {
		t.Error("expected non-nil logger when nil passed (should use NullLogger)")
	}
}
