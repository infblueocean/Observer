package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Sample RSS feed for testing
const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com</link>
    <description>A test RSS feed</description>
    <item>
      <title>First Article</title>
      <link>https://example.com/article/1</link>
      <description>This is the first article.</description>
      <pubDate>Mon, 27 Jan 2026 12:00:00 GMT</pubDate>
      <guid>unique-id-1</guid>
      <author>John Doe</author>
    </item>
    <item>
      <title>Second Article</title>
      <link>https://example.com/article/2</link>
      <description>This is the second article.</description>
      <pubDate>Mon, 27 Jan 2026 11:00:00 GMT</pubDate>
      <guid>unique-id-2</guid>
    </item>
  </channel>
</rss>`

// Sample Atom feed for testing
const sampleAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Test Atom Feed</title>
  <link href="https://example.com"/>
  <id>urn:uuid:test-feed-id</id>
  <updated>2026-01-27T12:00:00Z</updated>
  <entry>
    <title>Atom Entry One</title>
    <link href="https://example.com/atom/1"/>
    <id>urn:uuid:atom-entry-1</id>
    <updated>2026-01-27T12:00:00Z</updated>
    <published>2026-01-27T12:00:00Z</published>
    <summary>Summary of the first Atom entry.</summary>
    <author>
      <name>Jane Smith</name>
    </author>
  </entry>
  <entry>
    <title>Atom Entry Two</title>
    <link href="https://example.com/atom/2"/>
    <id>urn:uuid:atom-entry-2</id>
    <updated>2026-01-27T11:30:00Z</updated>
    <content type="html">Full content of the second entry.</content>
  </entry>
</feed>`

// Non-XML content for error testing (gofeed is lenient with malformed XML,
// so we use completely invalid content to test error handling)
const invalidContent = `This is not XML at all.
Just plain text that cannot be parsed as any feed format.
No RSS, no Atom, no JSON feed - nothing.`

func TestFetchRSS(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(sampleRSS))
	}))
	defer server.Close()

	// Create fetcher and source
	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss",
		Name: "Test Feed",
		URL:  server.URL,
	}

	// Fetch items
	ctx := context.Background()
	items, err := fetcher.Fetch(ctx, src)

	// Assertions
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Check first item
	if items[0].Title != "First Article" {
		t.Errorf("expected title 'First Article', got '%s'", items[0].Title)
	}

	if items[0].URL != "https://example.com/article/1" {
		t.Errorf("expected URL 'https://example.com/article/1', got '%s'", items[0].URL)
	}

	if items[0].Summary != "This is the first article." {
		t.Errorf("expected summary 'This is the first article.', got '%s'", items[0].Summary)
	}

	if items[0].SourceType != "rss" {
		t.Errorf("expected SourceType 'rss', got '%s'", items[0].SourceType)
	}

	if items[0].SourceName != "Test Feed" {
		t.Errorf("expected SourceName 'Test Feed', got '%s'", items[0].SourceName)
	}

	// Check that ID is deterministic (based on GUID)
	if items[0].ID == "" {
		t.Error("expected non-empty ID")
	}

	// Fetch again - ID should be the same (deterministic)
	items2, err := fetcher.Fetch(ctx, src)
	if err != nil {
		t.Fatalf("unexpected error on second fetch: %v", err)
	}

	if items[0].ID != items2[0].ID {
		t.Errorf("ID should be deterministic: first=%s, second=%s", items[0].ID, items2[0].ID)
	}
}

func TestFetchAtom(t *testing.T) {
	// Create test server with Atom feed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		w.Write([]byte(sampleAtom))
	}))
	defer server.Close()

	// Create fetcher and source
	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss", // Generic type, gofeed handles Atom transparently
		Name: "Atom Test Feed",
		URL:  server.URL,
	}

	// Fetch items
	ctx := context.Background()
	items, err := fetcher.Fetch(ctx, src)

	// Assertions
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Check first Atom entry
	if items[0].Title != "Atom Entry One" {
		t.Errorf("expected title 'Atom Entry One', got '%s'", items[0].Title)
	}

	if items[0].URL != "https://example.com/atom/1" {
		t.Errorf("expected URL 'https://example.com/atom/1', got '%s'", items[0].URL)
	}

	if items[0].Summary != "Summary of the first Atom entry." {
		t.Errorf("expected summary from Atom entry, got '%s'", items[0].Summary)
	}

	if items[0].Author != "Jane Smith" {
		t.Errorf("expected author 'Jane Smith', got '%s'", items[0].Author)
	}

	// Check that published time was parsed
	expectedPub := time.Date(2026, 1, 27, 12, 0, 0, 0, time.UTC)
	if !items[0].Published.Equal(expectedPub) {
		t.Errorf("expected published time %v, got %v", expectedPub, items[0].Published)
	}

	// Check second entry uses content as summary when description is missing
	if items[1].Summary != "Full content of the second entry." {
		t.Errorf("expected content as summary fallback, got '%s'", items[1].Summary)
	}
}

