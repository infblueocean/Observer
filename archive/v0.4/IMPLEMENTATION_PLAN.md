# Correlation Engine Implementation Plan

> **Design Principle:** Never block the UI. Every operation is either instant (<5ms) or async with progressive display.

## Current State (After v0.2 Cleanup)

### What Already Works

The correlation engine already has significant functionality in `engine.go` (1,037 LOC):

| Feature | Status | Location |
|---------|--------|----------|
| SimHash deduplication | ✅ Working | `findOrCreateDuplicateGroup()` |
| Cheap entity extraction | ✅ Working | `cheap.go` (tickers, countries, attributions) |
| Cluster formation | ✅ Working | `findOrCreateCluster()` |
| Velocity tracking | ✅ Working | `updateVelocitySnapshots()`, `GetClusterSparklineData()` |
| Disagreement detection | ✅ Working | `checkClusterDisagreements()` |
| Activity feed | ✅ Working | `addActivity()`, `GetRecentActivity()` |

### What's Missing

| Feature | Status | Needed For |
|---------|--------|------------|
| UI indicators | ❌ Not wired | Show ×N, ◐, ⚡ in stream |
| Entity pills | ❌ Not wired | Display extracted entities |
| Story Radar panel | ❌ Not built | Ambient awareness view |
| Catch Me Up briefing | ❌ Not built | Session resume |
| Event bus for Bubble Tea | ❌ Not built | Async UI updates |

### Existing API

```go
// Current signatures (engine.go)
func NewEngine(db *sql.DB, extractor EntityExtractor) (*Engine, error)
func NewEngineSimple(db *sql.DB) (*Engine, error)  // Uses CheapExtractor
func (e *Engine) ProcessItem(item feeds.Item) (*ItemCorrelations, error)
func (e *Engine) ProcessItems(items []feeds.Item)  // Batch processing
func (e *Engine) GetDuplicateCount(itemID string) int
func (e *Engine) GetClusterInfo(itemID string) *Cluster
func (e *Engine) GetClusterSparklineData(clusterID string, points int) []float64
func (e *Engine) HasDisagreements(clusterID string) bool
func (e *Engine) GetItemEntities(itemID string) []ItemEntity
func (e *Engine) GetActiveClusters(limit int) []ClusterSummary
func (e *Engine) GetTopEntities(since time.Time, limit int) ([]Entity, error)
```

---

## Architecture: Incremental Enhancement

Rather than rewriting the engine, we'll **add** the missing pieces:

```
┌──────────────────────────────────────────────────────────────────┐
│                    BUBBLE TEA EVENT LOOP                          │
│                   (never blocked - sacred)                        │
└──────────────────────────────────────────────────────────────────┘
                              ▲
                              │ tea.Msg
                              │
┌──────────────────────────────────────────────────────────────────┐
│                    correlation.Engine                             │
│                                                                  │
│  ProcessItem() ──→ [existing sync processing] ──→ Results chan   │
│       │                                                │         │
│       ▼                                                ▼         │
│  SimHash dedup                                   UI Events       │
│  Entity extraction                               - DuplicateFound│
│  Cluster assignment                              - ClusterUpdated│
│  Disagreement check                              - EntityExtracted│
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                       ┌──────────────┐
                       │   SQLite     │
                       │   (WAL)      │
                       └──────────────┘
```

### Key Insight

The existing `ProcessItem()` is **synchronous and fast** (~2-5ms per item). This is fine for batch processing on feed refresh. We don't need worker pools for the current scale.

What we DO need:
1. **Results channel** - Push events to Bubble Tea without blocking
2. **UI integration** - Wire indicators into stream view
3. **New views** - Radar panel, Catch Me Up briefing

---

## Phase 1: Wire Existing Engine to UI

**Goal:** Show indicators in stream view using existing engine methods.

### Step 1.1: Add Results Channel

