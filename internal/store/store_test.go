package store

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

// StoreInterface is used ONLY for testing UI components.
// It defines the subset of Store methods that the UI layer needs.
type StoreInterface interface {
	GetItems(limit int, includeRead bool) ([]Item, error)
	GetItemsSince(since time.Time) ([]Item, error)
	MarkRead(id string) error
	MarkSaved(id string, saved bool) error
	SaveItems(items []Item) (int, error)
}

// Verify Store implements StoreInterface at compile time.
var _ StoreInterface = (*Store)(nil)

func TestOpen(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	// Verify tables exist by querying them
	var name string
	err = st.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='items'").Scan(&name)
	if err != nil {
		t.Fatalf("items table not created: %v", err)
	}
	if name != "items" {
		t.Errorf("expected table name 'items', got %q", name)
	}
}

func TestSaveItems(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	items := []Item{
		{
			ID:         "item1",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Test Title 1",
			Summary:    "Test Summary 1",
			URL:        "https://example.com/1",
			Author:     "Author 1",
			Published:  now,
			Fetched:    now,
			Read:       false,
			Saved:      false,
		},
		{
			ID:         "item2",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Test Title 2",
			Summary:    "Test Summary 2",
			URL:        "https://example.com/2",
			Author:     "Author 2",
			Published:  now.Add(-time.Hour),
			Fetched:    now,
			Read:       false,
			Saved:      false,
		},
	}

	count, err := st.SaveItems(items)
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 new items, got %d", count)
	}

	// Verify items were saved
	got, err := st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 items, got %d", len(got))
	}
}

func TestSaveItemsDuplicate(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	item := Item{
		ID:         "item1",
		SourceType: "rss",
		SourceName: "Test Feed",
		Title:      "Test Title",
		Summary:    "Test Summary",
		URL:        "https://example.com/1",
		Author:     "Author",
		Published:  now,
		Fetched:    now,
	}

	// Insert first time
	count, err := st.SaveItems([]Item{item})
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 new item, got %d", count)
	}

	// Insert duplicate (same URL)
	item.ID = "item2" // Different ID, same URL
	item.Title = "Different Title"
	count, err = st.SaveItems([]Item{item})
	if err != nil {
		t.Fatalf("SaveItems duplicate failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 new items (duplicate URL), got %d", count)
	}

	// Verify only 1 item in database
	got, err := st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 item, got %d", len(got))
	}
	// Original title should be preserved (INSERT OR IGNORE doesn't update)
	if got[0].Title != "Test Title" {
		t.Errorf("expected original title 'Test Title', got %q", got[0].Title)
	}
}

func TestGetItems(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	items := make([]Item, 5)
	for i := 0; i < 5; i++ {
		items[i] = Item{
			ID:         fmt.Sprintf("item%d", i),
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      fmt.Sprintf("Title %d", i),
			URL:        fmt.Sprintf("https://example.com/%d", i),
			Published:  now.Add(-time.Duration(i) * time.Hour),
			Fetched:    now,
		}
	}

	_, err = st.SaveItems(items)
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Test with limit
	got, err := st.GetItems(3, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 items, got %d", len(got))
	}

	// Verify order (most recent first)
	if got[0].ID != "item0" {
		t.Errorf("expected first item to be item0, got %s", got[0].ID)
	}
}

func TestGetItemsIncludeRead(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	items := []Item{
		{
			ID:         "unread",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Unread Item",
			URL:        "https://example.com/unread",
			Published:  now,
			Fetched:    now,
			Read:       false,
		},
		{
			ID:         "read",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Read Item",
			URL:        "https://example.com/read",
			Published:  now.Add(-time.Hour),
			Fetched:    now,
			Read:       true, // Note: SaveItems respects this
		},
	}

	_, err = st.SaveItems(items)
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Mark one as read after insert (since SaveItems stores the Read field)
	err = st.MarkRead("read")
	if err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}

	// Get with includeRead=false
	got, err := st.GetItems(10, false)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 unread item, got %d", len(got))
	}
	if got[0].ID != "unread" {
		t.Errorf("expected unread item, got %s", got[0].ID)
	}

	// Get with includeRead=true
	got, err = st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 items, got %d", len(got))
	}
}

