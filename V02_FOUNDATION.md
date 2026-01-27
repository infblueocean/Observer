# v0.2 Foundation: The Killer Architecture

> **"Effortless concurrency, smooth as butter."**

This is the definitive plan. Everything else goes.

---

## The Three Laws

1. **Instant ops run inline** — SimHash, regex, cache lookups (<5ms)
2. **Everything else is a goroutine** — Embeddings, clustering, DB writes
3. **Results flow through channels** — Single pattern, predictable behavior

---

## The Pipeline

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     BUBBLE TEA EVENT LOOP                                │
│                    (NEVER BLOCKED - sacred ground)                       │
└─────────────────────────────────────────────────────────────────────────┘
                                    ▲
                                    │ tea.Msg (results only)
                                    │
┌─────────────────────────────────────────────────────────────────────────┐
│                           EVENT BUS                                      │
│                                                                         │
│   Items ──→ Stage1 ──→ Stage2 ──→ Stage3 ──→ Stage4 ──→ Results        │
│              dedup     entities   clusters   velocity                   │
│             (inline)   (worker)   (worker)   (worker)                   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
                            ┌──────────────┐
                            │   SQLite     │
                            │  (WAL mode)  │
                            │ (async writes)│
                            └──────────────┘
```

---

## Stage Definitions

| Stage | Name | Mode | Latency | What It Does |
|-------|------|------|---------|--------------|
| 1 | Dedup | Inline | <1ms | SimHash, LSH buckets, instant duplicate check |
| 2 | Entities | Worker pool | <5ms | Regex extraction, batched DB writes |
| 3 | Clusters | Dual-mode | <10ms inc | Incremental assign + periodic HDBSCAN batch |
| 4 | Velocity | Worker | async | Multi-window tracking (15m/1h/6h), spike detection |
| 5 | Radar | Aggregator | 30s | Top clusters, entities, geo distribution |
| 6 | Briefing | On-demand | <100ms | Session-aware "Catch Me Up" |

---

## Core Types

```go
// internal/correlation/types.go

// CorrelationEvent flows from pipeline to Bubble Tea
type CorrelationEvent interface {
    correlationEvent() // marker method
}

type DuplicateFound struct {
    ItemID     string
    PrimaryID  string
    GroupSize  int
}

type EntitiesExtracted struct {
    ItemID   string
    Entities []Entity
}

type ClusterUpdated struct {
    ClusterID string
    ItemID    string
    Size      int
    Velocity  float64
    Sparkline []float64
}

type DisagreementFound struct {
    ClusterID string
    ItemIDs   []string
    Reason    string
}

type VelocitySpike struct {
    ClusterID string
    Window    string // "15m", "1h", "6h"
    Rate      float64
}

// Entity extracted from text
type Entity struct {
    ID       string  // normalized: "$AAPL", "usa", "elon-musk"
    Name     string  // display: "$AAPL", "United States", "Elon Musk"
    Type     string  // "ticker", "country", "person", "org"
    Salience float64 // 0.0-1.0
}

