package correlation

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/logging"
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

// ActivityType represents a type of correlation activity
type ActivityType string

const (
	ActivityExtract    ActivityType = "extract"
	ActivityCluster    ActivityType = "cluster"
	ActivityDuplicate  ActivityType = "duplicate"
	ActivityDisagree   ActivityType = "disagree"
)

// Activity represents a single correlation engine action
type Activity struct {
	Type      ActivityType
	Time      time.Time
	ItemTitle string
	Details   string // e.g., "found: US, China, Trade" or "joined cluster with 5 items"
}

// Stats holds correlation engine statistics
type Stats struct {
	ItemsProcessed   int
	EntitiesFound    int
	ClustersFormed   int
	DuplicatesFound  int
	DisagreementsFound int
	StartTime        time.Time
}

// Engine handles correlation detection and storage
type Engine struct {
	db          *sql.DB
	extractor   EntityExtractor
	correlator  AICorrelator

	// Activity tracking for transparency
	recentActivity []Activity
	activityIndex  int
	stats          Stats

	// Duplicate tracking (in-memory for speed)
	duplicateGroups map[string]*DuplicateGroup // simhash key -> group
	itemToGroup     map[string]string          // item ID -> group ID
	itemHashes      map[string]uint64          // item ID -> simhash

	// Cluster tracking
	clusters       map[string]*Cluster      // cluster ID -> cluster
	itemToCluster  map[string]string        // item ID -> cluster ID
	clusterHistory map[string][]VelocitySnapshot // cluster ID -> velocity history

	// Entity tracking (in-memory cache)
	itemEntities   map[string][]ItemEntity       // item ID -> entities
	entityVelocity map[string][]VelocitySnapshot // entity ID -> velocity history

	// Claim and disagreement tracking
	itemClaims          map[string][]ExtractedClaim // item ID -> extracted claims
	clusterDisagreements map[string][]DisagreementInfo // cluster ID -> disagreements
}

const maxActivityEntries = 50

// addActivity adds an activity to the ring buffer
func (e *Engine) addActivity(actType ActivityType, itemTitle, details string) {
	if e.recentActivity == nil {
		e.recentActivity = make([]Activity, maxActivityEntries)
	}
	e.recentActivity[e.activityIndex] = Activity{
		Type:      actType,
		Time:      time.Now(),
		ItemTitle: itemTitle,
		Details:   details,
	}
	e.activityIndex = (e.activityIndex + 1) % maxActivityEntries
}

// GetRecentActivity returns recent activities (newest first)
func (e *Engine) GetRecentActivity(count int) []Activity {
	if e.recentActivity == nil {
		return nil
	}
	if count > maxActivityEntries {
		count = maxActivityEntries
	}

	result := make([]Activity, 0, count)
	idx := (e.activityIndex - 1 + maxActivityEntries) % maxActivityEntries
	for i := 0; i < count; i++ {
		act := e.recentActivity[idx]
		if act.Time.IsZero() {
			break
		}
		result = append(result, act)
		idx = (idx - 1 + maxActivityEntries) % maxActivityEntries
	}
	return result
}

// GetStats returns current engine statistics
func (e *Engine) GetStats() Stats {
	return e.stats
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
		db:                   db,
		extractor:            extractor,
		correlator:           correlator,
		duplicateGroups:      make(map[string]*DuplicateGroup),
		itemToGroup:          make(map[string]string),
		itemHashes:           make(map[string]uint64),
		clusters:             make(map[string]*Cluster),
		itemToCluster:        make(map[string]string),
		clusterHistory:       make(map[string][]VelocitySnapshot),
		itemEntities:         make(map[string][]ItemEntity),
		entityVelocity:       make(map[string][]VelocitySnapshot),
		itemClaims:           make(map[string][]ExtractedClaim),
		clusterDisagreements: make(map[string][]DisagreementInfo),
	}

	if err := e.migrate(); err != nil {
		return nil, err
	}

	return e, nil
}

