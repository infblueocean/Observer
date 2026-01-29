# Observer v0.7 Implementation Plan: Parallel Fetch

## Executive Summary

v0.7 converts sequential source fetching to parallel fetching using `errgroup`. This improves responsiveness as the number of sources grows.

**Current behavior:** Fetch sources one at a time (N sources × T seconds = N×T total)
**New behavior:** Fetch sources concurrently (N sources = ~T total, bounded by slowest)

---

## Scope

**In Scope:**
- Convert `fetchAll` to use `errgroup` for parallel fetching
- Add concurrency limit to avoid overwhelming network
- Update existing tests that rely on ordering
- Maintain all existing behavior (UI messages, embedding, error handling)

**Out of Scope:**
- Parallel embedding (separate concern, not needed yet)
- Configuration changes
- New sources

---

## Implementation

### 1. Add Dependency

```
go get golang.org/x/sync
```

### 2. Add Constant

```go
// maxConcurrentFetches limits parallel fetch operations.
const maxConcurrentFetches = 5
```

### 3. Update fetchAll

**Current (sequential):**
```go
func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
    for _, src := range c.sources {
        // fetch one at a time
    }
    c.embedNewItems(ctx)
}
```

**New (parallel):**
```go
func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
    var g errgroup.Group
    g.SetLimit(maxConcurrentFetches)

    for _, src := range c.sources {
        g.Go(func() error {
            // Early exit if context cancelled
            if ctx.Err() != nil {
                return nil
            }
            c.fetchSource(ctx, src, program)
            return nil // never fail the group - errors reported per-source
        })
    }

    g.Wait()
    c.embedNewItems(ctx)
}
```

**Note:** Go 1.24 - no `src := src` capture needed (loop variable per-iteration since Go 1.22).

### 4. Extract fetchSource

Move single-source fetch logic to its own method:

```go
func (c *Coordinator) fetchSource(ctx context.Context, src fetch.Source, program *tea.Program) {
    fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
    defer cancel()

    items, err := c.fetcher.Fetch(fetchCtx, src)

    // Save items if fetch succeeded
    newItems := 0
    if err == nil && len(items) > 0 {
        var saveErr error
        newItems, saveErr = c.store.SaveItems(items)
        _ = saveErr // intentionally ignored - fetch succeeded, save errors are rare
    }

    if program != nil {
        program.Send(ui.FetchComplete{
            Source:   src.Name,
            NewItems: newItems,
            Err:      err,
        })
    }
}
```

### 5. Key Design Decisions

**Why plain `errgroup.Group` instead of `errgroup.WithContext`?**
- We always `return nil` - we never want one failure to cancel others
- Each source reports its own error via `ui.FetchComplete`
- No need for derived context since we're not using cancellation-on-first-error

**Why `return nil` always?**
- One source failing shouldn't cancel others
- Errors are reported via `ui.FetchComplete` per source

**Why `g.SetLimit(5)`?**
- Prevents spawning 100 goroutines if user has 100 sources
- 5 is reasonable for network I/O (matches browser parallel connection limits)
- Can be made configurable later if needed

**Why early exit check in goroutine?**
- Goroutines blocked by SetLimit will still spawn after cancellation
- Early exit check prevents wasteful work during shutdown

**Message ordering:**
- `FetchComplete` messages may arrive in any order (non-deterministic)
- Each message contains source name - UI doesn't assume ordering

---

## Testing Strategy

### Tests to Update

1. **TestCoordinatorFetchesAllSources** - Currently verifies order, needs to verify set membership instead
2. **TestCoordinatorRespectsContextCancellation** - Needs adjustment for parallel behavior

### New Tests

```go
func TestCoordinatorFetchesInParallel(t *testing.T)
    // Use sync primitives to prove concurrency (not timing)
    // Mock fetcher blocks until N concurrent calls detected

func TestCoordinatorParallelRespectsLimit(t *testing.T)
    // Verify max 5 concurrent fetches using atomic counter

func TestCoordinatorParallelHandlesErrors(t *testing.T)
    // One source error doesn't affect others

func TestCoordinatorParallelEarlyExitOnCancel(t *testing.T)
    // Cancelled context causes goroutines to exit early
```

---

## Success Criteria

- [ ] `fetchAll` uses errgroup for parallel fetching
- [ ] Uses plain `errgroup.Group` (not WithContext)
- [ ] Concurrency limited to 5 simultaneous fetches
- [ ] Early exit check for cancelled context in goroutines
- [ ] One source failure doesn't affect others
- [ ] Context cancellation stops fetches promptly
- [ ] UI messages still sent for each source
- [ ] Embedding still runs after all fetches complete
- [ ] All existing tests pass (after updates)
- [ ] New parallel tests pass
- [ ] Tests pass with `-race`

---

## Risk Assessment

| Risk | Mitigation |
|------|------------|
| Race in SaveItems | Store uses mutex - safe (minor lock contention) |
| Race in UI messages | tea.Program.Send is goroutine-safe |
| Goroutine leak | errgroup.Wait ensures all complete |
| Network flood | SetLimit(5) caps concurrency |
| Message ordering | UI doesn't assume order, each message has source name |

---

## Deferred

- Configurable concurrency limit (add when users request)
- Per-source timeout configuration (add when needed)
- Retry logic (add when flaky sources become a problem)
- Batching SaveItems to reduce lock contention (profile first)
