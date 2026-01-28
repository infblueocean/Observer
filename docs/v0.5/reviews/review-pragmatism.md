# Pragmatism Review: v0.5 Implementation Plan

**Reviewer:** Grumpy Senior Engineer
**Date:** 2026-01-28
**Verdict:** This plan has good bones, but it's building a cathedral when you need a shed.

---

## Executive Summary

This plan commits the cardinal sin of v1 thinking: it designs for scale, flexibility, and "architectural purity" before proving the product works. You're six phases deep into building infrastructure for features that might never ship. Let me show you where the bodies are buried.

---

## 1. Over-Engineering (YAGNI Violations)

### 1.1 Grand Dispatch Central (GDC) - The Work Pool

**Section 1.2** proposes a full-featured work pool with:
- Six priority levels (Background, Low, Normal, High, Urgent, Critical)
- Seven work types (Fetch, Dedup, Embed, Rerank, Filter, Analyze, Intake)
- Event subscription system
- Ring buffer for completed work history
- Snapshot and Stats APIs

**The question nobody asked:** How many concurrent background operations will v0.5 actually have?

Answer: Maybe 2-3. Fetching RSS feeds. Occasionally embedding. That's it.

**What you actually need:** A simple goroutine with a channel. Maybe two goroutines. The stdlib `sync.WaitGroup` and a buffered channel would cover 90% of this. You're building a job scheduling system for what is essentially "fetch stuff periodically."

Six priority levels? When will you EVER have a "Critical" vs "Urgent" distinction that matters? You're an RSS reader, not a nuclear reactor control system.

**Recommendation:** Delete the entire `internal/work/` package. Use goroutines. Add complexity when you have an actual problem that demands it.

### 1.2 Event Subscription System

**Section 1.2** and **Section 2.4** both define event subscription patterns:
```go
Subscribe() <-chan Event
```

You're building a pub/sub system for internal communication. In a single-process application. Where you control all the code.

**What you actually need:** Function calls. Return values. Maybe a callback. This isn't a distributed system - it's one binary.

The Controller's `EventType` constants (Started, Progress, Completed, Error) with their associated structs - this is enterprise Java thinking infecting Go. Just return the result from the function.

### 1.3 Ring Buffer for History

**Section 1.2:**
> Ring buffer for completed work history (last 100 items)

Who is looking at this? Is "work history" a v0.5 feature? Did anyone ask for it? You're building forensic tools before you've built the product.

**Recommendation:** Delete it. Add logging if you need debugging.

---

## 2. Premature Abstraction

### 2.1 The Filter Interface

**Section 2.1** defines:
```go
type Filter interface {
    Name() string
    Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error)
}
```

Every filter takes a work pool. Why? "In case it needs async work." But which filters actually need it?

- TimeFilter: No
- DedupFilter: No
- SourceBalanceFilter: No
- EmbeddingFilter: No (reads from store, does math)
- RerankFilter: Maybe? But probably not.

You've polluted every filter's interface with a dependency it doesn't need, "just in case." Now every filter test needs to mock or provide a work pool.

**Recommendation:** `func([]Item) []Item` is your filter interface. If one filter genuinely needs async work, make it special-case that filter, not every filter.

### 2.2 The Controller Abstraction

**Section 2.4:**
```go
type Controller interface {
    ID() string
    Refresh(ctx context.Context, store *model.Store, pool *work.Pool)
    Subscribe() <-chan Event
}
```

How many controllers will v0.5 have? Two? `MainFeedController` and `FetchController`.

Do they need a common interface? No. They do completely different things. One runs a filter pipeline, one schedules fetches. The fact that both have "Controller" in the name doesn't mean they need polymorphism.