// Cluster groups related items
type Cluster struct {
    ID          string
    PrimaryID   string    // representative item
    ItemIDs     []string
    Title       string    // from primary item
    Size        int
    Velocity    float64
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// VelocitySnapshot for sparklines
type VelocitySnapshot struct {
    Timestamp time.Time
    Rate15m   float64
    Rate1h    float64
    Rate6h    float64
    Sources   int
}
```

---

## Event Bus

```go
// internal/correlation/bus.go

type Bus struct {
    // Pipeline channels (buffered, non-blocking sends)
    items    chan *feeds.Item      // Input
    deduped  chan *DedupResult     // Stage 1 → 2
    enriched chan *EntityResult    // Stage 2 → 3
    clustered chan *ClusterResult  // Stage 3 → 4

    // Output to Bubble Tea
    Results chan CorrelationEvent

    // Control
    ctx    context.Context
    cancel context.CancelFunc
}

func NewBus(bufferSize int) *Bus {
    return &Bus{
        items:     make(chan *feeds.Item, bufferSize),
        deduped:   make(chan *DedupResult, bufferSize),
        enriched:  make(chan *EntityResult, bufferSize),
        clustered: make(chan *ClusterResult, bufferSize),
        Results:   make(chan CorrelationEvent, bufferSize),
    }
}

func (b *Bus) Start(ctx context.Context) {
    b.ctx, b.cancel = context.WithCancel(ctx)
}

func (b *Bus) Stop() {
    b.cancel()
}

// Send is non-blocking - drops if full
func (b *Bus) Send(event CorrelationEvent) {
    select {
    case b.Results <- event:
    default:
        // Drop - UI will catch up
    }
}
```

---

## Worker Pool (Generic)

```go
// internal/correlation/worker.go

type Worker[In, Out any] struct {
    name    string
    input   chan In
    output  chan Out
    process func(In) Out
    workers int
}

func NewWorker[In, Out any](name string, workers, buffer int, fn func(In) Out) *Worker[In, Out] {
    return &Worker[In, Out]{
        name:    name,
        input:   make(chan In, buffer),
        output:  make(chan Out, buffer),
        process: fn,
        workers: workers,
    }
}

func (w *Worker[In, Out]) Start(ctx context.Context) {
    for i := 0; i < w.workers; i++ {
        go w.run(ctx, i)
    }
}

func (w *Worker[In, Out]) run(ctx context.Context, id int) {
    for {
        select {
        case <-ctx.Done():
            return
        case item, ok := <-w.input:
            if !ok {
                return
            }
            result := w.process(item)
            select {
            case w.output <- result:
            case <-ctx.Done():
                return
            }
        }
    }
}

func (w *Worker[In, Out]) In() chan<- In   { return w.input }
func (w *Worker[In, Out]) Out() <-chan Out { return w.output }
```

---

## Stage 1: Dedup (Inline)

```go
// internal/correlation/dedup.go

type DedupIndex struct {
    mu      sync.RWMutex
    hashes  map[string]uint64   // itemID → simhash
    buckets map[uint16][]string // LSH bucket → itemIDs
    groups  map[string][]string // groupID → itemIDs
    itemGroup map[string]string // itemID → groupID
}

func NewDedupIndex() *DedupIndex {
    return &DedupIndex{
        hashes:    make(map[string]uint64),
        buckets:   make(map[uint16][]string),
        groups:    make(map[string][]string),
        itemGroup: make(map[string]string),
    }
}

// Check returns (isDuplicate, primaryID, groupSize)
// This runs INLINE - must be <1ms
func (d *DedupIndex) Check(item *feeds.Item) (bool, string, int) {
    hash := SimHash(item.Title)

    d.mu.RLock()
    // Check LSH buckets for candidates
    bucket := uint16(hash >> 48)
    candidates := d.buckets[bucket]
    d.mu.RUnlock()

    for _, candidateID := range candidates {
        d.mu.RLock()
        candidateHash := d.hashes[candidateID]
        d.mu.RUnlock()

        if HammingDistance(hash, candidateHash) <= 3 { // ~90% similar
            // Found duplicate
            d.mu.Lock()
            groupID := d.itemGroup[candidateID]
            if groupID == "" {
                groupID = candidateID
                d.groups[groupID] = []string{candidateID}
                d.itemGroup[candidateID] = groupID
            }
            d.groups[groupID] = append(d.groups[groupID], item.ID)
            d.itemGroup[item.ID] = groupID
            d.hashes[item.ID] = hash
            size := len(d.groups[groupID])
            d.mu.Unlock()

            return true, groupID, size
        }
    }

    // Not a duplicate - add to index
    d.mu.Lock()
    d.hashes[item.ID] = hash
    d.buckets[bucket] = append(d.buckets[bucket], item.ID)
    d.mu.Unlock()

    return false, "", 0
}

func (d *DedupIndex) GetGroupSize(itemID string) int {
    d.mu.RLock()
    defer d.mu.RUnlock()
    if groupID, ok := d.itemGroup[itemID]; ok {
        return len(d.groups[groupID])
    }
    return 0
}

func (d *DedupIndex) IsPrimary(itemID string) bool {
    d.mu.RLock()
    defer d.mu.RUnlock()
    groupID := d.itemGroup[itemID]
    return groupID == "" || groupID == itemID
}
```

---

## Stage 2: Entities (Worker Pool)

```go
// internal/correlation/entities.go

type EntityResult struct {
    ItemID   string
    Item     *feeds.Item
    Entities []Entity
}

func NewEntityWorker(workers, buffer int) *Worker[*feeds.Item, *EntityResult] {
    return NewWorker("entities", workers, buffer, extractEntities)
}

func extractEntities(item *feeds.Item) *EntityResult {
    var entities []Entity

    // Tickers ($AAPL)
    for _, t := range ExtractTickers(item.Title) {
        entities = append(entities, Entity{
            ID:   t,
            Name: t,
            Type: "ticker",
            Salience: 0.9,
        })
    }

    // Countries
    for _, c := range ExtractCountries(item.Title) {
        entities = append(entities, Entity{
            ID:   strings.ToLower(c),
            Name: c,
            Type: "country",
            Salience: 0.7,
        })
    }

    // Source attributions
    if attr := ExtractSourceAttribution(item.Title); attr.OriginalSource != "" {
        entities = append(entities, Entity{
            ID:   "source:" + strings.ToLower(attr.OriginalSource),
            Name: attr.OriginalSource,
            Type: "source",
            Salience: 0.5,
        })
    }

    return &EntityResult{
        ItemID:   item.ID,
        Item:     item,
        Entities: entities,
    }
}
```

---

## Stage 3: Clusters (Dual-Mode)

```go
// internal/correlation/clusters.go

type ClusterEngine struct {
    mu       sync.RWMutex
    clusters map[string]*Cluster
    itemCluster map[string]string

    // For incremental clustering
    entityIndex map[string][]string // entityID → clusterIDs
}

func NewClusterEngine() *ClusterEngine {
    return &ClusterEngine{
        clusters:    make(map[string]*Cluster),
        itemCluster: make(map[string]string),
        entityIndex: make(map[string][]string),
    }
}

type ClusterResult struct {
    ItemID    string
    Cluster   *Cluster
    IsNew     bool
    Merged    []string // IDs of clusters that were merged
}

// AssignToCluster runs per-item (<10ms)
// Uses entity overlap for instant clustering
func (c *ClusterEngine) AssignToCluster(er *EntityResult) *ClusterResult {
    c.mu.Lock()
    defer c.mu.Unlock()

    // Find candidate clusters by entity overlap
    candidates := make(map[string]int) // clusterID → overlap count
    for _, e := range er.Entities {
        for _, clusterID := range c.entityIndex[e.ID] {
            candidates[clusterID]++
        }
    }

    // Find best match (>50% entity overlap)
    var bestCluster string
    var bestOverlap int
    threshold := len(er.Entities) / 2
    if threshold < 1 {
        threshold = 1
    }

    for clusterID, overlap := range candidates {
        if overlap >= threshold && overlap > bestOverlap {
            // Check temporal decay - skip if cluster is stale
            cluster := c.clusters[clusterID]
            if time.Since(cluster.UpdatedAt) > 48*time.Hour {
                continue
            }
            bestCluster = clusterID
            bestOverlap = overlap
        }
    }

    if bestCluster != "" {
        // Add to existing cluster
        cluster := c.clusters[bestCluster]
        cluster.ItemIDs = append(cluster.ItemIDs, er.ItemID)
        cluster.Size = len(cluster.ItemIDs)
        cluster.UpdatedAt = time.Now()
        c.itemCluster[er.ItemID] = bestCluster

        // Update entity index
        for _, e := range er.Entities {
            c.entityIndex[e.ID] = appendUnique(c.entityIndex[e.ID], bestCluster)
        }

        return &ClusterResult{
            ItemID:  er.ItemID,
            Cluster: cluster,
            IsNew:   false,
        }
    }

    // Create new cluster
    cluster := &Cluster{
        ID:        er.ItemID, // Use first item ID as cluster ID
        PrimaryID: er.ItemID,
        ItemIDs:   []string{er.ItemID},
        Title:     er.Item.Title,
        Size:      1,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }
    c.clusters[cluster.ID] = cluster
    c.itemCluster[er.ItemID] = cluster.ID

    // Index entities
    for _, e := range er.Entities {
        c.entityIndex[e.ID] = append(c.entityIndex[e.ID], cluster.ID)
    }

    return &ClusterResult{
        ItemID:  er.ItemID,
        Cluster: cluster,
        IsNew:   true,
    }
}

func (c *ClusterEngine) GetCluster(itemID string) *Cluster {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if clusterID, ok := c.itemCluster[itemID]; ok {
        return c.clusters[clusterID]
    }
    return nil
}
```

---

## Stage 4: Velocity (Async)

```go
// internal/correlation/velocity.go

type VelocityTracker struct {
    mu        sync.RWMutex
    snapshots map[string]*RingBuffer // clusterID → velocity history

    // Spike detection
    spikeThreshold float64
}

func NewVelocityTracker() *VelocityTracker {
    return &VelocityTracker{
        snapshots:      make(map[string]*RingBuffer),
        spikeThreshold: 2.0, // 2x baseline = spike
    }
}

func (v *VelocityTracker) Record(clusterID string, itemCount, sourceCount int) *VelocitySpike {
    v.mu.Lock()
    defer v.mu.Unlock()

    if _, ok := v.snapshots[clusterID]; !ok {
        v.snapshots[clusterID] = NewRingBuffer(288) // 24h at 5min intervals
    }

    buf := v.snapshots[clusterID]
    now := time.Now()

    // Calculate rates
    rate15m := v.calculateRate(buf, 15*time.Minute, itemCount)
    rate1h := v.calculateRate(buf, time.Hour, itemCount)
    rate6h := v.calculateRate(buf, 6*time.Hour, itemCount)

    snapshot := VelocitySnapshot{
        Timestamp: now,
        Rate15m:   rate15m,
        Rate1h:    rate1h,
        Rate6h:    rate6h,
        Sources:   sourceCount,
    }
    buf.Add(snapshot)

    // Check for spike (require 2-of-3 windows elevated)
    elevated := 0
    if rate15m > v.spikeThreshold*v.getBaseline(buf, 15*time.Minute) {
        elevated++
    }
    if rate1h > v.spikeThreshold*v.getBaseline(buf, time.Hour) {
        elevated++
    }
    if rate6h > v.spikeThreshold*v.getBaseline(buf, 6*time.Hour) {
        elevated++
    }

    if elevated >= 2 {
        window := "1h"
        if rate15m > rate1h {
            window = "15m"
        }
        return &VelocitySpike{
            ClusterID: clusterID,
            Window:    window,
            Rate:      rate1h,
        }
    }

    return nil
}

func (v *VelocityTracker) GetSparkline(clusterID string, points int) []float64 {
    v.mu.RLock()
    defer v.mu.RUnlock()

    buf, ok := v.snapshots[clusterID]
    if !ok {
        return nil
    }

    recent := buf.Last(points)
    data := make([]float64, len(recent))
    for i, s := range recent {
        data[i] = s.(VelocitySnapshot).Rate1h
    }
    return data
}
```

---

## The Engine (Orchestrator)

```go
// internal/correlation/engine.go

type Engine struct {
    // Pipeline components
    bus      *Bus
    dedup    *DedupIndex
    entities *Worker[*feeds.Item, *EntityResult]
    clusters *ClusterEngine
    velocity *VelocityTracker

    // Caches for UI (sync.Map for lock-free reads)
    entityCache sync.Map // itemID → []Entity

    // Storage
    db *sql.DB
}

func NewEngine(db *sql.DB) *Engine {
    return &Engine{
        bus:      NewBus(1000),
        dedup:    NewDedupIndex(),
        entities: NewEntityWorker(4, 1000),
        clusters: NewClusterEngine(),
        velocity: NewVelocityTracker(),
        db:       db,
    }
}

func (e *Engine) Start(ctx context.Context) {
    e.bus.Start(ctx)
    e.entities.Start(ctx)

    // Pipeline coordinator
    go e.runPipeline(ctx)

    // Periodic tasks
    go e.runPeriodicTasks(ctx)
}

func (e *Engine) runPipeline(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return

        // Stage 1 output → Stage 2 input
        case item := <-e.bus.items:
            // Dedup is INLINE (Law #1)
            isDupe, primaryID, size := e.dedup.Check(item)
            if isDupe {
                e.bus.Send(DuplicateFound{
                    ItemID:    item.ID,
                    PrimaryID: primaryID,
                    GroupSize: size,
                })
                continue // Don't process duplicates further
            }
            // Send to entity extraction
            e.entities.In() <- item

        // Stage 2 output → Stage 3 input + UI
        case er := <-e.entities.Out():
            // Cache for UI
            e.entityCache.Store(er.ItemID, er.Entities)

            // Emit event
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
                Velocity:  cr.Cluster.Velocity,
                Sparkline: e.velocity.GetSparkline(cr.Cluster.ID, 8),
            })

            // Emit spike if detected
            if spike != nil {
                e.bus.Send(*spike)
            }
        }
    }
}

