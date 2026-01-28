# Concurrency Review v3: Final Review

**Reviewer:** Grumpy Senior Concurrency Engineer (third time's the charm)
**Date:** 2026-01-28
**Verdict:** APPROVED - SHIP IT

---

## Outstanding Concerns from v2 - Status Check

### 1. Sequential Fetch Bottleneck (N1)

**My complaint:** 20 sources at 2 seconds each = 40 seconds per cycle.

**Status:** Unchanged, but documented. The plan explicitly notes this is acceptable for v0.5 and suggests `errgroup` for v0.6.

**Verdict:** Fine. Document it, defer it, move on. Correct decision for a first release.

### 2. No Timeout on Individual Fetches (N3)

**My complaint:** If a feed hangs, you're stuck.

**Status:** FIXED. Lines 462-465:
```go
fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
items, err := c.fetcher.Fetch(fetchCtx, src)
cancel()
```

**Verdict:** This is exactly what I asked for.

### 3. Missing Concurrency Test for Coordinator (N4)

**My complaint:** No test for the `lastFetch` map under concurrent access.

**Status:** They deleted `lastFetch` entirely (line 8). Can't have a race on a map that doesn't exist.

They also added a full testing strategy (lines 484-608) with mock fetcher injection. Tests cover:
- `TestCoordinatorFetchesAllSources`
- `TestCoordinatorRespectsContextCancellation`
- `TestCoordinatorHandlesFetchTimeout`

**Verdict:** FIXED, and then some.

### 4. Store Lacks Context Parameters

**My complaint:** Store methods should accept context for cancellation.

**Status:** Still not addressed. Methods remain context-free.

**Why I'm letting it go:** Single-user TUI. Sub-second queries. RWMutex ensures consistency. The risk of a 30-second SQLite query blocking shutdown is near zero. Add it in v0.6 when there's a real need.

**Verdict:** Acceptable technical debt.

---

## New Observations

### Coordinator Simplified

The revised Coordinator (lines 406-475) is even simpler than v2. They removed `stopCh` entirely - context cancellation is now the ONLY stop mechanism. One less thing to get wrong.

**Good call.**

### Sources Immutability

Explicitly documented at line 411 and the constructor copies the slice. No more guessing about thread safety of the sources list.

### Concurrency Table

Lines 735-739 provide a clear summary of every shared structure with its protection mechanism. This is the kind of documentation that prevents future engineers from adding races.

---

## Final Checklist

| Concern | Status |
|---------|--------|
| Race conditions | None possible - verified |
| Deadlocks | Impossible with single-mutex design |
| Goroutine leaks | Context + WaitGroup + Wait() = clean shutdown |
| Channel contracts | Only Bubble Tea's internal channels |
| Individual fetch timeouts | 30-second context.WithTimeout |
| Shutdown order | Documented at lines 643-646 |
| Tests with -race | Required in CI |

---

## Verdict: APPROVED

Ship it.

The codebase has exactly two goroutines (main + coordinator), one mutex (Store.mu), and zero custom channels. The concurrency model is so simple that a junior engineer could understand it.

I have no remaining concerns that block shipping v0.5.

*"Sometimes the best code review is boring. This one's boring. Well done."*