func TestGetItemsSince(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	items := []Item{
		{
			ID:         "recent",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Recent Item",
			URL:        "https://example.com/recent",
			Published:  now,
			Fetched:    now,
		},
		{
			ID:         "old",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Old Item",
			URL:        "https://example.com/old",
			Published:  now.Add(-24 * time.Hour),
			Fetched:    now,
		},
	}

	_, err = st.SaveItems(items)
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Get items since 1 hour ago
	since := now.Add(-time.Hour)
	got, err := st.GetItemsSince(since)
	if err != nil {
		t.Fatalf("GetItemsSince failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 item since %v, got %d", since, len(got))
	}
	if len(got) > 0 && got[0].ID != "recent" {
		t.Errorf("expected recent item, got %s", got[0].ID)
	}
}

func TestMarkRead(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	item := Item{
		ID:         "item1",
		SourceType: "rss",
		SourceName: "Test Feed",
		Title:      "Test Title",
		URL:        "https://example.com/1",
		Published:  now,
		Fetched:    now,
		Read:       false,
	}

	_, err = st.SaveItems([]Item{item})
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Verify initially unread
	got, err := st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 1 || got[0].Read {
		t.Errorf("expected item to be unread initially")
	}

	// Mark as read
	err = st.MarkRead("item1")
	if err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}

	// Verify now read
	got, err = st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 1 || !got[0].Read {
		t.Errorf("expected item to be marked as read")
	}
}

func TestMarkSaved(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	item := Item{
		ID:         "item1",
		SourceType: "rss",
		SourceName: "Test Feed",
		Title:      "Test Title",
		URL:        "https://example.com/1",
		Published:  now,
		Fetched:    now,
		Saved:      false,
	}

	_, err = st.SaveItems([]Item{item})
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Verify initially not saved
	got, err := st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 1 || got[0].Saved {
		t.Errorf("expected item to be not saved initially")
	}

	// Mark as saved
	err = st.MarkSaved("item1", true)
	if err != nil {
		t.Fatalf("MarkSaved failed: %v", err)
	}

	// Verify now saved
	got, err = st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 1 || !got[0].Saved {
		t.Errorf("expected item to be marked as saved")
	}

	// Toggle back to not saved
	err = st.MarkSaved("item1", false)
	if err != nil {
		t.Fatalf("MarkSaved failed: %v", err)
	}

	// Verify no longer saved
	got, err = st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(got) != 1 || got[0].Saved {
		t.Errorf("expected item to be marked as not saved")
	}
}

func TestConcurrentAccess(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	var wg sync.WaitGroup

	// Channel to collect errors from goroutines (testing.T methods are not goroutine-safe)
	errCh := make(chan error, 25) // buffer for all potential errors

	// Spawn 10 writer goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			item := Item{
				ID:         fmt.Sprintf("writer-%d", n),
				SourceType: "rss",
				SourceName: "Test Feed",
				Title:      fmt.Sprintf("Title %d", n),
				URL:        fmt.Sprintf("https://example.com/w%d", n),
				Published:  now,
				Fetched:    now,
			}
			_, err := st.SaveItems([]Item{item})
			if err != nil {
				errCh <- fmt.Errorf("SaveItems failed for writer %d: %v", n, err)
			}
		}(i)
	}

	// Spawn 10 reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.GetItems(100, true)
			if err != nil {
				errCh <- fmt.Errorf("GetItems failed: %v", err)
			}
		}()
	}

	// Spawn goroutines that do mark read/saved operations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// These may fail if the item doesn't exist yet, which is expected
			// since writers and markers run concurrently
			_ = st.MarkRead(fmt.Sprintf("writer-%d", n))
			_ = st.MarkSaved(fmt.Sprintf("writer-%d", n), true)
		}(i)
	}

	wg.Wait()
	close(errCh)

	// Report all errors from goroutines in the main goroutine
	for err := range errCh {
		t.Error(err)
	}

	// Verify some items were written
	items, err := st.GetItems(100, true)
	if err != nil {
		t.Fatalf("Final GetItems failed: %v", err)
	}
	if len(items) != 10 {
		t.Errorf("expected 10 items, got %d", len(items))
	}
}

func TestSaveItemsEmptySlice(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	count, err := st.SaveItems([]Item{})
	if err != nil {
		t.Fatalf("SaveItems with empty slice failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 items saved, got %d", count)
	}
}

