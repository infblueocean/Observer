# Concurrency Review: v0.5 Implementation Plan

**Reviewer:** Grumpy Senior Concurrency Engineer
**Date:** 2026-01-28
**Verdict:** NOT READY FOR IMPLEMENTATION

This plan reads like it was written by someone who has never been woken up at 3am by a deadlock in production. There are critical concurrency issues that will cause race conditions, deadlocks, and goroutine leaks if implemented as specified. Let me enumerate the sins.

---

## 1. Race Conditions

### 1.1 Work Pool Item Mutations (Section 1.2, lines 89-102)

The `Item` struct contains mutable fields that will be accessed from multiple goroutines:

```go
type Item struct {
    Status      Status      // Modified by workers
    StartedAt   time.Time   // Modified when work starts
    FinishedAt  time.Time   // Modified when work completes
    Progress    float64     // Modified during execution
    Result      string      // Modified at completion
    Error       error       // Modified at completion
}
```

**Problem:** The plan says nothing about how these fields are protected. Workers will modify `Status`, `Progress`, etc. while the UI reads them via `Snapshot()`. This is a textbook data race.

**Questions the plan must answer:**
- Is `Item` protected by a mutex? Per-item or global?
- Is `Snapshot()` returning copies or references?
- If the UI calls `Snapshot()` while a worker is updating `Progress`, what happens?

### 1.2 FetchController's lastFetched Map (Section 4.4, lines 486-498)

```go
type FetchController struct {
    lastFetched map[string]time.Time  // DANGER
}
```

**Problem:** Maps in Go are not safe for concurrent access. The plan shows `FetchController` submitting work to the pool, which means multiple goroutines (workers) may call back into the controller. If `lastFetched` is read while being updated, you get a runtime panic.

**The plan is silent on:**
- Who writes to `lastFetched`?
- When is it read?
- What synchronization primitive protects it?

### 1.3 DedupIndex Concurrent Access (Section 4.3, lines 471-480)

```go
type DedupIndex struct {
    index *hnsw.Graph[string]  // Is this thread-safe?
}
```

**Problem:** HNSW graphs typically are NOT thread-safe for concurrent reads and writes. The intake pipeline will be adding to the index while filters may be querying it.

