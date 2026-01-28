# Observer v0.5 Implementation Plan (Final)

## Executive Summary

This is the final implementation plan for Observer v0.5, incorporating feedback from round 2 adversarial reviews. Changes from v2:

1. **Coordinator simplified** - Context cancellation only (removed stopCh)
2. **Removed unused fields** - Deleted `lastFetch` map and `Source.Interval`
3. **Deleted `EmbedBatch`** - Single `Embed()` method until profiling proves batch needed
4. **Filters moved to Phase 2** - ByAge, Dedup, LimitPerSource are immediate, not deferred
5. **Coordinator testing strategy** - Function injection pattern documented
6. **Context timeout per fetch** - Each fetch wrapped in context.WithTimeout
7. **Phase 4 cut** - Embeddings moved to Future (v0.6) section
8. **Sources immutability documented** - Explicit note that sources slice never changes

**Core Architecture:**
- Removed work pool complexity (use simple goroutines)
- Removed event subscription system (use Bubble Tea messages)
- Fixed MVC: View receives items via messages, never holds Store reference
- Fixed Filter interface: sync filters don't see async concerns
- Consolidated to 3 committed phases + documented future work
- Explicit concurrency contracts for every shared structure

---

## Architecture Overview

```
cmd/observer/main.go           # Entry point, wires components
internal/
  store/
    store.go                   # SQLite persistence (concrete, not interface)
    store_test.go
  fetch/
    fetcher.go                 # RSS/HN/Reddit fetching
    fetcher_test.go
  filter/
    filter.go                  # Filter functions (not interfaces)
    filter_test.go
  ui/
    app.go                     # Bubble Tea root model
    stream.go                  # Main feed view
    styles.go                  # Lip Gloss styles
    messages.go                # Bubble Tea messages (the only "event system")
```

**Key Simplifications:**
- No `internal/work/` package. Background work uses goroutines with `sync.WaitGroup`.
- No `internal/controller/` layer. The UI coordinates fetching and filtering directly.
- No interfaces for Store, Embedder, or Fetcher in production code. Interfaces exist only in test files for mocking.

---

## Phase 1: Store and Fetch (Foundation)

**Goal:** Fetch RSS feeds, store in SQLite, retrieve for display.

### 1.1 Store

**File:** `internal/store/store.go`

```go
// Store handles SQLite persistence. NOT an interface - concrete type.
// Thread-safety: All methods are safe for concurrent use via internal mutex.
type Store struct {
    db *sql.DB
    mu sync.RWMutex  // Protects all database operations
}

// Item represents stored content.
type Item struct {
    ID         string
    SourceType string     // "rss", "hn", "reddit"
    SourceName string
    Title      string
    Summary    string
    URL        string
    Author     string
    Published  time.Time
    Fetched    time.Time
    Read       bool
    Saved      bool
}

// Open creates a new Store with the given database path.
// Creates tables if they don't exist.
func Open(dbPath string) (*Store, error)

// Close closes the database connection.
func (s *Store) Close() error

// SaveItems stores items, returning count of new items inserted.
// Duplicates (by URL) are silently ignored.
// Thread-safe: acquires write lock.
func (s *Store) SaveItems(items []Item) (int, error)

// GetItems retrieves items for display.
// Thread-safe: acquires read lock.
func (s *Store) GetItems(limit int, includeRead bool) ([]Item, error)

// GetItemsSince retrieves items published after the given time.
// Thread-safe: acquires read lock.
func (s *Store) GetItemsSince(since time.Time) ([]Item, error)

// MarkRead marks an item as read.
// Thread-safe: acquires write lock.
func (s *Store) MarkRead(id string) error

// MarkSaved toggles the saved state of an item.
// Thread-safe: acquires write lock.
func (s *Store) MarkSaved(id string, saved bool) error
```

**SQLite Schema:**

```sql
CREATE TABLE IF NOT EXISTS items (
    id TEXT PRIMARY KEY,
    source_type TEXT NOT NULL,
    source_name TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT,
    url TEXT UNIQUE,
    author TEXT,
    published_at DATETIME NOT NULL,
    fetched_at DATETIME NOT NULL,
    read INTEGER DEFAULT 0,
    saved INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_items_published ON items(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_source ON items(source_name);
CREATE INDEX IF NOT EXISTS idx_items_url ON items(url);
```