```go
// internal/correlation/engine.go - ADD to Engine struct

type Engine struct {
    // ... existing fields ...

    // UI event channel (new)
    results chan CorrelationEvent
}

// CorrelationEvent is sent to Bubble Tea
type CorrelationEvent interface {
    correlationEvent()
}

type DuplicateFoundEvent struct {
    PrimaryID   string
    DuplicateID string
    Count       int
}

type ClusterUpdatedEvent struct {
    ClusterID string
    ItemID    string
    Size      int
}

func (e *Engine) Results() <-chan CorrelationEvent {
    return e.results
}
```

### Step 1.2: Emit Events from ProcessItem

```go
// internal/correlation/engine.go - MODIFY ProcessItem()

func (e *Engine) ProcessItem(item feeds.Item) (*ItemCorrelations, error) {
    // ... existing code ...

    // After duplicate detection
    if result.DuplicateGroup != nil && len(result.DuplicateGroup.ItemIDs) > 1 {
        select {
        case e.results <- DuplicateFoundEvent{
            PrimaryID:   result.DuplicateGroup.ItemIDs[0],
            DuplicateID: item.ID,
            Count:       len(result.DuplicateGroup.ItemIDs),
        }:
        default:
            // Don't block if channel full
        }
    }

    // After cluster assignment
    if result.Cluster != nil {
        select {
        case e.results <- ClusterUpdatedEvent{
            ClusterID: result.Cluster.ID,
            ItemID:    item.ID,
            Size:      result.Cluster.ItemCount,
        }:
        default:
        }
    }

    return result, nil
}
```

### Step 1.3: Subscribe in app.go

```go
// internal/app/app.go - ADD subscription

func (m *Model) subscribeCorrelation() tea.Cmd {
    if m.correlationEngine == nil {
        return nil
    }
    return func() tea.Msg {
        event := <-m.correlationEngine.Results()
        return CorrelationEventMsg{Event: event}
    }
}

// In Update()
case CorrelationEventMsg:
    // Re-render affected items
    return m, m.subscribeCorrelation()
```

### Step 1.4: Add Indicators to Stream View

```go
// internal/ui/stream/model.go - MODIFY renderItem()

func (m *Model) renderItem(item *feeds.Item, index int) string {
    var badges []string

    // Duplicate count (×N)
    if m.correlation != nil {
        if count := m.correlation.GetDuplicateCount(item.ID); count > 0 {
            badges = append(badges, m.styles.muted.Render(fmt.Sprintf("×%d", count+1)))
        }

        // Cluster indicator (◐ N)
        if cluster := m.correlation.GetClusterInfo(item.ID); cluster != nil {
            if m.correlation.IsClusterPrimary(item.ID) && cluster.ItemCount > 1 {
                badge := fmt.Sprintf("◐%d", cluster.ItemCount)
                // Add sparkline if available
                if data := m.correlation.GetClusterSparklineData(cluster.ID, 8); len(data) > 0 {
                    badge += " " + sparkline(data)
                }
                badges = append(badges, m.styles.cluster.Render(badge))
            }
        }

        // Disagreement indicator (⚡)
        if m.correlation.ItemHasDisagreement(item.ID) {
            badges = append(badges, m.styles.warning.Render("⚡"))
        }
    }

    // ... rest of rendering
}

func sparkline(data []float64) string {
    blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
    max := 0.0
    for _, v := range data {
        if v > max { max = v }
    }
    if max == 0 { return "" }

    var s strings.Builder
    for _, v := range data {
        idx := int((v / max) * float64(len(blocks)-1))
        s.WriteRune(blocks[idx])
    }
    return s.String()
}
```

### Step 1.5: Entity Pills on Focused Item

