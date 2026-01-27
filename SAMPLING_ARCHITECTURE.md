# Sampling Architecture

## Overview

A decoupled architecture for feed sampling that separates source management from display sampling. This enables:
- Adaptive polling based on source activity
- Pluggable sampling strategies
- Clean separation of concerns
- Natural handling of "bursty" vs "quiet" sources without special detection

## Architecture

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
│  ┌────────────────────┐  ┌────────────────────┐  ┌────────────────────┐ │
│  │  RoundRobinSampler │  │  WeightedSampler   │  │  RecencyMergeSampler│ │
│  │  One from each,    │  │  By source weight  │  │  Global recency    │ │
│  │  rotate through    │  │  (wire > blog)     │  │  across all queues │ │
│  └────────────────────┘  └────────────────────┘  └────────────────────┘ │
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
│ *0.7   │                │              │   *1.5     │
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

## Sampler Strategies

### 1. RoundRobinSampler (Default)
Takes one item from each source in rotation. Ensures every source gets representation.

```
Queue A: [A1, A2, A3, A4, A5]
Queue B: [B1, B2]
Queue C: [C1, C2, C3]

Sample(6) → [A1, B1, C1, A2, B2, C2]
```

### 2. WeightedSampler
Sources have weights (wire services > blogs). Higher weight = more items sampled.

```
Reuters (weight 2.0): gets ~2x items
Tech blog (weight 0.5): gets ~0.5x items
```

### 3. RecencyMergeSampler
Ignores source boundaries, takes N most recent items globally. Good for "what's happening right now" view.

### 4. CategoryBalancedSampler
Groups sources by category (Wire, Tech, Finance, Regional), samples equally from each category.

## Source Queue Structure

```go
type SourceQueue struct {
    Name         string
    Type         SourceType
    Weight       float64       // importance weight (default 1.0)

    // Queue state
    items        []feeds.Item  // newest first, soft-ordered
    mu           sync.RWMutex

    // Adaptive polling
    pollInterval time.Duration // current interval
    basePoll     time.Duration // configured base (e.g., 5min for newspapers)
    minPoll      time.Duration // floor (e.g., 30s)
    maxPoll      time.Duration // ceiling (e.g., 15min)
    lastPoll     time.Time
    lastNewCount int           // items found in last poll

    // Stats
    totalItems   int
    itemsPerDay  float64       // rolling average
}
```

## Migration Path

1. **Phase 1**: Create SourceQueue and Sampler interfaces
2. **Phase 2**: Implement RoundRobinSampler as default
3. **Phase 3**: Add adaptive polling to SourceQueue
4. **Phase 4**: Wire into existing aggregator
5. **Phase 5**: Add additional samplers (Weighted, Recency)

## Benefits

1. **Simpler mental model**: Each source is independent, sampler decides what to show
2. **No special cases**: Burst detection, chatty source caps, etc. all become unnecessary
3. **Testable**: Samplers are pure functions, easy to unit test
4. **Extensible**: New sampling strategies without touching source code
5. **Adaptive**: System naturally adjusts to source behavior

## Configuration

```json
{
  "sampling": {
    "strategy": "round_robin",
    "max_items": 500,
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
