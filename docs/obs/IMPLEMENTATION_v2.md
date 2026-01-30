# Observer Observability System — Implementation Plan v2

## Table of Contents

1. [Overview](#overview)
2. [Implementation Order](#implementation-order)
3. [Priority 1: Structured JSONL Events](#priority-1-structured-jsonl-events)
4. [Priority 2: Ring Buffer](#priority-2-ring-buffer)
5. [Priority 3: Debug Overlay](#priority-3-debug-overlay)
6. [Priority 4: Query ID](#priority-4-query-id)
7. [Priority 5: OBSERVER\_TRACE](#priority-5-observer_trace)
8. [Appendix A: Decision Log](#appendix-a-decision-log)
9. [Appendix B: Goroutine Safety Map](#appendix-b-goroutine-safety-map)
10. [Appendix C: File Inventory](#appendix-c-file-inventory)
11. [Appendix D: JSONL Output Examples and jq Queries](#appendix-d-jsonl-output-examples-and-jq-queries)
12. [Appendix E: Attribution](#appendix-e-attribution)

---

## Overview

Observer's observability system is a 3-layer architecture:

1. **Structured JSONL events** — Every `log.Printf` becomes a typed `Event` struct serialized as a single JSONL line. Events are written asynchronously to a dedicated file (`observer.events.jsonl`) via a buffered channel and background drain goroutine. Machine-parseable, greppable with `jq`.

2. **In-memory ring buffer** — A fixed-size (1024-slot) circular buffer of recent events, readable by the UI for live inspection. Zero allocation on push (pre-allocated slots). Copy-on-read snapshots for goroutine safety.

3. **Debug overlay + OBSERVER_TRACE** — A toggle-key (Shift+D) debug panel showing pipeline stats and recent events. An opt-in env var enables full Bubble Tea message tracing with exhaustive type switches and defer-based timing.

### What Changed from v1 and Why

This v2 plan incorporates critical production-hardening fixes from an adversarial review by four frontier models (GPT-5, Grok 4, Gemini 3, Claude 4.5), plus the stronger typed-event design from a Claude+Gemini pair-programming synthesis. Key changes:

| Area | v1 Design | v2 Design | Why |
|------|-----------|-----------|-----|
| Event struct | `map[string]any` Data bag | Fixed fields + `Extra` escape hatch | Greppable with jq; no double serialization; type-safe |
| Log files | Single shared file | Separate `observer.events.jsonl` + `observer.log` | Prevents JSONL corruption from unstructured Bubble Tea lines |
| Writer | Synchronous `json.Encoder` under mutex | Async channel-based writer with background drain goroutine | Prevents file I/O from blocking the UI goroutine |
| Shutdown | None | `Logger.Close()` flushes channel, waits for drain, closes file | Prevents event loss on exit |
| Drop handling | Errors silently ignored | Drop counter (`atomic.Uint64`) + stderr fallback | Visibility into event loss |
| Ring buffer lock | `sync.RWMutex` | `sync.Mutex` | Write-heavy workload; RWMutex causes writer starvation |
| Ring buffer push | Public `Push()` only | Dual `push()` (unexported) / `Push()` (exported) | Avoids double-lock race when called from `Logger.Emit()` |
| Extra map | Not copied | Deep-copied in `Push()` | Prevents aliasing bugs |
| QueryID | Monotonic atomic counter | `crypto/rand` hex string | Unique across sessions; no package-global state |
| Session tracking | None | `session_id` field on every event | Groups events across log-append sessions |
| AppConfig | Flat fields | `ObsConfig` sub-struct | Prevents god-object growth |
| Trace default case | `%+v` / `fmt.Sprintf` | `%T` type name only | Prevents latent OOM on large message types |
| File permissions | 0644 | 0600 | Log contains user queries |
| Age formatting | No negative handling | Clamp to "0ms" | Handles clock skew gracefully |
| Lock ordering | Undocumented | Documented invariant in `logger.go` | Prevents future deadlocks |

---

## Implementation Order

**P1 -> P4 -> P2 -> P3 -> P5**

```
P1 (JSONL events)  -- Foundation: otel package, Logger, Event types
     |
     v
P4 (Query ID)      -- Small, isolated. Do while P1 is fresh (same message types, same AppConfig)
     |
     v
P2 (Ring buffer)   -- Builds on Logger from P1 (SetRingBuffer, push integration)
     |
     v
P3 (Debug overlay) -- Depends on ring buffer from P2 (reads via Last/Stats)
     |
     v
P5 (Trace)         -- Independent of P2-P4, but benefits from stable Logger
```

**Rationale for P4 before P2:** P4 touches the same files as P1 (AppConfig, messages.go, main.go closures). Doing P4 immediately after P1 reduces context-switching cost and avoids revisiting these files later. P2 and P3 are a separate cluster (ring buffer + UI) that can be implemented as a unit.

Each priority can be merged as a single commit/PR. All existing tests must pass after each priority.

---

## Priority 1: Structured JSONL Events

### 1.1 Package Layout

```
internal/otel/
    event.go        -- Event type, EventKind constants, Level constants
    logger.go       -- Logger struct, async writer, Close(), convenience helpers
    logger_test.go  -- Tests for Logger, Event serialization, async behavior
```

New package. No existing files modified at this stage (file changes come in integration step 1.5).

**Why `internal/otel/`:**
- Short and ergonomic at call sites: `otel.Emit(...)`, `otel.TraceEnabled()`
- `internal/` scope means no OpenTelemetry naming confusion for external consumers
- If real OpenTelemetry is ever adopted, this package gets replaced entirely, not extended
- Alternatives rejected: `internal/obs/` collides with "observer" abbreviation; `internal/event/` is too generic

**Why a new package rather than extending `ui/` or `coord/`:**
- Events are emitted from multiple packages (ui, coord, fetch, main)
- A dedicated package creates a single dependency direction: everything imports otel, otel imports nothing from observer
- Zero risk of import cycles

### 1.2 Event Type

```go
// internal/otel/event.go
package otel

import (
    "encoding/json"
    "time"
)

// Level defines event severity for filtering.
type Level string

const (
    LevelInfo  Level = "info"
    LevelWarn  Level = "warn"
    LevelError Level = "error"
    LevelDebug Level = "debug"
)

// EventKind identifies the category of an observability event.
// Dot-delimited: "<subsystem>.<action>".
type EventKind string

const (
    // Pipeline events
    KindFetchStart     EventKind = "fetch.start"
    KindFetchComplete  EventKind = "fetch.complete"
    KindFetchError     EventKind = "fetch.error"
    KindEmbedStart     EventKind = "embed.start"
    KindEmbedComplete  EventKind = "embed.complete"
    KindEmbedBatch     EventKind = "embed.batch"
    KindEmbedError     EventKind = "embed.error"

    // Search events
    KindSearchStart    EventKind = "search.start"
    KindSearchPool     EventKind = "search.pool"
    KindQueryEmbed     EventKind = "search.query_embed"
    KindCosineRerank   EventKind = "search.cosine_rerank"
    KindCrossEncoder   EventKind = "search.cross_encoder"
    KindSearchComplete EventKind = "search.complete"
    KindSearchCancel   EventKind = "search.cancel"

    // Store events
    KindStoreError     EventKind = "store.error"

    // UI events
    KindKeyPress       EventKind = "ui.key"
    KindViewRender     EventKind = "ui.render"

    // System events
    KindStartup        EventKind = "sys.startup"
    KindShutdown       EventKind = "sys.shutdown"
    KindError          EventKind = "sys.error"

    // Trace events (Priority 5)
    KindMsgReceived    EventKind = "trace.msg_received"
    KindMsgHandled     EventKind = "trace.msg_handled"
)

// Event is the universal observability record. Every field except Kind and
// Time is optional. Serialized as a single JSONL line.
type Event struct {
    Time      time.Time      `json:"t"`
    Level     Level          `json:"level,omitempty"`
    Kind      EventKind      `json:"kind"`
    Comp      string         `json:"comp,omitempty"`      // component: "coord", "ui", "fetch", "main"
    SessionID string         `json:"session_id,omitempty"` // random hex, same for entire app run
    QueryID   string         `json:"qid,omitempty"`       // Priority 4: search correlation ID
    Dur       time.Duration  `json:"-"`                   // not serialized directly
    DurMs     float64        `json:"dur_ms,omitempty"`    // computed from Dur at marshal time
    Count     int            `json:"count,omitempty"`
    Source    string         `json:"source,omitempty"`
    Query     string         `json:"query,omitempty"`
    Dims      int            `json:"dims,omitempty"`
    Err       string         `json:"err,omitempty"`
    Msg       string         `json:"msg,omitempty"`       // free text
    Extra     map[string]any `json:"extra,omitempty"`     // escape hatch for unusual fields
}

// MarshalJSON implements json.Marshaler, converting Dur to DurMs.
func (e Event) MarshalJSON() ([]byte, error) {
    type Alias Event
    a := struct {
        Alias
    }{Alias: Alias(e)}
    if e.Dur > 0 {
        a.DurMs = float64(e.Dur) / float64(time.Millisecond)
    }
    return json.Marshal(a)
}
```

**Key design decisions:**

- **Fixed fields, not `map[string]any` Data bag**: Every field is directly accessible to `jq`, grep, and Go code. The `Extra` escape hatch handles unusual cases without schema changes. No double serialization (no `json.RawMessage`).
- **`Level` field**: Enables severity-based filtering (`jq 'select(.level == "error")'`). Defaults to zero value (empty string), which is omitted in JSON via `omitempty`.
- **`Comp` field**: Short component name for subsystem filtering. "comp" instead of "component" to keep JSONL lines compact.
- **`SessionID` field**: A random hex ID generated once at Logger creation time. All events from one app run share it. Enables grouping across log-append sessions (the log file is append-only, so multiple sessions share the same file).
- **`omitempty` on all optional fields**: Most events use 3-5 fields. Omitting empty fields keeps lines short and readable.
- **`Dur`/`DurMs` split**: Callers set `Dur` using natural Go `time.Duration`. JSON output uses `dur_ms` (float64) for human readability and `jq` arithmetic. `Dur` is excluded from JSON via `json:"-"`.
- **`Time` set by `Emit()`, not callers**: Prevents clock skew between event creation and emission. Simplifies every call site.

### 1.3 Logger Type

```go
// internal/otel/logger.go
package otel

// Lock ordering invariant:
// Logger.mu is always acquired before RingBuffer internal state is accessed
// via push(). The ring buffer's own mu is only used by its public methods
// (Push, Snapshot, Last, Stats), never during Logger.Emit(). This prevents
// deadlocks between Logger and RingBuffer.

import (
    "crypto/rand"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "sync"
    "sync/atomic"
    "time"
)

const (
    // writerChanSize is the capacity of the async write channel.
    // At ~200 bytes/event, 4096 events buffers ~800KB.
    writerChanSize = 4096
)

// Logger serializes events as JSONL via an async background writer.
// Goroutine-safe. All emitted events flow through a buffered channel
// to a drain goroutine that writes to disk and pushes to the ring buffer.
type Logger struct {
    mu        sync.Mutex
    buf       *RingBuffer    // Priority 2: nil until SetRingBuffer
    sessionID string         // random hex, set once at creation
    ch        chan []byte     // buffered channel for async writes
    w         io.Writer      // destination (event log file)
    dropped   atomic.Uint64  // events dropped due to full channel or encode failure
    done      chan struct{}   // closed when drain goroutine exits
    closeOnce sync.Once
}

// NewLogger creates a Logger writing JSONL to w asynchronously.
// Starts a background drain goroutine. Call Close() to flush and stop.
func NewLogger(w io.Writer) *Logger {
    // Generate session ID: 8 random bytes -> 16 hex chars
    var sid [8]byte
    _, _ = rand.Read(sid[:])

    l := &Logger{
        sessionID: fmt.Sprintf("%x", sid[:]),
        ch:        make(chan []byte, writerChanSize),
        w:         w,
        done:      make(chan struct{}),
    }
    go l.drain()
    return l
}

// NewNullLogger creates a Logger that discards output. For tests.
func NewNullLogger() *Logger {
    return NewLogger(io.Discard)
}

// drain is the background goroutine that reads from ch and writes to disk + ring buffer.
func (l *Logger) drain() {
    defer close(l.done)
    for data := range l.ch {
        // Write to disk
        _, _ = l.w.Write(data)

        // Push to ring buffer if attached.
        // We unmarshal only when a ring buffer is present.
        // This keeps the fast path (no ring buffer) allocation-free in drain.
        l.mu.Lock()
        rb := l.buf
        l.mu.Unlock()

        if rb != nil {
            var ev Event
            if err := json.Unmarshal(data, &ev); err == nil {
                rb.Push(ev)
            }
        }
    }
}

// Emit writes an event to the JSONL log (and ring buffer if attached).
// Sets Time and SessionID. Goroutine-safe. Non-blocking: if the channel
// is full, the event is dropped and the drop counter is incremented.
func (l *Logger) Emit(e Event) {
    e.Time = time.Now()
    e.SessionID = l.sessionID

    data, err := json.Marshal(e)
    if err != nil {
        l.dropped.Add(1)
        return
    }
    // Append newline for JSONL format
    data = append(data, '\n')

    // Non-blocking send
    select {
    case l.ch <- data:
        // success
    default:
        l.dropped.Add(1)
    }
}

// Info emits an info-level event. Convenience wrapper.
func (l *Logger) Info(kind EventKind, comp string, msg string) {
    l.Emit(Event{Level: LevelInfo, Kind: kind, Comp: comp, Msg: msg})
}

// Error emits an error-level event. Convenience wrapper.
func (l *Logger) Error(kind EventKind, comp string, err error) {
    l.Emit(Event{Level: LevelError, Kind: kind, Comp: comp, Err: err.Error()})
}

// Warn emits a warn-level event. Convenience wrapper.
func (l *Logger) Warn(kind EventKind, comp string, msg string) {
    l.Emit(Event{Level: LevelWarn, Kind: kind, Comp: comp, Msg: msg})
}

// SetRingBuffer attaches a ring buffer for live inspection (Priority 2).
func (l *Logger) SetRingBuffer(buf *RingBuffer) {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.buf = buf
}

// Dropped returns the number of events dropped since creation.
func (l *Logger) Dropped() uint64 {
    return l.dropped.Load()
}

// Close flushes pending events, stops the drain goroutine, and reports
// any dropped events to stderr. Must be called during shutdown.
func (l *Logger) Close() {
    l.closeOnce.Do(func() {
        close(l.ch)   // signal drain to exit after processing remaining events
        <-l.done       // wait for drain goroutine to finish

        if d := l.dropped.Load(); d > 0 {
            fmt.Fprintf(os.Stderr, "observer: %d events dropped during session %s\n", d, l.sessionID)
        }
    })
}
```

**Key design decisions:**

- **Async channel-based writer**: `Emit()` marshals the event to `[]byte`, then sends to a buffered channel (capacity 4096). A background goroutine drains the channel, writing to disk and pushing to the ring buffer. This prevents file I/O from blocking the UI goroutine. If the channel is full, the event is dropped and a counter is incremented. The marshal happens in the caller's goroutine (fast, ~1us) while the I/O happens in the drain goroutine.

- **Dependency injection, not global singleton**: The Logger is explicitly passed through constructors (App, Coordinator, ClarionProvider). This is consistent with Observer's existing architecture (AppConfig closures, Coordinator constructor) and makes testing trivial via `NewNullLogger()`.

- **Convenience helpers `Info()`, `Error()`, `Warn()`**: Reduce boilerplate at common call sites. The full `Emit(Event{...})` form is still available for events that need more fields.

- **`Logger.Close()`**: Closes the channel (signals drain to exit after processing remaining events), waits for the drain goroutine to finish via `<-l.done`, and reports any dropped events to stderr. Called in `main.go` shutdown sequence. Uses `sync.Once` for idempotent close.

- **Drop counter + stderr fallback**: When marshal fails or the channel is full, `dropped` (`atomic.Uint64`) is incremented. On `Close()`, the total is reported to stderr. Log infrastructure must never crash the app.

- **`session_id`**: A random hex ID generated once at Logger creation time. All events from one app run share the same `session_id`. Enables grouping in append-mode log files where multiple sessions interleave.

- **Ring buffer integration in drain goroutine**: The drain goroutine pushes to the ring buffer after writing to disk. This means the ring buffer and the file always have the same event order. The ring buffer uses its own `Push()` (with its own lock) since the drain goroutine does not hold `Logger.mu` during the push. This is safe because the drain goroutine is the only writer and processes events sequentially.

### 1.4 Log-Site Migration Table

Complete inventory of every `log.Printf` call in the active codebase (excluding `archive/` and standalone CLI tools like `cmd/backfill/`, `cmd/search-test/`):

| # | File | Line | Current `log.Printf` | Replacement |
|---|------|------|----------------------|-------------|
| 1 | `internal/ui/app.go` | 227 | `search: query embedded in %dms (%d dims)` | `a.logger.Emit(otel.Event{Kind: otel.KindQueryEmbed, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Dims: len(msg.Embedding), Query: msg.Query})` |
| 2 | `internal/ui/app.go` | 231 | `search: cosine rerank applied in %dms (%d items)` | `a.logger.Emit(otel.Event{Kind: otel.KindCosineRerank, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Count: len(a.items), Query: a.filterInput.Value()})` |
| 3 | `internal/ui/app.go` | 260 | `search: pool loaded in %dms (%d items, %d embeddings)` | `a.logger.Emit(otel.Event{Kind: otel.KindSearchPool, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Count: len(msg.Items), Query: a.filterInput.Value(), Extra: map[string]any{"embeddings": len(msg.Embeddings)}})` |
| 4 | `internal/ui/app.go` | 281 | `search: cross-encoder rerank complete in %dms` | `a.logger.Emit(otel.Event{Kind: otel.KindSearchComplete, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Query: a.filterInput.Value()})` |
| 5 | `internal/ui/app.go` | 571 | `search: starting cross-encoder rerank (%d items, batch=%v)` | `a.logger.Emit(otel.Event{Kind: otel.KindCrossEncoder, Level: otel.LevelInfo, Comp: "ui", Count: topN, Query: query, Extra: map[string]any{"batch": a.batchRerank != nil}})` |
| 6 | `internal/ui/app.go` | 617 | `search: per-entry rerank complete in %dms (%d entries)` | `a.logger.Emit(otel.Event{Kind: otel.KindSearchComplete, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Count: len(a.rerankEntries)})` |
| 7 | `internal/coord/coordinator.go` | 178 | `coord: skipping embedding for %s (empty text)` | `c.logger.Emit(otel.Event{Kind: otel.KindEmbedError, Level: otel.LevelWarn, Comp: "coord", Source: item.ID, Msg: "skipping embedding: empty text"})` |
| 8 | `internal/coord/coordinator.go` | 195 | `coord: batch embedding failed, falling back to sequential: %v` | `c.logger.Emit(otel.Event{Kind: otel.KindEmbedError, Level: otel.LevelError, Comp: "coord", Msg: "batch embedding failed, falling back to sequential", Err: err.Error()})` |
| 9 | `internal/coord/coordinator.go` | 204 | `coord: failed to save embedding for %s: %v` | `c.logger.Emit(otel.Event{Kind: otel.KindError, Level: otel.LevelError, Comp: "coord", Source: pairs[i].item.ID, Msg: "failed to save embedding", Err: err.Error()})` |
| 10 | `internal/coord/coordinator.go` | 223 | `coord: failed to embed %s: %v` | `c.logger.Emit(otel.Event{Kind: otel.KindEmbedError, Level: otel.LevelError, Comp: "coord", Source: p.item.ID, Err: err.Error()})` |
| 11 | `internal/coord/coordinator.go` | 228 | `coord: failed to save embedding for %s: %v` | `c.logger.Emit(otel.Event{Kind: otel.KindError, Level: otel.LevelError, Comp: "coord", Source: p.item.ID, Msg: "failed to save embedding", Err: err.Error()})` |
| 12 | `internal/coord/coordinator.go` | 246 | `coord: failed to save items: %v` | `c.logger.Emit(otel.Event{Kind: otel.KindError, Level: otel.LevelError, Comp: "coord", Msg: "failed to save items", Err: saveErr.Error()})` |
| 13 | `internal/fetch/clarion.go` | 40 | `fetch: %s: %v` | `p.logger.Emit(otel.Event{Kind: otel.KindFetchError, Level: otel.LevelWarn, Comp: "fetch", Source: r.Source.Name, Err: r.Err.Error()})` |
| 14 | `cmd/observer/main.go` | 98 | `Warning: failed to get embeddings: %v` | `logger.Emit(otel.Event{Kind: otel.KindStoreError, Level: otel.LevelWarn, Comp: "main", Msg: "failed to get embeddings (recent)", Err: err.Error()})` |
| 15 | `cmd/observer/main.go` | 132 | `Warning: failed to get embeddings: %v` | `logger.Emit(otel.Event{Kind: otel.KindStoreError, Level: otel.LevelWarn, Comp: "main", Msg: "failed to get embeddings (full)", Err: err.Error()})` |
| 16 | `cmd/observer/main.go` | 166 | `Warning: failed to get embeddings for search pool: %v` | `logger.Emit(otel.Event{Kind: otel.KindStoreError, Level: otel.LevelWarn, Comp: "main", Msg: "failed to get embeddings (search pool)", Err: err.Error()})` |
| 17 | `cmd/observer/main.go` | 239 | `Error running program: %v` | `logger.Emit(otel.Event{Kind: otel.KindError, Level: otel.LevelError, Comp: "main", Msg: "program error", Err: err.Error()})` |

Additionally, new event brackets to add (not replacing existing log calls):

| File | Location | New Event |
|------|----------|-----------|
| `internal/coord/coordinator.go` | Top of `fetchAll()` | `c.logger.Emit(otel.Event{Kind: otel.KindFetchStart, Level: otel.LevelInfo, Comp: "coord"})` |
| `internal/coord/coordinator.go` | End of `fetchAll()` | `c.logger.Emit(otel.Event{Kind: otel.KindFetchComplete, Level: otel.LevelInfo, Comp: "coord", Dur: time.Since(start), Count: newItems})` |
| `cmd/observer/main.go` | After logger creation | `logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelInfo, Comp: "main", Msg: "observer starting"})` |
| `cmd/observer/main.go` | After `program.Run()` returns | `logger.Emit(otel.Event{Kind: otel.KindShutdown, Level: otel.LevelInfo, Comp: "main", Msg: "observer stopping"})` |

### 1.5 Integration

**A. `cmd/observer/main.go` — Logger initialization and separate log files**

```go
import "github.com/abelbrown/observer/internal/otel"

// In main(), after dataDir setup:

// Structured event log (JSONL) — separate from Bubble Tea's log output
eventLogPath := filepath.Join(dataDir, "observer.events.jsonl")
eventFile, err := os.OpenFile(eventLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
if err != nil {
    log.Fatalf("Failed to open event log: %v", err)
}
defer eventFile.Close()

logger := otel.NewLogger(eventFile)
defer logger.Close() // flush pending events on shutdown

// Bubble Tea / stdlib log stays on observer.log (unstructured)
stdlogPath := filepath.Join(dataDir, "observer.log")
if f, err := tea.LogToFile(stdlogPath, "observer"); err == nil {
    defer f.Close()
}

logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelInfo, Comp: "main", Msg: "observer starting"})
```

Pass Logger through constructors using `ObsConfig` sub-struct:

```go
obsCfg := ui.ObsConfig{
    Logger: logger,
    // Ring: ring,  // added in Priority 2
}

cfg := ui.AppConfig{
    // ... existing closures ...
    Obs: obsCfg,
}

coordinator := coord.NewCoordinator(st, provider, embedder, logger)
```

Emit shutdown event and close logger:

```go
// After program.Run() returns:
logger.Emit(otel.Event{Kind: otel.KindShutdown, Level: otel.LevelInfo, Comp: "main", Msg: "observer stopping"})
// logger.Close() called via defer above
```

**B. `internal/coord/coordinator.go` — Add logger field**

```go
type Coordinator struct {
    store    *store.Store
    provider Provider
    embedder embed.Embedder
    logger   *otel.Logger  // NEW
    wg       sync.WaitGroup
}

func NewCoordinator(s *store.Store, p Provider, e embed.Embedder, l *otel.Logger) *Coordinator {
    return &Coordinator{
        store:    s,
        provider: p,
        embedder: e,
        logger:   l,
    }
}
```

Add event brackets to `fetchAll()`:

```go
func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
    c.logger.Emit(otel.Event{Kind: otel.KindFetchStart, Level: otel.LevelInfo, Comp: "coord"})
    start := time.Now()
    // ... existing fetch logic ...
    c.logger.Emit(otel.Event{
        Kind:  otel.KindFetchComplete,
        Level: otel.LevelInfo,
        Comp:  "coord",
        Dur:   time.Since(start),
        Count: newItems,
    })
}
```

**C. `internal/fetch/clarion.go` — Add logger field**

```go
type ClarionProvider struct {
    sources []clarion.Source
    opts    clarion.FetchOptions
    logger  *otel.Logger  // NEW
}
```

Update `NewClarionProvider` to accept a logger parameter. Replace the `log.Printf` at line 40.

**D. `internal/ui/app.go` — Add logger via ObsConfig**

```go
// ObsConfig groups observability dependencies to prevent AppConfig god-object growth.
type ObsConfig struct {
    Logger *otel.Logger
    Ring   *otel.RingBuffer // Priority 2: nil initially
}

type AppConfig struct {
    // ... existing fields unchanged ...
    Obs ObsConfig  // NEW
}

type App struct {
    // ... existing fields ...
    logger *otel.Logger      // NEW
    ring   *otel.RingBuffer  // Priority 2
}
```

Wire in `newApp()`:

```go
func newApp(cfg AppConfig) App {
    // ... existing code ...
    logger := cfg.Obs.Logger
    if logger == nil {
        logger = otel.NewNullLogger()
    }
    return App{
        // ... existing fields ...
        logger: logger,
        ring:   cfg.Obs.Ring,
    }
}
```

**Logger pointer safety:** App is passed by value (Bubble Tea's `Update()` returns `tea.Model`). The `logger` field is a `*otel.Logger` pointer. All copies of App share the same Logger instance. This is safe because Logger is goroutine-safe and immutable after initialization.

### 1.6 Separate Log Files

Two log files exist after P1:

| File | Contents | Format | Writer |
|------|----------|--------|--------|
| `~/.observer/observer.events.jsonl` | Structured observability events | JSONL (one JSON object per line) | `otel.Logger` (async) |
| `~/.observer/observer.log` | Bubble Tea internals, stdlib `log` output | Unstructured text | `tea.LogToFile` / `log.SetOutput` |

**Why two files:** Mixing structured JSONL with unstructured Bubble Tea log lines corrupts JSONL parsing. Any tool that does `jq . < observer.events.jsonl` would fail on a plain-text line. Separating the files makes JSONL parsing reliable.

**File permissions:** Both files use `0600` (owner read/write only), not `0644`. The event log contains user search queries, which are private.

**Log rotation:** Deferred to a future iteration. The design supports it: separate event file, append mode, no file-level state. External rotation (logrotate, or periodic rename + reopen) is compatible with the append-only pattern. The `session_id` field enables grouping events within a session regardless of file boundaries.

### 1.7 Tests

`internal/otel/logger_test.go`:

- `TestEmitWritesValidJSONL`: emit events, read from a `bytes.Buffer`, decode as JSONL, verify fields.
- `TestEmitSetsTimeAndSessionID`: verify `Time` and `SessionID` are populated by `Emit()`, not by callers.
- `TestDurToMs`: verify `Dur` field converts to `DurMs` in JSON output.
- `TestOmitempty`: verify empty optional fields are omitted from JSON.
- `TestConcurrentEmit`: 100 goroutines emitting simultaneously, verify no panic and all events arrive.
- `TestNullLogger`: verify `NewNullLogger()` does not panic.
- `TestClose`: verify `Close()` flushes pending events and can be called multiple times.
- `TestDropCounter`: fill channel to capacity, emit one more, verify `Dropped()` returns 1.
- `TestConvenienceHelpers`: verify `Info()`, `Error()`, `Warn()` produce correct Level and Kind.

---

## Priority 2: Ring Buffer

### 2.1 Type Definition

```go
// internal/otel/ringbuf.go
package otel

import "sync"

// DefaultRingSize is the default ring buffer capacity.
// Power of two allows modulo to compile to bitwise AND.
const DefaultRingSize = 1024

// RingBuffer is a fixed-size circular buffer of Events.
// Goroutine-safe for concurrent Push and read operations.
type RingBuffer struct {
    mu    sync.Mutex
    buf   []Event
    size  int
    head  int // next write position
    count int // number of valid entries (0..size)
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
    if size <= 0 {
        size = DefaultRingSize
    }
    return &RingBuffer{
        buf:  make([]Event, size),
        size: size,
    }
}

// Push adds an event, overwriting the oldest if full. Goroutine-safe.
// Deep-copies the Extra map to prevent aliasing bugs.
func (r *RingBuffer) Push(e Event) {
    if e.Extra != nil {
        cp := make(map[string]any, len(e.Extra))
        for k, v := range e.Extra {
            cp[k] = v
        }
        e.Extra = cp
    }
    r.mu.Lock()
    r.buf[r.head] = e
    r.head = (r.head + 1) % r.size
    if r.count < r.size {
        r.count++
    }
    r.mu.Unlock()
}

// push is the internal variant called from Logger's drain goroutine.
// Does NOT acquire the ring buffer's own lock.
// ONLY safe when called from a single goroutine (the drain goroutine).
func (r *RingBuffer) push(e Event) {
    if e.Extra != nil {
        cp := make(map[string]any, len(e.Extra))
        for k, v := range e.Extra {
            cp[k] = v
        }
        e.Extra = cp
    }
    r.buf[r.head] = e
    r.head = (r.head + 1) % r.size
    if r.count < r.size {
        r.count++
    }
}

// Snapshot returns a copy of all events in chronological order (oldest first).
// The returned slice is safe to use without locks. Goroutine-safe.
func (r *RingBuffer) Snapshot() []Event {
    r.mu.Lock()
    defer r.mu.Unlock()

    if r.count == 0 {
        return nil
    }

    result := make([]Event, r.count)
    if r.count < r.size {
        copy(result, r.buf[:r.count])
    } else {
        n := copy(result, r.buf[r.head:])
        copy(result[n:], r.buf[:r.head])
    }
    return result
}

// Last returns the N most recent events in chronological order.
// If n > count, returns all events. Goroutine-safe.
func (r *RingBuffer) Last(n int) []Event {
    r.mu.Lock()
    defer r.mu.Unlock()

    if r.count == 0 {
        return nil
    }
    if n > r.count {
        n = r.count
    }

    result := make([]Event, n)
    start := (r.head - n + r.size) % r.size
    if start+n <= r.size {
        copy(result, r.buf[start:start+n])
    } else {
        first := r.size - start
        copy(result, r.buf[start:])
        copy(result[first:], r.buf[:n-first])
    }
    return result
}

// Len returns the number of events currently in the buffer. Goroutine-safe.
func (r *RingBuffer) Len() int {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.count
}

// Stats returns aggregated counts by EventKind over all buffered events.
// Operates on the buffer directly under lock (no copy). Goroutine-safe.
func (r *RingBuffer) Stats() map[EventKind]int {
    r.mu.Lock()
    defer r.mu.Unlock()

    counts := make(map[EventKind]int)
    start := 0
    if r.count >= r.size {
        start = r.head
    }
    for i := 0; i < r.count; i++ {
        idx := (start + i) % r.size
        counts[r.buf[idx].Kind]++
    }
    return counts
}
```

**Key design decisions:**

- **`sync.Mutex`, not `sync.RWMutex`**: The coordinator pushes events continuously, so readers rarely get a window without concurrent writers. `RWMutex` has higher per-operation overhead than `Mutex` and risks writer starvation in write-heavy workloads. `Mutex` is the right choice.

- **`push()` (unexported) / `Push()` (exported) dual-method**: The drain goroutine in Logger calls `push()` directly without acquiring the ring buffer's lock. This is safe because the drain goroutine is the sole writer to the ring buffer (it processes events sequentially from the channel). The exported `Push()` acquires the lock and is available for direct use (e.g., in tests or if the ring buffer is used independently).

  **Note on v2 design change:** In v1, `push()` was called from within `Logger.Emit()` under `Logger.mu` to avoid a dual-lock race. In v2, the async writer design means the drain goroutine is the only code path that writes to the ring buffer, so the lock-ordering concern is eliminated. The drain goroutine can safely call either `push()` or `Push()`. We use `push()` for the drain goroutine for performance (no lock acquisition overhead on the hot path).

- **Deep copy `Extra` map in both `push()` and `Push()`**: The `Extra` map is a reference type. Without a deep copy, callers who reuse or mutate a map after `Emit()` would corrupt the ring buffer's internal state. The copy cost is negligible (most events have 0-2 Extra keys).

- **Pre-allocated fixed slice**: No GC pressure from `append()`. Push is O(1) with no allocations after construction (except the Extra deep copy when present).

- **`Stats()` operates under lock without copying**: It only reads the `Kind` field, avoiding a full snapshot allocation. This is the primary method used by the debug overlay for the summary section.

### 2.2 Integration

**A. Update drain goroutine in Logger to use push():**

With the async writer design, the drain goroutine calls `push()` directly instead of the public `Push()`:

```go
// In Logger.drain():
if rb != nil {
    var ev Event
    if err := json.Unmarshal(data, &ev); err == nil {
        rb.push(ev)  // internal push, drain goroutine is sole writer
    }
}
```

**Alternative (simpler, slight overhead):** Use `rb.Push(ev)` in the drain goroutine. This acquires the ring buffer lock on every event but is simpler and still correct. The lock contention is between the drain goroutine (writer) and the UI goroutine (reader via `Last()`/`Stats()`). At typical event rates (~10/sec), contention is negligible.

**B. `cmd/observer/main.go`:**

```go
ring := otel.NewRingBuffer(otel.DefaultRingSize)
logger.SetRingBuffer(ring)

obsCfg := ui.ObsConfig{
    Logger: logger,
    Ring:   ring,
}
cfg := ui.AppConfig{
    // ... existing closures ...
    Obs: obsCfg,
}
```

**C. `internal/ui/app.go`:**

The `ring` field was already added in P1's `ObsConfig` integration. No additional changes needed.

### 2.3 Tests

`internal/otel/ringbuf_test.go`:

- `TestPushAndSnapshot`: push N < size events, verify `Snapshot()` returns chronological order.
- `TestWrapAround`: push 2*size events, verify oldest are evicted, newest are kept.
- `TestLast`: push known events, verify `Last(n)` returns correct recent subset.
- `TestLastMoreThanCount`: verify `Last(n)` clamps to available count.
- `TestStats`: push mixed EventKinds, verify counts match.
- `TestConcurrentPushSnapshot`: goroutine safety under contention (run with `-race`).
- `TestEmptySnapshot`: returns nil.
- `TestLen`: verify `Len()` tracks count correctly through fill and wrap.
- `TestDeepCopyExtra`: push event with Extra map, mutate original map, verify ring buffer copy is unchanged.

---

## Priority 3: Debug Overlay

### 3.1 Rendering Function

```go
// internal/ui/debug.go
package ui

import (
    "fmt"
    "strings"
    "time"

    "github.com/abelbrown/observer/internal/otel"
)

// debugOverlay renders the debug panel showing pipeline stats and recent events.
// Pure function with no side effects. Returns empty string if ring is nil.
func debugOverlay(ring *otel.RingBuffer, width, height int) string {
    if ring == nil {
        return ""
    }

    stats := ring.Stats()
    recent := ring.Last(20)

    // --- Stats section (keyed lookups, not map iteration) ---
    var lines []string
    lines = append(lines, DebugHeaderStyle.Render("Pipeline Stats"))
    lines = append(lines, fmt.Sprintf("  Fetches:    %d complete, %d errors",
        stats[otel.KindFetchComplete], stats[otel.KindFetchError]))
    lines = append(lines, fmt.Sprintf("  Embeds:     %d complete, %d batch, %d errors",
        stats[otel.KindEmbedComplete], stats[otel.KindEmbedBatch], stats[otel.KindEmbedError]))
    lines = append(lines, fmt.Sprintf("  Searches:   %d started, %d complete, %d cancelled",
        stats[otel.KindSearchStart], stats[otel.KindSearchComplete], stats[otel.KindSearchCancel]))
    lines = append(lines, fmt.Sprintf("  Reranks:    %d cosine, %d cross-encoder",
        stats[otel.KindCosineRerank], stats[otel.KindCrossEncoder]))
    lines = append(lines, fmt.Sprintf("  Buffer:     %d / %d events", ring.Len(), otel.DefaultRingSize))
    lines = append(lines, "")

    // --- Recent events section ---
    lines = append(lines, DebugHeaderStyle.Render("Recent Events"))
    for _, e := range recent {
        age := time.Since(e.Time)
        ageStr := formatAge(age)

        line := fmt.Sprintf("  %6s  %-22s", ageStr, string(e.Kind))
        if e.Msg != "" {
            line += "  " + truncateRunes(e.Msg, 40)
        }
        if e.Err != "" {
            line += "  ERR:" + truncateRunes(e.Err, 30)
        }
        if e.QueryID != "" {
            qidDisplay := e.QueryID
            if len(qidDisplay) > 8 {
                qidDisplay = qidDisplay[:8]
            }
            line += fmt.Sprintf("  qid:%s", qidDisplay)
        }
        lines = append(lines, line)
    }

    // Truncate to fit terminal height
    maxHeight := height - 4
    if maxHeight < 1 {
        maxHeight = 1
    }
    if len(lines) > maxHeight {
        lines = lines[:maxHeight]
    }

    panelWidth := 76
    if panelWidth > width-4 {
        panelWidth = width - 4
    }
    if panelWidth < 20 {
        panelWidth = 20
    }

    content := strings.Join(lines, "\n")
    return DebugPanel.Width(panelWidth).Render(content)
}

// formatAge formats a duration as a compact human string.
// Handles negative durations from clock skew by clamping to "0ms".
func formatAge(d time.Duration) string {
    if d < 0 {
        return "0ms"
    }
    switch {
    case d < time.Second:
        return fmt.Sprintf("%dms", d.Milliseconds())
    case d < time.Minute:
        return fmt.Sprintf("%.1fs", d.Seconds())
    default:
        return fmt.Sprintf("%.0fm", d.Minutes())
    }
}
```

**Key design decisions:**

- **Keyed stat lookups, not map iteration**: The stats section uses `stats[otel.KindFetchComplete]` (direct map lookup by known key) instead of iterating `range stats`. This eliminates random ordering that would cause the overlay to flicker between frames. Missing keys return 0 (the zero value for `int`), which is correct.

- **Negative duration handling**: `formatAge` clamps negative durations (from clock skew or NTP adjustments) to "0ms" instead of displaying confusing output like "-1234ms".

- **Terminal height truncation**: The panel truncates to `height - 4` lines (leaving room for borders and status bar). Guards against both zero and negative maxHeight.

### 3.2 Styles

Add to `internal/ui/styles.go`:

```go
// DebugPanel style for the debug overlay.
var DebugPanel = lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    BorderForeground(lipgloss.Color("63")).
    Foreground(lipgloss.Color("252")).
    Background(lipgloss.Color("235")).
    Padding(1, 2)

// DebugHeaderStyle for section headers in the debug panel.
var DebugHeaderStyle = lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("212")).
    MarginBottom(0)
```

### 3.3 Toggle Key (Shift+D)

In `handleKeyMsg`, in the normal mode key handling section:

```go
case "D":  // Shift+D toggles debug overlay
    a.debugVisible = !a.debugVisible
    return a, nil
```

Also add to the `rerankPending` mode section so debug is accessible during reranking.

**Why `Shift+D`:**
- `?` conflicts with help conventions in vim-style TUIs (less, vim, mutt all use it)
- `F12` is not reliably transmitted by all terminal emulators
- `Shift+D` is deliberate, non-conflicting, and consistent with TUI conventions where capital letters indicate modifier actions
- `d` (lowercase) is reserved for potential future features (delete, dismiss)
- The key does not conflict with any existing binding: q, j, k, g, G, r, f, /

### 3.4 View Integration

The debug panel replaces main content when visible (full-takeover approach):

```go
func (a App) View() string {
    if !a.ready {
        return "Loading..."
    }

    // Debug overlay: full takeover
    if a.debugVisible && a.ring != nil {
        contentHeight := a.height - 1
        overlay := debugOverlay(a.ring, a.width, contentHeight)
        statusBar := DebugStatusBar(a.width)
        return overlay + "\n" + statusBar
    }

    // ... existing normal rendering unchanged ...
}
```

Where `DebugStatusBar` renders:

```go
func DebugStatusBar(width int) string {
    keys := StatusBarKey.Render("D") + StatusBarText.Render(":close") +
        "  " +
        StatusBarKey.Render("Events") + StatusBarText.Render(" buffered")
    return StatusBar.Width(width).Render("  [DEBUG]  " + keys)
}
```

Add hint to normal status bar: `StatusBarKey.Render("D") + StatusBarText.Render(":debug")`

**Why full takeover (not floating overlay):**
- lipgloss does not support true z-ordering / compositing
- Full takeover avoids z-order, transparency, and overlap headaches
- Gives the debug view the entire terminal area for maximum readability
- Simple to implement: one `if` at the top of `View()`

**No refresh ticker needed:** The overlay updates on every `View()` call, which happens after every `Update()` cycle. The coordinator's continuous event emission triggers UI updates, so the overlay naturally stays fresh.

### 3.5 Tests

`internal/ui/debug_test.go`:

- `TestDebugOverlayNilRing`: returns empty string.
- `TestDebugOverlayRendersStats`: push known events, verify output contains expected stat lines.
- `TestDebugOverlayRecentEvents`: verify event lines appear with age and kind.
- `TestDebugOverlayTruncation`: verify panel respects height constraints.
- `TestDebugToggle`: simulate "D" key, verify `debugVisible` toggles.
- `TestFormatAge`: test sub-second, second, minute ranges, and negative duration.
- `TestFormatAgeNegative`: verify negative duration returns "0ms".

---

## Priority 4: Query ID

### 4.1 ID Generation (crypto/rand)

```go
// In internal/ui/app.go
import "crypto/rand"

// newQueryID generates a short random hex string for search correlation.
// 8 bytes = 16 hex chars. Unique within and across sessions.
func newQueryID() string {
    var b [8]byte
    _, _ = rand.Read(b[:])
    return fmt.Sprintf("%x", b[:])
}
```

**Why `crypto/rand` random hex, not monotonic atomic counter:**
- Survives log rotation and process restarts (unique across sessions). A counter like "q1" repeats every session, making cross-session `jq` queries ambiguous.
- 16 hex chars is short enough for JSONL readability but has no collision risk (2^64 space).
- No package-global state. The counter approach (`var querySeq atomic.Uint64`) leaks between parallel tests, producing flaky assertions.
- Easy to copy-paste into `jq` filter commands.

### 4.2 Message Type Changes

In `internal/ui/messages.go`, add `QueryID string` to all search-related message types:

```go
type QueryEmbedded struct {
    Query     string
    Embedding []float32
    QueryID   string  // search correlation ID
    Err       error
}

type SearchPoolLoaded struct {
    Items      []store.Item
    Embeddings map[string][]float32
    QueryID    string  // search correlation ID
    Err        error
}

type RerankComplete struct {
    Query   string
    Scores  []float32
    QueryID string  // search correlation ID
    Err     error
}

type EntryReranked struct {
    Index   int
    Score   float32
    QueryID string  // search correlation ID
    Err     error
}
```

**Backward compatibility:** Adding a string field with zero value `""` to existing structs does NOT break any existing code. All existing field accesses continue to work. Tests that construct these types without the field get `QueryID: ""` (harmless).

### 4.3 AppConfig/Closure Signature Changes

**App struct addition:**

```go
type App struct {
    // ... existing fields ...
    queryID string  // current search correlation ID; empty when no search active
}
```

**AppConfig closure signatures change to accept queryID:**

```go
type AppConfig struct {
    LoadItems       func() tea.Cmd
    LoadRecentItems func() tea.Cmd
    LoadSearchPool  func(queryID string) tea.Cmd              // CHANGED
    MarkRead        func(id string) tea.Cmd
    TriggerFetch    func() tea.Cmd
    EmbedQuery      func(query string, queryID string) tea.Cmd // CHANGED
    ScoreEntry      func(query string, doc string, index int, queryID string) tea.Cmd // CHANGED
    BatchRerank     func(query string, docs []string, queryID string) tea.Cmd         // CHANGED
    Embeddings      map[string][]float32
    Obs             ObsConfig
}
```

Corresponding App struct fields:

```go
loadSearchPool  func(queryID string) tea.Cmd
embedQuery      func(query string, queryID string) tea.Cmd
scoreEntry      func(query string, doc string, index int, queryID string) tea.Cmd
batchRerank     func(query string, docs []string, queryID string) tea.Cmd
```

**Update closures in `cmd/observer/main.go`:**

```go
LoadSearchPool: func(queryID string) tea.Cmd {
    return func() tea.Msg {
        // ... existing load logic ...
        return ui.SearchPoolLoaded{
            Items: items, Embeddings: filteredEmbeddings,
            QueryID: queryID,
        }
    }
},
EmbedQuery: func(query string, queryID string) tea.Cmd {
    return func() tea.Msg {
        emb, err := embedder.EmbedQuery(ctx, query)
        return ui.QueryEmbedded{
            Query: query, Embedding: emb, Err: err,
            QueryID: queryID,
        }
    }
},
BatchRerank: func(query string, docs []string, queryID string) tea.Cmd {
    return func() tea.Msg {
        scores, err := jinaReranker.Rerank(ctx, query, docs)
        // ... existing score extraction ...
        return ui.RerankComplete{
            Query: query, Scores: result, Err: err,
            QueryID: queryID,
        }
    }
},
ScoreEntry: func(query string, doc string, index int, queryID string) tea.Cmd {
    return func() tea.Msg {
        // ... existing scoring logic ...
        return ui.EntryReranked{Index: index, Score: score, Err: err, QueryID: queryID}
    }
},
```

### 4.4 Stale-Check Enhancement

In message handlers, add QueryID-based staleness detection. This is additive (never removes existing checks) and only activates when QueryID is non-empty (backward compatible with tests that omit it):

```go
// In QueryEmbedded handler:
case QueryEmbedded:
    a.embeddingPending = false
    if msg.Err != nil {
        a.statusText = ""
        return a, nil
    }
    // Enhanced stale check: QueryID takes precedence if available
    if msg.QueryID != "" && msg.QueryID != a.queryID {
        return a, nil  // stale result from previous search
    }
    if msg.Query == a.filterInput.Value() {
        // ... existing logic ...
    }

// In RerankComplete handler:
case RerankComplete:
    if !a.rerankPending {
        return a, nil
    }
    if msg.QueryID != "" && msg.QueryID != a.queryID {
        return a, nil  // stale
    }
    // ... existing logic ...

// In EntryReranked handler:
case EntryReranked:
    if msg.QueryID != "" && msg.QueryID != a.queryID {
        return a, nil  // stale
    }
    // ... existing logic ...

// In SearchPoolLoaded handler:
case SearchPoolLoaded:
    if msg.QueryID != "" && msg.QueryID != a.queryID {
        return a, nil  // stale
    }
    // ... existing logic ...
```

This fixes a subtle bug: if the user types the same query twice (e.g., clears and re-searches "climate"), the old text-based stale check (`msg.Query != a.filterInput.Value()`) would accept results from the first search. QueryID makes each search uniquely identifiable.

**Generate queryID in `submitSearch()`:**

```go
func (a App) submitSearch() (tea.Model, tea.Cmd) {
    query := a.filterInput.Value()
    if query == "" {
        a.searchActive = false
        a.filterInput.Blur()
        return a, nil
    }

    a.searchActive = false
    a.filterInput.Blur()
    // ... existing save logic ...

    a.searchStart = time.Now()
    a.queryID = newQueryID()  // NEW

    a.logger.Emit(otel.Event{
        Kind:    otel.KindSearchStart,
        Level:   otel.LevelInfo,
        Comp:    "ui",
        QueryID: a.queryID,
        Query:   query,
    })

    var cmds []tea.Cmd
    if a.loadSearchPool != nil {
        a.searchPoolPending = true
        cmds = append(cmds, a.loadSearchPool(a.queryID))
    }
    if a.embedQuery != nil {
        a.embeddingPending = true
        cmds = append(cmds, a.embedQuery(query, a.queryID))
    }
    // ... rest unchanged ...
}
```

**Clear queryID in `clearSearch()`:**

```go
func (a App) clearSearch() (tea.Model, tea.Cmd) {
    // ... existing clear logic ...
    a.queryID = ""

    a.logger.Emit(otel.Event{
        Kind:  otel.KindSearchCancel,
        Level: otel.LevelInfo,
        Comp:  "ui",
    })
    // ... rest unchanged ...
}
```

All search-related event emissions include `QueryID: a.queryID`.

### 4.5 Tests

Tests in `app_test.go` (integrated into existing test file):

- `TestNewQueryIDUniqueness`: generate 1000 IDs, verify all unique.
- `TestNewQueryIDFormat`: verify 16 hex chars.
- `TestStaleQueryIDCheck`: submit search, change queryID, verify old QueryEmbedded is discarded.
- `TestSameQueryDifferentID`: submit same query text twice, verify first result is discarded via QueryID.
- Update all existing tests that construct `AppConfig` or message types with new closure signatures (mechanical: add `queryID string` parameter, add `QueryID: "test"` to message literals).

---

## Priority 5: OBSERVER_TRACE

### 5.1 Trace Gate

```go
// internal/otel/trace.go
package otel

import "os"

// traceEnabled is set once at package init. Never modified at runtime.
var traceEnabled = os.Getenv("OBSERVER_TRACE") != ""

// TraceEnabled reports whether OBSERVER_TRACE is set.
// Inlineable by the Go compiler (simple accessor of package-level var).
// Zero cost: single boolean check when false.
func TraceEnabled() bool {
    return traceEnabled
}
```

**Zero-cost proof:**

When `OBSERVER_TRACE` is not set:
- `TraceEnabled()` returns `false` (inlined by compiler to a single boolean load)
- The branch in `Update()` is not taken
- No defer is registered
- No allocations occur
- No reflect calls happen

Verify with:
```bash
go build -gcflags='-m' ./internal/otel/ 2>&1 | grep TraceEnabled
# Expected: "can inline TraceEnabled"
```

### 5.2 traceMsg — Exhaustive Type Switch

```go
// Added to internal/ui/app.go

// traceMsg logs a trace event for the incoming message.
// Only called when OBSERVER_TRACE is set.
// Uses exhaustive type switch with %T for unknown types (never %+v).
func (a App) traceMsg(msg tea.Msg) {
    e := otel.Event{
        Kind:  otel.KindMsgReceived,
        Level: otel.LevelDebug,
        Comp:  "trace",
    }

    var typeName string
    switch m := msg.(type) {
    case tea.KeyMsg:
        typeName = "KeyMsg"
        e.Extra = map[string]any{"key": m.String()}
    case tea.WindowSizeMsg:
        typeName = "WindowSizeMsg"
        e.Extra = map[string]any{"w": m.Width, "h": m.Height}
    case spinner.TickMsg:
        typeName = "spinner.TickMsg"
    case ItemsLoaded:
        typeName = "ItemsLoaded"
        e.Count = len(m.Items)
        if m.Err != nil {
            e.Err = m.Err.Error()
        }
    case FetchComplete:
        typeName = "FetchComplete"
        e.Source = m.Source
        e.Count = m.NewItems
        if m.Err != nil {
            e.Err = m.Err.Error()
        }
    case QueryEmbedded:
        typeName = "QueryEmbedded"
        e.Query = m.Query
        e.QueryID = m.QueryID
        e.Dims = len(m.Embedding)
    case SearchPoolLoaded:
        typeName = "SearchPoolLoaded"
        e.QueryID = m.QueryID
        e.Count = len(m.Items)
    case RerankComplete:
        typeName = "RerankComplete"
        e.QueryID = m.QueryID
        e.Query = m.Query
    case EntryReranked:
        typeName = "EntryReranked"
        e.QueryID = m.QueryID
        e.Extra = map[string]any{"index": m.Index, "score": m.Score}
    case ItemMarkedRead:
        typeName = "ItemMarkedRead"
        e.Source = m.ID
    case RefreshTick:
        typeName = "RefreshTick"
    default:
        // %T gives the type name without allocating the full value string.
        // NEVER use %+v here — it triggers full reflection and can allocate
        // megabytes on large message types (e.g., SearchPoolLoaded with 2000 items).
        typeName = fmt.Sprintf("%T", msg)
    }

    e.Msg = typeName
    a.logger.Emit(e)
}
```

**Key design decisions:**

- **Exhaustive type switch, not `%+v`**: Each known message type has an explicit case that extracts only the relevant fields. This prevents the latent OOM vector where `fmt.Sprintf("%+v", msg)` on a `SearchPoolLoaded` with 2000 items allocates megabytes before any truncation.
- **`%T` for unknown types**: The default case uses `fmt.Sprintf("%T", msg)` which returns only the type name (e.g., `"bubbletea.sequenceMsg"`), not the value. This is safe regardless of message size.
- **No truncation needed**: Because we never format the full value, there is no need for a 200-char truncation step.

### 5.3 traceHandled — Defer Timing

```go
// traceHandled logs a trace event recording how long Update() took.
// Only called via defer when OBSERVER_TRACE is set.
func (a App) traceHandled(startTime time.Time) {
    a.logger.Emit(otel.Event{
        Kind:  otel.KindMsgHandled,
        Level: otel.LevelDebug,
        Comp:  "trace",
        Dur:   time.Since(startTime),
    })
}
```

### 5.4 Integration (4 lines in Update)

```go
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if otel.TraceEnabled() {
        a.traceMsg(msg)
        defer a.traceHandled(time.Now())
    }

    switch msg := msg.(type) {
    // ... all existing cases unchanged ...
    }
}
```

This is exactly 4 lines added to `Update()` (including braces). When `OBSERVER_TRACE` is not set, the cost is one boolean comparison per `Update()` call.

When enabled, every message produces two JSONL lines:
- `trace.msg_received` — type name, key fields
- `trace.msg_handled` — duration of `Update()` processing

### 5.5 Tests and Benchmarks

`internal/otel/trace_test.go`:

- `TestTraceEnabledDefault`: verify false when env unset.
- `TestTraceEnabledSet`: verify true when `OBSERVER_TRACE=1`.

Benchmark tests in `app_test.go`:

```go
func BenchmarkUpdateNoTrace(b *testing.B) {
    os.Unsetenv("OBSERVER_TRACE")
    app := NewAppWithConfig(AppConfig{Obs: ui.ObsConfig{Logger: otel.NewNullLogger()}})
    msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        app.Update(msg)
    }
}

func BenchmarkUpdateWithTrace(b *testing.B) {
    os.Setenv("OBSERVER_TRACE", "1")
    defer os.Unsetenv("OBSERVER_TRACE")
    app := NewAppWithConfig(AppConfig{Obs: ui.ObsConfig{Logger: otel.NewNullLogger()}})
    msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        app.Update(msg)
    }
}
```

Expected: `BenchmarkUpdateNoTrace` should show 0 allocs/op.

---

## Appendix A: Decision Log

| # | Decision | Chosen | Rejected | Rationale |
|---|----------|--------|----------|-----------|
| 1 | Package name | `internal/otel/` | `internal/event/`, `internal/obs/` | Ergonomic at call sites; internal so no OpenTelemetry confusion; `obs` collides with "observer" |
| 2 | Logger pattern | Dependency injection | Global singleton | Consistent with codebase (AppConfig closures, Coordinator ctor); testable via `NewNullLogger()` |
| 3 | Event structure | Fixed fields + `Extra` | `map[string]any` Data bag, `json.RawMessage` | Greppable with jq; no double serialization; type-safe field access |
| 4 | Ring buffer lock | `sync.Mutex` | `sync.RWMutex` | Write-heavy workload; RWMutex has higher overhead and writer-starvation risk |
| 5 | Debug toggle key | `Shift+D` | `?`, `F12` | Non-conflicting; deliberate; reliable in all terminals |
| 6 | Debug view mode | Full takeover | Floating overlay / top panel | No compositing complexity in lipgloss; full terminal width |
| 7 | Trace approach | Inline `if` check | `traceModel` wrapper struct | Truly zero-cost when off (no indirection, no allocation); 4 lines in Update() |
| 8 | Trace default case | `%T` (type name) | `%+v` (full value) | Prevents latent OOM on large message types |
| 9 | QueryID format | `crypto/rand` hex (8 bytes) | Monotonic atomic counter, `time.UnixNano` | Unique across sessions; no package-global state; no parallel-test flakiness |
| 10 | Severity levels | Yes (`Level` field) | No levels | Enables `jq 'select(.level=="error")'`; zero cost (string field, omitempty) |
| 11 | Component field | Yes (`Comp` field) | Encoded in Kind prefix only | Explicit filtering by subsystem; compact "comp" key |
| 12 | Convenience helpers | Yes (`Info()`, `Error()`, `Warn()`) | Raw `Emit(Event{...})` only | Reduce boilerplate at common call sites |
| 13 | Log files | Separate `events.jsonl` + `observer.log` | Single shared file | Prevents JSONL corruption from Bubble Tea unstructured lines |
| 14 | Writer model | Async channel + drain goroutine | Synchronous `json.Encoder` under mutex | Prevents file I/O from blocking UI goroutine |
| 15 | File permissions | `0600` | `0644` | Log contains user queries (privacy) |
| 16 | Session tracking | `session_id` field | None | Groups events across log-append sessions |
| 17 | AppConfig grouping | `ObsConfig` sub-struct | Flat fields on AppConfig | Prevents god-object growth |
| 18 | Ring buffer Extra | Deep copy in `Push()`/`push()` | Shallow copy (reference) | Prevents aliasing bugs from callers mutating maps |
| 19 | Lock ordering | Documented invariant in `logger.go` | Undocumented | Prevents future deadlocks |
| 20 | Negative ages | Clamp to "0ms" | Display raw negative | Graceful handling of clock skew |
| 21 | Log rotation | Deferred; design supports it | Implement now | Append-only + session_id + separate files = rotation-ready without code changes |
| 22 | slog integration | Custom otel package | stdlib `slog.JSONHandler` | Ring buffer integration needs custom handler regardless; thin custom package (~200 LOC) is clearer |

---

## Appendix B: Goroutine Safety Map

| Component | Writers | Readers | Mechanism |
|-----------|---------|---------|-----------|
| `Logger.Emit()` | Any goroutine | N/A (write to channel) | Non-blocking channel send; `atomic.Uint64` for drop counter |
| `Logger.drain()` | N/A | Reads from channel | Single drain goroutine; sequential processing |
| `Logger.buf` | `SetRingBuffer` (main goroutine, once) | `drain()` goroutine | `sync.Mutex` protects read of `buf` pointer in drain |
| `Logger.dropped` | Any goroutine (via `Emit`) | `Close()`, `Dropped()` | `atomic.Uint64` |
| `Logger.sessionID` | Set once in `NewLogger` | Any goroutine (via `Emit`) | Immutable after init |
| `RingBuffer.push()` | `drain()` goroutine only | N/A | Single-writer guarantee (drain goroutine) |
| `RingBuffer.Push()` | Any goroutine | N/A | `sync.Mutex` |
| `RingBuffer` public reads | N/A | `App.View()` (main goroutine) | `sync.Mutex` |
| `App.logger` | Shared pointer | Shared pointer | Immutable after init |
| `App.ring` | Shared pointer | Shared pointer | Immutable after init |
| `App.queryID` | `App.Update()` (main goroutine) | Closures (any goroutine) | Captured by value in closures at dispatch time |
| `traceEnabled` | Never written at runtime | Any goroutine | Read-only after package init |

**Summary:** The goroutine safety model is straightforward:
- The Logger uses a buffered channel for non-blocking writes from any goroutine, with a single drain goroutine handling I/O.
- The RingBuffer is written to only by the drain goroutine (via `push()`), and read by the UI goroutine (via `Last()`/`Stats()`/`Snapshot()`). The `sync.Mutex` protects the read path.
- App fields are only mutated on the main Bubble Tea goroutine (`Update`/`View`).
- Closures capture values by copy at dispatch time, so they are safe on any goroutine.
- `traceEnabled` and `sessionID` are set once at startup, never modified.

---

## Appendix C: File Inventory

### New Files

| File | Lines (est.) | Priority | Purpose |
|------|-------------|----------|---------|
| `internal/otel/event.go` | ~100 | P1 | Event type, EventKind constants, Level constants, MarshalJSON |
| `internal/otel/logger.go` | ~130 | P1 | Logger struct, async writer, Close(), convenience helpers |
| `internal/otel/logger_test.go` | ~100 | P1 | Logger and Event serialization tests |
| `internal/otel/ringbuf.go` | ~130 | P2 | RingBuffer type, push/Push, Snapshot, Last, Stats |
| `internal/otel/ringbuf_test.go` | ~120 | P2 | Ring buffer tests |
| `internal/otel/trace.go` | ~15 | P5 | TraceEnabled(), traceEnabled var |
| `internal/otel/trace_test.go` | ~40 | P5 | Trace gate tests |
| `internal/ui/debug.go` | ~120 | P3 | debugOverlay(), DebugStatusBar(), formatAge() |
| `internal/ui/debug_test.go` | ~80 | P3 | Debug overlay tests |

### Modified Files

| File | Lines Changed (est.) | Priorities | Changes |
|------|---------------------|------------|---------|
| `cmd/observer/main.go` | ~50 | P1, P2, P4 | Logger/RingBuffer creation; separate log files; closure signature updates for QueryID; startup/shutdown events |
| `internal/ui/app.go` | ~120 | P1, P3, P4, P5 | ObsConfig/logger/ring fields; debugVisible toggle; queryID generation/threading; traceMsg/traceHandled; View() debug panel; replace 6 log.Printf |
| `internal/ui/messages.go` | ~10 | P4 | Add QueryID to 4 message types |
| `internal/ui/styles.go` | ~15 | P3 | Add DebugPanel, DebugHeaderStyle |
| `internal/ui/stream.go` | ~5 | P3 | Add "D:debug" hint to status bar |
| `internal/ui/app_test.go` | ~70 | P1, P4, P5 | NewNullLogger in tests; QueryID in message literals; closure signature updates; benchmarks |
| `internal/coord/coordinator.go` | ~50 | P1 | Add Logger field; replace 6 log.Printf; add fetch start/complete brackets |
| `internal/coord/coordinator_test.go` | ~10 | P1 | Pass nil/NullLogger to NewCoordinator |
| `internal/fetch/clarion.go` | ~10 | P1 | Add Logger field; replace 1 log.Printf |
| `internal/fetch/clarion_test.go` | ~5 | P1 | Pass nil/NullLogger to ClarionProvider |

### Totals

- **New code:** ~835 lines across 9 new files
- **Changed code:** ~345 lines across 10 existing files
- **Grand total:** ~1180 lines of work

---

## Appendix D: JSONL Output Examples and jq Queries

### Sample `observer.events.jsonl` output

After all priorities are implemented:

```jsonl
{"t":"2026-01-29T14:32:01.123Z","level":"info","kind":"sys.startup","comp":"main","session_id":"a1b2c3d4e5f67890","msg":"observer starting"}
{"t":"2026-01-29T14:32:01.456Z","level":"info","kind":"fetch.start","comp":"coord","session_id":"a1b2c3d4e5f67890"}
{"t":"2026-01-29T14:32:05.789Z","level":"info","kind":"fetch.complete","comp":"coord","session_id":"a1b2c3d4e5f67890","dur_ms":4333.2,"count":47}
{"t":"2026-01-29T14:32:06.012Z","level":"info","kind":"embed.batch","comp":"coord","session_id":"a1b2c3d4e5f67890","count":47,"dur_ms":1200.5}
{"t":"2026-01-29T14:35:12.345Z","level":"info","kind":"search.start","comp":"ui","session_id":"a1b2c3d4e5f67890","qid":"f8e7d6c5b4a39281","query":"climate policy"}
{"t":"2026-01-29T14:35:12.678Z","level":"info","kind":"search.pool","comp":"ui","session_id":"a1b2c3d4e5f67890","qid":"f8e7d6c5b4a39281","count":2847,"dur_ms":333.1}
{"t":"2026-01-29T14:35:13.012Z","level":"info","kind":"search.query_embed","comp":"ui","session_id":"a1b2c3d4e5f67890","qid":"f8e7d6c5b4a39281","dur_ms":667.0,"dims":1024,"query":"climate policy"}
{"t":"2026-01-29T14:35:13.045Z","level":"info","kind":"search.cosine_rerank","comp":"ui","session_id":"a1b2c3d4e5f67890","qid":"f8e7d6c5b4a39281","dur_ms":700.0,"count":2847}
{"t":"2026-01-29T14:35:13.456Z","level":"info","kind":"search.cross_encoder","comp":"ui","session_id":"a1b2c3d4e5f67890","qid":"f8e7d6c5b4a39281","count":30,"extra":{"batch":true}}
{"t":"2026-01-29T14:35:14.789Z","level":"info","kind":"search.complete","comp":"ui","session_id":"a1b2c3d4e5f67890","qid":"f8e7d6c5b4a39281","dur_ms":2444.0}
{"t":"2026-01-29T14:35:15.001Z","level":"warn","kind":"fetch.error","comp":"fetch","session_id":"a1b2c3d4e5f67890","source":"Reuters","err":"context deadline exceeded"}
{"t":"2026-01-29T14:40:00.000Z","level":"info","kind":"sys.shutdown","comp":"main","session_id":"a1b2c3d4e5f67890","msg":"observer stopping"}
```

With `OBSERVER_TRACE=1`, additional lines interleave:

```jsonl
{"t":"2026-01-29T14:35:14.789Z","level":"debug","kind":"trace.msg_received","comp":"trace","session_id":"a1b2c3d4e5f67890","msg":"RerankComplete","qid":"f8e7d6c5b4a39281"}
{"t":"2026-01-29T14:35:14.790Z","level":"debug","kind":"trace.msg_handled","comp":"trace","session_id":"a1b2c3d4e5f67890","dur_ms":0.8}
```

### jq Query Examples

```bash
# All events from a specific search
jq 'select(.qid == "f8e7d6c5b4a39281")' ~/.observer/observer.events.jsonl

# All search events
jq 'select(.kind | startswith("search."))' ~/.observer/observer.events.jsonl

# Slow operations (>1 second)
jq 'select(.dur_ms > 1000)' ~/.observer/observer.events.jsonl

# All errors
jq 'select(.level == "error")' ~/.observer/observer.events.jsonl

# All coordinator events
jq 'select(.comp == "coord")' ~/.observer/observer.events.jsonl

# Events from current session only
jq 'select(.session_id == "a1b2c3d4e5f67890")' ~/.observer/observer.events.jsonl

# Average search latency
jq -s '[.[] | select(.kind == "search.complete") | .dur_ms] | add / length' ~/.observer/observer.events.jsonl

# Event count by kind
jq -s 'group_by(.kind) | map({kind: .[0].kind, count: length}) | sort_by(-.count)' ~/.observer/observer.events.jsonl

# Timeline of a specific search (sorted by time)
jq 'select(.qid == "f8e7d6c5b4a39281")' ~/.observer/observer.events.jsonl | jq -s 'sort_by(.t)'

# Trace: find slow Update() cycles
jq 'select(.kind == "trace.msg_handled" and .dur_ms > 10)' ~/.observer/observer.events.jsonl
```

---

## Appendix E: Attribution

This implementation plan was produced through a multi-stage collaborative process:

1. **Pair-programmed synthesis** (Claude Opus 4.5 + Gemini 3): Produced the unified architecture with typed events, ring buffer design, debug overlay, QueryID threading, and trace system. Claude provided the implementation-ready code and detailed integration plan; Gemini contributed severity levels, component tags, and convenience helpers.

2. **Adversarial review** (GPT-5, Grok 4, Gemini 3, Claude 4.5): Four frontier models independently reviewed the v1 plan as grumpy senior engineers. Identified critical issues: separate log files, async writer, Logger.Close(), file permissions, deep copy Extra, session_id, ObsConfig sub-struct, lock ordering documentation, negative duration handling, and `%T` vs `%+v` in trace.

3. **v2 synthesis** (Claude Opus 4.5): Merged the best elements from all three sources into this definitive document, resolving conflicts in favor of production-hardened correctness.
