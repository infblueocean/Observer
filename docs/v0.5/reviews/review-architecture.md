# Architectural Review: Observer v0.5 Implementation Plan

**Reviewer:** Senior Engineer
**Date:** 2026-01-28
**Verdict:** This plan needs significant work before implementation.

---

## Executive Summary

The v0.5 implementation plan has good intentions but exhibits several concerning patterns that will cause pain at scale. The "MVC" framing is misleading, the interface designs are leaky, and the phase dependencies hide critical coupling issues. Below is a detailed teardown.

---

## 1. The "MVC" Lie

The plan claims MVC separation but delivers something far messier.

### Section Reference: Phase 2.4, Phase 3.4

Look at the `Controller.Refresh` signature:

```go
Refresh(ctx context.Context, store *model.Store, pool *work.Pool)
```

And the root app model:

```go
type Model struct {
    store          *model.Store
    pool           *work.Pool
    fetchCtrl      *controllers.FetchController
    mainFeedCtrl   *controllers.MainFeedController
    // ...
}
```

**The Problem:** The View directly holds references to the Model (`*model.Store`) and passes it to Controllers. This is not MVC - this is "everything knows about everything." True MVC has the Controller mediate all Model-View interaction.

**Consequence:** When you need to change how the Store works (sharding, caching layer, different DB), you must touch the View layer. When you add a new Controller, you must modify the root app model. The "separation" is purely organizational, not architectural.

**What Should Happen:** The View should receive items via events/messages only. It should never hold a `*model.Store`. Controllers should own their data access entirely.

---

## 2. Filter Interface is Too Coupled

### Section Reference: Phase 2.1

```go
Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error)
```

**The Problem:** Why does every filter receive the entire work pool? The plan says (Section 2.1) this enables async filters, but:

1. Most filters are synchronous (TimeFilter, DedupFilter, SourceBalanceFilter)
2. The filters that need async (RerankFilter) have fundamentally different execution semantics
3. Passing `*work.Pool` to every filter creates an implicit dependency on GDC throughout the codebase

**Consequence:** You cannot test filters in isolation without mocking the work pool. You cannot reuse filters outside this system. Every filter author must understand GDC semantics even for trivial filters.

**What Should Happen:** Synchronous filters should not see the pool at all. Create a separate `AsyncFilter` interface:

```go
type Filter interface {
    Name() string
    Run(ctx context.Context, items []model.Item) ([]model.Item, error)
}

type AsyncFilter interface {
    Filter
    RunAsync(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error)
}
```

The Pipeline can check the interface and call appropriately.

---

## 3. The Intake Pipeline is a Controller in Disguise

### Section Reference: Phase 4.1, Phase 4.4

Compare these two things:

**Intake Pipeline (Phase 4.1):**
```go
type Pipeline struct {
    embedder   embedding.Embedder
    dedupIndex *embedding.DedupIndex
    store      *model.Store
    pool       *work.Pool
}
```

**FetchController (Phase 4.4):**
```go
type FetchController struct {
    sources     []fetch.SourceConfig
    store       *model.Store
    pool        *work.Pool
    lastFetched map[string]time.Time
}
```

**The Problem:** The "Intake Pipeline" in `internal/intake/` is structurally identical to Controllers in `internal/controller/controllers/`. It has the same dependencies, same responsibility pattern (process items, interact with store). But it lives in a different package with a different name.

**Consequence:** You now have two different "pipeline" concepts:
- `controller.Pipeline` - chains filters
- `intake.Pipeline` - processes fetched items

When someone asks "where does deduplication happen?" the answer is "both places" (DedupFilter in controller pipeline, DedupIndex in intake pipeline). This is a maintenance nightmare.

**What Should Happen:** The intake pipeline IS a controller. Put it in `internal/controller/controllers/intake.go`. Use the filter pipeline pattern consistently. Embed/dedup/store become filters in an intake pipeline.

---

## 4. Embedding Type Mismatch

### Section Reference: Phase 1.3, Phase 4.2

Model layer (Phase 1.3):
```go
Embedding  []float32  // For semantic operations
```

