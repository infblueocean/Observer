# Observer Master Fix Plan v2.1

**Created:** 2026-01-28
**Status:** REVISED after v2.0 adversarial review
**Iteration:** 3
**Changes from v2.0:** 15 issues fixed based on sign-off review

---

## Changes from v2.0

| Issue | v2.0 | v2.1 Fix |
|-------|------|----------|
| Unsubscribe method | Called but doesn't exist | Added implementation |
| Analysis goroutines | Only 1 spawn point shown | All 4 listed explicitly |
| Debug server | No shutdown | Added graceful shutdown |
| SaveItems signature | Changed to 3 returns | Keep `(int, error)`, log failed |
| Analyses schema | TEXT PRIMARY KEY | INTEGER AUTOINCREMENT |
| Migration script | Only embedding column | Full tables + validation |
| Type conversion | "Handle it" | Explicit conversion functions |
| Rollback testing | Untested | Phase 0.6 added |
| Unsubscribe verification | Assumed working | Phase 0.7 added |
| Week 5 checkpoint | Missing | Added |
| Analysis queue full | Silent drop | Return error to UI |
| Column existence check | Error string matching | pragma_table_info |
| Goroutine stability | Vague "±10" | ±10 over 1-hour window from baseline |
| Phase 2.7/2.8 | Week 2-3 overlap | Moved to Phase 1 Track B |

---

## Phase 0: Preparation (MUST COMPLETE FIRST)

**Timeline:** 3-4 days before Phase 1

### 0.1 Enable Race Detection

```bash
export CGO_ENABLED=1
go test -race ./internal/work/...
```

**If CGO unavailable (e.g., Alpine):**
- Race detection limited to dev/CI
- Add runtime goroutine monitoring as fallback

### 0.2 Add Observability Endpoints

**File:** `internal/debug/debug.go`
```go
package debug

import (
    "context"
    "fmt"
    "net/http"
    _ "net/http/pprof"
    "runtime"
    "time"
)

var server *http.Server

func StartDebugServer(addr string) {
    mux := http.NewServeMux()
    mux.HandleFunc("/debug/goroutines", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "goroutines: %d\n", runtime.NumGoroutine())
    })
    // pprof handlers registered automatically via import

    server = &http.Server{Addr: addr, Handler: mux}
    go server.ListenAndServe()
}

func StopDebugServer() {
    if server != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        server.Shutdown(ctx)
    }
}
```

**Call from main.go:**
```go
debug.StartDebugServer("localhost:6060")
defer debug.StopDebugServer()
```

### 0.3 Take Database Backup

```bash
cp ~/.observer/observer.db ~/.observer/observer.db.pre-fix-$(date +%Y%m%d)
```

### 0.4 Create Smoke Test

**File:** `internal/app/smoke_test.go`
```go
func TestAppStartsAndStops(t *testing.T) {
    tmpDir := t.TempDir()
    // Initialize with test config
    // Verify no panic
    // Call saveAndClose
    // Verify clean shutdown
}
```

### 0.5 Baseline Metrics

Record before any fixes:
- `runtime.NumGoroutine()` at startup (after feeds loaded) = **baseline**
- Memory RSS after 10 minutes
- HTTP connections via pprof

### 0.6 Test Rollback Procedure (NEW)

**Before Phase 1 starts:**
```bash
# 1. Take backup
cp ~/.observer/observer.db ~/.observer/observer.db.rollback-test

# 2. Make trivial schema change
sqlite3 ~/.observer/observer.db "ALTER TABLE items ADD COLUMN test_col TEXT"

# 3. Practice rollback
cp ~/.observer/observer.db.rollback-test ~/.observer/observer.db

# 4. Verify app starts
./observer  # Should work without test_col

# 5. Document
echo "Rollback tested $(date), took N minutes" >> ROLLBACK_LOG.md
```

### 0.7 Verify Unsubscribe Prevents Leaks (NEW)