**Recommendation:** Delete the Controller interface. Make them concrete types. Add an interface when you have three controllers that genuinely share behavior (you won't).

### 2.3 The Embedder Interface

**Section 4.2:**
```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float64, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
    Available() bool
    Dimension() int
}
```

How many embedder implementations will exist? One. Ollama.

"But what if we want to swap it out later?" Then you write the interface later, when you know what the second implementation needs. Right now you're guessing.

**Recommendation:** Make it a concrete `OllamaEmbedder` struct. Interfaces are for when you need polymorphism, not "maybe someday."

---

## 3. Complexity Budget Analysis

You have a finite complexity budget. Let's see where you're spending it.

| Component | Lines of Design | Actual User Value |
|-----------|-----------------|-------------------|
| Work Pool + Priority Queue | ~60 lines | Zero direct value |
| Event Subscription System | ~40 lines | Zero direct value |
| Filter Interface + Pipeline | ~50 lines | Medium - enables composition |
| View Layer | ~60 lines | High - users see this |
| Intake Pipeline | ~40 lines | Medium - core functionality |
| Advanced Filters | ~50 lines | Low - "nice to have" |

**Observation:** Half your complexity budget is spent on plumbing that users will never see, touch, or benefit from directly.

**The question:** What if fetching RSS and displaying it was one simple loop? What if embedding was just a function you call? What if there was no "pool" and no "pipeline" - just code that does the thing?

---

## 4. MVP Confusion: What v0.5 Actually Needs

Looking at the stated goal ("ambient news aggregation TUI"), here's what you actually need:

### Must Have (Real MVP)
1. Fetch RSS feeds
2. Store items in SQLite
3. Display items in a TUI with time bands
4. Mark items as read
5. Basic keyboard navigation

### Nice to Have (v0.5)
1. Embedding-based dedup
2. Multiple source types (HN, Reddit)

### Not MVP (v0.6+)
1. Reranking filters
2. Work queue visualization
3. Priority-based job scheduling
4. Event subscription systems
5. "Parallel Work Opportunities" (Section: Parallel Work) - you have one developer

**Phase 5 (Advanced Filters)** is explicitly not MVP, yet it's baked into the architecture. The Filter interface takes a work pool because "reranking might need it." You're designing for Phase 5 in Phase 1.

---

## 5. Phase Ordering Problems

### Problem 1: Phase 4 Before Phase 3 Makes No Sense

Phase 3 is "View Layer" - the thing users see.
Phase 4 is "Intake Pipeline" - background embedding and dedup.

**You should ship Phase 3 before Phase 4.** A TUI that shows RSS items without embeddings is useful. An intake pipeline with no UI is useless.

### Problem 2: Phase 1 is Three Phases

Phase 1 includes:
- Project structure
- Work Pool (complex async job system)
- Model Layer (SQLite store)
- Testing strategy

The work pool alone is a phase. The model layer is a phase. Combining them under "Foundation" hides the real scope.

### Problem 3: Phase 6 is a Confession

Phase 6 is "Integration & Polish" with the goal "Wire everything together."

If you need a phase to "wire everything together," your architecture is too disconnected. In a well-designed system, wiring happens naturally as you build each phase.

**Recommendation:**
1. Phase 1: Model + Store (just SQLite, no fancy pool)
2. Phase 2: Basic Fetch + Display (end-to-end, no filters)
3. Phase 3: Filters (add pipeline when you feel the pain)
4. Phase 4: Embeddings (add when you actually need semantic features)

Delete Phase 5 (Advanced Filters) and Phase 6 (Integration) from the plan. They're not real phases; they're "stuff we'll figure out later."

---

## 6. Specific Code Critiques

### 6.1 TimeBand Enum

**Section 1.3:**
```go
type TimeBand int
const (
    TimeBandJustNow TimeBand = iota  // < 15 min
    TimeBandPastHour                  // < 1 hour
    ...
)
```

This is in the Model layer. Why? Time bands are a VIEW concern - how you display items. The model shouldn't know or care about UI groupings.

**Recommendation:** Move to view layer. It's a display concern.

### 6.2 The Embedding in the Item Struct

**Section 1.3:**
```go
type Item struct {
    ...
    Embedding  []float32  // For semantic operations
}
```

Every Item carries a 384-dimension float array (1.5KB per item) even when you're just displaying a list. You'll load 1000 items to show 50 and wonder why memory is high.

**Recommendation:** Embeddings should be loaded on-demand via a separate method, not embedded in every Item struct.

### 6.3 Priority Constants Magic Numbers

**Section 1.2:**
```go
PriorityBackground = -10
PriorityLow        = 0
PriorityNormal     = 10
PriorityHigh       = 50
PriorityUrgent     = 100
PriorityCritical   = 200
```

Why these numbers? Why the gaps? This screams "I'm leaving room for future priorities" which screams YAGNI. Three levels (Low, Normal, High) would suffice. Or zero levels - just a FIFO queue - because you don't actually have priority conflicts yet.

### 6.4 Parallel Work Opportunities

**Section: Parallel Work Opportunities** lists three "streams" of parallel work.

You have one developer (or Claude). Parallel work streams are irrelevant. This section exists to make the plan look impressive, not to help anyone build the product.

**Recommendation:** Delete this section entirely.

---

## 7. What Should Stay

To be fair, some things are right:

1. **SQLite for persistence** - Simple, embedded, no setup. Good call.
2. **Bubble Tea for TUI** - Standard choice, well-supported.
3. **Filter pipeline concept** - The idea of composable filters is sound. The implementation is overengineered.
4. **Time band UI** - Good UX for ambient reading.
5. **Graceful Ollama degradation** (Risk Analysis) - Smart to plan for this.

---

## 8. Recommended Rewrite

Here's what v0.5 should actually look like:

```
observer/
├── cmd/observer/main.go
├── internal/
│   ├── store/
│   │   └── store.go          # SQLite, no interfaces
│   ├── fetch/
│   │   └── rss.go            # Fetch RSS, return items
│   ├── ui/
│   │   ├── app.go            # Bubble Tea app
│   │   └── styles.go         # Lipgloss
│   └── filter/
│       └── filter.go         # []Item -> []Item functions, no interface
└── go.mod
```

Four packages. No "work pool." No "controller interface." No "event subscription." Just code that fetches, stores, filters, and displays.

**Phase 1:** Fetch RSS, store in SQLite, display in TUI. Ship it.
**Phase 2:** Add more sources, add filters when the list is too long.
**Phase 3:** Add embeddings when you actually need semantic search.

---

## Conclusion

This plan is thoughtful, well-structured, and about 3x more complex than it needs to be. It's the plan of someone who has read about architecture but hasn't yet felt the pain of maintaining over-engineered systems.

The best v0.5 is the one that ships. Ship something simple, feel the real pain points, then add complexity to solve real problems - not imagined ones.

**Delete:**
- The entire work pool system
- Event subscription
- Controller interface
- Embedder interface
- Phase 5 and 6 as separate phases

**Keep:**
- SQLite store
- Bubble Tea UI
- Filter composition (as simple functions)
- The humility to add complexity later

---

*"Perfection is achieved not when there is nothing more to add, but when there is nothing left to take away."* - Antoine de Saint-Exupery

*"That plan has too much crap in it."* - Me, reading this document
