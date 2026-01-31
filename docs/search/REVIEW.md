# Adversarial Architecture Review: Search Implementation Plans

**Reviewers:** GPT-5, Grok-4, Gemini-3 (as grumpy principal engineers)
**Date:** 2026-01-30
**Subject:** 10-feature search upgrade for Observer (5K-line Go/Bubble Tea TUI)

---

## Unanimous Verdict: NO

All three reviewers independently rejected the implementation plans as written. The consensus: this is a product rewrite disguised as 10 features, and shipping all of them will result in shipping none.

> **GPT-5:** "No. This is a 5K-line Go TUI and these plans read like you're trying to build a miniature search product platform."
>
> **Grok-4:** "No, I would not approve any of this for implementation. Your plans balloon it into a 50K LoC monster."
>
> **Gemini-3:** "NO GO. You're trying to turn a perfectly good 5K-line RSS reader into a distributed RAG pipeline running inside a terminal loop."

---

## Consensus Critical Issues (all 3 agree)

### 1. Mode/State Explosion — The `App` God-Struct Problem

All three independently flagged that adding ~30 new boolean/struct fields to `App` creates unmaintainable state combinatorics.

- **GPT-5:** "By Feature 10 you have: `searchActive`, `historyActive`, `filterPickerActive`, `articleActive`, `debugVisible`, `moreLikeThis`... boolean combinatorics + ad-hoc priority cascades. You need one explicit top-level UI mode enum."
- **Grok-4:** "1b contradicts: boolean vs enum. This boolean combinatorics explosion will leak goroutines."
- **Gemini-3:** "You are adding roughly 30 new fields to your App struct. The Update() method is going to be a 1,000-line switch statement of doom. Decompose into sub-models."

**Fix:** Define a strict `AppMode` enum. Route `Update()` by mode first, then keys. Decompose features into sub-models that own their own state.

### 2. Keybinding Collisions Across Features

Every reviewer caught overlapping key assignments:

| Key | Conflict |
|-----|----------|
| `m` | "More Like This" (F1) vs "Mark read" (F10c) |
| `r`/`R` | Refresh (current) vs "Deep Rerank" (F2b) vs "Apply Rerank" (F3d) |
| `p` | "Pin search" (F9b) vs typing `p` in filter (F5c) |
| `Tab` | Chip focus (F8c) vs Tab switching (F9b/9d) |
| `Ctrl-P` | "Toggle pin" (F5c) vs Up navigation |
| `Enter` | Toggle read (current) vs Open article (F10c) |

**Fix:** Produce a single global keymap across all modes before writing code. No feature gets a key without checking the registry.

### 3. SQLite Write Contention — No Unified Strategy

All three identified that piling writes from fetch, embed, history, article cache, FTS triggers, and chunk storage will hit `SQLITE_BUSY` with no coherent mitigation.

- **GPT-5:** "You need busy_timeout, consistent transaction strategy, write batching and/or a single write queue."
- **Grok-4:** "No WAL, no migration strategy. Blast radius: locked DB on concurrent fetch+search."
- **Gemini-3:** "A user typing in the search bar (triggering History writes) competes with background RSS fetches and Article caching. You will hit SQLITE_BUSY."

**Fix:** Enable WAL mode. Implement a unified write serialization strategy (channel-based writer or explicit `busy_timeout`). Single migration versioning system instead of scattered `CREATE TABLE IF NOT EXISTS`.

### 4. Missing Cancellation — Goroutine/Resource Leaks

Repeatedly using `context.Background()` for long-running operations with no cancellation path.

- **GPT-5:** "User types fast -> you queue multiple reranks. User hits Esc -> you ignore stale results but all the goroutines keep running. Ollama will happily burn CPU for 45 seconds per request."
- **Grok-4:** "QueryID check prevents race conditions — no, it ignores messages but not shared state mutations."
- **Gemini-3:** "The closure captures a slice index or a pointer to the old slice — you are going to panic with an index out of bounds."

**Fix:** Tie context to current query/rerank job. Cancel on Esc, new query, or mode change. Capture IDs not indices in closures.

### 5. Feature 1 Has Conflicting Designs

All three noted that subtasks 1a-1d propose mutually incompatible approaches.

- **GPT-5:** "1a adds EnsureItemEmbedding. 1d says show an error rather than blocking. 1b/1c recommend FilterMode enum + view stack, while 1d reintroduces moreLikeThis bool."
- **Grok-4:** "1b contradicts: allow m only from chronological view. 1c proposes FilterMode enum, but 1d reverts to moreLikeThis bool."

**Fix:** Pick one design. Enum + seed ID + optional stack OR simple boolean. Not both.

### 6. Scope is Unrealistic for a 5K-Line Codebase

Unanimous agreement that 10 features simultaneously is a rewrite, not an iteration.

- **GPT-5:** "That's not '10 small features.' That's a product rewrite."
- **Grok-4:** "10 features = ~20K new LoC. Realistic blast radius: app reloads take 30s."
- **Gemini-3:** "This is a rewrite in disguise."

---

## Consensus: What to Cut

Features all three want killed or heavily deferred:

| Feature | GPT-5 | Grok-4 | Gemini-3 | Consensus |
|---------|-------|--------|----------|-----------|
| **F4: Timeline Status Bar** | Cut | Cut | **KILL** — "developer debugging info masquerading as a feature" | **CUT** |
| **F10d: Search Enrichment/Chunks** | Cut | Cut | **KILL** — "ROI is negative" | **CUT** |
| **F3c: Smooth Reorder/Flash** | Cut (part of F3 cut) | Gold-plating | **KILL** — "Animations in TUIs are finicky" | **CUT** |
| **F8: Filter Chips/Query Language** | Cut | Cut | Not explicitly cut but concerns about chip collision | **DEFER** |
| **F9c: Auto-Refresh Pinned** | Cut (F9 entirely) | Scope creep | **DEFER** — "refresh on view is sufficient" | **DEFER** |