**File:** `internal/work/pool_unsubscribe_test.go`
```go
func TestUnsubscribePreventsLeak(t *testing.T) {
    pool := NewPool(2)
    pool.Start()
    defer pool.Stop()

    startGoroutines := runtime.NumGoroutine()

    // Subscribe
    ch := pool.Subscribe()

    // Do some work
    for i := 0; i < 10; i++ {
        pool.SubmitFunc(TypeFetch, "test", func() (string, error) {
            return "ok", nil
        })
    }
    time.Sleep(100 * time.Millisecond)

    // Unsubscribe
    pool.Unsubscribe(ch)

    // Drain channel
    for len(ch) > 0 {
        <-ch
    }

    time.Sleep(100 * time.Millisecond)

    endGoroutines := runtime.NumGoroutine()
    if endGoroutines > startGoroutines+2 {
        t.Errorf("goroutine leak: started %d, ended %d", startGoroutines, endGoroutines)
    }
}
```

---

## Phase 1: Critical Safety Fixes

**Timeline:** Week 1-2 (parallel tracks)

### Track A: Goroutine & Concurrency (Week 1)

#### 1.1 Worker Goroutine Tracking

**File:** `internal/work/pool.go:282-283`

```go
// OLD:
go p.execute(item)

// NEW:
p.wg.Add(1)
go func(it *Item) {
    defer p.wg.Done()
    p.execute(it)
}(item)
```

**Test:** `pool_shutdown_test.go` (see Phase 0.7 pattern)

#### 1.2 Add Unsubscribe Method (NEW - was missing!)

**File:** `internal/work/pool.go` - Add this method:

```go
// Unsubscribe removes a subscriber channel and closes it.
// Safe to call multiple times with same channel.
func (p *Pool) Unsubscribe(ch <-chan Event) {
    p.subscribersMu.Lock()
    defer p.subscribersMu.Unlock()

    for i, sub := range p.subscribers {
        if sub == ch {
            // Remove from slice
            p.subscribers = append(p.subscribers[:i], p.subscribers[i+1:]...)
            // Close the channel (safe, we own it)
            close(sub)
            return
        }
    }
}
```

#### 1.3 Analysis Goroutine Limiting

**File:** `internal/brain/trust.go`

**Add to Analyzer struct:**
```go
type Analyzer struct {
    // existing fields...
    analysisSem chan struct{} // Limit concurrent analyses
}

func NewAnalyzer(provider Provider) *Analyzer {
    return &Analyzer{
        analyses:    make(map[string]*Analysis),
        analysisSem: make(chan struct{}, 4), // 4 concurrent max
        // ...
    }
}
```

**Wrap ALL 4 goroutine spawn points:**

| Line | Function | Description |
|------|----------|-------------|
| ~283 | analyzeInternal | Cloud provider analysis |
| ~318 | analyzeInternal | Local provider analysis |
| ~588 | AnalyzeRandomProvider | Random provider analysis |
| ~766 | generateZingers | Async zinger generation |

**Pattern for EACH spawn point:**
```go
// Before spawning goroutine:
select {
case a.analysisSem <- struct{}{}:
    go func() {
        defer func() { <-a.analysisSem }()
        // ... existing goroutine code
    }()
case <-ctx.Done():
    return ctx.Err()
default:
    // Queue full - return error to caller (NOT silent drop)
    return fmt.Errorf("analysis queue full, try again later")
}
```

**UI handling:** When analysis returns error, display in status bar:
```go
// In app.go handleAnalysis:
if err != nil {
    m.recentErrors = append(m.recentErrors, err.Error())
    m.lastErrorTime = time.Now()
}
```

#### 1.4 Subscriber Cleanup

**File:** `internal/app/app.go` in `saveAndClose()`:

```go
func (m Model) saveAndClose() {
    // FIRST: Stop receiving work events
    if m.workPool != nil && m.workEventChan != nil {
        m.workPool.Unsubscribe(m.workEventChan)
    }

    // ... rest of existing cleanup
}
```

### Track B: Resource Leaks (Week 1-2)

#### 1.5 HTTP Client Consolidation

**File:** `internal/brain/http_provider.go`

