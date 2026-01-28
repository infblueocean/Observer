# Observer Roadmap

## Design Philosophy

> **Primary failure mode we're designing against:** Noise/duplicates overwhelming the feed, while never missing important stories.

**Core Principle:** Don't curate. Illuminate. Show connections, don't hide content.

---

## v0.1 (Current) - Foundation ✓
**Tagged and working.** Core news aggregation with AI analysis.

### What Works
- 94 RSS feeds, ~7k headlines
- Streaming AI analysis (Claude, OpenAI, Gemini, Grok with random selection)
- Top stories with zingers and persistence
- Correlation engine scaffolding (simhash, entity extraction)
- Beautiful TUI with adaptive streaming metrics (TTFT, tokens, chunks)
- Skeptical analysis lens (I.F. Stone inspired)

---

## v0.2 - Correlation Engine + Clustering

### Critical Insight from Brain Trust
> **"HDBSCAN is a batch algorithm; news is a stream."** — Gemini
> **"Dedup ≠ Clustering. Need both."** — Claude

### Architecture: Two-Stage Pipeline

```
Headlines arrive (per-source handlers)
    ↓
Stage 1: DEDUPLICATION (high precision, real-time)
    - Simhash for exact/near duplicates
    - Cosine threshold 0.90 for syndicated rewrites
    - Instant, per-item as they arrive
    ↓
Stage 2a: MICRO-CLUSTERING (incremental, streaming)
    - Single-pass incremental clustering or BIRCH
    - Bucket into micro-clusters instantly
    - Cosine threshold 0.82 for "same story"
    ↓
Stage 2b: STORY CLUSTERING (batch, periodic)
    - HDBSCAN on micro-cluster centroids every 5-15 min
    - Merge micro-clusters into coherent stories
    - Parameters: min_cluster_size=3-4, min_samples=1-2
    ↓
Display: "Fed Rate Decision ×7 sources"
```

### Brain Trust Recommendations

| Question | Consensus | Implementation |
|----------|-----------|----------------|
| **Clustering threshold** | Two-tier: 0.90 (dedup) + 0.82 (story) | Graph threshold simpler than pure HDBSCAN |
| **Embedding model** | Model choice > threshold tuning | Start: all-MiniLM-L6-v2; Upgrade: BGE-large for news |
| **Time handling** | Critical: temporal decay | Articles >24h drift "farther" in vector space |

### Gotchas to Handle

1. **Zombie News Prevention**
   - Apply time penalty to vector distances
   - Stories >48h cannot merge with today's news
   - Decay function: `distance *= (1 + age_hours/24)`

2. **Threshold Trap**
   - 0.80 cosine means different things per embedding model
   - **Solution:** Calibrate on 200 labeled pairs from your actual feeds
   - Expected distribution: dupes >0.90, same story 0.80-0.88, different <0.78

3. **Incremental Clustering**
   - HDBSCAN doesn't support adding points natively
   - **Solution:** Micro-clusters (Stage 2a) handle streaming; HDBSCAN (Stage 2b) runs periodically

### Entity Extraction (Enhanced)

**Current (cheap.go):** Regex for $AAPL, countries, source attribution

**Enhanced:**
- Named Entity Recognition via local model
- People, organizations, locations, events
- Entity pages: "All news about Elon Musk"
- Entity timelines

### Disagreement Detection

**Goal:** Surface when sources conflict (the real value-add).

- Extract claims/numbers from headlines
- Detect contradictions within clusters
- `⚡` indicator for disagreements
- Side-by-side comparison view

### Update Detection (New from Brain Trust)

> **"News evolves: 'Explosion heard' → 'Gas leak confirmed' → '3 Injured'"** — Gemini

- If new article enters cluster with high similarity BUT contains new Named Entities or state-change keywords ("death toll", "verdict", "confirmed")
- Trigger re-notification as "Update" not duplicate
- Track story state: `developing → confirmed → resolved`

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

### Brain Trust Recommendations

| Question | Consensus | Implementation |
|----------|-----------|----------------|
| **Reranking query** | Per-view rubrics + user override | Not just "important news" - encode *criteria* |
| **Source weights** | Light tiebreaker, not censor | Tiers: wire 1.15, major 1.05, niche 1.0, unknown 0.9 |

### Rubric Queries (Not Slogans)

**Default (Front Page):**
> "Rank most consequential new developments from last 24 hours. Prefer high real-world impact, broad relevance, novelty, credible sourcing. Avoid duplicates; prefer one representative per story."

**Breaking (Alerts):**
> "Rank fastest-moving developments in last 2 hours with potential immediate impact. Prefer multi-source confirmation and clear factual claims."

**Personal (User-Custom):**
- Interests: {tech, markets, science}
- De-emphasize: {sports, celebrity}
- Region: {US, EU}
- Rendered to rubric string for reproducibility

