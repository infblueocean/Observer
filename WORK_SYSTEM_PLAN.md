# Work System - Unified Async Processing

## The Idea

Everything async is "work." Visualize it like a feed.

```
WORK QUEUE                                   Active: 3  Pending: 12  Done: 1,847

[●] Reranking 7,234 headlines               ████████░░ 78%  2.3s
[●] Fetching Reuters                        ░░░░░░░░░░      0.8s
[●] Dedup batch (50 items)                  ██████████ done 0.1s
[○] Fetch HN                                due in 45s
[○] Fetch BBC                               due in 2m
───────────────────────────────────────────────────────────────
[✓] Embedded 7,234 headlines                4.2s        3s ago
[✓] Fetched NYT                             12 new      5s ago
[✓] Filtered batch                          blocked 3   8s ago
[✗] Fetch Al Jazeera                        timeout     1m ago
```

## Why This Is Great

1. **Transparency** - See exactly what the system is doing
2. **Debugging** - Spot slow sources, failures, bottlenecks
3. **Satisfying** - Watch the machine work (like a build log)
4. **Unified** - One pattern for all async operations

---

## Work Types

| Type | Producer | Example |
|------|----------|---------|
| `fetch` | Aggregator | "Fetching Reuters" |
| `dedup` | Dedup index | "Dedup batch (50 items)" |
| `embed` | Embedder | "Embedding 7,234 headlines" |
| `rerank` | Reranker | "Reranking against 'frontpage'" |
| `filter` | Filter engine | "Applying 12 filters" |
| `analyze` | Brain trust | "Analyzing: Fed Rate Decision" |
| `enrich` | Future | "Fetching article content" |

---

## Architecture

### Core Types (`internal/work/types.go`)

```go
package work

type Type string

const (
    TypeFetch   Type = "fetch"
    TypeDedup   Type = "dedup"
    TypeEmbed   Type = "embed"
    TypeRerank  Type = "rerank"
    TypeFilter  Type = "filter"
    TypeAnalyze Type = "analyze"
)

type Status string

const (
    StatusPending  Status = "pending"
    StatusActive   Status = "active"
    StatusComplete Status = "complete"
    StatusFailed   Status = "failed"
)

type Item struct {
    ID          string
    Type        Type
    Status      Status
    Description string    // Human-readable: "Fetching Reuters"

    // Timing
    CreatedAt   time.Time
    StartedAt   time.Time
    FinishedAt  time.Time

    // Progress (optional)
    Progress    float64   // 0.0 to 1.0
    ProgressMsg string    // "1,234 of 7,234"

    // Result
    Result      string    // "12 new items", "blocked 3"
    Error       error

    // Context
    Source      string    // Source name, item ID, etc.
    Priority    int       // Higher = more urgent
}

func (i *Item) Duration() time.Duration {
    if i.FinishedAt.IsZero() {
        if i.StartedAt.IsZero() {
            return 0
        }
        return time.Since(i.StartedAt)
    }
    return i.FinishedAt.Sub(i.StartedAt)
}

func (i *Item) Age() time.Duration {
    if i.FinishedAt.IsZero() {
        return 0
    }
    return time.Since(i.FinishedAt)
}
```

### Work Pool (`internal/work/pool.go`)