```go
type HTTPProvider struct {
    config          *ProviderConfig
    client          *http.Client
    streamingClient *http.Client
}

func NewHTTPProvider(cfg *ProviderConfig) *HTTPProvider {
    transport := &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        MaxConnsPerHost:     50,
        IdleConnTimeout:     90 * time.Second,
    }

    return &HTTPProvider{
        config: cfg,
        client: &http.Client{
            Timeout:   120 * time.Second,
            Transport: transport,
        },
        streamingClient: &http.Client{
            Transport: &http.Transport{
                MaxIdleConns:        10,
                MaxIdleConnsPerHost: 5,
                MaxConnsPerHost:     10,
                IdleConnTimeout:     180 * time.Second,
            },
        },
    }
}
```

**In GenerateStream():** Use `p.streamingClient` instead of `&http.Client{}`

#### 1.6 Aggregator Memory Cap

**File:** `internal/feeds/aggregator.go`

```go
const (
    maxItemAge        = 7 * 24 * time.Hour
    maxItems          = 50000
    evictionInterval  = 1000
)

type Aggregator struct {
    // existing fields...
    mergesSinceEviction int // Protected by mu
}

func (a *Aggregator) MergeItems(newItems []Item) int {
    a.mu.Lock()
    defer a.mu.Unlock()

    // Check eviction under lock (counter protected by mu)
    a.mergesSinceEviction++
    if a.mergesSinceEviction >= evictionInterval || len(a.items) > maxItems-1000 {
        a.evictOldItems()
        a.mergesSinceEviction = 0
    }

    // ... rest of merge logic
}

func (a *Aggregator) evictOldItems() {
    // Called under lock
    cutoff := time.Now().Add(-maxItemAge)

    // Pre-allocate conservatively
    capacity := min(len(a.items), maxItems)
    newItems := make([]Item, 0, capacity)

    for _, item := range a.items {
        if item.PublishedAt.After(cutoff) {
            newItems = append(newItems, item)
        }
    }

    // Cap if still too many (keep newest)
    if len(newItems) > maxItems {
        newItems = newItems[len(newItems)-maxItems:]
    }

    a.items = newItems // Old array eligible for GC
}
```

#### 1.7 Context Cleanup Audit

**Grep and fix:**
```bash
grep -rn "context.WithTimeout\|context.WithCancel" internal/ | grep -v "_test.go"
```

**Known fixes:**
- `app.go:2290` - Change explicit `cancel()` to `defer cancel()` pattern
- `app.go:2014` - Use app lifecycle context, not `context.Background()`

#### 1.8 Database Connection Tuning

**File:** `internal/model/store.go` in `New()` after `sql.Open`:

```go
db.SetMaxOpenConns(25)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
```

#### 1.9 DB Connection Leak Fix (moved from Phase 2)

**File:** `internal/store/sqlite.go:26-29`

```go
if err := s.migrate(); err != nil {
    db.Close() // Prevent leak
    return nil, fmt.Errorf("migration failed: %w", err)
}
```

#### 1.10 Rows.Err() Check (moved from Phase 2)

**Files to fix:**
- `internal/store/sqlite.go` in `GetItems()` (after line ~234)
- `internal/store/sqlite.go` in `GetRecentItems()` (similar location)
- `internal/model/store.go` in `GetItems()` (verify present)

**Add after every `for rows.Next()` loop:**
```go
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("error iterating rows: %w", err)
}
```

---

## Phase 2: Error Handling Hardening

**Timeline:** Week 2-3 (parallel with Phase 1 Track B completion)

### 2.1 Config Load - Distinguish Missing vs Corrupted

**File:** `internal/app/app.go:125`

```go
cfg, err := config.Load()
if err != nil {
    if os.IsNotExist(err) {
        logging.Info("No config found, creating defaults")
        cfg = config.DefaultConfig()
    } else {
        return Model{}, fmt.Errorf("config corrupted: %w\nDelete %s or fix JSON syntax", err, config.Path())
    }
}
```

### 2.2 Store Init - Fail Fast

Already correct - return error from `New()`.

### 2.3 Track Failed Item Saves (REVISED - keep signature)

**File:** `internal/model/store.go` SaveItems method

**Keep signature as `(int, error)` - just add logging:**

