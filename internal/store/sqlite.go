package store

import (
	"database/sql"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	_ "github.com/mattn/go-sqlite3"
)

// Store handles persistence of feed items
type Store struct {
	db *sql.DB
}

// New creates a new SQLite store
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
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
	`
	_, err := s.db.Exec(schema)
	return err
}

// SaveItems saves or updates items, returns count of new items
func (s *Store) SaveItems(items []feeds.Item) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
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
		return 0, err
	}
	defer stmt.Close()

	newCount := 0
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
			continue
		}
		rows, _ := result.RowsAffected()
		if rows > 0 {
			newCount++
		}
	}

	return newCount, tx.Commit()
}

// GetItems retrieves items with optional filters
func (s *Store) GetItems(limit int, includeRead bool) ([]feeds.Item, error) {
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
		return nil, err
	}
	defer rows.Close()

	var items []feeds.Item
	for rows.Next() {
		var item feeds.Item
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
			continue
		}
		item.Source = feeds.SourceType(sourceType)
		items = append(items, item)
	}

	return items, nil
}

// MarkRead marks an item as read
func (s *Store) MarkRead(id string) error {
	_, err := s.db.Exec("UPDATE items SET read = 1 WHERE id = ?", id)
	return err
}

// MarkSaved toggles saved state
func (s *Store) MarkSaved(id string, saved bool) error {
	val := 0
	if saved {
		val = 1
	}
	_, err := s.db.Exec("UPDATE items SET saved = ? WHERE id = ?", val, id)
	return err
}

// UpdateSourceStatus updates the last fetch time for a source
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
	return err
}

// GetSourceStatus gets the last fetch time for a source
func (s *Store) GetSourceStatus(name string) (time.Time, error) {
	var lastFetched sql.NullTime
	err := s.db.QueryRow("SELECT last_fetched_at FROM sources WHERE name = ?", name).Scan(&lastFetched)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return lastFetched.Time, nil
}

// ItemCount returns total item count
func (s *Store) ItemCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE hidden = 0").Scan(&count)
	return count, err
}

// UnreadCount returns unread item count
func (s *Store) UnreadCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE hidden = 0 AND read = 0").Scan(&count)
	return count, err
}

// Close closes the database
func (s *Store) Close() error {
	return s.db.Close()
}
