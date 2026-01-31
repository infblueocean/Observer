# Search Implementation Plan v3 (Post-Review Round 2)

**Generated:** 2026-01-31
**Source:** [SEARCH.md](./SEARCH.md) — Brain Trust Synthesis
**Review Round 1:** [REVIEW.md](./REVIEW.md) — Adversarial Architecture Review (GPT-5, Grok-4, Gemini-3)
**Review Round 2:** [REVIEW_V2.md](./REVIEW_V2.md) — Second adversarial review of v2 plan

This document replaces the v2 `IMPLEMENTATION.md`. It addresses every issue from both adversarial review rounds, resolves internal contradictions (searchActive leftovers, queryID ordering, interface vs. cmd-factory split, Esc behavior, mode stack loops), and adds missing infrastructure (schema migration via `PRAGMA user_version`, `ensureSnapshot()` helper, two-stage Esc, `replaceMode()` for lateral transitions).

---
## 1. Review Response Summary

### 1.1 Factual Corrections

The reviewers made four factual errors about the existing codebase. These are corrected here for the record:

| # | Reviewer Claim | Actual Code | Citation |
|---|---------------|-------------|----------|
| 1 | "No WAL, no migration strategy" (Grok-4) | WAL mode + `busy_timeout=5000` already enabled on all file-based DBs | `store.go:68-77` — `PRAGMA journal_mode=WAL` and `PRAGMA busy_timeout=5000` |
| 2 | "If your current item upsert uses INSERT OR REPLACE, that deletes and reinserts, changing rowid" (GPT-5) | Items use `INSERT OR IGNORE` — rowid is stable, safe for FTS5 external content | `store.go:143-148` — `INSERT OR IGNORE INTO items (...)` |
| 3 | "Repeatedly using `context.Background()` for long-running operations with no cancellation path" (all 3) | All HTTP calls already use `http.NewRequestWithContext(ctx, ...)` | `embed/jina.go:166`, `embed/ollama.go:58,112`, `rerank/jina.go:111`, `rerank/ollama.go:65,84,208` |
| 4 | "No mutex / thread safety" (implied by write contention concern) | `sync.RWMutex` protects all store operations; `mu.Lock()` for writes, `mu.RLock()` for reads | `store.go:20` — `mu sync.RWMutex`, `store.go:134` — `s.mu.Lock()` |

**Nuance on #3:** While HTTP calls pass `ctx`, the *Bubble Tea command layer* currently creates contexts at invocation time with no per-query cancellation. The reviewers' concern about goroutine leaks is valid at the orchestration level — a new search doesn't cancel the previous search's in-flight API calls. Phase 0c addresses this.

**Nuance on #4:** The RWMutex serializes Go-level access. SQLite WAL + busy_timeout handles DB-level contention. Together they prevent `SQLITE_BUSY` panics. The reviewers' concern about *contention under load* remains valid for batch operations (e.g., backfill + concurrent fetch), but the existing infrastructure is sound.

### 1.2 Valid Concerns Acknowledged

These reviewer concerns are correct and addressed in this plan:

| # | Concern | Reviewer(s) | Where Addressed |
|---|---------|-------------|-----------------|
| 1 | Boolean soup in App struct needs AppMode enum | All 3 | Phase 0a |
| 2 | Keybinding collisions across features | All 3 | Phase 0b |
| 3 | Per-query cancellation for goroutine lifecycle | All 3 | Phase 0c |
| 4 | Feature 1 has 4 conflicting subtask designs | All 3 | Phase 1, Feature 1 (unified) |
| 5 | F5 and F9 are the same database concept | GPT-5 | Phase 2, Feature 5+9 (merged) |
| 6 | Scope is unrealistic for 10 simultaneous features | All 3 | Phased build order (3 phases + reassess) |
| 7 | Closure capture of indices (not IDs) risks panics | Gemini-3 | Phase 0c + all feature implementations |
| 8 | View() performance at 60fps with all features | Grok-4 | Phase 2, Feature 6 (precomputed render) |
| 9 | Mode stack unbounded growth (History/Results loop) | Gemini-3 | Phase 0a — `replaceMode()` helper |
| 10 | Global `?` key breaks text entry in ModeSearch | GPT-5 | Phase 0b — scope `?` to non-text-entry modes |
| 11 | Esc behavior contradiction (exit results vs cancel rerank) | GPT-5 | Phase 0b — two-stage Esc pattern |
| 12 | Interface vs AppConfig abstraction inconsistency | GPT-5 | Phase 0c — AppConfig-only model with `autoReranks bool` |
| 13 | `queryID` ordering bug in `cancelSearch`/`newSearchContext` | GPT-5 | Phase 0c — remove `queryID` clear from `cancelSearch` |
| 14 | `handleMoreLikeThis` missing mode transition to ModeResults | Grok-4 | Phase 0a — explicit `pushMode(ModeResults)` |
| 15 | `cancelSearch` not called on mode transitions | Grok-4 | Phase 0c — cancel in `popMode` when leaving Results/Search |
| 16 | Missing `features Features` field on App struct | Grok-4 | Phase 0a — added to App struct changes |

### 1.3 Cut List

Features permanently cut or deferred based on unanimous or 2/3 reviewer consensus:

| Feature | Verdict | Rationale |
|---------|---------|-----------|
| **F4: Timeline Status Bar** | **CUT** (unanimous) | "Developer debugging info masquerading as a feature" — the `obs events` CLI already serves this need |
| **F8: Filter Chips / Query Language** | **DEFERRED** (unanimous) | Premature for current user base; keybinding collision with Tab |
| **F10d: Search Enrichment / Chunks** | **CUT** (unanimous) | "ROI is negative" — research project, not a feature |
| **F3c: Smooth Reorder / Flash Animation** | **CUT** (unanimous) | "Animations in TUIs are finicky" — snap sort is sufficient |
| **F9c: Auto-Refresh Pinned Searches** | **DEFERRED** (2/3) | "Refresh on view is sufficient" — revisit after pinned searches prove valuable |

---

## 2. Phase 0: Prerequisites

These four changes must land before any feature work. They are tightly coupled and should be implemented as a single PR.

### 2a. AppMode Enum

**Problem:** The current App struct uses boolean flags (`searchActive`, `embeddingPending`, `rerankPending`, `searchPoolPending`, `debugVisible`) to determine input routing. As features add more modes (history browser, article reader, MLT results), boolean combinatorics become unmaintainable.

**Solution:** Add an explicit `AppMode` enum for top-level UI routing. Keep pipeline booleans orthogonal — they track async progress, not input mode.

```go
// AppMode determines which input handler and view layout are active.
type AppMode int

const (
    ModeList    AppMode = iota // chronological feed (default)
    ModeSearch                 // typing in search input
    ModeResults                // viewing search/MLT results
    ModeHistory                // browsing search history (Ctrl-R)
    ModeArticle                // reading full article (Jina Reader)
)
```

**Mode stack for modal modes.** ModeHistory and ModeArticle are "modal-ish" — Esc should return to the *previous* mode (e.g., Search -> History -> Esc should return to Search, not List). A mode stack prevents regressions.

To prevent unbounded stack growth from repeated History->Results->History cycles, we provide three helpers: `pushMode` (for entering a new modal layer), `popMode` (for returning to the previous mode), and `replaceMode` (for lateral transitions that swap the current mode without growing the stack):

```go
type App struct {
    mode      AppMode
    modeStack []AppMode // push current before entering modal; pop on close
    features  Features  // feature flags — see Section 2d
}

func (a *App) pushMode(m AppMode) {
    a.modeStack = append(a.modeStack, a.mode)
    a.mode = m
}

func (a *App) popMode(defaultMode AppMode) {
    // Cancel any in-flight search work when leaving Results or Search.
    if a.mode == ModeResults || a.mode == ModeSearch {
        a.cancelSearch()
    }

    n := len(a.modeStack)
    if n == 0 {
        a.mode = defaultMode
        return
    }
    a.mode = a.modeStack[n-1]
    a.modeStack = a.modeStack[:n-1]
}

// replaceMode swaps the current mode without growing the stack.
// Use this for lateral transitions (e.g., History Enter -> Results)
// where the old mode should not remain on the stack.
func (a *App) replaceMode(m AppMode) {
    a.mode = m
}
```

**Why `replaceMode` is needed:** Consider the flow ModeList -> ModeResults -> ModeHistory -> Enter (re-execute search). If Enter called `pushMode(ModeResults)`, the stack would grow to `[ModeList, ModeResults, ModeHistory]` and Esc from the new results would return to ModeHistory instead of ModeList. Repeated cycles would grow the stack without bound. Instead, Enter in ModeHistory calls `popMode(ModeList)` to remove ModeHistory, then `replaceMode(ModeResults)` to swap into results mode. The stack remains `[ModeList]` regardless of how many History->Results cycles occur.

**Integration with existing booleans:**

```go
// handleKeyMsg routes by mode FIRST, then by key within each mode.
// Global keys handled before mode dispatch.
func (a *App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    if cmd, ok := a.handleGlobalKeys(msg); ok {
        return a, cmd
    }
    switch a.mode {
    case ModeSearch:
        return a.handleSearchKeys(msg)
    case ModeResults:
        return a.handleResultsKeys(msg)
    case ModeHistory:
        return a.handleHistoryKeys(msg)
    case ModeArticle:
        return a.handleArticleKeys(msg)
    default: // ModeList
        return a.handleListKeys(msg)
    }
}

// Global keys extracted to avoid repetition across modes.
// Note: `?` is scoped to non-text-entry modes to avoid breaking
// character input in ModeSearch.
func (a *App) handleGlobalKeys(msg tea.KeyMsg) (tea.Cmd, bool) {
    switch msg.Type {
    case tea.KeyCtrlC:
        a.cancelSearch()
        return tea.Quit, true
    }
    switch msg.String() {
    case "q":
        a.cancelSearch()
        return tea.Quit, true
    case "?":
        if a.mode != ModeSearch {
            a.debugVisible = !a.debugVisible
            return nil, true
        }
    }
    return nil, false
}
```

The pipeline booleans (`embeddingPending`, `rerankPending`, `searchPoolPending`) remain on the App struct. They are checked by message handlers (e.g., `SearchPoolLoaded` checks `embeddingPending` to decide whether to rank immediately or wait), not by `handleKeyMsg`. This separation means:

- `a.mode` = "what keys do" (UI routing)
- `a.embeddingPending` = "what's in flight" (pipeline state)
- `a.statusText` = "what the user sees" (display)

**Migration path:** Replace `searchActive` with `a.mode == ModeSearch`. The `searchActive` field is deleted. Other booleans (`embeddingPending`, etc.) are untouched.

**`handleMoreLikeThis()` mode transition:** When called from ModeList, `handleMoreLikeThis()` must call `pushMode(ModeResults)` to enter results mode. When called from ModeResults (chaining), the mode is already correct — no transition needed. See Feature 1 for the full implementation.

