# Observer Observability System: Architecture, Design, and Implementation Outline

## Priorities 1-5

---

## Table of Contents

1. [Priority 1: Structured JSONL Events](#priority-1-structured-jsonl-events)
2. [Priority 2: Ring Buffer in App Struct](#priority-2-ring-buffer-in-app-struct)
3. [Priority 3: Debug Overlay on Toggle Key](#priority-3-debug-overlay-on-toggle-key)
4. [Priority 4: Query ID on All Search Messages](#priority-4-query-id-on-all-search-messages)
5. [Priority 5: OBSERVER_TRACE Env Var](#priority-5-observer_trace-env-var)
6. [Cross-Cutting Concerns](#cross-cutting-concerns)
7. [Critical Files for Implementation](#critical-files-for-implementation)

---

## Codebase Inventory: Current log.Printf Sites

Before designing replacements, here is every `log.Printf` call in the active codebase (excluding `archive/` and standalone CLI tools):

| File | Line | Current log.Printf | Event Type |
|------|------|-------------------|------------|
| `internal/ui/app.go` | 227 | `search: query embedded in %dms (%d dims)` | search.query_embedded |
| `internal/ui/app.go` | 231 | `search: cosine rerank applied in %dms (%d items)` | search.cosine_ranked |
| `internal/ui/app.go` | 260 | `search: pool loaded in %dms (%d items, %d embeddings)` | search.pool_loaded |
| `internal/ui/app.go` | 281 | `search: cross-encoder rerank complete in %dms` | search.rerank_complete |
| `internal/ui/app.go` | 571 | `search: starting cross-encoder rerank (%d items, batch=%v)` | search.rerank_started |
| `internal/ui/app.go` | 617 | `search: per-entry rerank complete in %dms (%d entries)` | search.rerank_complete |
| `internal/coord/coordinator.go` | 178 | `coord: skipping embedding for %s (empty text)` | embed.skipped |
| `internal/coord/coordinator.go` | 195 | `coord: batch embedding failed, falling back to sequential: %v` | embed.batch_failed |
| `internal/coord/coordinator.go` | 204 | `coord: failed to save embedding for %s: %v` | embed.save_failed |
| `internal/coord/coordinator.go` | 223 | `coord: failed to embed %s: %v` | embed.failed |
| `internal/coord/coordinator.go` | 228 | `coord: failed to save embedding for %s: %v` | embed.save_failed |
| `internal/coord/coordinator.go` | 246 | `coord: failed to save items: %v` | fetch.save_failed |
| `internal/fetch/clarion.go` | 40 | `fetch: %s: %v` | fetch.source_error |
| `cmd/observer/main.go` | 98 | `Warning: failed to get embeddings: %v` | store.embeddings_failed |
| `cmd/observer/main.go` | 132 | `Warning: failed to get embeddings: %v` | store.embeddings_failed |
| `cmd/observer/main.go` | 166 | `Warning: failed to get embeddings for search pool: %v` | store.embeddings_failed |
| `cmd/observer/main.go` | 239 | `Error running program: %v` | app.error |

---

## Priority 1: Structured JSONL Events

### Goal

Replace all ad-hoc `log.Printf` calls with typed event structs serialized as JSONL to `~/.observer/observer.log`. Every event becomes machine-parseable, greppable, and suitable for downstream tooling.

### Package/File Placement

**New package:** `internal/obs/` (short for "observability")

**New files:**
- `internal/obs/event.go` — Event type definitions, constants, and the `Emit` function
- `internal/obs/logger.go` — Logger initialization, file writer, JSONL serialization
- `internal/obs/obs_test.go` — Tests for serialization and logger

### Type Definitions

```go
// Package obs provides structured observability for Observer.
// Events are written as JSONL to the log file and to an optional ring buffer.
package obs

import (
    "encoding/json"
    "io"
    "sync"
    "time"
)

// Event is a structured observability event.
// All fields are serialized to JSONL. The Data map holds event-specific fields.
type Event struct {
    Time  time.Time      `json:"ts"`
    Type  string         `json:"event"`
    Data  map[string]any `json:"data,omitempty"`
}

// Common event type constants.
const (
    // Search pipeline events
    EventSearchStarted        = "search.started"
    EventSearchQueryEmbedded  = "search.query_embedded"
    EventSearchCosineRanked   = "search.cosine_ranked"
    EventSearchPoolLoaded     = "search.pool_loaded"
    EventSearchRerankStarted  = "search.rerank_started"
    EventSearchRerankComplete = "search.rerank_complete"

    // Embedding events
    EventEmbedSkipped     = "embed.skipped"
    EventEmbedBatchFailed = "embed.batch_failed"
    EventEmbedFailed      = "embed.failed"
    EventEmbedSaveFailed  = "embed.save_failed"
    EventEmbedComplete    = "embed.complete"

    // Fetch events
    EventFetchStarted     = "fetch.started"
    EventFetchComplete    = "fetch.complete"
    EventFetchSourceError = "fetch.source_error"
    EventFetchSaveFailed  = "fetch.save_failed"

    // Store events
    EventStoreEmbeddingsFailed = "store.embeddings_failed"

    // App lifecycle events
    EventAppStartup  = "app.startup"
    EventAppShutdown = "app.shutdown"
    EventAppError    = "app.error"
)

// Logger writes structured events to a writer (the log file).
// Thread-safe: multiple goroutines (coordinator, UI) write concurrently.
type Logger struct {
    mu      sync.Mutex
    w       io.Writer
    encoder *json.Encoder
    buf     *RingBuffer // optional: nil if no ring buffer
}

// NewLogger creates a Logger that writes JSONL to w.
// The ring buffer is optional (pass nil to disable in-memory buffering).
func NewLogger(w io.Writer, buf *RingBuffer) *Logger {
    return &Logger{
        w:       w,
        encoder: json.NewEncoder(w),
        buf:     buf,
    }
}

// Emit writes a structured event to both the log file and the ring buffer.
// Safe to call on a nil Logger (no-op). This nil-receiver pattern means
// tests never need to construct a real logger.
func (l *Logger) Emit(eventType string, data map[string]any) {
    if l == nil {
        return
    }

    ev := Event{
        Time: time.Now(),
        Type: eventType,
        Data: data,
    }

    l.mu.Lock()
    l.encoder.Encode(ev) // JSONL: one JSON object per line (Encode adds newline)
    l.mu.Unlock()

    if l.buf != nil {
        l.buf.Push(ev)
    }
}

// D is a convenience alias for map[string]any to reduce verbosity at call sites.
type D = map[string]any
```

### Integration Points

**1. `cmd/observer/main.go` — Logger initialization**

Replace `tea.LogToFile` with a custom file open. The key change: `tea.LogToFile` sets up Go's stdlib `log` package. We keep that for Bubble Tea's internal logging but add our own JSONL logger alongside it.

```go
// In main():
logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
if err != nil {
    log.Fatalf("Failed to open log file: %v", err)
}
defer logFile.Close()

// Keep stdlib log pointing to the file for Bubble Tea internals
log.SetOutput(logFile)
log.SetFlags(0) // No prefix/timestamp — JSONL handles its own timestamps

// Create structured logger
logger := obs.NewLogger(logFile, nil) // ring buffer added in Priority 2

// Pass logger into coordinator and UI via dependency injection
```

**2. `internal/coord/coordinator.go` — Add logger field**

```go
type Coordinator struct {
    store    *store.Store
    provider Provider
    embedder embed.Embedder
    logger   *obs.Logger // NEW
    wg       sync.WaitGroup
}

func NewCoordinator(s *store.Store, p Provider, e embed.Embedder, l *obs.Logger) *Coordinator {
    return &Coordinator{
        store:    s,
        provider: p,
        embedder: e,
        logger:   l,
    }
}
```

Replace each `log.Printf` in coordinator.go:

```go
// Before:
log.Printf("coord: batch embedding failed, falling back to sequential: %v", err)
// After:
c.logger.Emit(obs.EventEmbedBatchFailed, obs.D{"error": err.Error()})
```

**3. `internal/ui/app.go` — Add logger field to App and AppConfig**

```go
type App struct {
    // ... existing fields ...
    logger *obs.Logger // NEW: structured event logger
}

type AppConfig struct {
    // ... existing fields ...
    Logger *obs.Logger
}
```

Replace each `log.Printf` in app.go:

```go
// Before:
log.Printf("search: query embedded in %dms (%d dims)",
    time.Since(a.searchStart).Milliseconds(), len(msg.Embedding))
// After:
a.logger.Emit(obs.EventSearchQueryEmbedded, obs.D{
    "query":      msg.Query,
    "latency_ms": time.Since(a.searchStart).Milliseconds(),
    "dims":       len(msg.Embedding),
})
```

**4. `internal/fetch/clarion.go` — Accept logger parameter**

The `ClarionProvider` gains a logger field:

```go
type ClarionProvider struct {
    sources []clarion.Source
    opts    clarion.FetchOptions
    logger  *obs.Logger
}
```

**5. `cmd/observer/main.go` closures — Replace log.Printf in anonymous functions**

The closures in `main.go` (LoadRecentItems, LoadItems, LoadSearchPool) currently call `log.Printf`. These capture the logger from the outer scope:

```go
// Before:
log.Printf("Warning: failed to get embeddings: %v", err)
// After:
logger.Emit(obs.EventStoreEmbeddingsFailed, obs.D{"error": err.Error(), "context": "load_recent"})
```

### Implementation Steps

1. Create `internal/obs/event.go` with `Event` struct, constants, `D` type alias
2. Create `internal/obs/logger.go` with `Logger` struct, `NewLogger`, `Emit`
3. Create `internal/obs/obs_test.go` — test JSONL serialization roundtrip, concurrent Emit safety
4. Add `Logger` field to `Coordinator`, update `NewCoordinator` signature
5. Add `Logger` field to `App` and `AppConfig`, update `NewApp`/`NewAppWithConfig`
6. Add `Logger` field to `ClarionProvider`, update `NewClarionProvider`
7. Replace all 17 `log.Printf` calls with `logger.Emit(...)` calls
8. Update `main.go` to create logger, pass to all components
9. Add `EventAppStartup` emit at start, `EventAppShutdown` emit at end of main
10. Run `go test ./...` to verify nothing breaks

### Key Design Decisions

**Why a new `internal/obs` package instead of using stdlib `log/slog`?** Go 1.21+ has `slog`, and Go 1.24 supports it. However, `slog` is designed for general structured logging. Our events have a specific schema (`Event` struct with `Type` + `Data` map) and dual-destination (file + ring buffer). A thin custom package gives us full control over the wire format and the ring buffer integration without fighting `slog`'s Handler abstraction. The package is ~80 lines.

**Why `map[string]any` for Data instead of per-event structs?** Per-event structs (e.g., `SearchStartedEvent`, `EmbedFailedEvent`) provide type safety but create 17+ types and require a type switch for every consumer. The `map[string]any` approach keeps the event type as a string discriminator and the data as a schemaless bag. This is the pattern used by JSONL event systems (structured logging, analytics pipelines). Type safety for event fields is a future concern; getting events structured at all is the immediate win.

**Why `D` type alias?** `obs.D{"query": "climate", "latency_ms": 142}` is significantly more readable than `map[string]any{"query": "climate", "latency_ms": 142}` at every call site. This follows the MongoDB driver's `bson.D` convention.

**Why mutex on Logger instead of channel?** The Logger is write-only from multiple goroutines. A mutex on `Encode` is simpler and lower-latency than a channel+goroutine pattern. JSON encoding is fast (~1us per event), so contention is negligible.

**Why nil-receiver pattern on Emit?** Calling `Emit` on a nil `*Logger` is safe and returns immediately. This means any component with an optional logger (tests, standalone tools) does not need to construct a discard logger — just pass `nil`. This is idiomatic Go (used by `*log.Logger` and others).

---

## Priority 2: Ring Buffer in App Struct

### Goal

An in-memory circular buffer (1024 events) available to both the UI (for the debug overlay) and the coordinator (for writing events from background goroutines). Zero allocation cost when nobody is reading from it.

### Package/File Placement

**New file:** `internal/obs/ringbuffer.go`
**New file:** `internal/obs/ringbuffer_test.go`

The ring buffer lives in the `obs` package because it is tightly coupled to the `Event` type and the `Logger` uses it.

### Type Definitions

```go
// ringbuffer.go

package obs

import "sync"

const DefaultRingSize = 1024

// RingBuffer is a fixed-size circular buffer of Events.
// Thread-safe: the coordinator pushes events from background goroutines,
// the UI reads them on the main goroutine.
type RingBuffer struct {
    mu     sync.RWMutex
    events [DefaultRingSize]Event
    head   int  // next write position
    count  int  // total events written (monotonic, used for "items in buffer")
    len    int  // min(count, DefaultRingSize) — current number of valid events
}

// NewRingBuffer creates an empty RingBuffer.
func NewRingBuffer() *RingBuffer {
    return &RingBuffer{}
}

// Push adds an event to the buffer, overwriting the oldest if full.
func (rb *RingBuffer) Push(ev Event) {
    rb.mu.Lock()
    rb.events[rb.head] = ev
    rb.head = (rb.head + 1) % DefaultRingSize
    rb.count++
    if rb.len < DefaultRingSize {
        rb.len++
    }
    rb.mu.Unlock()
}

// Len returns the number of events currently in the buffer.
func (rb *RingBuffer) Len() int {
    rb.mu.RLock()
    defer rb.mu.RUnlock()
    return rb.len
}

// TotalCount returns the total number of events ever pushed (monotonic counter).
func (rb *RingBuffer) TotalCount() int {
    rb.mu.RLock()
    defer rb.mu.RUnlock()
    return rb.count
}

// Last returns the most recent n events in chronological order (oldest first).
// If n > Len(), returns all available events.
// Returns a new slice; caller owns the result.
func (rb *RingBuffer) Last(n int) []Event {
    rb.mu.RLock()
    defer rb.mu.RUnlock()

    if n > rb.len {
        n = rb.len
    }
    if n == 0 {
        return nil
    }

    result := make([]Event, n)
    // Start position: head - n (wrapping)
    start := (rb.head - n + DefaultRingSize) % DefaultRingSize
    for i := 0; i < n; i++ {
        result[i] = rb.events[(start+i)%DefaultRingSize]
    }
    return result
}

// LastOfType returns the most recent event matching the given type, or nil.
func (rb *RingBuffer) LastOfType(eventType string) *Event {
    rb.mu.RLock()
    defer rb.mu.RUnlock()

    // Walk backward from head
    for i := 0; i < rb.len; i++ {
        idx := (rb.head - 1 - i + DefaultRingSize) % DefaultRingSize
        if rb.events[idx].Type == eventType {
            ev := rb.events[idx]
            return &ev
        }
    }
    return nil
}

// CountByType returns counts of each event type in the buffer.
// Useful for the debug overlay summary.
func (rb *RingBuffer) CountByType() map[string]int {
    rb.mu.RLock()
    defer rb.mu.RUnlock()

    counts := make(map[string]int)
    for i := 0; i < rb.len; i++ {
        idx := (rb.head - 1 - i + DefaultRingSize) % DefaultRingSize
        counts[rb.events[idx].Type]++
    }
    return counts
}
```

### Integration Points

**1. `cmd/observer/main.go` — Create ring buffer and pass to Logger**

```go
// In main():
ringBuf := obs.NewRingBuffer()
logger := obs.NewLogger(logFile, ringBuf)

// Pass ring buffer to App via AppConfig
cfg := ui.AppConfig{
    // ... existing fields ...
    Logger:     logger,
    RingBuffer: ringBuf,
}
```

**2. `internal/ui/app.go` — Store ring buffer reference**

```go
type App struct {
    // ... existing fields ...
    logger   *obs.Logger
    ringBuf  *obs.RingBuffer // for debug overlay reads
}

type AppConfig struct {
    // ... existing fields ...
    Logger     *obs.Logger
    RingBuffer *obs.RingBuffer
}
```

**3. Logger.Emit — Dual write**

Already designed in Priority 1. The `Emit` method writes to both the file (via encoder) and the ring buffer (via `Push`). This is the only write path — no separate ring buffer population needed.

### Implementation Steps

1. Create `internal/obs/ringbuffer.go` with `RingBuffer` struct and methods
2. Create `internal/obs/ringbuffer_test.go`:
   - Test Push/Last with fewer than DefaultRingSize events
   - Test wraparound when buffer is full
   - Test concurrent Push from multiple goroutines (race detector)
   - Test LastOfType
   - Test CountByType
   - Test Len and TotalCount
3. Update `Logger` in `logger.go` to call `rb.Push` in `Emit`
4. Add `RingBuffer` field to `AppConfig` and `App`
5. Create ring buffer in `main.go` and wire it through
6. Run `go test -race ./...`

### Key Design Decisions

**Why a fixed array `[1024]Event` instead of a slice?** A fixed array means zero heap allocations for the buffer itself. The `Event` struct contains a `map[string]any` which does allocate, but the ring structure is allocation-free. This is the "zero cost when unviewed" property: pushing events into the ring is a struct copy + map pointer copy, not a slice append.

**Why `sync.RWMutex` instead of lock-free?** The ring buffer has many writers (coordinator goroutines, embedding worker, UI goroutine) and one reader (the debug overlay, read on every `View()` call when visible). `RWMutex` allows concurrent reads from `Last()` and `CountByType()` without blocking writers. Lock-free ring buffers are more complex and harder to get right with Go's memory model; the mutex overhead is ~50ns per operation, negligible compared to the 16ms frame budget.

**Why `Last(n)` returns a copy?** The ring buffer is written to by background goroutines. If `Last` returned a slice into the internal array, the caller would see data races as new events overwrite entries. Copying `n` events (each ~200 bytes with a small Data map) is cheap for `n <= 50` (the debug overlay's typical read).

**Why 1024 instead of 1000?** Power of two allows modulo operations to compile to bitwise AND (`& 1023`), which is a micro-optimization but costs nothing in code complexity.

---

## Priority 3: Debug Overlay on Toggle Key

### Goal

A keystroke-toggled panel showing live pipeline stats read from the ring buffer. Pressing the toggle key shows/hides the overlay without disrupting normal TUI operation.

### Package/File Placement

**New file:** `internal/ui/debug.go` — Debug overlay rendering
**Modified file:** `internal/ui/app.go` — Toggle state, View() integration, key handling
**Modified file:** `internal/ui/styles.go` — Debug overlay styles

### Type Definitions

```go
// debug.go

package ui

import (
    "fmt"
    "strings"
    "time"

    "observer/internal/obs"
    "github.com/charmbracelet/lipgloss"
)

// debugState holds transient state for the debug overlay.
// Computed fresh from the ring buffer on each View() call.
type debugState struct {
    totalEvents    int
    bufferLen      int
    countByType    map[string]int
    lastSearch     *obs.Event
    lastFetch      *obs.Event
    lastEmbed      *obs.Event
    recentEvents   []obs.Event
}

// computeDebugState reads from the ring buffer and builds a snapshot.
// Called only when the debug overlay is visible.
func computeDebugState(rb *obs.RingBuffer) debugState {
    if rb == nil {
        return debugState{}
    }
    return debugState{
        totalEvents:  rb.TotalCount(),
        bufferLen:    rb.Len(),
        countByType:  rb.CountByType(),
        lastSearch:   rb.LastOfType(obs.EventSearchRerankComplete),
        lastFetch:    rb.LastOfType(obs.EventFetchComplete),
        lastEmbed:    rb.LastOfType(obs.EventEmbedComplete),
        recentEvents: rb.Last(10),
    }
}

// renderDebugOverlay renders the debug panel as a string.
// Takes the full terminal width and a max height for the overlay.
func renderDebugOverlay(ds debugState, width, maxHeight int) string {
    var sections []string

    // Section 1: Summary line
    summary := fmt.Sprintf("Events: %d total, %d in buffer", ds.totalEvents, ds.bufferLen)
    sections = append(sections, summary)

    // Section 2: Last search timing
    if ds.lastSearch != nil {
        data := ds.lastSearch.Data
        latency := ""
        if ms, ok := data["latency_ms"]; ok {
            latency = fmt.Sprintf(" %vms", ms)
        }
        query := ""
        if q, ok := data["query"]; ok {
            query = fmt.Sprintf(" \"%v\"", q)
        }
        sections = append(sections, fmt.Sprintf("Last search:%s%s", query, latency))
    }

    // Section 3: Event type counts (top 5)
    if len(ds.countByType) > 0 {
        var counts []string
        for typ, count := range ds.countByType {
            short := typ
            if idx := strings.LastIndex(typ, "."); idx >= 0 {
                short = typ[idx+1:]
            }
            counts = append(counts, fmt.Sprintf("%s:%d", short, count))
        }
        if len(counts) > 5 {
            counts = counts[:5]
        }
        sections = append(sections, strings.Join(counts, " | "))
    }

    // Section 4: Recent events (last 5)
    numRecent := 5
    if len(ds.recentEvents) < numRecent {
        numRecent = len(ds.recentEvents)
    }
    if numRecent > 0 {
        sections = append(sections, "--- Recent ---")
        for i := len(ds.recentEvents) - numRecent; i < len(ds.recentEvents); i++ {
            ev := ds.recentEvents[i]
            age := time.Since(ev.Time)
            ageStr := formatAge(age)
            line := fmt.Sprintf("%s %s", ageStr, ev.Type)
            if q, ok := ev.Data["query"]; ok {
                line += fmt.Sprintf(" q=%v", q)
            }
            if ms, ok := ev.Data["latency_ms"]; ok {
                line += fmt.Sprintf(" %vms", ms)
            }
            if e, ok := ev.Data["error"]; ok {
                line += fmt.Sprintf(" err=%v", e)
            }
            sections = append(sections, line)
        }
    }

    content := strings.Join(sections, "\n")
    return DebugOverlay.Width(width - 4).MaxHeight(maxHeight).Render(content)
}

// formatAge formats a duration as a compact human string.
func formatAge(d time.Duration) string {
    switch {
    case d < time.Second:
        return fmt.Sprintf("%dms", d.Milliseconds())
    case d < time.Minute:
        return fmt.Sprintf("%ds", int(d.Seconds()))
    case d < time.Hour:
        return fmt.Sprintf("%dm", int(d.Minutes()))
    default:
        return fmt.Sprintf("%dh", int(d.Hours()))
    }
}
```

### Styles Addition

Add to `internal/ui/styles.go`:

```go
// DebugOverlay style for the debug panel.
var DebugOverlay = lipgloss.NewStyle().
    Foreground(lipgloss.Color("252")).
    Background(lipgloss.Color("234")).
    Border(lipgloss.RoundedBorder()).
    BorderForeground(lipgloss.Color("240")).
    Padding(0, 1).
    MarginTop(0)

// DebugLabel style for the "DEBUG" label.
var DebugLabel = lipgloss.NewStyle().
    Foreground(lipgloss.Color("208")).
    Bold(true)
```

### Integration into App

**New field in App:**

```go
type App struct {
    // ... existing fields ...
    debugVisible bool // toggle for debug overlay
}
```

**Key handling — add `?` as the toggle key:**

In `handleKeyMsg`, in the normal mode key handling (after search/embedding/rerank guards), add:

```go
case "?":
    a.debugVisible = !a.debugVisible
    return a, nil
```

The `?` key is currently unbound and is conventional for help/debug in TUI apps (less, vim, mutt).

**View() integration:**

The debug overlay renders as a fixed-height panel at the top of the screen, pushing content down. This avoids z-order/overlap issues with lipgloss (which does not support true layering).

```go
func (a App) View() string {
    if !a.ready {
        return "Loading..."
    }

    // Debug overlay steals height from content area
    debugHeight := 0
    debugPanel := ""
    if a.debugVisible && a.ringBuf != nil {
        maxDebugHeight := 12 // cap: ~12 lines
        ds := computeDebugState(a.ringBuf)
        debugPanel = renderDebugOverlay(ds, a.width, maxDebugHeight)
        debugHeight = lipgloss.Height(debugPanel)
    }

    contentHeight := a.height - 1 - debugHeight
    // ... rest of existing View() logic, using contentHeight ...

    // Compose: debug + stream + errorBar + searchBar + statusBar
    return debugPanel + stream + errorBar + searchBar + statusBar
}
```

**Status bar hint:**

```go
// In RenderStatusBar, add to keys:
StatusBarKey.Render("?") + StatusBarText.Render(":debug"),
```

### Implementation Steps

1. Add `DebugOverlay` and `DebugLabel` styles to `styles.go`
2. Create `internal/ui/debug.go` with `debugState`, `computeDebugState`, `renderDebugOverlay`, `formatAge`
3. Add `debugVisible` field to `App` struct
4. Add `"?"` key handler in `handleKeyMsg` normal mode
5. Integrate debug panel into `View()` — render at top, subtract height from content area
6. Add `?:debug` hint to status bar key hints
7. Create `internal/ui/debug_test.go`:
   - Test `computeDebugState` with empty ring buffer
   - Test `computeDebugState` with populated ring buffer
   - Test `renderDebugOverlay` produces non-empty string
   - Test `formatAge` edge cases
   - Test toggle: press `?` sets `debugVisible`, press again clears it
   - Test `View()` output changes when `debugVisible` is true
8. Run `go test ./...`

### Key Design Decisions

**Why `?` and not F12 or backtick?** F12 is not reliably captured by all terminal emulators (some terminals intercept it). Backtick conflicts with potential future keybindings. `?` is conventional for "help" in terminal applications (less, vim, mutt all use it). It is currently unbound in Observer.

**Why top-of-screen instead of bottom or overlay?** The status bar is at the bottom and is always visible. Putting the debug panel at the bottom would push the status bar up or require z-ordering, which lipgloss does not support cleanly. Top-of-screen is natural: the debug panel pushes content down, and the content area simply gets shorter. This matches how vim's command window works.

**Why recompute `debugState` on every View()?** `View()` is called at ~60fps when the spinner is active, ~0fps when idle. Computing `debugState` involves 4 ring buffer reads (each acquiring an RLock briefly). At 60fps this is 240 lock acquisitions per second, each taking ~50ns. Total overhead: ~12 microseconds per second — negligible. Caching would add staleness and complexity.

**Why not a separate Bubble Tea model/component?** The debug overlay is a simple render function, not an interactive widget. It has no input handling, no sub-models, no commands. Making it a separate `tea.Model` would add `Init`/`Update`/`View` boilerplate for zero benefit. A render function that reads from the ring buffer is simpler and follows the existing pattern where all rendering is done by functions called from `App.View()`.

---

## Priority 4: Query ID on All Search Messages

### Goal

Add a correlation ID to every search-related event so events belonging to the same search can be grouped. This enables tooling to reconstruct the full timeline of a single search across embed, pool load, cosine rank, and cross-encoder rerank stages.

### Package/File Placement

**Modified files:**
- `internal/ui/messages.go` — Add QueryID field to all search messages
- `internal/ui/app.go` — Generate QueryID in `submitSearch`, propagate through handlers
- `cmd/observer/main.go` — Thread QueryID through closure-based commands

**New file:**
- `internal/obs/queryid.go` — QueryID generation

### Type Definitions

```go
// queryid.go (in internal/obs/)

package obs

import (
    "fmt"
    "sync/atomic"
)

// querySeq is a monotonic counter for query IDs within a session.
var querySeq atomic.Uint64

// NewQueryID returns a unique query identifier for the current session.
// Format: "q1", "q2", etc. Monotonically increasing, never reused.
// The short format is intentional: query IDs appear in every search event
// and in log grep output. "q42" is more readable than a UUID.
func NewQueryID() string {
    seq := querySeq.Add(1)
    return fmt.Sprintf("q%d", seq)
}
```

**Updated message types in `messages.go`:**

```go
// QueryEmbedded is sent when a filter query has been embedded.
type QueryEmbedded struct {
    QueryID   string    // correlation ID for this search
    Query     string
    Embedding []float32
    Err       error
}

// EntryReranked is sent when a single entry has been scored by the cross-encoder.
type EntryReranked struct {
    QueryID string  // correlation ID for this search
    Index   int
    Score   float32
    Err     error
}

// RerankComplete is sent when batch reranking finishes (Jina API path).
type RerankComplete struct {
    QueryID string    // correlation ID for this search
    Query   string
    Scores  []float32
    Err     error
}

// SearchPoolLoaded is sent when the full item pool for search is ready.
type SearchPoolLoaded struct {
    QueryID    string   // correlation ID for this search
    Items      []store.Item
    Embeddings map[string][]float32
    Err        error
}
```

**New field in App:**

```go
type App struct {
    // ... existing fields ...
    currentQueryID string // correlation ID for the active search
}
```

### Integration Points

**1. `submitSearch()` — Generate QueryID at search initiation**

```go
func (a App) submitSearch() (tea.Model, tea.Cmd) {
    query := a.filterInput.Value()
    if query == "" {
        // ... existing logic ...
    }

    // Generate a new query ID for this search
    a.currentQueryID = obs.NewQueryID()
    qid := a.currentQueryID

    a.logger.Emit(obs.EventSearchStarted, obs.D{
        "qid":   qid,
        "query": query,
    })

    // ... existing save/setup logic ...

    // Closures capture qid for the commands they produce
    var cmds []tea.Cmd
    if a.loadSearchPool != nil {
        a.searchPoolPending = true
        cmds = append(cmds, a.loadSearchPool(qid))
    }
    if a.embedQuery != nil {
        a.embeddingPending = true
        cmds = append(cmds, a.embedQuery(query, qid))
    }
    // ... rest unchanged ...
}
```

**2. `AppConfig` closures — Accept QueryID parameter**

The closure signatures in `AppConfig` change to accept the query ID:

```go
type AppConfig struct {
    // ... existing ...
    EmbedQuery     func(query string, qid string) tea.Cmd
    LoadSearchPool func(qid string) tea.Cmd
    BatchRerank    func(query string, docs []string, qid string) tea.Cmd
    ScoreEntry     func(query string, doc string, index int, qid string) tea.Cmd
}
```

**3. `cmd/observer/main.go` — Thread QueryID through closures**

```go
// EmbedQuery closure:
EmbedQuery: func(query string, qid string) tea.Cmd {
    return func() tea.Msg {
        emb, err := embedder.EmbedQuery(ctx, query)
        return ui.QueryEmbedded{QueryID: qid, Query: query, Embedding: emb, Err: err}
    }
},

// BatchRerank closure:
BatchRerank: func(query string, docs []string, qid string) tea.Cmd {
    return func() tea.Msg {
        scores, err := jinaReranker.Rerank(ctx, query, docs)
        if err != nil {
            return ui.RerankComplete{QueryID: qid, Query: query, Err: err}
        }
        result := make([]float32, len(docs))
        for _, s := range scores {
            if s.Index < len(result) {
                result[s.Index] = s.Score
            }
        }
        return ui.RerankComplete{QueryID: qid, Query: query, Scores: result}
    }
},
```

**4. Event handlers — Include QueryID in Emit calls**

```go
// In QueryEmbedded handler:
a.logger.Emit(obs.EventSearchQueryEmbedded, obs.D{
    "qid":        msg.QueryID,
    "query":      msg.Query,
    "latency_ms": time.Since(a.searchStart).Milliseconds(),
    "dims":       len(msg.Embedding),
})
```

**5. Stale-check handlers — Use QueryID for stale detection**

Currently, stale results are detected by comparing `msg.Query` against `a.filterInput.Value()`. With QueryID, use a stronger check:

```go
// In RerankComplete handler:
if msg.QueryID != a.currentQueryID {
    return a, nil // stale result from a previous search
}
```

This is more robust than string comparison because the user could type the same query twice, and the old results should still be discarded.

### Implementation Steps

1. Create `internal/obs/queryid.go` with `NewQueryID()`
2. Create `internal/obs/queryid_test.go` — test monotonicity, concurrent safety, format
3. Add `QueryID` field to `QueryEmbedded`, `EntryReranked`, `RerankComplete`, `SearchPoolLoaded` in `messages.go`
4. Add `currentQueryID` field to `App` struct
5. Update `AppConfig` closure signatures to accept `qid string`
6. Update `submitSearch()` to generate QueryID and pass to closures
7. Update `clearSearch()` to clear `currentQueryID`
8. Update `startReranking()` to pass `currentQueryID` to rerank closures
9. Update all event handlers to include `qid` in `logger.Emit` calls
10. Update stale-check logic to compare QueryID instead of (or in addition to) query string
11. Update `main.go` closures to accept and thread QueryID
12. Update all tests in `app_test.go` that construct message types (add QueryID field)
13. Run `go test ./...`

### Key Design Decisions

**Why monotonic counter ("q1", "q2") instead of UUID?** Query IDs appear in every search event in the log file. UUIDs are 36 characters; "q42" is 3 characters. When grepping `observer.log`, short IDs are vastly more readable. Since query IDs are session-scoped (reset on restart), monotonic counters are sufficient. There is no cross-session correlation requirement.

**Why modify message struct fields instead of using context?** Bubble Tea messages are values passed through channels. Context does not flow through `tea.Msg`. The only way to correlate a response message with its request is to include the correlation data in the message itself. This is the standard pattern in Elm architecture / Bubble Tea.

**Why change closure signatures instead of capturing QueryID via closure?** The closures in `AppConfig` are set once at initialization. The QueryID changes per search. The closure must receive the QueryID as a parameter so each invocation can use the correct ID. Alternatively, the closures could close over a `*string` pointer, but parameter passing is cleaner and more explicit.

**Impact on existing tests:** All tests that construct `QueryEmbedded`, `RerankComplete`, `SearchPoolLoaded`, or `EntryReranked` need a `QueryID` field added. This is a mechanical change (add `QueryID: "test"` to each literal). The test assertions do not check QueryID unless specifically testing the stale-check behavior.

---

## Priority 5: OBSERVER_TRACE Env Var

### Goal

Full message tracing via an `Update()` middleware. When `OBSERVER_TRACE` is set, every `tea.Msg` received by `Update()` is logged with its type and key fields. When unset, there is truly zero cost — no allocations, no function calls, no interface checks.

### Package/File Placement

**New file:** `internal/obs/trace.go` — Trace middleware and message formatting
**Modified file:** `internal/ui/app.go` — Wrap `Update()` with trace
**Modified file:** `cmd/observer/main.go` — Check env var, enable tracing

### Type Definitions

```go
// trace.go

package obs

import (
    "fmt"
    "reflect"
    "strings"

    tea "github.com/charmbracelet/bubbletea"
)

// Tracer logs every tea.Msg that flows through Update().
// When nil, all trace paths are dead code.
type Tracer struct {
    logger *Logger
}

// NewTracer creates a Tracer that logs to the given Logger.
// Returns nil if tracing is not enabled (zero-cost: all callers check for nil).
func NewTracer(logger *Logger, enabled bool) *Tracer {
    if !enabled {
        return nil
    }
    return &Tracer{logger: logger}
}

// TraceMsg logs a message's type and summary.
// Called at the top of Update(). The caller must nil-check the Tracer.
func (t *Tracer) TraceMsg(msg tea.Msg) {
    typeName := reflect.TypeOf(msg).String()
    summary := summarizeMsg(msg)
    data := D{
        "type": typeName,
    }
    if summary != "" {
        data["summary"] = summary
    }
    t.logger.Emit("trace.msg", data)
}

// summarizeMsg extracts key fields from known message types for logging.
// Returns empty string for types without interesting summary data.
func summarizeMsg(msg tea.Msg) string {
    switch m := msg.(type) {
    case tea.KeyMsg:
        return fmt.Sprintf("key=%s", m.String())
    case tea.WindowSizeMsg:
        return fmt.Sprintf("size=%dx%d", m.Width, m.Height)
    default:
        // For custom message types, use fmt's default rendering
        // but truncate to avoid logging huge slices
        s := fmt.Sprintf("%+v", msg)
        if len(s) > 200 {
            s = s[:200] + "..."
        }
        // Remove newlines for JSONL compatibility
        s = strings.ReplaceAll(s, "\n", " ")
        return s
    }
}
```

### Integration into App

**New fields in App and AppConfig:**

```go
type App struct {
    // ... existing fields ...
    tracer *obs.Tracer // nil when OBSERVER_TRACE is not set
}

type AppConfig struct {
    // ... existing fields ...
    Tracer *obs.Tracer
}
```

**Update() wrapping:**

The trace check is a single nil pointer comparison — the cheapest possible guard:

```go
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Trace: log every incoming message (zero-cost when tracer is nil)
    if a.tracer != nil {
        a.tracer.TraceMsg(msg)
    }

    // ... existing switch statement ...
}
```

### Zero-Cost Guarantee

When `OBSERVER_TRACE` is not set:
1. `NewTracer(logger, false)` returns `nil`
2. `a.tracer` is `nil`
3. The `if a.tracer != nil` check is a single pointer comparison (1 CPU instruction)
4. No `reflect.TypeOf` call, no `fmt.Sprintf`, no string allocation, no `map[string]any` creation
5. The `summarizeMsg` function is never called (dead code elimination candidate)

When `OBSERVER_TRACE` is set:
1. `NewTracer(logger, true)` returns a `*Tracer`
2. Every `Update()` call logs the message type and summary
3. Cost per message: ~1-5 microseconds (reflection + JSON encode + file write)
4. At ~60 messages/second (spinner ticks), this is ~300 microseconds/second — imperceptible

### main.go Integration

```go
// In main():
traceEnabled := os.Getenv("OBSERVER_TRACE") != ""
tracer := obs.NewTracer(logger, traceEnabled)

cfg := ui.AppConfig{
    // ... existing fields ...
    Tracer: tracer,
}
```

### Implementation Steps

1. Create `internal/obs/trace.go` with `Tracer`, `NewTracer`, `TraceMsg`, `summarizeMsg`
2. Create `internal/obs/trace_test.go`:
   - Test `NewTracer` returns nil when disabled
   - Test `TraceMsg` emits event with correct type
   - Test `summarizeMsg` for `tea.KeyMsg`, `tea.WindowSizeMsg`, and custom types
   - Test `summarizeMsg` truncation for large messages
3. Add `tracer *obs.Tracer` to `App` and `Tracer *obs.Tracer` to `AppConfig`
4. Add `if a.tracer != nil` guard at top of `Update()`
5. Add env var check and `NewTracer` call in `main.go`
6. Run `go test ./...`
7. Manual test: `OBSERVER_TRACE=1 ./observer` then `tail -f ~/.observer/observer.log | jq .`

### Key Design Decisions

**Why nil-pointer check instead of a no-op interface?** A no-op interface (`type Tracer interface { TraceMsg(msg tea.Msg) }`) with a no-op implementation still requires an interface method dispatch on every `Update()` call (~3ns per call, plus preventing inlining of the caller). A nil pointer check is 0.3ns and allows the compiler to optimize the dead branch. This is the "zero cost" guarantee.

**Why `reflect.TypeOf` instead of a type switch?** A type switch with explicit cases for all message types would be faster but requires maintaining a list of every `tea.Msg` type. When new message types are added, the trace code would need updating. `reflect.TypeOf` handles all types automatically with no maintenance burden. The ~100ns cost per reflect call is acceptable since tracing is opt-in and already implies a willingness to pay overhead.

**Why not log the full message via `json.Marshal`?** Many `tea.Msg` types contain slices of `store.Item` (with string fields, timestamps) or `[]float32` embeddings. JSON-serializing a `SearchPoolLoaded` message with 2000 items and embeddings would produce megabytes of output per event. The `summarizeMsg` function intentionally truncates to 200 characters, keeping the trace readable and the log file small.

**Why not use build tags for zero-cost?** Build tags (`//go:build trace`) would eliminate the trace code entirely from the binary. But this means you cannot enable tracing on a running binary — you have to rebuild. Environment variable checking at startup is more practical: users can set `OBSERVER_TRACE=1` and restart the app without recompiling.

---

## Cross-Cutting Concerns

### Dependency Injection Flow

The wiring in `main.go` follows the existing pattern of creating dependencies and passing them through `AppConfig`:

```
main.go:
    logFile := os.OpenFile(...)
    ringBuf := obs.NewRingBuffer()
    logger  := obs.NewLogger(logFile, ringBuf)
    tracer  := obs.NewTracer(logger, os.Getenv("OBSERVER_TRACE") != "")

    coordinator := coord.NewCoordinator(st, provider, embedder, logger)
    provider    := fetch.NewClarionProvider(nil, opts, logger)

    cfg := ui.AppConfig{
        Logger:     logger,
        RingBuffer: ringBuf,
        Tracer:     tracer,
        // ... all existing fields ...
    }
```

### Test Strategy

Each priority has isolated tests:
- **P1 (events):** Test JSONL roundtrip, concurrent Emit
- **P2 (ring buffer):** Test push/read, wraparound, concurrency with `-race`
- **P3 (debug overlay):** Test rendering, toggle key, View() changes
- **P4 (query ID):** Test monotonicity, threading through messages, stale checks
- **P5 (trace):** Test nil-check path, TraceMsg output, summarize truncation

Existing tests in `app_test.go` and `coordinator_test.go` need minor updates:
- `NewCoordinator` gains a `logger` parameter — pass `nil` or a `NewLogger(io.Discard, nil)` in tests
- Message struct literals gain a `QueryID` field — add `QueryID: ""` or `QueryID: "test"`
- `App` construction in tests should use `NewAppWithConfig` with a nil logger (the nil-receiver pattern on `Emit` handles this)

### Migration Path

The 5 priorities are designed to be implemented sequentially, each building on the previous:

1. **P1 (events)** is standalone: create `obs` package, replace `log.Printf`, wire logger
2. **P2 (ring buffer)** adds to P1: create `RingBuffer`, pass to `Logger`, wire to `App`
3. **P3 (debug overlay)** reads from P2: create `debug.go`, toggle key, View() integration
4. **P4 (query ID)** modifies message types: add QueryID, update closures, update tests
5. **P5 (trace)** adds to P1: create `Tracer`, add nil-check in `Update()`

Each priority can be merged independently as a single commit/PR. The only breaking change across priorities is P4 (message type changes), which requires updating all tests that construct those messages.

### Impact on Existing Behavior

- **Log file format changes:** The log file switches from unstructured `log.Printf` lines to JSONL. Any external tooling reading the log file needs updating. The `tail -f observer.log` workflow still works (JSONL is line-oriented), but output is less human-readable without `jq`.
- **Status bar gains `?` hint:** One additional key hint in the status bar.
- **Message types gain fields:** `QueryID` is added to 4 message types. All fields are additive (no removal or rename), so this is a backward-compatible change for any code constructing these types with field names (the zero value of `string` is `""`).
- **TUI rendering:** When `debugVisible` is true, the content area shrinks by ~12 lines. This is the intended behavior.

---

## New Files Summary

| File | Package | Priority | Purpose |
|------|---------|----------|---------|
| `internal/obs/event.go` | obs | P1 | Event struct, type constants, D alias |
| `internal/obs/logger.go` | obs | P1 | Logger struct, NewLogger, Emit |
| `internal/obs/ringbuffer.go` | obs | P2 | RingBuffer struct, Push, Last, LastOfType, CountByType |
| `internal/obs/queryid.go` | obs | P4 | NewQueryID (atomic counter) |
| `internal/obs/trace.go` | obs | P5 | Tracer struct, NewTracer, TraceMsg, summarizeMsg |
| `internal/ui/debug.go` | ui | P3 | debugState, computeDebugState, renderDebugOverlay |
| `internal/obs/obs_test.go` | obs | P1 | Logger tests |
| `internal/obs/ringbuffer_test.go` | obs | P2 | RingBuffer tests |
| `internal/obs/queryid_test.go` | obs | P4 | QueryID tests |
| `internal/obs/trace_test.go` | obs | P5 | Tracer tests |
| `internal/ui/debug_test.go` | ui | P3 | Debug overlay tests |

## Modified Files Summary

| File | Priorities | Changes |
|------|-----------|---------|
| `cmd/observer/main.go` | P1, P2, P4, P5 | Logger/RingBuffer/Tracer creation; closure signature updates for QueryID; replace tea.LogToFile |
| `internal/ui/app.go` | P1, P2, P3, P4, P5 | Logger/RingBuffer/Tracer/debugVisible/currentQueryID fields; View() debug panel; Update() trace guard; submitSearch() QueryID; replace log.Printf |
| `internal/ui/messages.go` | P4 | Add QueryID to QueryEmbedded, EntryReranked, RerankComplete, SearchPoolLoaded |
| `internal/ui/styles.go` | P3 | Add DebugOverlay and DebugLabel styles |
| `internal/ui/stream.go` | P3 | Add ?:debug hint to status bar |
| `internal/coord/coordinator.go` | P1 | Add Logger field; replace 6 log.Printf calls |
| `internal/fetch/clarion.go` | P1 | Add Logger field; replace 1 log.Printf call |

## Critical Files for Implementation

- `internal/ui/app.go` — Central file: all 5 priorities modify this file (logger field, ring buffer field, debug toggle, query ID generation, tracer guard in Update)
- `internal/ui/messages.go` — Priority 4 adds QueryID to 4 message types; all search event correlation flows through these types
- `internal/coord/coordinator.go` — Priority 1 replaces 6 log.Printf calls with structured events; gains Logger dependency
- `cmd/observer/main.go` — Wiring: creates Logger, RingBuffer, Tracer; updates all closure signatures for QueryID; replaces tea.LogToFile setup
- `internal/ui/app_test.go` — Tests that need updating for new constructor signatures (Logger parameter) and message types (QueryID field)
