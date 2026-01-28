# Pragmatism Review v2: The Revised Implementation Plan

**Reviewer:** Same Grumpy Senior Engineer
**Date:** 2026-01-28
**Verdict:** They actually listened. Mostly. I'm almost impressed.

---

## Executive Summary

Well, well, well. Someone read my review and didn't just get defensive about it. The revised plan addresses about 80% of my concerns, and addresses them correctly. The work pool is gone. The event subscription system is gone. The interfaces are gone. The phase ordering makes sense.

But I'm not here to hand out gold stars. Let me go through this properly.

---

## 1. What Was Fixed (Credit Where Due)

### 1.1 Work Pool: DELETED

**Original sin:** 60+ lines designing a job scheduling system with six priority levels for what is essentially "fetch stuff periodically."

**v2 approach:** "No work pool. Just a goroutine with a ticker."

```go
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C: c.fetchAll(ctx, program)
        }
    }
}()
```

**Verdict:** Correct. This is what fetching RSS feeds looks like. Not a priority queue. Not a ring buffer. A goroutine with a ticker. I'm satisfied.

### 1.2 Event Subscription System: DELETED

**Original sin:** Building pub/sub for internal communication in a single-process app.

**v2 approach:** "Messages are the ONLY event system. No custom pub/sub."

They use Bubble Tea's built-in message passing, which is what you should do when you're already using Bubble Tea. Novel concept: use the framework's features instead of reimplementing them.

**Verdict:** Correct.

### 1.3 Controller Interface: DELETED

**Original sin:** Premature polymorphism for two completely different "controllers."

**v2 approach:** No Controller interface. No controller package. The Coordinator is a concrete struct. The UI coordinates via injected functions.

**Verdict:** Correct.

### 1.4 Embedder Interface: DELETED

**Original sin:** "But what if we want to swap it out later?"

**v2 approach:** `OllamaEmbedder` is a concrete struct. Comment says: "Add interface only if second impl needed."

**Verdict:** Correct. That comment shows they understood the lesson.

### 1.5 Filter Interface: DELETED

**Original sin:** Every filter taking a work pool "just in case."

**v2 approach:**
```go
func ByAge(items []store.Item, maxAge time.Duration) []store.Item
func Dedup(items []store.Item) []store.Item
```

Pure functions. No interfaces. No pools. `[]Item` in, `[]Item` out.

**Verdict:** Correct. This is what filter functions should look like.

### 1.6 Phase Ordering: FIXED

**Original sin:** Phase 4 (Intake Pipeline) before Phase 3 (View Layer).

**v2 approach:**
- Phase 1: Store and Fetch
- Phase 2: UI
- Phase 3: Background Fetch
- Phase 4: Filters and Embeddings (marked OPTIONAL)

**Verdict:** Correct. Users see something in Phase 2. Background stuff comes after.

### 1.7 TimeBand in Model: FIXED

**Original sin:** `TimeBand` enum in the Model layer.

**v2 approach:** `TimeBand()` function in `ui/stream.go`.

**Verdict:** Correct. It's a view concern, it lives in the view.

### 1.8 Embedding in Item Struct: FIXED

**Original sin:** Every Item carrying 1.5KB of embedding data.

**v2 approach:**
> Embeddings are NOT loaded by default.
> Embedding storage extends the store with separate methods.

```go
func (s *Store) SaveEmbedding(id string, embedding []float32) error
func (s *Store) GetItemsWithEmbeddings(ids []string) (map[string][]float32, error)
```

**Verdict:** Correct. Load embeddings when you need them.

### 1.9 float32/float64: FIXED

**Original sin:** Type mismatch in the plan.

**v2 approach:** "Type Decision: `float32` everywhere."

**Verdict:** Correct. Pick one, document why, move on.

---

## 2. What Remains Problematic

### 2.1 The Coordinator Is Still Too Complex

Look at this thing:

```go
type Coordinator struct {
    store    *store.Store
    fetcher  *fetch.Fetcher
    sources  []fetch.Source
    mu       sync.Mutex
    lastFetch map[string]time.Time
    stopCh   chan struct{}
    wg       sync.WaitGroup
}
```

Do you actually need to track `lastFetch` per source? The plan says sources have an `Interval` field, but then the Coordinator just... ignores it and fetches everything every 5 minutes anyway.

Either:
1. Use per-source intervals (and need `lastFetch`), or
2. Fetch everything on a fixed interval (and delete `lastFetch`)

Right now you're carrying complexity for a feature you don't use.

**Recommendation:** Delete `lastFetch` and `mu`. The Coordinator becomes:

```go
type Coordinator struct {
    store   *store.Store
    fetcher *fetch.Fetcher
    sources []fetch.Source
    stopCh  chan struct{}
    wg      sync.WaitGroup
}
```

