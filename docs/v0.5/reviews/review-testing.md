# Testing Strategy Review: v0.5 Implementation Plan

**Reviewer:** Senior QA Engineer
**Date:** 2026-01-28
**Status:** CRITICAL ISSUES IDENTIFIED

---

## Executive Summary

The v0.5 implementation plan has significant gaps in its testing strategy. While Section 1.4 mentions "unit tests" and "integration tests," the actual testing approach is dangerously underspecified. The plan appears to assume that if individual units work, the system will work. This is the kind of optimism that leads to 3 AM production incidents.

**Overall Grade: D+**

The plan gets credit for mentioning testing exists, but fails to address the hard problems: external dependencies, concurrency, timing-sensitive operations, and failure modes.

---

## Section-by-Section Analysis

### Phase 1: Foundation (Section 1.4)

**What the plan says:**
> - Unit tests for priority queue (heap operations, ordering)
> - Unit tests for ring buffer (push, overflow, retrieval order)
> - Unit tests for work pool (submit, complete, cancel, stats)
> - Unit tests for store (CRUD operations, transactions)
> - Integration test: work pool with multiple concurrent workers

**Missing Test Cases:**

1. **Priority Queue Edge Cases Not Covered:**
   - What happens when two items have identical priority AND identical `CreatedAt` timestamps? The plan assumes FIFO based on time, but time resolution is finite. The archived `work_test.go` uses `time.Millisecond` gaps - that's not guaranteed on all platforms.
   - What happens when `heap.Pop` is called on an empty queue? The plan doesn't specify defensive behavior.
   - What about integer overflow in priority values? The plan uses `int` for priority (lines 67-74) but doesn't document bounds.

2. **Ring Buffer Edge Cases:**
   - Zero-capacity ring buffer - should this panic, return nil, or default?
   - Concurrent push/read operations - the plan doesn't mention thread safety for the ring buffer.
   - Memory behavior with very large items - ring buffer holds `*Item` pointers but what if `Item.Data` is huge?

3. **Work Pool Concurrency Gaps:**
   - Race conditions: The plan says "Signal-based work dispatch (non-blocking channel)" (line 121) but doesn't test channel buffer overflow.
   - Worker starvation: What if all workers block on external calls?
   - Panic recovery: What happens when a submitted function panics? The archived tests don't cover this.
   - Subscription leak: The plan has `Subscribe() <-chan Event` (line 112) but no `Unsubscribe()`. What happens if subscribers never drain their channels?

4. **Store Tests Missing:**
   - Concurrent writes from multiple goroutines (SQLite locking behavior)
   - Database corruption recovery
   - Schema migration testing (plan shows schema at lines 186-205 but no version migration strategy)
   - Disk full conditions
   - Invalid embedding data (malformed blobs)
   - Unicode edge cases in titles/summaries

**Flaky Test Potential (HIGH RISK):**

The archived `work_test.go` (lines 199-281) already shows flaky test patterns:
```go
time.Sleep(50 * time.Millisecond)
if pool.PendingCount() != 3 {
    t.Fatalf("expected 3 pending items, got %d", pool.PendingCount())
}
```

This is a race condition waiting to happen. The test assumes items are queued within 50ms, but under load this can fail. The timeout-based tests at lines 126-130, 158-160, and 265-266 use 2-second timeouts, which will cause CI slowdowns and intermittent failures.

**Recommendation:** Use channels and synchronization primitives instead of `time.Sleep`. Implement a test helper that waits for specific conditions with bounded retries.

---

### Phase 2: Controller Core (Section 2.6)

**What the plan says:**
> - Pipeline chains filters correctly
> - Context cancellation propagates
> - All basic filters pass tests
> - MainFeedController refreshes correctly

**Missing Test Cases:**

1. **Pipeline Edge Cases:**
   - Empty pipeline (no filters added) - what happens when `Run()` is called?
   - Filter returns `nil` slice vs empty slice - are these handled differently?
   - Filter modifies input slice in-place vs returning new slice - plan doesn't specify contract
   - Filter returns more items than it received (is this allowed?)

2. **Filter Interface Problems (Section 2.1, lines 233-247):**
   The `Filter.Run()` signature is:
   ```go
   Run(ctx context.Context, items []model.Item, pool *work.Pool) ([]model.Item, error)
   ```

   **Question:** Why does every filter get a `*work.Pool`? Only async filters need it. This creates a testing burden where every filter test needs a mock pool or nil-check.

3. **Context Cancellation Testing:**
   The archived `filters_test.go` shows a test for context cancellation (lines 53-64), but it only checks that the error is `context.Canceled`. It doesn't verify:
   - Partial results are not returned
   - In-progress work is properly cleaned up
   - Downstream filters are not invoked after cancellation

