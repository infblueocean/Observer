# Phase 3 Code Review

**Reviewer:** Grumpy Senior Go Engineer
**Date:** 2026-01-28

## Summary

Implementation is **solid**. A few nits, one real bug in main.go.

## Issues

### BUG: main.go:24 - Silent error swallowing

```go
homeDir, _ := os.UserHomeDir()
```

If this fails, you'll create `.observer` in the current directory. Either handle it or `log.Fatal`.

### ISSUE: main.go:26 - Ignoring MkdirAll error

```go
os.MkdirAll(dataDir, 0755)
```

If directory creation fails, `store.Open` will fail anyway. Handle the error explicitly.

### NIT: coordinator.go:105 - Silent SaveItems error

```go
newItems, _ = c.store.SaveItems(items)
```

You're dropping the error. At minimum, include it in the `FetchComplete` message so the UI knows about partial failures.

## Go Idioms

**Good:**
- Interface defined where used (line 23-25) - textbook Go
- Defensive copy of sources slice (line 44-45)
- Proper context timeout cleanup (line 100)
- `sync.WaitGroup` used correctly for goroutine lifecycle

**Minor nits:**
- `fetchInterval` and `fetchTimeout` could be configurable via constructor, but constants are fine for now

## Concurrency

**Correct.** No races. The pattern is clean:
- Immutable sources (copied at construction)
- No shared mutable state
- Context cancellation is single stop mechanism
- `wg.Wait()` synchronizes shutdown

The check at line 91 (`if ctx.Err() != nil`) before each fetch is good.

## Shutdown Sequence

main.go lines 87-89:
```go
cancel()
coordinator.Wait()
```

**Correct.** Context cancels, then Wait ensures goroutine exits before `st.Close()` (deferred).

## Tests

**Coverage is good.** Tests hit all the important scenarios:
- All sources fetched (line 52)
- Context cancellation (line 86)
- Timeout handling (line 135)
- Item persistence (line 168)
- Start/Wait lifecycle (line 222)
- Source immutability (line 270)
- Error continuity (line 386)

**Missing:** No test that verifies `program.Send()` actually receives correct messages (uses nil program). The plan (line 582-593) shows a mockTeaProgram approach. Consider adding.

## Plan Compliance

| Requirement | Status |
|-------------|--------|
| 5-minute interval | PASS (line 17) |
| 30-second timeout | PASS (line 20) |
| Immutable sources | PASS (line 32, 44-45) |
| Context cancellation only | PASS |
| wg.Wait() for shutdown | PASS |
| Interface for testing | PASS (lines 23-25) |

**Minor deviation:** Plan shows `FetchComplete` sent on error with `continue`, actual impl sends it for both success and error (lines 109-115). This is arguably better.

## Verdict

**APPROVE with minor fixes.** Handle the errors in main.go. The coordinator itself is clean.
