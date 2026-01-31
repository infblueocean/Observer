# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

**Observer is starting fresh.** The v0.4 codebase has been archived to `archive/v0.4/`. New development should not reference or build upon the archived code without explicit instruction.

## What is Observer?

Observer is an ambient news aggregation TUI built with Go 1.24. It aggregates content from 200+ RSS/API sources (via the Clarion library), embeds them with Jina AI, stores everything in SQLite, and presents a Bubble Tea terminal interface. Core philosophy: radical transparency — no hidden algorithms, all filters visible and adjustable.

## Build Commands

```bash
# Build
go build -o observer ./cmd/observer
go build -o obs ./cmd/obs

# Test
go test ./...
go test -race ./...
go test -run TestName ./path/to/package

# Run
./observer                              # TUI (requires JINA_API_KEY)
./obs stats --db                        # DB health + pipeline counts
./obs search "query"                    # two-stage search debug
./obs search --cosine-only "query"      # cosine stage only
./obs backfill                          # batch embed items missing embeddings
./obs backfill --dry-run                # check counts without embedding
./obs events --tail 20                  # last 20 events
./obs events -f --level warn            # follow warns+errors
./obs rerank                            # Ollama reranker validation
```

## Architecture

### Package Structure

```
cmd/observer/       Main entry point — wires dependencies, starts TUI
cmd/obs/            Unified debug & maintenance CLI (backfill, stats, search, rerank, events)
internal/coord/     Coordinator — background fetch + embedding pipeline
internal/embed/     Embedder interface + Jina API and Ollama implementations
internal/fetch/     RSS/source fetching via Clarion library
internal/filter/    Item filtering, dedup, cosine/cross-encoder reranking
internal/otel/      Structured observability — async JSONL logger, ring buffer, event types
internal/rerank/    Reranker interface + Jina API and Ollama implementations
internal/store/     SQLite store (items, embeddings, read state)
internal/ui/        Bubble Tea TUI (App model, messages, styles, stream rendering)
```

### Dependency Injection Pattern

The UI has zero direct dependencies on store, embedder, or reranker. `cmd/observer/main.go` wires everything by injecting callback functions via `ui.AppConfig`:

```
main.go creates: store, embedder, reranker, provider, logger
    ↓ injects into AppConfig as closures
ui.App receives: LoadItems, LoadRecentItems, LoadSearchPool, MarkRead,
                 TriggerFetch, EmbedQuery, BatchRerank
```

All async work returns `tea.Msg` types defined in `internal/ui/messages.go`. The UI never calls external services directly.

### Coordinator Pipeline

`internal/coord/coordinator.go` runs two background loops:

1. **Fetch loop** (5-min interval): calls Clarion provider → saves to SQLite → sends `FetchComplete` to UI via `program.Send()`
2. **Embedding worker** (2-sec interval): queries items with NULL embeddings → embeds in batches of 100 → stores vectors back to SQLite

The coordinator detects `embed.BatchEmbedder` at runtime and uses batch calls when available (Jina), falling back to sequential (Ollama).

### Two-Stage UI Loading

On startup, the UI loads items in two stages for fast first paint:

1. **Stage 1:** `LoadRecentItems` — last 1h, unread only (appears instantly)
2. **Stage 2:** `LoadItems` — full 24h corpus (chains after Stage 1 completes via `fullLoaded` flag)

Both stages apply: SemanticDedup (0.85 threshold) → LimitPerSource (50).

### Search Flow

1. Press `/` → search mode, text input active
2. Press Enter → parallel: load search pool (all items) + embed query via Jina
3. Query embedding arrives → `RerankByQuery()` (cosine similarity, fast)
4. Search pool arrives → `startReranking()` → Jina batch cross-encoder rerank
5. Results sorted by cross-encoder scores
6. Press Esc → `clearSearch()` restores chronological view

Pipeline state tracked by `queryID` (random hex) — stale results from cancelled searches are discarded.

### Status Bar Pattern

`statusText string` field decouples `View()` from pipeline internals. Non-empty → renders `spinner + statusText`; empty → renders normal position/key hints. Set by handlers starting async work, cleared by every completion/error/cancel path.

### Embedding & Reranking Backends

**Jina API (required for production):** `JINA_API_KEY` must be set. Rate-limited to ~80 RPM with retry on 429/5xx. Jina embedder implements `BatchEmbedder` (batches of 25). Separate `Embed()` (passage task) vs `EmbedQuery()` (query task) for asymmetric retrieval.

**Ollama (fallback/testing):** Local `mxbai-embed-large` for embeddings, cross-encoder prompting for reranking. Sequential, one item at a time. Only implements `Embedder` (not `BatchEmbedder`).

### SQLite Store

Single table `items` with embedding stored as BLOB (little-endian float32 array). WAL mode enabled for concurrent reads. Key indexes: `published_at DESC`, `source_name`, `url` (unique), and partial index on `id WHERE embedding IS NULL` for the embedding worker.

### Observability

`internal/otel/` provides structured JSONL event logging:
- Async writer with 4096-event buffered channel + ring buffer for live inspection
- Event kinds: `fetch.*`, `embed.*`, `search.*`, `store.*`, `ui.*`, `sys.*`, `trace.*`
- Events written to `~/.observer/observer.events.jsonl`
- Ring buffer powers the debug overlay in the TUI

### Data Directory

All runtime data lives in `~/.observer/`:
- `observer.db` — SQLite database (items + embeddings)
- `observer.events.jsonl` — structured event log
- `observer.log` — Bubble Tea stderr redirect

### Clarion Dependency

Clarion is a local Go module (replace directive in `go.mod` → `/home/abelbrown/src/clarion`). It provides the RSS/API source library and unified `Item` type. The fetch provider wraps Clarion with config: 10 concurrent fetchers, 30s timeout, 50 items per source.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `JINA_API_KEY` | (required) | Jina AI API key. Enables embeddings + reranking. |
| `JINA_EMBED_MODEL` | `jina-embeddings-v3` | Jina embedding model name. |
| `JINA_RERANK_MODEL` | `jina-reranker-v3` | Jina reranking model name. |
| `OBSERVER_TRACE` | (unset) | When set, enables trace-level events (msg received/handled). |

## Workflow Requirements

**Always use subagents.** For any non-trivial task, use the Task tool to spawn subagents. This preserves context and prevents the main conversation from hitting compaction limits. Use parallel subagents when tasks are independent.

**Ask before assuming.** Previous conversation context may be lost after compaction. If working on a significant task, confirm the current direction before proceeding with implementation.
