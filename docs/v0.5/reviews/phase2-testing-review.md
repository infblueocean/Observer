# Phase 2 Testing Review

**Reviewer:** Senior QA Engineer
**Date:** 2026-01-28
**Verdict:** Acceptable with reservations

## filter_test.go

### What's Good
- `TestByAgeEmpty` and `TestBySourceEmpty` properly test nil vs empty slice. Someone actually thought about this.
- `TestLimitPerSourceZeroLimit` catches negative and zero limits. Rare to see.
- `TestDedupEmptyURL` validates empty URLs don't cause false deduplication.

### Coverage Gaps

1. **`TestByAge` uses `time.Now()`** - Not flaky today, but will bite you eventually. Use a fixed reference time.

2. **Missing `ByAge` boundary test** - What happens when `Published` equals exactly `now - maxAge`? Off-by-one waiting to happen.

3. **`TestBySource` lacks case sensitivity test** - Does `"TechNews"` match `"technews"`? Untested behavior.

4. **`TestDedup` missing: single-item input** - Edge case not covered.

5. **`TestNormalizeTitle` prefixes are incomplete** - Tests `BREAKING`, `UPDATE`, `EXCLUSIVE`. What about `JUST IN:`, `ALERT:`, `WATCH:`? Either document the full list or test exhaustively.

6. **No test for `LimitPerSource` with all items from one source** - Only tests mixed sources.

## app_test.go

### What's Good
- Function injection pattern is used correctly. `mockCmd` is clean.
- `TestAppMarkReadEmpty` covers the nil-items case.
- Navigation bounds testing is thorough.

### Coverage Gaps

1. **`TestAppView` is weak** - Asserts `view != ""`. That's it. No content validation. A view returning `"CRASH"` would pass.

2. **`TestAppCursorResetOnItemsLoaded`** - Tests cursor=10 with 2 items. Does NOT test cursor=0 with 0 items (empty reload case).

3. **Missing: `triggerFetch` callback never tested** - The mock has `triggerFetch`, it's passed to `NewApp`, but NO test invokes it. What key triggers fetch? Untested.

4. **`TestAppItemMarkedRead` lacks ID-not-found test** - What if `ItemMarkedRead{ID: "nonexistent"}` arrives? Silently ignored? Panic? Unknown.

5. **No `View()` test with many items** - Pagination/scrolling behavior? Viewport limits? All untested.

6. **`TestAppRefresh` doesn't verify items actually reload** - Only checks command returned. Doesn't execute it.

## Flaky Test Risk

- **Low** - No goroutines, no real I/O, no sleeps. The `time.Now()` calls are the only concern.

## Summary

| Area | Score |
|------|-------|
| Edge cases | 6/10 |
| Assertions quality | 5/10 |
| Function injection | 8/10 |
| Flaky risk | 9/10 |

The tests prove the happy path works. They don't prove the sad paths don't crash. Ship it, but add the missing edge cases before Phase 3.