```go
func (s *Store) SaveItems(items []model.Item) (int, error) {
    // ... existing transaction setup

    var saved int
    var failedIDs []string

    for _, item := range items {
        _, err := stmt.Exec(...)
        if err != nil {
            logging.Error("Failed to save item",
                "id", item.ID,
                "title", item.Title[:min(50, len(item.Title))],
                "error", err)
            failedIDs = append(failedIDs, item.ID)
            continue
        }
        saved++
    }

    if err := tx.Commit(); err != nil {
        // Commit failed = ALL saves lost
        return 0, fmt.Errorf("commit failed (all %d items lost): %w", saved, err)
    }

    if len(failedIDs) > 0 {
        logging.Warn("Some items failed to save",
            "failed_count", len(failedIDs),
            "saved_count", saved,
            "failed_ids", failedIDs)
    }

    return saved, nil
}
```

### 2.4 Panic Recovery Stack Traces

**File:** `internal/work/pool.go:289-295`

```go
defer func() {
    if r := recover(); r != nil {
        buf := make([]byte, 8192)
        n := runtime.Stack(buf, false)
        logging.Error("Work panicked",
            "id", item.ID,
            "type", item.Type,
            "desc", item.Description,
            "panic", r,
            "stack", string(buf[:n]))
        p.complete(item, "", fmt.Errorf("panic: %v", r))
    }
}()
```

### 2.5 Migration - Use pragma_table_info (REVISED)

**File:** `internal/model/store.go`

```go
func (s *Store) migrate() error {
    if err := s.db.Ping(); err != nil {
        return fmt.Errorf("database connection failed: %w", err)
    }

    // Run base schema
    if _, err := s.db.Exec(schema); err != nil {
        return fmt.Errorf("schema creation failed: %w", err)
    }

    // Add embedding column if missing (use pragma, not error strings)
    if !s.columnExists("items", "embedding") {
        if _, err := s.db.Exec("ALTER TABLE items ADD COLUMN embedding BLOB"); err != nil {
            return fmt.Errorf("failed to add embedding column: %w", err)
        }
    }

    return nil
}

func (s *Store) columnExists(table, column string) bool {
    query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='%s'", table, column)
    var count int
    s.db.QueryRow(query).Scan(&count)
    return count > 0
}
```

### 2.6 HomeDir Error

**File:** `internal/app/app.go:138`

```go
homeDir, err := os.UserHomeDir()
if err != nil {
    return Model{}, fmt.Errorf("cannot determine home directory: %w", err)
}
```

---

## Phase 3: Store Consolidation

**Timeline:** Week 3-4.5 (extended from v2.0)

### 3.1 Schema Audit (CORRECTED)

**Actual differences:**

| Feature | store/sqlite.go | model/store.go |
|---------|-----------------|----------------|
| items table | ✅ | ✅ |
| sources table | ✅ | ✅ |
| sessions table | ✅ | ✅ |
| analyses table | ✅ | ❌ MISSING |
| top_stories_cache | ✅ | ❌ MISSING |
| embedding column | ❌ | ✅ |

### 3.2 Add Missing Tables to model/store.go (CORRECTED SCHEMA)

**Add to schema string:**
```sql
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
    title TEXT NOT NULL,
    label TEXT NOT NULL,
    reason TEXT,
    zinger TEXT,
    first_seen DATETIME NOT NULL,
    last_seen DATETIME NOT NULL,
    hit_count INTEGER DEFAULT 1,
    miss_count INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_top_stories_last_seen ON top_stories_cache(last_seen DESC);
```

### 3.3 Port Analysis Methods

Copy from `store/sqlite.go` to `model/store.go`:
- `SaveAnalysis(itemID, provider, model, prompt, rawResponse, content, error string) error`
- `GetAnalysisContent(itemID string) (string, error)`

### 3.4 Port Top Stories Cache Methods

Copy from `store/sqlite.go` to `model/store.go`:
- `SaveTopStoriesCache(entries []TopStoryCacheEntry) error`
- `LoadTopStoriesCache() ([]TopStoryCacheEntry, error)`

### 3.5 Migration Script (COMPLETE)

