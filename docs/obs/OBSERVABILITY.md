# Observability for Observer

Observer is a TUI app. The terminal is occupied by the UI, so traditional observability (dashboards, `/metrics` endpoints, stdout logging) doesn't apply. Everything has to be **out-of-band**. The log file is the observability plane, not the UI.

This document captures the observability strategy, informed by a brain trust consultation (GPT-5, Grok 4, Gemini 3, Claude 4.5) and analysis of the current codebase.

## Current State

### What Exists

**File-based logging.** All log output redirects to `~/.observer/observer.log` via Bubble Tea's `LogToFile()`. Uses Go's standard `log` package — unstructured, ad-hoc `log.Printf` strings.

**Search timing.** The `searchStart` field in the `App` struct tracks when a search began. Five log statements measure phase latencies:

```
search: query embedded in 142ms (1024 dims)
search: cosine rerank applied in 1ms (847 items)
search: pool loaded in 310ms (2104 items, 1987 embeddings)
search: cross-encoder rerank complete in 1530ms
search: starting cross-encoder rerank (30 items, batch=true)
```

**Pipeline error logging.** The coordinator logs embedding failures, batch fallbacks, and save errors. The fetch layer logs per-source errors. All go to the same log file.

**Status bar.** The `statusText` field shows a spinner + activity text during async operations. This is the only user-facing observability — and it's minimal by design.

### What's Missing

