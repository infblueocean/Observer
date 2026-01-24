package correlation

import (
	"database/sql"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

// EntityType categorizes extracted entities
type EntityType string

const (
	EntityPerson       EntityType = "person"
	EntityOrganization EntityType = "organization"
	EntityLocation     EntityType = "location"
	EntityTopic        EntityType = "topic"
	EntityTicker       EntityType = "ticker"  // Stock symbols
	EntityMarket       EntityType = "market"  // Prediction market
	EntityEvent        EntityType = "event"   // Named events (elections, etc.)
)

// Entity represents an extracted entity from content
type Entity struct {
	ID         string
	Name       string
	Type       EntityType
	Aliases    []string  // Alternative names
	FirstSeen  time.Time
	LastSeen   time.Time
	Mentions   int       // Total mention count
}

// ItemEntity links items to entities
type ItemEntity struct {
	ItemID   string
	EntityID string
	Context  string  // The sentence/phrase where entity appeared
	Salience float64 // How important is this entity to the item (0-1)
}

// Correlation represents a connection between items
type Correlation struct {
	ID           string
	Type         CorrelationType
	Items        []string    // Item IDs involved
	Entities     []string    // Shared entity IDs
	Strength     float64     // How strong is the correlation (0-1)
	CreatedAt    time.Time
	Description  string      // Human-readable explanation
}

// CorrelationType categorizes correlations
type CorrelationType string

const (
	// Same entities appearing across time
	CorrelationEntity CorrelationType = "entity"

	// Same topic/theme evolving
	CorrelationTopic CorrelationType = "topic"

	// Prediction → Outcome (market predicted, now resolved)
	CorrelationPrediction CorrelationType = "prediction"

	// Multiple sources covering same story
	CorrelationCoverage CorrelationType = "coverage"

	// Cause → Effect relationships
	CorrelationCausal CorrelationType = "causal"

	// Similar events in history
	CorrelationHistorical CorrelationType = "historical"
)

// Thread represents an ongoing story/topic over time
type Thread struct {
	ID          string
	Name        string
	Description string
	Entities    []string    // Core entities in this thread
	Items       []string    // Item IDs in chronological order
	FirstItem   time.Time
	LastItem    time.Time
	Active      bool        // Still developing?
}

// Engine handles correlation detection and storage
type Engine struct {
	db          *sql.DB
	extractor   EntityExtractor
	correlator  AICorrelator
}

// EntityExtractor extracts entities from text (AI-powered)
type EntityExtractor interface {
	// Extract entities from item title and content
	Extract(item feeds.Item) ([]ItemEntity, error)
}

// AICorrelator finds deeper correlations (AI-powered)
type AICorrelator interface {
	// FindCorrelations between a new item and historical items
	FindCorrelations(newItem feeds.Item, candidates []feeds.Item) ([]Correlation, error)

	// ExplainCorrelation generates human-readable explanation
	ExplainCorrelation(corr Correlation, items []feeds.Item) (string, error)

	// IdentifyThread determines if item belongs to existing thread
	IdentifyThread(item feeds.Item, threads []Thread) (*Thread, float64, error)
}

// NewEngine creates a correlation engine
func NewEngine(db *sql.DB, extractor EntityExtractor, correlator AICorrelator) (*Engine, error) {
	e := &Engine{
		db:         db,
		extractor:  extractor,
		correlator: correlator,
	}

	if err := e.migrate(); err != nil {
		return nil, err
	}

	return e, nil
}

func (e *Engine) migrate() error {
	schema := `
	-- Entities table
	CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		aliases TEXT,  -- JSON array
		first_seen DATETIME,
		last_seen DATETIME,
		mentions INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);
	CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);

	-- Item-Entity relationships
	CREATE TABLE IF NOT EXISTS item_entities (
		item_id TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		context TEXT,
		salience REAL DEFAULT 0.5,
		PRIMARY KEY (item_id, entity_id)
	);
	CREATE INDEX IF NOT EXISTS idx_item_entities_entity ON item_entities(entity_id);

	-- Correlations
	CREATE TABLE IF NOT EXISTS correlations (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		items TEXT NOT NULL,      -- JSON array of item IDs
		entities TEXT,            -- JSON array of entity IDs
		strength REAL DEFAULT 0.5,
		description TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_correlations_type ON correlations(type);

	-- Threads (ongoing stories)
	CREATE TABLE IF NOT EXISTS threads (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		entities TEXT,            -- JSON array
		items TEXT,               -- JSON array of item IDs (chronological)
		first_item DATETIME,
		last_item DATETIME,
		active INTEGER DEFAULT 1
	);
	`
	_, err := e.db.Exec(schema)
	return err
}

// ProcessItem extracts entities and finds correlations for a new item
func (e *Engine) ProcessItem(item feeds.Item) (*ItemCorrelations, error) {
	result := &ItemCorrelations{
		Item: item,
	}

	// Extract entities
	if e.extractor != nil {
		entities, err := e.extractor.Extract(item)
		if err == nil {
			result.Entities = entities
			e.storeEntities(item, entities)
		}
	}

	// Find related historical items
	related, err := e.findRelatedItems(item, result.Entities)
	if err == nil {
		result.RelatedItems = related
	}

	// Find/create correlations
	if e.correlator != nil && len(related) > 0 {
		correlations, err := e.correlator.FindCorrelations(item, related)
		if err == nil {
			result.Correlations = correlations
			e.storeCorrelations(correlations)
		}
	}

	// Check if this belongs to an existing thread
	threads, _ := e.getActiveThreads()
	if e.correlator != nil && len(threads) > 0 {
		thread, confidence, err := e.correlator.IdentifyThread(item, threads)
		if err == nil && confidence > 0.7 {
			result.Thread = thread
			e.addItemToThread(thread.ID, item.ID)
		}
	}

	return result, nil
}

// ItemCorrelations holds all correlation data for an item
type ItemCorrelations struct {
	Item         feeds.Item
	Entities     []ItemEntity
	RelatedItems []feeds.Item
	Correlations []Correlation
	Thread       *Thread
}

func (e *Engine) storeEntities(item feeds.Item, entities []ItemEntity) error {
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, ie := range entities {
		// Upsert entity
		_, err := tx.Exec(`
			INSERT INTO entities (id, name, type, first_seen, last_seen, mentions)
			VALUES (?, ?, ?, ?, ?, 1)
			ON CONFLICT(id) DO UPDATE SET
				last_seen = excluded.last_seen,
				mentions = mentions + 1
		`, ie.EntityID, ie.EntityID, "unknown", item.Published, item.Published)
		if err != nil {
			continue
		}

		// Link to item
		_, err = tx.Exec(`
			INSERT OR REPLACE INTO item_entities (item_id, entity_id, context, salience)
			VALUES (?, ?, ?, ?)
		`, item.ID, ie.EntityID, ie.Context, ie.Salience)
		if err != nil {
			continue
		}
	}

	return tx.Commit()
}

func (e *Engine) findRelatedItems(item feeds.Item, entities []ItemEntity) ([]feeds.Item, error) {
	if len(entities) == 0 {
		// Fall back to text similarity search
		return e.findByTextSimilarity(item)
	}

	// Find items sharing entities
	entityIDs := make([]string, len(entities))
	for i, ent := range entities {
		entityIDs[i] = ent.EntityID
	}

	placeholders := strings.Repeat("?,", len(entityIDs))
	placeholders = placeholders[:len(placeholders)-1]

	query := `
		SELECT DISTINCT i.id, i.source_type, i.source_name, i.title, i.summary,
		       i.url, i.author, i.published_at, i.fetched_at
		FROM items i
		JOIN item_entities ie ON i.id = ie.item_id
		WHERE ie.entity_id IN (` + placeholders + `)
		  AND i.id != ?
		ORDER BY i.published_at DESC
		LIMIT 20
	`

	args := make([]interface{}, len(entityIDs)+1)
	for i, id := range entityIDs {
		args[i] = id
	}
	args[len(entityIDs)] = item.ID

	rows, err := e.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []feeds.Item
	for rows.Next() {
		var it feeds.Item
		var sourceType string
		err := rows.Scan(&it.ID, &sourceType, &it.SourceName, &it.Title,
			&it.Summary, &it.URL, &it.Author, &it.Published, &it.Fetched)
		if err != nil {
			continue
		}
		it.Source = feeds.SourceType(sourceType)
		items = append(items, it)
	}

	return items, nil
}

func (e *Engine) findByTextSimilarity(item feeds.Item) ([]feeds.Item, error) {
	// Simple keyword extraction from title
	words := strings.Fields(strings.ToLower(item.Title))
	var keywords []string
	for _, w := range words {
		if len(w) > 4 { // Skip short words
			keywords = append(keywords, w)
		}
	}

	if len(keywords) == 0 {
		return nil, nil
	}

	// Build LIKE query for keywords
	var conditions []string
	var args []interface{}
	for _, kw := range keywords[:min(5, len(keywords))] {
		conditions = append(conditions, "LOWER(title) LIKE ?")
		args = append(args, "%"+kw+"%")
	}
	args = append(args, item.ID)

	query := `
		SELECT id, source_type, source_name, title, summary, url, author, published_at, fetched_at
		FROM items
		WHERE (` + strings.Join(conditions, " OR ") + `)
		  AND id != ?
		ORDER BY published_at DESC
		LIMIT 10
	`

	rows, err := e.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []feeds.Item
	for rows.Next() {
		var it feeds.Item
		var sourceType string
		err := rows.Scan(&it.ID, &sourceType, &it.SourceName, &it.Title,
			&it.Summary, &it.URL, &it.Author, &it.Published, &it.Fetched)
		if err != nil {
			continue
		}
		it.Source = feeds.SourceType(sourceType)
		items = append(items, it)
	}

	return items, nil
}

func (e *Engine) storeCorrelations(correlations []Correlation) error {
	// TODO: Implement correlation storage
	return nil
}

func (e *Engine) getActiveThreads() ([]Thread, error) {
	// TODO: Implement thread retrieval
	return nil, nil
}

func (e *Engine) addItemToThread(threadID, itemID string) error {
	// TODO: Implement adding item to thread
	return nil
}

// GetEntityTimeline returns all items mentioning an entity, chronologically
func (e *Engine) GetEntityTimeline(entityID string) ([]feeds.Item, error) {
	query := `
		SELECT i.id, i.source_type, i.source_name, i.title, i.summary,
		       i.url, i.author, i.published_at, i.fetched_at
		FROM items i
		JOIN item_entities ie ON i.id = ie.item_id
		WHERE ie.entity_id = ?
		ORDER BY i.published_at ASC
	`

	rows, err := e.db.Query(query, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []feeds.Item
	for rows.Next() {
		var it feeds.Item
		var sourceType string
		err := rows.Scan(&it.ID, &sourceType, &it.SourceName, &it.Title,
			&it.Summary, &it.URL, &it.Author, &it.Published, &it.Fetched)
		if err != nil {
			continue
		}
		it.Source = feeds.SourceType(sourceType)
		items = append(items, it)
	}

	return items, nil
}

// GetTopEntities returns most-mentioned entities in a time range
func (e *Engine) GetTopEntities(since time.Time, limit int) ([]Entity, error) {
	query := `
		SELECT e.id, e.name, e.type, e.first_seen, e.last_seen,
		       COUNT(ie.item_id) as recent_mentions
		FROM entities e
		JOIN item_entities ie ON e.id = ie.entity_id
		JOIN items i ON ie.item_id = i.id
		WHERE i.published_at > ?
		GROUP BY e.id
		ORDER BY recent_mentions DESC
		LIMIT ?
	`

	rows, err := e.db.Query(query, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var ent Entity
		var mentions int
		err := rows.Scan(&ent.ID, &ent.Name, &ent.Type, &ent.FirstSeen, &ent.LastSeen, &mentions)
		if err != nil {
			continue
		}
		ent.Mentions = mentions
		entities = append(entities, ent)
	}

	return entities, nil
}

// GetThreads returns all threads, optionally filtered by active status
func (e *Engine) GetThreads(activeOnly bool) ([]Thread, error) {
	query := `SELECT id, name, description, first_item, last_item, active FROM threads`
	if activeOnly {
		query += ` WHERE active = 1`
	}
	query += ` ORDER BY last_item DESC`

	rows, err := e.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var t Thread
		var active int
		err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.FirstItem, &t.LastItem, &active)
		if err != nil {
			continue
		}
		t.Active = active == 1
		threads = append(threads, t)
	}

	return threads, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