**App struct changes:**

```go
type App struct {
    // ... existing fields ...

    mode      AppMode   // replaces searchActive; default ModeList
    modeStack []AppMode // for modal return (History, Article)
    features  Features  // feature flags gating optional functionality (Phase 0d)

    // DELETE: searchActive bool
    // KEEP: embeddingPending, rerankPending, searchPoolPending (pipeline state)
    // KEEP: debugVisible (overlay, not a mode — renders on top of any mode)
}
```

### 2b. Global Keymap

**Problem:** Features 1-10 each proposed keybindings independently, resulting in 6+ collisions identified by reviewers.

**Solution:** A single definitive keymap, organized by mode. Every key assignment is listed here; no feature may claim a key without updating this table.

#### ModeList (chronological feed)

| Key | Action | Notes |
|-----|--------|-------|
| `j` / `Down` | Move cursor down | |
| `k` / `Up` | Move cursor up | |
| `Enter` | Toggle read status | |
| `/` | Enter search mode | Transitions to ModeSearch |
| `m` | More Like This | Uses current item's embedding; transitions to ModeResults |
| `r` | Refresh sources | Triggers coordinator fetch |
| `x` | Toggle score column | Only visible when embeddings loaded |
| `Ctrl-R` | Open search history | Transitions to ModeHistory; no-op if `SearchHistory` feature is false |
| `?` | Toggle debug overlay | Sets `debugVisible` (overlay, not mode) |
| `q` / `Ctrl-C` | Quit | Handled by global dispatch |

#### ModeSearch (typing query)

| Key | Action | Notes |
|-----|--------|-------|
| *any printable* | Input to filterInput | Standard textinput handling; includes `?` (not intercepted by globals) |
| `Enter` | Submit search | Transitions to ModeResults |
| `Esc` | Cancel search | Clears input; transitions to ModeList |
| `Ctrl-R` | Open search history | Transitions to ModeHistory (preserves input); no-op if `SearchHistory` feature is false |

#### ModeResults (viewing search/MLT results)

| Key | Action | Notes |
|-----|--------|-------|
| `j` / `Down` | Move cursor down | |
| `k` / `Up` | Move cursor up | |
| `Enter` | Toggle read status | Same as ModeList |
| `m` | More Like This (chain) | Pivots to new seed item |
| `R` | Deep Rerank (Ollama) | Manual apply; no-op if `autoReranks` is true |
| `x` | Toggle score column | |
| `o` | Open article | Transitions to ModeArticle (Phase 3) |
| `/` | New search | Transitions to ModeSearch |
| `Esc` | Cancel or exit (two-stage) | See below |
| `Tab` | Next pinned search | Phase 2; no-op if no pinned searches exist |
| `Shift-Tab` | Previous pinned search | Phase 2; see terminal compatibility note below. No-op if no pinned searches exist |
| `H` / `[` | Previous pinned search | Fallback for terminals that don't send `Shift-Tab` |
| `L` / `]` | Next pinned search | Fallback for `Tab` in contexts where it conflicts |
| `Ctrl-R` | Open search history | Transitions to ModeHistory; no-op if `SearchHistory` feature is false |

**Two-stage Esc in ModeResults.** The Esc key has two behaviors depending on whether async work is in flight:

- **First Esc with work in flight** (`rerankPending` or `embeddingPending` is true): Cancel the in-flight work, stay in ModeResults with current results. Set status hint: `"Cancelled -- Esc again to exit"`.
- **Second Esc** (or first Esc with no work in flight): Exit ModeResults, restore `savedItems`, return to ModeList via `popMode(ModeList)`.

This resolves the contradiction between "Esc exits results" and "Esc cancels rerank": cancellation is always the first action, and exiting is always the second.

```go
func (a *App) handleResultsEsc() (tea.Model, tea.Cmd) {
    // Stage 1: cancel in-flight work, stay in results
    if a.embeddingPending || a.rerankPending || a.searchPoolPending {
        if a.searchCancel != nil {
            a.searchCancel()
        }
        a.embeddingPending = false
        a.rerankPending = false
        a.searchPoolPending = false
        a.statusText = "Cancelled -- Esc again to exit"
        return a, nil
    }

    // Stage 2: exit results entirely
    return a.clearSearch()
}
```

**Terminal compatibility note for `Shift-Tab`.** `Shift-Tab` sends `\x1b[Z` (CSI Z) and may not be recognized on all terminals. Bubble Tea parses this as `tea.KeyShiftTab` on supported terminals. Match on `msg.Type == tea.KeyShiftTab`, not `msg.String()`. For terminals that do not support `Shift-Tab`, the `H`/`[` fallback keys provide equivalent functionality.

#### ModeHistory (search history browser)

| Key | Action | Notes |
|-----|--------|-------|
| `j` / `Down` | Move cursor down | |
| `k` / `Up` | Move cursor up | |
| `Enter` | Re-execute selected search | Uses `replaceMode(ModeResults)` — see stack note below |
| `p` | Toggle pin on selected | Pins/unpins search as persistent view |
| `d` | Delete selected entry | Removes from history |
| `Esc` | Close history | Returns to *previous* mode via `popMode(ModeList)` |
| `/` | Filter history | Fuzzy-match within history list |

**Enter in ModeHistory uses `replaceMode`, not `pushMode`.** When the user selects a history entry and presses Enter, the handler calls `popMode(ModeList)` to remove ModeHistory from the stack, then `replaceMode(ModeResults)` to enter results mode. This prevents unbounded stack growth from History->Results->History cycles:

```go
func (a *App) handleHistoryEnter() (tea.Model, tea.Cmd) {
    entry := a.historyEntries[a.historyCursor]

    // Remove ModeHistory from stack, restoring the previous mode
    a.popMode(ModeList)
    // Lateral transition into results (does not grow the stack)
    a.replaceMode(ModeResults)

    // Re-execute the stored query
    return a.reExecuteSearch(entry.QueryText)
}
```

#### ModeArticle (reading full article -- Phase 3)

| Key | Action | Notes |
|-----|--------|-------|
| `j` / `Down` | Scroll down | |
| `k` / `Up` | Scroll up | |
| `Esc` | Close article | Returns to previous mode via `popMode(ModeResults)` |
| `m` | More Like This | Uses article's embedding as seed |
| `o` | Open in browser | `xdg-open` / `open` |

Note: `q` and `Ctrl-C` are not listed here because they are handled by global key dispatch before mode-specific handling. All modes inherit quit behavior from `handleGlobalKeys`.

**Collision resolution record:**

| Original Conflict | Resolution |
|-------------------|------------|
| `m` ("More Like This" vs "Mark read") | `m` = MLT everywhere; `Enter` = toggle read (already works) |
| `r`/`R` (refresh vs deep rerank) | `r` = refresh (ModeList only); `R` = deep rerank (ModeResults only); case-distinct |
| `p` ("Pin search" vs typing in filter) | `p` = pin (ModeHistory only); typing only in ModeSearch |
| `Tab` (chip focus vs tab switching) | `Tab` = pinned tab switching (F8 chips deferred) |
| `Ctrl-P` ("Toggle pin" vs up) | Removed; `p` in ModeHistory instead |
| `Enter` (toggle read vs open article) | `Enter` = toggle read; `o` = open article |
| `?` (debug toggle vs search input) | `?` = debug toggle in non-text modes; normal character in ModeSearch |

### 2c. Per-Query Context Cancellation

**Problem:** When a user starts a new search or presses Esc, in-flight API calls from the previous search continue running. The QueryID staleness check prevents stale *results* from being applied, but the goroutines and HTTP requests are not cancelled. With Ollama, this means 30+ seconds of wasted CPU.

**Solution:** Add a per-query `context.Context` to the App struct. Cancel it on new search, Esc, or mode change.

```go
type App struct {
    // ... existing fields ...

    // Per-query cancellation
    searchCtx    context.Context    // initialized to context.Background(), never nil
    searchCancel context.CancelFunc

    // Auto-rerank policy (set from AppConfig at init, not from interface)
    autoReranks bool
}
```

**Interface reconciliation.** The UI layer does not hold a `Reranker` interface. All async operations are injected as closures via `AppConfig` (consistent with `LoadSearchPool`, `EmbedQuery`, etc.). The rerank policy is expressed as a simple `autoReranks bool` on App, set from `AppConfig.AutoReranks` at init time. The UI never performs type assertions on backend objects — it only sees closures and config bools.

**Lifecycle:**

```go
// cancelSearch cancels any in-flight search work.
// Safe to call multiple times or when searchCancel is nil.
//
// Note: cancelSearch does NOT clear a.queryID. Staleness is handled
// naturally — when a new search overwrites queryID with a fresh value,
// completion messages carrying the old queryID are discarded by the
// staleness check (msg.QueryID != a.queryID). Clearing queryID here
// would break the ordering in submitSearch/handleMoreLikeThis, where
// queryID is set BEFORE newSearchContext() is called.
func (a *App) cancelSearch() {
    if a.searchCancel != nil {
        a.searchCancel()
    }
    a.searchCancel = nil
    a.searchCtx = context.Background() // never nil

    // Reset pipeline flags to avoid stuck spinners
    a.embeddingPending = false
    a.rerankPending = false
    a.searchPoolPending = false
}

// newSearchContext creates a fresh context for a new search.
// Cancels any existing search first (which cancels the old context
// and resets pipeline flags, but preserves queryID).
func (a *App) newSearchContext() context.Context {
    a.cancelSearch()
    a.searchCtx, a.searchCancel = context.WithCancel(context.Background())
    return a.searchCtx
}
```

**Call sites that cancel:**
- `submitSearch()` — cancel previous before starting new
- `clearSearch()` (Esc stage 2) — cancel current
- `handleMoreLikeThis()` — cancel previous before starting MLT
- `popMode()` — cancels when popping from ModeResults or ModeSearch (see Section 2a)

**All call sites must thread `a.searchCtx`.** Every async operation that accepts a `ctx` parameter must receive `a.searchCtx` (or the return value of `a.newSearchContext()`). This includes:

- `submitSearch()`: calls `a.newSearchContext()`, then passes the returned ctx to `a.loadSearchPool(ctx, ...)` and `a.embedQuery(ctx, ...)`
- `handleMoreLikeThis()`: calls `a.newSearchContext()`, then passes ctx to `a.loadSearchPool(ctx, ...)`
- `startReranking()`: passes `a.searchCtx` to `a.batchRerank(ctx, ...)` or `a.scoreEntry(ctx, ...)`

**AppConfig signature changes:**