4. **MainFeedController (Section 2.5):**
   - No test for what happens when the store is unavailable
   - No test for refresh during an ongoing refresh (reentrancy)
   - Event subscription cleanup not tested

**Mock Strategy Problems:**

The plan's filter interface at line 237 takes `*work.Pool` directly, not an interface. This means tests either need:
- A real work pool (heavyweight, flaky)
- Nil (which may cause panics in filters that use it)
- A mock that requires modifying the production code

**Recommendation:** Change `*work.Pool` to `WorkPool interface` in the filter signature.

---

### Phase 3: View Layer (Section 3.6)

**What the plan says:**
> - Stream view renders items with time bands
> - Navigation works correctly
> - Work view shows pool state
> - Mode switching works

**This is embarassingly thin for a TUI application.**

**Missing Test Cases:**

1. **Bubble Tea Testing Strategy Completely Absent:**
   - How will view models be tested? Bubble Tea's `tea.Model` is an interface, but the plan doesn't show how messages will be simulated.
   - No mention of golden file testing for rendered output.
   - No mention of testing terminal size edge cases (0 width, 0 height, very large).

2. **Time Band Logic:**
   - Timezone handling - if system timezone changes during runtime, do bands update?
   - DST transitions - items near midnight during daylight saving time changes.
   - Clock skew - what if `item.Published` is in the future?

3. **Navigation Edge Cases:**
   - Empty list navigation (j/k on zero items)
   - Cursor position after item deletion
   - Cursor position after item insertion above cursor
   - Viewport scroll position when window resizes

4. **Rendering Performance:**
   - No tests for render time with 10,000+ items
   - No tests for memory usage during rendering

**Recommendation:** Adopt a Bubble Tea testing pattern using `tea.NewProgram` with `tea.WithInput/tea.WithOutput` for capturing/injecting. Create a test harness that can send messages and capture the rendered output.

---

### Phase 4: Intake Pipeline (Section 4.5)

**What the plan says:**
> - Intake pipeline embeds and stores items
> - Dedup correctly identifies similar items
> - FetchController periodically fetches
> - Integration with work pool works

**Missing Test Cases:**