**Concurrency Contract:**
- `Store.mu` is a `sync.RWMutex`
- Read methods (`GetItems`, `GetItemsSince`) acquire `mu.RLock()`
- Write methods (`SaveItems`, `MarkRead`, `MarkSaved`) acquire `mu.Lock()`
- All methods release locks via `defer`
- SQLite is opened with `_journal_mode=WAL` for better concurrent read performance

**Testing Strategy:**
```go
// store_test.go
func TestSaveItems(t *testing.T)           // Basic insert
func TestSaveItemsDuplicate(t *testing.T)  // URL dedup
func TestGetItems(t *testing.T)            // Retrieval with limit
func TestGetItemsIncludeRead(t *testing.T) // Read filter
func TestMarkRead(t *testing.T)            // State change
func TestConcurrentAccess(t *testing.T)    // Multiple goroutines read/write
```

### 1.2 Fetcher

**File:** `internal/fetch/fetcher.go`

```go
// Source represents a feed source configuration.
// NOTE: No Interval field - all sources fetched on the same global interval in v0.5.
type Source struct {
    Type string // "rss", "hn", "reddit"
    Name string // Display name
    URL  string // Feed URL
}

// Fetcher retrieves items from sources. NOT an interface.
type Fetcher struct {
    client *http.Client
}

// NewFetcher creates a Fetcher with the given timeout.
func NewFetcher(timeout time.Duration) *Fetcher

// Fetch retrieves items from a source. Returns items and any error.
// Does NOT store items - caller decides what to do with them.
func (f *Fetcher) Fetch(ctx context.Context, src Source) ([]store.Item, error)
```

**Testing Strategy:**
```go
// fetcher_test.go
// Use httptest.Server for all tests - no real network calls

func TestFetchRSS(t *testing.T)           // Parse RSS XML
func TestFetchMalformedRSS(t *testing.T)  // Handle bad XML gracefully
func TestFetchTimeout(t *testing.T)       // Context cancellation
func TestFetchHTTPError(t *testing.T)     // 404, 500, etc.
```

### 1.3 Phase 1 Success Criteria

- [ ] Store saves and retrieves items
- [ ] Store handles concurrent access without races (`go test -race`)
- [ ] Fetcher parses RSS feeds
- [ ] Fetcher handles errors gracefully
- [ ] All tests pass

---

## Phase 2: UI and Filters (Users See Something)

**Goal:** Display items in a TUI with time bands, navigation, and basic filtering.

**Rationale:** Ship the UI before background features. A TUI that shows items is useful. Background embedding without UI is useless. Filters are included here because they're needed immediately - the first fetch will return duplicates and old items.

### 2.1 Bubble Tea Messages

**File:** `internal/ui/messages.go`

```go
// Messages are the ONLY event system. No custom pub/sub.

// ItemsLoaded is sent when items are fetched from the store.
type ItemsLoaded struct {
    Items []store.Item
    Err   error
}

// ItemMarkedRead is sent when an item is marked as read.
type ItemMarkedRead struct {
    ID string
}

// FetchComplete is sent when background fetch finishes.
type FetchComplete struct {
    Source    string
    NewItems  int
    Err       error
}

// RefreshTick triggers periodic refresh.
type RefreshTick struct{}
```

### 2.2 App Model

**File:** `internal/ui/app.go`

```go
// App is the root Bubble Tea model.
// IMPORTANT: App does NOT hold *store.Store. It receives items via messages.
type App struct {
    // Dependencies passed as functions FOR TESTING
    // In production, main.go creates closures that call the real store
    loadItems    func() tea.Cmd
    markRead     func(id string) tea.Cmd
    triggerFetch func() tea.Cmd

    // View state
    items    []store.Item
    cursor   int
    viewport viewport.Model
    err      error

    // UI state
    width  int
    height int
}

// NewApp creates the app with dependency functions.
// These functions return tea.Cmd that produce messages.
func NewApp(
    loadItems func() tea.Cmd,
    markRead func(id string) tea.Cmd,
    triggerFetch func() tea.Cmd,
) App

func (a App) Init() tea.Cmd
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (a App) View() string
```

