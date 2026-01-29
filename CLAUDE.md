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

# Test
go test ./...

# Test with race detector
go test -race ./...

# Run single test
go test -run TestName ./path/to/package

# Run
./observer
```

## Architecture

### Package Structure

```
cmd/observer/       Main entry point — wires dependencies, starts TUI
cmd/backfill/       Standalone CLI to backfill Jina embeddings in the database
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

The coordinator detects `BatchEmbedder` at runtime and uses batch calls when available. The UI detects batch reranking via the `BatchRerank` callback and shows a spinner instead of per-entry progress.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `JINA_API_KEY` | (none) | Jina AI API key. Enables Jina embeddings + reranking. |
| `JINA_EMBED_MODEL` | `jina-embeddings-v3` | Jina embedding model name. |
| `JINA_RERANK_MODEL` | `jina-reranker-v3` | Jina reranking model name. |

### Backfill Tool

When switching from Ollama to Jina embeddings, existing embeddings are incompatible. Run the backfill tool to re-embed:

```bash
source ~/src/claude/keys.sh  # or export JINA_API_KEY=...
go run ./cmd/backfill
```

The tool is idempotent — it only processes items with NULL embeddings. First run prompts to clear old embeddings; subsequent runs resume from where they left off.

## Workflow Requirements

**Always use subagents.** For any non-trivial task, use the Task tool to spawn subagents. This preserves context and prevents the main conversation from hitting compaction limits. Use parallel subagents when tasks are independent.

**Ask before assuming.** Previous conversation context may be lost after compaction. If working on a significant task, confirm the current direction before proceeding with implementation.