func TestGetItemsEmpty(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	items, err := st.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems on empty store failed: %v", err)
	}
	if items == nil {
		// nil slice is OK, but we might prefer empty slice
		// Just verify no error
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestSaveEmbedding(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	item := Item{
		ID:         "item1",
		SourceType: "rss",
		SourceName: "Test Feed",
		Title:      "Test Title",
		URL:        "https://example.com/1",
		Published:  now,
		Fetched:    now,
	}

	_, err = st.SaveItems([]Item{item})
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Save embedding
	embedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	err = st.SaveEmbedding("item1", embedding)
	if err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}

	// Retrieve embedding
	got, err := st.GetEmbedding("item1")
	if err != nil {
		t.Fatalf("GetEmbedding failed: %v", err)
	}

	if len(got) != len(embedding) {
		t.Fatalf("expected %d floats, got %d", len(embedding), len(got))
	}

	for i, v := range embedding {
		if got[i] != v {
			t.Errorf("embedding[%d] = %f, want %f", i, got[i], v)
		}
	}
}

func TestGetItemsNeedingEmbedding(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	items := []Item{
		{
			ID:         "item1",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Title 1",
			URL:        "https://example.com/1",
			Published:  now,
			Fetched:    now,
		},
		{
			ID:         "item2",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Title 2",
			URL:        "https://example.com/2",
			Published:  now.Add(-time.Hour),
			Fetched:    now.Add(-time.Hour),
		},
		{
			ID:         "item3",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Title 3",
			URL:        "https://example.com/3",
			Published:  now.Add(-2 * time.Hour),
			Fetched:    now.Add(-2 * time.Hour),
		},
	}

	_, err = st.SaveItems(items)
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// All items need embedding initially
	needing, err := st.GetItemsNeedingEmbedding(10)
	if err != nil {
		t.Fatalf("GetItemsNeedingEmbedding failed: %v", err)
	}
	if len(needing) != 3 {
		t.Errorf("expected 3 items needing embedding, got %d", len(needing))
	}

	// Oldest should be first (by fetched_at)
	if needing[0].ID != "item3" {
		t.Errorf("expected item3 first (oldest), got %s", needing[0].ID)
	}

	// Add embedding to item2
	err = st.SaveEmbedding("item2", []float32{0.1, 0.2})
	if err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}

	// Now only 2 items need embedding
	needing, err = st.GetItemsNeedingEmbedding(10)
	if err != nil {
		t.Fatalf("GetItemsNeedingEmbedding failed: %v", err)
	}
	if len(needing) != 2 {
		t.Errorf("expected 2 items needing embedding, got %d", len(needing))
	}

	// Test limit
	needing, err = st.GetItemsNeedingEmbedding(1)
	if err != nil {
		t.Fatalf("GetItemsNeedingEmbedding with limit failed: %v", err)
	}
	if len(needing) != 1 {
		t.Errorf("expected 1 item with limit=1, got %d", len(needing))
	}
}

func TestGetEmbedding(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	item := Item{
		ID:         "item1",
		SourceType: "rss",
		SourceName: "Test Feed",
		Title:      "Test Title",
		URL:        "https://example.com/1",
		Published:  now,
		Fetched:    now,
	}

	_, err = st.SaveItems([]Item{item})
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Get embedding for item without embedding
	got, err := st.GetEmbedding("item1")
	if err != nil {
		t.Fatalf("GetEmbedding failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil embedding, got %v", got)
	}

	// Get embedding for non-existent item
	got, err = st.GetEmbedding("nonexistent")
	if err != nil {
		t.Fatalf("GetEmbedding for nonexistent failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent item, got %v", got)
	}

	// Save and retrieve
	embedding := []float32{1.0, 2.0, 3.0}
	err = st.SaveEmbedding("item1", embedding)
	if err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}

	got, err = st.GetEmbedding("item1")
	if err != nil {
		t.Fatalf("GetEmbedding after save failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 floats, got %d", len(got))
	}
}

