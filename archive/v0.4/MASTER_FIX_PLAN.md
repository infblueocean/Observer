# Observer Master Fix Plan v1.0

**Created:** 2026-01-28
**Status:** DRAFT - Pending Review
**Iteration:** 1

## Executive Summary

This plan addresses 200+ issues identified by adversarial code review across 6 categories:
- Architecture & Design
- Error Handling
- Concurrency & Race Conditions
- Resource Management
- Testing
- Code Quality

The plan is organized into 4 phases, each building on the previous. Each phase should be completed and reviewed before proceeding.

---

## Phase 1: Critical Safety Fixes (Memory, Goroutines, Data Races)

**Goal:** Stop the bleeding. Fix issues that cause crashes, data loss, or resource exhaustion.

**Estimated scope:** ~15 files, ~200 lines changed

### 1.1 Worker Goroutine Tracking (CRITICAL)

**File:** `internal/work/pool.go`
**Problem:** Worker goroutines not tracked by WaitGroup, so `Stop()` doesn't wait for them.
**Risk:** Data corruption on shutdown, incomplete database writes.

**Fix:**
```go
// In dispatchPending(), change:
go p.execute(item)

// To:
p.wg.Add(1)
go func(it *Item) {
    defer p.wg.Done()
    p.execute(it)
}(item)
```

**Verification:** Add test that calls `Stop()` during active work and verifies all work completes.

### 1.2 Streaming HTTP Client Leak (CRITICAL)

**File:** `internal/brain/http_provider.go`
**Problem:** New `http.Client{}` created per stream call, leaking connection pools.
**Risk:** Socket exhaustion after ~100 streaming requests.

**Fix:**
```go
type HTTPProvider struct {
    client          *http.Client // Regular requests (with timeout)
    streamingClient *http.Client // Streaming (no timeout, but with idle conn settings)
}

func NewHTTPProvider(...) *HTTPProvider {
    return &HTTPProvider{
        client: &http.Client{Timeout: 2 * time.Minute},
        streamingClient: &http.Client{
            Transport: &http.Transport{
                MaxIdleConns:        10,
                MaxIdleConnsPerHost: 2,
                IdleConnTimeout:     90 * time.Second,
            },
        },
    }
}
```

**Verification:** Monitor `netstat` during extended streaming sessions.

### 1.3 Data Race in Embedding Dedup (CRITICAL)

**File:** `internal/embedding/dedup.go`
**Problem:** `item.Embedding = vec32` modifies caller's slice while they may be reading it.
**Risk:** Silent data corruption, undefined behavior.

**Fix Option A:** Return new slice with embeddings (preferred - no mutation):
```go
func (d *DedupIndex) IndexModelBatch(ctx context.Context, items []model.Item) ([]model.Item, error) {
    result := make([]model.Item, len(items))
    copy(result, items)
    // Modify result[i].Embedding instead of items[i].Embedding
    return result, nil
}
```

**Fix Option B:** Document ownership transfer and require callers not to read during/after call.

**Verification:** Run with `-race` flag, add concurrent access test.

### 1.4 Unbounded Analysis Goroutines (CRITICAL)

**File:** `internal/brain/trust.go`
**Problem:** `go func()` spawned for every analysis with no limit.
**Risk:** OOM, Ollama lockup under rapid requests.

**Fix:** Route through work pool or add semaphore:
```go
var analysisSem = make(chan struct{}, 4) // Max 4 concurrent analyses

func (a *Analyzer) analyzeInternal(...) {
    select {
    case analysisSem <- struct{}{}:
        defer func() { <-analysisSem }()
    case <-ctx.Done():
        return
    }
    // ... existing analysis code
}
```

**Verification:** Spam analysis requests, verify goroutine count stays bounded.

### 1.5 Aggregator Unbounded Growth (HIGH)

**File:** `internal/feeds/aggregator.go`
**Problem:** `items` slice grows forever with no eviction.
**Risk:** Memory exhaustion over days/weeks.

