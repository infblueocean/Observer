# Correlation Engine Implementation Plan

## Unified Architecture: The Pipeline

> **Design Principle:** Never block the UI. Every operation is either instant (<5ms) or async with progressive display.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           BUBBLE TEA EVENT LOOP                              │
│                        (NEVER BLOCKED - sacred ground)                       │
└─────────────────────────────────────────────────────────────────────────────┘
                                      ▲
                                      │ tea.Msg (results only)
                                      │
┌─────────────────────────────────────────────────────────────────────────────┐
│                              EVENT BUS (single)                              │
│   Channels: itemsCh, entitiesCh, clustersCh, velocityCh, resultsCh          │
└─────────────────────────────────────────────────────────────────────────────┘
          │              │              │              │              │
          ▼              ▼              ▼              ▼              ▼
    ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐
    │  STAGE 1 │  │  STAGE 2 │  │  STAGE 3 │  │  STAGE 4 │  │  STAGE 5 │
    │  Dedup   │  │ Entities │  │ Clusters │  │  Claims  │  │  Radar   │
    │ (inline) │  │ (worker) │  │ (worker) │  │ (worker) │  │  (agg)   │
    │   <1ms   │  │  <5ms    │  │  async   │  │  async   │  │  async   │
    └──────────┘  └──────────┘  └──────────┘  └──────────┘  └──────────┘
          │              │              │              │              │
          └──────────────┴──────────────┴──────────────┴──────────────┘
                                      │
                                      ▼
                              ┌──────────────┐
                              │   SQLITE     │
                              │ (WAL mode)   │
                              │ (write queue)│
                              └──────────────┘
```

---

## Core Concurrency Model

### The Three Laws

1. **Instant ops run inline** - SimHash, regex extraction, lookups from hot cache
2. **Everything else is a goroutine** - Embeddings, clustering, LLM calls
3. **Results flow through channels** - Single pattern, predictable behavior

### The Event Bus

```go
// internal/correlation/bus.go

type EventBus struct {
    // Input channels (buffered, never block sender)
    Items      chan *feeds.Item       // New items arrive here

    // Stage outputs (internal, stage-to-stage)
    dedupOut   chan *DedupResult      // Stage 1 → Stage 2
    entityOut  chan *EntityResult     // Stage 2 → Stage 3
    clusterOut chan *ClusterResult    // Stage 3 → Stage 4

    // UI notifications (to Bubble Tea)
    Results    chan CorrelationEvent  // All UI updates flow here

    // Control
    ctx        context.Context
    cancel     context.CancelFunc
    wg         sync.WaitGroup
}

// CorrelationEvent is what Bubble Tea receives
type CorrelationEvent interface {
    isCorrelationEvent()
}

type DuplicateFoundEvent struct {
    Primary   string   // Item ID
    Duplicate string   // Item ID
    Similarity float64
}

type EntitiesExtractedEvent struct {
    ItemID   string
    Entities []Entity
    Duration time.Duration
}

type ClusterUpdatedEvent struct {
    ClusterID string
    Items     []string
    Velocity  VelocitySnapshot
    IsNew     bool
}

type DisagreementDetectedEvent struct {
    ClusterID string
    Claims    []Claim
    Conflict  string
}

type RadarUpdateEvent struct {
    TopClusters []ClusterSummary
    TopEntities []EntityCount
    Velocity    map[string]float64
}
```

### Worker Pool Pattern

```go
// internal/correlation/workers.go

type WorkerPool[T any, R any] struct {
    name     string
    workers  int
    input    chan T
    output   chan R
    process  func(T) R
    ctx      context.Context
}

func NewWorkerPool[T any, R any](name string, workers int, bufSize int, fn func(T) R) *WorkerPool[T, R] {
    return &WorkerPool[T, R]{
        name:    name,
        workers: workers,
        input:   make(chan T, bufSize),
        output:  make(chan R, bufSize),
        process: fn,
    }
}

func (p *WorkerPool[T, R]) Start(ctx context.Context) {
    p.ctx = ctx
    for i := 0; i < p.workers; i++ {
        go p.worker(i)
    }
}