```go
package work

type Pool struct {
    mu          sync.RWMutex
    workers     int

    pending     []*Item           // Priority queue
    active      map[string]*Item  // ID -> active work
    completed   *RingBuffer       // Last N completed (success + failure)

    workChan    chan *Item
    resultChan  chan *Item

    // Event subscribers (for UI updates)
    subscribers []chan Event

    // Stats
    totalCreated   int64
    totalCompleted int64
    totalFailed    int64
}

type Event struct {
    Item   *Item
    Change string // "created", "started", "progress", "completed", "failed"
}

func NewPool(workers int) *Pool {
    p := &Pool{
        workers:   workers,
        active:    make(map[string]*Item),
        completed: NewRingBuffer(100), // Keep last 100
        workChan:  make(chan *Item, 1000),
        resultChan: make(chan *Item, 100),
    }
    return p
}

func (p *Pool) Start(ctx context.Context) {
    // Start worker goroutines
    for i := 0; i < p.workers; i++ {
        go p.worker(ctx, i)
    }

    // Start result collector
    go p.collector(ctx)
}

// Submit adds work to the queue
func (p *Pool) Submit(item *Item) {
    item.ID = generateID()
    item.Status = StatusPending
    item.CreatedAt = time.Now()

    p.mu.Lock()
    p.pending = append(p.pending, item)
    p.totalCreated++
    p.mu.Unlock()

    p.notify(Event{Item: item, Change: "created"})

    // Non-blocking send to work channel
    select {
    case p.workChan <- item:
    default:
        // Queue full, stays in pending
    }
}

// SubmitFunc is a convenience for ad-hoc work
func (p *Pool) SubmitFunc(typ Type, desc string, fn func() (string, error)) {
    item := &Item{
        Type:        typ,
        Description: desc,
    }
    item.work = fn
    p.Submit(item)
}

func (p *Pool) worker(ctx context.Context, id int) {
    for {
        select {
        case <-ctx.Done():
            return
        case item := <-p.workChan:
            p.execute(item)
        }
    }
}

func (p *Pool) execute(item *Item) {
    // Mark active
    p.mu.Lock()
    item.Status = StatusActive
    item.StartedAt = time.Now()
    p.active[item.ID] = item
    // Remove from pending
    p.removePending(item.ID)
    p.mu.Unlock()

    p.notify(Event{Item: item, Change: "started"})

    // Execute the work
    result, err := item.work()

    // Mark complete
    p.mu.Lock()
    item.FinishedAt = time.Now()
    if err != nil {
        item.Status = StatusFailed
        item.Error = err
        p.totalFailed++
    } else {
        item.Status = StatusComplete
        item.Result = result
        p.totalCompleted++
    }
    delete(p.active, item.ID)
    p.completed.Push(item)
    p.mu.Unlock()

    change := "completed"
    if err != nil {
        change = "failed"
    }
    p.notify(Event{Item: item, Change: change})
}

// Snapshot returns current state for UI
func (p *Pool) Snapshot() Snapshot {
    p.mu.RLock()
    defer p.mu.RUnlock()

    return Snapshot{
        Pending:   copyItems(p.pending),
        Active:    copyActiveItems(p.active),
        Completed: p.completed.All(),
        Stats: Stats{
            TotalCreated:   p.totalCreated,
            TotalCompleted: p.totalCompleted,
            TotalFailed:    p.totalFailed,
            WorkersActive:  len(p.active),
            WorkersTotal:   p.workers,
        },
    }
}

// Subscribe returns a channel that receives work events
func (p *Pool) Subscribe() <-chan Event {
    ch := make(chan Event, 100)
    p.mu.Lock()
    p.subscribers = append(p.subscribers, ch)
    p.mu.Unlock()
    return ch
}
```

### Progress Updates

For long-running work (reranking, embedding), support progress:

```go
// Work with progress reporting
func (p *Pool) SubmitWithProgress(item *Item, fn func(progress func(float64, string)) (string, error)) {
    item.work = func() (string, error) {
        return fn(func(pct float64, msg string) {
            p.mu.Lock()
            item.Progress = pct
            item.ProgressMsg = msg
            p.mu.Unlock()
            p.notify(Event{Item: item, Change: "progress"})
        })
    }
    p.Submit(item)
}

// Usage:
pool.SubmitWithProgress(&work.Item{
    Type:        work.TypeRerank,
    Description: "Reranking 7,234 headlines",
}, func(progress func(float64, string)) (string, error) {
    for i, doc := range docs {
        // ... process doc ...
        progress(float64(i)/float64(len(docs)), fmt.Sprintf("%d of %d", i, len(docs)))
    }
    return "ranked 7,234", nil
})
```

---

## UI View (`internal/ui/workview/model.go`)

```go
package workview

type Model struct {
    pool     *work.Pool
    snapshot work.Snapshot

    width    int
    height   int
    scroll   int

    // Filter what to show
    showPending   bool
    showActive    bool
    showCompleted bool
    showFailed    bool
    filterType    work.Type // empty = all
}

func New(pool *work.Pool) Model {
    return Model{
        pool:          pool,
        showPending:   true,
        showActive:    true,
        showCompleted: true,
        showFailed:    true,
    }
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "p":
            m.showPending = !m.showPending
        case "a":
            m.showActive = !m.showActive
        case "c":
            m.showCompleted = !m.showCompleted
        case "f":
            m.showFailed = !m.showFailed
        case "1", "2", "3", "4", "5", "6":
            // Filter by type
        }
    case work.Event:
        m.snapshot = m.pool.Snapshot()
    case TickMsg:
        m.snapshot = m.pool.Snapshot()
    }
    return m, nil
}

func (m Model) View() string {
    var b strings.Builder

    // Header with stats
    s := m.snapshot.Stats
    b.WriteString(fmt.Sprintf(
        "WORK QUEUE                    Active: %d  Pending: %d  Done: %d  Failed: %d\n\n",
        s.WorkersActive, len(m.snapshot.Pending), s.TotalCompleted, s.TotalFailed,
    ))

    // Active work (always on top)
    for _, item := range m.snapshot.Active {
        b.WriteString(m.renderItem(item))
        b.WriteString("\n")
    }

    // Pending work
    if m.showPending && len(m.snapshot.Pending) > 0 {
        for _, item := range m.snapshot.Pending[:min(5, len(m.snapshot.Pending))] {
            b.WriteString(m.renderItem(item))
            b.WriteString("\n")
        }
        if len(m.snapshot.Pending) > 5 {
            b.WriteString(fmt.Sprintf("  ... and %d more pending\n", len(m.snapshot.Pending)-5))
        }
    }

    // Divider
    b.WriteString("─────────────────────────────────────────────────────\n")

    // Recent completed/failed
    for _, item := range m.snapshot.Completed {
        if item.Status == StatusFailed && !m.showFailed {
            continue
        }
        if item.Status == StatusComplete && !m.showCompleted {
            continue
        }
        b.WriteString(m.renderItem(item))
        b.WriteString("\n")
    }

    return b.String()
}

func (m Model) renderItem(item *work.Item) string {
    var icon, status string

    switch item.Status {
    case work.StatusPending:
        icon = "○"
        status = formatDue(item)  // "due in 45s"
    case work.StatusActive:
        icon = "●"
        if item.Progress > 0 {
            status = renderProgress(item.Progress) + " " + formatDuration(item.Duration())
        } else {
            status = formatDuration(item.Duration())
        }
    case work.StatusComplete:
        icon = "✓"
        status = fmt.Sprintf("%-12s %s ago", item.Result, formatAge(item.Age()))
    case work.StatusFailed:
        icon = "✗"
        status = fmt.Sprintf("%-12s %s ago", item.Error, formatAge(item.Age()))
    }

    // Truncate description to fit
    desc := truncate(item.Description, 40)

    return fmt.Sprintf("[%s] %-42s %s", icon, desc, status)
}

func renderProgress(pct float64) string {
    width := 10
    filled := int(pct * float64(width))
    return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
```

