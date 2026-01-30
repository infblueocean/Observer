# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

**Observer is starting fresh.** The v0.4 codebase has been archived to `archive/v0.4/`. New development should not reference or build upon the archived code without explicit instruction.

## What is Observer?

Observer is an ambient news aggregation TUI (Terminal User Interface) built with Go. The goal is to let users "watch the world go by" - aggregating content from many sources with radical transparency and user control over curation.

### Core Philosophy

- **You Own Your Attention** - No algorithm stands between you and information
- **Curation By Consent** - Every filter is visible and adjustable
- **AI as Tool, Never Master** - AI assists when asked, never decides secretly

## Build Commands

```bash
# Build
go build -o observer ./cmd/observer
go build -o obs ./cmd/obs

# Test
go test ./...

# Test with race detector
go test -race ./...

# Run single test
go test -run TestName ./path/to/package

# Run TUI
./observer

# Debug & maintenance CLI
./obs                           # help
./obs stats                     # pipeline counts
./obs stats --db                # + DB health
./obs search "query"            # two-stage search debug (requires JINA_API_KEY)
./obs search --cosine-only "q"  # cosine stage only
./obs rerank                    # reranker validation (Ollama)
./obs backfill                  # batch embed missing items (requires JINA_API_KEY)
./obs backfill --dry-run        # check counts without embedding
./obs events --tail 20          # last 20 events
./obs events -f --level warn    # follow warns+errors
```

## Architecture

### Package Structure

```
cmd/observer/       Main entry point — wires dependencies, starts TUI
cmd/obs/            Unified debug & maintenance CLI (backfill, stats, search, rerank, events)
internal/coord/     Coordinator — background fetch + embedding pipeline
internal/embed/     Embedder interface + Jina API and Ollama implementations
internal/fetch/     RSS/source fetching
internal/filter/    Item filtering, dedup, and reranking by embedding similarity
internal/rerank/    Reranker interface + Jina API and Ollama implementations
internal/store/     SQLite store (items, embeddings, read state)
internal/ui/        Bubble Tea TUI (App model, messages, styles, stream rendering)
```

### Embedding & Reranking Pipeline

Observer supports two backends for AI features, selected by the `JINA_API_KEY` environment variable:

**Jina API (preferred):** Set `JINA_API_KEY` to enable. Uses `jina-embeddings-v3` for embeddings and `jina-reranker-v3` for reranking. Batch APIs for efficiency. Rate-limited to ~80 RPM with retry on 429/5xx.

**Ollama (fallback):** When no Jina key is set, uses local Ollama with `mxbai-embed-large` for embeddings and cross-encoder prompting for reranking. Sequential, one item at a time.

Key interfaces:
- `embed.Embedder` — `Available()`, `Embed(ctx, text)`
- `embed.BatchEmbedder` — extends Embedder with `EmbedBatch(ctx, texts)`
- `rerank.Reranker` — `Available()`, `Rerank(ctx, query, docs)`

The coordinator detects `BatchEmbedder` at runtime and uses batch calls when available.

### UI Status Bar Pattern (`statusText`)

The UI uses a single `statusText string` field to decouple View() from pipeline internals. When non-empty, the status bar renders `spinner + statusText`; when empty, it renders the normal position/key hints.

**Setting statusText:** Handlers set it when starting async work:
- `submitSearch()` sets `Searching for "X"...`
- `startReranking()` sets `Reranking "X"...`

**Clearing statusText:** Every completion, error, and cancellation path clears it:
- `RerankComplete` / `handleEntryReranked` (all done) / `QueryEmbedded` error / `SearchPoolLoaded` error / `ItemsLoaded` (cancel rerank) / `clearSearch()` (Esc)

Handlers set `statusText` directly and include `spinner.Tick` in their command batch when starting async work. Clears are direct `a.statusText = ""` assignments.

The `handleKeyMsg` cascade (`searchActive` -> `embeddingPending` -> `rerankPending` -> normal) remains unchanged and uses the pipeline booleans directly for input routing. Only View() uses `statusText`.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `JINA_API_KEY` | (none) | Jina AI API key. Enables Jina embeddings + reranking. |
| `JINA_EMBED_MODEL` | `jina-embeddings-v3` | Jina embedding model name. |
| `JINA_RERANK_MODEL` | `jina-reranker-v3` | Jina reranking model name. |

### `obs` CLI Tool

The `obs` binary consolidates all debug and maintenance utilities:

| Subcommand | Description |
|------------|-------------|
| `obs stats` | Pipeline stage counts, source distribution |
| `obs stats --db` | + DB health: embedding coverage, timestamps |
| `obs search <query>` | Two-stage search debug (cosine + cross-encoder) |
| `obs rerank` | Ollama reranker validation with test headlines |
| `obs backfill` | Batch embed items missing embeddings |
| `obs events` | JSONL event log viewer with filtering and follow mode |

Backfill is idempotent — it only processes items with NULL embeddings. Use `--clear` to wipe and re-embed, `--dry-run` to check counts.

## Workflow Requirements

**Always use subagents.** For any non-trivial task, use the Task tool to spawn subagents. This preserves context and prevents the main conversation from hitting compaction limits. Use parallel subagents when tasks are independent.

**Ask before assuming.** Previous conversation context may be lost after compaction. If working on a significant task, confirm the current direction before proceeding with implementation.