**File:** `scripts/migrate-store.sh`
```bash
#!/bin/bash
set -e

DB_PATH="${HOME}/.observer/observer.db"
BACKUP_PATH="${DB_PATH}.backup.$(date +%Y%m%d%H%M%S)"

echo "=== Observer Database Migration ==="
echo "Database: $DB_PATH"

# Backup
echo "1. Creating backup at $BACKUP_PATH"
cp "$DB_PATH" "$BACKUP_PATH"

echo "2. Running migrations..."
sqlite3 "$DB_PATH" <<'EOF'
-- Add embedding column if missing
SELECT CASE
    WHEN COUNT(*) = 0 THEN 'Adding embedding column'
    ELSE 'Embedding column exists'
END FROM pragma_table_info('items') WHERE name='embedding';

ALTER TABLE items ADD COLUMN embedding BLOB;

-- Create analyses table if missing
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

-- Create top_stories_cache table if missing
CREATE TABLE IF NOT EXISTS top_stories_cache (
    item_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    label TEXT NOT NULL,
    reason TEXT,
    zinger TEXT,
    first_seen DATETIME NOT NULL,
    last_seen DATETIME NOT NULL,
    hit_count INTEGER DEFAULT 1,
    miss_count INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_top_stories_last_seen ON top_stories_cache(last_seen DESC);
EOF

echo "3. Verifying migration..."
# Errors from ALTER TABLE on existing column are expected, ignore them

echo "4. Running integrity check..."
sqlite3 "$DB_PATH" "PRAGMA integrity_check"

echo "=== Migration complete ==="
```

### 3.5.1 Post-Migration Data Validation (NEW)

```bash
# After running migration script:
echo "Validating data..."

# Count items
ITEMS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM items")
echo "Items: $ITEMS"

# Count with embeddings
EMBEDDED=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM items WHERE embedding IS NOT NULL")
echo "Items with embeddings: $EMBEDDED"

# Check analyses
ANALYSES=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM analyses")
echo "Analyses: $ANALYSES"

# Check top stories cache
CACHE=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM top_stories_cache")
echo "Top stories cache entries: $CACHE"

# Verify no orphaned analyses
ORPHANS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM analyses WHERE item_id NOT IN (SELECT id FROM items)")
if [ "$ORPHANS" -gt 0 ]; then
    echo "WARNING: $ORPHANS orphaned analyses found"
fi

echo "Validation complete"
```

### 3.6 Type Conversion Strategy (NEW - was missing)

**Problem:** Two Item types exist:
- `feeds.Item` - Used by aggregator, fetchers
- `model.Item` - Used by model/store, embedding

**Strategy:** Create explicit conversion functions

**File:** `internal/model/convert.go`
```go
package model

import "github.com/user/observer/internal/feeds"

// FromFeedsItem converts feeds.Item to model.Item
func FromFeedsItem(f feeds.Item) Item {
    return Item{
        ID:          f.ID,
        SourceType:  string(f.SourceType),
        SourceName:  f.SourceName,
        Title:       f.Title,
        Summary:     f.Summary,
        URL:         f.URL,
        PublishedAt: f.PublishedAt,
        Read:        f.Read,
        Saved:       f.Saved,
        Hidden:      f.Hidden,
        // Embedding left nil - populated by dedup
    }
}

// ToFeedsItem converts model.Item to feeds.Item
func (m Item) ToFeedsItem() feeds.Item {
    return feeds.Item{
        ID:          m.ID,
        SourceType:  feeds.SourceType(m.SourceType),
        SourceName:  m.SourceName,
        Title:       m.Title,
        Summary:     m.Summary,
        URL:         m.URL,
        PublishedAt: m.PublishedAt,
        Read:        m.Read,
        Saved:       m.Saved,
        Hidden:      m.Hidden,
    }
}

// FromFeedsItems converts a slice
func FromFeedsItems(items []feeds.Item) []Item {
    result := make([]Item, len(items))
    for i, item := range items {
        result[i] = FromFeedsItem(item)
    }
    return result
}
```

**Usage in app.go when saving:**
```go
modelItems := model.FromFeedsItems(feedsItems)
saved, err := store.SaveItems(modelItems)
```

### 3.7 Update Imports (Week 4)

