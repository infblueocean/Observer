# Brain Trust Synthesis: Observer Search UX

**Consulted:** GPT-5, Grok-4, Gemini-3
**Date:** 2026-01-30

---

## Unanimous Consensus (all 3 models agree)

### 1. "More Like This" — the killer feature you're not shipping

All three independently proposed the same thing: press a key on any item, use its *existing stored embedding* as the query vector, cosine-rank the corpus. **Zero API calls. Zero latency. 1-5ms.**

- GPT-5: `m` key, "killer feature in a news firehose TUI"
- Grok-4: `M` key, "embed selected -> new search"
- Gemini-3: `Tab` key, "Pivot Searching — the most powerful feature you're missing"

This is the single highest-ROI feature. We already have every embedding in SQLite. The wiring is ~50 lines of Go.

### 2. Stop auto-reranking on Ollama — make it opt-in

Universal agreement: the 32-second auto-rerank is a UX catastrophe. Fix:

- **Default (no Jina):** Show cosine-only results immediately. They're already good.
- **Opt-in rerank:** `r` or `D` = "Deep Rerank top 10" with live progress
- **Never surprise-block** the user for 30+ seconds
- GPT-5 additionally proposes: convert 30 sequential Ollama calls into **1 listwise prompt** (query + 30 candidates -> JSON scores). Could cut 32s to ~5s.

### 3. Progressive search pipeline with visual step tracking

Replace the single spinner with a **step timeline** showing real latencies from obs events:

```
/ climate risk    [fts 12ms] [embed 840ms] [cosine 3ms] [rerank 5/30...]
```