**Fix:** Add time-based eviction in `MergeItems()`:
```go
const maxItemAge = 7 * 24 * time.Hour
const maxItems = 50000

func (a *Aggregator) MergeItems(newItems []Item) int {
    a.mu.Lock()
    defer a.mu.Unlock()

    // Evict old items first
    cutoff := time.Now().Add(-maxItemAge)
    filtered := a.items[:0]
    for _, item := range a.items {
        if item.PublishedAt.After(cutoff) {
            filtered = append(filtered, item)
        }
    }
    a.items = filtered

    // Cap total count
    if len(a.items) > maxItems {
        a.items = a.items[len(a.items)-maxItems:]
    }

    // ... rest of merge logic
}
```

**Verification:** Add test that inserts 100k items, verifies cap is enforced.

### 1.6 Work Pool Subscriber Cleanup (HIGH)

**File:** `internal/work/pool.go` and `internal/app/app.go`
**Problem:** Subscribers never unsubscribed, channels leak.

**Fix in pool.go:** Already has `Unsubscribe()`, just needs to be called.

**Fix in app.go `saveAndClose()`:**
```go
func (m Model) saveAndClose() {
    // Add near the top:
    if m.workPool != nil && m.workEventChan != nil {
        m.workPool.Unsubscribe(m.workEventChan)
    }
    // ... rest of cleanup
}
```

**Verification:** Check subscriber count before/after app lifecycle.

### 1.7 Context Cancellation Cleanup (MEDIUM)

**Files:** Multiple (grep for `context.WithTimeout` without `defer cancel()`)
**Problem:** Context timers leak when cancel not deferred.

**Fix:** Add `defer cancel()` after every context creation:
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()  // ADD THIS LINE
```

**Files to check:**
- `internal/app/app.go` (lines ~2290, ~2020)
- `internal/brain/trust.go`
- `internal/rerank/ollama.go`

**Verification:** Run with GODEBUG=gctrace=1, check for timer leaks.

---

## Phase 2: Error Handling Hardening

**Goal:** Make errors visible, actionable, and recoverable.

**Estimated scope:** ~20 files, ~300 lines changed

### 2.1 Don't Swallow Config Load Errors

**File:** `internal/app/app.go:125`
**Current:** `cfg, _ := config.Load()`
**Fix:**
```go
cfg, err := config.Load()
if err != nil {
    logging.Warn("Config load failed, using defaults", "error", err)
    cfg = config.DefaultConfig()
}
```

### 2.2 Fail Fast on Critical Store Initialization

**File:** `internal/app/app.go:142-148`
**Current:** Sets `st = nil` and continues.
**Fix:** Return error or show persistent UI warning:
```go
st, err := store.New(dbPath)
if err != nil {
    return Model{}, fmt.Errorf("database initialization failed: %w", err)
}
```

### 2.3 Track and Report Failed Item Saves

**File:** `internal/store/sqlite.go` (or `model/store.go`)
**Current:** `continue` on insert failure, silent data loss.
**Fix:**
```go
var failedCount int
for _, item := range items {
    _, err := stmt.Exec(...)
    if err != nil {
        logging.Error("Failed to save item", "id", item.ID, "error", err)
        failedCount++
        continue
    }
}
if failedCount > 0 {
    logging.Warn("Some items failed to save", "failed", failedCount, "total", len(items))
}
```

### 2.4 Add Stack Traces to Panic Recovery

**File:** `internal/work/pool.go:289-295`
**Fix:**
```go
defer func() {
    if r := recover(); r != nil {
        buf := make([]byte, 4096)
        n := runtime.Stack(buf, false)
        logging.Error("Work panicked",
            "id", item.ID,
            "panic", r,
            "stack", string(buf[:n]))
        p.complete(item, "", fmt.Errorf("panic: %v", r))
    }
}()
```

### 2.5 Wrap Migration in Transaction

**File:** `internal/store/sqlite.go:35-108` (or model/store.go)
**Fix:**
```go
func (s *Store) migrate() error {
    tx, err := s.db.Begin()
    if err != nil {
        return fmt.Errorf("failed to start migration: %w", err)
    }
    defer tx.Rollback()

    _, err = tx.Exec(schema)
    if err != nil {
        return fmt.Errorf("migration failed: %w", err)
    }

    return tx.Commit()
}
```

### 2.6 HomeDir Error Handling

**File:** `internal/app/app.go:138`
**Current:** `homeDir, _ := os.UserHomeDir()`
**Fix:**
```go
homeDir, err := os.UserHomeDir()
if err != nil {
    return Model{}, fmt.Errorf("cannot determine home directory: %w", err)
}
```

### 2.7 Standardize Error Wrapping

**Convention:** Always use `%w` for error wrapping, never `%v`:
```go
// Good
return fmt.Errorf("failed to fetch %s: %w", url, err)