```go
// internal/ui/stream/model.go - ADD to focused item rendering

func (m *Model) renderFocusedItem(item *feeds.Item) string {
    // ... existing focused rendering ...

    // Add entity pills
    if m.correlation != nil {
        entities := m.correlation.GetItemEntities(item.ID)
        if len(entities) > 0 {
            pills := m.renderEntityPills(entities[:min(5, len(entities))])
            // Insert pills after title
        }
    }
}

func (m *Model) renderEntityPills(entities []correlation.ItemEntity) string {
    var pills []string
    for _, e := range entities {
        var style lipgloss.Style
        switch {
        case strings.HasPrefix(e.EntityID, "$"):
            style = m.styles.ticker  // Blue for tickers
        case e.Type == "country":
            style = m.styles.country // Flag style
        default:
            style = m.styles.entity  // Gray
        }
        pills = append(pills, style.Render(e.EntityID))
    }
    return lipgloss.JoinHorizontal(lipgloss.Center, pills...)
}
```

### Files Modified (Phase 1)

| File | Changes |
|------|---------|
| `internal/correlation/engine.go` | Add results channel, emit events |
| `internal/app/app.go` | Subscribe to correlation events |
| `internal/app/messages.go` | Add CorrelationEventMsg |
| `internal/ui/stream/model.go` | Add indicator rendering |

### Verification (Phase 1)

```bash
go build ./... && ./observer
# - See ×N on duplicate stories
# - See ◐N on clustered stories
# - See ⚡ on stories with disagreements
# - See entity pills on focused item
```

---

## Phase 2: Story Radar Panel

**Goal:** Ambient awareness view showing what's happening across all sources.

### Step 2.1: Create Radar Data Methods

```go
// internal/correlation/engine.go - ADD methods

type RadarData struct {
    TopClusters   []ClusterSummary
    TopEntities   []EntityCount
    GeoDistrib    map[string]int
    UpdatedAt     time.Time
}

type EntityCount struct {
    EntityID string
    Name     string
    Type     string
    Count    int
}

func (e *Engine) GetRadarData() RadarData {
    now := time.Now()
    since := now.Add(-1 * time.Hour)

    return RadarData{
        TopClusters: e.GetActiveClusters(10),
        TopEntities: e.getEntityCounts(since, 20),
        GeoDistrib:  e.getGeoDistribution(since),
        UpdatedAt:   now,
    }
}

func (e *Engine) getEntityCounts(since time.Time, limit int) []EntityCount {
    // Query item_entities joined with items where published_at > since
    // Group by entity_id, order by count desc
}

func (e *Engine) getGeoDistribution(since time.Time) map[string]int {
    // Count items per country entity in time window
}
```

### Step 2.2: Create Radar UI Component

```go
// internal/ui/radar/model.go - NEW FILE

package radar

type Model struct {
    engine    *correlation.Engine
    data      correlation.RadarData

    focused   int  // Which section: 0=clusters, 1=entities, 2=geo
    scroll    int
    width     int
    height    int
}

func New(engine *correlation.Engine) Model {
    return Model{engine: engine}
}

func (m Model) Init() tea.Cmd {
    return m.refresh()
}

func (m *Model) refresh() tea.Cmd {
    return func() tea.Msg {
        return RadarDataMsg{Data: m.engine.GetRadarData()}
    }
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
    switch msg := msg.(type) {
    case RadarDataMsg:
        m.data = msg.Data
        // Schedule next refresh in 30s
        return m, tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
            return RefreshRadarMsg{}
        })
    case RefreshRadarMsg:
        return m, m.refresh()
    case tea.KeyMsg:
        switch msg.String() {
        case "tab":
            m.focused = (m.focused + 1) % 3
        case "j", "down":
            m.scroll++
        case "k", "up":
            m.scroll--
        }
    }
    return m, nil
}

func (m Model) View() string {
    // Three-column layout
    left := m.renderClusters()
    middle := m.renderEntities()
    right := m.renderGeo()

    return lipgloss.JoinHorizontal(lipgloss.Top,
        m.panel(left, m.focused == 0),
        m.panel(middle, m.focused == 1),
        m.panel(right, m.focused == 2),
    )
}

func (m Model) renderClusters() string {
    var lines []string
    lines = append(lines, "🔥 Velocity")

    for _, c := range m.data.TopClusters {
        line := fmt.Sprintf("%s %s ×%d",
            sparkline(c.SparklineData),
            truncate(c.Title, 25),
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

### Step 2.3: Wire Radar to App

```go
// internal/app/app.go - ADD radar mode

