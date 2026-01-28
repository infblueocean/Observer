# v0.3 Ranking - Clean Slate Implementation Plan

## Philosophy

**v0.2 was about correlation** (clustering, entities, velocity) - complex pipeline, limited UI payoff.

**v0.3 is about ranking** - make "top stories" actually top, using ML reranking on GPU.

**Clean slate principle:** Keep only what directly serves ranking. Remove complexity that doesn't pay rent.

**Key insight:** The correlation engine's async infrastructure (bus, workers, events) was the right idea but over-specialized. The **work system** is the generalized version - all async ops flow through one observable pool.

---

## What We Keep

### 1. SimHash Dedup (from `correlation/dedup.go`, `correlation/cheap.go`)
- Fast O(1) duplicate detection via LSH buckets
- `SimHash()`, `HammingDistance()`, `AreDuplicates()`
- **Why keep:** Prevents showing same headline twice - essential

### 2. Cheap Extractors (from `correlation/cheap.go`)
- `ExtractTickers()` - $AAPL, $TSLA
- `ExtractCountries()` - geopolitical tagging
- **Why keep:** Zero-cost metadata enrichment, useful for future filtering

### 3. Basic Ranker Interface (from `ranking/ranker.go`)
- `Ranker` interface with `Score(item, ctx) float64`
- `Context` struct for ranking context
- **Why keep:** Clean composable design

### 4. FreshnessRanker, DiversityRanker (from `ranking/rankers.go`)
- Exponential decay for recency
- Diversity penalty for over-represented sources
- **Why keep:** Combine with ML scores for final ranking

### 5. Samplers (from `sampling/round_robin.go`)
- 6 sampler strategies already implemented
- **Why keep:** Source balancing orthogonal to ranking

---

## What We Remove

### 1. Full Correlation Engine Pipeline
**Files:** `correlation/engine.go`, `correlation/bus.go`, `correlation/worker.go`, `correlation/velocity.go`, `correlation/clusters.go`, `correlation/entities.go`, `correlation/events.go`

**Why remove:**
- Complex async pipeline with bus, workers, channels
- ClusterEngine, VelocityTracker, EntityWorker - unused in UI
- Over-engineered for features not yet visible to user

**Replaced by:** The **work system** provides the same capabilities (worker pool, event subscription, async coordination) but generalized for ALL async work, not just correlation.

### 2. ClusterRanker, EntityRanker
**From:** `ranking/rankers.go`

**Why remove:**
- Depend on correlation engine we're removing
- Will be replaced by embedding-based clustering

### 3. LLM-based Top Stories Classification
**From:** `brain/trust.go` - `AnalyzeTopStories()`, `TopStoriesCache`, `GetBreathingTopStories()`, zingers, etc.

**Why remove:**
- Asks LLM to pick top stories from headlines (slow ~60s, inconsistent)
- Small models (1-2B) can't follow structured output format reliably
- Complex parsing with multiple fallbacks still fails often
- Cache/hit-count/zinger tracking adds significant complexity
- "Breathing" logic (merge cached + current) is a workaround for unreliable LLM output

**Replaced by:** ML reranker scores ALL headlines against "top stories" rubric
- Rerankers are specifically trained for relevance scoring
- Returns float scores, not structured text to parse
- Deterministic, fast (2-6s), works with small models
- Top stories = highest scoring headlines (simple!)

---

## New v0.3 Architecture

```
                         ┌─────────────────────┐
                         │   WORK POOL         │
                         │   (central hub)     │
                         │   /w to observe     │
                         └──────────┬──────────┘
                                    │
        ┌───────────────┬───────────┼───────────┬───────────────┐
        ↓               ↓           ↓           ↓               ↓
    [fetch]         [dedup]     [rerank]    [embed]       [analyze]
   Per-Source       SimHash     Qwen3/jina  Optional      AI analysis
   Handlers         Batches     on GPU      embeddings    on demand
        │               │           │           │               │
        └───────────────┴───────────┴───────────┴───────────────┘
                                    ↓
                         Combined Ranking Score
                                    ↓
                         Top Stories Display
```

### New Packages