1. Update `internal/app/app.go`:
   - Change `import "internal/store"` to `import "internal/model"`
   - Use conversion functions at boundaries
2. Run full test suite
3. Delete `internal/store/` directory

### 3.8 Archive v0.5 Code

**Create:** `archive/v0.5/README.md`

**Move:** `cmd/observer-v05/` → `archive/v0.5/cmd/`

**Verify v0.5-only packages:**
```bash
# Check what uses internal/controller, internal/view, internal/intake
grep -r "internal/controller\|internal/view\|internal/intake" cmd/ internal/app/
```

If only `cmd/observer-v05` uses them → move to archive.
If main app uses them → keep in place.

---

## Phase 4: Testing & Quality

**Timeline:** Parallel throughout Weeks 1-4

### 4.1 Tests FOR THE FIXES (Priority 1)

| Fix | Test File | What to Test |
|-----|-----------|--------------|
| 1.1 Worker tracking | `pool_shutdown_test.go` | Stop() waits for all workers |
| 1.2 Unsubscribe | `pool_unsubscribe_test.go` | Goroutine count drops after unsubscribe |
| 1.3 Analysis limiting | `trust_concurrency_test.go` | Max 4 concurrent, error on full |
| 1.6 Aggregator cap | `aggregator_eviction_test.go` | Items capped at 50k |
| 3.6 Type conversion | `convert_test.go` | Round-trip preserves data |

### 4.2 Race Detection Tests

```bash
go test -race ./internal/work/...
go test -race ./internal/embedding/...
go test -race ./internal/brain/...
```

### 4.3 Smoke Test

Implemented in Phase 0.4.

### 4.4 Constants Extraction (Local Only)

Extract to top of each file, not global package:

**`internal/app/app.go`:**
```go
const (
    errorDisplayDuration = 10 * time.Second
    aiPanelMaxLines      = 12
    aiPanelMinLines      = 6
)
```

**`internal/ui/stream/model.go`:**
```go
const (
    timeBandJustNow   = 15 * time.Minute
    timeBandPastHour  = 1 * time.Hour
    timeBandToday     = 24 * time.Hour
    breakingThreshold = 30 * time.Minute
)
```

### 4.5 Delete Commented Code

**File:** `internal/feeds/sources.go`
Remove all 38 commented-out feed sources.

### 4.6 Add -race to CI

**Makefile:**
```makefile
test:
	go test ./...

test-race:
	CGO_ENABLED=1 go test -race ./...

lint:
	golangci-lint run
```

---

## Implementation Schedule (6 Weeks)

```
Week 0 (Prep) - 3-4 days:
  ├── Day 1: 0.1-0.3 (CGO, pprof, backup)
  ├── Day 2: 0.4-0.5 (smoke test, baseline)
  ├── Day 3: 0.6 (test rollback procedure)
  └── Day 4: 0.7 (verify unsubscribe works)

Week 1:
  Track A (Critical):
  ├── Day 1: 1.1 Worker tracking + test
  ├── Day 2: 1.2 Add Unsubscribe method + test
  ├── Day 3-4: 1.3 Analysis limiting (all 4 spawn points) + test
  └── Day 5: 1.4 Subscriber cleanup

  Track B (Parallel):
  ├── Day 1-2: 1.5 HTTP client consolidation
  ├── Day 3: 1.8 DB connection tuning
  ├── Day 4: 1.9-1.10 DB leak fixes (moved from Phase 2)
  └── Day 5: 4.5 Delete commented code

Week 2:
  Track A:
  ├── Day 1-2: 1.6 Aggregator eviction + test
  ├── Day 3: 1.7 Context cleanup audit
  └── Day 4-5: Run 48h stability test

  Track B:
  ├── Day 1-2: 2.1, 2.2 Config + store error handling
  ├── Day 3: 2.5-2.6 Migration + homedir errors
  └── Day 4-5: 4.4 Extract local constants

  CHECKPOINT (end of Week 2):
  - [ ] go test -race ./... passes
  - [ ] Goroutine count after 48h: baseline ±10
  - [ ] Memory stable (no growth trend)

Week 3:
  ├── Day 1-2: 2.3, 2.4 Error handling (saves, panics)
  ├── Day 3: 3.1-3.2 Schema audit, add tables
  ├── Day 4-5: 3.3-3.4 Port analysis + cache methods

Week 4:
  ├── Day 1: 3.5 Run migration script on test DB
  ├── Day 2: 3.5.1 Data validation
  ├── Day 3: 3.6 Add type conversion functions + tests
  ├── Day 4-5: 3.7 Update imports, run tests

  CHECKPOINT (mid-Week 4):
  - [ ] Single store implementation
  - [ ] All features work
  - [ ] Migration tested on backup DB

Week 5:
  ├── Day 1: 3.8 Archive v0.5 code
  ├── Day 2-3: Integration testing
  ├── Day 4-5: Start 7-day stability run

  CHECKPOINT (end of Week 5 - NEW):
  - [ ] 7-day stability run started
  - [ ] Zero crashes in first 48h
  - [ ] Memory growth < 5MB/day
  - [ ] Database integrity check passes

Week 6:
  ├── Day 1-2: Monitor stability run, fix issues
  ├── Day 3: Final review
  ├── Day 4: Document changes
  └── Day 5: Tag release
```

