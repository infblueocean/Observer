# v0.6 Plan Architecture Review

**Reviewer:** Senior Architect (grumpy)
**Date:** 2026-01-28
**Verdict:** Mostly sound. A few things need fixing before implementation.

---

## 1. Architectural Fit

**Good:** The plan respects v0.5 boundaries. Store gets new methods, Coordinator gets an optional dependency. No surgery on existing code.

**Problem:** The `embedder` interface is defined *inside* the coord package (Phase 3.1). Wrong place. Put it in `internal/embed/embed.go` as a proper exported interface. Coordinator should import it, not define it.

**Fix:** Create `embed.Embedder` interface. Coordinator depends on `embed.Embedder`, not its own internal type.

---

## 2. Interface Design

The OllamaEmbedder is concrete, but where's the interface? You have:
- `OllamaEmbedder` struct in Phase 1
- `embedder` interface in Phase 3 (buried in Coordinator)

This is backwards. Define the interface first in `internal/embed/`:

```go
type Embedder interface {
    Available() bool
    Embed(ctx context.Context, text string) ([]float32, error)
}
```

Then `OllamaEmbedder` implements it. Clean dependency injection, testable everywhere.

---

## 3. Graceful Degradation

**Solid.** The degradation matrix is clear. The check-and-skip pattern in `embedNewItems` is correct.

**Missing:** What happens if Ollama becomes unavailable mid-batch? The plan shows checking once at the start. Add re-check logic or document that partial embedding is acceptable.

---

## 4. Phase Ordering

**Problem:** Phase 5 (parallel fetch) is independent of Phases 1-4. It doesn't need embeddings. Do it first or in parallel with Phase 1. Faster wins from day one.

**Suggested order:** 5 -> 1 -> 2 -> 3 -> 4

---

## 5. Missing Pieces

1. **Migration versioning.** How does `ALTER TABLE` know it ran already? SQLite will error on duplicate column add. Need a migrations table or check-before-alter pattern.

2. **Embedding dimension validation.** What if someone switches models mid-stream (768 dims vs 1536)? Old embeddings become garbage. Either store dimension metadata or invalidate on model change.

3. **No status visibility.** User has no idea if embeddings are happening. Add a status message or log line.

4. **CosineSimilarity belongs in embed package,** not as a package-level function in filter. Filter shouldn't know about embedding math.

---

## Summary

| Issue | Severity | Fix |
|-------|----------|-----|
| Interface in wrong package | Medium | Move to `embed.Embedder` |
| Phase ordering suboptimal | Low | Move Phase 5 first |
| No migration versioning | High | Add migrations table |
| Dimension mismatch risk | Medium | Store model/dimension metadata |
| CosineSimilarity misplaced | Low | Move to embed package |

The bones are good. Fix the interface location and migration story before coding.