---

## Integration Points

### 1. Feed Fetching

```go
// In aggregator.go
func (a *Aggregator) fetchSource(source feeds.Source) {
    a.workPool.Submit(&work.Item{
        Type:        work.TypeFetch,
        Description: fmt.Sprintf("Fetching %s", source.Name()),
        Source:      source.Name(),
        work: func() (string, error) {
            items, err := source.Fetch()
            if err != nil {
                return "", err
            }
            a.processItems(items)
            return fmt.Sprintf("%d items", len(items)), nil
        },
    })
}
```

### 2. Deduplication

```go
// In dedup processing
func (d *DedupProcessor) ProcessBatch(items []feeds.Item) {
    d.workPool.Submit(&work.Item{
        Type:        work.TypeDedup,
        Description: fmt.Sprintf("Dedup batch (%d items)", len(items)),
        work: func() (string, error) {
            dupes := 0
            for _, item := range items {
                if isDupe, _ := d.index.Check(&item); isDupe {
                    dupes++
                }
            }
            return fmt.Sprintf("%d dupes", dupes), nil
        },
    })
}
```

### 3. Reranking

```go
// In ranking
func (r *RankingEngine) Rerank(headlines []feeds.Item, rubric string) {
    r.workPool.SubmitWithProgress(&work.Item{
        Type:        work.TypeRerank,
        Description: fmt.Sprintf("Reranking %d headlines", len(headlines)),
    }, func(progress func(float64, string)) (string, error) {
        scores, err := r.reranker.RerankWithProgress(rubric, headlines, progress)
        if err != nil {
            return "", err
        }
        r.applyScores(scores)
        return fmt.Sprintf("ranked %d", len(headlines)), nil
    })
}
```

### 4. AI Analysis

```go
// In brain trust
func (a *Analyzer) Analyze(item feeds.Item) {
    a.workPool.Submit(&work.Item{
        Type:        work.TypeAnalyze,
        Description: fmt.Sprintf("Analyzing: %s", truncate(item.Title, 30)),
        Source:      item.ID,
        work: func() (string, error) {
            result, err := a.runAnalysis(item)
            if err != nil {
                return "", err
            }
            return "complete", nil
        },
    })
}
```

---

## Keyboard Shortcuts (Work View)

| Key | Action |
|-----|--------|
| `w` or `/w` | Toggle work view |
| `p` | Toggle pending visibility |
| `a` | Toggle active visibility |
| `c` | Toggle completed visibility |
| `f` | Toggle failed visibility |
| `1-6` | Filter by work type |
| `r` | Refresh snapshot |
| `x` | Clear completed history |
| `esc` | Close work view |

---

## Package Structure

```
internal/work/
├── types.go      # Item, Type, Status, Event
├── pool.go       # Pool, workers, queue management
├── ring.go       # RingBuffer for completed history
└── work_test.go

internal/ui/workview/
├── model.go      # Bubble Tea model
└── render.go     # Rendering helpers
```

---

## Benefits

1. **Single pattern** for all async work
2. **Observable** - see what's happening in real-time
3. **Debuggable** - spot slow sources, failures
4. **Satisfying** - watch the machine work
5. **Extensible** - new work types just plug in
6. **Metrics ready** - stats are built in

---

## Open Questions

1. **Worker count:** Fixed pool (e.g., 8) or dynamic?
2. **Priority queue:** Simple FIFO or priority-based?
3. **Persistence:** Save work history to SQLite?
4. **Cancellation:** Allow canceling pending work?

---

## Relationship to v0.3 Ranking

The work system is **foundational infrastructure** that v0.3 builds on:

```
v0.3 Ranking
    ├── Reranker submits work to pool
    ├── Embedder submits work to pool
    ├── Results flow back via events
    └── UI shows ranking progress in work view

Work System
    ├── Unified pool for all async ops
    ├── Progress tracking
    ├── Event subscription for UI
    └── Stats and history
```

Build work system first, then v0.3 ranking uses it naturally.