```go
type AppConfig struct {
    // Async operations that accept per-query context for cancellation
    EmbedQuery     func(ctx context.Context, query string, queryID string) tea.Cmd
    BatchRerank    func(ctx context.Context, query string, docs []string, queryID string) tea.Cmd
    ScoreEntry     func(ctx context.Context, query string, doc string, index int, queryID string) tea.Cmd
    LoadSearchPool func(ctx context.Context, queryID string) tea.Cmd

    // Rerank policy: true for fast batch backends (Jina), false for slow
    // per-item backends (Ollama). The UI uses this bool to decide whether
    // to auto-trigger reranking or wait for manual `R` key. Set by cmd/
    // factory based on which reranker is available.
    AutoReranks bool

    // Synchronous operations (no per-query cancellation needed)
    LoadItems       func() tea.Cmd
    LoadRecentItems func() tea.Cmd
    MarkRead        func(id string) tea.Cmd
    TriggerFetch    func() tea.Cmd

    // FTS5 instant search (synchronous, called directly in submitSearch)
    // nil when FTS5 is not available.
    SearchFTS func(query string, limit int) ([]store.Item, error)

    // Feature flags
    Features Features
}
```

**Closure safety (Gemini-3's concern):** All closures capture `queryID` (string, copied) and `itemID` (string, copied), never slice indices. The completion handler re-looks up position by ID:

```go
// WRONG: captures index
cmd := func() tea.Msg {
    return EntryReranked{Index: i, Score: score}
}

// RIGHT: captures ID, handler re-looks up
cmd := func() tea.Msg {
    return EntryReranked{ItemID: item.ID, Score: score, QueryID: qid}
}
```

### 2d. Feature Flags

**Problem:** With phased delivery, partially-implemented features must be gatable without `#ifdef` hacks.

**Solution:** A simple feature flag struct passed via AppConfig. No runtime toggling — compile-time or config-file constants.

```go
// Features gates optional functionality. All default to false.
type Features struct {
    MLT           bool // Feature 1: "More Like This"
    OllamaRerank  bool // Feature 2: Opt-in Ollama rerank
    FTS5          bool // Feature 7: Full-text search
    SearchHistory bool // Feature 5+9: Search history + pinned views
    ScoreColumn   bool // Feature 6: Score transparency
    JinaReader    bool // Feature 10: Article reader
}
```

**Config validation.** Validate that feature flags have their required dependencies at startup:

```go
func (c AppConfig) Validate() error {
    if c.Features.MLT && c.LoadSearchPool == nil {
        return fmt.Errorf("MLT enabled but LoadSearchPool is nil (needed to load full corpus for similarity)")
    }
    if c.Features.OllamaRerank && c.BatchRerank == nil && c.ScoreEntry == nil {
        return fmt.Errorf("OllamaRerank enabled but no rerank functions provided")
    }
    if c.Features.FTS5 && c.SearchFTS == nil {
        return fmt.Errorf("FTS5 enabled but SearchFTS is nil")
    }
    return nil
}
```

Note: MLT validates against `LoadSearchPool` (not `EmbedQuery`) because MLT uses locally-stored embeddings for cosine ranking and the seed item's text for cross-encoder reranking. It does not call the embedding API. MLT can function even if the embedding API is down, provided items have pre-calculated embeddings.

**Feature checks are simple `if` guards at the key handler level:**

```go
func (a *App) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch msg.String() {
    case "m":
        if a.features.MLT {
            return a.handleMoreLikeThis()
        }
    case "Ctrl-R":
        if a.features.SearchHistory {
            return a.openHistory()
        }
    // ...
    }
}
```

**Feature flag gating for all keybindings:**

| Key | Feature Flag | Behavior When Disabled |
|-----|-------------|----------------------|
| `m` | `MLT` | No-op (silent) |
| `R` | `OllamaRerank` | No-op (silent) |
| `Ctrl-R` | `SearchHistory` | No-op (silent) |
| `Tab`/`Shift-Tab` | `SearchHistory` | No-op when no pinned searches exist |
| `o` | `JinaReader` | No-op (silent) |
| `x` | `ScoreColumn` | No-op (silent) |

No feature flag should gate more than one `if` check per call site. If a feature requires flags scattered across 5+ locations, the feature needs better encapsulation.
## 3. Phase 1: Core Search

Phase 1 features can be developed in parallel after Phase 0 lands. They are independent of each other.

### Feature 1: "More Like This" (REVISED -- unified design)

**Reviewer consensus:** All 3 reviewers identified F1's 4 subtasks as conflicting designs that needed unification. GPT-5 noted the mlt bool/FilterMode enum was redundant with AppMode.

#### Key Decisions

| Decision | Resolution | Rationale |
|----------|-----------|-----------
| App mode tracking | Reuse `ModeResults` | MLT results are a ranked list, identical to search results. No new mode needed. |
| State Management | `mltSeedID` + `mltSeedTitle` | Single source of truth. Title is cached to prevent O(N) lookups during rendering. |
| View stack | None for v1 | Esc always returns to `ModeList` (chronological). Simplicity over nesting. |
| Missing embeddings | Exclude silently | Items without embeddings cannot be ranked by cosine similarity. They are naturally filtered out. |
| Chaining | `m` pivots seed | `savedItems` are preserved across chains. Only Esc restores them. |
| Pipeline | Hybrid Reuse | Cosine uses local vectors (immediate); Cross-encoder uses seed text (background). |

#### App Struct Additions

We add two fields to the `App` struct. `mltSeedID` acts as the mode switch, while `mltSeedTitle` is a performance optimization for the render loop.

```go
type App struct {
    // ... existing fields ...

    // MLT state: when non-empty, the current results are "More Like This"
    // seeded from this item ID rather than a text query.
    mltSeedID string

    // Cache the seed title to avoid O(N) lookups in View()
    // Set in handleMoreLikeThis, cleared in clearSearch.
    mltSeedTitle string
}
```

The `mltSeedID` field interacts with existing fields as follows:

| Field | Text Search | MLT |
|-------|------------|-----|
| `mltSeedID` | `""` | item ID of seed |
| `mltSeedTitle` | `""` | seed item's title (cached) |
| `filterInput.Value()` | user's query text | `""` (not used) |
| `queryEmbedding` | from `embedQuery()` API call | from `embeddings[seedID]` (local lookup) |
| `savedItems` | snapshot before search | snapshot before first MLT (preserved across chains) |
| `searchPoolPending` | true (loading all items) | true (loading all items) |
| `embeddingPending` | true (API call in flight) | **false** (embedding is local) |

#### Snapshot Helper

Both `handleMoreLikeThis()` and `submitSearch()` need to snapshot the chronological view on first entry into results mode, but preserve existing snapshots across chains and re-searches. This is centralized in a single helper:

```go
// ensureSnapshot saves the current chronological view if no snapshot exists.
// Called on any entry into ModeResults (text search, MLT, history re-run).
// If savedItems is already set (e.g., chaining MLT or re-searching from results),
// the existing snapshot is preserved — it holds the original chronological view.
func (a *App) ensureSnapshot() {
    if a.savedItems != nil {
        return // already snapshotted; preserve original chronological view
    }
    a.savedItems = make([]store.Item, len(a.items))
    copy(a.savedItems, a.items)
    a.savedEmbeddings = make(map[string][]float32, len(a.embeddings))
    for k, v := range a.embeddings {
        a.savedEmbeddings[k] = v
    }
}
```

#### `hasQuery()` Update

`hasQuery()` determines whether the app is in an active query state (MLT or text search). It must return true in all cases where results are meaningfully displayed, including the Ollama cosine-only path where no reranking is pending but results are on screen.

```go
func (a App) hasQuery() bool {
    return a.mltSeedID != "" || a.activeQuery != ""
}
```

This replaces the previous definition which mixed `filterInput.Value()` and the deleted `searchActive` boolean. The two conditions cover:
- `mltSeedID != ""`: MLT is active (seed-based similarity search)
- `activeQuery != ""`: text search is active (set at search execution, not from live input)

#### Implementation: `handleMoreLikeThis()`

```go
// handleMoreLikeThis initiates a "More Like This" search from the currently
// selected item. Uses the item's stored embedding as the query vector for
// immediate cosine ranking, then starts background cross-encoder reranking.
func (a App) handleMoreLikeThis() (tea.Model, tea.Cmd) {
    if len(a.items) == 0 || a.cursor >= len(a.items) {
        return a, nil
    }

    seed := a.items[a.cursor]

    // Gate: seed must have a stored embedding
    seedEmb, ok := a.embeddings[seed.ID]
    if !ok || len(seedEmb) == 0 {
        a.logger.Emit(otel.Event{
            Kind:  otel.KindMLT,
            Level: otel.LevelWarn,
            Comp:  "ui",
            Extra: map[string]any{"seed": seed.ID, "reason": "no_embedding"},
        })
        return a, nil // Silent no-op; item has no embedding
    }

    // Cancel any in-flight search and create a fresh context.
    ctx := a.newSearchContext()

    // Transition to results mode.
    a.mode = ModeResults

    // Save chronological view on first entry into results mode.
    // If we're chaining (savedItems already set), the original snapshot is preserved.
    a.ensureSnapshot()

    // Set MLT state
    a.mltSeedID = seed.ID
    a.mltSeedTitle = seed.Title    // Cache title for View()
    a.activeQuery = entryText(seed) // Set for R key (manual Ollama rerank)
    a.filterInput.SetValue("")      // Clear any text query
    a.filterInput.Blur()
    a.queryEmbedding = seedEmb      // Local lookup, no API call
    a.embeddingPending = false      // No async embedding needed
    a.searchStart = time.Now()
    a.queryID = newQueryID()

    a.logger.Emit(otel.Event{
        Kind:    otel.KindMLT,
        Level:   otel.LevelInfo,
        Comp:    "ui",
        QueryID: a.queryID,
        Extra:   map[string]any{"seed": seed.ID, "title": truncateRunes(seed.Title, 60)},
    })

    // Load full search pool (may already be loaded if chaining)
    var cmds []tea.Cmd
    if a.loadSearchPool != nil {
        a.searchPoolPending = true
        cmds = append(cmds, a.loadSearchPool(ctx, a.queryID))
    }

    // Immediate cosine ranking on current items while pool loads
    a.rerankItemsByEmbedding()

    // Exclude the seed item from results — users don't want to see
    // the item they just pressed m on at position #1.
    a.excludeItem(seed.ID)

    a.statusText = fmt.Sprintf("Finding similar to \"%s\"...", truncateRunes(seed.Title, 30))
    cmds = append(cmds, a.spinner.Tick)

    // If pool is not loading (nil loadSearchPool), start reranking immediately
    if !a.searchPoolPending {
        return a.startMLTReranking(seed)
    }

    return a, tea.Batch(cmds...)
}

// excludeItem removes a single item by ID from a.items.
func (a *App) excludeItem(id string) {
    for i, item := range a.items {
        if item.ID == id {
            a.items = append(a.items[:i], a.items[i+1:]...)
            if a.cursor >= len(a.items) && len(a.items) > 0 {
                a.cursor = len(a.items) - 1
            } else {
                a.cursor = 0
            }
            return
        }
    }
}
```

#### Reranking: Reuse Existing Pipeline

MLT reranking reuses `startReranking()` but needs a synthetic "query" for the cross-encoder. The seed item's title (+summary) serves this purpose.

```go
// startMLTReranking begins cross-encoder reranking using the seed item's
// title as the query. This reuses the existing startReranking pipeline.
func (a App) startMLTReranking(seed store.Item) (tea.Model, tea.Cmd) {
    query := entryText(seed)
    return a.startReranking(query)
}
```

This is the critical insight: the cosine path uses the seed's **embedding vector** (no API call), while the cross-encoder rerank path uses the seed's **title text** (passed to `startReranking` which feeds it to `batchRerank` or `scoreEntry`). Both paths are already implemented; MLT just wires them differently.

#### Message Handling: SearchPoolLoaded for MLT

The existing `SearchPoolLoaded` handler detects MLT mode via `mltSeedID`. It includes a simplified guard clause.

```go
case SearchPoolLoaded:
    a.searchPoolPending = false
    if msg.QueryID != "" && msg.QueryID != a.queryID {
        return a, nil
    }

    if !a.hasQuery() {
        return a, nil
    }

    if msg.Err != nil {
        a.err = msg.Err
        a.statusText = ""
        return a, nil
    }
    // Cancel in-flight reranking — items are about to change
    if a.rerankPending {
        a.rerankPending = false
        a.rerankEntries = nil
        a.rerankScores = nil
        a.rerankProgress = 0
        a.statusText = ""
    }
    a.items = msg.Items
    a.embeddings = msg.Embeddings

    if len(a.queryEmbedding) > 0 {
        a.rerankItemsByEmbedding()

        // MLT path: exclude seed, rerank with seed title
        if a.mltSeedID != "" {
            a.excludeItem(a.mltSeedID)

            seed := a.findItemInSaved(a.mltSeedID)
            if seed != nil {
                return a.startMLTReranking(*seed)
            }
            // Seed not found in saved items — cosine ranking is sufficient
            a.statusText = ""
            return a, nil
        }

        // Text search path (unchanged)
        return a.startReranking(a.filterInput.Value())
    }
    return a, nil
```

Helper to find the seed item for reranking:

```go
// findItemInSaved looks up an item by ID in savedItems (the pre-search snapshot).
func (a *App) findItemInSaved(id string) *store.Item {
    for i := range a.savedItems {
        if a.savedItems[i].ID == id {
            return &a.savedItems[i]
        }
    }
    for i := range a.items {
        if a.items[i].ID == id {
            return &a.items[i]
        }
    }
    return nil
}
```

`RerankComplete` requires **no changes** -- it is query-ID correlated and operates on `rerankEntries`/`rerankScores` which are set by `startReranking()` regardless of whether the trigger was text search or MLT.

#### Key Binding and Routing

Add `m` to the normal mode and rerank-pending key handling in `handleKeyMsg`:

```go
// In handleKeyMsg, normal mode string-based keys:
case "m":
    return a.handleMoreLikeThis()

// In handleKeyMsg, rerankPending block (allows chaining while loading):
case "m":
    return a.handleMoreLikeThis()
```

#### `clearSearch()` Update

```go
func (a App) clearSearch() (tea.Model, tea.Cmd) {
    // ... existing clearing logic ...
    a.mltSeedID = ""
    a.mltSeedTitle = "" // Clear cached title
    a.activeQuery = ""  // Clear active query

    // Restore chronological view
    if a.savedItems != nil {
        a.items = a.savedItems
        a.embeddings = a.savedEmbeddings
        a.savedItems = nil
        a.savedEmbeddings = nil
    } else {
        a.sortByFetchTime()
    }
    return a, nil
}
```

#### View() Updates

```go
// In View(), where the filter bar is rendered:
if a.mltSeedID != "" && a.statusText == "" {
    // Use cached string instead of O(N) lookup
    searchBar = RenderFilterBarWithStatus(
        fmt.Sprintf("Similar to: %s", truncateRunes(a.mltSeedTitle, 40)),
        len(a.items), len(a.items), a.width, "",
    )
} else if a.hasQuery() && a.statusText == "" {
    // existing text search bar
}
```

#### Chaining Behavior

When the user presses `m` while already viewing MLT or text search results:

1. `handleMoreLikeThis()` is called with the new seed item.
2. `newSearchContext()` cancels any in-flight work from the previous query.
3. `ensureSnapshot()` detects `savedItems != nil` and preserves the original chronological snapshot.
4. `mltSeedID` is updated to the new seed. `mltSeedTitle` and `activeQuery` are updated.
5. `queryID` is regenerated (stale-checks discard in-flight results from previous seed).
6. Cosine ranking runs immediately on current items.
7. Search pool reloads (full corpus) and cross-encoder reranks.

Pressing **Esc at any point** restores the original chronological view from `savedItems`.

#### Edge Cases

| Edge Case | Behavior |
|-----------|----------|
| Item has no embedding | `handleMoreLikeThis()` returns `nil` (silent no-op); logs warning. |
| Seed in results | `excludeItem()` called twice: immediately after cosine sort, and again in `SearchPoolLoaded`. |
| Empty corpus | Guards on `len(a.items) == 0` prevent crashes throughout. |
| Esc during MLT reranking | Routes through `clearSearch()` which clears all MLT state and restores `savedItems`. |
| Rapid chaining (m, m, m) | Each press calls `newSearchContext()` (cancels prior), generates new `queryID`. Stale checks discard old results. |

#### Stale Check Summary

| Message | Stale Check | MLT-safe? |
|---------|------------|-----------|
| `QueryEmbedded` | `msg.QueryID != a.queryID` | Not used by MLT (no embedding API call) |
| `SearchPoolLoaded` | `msg.QueryID != a.queryID` | Yes -- new `queryID` per chain |
| `RerankComplete` | `msg.QueryID != a.queryID` | Yes -- new `queryID` per chain |
| `EntryReranked` | `msg.QueryID != a.queryID` | Yes -- new `queryID` per chain |

#### What Does NOT Change

- `RerankComplete` handler -- already query-ID correlated
- `EntryReranked` handler -- already query-ID correlated
- `AppConfig` / `NewApp` -- no new function dependencies
- `filter.RerankByQuery` -- already handles mixed embedded/unembedded items
- `startReranking()` -- called with seed title text, works as-is

---

### Feature 2: Opt-in Ollama Rerank (SIMPLIFIED -- no listwise)

**Reviewer consensus:** All 3 reviewers agreed Ollama reranking must be opt-in given its ~32s latency. Listwise reranking prompt is cut -- native Qwen3-Reranker format suffices.

#### Problem

Ollama reranking with Qwen3-Reranker is per-item and local, requiring ~30 sequential HTTP calls to `localhost:11434` for 30 items, resulting in **~32 seconds** wall-clock time. Auto-triggering this after every search is a UX disaster: users see cosine results, then endure a 30+ second spinner before reshuffling. Jina AI, by contrast, handles the same job in a single batch API call in ~1 second.

The backends have fundamentally different performance profiles, necessitating distinct UX behaviors: auto for fast batch backends (Jina), opt-in for slow per-item backends (Ollama).

#### Solution: Backend-Gated Behavior via AppConfig

The `AutoReranker` interface is defined in the `rerank` package for use during wiring in `main.go`. The UI layer does not hold a reranker reference -- it uses AppConfig fields exclusively.

**Rerank package interface (for main.go wiring):**

```go
// In rerank package:

type Reranker interface {
    Available() bool
    Rerank(ctx context.Context, query string, documents []string) ([]Score, error)
    Name() string
}

// AutoReranker is an optional extension. Backends that support fast batch
// reranking (e.g., Jina) return true. Slow per-item backends (e.g., Ollama)
// return false. Used by main.go when constructing AppConfig.
type AutoReranker interface {
    AutoReranks() bool
}
```

**UI layer uses AppConfig capability flags (not interface assertions):**

```go
type AppConfig struct {
    // ... existing fields ...

    // autoReranks is true when the backend is fast enough to auto-rerank
    // after every search without user confirmation. Set from AutoReranker
    // interface check in main.go during wiring.
    AutoReranks bool
}

type App struct {
    // ... existing fields ...

    // Capability flags (set from AppConfig at construction)
    autoReranks bool // true = Jina (auto); false = Ollama (manual R)
}
```

**Wiring in main.go:**

```go
autoReranks := false
if ar, ok := reranker.(rerank.AutoReranker); ok {
    autoReranks = ar.AutoReranks()
}
cfg := ui.AppConfig{
    AutoReranks: autoReranks,
    BatchRerank: /* ... */,
    ScoreEntry:  /* ... */,
    // ...
}
```

**Reranker availability check in UI:** The UI determines reranker availability from the AppConfig cmd factories, not from an interface reference:

```go
// rerankerAvailable returns true if any rerank capability is wired.
func (a *App) rerankerAvailable() bool {
    return a.batchRerank != nil || a.scoreEntry != nil
}
```

| Backend          | `AutoReranks()` | `autoReranks` | Behavior |
|------------------|-----------------|---------------|----------|
| `JinaReranker`  | `true`         | `true`        | Auto-rerank after cosine stage (unchanged). |
| `OllamaReranker`| `false`        | `false`       | Show cosine results immediately; `R` to opt-in. |
| No reranker      | N/A            | `false`       | Cosine-only; `R` is no-op. |

#### Jina Path (Unchanged)

1. User types query, presses Enter.
2. Embed query, load search pool.
3. Cosine similarity ranks (instant).
4. `startReranking()` auto-fires.
5. Jina batch API (~1s); re-sort by cross-encoder score.

#### Ollama Path (New)

1. User types query, presses Enter.
2. Embed query, load search pool.
3. Cosine ranks (instant) -- **results shown immediately**.
4. Status: `Cosine results -- press R to rerank`.
5. User reviews cosine results.
6. Press `R`:
   - Status: `Reranking 0/30...`
   - `startReranking(activeQuery)` with per-item pipeline.
   - Progress: `Reranking 5/30...` (derived from completions).
   - On finish: re-sort by cross-encoder score.
7. Esc behavior follows two-stage pattern (see Edge Cases).

**Key invariant:** Store `activeQuery` at search execution (not `filterInput.Value()`), and use it for rerank to avoid mismatches if user edits input post-search.

#### UI Integration

**Decision point in `Update()`:** Fork after cosine completes, in `QueryEmbedded` handler:

```go
case QueryEmbedded:
    // ... cosine reranking ...
    a.rerankItemsByEmbedding()
    a.activeQuery = msg.Query // Capture for rerank

    if a.autoReranks {
        return a.startReranking(a.activeQuery) // Auto-fire
    }
    // Manual: Show results, hint for R
    a.statusText = "Cosine results -- press R to rerank"
    return a, a.spinner.Tick
```

**`R` key handler:** State-based availability using AppConfig capability flags:

```go
case "R":
    if !a.autoReranks && a.activeQuery != "" && !a.rerankPending && a.rerankerAvailable() {
        return a.startReranking(a.activeQuery)
    }
    a.statusText = "R not available (already reranking or no results)"
    return a, a.spinner.Tick
```

`R` (uppercase) avoids conflict with `r` (refresh); signals deliberate action.

**Progress display:** Leverage existing `rerankProgress` / `rerankEntries`:

```go
// In handleEntryReranked:
a.statusText = fmt.Sprintf("Reranking %d/%d...", a.rerankProgress, len(a.rerankEntries))
```

#### Key Hints in Status Bar

- Cosine shown: `Up/Down navigate . / search . R rerank . q quit`
- Reranking: `Reranking 12/30... . Esc cancel`

#### Two-Stage Esc Behavior

Esc follows a consistent two-stage pattern aligned with Phase 0b's keymap:

1. **First Esc during rerank:** Cancels the in-flight rerank, reverts to cosine-sorted results, clears status text. The user stays in `ModeResults` with their cosine results visible. This is the "cancel work" stage.

2. **Second Esc (or first Esc when no rerank is in flight):** Exits `ModeResults`, restores `savedItems`, transitions to `ModeList`. This is the "exit view" stage.

Implementation in `handleResultsKeys`:

```go
case "esc":
    if a.rerankPending {
        // Stage 1: cancel rerank, keep cosine results
        a.cancelSearch()
        a.rerankPending = false
        a.rerankEntries = nil
        a.rerankScores = nil
        a.rerankProgress = 0
        a.statusText = "Cosine results -- press R to rerank"
        // Re-sort by cosine (undo any partial rerank reordering)
        if len(a.queryEmbedding) > 0 {
            a.rerankItemsByEmbedding()
        }
        return a, nil
    }
    // Stage 2: exit results entirely
    return a.clearSearch()
```

#### Edge Cases

| Scenario                  | Behavior |
|---------------------------|----------|
| No reranker / unavailable | `R` no-op; no hint shown. |
| `R` during rerank         | No-op; status: "Already reranking...". |
| First Esc during rerank   | Cancel rerank; revert to cosine order; stay in ModeResults. |
| Second Esc (no rerank)    | Exit to ModeList; restore savedItems. |
| New search during rerank  | Cancel rerank via `newSearchContext()` (existing). |
| Ollama down mid-rerank    | Partial scores -> 0.5; cosine tiebreaker; scored > unscored. |
| Zero results              | Early return (existing `topN == 0`). |
| Edit input post-search    | `R` uses stored `activeQuery` (ignores edits). |
| `R` on MLT results        | Works: `activeQuery` is set to `entryText(seed)` by `handleMoreLikeThis()`. |

---

### Feature 7: FTS5 Instant Lexical Search (REVISED -- 7a/7b/7c only, 7d deferred)

**Goal:** Provide instant (<50ms) lexical search results while the embedding API call is in flight. FTS5 results appear immediately on Enter, then get progressively replaced as cosine similarity and cross-encoder reranking complete.

**Prerequisite:** Phase 0a (AppMode enum), Phase 0c (per-query context cancellation).

**Scope:** 7a (schema), 7b (store methods), 7c (search integration). 7d (hot cache) explicitly deferred.

#### 7a. FTS5 Schema

We use **external content FTS5** -- the FTS index references the `items` table by rowid rather than storing a copy of the text. This halves storage overhead since the full text already lives in `items`.

**Why external content is safe here:** Observer uses `INSERT OR IGNORE` for items (`store.go:143`), which means rowids are stable -- an ignored duplicate never deletes/reinserts a row. The only mutations after insert are `UPDATE items SET read = 1` and `UPDATE items SET saved = ?`, neither of which touch indexed columns (title, summary, source_name). This makes external content FTS5 safe without complex rowid tracking.

##### Virtual Table

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
    title,
    summary,
    source_name,
    author,         -- users search for columnists; matches items.author (store.go:104)
    content='items',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);
