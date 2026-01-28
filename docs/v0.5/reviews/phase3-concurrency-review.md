# Phase 3 Concurrency Review

**Reviewer:** Grumpy Senior Concurrency Engineer
**Verdict:** ACCEPTABLE (minor issues)

## Goroutine Lifecycle

**GOOD:** Single goroutine with `wg.Add(1)` before spawn (L57) and `defer wg.Done()` (L59). Clean pattern.

**GOOD:** `Wait()` method (L81-83) allows callers to block until shutdown completes.

## Context Handling

**GOOD:** Context cancellation checked in loop (L70) and before each fetch (L91).

**GOOD:** Per-fetch timeout context (L96) with immediate `cancel()` call (L100).

## Race Conditions

**NONE FOUND.** Sources slice is copied at construction (L44-45) and never mutated. Store operations are assumed thread-safe. `program.Send()` is documented as goroutine-safe by Bubble Tea.

## Potential Issues

### Issue 1: main.go Double-Cancel (L88)

```go
defer cancel()  // L21
// ...
cancel()        // L88
coordinator.Wait()
```

The `cancel()` is called explicitly then again via `defer`. Harmless (context cancel is idempotent) but sloppy. The explicit call is actually necessary for correct shutdown order.

**Severity:** Cosmetic
**Fix:** Remove `defer cancel()` since explicit call is required for ordering.

### Issue 2: No Shutdown Timeout

If `program.Send()` blocks (e.g., Bubble Tea is stuck), the goroutine could hang indefinitely during shutdown. `coordinator.Wait()` (L89) would block forever.

**Severity:** Low (unlikely in practice)
**Fix:** Add timeout to Wait: `time.AfterFunc(5*time.Second, cancel)` pattern or `select` with deadline.

### Issue 3: Store Access During Shutdown

`fetchAll` may call `store.SaveItems()` (L105) while main is about to call `st.Close()` (L34). The `coordinator.Wait()` before implicit `defer st.Close()` prevents this, but the ordering is implicit and fragile.

**Severity:** Low (currently safe)
**Recommendation:** Document the shutdown order dependency.

## Test Coverage

Tests cover: context cancellation, timeout, start/wait lifecycle, source immutability. Race detector would catch any issues with `-race` flag.

## Verdict

No data races. No deadlocks. Shutdown sequence is correct but fragile. Ship it.
