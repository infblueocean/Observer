# Search Implementation Plan v2 — Adversarial Review (Round 2)

**Date:** 2026-01-30
**Document reviewed:** docs/search/IMPLEMENTATION.md (v2)
**Method:** Same 3-model adversarial panel as Round 1

---

## Verdict Summary

| Model | Verdict |
|-------|---------|
| GPT-5 | NEEDS REVISION |
| Grok-4 | NEEDS REVISION |
| Gemini-3 | APPROVED WITH CONCERNS |

---

## GPT-5 Review

## Verdict: NEEDS REVISION

## Critical Issues (must fix)

1. **Global `?` key breaks typing in search input (keymap collision across modes).**  
   - **Where:** §2b “ModeSearch (typing query)” says “*any printable* → Input to filterInput”; §2a `handleGlobalKeys` toggles debug overlay on `"?"` *before mode dispatch*.  
   - **Why it’s a bug:** In ModeSearch you can no longer type a literal `?` into the query because it will always toggle debug instead of inserting a character. This is exactly the kind of “global key” collision Phase 0b was supposed to eliminate.  
   - **Fix:** Make debug toggle non-printable (e.g., `F1`), or scope it to non-text-entry modes, or only treat `?` as global when `a.mode != ModeSearch` (and any other text-entry views).

2. **Phase 0 AppMode migration is not actually applied in Phase 1: compile-breaking leftovers (`searchActive`).**  
   - **Where:** §2a says **DELETE: `searchActive bool`** and replace with `a.mode`. But §3 Feature 1 uses `a.searchActive = false` and `hasQuery()` returns `a.filterInput.Value() != "" && !a.searchActive`.  
   - **Impact:** This won’t compile after Phase 0, and it also contradicts the plan’s own mode-routing model.  
   - **Fix:** Replace all `searchActive` references with AppMode logic (e.g., `a.mode == ModeSearch` for “typing” state) and re-define `hasQuery()` in terms of `(mltSeedID != "" || activeQuery != "")` rather than mixing UI mode and query presence.

3. **Per-query cancellation design as written will erase `queryID` at the start of a new search (ordering bug), breaking staleness correlation.**  
   - **Where:** §2c `newSearchContext()` calls `a.cancelSearch()`; `cancelSearch()` sets `a.queryID = ""`. But §3 Feature 7 `submitSearch()` does `a.queryID = newQueryID()` *then* `ctx := a.newSearchContext()`.  
   - **Impact:** Your freshly generated queryID will be cleared, so subsequent async messages won’t correlate, logs will emit empty QueryID, and stale-discard logic becomes unreliable.  
   - **Fix options:**  
     - (Preferred) Move `queryID` generation **after** `newSearchContext()` in every call site, or  
     - Change `cancelSearch()` to *not* mutate `queryID`, and instead introduce a separate `activeSearchToken` that is replaced atomically on new searches.

4. **Function signature mismatch for `LoadSearchPool` (ctx added in Phase 0c) vs Feature 1 call sites.**  
   - **Where:** §2c updates AppConfig: `LoadSearchPool func(ctx context.Context, queryID string) tea.Cmd`. But §3 Feature 1 `handleMoreLikeThis()` calls `a.loadSearchPool(a.queryID)` (no ctx).  
   - **Impact:** Won’t compile / inconsistent command wiring. Also contradicts §2c “Call sites that cancel: handleMoreLikeThis()”. Feature 1 as written does not create a new per-query ctx nor cancel the prior one.  
   - **Fix:** Thread `ctx := a.newSearchContext()` into MLT just like text search, and call `a.loadSearchPool(ctx, a.queryID)`.

5. **Major abstraction inconsistency: Phase 0c switches to function-based AppConfig (`BatchRerank`, `ScoreEntry`), but Feature 2 is written around a `Reranker` interface and `a.reranker.Available()`.**  
   - **Where:** §2c “AppConfig signature changes” vs §3 Feature 2 “Primary interface remains unchanged: type Reranker interface …” and key handler checks `a.reranker.Available()`.  
   - **Impact:** You cannot implement Feature 2 in the planned order without first reconciling which architecture is real: interface-driven backend objects or injected tea.Cmd factories. Right now the plan is internally contradictory.  
   - **Fix:** Choose one:  
     - If you keep AppConfig as cmd factories, then “AutoReranker” needs to be expressed as config capability flags, not type assertions on a backend object.  
     - If you keep backend interfaces, then Phase 0c’s AppConfig changes need to be rewritten around those interfaces (and you pass ctx into the interface methods and wrap them in Cmds).

