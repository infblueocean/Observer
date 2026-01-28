# Observer Master Fix Plan v2.0

**Created:** 2026-01-28
**Status:** REVISED after adversarial review
**Iteration:** 2
**Reviewer Feedback:** 6 senior engineers, consolidated

---

## Executive Summary - Changes from v1.0

Based on adversarial review, v2.0 makes significant changes:

1. **Removed Phase 1.3** - The "data race" doesn't exist (callers get copies)
2. **Restructured Phase 1.4** - Per-instance semaphore instead of global
3. **Fixed Phase 2 errors** - Config error handling, SQLite migration approach
4. **Completely rewrote Phase 3** - Focus on Store consolidation, not architecture rewrite
5. **Reorganized Phase 4** - Tests FIRST, not last; dropped bad extraction ideas
6. **Added Phase 0** - Preparation (enable -race, add monitoring)
7. **Parallel execution tracks** - 6 weeks → achievable timeline
8. **Added rollback procedures** - Every phase has abort criteria

---

## Phase 0: Preparation (MUST COMPLETE FIRST)

**Goal:** Enable tooling needed to verify fixes work.

**Timeline:** 2-3 days before Phase 1 starts

### 0.1 Enable Race Detection

**Problem:** `go test -race ./...` fails with "requires cgo"

**Fix:**
```bash
# Verify CGO is available
export CGO_ENABLED=1
go test -race ./internal/work/... # Should work now
```

**If CGO not available:** Install gcc/clang on the system.

### 0.2 Add Observability Endpoints

**File:** Create `internal/debug/debug.go`
```go
package debug

import (
    "net/http"
    _ "net/http/pprof"
    "runtime"
)

func StartDebugServer(addr string) {
    go func() {
        http.HandleFunc("/debug/goroutines", func(w http.ResponseWriter, r *http.Request) {
            fmt.Fprintf(w, "goroutines: %d\n", runtime.NumGoroutine())
        })
        http.ListenAndServe(addr, nil)
    }()
}
```

**Call from main.go:** `debug.StartDebugServer("localhost:6060")`

### 0.3 Take Database Backup

```bash
cp ~/.observer/observer.db ~/.observer/observer.db.pre-fix-$(date +%Y%m%d)
```

### 0.4 Create Smoke Test

**File:** `internal/app/smoke_test.go`
```go
func TestAppStartsAndStops(t *testing.T) {
    // Create temp config and DB
    // Initialize app
    // Verify no panic
    // Call saveAndClose
    // Verify clean shutdown
}
```

### 0.5 Baseline Metrics

Before any fixes, record:
- `runtime.NumGoroutine()` at startup
- Memory usage after 10 minutes with feeds loaded
- HTTP connections open (via pprof)

---

## Phase 1: Critical Safety Fixes

**Goal:** Stop goroutine leaks, memory leaks, connection leaks.

**Timeline:** Week 1-2 (parallel tracks)

### Track A: Goroutine & Concurrency (Week 1)

#### 1.1 Worker Goroutine Tracking (CRITICAL)

**File:** `internal/work/pool.go:282-283`

**Current:**
```go
go p.execute(item)
```

**Fix:**
```go
p.wg.Add(1)
go func(it *Item) {
    defer p.wg.Done()
    p.execute(it)
}(item)
```

**Test:** `pool_shutdown_test.go`
```go
func TestStopWaitsForWorkers(t *testing.T) {
    pool := NewPool(2)
    pool.Start()

    var completed atomic.Int32
    for i := 0; i < 10; i++ {
        pool.SubmitFunc(TypeFetch, "test", func() (string, error) {
            time.Sleep(100 * time.Millisecond)
            completed.Add(1)
            return "", nil
        })
    }

    time.Sleep(50 * time.Millisecond) // Let some start
    pool.Stop() // Should wait for all

    if completed.Load() != 10 {
        t.Errorf("expected 10 completed, got %d", completed.Load())
    }
}
```

**Rollback:** Revert commit if tests fail.

#### 1.2 Analysis Goroutine Limiting (CRITICAL)

**File:** `internal/brain/trust.go`