```

The `author` column corresponds to `items.author` (defined at `store.go:104`). It is indexed now because changing FTS schema later requires a full drop/recreate/rebuild. Users search for specific columnists (e.g., "Ezra Klein", "Paul Krugman"). Adding it now is cheap.

**Tokenizer choice: `unicode61 remove_diacritics 2`**

- `unicode61` handles non-ASCII characters correctly (important for international news sources -- accented names, non-Latin scripts).
- `remove_diacritics 2` means diacritics are removed for matching but the original text is preserved in results. A search for "cafe" matches "cafe".
- We do **not** use `porter` stemming because news headlines are short and precise -- stemming causes false matches ("running" matching "run" is rarely helpful for news titles) and prevents exact-phrase search from working as expected.

##### Sync Triggers

These triggers keep the FTS index in sync with the `items` table automatically:

```sql
-- After INSERT: index the new row
CREATE TRIGGER IF NOT EXISTS items_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, summary, source_name, author)
    VALUES (new.rowid, new.title, new.summary, new.source_name, new.author);
END;

-- After UPDATE on indexed columns: remove old entry, insert new
CREATE TRIGGER IF NOT EXISTS items_au AFTER UPDATE OF title, summary, source_name, author ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, summary, source_name, author)
    VALUES ('delete', old.rowid, old.title, old.summary, old.source_name, old.author);
    INSERT INTO items_fts(rowid, title, summary, source_name, author)
    VALUES (new.rowid, new.title, new.summary, new.source_name, new.author);