Embedder interface (Phase 4.2):
```go
Embed(ctx context.Context, text string) ([]float64, error)
EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
```

**The Problem:** The model stores `float32`, the embedder returns `float64`. This is not a minor issue - it affects every single place that touches embeddings.

**Consequence:** Silent precision loss on every embed operation. Conversion code scattered throughout the codebase. Potential subtle bugs in similarity calculations when someone forgets to convert.

**What Should Happen:** Pick ONE type. `float32` is correct for embeddings (saves memory, GPU uses float32 anyway). Fix the Embedder interface.

---

## 5. Store Interface Has Cohesion Problems

### Section Reference: Phase 1.3

```go
type Store interface {
    SaveItems(items []Item) (int, error)
    GetItems(limit int, includeRead bool) ([]Item, error)
    GetItemsSince(since time.Time) ([]Item, error)
    MarkRead(id string) error
    MarkSaved(id string, saved bool) error
    SaveEmbedding(id string, embedding []float32) error
    SaveEmbeddings(embeddings map[string][]float32) error
    GetItemsWithEmbeddings(limit int, since time.Time) ([]Item, error)
    GetItemsWithoutEmbedding(limit int) ([]Item, error)
    ItemCount() (int, error)
    Close() error
}
```

**The Problem:** This is three different responsibilities crammed into one interface:

1. Item CRUD: `SaveItems`, `GetItems`, `GetItemsSince`, `MarkRead`, `MarkSaved`
2. Embedding storage: `SaveEmbedding`, `SaveEmbeddings`, `GetItemsWithEmbeddings`, `GetItemsWithoutEmbedding`
3. Lifecycle: `ItemCount`, `Close`

**Consequence:** Every consumer of the Store must depend on the full interface even if they only need items. The embedding subsystem is tightly coupled to item storage. You cannot swap embedding storage to a vector DB without touching the core Store.

**What Should Happen:** Interface segregation:

```go
type ItemStore interface {
    SaveItems(items []Item) (int, error)
    GetItems(opts QueryOptions) ([]Item, error)
    MarkRead(id string) error
    MarkSaved(id string, saved bool) error
}

type EmbeddingStore interface {
    SaveEmbedding(id string, embedding []float32) error
    SaveEmbeddings(embeddings map[string][]float32) error
    GetEmbeddings(ids []string) (map[string][]float32, error)
}
```

---

## 6. Hidden Phase Dependencies

### Section Reference: Parallel Work Opportunities

The plan says:
- Phase 2 depends on Phase 1
- Phase 3 depends on Phase 2
- Phase 4 depends on Phase 1
- Phase 5 depends on Phase 4

**The Problem:** This is incomplete. Let me trace the ACTUAL dependencies:

**Phase 3 (View) depends on:**
- Phase 1 (model.Store - directly held in Model)
- Phase 1 (work.Pool - directly held in Model)
- Phase 2 (controller.Event)
- Phase 2 (MainFeedController)
- Phase 4 (FetchController) - Look at the root app model! It holds `fetchCtrl`

**Phase 5 (Advanced Filters) depends on:**
- Phase 1 (model.Store - EmbeddingFilter holds it)
- Phase 4 (embedding.Embedder - needed for rerank)

**Consequence:** Phase 3 cannot actually start until Phase 4's FetchController is done. The "parallel streams" are a fiction. Stream 3 (UI) is blocked on Stream 1 AND parts of Phase 4.

**What Should Happen:** Either:
1. Remove FetchController from the root app model (make it truly background)
2. Or be honest about dependencies and revise the schedule

---

## 7. Work Pool API is Inconsistent

### Section Reference: Phase 1.2

```go
Submit(item *Item) string
SubmitFunc(typ Type, desc string, fn func() (string, error)) string
SubmitWithPriority(typ Type, desc string, priority int, fn func() (string, error)) string
```

**The Problem:** Three ways to submit work with overlapping functionality:

- `Submit` takes an `*Item` (which has Priority, Type, Description already)
- `SubmitFunc` takes separate type, desc, and a function
- `SubmitWithPriority` adds priority to `SubmitFunc`