func TestFetchMalformedRSS(t *testing.T) {
	// Create test server with unparseable content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(invalidContent))
	}))
	defer server.Close()

	// Create fetcher and source
	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss",
		Name: "Broken Feed",
		URL:  server.URL,
	}

	// Fetch should return error for unparseable content
	ctx := context.Background()
	_, err := fetcher.Fetch(ctx, src)

	if err == nil {
		t.Error("expected error for invalid content, got nil")
	}
}

func TestFetchTimeout(t *testing.T) {
	// Create test server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait longer than the timeout
		select {
		case <-r.Context().Done():
			// Context cancelled, stop waiting
			return
		case <-time.After(5 * time.Second):
			w.Write([]byte(sampleRSS))
		}
	}))
	defer server.Close()

	// Create fetcher and source
	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss",
		Name: "Slow Feed",
		URL:  server.URL,
	}

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Fetch should timeout
	_, err := fetcher.Fetch(ctx, src)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	// Verify it's a context error
	if ctx.Err() == nil {
		t.Error("expected context to be cancelled")
	}
}

func TestFetchContextCancellation(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait for context cancellation
		<-r.Context().Done()
	}))
	defer server.Close()

	// Create fetcher and source
	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss",
		Name: "Test Feed",
		URL:  server.URL,
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	// Fetch should return context cancelled error
	_, err := fetcher.Fetch(ctx, src)

	if err == nil {
		t.Error("expected error when context is cancelled, got nil")
	}
}

func TestFetchHTTPError404(t *testing.T) {
	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	// Create fetcher and source
	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss",
		Name: "Missing Feed",
		URL:  server.URL,
	}

	// Fetch should return HTTP error
	ctx := context.Background()
	_, err := fetcher.Fetch(ctx, src)

	if err == nil {
		t.Error("expected HTTP error for 404, got nil")
	}

	// Error message should contain status code
	if err.Error() != "HTTP error: 404 404 Not Found" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFetchHTTPError500(t *testing.T) {
	// Create test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	// Create fetcher and source
	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss",
		Name: "Error Feed",
		URL:  server.URL,
	}

	// Fetch should return HTTP error
	ctx := context.Background()
	_, err := fetcher.Fetch(ctx, src)

	if err == nil {
		t.Error("expected HTTP error for 500, got nil")
	}

	// Error message should contain status code
	if err.Error() != "HTTP error: 500 500 Internal Server Error" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateID(t *testing.T) {
	// Test ID generation is deterministic
	id1 := hashString("test-guid-123")
	id2 := hashString("test-guid-123")

	if id1 != id2 {
		t.Errorf("hashString should be deterministic: %s != %s", id1, id2)
	}

	// Test different inputs produce different IDs
	id3 := hashString("different-guid")

	if id1 == id3 {
		t.Error("different inputs should produce different IDs")
	}

	// Test ID is reasonable length (16 hex chars = 8 bytes)
	if len(id1) != 16 {
		t.Errorf("expected ID length 16, got %d", len(id1))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"", 10, ""},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestFetchEmptyFeed(t *testing.T) {
	// Create test server with empty feed
	emptyRSS := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Empty Feed</title>
    <link>https://example.com</link>
  </channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(emptyRSS))
	}))
	defer server.Close()

	// Create fetcher and source
	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss",
		Name: "Empty Feed",
		URL:  server.URL,
	}

	// Fetch should succeed but return no items
	ctx := context.Background()
	items, err := fetcher.Fetch(ctx, src)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("expected 0 items from empty feed, got %d", len(items))
	}
}

func TestFetchSetsCorrectFetchedTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(sampleRSS))
	}))
	defer server.Close()

	fetcher := NewFetcher(10 * time.Second)
	src := Source{
		Type: "rss",
		Name: "Test Feed",
		URL:  server.URL,
	}

	beforeFetch := time.Now()
	ctx := context.Background()
	items, err := fetcher.Fetch(ctx, src)
	afterFetch := time.Now()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All items should have Fetched time within our window
	for _, item := range items {
		if item.Fetched.Before(beforeFetch) || item.Fetched.After(afterFetch) {
			t.Errorf("item Fetched time %v outside expected range [%v, %v]",
				item.Fetched, beforeFetch, afterFetch)
		}
	}
}