func (e *Engine) runPeriodicTasks(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Batch operations
            e.persistToDB()
            e.pruneStaleData()
        }
    }
}

// ProcessItem is the entry point (non-blocking)
func (e *Engine) ProcessItem(item *feeds.Item) {
    select {
    case e.bus.items <- item:
    default:
        // Backpressure - drop
    }
}

// Results returns the event channel for Bubble Tea
func (e *Engine) Results() <-chan CorrelationEvent {
    return e.bus.Results
}

// UI query methods (all use caches - instant)

func (e *Engine) GetDuplicateCount(itemID string) int {
    return e.dedup.GetGroupSize(itemID)
}

func (e *Engine) IsPrimary(itemID string) bool {
    return e.dedup.IsPrimary(itemID)
}

func (e *Engine) GetEntities(itemID string) []Entity {
    if v, ok := e.entityCache.Load(itemID); ok {
        return v.([]Entity)
    }
    return nil
}

func (e *Engine) GetCluster(itemID string) *Cluster {
    return e.clusters.GetCluster(itemID)
}

func (e *Engine) GetSparkline(clusterID string) []float64 {
    return e.velocity.GetSparkline(clusterID, 8)
}
```

---

## Bubble Tea Integration

```go
// internal/app/app.go

func (m *Model) Init() tea.Cmd {
    // Start correlation engine
    if m.correlation != nil {
        go m.correlation.Start(m.ctx)
    }

    return tea.Batch(
        m.loadFeeds(),
        m.subscribeCorrelation(),
    )
}