```
internal/
├── work/                   # NEW: Unified async work system
│   ├── types.go            # Item, Type, Status, Event
│   ├── pool.go             # Worker pool, queue, subscribers
│   └── ring.go             # History buffer for completed work
│
├── rerank/                 # NEW: ML Reranking
│   ├── reranker.go         # Interface + factory
│   ├── ollama.go           # Qwen3-Reranker-4B via Ollama
│   ├── onnx.go             # jina-reranker-v3 via ONNX (optional)
│   └── rubric.go           # Query rubrics (front page, breaking, custom)
│
├── dedup/                  # MOVED from correlation/
│   ├── simhash.go          # SimHash + LSH dedup (from cheap.go)
│   └── index.go            # DedupIndex (from dedup.go)
│
├── embed/                  # NEW: Embedding generation (optional for v0.3)
│   ├── embedder.go         # Interface + factory
│   └── ollama.go           # Ollama embedding endpoint
│
├── ranking/                # UPDATED
│   ├── ranker.go           # Keep: interface, Context
│   ├── freshness.go        # Keep: FreshnessRanker
│   ├── diversity.go        # Keep: DiversityRanker
│   ├── rerank.go           # NEW: RerankScoreRanker (wraps reranker)
│   └── composite.go        # Keep: CompositeRanker
│
├── ui/workview/            # NEW: Work queue visualization
│   └── model.go            # The /w view
```

### Remove These Files
```
internal/correlation/
├── engine.go       # DELETE
├── bus.go          # DELETE
├── worker.go       # DELETE
├── velocity.go     # DELETE
├── clusters.go     # DELETE
├── entities.go     # DELETE
├── events.go       # DELETE
└── types.go        # DELETE (or trim to just Entity/Claim types)
```

---

## Implementation Details

### 1. Embedding Generator (`internal/embed/`)

```go
// Interface
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimension() int
}

// Factory - tries GPU first, falls back to CPU
func NewEmbedder(cfg *config.Config) (Embedder, error)
```

**Options:**
| Model | Method | Speed (7k headlines) | Quality |
|-------|--------|---------------------|---------|
| all-MiniLM-L6-v2 | ONNX (hugot) | ~2s GPU, ~20s CPU | Good |
| nomic-embed-text | Ollama | ~5s GPU | Better |
| bge-large-en | ONNX | ~4s GPU | Best |

**Recommendation:** Start with `all-MiniLM-L6-v2` via ONNX - fast, good enough, pure Go.

### 2. Reranker (`internal/rerank/`)

```go
// Interface
type Reranker interface {
    Rerank(ctx context.Context, query string, docs []string) ([]float64, error)
}

// Factory
func NewReranker(cfg *config.Config) (Reranker, error)
```

**Options:**
| Model | Method | 7k Headlines | VRAM |
|-------|--------|--------------|------|
| jina-reranker-v3 | ONNX (hugot) | 1-4s | ~4-6GB |
| Qwen3-Reranker-4B | Ollama | 2-6s (batched) | ~3GB |

**Recommendation:** Start with Ollama + Qwen3 for easiest setup, add ONNX later.

### 3. Rubric Queries (`internal/rerank/rubric.go`)

Not just "most important" - encode actual criteria:

```go
var Rubrics = map[string]string{
    "frontpage": `Rank by: real-world impact, broad relevance, novelty, credible sourcing.
                  Prefer: clear facts over speculation, multi-source confirmation.
                  Avoid: duplicates, clickbait, promotional content.`,

    "breaking":  `Rank by: immediacy (last 2 hours), potential impact, source reliability.
                  Prefer: wire services, developing situations, factual claims.`,

    "tech":      `Rank by: technical significance, industry impact, innovation.
                  Focus: AI/ML, security, infrastructure, developer tools.`,

    "topstories": `Most important breaking news and major world developments.
                   Significant events with broad impact. Major policy changes.
                   Breaking situations. High-impact technology or science news.`,
}
```

### 4. Top Stories via Reranking (Replaces LLM Classification)

**Current approach (v0.2):** Ask LLM "which headlines are top stories?"
- Slow (~60s), inconsistent output format, parsing gymnastics, empty results with small models

**New approach (v0.3):** Rerank all headlines against "top stories" rubric, take top N
- Fast (2-6s), deterministic, no parsing, works with any model

```go
// Top stories is just a reranker query
func (m *Model) GetTopStories(count int) []feeds.Item {
    items := m.store.GetRecentItems(6 * time.Hour)

    // Score all headlines against "top stories" rubric
    scores, _ := m.reranker.Rerank(ctx, Rubrics["topstories"], itemTitles(items))

    // Sort by score, return top N
    ranked := sortByScores(items, scores)
    return ranked[:min(count, len(ranked))]
}
```

