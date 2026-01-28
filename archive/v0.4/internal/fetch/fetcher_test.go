package fetch

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abelbrown/observer/internal/model"
)

func TestRSSFetcherName(t *testing.T) {
	fetcher := NewRSSFetcher("Test Feed", "http://example.com/feed.xml")
	if fetcher.Name() != "Test Feed" {
		t.Errorf("expected 'Test Feed', got %s", fetcher.Name())
	}
}

func TestRSSFetcherFetch(t *testing.T) {
	// Create a mock RSS server
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Article 1</title>
      <link>http://example.com/article1</link>
      <description>First article</description>
      <pubDate>Mon, 01 Jan 2024 12:00:00 GMT</pubDate>
    </item>
    <item>
      <title>Article 2</title>
      <link>http://example.com/article2</link>
      <description>Second article</description>
      <pubDate>Mon, 01 Jan 2024 11:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(rss))
	}))
	defer server.Close()

	fetcher := NewRSSFetcher("Test Feed", server.URL)
	items, err := fetcher.Fetch()
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Verify first item
	if items[0].Title != "Article 1" {
		t.Errorf("expected 'Article 1', got %s", items[0].Title)
	}
	if items[0].URL != "http://example.com/article1" {
		t.Errorf("unexpected URL: %s", items[0].URL)
	}
	if items[0].Source != model.SourceRSS {
		t.Errorf("expected SourceRSS, got %v", items[0].Source)
	}
	if items[0].SourceName != "Test Feed" {
		t.Errorf("expected 'Test Feed', got %s", items[0].SourceName)
	}
}

func TestRSSFetcherFetchError(t *testing.T) {
	// Non-existent server
	fetcher := NewRSSFetcher("Bad Feed", "http://localhost:99999/nonexistent")
	_, err := fetcher.Fetch()
	if err == nil {
		t.Error("expected error for non-existent server")
	}
}

func TestRSSFetcherFetch404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	fetcher := NewRSSFetcher("Test Feed", server.URL)
	_, err := fetcher.Fetch()
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestRSSFetcherFetchInvalidXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte("not valid xml"))
	}))
	defer server.Close()

	fetcher := NewRSSFetcher("Test Feed", server.URL)
	_, err := fetcher.Fetch()
	if err == nil {
		t.Error("expected error for invalid XML")
	}
}

func TestRSSFetcherIDGeneration(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test</title>
    <item>
      <title>Article</title>
      <link>http://example.com/unique-url</link>
    </item>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(rss))
	}))
	defer server.Close()

	fetcher := NewRSSFetcher("Test", server.URL)

	// Fetch twice - IDs should be deterministic
	items1, _ := fetcher.Fetch()
	items2, _ := fetcher.Fetch()

	if items1[0].ID != items2[0].ID {
		t.Error("IDs should be deterministic for same URL")
	}
}

func TestDefaultSources(t *testing.T) {
	sources := DefaultSources()

	if len(sources) == 0 {
		t.Error("expected non-empty source list")
	}

	// Verify all sources have required fields
	for _, src := range sources {
		if src.Name == "" {
			t.Error("source has empty name")
		}
		if src.URL == "" {
			t.Errorf("source %s has empty URL", src.Name)
		}
		if src.RefreshMinutes <= 0 {
			t.Errorf("source %s has invalid refresh interval", src.Name)
		}
	}
}

func TestCreateFetcher(t *testing.T) {
	cfg := SourceConfig{
		Name: "Test",
		URL:  "http://example.com/feed",
		Type: model.SourceRSS,
	}

	fetcher := CreateFetcher(cfg)
	if fetcher.Name() != "Test" {
		t.Errorf("expected 'Test', got %s", fetcher.Name())
	}
}

func TestCreateFetcherUnknownType(t *testing.T) {
	// Unknown types should default to RSS
	cfg := SourceConfig{
		Name: "Test",
		URL:  "http://example.com/feed",
		Type: model.SourceType("unknown"),
	}

	fetcher := CreateFetcher(cfg)
	if fetcher == nil {
		t.Error("expected fetcher even for unknown type")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hi", 2, "hi"},
		{"hi", 1, "h"},
		{"", 5, ""},
	}

	for _, tc := range tests {
		result := truncate(tc.input, tc.maxLen)
		if result != tc.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, result, tc.expected)
		}
	}
}
