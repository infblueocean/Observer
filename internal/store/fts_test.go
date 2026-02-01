package store

import (
	"testing"
	"time"
)

func TestMigrateFTS(t *testing.T) {
	dbPath := ":memory:"
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Verify schema version is 2
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version failed: %v", err)
	}
	if version != 2 {
		t.Errorf("expected user_version=2, got %d", version)
	}

	// Verify table exists
	if _, err := s.db.Exec("SELECT * FROM items_fts LIMIT 0"); err != nil {
		t.Errorf("items_fts table does not exist: %v", err)
	}
}

func TestFTSTriggers(t *testing.T) {
	dbPath := ":memory:"
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	item := Item{
		ID:         "1",
		SourceType: "rss",
		SourceName: "test",
		Title:      "Climate Change Report",
		Summary:    "Global warming impacts",
		URL:        "http://example.com/1",
		Author:     "Alice",
		Published:  time.Now(),
		Fetched:    time.Now(),
	}

	// 1. INSERT -> Trigger AI
	if _, err := s.SaveItems([]Item{item}); err != nil {
		t.Fatalf("SaveItems failed: %v", err)
	}

	results, err := s.SearchFTS("climate", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "1" {
		t.Errorf("expected ID 1, got %s", results[0].ID)
	}

	// 2. UPDATE -> Trigger AU
	// We need to do a raw SQL update to test the trigger because SaveItems uses INSERT OR IGNORE
	_, err = s.db.Exec("UPDATE items SET title = 'Weather Report' WHERE id = '1'")
	if err != nil {
		t.Fatalf("UPDATE failed: %v", err)
	}

	results, err = s.SearchFTS("climate", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for 'climate', got %d", len(results))
	}

	results, err = s.SearchFTS("weather", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'weather', got %d", len(results))
	}

	// 3. DELETE -> Trigger AD
	_, err = s.db.Exec("DELETE FROM items WHERE id = '1'")
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}

	results, err = s.SearchFTS("weather", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}

func TestSearchFTS_Retry(t *testing.T) {
	dbPath := ":memory:"
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	item := Item{
		ID:         "1",
		SourceType: "rss",
		SourceName: "test",
		Title:      "C++ Programming",
		Summary:    "Pointers and refs",
		URL:        "http://example.com/1",
		Author:     "Bob",
		Published:  time.Now(),
		Fetched:    time.Now(),
	}
	s.SaveItems([]Item{item})

	// "C++" is a syntax error in FTS5 standard query syntax (unbalanced +)
	// But our SearchFTS should catch it and retry as quoted string "C++"
	results, err := s.SearchFTS("C++", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed on syntax error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRebuildFTS(t *testing.T) {
	// Create a raw DB without FTS tables to simulate existing data
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	
	// Create raw table and insert data
	{
		db, err := Open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		// Manually drop FTS to simulate pre-FTS state
		db.db.Exec("DROP TABLE items_fts")
		db.db.Exec("DROP TRIGGER items_ai")
		db.db.Exec("DROP TRIGGER items_au")
		db.db.Exec("DROP TRIGGER items_ad")
		db.db.Exec("PRAGMA user_version = 0")
		
		item := Item{
			ID: "1", Title: "Legacy Item", URL: "http://example.com",
			Published: time.Now(), Fetched: time.Now(),
			SourceType: "rss", SourceName: "old",
		}
		if _, err := db.SaveItems([]Item{item}); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}

	// Re-open with normal Open(), which should trigger migrate + rebuild
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Re-open failed: %v", err)
	}
	defer s.Close()

	results, err := s.SearchFTS("Legacy", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'Legacy', got %d", len(results))
	}
}