package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "observer-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Verify database was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestSaveAndGetItems(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Create test items
	items := []Item{
		{
			ID:         "item1",
			Source:     SourceRSS,
			SourceName: "Test Source",
			Title:      "First Article",
			URL:        "https://example.com/1",
			Published:  time.Now().Add(-1 * time.Hour),
			Fetched:    time.Now(),
		},
		{
			ID:         "item2",
			Source:     SourceRSS,
			SourceName: "Test Source",
			Title:      "Second Article",
			URL:        "https://example.com/2",
			Published:  time.Now().Add(-2 * time.Hour),
			Fetched:    time.Now(),
		},
	}

	// Save items
	affected, err := store.SaveItems(items)
	if err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}
	if affected != 2 {
		t.Errorf("expected 2 affected rows, got %d", affected)
	}

	// Get items back
	retrieved, err := store.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(retrieved) != 2 {
		t.Errorf("expected 2 items, got %d", len(retrieved))
	}

	// Verify order (newest first)
	if retrieved[0].ID != "item1" {
		t.Errorf("expected item1 first, got %s", retrieved[0].ID)
	}
}

func TestSaveItemsIdempotent(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	item := Item{
		ID:         "item1",
		Source:     SourceRSS,
		SourceName: "Test",
		Title:      "Original Title",
		Published:  time.Now(),
		Fetched:    time.Now(),
	}

	// Save once
	_, err := store.SaveItems([]Item{item})
	if err != nil {
		t.Fatalf("first SaveItems failed: %v", err)
	}

	// Save again with different title (should update)
	item.Title = "Updated Title"
	_, err = store.SaveItems([]Item{item})
	if err != nil {
		t.Fatalf("second SaveItems failed: %v", err)
	}

	// Verify only one item exists
	items, err := store.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
	if items[0].Title != "Updated Title" {
		t.Errorf("expected updated title, got %s", items[0].Title)
	}
}

func TestGetItemsSince(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	now := time.Now()
	items := []Item{
		{ID: "old", Source: SourceRSS, SourceName: "Test", Title: "Old", Published: now.Add(-48 * time.Hour), Fetched: now},
		{ID: "recent", Source: SourceRSS, SourceName: "Test", Title: "Recent", Published: now.Add(-1 * time.Hour), Fetched: now},
		{ID: "new", Source: SourceRSS, SourceName: "Test", Title: "New", Published: now.Add(-5 * time.Minute), Fetched: now},
	}

	if _, err := store.SaveItems(items); err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Get items from last 2 hours
	retrieved, err := store.GetItemsSince(now.Add(-2 * time.Hour))
	if err != nil {
		t.Fatalf("GetItemsSince failed: %v", err)
	}
	if len(retrieved) != 2 {
		t.Errorf("expected 2 items, got %d", len(retrieved))
	}
}

func TestMarkRead(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	item := Item{
		ID:         "item1",
		Source:     SourceRSS,
		SourceName: "Test",
		Title:      "Test Article",
		Published:  time.Now(),
		Fetched:    time.Now(),
	}

	if _, err := store.SaveItems([]Item{item}); err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Mark as read
	if err := store.MarkRead("item1"); err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}

	// Verify unread query excludes it
	unread, err := store.GetItems(10, false)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("expected 0 unread items, got %d", len(unread))
	}

	// Verify includeRead still returns it
	all, err := store.GetItems(10, true)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 item, got %d", len(all))
	}
}

func TestMarkReadNotFound(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	err := store.MarkRead("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent item")
	}
}