END;

-- After DELETE: remove from FTS index
CREATE TRIGGER IF NOT EXISTS items_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, summary, source_name, author)
    VALUES ('delete', old.rowid, old.title, old.summary, old.source_name, old.author);
END;
```

**Edge case: `INSERT OR IGNORE` and triggers.** When `INSERT OR IGNORE` encounters a duplicate (URL conflict), the `INSERT` is silently skipped -- the `AFTER INSERT` trigger does **not** fire. This is correct behavior: the item already exists in both `items` and `items_fts`, so no FTS update is needed.

**Edge case: `UPDATE items SET embedding = ?`.** This updates the `embedding` column, which is not in the trigger's `UPDATE OF` column list. The `items_au` trigger does not fire, which is correct.

##### Schema Migration via `PRAGMA user_version`

`CREATE VIRTUAL TABLE IF NOT EXISTS` will not update the schema if the table already exists (e.g., a prior version without the `author` column). We use `PRAGMA user_version` for lightweight schema migration:

```go
// migrateFTS ensures the FTS5 schema is current.
// Uses PRAGMA user_version to track schema versions:
//   0 = fresh install or pre-FTS (no FTS tables)
//   1 = FTS without author column (early adopters)
//   2 = FTS with author column (current)
func (s *Store) migrateFTS() error {
    s.mu.Lock()
    defer s.mu.Unlock()

    var version int
    if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
        return fmt.Errorf("read user_version: %w", err)
    }

    switch {
    case version >= 2:
        // Current schema — no migration needed.
        return nil

    case version == 1:
        // Upgrade from FTS without author → FTS with author.
        // Must drop and recreate because FTS5 virtual tables cannot be ALTERed.
        _, err := s.db.Exec(`
            DROP TRIGGER IF EXISTS items_ai;
            DROP TRIGGER IF EXISTS items_au;
            DROP TRIGGER IF EXISTS items_ad;
            DROP TABLE IF EXISTS items_fts;
        `)
        if err != nil {
            return fmt.Errorf("drop old FTS schema: %w", err)
        }
        // Fall through to create fresh schema below.
        fallthrough

    case version == 0:
        // Fresh install or pre-FTS: create tables from scratch.
        ftsSchema := `
            CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
                title,
                summary,
                source_name,
                author,
                content='items',
                content_rowid='rowid',
                tokenize='unicode61 remove_diacritics 2'
            );

            CREATE TRIGGER IF NOT EXISTS items_ai AFTER INSERT ON items BEGIN
                INSERT INTO items_fts(rowid, title, summary, source_name, author)
                VALUES (new.rowid, new.title, new.summary, new.source_name, new.author);
            END;

            CREATE TRIGGER IF NOT EXISTS items_au AFTER UPDATE OF title, summary, source_name, author ON items BEGIN
                INSERT INTO items_fts(items_fts, rowid, title, summary, source_name, author)
                VALUES ('delete', old.rowid, old.title, old.summary, old.source_name, old.author);
                INSERT INTO items_fts(rowid, title, summary, source_name, author)
                VALUES (new.rowid, new.title, new.summary, new.source_name, new.author);
            END;

            CREATE TRIGGER IF NOT EXISTS items_ad AFTER DELETE ON items BEGIN
                INSERT INTO items_fts(items_fts, rowid, title, summary, source_name, author)
                VALUES ('delete', old.rowid, old.title, old.summary, old.source_name, old.author);
            END;
        `
        if _, err := s.db.Exec(ftsSchema); err != nil {
            return fmt.Errorf("create FTS schema: %w", err)
        }
    }

    // Set version to current.
    if _, err := s.db.Exec("PRAGMA user_version = 2"); err != nil {
        return fmt.Errorf("set user_version: %w", err)
    }

    return nil
}
```

**Call order on startup:** `createTables()` (base schema) -> `migrateFTS()` (FTS schema + migration) -> `rebuildFTS()` (backfill if needed).

##### Backfill Existing Data

Conditional rebuild: instead of rebuilding on every startup (which grows costly at 50k+ items), check if the index needs rebuilding first:

```go
// rebuildFTS populates the FTS index from all existing items.
// Only rebuilds if the index is empty but items exist,
// avoiding unnecessary work on normal startups.
func (s *Store) rebuildFTS() error {
    s.mu.Lock()
    defer s.mu.Unlock()

    var ftsCount int
    if err := s.db.QueryRow("SELECT count(*) FROM items_fts").Scan(&ftsCount); err != nil {
        return fmt.Errorf("check FTS count: %w", err)
    }

    if ftsCount > 0 {
        return nil // Index already populated, triggers keep it in sync
    }

    var itemsCount int
    if err := s.db.QueryRow("SELECT count(*) FROM items").Scan(&itemsCount); err != nil {
        return fmt.Errorf("check items count: %w", err)
    }

    if itemsCount == 0 {
        return nil // Nothing to index
    }

    _, err := s.db.Exec("INSERT INTO items_fts(items_fts) VALUES('rebuild')")
    if err != nil {
        return fmt.Errorf("rebuild FTS index: %w", err)
    }
    return nil
}
```

**When to call:** On startup, after `migrateFTS()`. For databases with existing items that predate the FTS table, this one-time rebuild populates the index. The version-1-to-2 migration (which drops and recreates the table) also triggers a rebuild here since the new table starts empty.

#### 7b. Store Methods

##### SearchFTS

```go
// SearchFTS performs a full-text search using FTS5 and returns matching items
// ordered by BM25 relevance.
//
// Column weights: title=10, summary=5, source_name=1, author=3.
// If the raw query fails (FTS5 syntax error), retries as a quoted literal string.
//
// Thread-safe: acquires read lock.
func (s *Store) SearchFTS(query string, limit int) ([]Item, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    if limit <= 0 {
        limit = 50
    }

    items, err := s.searchFTSRaw(query, limit)
    if err != nil {
        // Retry with quoted literal on FTS5 syntax error.
        // This handles queries like "C++", unclosed quotes, reserved words.
        escaped := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
        items, err = s.searchFTSRaw(escaped, limit)
        if err != nil {
            return nil, fmt.Errorf("FTS search: %w", err)
        }
    }
    return items, nil
}