func (m *Model) subscribeCorrelation() tea.Cmd {
    if m.correlation == nil {
        return nil
    }
    return func() tea.Msg {
        event := <-m.correlation.Results()
        return CorrelationEventMsg{Event: event}
    }
}

// In Update()
case CorrelationEventMsg:
    // Just re-render - all data comes from cache queries
    return m, m.subscribeCorrelation()
```

---

## What to KEEP

| File | Keep? | Reason |
|------|-------|--------|
| `cheap.go` | ✅ YES | Regex extractors work great |
| `cheap_test.go` | ✅ YES | Tests are valuable |
| `types.go` | ✅ PARTIAL | Keep Entity, Cluster, VelocitySnapshot |

## What to DELETE

| File | Delete? | Reason |
|------|---------|--------|
| `engine.go` | ❌ DELETE | Synchronous, no channels, wrong pattern |

## What to CREATE

| File | Purpose |
|------|---------|
| `bus.go` | Event bus with channels |
| `worker.go` | Generic worker pool |
| `dedup.go` | SimHash + LSH index |
| `entities.go` | Entity extraction worker |
| `clusters.go` | Cluster engine |
| `velocity.go` | Multi-window velocity tracking |
| `engine.go` | Pipeline orchestrator (new) |

---

## Performance Budget

| Operation | Budget | How |
|-----------|--------|-----|
| Dedup check | <1ms | LSH buckets, in-memory |
| Entity extraction | <5ms | Regex only, no LLM |
| Cluster assignment | <10ms | Entity index lookup |
| UI cache lookup | <0.1ms | sync.Map |
| Full render | <16ms | Cached queries only |

---

## Success Criteria

- [ ] Bubble Tea never blocks
- [ ] Smooth scrolling at 60fps
- [ ] Indicators appear instantly on items
- [ ] Backpressure handled gracefully (drop, don't block)
- [ ] All state queryable via sync.Map caches
