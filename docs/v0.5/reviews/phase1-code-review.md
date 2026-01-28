# Phase 1 Code Review

**Reviewer:** Grumpy Senior Go Engineer
**Date:** 2026-01-28

## Summary

Implementation matches plan. A few nits, one actual bug.

## Issues

### store.go

**Line 47-48: Unwrapped errors.** `sql.Open` error has no context.
```go
// Bad
return nil, err
// Better
return nil, fmt.Errorf("open database: %w", err)
```
Same at lines 58, 65, 73.

**Line 176: `[]interface{}` is deprecated.** Use `[]any`.

**Line 247: Pre-allocate slice.** `queryItems` appends to nil slice. Minor, but:
```go
items := make([]Item, 0, 16) // reasonable default
```

**Line 103: Missing error wrap in `createTables`.** Silent failure context.

### store_test.go

**Lines 10-21: Interface in test file.** This is correct per plan. Good.

**Lines 328, 340, 371, 395: Ignoring errors.** `got, _ := st.GetItems(...)` is sloppy. Tests should check all errors.

### fetcher.go

**Line 70: Redundant info in error.** `resp.Status` already includes the code.
```go
// Current: "HTTP error: 404 404 Not Found"
// Better:  "HTTP error: status 404"
```

**Line 161-168: Truncate counts bytes, not runes.** Will mangle UTF-8:
```go
s := "Hello \xf0\x9f\x91\x8b" // "Hello wave emoji"
truncate(s, 8) // Slices mid-emoji
```
Fix: use `[]rune` conversion.

**Line 82: Pre-allocate is good.** Nice.

### fetcher_test.go

**Lines 73, 139, 197, etc.: Unchecked `w.Write`.** Linter will complain:
```go
_, _ = w.Write([]byte(sampleRSS)) // acknowledge intentionally ignored
```

**Line 312: Brittle assertion.** Exact error message match will break:
```go
if !strings.Contains(err.Error(), "404") {
```

## Plan Compliance

| Requirement | Status |
|-------------|--------|
| Store schema matches plan | Yes |
| Store thread-safety via mutex | Yes |
| WAL mode for file DBs | Yes |
| Fetcher accepts context | Yes |
| Fetcher uses gofeed | Yes |
| No interfaces in prod code | Yes |
| Interface in test file only | Yes |
| Tests cover duplicates, concurrency | Yes |

## Verdict

**Ship it.** The UTF-8 truncation is the only real bug. The unwrapped errors are annoying but harmless. Fix before Phase 2.