func (p *WorkerPool[T, R]) worker(id int) {
    for {
        select {
        case <-p.ctx.Done():
            return
        case item, ok := <-p.input:
            if !ok {
                return
            }
            result := p.process(item)
            select {
            case p.output <- result:
            case <-p.ctx.Done():
                return
            }
        }
    }
}
```

---

## Stage 1: Deduplication (Inline, <1ms)

### Design

Dedup is **synchronous** because SimHash is instant. No goroutine needed.

```go
// Called inline during item ingestion
func (e *Engine) ProcessItem(item *feeds.Item) ProcessResult {
    start := time.Now()

    // Stage 1: Instant dedup check
    hash := SimHash(item.Title)
    if existing := e.dedupIndex.FindSimilar(hash, 0.9); existing != "" {
        e.bus.Results <- DuplicateFoundEvent{
            Primary:    existing,
            Duplicate:  item.ID,
            Similarity: e.dedupIndex.Similarity(hash, existing),
        }
        return ProcessResult{IsDuplicate: true, Duration: time.Since(start)}
    }

    // Add to index
    e.dedupIndex.Add(item.ID, hash)

    // Queue for Stage 2 (non-blocking)
    select {
    case e.bus.dedupOut <- &DedupResult{Item: item, Hash: hash}:
    default:
        // Buffer full - log but don't block
        e.metrics.droppedItems.Inc()
    }

    return ProcessResult{IsDuplicate: false, Duration: time.Since(start)}
}
```

### Data Structures

```go
// internal/correlation/dedup.go

// SimHash index with LSH for fast lookup
type DedupIndex struct {
    mu       sync.RWMutex
    hashes   map[string]uint64        // itemID → hash
    buckets  map[uint64][]string      // LSH bucket → itemIDs
    maxAge   time.Duration            // Evict entries older than this
}

func (d *DedupIndex) FindSimilar(hash uint64, threshold float64) string {
    d.mu.RLock()
    defer d.mu.RUnlock()

    // Check LSH buckets (O(1) lookup)
    bucket := hash >> 32  // Use top 32 bits as bucket key
    candidates := d.buckets[bucket]

    for _, id := range candidates {
        if similarity(hash, d.hashes[id]) >= threshold {
            return id
        }
    }
    return ""
}
```

### UI Integration

```go
// In stream/model.go

func (m *Model) renderItem(item *feeds.Item) string {
    var badges []string

    // Check for duplicates (instant lookup)
    if count := m.correlation.DuplicateCount(item.ID); count > 0 {
        badges = append(badges, m.styles.muted.Render(fmt.Sprintf("×%d", count+1)))
    }

    // ... rest of rendering
}
```

---

## Stage 2: Entity Extraction (Worker Pool, <5ms/item)

### Design

Regex extraction is fast but we still use a worker pool for:
1. Consistent async pattern
2. Easy upgrade path to LLM extraction later
3. Batching for SQLite writes

```go
// internal/correlation/entities.go

type EntityWorker struct {
    pool     *WorkerPool[*DedupResult, *EntityResult]
    store    *store.Store
    batch    *EntityBatch
    batchMu  sync.Mutex
}

func NewEntityWorker(store *store.Store) *EntityWorker {
    w := &EntityWorker{
        store: store,
        batch: NewEntityBatch(100), // Flush every 100 items
    }

    w.pool = NewWorkerPool[*DedupResult, *EntityResult](
        "entities",
        4,      // 4 workers
        1000,   // Buffer 1000 items
        w.extract,
    )

    return w
}

func (w *EntityWorker) extract(d *DedupResult) *EntityResult {
    start := time.Now()

    entities := make([]Entity, 0, 10)

    // Instant regex extraction (all run in parallel conceptually)
    entities = append(entities, ExtractTickers(d.Item.Title)...)
    entities = append(entities, ExtractCountries(d.Item.Title)...)
    entities = append(entities, ExtractSourceAttribution(d.Item.Title)...)

    // Dedupe and score
    entities = dedupeEntities(entities)
    for i := range entities {
        entities[i].Salience = computeSalience(entities[i], d.Item)
    }

    // Batch for DB write (non-blocking)
    w.queueBatch(d.Item.ID, entities)

    return &EntityResult{
        ItemID:   d.Item.ID,
        Item:     d.Item,
        Entities: entities,
        Duration: time.Since(start),
    }
}