**Benefits:**
- Every headline gets scored (not just LLM's arbitrary picks)
- Scores are comparable ("this story scored 0.92 vs 0.87")
- Can blend with freshness/diversity for final ranking
- No LLM output parsing - just float scores
- Works reliably with small local models (rerankers are trained for this task)
```

### 4. Combined Ranking (`internal/ranking/`)

```go
// RerankScoreRanker wraps the ML reranker
type RerankScoreRanker struct {
    scores map[string]float64  // itemID -> rerank score
}

func (r *RerankScoreRanker) Score(item *feeds.Item, ctx *Context) float64 {
    if score, ok := r.scores[item.ID]; ok {
        return score
    }
    return 0.5  // neutral if not yet scored
}

// Default v0.3 composite ranker
func DefaultRanker() Ranker {
    return NewComposite("v03").
        Add(NewRerankScoreRanker(), 5.0).  // ML rerank dominates
        Add(NewFreshnessRanker(), 2.0).    // Recency tiebreaker
        Add(NewDiversityRanker(), 1.0)     // Source variety
}
```

### 5. Dedup Integration

Keep SimHash for fast first-pass dedup:

```go
// In feed processing pipeline
func (a *Aggregator) processItem(item *feeds.Item) {
    // Fast dedup check (< 1ms)
    isDupe, primaryID := a.dedup.Check(item)
    if isDupe {
        // Link to primary, don't add to main list
        return
    }
    // Continue processing...
}
```

---

## Data Flow

```
1. Source due for refresh
           ↓
2. Aggregator submits [fetch] work item ──────────────┐
           ↓                                          │
3. Worker fetches source, returns items               │
           ↓                                          │
4. Dedup submits [dedup] work item ───────────────────┤
           ↓                                          │
5. Worker runs SimHash, filters duplicates            │
           ↓                                          │  WORK POOL
6. New items stored, trigger rerank                   │  (visible in /w)
           ↓                                          │
7. Ranking engine submits [rerank] work item ─────────┤
           ↓                                          │
8. Worker runs ML reranker with progress callback     │
           ↓                                          │
9. Scores applied to items                            │
           ↓                                         ─┘
10. Combine: rerank × freshness × diversity
           ↓
11. Display sorted by combined score
```

**Key insight:** Steps 2, 4, 7 are all work items. User can press `/w` at any time and see exactly what's happening.

### Batching Strategy

```go
// Rerank submits work to the pool with progress tracking
func (m *Model) triggerRerank() {
    headlines := m.store.GetRecentHeadlines(24 * time.Hour)

    m.workPool.SubmitWithProgress(&work.Item{
        Type:        work.TypeRerank,
        Description: fmt.Sprintf("Reranking %d headlines", len(headlines)),
    }, func(progress func(float64, string)) (string, error) {
        // Build document list
        docs := make([]string, len(headlines))
        for i, h := range headlines {
            docs[i] = h.Title + " " + h.Summary
        }

        // Rerank with progress callback
        scores, err := m.reranker.RerankWithProgress(
            m.currentRubric,
            docs,
            progress,  // Reports "1,234 of 7,234"
        )
        if err != nil {
            return "", err
        }

        // Apply scores
        m.rankingEngine.ApplyScores(headlines, scores)

        return fmt.Sprintf("ranked %d", len(headlines)), nil
    })
}

// Pool notifies completion via event channel
func (m *Model) handleWorkEvent(event work.Event) {
    if event.Item.Type == work.TypeRerank && event.Change == "completed" {
        // Refresh display with new rankings
        m.refreshTopStories()
    }
}
```

---

## Config Changes

```json
{
  "ranking": {
    "enabled": true,
    "reranker": "qwen3",
    "rubric": "frontpage",
    "refresh_interval_sec": 30,
    "use_gpu": true
  },
  "embedding": {
    "enabled": false,
    "model": "all-MiniLM-L6-v2",
    "use_for_clustering": false
  }
}
```

---

## Migration Steps

### Phase 1: Work System Foundation
1. Create `internal/work/` with types, pool, ring buffer
2. Create `internal/ui/workview/` for `/w` visualization
3. Wire work pool into `app.go` - single global pool
4. Add `/w` command to toggle work view
5. **Test:** Submit dummy work, see it in the view

### Phase 2: Migrate Existing Async Work
1. Feed fetching → submits work items
2. Move SimHash to `internal/dedup/`, dedup batches → work items
3. AI analysis → submits work items
4. Remove correlation engine initialization from `app.New()`
5. **Test:** All existing async now visible in `/w`

### Phase 3: Reranker Implementation
1. Create `internal/rerank/` with interface
2. Implement Ollama-based Qwen3 reranker
3. Reranking submits work item with progress callback
4. Add rubric query system
5. **Test:** See "Reranking 7,234 headlines ████░░ 45%" in work view

### Phase 4: Ranking Integration
1. Create `RerankScoreRanker` that uses reranker results
2. Update `DefaultRanker()` to combine rerank + freshness + diversity
3. Replace LLM top stories with rerank-based top stories
4. Add rubric switcher (1-5 keys or `/rubric` command)
5. **Test:** Top stories are actually the highest-scored headlines

### Phase 5: Cleanup
1. Delete old correlation engine files (engine, bus, worker, velocity, clusters)
2. Remove ClusterRanker, EntityRanker from ranking/
3. Remove LLM top stories code from brain/trust.go (keep basic analysis)
4. Update CLAUDE.md

### Phase 6: Optional Enhancements
1. Add ONNX jina-reranker-v3 for faster/better ranking
2. Add embedding generation (for future clustering)
3. Add custom user rubrics
4. Persist work history to SQLite

---

## Success Metrics

| Metric | v0.2 (LLM classification) | v0.3 (ML reranking) |
|--------|---------------------------|---------------------|
| Time to rank 7k | ~60s (LLM call) | 2-6s (GPU) |
| Consistency | Low (LLM output varies) | High (deterministic scores) |
| Coverage | 3-6 stories (LLM picks) | All headlines scored |
| Small model support | Fails (can't follow format) | Works (rerankers trained for this) |
| Parsing complexity | 3 fallback parsers, still fails | None (float scores) |
| Transparency | "Why did LLM pick this?" | "Score: 0.92 (frontpage rubric)" |
| Empty results | Common with small LLMs | Never (always get scores) |

---

## Questions to Resolve

1. **Embeddings:** Defer to v0.4?
   - Reranking alone should be enough for v0.3
   - Embeddings enable clustering, but that's a separate feature
   - **Recommendation:** Skip for v0.3, add in v0.4 if needed

2. **Rubric UX:** How does user switch rubrics?
   - Option A: Number keys (1=front page, 2=breaking, 3=tech)
   - Option B: Command `/rubric tech`
   - Option C: Both
   - **Recommendation:** Start with `/rubric`, add keys later

3. **Rerank trigger:** When to re-rerank?
   - On new headlines batch (natural trigger)
   - Every 30 seconds if idle (background refresh)
   - Manual `/rerank` command
   - **Recommendation:** All three - batch triggers it, timer refreshes, manual available

4. **Worker count:** How many workers in the pool?
   - Fixed (e.g., 8)?
   - Dynamic based on CPU cores?
   - **Recommendation:** Start with `runtime.NumCPU()`, tune later

5. **Keep zingers?** The one-liner summaries were nice.
   - Could generate for top 5 after reranking (as a work item!)
   - Or remove entirely (simplicity)
   - **Decision:** Remove for v0.3. Can add back later if needed - would be a quick work item after top stories are identified by reranker

---

## Summary

**v0.3 = Work system + ML reranking**

**Foundation:** Work system - unified async processing with `/w` observability

**Remove:**
- Correlation engine pipeline (bus, workers, velocity, clusters)
- LLM top stories classification

**Keep:**
- SimHash dedup (moved to `internal/dedup/`)
- Basic rankers (freshness, diversity)
- Samplers

**Add:**
- Work system (`internal/work/`) - all async flows through here
- Work view (`/w`) - watch the machine work
- Reranker (`internal/rerank/`) - Qwen3/jina on GPU
- Rubric queries - encoded ranking criteria

**Result:**
- Every async operation is observable
- Every headline gets scored
- Top stories are actually top
- Runs in seconds on GPU
- System is transparent ("why am I seeing this?" → "score: 0.87")