type Model struct {
    // ... existing fields ...
    radarMode    bool
    radar        radar.Model
}

// In key handler
case "ctrl+r", "R":
    m.radarMode = !m.radarMode
    if m.radarMode && m.correlationEngine != nil {
        m.radar = radar.New(m.correlationEngine)
        return m, m.radar.Init()
    }
    return m, nil

// In View()
if m.radarMode {
    return m.radar.View()
}
```

### Files Created/Modified (Phase 2)

| File | Changes |
|------|---------|
| `internal/correlation/engine.go` | Add GetRadarData(), helper methods |
| `internal/ui/radar/model.go` | **NEW** - Radar panel component |
| `internal/app/app.go` | Add radar mode toggle |

---

## Phase 3: Catch Me Up Briefing

**Goal:** Smart summary for returning users.

### Step 3.1: Session Tracking

```go
// internal/store/sqlite.go - ADD session tracking

func (s *Store) RecordSessionEnd() error {
    _, err := s.db.Exec(`
        INSERT OR REPLACE INTO session_state (key, value, updated_at)
        VALUES ('last_session', datetime('now'), datetime('now'))
    `)
    return err
}

func (s *Store) GetLastSessionTime() (time.Time, error) {
    var ts string
    err := s.db.QueryRow(`
        SELECT value FROM session_state WHERE key = 'last_session'
    `).Scan(&ts)
    if err != nil {
        return time.Time{}, err
    }
    return time.Parse("2006-01-02 15:04:05", ts)
}
```

### Step 3.2: Briefing Generator

```go
// internal/correlation/briefing.go - NEW FILE

package correlation

type Briefing struct {
    Duration        time.Duration
    NewItemCount    int
    TopDevelopments []ClusterSummary
    EntityUpdates   []EntityUpdate
}

type EntityUpdate struct {
    EntityID string
    Name     string
    NewCount int
}

func (e *Engine) GenerateBriefing(since time.Time) *Briefing {
    // Get clusters formed/grown since last session
    clusters := e.getClustersSince(since)

    // Filter to significant ones (size >= 3 or velocity > threshold)
    var significant []ClusterSummary
    for _, c := range clusters {
        if c.Size >= 3 || c.Velocity > 5.0 {
            significant = append(significant, c)
        }
    }

    // Sort by velocity
    sort.Slice(significant, func(i, j int) bool {
        return significant[i].Velocity > significant[j].Velocity
    })

    // Get entity activity
    entityUpdates := e.getEntityUpdatesSince(since)

    return &Briefing{
        Duration:        time.Since(since),
        NewItemCount:    e.getItemCountSince(since),
        TopDevelopments: significant[:min(5, len(significant))],
        EntityUpdates:   entityUpdates,
    }
}
```

### Step 3.3: Briefing UI Component

```go
// internal/ui/briefing/model.go - NEW FILE

package briefing

type Model struct {
    briefing *correlation.Briefing
    width    int
    height   int
}

func New(b *correlation.Briefing, w, h int) Model {
    return Model{briefing: b, width: w, height: h}
}

func (m Model) View() string {
    if m.briefing == nil {
        return ""
    }

    var sections []string

    // Header
    sections = append(sections,
        style.Header.Render(fmt.Sprintf(
            "📰 While you were away (%s)",
            formatDuration(m.briefing.Duration),
        )),
    )

    // Top developments
    if len(m.briefing.TopDevelopments) > 0 {
        sections = append(sections, style.Subheader.Render("Top Developments"))
        for i, c := range m.briefing.TopDevelopments {
            sections = append(sections, fmt.Sprintf(
                "%d. %s (%d sources)",
                i+1, c.Title, c.Size,
            ))
        }
    }

    // Footer
    sections = append(sections,
        style.Muted.Render(fmt.Sprintf(
            "%d new items · Press Enter to dismiss",
            m.briefing.NewItemCount,
        )),
    )

    return lipgloss.JoinVertical(lipgloss.Left, sections...)
}
```

### Step 3.4: Show Briefing on Startup

```go
// internal/app/app.go - ADD briefing check