func TestItemCount(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Initially empty
	count, err := store.ItemCount()
	if err != nil {
		t.Fatalf("ItemCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	// Add items
	items := []Item{
		{ID: "1", Source: SourceRSS, SourceName: "Test", Title: "A", Published: time.Now(), Fetched: time.Now()},
		{ID: "2", Source: SourceRSS, SourceName: "Test", Title: "B", Published: time.Now(), Fetched: time.Now()},
	}
	if _, err := store.SaveItems(items); err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	count, err = store.ItemCount()
	if err != nil {
		t.Fatalf("ItemCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestSaveItemsEmpty(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Should handle empty slice gracefully
	affected, err := store.SaveItems([]Item{})
	if err != nil {
		t.Fatalf("SaveItems with empty slice failed: %v", err)
	}
	if affected != 0 {
		t.Errorf("expected 0 affected, got %d", affected)
	}
}

func TestSourceStatus(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Initially no status
	lastFetch, err := store.GetSourceStatus("TestSource")
	if err != nil {
		t.Fatalf("GetSourceStatus failed: %v", err)
	}
	if !lastFetch.IsZero() {
		t.Error("expected zero time for new source")
	}

	// Update status
	if err := store.UpdateSourceStatus("TestSource", 10, ""); err != nil {
		t.Fatalf("UpdateSourceStatus failed: %v", err)
	}

	// Verify update
	lastFetch, err = store.GetSourceStatus("TestSource")
	if err != nil {
		t.Fatalf("GetSourceStatus failed: %v", err)
	}
	if lastFetch.IsZero() {
		t.Error("expected non-zero time after update")
	}
}

func TestSession(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Start session
	sessionID, err := store.StartSession()
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	if sessionID <= 0 {
		t.Errorf("expected positive session ID, got %d", sessionID)
	}

	// End session
	if err := store.EndSession(sessionID); err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}
}

func TestSaveAndGetEmbedding(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Create and save an item
	item := Item{
		ID:         "item1",
		Source:     SourceRSS,
		SourceName: "Test",
		Title:      "Test Article",
		Published:  time.Now(),
		Fetched:    time.Now(),
	}
	if _, err := store.SaveItems([]Item{item}); err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Save embedding
	embedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	if err := store.SaveEmbedding("item1", embedding); err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}

	// Get embedding back
	retrieved, err := store.GetEmbedding("item1")
	if err != nil {
		t.Fatalf("GetEmbedding failed: %v", err)
	}
	if len(retrieved) != len(embedding) {
		t.Fatalf("expected %d floats, got %d", len(embedding), len(retrieved))
	}
	for i, v := range embedding {
		if retrieved[i] != v {
			t.Errorf("embedding[%d] = %f, expected %f", i, retrieved[i], v)
		}
	}
}

func TestSaveEmbeddings(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Create and save items
	items := []Item{
		{ID: "1", Source: SourceRSS, SourceName: "Test", Title: "A", Published: time.Now(), Fetched: time.Now()},
		{ID: "2", Source: SourceRSS, SourceName: "Test", Title: "B", Published: time.Now(), Fetched: time.Now()},
		{ID: "3", Source: SourceRSS, SourceName: "Test", Title: "C", Published: time.Now(), Fetched: time.Now()},
	}
	if _, err := store.SaveItems(items); err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Save embeddings in batch
	embeddings := map[string][]float32{
		"1": {0.1, 0.2},
		"2": {0.3, 0.4},
		"3": {0.5, 0.6},
	}
	if err := store.SaveEmbeddings(embeddings); err != nil {
		t.Fatalf("SaveEmbeddings failed: %v", err)
	}

	// Verify all embeddings saved
	for id, expected := range embeddings {
		retrieved, err := store.GetEmbedding(id)
		if err != nil {
			t.Fatalf("GetEmbedding(%s) failed: %v", id, err)
		}
		if len(retrieved) != len(expected) {
			t.Errorf("embedding %s: expected %d floats, got %d", id, len(expected), len(retrieved))
		}
	}
}

func TestGetItemsWithoutEmbedding(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Create items
	now := time.Now()
	items := []Item{
		{ID: "1", Source: SourceRSS, SourceName: "Test", Title: "A", Published: now, Fetched: now},
		{ID: "2", Source: SourceRSS, SourceName: "Test", Title: "B", Published: now.Add(-time.Hour), Fetched: now},
		{ID: "3", Source: SourceRSS, SourceName: "Test", Title: "C", Published: now.Add(-2 * time.Hour), Fetched: now},
	}
	if _, err := store.SaveItems(items); err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Initially all items should be without embedding
	noEmbed, err := store.GetItemsWithoutEmbedding(10)
	if err != nil {
		t.Fatalf("GetItemsWithoutEmbedding failed: %v", err)
	}
	if len(noEmbed) != 3 {
		t.Errorf("expected 3 items without embedding, got %d", len(noEmbed))
	}

	// Save embedding for one item
	if err := store.SaveEmbedding("2", []float32{0.1, 0.2}); err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}

	// Now should have 2 items without embedding
	noEmbed, err = store.GetItemsWithoutEmbedding(10)
	if err != nil {
		t.Fatalf("GetItemsWithoutEmbedding failed: %v", err)
	}
	if len(noEmbed) != 2 {
		t.Errorf("expected 2 items without embedding, got %d", len(noEmbed))
	}

	// Verify the count method too
	count, err := store.GetItemsWithoutEmbeddingCount()
	if err != nil {
		t.Fatalf("GetItemsWithoutEmbeddingCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestGetItemsWithEmbeddings(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Create items
	now := time.Now()
	items := []Item{
		{ID: "1", Source: SourceRSS, SourceName: "Test", Title: "A", Published: now, Fetched: now},
		{ID: "2", Source: SourceRSS, SourceName: "Test", Title: "B", Published: now.Add(-time.Hour), Fetched: now},
	}
	if _, err := store.SaveItems(items); err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	// Save embeddings
	embedding1 := []float32{0.1, 0.2, 0.3}
	embedding2 := []float32{0.4, 0.5, 0.6}
	if err := store.SaveEmbedding("1", embedding1); err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}
	if err := store.SaveEmbedding("2", embedding2); err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}

	// Get items with embeddings
	retrieved, err := store.GetItemsWithEmbeddings(10, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("GetItemsWithEmbeddings failed: %v", err)
	}
	if len(retrieved) != 2 {
		t.Fatalf("expected 2 items, got %d", len(retrieved))
	}

	// Verify embeddings are populated
	for _, item := range retrieved {
		if item.Embedding == nil {
			t.Errorf("item %s has nil embedding", item.ID)
		}
		if len(item.Embedding) != 3 {
			t.Errorf("item %s embedding has %d floats, expected 3", item.ID, len(item.Embedding))
		}
	}
}

func TestEmbeddingRoundTrip(t *testing.T) {
	// Test that embedding serialization/deserialization preserves values
	testCases := [][]float32{
		{0.0, 1.0, -1.0},
		{0.123456, 0.789012, 0.345678},
		{1e-10, 1e10, -1e-10},
		make([]float32, 1024), // Large embedding
	}

	for i, original := range testCases {
		blob := serializeEmbedding(original)
		restored := deserializeEmbedding(blob)

		if len(restored) != len(original) {
			t.Errorf("case %d: length mismatch %d vs %d", i, len(restored), len(original))
			continue
		}

		for j, v := range original {
			if restored[j] != v {
				t.Errorf("case %d: value[%d] = %f, expected %f", i, j, restored[j], v)
			}
		}
	}
}

// newTestStore creates a temporary store for testing.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "observer-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	return store
}