### Ranking Score Formula

```go
story.Score = recency * sourceWeight * clusterVelocity * diversityBonus

// Where:
// - recency: exponential decay from publish time
// - sourceWeight: tier-based (1.15 wire → 0.9 unknown)
// - clusterVelocity: normalized items/hour
// - diversityBonus: penalize single-source dominance
```

### The "Scoop" Problem (from Gemini)

> **Risk:** Small blog breaks story at 10:00 AM. NYT covers at 10:45 AM. If you favor NYT, you show 45 min late and bury the original.

**Solution:** `(SourceWeight * 0.7) + (FreshnessScore * 0.3)` for canonical leader. Always link "Earliest Source" if different from representative.

### Reranker Options

| Model | Setup | 7k Headlines | VRAM |
|-------|-------|--------------|------|
| jina-reranker-v3 | ONNX + hugot | 1-4 seconds | ~4-6GB |
| Qwen3-Reranker-4B | Ollama | 2-6 seconds | ~3GB |

### CPU Fallback
- Same code path, just slower (20-70s for 7k)
- Automatic detection via ONNX runtime

---

## v0.4 - Article Enrichment

### Brain Trust Recommendations

| Question | Consensus | Implementation |
|----------|-----------|----------------|
| **When to fetch** | 3-tier budgeted approach | Never unbounded; cap at 20/hour |
| **Breaking news** | Fast-path critical | Display raw → enrich async → re-rank on completion |

### 3-Tier Enrichment Pipeline

```
Tier 1: GATEKEEPER (cheap, every article)
    - "News Value" classifier (filter spam/SEO)
    - If fails → stop here, don't waste Tier 2/3
    ↓
Tier 2: ON-DEMAND (user action)
    - User opens/selects story → fetch immediately
    - Cache for 1 hour
    ↓
Tier 3: AUTO-PREFETCH (background, budgeted)
    - Top 3-10 clusters by velocity/score
    - Max 20 fetches/hour hard cap
    - Circuit breaker per domain (rate limits, paywalls)
```

### Breaking News Fast-Path (from Claude)

> **Problem:** 3-tier enrichment may be too slow for breaking news.

**Solution:**
1. Display raw headline immediately
2. Enrich asynchronously in background
3. Re-rank and update display on completion
4. Never block the UI for enrichment

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
- Track: `enrichment_status` (success, paywalled, blocked, extracted_length)
- Graceful fallback to headline+snippet

### Multi-Document Summarization (from Gemini)

> **"Users don't want to read 10 headlines. They want 1 paragraph that synthesizes the 10 articles."**

**Strategy:**
- Take top 3 distinct articles in cluster (Left, Right, Center if detectable)
- Feed to LLM for "Consensus Summary"
- Display as cluster summary, expandable to individual sources

---

## v0.5 - Semantic Filters

### Brain Trust Recommendations

| Question | Consensus | Implementation |
|----------|-----------|----------------|
| **Filter UX** | Hybrid: NL + examples + packs | Pure NL too vague |
| **Cost control** | Vector pre-filter before NL classification | Don't run expensive classifier on everything |

### Architecture

```
User creates filter: "Show AI ethics news, not product launches"
    ↓
Parse to structured query (topics, entities, sources, time)
    ↓
Stage 1: Vector pre-filter (cheap)
    - Broad category match (e.g., "Finance", "Tech")
    - Reduces candidate set 90%
    ↓
Stage 2: NL classifier (expensive, only on candidates)
    - Few-shot classification on filtered set
    - Apply action: hide/dim/tag/boost
```

### Filter Creation UX

1. **Quick NL prompt:**
   ```
   /filter "Show me breaking AI policy news, not product launches"
   ```

2. **Refine with examples:**
   ```
   /filter like "EU passes AI Act" unlike "ChatGPT gets new feature"
   ```

3. **Filter packs (import/export):**
   ```yaml
   tech-essentials:
     like: ["machine learning", "hardware", "security"]
     unlike: ["NFT", "crypto pump", "sponsored"]
   ```

### Debuggability (Critical)

Always show "Explain" view:
- Included entities/topics
- Excluded entities/topics
- Source constraints
- Recency window
- Live "test this filter" preview with affected count

### Cold Start Handling (from Claude)

- **New user:** Offer starter packs, learn from first 10 interactions
- **No examples:** Fall back to pure NL with explicit uncertainty
- **Import from community:** Curated filter packs as distribution channel

---

## v0.6 - Story Radar & Catch Me Up

### Brain Trust Recommendations

| Question | Consensus | Implementation |
|----------|-----------|----------------|
| **Velocity windows** | Multi-window: 15m/1h/6h | Show as separate badges |
| **Normalization** | Per-source baseline critical | 100 hits massive for blog, noise for CNN |

