# Phase 1 Test Coverage Review

**Reviewer:** Grumpy Senior QA
**Date:** 2026-01-28

## store_test.go

### Coverage Gaps
- **No test for `MarkRead` on non-existent ID** - silently succeeds or fails?
- **No test for `MarkSaved` on non-existent ID** - same issue
- **GetItems with limit=0** - undefined behavior, untested
- **Negative limit values** - will they crash or return all?
- **GetItemsSince boundary condition** - what about items AT exactly `since` time?
- **Close() behavior** - no test for double-close or operations after close

### Test Quality Issues
- `TestConcurrentAccess` (line 450-451): Silently ignores MarkRead/MarkSaved errors with comment "which is OK" - no, it's not. You're hiding potential bugs.
- `TestGetItemsEmpty` (line 494-497): Comment says "nil slice is OK" but doesn't actually verify the behavior is intentional. This is documentation, not a test.

### Flaky Test Potential
- `TestConcurrentAccess`: No deterministic verification. 10 writers racing; test only checks final count. Could mask intermittent failures. Race detector helps but isn't a substitute for proper assertions.

### Missing Error Path Tests
- What happens when SQLite is corrupted?
- Disk full during write?
- Schema migration failures on Open()?

---

## fetcher_test.go

### Coverage Gaps
- **No test for unreachable host** - DNS failure, connection refused
- **No test for redirect handling** - 301/302 responses
- **No test for extremely large responses** - memory exhaustion?
- **No test for slow trickle responses** - server sends 1 byte/second
- **Missing pubDate in RSS item** - how is Published set?
- **Missing link in RSS item** - URL field handling?
- **Missing guid in RSS item** - ID generation fallback?

### Test Quality Issues
- `TestFetchHTTPError404` (line 312): Hardcodes exact error message string. Fragile. Should use `strings.Contains` or error type checking.
- `TestFetchHTTPError500` (line 342): Same problem.
- `TestFetchSetsCorrectFetchedTime`: Uses wall clock comparison - could flake on slow CI.

### Test Isolation
- All tests properly use `httptest.Server` with deferred Close - good.
- Each test creates fresh Fetcher - no shared state - good.

### Flaky Test Potential
- `TestFetchTimeout`: 100ms timeout is aggressive. Could flake on loaded systems.
- `TestFetchSetsCorrectFetchedTime`: Wall clock race between beforeFetch/afterFetch.

---

## Summary

Tests cover the happy path adequately. Error handling and edge cases are Swiss cheese. The concurrent test is security theater. Fix the timeout-dependent tests before they haunt you in CI at 3am.
