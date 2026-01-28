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
	"math"
	"time"

	"github.com/abelbrown/observer/internal/logging"

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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		embedding BLOB
	);

	CREATE INDEX IF NOT EXISTS idx_items_published ON items(published_at DESC);
	CREATE INDEX IF NOT EXISTS idx_items_source ON items(source_name);
	CREATE INDEX IF NOT EXISTS idx_items_read ON items(read);
	CREATE INDEX IF NOT EXISTS idx_items_url ON items(url);
	CREATE INDEX IF NOT EXISTS idx_items_no_embedding ON items(embedding) WHERE embedding IS NULL;

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
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Migration: add embedding column if it doesn't exist (for existing DBs)
	if !s.columnExists("items", "embedding") {
		if _, err := s.db.Exec("ALTER TABLE items ADD COLUMN embedding BLOB"); err != nil {
			return fmt.Errorf("failed to add embedding column: %w", err)
		}
	}

	return nil
}

// isValidIdentifier checks if a string is a safe SQL identifier (alphanumeric and underscore only).
func isValidIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// columnExists checks if a column exists in a table using pragma_table_info.
// This is more reliable than checking error messages from ALTER TABLE.
func (s *Store) columnExists(table, column string) bool {
	// Validate identifiers to prevent SQL injection
	if !isValidIdentifier(table) || !isValidIdentifier(column) {
		logging.Error("Invalid identifier in columnExists", "table", table, "column", column)
		return false
	}
	// Table name can't be parameterized, but column name can be
	query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?", table)
	var count int
	if err := s.db.QueryRow(query, column).Scan(&count); err != nil {
		logging.Error("columnExists check failed", "table", table, "column", column, "error", err)
		return false
	}
	return count > 0
}

// SaveItems saves or updates items in a single transaction.
//
// Returns the number of rows saved and any error. Individual item failures are
// logged but do not stop the transaction - other items will still be saved.
// If the transaction itself fails (begin/commit), an error is returned.
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

	var saved int
	var failedIDs []string

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
			logging.Debug("Failed to save item",
				"id", item.ID,
				"title", truncateString(item.Title, 50),
				"error", err)
			failedIDs = append(failedIDs, item.ID)
			continue
		}
		rows, err := result.RowsAffected()
		if err == nil && rows > 0 {
			saved++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	if len(failedIDs) > 0 {
		logging.Warn("Some items failed to save",
			"failed_count", len(failedIDs),
			"saved_count", saved,
			"failed_ids", failedIDs)
	}

	return saved, nil
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

// SaveEmbedding saves the embedding vector for a single item.
// Returns an error if the item does not exist.
func (s *Store) SaveEmbedding(id string, embedding []float32) error {
	blob := serializeEmbedding(embedding)
	result, err := s.db.Exec("UPDATE items SET embedding = ? WHERE id = ?", blob, id)
	if err != nil {
		return fmt.Errorf("failed to save embedding: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("item not found: %s", id)
	}
	return nil
}

// SaveEmbeddings saves embeddings for multiple items in a transaction.
// Returns an error if any item does not exist.
func (s *Store) SaveEmbeddings(embeddings map[string][]float32) error {
	if len(embeddings) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE items SET embedding = ? WHERE id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for id, embedding := range embeddings {
		blob := serializeEmbedding(embedding)
		result, err := stmt.Exec(blob, id)
		if err != nil {
			return fmt.Errorf("failed to save embedding for %s: %w", id, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to check rows affected for %s: %w", id, err)
		}
		if rows == 0 {
			return fmt.Errorf("item not found: %s", id)
		}
	}

	return tx.Commit()
}

// GetItemsWithoutEmbedding retrieves items that don't have embeddings yet.
// Results are ordered by published time, newest first.
func (s *Store) GetItemsWithoutEmbedding(limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, source_type, source_name, source_url, title, summary, content, url, author, published_at, fetched_at, read, saved
		FROM items
		WHERE hidden = 0 AND embedding IS NULL
		ORDER BY published_at DESC
		LIMIT ?
	`

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	return scanItems(rows)
}

// GetItemsWithoutEmbeddingCount returns the count of items without embeddings.
func (s *Store) GetItemsWithoutEmbeddingCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE hidden = 0 AND embedding IS NULL").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count items without embeddings: %w", err)
	}
	return count, nil
}

// GetEmbedding retrieves the embedding for a single item.
// Returns nil if the item doesn't have an embedding.
func (s *Store) GetEmbedding(id string) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRow("SELECT embedding FROM items WHERE id = ?", id).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get embedding: %w", err)
	}
	if blob == nil {
		return nil, nil
	}
	return deserializeEmbedding(blob), nil
}

// GetItemsWithEmbeddings retrieves items that have embeddings.
// Results include the embedding vector and are ordered by published time.
func (s *Store) GetItemsWithEmbeddings(limit int, since time.Time) ([]Item, error) {
	if limit <= 0 {
		limit = 500
	}

	query := `
		SELECT id, source_type, source_name, source_url, title, summary, content, url, author, published_at, fetched_at, read, saved, embedding
		FROM items
		WHERE hidden = 0 AND embedding IS NOT NULL AND published_at > ?
		ORDER BY published_at DESC
		LIMIT ?
	`

	rows, err := s.db.Query(query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	return scanItemsWithEmbedding(rows)
}

// scanItemsWithEmbedding scans rows including the embedding column.
func scanItemsWithEmbedding(rows *sql.Rows) ([]Item, error) {
	var items []Item
	for rows.Next() {
		var item Item
		var sourceType string
		var blob []byte
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
			&blob,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		item.Source = SourceType(sourceType)
		if blob != nil {
			item.Embedding = deserializeEmbedding(blob)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return items, nil
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// serializeEmbedding converts a float32 slice to bytes for storage.
// Uses little-endian IEEE 754 format (4 bytes per float).
func serializeEmbedding(embedding []float32) []byte {
	if embedding == nil {
		return nil
	}
	blob := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		bits := float32ToBits(v)
		blob[i*4] = byte(bits)
		blob[i*4+1] = byte(bits >> 8)
		blob[i*4+2] = byte(bits >> 16)
		blob[i*4+3] = byte(bits >> 24)
	}
	return blob
}

// deserializeEmbedding converts bytes back to a float32 slice.
func deserializeEmbedding(blob []byte) []float32 {
	if len(blob) == 0 || len(blob)%4 != 0 {
		return nil
	}
	embedding := make([]float32, len(blob)/4)
	for i := range embedding {
		bits := uint32(blob[i*4]) |
			uint32(blob[i*4+1])<<8 |
			uint32(blob[i*4+2])<<16 |
			uint32(blob[i*4+3])<<24
		embedding[i] = bitsToFloat32(bits)
	}
	return embedding
}

// float32ToBits converts a float32 to its IEEE 754 bit representation.
func float32ToBits(f float32) uint32 {
	return math.Float32bits(f)
}

// bitsToFloat32 converts IEEE 754 bits back to float32.
func bitsToFloat32(bits uint32) float32 {
	return math.Float32frombits(bits)
}