Why does `Submit` take an `*Item` pointer but the others construct items internally? Why isn't `SubmitFunc` just `SubmitWithPriority(..., PriorityNormal, ...)`?

**Consequence:** Inconsistent usage patterns across the codebase. Some places use `Submit` with pre-built items, others use `SubmitFunc`. When you need to add metadata to work items, which path do you modify?

**What Should Happen:** One canonical submission method:

```go
Submit(opts WorkOptions) string

type WorkOptions struct {
    Type        Type
    Description string
    Priority    int  // defaults to PriorityNormal
    Fn          func() (string, error)
}
```

---

## 8. Event System is Underspecified

### Section Reference: Phase 1.2, Phase 2.4

Work pool events (Phase 1.2):
```go
Subscribe() <-chan Event
```

Controller events (Phase 2.4):
```go
Subscribe() <-chan Event
```

**The Problem:** Two different `Event` types with the same pattern. The view must manage multiple event channels:

```go
controllerChan <-chan controller.Event
workEventChan  <-chan work.Event
```

**Consequence:** The view's Update function becomes a fan-in nightmare. Adding a new event source means modifying the root app. No unified event bus means no way to add cross-cutting concerns (logging, metrics) to events.

**What Should Happen:** Either:
1. A unified event system with typed events
2. Or Bubble Tea commands (the idiomatic approach) instead of raw channels

Bubble Tea ALREADY has a message system. Why are we building a parallel channel-based event system?

---

## 9. The DedupIndex Has No Persistence Story

### Section Reference: Phase 4.3

```go
type DedupIndex struct {
    embedder  Embedder
    threshold float64
    index     *hnsw.Graph[string]
}
```

**The Problem:** The HNSW index lives in memory. It's populated from... where? The plan doesn't say. If it's rebuilt from SQLite on startup, you're loading ALL embeddings into memory. If it's persisted separately, you have consistency issues between SQLite and the HNSW file.

**Consequence:** Either OOM on startup with large datasets, or subtle bugs when the index diverges from the database, or both.

**What Should Happen:** Define the persistence model explicitly:
- Is the HNSW index ephemeral (rebuilt each run)?
- If persisted, how is it kept in sync with SQLite?
- What happens when you add items while the index is being rebuilt?

---

## 10. No Error Recovery Strategy

### Section Reference: Entire document

The plan mentions errors in passing but has no recovery strategy:

- What happens when Ollama fails mid-batch? (Phase 4)
- What happens when SQLite transactions fail? (Phase 1)
- What happens when a filter panics? (Phase 2)
- What happens when the work pool deadlocks? (Phase 1)

**Consequence:** The first production error will cascade unpredictably. You'll be debugging "why did the whole app freeze" instead of "why did this one operation fail."

**What Should Happen:** Each phase needs:
1. Failure modes enumerated
2. Recovery actions defined
3. Degraded operation modes specified

---

## Summary of Required Changes

1. **Fix the MVC violation** - View should not hold Store references
2. **Split Filter interfaces** - Sync and Async are different concerns
3. **Merge Intake into Controller pattern** - One pipeline concept, not two
4. **Fix embedding types** - float32 everywhere
5. **Segregate Store interfaces** - Items and Embeddings are separate concerns
6. **Revise phase dependencies** - Phase 3 actually needs Phase 4
7. **Unify Work Pool submission API** - One method, not three
8. **Use Bubble Tea messages** - Don't build parallel event system
9. **Define DedupIndex persistence** - Memory or disk, pick one and document
10. **Add error recovery to each phase** - Failure modes are not optional

---

## Conclusion

This plan will produce a working system, but it will be painful to maintain and extend. The architectural boundaries are drawn in the wrong places, creating coupling where there should be separation and separation where there should be unity.

The biggest red flag is the "MVC" claim. When your View holds a `*model.Store`, you don't have MVC - you have a monolith with extra directories. Fix the fundamentals before writing code, or you'll be rewriting again in v0.6.