**In main.go:**
```go
func main() {
    st, _ := store.Open("observer.db")
    defer st.Close()

    fetcher := fetch.NewFetcher(30 * time.Second)
    sources := loadSources()

    app := ui.NewApp(
        // loadItems: returns a Cmd that loads from store and applies filters
        func() tea.Cmd {
            return func() tea.Msg {
                items, err := st.GetItems(500, false)
                if err != nil {
                    return ui.ItemsLoaded{Err: err}
                }
                // Apply filters before sending to UI
                items = filter.ByAge(items, 24*time.Hour)
                items = filter.Dedup(items)
                items = filter.LimitPerSource(items, 20)
                return ui.ItemsLoaded{Items: items}
            }
        },
        // markRead: returns a Cmd that marks item read
        func(id string) tea.Cmd {
            return func() tea.Msg {
                st.MarkRead(id)
                return ui.ItemMarkedRead{ID: id}
            }
        },
        // triggerFetch: returns a Cmd that fetches all sources
        func() tea.Cmd {
            return func() tea.Msg {
                // Fetch in goroutine, results come back as messages
                // (detailed in Phase 3)
            }
        },
    )

    tea.NewProgram(app, tea.WithAltScreen()).Run()
}
```

### 2.3 Filter Functions

**File:** `internal/filter/filter.go`

```go
// Filter functions are simple: []Item in, []Item out.
// No interfaces, no work pools, no async.

// ByAge removes items older than maxAge.
func ByAge(items []store.Item, maxAge time.Duration) []store.Item

// BySource keeps only items from specified sources.
func BySource(items []store.Item, sources []string) []store.Item

// Dedup removes items with duplicate URLs or very similar titles.
func Dedup(items []store.Item) []store.Item

// LimitPerSource caps the number of items per source.
func LimitPerSource(items []store.Item, maxPerSource int) []store.Item
```

**Testing Strategy:**
```go
func TestByAge(t *testing.T)           // Keeps recent, removes old
func TestBySource(t *testing.T)        // Filters by source name
func TestDedup(t *testing.T)           // URL and title similarity
func TestLimitPerSource(t *testing.T)  // Caps per source
```

### 2.4 Stream View

**File:** `internal/ui/stream.go`

```go
// RenderStream renders the item list with time bands.
func RenderStream(items []store.Item, cursor int, width, height int) string

// TimeBand returns a display string for grouping items.
// This is a VIEW concern, not a model concern.
func TimeBand(published time.Time) string {
    age := time.Since(published)
    switch {
    case age < 15*time.Minute:
        return "Just Now"
    case age < 1*time.Hour:
        return "Past Hour"
    case age < 24*time.Hour:
        return "Today"
    case age < 48*time.Hour:
        return "Yesterday"
    default:
        return "Older"
    }
}
```

### 2.5 Key Bindings

| Key | Action |
|-----|--------|
| `j/k` or `up/down` | Navigate |
| `Enter` | Mark read |
| `g/G` | Top/bottom |
| `r` | Refresh |
| `q` | Quit |

### 2.6 Phase 2 Success Criteria

- [ ] TUI displays items with time band groupings
- [ ] Navigation works (j/k, g/G)
- [ ] Enter marks item as read
- [ ] 'r' triggers refresh
- [ ] 'q' quits cleanly
- [ ] App receives items via messages (no Store reference)
- [ ] Filters reduce item count as expected
- [ ] Filters are pure functions (no side effects)

---

## Phase 3: Background Fetch (Continuous Updates)

**Goal:** Periodically fetch new items in the background.

### 3.1 Coordinator

**File:** `internal/coord/coordinator.go` (or inline in main.go)