**Questions:**
- Is `coder/hnsw` thread-safe? (Probably not - most implementations aren't)
- What happens when embedding filter queries the index while intake adds new vectors?
- The plan just assumes this magically works.

### 1.4 Store Concurrent Access (Section 1.3, lines 168-180)

The `Store` interface is accessed from:
- Work pool workers (embedding, dedup, intake)
- FetchController (storing items)
- MainFeedController (reading items)
- UI (marking read/saved)

**Problem:** While SQLite with WAL mode can handle concurrent reads, the plan doesn't specify:
- Connection pooling strategy
- Transaction isolation levels
- Whether `SaveEmbeddings` batches are atomic
- What happens if UI's `MarkRead` conflicts with intake's `SaveItems`

### 1.5 Subscription Channel Races (Section 1.2, lines 111-112)

```go
Subscribe() <-chan Event
```

**Problem:** The plan doesn't address:
- What happens if `Subscribe()` is called multiple times?
- How is the subscriber list managed?
- If a slow subscriber blocks, do all subscribers block?
- What happens if a subscriber is added while events are being dispatched?

---

## 2. Deadlocks

### 2.1 Filter Pipeline Holding Store Lock While Waiting on Pool (Section 2.2)

The pipeline runs filters sequentially (lines 257-266). Consider this scenario:

1. EmbeddingFilter acquires a read lock on store
2. EmbeddingFilter submits work to pool
3. Pool worker needs to write to store (some cleanup task)
4. Worker blocks waiting for store lock
5. Pool is at capacity
6. EmbeddingFilter waits for pool result
7. **DEADLOCK**: Filter holds lock, waits for pool. Pool waits for lock.

The plan shows filters receiving `*work.Pool` (line 239) but says nothing about lock ordering or preventing this scenario.

### 2.2 Context Cancellation During Shutdown (Section 6.1, lines 576-598)

```go
pool.Start(context.Background())
defer pool.Stop()
fetchCtrl.Start(context.Background())
defer fetchCtrl.Stop()
```

**Problem:** The shutdown order is wrong. Defers execute in LIFO order, so:
1. `fetchCtrl.Stop()` is called
2. `pool.Stop()` is called
3. But what if `fetchCtrl.Stop()` needs to submit cleanup work to the pool that's already stopped?

Also, both use `context.Background()`. How does the application signal shutdown? Does `pool.Stop()` cancel its context? Does it wait for in-flight work?

### 2.3 Circular Dependencies Between Controllers (Section 6.2 diagram)

The diagram shows:
- MainFeedController calls Store
- FetchController calls Store and Pool
- Pool workers call Store

If any of these acquire locks in different orders, you get deadlock. The plan specifies NO lock ordering protocol.

---

## 3. Channel Semantics

### 3.1 Who Closes Channels? (Multiple sections)

The plan defines multiple subscription channels:
- `Pool.Subscribe() <-chan Event` (line 111)
- `Controller.Subscribe() <-chan Event` (line 304)

**Critical questions not answered:**
- Who closes these channels?
- When are they closed?
- What does the receiver do when the channel closes?
- If the pool is stopped, do subscribers get a close signal or just hang forever?

### 3.2 Buffered vs Unbuffered - The Plan is Silent

Line 121 mentions "Signal-based work dispatch (non-blocking channel)" but:
- How big is the buffer?
- What happens when the buffer is full?
- Does `Submit()` block? Return error? Drop work?
- If unbuffered, any slow worker starves all submissions.

### 3.3 Goroutine Leaks in Subscriptions

If a subscriber forgets to drain its channel:
1. Pool sends event
2. Subscriber's channel blocks (full or unbuffered)
3. Pool's send goroutine blocks
4. Over time, you leak goroutines

**The plan must specify:**
- Maximum subscribers
- Send timeout/non-blocking sends
- How to unsubscribe
- Automatic cleanup of dead subscribers

### 3.4 Event Channel in Controller (Section 2.5, line 320)

```go
type MainFeedController struct {
    events chan controller.Event
}
```

**Problems:**
- Buffer size?
- What if UI doesn't consume events fast enough?
- Memory growth when events queue up?

---

## 4. Resource Contention

### 4.1 Worker Count vs Resource Limits (Section 1.2, line 119)

"Worker count defaults to `runtime.NumCPU()`"

**Problem:** This is naive. Different work types have different resource profiles:
- `TypeEmbed` - GPU/memory bound (Ollama)
- `TypeFetch` - Network I/O bound
- `TypeRerank` - GPU/memory bound
- `TypeFilter` - CPU bound

Running NumCPU embedding tasks simultaneously will OOM your system. Running NumCPU fetch tasks will be throttled by network.

**The plan should specify:**
- Per-type worker limits
- Resource semaphores
- Backpressure mechanisms

### 4.2 Ollama as Singleton Resource (Section 4.2)

The embedder hits a single Ollama instance. If you submit 16 embed jobs to a pool with 16 workers:
- All 16 workers try to hit Ollama simultaneously
- Ollama queues them internally or fails
- You've just turned your parallel pool into a serial bottleneck with extra overhead

**Missing:** Rate limiting, connection pooling, or dedicated embed workers.

### 4.3 SQLite Connection Contention

With multiple workers hitting SQLite:
- `SaveItems` and `SaveEmbeddings` will contend for write locks
- Long-running filters reading items block writers
- The "proper indexing" mitigation (line 659) doesn't help with lock contention

**Missing:** Connection pool configuration, read replica strategy, transaction sizing.

---

## 5. Context Handling

### 5.1 Context Not Threaded Through Store Operations

The `Store` interface (lines 168-180) has NO context parameters:

```go
SaveItems(items []Item) (int, error)
GetItems(limit int, includeRead bool) ([]Item, error)
```

**Problem:** If a filter is cancelled, in-flight database operations can't be cancelled. Long queries continue running while the caller has given up.

### 5.2 Pipeline Cancellation Is Incomplete (Section 2.2, line 266)

"Check context cancellation between each filter stage"

This is insufficient. If a filter's `Run()` takes 30 seconds, checking between stages means you wait 30 seconds to respond to cancellation. The context must be checked WITHIN long-running filters, not just between them.

### 5.3 Work Pool Context Propagation

When `Pool.Stop()` is called:
- Is the context passed to `Start()` cancelled?
- Do in-flight work items receive cancellation?
- Is there a grace period?
- What happens to pending work items?

The plan mentions "Signal-based work dispatch" but nothing about cancellation propagation.

### 5.4 FetchController Context Leaks (Section 4.4)

```go
func (c *FetchController) Start(ctx context.Context)
```

If this spawns goroutines (it must, to periodically fetch), what happens when `ctx` is cancelled but `Stop()` is never called? Goroutine leak.

---

## 6. Additional Concerns

### 6.1 Priority Queue Thread Safety (Section 1.2)

"Use `container/heap` for O(log n) priority queue operations"

**Problem:** `container/heap` is NOT thread-safe. The plan must specify how the queue is protected when:
- `Submit()` pushes items
- Workers pop items
- `Snapshot()` reads the queue state

### 6.2 Ring Buffer Concurrent Access (Section 1.2, line 121)

"Ring buffer for completed work history (last 100 items)"

**Problem:** Ring buffers have notoriously tricky concurrent access patterns. When a worker writes to position N while `Snapshot()` reads:
- Do you see partial writes?
- Do you get consistent snapshots?
- What synchronization is used?

### 6.3 No Mention of `sync.WaitGroup` for Graceful Shutdown

The plan shows `defer pool.Stop()` but doesn't describe how to wait for in-flight work. Without proper wait groups:
- Application exits while work is in progress
- Database writes may be interrupted
- Partial state is left behind

---

## Summary of Required Changes

Before this plan is implementation-ready, it MUST specify:

1. **Synchronization strategy for every shared data structure:**
   - `Item` fields: mutex or copy-on-read?
   - `lastFetched` map: `sync.RWMutex` or `sync.Map`?
   - Priority queue: protected by what?
   - Ring buffer: lock-free or mutex?
   - DedupIndex: external synchronization required?

2. **Channel contracts for every channel:**
   - Buffer sizes
   - Who creates, who closes
   - Backpressure behavior
   - Unsubscribe mechanism

3. **Lock ordering protocol:**
   - Document which locks may be held simultaneously
   - Specify acquisition order
   - Prohibit holding locks while waiting on channels/pools

4. **Context threading:**
   - Add `context.Context` to ALL Store methods
   - Specify cancellation behavior for each component
   - Document graceful shutdown sequence

5. **Resource limits:**
   - Per-type worker limits or semaphores
   - Ollama concurrency limit
   - SQLite connection pool size

6. **Shutdown protocol:**
   - Order of component shutdown
   - Grace period for in-flight work
   - WaitGroup usage for completion signaling

---

## Verdict

This plan has the architecture roughly right but is dangerously underspecified on concurrency. Implementing it as-is will result in:
- Data races detectable by `-race` (if you're lucky)
- Subtle corruption (if you're not)
- Deadlocks under load
- Goroutine leaks
- Resource exhaustion

**Do not proceed to implementation until every question above has a concrete answer.**

---

*"Concurrency is hard. Distributed systems are harder. But at least in distributed systems, you expect things to fail. In concurrent code, people expect it to work."*