// Bad
return fmt.Errorf("failed to fetch %s: %v", url, err)
```

Create a grep pattern to find violations: `fmt.Errorf.*%v.*err`

---

## Phase 3: Architecture Consolidation

**Goal:** Eliminate duplicate code paths, establish single source of truth.

**Estimated scope:** ~10 files deleted, ~500 lines refactored

### 3.1 Consolidate to Single Store

**Decision:** Keep `internal/model/store.go`, delete `internal/store/sqlite.go`

**Steps:**
1. Audit all imports of `internal/store`
2. Update to use `internal/model`
3. Delete `internal/store/` directory

**Files affected:**
- `internal/app/app.go` - update imports
- Any other files importing old store

### 3.2 Remove Aggregator Shadow Model (Deferred to v0.6)

**Rationale:** This is a large architectural change. The Aggregator is deeply integrated with:
- Queue manager
- Stream view
- Correlation engine

**Short-term fix:** Add eviction (done in 1.5) to prevent unbounded growth.

**Long-term plan (v0.6):**
1. Make Store the single source of truth
2. Aggregator becomes a cache/index layer only
3. Views query through Controller filter pipelines

### 3.3 Consolidate Fetch Logic

**Current state:** Fetching happens in 3 places.

**Decision:** Keep `app.go:refreshDueSources()` for now (it works), document as authoritative.

**Add comment:**
```go
// refreshDueSources is the SINGLE entry point for feed fetching.
// Do not add fetch logic elsewhere. See MASTER_FIX_PLAN.md Phase 3.3.
```

### 3.4 Clean Up v0.5 Experimental Code

**Decision:** The v0.5 code in `internal/{model,controller,view}` and `cmd/observer-v05/` is experimental.

**Options:**
- A) Port current app to v0.5 architecture (large effort, risky)
- B) Delete v0.5 code and continue with current architecture (loses good patterns)
- C) Keep v0.5 as reference, incrementally adopt patterns (recommended)

**Recommended approach (C):**
1. Keep v0.5 code as reference
2. Adopt filter pipeline pattern from `internal/controller/pipeline.go`
3. Adopt Store interface from `internal/model/store.go`
4. Do NOT attempt full rewrite mid-project

---

## Phase 4: Code Quality & Testing

**Goal:** Improve maintainability, add critical test coverage.

**Estimated scope:** ~30 files, ~1000 lines changed/added

### 4.1 Extract Magic Numbers to Constants

**Create new file:** `internal/constants/constants.go`
```go
package constants

import "time"

// UI Timing
const (
    ErrorDisplayDuration    = 10 * time.Second
    TopStoriesRefreshInterval = 30 * time.Second
    BreakingNewsThreshold   = 30 * time.Minute
    FreshIndicatorThreshold = 10 * time.Minute
)

// Time Bands
const (
    TimeBandJustNow   = 15 * time.Minute
    TimeBandPastHour  = 1 * time.Hour
    TimeBandToday     = 24 * time.Hour
    TimeBandYesterday = 48 * time.Hour
)

// Limits
const (
    MinItemsForTopStories = 10
    MaxItemsInMemory      = 50000
    MaxItemAge            = 7 * 24 * time.Hour
    WorkPoolHistorySize   = 100
    MaxBatchSize          = 200
)

