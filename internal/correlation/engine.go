package correlation

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/logging"
)

// Engine is the correlation pipeline orchestrator.
// It coordinates all stages and provides non-blocking queries for the UI.
type Engine struct {
	// Pipeline components
	bus      *Bus
	dedup    *DedupIndex
	entities *Worker[*feeds.Item, *EntityResult]
	clusters *ClusterEngine
	velocity *VelocityTracker

	// Caches for UI (sync.Map for lock-free reads)
	entityCache sync.Map // itemID → []Entity

	// Storage (async writes)
	db *sql.DB

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewEngine creates a correlation engine.
func NewEngine(db *sql.DB) *Engine {
	return &Engine{
		bus:      NewBus(1000),
		dedup:    NewDedupIndex(),
		entities: NewEntityWorker(4, 1000), // 4 workers
		clusters: NewClusterEngine(),
		velocity: NewVelocityTracker(),
		db:       db,
	}
}

// NewEngineSimple creates a correlation engine (alias for NewEngine for backward compatibility).
func NewEngineSimple(db *sql.DB) (*Engine, error) {
	return NewEngine(db), nil
}

// Start launches the pipeline goroutines.
func (e *Engine) Start(ctx context.Context) {
	e.ctx, e.cancel = context.WithCancel(ctx)
	e.bus.Start(e.ctx)
	e.entities.Start(e.ctx)

	// Pipeline coordinator
	e.wg.Add(1)
	go e.runPipeline()

	// Periodic tasks (DB persistence, pruning)
	e.wg.Add(1)
	go e.runPeriodicTasks()

	logging.Info("Correlation engine started")
}

// Stop gracefully shuts down the engine.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.bus.Stop()
	e.wg.Wait()
	logging.Info("Correlation engine stopped")
}

// runPipeline is the main pipeline coordinator goroutine.
func (e *Engine) runPipeline() {
	defer e.wg.Done()

	for {
		select {
		case <-e.ctx.Done():
			return

		// Stage 1: Dedup (INLINE - Law #1)
		case item := <-e.bus.items:
			isDupe, primaryID, size := e.dedup.Check(item)
			if isDupe {
				e.bus.Send(DuplicateFound{
					ItemID:    item.ID,
					PrimaryID: primaryID,
					GroupSize: size,
				})
				continue // Don't process duplicates further
			}
			// Send to Stage 2: Entity extraction
			select {
			case e.entities.In() <- item:
			default:
				// Backpressure - drop
				logging.Debug("Entity worker backpressure, dropping item", "id", item.ID)
			}

		// Stage 2 output → Stage 3+4
		case er := <-e.entities.Out():
			// Cache for instant UI queries
			e.entityCache.Store(er.ItemID, er.Entities)

			// Emit entities event
			e.bus.Send(EntitiesExtracted{
				ItemID:   er.ItemID,
				Entities: er.Entities,
			})

			// Stage 3: Cluster assignment
			cr := e.clusters.AssignToCluster(er)

			// Stage 4: Velocity tracking
			spike := e.velocity.Record(cr.Cluster.ID, cr.Cluster.Size, 1)

			// Emit cluster event
			e.bus.Send(ClusterUpdated{
				ClusterID: cr.Cluster.ID,
				ItemID:    cr.ItemID,
				Size:      cr.Cluster.Size,
				Velocity:  e.velocity.GetVelocity(cr.Cluster.ID),
				Sparkline: e.velocity.GetSparkline(cr.Cluster.ID, 8),
				IsNew:     cr.IsNew,
			})

			// Emit spike if detected
			if spike != nil {
				logging.Info("Velocity spike detected",
					"cluster", spike.ClusterID,
					"window", spike.Window,
					"rate", spike.Rate)
			}
		}
	}
}

// runPeriodicTasks handles DB persistence and pruning.
func (e *Engine) runPeriodicTasks() {
	defer e.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.persistToDB()
			e.pruneStaleData()
		}
	}
}

// persistToDB writes cached data to SQLite.
func (e *Engine) persistToDB() {
	if e.db == nil {
		return
	}

	// Batch persist entities
	e.entityCache.Range(func(key, value interface{}) bool {
		itemID := key.(string)
		entities := value.([]Entity)
		for _, ent := range entities {
			_, _ = e.db.Exec(`
				INSERT OR IGNORE INTO item_entities (item_id, entity_id, salience)
				VALUES (?, ?, ?)
			`, itemID, ent.ID, ent.Salience)
		}
		return true
	})

	logging.Debug("Correlation data persisted to DB")
}

// pruneStaleData removes old data from memory.
func (e *Engine) pruneStaleData() {
	// TODO: Implement pruning of old clusters, velocity data, etc.
}

// ProcessItem is the entry point (non-blocking).
// Items are sent to the pipeline and processed asynchronously.
func (e *Engine) ProcessItem(item *feeds.Item) {
	select {
	case e.bus.items <- item:
	default:
		// Backpressure - drop
		logging.Debug("Pipeline backpressure, dropping item", "id", item.ID)
	}
}

// ProcessItems processes multiple items (non-blocking).
func (e *Engine) ProcessItems(items []feeds.Item) {
	for i := range items {
		e.ProcessItem(&items[i])
	}
	logging.Info("Correlation: queued items for processing", "count", len(items))
}

// Results returns the event channel for Bubble Tea subscription.
func (e *Engine) Results() <-chan CorrelationEvent {
	return e.bus.Results
}

// ===== UI Query Methods (all use caches - instant) =====

// GetDuplicateCount returns the number of duplicates for an item.
func (e *Engine) GetDuplicateCount(itemID string) int {
	return e.dedup.GetGroupSize(itemID)
}

