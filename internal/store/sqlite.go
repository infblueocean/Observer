package store

import (
	"database/sql"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/logging"
	_ "modernc.org/sqlite" // Pure-Go SQLite driver (no CGO required)
)

// Store handles persistence of feed items
type Store struct {
	db *sql.DB
}

// New creates a new SQLite store
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		logging.Error("Failed to open database", "path", dbPath, "error", err)
		return nil, err
	}

	// SQLite connection pool limits
	// Allow multiple readers with WAL mode (single writer is handled by SQLite internally)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	// Enable WAL mode and verify it was set
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		logging.Error("Failed to set WAL mode", "error", err)
		db.Close()
		return nil, err
	}
	if journalMode != "wal" {
		logging.Warn("WAL mode not enabled", "actual_mode", journalMode)
	}

	// Apply other SQLite PRAGMA settings for concurrent access and durability
	pragmas := []string{
		"PRAGMA busy_timeout=5000",     // Wait 5s when database is locked
		"PRAGMA synchronous=NORMAL",    // Balance between safety and performance
		"PRAGMA foreign_keys=ON",       // Enable foreign key constraints
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			logging.Error("Failed to set PRAGMA", "pragma", pragma, "error", err)
			db.Close()
			return nil, err
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		logging.Error("Failed to migrate database", "error", err)
		db.Close()
		return nil, err
	}

	logging.Info("Database initialized", "path", dbPath)
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

	CREATE TABLE IF NOT EXISTS analyses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		item_id TEXT NOT NULL,
		provider TEXT NOT NULL,
		model TEXT,
		prompt TEXT,
		raw_response TEXT NOT NULL,
		content TEXT NOT NULL,
		error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (item_id) REFERENCES items(id)
	);

	CREATE INDEX IF NOT EXISTS idx_analyses_item ON analyses(item_id);
	CREATE INDEX IF NOT EXISTS idx_analyses_created ON analyses(created_at DESC);

	CREATE TABLE IF NOT EXISTS top_stories_cache (
		item_id TEXT PRIMARY KEY,
		title TEXT,
		label TEXT,
		reason TEXT,
		zinger TEXT,
		first_seen DATETIME NOT NULL,
		last_seen DATETIME NOT NULL,
		hit_count INTEGER DEFAULT 1,
		miss_count INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_top_stories_last_seen ON top_stories_cache(last_seen DESC);

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

// GetLastSession returns when the user was last active
func (s *Store) GetLastSession() (time.Time, error) {
	var endedAt sql.NullTime
	err := s.db.QueryRow(`
		SELECT ended_at FROM sessions
		WHERE ended_at IS NOT NULL
		ORDER BY ended_at DESC
		LIMIT 1
	`).Scan(&endedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return endedAt.Time, nil
}

// StartSession records a new session
func (s *Store) StartSession() (int64, error) {
	result, err := s.db.Exec(`INSERT INTO sessions (started_at) VALUES (?)`, time.Now())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// EndSession marks the current session as ended
func (s *Store) EndSession(sessionID int64) error {
	_, err := s.db.Exec(`UPDATE sessions SET ended_at = ? WHERE id = ?`, time.Now(), sessionID)
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
			logging.Warn("Failed to save item", "error", err)
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
			logging.Warn("Failed to scan row", "error", err)
			continue
		}
		item.Source = feeds.SourceType(sourceType)
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
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

// AnalysisRecord represents a stored AI analysis
type AnalysisRecord struct {
	ID          int64
	ItemID      string
	Provider    string
	Model       string
	Prompt      string
	RawResponse string
	Content     string
	Error       string
	CreatedAt   time.Time
}

// SaveAnalysis saves an AI analysis to the database
func (s *Store) SaveAnalysis(itemID, provider, model, prompt, rawResponse, content, errMsg string) error {
	_, err := s.db.Exec(`
		INSERT INTO analyses (item_id, provider, model, prompt, raw_response, content, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, itemID, provider, model, prompt, rawResponse, content, errMsg)
	if err != nil {
		logging.Error("Failed to save analysis", "item_id", itemID, "error", err)
		return err
	}
	logging.Info("Analysis saved", "item_id", itemID, "provider", provider, "content_len", len(content))
	return nil
}

// GetAnalysisContent retrieves just the content fields for an item's analysis
// This is the simpler interface used by brain trust
func (s *Store) GetAnalysisContent(itemID string) (content, provider, model string, found bool) {
	err := s.db.QueryRow(`
		SELECT content, provider, model
		FROM analyses
		WHERE item_id = ? AND content != ''
		ORDER BY created_at DESC
		LIMIT 1
	`, itemID).Scan(&content, &provider, &model)
	if err != nil {
		return "", "", "", false
	}
	return content, provider, model, true
}

// GetAnalysis retrieves the most recent analysis for an item
func (s *Store) GetAnalysis(itemID string) (*AnalysisRecord, error) {
	var record AnalysisRecord
	var errMsg sql.NullString
	err := s.db.QueryRow(`
		SELECT id, item_id, provider, model, prompt, raw_response, content, error, created_at
		FROM analyses
		WHERE item_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, itemID).Scan(
		&record.ID,
		&record.ItemID,
		&record.Provider,
		&record.Model,
		&record.Prompt,
		&record.RawResponse,
		&record.Content,
		&errMsg,
		&record.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record.Error = errMsg.String
	return &record, nil
}

// GetAllAnalysesForItem retrieves all analyses for an item (history)
func (s *Store) GetAllAnalysesForItem(itemID string) ([]AnalysisRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, item_id, provider, model, prompt, raw_response, content, error, created_at
		FROM analyses
		WHERE item_id = ?
		ORDER BY created_at DESC
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []AnalysisRecord
	for rows.Next() {
		var record AnalysisRecord
		var errMsg sql.NullString
		err := rows.Scan(
			&record.ID,
			&record.ItemID,
			&record.Provider,
			&record.Model,
			&record.Prompt,
			&record.RawResponse,
			&record.Content,
			&errMsg,
			&record.CreatedAt,
		)
		if err != nil {
			logging.Warn("Failed to scan row", "error", err)
			continue
		}
		record.Error = errMsg.String
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

// AnalysisCount returns total analysis count
func (s *Store) AnalysisCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM analyses").Scan(&count)
	return count, err
}

// TopStoryCacheEntry represents a cached top story
type TopStoryCacheEntry struct {
	ItemID    string
	Title     string
	Label     string
	Reason    string
	Zinger    string
	FirstSeen time.Time
	LastSeen  time.Time
	HitCount  int
	MissCount int
}

// SaveTopStoriesCache saves the top stories cache to the database
func (s *Store) SaveTopStoriesCache(entries []TopStoryCacheEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear old entries (older than 48 hours)
	_, err = tx.Exec("DELETE FROM top_stories_cache WHERE last_seen < ?", time.Now().Add(-48*time.Hour))
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO top_stories_cache (item_id, title, label, reason, zinger, first_seen, last_seen, hit_count, miss_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			title = excluded.title,
			label = excluded.label,
			reason = excluded.reason,
			zinger = excluded.zinger,
			last_seen = excluded.last_seen,
			hit_count = excluded.hit_count,
			miss_count = excluded.miss_count
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		_, err = stmt.Exec(e.ItemID, e.Title, e.Label, e.Reason, e.Zinger, e.FirstSeen, e.LastSeen, e.HitCount, e.MissCount)
		if err != nil {
			logging.Error("Failed to save top story cache entry", "item_id", e.ItemID, "error", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	logging.Debug("Top stories cache saved", "count", len(entries))
	return nil
}

// LoadTopStoriesCache loads the top stories cache from the database
func (s *Store) LoadTopStoriesCache() ([]TopStoryCacheEntry, error) {
	// Only load recent entries (last 24 hours)
	rows, err := s.db.Query(`
		SELECT item_id, title, label, reason, zinger, first_seen, last_seen, hit_count, miss_count
		FROM top_stories_cache
		WHERE last_seen > ?
		ORDER BY hit_count DESC
	`, time.Now().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []TopStoryCacheEntry
	for rows.Next() {
		var e TopStoryCacheEntry
		var title, label, reason, zinger sql.NullString
		err := rows.Scan(&e.ItemID, &title, &label, &reason, &zinger, &e.FirstSeen, &e.LastSeen, &e.HitCount, &e.MissCount)
		if err != nil {
			logging.Warn("Failed to scan row", "error", err)
			continue
		}
		e.Title = title.String
		e.Label = label.String
		e.Reason = reason.String
		e.Zinger = zinger.String
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	logging.Debug("Top stories cache loaded", "count", len(entries))
	return entries, nil
}

// DB returns the underlying database connection for use by other packages
func (s *Store) DB() *sql.DB {
	return s.db
}

// Close closes the database
func (s *Store) Close() error {
	return s.db.Close()
}
