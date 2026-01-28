# Sampling Architecture

## Overview

A decoupled architecture for feed sampling that separates source management from display sampling. This enables:
- Adaptive polling based on source activity
- Pluggable sampling strategies
- Clean separation of concerns
- Natural handling of "bursty" vs "quiet" sources without special detection

**Core Philosophy**: "Firehose to DB, curated to UI"
- Polling stores everything (complete record)
- Sampling controls what appears (balanced exposure)

---

## Current Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           SOURCE QUEUES                                  │
│                                                                          │
│  Each source maintains its own queue of items (newest first) and        │
│  adapts its polling interval based on observed activity.                 │
│                                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                   │
│  │   Reuters    │  │     SEC      │  │    Nikkei    │   ... ×200+      │
│  │──────────────│  │──────────────│  │──────────────│                   │
│  │ items: 15    │  │ items: 127   │  │ items: 89    │                   │
│  │ poll: 45s    │  │ poll: 8m     │  │ poll: 3m     │   ← adaptive     │
│  │ lastPoll: .. │  │ lastPoll: .. │  │ lastPoll: .. │                   │
│  └──────────────┘  └──────────────┘  └──────────────┘                   │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         SAMPLER INTERFACE                                │
│                                                                          │
│  type Sampler interface {                                                │
│      Sample(queues []*SourceQueue, n int) []feeds.Item                   │
│  }                                                                       │
│                                                                          │
│  The sampler is agnostic to source chattiness - it just pulls from      │
│  queues according to its strategy.                                       │
│                                                                          │
│  Implementations:                                                        │
│  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐            │
│  │ RoundRobin      │ │ DeficitRR       │ │ FairRecent      │            │
│  │ Simple rotation │ │ Credit-based    │ │ Quota + recency │            │
│  └─────────────────┘ └─────────────────┘ └─────────────────┘            │
│  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐            │
│  │ WeightedRR      │ │ ThrottledRecency│ │ RecencyMerge    │            │
│  │ By source weight│ │ Caps + recency  │ │ Global newest   │            │
│  └─────────────────┘ └─────────────────┘ └─────────────────┘            │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                           STREAM VIEW                                    │
│                                                                          │
│  Displays whatever the sampler provides. Time bands, interleaving,      │
│  and visual presentation remain here.                                    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Adaptive Polling

Instead of fixed refresh intervals, sources adapt based on observed activity:

```
                    Found new items
                          │
    ┌─────────────────────┼─────────────────────┐
    │                     │                     │
    ▼                     │                     ▼
┌────────┐                │              ┌────────────┐
│ SPEED  │                │              │   SLOW     │
│  UP    │                │              │   DOWN     │
│        │                │              │            │
│ ×0.7   │                │              │   ×1.5     │
│        │                │              │            │
│ floor: │                │              │ ceiling:   │
│  30s   │                │              │   15min    │
└────────┘                │              └────────────┘
                          │
                   No new items
```

**Behavior:**
- Source with frequent updates (Reuters): settles around 30-60s
- Source with rare updates (academic blog): settles around 10-15min
- Source that suddenly has news: quickly adapts down to floor
- "Burst detection" becomes unnecessary - adaptive polling handles it naturally

---

## Sampler Strategies

### Core Principle: Two-Stage Sampling

From brain trust recommendations (GPT-5, Grok-4):

1. **Choose a source** under fairness/weights/exploration rules
2. **Choose item(s) within that source** under recency/quality rules

This prevents "one chatty source dominates" while keeping per-source queues useful.

---

### Implemented Samplers

#### 1. RoundRobinSampler
**Purpose**: Simple balanced view with equal representation.

Takes one item from each source in rotation. Ensures every source gets representation regardless of how many items they have.

```
Queue A: [A1, A2, A3, A4, A5]
Queue B: [B1, B2]
Queue C: [C1, C2, C3]

Sample(6) → [A1, B1, C1, A2, B2, C2]
```

**Config**:
- `MaxPerSource`: Cap items per source (0 = no limit)

**Good for**: Simple balanced view, testing.

---

#### 2. DeficitRoundRobinSampler (Recommended Default)
**Purpose**: Strict long-run fairness even with bursty sources.
**Source**: GPT-5's top recommendation for fairness.

Each source accumulates "credit" (deficit) based on its weight. Items are emitted when credit >= 1.0. This handles sources that are empty for a while then explode with content.

```go
// Each sampling tick:
deficit[source] += quantum * weight
while deficit[source] >= 1.0 && source.hasItems():
    emit(source.next())
    deficit[source] -= 1.0
```

**Config**:
- `Quantum`: Credit added per round (default 1.0)
- `MaxPerSource`: Cap items per source per sample