---

## Success Criteria (MEASURABLE)

### Phase 0 Complete When:
- [ ] `go test -race ./internal/work/...` passes
- [ ] pprof endpoint accessible at localhost:6060
- [ ] Baseline goroutine count recorded
- [ ] Rollback procedure tested and documented
- [ ] Unsubscribe test passes

### Phase 1 Complete When:
- [ ] `go test -race ./...` passes
- [ ] Goroutine count after 1 hour: baseline ±10
- [ ] Goroutine count after 24 hours: baseline ±10
- [ ] No "goroutine leak" warnings in logs
- [ ] Analysis requests return error when queue full (not silent drop)

### Phase 2 Complete When:
- [ ] Corrupted config → clear error message with path
- [ ] Store init failure → app exits with error
- [ ] Failed saves → logged with item IDs

### Phase 3 Complete When:
- [ ] `internal/store/` directory deleted
- [ ] All imports use `internal/model`
- [ ] Migration script runs without error
- [ ] Data validation passes (no orphans, counts match)
- [ ] Type conversion round-trip test passes

### Overall Complete When:
- [ ] 7-day stability run: memory RSS < 500MB
- [ ] 7-day stability run: goroutine count stable (baseline ±10 over any 1-hour window)
- [ ] All tests pass including -race
- [ ] `golangci-lint run` clean

---

## Rollback Procedures

### Phase 1 Rollback
```bash
git revert HEAD~N  # Revert Phase 1 commits
./observer  # Verify starts
```

### Phase 2 Rollback
```bash
git revert HEAD~N
./observer  # Verify starts and loads data
```

### Phase 3 Rollback (CRITICAL)
```bash
# 1. Stop app immediately
pkill observer

# 2. Restore database
cp ~/.observer/observer.db.backup.YYYYMMDD ~/.observer/observer.db

# 3. Revert code
git checkout tags/pre-phase-3

# 4. Rebuild and verify
go build -o observer ./cmd/observer
./observer
```

**Test rollback in Phase 0.6 before starting Phase 3.**

---

## What This Plan Does NOT Do (Deferred)

1. **Remove Aggregator shadow model** - v0.6
2. **Unify Item types completely** - v0.6 (conversion functions are interim)
3. **Full v0.5 architecture adoption** - Archived as reference
4. **Security hardening** - Separate plan
5. **Performance optimization** - After stability

---

## Review Sign-Off

- [x] Phase 0 includes rollback testing
- [x] Phase 0 includes Unsubscribe verification
- [x] Unsubscribe method implementation added
- [x] All 4 analysis goroutine spawn points listed
- [x] SaveItems keeps `(int, error)` signature
- [x] Analyses schema uses INTEGER AUTOINCREMENT
- [x] Migration script adds both tables
- [x] Type conversion strategy documented
- [x] Week 5 checkpoint added
- [x] Goroutine stability clarified (±10 over 1-hour window)

**Document Version:** 2.1
**Ready for Implementation:** YES