func (s *Store) searchFTSRaw(query string, limit int) ([]Item, error) {
    // bm25() returns values where smaller (more negative) = more relevant.
    // ORDER BY bm25(...) sorts most relevant first.
    // Column weights: title=10, summary=5, source_name=1, author=3.
    rows, err := s.db.Query(`
        SELECT i.id, i.source_type, i.source_name, i.title, i.summary,
               i.url, i.author, i.published_at, i.fetched_at, i.read, i.saved
        FROM items_fts fts
        JOIN items i ON i.rowid = fts.rowid
        WHERE fts MATCH ?
        ORDER BY bm25(fts, 10.0, 5.0, 1.0, 3.0)
        LIMIT ?
    `, query, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var items []Item
    for rows.Next() {
        var item Item
        var read, saved int
        if err := rows.Scan(
            &item.ID, &item.SourceType, &item.SourceName, &item.Title,
            &item.Summary, &item.URL, &item.Author, &item.Published,
            &item.Fetched, &read, &saved,
        ); err != nil {
            return nil, fmt.Errorf("scan FTS result: %w", err)
        }
        item.Read = read != 0
        item.Saved = saved != 0
        items = append(items, item)
    }
    return items, rows.Err()
}
```

**Query sanitization:** FTS5's MATCH syntax accepts bare words (`climate change`), phrases (`"climate change"`), boolean operators (`climate OR weather`), prefix queries (`clim*`), and column filters (`title:climate`). We pass the query through as-is to give power users access to the full syntax. If the query contains FTS5 syntax errors, we automatically retry as a quoted literal string rather than showing an error.

**Why JOIN instead of content table lookup:** With external content FTS5, `SELECT * FROM items_fts` returns empty strings for content columns. We must JOIN back to `items` to get the actual data. The JOIN on rowid is an integer primary key lookup -- effectively free.

#### 7c. Search Flow Integration

The current search flow (from `submitSearch()`) is:

```
Enter -> loadSearchPool + embedQuery (parallel) -> cosine sort -> cross-encoder rerank
```

With FTS5, the flow becomes:

```
Enter -> FTS5 (instant, sync) -> display results
      -> loadSearchPool + embedQuery (parallel, async)
      -> cosine sort -> merge with FTS -> display updated results
      -> cross-encoder rerank -> display final results
```

FTS results are produced synchronously in `submitSearch()` and never go through an async message. If FTS ever needs to become async (e.g., dataset grows beyond 100k items and latency spikes >16ms), a message type should be added at that point.

##### Modified submitSearch()

```go
func (a App) submitSearch() (tea.Model, tea.Cmd) {
    query := a.filterInput.Value()
    if query == "" {
        a.mode = ModeList
        a.filterInput.Blur()
        return a, nil
    }

    a.mode = ModeResults
    a.filterInput.Blur()

    // Save current chronological view for restore on Esc.
    // Only snapshots when entering results for the first time;
    // re-searches from ModeResults preserve the original snapshot.
    a.ensureSnapshot()

    a.searchStart = time.Now()
    ctx := a.newSearchContext() // Phase 0c: per-query cancellation
    a.queryID = newQueryID()

    a.logger.Emit(otel.Event{
        Kind:    otel.KindSearchStart,
        Level:   otel.LevelInfo,
        Comp:    "ui",
        QueryID: a.queryID,
        Query:   query,
    })

    // === FTS5: instant lexical results ===
    if a.features.FTS5 && a.searchFTS != nil {
        ftsItems, err := a.searchFTS(query, 50)
        if err != nil {
            a.logger.Emit(otel.Event{
                Kind:  otel.KindSearchFTS,
                Level: otel.LevelWarn,
                Comp:  "ui",
                QueryID: a.queryID,
                Msg:   fmt.Sprintf("FTS error: %v", err),
            })
            // FTS failure is non-fatal — fall through to embedding search
        } else if len(ftsItems) > 0 {
            a.items = ftsItems
            a.cursor = 0
            a.logger.Emit(otel.Event{
                Kind:    otel.KindSearchFTS,
                Level:   otel.LevelInfo,
                Comp:    "ui",
                QueryID: a.queryID,
                Msg:     fmt.Sprintf("FTS returned %d results", len(ftsItems)),
            })
        }
    }

    // === Async: embedding + search pool (unchanged from current flow) ===
    var cmds []tea.Cmd
    if a.loadSearchPool != nil {
        a.searchPoolPending = true
        cmds = append(cmds, a.loadSearchPool(ctx, a.queryID))
    }
    if a.embedQuery != nil {
        a.embeddingPending = true
        cmds = append(cmds, a.embedQuery(ctx, query, a.queryID))
    }
    if len(cmds) > 0 {
        a.statusText = fmt.Sprintf("Searching for \"%s\"...", truncateRunes(query, 30))
        cmds = append(cmds, a.spinner.Tick)
        return a, tea.Batch(cmds...)
    }

    // No embedding available; FTS results are final
    a.statusText = ""
    return a, nil
}
```

**Key design decision: FTS is synchronous.** The SQLite FTS5 query runs in <50ms even with 10,000+ items. Running it synchronously in `submitSearch()` means the results are visible in the very same frame as the Enter keypress. If FTS latency ever spikes >16ms (one frame), move to a `tea.Cmd`. Monitor in practice.

**Fallback behavior:** If `a.features.FTS5` is false or `a.searchFTS` is nil, the flow is identical to today's behavior. FTS is purely additive.

**Ordering note:** `newSearchContext()` is called before `newQueryID()`. This is important because `newSearchContext()` calls `cancelSearch()`, which clears the previous `queryID`. The fresh `queryID` is generated after cancellation, ensuring it is never inadvertently cleared.

##### AppConfig Addition

```go
type AppConfig struct {
    // ... existing fields ...

    // FTS5 instant search (synchronous, called directly in submitSearch)
    // nil when FTS5 is not available.
    SearchFTS func(query string, limit int) ([]store.Item, error)
}
```

##### Result Merging Strategy

When cosine/rerank results arrive after FTS results are already displayed:

1. **`SearchPoolLoaded` handler:** If FTS results are showing, the search pool replaces them entirely.

2. **Cosine scoring:** Proceeds as normal -- ranks pool items by embedding similarity.

3. **Cross-encoder reranking:** Proceeds as normal on the cosine-ranked subset.

**No interleaving.** We do not attempt to merge BM25 scores with cosine scores. The scoring domains are incomparable (BM25 is log-frequency based; cosine is geometric). Instead, FTS results are a **complete replacement** that gets **completely replaced** when semantic results arrive. The UX intent is:

- Frame 0-1: Search submitted, FTS results appear instantly
- Frame ~50-200: Embedding returns, cosine-ranked results replace FTS
- Frame ~200-500: Cross-encoder scores arrive, final reorder

##### Event Types

Add to `internal/otel/event.go`:

```go
KindSearchFTS EventKind = "search.fts" // FTS5 instant results
```

#### 7d. Hot Cache (Deferred)

**Why deferred:**
1. FTS5 is already fast enough (<50ms for 10k items).
2. Incremental search is not in scope (current UI submits on Enter, not per-keystroke).
3. Cache invalidation is non-trivial (new items arrive every 5 minutes).

**Revisit when:** As-you-type incremental search is implemented, or items table exceeds 100,000 rows.

#### Implementation Order

| Step | What | Lines (est.) | Test |
|------|------|-------------|------|
| 7a-1 | Add `migrateFTS()` with `PRAGMA user_version` migration | ~60 | `TestMigrateFTS` — fresh install, v1->v2 upgrade, v2 no-op |
| 7a-2 | Add `rebuildFTS()` method | ~25 | `TestRebuildFTS` inserts items, rebuilds, verifies searchable |
| 7b-1 | Add `SearchFTS()` method with retry | ~60 | `TestSearchFTS` — basic match, BM25 ordering, empty query, syntax error, retry |
| 7b-2 | Trigger correctness tests | ~40 | `TestFTSTriggers` — INSERT, INSERT OR IGNORE dup, UPDATE read (no trigger), DELETE |
| 7c-1 | Add `SearchFTS` to AppConfig | ~5 | -- |
| 7c-2 | Wire FTS into `submitSearch()` | ~25 | `TestSubmitSearchFTS` — FTS results appear, then get replaced by pool |
| 7c-3 | Add `search.fts` event kind | ~3 | -- |

**Total estimate:** ~220 lines of production code, ~140 lines of tests.
## 4. Phase 2: Persistence & Transparency

Phase 2 features depend on Phase 1 being stable. F5+9 depends on Phase 0 (it needs AppMode and the store); it does not depend on F7's FTS tables since `search_history` and `search_results` are independent schemas. F6 depends on having scores from F1 or F2.

### Feature 5+9: Search History & Pinned Views (MERGED)

**Reviewer consensus:** GPT-5 identified F5 and F9 as "the same database concept." Grok-4 wanted F9 cut entirely. Gemini-3 kept both but simplified. Resolution: merge into one schema, one set of store methods. Pinning is a boolean flag on a search history row.

#### Schema

```sql
-- Search history with integrated pinning (F5 + F9 merged).
-- Note: query_embedding BLOB is omitted from v1. It can be added later
-- for semantic history matching (e.g., "find searches similar to this one")
-- once there is a concrete use case and pipeline for populating/invalidating it.
CREATE TABLE search_history (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    query_text     TEXT NOT NULL,
    query_norm     TEXT NOT NULL,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    last_used_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    backend        TEXT NOT NULL,         -- 'jina'|'ollama'|'cosine-only'
    duration_ms    INTEGER,
    result_count   INTEGER DEFAULT 0,
    is_pinned      INTEGER NOT NULL DEFAULT 0,
    use_count      INTEGER NOT NULL DEFAULT 1,

    CHECK (is_pinned IN (0, 1))
);

-- Enforce exact-match dedup at the DB level (required for true upsert).
CREATE UNIQUE INDEX uq_search_history_query_norm ON search_history(query_norm);

CREATE INDEX idx_search_history_last_used_at ON search_history(last_used_at DESC);
CREATE INDEX idx_search_history_pinned ON search_history(is_pinned) WHERE is_pinned = 1;

-- Search result snapshot (soft reference to items; no FK on item_id).
CREATE TABLE search_results (
    search_id    INTEGER NOT NULL,
    rank         INTEGER NOT NULL,
    item_id      TEXT NOT NULL,
    cosine_score REAL,
    rerank_score REAL,
    PRIMARY KEY (search_id, rank),
    FOREIGN KEY (search_id) REFERENCES search_history(id) ON DELETE CASCADE
);
```

#### Key Decisions (synthesized)

- **Merged concept:** pinned views are simply **history rows with `is_pinned=1`**.
- **`query_norm` exact dedup:** collapse trivial variants (case/trim). Do **not** attempt semantic merging in v1.
- **Soft reference to `item_id`:** history/results remain even if items are later TTL-cleaned; dangling IDs are filtered at read time.
- **`ON DELETE CASCADE` for `search_results`:** deletion is explicit and fully removes the snapshot.
- **Indexes:** partial index for pinned rows; `last_used_at` index to keep history browsing fast as rows grow.
- **Operational note:** ensure `PRAGMA foreign_keys = ON;` in SQLite connections.

#### Store API

```go
// SaveSearch records a completed search.
// If query_norm already exists, it updates last_used_at, increments use_count,
// and refreshes duration_ms/result_count and the stored snapshot.
//
// Implementation note: store methods should accept context.Context and use
// db.ExecContext/db.QueryContext so that the per-query cancellation from Phase 0c
// propagates through to the database layer. Without this, cancelling a search in
// the UI won't interrupt a long-running SaveSearch transaction.
func (s *Store) SaveSearch(ctx context.Context, query, backend string, durationMs int, results []SearchResult) (int64, error)

type SearchResult struct {
    Rank        int
    ItemID      string
    CosineScore float64
    RerankScore float64
}

// GetSearchHistory returns entries for the history browser.
// Pinned searches are always included even if they exceed limit.
//
// Implementation note: accepts context.Context and uses db.QueryContext
// for cancellation support.
func (s *Store) GetSearchHistory(ctx context.Context, limit int) ([]SearchHistoryEntry, error)

type SearchHistoryEntry struct {
    ID          int64
    QueryText   string
    LastUsedAt  time.Time
    Backend     string
    DurationMs  int
    ResultCount int
    IsPinned    bool
    UseCount    int
}

func (s *Store) GetPinnedSearches(ctx context.Context) ([]SearchHistoryEntry, error)
func (s *Store) TogglePinned(ctx context.Context, searchID int64) error
func (s *Store) DeleteSearch(ctx context.Context, searchID int64) error

// GetSearchResults loads the stored snapshot, joining against items to
// drop dangling item_ids (items may have been TTL-deleted).
func (s *Store) GetSearchResults(ctx context.Context, searchID int64) ([]store.Item, error)
```

#### Implementation Notes

`SaveSearch` should be a single transaction:
1. Upsert `search_history` by `query_norm` (increment `use_count`, update `last_used_at`, etc.).
2. Delete prior snapshot rows for that `search_id`.
3. Insert new `search_results`.

Upsert sketch:

```sql
INSERT INTO search_history(query_text, query_norm, backend, duration_ms, result_count)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(query_norm) DO UPDATE SET
  query_text    = excluded.query_text,
  backend       = excluded.backend,
  duration_ms   = excluded.duration_ms,
  result_count  = excluded.result_count,
  last_used_at  = datetime('now'),
  use_count     = use_count + 1;
```

`GetSearchResults` should use an `INNER JOIN` on `items` so dangling refs vanish silently:

```sql
SELECT i.*
FROM search_results sr
JOIN items i ON i.id = sr.item_id
WHERE sr.search_id = ?
ORDER BY sr.rank ASC;
```

#### UI/UX

##### ModeHistory (Ctrl-R)

A lightweight history browser that supports re-run, pin/unpin, and delete.

```
+-  Search History ----------------------------------------+
| * climate risk         (42 results, 3m ago)              |
| * semiconductor supply (18 results, 1h ago)              |
|   ukraine conflict     (67 results, 2h ago)              |
|   interest rates       (31 results, yesterday)           |
|                                                          |
| Enter=re-run  p=pin/unpin  d=delete  /=filter            |
| Esc=close                                                |
+----------------------------------------------------------+
```

- `Ctrl-R` enters ModeHistory via `pushMode(ModeHistory)` and loads entries.
- `Enter`: re-executes `query_text` and transitions to results.
- `p`: toggles pinned state and refreshes list in place.
- `d`: deletes the history entry (no undo in v1; deletion is explicit).
- `/`: fuzzy filter on `query_text`.

**Iconography:** Use ASCII `*` for pinned by default for terminal width safety. Optional Unicode pin behind a config flag.

##### Pinned Search Tabs (ModeResults)

Pinned searches show as tabs in the status area while viewing results:

```
[All] [* climate risk] [* semiconductors]   Searching...
```

- `Tab` / `Shift-Tab`: cycles `[All]` + pinned tabs.
- Switching to a pinned tab re-runs the stored query.
- Key handling is mode-specific to avoid conflicts.

State:

```go
type App struct {
    // ...
    historyEntries []SearchHistoryEntry
    historyCursor  int
    historyFilter  string

    activePinnedIdx int // -1 = All, 0..len(pinned)-1
}
```

#### Query Semantics: History vs. Pinned Inclusion

`GetSearchHistory(limit)` should:
- Return pinned entries regardless of `limit`.
- Sort pinned first (stable UX), then recent unpinned by `last_used_at DESC`.

---

### Feature 6: Score Column Toggle

**Reviewer consensus:** All 3 want visible scores. GPT-5 praised the fixed-width formatting to avoid jitter. Grok-4 warned about View() performance at 60fps.

#### Design

Press `x` in ModeList or ModeResults to toggle a score column. The column shows the best available score for each item:

```
                                          Without scores:
+------------------------------------------------------+
| * Climate risk assessment gains traction    Reuters   |
|   Fed signals pause on rate hikes           AP News   |
|   SpaceX Starship test flight #7            Ars Tech  |
+------------------------------------------------------+

                                          With scores (x):
+------------------------------------------------------+
| 0.91 * Climate risk assessment gains traction Reuters |
| 0.84   Fed signals pause on rate hikes        AP News |
| 0.72   SpaceX Starship test flight #7        Ars Tech |
+------------------------------------------------------+
```

**Score precedence:** `rerank_score > cosine_score > no score (show "-")`. Explicitly exclude FTS5/BM25 scores (unbounded, confuses users).

**Implementation:**

```go
type App struct {
    // ... existing fields ...
    showScores      bool                  // toggled by 'x' key
    rerankScoreMap  map[string]float32    // O(1) lookup, populated on RerankComplete
    cosineScoreMap  map[string]float32    // O(1) lookup, precomputed when queryEmbedding changes
}
```

**Precomputing cosine scores:** When `queryEmbedding` changes (in `rerankItemsByEmbedding()` or `SearchPoolLoaded`), precompute cosine scores for all items with embeddings. This avoids recomputing cosine similarity on every `getBestScore()` call. At 60fps with 50 visible items, this eliminates up to 3000 cosine computations per second.

```go
// buildCosineScoreMap precomputes cosine similarity for all items that have
// embeddings. Called when queryEmbedding changes (in rerankItemsByEmbedding
// or SearchPoolLoaded handler). The map is cleared when the query changes
// or results are cleared.
func (a *App) buildCosineScoreMap() {
    if len(a.queryEmbedding) == 0 {
        a.cosineScoreMap = nil
        return
    }
    m := make(map[string]float32, len(a.embeddings))
    for id, emb := range a.embeddings {
        if len(emb) > 0 {
            m[id] = cosine(a.queryEmbedding, emb)
        }
    }
    a.cosineScoreMap = m
}
```

Call `buildCosineScoreMap()` at the end of `rerankItemsByEmbedding()` and in the `SearchPoolLoaded` handler after updating `a.embeddings`. Clear the map in `clearSearch()`.

**Rendering (in View):**

```go
func (a *App) renderItem(item store.Item, index int) string {
    var scoreStr string
    if a.showScores {
        score := a.getBestScore(item.ID)
        if score > 0 {
            scoreStr = fmt.Sprintf("%4.2f ", score) // fixed 5-char width: "0.91 "
        } else {
            scoreStr = "  -  " // same width, centered dash
        }
    }
    // ... rest of rendering
}

// getBestScore returns the best available score for an item.
// Uses only O(1) map lookups — no computation in the hot path.
func (a *App) getBestScore(itemID string) float32 {
    // 1. Rerank score (highest fidelity)
    if score, ok := a.rerankScoreMap[itemID]; ok {
        return score
    }
    // 2. Precomputed cosine score
    if score, ok := a.cosineScoreMap[itemID]; ok {
        return score
    }
    return 0
}
```

**Performance:** Both lookups are O(1) map reads. No cosine computation occurs in `getBestScore()` at render time. The cosine map is precomputed once when query embedding changes, ensuring zero jitter at 60fps regardless of visible item count.

---

## 5. Phase 3: Progressive UX (Reassess After Phase 2)

Phase 3 features are only implemented if Phase 2 ships successfully and the features still make sense in practice. Each has a "reassess" gate -- if Phase 1+2 solve the UX problem sufficiently, Phase 3 may be unnecessary.

### Feature 3: Progressive Search Pipeline (SIMPLIFIED)

**Original scope:** 4 subtasks (3a state machine, 3b step timeline, 3c smooth reorder animation, 3d progress bar with ETA).

**Revised scope:** 3a only. 3c and 3d are cut per reviewer consensus. 3b (step timeline in status bar) is replaced by the existing `statusText` pattern.

**Reassess gate:** After FTS5 (F7) ships, search results appear in 10-50ms. If users perceive search as "instant" with FTS5 + cosine, the progressive pipeline adds complexity without perceived benefit. Measure: if >80% of searches complete before the user scrolls, skip F3.

**If implemented:**

The progressive pipeline shows results as each stage completes:

1. **FTS5 results** (10-50ms) -- appear immediately after Enter
2. **Cosine results** (50-200ms) -- replace FTS results when embedding arrives
3. **Reranked results** (Jina: 200-500ms total / Ollama: manual R) -- final ordering

Each stage transition is a snap-sort (items reorder without animation). The `statusText` field shows the current stage:

```
Searching "climate"...          -> FTS results visible
Ranking "climate"...            -> cosine results visible
Reranking "climate" (12/30)...  -> cross-encoder in progress
(empty)                         -> final results
```

**State model:** No new enum. The existing pipeline booleans (`embeddingPending`, `rerankPending`) already track stages. The only addition is a `ftsResultsPending bool` for the FTS5 stage.

### Feature 10: Jina Reader (10a-10c only, 10d killed)

**Original scope:** 4 subtasks (10a fetch+render, 10b cache, 10c viewport, 10d chunk embeddings for search enrichment).

**Revised scope:** 10a-10c. 10d (chunk embeddings) is permanently cut -- "ROI is negative" per unanimous review.

**Reassess gate:** This feature is independent and low-coupling. Implement when there's a clear user need for reading articles inline rather than opening in browser. If `o` (open in browser) satisfies users, skip.

**If implemented:**

- `o` in ModeResults -> fetch URL via `r.jina.ai/{url}` -> render markdown in ModeArticle viewport
- Cache fetched articles in SQLite (`article_cache` table with TTL)
- ModeArticle is a scrollable viewport (Bubble Tea `viewport.Model`)
- `Esc` returns to ModeResults, `o` opens in system browser, `m` triggers MLT from article

**Schema (only created if feature enabled):**

```sql
CREATE TABLE IF NOT EXISTS article_cache (
    url         TEXT PRIMARY KEY,
    content_md  TEXT NOT NULL,       -- clean markdown from Jina Reader
    fetched_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    ttl_hours   INTEGER DEFAULT 24   -- cache TTL
);
```

---

## 6. Cut List (Permanent Record)

These features are permanently cut or indefinitely deferred. This list exists so future developers don't re-propose them without understanding the rationale.

| Feature | Status | Rationale | Reviewer Consensus |
|---------|--------|-----------|-------------------|
| **F4: Timeline Status Bar** | CUT | "Developer debugging info masquerading as a feature." The `obs events` CLI and debug overlay (`?` key) already serve this need. | Unanimous CUT |
| **F8: Filter Chips / Query Language** | DEFERRED | Premature for current user base. Keybinding collision with Tab (now used for pinned tab cycling). | Unanimous DEFER |
| **F10d: Search Enrichment / Chunk Embeddings** | CUT | "ROI is negative." Segmenting articles into chunks and embedding each is a research project, not a feature. | Unanimous CUT |
| **F3c: Smooth Reorder / Flash Animation** | CUT | "Animations in TUIs are finicky." Snap-sort is sufficient. | Unanimous CUT |
| **F3d: Progress Bar with ETA** | CUT | "Recomputes ETA 60fps in View()." The existing `statusText` pattern is sufficient. | 2/3 CUT |
| **F9c: Auto-Refresh Pinned Searches** | DEFERRED | "Refresh on view is sufficient." Manual refresh on tab switch is adequate. | 2/3 DEFER |
| **F9d: Persistent Tab Ordering** | DEFERRED | Adds schema/migration complexity for marginal benefit. Tabs sort alphabetically for now. | Implicit |

---

## 7. Testing Strategy

Testing enforces **phase gates**: unit tests must pass before advancing. All backends (Vectorizer, Reranker) use interfaces for deterministic mocking.

All async messages carry QueryID to discard stale results.

### Phase 0 Tests

| Test | Package | What It Verifies |
|------|---------|-----------------|
| `TestAppModeTransitions` | `ui` | Mode enum transitions: ModeList->ModeSearch->ModeResults->ModeList |
| `TestModeStack` | `ui` | pushMode/popMode: History->Esc returns to previous mode |
| `TestKeymapNoCollisions` | `ui` | No key collisions in reachable transitions |
| `TestGlobalKeys` | `ui` | Ctrl-C and `?` handled before mode dispatch |
| `TestSearchContextCancellation` | `ui` | New search cancels prior context; Esc cancels current; queryID invalidated |
| `TestFeatureFlagGating` | `ui` | Disabled features ignore keys |
| `TestConfigValidation` | `ui` | Feature flags with missing dependencies return error |
| `TestView_EmptyState_NoPanic` | `ui` | View() handles nil/empty lists, out-of-bounds index |

### Phase 1 Tests

| Test | Package | What It Verifies |
|------|---------|-----------------|
| `TestMoreLikeThis_FastPath` | `ui` | Item with embedding: immediate cosine rank, seed excluded |
| `TestMoreLikeThis_NoEmbedding` | `ui` | Item without embedding: silent no-op |
| `TestMoreLikeThis_Chain` | `ui` | m from ModeResults pivots to new seed, savedItems preserved |
| `TestMoreLikeThis_Esc` | `ui` | Esc restores savedItems |
| `TestMLTOllamaRerank` | `ui` | MLT -> `R` with `activeQuery` set to seed title text |
| `TestMLTModeTransition` | `ui` | Post-MLT keys route via `handleResultsKeys` (not `handleListKeys`) |
| `TestEscTwoStage` | `ui` | First Esc cancels in-flight rerank (stays in ModeResults), second Esc exits results |
| `TestEnsureSnapshot` | `ui` | `savedItems` snapshot only taken once across search/MLT chains; all entry points guard on `savedItems == nil` |
| `TestAutoReranks_Jina` | `ui` | Jina backend auto-triggers rerank after search |
| `TestAutoReranks_Ollama` | `ui` | Ollama backend shows cosine-only, R key triggers rerank |
| `TestFTS5_SearchFTS` | `store` | FTS5 query returns matching items by title/summary/author |
| `TestFTS5_SearchFTS_SyntaxError` | `store` | Bad FTS syntax retries as quoted literal |
| `TestFTS5_TriggerSync` | `store` | INSERT into items -> FTS5 index updated |
| `TestFTS5_RebuildConditional` | `store` | Rebuild only runs when FTS is empty but items exist |
| `TestFTS5_Migration` | `store` | Existing items populated into FTS on first run |
| `TestSearchFlow_FTSFirst` | `ui` | FTS results shown immediately, replaced by cosine on embed complete |

### Phase 2 Tests

| Test | Package | What It Verifies |
|------|---------|-----------------|
| `TestSaveSearch_Dedup` | `store` | Same query_norm -> update, not insert |
| `TestSaveSearch_Results` | `store` | Results stored/retrievable in rank order |
| `TestGetSearchHistory` | `store` | Sorted by last_used_at DESC; pinned included |
| `TestTogglePinned` | `store` | Flip pin; reflected in GetPinnedSearches |
| `TestDeleteSearch_Cascade` | `store` | Delete -> results cleaned up |
| `TestSoftReference` | `store` | Item deleted -> results survive, item filtered |
| `TestModeHistory_Navigation` | `ui` | Ctrl-R opens, Enter re-executes, Esc closes |
| `TestCancelOnModePop` | `ui` | History Esc cancels any in-flight search before returning to previous mode |
| `TestHistoryPinnedSort` | `ui` | Pinned entries sort first (stable), then recent unpinned by last_used_at |
| `TestScoreColumn_Toggle` | `ui` | `x` toggles `showScores`; fixed-width renders |
| `TestScoreColumn_Precedence` | `ui` | Rerank map > cosine map > dash |
| `TestScoreColumn_MapPopulation` | `ui` | RerankComplete populates `rerankScoreMap` |
| `TestScoreColumn_CosineMapPopulation` | `ui` | `buildCosineScoreMap()` called when queryEmbedding changes; map populated correctly |
| `TestScoreColumn_CosineFallbackMLT` | `ui` | MLT path populates cosineScoreMap; getBestScore uses precomputed map (no per-call cosine) |
| `TestScoreColumn_StaleDiscard` | `ui` | Stale RerankComplete (wrong QueryID) ignored |

### Phase 3 Tests

| Test | Package | What It Verifies |
|------|---------|-----------------|
| `TestProgressivePipeline_Stages` | `ui` | FTS -> cosine -> rerank displayed progressively |
| `TestJinaReader_Fetch` | `reader` | URL -> clean markdown via Reader API |
| `TestJinaReader_Cache` | `store` | Second fetch for same URL returns cached content |

### Test Helpers

```go
// newTestApp: Synchronous mocks via interfaces
func newTestApp(opts ...testOpt) App { /* ... */ }

type testOpt func(*AppConfig)

func withVectorizer(v Vectorizer) testOpt { /* inject mock */ }
func withReranker(r Reranker) testOpt { /* inject mock */ }

// teaRunner executes a message through the model's Update loop, chasing
// returned Cmds until completion or step limit.
//
// maxSteps prevents infinite loops from spinner ticks or other recurring Cmds.
// skipCmd filters out Cmds that should not be chased (e.g., spinner.Tick,
// timer ticks) to avoid infinite loops in test.
func teaRunner(model tea.Model, msg tea.Msg, maxSteps int, skipCmd func(tea.Msg) bool) (tea.Model, tea.Cmd) {
    for step := 0; step < maxSteps; step++ {
        var cmd tea.Cmd
        model, cmd = model.Update(msg)
        if cmd == nil {
            return model, nil
        }
        msg = cmd() // Execute Cmd -> Msg
        if skipCmd != nil && skipCmd(msg) {
            // Skip spinner/timer messages that would loop forever
            continue
        }
    }
    return model, nil // Step limit reached
}

// skipSpinnerTicks is a standard skipCmd filter for teaRunner that
// skips spinner.TickMsg and timer messages.
func skipSpinnerTicks(msg tea.Msg) bool {
    switch msg.(type) {
    case spinner.TickMsg:
        return true
    }
    return false
}
```

Usage:

```go
func TestMoreLikeThis_FastPath(t *testing.T) {
    app := newTestApp(withEmbeddings(mockEmbeddings))
    // Send 'm' key
    result, _ := teaRunner(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}}, 100, skipSpinnerTicks)
    got := result.(App)
    assert.Equal(t, ModeResults, got.mode)
    assert.NotEmpty(t, got.mltSeedID)
}
```

**Coverage Goal:** 90%+ on `ui` package; all async paths exercised with mocks.

---

## 8. Dependency Graph

```
Phase 0 (prerequisites -- single PR)
+-- 0a. AppMode Enum + Mode Stack
+-- 0b. Global Keymap + Global Key Handler
+-- 0c. Per-Query Context Cancellation
+-- 0d. Feature Flags + Config Validation
        |
        v
Phase 1 (core search -- can parallelize after Phase 0)
+-- F1: More Like This ----------------+
|   (depends on: 0a, 0b, 0c)          |
+-- F2: Opt-in Ollama Rerank           | independent
|   (depends on: 0b, 0d)              |
+-- F7: FTS5 Instant Lexical ----------+
    (depends on: 0a, 0c, 0d)
        |
        v
Phase 2 (persistence -- after Phase 1 stable)
+-- F5+9: Search History + Pinned -----+
|   (depends on: Phase 0)             | independent
+-- F6: Score Column Toggle -----------+
    (depends on: F1 or F2 scores)
        |
        v
Phase 3 (reassess gate)
+-- F3: Progressive Pipeline
|   (depends on: F7 + F2)
+-- F10: Jina Reader
    (independent)
```

**Critical path:** Phase 0 -> F7 (FTS5) -> F3 (Progressive)

**Parallel opportunities:**
- F1, F2, F7 can all start after Phase 0 (independent of each other)
- F5+9 can start after Phase 0 (it only needs AppMode and the store; no dependency on F7's FTS tables)
- F6 can start after any Phase 1 feature that produces scores ships
- F10 can start at any time (fully independent)