**Why better than plain RoundRobin**: When source A has 0 items and source B has 100, plain RR would skip A. DRR accumulates credit for A, so when A finally publishes, it gets fair representation.

**Good for**: Balanced view, front page, default stream.

---

#### 3. FairRecentSampler (Balanced + Fresh)
**Purpose**: Combine fairness quotas with recency preference.
**Source**: Grok-4's top recommendation.

1. Take up to N items per source from recent window (default 24h)
2. Sort all candidates by recency (newest first)
3. Optional: apply per-source cooldown (minimum spacing)

```
Config: QuotaPerSource=20, MaxAge=24h

SEC (127 items)     → takes 20 newest
Reuters (15 items)  → takes all 15
Academic (3 items)  → takes all 3

Sort by recency → return top N
```

**Config**:
- `QuotaPerSource`: Max items per source (default 20)
- `MaxAge`: Filter out items older than this (default 24h)
- `PerSourceCooldown`: Minimum items between same source (default 0)

**Good for**: Default stream view, "what's happening" with balance.

---

#### 4. ThrottledRecencySampler (Breaking News)
**Purpose**: Recency-first with per-source caps to prevent firehose dominance.
**Source**: GPT-5's recommendation for "Recent" view.

1. Sort all items by recency (newest first)
2. Take items, capping each source at MaxPerSource

```
Config: MaxPerSource=3

Firehose source publishes 50 items in 10 minutes
→ Only 3 make it to the view
→ Other sources still get representation
```

**Config**:
- `MaxPerSource`: Cap items per source in result (default 3)

**Good for**: Breaking news view, "right now" without firehose dominance.

---

#### 5. WeightedRoundRobinSampler
**Purpose**: Editorial control via source weights.

Sources have weights (wire services > blogs). Higher weight = more items sampled proportionally.

```
Reuters (weight 2.0): gets ~2x items
Tech blog (weight 0.5): gets ~0.5x items
```

Uses credit system similar to DRR but normalized by average weight.

**Config**:
- `MinWeight`: Sources below this are skipped (default 0.1)

**Good for**: Trusted sources priority, editorial curation.

---

#### 6. RecencyMergeSampler
**Purpose**: Pure recency, ignore source boundaries.

Collects all items from all queues, sorts by published time, returns top N.

**Warning**: Can let chatty sources dominate. Use with per-source caps if needed.

**Good for**: "What's happening right now" when you don't care about balance.

---

### Recommended View Recipes

| View | Sampler | Config | Rationale |
|------|---------|--------|-----------|
| **Default Stream** | FairRecentSampler | QuotaPerSource=20, MaxAge=24h | Balanced + fresh |
| **Breaking News** | ThrottledRecencySampler | MaxPerSource=3 | Recency without firehose |
| **Front Page** | DeficitRoundRobinSampler | Weighted | Strict fairness |
| **Firehose** | RecencyMergeSampler | (none) | Raw chronological |
| **Curated** | WeightedRoundRobinSampler | Custom weights | Editorial control |

---

## Brain Trust Insights

### GPT-5 Recommendations

#### Fairness Strategies

| Strategy | Description | Implementation |
|----------|-------------|----------------|
| **Deficit Round Robin (DRR)** | Credit-based emission, strict long-run fairness | `DeficitRoundRobinSampler` |
| **Exposure caps (sliding window)** | Max N per source in last M items shown | Config option |
| **Target-share controller** | Maintain exposure ratios, correct drift | Future enhancement |

#### Anti-Domination Tactics

1. **Hard caps**: Max 3 items per source in any sample
2. **Sliding window caps**: Max 5 items per source in last 50 shown
3. **Cooldown**: Minimum spacing between same-source items (implemented in FairRecentSampler)

#### Advanced Strategies (Future)

- **MMR (Maximal Marginal Relevance)**: Re-rank for topic diversity
- **Stratified quotas by topic**: Allocate slots per topic category
- **Constrained contextual bandit**: Engagement optimization with fairness floors
- **Interest decay**: Anti-obsession for repeated topic exposure

### Grok-4 Recommendations

#### Core Pattern: FairRecent

```python
def sample(stories, count):
    recent = [s for s in stories if (now - s.pub_ts) < 24h]
    quota = defaultdict(list)
    for s in recent:
        quota[s.source_id].append(s)
    balanced = []
    for src, lst in quota.items():
        balanced.extend(lst[:20])  # quota
    return sorted(balanced, key=lambda s: s.pub_ts)[-count:]
```

#### Recommended Metrics

Track per view:
- **Source entropy / Gini**: Measure fairness (lower Gini = more balanced)
- **Duplicate rate**: Cluster collisions (needs correlation engine)
- **Median age**: How fresh is the view
- **Topic entropy**: Diversity (needs topic classification)

---

## Advanced Architecture (Future)

### Per-Source Queuing with Worker Pools

