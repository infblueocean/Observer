# Concurrency Review v2: Revised Implementation Plan

**Reviewer:** Grumpy Senior Concurrency Engineer (same guy)
**Date:** 2026-01-28
**Verdict:** CONDITIONALLY APPROVED

Well. Someone actually listened. That's rare.

The revised plan addresses most of my concerns by doing something radical: deleting the problematic code. No work pool means no work pool bugs. No event subscription system means no subscription bugs. Sometimes the best concurrency design is no concurrency design.

Let me go through my original criticisms point by point.

---

## 1. Original Concerns: Race Conditions

### 1.1 Work Pool Item Mutations - RESOLVED (by deletion)

**Original complaint:** Mutable `Item` fields accessed from multiple goroutines without synchronization.

**Status:** The entire work pool is gone. Items are now fetched in a single goroutine (Coordinator) and passed to the store. The Store has explicit `sync.RWMutex` protection. No more racing workers.

**Verdict:** Fixed.

### 1.2 FetchController's lastFetched Map - RESOLVED

**Original complaint:** Map concurrent access panic waiting to happen.

**Status:** The plan now explicitly shows (lines 377-378, 418-420):

```go
mu       sync.Mutex        // Protects lastFetch
lastFetch map[string]time.Time
```

With proper lock/unlock around access. This is correct.

**Verdict:** Fixed.

### 1.3 DedupIndex Concurrent Access - RESOLVED (by deletion)

**Original complaint:** HNSW graphs aren't thread-safe.

**Status:** From line 589: "No HNSW index in v0.5. For <10k items, brute-force cosine similarity is fast enough (~100ms)."

Brilliant. Delete the complex concurrent data structure. Use the simple thing that works.

**Verdict:** Fixed by avoidance.

### 1.4 Store Concurrent Access - RESOLVED

**Original complaint:** Multiple goroutines hitting Store without clear protection.

**Status:** Lines 64-67:

```go
type Store struct {
    db *sql.DB
    mu sync.RWMutex  // Protects all database operations
}
```

And lines 136-140 spell out the concurrency contract:
- Read methods acquire `mu.RLock()`
- Write methods acquire `mu.Lock()`
- All methods release via `defer`
- SQLite with WAL mode

This is textbook correct.

**Verdict:** Fixed.

### 1.5 Subscription Channel Races - RESOLVED (by deletion)

**Original complaint:** Custom pub/sub system had no synchronization specified.

**Status:** Line 211: "Messages are the ONLY event system. No custom pub/sub."

All events now go through Bubble Tea's message system, which is well-tested and single-threaded in its Update loop.

**Verdict:** Fixed by using a battle-tested library.

---

## 2. Original Concerns: Deadlocks

### 2.1 Filter Pipeline Holding Store Lock - RESOLVED

**Original complaint:** Complex lock ordering between filters, pool, and store.

**Status:** No more work pool. Filters are pure functions (lines 487-502):

```go
func ByAge(items []store.Item, maxAge time.Duration) []store.Item
func BySource(items []store.Item, sources []string) []store.Item
```

Filters take slices, return slices. No locks, no pool, no deadlock potential.

**Verdict:** Fixed.

### 2.2 Context Cancellation During Shutdown - RESOLVED

**Original complaint:** Wrong shutdown order, context not threaded properly.

**Status:** Lines 436-462 show correct shutdown:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
// ...
coord.Start(ctx, program)
program.Run()
cancel()       // Signal goroutines to stop
coord.Stop()   // Wait for completion
```

And the Coordinator uses `sync.WaitGroup` (lines 384, 393, 405):

```go
c.wg.Add(1)
go func() {
    defer c.wg.Done()
    // ...
}()