6. **Esc behavior is contradictory between the keymap and Feature 2’s UX claims (“Esc cancels rerank and reverts to cosine”).**  
   - **Where:** §2b ModeResults: `Esc` = “Exit results; restores savedItems; transitions to ModeList.” But §3 Feature 2 “Esc during rerank → Cancel; revert to cosine; clear status.”  
   - **Impact:** These are different actions. If Esc exits results, you can’t use it as “cancel rerank but stay on results.” If you repurpose Esc to cancel rerank, you lose the universal “back out of results” behavior.  
   - **Fix:** Decide a single consistent rule. Common options:  
     - `Esc` always backs out a view (ModeResults→ModeList), and introduce another cancel key during rerank (e.g., `Ctrl-G`).  
     - Or `Esc` cancels in-flight work **first** (rerank/search), and a second `Esc` exits results (requires explicit state & UI hint).

7. **`savedItems` snapshot semantics are inconsistent; chaining searches will not restore the original chronological view.**  
   - **Where:** Feature 1 carefully preserves the first snapshot: “Save chronological view on first entry… if `savedItems == nil`”. But Feature 7 `submitSearch()` unconditionally overwrites `savedItems` every time Enter is pressed.  
   - **Impact:** From ModeResults, pressing `/` then Enter will snapshot the current results as “savedItems”; Esc then restores to the *previous results* (not chronological), while keymap claims Esc returns to ModeList chronological feed.  
   - **Fix:** Make snapshotting consistent across **all** entry points into ModeResults (text search, MLT, history re-run, pinned tabs): only take `savedItems` when entering results from ModeList (or when `savedItems == nil`).

8. **FTS5 query SQL likely wrong due to table aliasing (`WHERE items_fts MATCH ?` while alias is `fts`).**  
   - **Where:** §3 Feature 7b `searchFTSRaw()` uses `FROM items_fts fts ... WHERE items_fts MATCH ? ORDER BY bm25(items_fts, ...)`.  
   - **Impact:** In SQLite, once you alias a table (`items_fts fts`), the original name is generally not valid in expression contexts; `items_fts` may be treated as an identifier/column and can error. The safe form is `WHERE fts MATCH ?` and `bm25(fts, ...)`.  
   - **Fix:** Use the alias consistently: `WHERE fts MATCH ?` and `ORDER BY bm25(fts, 10.0, ...)`.

9. **FTS5 schema migration is incomplete/unsafe for existing installs (IF NOT EXISTS won’t add columns like `author`).**  
   - **Where:** §3 Feature 7a adds `author` to `items_fts` and triggers, but only via `CREATE VIRTUAL TABLE IF NOT EXISTS` and `CREATE TRIGGER IF NOT EXISTS`.  
   - **Impact:** If a user already has an `items_fts` table from a prior version (or from partial rollout) without `author`, your new schema won’t apply; rebuild logic (§7a “Conditional rebuild”) won’t fix schema mismatch because `count(*) > 0` short-circuits.  
   - **Fix:** You need a minimal migration/versioning story (e.g., `PRAGMA user_version` and explicit migrations), or at least detect schema signature and drop/recreate/rebuild FTS when mismatched.

## Minor Issues (should fix)

1. **`FTSResults` message type is defined but not used in the described synchronous FTS flow.**  
   - **Where:** §3 Feature 7c “New Message Type” defines `FTSResults`, but `submitSearch()` calls `a.searchFTS(...)` synchronously and never emits `FTSResults`. Tests list “Type exists” which is not meaningful.  
   - **Fix:** Either remove `FTSResults` for now, or actually implement FTS as a Cmd that returns `FTSResults` (especially if Phase 3 wants an explicit pipeline stage).

2. **Dependency graph contradicts Feature 7’s stated prerequisites.**  
   - **Where:** §3 Feature 7 says prerequisites include Phase 0a and 0c; §8 dependency graph says F7 depends on 0d.  
   - **Fix:** Align the graph with the text, or vice versa.