// NewEngineSimple creates a correlation engine with just cheap extraction (no AI)
func NewEngineSimple(db *sql.DB) (*Engine, error) {
	return NewEngine(db, NewCheapExtractor(), nil)
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

	// Calculate SimHash for duplicate detection
	hash := SimHash(item.Title)
	e.itemHashes[item.ID] = hash

	// Check for duplicates
	result.DuplicateGroup = e.findOrCreateDuplicateGroup(item, hash)

	// Extract entities
	if e.extractor != nil {
		entities, err := e.extractor.Extract(item)
		if err == nil {
			result.Entities = entities
			e.itemEntities[item.ID] = entities
			e.storeEntities(item, entities)
			if len(entities) > 0 {
				entityNames := make([]string, len(entities))
				for i, ent := range entities {
					entityNames[i] = ent.EntityID
				}
				logging.Info("Correlation: extracted entities", "item", truncateTitle(item.Title, 50), "entity_count", len(entities), "entities", entityNames)

				// Track activity
				e.stats.EntitiesFound += len(entities)
				details := fmt.Sprintf("→ %s", strings.Join(entityNames[:min(len(entityNames), 4)], ", "))
				if len(entityNames) > 4 {
					details += fmt.Sprintf(" +%d more", len(entityNames)-4)
				}
				e.addActivity(ActivityExtract, truncateTitle(item.Title, 40), details)
			}
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

	// Try to assign to a cluster (by entity overlap or title similarity)
	result.Cluster = e.findOrCreateCluster(item, result.Entities, hash)
	if result.Cluster != nil {
		logging.Debug("Correlation: item assigned to cluster", "item", truncateTitle(item.Title, 50), "cluster", result.Cluster.ID, "cluster_items", result.Cluster.ItemCount)

		// Track activity
		e.stats.ClustersFormed++
		details := fmt.Sprintf("joined cluster (%d items)", result.Cluster.ItemCount)
		e.addActivity(ActivityCluster, truncateTitle(item.Title, 40), details)
	}

	// Extract claims for disagreement detection
	text := item.Title + " " + item.Summary + " " + item.Content
	claims := ExtractClaims(text)
	if len(claims) > 0 {
		e.itemClaims[item.ID] = claims

		// Check for disagreements within cluster
		if result.Cluster != nil {
			e.checkClusterDisagreements(result.Cluster.ID, item.ID, claims)
		}
	}

	return result, nil
}

// checkClusterDisagreements checks for disagreements between this item and others in the cluster
func (e *Engine) checkClusterDisagreements(clusterID, newItemID string, newClaims []ExtractedClaim) {
	itemIDs := e.getClusterItemIDs(clusterID)

	for _, existingID := range itemIDs {
		if existingID == newItemID {
			continue
		}

		existingClaims := e.itemClaims[existingID]
		if len(existingClaims) == 0 {
			continue
		}

		// Detect conflicts
		conflicts := DetectConflicts(newClaims, existingClaims)
		if len(conflicts) > 0 {
			e.clusterDisagreements[clusterID] = append(e.clusterDisagreements[clusterID], conflicts...)
		}
	}
}

// HasDisagreements returns true if the cluster has detected disagreements
func (e *Engine) HasDisagreements(clusterID string) bool {
	return len(e.clusterDisagreements[clusterID]) > 0
}

// GetDisagreements returns disagreements for a cluster
func (e *Engine) GetDisagreements(clusterID string) []DisagreementInfo {
	return e.clusterDisagreements[clusterID]
}

// ItemHasDisagreement returns true if any cluster containing this item has disagreements
func (e *Engine) ItemHasDisagreement(itemID string) bool {
	clusterID, ok := e.itemToCluster[itemID]
	if !ok {
		return false
	}
	return e.HasDisagreements(clusterID)
}

// findOrCreateDuplicateGroup finds an existing duplicate group for this item or creates one
func (e *Engine) findOrCreateDuplicateGroup(item feeds.Item, hash uint64) *DuplicateGroup {
	// Check against existing hashes for duplicates
	for existingID, existingHash := range e.itemHashes {
		if existingID == item.ID {
			continue
		}
		if AreDuplicates(hash, existingHash) {
			// Found a duplicate! Check if it already has a group
			if groupID, ok := e.itemToGroup[existingID]; ok {
				// Add to existing group
				group := e.duplicateGroups[groupID]
				if group != nil {
					group.ItemIDs = append(group.ItemIDs, item.ID)
					e.itemToGroup[item.ID] = groupID
					return group
				}
			} else {
				// Create new group with both items
				groupID := "dup:" + existingID // Use first item's ID as group key
				group := &DuplicateGroup{
					ID:         groupID,
					ItemIDs:    []string{existingID, item.ID},
					SimHash:    existingHash,
					DetectedAt: time.Now(),
				}
				e.duplicateGroups[groupID] = group
				e.itemToGroup[existingID] = groupID
				e.itemToGroup[item.ID] = groupID
				return group
			}
		}
	}
	return nil
}

// GetDuplicateCount returns how many duplicates an item has (0 if none)
func (e *Engine) GetDuplicateCount(itemID string) int {
	groupID, ok := e.itemToGroup[itemID]
	if !ok {
		return 0
	}
	group := e.duplicateGroups[groupID]
	if group == nil {
		return 0
	}
	// Return total count minus 1 (don't count the item itself)
	return len(group.ItemIDs) - 1
}

// GetDuplicates returns all duplicate item IDs for an item
func (e *Engine) GetDuplicates(itemID string) []string {
	groupID, ok := e.itemToGroup[itemID]
	if !ok {
		return nil
	}
	group := e.duplicateGroups[groupID]
	if group == nil {
		return nil
	}
	// Return all IDs except the queried one
	var dups []string
	for _, id := range group.ItemIDs {
		if id != itemID {
			dups = append(dups, id)
		}
	}
	return dups
}

// IsPrimaryInGroup returns true if this is the first (primary) item in its duplicate group
func (e *Engine) IsPrimaryInGroup(itemID string) bool {
	groupID, ok := e.itemToGroup[itemID]
	if !ok {
		return true // No group, so it's "primary" by default
	}
	group := e.duplicateGroups[groupID]
	if group == nil || len(group.ItemIDs) == 0 {
		return true
	}
	return group.ItemIDs[0] == itemID
}

// GetItemEntities returns entities for an item from cache
func (e *Engine) GetItemEntities(itemID string) []ItemEntity {
	return e.itemEntities[itemID]
}

// findOrCreateCluster finds or creates a cluster for this item
// Clustering criteria: entity overlap >50% OR title similarity >0.7
func (e *Engine) findOrCreateCluster(item feeds.Item, entities []ItemEntity, hash uint64) *Cluster {
	// Build entity set for this item
	entitySet := make(map[string]bool)
	for _, ent := range entities {
		entitySet[ent.EntityID] = true
	}

	// Check existing clusters for a match
	var bestCluster *Cluster
	var bestScore float64

	for _, cluster := range e.clusters {
		// Skip stale clusters
		if cluster.Status == ClusterStale {
			continue
		}

		// Check if any items in this cluster match
		for _, clusterItemID := range e.getClusterItemIDs(cluster.ID) {
			// Check title similarity
			if existingHash, ok := e.itemHashes[clusterItemID]; ok {
				sim := SimilarityScore(hash, existingHash)
				if sim > 0.7 && sim > bestScore {
					bestScore = sim
					bestCluster = cluster
				}
			}

			// Check entity overlap
			existingEntities := e.itemEntities[clusterItemID]
			if len(existingEntities) > 0 && len(entities) > 0 {
				overlap := e.calculateEntityOverlap(entitySet, existingEntities)
				if overlap > 0.5 && overlap > bestScore {
					bestScore = overlap
					bestCluster = cluster
				}
			}
		}
	}

	// If found a matching cluster, add this item
	if bestCluster != nil {
		e.itemToCluster[item.ID] = bestCluster.ID
		bestCluster.ItemCount++
		bestCluster.LastItemAt = item.Published
		// Update velocity (items per hour)
		hours := time.Since(bestCluster.FirstItemAt).Hours()
		if hours > 0 {
			bestCluster.Velocity = float64(bestCluster.ItemCount) / hours
		}
		return bestCluster
	}

	// Create a new cluster if we have enough signal
	if len(entities) >= 2 {
		clusterID := "cluster:" + item.ID
		cluster := &Cluster{
			ID:          clusterID,
			EventSummary: item.Title,
			FirstItemAt: item.Published,
			LastItemAt:  item.Published,
			ItemCount:   1,
			SourceCount: 1,
			Status:      ClusterActive,
			Velocity:    0,
		}
		e.clusters[clusterID] = cluster
		e.itemToCluster[item.ID] = clusterID
		return cluster
	}

	return nil
}

// getClusterItemIDs returns all item IDs in a cluster
func (e *Engine) getClusterItemIDs(clusterID string) []string {
	var items []string
	for itemID, cID := range e.itemToCluster {
		if cID == clusterID {
			items = append(items, itemID)
		}
	}
	return items
}

// calculateEntityOverlap calculates Jaccard similarity between entity sets
func (e *Engine) calculateEntityOverlap(set1 map[string]bool, entities2 []ItemEntity) float64 {
	set2 := make(map[string]bool)
	for _, ent := range entities2 {
		set2[ent.EntityID] = true
	}

	// Calculate intersection and union
	intersection := 0
	for id := range set1 {
		if set2[id] {
			intersection++
		}
	}

	union := len(set1)
	for id := range set2 {
		if !set1[id] {
			union++
		}
	}

	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// GetClusterInfo returns cluster info for an item
func (e *Engine) GetClusterInfo(itemID string) *Cluster {
	clusterID, ok := e.itemToCluster[itemID]
	if !ok {
		return nil
	}
	return e.clusters[clusterID]
}

// IsClusterPrimary returns true if this item is the primary in its cluster
func (e *Engine) IsClusterPrimary(itemID string) bool {
	clusterID, ok := e.itemToCluster[itemID]
	if !ok {
		return false
	}
	cluster := e.clusters[clusterID]
	if cluster == nil {
		return false
	}
	// Primary is the first item that created the cluster
	return strings.HasSuffix(cluster.ID, itemID)
}

// ProcessItems processes multiple items in batch
func (e *Engine) ProcessItems(items []feeds.Item) {
	if len(items) == 0 {
		return
	}

	// Initialize stats start time if not set
	if e.stats.StartTime.IsZero() {
		e.stats.StartTime = time.Now()
	}

	logging.Info("CORRELATION: Processing batch", "item_count", len(items))

	entitiesFound := 0
	clustersCreated := 0
	var sampleEntities []string

	for _, item := range items {
		result, _ := e.ProcessItem(item)
		e.stats.ItemsProcessed++
		if result != nil {
			if len(result.Entities) > 0 {
				entitiesFound++
				// Capture sample entities for debugging
				if len(sampleEntities) < 5 {
					for _, ent := range result.Entities {
						sampleEntities = append(sampleEntities, ent.EntityID)
						if len(sampleEntities) >= 5 {
							break
						}
					}
				}
			}
			if result.Cluster != nil {
				clustersCreated++
			}
		}
	}

	logging.Info("CORRELATION: Batch complete",
		"items_processed", len(items),
		"items_with_entities", entitiesFound,
		"items_with_clusters", clustersCreated,
		"total_clusters", len(e.clusters),
		"sample_entities", sampleEntities)

	// After processing batch, update velocity snapshots
	e.updateVelocitySnapshots()
}

// updateVelocitySnapshots creates velocity snapshots for active clusters
func (e *Engine) updateVelocitySnapshots() {
	now := time.Now()
	oneHourAgo := now.Add(-time.Hour)
	oneDayAgo := now.Add(-24 * time.Hour)

	for clusterID, cluster := range e.clusters {
		if cluster.Status == ClusterStale {
			continue
		}

		// Count items in last hour and last 24 hours
		itemIDs := e.getClusterItemIDs(clusterID)
		mentions1h := 0
		mentions24h := 0

		for _, itemID := range itemIDs {
			hash := e.itemHashes[itemID]
			_ = hash // We'd need item timestamps; for now use cluster times

			// Simple approximation: if cluster last item is within time window
			if cluster.LastItemAt.After(oneHourAgo) {
				mentions1h++
			}
			if cluster.LastItemAt.After(oneDayAgo) {
				mentions24h++
			}
		}

		// Calculate velocity trend
		var velocity float64
		if len(e.clusterHistory[clusterID]) > 0 {
			lastSnapshot := e.clusterHistory[clusterID][len(e.clusterHistory[clusterID])-1]
			velocity = float64(cluster.ItemCount-lastSnapshot.Mentions24h) / time.Since(lastSnapshot.SnapshotAt).Hours()
		}

		snapshot := VelocitySnapshot{
			ClusterID:   clusterID,
			SnapshotAt:  now,
			Mentions1h:  mentions1h,
			Mentions24h: cluster.ItemCount,
			Velocity:    velocity,
		}

		// Keep last 24 snapshots (one per hour for a day)
		history := e.clusterHistory[clusterID]
		history = append(history, snapshot)
		if len(history) > 24 {
			history = history[len(history)-24:]
		}
		e.clusterHistory[clusterID] = history

		// Update cluster velocity
		cluster.Velocity = velocity

		// Check if cluster should be marked stale
		if time.Since(cluster.LastItemAt) > 6*time.Hour {
			cluster.Status = ClusterStale
		}
	}
}

// GetClusterVelocityTrend returns the velocity trend for a cluster
func (e *Engine) GetClusterVelocityTrend(clusterID string) VelocityTrend {
	cluster := e.clusters[clusterID]
	if cluster == nil {
		return TrendSteady
	}

	if cluster.Velocity > 5 {
		return TrendSpiking
	} else if cluster.Velocity > 1 {
		return TrendSteady
	}
	return TrendFading
}

// GetClusterSparklineData returns normalized velocity values for sparkline rendering
func (e *Engine) GetClusterSparklineData(clusterID string, points int) []float64 {
	history := e.clusterHistory[clusterID]
	if len(history) == 0 {
		return nil
	}

	// Normalize to 0-1 range
	var maxVal float64
	for _, s := range history {
		if float64(s.Mentions1h) > maxVal {
			maxVal = float64(s.Mentions1h)
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	// Sample to requested number of points
	result := make([]float64, points)
	step := float64(len(history)) / float64(points)

	for i := 0; i < points; i++ {
		idx := int(float64(i) * step)
		if idx >= len(history) {
			idx = len(history) - 1
		}
		result[i] = float64(history[idx].Mentions1h) / maxVal
	}

	return result
}

// ClusterSummary holds summary info for radar display
type ClusterSummary struct {
	ID          string
	Summary     string
	ItemCount   int
	Velocity    float64
	Trend       VelocityTrend
	HasConflict bool
	FirstItemAt time.Time
}

// GetActiveClusters returns active clusters sorted by velocity (for radar display)
func (e *Engine) GetActiveClusters(limit int) []ClusterSummary {
	var summaries []ClusterSummary

	for id, cluster := range e.clusters {
		if cluster.Status == ClusterStale {
			continue
		}

		summary := ClusterSummary{
			ID:          id,
			Summary:     cluster.EventSummary,
			ItemCount:   cluster.ItemCount,
			Velocity:    cluster.Velocity,
			Trend:       e.GetClusterVelocityTrend(id),
			HasConflict: e.HasDisagreements(id),
			FirstItemAt: cluster.FirstItemAt,
		}
		summaries = append(summaries, summary)
	}

	// Sort by velocity (descending)
	for i := 0; i < len(summaries)-1; i++ {
		for j := i + 1; j < len(summaries); j++ {
			if summaries[j].Velocity > summaries[i].Velocity {
				summaries[i], summaries[j] = summaries[j], summaries[i]
			}
		}
	}

	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}

	return summaries
}

// ItemCorrelations holds all correlation data for an item
type ItemCorrelations struct {
	Item           feeds.Item
	Entities       []ItemEntity
	RelatedItems   []feeds.Item
	Correlations   []Correlation
	Thread         *Thread
	DuplicateGroup *DuplicateGroup
	Cluster        *Cluster
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

func truncateTitle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