func (c *Coordinator) Stop() {
    close(c.stopCh)
    c.wg.Wait()  // Wait for goroutine to finish
}
```

This is the correct pattern.

**Verdict:** Fixed.

### 2.3 Circular Dependencies Between Controllers - RESOLVED (by deletion)

**Original complaint:** Multiple controllers with no lock ordering protocol.

**Status:** There's only one Coordinator now. It holds a single mutex for a single map. No circular dependencies possible.

**Verdict:** Fixed.

---

## 3. Original Concerns: Channel Semantics

### 3.1 Who Closes Channels? - RESOLVED

**Original complaint:** No specification for channel lifecycle.

**Status:** Lines 682-688 provide explicit channel contracts:

| Channel | Buffer Size | Creator | Closer | Backpressure |
|---------|-------------|---------|--------|--------------|
| `Coordinator.stopCh` | 0 (unbuffered) | `NewCoordinator` | `Stop()` | N/A - signal only |
| Bubble Tea internal | (managed by tea) | tea.Program | tea.Program | tea.Program handles |

And critically: "There are no custom event channels."

**Verdict:** Fixed.

### 3.2 Buffered vs Unbuffered - RESOLVED

**Original complaint:** No specification of buffer sizes or blocking behavior.

**Status:** There's only one channel (`stopCh`) and it's explicitly unbuffered. It's a close-only signal channel, which is correct.

**Verdict:** Fixed.

### 3.3 Goroutine Leaks in Subscriptions - RESOLVED

**Original complaint:** No unsubscribe mechanism.

**Status:** No subscriptions exist. One goroutine, one WaitGroup, clean shutdown.

**Verdict:** Fixed.

### 3.4 Event Channel in Controller - RESOLVED

**Original complaint:** Unknown buffer sizes, potential memory growth.

**Status:** No custom event channels. Bubble Tea handles its own message queue.

**Verdict:** Fixed.

---

## 4. Original Concerns: Resource Contention

### 4.1 Worker Count vs Resource Limits - RESOLVED (by simplification)

**Original complaint:** NumCPU workers don't account for different resource profiles.

**Status:** No worker pool. The Coordinator fetches sources sequentially (line 409: `for _, src := range c.sources`). This is actually correct for network-bound work - you don't want to hammer 16 RSS servers simultaneously anyway.

**Verdict:** Fixed.

### 4.2 Ollama as Singleton Resource - PARTIALLY ADDRESSED

**Original complaint:** Multiple workers hitting Ollama simultaneously.

**Status:** Embeddings are now optional (Phase 4) and there's no parallel embedding. However, the plan still doesn't specify Ollama concurrency limits.

**Remaining concern:** If `EmbedBatch` sends all texts to Ollama simultaneously, you could still overwhelm it.

**Verdict:** Acceptable for v0.5, but should specify in v0.6 if embedding volume increases.

### 4.3 SQLite Connection Contention - RESOLVED

**Original complaint:** No connection pool or transaction strategy.

**Status:** Single connection protected by RWMutex. For a single-user TUI app with modest write rates, this is fine. The plan specifies WAL mode and proper indexing.

**Verdict:** Fixed.

---

## 5. Original Concerns: Context Handling

### 5.1 Context Not Threaded Through Store Operations - NOT ADDRESSED

**Original complaint:** Store methods lack context parameters.

**Status:** The Store interface still has no context:

```go
func (s *Store) SaveItems(items []Item) (int, error)
func (s *Store) GetItems(limit int, includeRead bool) ([]Item, error)
```

**Why this matters less now:** With a single Coordinator goroutine and simple queries, a 30-second SQLite query is unlikely. If the user hits Ctrl+C during a write, the RWMutex ensures consistency. SQLite itself handles incomplete transactions.

**Verdict:** Still a minor wart, but acceptable for v0.5.

### 5.2 Pipeline Cancellation - RESOLVED (by simplification)

**Original complaint:** Cancellation only between filter stages.

**Status:** Filters are now pure functions operating on in-memory slices. They complete in milliseconds. No need for mid-filter cancellation.

**Verdict:** Fixed.

### 5.3 Work Pool Context Propagation - RESOLVED (by deletion)

**Original complaint:** Unclear context propagation in pool.

**Status:** No pool. The Coordinator respects `ctx.Done()` in its select loop (line 392).

**Verdict:** Fixed.

### 5.4 FetchController Context Leaks - RESOLVED

**Original complaint:** Goroutine leak if context cancelled but Stop not called.

**Status:** The Coordinator checks BOTH `ctx.Done()` and `stopCh`:

```go
select {
case <-ctx.Done():
    return
case <-c.stopCh:
    return
case <-ticker.C:
    c.fetchAll(ctx, program)
}
```

**Verdict:** Fixed.

---

## 6. Original Concerns: Additional Issues

### 6.1 Priority Queue Thread Safety - RESOLVED (by deletion)

**Original complaint:** `container/heap` not thread-safe.

**Status:** No priority queue. No work pool. No problem.

**Verdict:** Fixed.

### 6.2 Ring Buffer Concurrent Access - RESOLVED (by deletion)

**Original complaint:** Ring buffer for history has tricky access patterns.

**Status:** Line 751: "Ring buffer for history: Deleted. Use logging."

**Verdict:** Fixed.

### 6.3 No WaitGroup for Graceful Shutdown - RESOLVED

**Original complaint:** No way to wait for in-flight work.

**Status:** Coordinator explicitly uses `sync.WaitGroup` (lines 384, 393, 405).

**Verdict:** Fixed.

---

## NEW Concerns in v2

The revised plan is vastly simpler, but I have a few new observations:

### N1. Single Coordinator Goroutine is a Bottleneck

The Coordinator fetches sources sequentially (line 409-423):

```go
for _, src := range c.sources {
    items, err := c.fetcher.Fetch(ctx, src)
    // ...
}
```

If you have 20 sources and each takes 2 seconds, that's 40 seconds per fetch cycle. This is probably fine for 5-10 sources but will become painful as sources grow.

**Recommendation for v0.6:** Use `errgroup` to fetch sources in parallel with bounded concurrency.

**Severity:** Low. Correctness > performance for v0.5.

### N2. Bubble Tea Send from Goroutine

Line 422: `program.Send(ui.FetchComplete{...})`

This is called from the Coordinator goroutine. Bubble Tea's `program.Send()` IS thread-safe (it uses an internal channel), so this is correct.

**Verdict:** No issue. Just noting I checked.

### N3. No Timeout on Individual Fetches

The Fetcher uses `http.Client` with a timeout (line 172: `NewFetcher(timeout time.Duration)`), but the overall `fetchAll` loop doesn't have a per-iteration timeout.

If a feed hangs at the TCP level (before timeout kicks in), you could be stuck.

**Recommendation:** Wrap each `Fetch` call in a context with timeout:

```go
fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
items, err := c.fetcher.Fetch(fetchCtx, src)
cancel()
```

**Severity:** Low. The HTTP client timeout should catch most cases.

### N4. Missing Concurrency Test for Coordinator

Line 659-660 shows a concurrent store test, but there's no test for Coordinator's `lastFetch` map under concurrent access.

Granted, the current design only has one goroutine accessing `lastFetch`, so races are impossible. But a test would catch future regressions.

**Severity:** Very low. The design makes races impossible.

---

## Summary of Required Changes from v1

| Item | Status |
|------|--------|
| Synchronization for every shared structure | DONE - Table at lines 676-680 |
| Channel contracts | DONE - Table at lines 682-688, plus deletion of custom channels |
| Lock ordering protocol | N/A - Only one lock now |
| Context threading | PARTIALLY - Store lacks context but acceptable for v0.5 |
| Resource limits | SIMPLIFIED - Sequential fetch, no parallelism to limit |
| Shutdown protocol | DONE - Correct order, WaitGroup usage |

---

## Verdict: CONDITIONALLY APPROVED

The revised plan passes concurrency review for v0.5. The authors took the best possible approach: delete complexity until the concurrency model becomes trivial.

**Conditions for approval:**

1. Run `go test -race ./...` in CI (already mentioned at line 660)
2. Add context with timeout to individual fetch operations (minor)
3. Document the sequential fetch limitation for future optimization

**For v0.6:**

- Add context to Store methods
- Consider `errgroup` for parallel fetching with bounded concurrency
- If embeddings see heavy use, add Ollama concurrency limits

---

*"The best concurrent code is no concurrent code. The second best is obviously correct concurrent code. This plan achieves both."*

---

**Comparison to v1:**

| Metric | v1 | v2 |
|--------|----|----|
| Mutexes | 0 specified | 2 (Store.mu, Coordinator.mu) |
| Channels | Multiple unspecified | 1 (stopCh, well-specified) |
| Goroutines | Unknown (worker pool) | 2 (main + coordinator) |
| Potential deadlocks | 3+ | 0 |
| Potential races | 5+ | 0 |
| WaitGroups | 0 | 1 |
| Concurrency bugs I expect | Many | Zero |

That's how you address a review.