### Velocity Detection

```go
// Per-cluster velocity score
cluster.Velocity = (
    rate_z * 0.6 +      // items/hour vs 24h EWMA
    sources_z * 0.4 +   // unique sources vs baseline
    highCredBonus       // new wire service joined
)

// Normalize per source
source.NormalizedRate = source.Rate / source.HistoricalAverage

// Multi-window signals
if velocity_15m > threshold: badge = "🔴 Just Now"
if velocity_1h > threshold:  badge = "🟠 Breaking"
if velocity_6h > threshold:  badge = "🟡 Today's Big Story"
```

### Spike Detection Rules (from GPT5)

- **15 minutes:** "Something just happened" — but noisy (single viral tweet)
- **1 hour:** "Breaking / developing" — require 2-of-3 windows elevated
- **6 hours:** "Today's big story" — sustained coverage

**Anti-noise:** Require ≥2 windows elevated before "spiking" label.

### Story Radar (Ambient Awareness)

- What's spiking right now (velocity heatmap)
- Geographic distribution (map view)
- Entity cloud (who's being talked about)
- Press `Ctrl+R` to toggle radar view
- Click to filter/jump

### Catch Me Up

- On startup after >4 hours away
- "While you were gone: 3 major developments"
- Smart briefing based on:
  - New clusters that formed
  - Velocity changes (what spiked)
  - Your tracked entities
  - Stories that evolved (update detection)

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

## Brain Trust Feedback Summary

### Consulted Models
- **Grok 4.1** (reasoning + non-reasoning)
- **GPT-5.2**
- **Gemini 3 Pro**
- **Claude Sonnet 4.5**

### Key Gotchas Identified

| Issue | Source | Solution |
|-------|--------|----------|
| HDBSCAN is batch, news is stream | Gemini | Two-stage: incremental micro-clusters → periodic HDBSCAN |
| Hard-coded threshold trap | Gemini, Claude | Calibrate on labeled pairs; model choice > threshold |
| Dedup ≠ Clustering | Claude | Need both: near-duplicate collapse + topical clustering |
| Zombie news / temporal decay | Gemini | Time penalty in vector space |
| Source weights topic-dependent | Claude | Define precisely; quality varies by topic |
| Breaking news latency | Claude | Fast-path: display raw → enrich async → re-rank |
| The "Scoop" problem | Gemini | Weight freshness, link earliest source |
| Velocity noise | GPT5, Claude | Require 2-of-3 windows; normalize per source |

### Missing Pieces Added

1. ✅ **Temporal story evolution** — Anchor-based clustering, update detection
2. ✅ **Update detection** — State change keywords trigger re-notification
3. ✅ **Cold start paths** — Starter packs, graceful degradation
4. ✅ **Breaking news fast-path** — Display raw, enrich async
5. ✅ **Reclustering strategy** — Micro-clusters (streaming) + HDBSCAN (batch)
6. ✅ **Multi-doc summarization** — Consensus paragraph from cluster
7. ✅ **Velocity normalization** — Per-source historical baseline
8. 🔲 **Feedback signal** — Click/dwell/save to close the loop (v0.7?)

### Evaluation Metrics to Track

```
□ Cluster purity (manual label 100 stories)
□ Duplicate escape rate (same story shown twice)
□ Important story miss rate (major news not surfaced)
□ Time-to-surface (how fast do breaking stories appear)
□ User engagement proxy (clicks, dwell time, saves)
```

---

## Implementation Order

```
v0.2: Two-Stage Clustering (biggest UX win)
  - Dedup at 0.90, story clustering at 0.82
  - Temporal decay, update detection
  ↓
v0.3: GPU Reranking (make top stories actually top)
  - Rubric queries, source weights as tiebreaker
  - Scoop detection (earliest source)
  ↓
v0.4: Article Enrichment (deeper analysis)
  - 3-tier budget, fast-path for breaking
  - Multi-doc summarization
  ↓
v0.5: Semantic Filters (user control)
  - Hybrid NL + examples + packs
  - Vector pre-filter for cost control
  ↓
v0.6: Radar + Catch Me Up (polish)
  - Multi-window velocity with normalization
  - Require 2-of-3 windows for "spiking"
```

Each version is independently useful. Ship incrementally.

---

## Appendix: Calibration Checklist

Before shipping v0.2, calibrate these:

```
□ Sample 200 random headline pairs from same day
□ Label: "same story" vs "different story" vs "duplicate"
□ Plot cosine similarity distributions
□ Set thresholds based on actual data, not theory
□ Choose embedding model (all-MiniLM-L6 vs BGE-large)
□ Test temporal decay function on 1-week archive
□ Validate update detection on 10 evolving stories
```