1. **Embedder Testing (Section 4.2):**
   - The `Embedder` interface (lines 460-466) has an `Available()` method, but no test strategy for unavailable embedders.
   - `EmbedBatch` returns `[][]float64` - what if some items fail and others succeed? Does it return partial results? Errors for each item?
   - Empty string embedding behavior
   - Very long text embedding (what's the limit? Does Ollama have a context window for embeddings?)
   - Unicode/emoji in text - does embedding preserve semantic meaning?

2. **Dedup Index (Section 4.3):**
   - HNSW is probabilistic - how will tests account for approximate results?
   - Threshold tuning tests (0.85 mentioned at line 457, but what about 0.84 vs 0.86 boundary cases?)
   - Index persistence and reload - the plan doesn't mention persisting the HNSW index.
   - Index corruption recovery
   - Memory usage with 100k+ items

3. **FetchController (Section 4.4):**
   - Network timeout handling
   - Retry logic testing
   - Rate limiting per source
   - Handling of sources that return errors repeatedly
   - Graceful degradation when all sources fail

4. **Integration Gaps:**
   The archived `intake_test.go` has this concerning line:
   ```go
   //go:build ignore
   ```

   This means the intake tests ARE NOT RUNNING. The plan doesn't address this.

**Flaky Test Potential (HIGH RISK):**

The `FetchController` involves:
- Network I/O (inherently flaky)
- Time-based scheduling (inherently flaky)
- Multiple goroutines (inherently racy)

**Recommendation:** Create comprehensive mock interfaces for all external services. The plan shows concrete types in signatures (e.g., `*work.Pool`, not interfaces), which makes mocking difficult.

---

### Phase 5: Advanced Filters (Section 5.4)

**What the plan says:**
> - EmbeddingFilter filters by similarity
> - RerankFilter uses ML reranking as pass/fail
> - Full pipeline reduces items at each stage
> - Performance acceptable (< 1s typical refresh)

**Critical Missing Test Cases:**

1. **EmbeddingFilter (Section 5.1):**
   - Time decay formula verification: `score = cosineSim * 0.5^(age/halfLife)` (line 527)
   - What happens with zero-vector embeddings?
   - What happens with NaN/Inf in embedding values?
   - Items without embeddings - are they filtered or passed through?
   - Anchor embedding source - where does it come from? How is it tested?

2. **RerankFilter (Section 5.2):**
   - Reranker unavailable - graceful degradation?
   - Reranker timeout during batch
   - Partial batch failure
   - Score distribution edge cases (all items score 0, all items score 10)

3. **Performance Testing Strategy is Vague:**
   The plan says "Performance acceptable (< 1s typical refresh)" but doesn't specify:
   - What is "typical"? 100 items? 10,000?
   - Where is this measured? CI? Local development?
   - What percentile? p50? p95? p99?
   - What hardware baseline?

**Recommendation:** Define concrete performance benchmarks with specific item counts, hardware specs, and percentile requirements. Use Go's `testing.B` for reproducible benchmarks.

---

### Phase 6: Integration & Polish (Section 6.3)

**What the plan says:**
> - Application starts and runs
> - Full data flow works end-to-end
> - All tests pass
> - Documentation complete

**This is not a testing strategy. This is wishful thinking.**

**Missing Integration Tests:**

1. **End-to-End Scenarios:**
   - Fresh startup with empty database
   - Startup with corrupted database
   - Startup with very large database (100k+ items)
   - Graceful shutdown during active work
   - Hard kill (SIGKILL) and restart
   - Long-running operation (24+ hours)

2. **Component Integration:**
   - Work pool backpressure affecting fetch controller
   - Store writes blocking view updates
   - Memory growth over time
   - Goroutine leak detection

3. **External Dependency Failures:**
   - Ollama crashes mid-request
   - SQLite file locked by another process
   - Network partition during fetch
   - Disk quota exceeded during write

---

## Interface Mockability Analysis

The plan has several interfaces that are NOT properly mockable:

| Interface/Type | Problem | Severity |
|---------------|---------|----------|
| `*work.Pool` | Concrete type, not interface | HIGH |
| `*model.Store` | Concrete type, not interface | HIGH |
| Filter takes `*work.Pool` | Forces pool in all filter tests | MEDIUM |
| No `Embedder` mock shown | Tests need Ollama running | HIGH |
| No network mock for fetching | Tests need internet | HIGH |

The archived tests show the pattern problem - `intake_test.go` creates real stores and the tests are disabled (`//go:build ignore`).

**Recommendation:** Define interfaces for all external dependencies:
```go
type Store interface { ... }
type WorkPool interface { ... }
type Embedder interface { ... }  // Already exists, good
type Fetcher interface { ... }
```

---

## Flaky Test Risk Assessment

| Component | Risk | Cause | Mitigation |
|-----------|------|-------|------------|
| Work Pool | HIGH | time.Sleep based synchronization | Use proper sync primitives |
| Priority Queue | MEDIUM | Timestamp resolution | Use sequence numbers for tiebreaking |
| Fetch Controller | HIGH | Network I/O | Mock HTTP transport |
| Embedder | HIGH | Ollama availability | Mock embedder for unit tests |
| Time Band UI | MEDIUM | Time-sensitive logic | Inject clock interface |
| View Rendering | MEDIUM | Terminal size assumptions | Test with fixed dimensions |

---

## Recommended Testing Architecture

The plan should include:

1. **Test Fixtures Package** (`internal/testutil/`)
   - Mock implementations for all interfaces
   - Test data generators
   - Time manipulation utilities

2. **Integration Test Suite** (`test/integration/`)
   - Docker Compose for Ollama
   - Database fixtures
   - End-to-end scenarios

3. **Benchmark Suite** (`internal/benchmark/`)
   - Pipeline throughput
   - Memory profiling
   - Goroutine leak detection

4. **Golden File Tests** (`testdata/`)
   - UI rendering snapshots
   - SQL query results

5. **Chaos Testing**
   - Random failure injection
   - Network partition simulation
   - Slow dependency simulation

---

## Critical Omissions

1. **No test for the main() function** (Section 6.1)
   - Application entry point is untested
   - Initialization order bugs won't be caught

2. **No regression test strategy**
   - How will regressions from v0.4 be detected?
   - No mention of comparing behavior with archived code

3. **No load testing**
   - What happens with 50 sources publishing simultaneously?
   - What happens with 100k items in the view?

4. **No security testing**
   - SQL injection in search/filter inputs
   - Malformed RSS/feed data handling
   - Resource exhaustion attacks (huge items, infinite feeds)

5. **No accessibility testing**
   - Screen reader compatibility
   - Color contrast for colorblind users
   - Keyboard-only navigation

---

## Verdict

The testing strategy in this plan is insufficient for a production application. The plan acknowledges the need for testing (which is more than some plans do), but fails to address:

1. The hard problems of testing concurrent code
2. External dependency mocking
3. Performance benchmarking methodology
4. Failure mode coverage
5. Integration test architecture

Before implementation begins, the plan needs:
- A dedicated testing architecture section
- Interface definitions that enable mocking
- Specific benchmark targets with measurement methodology
- Failure scenario enumeration for each component
- CI pipeline specification including test categorization (unit/integration/e2e)

**Do not ship this plan without addressing these issues.**

---

*"Every line of code you write is a liability. Every test you don't write is a timebomb." - Senior QA Engineer, 15 years of battle scars*