func (w *EntityWorker) queueBatch(itemID string, entities []Entity) {
    w.batchMu.Lock()
    defer w.batchMu.Unlock()

    w.batch.Add(itemID, entities)

    if w.batch.Full() {
        batch := w.batch
        w.batch = NewEntityBatch(100)

        // Async write
        go func() {
            if err := w.store.BatchInsertEntities(batch); err != nil {
                log.Printf("entity batch write failed: %v", err)
            }
        }()
    }
}
```

### Hot Cache for Display

```go
// internal/correlation/cache.go

// EntityCache is a fast lookup for UI rendering
// Uses sync.Map for lock-free reads (common case)
type EntityCache struct {
    items    sync.Map  // itemID → []Entity
    entities sync.Map  // entityID → []string (itemIDs)
    maxItems int
    evictCh  chan string
}

func (c *EntityCache) GetForItem(itemID string) []Entity {
    if v, ok := c.items.Load(itemID); ok {
        return v.([]Entity)
    }
    return nil
}

func (c *EntityCache) GetItemsForEntity(entityID string) []string {
    if v, ok := c.entities.Load(entityID); ok {
        return v.([]string)
    }
    return nil
}
```

### UI Integration

```go
// Entity pills on focused item
func (m *Model) renderEntityPills(item *feeds.Item) string {
    entities := m.correlation.GetEntities(item.ID)
    if len(entities) == 0 {
        return ""
    }

    var pills []string
    for _, e := range entities[:min(5, len(entities))] {
        style := m.entityStyle(e.Type)
        pills = append(pills, style.Render(e.Display()))
    }

    return lipgloss.JoinHorizontal(lipgloss.Top, pills...)
}
```

---

## Stage 3: Clustering (Dual-Mode: Incremental + Batch)

### Design

This is where the Brain Trust insight matters most:
- **Incremental**: Assign items to existing clusters instantly
- **Batch**: Run HDBSCAN periodically to refine/merge clusters

```go
// internal/correlation/cluster.go

type ClusterEngine struct {
    // Incremental state
    clusters   map[string]*Cluster      // clusterID → cluster
    itemToCluster map[string]string     // itemID → clusterID
    centroids  map[string][]float32     // clusterID → centroid vector
    mu         sync.RWMutex

    // Batch processing
    batchTicker *time.Ticker
    embedQueue  chan *EntityResult
    embedder    Embedder

    // Output
    out        chan *ClusterResult
}

// Incremental clustering (runs per-item, <10ms with cached embeddings)
func (c *ClusterEngine) ProcessItem(er *EntityResult) *ClusterResult {
    // Get or compute embedding
    embedding := c.getEmbedding(er.Item)

    c.mu.Lock()
    defer c.mu.Unlock()

    // Apply temporal decay to distances
    now := time.Now()

    // Find best matching cluster
    var bestCluster string
    var bestSim float64

    for id, centroid := range c.centroids {
        cluster := c.clusters[id]

        // Skip if cluster is too old (zombie prevention)
        if now.Sub(cluster.NewestItem) > 48*time.Hour {
            continue
        }

        sim := cosineSimilarity(embedding, centroid)

        // Apply temporal decay
        age := now.Sub(cluster.NewestItem).Hours()
        decayedSim := sim / (1 + age/24)

        if decayedSim > bestSim && decayedSim >= 0.82 {
            bestCluster = id
            bestSim = decayedSim
        }
    }

    var result *ClusterResult

    if bestCluster != "" {
        // Add to existing cluster
        result = c.addToCluster(bestCluster, er.Item, embedding)
    } else {
        // Create new micro-cluster
        result = c.createCluster(er.Item, embedding)
    }

    return result
}

// Batch refinement (runs every 5 minutes)
func (c *ClusterEngine) runBatchRefinement(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            c.refineClusters()
        }
    }
}

func (c *ClusterEngine) refineClusters() {
    c.mu.Lock()

    // Collect centroids for HDBSCAN
    var centroids [][]float32
    var clusterIDs []string

    for id, centroid := range c.centroids {
        // Only include recent clusters
        if time.Since(c.clusters[id].NewestItem) < 24*time.Hour {
            centroids = append(centroids, centroid)
            clusterIDs = append(clusterIDs, id)
        }
    }
    c.mu.Unlock()

    if len(centroids) < 10 {
        return // Not enough for meaningful clustering
    }

    // Run HDBSCAN on centroids (this can take 100ms+)
    labels := hdbscan.Cluster(centroids, hdbscan.Config{
        MinClusterSize: 3,
        MinSamples:     2,
    })

    // Merge clusters with same label
    c.mu.Lock()
    defer c.mu.Unlock()

    mergeGroups := make(map[int][]string)
    for i, label := range labels {
        if label >= 0 { // -1 = noise
            mergeGroups[label] = append(mergeGroups[label], clusterIDs[i])
        }
    }

    for _, group := range mergeGroups {
        if len(group) > 1 {
            c.mergeClusters(group)
        }
    }
}
```

### Velocity Tracking

```go
// internal/correlation/velocity.go