func TestGetItemsWithEmbeddings(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	now := time.Now()
	items := []Item{
		{
			ID:         "item1",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Title 1",
			URL:        "https://example.com/1",
			Published:  now,
			Fetched:    now,
		},
		{
			ID:         "item2",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Title 2",
			URL:        "https://example.com/2",
			Published:  now,
			Fetched:    now,
		},
		{
			ID:         "item3",
			SourceType: "rss",
			SourceName: "Test Feed",
			Title:      "Title 3",
			URL:        "https://example.com/3",
			Published:  now,
			Fetched:    now,
		},
	}

	_, err = st.SaveItems(items)
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Save embeddings for item1 and item3
	err = st.SaveEmbedding("item1", []float32{0.1, 0.2})
	if err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}
	err = st.SaveEmbedding("item3", []float32{0.3, 0.4})
	if err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}

	// Get embeddings for all 3 items
	result, err := st.GetItemsWithEmbeddings([]string{"item1", "item2", "item3"})
	if err != nil {
		t.Fatalf("GetItemsWithEmbeddings failed: %v", err)
	}

	// Should only have 2 items (item2 has no embedding)
	if len(result) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(result))
	}

	if _, ok := result["item1"]; !ok {
		t.Error("expected item1 in result")
	}
	if _, ok := result["item3"]; !ok {
		t.Error("expected item3 in result")
	}
	if _, ok := result["item2"]; ok {
		t.Error("did not expect item2 in result (no embedding)")
	}

	// Verify values
	if result["item1"][0] != 0.1 || result["item1"][1] != 0.2 {
		t.Errorf("item1 embedding = %v, want [0.1 0.2]", result["item1"])
	}

	// Test empty input
	result, err = st.GetItemsWithEmbeddings([]string{})
	if err != nil {
		t.Fatalf("GetItemsWithEmbeddings empty failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d", len(result))
	}
}

func TestEmbeddingRoundTrip(t *testing.T) {
	testCases := []struct {
		name      string
		embedding []float32
	}{
		{
			name:      "simple values",
			embedding: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		},
		{
			name:      "negative values",
			embedding: []float32{-0.5, -0.25, 0.0, 0.25, 0.5},
		},
		{
			name:      "large values",
			embedding: []float32{1000.0, -1000.0, 0.001, -0.001},
		},
		{
			name:      "special values",
			embedding: []float32{0.0, float32(math.Inf(1)), float32(math.Inf(-1))},
		},
		{
			name:      "empty",
			embedding: []float32{},
		},
		{
			name:      "single value",
			embedding: []float32{3.14159},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := encodeEmbedding(tc.embedding)
			decoded := decodeEmbedding(encoded)

			if len(decoded) != len(tc.embedding) {
				t.Fatalf("length mismatch: got %d, want %d", len(decoded), len(tc.embedding))
			}

			for i, v := range tc.embedding {
				// Use bit comparison for special values like Inf
				if math.IsInf(float64(v), 0) || math.IsNaN(float64(v)) {
					if math.Float32bits(decoded[i]) != math.Float32bits(v) {
						t.Errorf("index %d: bits mismatch, got %x, want %x",
							i, math.Float32bits(decoded[i]), math.Float32bits(v))
					}
				} else if decoded[i] != v {
					t.Errorf("index %d: got %f, want %f", i, decoded[i], v)
				}
			}
		})
	}
}

func TestSaveEmbeddingNonExistentItem(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	// SaveEmbedding on non-existent item should not error (UPDATE affects 0 rows)
	// This is intentional - we silently skip non-existent items
	embedding := []float32{0.1, 0.2, 0.3}
	err = st.SaveEmbedding("nonexistent-id", embedding)
	if err != nil {
		t.Errorf("SaveEmbedding on non-existent item should not error: %v", err)
	}

	// Verify nothing was saved
	result, err := st.GetEmbedding("nonexistent-id")
	if err != nil {
		t.Errorf("GetEmbedding error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil embedding for non-existent item, got %v", result)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer st.Close()

	// migrateEmbeddings was already called by Open
	// Call it again to verify idempotency
	err = st.migrateEmbeddings()
	if err != nil {
		t.Fatalf("Second migrateEmbeddings failed: %v", err)
	}

	// Call it a third time
	err = st.migrateEmbeddings()
	if err != nil {
		t.Fatalf("Third migrateEmbeddings failed: %v", err)
	}

	// Verify the column exists
	var count int
	err = st.db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('items')
		WHERE name = 'embedding'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("pragma query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected embedding column to exist once, count = %d", count)
	}

	// Verify the index exists
	err = st.db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_items_no_embedding'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("index query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected index to exist once, count = %d", count)
	}
}