Add `lastFetch` when you actually implement per-source intervals.

### 2.2 The Function Injection Pattern Is Correct But Over-documented

The App takes three functions:
```go
type App struct {
    loadItems    func() tea.Cmd
    markRead     func(id string) tea.Cmd
    triggerFetch func() tea.Cmd
    // ...
}
```

This is good! But the plan spends 30 lines explaining why functions instead of interfaces, with a full `main.go` example. This is over-documentation for a simple concept.

**Not a bug, just... relax.** You don't need to justify every decision like you're defending a thesis.

### 2.3 Phase 4 Is Still Vague

Phase 4 says:
> This phase is optional for v0.5. Only implement when:
> - Item count exceeds ~500 and scrolling becomes painful
> - Users request semantic dedup or similarity features

That's not a phase, that's a "maybe someday." Either commit to it or cut it from the v0.5 plan entirely.

The filters (ByAge, Dedup, LimitPerSource) are useful NOW, not "when item count exceeds 500." You'll want ByAge immediately so you don't show week-old articles.

**Recommendation:** Split Phase 4:
- Move basic filters (ByAge, Dedup, LimitPerSource) to Phase 2 or 3
- Move embeddings and SemanticDedup to "v0.6 Ideas" section

### 2.4 The Concurrency Summary Table Is Good But Incomplete

The table lists:
| Structure | Protection |
|-----------|------------|
| `Store.db` | `Store.mu sync.RWMutex` |
| `Coordinator.lastFetch` | `Coordinator.mu sync.Mutex` |
| `App.items` | None needed - Bubble Tea single-threaded |

But what about `Coordinator.sources`? It's set at construction and never modified, so it's safe to read concurrently - but that assumption should be documented.

**Recommendation:** Add a note: "sources is set at construction and immutable thereafter."

---

## 3. New YAGNI Violations

Surprisingly few. But let me nitpick:

### 3.1 Source.Interval Field

```go
type Source struct {
    Type     string
    Name     string
    URL      string
    Interval time.Duration  // How often to fetch
}
```

The Coordinator fetches everything every 5 minutes regardless of this field. Why is it here? If you're not using per-source intervals, delete the field.

### 3.2 EmbedBatch

```go
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, []error)
```

When will you call this? The plan doesn't show any batch embedding flow. The single `Embed()` method is sufficient until you profile and find that embedding is a bottleneck.

**Recommendation:** Delete `EmbedBatch`. Add it when you have a profiler trace showing you need it.

### 3.3 MarkSaved Toggle

```go
func (s *Store) MarkSaved(id string, saved bool) error
```

The plan mentions "saved" items but never explains what saving does. Is this for v0.5? If not, delete it.

---

## 4. What I Would Change

### 4.1 Simplify Further

The plan is now 4 packages (store, fetch, filter, ui) plus a Coordinator in main. That's reasonable.

But I'd go one step further and inline the Coordinator into main.go until it grows past 100 lines. You don't need a type for "a goroutine that calls fetch and store."

### 4.2 Make Filters Immediate

Don't wait for Phase 4 to add filters. The first time you fetch 100 items and see duplicates and old stuff, you'll want filters. Build them in Phase 2.

### 4.3 Kill the "Optional" Label

Phase 4 being "optional" signals that you're not committed to shipping it. Either ship it or don't. Wishy-washy "optional" phases are where projects go to die.

---

## 5. The Good Parts (Being Fair)

To be clear, this revised plan is dramatically better:

1. **Four packages instead of ten.** The structure is obvious.
2. **No unnecessary interfaces.** Concrete types with test interfaces defined in test files.
3. **Bubble Tea messages as the event system.** Use the framework.
4. **Explicit concurrency contracts.** Every mutex documented.
5. **Phase 2 is the UI.** Users see something early.
6. **"What's NOT in v0.5" section.** Explicitly deferring complexity shows maturity.

The Appendix table mapping criticisms to fixes shows someone actually read the review and responded to specific points. That's rare and appreciated.

---

## 6. Final Verdict

**v1 Plan:** Building a cathedral when you need a shed.
**v2 Plan:** Building a shed. Might be slightly over-engineered for a shed, but it's definitely a shed.

**Approved with minor revisions:**
1. Delete `lastFetch` from Coordinator (YAGNI until per-source intervals)
2. Delete `Source.Interval` or implement it (pick one)
3. Delete `EmbedBatch` (add when profiler says so)
4. Move basic filters to Phase 2
5. Commit to Phase 4 or cut it

The bones are right. The complexity is appropriate for v0.5. Ship it.

---

*"I've seen worse."* - The nicest thing I say about code

*"This is actually pretty good."* - What I'm thinking but will never admit out loud