type VelocityTracker struct {
    snapshots map[string]*RingBuffer[VelocitySnapshot]  // clusterID → recent snapshots
    mu        sync.RWMutex
}

type VelocitySnapshot struct {
    Timestamp    time.Time
    ItemCount    int
    SourceCount  int
    Rate15m      float64
    Rate1h       float64
    Rate6h       float64
}

func (v *VelocityTracker) Record(clusterID string, itemCount, sourceCount int) {
    v.mu.Lock()
    defer v.mu.Unlock()

    if _, ok := v.snapshots[clusterID]; !ok {
        v.snapshots[clusterID] = NewRingBuffer[VelocitySnapshot](288)  // 24h at 5min intervals
    }

    now := time.Now()
    buf := v.snapshots[clusterID]

    // Calculate rates from history
    rate15m := v.calculateRate(buf, 15*time.Minute, itemCount)
    rate1h := v.calculateRate(buf, time.Hour, itemCount)
    rate6h := v.calculateRate(buf, 6*time.Hour, itemCount)

    buf.Add(VelocitySnapshot{
        Timestamp:   now,
        ItemCount:   itemCount,
        SourceCount: sourceCount,
        Rate15m:     rate15m,
        Rate1h:      rate1h,
        Rate6h:      rate6h,
    })
}

func (v *VelocityTracker) GetSparkline(clusterID string) string {
    v.mu.RLock()
    defer v.mu.RUnlock()

    buf, ok := v.snapshots[clusterID]
    if !ok {
        return ""
    }

    // Get last 8 snapshots (40 min at 5min intervals)
    recent := buf.Last(8)
    if len(recent) < 2 {
        return ""
    }

    // Convert to sparkline
    blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
    var max float64
    for _, s := range recent {
        if s.Rate1h > max {
            max = s.Rate1h
        }
    }

    if max == 0 {
        return ""
    }

    var spark strings.Builder
    for _, s := range recent {
        idx := int((s.Rate1h / max) * float64(len(blocks)-1))
        spark.WriteRune(blocks[idx])
    }

    return spark.String()
}
```

### UI Integration

```go
// Cluster indicator in stream
func (m *Model) renderClusterBadge(item *feeds.Item) string {
    cluster := m.correlation.GetCluster(item.ID)
    if cluster == nil || cluster.Size < 2 {
        return ""
    }

    // Only show badge on primary item
    if cluster.PrimaryID != item.ID {
        return ""
    }

    badge := fmt.Sprintf("◐ %d", cluster.Size)

    // Add sparkline if velocity is interesting
    if spark := m.correlation.GetSparkline(cluster.ID); spark != "" {
        badge += " " + m.styles.muted.Render(spark)
    }

    return m.styles.clusterBadge.Render(badge)
}
```

---

## Stage 4: Disagreement Detection (Async, Per-Cluster)

### Design

Disagreement detection runs asynchronously when clusters update.

```go
// internal/correlation/disagreement.go

type DisagreementDetector struct {
    pool    *WorkerPool[*ClusterResult, *DisagreementResult]
    cache   *DisagreementCache
}

func (d *DisagreementDetector) process(cr *ClusterResult) *DisagreementResult {
    if cr.Cluster.Size < 2 {
        return nil // Need at least 2 items to disagree
    }

    // Extract claims from all items in cluster
    claims := make([]Claim, 0)
    for _, item := range cr.Cluster.Items {
        claims = append(claims, ExtractClaims(item.Title, item.Summary)...)
    }

    if len(claims) < 2 {
        return nil
    }

    // Find conflicts
    conflicts := findConflicts(claims)

    if len(conflicts) == 0 {
        return nil
    }

    return &DisagreementResult{
        ClusterID: cr.Cluster.ID,
        Claims:    claims,
        Conflicts: conflicts,
    }
}

