# v0.6 Plan Pragmatism Review

**Reviewer:** Grumpy Senior Engineer
**Verdict:** Too many phases. Collapse it.

---

## YAGNI Violations

1. **Phase 5 (Parallel Fetch)** - Why is this here? This is a performance optimization for a problem you don't have yet. With 5-10 RSS feeds, sequential fetch takes what, 3 seconds? Ship without it. Add when users complain.

2. **`Dimension()` method** - You hardcode `nomic-embed-text`. You know the dimension. Delete this method.

3. **Graceful degradation matrix** - You wrote a table for 3 rows of "it works or it doesn't." This is documentation theater.

---

## Complexity Budget

The plan is reasonable on complexity. Binary blob encoding, cosine similarity, and O(N^2) dedup are all appropriate choices. Good that HNSW is deferred.

BUT: 5 phases for what amounts to "add embeddings to SQLite and use them for dedup" is too many coordination points.

---

## Phase Ordering Problems

Current: Embedder -> Storage -> Worker -> Dedup -> Parallel Fetch

**Issues:**
- Phase 3 (Worker) and Phase 4 (Dedup) are tightly coupled but split apart. You can't test dedup meaningfully without embeddings existing.
- Phase 5 doesn't belong in this version at all.

**Suggested:**
1. Embedder + Storage (can't use one without the other)
2. Worker + Dedup (the actual feature)
3. Delete Phase 5 entirely

---

## Premature Optimization

- `embedBatchSize = 10` and `embedDelay = 100ms` - Are these numbers from profiling or your imagination? Start with batch size 1, no delay. Optimize when Ollama actually chokes.
- `maxConcurrentFetches = 5` - Again, measured nothing. Delete Phase 5.

---

## What v0.6 Actually Needs

1. OllamaEmbedder that can embed text
2. Store embeddings in SQLite
3. After fetch, embed items that don't have embeddings
4. Filter with cosine similarity when available

That's it. 2 phases max.

---

## Recommended Changes

1. **Merge Phases 1+2** - Embedder and storage are pointless without each other
2. **Merge Phases 3+4** - Worker and dedup are the actual feature
3. **Delete Phase 5** - Parallel fetch is scope creep
4. **Remove rate limiting** - Add it when you have evidence of problems
5. **Simplify testing** - You don't need 5 test functions for cosine similarity edge cases

Ship in 2 weeks, not 5.