- No structured events (logs are free-form strings, not machine-parseable)
- No in-memory event buffer (can only observe via log file tailing)
- No debug overlay or toggle (no way to see pipeline state without leaving the TUI)
- No message tracing (Bubble Tea messages are invisible)
- No startup/shutdown health summary
- No query correlation (can't group events belonging to the same search)
- No fetch success rate, embedding queue depth, retry counts, or API latency histograms

## The TUI Observability Problem

Gemini named it well: **Heisenberg's UI**. Observing via stdout destroys the thing you're observing. In a web app, observability runs alongside the product. In a TUI, observability runs behind the product.

Three implications:

1. **The log file is the primary observability surface.** It needs to be structured, complete, and machine-readable — not an afterthought for error dumping.
2. **Bubble Tea's message-passing is a free event stream.** Every state change is a discrete `tea.Msg`. Wrapping `Update()` gives you 100% pipeline coverage with minimal instrumentation.
3. **Any in-TUI observability must be toggleable.** Screen real estate is the constraint.

## Architecture: Three Layers

### Layer 1: Structured JSONL Events

Replace ad-hoc `log.Printf` calls with typed event structs serialized as JSONL:

```json
{"event":"search.started","query":"climate","t":0}
{"event":"search.query_embedded","query":"climate","t":142,"dims":1024}
{"event":"search.cosine_ranked","query":"climate","t":143,"items":847}
{"event":"search.pool_loaded","query":"climate","t":310,"items":2104}
{"event":"search.rerank_started","query":"climate","t":312,"items":30,"batch":true}
{"event":"search.rerank_complete","query":"climate","t":1842}
```

This is greppable, parseable, and enables tooling. A companion CLI (`go run ./cmd/search-metrics`) could read the log and print:

```
Last 5 searches:
Query          Embed   Cosine  Pool    Rerank  Total
"climate"      142ms   1ms     310ms   1530ms  1842ms
"fed rates"    138ms   1ms     295ms   1210ms  1508ms
```

**Query ID correlation** (GPT-5): Tag every event with a query ID so events belonging to the same search are groupable. E.g. `"qid":"q41"`.

### Layer 2: In-Memory Ring Buffer

A fixed-size circular buffer (1000 events) in the `App` struct. Zero allocation when not viewed. The ring buffer is the backbone — both the debug overlay and the Unix socket read from it.

```go
type Event struct {
    Time  time.Time
    Type  string
    Data  map[string]any
}

type RingBuffer struct {
    events [1024]Event
    head   int
    count  int
}
```

Events are written to both the ring buffer and the log file. The buffer exists for live inspection; the log file exists for post-mortem.

### Layer 3: Debug Overlay

A keypress (e.g., `?` or `F12`) toggles a panel showing live pipeline stats read from the ring buffer:

```
embed: 142ms | pool: 310ms | rerank: 1530ms | total: 1.8s | items: 2104->30
```

Implementation: store the last timing breakdown in the `App` struct, render it in `View()` when a `debugOverlay` bool is set. Show briefly after search completes, or persistently via toggle.

**Sparkline latencies** (Grok): Optionally render visual trend lines in the overlay:

```
Embed: ▁▂▅▃ 142ms p99
```

## Additional Capabilities

### Message Tracing (`OBSERVER_TRACE`)

Since every state change is a discrete `tea.Msg`, wrapping `Update()` gives full tracing:

```go
if os.Getenv("OBSERVER_TRACE") != "" {
    log.Printf("msg: %T %+v", msg, msg)
}
```

Full trace, opt-in, zero cost when off. This is the "Redux middleware" pattern (Gemini) applied to Bubble Tea.

### Startup/Shutdown Health Summary

Log a snapshot when the app starts and shuts down:

```json
{"event":"startup","sources":47,"sources_ok":42,"sources_err":5,"items":2104,"embedded":1987,"embed_coverage":"94.4%"}
{"event":"shutdown","uptime":"12m","searches":3,"fetches":2,"errors":["RAND: HTTP 403","SEC: HTTP 403"]}
```

Catches regressions (e.g., 5 sources permanently 403ing, embedding coverage dropping) without any user interaction.

### SLO Thresholds (Grok)

Define slow-step thresholds and surface them:

| Stage | Normal | Slow | Critical |
|-------|--------|------|----------|
| Embed query | <200ms | 200-400ms | >400ms |
| Pool load | <500ms | 500ms-1s | >1s |
| Rerank (batch) | <2s | 2-3s | >3s |
| Rerank (sequential) | <5s | 5-8s | >8s |

Slow steps get a warning in the log. Critical steps trigger an ephemeral toast in the UI.

### Unix Domain Socket (Sidecar)

Stream NDJSON events to `/tmp/observer.sock`. Connect from another terminal:

```bash
nc -U /tmp/observer.sock
```

No TUI corruption, real-time, richer than log tailing. Useful for development when you want to watch events alongside the app.

### Flight Recorder (Claude 4.5)

An always-on circular buffer that dumps to disk on crash. Combined with message recording, enables deterministic replay of bug reports.

### State Snapshot Diffing (Claude 4.5)

Log what each message changed, not just that it arrived:

```
statusText: "Embedding..." -> "Reranking..."
rerankProgress: 0 -> 12
```

Shows causality, not just events.

### Perfetto/Chrome Trace Export (GPT-5)

The 5-stage pipeline (embed -> cosine -> pool -> rerank -> display) maps to trace spans. Export to Chrome's `chrome://tracing` format for waterfall visualization.

### Nerd Mode (Gemini)

Turn latency into a user feature — show users why it's slow:

```
[done] Embedding          142ms
[done] Local Cosine         1ms
[...]  Cross-Encoder      1.2s...
```

Users forgive latency when they see progress. This is already partially implemented via the `statusText` pattern.

## Implementation Priority

### The 20%: Core Observability (Do First)

These five items give ~80% of the observability value. Each builds on the previous.

| Priority | What | Why |
|----------|------|-----|
| **1** | Structured JSONL events | Foundation for everything else. Replace `log.Printf` with typed event structs. Without this, nothing downstream is machine-readable. |
| **2** | Ring buffer in `App` struct | Last 1000 events in memory. Zero cost when unviewed. Enables both the debug overlay and the socket without touching disk. |
| **3** | Debug overlay on toggle key | Reads from ring buffer. First time you can see pipeline state without leaving the TUI. |
| **4** | Query ID on all search messages | Makes the event stream groupable. Without this, concurrent searches produce interleaved noise. |
| **5** | `OBSERVER_TRACE` env var | Full message tracing via `Update()` middleware. Opt-in, zero cost when off. The "Redux DevTools" for Bubble Tea. |

### The 80%: Deferred Big Ideas (And Why)

These ideas came out of the brain trust and are genuinely good — but each adds significant scope, complexity, or maintenance burden relative to what it buys. They're documented here so the thinking isn't lost.

| Idea | Source | What It Is | Why It's Deferred |
|------|--------|-----------|-------------------|
| **SLO thresholds + toast alerts** | Grok | Define per-stage latency budgets (e.g., embed >400ms = slow). Surface slow steps as ephemeral UI toasts. | Requires tuning thresholds empirically first. Premature without enough data from structured events (priority 1) to know what "normal" actually is. |
| **Sparkline latencies** | Grok | Render `▁▂▅▃ 142ms p99` trend lines in the debug panel. | Needs a history of measurements per stage. The ring buffer (priority 2) stores events, not aggregated time series. Would need a second data structure. Nice polish, not foundational. |
| **Flight recorder + crash dump** | Claude 4.5 | Always-on circular buffer that dumps to disk on panic. Enables post-crash analysis. | Go panics in a TUI already dump a stack trace. The ring buffer (priority 2) gives you the event history. A crash-triggered flush is an incremental add-on, not a prerequisite. |
| **State snapshot diffing** | Claude 4.5 | Log what each message *changed*: `statusText: "Embedding..." -> "Reranking..."`. Shows causality. | Requires deep-comparing the full `App` struct before/after every `Update()`. Expensive, intrusive, and the diff format is hard to get right for nested structs. Message tracing (priority 5) gets you 90% of this for free. |
| **Record/replay** | Claude 4.5 | Record message streams for deterministic bug reproduction. | Bubble Tea messages contain closures (command functions), which aren't serializable. Would need a shadow message format. High effort, niche benefit. |
| **Perfetto/Chrome trace export** | GPT-5 | Export the 5-stage pipeline as trace spans. View in `chrome://tracing` for waterfall visualization. | Great for one-off deep dives, but the pipeline is simple enough (5 stages, linear) that the debug overlay shows the same information. The export format is fiddly. Worth it if pipeline branching gets more complex. |
| **`observer bugreport` command** | GPT-5 | Dump last N events + config + build info + pprof into a zip file for user-submitted bug reports. | Observer doesn't have external users yet. When it does, this becomes high priority. Until then, the log file suffices. |
| **Unix domain socket (sidecar)** | All four | Stream NDJSON events to `/tmp/observer.sock` for live monitoring from another terminal. | Genuinely useful for development, but `tail -f ~/.observer/observer.log` works once events are structured (priority 1). The socket adds Unix-specific code, connection lifecycle management, and a failure mode (socket file cleanup). Worth adding when log tailing stops being enough. |
| **Nerd Mode** | Gemini | Show users per-stage progress: `[done] Embedding 142ms / [..] Cross-Encoder 1.2s...` | Already partially implemented via `statusText`. Full nerd mode means multiple status lines or a progress breakdown, which is a UX design question more than an observability one. Revisit when the debug overlay (priority 3) proves out the concept. |
| **Startup/shutdown health summary** | Synthesis | Log source count, error count, embedding coverage on start and stop. | Low effort, real value — but it's a feature of structured events (priority 1), not a separate system. Once events are structured, adding two more event types is trivial. It'll happen naturally. |

## Brain Trust Attribution

This strategy was developed through consultation with four frontier models. Key unique contributions:

| Model | Contribution |
|-------|-------------|
| **GPT-5** | Perfetto/Chrome trace export; `observer bugreport` command (dump last N events + config + build info into a zip); QueryID correlation |
| **Grok 4** | Sparkline latencies in debug panel; SLO thresholds with slow-step alerts; integrated "Ops Pane" concept |
| **Gemini 3** | "Heisenberg's UI" framing; "Redux middleware" pattern naming for `Update()` wrapping; shadow socket architecture; "Nerd Mode" |
| **Claude 4.5** | Flight recorder + record/replay for deterministic bug reproduction; state snapshot diffing for causality tracing |

All four independently converged on: in-app debug panel, ring buffer in memory, message middleware wrapping `Update()`, and Unix domain socket for sidecar debugging.