**Problem:** 5 separate `go func()` calls with no limit:
- Line ~283: cloud analysis
- Line ~318: local analysis
- Line ~588: random provider analysis
- Line ~766: zinger generation
- Plus streaming goroutines in http_provider.go

**Fix:** Add semaphore to Analyzer struct (NOT global):
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

// Before EVERY go func() that does analysis work:
select {
case a.analysisSem <- struct{}{}:
    go func() {
        defer func() { <-a.analysisSem }()
        // ... analysis work
    }()
case <-ctx.Done():
    return // Context canceled
default:
    logging.Warn("Analysis queue full, dropping request")
    return
}
```

**Test:** `trust_concurrency_test.go`
```go
func TestAnalysisRateLimiting(t *testing.T) {
    analyzer := NewAnalyzer(mockProvider)

    // Spam 100 analysis requests
    for i := 0; i < 100; i++ {
        go analyzer.Analyze(context.Background(), &feeds.Item{ID: fmt.Sprintf("item%d", i)})
    }

    time.Sleep(100 * time.Millisecond)

    // Should never exceed 4 concurrent
    if runtime.NumGoroutine() > startGoroutines+10 {
        t.Errorf("goroutine leak: started with %d, now %d", startGoroutines, runtime.NumGoroutine())
    }
}
```

#### 1.3 Subscriber Cleanup (HIGH)

**File:** `internal/app/app.go`

**In saveAndClose() (around line 1168), add:**
```go
func (m Model) saveAndClose() {
    // FIRST: Stop receiving events
    if m.workPool != nil && m.workEventChan != nil {
        m.workPool.Unsubscribe(m.workEventChan)
    }

    // ... rest of cleanup
}
```

**Also add defer in New() as backup:**
```go
// Near end of New(), after workEventChan is assigned:
// Note: tea.Quit should call saveAndClose, but be defensive
```

### Track B: Resource Leaks (Week 1-2)

#### 1.4 HTTP Client Consolidation (HIGH)

**File:** `internal/brain/http_provider.go`

**Current (line ~58 and ~146):** Creates new clients per request type.

**Fix:**
```go
type HTTPProvider struct {
    config          *ProviderConfig
    client          *http.Client // For regular requests
    streamingClient *http.Client // For streaming (no timeout)
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
            // No timeout for streaming
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

**In GenerateStream(), change:**
```go
// OLD:
client := &http.Client{}

// NEW:
client := p.streamingClient
```

#### 1.5 Aggregator Memory Cap (MEDIUM)

**File:** `internal/feeds/aggregator.go`

**Fix with EFFICIENT eviction:**
```go
const (
    maxItemAge = 7 * 24 * time.Hour
    maxItems   = 50000
    evictionInterval = 1000 // Only evict every N merges
)

type Aggregator struct {
    // existing fields...
    mergesSinceEviction int
}

func (a *Aggregator) MergeItems(newItems []Item) int {
    a.mu.Lock()
    defer a.mu.Unlock()

    // Only evict periodically, not every merge
    a.mergesSinceEviction++
    if a.mergesSinceEviction >= evictionInterval || len(a.items) > maxItems-1000 {
        a.evictOldItems()
        a.mergesSinceEviction = 0
    }

    // ... rest of merge logic
}

func (a *Aggregator) evictOldItems() {
    cutoff := time.Now().Add(-maxItemAge)

    // Pre-allocate new slice (don't reuse backing array)
    newItems := make([]Item, 0, len(a.items)/2)
    for _, item := range a.items {
        if item.PublishedAt.After(cutoff) {
            newItems = append(newItems, item)
        }
    }

    // Cap if still too many
    if len(newItems) > maxItems {
        newItems = newItems[len(newItems)-maxItems:]
    }

    a.items = newItems // Old backing array now eligible for GC
}
```

#### 1.6 Context Cleanup Audit

**Grep for all context creation:**
```bash
grep -rn "context.WithTimeout\|context.WithCancel" internal/
```

**For each occurrence, verify:**
1. Has `defer cancel()` immediately after, OR
2. Has explicit `cancel()` in ALL code paths (including error paths)

**Known issues to fix:**
- `app.go:2290` - Add defer, remove explicit cancel
- `app.go:2014` - Use app's lifecycle context, not Background()

#### 1.7 Database Connection Tuning (NEW)

**File:** `internal/model/store.go` (in New or after Open)

**Add:**
```go
db.SetMaxOpenConns(25)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
```

---

## Phase 2: Error Handling Hardening

**Timeline:** Week 2-3 (parallel with Phase 1 Track B)

### 2.1 Config Load - Distinguish Missing vs Corrupted

**File:** `internal/app/app.go:125`

**Fix:**
```go
cfg, err := config.Load()
if err != nil {
    if os.IsNotExist(err) {
        logging.Info("No config found, creating defaults")
        cfg = config.DefaultConfig()
    } else {
        // Corrupted config - fail with clear message
        return Model{}, fmt.Errorf("config corrupted: %w\nDelete %s or fix JSON syntax", err, config.Path())
    }
}
```

### 2.2 Store Init - Fail Fast (KEEP from v1)

Already correct - return error from New().

### 2.3 Track Failed Item Saves - Return Failed Items

**File:** `internal/model/store.go` SaveItems method

**Change signature:**
```go
func (s *Store) SaveItems(items []model.Item) (saved int, failed []model.Item, err error)
```

**Implementation:**
```go
var failedItems []model.Item
for _, item := range items {
    _, err := stmt.Exec(...)
    if err != nil {
        logging.Error("Failed to save item", "id", item.ID, "error", err)
        failedItems = append(failedItems, item)
        continue
    }
    saved++
}

if err := tx.Commit(); err != nil {
    return 0, nil, fmt.Errorf("commit failed: %w", err)
}

if len(failedItems) > 0 {
    return saved, failedItems, fmt.Errorf("%d items failed to save", len(failedItems))
}
return saved, nil, nil
```

### 2.4 Panic Recovery Stack Traces (KEEP from v1)

**File:** `internal/work/pool.go:289-295`

Use 8KB buffer, include work item details:
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

### 2.5 Migration - Better Error Messages (REVISED)

**Note:** SQLite DDL auto-commits, so transaction doesn't help.

**File:** `internal/model/store.go`

**Fix:**
```go
func (s *Store) migrate() error {
    // Verify connection
    if err := s.db.Ping(); err != nil {
        return fmt.Errorf("database connection failed: %w", err)
    }

    // Run schema (each statement auto-commits in SQLite)
    if _, err := s.db.Exec(schema); err != nil {
        return fmt.Errorf("schema creation failed: %w\nDatabase may need to be deleted", err)
    }

    // Column migrations with existence check
    _, err := s.db.Exec("ALTER TABLE items ADD COLUMN embedding BLOB")
    if err != nil && !strings.Contains(err.Error(), "duplicate column") {
        return fmt.Errorf("embedding column migration failed: %w", err)
    }

    return nil
}
```

### 2.6 HomeDir Error (KEEP from v1)

Return error if `os.UserHomeDir()` fails.

### 2.7 DB Connection Leak in Old Store (NEW)

**File:** `internal/store/sqlite.go:26-29`

**Fix:**
```go
if err := s.migrate(); err != nil {
    db.Close() // ADD THIS LINE
    return nil, fmt.Errorf("migration failed: %w", err)
}
```

### 2.8 Rows.Err() Check (NEW)

**File:** `internal/store/sqlite.go` scanItems function

**After the for loop:**
```go
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("error iterating rows: %w", err)
}
```

---

## Phase 3: Store Consolidation (REVISED)

**Timeline:** Week 3-4

**Goal:** Single store with full feature parity. NOT a full architecture rewrite.

### 3.1 Schema Audit

**Run diff on both stores to identify differences:**

| Feature | store/sqlite.go | model/store.go |
|---------|----------------|----------------|
| items table | ✅ | ✅ |
| analyses table | ✅ | ❌ MISSING |
| top_stories_cache | ✅ | ❌ MISSING |
| embedding column | ❌ | ✅ |
| Session tracking | ❌ | ✅ |

### 3.2 Add Missing Tables to model/store.go

**Add to schema:**
```sql
CREATE TABLE IF NOT EXISTS analyses (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt TEXT NOT NULL,
    raw_response TEXT,
    content TEXT,
    error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

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
```

### 3.3 Port Analysis Methods

Copy from `store/sqlite.go` to `model/store.go`:
- `SaveAnalysis()`
- `GetAnalysisContent()`

### 3.4 Port Top Stories Cache Methods

Copy from `store/sqlite.go` to `model/store.go`:
- `SaveTopStoriesCache()`
- `LoadTopStoriesCache()`

### 3.5 Migration Script for Existing DBs

**File:** `scripts/migrate-store.sh`
```bash
#!/bin/bash
DB_PATH="${HOME}/.observer/observer.db"
BACKUP_PATH="${DB_PATH}.backup.$(date +%Y%m%d%H%M%S)"

echo "Backing up to $BACKUP_PATH"
cp "$DB_PATH" "$BACKUP_PATH"

echo "Adding missing columns/tables..."
sqlite3 "$DB_PATH" <<EOF
-- Add embedding column if missing
ALTER TABLE items ADD COLUMN embedding BLOB;

-- Add session table if missing
CREATE TABLE IF NOT EXISTS sessions (...);
EOF

echo "Migration complete"
```

### 3.6 Update Imports (Week 4)

**After feature parity verified:**
1. Update `internal/app/app.go` to import `internal/model` instead of `internal/store`
2. Handle type conversion (`feeds.Item` ↔ `model.Item`)
3. Run full test suite
4. Delete `internal/store/` directory

### 3.7 Archive v0.5 Code

**Create:** `archive/v0.5/README.md`
```markdown
# v0.5 Architecture Reference (FROZEN)

This code is preserved for reference only. Do NOT build or run.

The v0.5 architecture explored:
- Clean MVC separation
- Filter pipelines
- Controller orchestration

Patterns adopted in main codebase:
- model.Store (embedding support)
- Filter pipeline concept

This code is NOT maintained. For active development, use cmd/observer/.
```

Move `cmd/observer-v05/` to `archive/v0.5/cmd/`

---

## Phase 4: Testing & Quality (Parallel Throughout)

**Timeline:** Runs parallel to Phases 1-3

### 4.1 Write Tests FOR THE FIXES (Priority 1)

These tests verify our fixes work:

| Fix | Test File | What to Test |
|-----|-----------|--------------|
| 1.1 Worker tracking | `pool_shutdown_test.go` | Stop() waits for workers |
| 1.2 Analysis limiting | `trust_concurrency_test.go` | Goroutine count bounded |
| 1.5 Aggregator cap | `aggregator_eviction_test.go` | Items capped at 50k |
| 2.3 Save tracking | `store_save_test.go` | Failed items returned |

### 4.2 Add Race Detection Tests (Priority 1)

```go
// Run with: go test -race ./...

func TestConcurrentEmbedding(t *testing.T) {
    dedup := NewDedupIndex(mockEmbedder, 0.85)

    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            items := []model.Item{{ID: fmt.Sprintf("item%d", id)}}
            dedup.IndexModelBatch(context.Background(), items)
        }(i)
    }
    wg.Wait()
}
```

### 4.3 Smoke Test (Priority 1)

```go
func TestAppLifecycle(t *testing.T) {
    // Create temp dir
    tmpDir := t.TempDir()

    // Initialize app with test config
    app, err := New(WithConfigDir(tmpDir))
    if err != nil {
        t.Fatalf("failed to create app: %v", err)
    }

    // Verify it starts without panic
    // Send a few messages
    // Call shutdown
    app.saveAndClose()

    // Verify clean shutdown
}
```

### 4.4 Constants Extraction (REVISED)

**Don't create global constants package.**

**Instead:** Extract constants LOCAL to each package where they appear > 3 times.

Example for `internal/app/app.go`:
```go
// At top of file, not in separate package
const (
    errorDisplayDuration = 10 * time.Second
    aiPanelMaxLines      = 12
    aiPanelMinLines      = 6
)
```

### 4.5 Delete Commented Code

**File:** `internal/feeds/sources.go`

Delete all 38 commented-out feed sources. Git history preserves them.

### 4.6 Add -race to CI

**Add to Makefile:**
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
Week 0 (Prep):
  ├── Day 1: Enable CGO, verify -race works
  ├── Day 2: Add pprof endpoint, baseline metrics
  └── Day 3: Take DB backup, create smoke test

Week 1:
  Track A (Critical):
  ├── Day 1-2: 1.1 Worker tracking + test
  ├── Day 3-4: 1.2 Analysis limiting + test
  └── Day 5: 1.3 Subscriber cleanup

  Track B (Parallel):
  ├── Day 1-2: 1.4 HTTP client consolidation
  ├── Day 3: 1.7 DB connection tuning
  └── Day 4-5: 4.5 Delete commented code

Week 2:
  Track A:
  ├── Day 1-2: 1.5 Aggregator eviction + test
  ├── Day 3: 1.6 Context cleanup audit
  └── Day 4-5: Run 48h stability test

  Track B:
  ├── Day 1-3: 2.1, 2.2, 2.6 Error handling (low risk)
  └── Day 4-5: 2.7, 2.8 DB leak fixes

  CHECKPOINT: -race passes, goroutines stable, memory stable 48h

Week 3:
  Track A:
  ├── Day 1-2: 2.3, 2.4, 2.5 Error handling (data paths)
  └── Day 3-5: 3.1, 3.2 Schema audit and additions

  Track B:
  ├── Day 1-3: 4.1, 4.2 Write tests for fixes
  └── Day 4-5: 4.4 Extract local constants

Week 4:
  ├── Day 1-2: 3.3, 3.4 Port analysis + cache methods
  ├── Day 3: 3.5 Test migration script
  ├── Day 4: 3.6 Update imports, run tests
  └── Day 5: 3.7 Archive v0.5 code

  CHECKPOINT: Single store, all features work, no data loss

Week 5:
  ├── Day 1-3: Integration testing
  ├── Day 4-5: 7-day stability run

Week 6:
  ├── Day 1-2: Fix any issues from stability run
  ├── Day 3: Final review
  ├── Day 4: Document changes
  └── Day 5: Tag release
```

---

## Success Criteria (MEASURABLE)

### Phase 1 Complete When:
- [ ] `go test -race ./internal/work/...` passes
- [ ] `runtime.NumGoroutine()` after 1 hour < startup + 20
- [ ] No "goroutine leak" log messages
- [ ] pprof shows no unbounded growth in any metric

### Phase 2 Complete When:
- [ ] Config corruption → clear error message (not silent fallback)
- [ ] Store init failure → app exits with error (not nil store)
- [ ] SaveItems returns list of failed items (not silent drop)

### Phase 3 Complete When:
- [ ] `internal/store/` directory deleted
- [ ] All imports use `internal/model`
- [ ] Existing databases load without data loss
- [ ] Analysis and top stories cache work as before

### Overall Complete When:
- [ ] 7-day stability run: memory < 500MB RSS
- [ ] 7-day stability run: goroutine count stable (±10)
- [ ] All Phase 1-3 tests pass
- [ ] `go test -race ./...` passes
- [ ] `golangci-lint run` clean (or documented exceptions)

---

## Rollback Procedures

### Phase 1 Rollback
```bash
# If goroutine issues
git revert HEAD~N  # Revert Phase 1 commits
# Restart app, verify stable
```

### Phase 2 Rollback
```bash
# If error handling breaks things
git revert HEAD~N
# Verify app starts and loads data
```

### Phase 3 Rollback
```bash
# CRITICAL: Database changes
# 1. Stop app
# 2. Restore database backup
cp ~/.observer/observer.db.pre-fix-* ~/.observer/observer.db
# 3. Revert code
git checkout pre-phase-3-tag
# 4. Restart app
```

---

## What This Plan Does NOT Do (Deferred)

1. **Remove Aggregator shadow model** - Requires scheduler refactor (v0.6)
2. **Unify Item types** (feeds.Item vs model.Item) - Separate effort
3. **Full v0.5 architecture adoption** - Too risky mid-flight
4. **Security hardening** - Important but separate plan needed
5. **Performance optimization** - Profile first, fix after stability

---

## Review Sign-Off

Before implementation:
- [ ] Phase 0 tooling verified working
- [ ] Database backup taken
- [ ] Smoke test written and passes
- [ ] Team agrees on 6-week timeline
- [ ] Rollback procedures tested

**Document Version:** 2.0
**Changes from v1.0:** 47 items revised based on 6 adversarial reviews