- Show cosine results **immediately** (don't wait for cross-encoder)
- Let user browse/scroll while reranking runs in background
- When rerank completes: items smoothly reorder (Jina) or toast "Press R to apply" (Ollama)
- GPT-5: mark items L/C/R (lexical/cosine/reranked) — dims until "promoted"
- Grok-4: score jump arrows `^0.12` during live reorder
- Gemini-3: color-code left border by score band (green >0.8, blue 0.5-0.8, grey <0.5)

### 4. Search history persistence with semantic cache

Converged schema (synthesized from all three):

```sql
CREATE TABLE search_history (
    id INTEGER PRIMARY KEY,
    query_text TEXT NOT NULL,
    query_norm TEXT NOT NULL,       -- lowercase/trimmed for exact match
    query_embedding BLOB,          -- 1024-dim, 4KB
    created_at DATETIME NOT NULL,
    backend TEXT NOT NULL,          -- 'jina'|'ollama'|'cosine'
    duration_ms INTEGER,
    is_pinned BOOLEAN DEFAULT 0,   -- "Saved Views"
    use_count INTEGER DEFAULT 1
);

CREATE TABLE search_results (
    search_id INTEGER NOT NULL,
    rank INTEGER NOT NULL,
    item_id TEXT NOT NULL,
    cosine_score REAL,
    rerank_score REAL,
    PRIMARY KEY(search_id, rank),
    FOREIGN KEY(search_id) REFERENCES search_history(id)
);
```

**Reuse logic:**
- On new query: embed it, cosine against last ~200 stored query embeddings (trivial)
- Similarity > 0.95: show cached results instantly as placeholder, refresh in background
- Similarity 0.80-0.95: show "Similar searches: [X] [Y]" suggestions
- `Ctrl-R` or `?`: browse search history (fuzzy-matched, sorted by recency/frequency)

### 5. Score transparency — "Why this item?"

All three want visible scores. Synthesized approach:

- Toggle score column with `x` — shows `0.82` next to each item in search mode
- Inspector modal with `?` or `e` on selected item:
  - Cosine score: 0.77
  - Rerank score: 0.84
  - Source, age, read status
  - Dedup info: "Near-duplicate of [other item] (sim=0.88)"

This is the "radical transparency" philosophy turned into a feature.

---

## Unique Ideas (one model only, worth highlighting)

### GPT-5: FTS5 for instant lexical results

Add SQLite FTS5 virtual table. On Enter, *immediately* show text-match results (10-50ms) while embedding API is in flight. User sees results literally instantly. When embeddings arrive, merge/switch to semantic view. This is the cheapest "instant feedback" win.

```sql
CREATE VIRTUAL TABLE items_fts USING fts5(title, summary, content='items', content_rowid='rowid');
```

### GPT-5: Query language with visible filter chips

```
/ climate risk source:arxiv after:7d unread
```

Parsed into chips displayed above results: `[source:arxiv] [after:7d] [unread]`. All filters visible, all removable. Fits the "no hidden algorithms" philosophy.

### GPT-5: Keep search pool hot in memory

Stop reloading 5000 items + embeddings from SQLite on every search (200-500ms). Keep an in-memory `SearchPool` updated incrementally by the coordinator. Search latency drops to 0ms for pool load.

### Gemini-3: "Pinned Searches" as persistent views

Pin a search -> it becomes a tab alongside "Just Now" / "Today" / "Yesterday". E.g., a "Physics" tab is just a persistent cosine search. Runs on every fetch cycle. Turns search into curation.

### Grok-4: Live ETA from observability data

Use the existing obs event stream to compute average search latencies. Show live ETA: "ETA 1.2s based on last 5 searches". Telemetry becomes a UI feature.

### Grok-4: Streaming Ollama leaderboard

For Ollama sequential reranking: emit a Bubble Tea message after *each* item is scored. User watches items shuffle up/down in real-time like a live leaderboard. Turns "waiting 32 seconds" into "watching a race."

---

## Jina API Ecosystem Expansion

Observer already uses Jina Embeddings v3 and Jina Reranker v3. The broader Jina stack shares the same API key and token-based pricing (~$0.045-0.050/M tokens). At ~4k headlines/day, adding endpoints costs pennies/month.

### Reader API (high priority)

`r.jina.ai/{url}` — takes any URL, returns clean LLM-friendly Markdown (via ReaderLM-v2). Handles JS-heavy sites, strips ads/trackers.

**Use cases:**
- Full-article fallback: when user clicks "Read more", pipe URL through Reader -> display clean Markdown inline
- Enrich clusters: for grouped stories, Reader the top headline's URL -> unified summary
- Search augmentation: `s.jina.ai/{query}` for top-5 web results blended with RSS feeds

### Segmenter API (free, nice-to-have)

`api.jina.ai/v1/segment` — intelligently chunks long text into coherent segments. No token charge.

**Use cases:**
- After Reader on a URL: segment -> embed chunks -> finer-grained "More Like This" at paragraph level
- Quote extraction: segment articles -> find high-relevance chunks for "key quotes" display
- Improve reranking: segment longer descriptions -> rerank on chunks -> pick best segments

### Lower priority endpoints

- **DeepSearch** (`deepsearch.jina.ai`): search-grounded reasoning for "explain this story" — overkill for raw ethos
- **Classifier** (`api.jina.ai/v1/classify`): few-shot tagging (e.g., "military", "geopolitics") — risks feeling too "algorithmic"

---

## Observability as UI Features

The existing structured JSONL event system and ring buffer can surface directly in the normal UI (not just debug overlay):

- **Step timeline in status bar** uses real KindQueryEmbed / KindCosineRerank / KindCrossEncoder durations
- **Live ETA** from rolling average of KindSearchComplete events
- **"Why this item?"** inspector pulls from event log (which stage produced the rank)
- **`obs search-stats`** CLI: avg/p95 search latencies, cache hit rate, backend distribution
- **Embedding coverage**: "847/900 items embedded" surfaced as a health indicator

Telemetry IS transparency. Every event already emitted can become a visible feature.

---

## Recommended Build Order

| Priority | Feature | Impact | Complexity |
|----------|---------|--------|------------|
| 1 | **"More Like This"** (`m` key) | Killer feature, 0 API cost | Low |
| 2 | **Ollama: opt-in rerank, not auto** | Fixes 32s UX disaster | Low |
| 3 | **Progressive search pipeline** (show cosine immediately) | Perceived instant search | Medium |
| 4 | **Step timeline in status bar** (obs telemetry -> UI) | Transparency + liveness | Low |
| 5 | **Search history persistence** (schema + `Ctrl-R`) | Search result reuse | Medium |
| 6 | **Score column toggle** (`x` key) | Transparency | Low |
| 7 | **FTS5 instant lexical results** | True instant feedback | Medium |
| 8 | **Filter chips / query language** | Power user delight | Medium |
| 9 | **Pinned searches as views** | Curation by consent | Medium |
| 10 | **Jina Reader for full articles** | Reading experience | Low |
