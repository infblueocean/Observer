# Phase 1 Concurrency Review

**Reviewer:** Grumpy Senior Concurrency Engineer
**Date:** 2026-01-28
**Files:** `internal/store/store.go`, `internal/store/store_test.go`

## Issues Found

### 1. Lock Not Held During `queryItems` (CRITICAL)

**Lines 240-277:** `queryItems()` is called by `GetItems` (L199) and `GetItemsSince` (L216) while holding RLock, but `queryItems` itself holds no lock. If someone adds a new method calling `queryItems` without the lock, we have a race.

**Fix:** Either document "caller must hold lock" or move lock acquisition into `queryItems`.

### 2. `Close()` Has No Lock (MEDIUM)

**Line 108-110:** `Close()` doesn't acquire any lock. If called while another goroutine holds mu and is mid-query, `db.Close()` may corrupt in-flight operations.

**Fix:** Acquire write lock in `Close()`.

### 3. `createTables()` Called Without Lock (LOW)

**Line 73:** Called during `Open()` before Store is returned, so technically safe. But inconsistent with the "all methods are safe" promise in the comment (L13).

### 4. Test Uses `t.Errorf` From Goroutine (BUG)

**Lines 427, 439:** Using `t.Errorf` from goroutines is a data race. The `testing.T` methods are not goroutine-safe for failure reporting.

**Fix:** Collect errors in a channel or use `t.Error` only from the main goroutine.

### 5. Concurrent Test Doesn't Actually Stress Contention (WEAK)

**Lines 401-465:** All goroutines start, do one operation, and exit. Real contention needs loops. Also, no verification that readers actually see consistent data during writes.

**Fix:** Add iteration loops and actual consistency checks.

## What's Correct

- RWMutex usage pattern is correct (RLock for reads, Lock for writes)
- No lock ordering issues (single mutex, no nested locks)
- SQLite connection with `SetMaxOpenConns(1)` for `:memory:` is correct
- WAL mode for file-based DBs is appropriate

## Verdict

Mostly fine for a simple store. Fix the `Close()` locking and the test goroutine bug. The `queryItems` issue is low-risk but document the invariant.