From Grok's architecture feedback, the next evolution adds:
1. **Bounded channels** for backpressure
2. **Worker pool** for GPU-efficient batching
3. **Reranking step** before display

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     PER-SOURCE QUEUING (Future)                          │
│                                                                          │
│  SourceHandler A → chan[200] ─┐                                         │
│  SourceHandler B → chan[200] ─┼→ Fan-in → Worker Pool → Reranker        │
│  SourceHandler C → chan[200] ─┘                                         │
│                                                                          │
│  Benefits:                                                               │
│  • Natural backpressure (bounded channels)                              │
│  • Fairness per source (per-source caps)                                │
│  • Easy parallelism (goroutines)                                        │
│  • Per-source metrics (queue length, latency, errors)                   │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### Full Pipeline Vision

```
Polling Layer (adaptive intervals)
    ↓
Per-Source Queues (bounded channels, backpressure)
    ↓
Fan-in to Worker Pool (batches across sources)
    ↓
GPU Reranking (jina-reranker-v3 or Qwen3-Reranker-4B)
    ↓
Correlation Engine (clustering, dedup, entity extraction)
    ↓
Samplers (fairness, diversity)
    ↓
Stream View
```

### Reranking Options

| Option | Setup | Performance (RTX 5070) | Fallback |
|--------|-------|------------------------|----------|
| **jina-reranker-v3** | ONNX + Hugot (pure Go) | 7k headlines in 1-4s | CPU: 20-70s |
| **Qwen3-Reranker-4B** | Ollama | 7k headlines in 2-6s | CPU: 20-90s |

Both options:
- Auto-detect GPU (CUDA)
- Graceful CPU fallback
- Batch efficiently (500-2000 items per call)

### Integration with Correlation Engine

The correlation engine (see CORRELATION_ENGINE.md) handles:
- **Duplicate detection**: SimHash on titles
- **Entity extraction**: Tickers, countries, people
- **Clustering**: Group by entity overlap + similarity
- **Disagreement detection**: Conflicting claims

Integration points:
1. Correlation engine removes duplicates BEFORE sampling
2. Reranker scores unique stories
3. Sampler applies fairness on top

### Evolution Path

| Phase | Status | Description |
|-------|--------|-------------|
| 1. Sampling architecture | ✅ Done | SourceQueue, Sampler interface |
| 2. Basic samplers | ✅ Done | RoundRobin, Weighted, Recency |
| 3. Advanced samplers | ✅ Done | DRR, FairRecent, Throttled |
| 4. Correlation engine | 🔜 Next | Clustering, dedup, entities |
| 5. Reranking worker pool | 🔮 Future | GPU batching, ML scoring |
| 6. Bounded channels | 🔮 Future | Backpressure, metrics |

---

## Source Queue Structure

```go
type SourceQueue struct {
    Name         string
    SourceType   feeds.SourceType
    Weight       float64           // importance weight (default 1.0)

    // Queue state
    items        []feeds.Item      // newest first, soft-ordered
    mu           sync.RWMutex

    // Adaptive polling
    pollInterval time.Duration     // current interval
    basePoll     time.Duration     // configured base (e.g., 5min for newspapers)
    minPoll      time.Duration     // floor (e.g., 30s)
    maxPoll      time.Duration     // ceiling (e.g., 15min)
    lastPoll     time.Time
    lastNewCount int               // items found in last poll

    // Stats
    totalItems   int
    itemsPerDay  float64           // rolling average
}
```

---

## Configuration

```json
{
  "sampling": {
    "strategy": "fair_recent",
    "max_items": 500,
    "fair_recent": {
      "quota_per_source": 20,
      "max_age_hours": 24,
      "cooldown": 0
    },
    "throttled_recency": {
      "max_per_source": 3
    },
    "deficit_rr": {
      "quantum": 1.0,
      "max_per_source": 0
    },
    "adaptive_polling": {
      "enabled": true,
      "min_interval": "30s",
      "max_interval": "15m",
      "speedup_factor": 0.7,
      "slowdown_factor": 1.5
    }
  }
}
```

---

## Benefits

1. **Simpler mental model**: Each source is independent, sampler decides what to show
2. **No special cases**: Burst detection, chatty source caps, etc. all become unnecessary
3. **Testable**: Samplers are pure functions, easy to unit test
4. **Extensible**: New sampling strategies without touching source code
5. **Adaptive**: System naturally adjusts to source behavior
6. **Transparent**: User can understand why they see what they see

---

## References

- GPT-5 consultation (2026-01-26): Fairness strategies, DRR, MMR
- Grok-4 consultation (2026-01-26): FairRecent, per-source queuing architecture
- Google News algorithm: Clustering + source diversity + freshness
- Deficit Round Robin: Network fair queuing literature
