# Observer Roadmap

## v0.1 (Current) - Foundation
**Tagged and working.** Core news aggregation with AI analysis.

### What Works
- 94 RSS feeds, ~7k headlines
- Streaming AI analysis (Claude, OpenAI, Gemini, Grok with random selection)
- Top stories with zingers and persistence
- Correlation engine scaffolding (simhash, entity extraction)
- Beautiful TUI with adaptive streaming metrics

---

## v0.2 - Correlation Engine + Clustering

### Phase 1: Deduplication & Clustering
**Goal:** Group similar headlines into "stories" to reduce noise without hiding content.

**Architecture:**
```
Headlines arrive (per-source handlers)
    ↓
Embedding (local ONNX model or Ollama)
    ↓
Clustering (HDBSCAN or cosine threshold >0.8)
    ↓
Each cluster = one "story" with multiple sources
    ↓
Display: "Fed Rate Decision ×7 sources" instead of 7 separate items
```

**Tech Options:**
| Component | Option A (Pure Go) | Option B (Ollama) |
|-----------|-------------------|-------------------|
| Embeddings | `hugot` + all-MiniLM-L6-v2 ONNX | Ollama embedding endpoint |
| Clustering | In-memory HDBSCAN/threshold | Same |
| Speed | ~1-2s for 7k on GPU | ~5-10s |

**UI Changes:**
- Cluster indicator: `◐ 7` (7 sources covering same story)
- Expand cluster to see all headlines
- "×N sources" badge in stream view

### Phase 2: Entity Extraction (Enhanced)
**Goal:** Rich entity recognition beyond regex.

**Current (cheap.go):**
- Regex: $AAPL, country names, source attribution

**Enhanced:**
- Named Entity Recognition via local model
- People, organizations, locations, events
- Entity pages: "All news about Elon Musk"
- Entity timelines

### Phase 3: Disagreement Detection
**Goal:** Surface when sources conflict.

- Extract claims/numbers from headlines
- Detect contradictions within clusters
- `⚡` indicator for disagreements
- Side-by-side comparison view

---

## v0.3 - GPU-Powered Reranking

### Architecture
```
Per-Source Handlers (goroutines)
    ↓ (bounded channels, ~200 items each)
Fan-in to Ranking Queue
    ↓
Worker Pool (4-16 workers)
    ↓
Batch Reranker (jina-reranker-v3 on RTX 5070)
    ↓
Ranked headlines → Top Stories
```

### Reranker Options
| Model | Setup | 7k Headlines | VRAM |
|-------|-------|--------------|------|
| jina-reranker-v3 | ONNX + hugot | 1-4 seconds | ~4-6GB |
| Qwen3-Reranker-4B | Ollama | 2-6 seconds | ~3GB |

### Ranking Signals (Transparent, No Hidden Algo)
- Recency boost (last 1-24 hours)
- Source count in cluster (more = more important)
- Source diversity (avoid same outlet dominating)
- Optional: user-defined boosts per source

### CPU Fallback
- Same code path, just slower (20-70s for 7k)
- Automatic detection via ONNX runtime

---

## v0.4 - Article Enrichment

### Goal
Fetch full article content for richer analysis.

### When to Enrich
- On-demand (user requests deep analysis)
- Background for top stories only
- Never for all 7k (too slow, too much storage)

### Tech Stack (Pure Go)
```go
// Fetch and parse
resp, _ := http.Get(item.Link)
doc, _ := goquery.NewDocumentFromReader(resp.Body)

// Extract clean content (strips ads, nav, boilerplate)
article, _ := readability.FromDocument(doc, item.Link)
cleanText := article.TextContent
```

| Library | Purpose |
|---------|---------|
| `goquery` | jQuery-style HTML parsing |
| `go-readability` | Extract main article content |

### Storage
- Cache enriched content in SQLite
- TTL: 7 days
- Only for items user interacted with or top stories

---

## v0.5 - Semantic Filters

### Goal
Natural language filters that understand meaning, not just keywords.

### Examples
- "Hide opinion pieces" (detects op-eds even without keyword)
- "Show only breaking news" (urgency detection)
- "Filter clickbait" (trained classifier)
- "Boost local news about Seattle"

### Architecture
```
User creates filter: "Hide sensationalist headlines"
    ↓
Embed the filter description
    ↓
For each headline: cosine similarity with filter
    ↓
High similarity → apply filter action (hide/dim/tag)
```

### UI
- Filter Workshop: conversational filter creation
- Preview: "This filter would affect 23 items"
- Transparency: always show why something was filtered

---

## v0.6 - Story Radar & Catch Me Up

### Story Radar (Ambient Awareness)
- What's spiking right now (velocity tracking)
- Geographic distribution
- Entity cloud
- Press `Ctrl+R` to toggle radar view

### Catch Me Up
- On startup after >4 hours away
- "While you were gone: 3 major developments"
- Smart briefing based on:
  - New clusters that formed
  - Velocity changes
  - Your tracked entities

---

## Technical Decisions

### Why Pure Go (No Python Bridge)
- Single binary deployment
- No language boundary latency
- Native concurrency with goroutines
- ONNX runtime works great in Go via hugot

### Why Local-First AI
- Privacy: headlines never leave your machine
- Speed: no API latency for embeddings/clustering
- Cost: no per-token charges for bulk operations
- Control: you own your attention

### Why RTX 5070 Matters
- 12GB VRAM handles 7k headlines in one pass
- Embeddings + reranking in seconds
- Enables real-time clustering as headlines arrive
- CPU fallback always available

---

## Open Questions for Brain Trust

1. **Clustering granularity**: How similar should headlines be to cluster? 0.8 cosine threshold? HDBSCAN auto-detection?

2. **Reranking query**: What should the "query" be for the reranker?
   - "Most important global news today"
   - "Breaking developments that affect ordinary people"
   - User-customizable?

3. **Enrichment triggers**: When should we fetch full articles?
   - Only on explicit user action?
   - Auto for top 10 stories?
   - Based on headline "interestingness" score?

4. **Semantic filter UX**: How to make filter creation intuitive?
   - Natural language only?
   - Hybrid with examples ("like this headline, unlike that one")?
   - Import/export filter packs?

5. **Velocity detection**: What timeframe for "spiking"?
   - Items per hour in cluster?
   - Rate of new sources joining cluster?
   - Sudden appearance in multiple categories?

6. **Source credibility**: Should we have source weights?
   - Wire services > blogs?
   - User-adjustable?
   - Or pure chronological (no editorial judgment)?

---

## Implementation Order

```
v0.2: Clustering (biggest UX win - reduce noise)
  ↓
v0.3: GPU Reranking (make top stories actually top)
  ↓
v0.4: Article Enrichment (deeper analysis)
  ↓
v0.5: Semantic Filters (user control)
  ↓
v0.6: Radar + Catch Me Up (polish)
```

Each version is independently useful. Ship incrementally.
