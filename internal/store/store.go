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

	// Enable WAL mode and busy timeout for file-based databases (not :memory:)
	if dbPath != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable WAL mode: %w", err)
		}
		if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
			db.Close()
			return nil, fmt.Errorf("set busy timeout: %w", err)
		}
	}

	s := &Store{db: db}

	if err := s.createTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	if err := s.migrateFTS(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate FTS: %w", err)
	}

	if err := s.rebuildFTS(); err != nil {
		db.Close()
		return nil, fmt.Errorf("rebuild FTS: %w", err)
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

// migrateFTS ensures the FTS5 schema is current.
// Uses PRAGMA user_version to track schema versions:
//   0 = fresh install or pre-FTS (no FTS tables)
//   1 = FTS without author column (early adopters)
//   2 = FTS with author column (current)
func (s *Store) migrateFTS() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	switch {
	case version >= 2:
		// Current schema — no migration needed.
		return nil

	case version == 1:
		// Upgrade from FTS without author → FTS with author.
		// Must drop and recreate because FTS5 virtual tables cannot be ALTERed.
		_, err := s.db.Exec(`
			DROP TRIGGER IF EXISTS items_ai;
			DROP TRIGGER IF EXISTS items_au;
			DROP TRIGGER IF EXISTS items_ad;
			DROP TABLE IF EXISTS items_fts;
		`)
		if err != nil {
			return fmt.Errorf("drop old FTS schema: %w", err)
		}
		// Fall through to create fresh schema below.
		fallthrough

	case version == 0:
		// Fresh install or pre-FTS: create tables from scratch.
		ftsSchema := `
			CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
				title,
				summary,
				source_name,
				author,
				content='items',
				content_rowid='rowid',
				tokenize='unicode61 remove_diacritics 2'
			);

			CREATE TRIGGER IF NOT EXISTS items_ai AFTER INSERT ON items BEGIN
				INSERT INTO items_fts(rowid, title, summary, source_name, author)
				VALUES (new.rowid, new.title, new.summary, new.source_name, new.author);
			END;

			CREATE TRIGGER IF NOT EXISTS items_au AFTER UPDATE OF title, summary, source_name, author ON items BEGIN
				INSERT INTO items_fts(items_fts, rowid, title, summary, source_name, author)
				VALUES ('delete', old.rowid, old.title, old.summary, old.source_name, old.author);
				INSERT INTO items_fts(rowid, title, summary, source_name, author)
				VALUES (new.rowid, new.title, new.summary, new.source_name, new.author);
			END;

			CREATE TRIGGER IF NOT EXISTS items_ad AFTER DELETE ON items BEGIN
				INSERT INTO items_fts(items_fts, rowid, title, summary, source_name, author)
				VALUES ('delete', old.rowid, old.title, old.summary, old.source_name, old.author);
			END;
		`
		if _, err := s.db.Exec(ftsSchema); err != nil {
			return fmt.Errorf("create FTS schema: %w", err)
		}
	}

	// Set version to current.
	if _, err := s.db.Exec("PRAGMA user_version = 2"); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}

	return nil
}

// rebuildFTS populates the FTS index from all existing items.
// Only rebuilds if the index is empty but items exist,
// avoiding unnecessary work on normal startups.
func (s *Store) rebuildFTS() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ftsCount int
	if err := s.db.QueryRow("SELECT count(*) FROM items_fts_docsize").Scan(&ftsCount); err != nil {
		return fmt.Errorf("check FTS docsize count: %w", err)
	}

	if ftsCount > 0 {
		return nil // Index already populated, triggers keep it in sync
	}

	var itemsCount int
	if err := s.db.QueryRow("SELECT count(*) FROM items").Scan(&itemsCount); err != nil {
		return fmt.Errorf("check items count: %w", err)
	}

	if itemsCount == 0 {
		return nil // Nothing to index
	}

	_, err := s.db.Exec("INSERT INTO items_fts(items_fts) VALUES('rebuild')")
	if err != nil {
		return fmt.Errorf("rebuild FTS index: %w", err)
	}
	return nil
}

// SearchFTS performs a full-text search using FTS5 and returns matching items
// ordered by BM25 relevance.
//
// Column weights: title=10, summary=5, source_name=1, author=3.
// If the raw query fails (FTS5 syntax error), retries as a quoted literal string.
//
// Thread-safe: acquires read lock.
func (s *Store) SearchFTS(query string, limit int) ([]Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	items, err := s.searchFTSRaw(query, limit)
	if err != nil {
		// Retry with quoted literal on FTS5 syntax error.
		// This handles queries like "C++", unclosed quotes, reserved words.
		escaped := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
		items, err = s.searchFTSRaw(escaped, limit)
		if err != nil {
			return nil, fmt.Errorf("FTS search: %w", err)
		}
	}
	return items, nil
}

func (s *Store) searchFTSRaw(query string, limit int) ([]Item, error) {
	// bm25() returns values where smaller (more negative) = more relevant.
	// ORDER BY bm25(...) sorts most relevant first.
	// Column weights: title=10, summary=5, source_name=1, author=3.
	rows, err := s.db.Query(`
		SELECT i.id, i.source_type, i.source_name, i.title, i.summary,
			   i.url, i.author, i.published_at, i.fetched_at, i.read, i.saved
		FROM items_fts
		JOIN items i ON i.rowid = items_fts.rowid
		WHERE items_fts MATCH ?
		ORDER BY bm25(items_fts, 10.0, 5.0, 1.0, 3.0)
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		var read, saved int
		if err := rows.Scan(
			&item.ID, &item.SourceType, &item.SourceName, &item.Title,
			&item.Summary, &item.URL, &item.Author, &item.Published,
			&item.Fetched, &read, &saved,
		); err != nil {
			return nil, fmt.Errorf("scan FTS result: %w", err)
		}
		item.Read = read != 0
		item.Saved = saved != 0
		items = append(items, item)
	}
	return items, rows.Err()
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

// CountItemsNeedingEmbedding returns the number of items with NULL embedding.
// Thread-safe: acquires read lock.
func (s *Store) CountItemsNeedingEmbedding() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE embedding IS NULL").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count items needing embedding: %w", err)
	}
	return count, nil
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

// ClearAllEmbeddings sets all embeddings to NULL.
// Returns the number of items that had their embeddings cleared.
// Thread-safe: acquires write lock.
func (s *Store) ClearAllEmbeddings() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("UPDATE items SET embedding = NULL WHERE embedding IS NOT NULL")
	if err != nil {
		return 0, fmt.Errorf("clear embeddings: %w", err)
	}
	return result.RowsAffected()
}

// CountAllItems returns the total number of items in the database.
// Thread-safe: acquires read lock.
func (s *Store) CountAllItems() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count all items: %w", err)
	}
	return count, nil
}