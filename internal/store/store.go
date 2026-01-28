// Package store provides SQLite persistence for Observer.
package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store handles SQLite persistence. NOT an interface - concrete type.
// Thread-safety: All methods are safe for concurrent use via internal mutex.
type Store struct {
	db *sql.DB
	mu sync.RWMutex // Protects all database operations
}

// Item represents stored content.
type Item struct {
	ID         string
	SourceType string // "rss", "hn", "reddit"
	SourceName string
	Title      string
	Summary    string
	URL        string
	Author     string
	Published  time.Time
	Fetched    time.Time
	Read       bool
	Saved      bool
}

// Open creates a new Store with the given database path.
// Creates tables if they don't exist.
// Uses WAL mode for better concurrent read performance (file-based DBs only).
func Open(dbPath string) (*Store, error) {
	// Build connection string based on database type
	connStr := dbPath
	if dbPath == ":memory:" {
		// For in-memory databases, use shared cache mode so all connections
		// in the pool see the same database
		connStr = "file::memory:?cache=shared"
	}

	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// For in-memory databases, limit to 1 connection to avoid issues
	// with multiple connections getting different databases
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	// Enable WAL mode for file-based databases (not :memory:)
	if dbPath != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable WAL mode: %w", err)
		}
	}

	s := &Store{db: db}

	if err := s.createTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	return s, nil
}

// createTables creates the required tables and indexes if they don't exist.
func (s *Store) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS items (
		id TEXT PRIMARY KEY,
		source_type TEXT NOT NULL,
		source_name TEXT NOT NULL,
		title TEXT NOT NULL,
		summary TEXT,
		url TEXT UNIQUE,
		author TEXT,
		published_at DATETIME NOT NULL,
		fetched_at DATETIME NOT NULL,
		read INTEGER DEFAULT 0,
		saved INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_items_published ON items(published_at DESC);
	CREATE INDEX IF NOT EXISTS idx_items_source ON items(source_name);
	CREATE INDEX IF NOT EXISTS idx_items_url ON items(url);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("execute schema: %w", err)
	}
	return nil
}

// Close closes the database connection.
// Thread-safe: acquires write lock to prevent closing during in-flight operations.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// SaveItems stores items, returning count of new items inserted.
// Duplicates (by URL) are silently ignored via INSERT OR IGNORE.
// Thread-safe: acquires write lock.
func (s *Store) SaveItems(items []Item) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(items) == 0 {
		return 0, nil
	}

	// Use INSERT OR IGNORE to handle URL conflicts
	// We count rows affected to determine new inserts
	stmt, err := s.db.Prepare(`
		INSERT OR IGNORE INTO items (
			id, source_type, source_name, title, summary, url, author,
			published_at, fetched_at, read, saved
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	newCount := 0
	for _, item := range items {
		result, err := stmt.Exec(
			item.ID,
			item.SourceType,
			item.SourceName,
			item.Title,
			item.Summary,
			item.URL,
			item.Author,
			item.Published,
			item.Fetched,
			boolToInt(item.Read),
			boolToInt(item.Saved),
		)
		if err != nil {
			return newCount, err
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return newCount, err
		}
		if affected > 0 {
			newCount++
		}
	}

	return newCount, nil
}

// GetItems retrieves items for display.
// If includeRead is false, only unread items are returned.
// Items are ordered by published_at DESC.
// Thread-safe: acquires read lock.
func (s *Store) GetItems(limit int, includeRead bool) ([]Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var query string
	var args []any

	if includeRead {
		query = `
			SELECT id, source_type, source_name, title, summary, url, author,
				published_at, fetched_at, read, saved
			FROM items
			ORDER BY published_at DESC
			LIMIT ?
		`
		args = []any{limit}
	} else {
		query = `
			SELECT id, source_type, source_name, title, summary, url, author,
				published_at, fetched_at, read, saved
			FROM items
			WHERE read = 0
			ORDER BY published_at DESC
			LIMIT ?
		`
		args = []any{limit}
	}

	return s.queryItems(query, args...)
}

// GetItemsSince retrieves items published after the given time.
// Thread-safe: acquires read lock.
func (s *Store) GetItemsSince(since time.Time) ([]Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, source_type, source_name, title, summary, url, author,
			published_at, fetched_at, read, saved
		FROM items
		WHERE published_at > ?
		ORDER BY published_at DESC
	`

	return s.queryItems(query, since)
}

// MarkRead marks an item as read.
// Thread-safe: acquires write lock.
func (s *Store) MarkRead(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("UPDATE items SET read = 1 WHERE id = ?", id)
	return err
}

// MarkSaved toggles the saved state of an item.
// Thread-safe: acquires write lock.
func (s *Store) MarkSaved(id string, saved bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("UPDATE items SET saved = ? WHERE id = ?", boolToInt(saved), id)
	return err
}

// queryItems is a helper that executes a query and scans results into Items.
// Caller must hold s.mu (read lock is sufficient).
func (s *Store) queryItems(query string, args ...any) ([]Item, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		var readInt, savedInt int
		err := rows.Scan(
			&item.ID,
			&item.SourceType,
			&item.SourceName,
			&item.Title,
			&item.Summary,
			&item.URL,
			&item.Author,
			&item.Published,
			&item.Fetched,
			&readInt,
			&savedInt,
		)
		if err != nil {
			return nil, err
		}
		item.Read = readInt != 0
		item.Saved = savedInt != 0
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// boolToInt converts a bool to an int for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