3. **Feature-flag gating is incomplete relative to the keymap table.**  
   - **Where:** §2b keymap lists `Ctrl-R` history everywhere and `Tab` pinned cycling in ModeResults, but §2d gating example only shows `m`.  
   - **Fix:** Spell out behavior when `SearchHistory` is false (keys no-op? hidden?), and ensure handlers check feature flags consistently so tests don’t encode contradictory expectations.

4. **Test helper `teaRunner()` can infinite-loop on spinner ticks or any Cmd that schedules another Cmd.**  
   - **Where:** §7 “Tea Runner for async Cmd testing” loops until `cmd == nil`, but many updates return `a.spinner.Tick` repeatedly.  
   - **Fix:** Add a step limit, or a runner that executes exactly one Cmd, or a runner that filters out timer/spinner cmds.

5. **Search history schema includes `query_embedding BLOB` but the plan never defines how it’s populated/used.**  
   - **Where:** §4 Feature 5+9 schema.  
   - **Concern:** This looks like reintroducing “semantic history” scope without design/testing, and it complicates migrations.  
   - **Fix:** Remove until there’s a concrete use (e.g., fast rerun without embedding API), or specify the exact pipeline for filling it and invalidating it.

6. **`bm25()` factual note is questionable.**  
   - **Where:** §3 Feature 7b comment “bm25() returns negative values”.  
   - **Why:** FTS5’s `bm25()` is typically “smaller is better,” but not necessarily negative; the sign claim is not reliable.  
   - **Fix:** Keep only the ordering property (“smaller is more relevant”) and drop the sign claim.

## Observations (informational)

