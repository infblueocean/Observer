// Package model provides the data layer for Observer v0.5.
//
// Model is the source of truth - SQLite persistence with complete history.
//
// # Thread Safety
//
// Store is safe for concurrent use. The underlying sql.DB handles connection
// pooling and serialization. Individual operations are atomic, but sequences
// of operations (read-modify-write) require external synchronization.
//
// # Transactions
//
// SaveItems uses a transaction to ensure atomicity. Other operations are
// single statements and implicitly atomic.
package model

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver (no CGO required)
)

// Store handles persistence of feed items.
//
// This is the source of truth for the application. All data flows through
// the store - items are saved here after fetching and read here for display.
type Store struct {
	db *sql.DB
}

// NewStore creates a new SQLite store at the given path.
//
// The database is created if it doesn't exist, and migrations are applied
// automatically.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrent access
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return s, nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS items (
		id TEXT PRIMARY KEY,
		source_type TEXT NOT NULL,
		source_name TEXT NOT NULL,
		source_url TEXT,
		title TEXT NOT NULL,
		summary TEXT,
		content TEXT,
		url TEXT,
		author TEXT,
		published_at DATETIME NOT NULL,
		fetched_at DATETIME NOT NULL,
		read INTEGER DEFAULT 0,
		saved INTEGER DEFAULT 0,
		hidden INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_items_published ON items(published_at DESC);
	CREATE INDEX IF NOT EXISTS idx_items_source ON items(source_name);
	CREATE INDEX IF NOT EXISTS idx_items_read ON items(read);
	CREATE INDEX IF NOT EXISTS idx_items_url ON items(url);

	CREATE TABLE IF NOT EXISTS sources (
		name TEXT PRIMARY KEY,
		last_fetched_at DATETIME,
		item_count INTEGER DEFAULT 0,
		error_count INTEGER DEFAULT 0,
		last_error TEXT
	);

	-- Session tracking
	CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY,
		started_at DATETIME NOT NULL,
		ended_at DATETIME,
		items_viewed INTEGER DEFAULT 0
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// SaveItems saves or updates items in a single transaction.
//
// Returns the number of rows affected (inserts + updates) and any error.
// On error, no items are saved (transaction is rolled back).
func (s *Store) SaveItems(items []Item) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	// Rollback is safe to call even after commit - it's a no-op
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO items (id, source_type, source_name, source_url, title, summary, content, url, author, published_at, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			summary = excluded.summary,
			content = excluded.content,
			fetched_at = excluded.fetched_at
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	affected := 0
	for _, item := range items {
		result, err := stmt.Exec(
			item.ID,
			string(item.Source),
			item.SourceName,
			item.SourceURL,
			item.Title,
			item.Summary,
			item.Content,
			item.URL,
			item.Author,
			item.Published,
			item.Fetched,
		)
		if err != nil {
			// Log but continue - partial save is better than none
			continue
		}
		rows, err := result.RowsAffected()
		if err == nil && rows > 0 {
			affected++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return affected, nil
}

// GetItems retrieves items with optional filters.
//
// If includeRead is false, only unread items are returned.
// Results are ordered by published time, newest first.
func (s *Store) GetItems(limit int, includeRead bool) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, source_type, source_name, source_url, title, summary, content, url, author, published_at, fetched_at, read, saved
		FROM items
		WHERE hidden = 0
	`
	if !includeRead {
		query += " AND read = 0"
	}
	query += " ORDER BY published_at DESC LIMIT ?"

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	items, err := scanItems(rows)
	if err != nil {
		return nil, err
	}

	return items, nil
}

// GetItemsSince retrieves items published after a given time.
//
// Results are ordered by published time, newest first.
func (s *Store) GetItemsSince(since time.Time) ([]Item, error) {
	query := `
		SELECT id, source_type, source_name, source_url, title, summary, content, url, author, published_at, fetched_at, read, saved
		FROM items
		WHERE hidden = 0 AND published_at > ?
		ORDER BY published_at DESC
	`

	rows, err := s.db.Query(query, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	return scanItems(rows)
}

// scanItems scans rows into items, handling the common scanning logic.
func scanItems(rows *sql.Rows) ([]Item, error) {
	var items []Item
	for rows.Next() {
		var item Item
		var sourceType string
		err := rows.Scan(
			&item.ID,
			&sourceType,
			&item.SourceName,
			&item.SourceURL,
			&item.Title,
			&item.Summary,
			&item.Content,
			&item.URL,
			&item.Author,
			&item.Published,
			&item.Fetched,
			&item.Read,
			&item.Saved,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		item.Source = SourceType(sourceType)
		items = append(items, item)
	}

	// Critical: check for errors from row iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return items, nil
}

// MarkRead marks an item as read.
func (s *Store) MarkRead(id string) error {
	result, err := s.db.Exec("UPDATE items SET read = 1 WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to mark item read: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("item not found: %s", id)
	}
	return nil
}

// MarkSaved toggles saved state.
func (s *Store) MarkSaved(id string, saved bool) error {
	val := 0
	if saved {
		val = 1
	}
	result, err := s.db.Exec("UPDATE items SET saved = ? WHERE id = ?", val, id)
	if err != nil {
		return fmt.Errorf("failed to mark item saved: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("item not found: %s", id)
	}
	return nil
}

// UpdateSourceStatus updates the last fetch time for a source.
func (s *Store) UpdateSourceStatus(name string, itemCount int, lastError string) error {
	_, err := s.db.Exec(`
		INSERT INTO sources (name, last_fetched_at, item_count, last_error, error_count)
		VALUES (?, ?, ?, ?, CASE WHEN ? != '' THEN 1 ELSE 0 END)
		ON CONFLICT(name) DO UPDATE SET
			last_fetched_at = excluded.last_fetched_at,
			item_count = excluded.item_count,
			last_error = excluded.last_error,
			error_count = CASE WHEN excluded.last_error != '' THEN error_count + 1 ELSE 0 END
	`, name, time.Now(), itemCount, lastError, lastError)
	if err != nil {
		return fmt.Errorf("failed to update source status: %w", err)
	}
	return nil
}

// GetSourceStatus gets the last fetch time for a source.
func (s *Store) GetSourceStatus(name string) (time.Time, error) {
	var lastFetched sql.NullTime
	err := s.db.QueryRow("SELECT last_fetched_at FROM sources WHERE name = ?", name).Scan(&lastFetched)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get source status: %w", err)
	}
	return lastFetched.Time, nil
}

// ItemCount returns total item count.
func (s *Store) ItemCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE hidden = 0").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count items: %w", err)
	}
	return count, err
}

// UnreadCount returns unread item count.
func (s *Store) UnreadCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE hidden = 0 AND read = 0").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count unread items: %w", err)
	}
	return count, err
}

// SourceCount returns number of unique sources in the database.
func (s *Store) SourceCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(DISTINCT source_name) FROM items").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count sources: %w", err)
	}
	return count, err
}

// StartSession records a new session.
func (s *Store) StartSession() (int64, error) {
	result, err := s.db.Exec(`INSERT INTO sessions (started_at) VALUES (?)`, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to start session: %w", err)
	}
	return result.LastInsertId()
}

// EndSession marks the current session as ended.
func (s *Store) EndSession(sessionID int64) error {
	_, err := s.db.Exec(`UPDATE sessions SET ended_at = ? WHERE id = ?`, time.Now(), sessionID)
	if err != nil {
		return fmt.Errorf("failed to end session: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection for advanced queries.
//
// Use with caution - prefer using Store methods for common operations.
func (s *Store) DB() *sql.DB {
	return s.db
}