// Claim extraction (regex-based for now, LLM later)
func ExtractClaims(title, summary string) []Claim {
    var claims []Claim

    // Numbers with context
    // "killed 47" → Claim{Type: "casualty", Value: 47, Source: ...}
    numberPatterns := []struct {
        pattern *regexp.Regexp
        typ     string
    }{
        {regexp.MustCompile(`(\d+)\s*(?:killed|dead|died)`), "casualties"},
        {regexp.MustCompile(`(\d+)\s*(?:injured|wounded|hurt)`), "injuries"},
        {regexp.MustCompile(`\$(\d+(?:\.\d+)?)\s*(?:billion|million|B|M)`), "money"},
        {regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`), "percentage"},
    }

    text := title + " " + summary
    for _, p := range numberPatterns {
        if matches := p.pattern.FindStringSubmatch(text); matches != nil {
            val, _ := strconv.ParseFloat(matches[1], 64)
            claims = append(claims, Claim{
                Type:  p.typ,
                Value: val,
                Text:  matches[0],
            })
        }
    }

    return claims
}

func findConflicts(claims []Claim) []Conflict {
    var conflicts []Conflict

    // Group by type
    byType := make(map[string][]Claim)
    for _, c := range claims {
        byType[c.Type] = append(byType[c.Type], c)
    }

    // Find conflicts within each type
    for typ, typeClaims := range byType {
        if len(typeClaims) < 2 {
            continue
        }

        // For numeric claims, check for significant variance
        var values []float64
        for _, c := range typeClaims {
            values = append(values, c.Value)
        }

        if variance(values) > 0.2 { // >20% variance
            conflicts = append(conflicts, Conflict{
                Type:   typ,
                Claims: typeClaims,
                Reason: fmt.Sprintf("%s values differ significantly", typ),
            })
        }
    }

    return conflicts
}
```

### UI Integration

```go
// Disagreement indicator
func (m *Model) renderDisagreementBadge(item *feeds.Item) string {
    cluster := m.correlation.GetCluster(item.ID)
    if cluster == nil {
        return ""
    }

    if d := m.correlation.GetDisagreement(cluster.ID); d != nil {
        return m.styles.disagreement.Render("⚡")
    }

    return ""
}
```

---

## Stage 5: Story Radar (Aggregation Layer)

### Design

The radar aggregates all correlation data into a live dashboard.

```go
// internal/correlation/radar.go

type Radar struct {
    engine *Engine

    // Cached aggregations (updated every 30s)
    topClusters []ClusterSummary
    topEntities []EntityCount
    geoDistrib  map[string]int
    mu          sync.RWMutex

    updateCh    chan struct{}
}

func (r *Radar) Start(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    // Initial compute
    r.compute()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            r.compute()
        case <-r.updateCh:
            r.compute()
        }
    }
}

func (r *Radar) compute() {
    r.mu.Lock()
    defer r.mu.Unlock()

    // Top clusters by velocity
    clusters := r.engine.GetActiveClusters()
    sort.Slice(clusters, func(i, j int) bool {
        return clusters[i].Velocity.Rate1h > clusters[j].Velocity.Rate1h
    })
    r.topClusters = clusters[:min(20, len(clusters))]

    // Top entities (last hour)
    entities := r.engine.GetEntityCounts(time.Hour)
    sort.Slice(entities, func(i, j int) bool {
        return entities[i].Count > entities[j].Count
    })
    r.topEntities = entities[:min(30, len(entities))]

    // Geographic distribution
    r.geoDistrib = r.engine.GetGeoDistribution(time.Hour)
}

func (r *Radar) GetData() RadarData {
    r.mu.RLock()
    defer r.mu.RUnlock()

    return RadarData{
        TopClusters: r.topClusters,
        TopEntities: r.topEntities,
        GeoDistrib:  r.geoDistrib,
        UpdatedAt:   time.Now(),
    }
}
```

### UI Component

```go
// internal/ui/radar/model.go

type Model struct {
    engine   *correlation.Engine
    radar    *correlation.Radar

    // View state
    focused  int        // Which section is focused
    scroll   int        // Scroll offset
    width    int
    height   int

    // Sections
    clusters list.Model
    entities list.Model
}

func (m Model) View() string {
    data := m.radar.GetData()

    // Three-column layout
    left := m.renderClusters(data.TopClusters)
    middle := m.renderEntities(data.TopEntities)
    right := m.renderGeo(data.GeoDistrib)

    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        m.styles.panel.Width(m.width/3).Render(left),
        m.styles.panel.Width(m.width/3).Render(middle),
        m.styles.panel.Width(m.width/3).Render(right),
    )
}

func (m *Model) renderClusters(clusters []correlation.ClusterSummary) string {
    var lines []string

    lines = append(lines, m.styles.header.Render("🔥 Velocity"))

    for _, c := range clusters[:min(10, len(clusters))] {
        line := fmt.Sprintf("%s %s ×%d",
            c.Sparkline,
            truncate(c.Title, 30),
            c.Size,
        )

        if c.HasDisagreement {
            line += " ⚡"
        }

        lines = append(lines, line)
    }

    return strings.Join(lines, "\n")
}
```

---

## Stage 6: Catch Me Up (Session-Aware Briefing)

### Design

```go
// internal/correlation/catchmeup.go

type CatchMeUp struct {
    engine    *Engine
    store     *store.Store
    lastSeen  time.Time
}

func (c *CatchMeUp) ShouldShow() bool {
    lastSession := c.store.GetLastSessionTime()
    return time.Since(lastSession) > 4*time.Hour
}

func (c *CatchMeUp) Generate() *Briefing {
    lastSession := c.store.GetLastSessionTime()

    // Get clusters that formed/grew since last session
    newClusters := c.engine.GetClustersSince(lastSession)

    // Filter to significant ones
    significant := make([]ClusterSummary, 0)
    for _, cluster := range newClusters {
        if cluster.Size >= 3 || cluster.Velocity.Rate1h > 5 {
            significant = append(significant, cluster)
        }
    }

    // Sort by velocity
    sort.Slice(significant, func(i, j int) bool {
        return significant[i].Velocity.Rate1h > significant[j].Velocity.Rate1h
    })

    // Get tracked entities that had activity
    trackedEntities := c.store.GetTrackedEntities()
    entityUpdates := make([]EntityUpdate, 0)
    for _, e := range trackedEntities {
        count := c.engine.GetEntityCountSince(e.ID, lastSession)
        if count > 0 {
            entityUpdates = append(entityUpdates, EntityUpdate{
                Entity:   e,
                NewItems: count,
            })
        }
    }

    return &Briefing{
        Duration:      time.Since(lastSession),
        TopDevelopments: significant[:min(5, len(significant))],
        TrackedUpdates:  entityUpdates,
        TotalNewItems:   c.engine.GetItemCountSince(lastSession),
    }
}
```

### UI Component

```go
// internal/ui/briefing/model.go

type Model struct {
    briefing *correlation.Briefing
    width    int
    height   int
    scroll   int
}

func (m Model) View() string {
    if m.briefing == nil {
        return ""
    }

    var sections []string

    // Header
    header := fmt.Sprintf("📰 While you were away (%s)", formatDuration(m.briefing.Duration))
    sections = append(sections, m.styles.header.Render(header))

    // Top developments
    sections = append(sections, m.styles.subheader.Render("Top Developments"))
    for i, c := range m.briefing.TopDevelopments {
        line := fmt.Sprintf("%d. %s (%d sources)", i+1, c.Title, c.Size)
        sections = append(sections, line)
    }

    // Tracked entities
    if len(m.briefing.TrackedUpdates) > 0 {
        sections = append(sections, "")
        sections = append(sections, m.styles.subheader.Render("Your Tracked Topics"))
        for _, e := range m.briefing.TrackedUpdates {
            line := fmt.Sprintf("• %s: %d new items", e.Entity.Name, e.NewItems)
            sections = append(sections, line)
        }
    }

    // Footer
    sections = append(sections, "")
    sections = append(sections, m.styles.muted.Render(
        fmt.Sprintf("%d total new items · Press Enter to dismiss", m.briefing.TotalNewItems),
    ))

    return lipgloss.JoinVertical(lipgloss.Left, sections...)
}
```

---

## Unified Engine Initialization

```go
// internal/correlation/engine.go

type Engine struct {
    // Core components
    bus        *EventBus
    dedup      *DedupIndex
    entities   *EntityWorker
    clusters   *ClusterEngine
    disagreements *DisagreementDetector
    radar      *Radar
    catchmeup  *CatchMeUp

    // Caches
    entityCache    *EntityCache
    clusterCache   *ClusterCache

    // Storage
    store      *store.Store

    // Metrics
    metrics    *Metrics
}

func NewEngine(store *store.Store) *Engine {
    e := &Engine{
        bus:           NewEventBus(),
        dedup:         NewDedupIndex(48 * time.Hour),
        entityCache:   NewEntityCache(10000),
        clusterCache:  NewClusterCache(1000),
        store:         store,
        metrics:       NewMetrics(),
    }

    // Initialize workers
    e.entities = NewEntityWorker(store)
    e.clusters = NewClusterEngine()
    e.disagreements = NewDisagreementDetector()
    e.radar = NewRadar(e)
    e.catchmeup = NewCatchMeUp(e, store)

    return e
}

func (e *Engine) Start(ctx context.Context) {
    // Start all workers
    e.entities.pool.Start(ctx)
    e.clusters.Start(ctx)
    e.disagreements.pool.Start(ctx)

    // Start radar aggregation
    go e.radar.Start(ctx)

    // Start the pipeline coordinator
    go e.runPipeline(ctx)
}

func (e *Engine) runPipeline(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return

        // Stage 1 → Stage 2
        case dr := <-e.bus.dedupOut:
            e.entities.pool.input <- dr

        // Stage 2 → Stage 3 + UI
        case er := <-e.entities.pool.output:
            e.entityCache.Set(er.ItemID, er.Entities)
            e.clusters.embedQueue <- er
            e.bus.Results <- EntitiesExtractedEvent{
                ItemID:   er.ItemID,
                Entities: er.Entities,
                Duration: er.Duration,
            }

        // Stage 3 → Stage 4 + UI
        case cr := <-e.clusters.out:
            e.clusterCache.Set(cr.Cluster.ID, cr.Cluster)
            e.disagreements.pool.input <- cr
            e.bus.Results <- ClusterUpdatedEvent{
                ClusterID: cr.Cluster.ID,
                Items:     cr.Cluster.ItemIDs(),
                Velocity:  cr.Velocity,
                IsNew:     cr.IsNew,
            }

        // Stage 4 → UI
        case dr := <-e.disagreements.pool.output:
            if dr != nil {
                e.bus.Results <- DisagreementDetectedEvent{
                    ClusterID: dr.ClusterID,
                    Claims:    dr.Claims,
                    Conflict:  dr.Conflicts[0].Reason,
                }
            }
        }
    }
}
```

---

## Bubble Tea Integration

```go
// internal/app/app.go

type Model struct {
    // ... existing fields
    correlation *correlation.Engine
}

func New(cfg *config.Config) *Model {
    m := &Model{
        // ... existing init
    }

    // Initialize correlation engine
    m.correlation = correlation.NewEngine(m.store)

    return m
}

func (m *Model) Init() tea.Cmd {
    // Start correlation engine
    go m.correlation.Start(m.ctx)

    // Subscribe to correlation events
    return tea.Batch(
        m.subscribeToCorrelation(),
        // ... other init commands
    )
}

func (m *Model) subscribeToCorrelation() tea.Cmd {
    return func() tea.Msg {
        // Block until event arrives
        event := <-m.correlation.Results()
        return CorrelationEventMsg{Event: event}
    }
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {

    case ItemsLoadedMsg:
        // Process items through correlation engine (non-blocking)
        for _, item := range msg.Items {
            m.correlation.ProcessItem(item) // Returns immediately
        }
        return m, nil

    case CorrelationEventMsg:
        // Handle correlation updates
        switch e := msg.Event.(type) {
        case correlation.DuplicateFoundEvent:
            // Mark item as duplicate in store
            m.store.MarkDuplicate(e.Duplicate, e.Primary)

        case correlation.ClusterUpdatedEvent:
            // Trigger re-render if visible item is affected
            if m.isItemVisible(e.Items...) {
                return m, nil // View will pick up changes
            }

        case correlation.DisagreementDetectedEvent:
            // Could show notification
        }

        // Re-subscribe for next event
        return m, m.subscribeToCorrelation()

    // ... other cases
    }
}
```

---

## Performance Budget

| Operation | Budget | Actual (Target) |
|-----------|--------|-----------------|
| SimHash | <1ms | ~0.1ms |
| Regex extraction (all) | <5ms | ~2ms |
| Cache lookup | <0.1ms | ~0.01ms |
| Incremental cluster assign | <10ms | ~5ms |
| UI render (full) | <16ms | ~8ms |
| Batch HDBSCAN (100 clusters) | <500ms | background |
| DB write (batched) | <50ms | background |

### Backpressure Handling

```go
// If any channel fills up, we drop with logging
func (e *Engine) ProcessItem(item *feeds.Item) {
    // ... dedup check

    select {
    case e.bus.dedupOut <- result:
        // Sent successfully
    default:
        // Channel full - drop and log
        e.metrics.droppedItems.Inc()
        log.Printf("warning: dropping item %s due to backpressure", item.ID)
    }
}
```

---

## Testing Strategy

### Unit Tests

```go
// internal/correlation/dedup_test.go
func TestSimHashSimilarity(t *testing.T) {
    h1 := SimHash("Fed raises rates by 0.25%")
    h2 := SimHash("Federal Reserve raises interest rates by 0.25%")
    h3 := SimHash("Apple announces new iPhone")

    assert.True(t, similarity(h1, h2) > 0.8, "similar headlines should match")
    assert.True(t, similarity(h1, h3) < 0.5, "different headlines should not match")
}
```

### Integration Tests

```go
// internal/correlation/engine_test.go
func TestPipelineFlow(t *testing.T) {
    engine := NewEngine(testStore)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go engine.Start(ctx)

    // Send test items
    for _, item := range testItems {
        engine.ProcessItem(item)
    }

    // Collect events with timeout
    var events []CorrelationEvent
    timeout := time.After(5 * time.Second)

    for len(events) < expectedCount {
        select {
        case e := <-engine.Results():
            events = append(events, e)
        case <-timeout:
            t.Fatalf("timeout waiting for events, got %d", len(events))
        }
    }

    // Verify
    assert.Contains(t, events, DuplicateFoundEvent{...})
}
```

### Benchmark Tests

```go
func BenchmarkSimHash(b *testing.B) {
    titles := loadTestTitles(1000)
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        SimHash(titles[i%len(titles)])
    }
}

func BenchmarkEntityExtraction(b *testing.B) {
    titles := loadTestTitles(1000)
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        ExtractTickers(titles[i%len(titles)])
        ExtractCountries(titles[i%len(titles)])
    }
}
```

---

## Implementation Order

```
Week 1: Foundation
├── Day 1-2: EventBus + WorkerPool infrastructure
├── Day 3-4: Stage 1 (Dedup) with UI indicator
└── Day 5: Stage 2 (Entities) with entity pills

Week 2: Clustering
├── Day 1-2: Incremental clustering
├── Day 3: Velocity tracking + sparklines
├── Day 4-5: Batch HDBSCAN refinement
└── Day 5: Cluster UI indicators

Week 3: Intelligence
├── Day 1-2: Disagreement detection
├── Day 3-4: Story Radar panel
└── Day 5: Catch Me Up briefing

Week 4: Polish
├── Day 1-2: Performance tuning
├── Day 3: Edge cases + error handling
├── Day 4-5: Testing + documentation
```

---

## Key Files to Create/Modify

### New Files
```
internal/correlation/
├── bus.go           # Event bus + channels
├── workers.go       # Generic worker pool
├── dedup.go         # SimHash index
├── entities.go      # Entity extraction worker
├── cluster.go       # Clustering engine
├── velocity.go      # Velocity tracking
├── disagreement.go  # Conflict detection
├── radar.go         # Aggregation layer
├── catchmeup.go     # Briefing generator
└── cache.go         # Hot caches

internal/ui/
├── radar/
│   └── model.go     # Radar panel
└── briefing/
    └── model.go     # Catch Me Up view
```

### Modified Files
```
internal/app/app.go       # Wire correlation engine
internal/app/messages.go  # Add CorrelationEventMsg
internal/ui/stream/model.go  # Add indicators (×N, ◐, ⚡, pills, sparklines)
internal/store/sqlite.go  # Add correlation tables + queries
```

---

## Success Metrics

- [ ] UI never blocks >16ms during correlation processing
- [ ] Duplicate detection catches >95% of syndicated rewrites
- [ ] Clustering groups related stories with >90% precision
- [ ] Velocity detection surfaces breaking news within 5 minutes
- [ ] Radar provides useful overview of current news cycle
- [ ] Catch Me Up correctly summarizes missed developments