// AI Panel
const (
    AIPanelHeightRatio = 3
    AIPanelMaxLines    = 12
    AIPanelMinLines    = 6
)
```

### 4.2 Extract Long Functions

**Target functions (>100 lines):**

| File | Function | Lines | Extract To |
|------|----------|-------|------------|
| app.go | `New()` | 263 | `initStore()`, `initSources()`, `initAI()`, `initCorrelation()` |
| app.go | `Update()` | 282 | One handler per message type |
| app.go | `View()` | 130 | `renderHeader()`, `renderStatus()`, `renderErrors()` |
| stream/model.go | `renderSelectedItem()` | 188 | `renderTitle()`, `renderSummary()`, `renderMetadata()` |

**Pattern for Update():**
```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        return m.handleWindowSize(msg)
    case tea.KeyMsg:
        return m.handleKey(msg)
    case tea.MouseMsg:
        return m.handleMouse(msg)
    case ItemsLoadedMsg:
        return m.handleItemsLoaded(msg)
    // ... etc
    }
}
```

### 4.3 Delete Commented-Out Code

**File:** `internal/feeds/sources.go`
**Action:** Remove all 38 commented-out feed sources.
**Rationale:** Git history preserves them if needed.

### 4.4 Resolve or Delete TODOs

| File | Line | TODO | Action |
|------|------|------|--------|
| correlation/engine.go | 187 | Implement pruning | Create issue #XX |
| correlation/engine.go | 289 | Implement disagreement | Create issue #XX |
| correlation/engine.go | 373 | Implement disagreement | Delete (duplicate) |
| curation/alerts.go | 306 | Implement probability | Create issue #XX |

### 4.5 Add Critical Tests

**Priority 1 - Must Have:**
```
internal/app/app_test.go           - Test initialization, basic lifecycle
internal/config/config_test.go     - Test load, save, defaults, migration
internal/work/pool_race_test.go    - Test with -race flag
```

**Priority 2 - Should Have:**
```
internal/curation/filter_test.go   - Test pattern matching, edge cases
internal/embedding/dedup_race_test.go - Concurrent access tests
```

**Priority 3 - Nice to Have:**
```
internal/app/integration_test.go   - End-to-end lifecycle test
```

### 4.6 Add Race Detection to CI

**Add to Makefile or CI:**
```bash
test-race:
    go test -race ./...
```

---

## Implementation Order

```
Week 1: Phase 1 (Critical Safety)
  ├── Day 1-2: Items 1.1-1.4 (goroutines, HTTP, race, analysis)
  ├── Day 3: Items 1.5-1.6 (aggregator cap, subscriber cleanup)
  └── Day 4-5: Item 1.7 (context cleanup) + Testing + Review

Week 2: Phase 2 (Error Handling)
  ├── Day 1-2: Items 2.1-2.4 (config, store, saves, panics)
  ├── Day 3: Items 2.5-2.6 (migration, homedir)
  └── Day 4-5: Item 2.7 (standardization) + Testing + Review

Week 3: Phase 3 (Architecture)
  ├── Day 1-2: Item 3.1 (consolidate stores)
  ├── Day 3: Items 3.3-3.4 (document, clean up)
  └── Day 4-5: Testing + Review

Week 4: Phase 4 (Quality)
  ├── Day 1-2: Items 4.1-4.2 (constants, extract functions)
  ├── Day 3: Items 4.3-4.4 (delete dead code, TODOs)
  └── Day 4-5: Items 4.5-4.6 (tests, CI) + Final Review
```

---

## Success Criteria

After all phases:
- [ ] `go test -race ./...` passes
- [ ] No goroutine leaks (verified with runtime.NumGoroutine())
- [ ] Memory stable over 24h run (no unbounded growth)
- [ ] All critical paths have test coverage
- [ ] No magic numbers in hot paths
- [ ] No functions > 100 lines
- [ ] Clean `go vet` and `golint` output

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Fixes introduce new bugs | Each phase followed by adversarial review |
| Scope creep | Stick to plan, defer nice-to-haves to v0.6 |
| Breaking existing features | Run full test suite after each change |
| Large refactors destabilize | Small, incremental changes with commits |

---

## Review Checklist

Before implementation, this plan needs sign-off on:
- [ ] Phase 1 fixes are correctly prioritized
- [ ] Phase 2 error handling approach is appropriate
- [ ] Phase 3 architecture decisions are sound
- [ ] Phase 4 scope is realistic
- [ ] Implementation order makes sense
- [ ] Nothing critical is missing

---

**Document Version:** 1.0
**Next Action:** Submit for adversarial review by senior engineers