```go
// Coordinator manages background fetching.
// Uses context cancellation as the ONLY stop mechanism.
type Coordinator struct {
    store   *store.Store
    fetcher *fetch.Fetcher
    sources []fetch.Source  // IMMUTABLE: set at construction, never modified
    wg      sync.WaitGroup
}

// NewCoordinator creates a Coordinator.
// The sources slice is copied and never modified after construction.
func NewCoordinator(store *store.Store, fetcher *fetch.Fetcher, sources []fetch.Source) *Coordinator {
    // Copy sources to ensure immutability
    srcCopy := make([]fetch.Source, len(sources))
    copy(srcCopy, sources)
    return &Coordinator{
        store:   store,
        fetcher: fetcher,
        sources: srcCopy,
    }
}

func (c *Coordinator) Start(ctx context.Context, program *tea.Program) {
    c.wg.Add(1)
    go func() {
        defer c.wg.Done()
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()

        // Initial fetch on startup
        c.fetchAll(ctx, program)

        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                c.fetchAll(ctx, program)
            }
        }
    }()
}

// Wait blocks until the background goroutine exits.
// Call after canceling the context passed to Start.
func (c *Coordinator) Wait() {
    c.wg.Wait()
}

func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
    for _, src := range c.sources {
        // Check context before each fetch
        if ctx.Err() != nil {
            return
        }

        // Wrap each fetch in its own timeout
        fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
        items, err := c.fetcher.Fetch(fetchCtx, src)
        cancel()

        if err != nil {
            program.Send(ui.FetchComplete{Source: src.Name, Err: err})
            continue
        }

        n, _ := c.store.SaveItems(items)
        program.Send(ui.FetchComplete{Source: src.Name, NewItems: n})
    }
}
```

**Concurrency Contract for Coordinator:**
- `sources` slice is immutable - set at construction, never modified (safe for concurrent reads)
- No internal mutex needed - Coordinator has no mutable state
- Context cancellation is the ONLY stop mechanism
- `wg.Wait()` ensures goroutine completes before caller proceeds

### 3.2 Testing Strategy for Coordinator

Test Coordinator without real network using function injection, similar to App:

```go
// coordinator_test.go

// mockFetcher allows controlling fetch behavior in tests
type mockFetcher struct {
    fetchFunc func(ctx context.Context, src fetch.Source) ([]store.Item, error)
}

func (m *mockFetcher) Fetch(ctx context.Context, src fetch.Source) ([]store.Item, error) {
    return m.fetchFunc(ctx, src)
}

func TestCoordinatorFetchesAllSources(t *testing.T) {
    st, _ := store.Open(":memory:")
    defer st.Close()

    fetchCalls := make([]string, 0)
    mf := &mockFetcher{
        fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
            fetchCalls = append(fetchCalls, src.Name)
            return []store.Item{{ID: src.Name + "-1"}}, nil
        },
    }

    sources := []fetch.Source{
        {Name: "source1", URL: "http://example.com/1"},
        {Name: "source2", URL: "http://example.com/2"},
    }

    // Create coordinator with mock
    coord := NewCoordinatorWithFetcher(st, mf, sources)

    ctx, cancel := context.WithCancel(context.Background())
    // Use a mock program or nil if Send is not called in test
    coord.fetchAll(ctx, nil)
    cancel()

    assert.Equal(t, []string{"source1", "source2"}, fetchCalls)
}

func TestCoordinatorRespectsContextCancellation(t *testing.T) {
    st, _ := store.Open(":memory:")
    defer st.Close()

    fetchCount := 0
    mf := &mockFetcher{
        fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
            fetchCount++
            return nil, nil
        },
    }

    sources := []fetch.Source{
        {Name: "source1"},
        {Name: "source2"},
        {Name: "source3"},
    }

    coord := NewCoordinatorWithFetcher(st, mf, sources)

    // Cancel immediately
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    coord.fetchAll(ctx, nil)

    // Should stop early due to cancelled context
    assert.Less(t, fetchCount, 3)
}

func TestCoordinatorHandlesFetchTimeout(t *testing.T) {
    st, _ := store.Open(":memory:")
    defer st.Close()

    mf := &mockFetcher{
        fetchFunc: func(ctx context.Context, src fetch.Source) ([]store.Item, error) {
            // Simulate slow fetch
            select {
            case <-ctx.Done():
                return nil, ctx.Err()
            case <-time.After(1 * time.Second):
                return []store.Item{}, nil
            }
        },
    }

    sources := []fetch.Source{{Name: "slow"}}
    coord := NewCoordinatorWithFetcher(st, mf, sources)

    // Very short timeout
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancel()

    var receivedErr error
    mockProgram := &mockTeaProgram{
        sendFunc: func(msg tea.Msg) {
            if fc, ok := msg.(ui.FetchComplete); ok {
                receivedErr = fc.Err
            }
        },
    }

    coord.fetchAll(ctx, mockProgram)

    assert.Error(t, receivedErr)
}
```

