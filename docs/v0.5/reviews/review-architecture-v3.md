# Architectural Review: Observer v0.5 Implementation Plan v3 (Final)

**Reviewer:** Senior Engineer (third time's the charm)
**Date:** 2026-01-28
**Previous Verdict:** "Ship it" with 4 recommended actions
**Current Verdict:** Ship it. No blockers.

---

## My v2 Concerns: Did They Fix Them?

| Concern | Status | Notes |
|---------|--------|-------|
| Function injection signature drift (AppDeps struct) | **ACKNOWLEDGED** | Plan says "consider AppDeps struct if > 5". Acceptable. |
| Coordinator had two stop mechanisms | **FIXED** | `stopCh` deleted. Context cancellation only. Clean. |
| No embedding background worker described | **FIXED** | Embeddings punted to v0.6 entirely. Problem deferred correctly. |
| Error recovery still thin | **PARTIAL** | Per-fetch timeout (30s) added. SQLITE_BUSY still not addressed. Acceptable for v0.5. |
| Test-only interfaces slippery slope | **FIXED** | Development Guidelines section added: "Interfaces are created only when there are 2+ implementations." |

**Score: 4.5/5.** The half-point is for error recovery, but that's polish, not architecture.

---

## What's New in v3 That I Like

1. **Coordinator testing strategy is now real** - Mock fetcher pattern, context cancellation tests, timeout tests. This was missing before.

2. **Sources immutability is explicit** - "Copied at construction, never modified." This prevents a whole class of bugs.

3. **Filters in Phase 2, not Phase 4** - Shipping usable UI means shipping with filters. Good prioritization.

4. **Phase 4 deleted, not "optional"** - Embeddings are v0.6. No wishy-washy "maybe we'll do it" language.

5. **Lifecycle documentation** - "Coordinator must have its context cancelled before Store is closed." This is the kind of comment that saves 3am debugging sessions.

---

## Minor Nits (Not Blockers)

1. **`NewCoordinatorWithFetcher` interface injection** - The plan shows a `fetcher` interface in coordinator.go for testing. This slightly violates "interfaces in test files only." Acceptable pragmatism, but watch for scope creep.

2. **No integration test spec** - Still "deferred to after Phase 3 ships." That's fine, but don't forget.

3. **SQLITE_BUSY handling** - If concurrent access is high, WAL mode alone won't save you. Consider a retry loop for transient errors. Not a v0.5 blocker.

---

## Verdict

This is a clean plan. The architecture is boring in all the right ways:

- One goroutine for background work
- One stop mechanism (context)
- Pure functions for filtering
- No custom event system
- No premature interfaces

The authors successfully resisted the urge to add complexity in response to criticism. That's mature engineering.

**Ship it.**

---

*Grumpy Senior Architect*
*Three reviews, one codebase. I expect it to work.*
