// Package store provides SQLite persistence for Observer.
package store

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
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

	if err := s.migrateEmbeddings(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate embeddings: %w", err)
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
		return 0, fmt.Errorf("prepare insert: %w", err)
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
	if err != nil {
		return fmt.Errorf("mark read %s: %w", id, err)
	}
	return nil
}

// MarkSaved toggles the saved state of an item.
// Thread-safe: acquires write lock.
func (s *Store) MarkSaved(id string, saved bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("UPDATE items SET saved = ? WHERE id = ?", boolToInt(saved), id)
	if err != nil {
		return fmt.Errorf("mark saved %s: %w", id, err)
	}
	return nil
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

// migrateEmbeddings adds the embedding column and index if they don't exist.
func (s *Store) migrateEmbeddings() error {
	// Check if column exists using pragma_table_info
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('items')
		WHERE name = 'embedding'
	`).Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		_, err = s.db.Exec(`ALTER TABLE items ADD COLUMN embedding BLOB DEFAULT NULL`)
		if err != nil {
			return fmt.Errorf("add embedding column: %w", err)
		}
	}

	// Create partial index (IF NOT EXISTS is idempotent)
	_, err = s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_items_no_embedding
		ON items(id) WHERE embedding IS NULL
	`)
	return err
}

// SaveEmbedding stores an embedding for an item.
// Thread-safe: acquires write lock.
func (s *Store) SaveEmbedding(id string, embedding []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := encodeEmbedding(embedding)
	_, err := s.db.Exec("UPDATE items SET embedding = ? WHERE id = ?", data, id)
	if err != nil {
		return fmt.Errorf("save embedding for %s: %w", id, err)
	}
	return nil
}

// GetItemsNeedingEmbedding returns items with NULL embedding.
// Returns oldest items first (by fetched_at) up to limit.
// Thread-safe: acquires read lock.
func (s *Store) GetItemsNeedingEmbedding(limit int) ([]Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, source_type, source_name, title, summary, url, author,
			published_at, fetched_at, read, saved
		FROM items
		WHERE embedding IS NULL
		ORDER BY fetched_at ASC
		LIMIT ?
	`

	return s.queryItems(query, limit)
}

// GetEmbedding returns the embedding for an item, or nil if not set.
// Thread-safe: acquires read lock.
func (s *Store) GetEmbedding(id string) ([]float32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var data []byte
	err := s.db.QueryRow("SELECT embedding FROM items WHERE id = ?", id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return decodeEmbedding(data), nil
}

// GetItemsWithEmbeddings returns embeddings for given item IDs.
// Thread-safe: acquires read lock.
func (s *Store) GetItemsWithEmbeddings(ids []string) (map[string][]float32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(ids) == 0 {
		return make(map[string][]float32), nil
	}

	result := make(map[string][]float32)

	// Build query with placeholders
	query := "SELECT id, embedding FROM items WHERE id IN (?" + repeatString(",?", len(ids)-1) + ") AND embedding IS NOT NULL"

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		if data != nil {
			result[id] = decodeEmbedding(data)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// encodeEmbedding converts a float32 slice to bytes using little-endian encoding.
func encodeEmbedding(embedding []float32) []byte {
	data := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(v))
	}
	return data
}

// decodeEmbedding converts bytes to a float32 slice using little-endian encoding.
func decodeEmbedding(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		embedding[i] = math.Float32frombits(bits)
	}
	return embedding
}

// repeatString returns s repeated n times.
func repeatString(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}