**Interface for Testing:**

To support the mock fetcher, define an interface in the test file or a small internal interface:

```go
// In coordinator.go or coordinator_test.go
type fetcher interface {
    Fetch(ctx context.Context, src fetch.Source) ([]store.Item, error)
}

// NewCoordinatorWithFetcher allows injecting a custom fetcher (for testing)
func NewCoordinatorWithFetcher(store *store.Store, f fetcher, sources []fetch.Source) *Coordinator
```

### 3.3 Graceful Shutdown

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    st, _ := store.Open("observer.db")
    defer st.Close()

    fetcher := fetch.NewFetcher(30 * time.Second)
    sources := loadSources()

    coord := NewCoordinator(st, fetcher, sources)

    app := ui.NewApp(/* ... */)
    program := tea.NewProgram(app, tea.WithAltScreen())

    coord.Start(ctx, program)

    // Run blocks until quit
    program.Run()

    // Shutdown sequence:
    // 1. Cancel context (signals goroutine to stop)
    // 2. coord.Wait() waits for background goroutine
    // 3. st.Close() closes database (deferred)
    cancel()
    coord.Wait()
}
```

**Lifecycle Documentation:**
- Coordinator must have its context cancelled before Store is closed
- `coord.Wait()` must be called after cancelling context to ensure clean shutdown
- Store methods may still be called during shutdown (they're thread-safe)

### 3.4 Phase 3 Success Criteria

- [ ] Background fetch runs every 5 minutes
- [ ] New items appear in UI after fetch
- [ ] Individual fetches have 30-second timeout
- [ ] Graceful shutdown waits for in-flight fetches
- [ ] No goroutine leaks (verified with runtime inspection)
- [ ] All Coordinator tests pass without real network
- [ ] `go test -race ./...` passes

---

## Testing Architecture

### Mock Strategy

Interfaces exist ONLY in test files, not production code.

**File:** `internal/store/store_test.go`
```go
// StoreInterface is used ONLY for testing UI components
type StoreInterface interface {
    GetItems(limit int, includeRead bool) ([]Item, error)
    MarkRead(id string) error
    SaveItems(items []Item) (int, error)
}

// Verify Store implements StoreInterface
var _ StoreInterface = (*Store)(nil)
```

**File:** `internal/ui/app_test.go`
```go
func TestAppLoadsItems(t *testing.T) {
    // Mock via function injection
    app := NewApp(
        func() tea.Cmd {
            return func() tea.Msg {
                return ItemsLoaded{Items: testItems}
            }
        },
        func(id string) tea.Cmd { return nil },
        func() tea.Cmd { return nil },
    )

    // Send Init, verify items appear
}
```

### Concurrency Testing

```go
func TestStoreConcurrent(t *testing.T) {
    st, _ := store.Open(":memory:")
    defer st.Close()

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(2)
        go func(n int) {
            defer wg.Done()
            st.SaveItems([]Item{{ID: fmt.Sprintf("w%d", n)}})
        }(i)
        go func() {
            defer wg.Done()
            st.GetItems(100, true)
        }()
    }
    wg.Wait()
}
```

Run with: `go test -race ./...`

### Test Categories

| Category | Location | Run Command |
|----------|----------|-------------|
| Unit | `*_test.go` in each package | `go test ./...` |
| Race | Same files | `go test -race ./...` |
| Integration | `test/integration/` (future) | `go test ./test/integration/` |

---

## Concurrency Summary

Every shared data structure with explicit synchronization:

| Structure | Location | Protection | Who Reads | Who Writes |
|-----------|----------|------------|-----------|------------|
| `Store.db` | store.go | `Store.mu sync.RWMutex` | UI (via functions), Coordinator | Coordinator, UI mark-read |
| `Coordinator.sources` | coordinator.go | None needed - immutable after construction | Coordinator goroutine | Never (set at construction) |
| `App.items` | app.go | None needed - Bubble Tea is single-threaded | App.View() | App.Update() |

**Channel Contracts:**

| Channel | Buffer Size | Creator | Closer | Backpressure |
|---------|-------------|---------|--------|--------------|
| Bubble Tea internal | (managed by tea) | tea.Program | tea.Program | tea.Program handles |

**There are no custom event channels.** All events flow through Bubble Tea's message system.

---

## Future Work (v0.6+)

Explicitly deferred to future versions:

### Embeddings and Semantic Features
- **OllamaEmbedder** - Local embeddings via Ollama
- **SemanticDedup** - Embedding-based duplicate detection
- **Similarity search** - Find related items
- **float32 embeddings** - Efficient storage and comparison

When implemented:
```go
// internal/embed/ollama.go
type OllamaEmbedder struct {
    baseURL string
    model   string
    client  *http.Client
}