// IsPrimaryInGroup returns true if this is the primary item in its duplicate group.
func (e *Engine) IsPrimaryInGroup(itemID string) bool {
	return e.dedup.IsPrimary(itemID)
}

// GetDuplicates returns all duplicate item IDs for an item.
func (e *Engine) GetDuplicates(itemID string) []string {
	return e.dedup.GetDuplicates(itemID)
}

// GetItemEntities returns entities for an item (from cache).
func (e *Engine) GetItemEntities(itemID string) []Entity {
	if v, ok := e.entityCache.Load(itemID); ok {
		return v.([]Entity)
	}
	return nil
}

// GetClusterInfo returns cluster info for an item.
func (e *Engine) GetClusterInfo(itemID string) *Cluster {
	return e.clusters.GetCluster(itemID)
}

// IsClusterPrimary returns true if this item is the primary in its cluster.
func (e *Engine) IsClusterPrimary(itemID string) bool {
	return e.clusters.IsClusterPrimary(itemID)
}

// GetSparkline returns velocity sparkline data for a cluster.
func (e *Engine) GetSparkline(clusterID string) []float64 {
	return e.velocity.GetSparkline(clusterID, 8)
}

// Stats returns engine statistics.
func (e *Engine) Stats() (items, groups, clusters int) {
	items, groups = e.dedup.Stats()
	clusters, _ = e.clusters.Stats()
	return
}

// GetStats returns detailed statistics for UI display.
func (e *Engine) GetStats() Stats {
	items, groups := e.dedup.Stats()
	clusters, _ := e.clusters.Stats()
	return Stats{
		ItemsProcessed:  items,
		DuplicatesFound: groups,
		ClustersFormed:  clusters,
	}
}

// GetActiveClusters returns active clusters sorted by velocity (for radar display).
func (e *Engine) GetActiveClusters(limit int) []ClusterSummary {
	allClusters := e.clusters.GetAllClusters()
	var summaries []ClusterSummary

	for _, cluster := range allClusters {
		// Skip tiny clusters
		if cluster.Size < 2 {
			continue
		}

		summary := ClusterSummary{
			ID:          cluster.ID,
			Summary:     cluster.Title,
			ItemCount:   cluster.Size,
			Velocity:    e.velocity.GetVelocity(cluster.ID),
			Trend:       e.GetClusterVelocityTrend(cluster.ID),
			HasConflict: false, // TODO: implement disagreement tracking
			FirstItemAt: cluster.CreatedAt,
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

// GetTopEntities returns most-mentioned entities in a time range.
func (e *Engine) GetTopEntities(since time.Time, limit int) ([]Entity, error) {
	// Count entity mentions from cache
	counts := make(map[string]int)
	names := make(map[string]Entity)

	e.entityCache.Range(func(key, value interface{}) bool {
		entities := value.([]Entity)
		for _, ent := range entities {
			counts[ent.ID]++
			names[ent.ID] = ent
		}
		return true
	})

	// Convert to slice and sort
	type entityCount struct {
		entity Entity
		count  int
	}
	var sorted []entityCount
	for id, count := range counts {
		ent := names[id]
		sorted = append(sorted, entityCount{ent, count})
	}

	// Sort by count descending
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Return top N
	var result []Entity
	for i := 0; i < len(sorted) && i < limit; i++ {
		result = append(result, sorted[i].entity)
	}

	return result, nil
}

// GetClusterVelocityTrend returns the velocity trend for a cluster.
func (e *Engine) GetClusterVelocityTrend(clusterID string) VelocityTrend {
	velocity := e.velocity.GetVelocity(clusterID)
	if velocity > 5 {
		return TrendSpiking
	} else if velocity > 1 {
		return TrendSteady
	}
	return TrendFading
}

// GetClusterSparklineData returns normalized velocity values for sparkline rendering.
func (e *Engine) GetClusterSparklineData(clusterID string, points int) []float64 {
	return e.velocity.GetSparkline(clusterID, points)
}

// ItemHasDisagreement returns true if any cluster containing this item has disagreements.
func (e *Engine) ItemHasDisagreement(itemID string) bool {
	// TODO: Implement disagreement tracking
	return false
}

// Activity tracking for transparency
var (
	recentActivity []Activity
	activityIndex  int
	activityMu     sync.Mutex
)

const maxActivityEntries = 50

// addActivity adds an activity to the ring buffer.
func addActivity(actType ActivityType, itemTitle, details string) {
	activityMu.Lock()
	defer activityMu.Unlock()

	if recentActivity == nil {
		recentActivity = make([]Activity, maxActivityEntries)
	}
	recentActivity[activityIndex] = Activity{
		Type:      actType,
		Time:      time.Now(),
		ItemTitle: itemTitle,
		Details:   details,
	}
	activityIndex = (activityIndex + 1) % maxActivityEntries
}

// GetRecentActivity returns recent activities (newest first).
func (e *Engine) GetRecentActivity(count int) []Activity {
	activityMu.Lock()
	defer activityMu.Unlock()

	if recentActivity == nil {
		return nil
	}
	if count > maxActivityEntries {
		count = maxActivityEntries
	}

	result := make([]Activity, 0, count)
	idx := (activityIndex - 1 + maxActivityEntries) % maxActivityEntries
	for i := 0; i < count; i++ {
		act := recentActivity[idx]
		if act.Time.IsZero() {
			break
		}
		result = append(result, act)
		idx = (idx - 1 + maxActivityEntries) % maxActivityEntries
	}
	return result
}