- The plan is much improved in explicitly acknowledging orchestration-level cancellation (§1.1 nuance on #3) and in trying to separate “mode” from “pipeline state” (§2a). The remaining problems are mostly from **unfinished refactoring propagation** (Phase 1 sections still written against pre-Phase-0 state).
- The biggest implementation risk now is **state coherence across entry points into ModeResults**: text search, MLT, history re-run, pinned tabs. You’ll want a single “enterResults(snapshotPolicy)” helper that centralizes: snapshotting, queryID creation, ctx creation, pipeline-flag initialization, and status text—otherwise you’ll keep reintroducing subtle divergence.
- SQLite schema evolution is still under-specified. Adding FTS + history tables via “IF NOT EXISTS” is fine for first install, but the moment you change columns/triggers, you need at least a lightweight migration/version check to avoid silently running with a partially-upgraded schema.

---

## Grok-4 Review

## Verdict: NEEDS REVISION
## Critical Issues (must fix)
- **App struct missing `features Features` field (blocks Phase 0d/1 implementation).** Section 2d defines `Features` struct in `AppConfig` with `Validate()`, and key handlers (e.g., Feature 1 `handleListKeys` example: `if a.features.MLT`) assume `a.features` exists on `App`. But no App struct addition shown (2a only adds `mode`/`modeStack`). Add `features Features` to App init from `AppConfig.Features`. Without this, feature gating fails to compile.
- **Feature 1 `handleMoreLikeThis()` does not transition to `ModeResults` (blocks UI routing).** Section 3 Feature 1 claims "Reuse `ModeResults`" and "transitions to ModeResults", but code sets `mltSeedID`/`mltSeedTitle` without `a.mode = ModeResults` or `a.pushMode(ModeResults)`. From `ModeList`, post-MLT keys route via `handleListKeys` (e.g., no `R` for rerank, `m` chains incorrectly). `submitSearch()` (F7c) correctly sets `a.mode = ModeResults`; mirror for MLT. Also deletes `searchActive` (2a migration) but code sets `a.searchActive = false` (line in `handleMoreLikeThis()`). Contradicts 2a "DELETE: searchActive".
- **Feature 1/2 inconsistency: `activeQuery` unset for MLT (blocks Ollama manual rerank in MLT).** Feature 2 `QueryEmbedded` sets `a.activeQuery = msg.Query` (text search only). MLT path (`handleMoreLikeThis` → `startMLTReranking` → `startReranking`) passes synthetic `entryText(seed)` but Feature 2 `R` handler uses `a.activeQuery` (unset, since MLT skips `embedQuery`). Chains (`m` from results) exacerbate. Set `a.activeQuery = entryText(seed)` in `handleMoreLikeThis`/`startMLTReranking`. Without, `R` no-ops in MLT results.
- **Phasing error: F5+9 deps on F7 schema unfounded (blocks Phase 2).** Section 8 graph/4: "F5+9 (depends on: F7 schema)". History schema (`search_history`/`search_results`) independent of FTS5 (`items_fts`). No cross-ref; can build post-Phase 0. Falsely serializes phases.
- **`cancelSearch()` called on mode transitions? (invalid states).** Section 2c lists call sites: submitSearch/clearSearch/handleMoreLikeThis/Esc from Results→List. But `handleGlobalKeys`/`Esc` in modes (e.g., ModeHistory `Esc` → `popMode`) don't call it. If search in-flight when entering History/Article, leaks goroutines. Add `cancelSearch()` to `popMode()` or mode transitions out of Results/Search.
## Minor Issues (should fix)
- **Keymap: ModeArticle `q`/Ctrl-C inconsistent with globals (usability).** Section 2b ModeArticle lists `q` / `Ctrl-C` quit, but 2a `handleGlobalKeys` catches `Ctrl-C`/`q` first (quits app). ModeArticle table redundant; remove or clarify `q` as local if globals skipped (but code shows globals first).
- **`hasQuery()` incomplete for Feature 2 Ollama path (stale UI).** Section 3 Feature 1 updates `hasQuery()` to `mltSeedID != "" || filterInput.Value() != ""`. But post-cosine Ollama (no auto-rerank), `embeddingPending=false`, `rerankPending=false`, `mltSeedID=""` → `!hasQuery()` hides results bar prematurely. Add `len(a.items) > 0 && a.queryID != ""` or `activeQuery != ""`.
- **FTS5 `author` assumes schema change (factual risk).** Section 3.7a adds `author` to FTS/items JOIN/Scan but no evidence `items.author` exists pre-v2 (1.1 cites `store.go:143` items upsert without it). If missing, migration/Scan fails. Add `ALTER TABLE` or confirm in prerequisites.
- **Testing gaps (Phase 1/2 interactions).** Section 7: No `TestMLTOllamaRerank` (MLT → `R` with `activeQuery`), `TestMLTModeTransition` (post-MLT keys route correctly), `TestCancelOnModePop` (History `Esc` cancels search), `TestHistoryPinnedSort` (pinned-first stable sort per 4). `TestScoreColumn_CosineFallbackMLT` missing (MLT `getBestScore` fallback recomputes cosine 60fps if no `rerankScoreMap`).
- **View() perf: `getBestScore()` fallback O(1) but embeds map scan?** Section 4 Feature 6: Claims O(1) but cosine fallback `a.embeddings[itemID]` is O(1), then `cosine()` func call (cheap). But if `rerankScoreMap` sparse + many items, 60fps jitter risk (Grok-4 concern). Precompute full cosine map on `SearchPoolLoaded` if `showScores`.
## Observations (informational)
- Strong improvements: Mode stack unifies modals (addresses 1.2 #1); queryID staleness + ctx cancel solid (Gemini-3 closure fix); FTS5 external content leverages stable rowids correctly (1.1 #2).
- Phasing mostly feasible post-fixes: Phase 1 parallel OK (F1/F2/F7 indep); cut list rigorous (no creep).
- Factuals accurate: WAL/busy_timeout (1.1 #1), ctx in HTTP (1.1 #3), RWMutex (1.1 #4), FTS triggers handle `OR IGNORE` (3.7a).
- Scope stable: Author in FTS "now is cheap" (Gemini-3); no new features vs. v1 cuts.

---

## Gemini-3 Review

Here is the adversarial review of the Search Implementation Plan v2.

## Verdict: APPROVED WITH CONCERNS

This v2 plan is significantly improved. It corrects the factual errors regarding SQLite concurrency and standardizes the application state model. The cut list and phasing are realistic. However, there is a critical ambiguity in the **Mode Stack** logic regarding history navigation that could lead to a broken UX loop, and a few minor technical risks regarding Bubble Tea compatibility.

---

## Critical Issues (Must Fix)

### 1. Mode Stack Logic: History → Results Transition
**Section:** 2a (AppMode Enum) & 2b (Keymap - ModeHistory)
**Issue:** The plan describes a `modeStack` for modal windows but is ambiguous about the `Enter` transition from `ModeHistory` to `ModeResults`.
*   **Scenario:** User flow: `ModeList` → `/` (Search) → `Enter` (Results) → `Ctrl-R` (History).
    *   Stack is currently: `[ModeList, ModeResults]`. Current Mode: `ModeHistory`.
*   **The Problem:** If `Enter` in `ModeHistory` simply calls `pushMode(ModeResults)`, the stack becomes `[ModeList, ModeResults, ModeHistory]`.
    *   Pressing `Esc` from the new search results would return the user to *History*, not the previous view or the List.
    *   Repeatedly picking items from History would infinitely grow the stack: `[..., History, Results, History, Results]`.
*   **Required Fix:** Explicitly define `Enter` in `ModeHistory` to **replace** the current stack tip or **pop** the history mode before transitioning.
    *   *Correct Behavior:* `Enter` should `popMode()` (remove History) AND update the underlying `ModeResults` state (or `ModeList` state if that was the parent).
    *   **Action:** Add a `replaceMode(AppMode)` helper or clarify that `Enter` in History performs `popMode` + state update, rather than a push.

---

## Minor Issues (Should Fix)

### 2. `Shift-Tab` Compatibility in TUI
**Section:** 2b (Keymap - ModeResults)
**Issue:** The plan relies on `Shift-Tab` to cycle pinned tabs backwards.
*   **Technical Context:** In many terminal environments (standard VT100/ANSI), `Shift-Tab` sends the exact same byte sequence as `Tab` unless specific terminal flags are set (which Bubble Tea handles inconsistently across platforms) or `[Z` is parsed manually.
*   **Recommendation:** Keep `Shift-Tab` as the happy path, but implement a fallback keybinding (e.g., `h`/`l` or `Left`/`Right` if focus is not in an input field) to ensure navigation is possible on all terminals.

### 3. Synchronous FTS Performance Risk
**Section:** 7c (Search Flow Integration)
**Issue:** The plan executes `a.searchFTS(query)` synchronously inside `submitSearch`.
*   **Risk:** While SQLite FTS is fast, a large dataset or cold disk cache could spike this >16ms (1 frame), causing a perceptible UI stutter on `Enter`.
*   **Recommendation:** Strictly enforce the "monitor in practice" note. If *any* stutter is observed, wrap `searchFTS` in a `tea.Cmd` immediately. Do not hesitate to make the "instant" search slightly async (1-tick delay) to preserve the render loop integrity.

### 4. AppConfig Validation Logic
**Section:** 2d (Feature Flags)
**Issue:** `if c.Features.MLT && c.EmbedQuery == nil`
*   **Technical Nuance:** Feature 1 (MLT) technically relies on *local* embeddings (`seedEmb`) and the *local* cosine distance. It does not necessarily require the *API* `EmbedQuery` function (which embeds a string text).
*   **Correction:** While `EmbedQuery` is a decent proxy for "Embeddings are enabled," a more accurate check might be `if c.Features.MLT && c.LoadSearchPool == nil`. MLT can function even if the embedding API is down, provided items have pre-calculated embeddings.

### 5. Context Propagation in Store
**Section:** 2c (Per-Query Context Cancellation)
**Issue:** The plan changes `AppConfig` signatures to accept `ctx`, but does not explicitly show the `store` package methods (like `LoadSearchPool`) being updated to accept/use `ctx`.
*   **Recommendation:** Verify that the `store` implementation actually passes this `ctx` to `db.QueryContext`. If the store methods ignore the context, the cancellation work in the UI layer is futile.

---

## Observations (Informational)

*   **FTS5 Triggers & External Content:** The trigger definitions in Section 7a are syntactically correct for SQLite FTS5 external content tables. The reliance on `INSERT OR IGNORE` preserving `rowid` is standard behavior for SQLite, making this a sound design choice.
*   **Score Column Performance:** The shift to an O(1) map lookup in Phase 2/Feature 6 is a necessary optimization. The v1 reviews correctly identified the O(N) risk in `View()`.
*   **Scope & Phasing:** The breakdown into Phase 0/1/2/3 is excellent. Phase 0 is a heavy lift (refactoring core architecture) but strictly necessary.
*   **Test Strategy:** The addition of `teaRunner` for async command testing is a good inclusion.

## Final Approval Status
**Proceed to implementation**, contingent on resolving the **Mode Stack** transition logic to prevent navigation loops.

---