func (e *OllamaEmbedder) Available() bool
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error)
```

### Infrastructure
- **Work pool / job queue** - If parallel fetching needed
- **HNSW index** - If item count exceeds 10k
- **Per-source fetch intervals** - Different refresh rates per source
- **Reranking filters** - User-controlled semantic ranking
- **Priority levels** - Different urgency for different work

---

## What's NOT in v0.5

Explicitly excluded:

- **Work pool / job queue**: Use simple goroutines
- **Event subscription system**: Use Bubble Tea messages
- **Controller interfaces**: Concrete types only
- **Embeddings**: Deferred to v0.6
- **HNSW index**: Brute-force similarity for <10k items (when embeddings added)
- **Per-source intervals**: Global 5-minute interval for all sources
- **Multiple priority levels**: FIFO is fine

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| SQLite slow at scale | WAL mode, proper indexes, limit queries to 500 items |
| Too many items | ByAge filter default 24h, LimitPerSource default 20 |
| Fetch errors | Log and continue, show error count in status bar |
| Individual fetch hangs | 30-second context timeout per fetch |
| Race conditions | Explicit mutex for every shared structure, `-race` in CI |
| Goroutine leaks | Context cancellation + WaitGroup for clean shutdown |

---

## Success Criteria (Full v0.5)

- [ ] Fetch RSS feeds from configured sources
- [ ] Store items in SQLite
- [ ] Display items in TUI with time bands
- [ ] Navigate with j/k, mark read with Enter
- [ ] Basic filters applied (ByAge, Dedup, LimitPerSource)
- [ ] Background refresh every 5 minutes
- [ ] Graceful shutdown (no goroutine leaks)
- [ ] All tests pass with `-race`

---

## Appendix: Addressing Review Criticisms

### From Architecture Review v2

| Criticism | How Addressed |
|-----------|---------------|
| Two stop mechanisms (context + stopCh) | Context cancellation only |
| Function injection may not scale | Acceptable for 3 functions; consider AppDeps struct if > 5 |
| No embedding generation strategy | Embeddings moved to v0.6 |

### From Concurrency Review v2

| Criticism | How Addressed |
|-----------|---------------|
| No timeout on individual fetches | 30-second context.WithTimeout per fetch |
| Sequential fetch is bottleneck | Acceptable for v0.5; consider errgroup in v0.6 |
| Sources slice not documented as immutable | Explicit note in Coordinator |

### From Testing Review v2

| Criticism | How Addressed |
|-----------|---------------|
| Coordinator testing strategy missing | Function injection pattern with mock fetcher |
| No integration test spec | Deferred to after Phase 3 ships |

### From Pragmatism Review v2

| Criticism | How Addressed |
|-----------|---------------|
| lastFetch unused | Deleted |
| Source.Interval unused | Deleted |
| EmbedBatch premature | Deleted |
| Filters should be immediate | Moved to Phase 2 |
| Phase 4 "optional" is wishy-washy | Cut from v0.5, moved to Future section |

---

## Development Guidelines

**Interface Policy:** Interfaces are created only when there are 2+ implementations. Test-only interfaces stay in test files.

**Commit Policy:** Run `go test -race ./...` before committing.

**Dependency Ownership:** Components are owned by their creator:
- `main()` creates Store, Fetcher, Coordinator
- Coordinator must be stopped (via context cancellation + Wait) before Store is closed
- Store is closed last (via defer)