### Features 2/3 reviewers want cut:

| Feature | Who Wants It Cut | Who Keeps It |
|---------|-----------------|--------------|
| **F3: Progressive Pipeline** | GPT-5 ("rewrite disguised as feature"), Grok-4 (gold-plating) | Gemini-3 (keeps 3a state machine, cuts 3c) |
| **F9: Pinned Searches** | GPT-5 (cut tabs/auto-refresh), Grok-4 (cut entirely) | Gemini-3 (keeps, but simplifies) |
| **F5: Search History** | Grok-4 (cut — "explodes state") | GPT-5 (keep but merge with F9), Gemini-3 (keep) |

---

## Consensus: What's Actually Good

Even grumpy engineers acknowledge good work:

- **QueryID stale-message immunity** (all 3) — correct Bubble Tea pattern
- **FTS5 triggers restricted to `AFTER UPDATE OF title, summary`** (GPT-5) — avoids read/saved flag churn
- **Ollama "manual apply" pattern** (GPT-5, Grok-4) — correct UX for slow backends
- **`AutoReranks()` backend gating** (Grok-4) — smart opt-out without breaking fast path
- **Score column fixed-width to avoid jitter** (GPT-5) — shows real TUI experience
- **Listwise rerank fallback preserving ordering** (GPT-5) — "don't make things worse" strategy
- **Feature 1 core concept** (Grok-4) — high ROI, reuses existing embeddings, no new deps

---

## Recommended Priority Reorder

### GPT-5's Order (phased):
- **Phase 0:** Stabilize — add real cancellation contexts, SQLite busy handling
- **Phase 1:** F2a/2b (backend gating), F7a/7b (FTS5), F1 (MLT — one model)
- **Phase 2:** Merge F5+F9a (one schema), F5c (history browser redesigned)
- **Phase 3:** F6 (scores) only after stable pipeline

### Grok-4's Order (aggressive cuts):
1. Simplified F1 (1a only — no nesting/UI polish)
2. F3a (phase enum — unblocks rest)
3. F7a (FTS schema + basic lexical)
- Kill everything else. Ship F1 in 1 week; reassess.

### Gemini-3's Order (infrastructure first):
1. F7 (FTS5) — changes retrieval backend, do first
2. F5 (History) — adds DB infrastructure needed for others
3. F2 & F3 (Reranking) — implement pipelines on top of FTS
4. F9 (Pinned) — needs search pipeline stable first
5. F10 (Reader) — independent but risky complexity

### Synthesized Recommendation:

**Phase 0 (prerequisites):**
- Real cancellation contexts (stop using `context.Background()`)
- SQLite WAL + busy_timeout + unified write strategy
- Single `AppMode` enum + global keymap registry
- Resolve F1 design conflicts (pick one model)

**Phase 1 (high-value, low-risk):**
- F1: "More Like This" (simplified — one state model, no stack)
- F2a/2b: Backend detection + opt-in gating (no listwise yet)
- F7a/7b: FTS5 schema + store methods (verify no INSERT OR REPLACE first)

**Phase 2 (persistence layer):**
- Merged F5+F9a: Single search history schema with pin flag
- F5c: History browser (redesigned keybindings)
- F6: Score column toggle

**Phase 3 (reassess after Phase 2 ships):**
- F3: Progressive pipeline (if needed — may not be after FTS5 gives instant results)
- F9b-d: Tabs/auto-refresh (if pinned searches prove valuable)
- F10a-c: Reader (independent, low coupling)

**Kill list:**
- F4 (Timeline Status Bar) — move to debug overlay
- F10d (Chunk Embeddings) — research project, not a feature
- F3c (Smooth Reorder) — snap sort is fine
- F8 (Query Language/Chips) — premature for current user base

---

## Unique Insights (one reviewer only)

### GPT-5: FTS5 + INSERT OR REPLACE is a timebomb
> "If your current item upsert uses INSERT OR REPLACE, that deletes and reinserts, changing rowid and potentially breaking FTS5 external content assumptions."

Must verify the actual `items` write pattern before implementing F7.

### GPT-5: Feature 5 and Feature 9 are the same database concept
> "These are the same thing. You're going to fork schema definitions, then spend a weekend writing migrations to reconcile your own migrations."

Merge into one schema + one set of store methods. Layer pinning and tabs on top.

### Grok-4: View() at 60fps with all features active
> "Feature 3d progress bar recomputes ETA 60fps in View(); 5c Ctrl-R queries DB on every keystroke (50x per query)."

Precompute render strings. Debounce DB queries. Cache lipgloss-formatted strings.

### Gemini-3: The `tea.Cmd` Closure Trap
> "If `a.items` changes while that closure is in the work queue, and the closure captures a slice index or a pointer to the old slice, you are going to panic with an index out of bounds."

Capture IDs, not indices. Re-lookup on completion.

### Gemini-3: Configurable Keymap is now mandatory
> "With this many features, users will hate your defaults."

The key collision problem is severe enough that a keymap registry isn't a nice-to-have — it's a prerequisite.

---

## Summary: What Must Happen Before Code Gets Written

1. **One state model** — `AppMode` enum, not boolean soup
2. **One keymap** — global registry across all modes, no collisions
3. **One write strategy** — WAL + serialization for SQLite
4. **Real cancellation** — context-based, not just QueryID staleness
5. **One schema** — merge F5/F9 into unified search history + pin
6. **One F1 design** — resolve the 4 conflicting subtask proposals
7. **Verify rowid stability** — check items upsert pattern before FTS5
8. **Cut F4, F8, F10d, F3c** — complexity without proportional value