func (m *Model) Init() tea.Cmd {
    cmds := []tea.Cmd{
        // ... existing init commands ...
    }

    // Check if we should show briefing
    if m.store != nil && m.correlationEngine != nil {
        lastSession, err := m.store.GetLastSessionTime()
        if err == nil && time.Since(lastSession) > 4*time.Hour {
            m.briefing = m.correlationEngine.GenerateBriefing(lastSession)
            m.showBriefing = true
        }
    }

    return tea.Batch(cmds...)
}

// Record session end on quit
case "q", "ctrl+c":
    if m.store != nil {
        m.store.RecordSessionEnd()
    }
    return m, tea.Quit
```

### Files Created/Modified (Phase 3)

| File | Changes |
|------|---------|
| `internal/store/sqlite.go` | Add session tracking |
| `internal/correlation/briefing.go` | **NEW** - Briefing generator |
| `internal/ui/briefing/model.go` | **NEW** - Briefing UI |
| `internal/app/app.go` | Show briefing on startup |

---

## Implementation Order

```
Phase 1: Wire UI Indicators (2-3 days)
├── Add results channel to engine
├── Emit events from ProcessItem
├── Subscribe in app.go
├── Add ×N, ◐, ⚡ indicators to stream
├── Add entity pills on focused item
└── TEST: Visual verification

Phase 2: Story Radar (2-3 days)
├── Add GetRadarData() and helpers
├── Create radar/model.go
├── Wire Ctrl+R toggle
└── TEST: Radar shows live data

Phase 3: Catch Me Up (2 days)
├── Add session tracking to store
├── Create briefing.go
├── Create briefing/model.go
├── Show on startup after 4h gap
└── TEST: Briefing appears correctly
```

---

## Performance Guarantees

| Operation | Budget | Current |
|-----------|--------|---------|
| ProcessItem() | <10ms | ~3ms |
| GetDuplicateCount() | <1ms | <0.1ms (in-memory) |
| GetClusterInfo() | <1ms | <0.1ms (in-memory) |
| GetRadarData() | <50ms | ~20ms (batched queries) |
| GenerateBriefing() | <100ms | ~50ms |
| UI render | <16ms | ~8ms |

### Backpressure

Results channel is buffered (size 100). If full, events are dropped silently - UI will catch up on next render cycle.

---

## Testing Checklist

### Phase 1
- [ ] ×N shows on duplicate stories
- [ ] ◐N shows on cluster primary
- [ ] Sparkline animates with velocity
- [ ] ⚡ shows on disagreements
- [ ] Entity pills appear on focus
- [ ] No UI lag during feed refresh

### Phase 2
- [ ] Ctrl+R toggles radar
- [ ] Clusters sorted by velocity
- [ ] Entities show counts
- [ ] Geo distribution renders
- [ ] Auto-refresh every 30s
- [ ] Escape returns to stream

### Phase 3
- [ ] Briefing shows after 4h+ gap
- [ ] Top developments listed
- [ ] Enter dismisses briefing
- [ ] Session end recorded on quit
- [ ] Fresh start shows no briefing

---

## Success Metrics

- [ ] UI never blocks >16ms during correlation processing
- [ ] Duplicate detection catches >95% of syndicated rewrites
- [ ] Clustering groups related stories with >90% precision
- [ ] Velocity sparklines update smoothly
- [ ] Radar provides useful overview
- [ ] Catch Me Up correctly summarizes missed news
