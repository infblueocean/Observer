# Search Features — Implementation Plans

**Generated:** 2026-01-30
**Method:** Pair-programmed (Claude + external model synthesis per subtask)
**Source:** [SEARCH.md](./SEARCH.md) — Brain Trust Synthesis

---

## Feature 1: "More Like This" (`m` key)

**Priority:** 1 (highest ROI, lowest complexity)
**Consensus:** All 3 brain trust models (GPT-5, Grok-4, Gemini-3) independently proposed this
**Cost:** Zero API calls for cosine path (embeddings already in SQLite)

### 1a. Data Flow

Below is the authoritative implementation plan for the "More Like This" (MLT) feature.

#### 1. Architecture Changes

We need to add one function to `AppConfig` and one new message type. This separates the "seed" logic from standard text queries while reusing the downstream reranking machinery.

**In `AppConfig`:**

```go
// EnsureItemEmbedding checks the DB for an embedding.
// If missing, it uses the Embedder API, saves to DB, and returns it.
EnsureItemEmbedding func(item store.Item, queryID string) tea.Cmd
```

**New Message Type:**

```go
type SeedEmbedded struct {
    ItemID    string
    Embedding []float32
    QueryText string // The text used to generate the embedding (for cross-encoder)
    QueryID   string
    Err       error
}
```

#### 2. The `handleMoreLikeThis` Flow

When the user presses `m`, we perform a tiered lookup. If the embedding is in memory, we proceed immediately. If not, we trigger the async retrieval.

```go
func (a *App) handleMoreLikeThis() (tea.Model, tea.Cmd) {
    // 1. Validation
    if len(a.items) == 0 || a.cursor < 0 || a.cursor >= len(a.items) {
        return a, nil
    }
    item := a.items[a.cursor]

    // 2. State Management (The "Pivot" Check)
    // Only save state if we are effectively starting a fresh search chain.
    // If queryID is set, we are likely already deep in a search results view.
    if a.queryID == "" {
        a.savedItems = make([]store.Item, len(a.items))
        copy(a.savedItems, a.items)
        // Clone map to avoid reference issues
        a.savedEmbeddings = make(map[string][]float32, len(a.embeddings))
        for k, v := range a.embeddings {
            a.savedEmbeddings[k] = v
        }
    }

    // 3. Setup Query State
    a.queryID = newQueryID()
    a.searchStart = time.Now()
    a.searchActive = false // We are not typing

    // UX: Set input to explain what is happening.
    // This also ensures hasQuery() returns true for SearchPoolLoaded.
    a.filterInput.SetValue(fmt.Sprintf("More like: %s", truncate(item.Title, 40)))
    a.statusText = "Finding similar items..."

    // 4. Data Flow: Tiered Lookup
    emb, inMemory := a.embeddings[item.ID]

    if inMemory && len(emb) > 0 {
        // Fast Path: Embedding exists in RAM.
        // Set vector, rank immediately, then load full pool for better recall.
        a.queryEmbedding = emb
        a.rerankItemsByEmbedding() // Updates a.items immediately (local view)
        a.removeSeedItem(item.ID)  // Don't show the item itself

        // Start background tasks
        a.searchPoolPending = true
        a.embeddingPending = false // We already have it

        // Use item text for the cross-encoder refinement
        queryText := entryText(item)

        return a, tea.Batch(
            a.loadSearchPool(a.queryID),   // Fetch full corpus
            a.startReranking(queryText),   // Start cross-encoder on top 30
        )
    }

    // Slow Path: Embedding missing from RAM.
    a.queryEmbedding = nil
    a.searchPoolPending = true
    a.embeddingPending = true

    return a, tea.Batch(
        a.loadSearchPool(a.queryID),
        a.ensureItemEmbedding(item, a.queryID),
    )
}
```

#### 3. Handling the Async Responses

We need to handle the new `SeedEmbedded` message and ensure `SearchPoolLoaded` respects the MLT context.

**Handling `SeedEmbedded`:**

```go
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case SeedEmbedded:
        if msg.QueryID != a.queryID {
            return a, nil // Stale result
        }
        a.embeddingPending = false

        if msg.Err != nil {
            a.statusText = "Error embedding item: " + msg.Err.Error()
            return a, nil
        }

        // Store for future use
        if a.embeddings == nil {
            a.embeddings = make(map[string][]float32)
        }
        a.embeddings[msg.ItemID] = msg.Embedding

        // Set query vector and rank
        a.queryEmbedding = msg.Embedding

        // If pool is already loaded, we can rank everything now
        if !a.searchPoolPending {
            a.rerankItemsByEmbedding()
            a.removeSeedItem(msg.ItemID)
            return a, a.startReranking(msg.QueryText)
        }
        // If pool is still pending, SearchPoolLoaded will handle the ranking
        return a, nil
    // ...
```

**Handling `SearchPoolLoaded` (Adjustment):**

The existing handler likely checks `hasQuery()`. Since we set `filterInput` in `handleMoreLikeThis`, standard checks pass. However, we must ensure we don't accidentally trigger text embedding logic.

```go
    case SearchPoolLoaded:
        if msg.QueryID != a.queryID {
            return a, nil
        }
        a.searchPoolPending = false

        // Update corpus
        a.items = msg.Items
        a.embeddings = msg.Embeddings

        // If we have the vector ready (Fast Path or SeedEmbedded arrived first)
        if len(a.queryEmbedding) > 0 {
            a.rerankItemsByEmbedding()

            // CRITICAL: Remove the seed item from results
            // We need to know the seed ID. We can parse it from filterInput
            // or store it in a temporary a.mltSeedID field.
            // Assuming we stored it or can deduce it:
            a.removeSeedItem(a.mltSeedID)

            // Trigger cross-encoder
            // Note: We need the original text. If we didn't store it,
            // we can extract it from the item in the new pool by ID.
            return a, a.startReranking(a.getSeedText())
        }
        return a, nil
```

#### 4. Helper Functions

**Removing the Seed:**
The selected item will always have a cosine similarity of 1.0 (perfect match). It must be removed to show *other* similar items.

```go
func (a *App) removeSeedItem(id string) {
    // Go 1.21+ slices.DeleteFunc, or standard filter loop
    keep := a.items[:0]
    for _, item := range a.items {
        if item.ID != id {
            keep = append(keep, item)
        }
    }
    a.items = keep
}
```

**Getting Text for Cross-Encoder:**
The existing `entryText` function (Title + Summary) is perfect. We pass this string to `startReranking`, which eventually calls `batchRerank` or `scoreEntry`.

#### 5. Edge Case Analysis

1.  **Missing Embeddings:** Handled via `EnsureItemEmbedding`. If the API fails, we display the error in `statusText`, effectively reverting to a standard list view (or potentially falling back to cross-encoder only, though that is slow for the full pool).
2.  **Esc Key:** Because we reused `a.queryID` and saved state into `savedItems`, the existing `clearSearch` function works without modification. It will restore the pre-MLT view.
3.  **Active Search:** If `m` is pressed while `searchPoolPending` is true, the `queryID` check prevents race conditions. The new `queryID` invalidates the old in-flight messages.
4.  **No Results:** `filter.RerankByQuery` pushes items without embeddings to the bottom. If the pool is empty or no items match well, the user sees the raw list, which is acceptable fail-safe behavior.

> **Audit trail:** Synthesized by Gemini-3 from Claude (internal) + GPT-5 (external). Raw responses: `20260130_222556_claude_gpt5_gemini3.json`

### 1b. UI State & Messages

#### 1) New App fields needed
`moreLikeThisActive bool` + `moreLikeThisItem store.Item` is close, but should be tightened/adjusted:

- **Keep** a "MLT is showing" flag, but consider modeling filter state as an enum to avoid boolean combinatorics:
  ```go
  type FilterMode int
  const (
      FilterNone FilterMode = iota
      FilterText
      FilterMLT
  )
  // searchActive remains separate: it means "typing in the input"
  filterMode FilterMode
  mltSeed    store.Item // or just ID+Title if you prefer
  ```
  This prevents impossible combos like `hasQuery() && moreLikeThisActive`.

- **Seed storage is not redundant.** You *cannot reliably recover the seed item later*:
  - the pool load replaces `items`, and the seed might not be present
  - cursor moves; highlighted item is not the seed
  - View needs stable "More like: ..." text even if results reorder
  - cross-encoder query needs seed text even after pool swap

- **You probably do not need an extra `mltQueryID` field** if you reuse your existing `queryID`:
  - Starting MLT should set `a.queryID = newQueryID()`
  - All pending async work from the prior mode becomes stale and is ignored
  - This matches your current "single active pipeline" architecture

- **If you want nesting (search -> MLT -> back to search)**, add a small stack:
  ```go
  type viewSnapshot struct {
      items      []store.Item
      embeddings map[string][]float32
      cursor     int

      filterMode FilterMode
      filterText string       // filterInput.Value()
      mltSeed    store.Item
  }
  viewStack []viewSnapshot
  ```
  Without this, you must choose between (a) disallowing MLT from search results, or (b) Esc always returning to chronological.

#### 2) New message types
You don't need new message *types* if your existing ones already support correlation.

- **Strong requirement:** every async-result message involved in pool/rerank must include `QueryID` and your handlers must ignore mismatches:
  ```go
  case SearchPoolLoaded:
      if msg.QueryID != a.queryID { return a, nil }
  case EntryReranked:
      if msg.QueryID != a.queryID { return a, nil }
  ```
- A `MoreLikeThisInitiated` message is not necessary in Bubble Tea -- key handlers can mutate state directly and return commands. Introducing it would mostly be stylistic.

#### 3) Mode interaction -- independent vs. sub-mode
MLT should be an **independent filter mode** (no text input), but you have a product decision:

- **Simplest v1**: allow `m` only from normal chronological view.
  Pros: no stack; minimal code.
  Cons: users can't pivot from a search result, which is a very common workflow.

- **Better UX with modest complexity**: add the snapshot stack and allow MLT from anywhere.
  Then Esc pops one level: MLT -> (back to search results) -> chronological.

- **"Pivot-to-chronological" compromise** (no stack): allow `m` from search results, but Esc always returns to chronological (by not overwriting the original saved state).
  Pros: avoids stack.
  Cons: breaks "back" expectations; harder to explain.

If you can afford it, the **stack is worth it**: it localizes complexity and prevents future refactors when you inevitably want "pivot from results".

#### 4) Save/restore pattern
Extending `clearSearch()` to handle MLT "works" short-term, but it conflates concepts.

Recommended refactor:
- `pushView()` before entering any filtered mode (search results or MLT results)
- `popView()` on Esc
- a small `resetQueryState()` helper for pending flags/spinner/queryEmbedding

Sketch:
```go
func (a *App) pushView() {
    snap := viewSnapshot{
        items:      append([]store.Item(nil), a.items...), // must copy because you sort in place
        embeddings: a.embeddings, // ok if treated immutable; deep copy only if you mutate it
        cursor:     a.cursor,
        filterMode: a.filterMode,
        filterText: a.filterInput.Value(),
        mltSeed:    a.mltSeed,
    }
    a.viewStack = append(a.viewStack, snap)
}

func (a *App) popView() {
    if len(a.viewStack) == 0 { return }
    snap := a.viewStack[len(a.viewStack)-1]
    a.viewStack = a.viewStack[:len(a.viewStack)-1]

    a.items = snap.items
    a.embeddings = snap.embeddings
    a.cursor = snap.cursor
    a.filterMode = snap.filterMode
    a.mltSeed = snap.mltSeed
    a.filterInput.SetValue(snap.filterText)
    a.resetQueryState()
}

func (a *App) resetQueryState() {
    a.queryEmbedding = nil
    a.embeddingPending = false
    a.searchPoolPending = false
    a.rerankPending = false
    a.statusText = ""
}
```
Then Esc is just "pop view" regardless of whether you were in text search results or MLT results.

#### 5) `handleKeyMsg` routing (where to check `moreLikeThisActive`)
Treat MLT as "normal navigation, but filtered", not as an input-capture layer.

A good ordering is:
1) `searchActive` (text input captures most keys)
2) pending states (`embeddingPending || searchPoolPending || rerankPending`) -- still allow Esc to cancel/pop and `q` to quit
3) filtered display modes (MLT/search-results) vs normal

Concretely:
- check `searchActive` first
- then handle "global" keys (`ctrl+c`, `q`, arrows/jk, Esc)
- then mode-specific keys (`/` to start typing, `m` to pivot)

**Position relative to `rerankPending`:** pending should generally be checked *before* "mode" so you don't accidentally block Esc/quit while a rerank is running. MLT doesn't need to be "above" pending; it's orthogonal.

#### 6) Cross-encoder reranking semantics
Using seed text as the cross-encoder "query" is **plausible** (doc-to-doc similarity), but there are practical concerns:

- Many cross-encoders are trained for **short query vs passage** (MS MARCO style). Feeding a whole article body as "query" can:
  - hit token limits (often 512 total tokens)
  - degrade scoring because the "query" side isn't query-like

Recommendations:
- Use **title + short summary** as the query side, and **truncate** aggressively:
  ```go
  func mltQueryText(seed store.Item) string {
      s := seed.Title
      if seed.Summary != "" {
          s += "\n" + seed.Summary
      }
      return truncateRunes(s, 400) // or token-based truncation if you have it
  }
  ```
- Rerank only **top-K** from cosine (e.g., 50-200) to control latency and cost.
- Consider skipping cross-encoder for MLT v1 if your cross-encoder is clearly "query->doc" oriented.

Also decide what to do with the seed item in results:
- **Pin seed at top** (if present), or
- **Exclude seed** from results (often cleaner for "more like this")

#### 7) Edge cases
- **Seed has no embedding:** don't silently ignore; set a brief status:
  - `statusText = "No embedding for selected item"` and clear it after a tick/timeout, or just leave it until next action.
  - Do not enter MLT mode.
- **Press `m` while MLT active:** allow it as "re-seed":
  - set `a.queryID = newQueryID()`
  - set `a.mltSeed = currently selected item`
  - set `a.queryEmbedding = embeddings[seed.ID]`
  - reset pending flags, rerank immediately, kick off pool load again
  - stale pool/rerank messages are ignored by `QueryID`
- **Pool loads but seed isn't in it:** totally possible (feeds differ, dedupe, time windows).
  - If you want the seed visible/pinned, inject it manually at index 0 (and ensure its embedding exists).
  - Otherwise, keep showing header "More like: ..." and just rank corpus results.
- **Cross-encoder scores seed low / unexpected ordering:** if you include the seed in candidates, pin or exclude it so the UI doesn't look "wrong". Also keep cosine score available as fallback if rerank results look degenerate.

#### 8) View rendering / UX
Header text like `More like: "Title..."` is a good baseline, but add clarity and key hints:

- Filter/header bar:
  - `More like: <truncated title>   (Esc back, m re-seed, / search)`
- Status/spinner:
  - reuse `statusText` + spinner while `searchPoolPending` or `rerankPending`
  - consider showing phase: `Loading corpus...`, `Reranking...`
- If you support chaining pivots, make `m` hint explicit in MLT mode: `m: pivot`.

Finally, make sure your "search result rendering differences" (e.g., hiding chronological time bands) are applied for MLT as well -- conceptually it's the same "filtered/sorted results" view.

**Implementation nucleus (what actually happens on `m`)**
- validate selection + embedding exists
- `pushView()` (or single-level save if you insist)
- set `filterMode = FilterMLT`, `mltSeed = seed`, `queryEmbedding = seedEmbedding`
- set `queryID = newQueryID()`
- immediate `rerankItemsByEmbedding()` (fast)
- start `loadSearchPool(queryID)` (sets `searchPoolPending = true`)
- when `SearchPoolLoaded(queryID)` arrives: replace items/embeddings, rerank by embedding again, then optionally `startReranking(mltQueryText(seed))`

> **Audit trail:** Synthesized by GPT-5 from Claude (internal) + Gemini-3 (external). Raw responses: `20260130_222554_claude_gemini3_gpt5.json`

### 1c. View Rendering

Below is an opinionated, concrete plan addressing filter bar content, status bar text, similarity ordering cues, key hints, inline scores, color/style differentiation, and View() branching.

#### 1) Filter bar content during "More Like This"

Make it a **static, contextual bar** (not editable). It answers: *"What am I looking at and why is it ordered this way?"*

**Format (single line):**
```
~ More like: {seedTitle...} | {seedSource}                          Sim {shown}/{total}
```

- **Prompt glyph**: `~` (ASCII) for maximum terminal/font compatibility.
- **Label**: `More like:` (plain language, not "related to" or algorithmic terms).
- **Seed title**: use **middle ellipsis** so you preserve both the start and end of headlines:
  - `Trump trial ... jury begins deliberations`
- **Include seed source**: `| Reuters` helps when titles are similar across sources.
- **Right side**: show mode + counts, e.g. `Sim 24/247` (shown vs corpus). If you only show top-K results, then `Sim 24` is fine, but showing "of total" keeps filtering transparent.

**Truncation strategy (width-safe):**
Compute available space after reserving the right-side counter.

Go-ish pseudocode:
```go
func RenderMoreLikeThisBar(seedTitle, seedSource string, shown, total, width int) string {
    leftPrefix := MoreLikePrompt.Render("~") + " " + MoreLikeLabel.Render("More like: ")
    right := MoreLikeRight.Render(fmt.Sprintf("Sim %d/%d", shown, total))

    // Reserve at least 1 space between left and right.
    avail := width - lipgloss.Width(leftPrefix) - lipgloss.Width(right) - 1
    if avail < 10 { avail = 10 }

    seed := seedTitle
    if seedSource != "" {
        seed = fmt.Sprintf("%s | %s", seedTitle, seedSource)
    }
    seed = truncateMiddleRunes(seed, avail) // adds "..." in the middle

    left := leftPrefix + MoreLikeSeed.Render(seed)
    return MoreLikeBar.Width(width).Render(
        lipgloss.PlaceHorizontal(width, lipgloss.Left, left) + right,
    )
}
```

**Interactive or static?**
**Static.** Editing doesn't make sense: the "query" is an item, and pivoting is done by selecting another item and pressing `m` again.

#### 2) Status bar text while loading/processing (statusText strings)

Keep language user-centered and non-technical. The user cares about: *finding* then *refining*.

Pipeline phases:

**(a) Embedding lookup / preparation:**
If it's truly fast, you can merge it with the next phase. If it can take noticeable time (cache miss / compute), show:
- `Finding similar items...`

Optionally include a short seed context:
- `Finding similar to "Fed holds rates..."...`

**(b) Cosine pre-ranking (instant):**
No separate message; it will flash. Treat it as part of (a).

**(c) Cross-encoder reranking (slow):**
- `Refining best matches...`

If you can report progress over the top-30 rerank, this is a big UX win without being "overly technical":
- `Refining best matches (12/30)...`

When complete:
- `statusText = ""` (return to normal status bar content + key hints)

#### 3) Indicating similarity ordering (not chronological)

**Time band headers:**
**Suppress them** in MLT mode. Bands like "Today / This Week" actively mislead when the list is similarity-sorted.

In `View()`:
```go
showBands := !(a.hasQuery() || a.filterMode == FilterMoreLike)
stream := RenderStream(a.items, a.cursor, a.width, contentHeight, showBands)
```

**Status bar left-side position text:**
Make the left-side counter explicitly similarity-mode:
- `Sim 3/24` or `3/24 (similar)`

If you also track corpus size:
- `Sim 3/24 of 247`

**Additional cue (optional, low-noise):**
The top bar already says "More like: ...". That plus suppressed bands is usually enough; don't change item rendering just to signal ordering.

#### 4) Key hints during "More Like This"

**Results-ready state (statusText == ""):**
Show mode-appropriate hints:

Suggested (wide terminals):
```
Sim 3/24  j/k:nav  Enter:open  m:pivot  /:search  Esc:back  r:refresh  q:quit
```

- **Yes, allow chaining**: `m:pivot` is a core strength of MLT.
- **Yes, allow `/`**: switching to text search should clear MLT state first.
- `Esc:back` should restore the previous (chronological) list.

On narrow widths, compress (you likely already do something similar):
```
Sim 3/24  j/k  Enter  m  /  Esc  r  q
```

**Loading state (statusText != ""):**
Your current pattern is "spinner + statusText". For MLT, strongly recommend still surfacing **at least cancel/back**:
- Left: `... Refining best matches (12/30)...`
- Right: `Esc:cancel`

This prevents users from feeling "stuck" during a 1-5s rerank.

#### 5) Similarity scores inline (e.g., `[0.87]`)

**Default: no numeric scores inline.** Reasons:
- Scores are not stable/meaningful across seeds; they create false precision.
- They steal columns from titles in 80-120 col terminals.
- The visible filter context ("More like: ..." + `Sim ...`) already makes the AI action explicit.

If you want to honor "AI as Tool, Never Master" with optional transparency, add a **toggle** rather than always-on:
- `S:score` toggles an inline prefix like `[87]` (percent, not float), or a subtle right-aligned score column.
- Keep it off by default; show the toggle only in MLT mode.

#### 6) Color/style differentiation (256-color codes)

Goal: text search feels like "input mode" (gray + pink); MLT feels like "pivot mode" (purple).

Recommended styles:

- **MLT bar background**: `53` (deep purple, distinct from gray 240 and from bright accents)
- **MLT bar foreground**: `255` (white)
- **MLT prompt `~`**: `141` (lilac), **bold**
- **MLT label ("More like:")**: `248` (light gray)
- **Seed text**: `255` (white)
- **Right-side `Sim ...`**: `252` (near-white) or `141` if you want it to pop

`styles.go` additions:
```go
var MoreLikeBar = lipgloss.NewStyle().
    Foreground(lipgloss.Color("255")).
    Background(lipgloss.Color("53")).
    Padding(0, 1)

var MoreLikePrompt = lipgloss.NewStyle().
    Foreground(lipgloss.Color("141")).
    Bold(true)

var MoreLikeLabel = lipgloss.NewStyle().
    Foreground(lipgloss.Color("248"))

var MoreLikeSeed = lipgloss.NewStyle().
    Foreground(lipgloss.Color("255"))

var MoreLikeRight = lipgloss.NewStyle().
    Foreground(lipgloss.Color("252"))
```

**Item-level styling**: keep unchanged. The mode bars are sufficient and preserve scan familiarity.

#### 7) View() detection/branching (state + exact logic)

**Don't overload `hasQuery()`.**
Keep `hasQuery()` strictly "text query exists." Add explicit mode.

Best structure: a small enum:
```go
type FilterMode int
const (
    FilterNone FilterMode = iota
    FilterText
    FilterMoreLike
)

type App struct {
    // existing
    searchActive bool
    filterInput  textinput.Model
    statusText   string

    // new
    filterMode FilterMode

    mltSeedID     string
    mltSeedTitle  string
    mltSeedSource string

    // to restore prior list
    savedItems  []Item
    savedCursor int

    // optional: total corpus count if items becomes filtered list
    corpusCount int
}
```

**View() branching (layout-stable):**
Reserve the top bar line whenever you are in *any* filtered mode (text results or MLT), regardless of `statusText`, to avoid list height jumping.

```go
func (a App) View() string {
    contentHeight := a.height - 1 // status bar

    if a.err != nil {
        contentHeight--
    }

    topBarVisible := a.searchActive || a.filterMode != FilterNone
    if topBarVisible {
        contentHeight--
    }

    showBands := a.filterMode == FilterNone && !a.searchActive
    stream := RenderStream(a.items, a.cursor, a.width, contentHeight, showBands)

    errorBar := ""
    if a.err != nil {
        errorBar = RenderErrorBar(...)
    }

    topBar := ""
    switch {
    case a.searchActive:
        topBar = a.renderSearchInput() // existing "/ + textinput"
    case a.filterMode == FilterMoreLike:
        topBar = RenderMoreLikeThisBar(a.mltSeedTitle, a.mltSeedSource, len(a.items), a.corpusCount, a.width)
    case a.filterMode == FilterText:
        topBar = RenderFilterBarWithStatus(a.filterInput.Value(), len(a.items), a.corpusCount, a.width, "")
    }

    var statusBar string
    if a.statusText != "" {
        statusBar = RenderSpinnerStatusWithCancel(a.spinner.View(), a.statusText, a.width) // ideally includes "Esc:cancel"
    } else {
        switch a.filterMode {
        case FilterMoreLike:
            statusBar = RenderStatusBarMoreLike(a.cursor, len(a.items), a.corpusCount, a.width)
        case FilterText:
            statusBar = RenderStatusBarWithFilter(a.cursor, len(a.items), a.corpusCount, a.width, a.loading)
        default:
            statusBar = RenderStatusBar(a.cursor, len(a.items), a.width, a.loading)
        }
    }

    return stream + errorBar + topBar + statusBar
}
```

**Mode conflict edge cases (and how to avoid them):**
- **`searchActive` and MLT simultaneously**: forbid by construction.
  - On `/` while in MLT: `clearMoreLikeThis()` then `enterSearchMode()`.
- **Press `m` while in text-search results**: decide explicitly.
  - Recommended: pressing `m` **switches to MLT mode** (clears text filter mode) but uses the currently selected item as seed (the seed is still a real item). This avoids "stacked filters" complexity.
- **Loading + mode switch**:
  - If rerank is in-flight and user hits `Esc`, cancel the in-flight command (or ignore results by checking a `requestID`), clear `statusText`, and restore saved list.
- **Restoring**:
  - `Esc` in MLT restores `savedItems/savedCursor` (chronological or prior view), not "whatever happens to be in memory."

Minimal clear function:
```go
func (a *App) clearMoreLikeThis() {
    a.filterMode = FilterNone
    a.mltSeedID, a.mltSeedTitle, a.mltSeedSource = "", "", ""
    a.statusText = ""
    if a.savedItems != nil {
        a.items = a.savedItems
        a.cursor = a.savedCursor
        a.savedItems = nil
    }
}
```

One additional "opinionated" improvement: keep the **MLT context bar visible even during rerank** (and reserve its line in height calc). It reduces disorientation and prevents the list from resizing mid-operation, which is one of the most noticeable UX papercuts in TUIs.

> **Audit trail:** Synthesized by GPT-5 from Claude (internal) + Grok-4 (external). Raw responses: `20260130_223145_claude_grok4_gpt5.json`

### 1d. Wiring

Below is the authoritative implementation plan for wiring the feature.

#### 1. App State Changes (`app.go`)

Add two fields to the `App` struct to track the mode and the anchor item's title for the UI.

```go
type App struct {
    // ... existing fields
    moreLikeThis      bool   // True when displaying similarity results
    moreLikeThisTitle string // Title of the anchor item for the header
}
```

#### 2. Key Binding (`handleKeyMsg`)

Place the key binding in the **normal mode** switch block. This ensures it respects the existing priority cascade (`searchActive` > `embeddingPending` > `rerankPending` > `Normal`).

```go
func (a App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    // ... existing guards (searchActive, embeddingPending, rerankPending) ...

    switch msg.String() {
    // ... existing cases (j, k, /, q) ...

    case "m":
        // 1. Validate cursor is on a valid item
        if len(a.items) == 0 || a.cursor >= len(a.items) {
            return a, nil
        }
        // 2. Start the flow
        return a.startMoreLikeThis()
    }
    return a, nil
}
```

#### 3. The Core Logic: `startMoreLikeThis`

This method orchestrates the transition. It preserves the original view, sets up the new query vector, performs an instant sort, and triggers background expansion.

```go
func (a App) startMoreLikeThis() (tea.Model, tea.Cmd) {
    selectedItem := a.items[a.cursor]

    // 1. Get Embedding
    // We expect items to have embeddings via the background worker.
    // If missing, we show an error rather than blocking on a synchronous embed.
    itemEmbedding, ok := a.embeddings[selectedItem.ID]
    if !ok || len(itemEmbedding) == 0 {
        a.err = fmt.Errorf("embedding pending for item: %s", selectedItem.ID)
        return a, nil
    }

    // 2. Save State (only if not already in a saved state)
    // This allows chaining 'm' multiple times while 'Esc' always returns to the
    // original chronological timeline.
    if a.savedItems == nil {
        a.savedItems = make([]store.Item, len(a.items))
        copy(a.savedItems, a.items)
        // Deep copy embeddings map to ensure isolation
        a.savedEmbeddings = make(map[string][]float32, len(a.embeddings))
        for k, v := range a.embeddings {
            a.savedEmbeddings[k] = v
        }
    }

    // 3. Update Search State
    a.moreLikeThis = true
    a.moreLikeThisTitle = selectedItem.Title
    a.queryID = newQueryID()       // Generate unique ID for this 'search' context
    a.queryEmbedding = itemEmbedding // Use item vector as query vector

    // 4. Instant Feedback: Sort current items immediately
    // This is valid because Passage-to-Passage cosine similarity is symmetric.
    a.rerankItemsByEmbedding()
    a.cursor = 0 // Reset cursor to top of new results

    // 5. Trigger Background Expansion & Refinement
    // We load the full pool to find similar items not currently visible.
    // We also trigger reranking using the item's text as the semantic query.
    anchorText := fmt.Sprintf("%s\n%s", selectedItem.Title, selectedItem.Summary)

    var cmds []tea.Cmd
    cmds = append(cmds, a.loadSearchPool(a.queryID)) // Fetch full corpus

    // Use the existing startReranking helper to manage batch vs single logic
    // passing the anchor text as the "query"
    rerankCmd := a.startReranking(anchorText)
    cmds = append(cmds, rerankCmd)

    return a, tea.Batch(cmds...)
}
```

#### 4. Logic Updates (`hasQuery` & `View`)

The `hasQuery` method is the central predicate for "are we in a results view?". It must return true for MLT mode so `View` suppresses time bands and `SearchPoolLoaded` knows to re-apply sorts.

```go
func (a App) hasQuery() bool {
    if a.moreLikeThis {
        return true
    }
    // Existing logic
    return a.filterInput.Value() != "" && !a.searchActive
}
```

**View Rendering:**
Update `View()` to display a specific header when in MLT mode.

```go
// In View():
if a.moreLikeThis {
    // Render a special header instead of the search bar
    header = fmt.Sprintf("~ More like: %s", truncate(a.moreLikeThisTitle, 60))
    // Render header block...
} else if a.hasQuery() {
    // Existing search results header...
}
```

#### 5. Reset Logic (`clearSearch`)

When `Esc` is pressed, we must clear the MLT flags alongside the search flags.

```go
func (a App) clearSearch() {
    // Existing clears
    a.filterInput.Reset()
    a.queryEmbedding = nil

    // New clears
    a.moreLikeThis = false
    a.moreLikeThisTitle = ""

    // Restore logic (already exists)
    if a.savedItems != nil {
        a.items = a.savedItems
        a.embeddings = a.savedEmbeddings
        a.savedItems = nil
        a.savedEmbeddings = nil
    }
}
```

#### 6. Handling Edge Cases & Stale Checks

The most critical edge case is the **Stale Check** in `handleEntryReranked` (for the Ollama/per-entry path). The current code likely checks `if msg.Query != a.filterInput.Value()`. In MLT mode, `filterInput` is empty, causing all rerank results to be discarded.

**Fix in `handleEntryReranked`:**

```go
func (a App) handleEntryReranked(msg EntryRerankedMsg) (tea.Model, tea.Cmd) {
    // 1. Strict QueryID check (Preferred)
    // Ensure all rerank commands pass the queryID generated in startMoreLikeThis
    if msg.QueryID != a.queryID {
        return a, nil // Discard stale result
    }

    // 2. Validation fallback
    // Only check filterInput match if we are NOT in MoreLikeThis mode
    if !a.moreLikeThis && msg.Query != a.filterInput.Value() {
         return a, nil
    }

    // ... proceed to update score ...
}
```

**Embed Item Task Mismatch:**
Using a "Passage" task embedding (the item) as a query against other "Passage" embeddings is valid. Cosine similarity is a geometric angle measurement; checking the angle between two "Passage" vectors correctly identifies how similar the documents are to each other.

#### Summary of Flow

1.  User presses `m` on "Article A".
2.  App checks if "Article A" has an embedding.
3.  App saves current timeline to `savedItems`.
4.  App sets `queryEmbedding` = `Article A.Embedding`.
5.  App runs `rerankItemsByEmbedding()` instantly (CPU sort). User sees immediate results from current list.
6.  App triggers `LoadSearchPool`.
7.  When `SearchPoolLoaded` returns, App replaces `items` with the full history and runs `rerankItemsByEmbedding()` again (automatically via existing handler).
8.  App triggers `BatchRerank` using "Article A Title" as text.
9.  When `RerankComplete` returns, items are re-ordered by the cross-encoder's high-precision similarity score.
10. User presses `Esc` to return to the original chronological view.

> **Audit trail:** Synthesized by Gemini-3 from Claude (internal) + Grok-4 (external). Raw responses: `20260130_222256_claude_grok4_gemini3.json`

### Verification & Testing

#### Unit Tests

1. **`TestStartMoreLikeThis_ValidItem`** -- Press `m` on an item with an embedding. Verify:
   - `moreLikeThis` (or `filterMode`) is set
   - `queryEmbedding` equals the selected item's embedding
   - `savedItems` contains a copy of the original items
   - `queryID` is set (non-empty)
   - Items are reordered by cosine similarity

2. **`TestStartMoreLikeThis_NoEmbedding`** -- Press `m` on an item without an embedding. Verify:
   - `moreLikeThis` remains false
   - An error is set (or `statusText` shows a message)
   - `savedItems` is not modified

3. **`TestStartMoreLikeThis_EmptyList`** -- Press `m` when `a.items` is empty. Verify:
   - No panic, no state change, returns nil cmd

4. **`TestStartMoreLikeThis_Chaining`** -- Press `m`, navigate to a result, press `m` again. Verify:
   - `savedItems` still contains the *original* chronological view (not the first MLT results)
   - `queryEmbedding` is updated to the new seed item's embedding
   - `queryID` has changed (old async messages will be discarded)

5. **`TestClearSearch_ResetsMLT`** -- Enter MLT mode, then trigger `clearSearch()`. Verify:
   - `moreLikeThis` is false
   - `moreLikeThisTitle` is empty
   - `queryEmbedding` is nil
   - Items are restored to the original `savedItems`

6. **`TestHasQuery_MLTMode`** -- Verify `hasQuery()` returns true when `moreLikeThis` is true (or `filterMode == FilterMLT`).

7. **`TestHandleEntryReranked_StaleCheck`** -- Send an `EntryReranked` message with a stale `queryID` during MLT mode. Verify it is discarded. Send one with the correct `queryID` and verify it is processed.

8. **`TestRemoveSeedItem`** -- Verify the seed item is excluded from results after cosine reranking.

#### Integration Test Scenarios

1. **Full MLT Pipeline** -- Set up an App with items and embeddings. Press `m` on item 3. Verify:
   - Immediate cosine sort produces a reordered list
   - `LoadSearchPool` command is returned
   - Simulating `SearchPoolLoaded` replaces items and re-sorts
   - Simulating `RerankComplete` produces final ordering
   - `statusText` is cleared after completion

2. **MLT -> Esc -> Verify Restore** -- Enter MLT, let the pipeline complete, press Esc. Verify items match the original chronological order exactly.

3. **MLT -> Text Search Transition** -- Enter MLT, press `/`, type a query. Verify:
   - MLT state is cleared
   - Search mode is entered cleanly
   - No stale MLT messages interfere

4. **Search -> MLT Transition** -- Perform a text search, then press `m` on a search result. Verify:
   - MLT activates with the selected item as seed
   - `savedItems` preserves the original chronological view (not search results)

#### Edge Cases to Verify

- Press `m` during `embeddingPending` -- should be blocked by the key cascade
- Press `m` during `rerankPending` -- should be blocked by the key cascade
- Press `m` when `searchActive` (typing in search box) -- `m` goes to textinput, not MLT
- `SearchPoolLoaded` arrives with a stale `queryID` after user pressed `m` twice quickly -- stale message discarded
- Item with cosine similarity 1.0 (the seed itself) is excluded from displayed results
- Very narrow terminal (80 cols) -- filter bar truncates gracefully without overflow

#### Manual Testing Steps

1. Launch Observer with `JINA_API_KEY` set
2. Wait for items to load and embeddings to populate
3. Navigate to an item with `j`/`k`, press `m`
4. Confirm the purple `~ More like:` bar appears at top with the seed title
5. Confirm the status bar shows `Sim X/Y` instead of the normal position counter
6. Confirm items are reordered (the seed's topic should dominate top results)
7. Press `m` on a result to chain -- confirm new seed is shown, results update
8. Press `Esc` -- confirm return to the original chronological view
9. Press `/` from MLT mode -- confirm clean transition to text search
10. Test with an item that has no embedding (e.g., just fetched) -- confirm error message

---

---

## Feature 2: Opt-in Ollama Rerank

### 2a. Backend Detection

### ANALYZE

**Response A (Claude)** focuses on the architectural high-level design.
*   **Strengths:** Correctly identifies the need to remove `log.Fatal` in `main.go`. Proposes a clean Enum strategy (`RerankBackend`) and suggests good locations for UI feedback (status bar and search filter).
*   **Weaknesses:** The implementation code is somewhat pseudo-code (e.g., checks empty strings manually rather than relying purely on the `Available()` interface). It glosses over the "Feature 2b" requirement (gating behavior based on the backend) and does not provide the concrete closure implementation for the Ollama path (`scoreEntry`), which is a complex part of the request.

**Response B (grok4)** focuses on robust implementation details.
*   **Strengths:** Provides excellent, production-ready Go code. It introduces a `Badge()` method on the enum for the UI, uses `context.WithTimeout` to satisfy the non-blocking startup requirement, and implements the specific closure factories (`makeOllamaScoreEntry`) required to wire the interface to `tea.Cmd`. It also includes OpenTelemetry attributes.
*   **Weaknesses:** It suggests putting logic in `internal/app/model.go`, while the context implies the main UI application logic lives in `internal/ui/app.go`.

**Comparison:**
Both agree on using an Enum for state and running detection once at startup. Response B is significantly stronger on the implementation details (timeouts, closures, helper methods), while Response A provides a good checklist of user-facing changes.

### SYNERGIZE

The unified solution will leverage Response B's technical implementation (Enums with methods, Context timeouts, Closure factories) while respecting the file structure implied by Response A and the prompt.

*   **Detection Strategy:** We will use Response B's strategy: Check Jina (fast/synchronous) -> Fallback to Ollama (async with 3s timeout) -> Default to None.
*   **Type Safety:** We will adopt the `RerankBackend` string enum from Response B, as it makes logging and debugging easier than integer iotas.
*   **UI Integration:** We will use the `Badge()` method approach (Response B) to keep the Status Bar code clean.
*   **Closures:** We will include the specific adapter code to turn the `Reranker` interface into the `func(...) tea.Cmd` closures the UI expects.

### UNIFY

Here is the complete design and implementation for the backend detection system.

#### 1. Define Backend Types and Enum
Create a new file (or append to `internal/ui/types.go` if it exists, otherwise `internal/ui/backend.go`) to handle the backend identity, UI representation, and observability attributes.

**File: `internal/ui/backend.go`**
```go
package ui

import (
	"go.opentelemetry.io/otel/attribute"
)

// RerankBackend identifies the active reranking service.
type RerankBackend string

const (
	RerankNone   RerankBackend = "none"
	RerankJina   RerankBackend = "jina"
	RerankOllama RerankBackend = "ollama"
)

// Badge returns a compact 3-character tag for the status bar.
func (b RerankBackend) Badge() string {
	switch b {
	case RerankJina:
		return "[J]" // Jina
	case RerankOllama:
		return "[O]" // Ollama
	default:
		return "[-]" // None
	}
}

// Attributes returns OpenTelemetry attributes for the backend.
func (b RerankBackend) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("rerank.backend", string(b)),
	}
}
```

#### 2. Update App Struct and Initialization
We must modify the `App` struct to store the backend enum and implement the detection logic. The detection logic uses a `context.WithTimeout` to ensure the application starts within the 3-5 second window, even if Ollama is hanging.

**File: `internal/ui/app.go`**

```go
package ui

import (
	"context"
	"time"

	"github.com/observer/internal/rerank"
	"go.opentelemetry.io/otel/attribute"
	// ... other imports
)

type App struct {
	// ... existing fields ...
	rerankBackend RerankBackend
	batchRerank   func(query string, docs []string, queryID string) tea.Cmd
	scoreEntry    func(query string, doc string, index int, queryID string) tea.Cmd
}

// NewApp creates the application and detects the reranking backend.
// This runs once at startup.
func NewApp(cfg AppConfig, logger *observability.Logger) *App {
	app := &App{
		// ... existing initialization ...
		rerankBackend: RerankNone,
	}

	// Backend Detection Logic
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 1. Try Jina (Preferred)
	// Requires API Key. Check is fast (local string check + http ping if implemented).
	if cfg.JinaAPIKey != "" {
		jina := rerank.NewJinaReranker(cfg.JinaAPIKey, "jina-embeddings-v3")
		if jina.Available() {
			app.rerankBackend = RerankJina
			app.batchRerank = makeBatchRerankClosure(jina)
			logger.Info(ctx, "Reranking backend selected", 
				attribute.String("backend", "jina"), 
				attribute.String("model", jina.Name()))
		}
	}

	// 2. Fallback to Ollama if Jina failed
	if app.rerankBackend == RerankNone {
		// Use a goroutine to check Ollama availability without blocking indefinitely,
		// though we wait up to the context timeout.
		ollama := rerank.NewOllamaReranker(cfg.OllamaURL) // e.g. "http://localhost:11434"
		
		availableCh := make(chan bool, 1)
		go func() {
			availableCh <- ollama.Available() // Pings /api/tags
		}()

		select {
		case isAvailable := <-availableCh:
			if isAvailable {
				app.rerankBackend = RerankOllama
				// Wire the sequential scorer for Ollama
				app.scoreEntry = makeScoreEntryClosure(ollama)
				logger.Info(ctx, "Reranking backend selected", 
					attribute.String("backend", "ollama"), 
					attribute.String("model", ollama.Name()))
			}
		case <-ctx.Done():
			logger.Warn(ctx, "Reranking backend detection timed out; disabling reranking")
		}
	}

	// 3. Final State: None
	if app.rerankBackend == RerankNone {
		logger.Warn(ctx, "No reranking backend available")
	}

	return app
}

// --- Closure Factories ---

// makeBatchRerankClosure adapts the Reranker interface for Jina (Batch)
func makeBatchRerankClosure(r rerank.Reranker) func(string, []string, string) tea.Cmd {
	return func(query string, docs []string, queryID string) tea.Cmd {
		return func() tea.Msg {
			scores, err := r.Rerank(context.Background(), query, docs)
			if err != nil {
				return RerankErrorMsg{Err: err, QueryID: queryID}
			}
			return RerankCompleteMsg{Scores: scores, QueryID: queryID}
		}
	}
}

// makeScoreEntryClosure adapts the Reranker interface for Ollama (Sequential/Single)
func makeScoreEntryClosure(r rerank.Reranker) func(string, string, int, string) tea.Cmd {
	return func(query string, doc string, index int, queryID string) tea.Cmd {
		return func() tea.Msg {
			// Rerank single document
			scores, err := r.Rerank(context.Background(), query, []string{doc})
			if err != nil {
				return ScoreEntryErrorMsg{Err: err, QueryID: queryID, Index: index}
			}
			if len(scores) == 0 {
				return ScoreEntryErrorMsg{Err: fmt.Errorf("no score returned"), QueryID: queryID, Index: index}
			}
			return ScoreEntryCompleteMsg{
				Score:   scores[0], // Access first score
				Index:   index,
				QueryID: queryID,
			}
		}
	}
}
```

#### 3. Cleanup Main
Remove the fatal requirement for the Jina key.

**File: `cmd/observer/main.go`**

```go
func main() {
    // ... config loading ...
    
    // REMOVE THIS BLOCK:
    // if os.Getenv("JINA_API_KEY") == "" {
    //     log.Fatal("JINA_API_KEY environment variable is required")
    // }

    // Load config (Jina key is now optional)
    cfg := loadConfig() 
    
    logger := observability.NewLogger()
    
    // App.NewApp handles detection internally now
    model := ui.NewApp(cfg, logger)

    p := tea.NewProgram(model)
    if _, err := p.Run(); err != nil {
        // ...
    }
}
```

#### 4. Update Status Bar
Display the backend badge.

**File: `internal/ui/status_bar.go`**

```go
func (m Model) ViewStatusBar() string {
    // ... existing status bar logic ...
    
    // Retrieve the badge from the active backend
    // Assuming 'm.app' gives access to the App struct, or Model embeds App
    backendBadge := m.app.rerankBackend.Badge() 
    
    // Style the badge (optional: gray for None, green for Jina, yellow for Ollama)
    badgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
    
    // Append to status bar string. 
    // Example: "  ...  | CPU: 2% | [J]"
    status := fmt.Sprintf("%s %s", existingContent, badgeStyle.Render(backendBadge))
    
    return status
}
```

#### 5. Handling Feature 2b (Gating)
When implementing the search logic (Feature 2b), you can now gate the auto-rerank behavior using the enum.

**File: `internal/ui/app.go` (Method: `startReranking`)**

```go
func (a *App) startReranking(query string, docs []string, queryID string) tea.Cmd {
    // Feature 2b: Skip auto-rerank if using local LLM (too slow for type-ahead)
    if a.rerankBackend == RerankOllama {
        return nil // User must manually trigger 'R' to rerank
    }

    if a.rerankBackend == RerankNone {
        return nil
    }

    // Existing closure logic
    if a.batchRerank != nil {
        return a.batchRerank(query, docs, queryID)
    }
    // ...
}
```

### 2b. Rerank Gating

Here is the synthesized solution for the Observer rerank gating system.

### ANALYZE

Both responses correctly identify the core requirement: splitting the execution path based on the active backend (`RerankJina` vs. `RerankOllama`) inside the search pipeline handlers.

**Response A (Claude)** focuses heavily on the UX flow. It correctly identifies the two specific message handlers (`QueryEmbedded` and `SearchPoolLoaded`) that trigger the current automatic behavior. It proposes a clean string logic for the UI hint and repurposes the `r` key contextually. However, its code snippets are somewhat fragmented, and it glosses over the "Refresh" fallback logic.

**Response B (Grok4)** provides a more structural approach. It introduces a helper method `AutoReranks()` on the backend enum, which cleans up the main update loop. It also explicitly handles the `Esc` cancellation logic (ensuring `rerankDone` resets to `false`) and defines the fallback behavior for `r` (refreshing the search vs. reloading the DB).

**Key Synergies & Corrections:**
*   **Agreement:** Both agree on adding a `rerankDone` boolean to the App struct.
*   **Correction:** Response A assumes clearing `statusText` inside the update loop is sufficient, but Response B correctly notes we need to ensure the "searching" state is cleared specifically for the manual path.
*   **Missing Piece:** Neither explicitly details how to handle the `refresh` logic if the user presses `r` *after* a deep rerank is already complete. The unified solution must clarify that `r` essentially toggles: if "dirty" (cosine only), it reranks; if "clean" (reranked), it re-fetches.

### SYNERGIZE

The unified solution combines the specific handler targeting of Response A with the structural robustness of Response B.

1.  **Architecture:** We will use Response B's `AutoReranks()` helper to keep the `Update` loop clean.
2.  **State Management:** We will implement `rerankDone` (from both) but also ensure it resets correctly on `Esc` (Response B) and new searches.
3.  **UI Logic:** We will use Response A's logic for the filter bar hint but refine the condition: the hint only appears if we have a query, use Ollama, *and* haven't reranked yet.
4.  **Keybinding Strategy:** We will implement a strict priority queue for the `r` key:
    1.  If `Ollama` + `!rerankDone` + `!pending`: **Trigger Deep Rerank**.
    2.  Otherwise: **Trigger Refresh** (re-run search or reload DB).

### UNIFY

Here is the complete design and implementation.

#### 1. Data Structures & Helpers

First, we modify the `App` struct to track rerank status and add a helper method to the `RerankBackend` enum to determine behavior.

**File:** `internal/ui/app.go` (and where `RerankBackend` is defined)

```go
// In your backend definition file
func (b RerankBackend) AutoReranks() bool {
    // Jina is fast enough to auto-run; Ollama is too slow.
    return b == RerankJina
}

// In internal/ui/app.go
type App struct {
    // ... existing fields ...
    rerankDone    bool // NEW: tracks if deep rerank finished for current query
    // ...
}
```

#### 2. Search Pipeline Handlers

We must modify the two entry points where the search pool and embeddings converge. This is where we fork the logic: Jina continues automatically, Ollama stops and waits.

**File:** `internal/ui/app.go` (`Update` method)

```go
// Inside func (a *App) Update(msg tea.Msg) ...

case QueryEmbedded: // Message sent when embedding API returns
    // ... existing staleness checks ...
    a.queryEmbedding = msg.Embedding
    
    // Only proceed if we have both pieces of data
    if !a.searchPoolPending {
        a.rerankItemsByEmbedding() // Fast cosine sort (~3ms)
        a.rerankDone = false       // Reset status for new results
        
        if a.rerankBackend.AutoReranks() {
            // Jina Path: Auto-start
            return a.startReranking(msg.Query)
        }
        
        // Ollama Path: Stop here. Clear status to hide spinner.
        a.statusText = "" 
        return a, nil
    }

case SearchPoolLoaded: // Message sent when DB items are loaded
    // ... existing staleness checks ...
    a.searchPool = msg.Pool
    
    // Only proceed if we have both pieces of data
    if len(a.queryEmbedding) > 0 {
        a.rerankItemsByEmbedding() // Fast cosine sort (~3ms)
        a.rerankDone = false       // Reset status for new results
        
        if a.rerankBackend.AutoReranks() {
            // Jina Path: Auto-start
            return a.startReranking(a.filterInput.Value())
        }
        
        // Ollama Path: Stop here. Clear status to hide spinner.
        a.statusText = "" 
        return a, nil
    }
```

#### 3. Key Handler (`r`)

We repurpose the `r` key. In "Normal Mode" (no query), it refreshes the DB. In "Search Mode" (has query), it decides between Deep Rerank and Refresh based on the current state.

**File:** `internal/ui/app.go` (`Update` method)

```go
case tea.KeyMsg:
    switch msg.String() {
    case "r":
        // 1. If we are reranking, ignore input (or let global handler catch it)
        if a.rerankPending {
            return nil
        }

        // 2. Check if we are in "Search Mode" (results visible)
        if a.hasQuery() {
            // 3. Gating Logic: If Ollama + Not Done Yet -> Manual Deep Rerank
            if a.rerankBackend == RerankOllama && !a.rerankDone {
                return a.startReranking(a.filterInput.Value())
            }
            
            // 4. Otherwise (Jina, or Ollama already done) -> Standard Refresh
            // This re-runs the search to fetch new items from DB
            return a.refreshSearch() 
        }

        // 5. Normal Mode -> Reload all items from DB
        return a.reloadAllItems()
    }
```

#### 4. Completion & Cancellation Handling

We need to set the `rerankDone` flag when the process finishes, and ensure `Esc` allows the user to bail out safely.

**File:** `internal/ui/app.go`

```go
// When reranking finishes (Jina or Ollama)
case RerankCompleteMsg:
    a.rerankPending = false
    a.statusText = ""
    a.rerankDone = true // Mark as complete so 'r' becomes Refresh again
    a.results = msg.Results
    return a, nil

// When user presses Esc
case "esc":
    if a.rerankPending {
        // Cancel the context/operation
        a.cancelReranking() 
        a.rerankPending = false
        a.statusText = ""
        a.rerankDone = false // It wasn't finished, so 'r' remains "Deep Rerank"
        return a, nil
    }
    // ... existing clear search logic ...
```

#### 5. UI Hint (Filter Bar)

Finally, we display the visual cue. The hint is appended to the result count in the filter bar, but only when relevant.

**File:** `internal/ui/stream.go` (or wherever `RenderFilterBar` is defined)

```go
func (a *App) View() string {
    // ... 
    if a.hasQuery() {
        // Base filter text
        filterText := fmt.Sprintf("/ %s  %d results", a.filterInput.Value(), len(a.results))
        
        // Append Hint if: Backend is Ollama, Rerank NOT done, NOT currently processing
        if a.rerankBackend == RerankOllama && !a.rerankDone && !a.rerankPending {
            // Use a subtle style (dimmed/gray)
            hint := subtleStyle.Render(" — Press 'r' to deep rerank")
            filterText += hint
        }
        
        // ... render bar ...
    }
    // ...
}
```

### Summary of Behavior

1.  **Jina User:** Types query -> Cosine (immediate) -> Auto Rerank (1s, spinner) -> Final Results.
2.  **Ollama User:** Types query -> Cosine (immediate) -> **Stops**.
    *   **UI:** Shows results sorted by vector similarity.
    *   **Hint:** Filter bar reads `/ query 30 results — Press 'r' to deep rerank`.
    *   **Action:** User presses `r`.
    *   **State:** UI blocks, spinner appears ("Reranking..."), `rerankPending=true`.
    *   **Completion:** Rerank finishes, list updates, `rerankDone=true`, hint disappears.
    *   **Refinement:** If user presses `r` *again*, it triggers a standard data refresh.

### 2c. Listwise Reranking

### ANALYZE

**Response A (Claude)** correctly identifies that the specific `Qwen3-Reranker` model is trained for pairwise inference and suggests switching to a general instruction model (like `qwen3:8b`). It proposes a raw prompt injection strategy (`<|im_start|>`) via the `/api/generate` endpoint. However, raw prompt templates are brittle; if the user changes the model, the template might break. Its fallback mechanism (uniform 0.5 scores) is poor because it destroys the original ranking order provided by the source (e.g., time-based or BM25).

**Response B (grok4)** provides a significantly more robust implementation. It uses the `/api/chat` endpoint, allowing Ollama to handle the prompt templating (ChatML vs. Llama3) automatically. It suggests `qwen2.5:7b`, which is currently superior to Qwen3/Llama3.1 for instruction following and JSON adherence in the 7B parameter class. Crucially, Response B includes a "Linear Decay" fallback—if the model fails, it returns scores that preserve the original list order (1.0, 0.98, 0.96...) rather than a flat list. It also includes specific logic for "JSON scraping" using Regex if strict JSON unmarshalling fails, which is essential for local LLMs that often include conversational filler ("Here is the JSON...").

**Comparison:**
*   **Prompting:** Response B is superior (Chat API > Raw Template).
*   **Parsing:** Response B is superior (Multi-stage regex fallback).
*   **Fallback Logic:** Response B is superior (Linear decay > Uniform score).
*   **Integration:** Response A explains the UI closure integration slightly better, but Response B's code is more complete.

### SYNERGIZE

The unified solution will adopt **Response B's architectural core** (using `/api/chat`, linear decay fallback, and robust regex parsing) but incorporates specific refinements from **Response A**:
1.  **Headline Truncation:** Response A suggested truncating long headlines to 200 chars. This is vital to ensure 30-50 documents fit in the context window without overflow, which Response B didn't explicitly enforce in the loop.
2.  **Model Selection:** We will stick with Response B's recommendation of `qwen2.5:7b-instruct` as it is the current state-of-the-art for small-model instruction following.
3.  **Integration Path:** We will combine Response B's struct implementation with Response A's explanation of how to wire this into the `cfg.BatchRerank` closure in `main.go`.

The final code will utilize a "tolerant" JSON parser that attempts standard unmarshalling first, then falls back to finding a JSON array via Regex, and finally falls back to the linear decay method if the model output is unusable.

### UNIFY

Here is the complete design and implementation for the Listwise Reranker.

#### 1. Design Decisions

*   **Model:** `qwen2.5:7b-instruct`. It has a 32k context window and follows JSON constraints significantly better than Llama 3.1 8B.
*   **Endpoint:** `/api/chat`. This abstracts the prompt template (ChatML, Alpaca, etc.), making the code model-agnostic.
*   **Prompting Strategy:** Single prompt requesting a JSON array of `{index, score}` objects.
*   **Candidate Limit:** 50 items.
    *   50 headlines * ~30 tokens = 1,500 tokens.
    *   System prompt + overhead = ~200 tokens.
    *   Total ~1,700 tokens, well within standard 4k/8k context windows.
*   **Fallback Strategy:** "Linear Decay". If the model fails or produces bad JSON, we generate scores that degrade slightly (1.0 -> 0.99 -> 0.98). This preserves the original sort order (usually Recency or simple keyword match) rather than randomizing it or flattening it to 0.5.

#### 2. Implementation (`internal/rerank/ollama_listwise.go`)

```go
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ListwiseReranker implements the Reranker interface using a single
// context-aware prompt to rank multiple documents simultaneously.
type ListwiseReranker struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewListwiseReranker creates a reranker that uses Ollama's /api/chat.
// Recommended model: "qwen2.5:7b-instruct"
func NewListwiseReranker(baseURL, model string) *ListwiseReranker {
	if model == "" {
		model = "qwen2.5:7b-instruct"
	}
	// Normalize URL
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &ListwiseReranker{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: 45 * time.Second, // Generous timeout for batch processing
		},
	}
}

func (r *ListwiseReranker) Name() string {
	return fmt.Sprintf("ollama-listwise-%s", r.model)
}

// Available checks if the specific model is loaded/pullable in Ollama
func (r *ListwiseReranker) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", r.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false
	}

	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}

	// Check for substring match (handles :latest vs specific tags)
	for _, m := range payload.Models {
		if strings.Contains(m.Name, r.model) {
			return true
		}
	}
	return false
}

func (r *ListwiseReranker) Rerank(ctx context.Context, query string, documents []string) ([]Score, error) {
	if len(documents) == 0 {
		return []Score{}, nil
	}

	// 1. Prepare batch (Top 50 max to respect context/latency)
	const maxBatchSize = 50
	processCount := len(documents)
	if processCount > maxBatchSize {
		processCount = maxBatchSize
	}
	
	candidates := documents[:processCount]

	// 2. Construct Prompt
	systemPrompt := `You are a relevance ranking system.
1. Analyze the query and the numbered list of headlines.
2. Assign a relevance score between 0.0 (irrelevant) and 1.0 (highly relevant) to each headline.
3. Return ONLY a JSON array of objects with "index" and "score" keys.
4. The "index" must match the provided list number.
5. Do not include explanations or markdown.`

	userPrompt := buildUserPrompt(query, candidates)

	// 3. Execute Request
	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	}

	responseBody, err := r.callOllama(ctx, messages)
	
	// 4. Handle Failures with Graceful Fallback
	if err != nil {
		slog.Warn("listwise rerank failed, using linear decay fallback", "error", err)
		return linearDecayScores(len(documents)), nil
	}

	// 5. Parse JSON
	scores, err := parseRobustJSON(responseBody, processCount)
	if err != nil {
		slog.Warn("json parse failed, using linear decay fallback", "error", err, "raw", responseBody)
		return linearDecayScores(len(documents)), nil
	}

	// 6. Handle "Tail" documents (those beyond maxBatchSize)
	// If we had 60 docs but only ranked 50, the last 10 get very low scores
	finalScores := make([]Score, len(documents))
	copy(finalScores, scores)
	
	// Fill the rest with near-zero scores
	for i := processCount; i < len(documents); i++ {
		finalScores[i] = Score{Index: i, Score: 0.01}
	}

	return finalScores, nil
}

// callOllama handles the HTTP POST to /api/chat
func (r *ListwiseReranker) callOllama(ctx context.Context, messages []map[string]string) (string, error) {
	reqData := map[string]interface{}{
		"model":    r.model,
		"messages": messages,
		"stream":   false,
		"options": map[string]interface{}{
			"temperature": 0.0, // Deterministic
			"num_ctx":     8192,
		},
	}

	bodyBytes, _ := json.Marshal(reqData)
	req, err := http.NewRequestWithContext(ctx, "POST", r.baseURL+"/api/chat", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status code %d", resp.StatusCode)
	}

	var response struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}

	return response.Message.Content, nil
}

// buildUserPrompt formats the query and documents list
func buildUserPrompt(query string, docs []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Query: %s\n\nHeadlines:\n", query))
	for i, doc := range docs {
		// Truncate overly long docs to save tokens/confusion
		cleanDoc := doc
		if len(cleanDoc) > 200 {
			cleanDoc = cleanDoc[:200] + "..."
		}
		// Escape newlines to keep list format clean
		cleanDoc = strings.ReplaceAll(cleanDoc, "\n", " ")
		sb.WriteString(fmt.Sprintf("%d: %s\n", i, cleanDoc))
	}
	return sb.String()
}

// parseRobustJSON attempts multiple strategies to extract the scores
func parseRobustJSON(content string, expectedCount int) ([]Score, error) {
	var scores []Score

	// Strategy A: Direct Unmarshal
	// Clean markdown code blocks if present
	clean := strings.TrimSpace(content)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	
	if err := json.Unmarshal([]byte(clean), &scores); err == nil {
		if validateScores(scores, expectedCount) {
			return scores, nil
		}
	}

	// Strategy B: Regex extraction of the array
	// Matches [...] across newlines
	arrayRegex := regexp.MustCompile(`\[[\s\S]*\]`)
	match := arrayRegex.FindString(clean)
	if match != "" {
		if err := json.Unmarshal([]byte(match), &scores); err == nil {
			if validateScores(scores, expectedCount) {
				return scores, nil
			}
		}
	}

	return nil, fmt.Errorf("unable to parse valid scores from response")
}

// validateScores ensures the model didn't hallucinate indices
func validateScores(scores []Score, expectedCount int) bool {
	if len(scores) == 0 {
		return false
	}
	// Check strictly that we have indices 0 to N? 
	// Or just ensure they are valid. Loose validation is safer.
	for _, s := range scores {
		if s.Index < 0 || s.Index >= expectedCount {
			return false
		}
	}
	return true
}

// linearDecayScores creates a fallback that preserves original order
// e.g. 0: 1.0, 1: 0.98, 2: 0.96 ...
func linearDecayScores(count int) []Score {
	scores := make([]Score, count)
	step := 1.0 / float32(count+1)
	current := float32(1.0)
	
	for i := 0; i < count; i++ {
		scores[i] = Score{Index: i, Score: current}
		current -= step
		if current < 0 {
			current = 0
		}
	}
	return scores
}
```

#### 3. Integration (`cmd/observer/main.go`)

This integrates with the existing UI batch reranking path.

```go
// Inside your main setup or configuration loading
if cfg.RerankBackend == "ollama" {
    // Initialize the listwise reranker
    // Note: User must have run `ollama pull qwen2.5:7b-instruct`
    ollamaReranker := rerank.NewListwiseReranker("http://localhost:11434", "qwen2.5:7b-instruct")

    if ollamaReranker.Available() {
        // Wire into the UI's BatchRerank hook
        // This closure adapts the Reranker interface to the UI Command pattern
        cfg.BatchRerank = func(query string, docs []string, queryID string) tea.Cmd {
            return func() tea.Msg {
                scores, err := ollamaReranker.Rerank(context.Background(), query, docs)
                if err != nil {
                    // Log error but don't crash UI
                    slog.Error("Rerank failed", "err", err)
                    return nil 
                }
                
                // Return the standard message expected by the UI
                return ui.RerankCompleteMsg{
                    QueryID: queryID,
                    Scores:  scores,
                }
            }
        }
    } else {
        slog.Warn("Ollama listwise model not found, reranking disabled")
    }
}
```

#### 4. Key Advantages of This Implementation

1.  **Speed:** Reduces inference from ~32s (30 requests) to ~4-6s (1 request).
2.  **Stability:** The "Linear Decay" fallback ensures that even if the LLM crashes or outputs garbage, the user still sees a list in a logical order (the original retrieval order), preventing UI breakage.
3.  **Robustness:** The regex parsing strategy handles the conversational "chattiness" of local models (e.g., "Here is your JSON:") without failing.
4.  **Hardware Aware:** Caps candidates at 50 to prevent OOM errors on consumer GPUs with 8-12GB VRAM while keeping response times interactive.

### 2d. Progress Streaming

### ANALYZE
Both responses address the core task effectively: updating `statusText` in `handleEntryReranked()` for live progress via Bubble Tea's re-rendering. Key agreements include:
- Progress counter `(N/Total)`: Both implement it directly.
- ETA estimation: Both yes, using `time.Since(a.searchStart)` for per-item average * remaining.
- Progress bar: Both yes, using Unicode blocks (█/░).
- Observability: Both suggest milestone events/logs (A: first item; B: 50%/finish).
- No major code changes beyond `handleEntryReranked()` and helpers.

Disagreements:
- Live reordering: A suggests optional "every 5 items" with `applyPartialScores()` for a "streaming leaderboard"; B firmly rejects it to avoid "list jitter" and cursor confusion during navigation. B's stance aligns better with task concerns (user navigation allowed, cursor stability critical).
- Code structure: A embeds logic inline with simple `fmt.Sprintf`; B uses reusable helpers (`generateProgressBar`, `updateRerankStatus`) with better precision (e.g., `math.Round`, clamping).
- Status format: A includes truncated query (`"Deep reranking \"X\"..."`); B omits query (`"Reranking [bar] N/Total (Xs rem)"`). Original context implies query inclusion (`"Reranking \"X\"..."`).
- Observability details: A emits on first item (good for model load latency); B on 50%/finish with structured `slog`.
- Cursor handling: Only B explicitly addresses via reset-to-top in `applyScoresAsOrder()` post-rerank.
- Validation: B adds `msg.QueryID != a.currentQueryID` check (critical for stale messages, implied in A's "stale checks" comment but not shown).
- Minor flaws: A uses `searchStart` but calls it `rerankStartTime`; labels it "Deep reranking" (mismatch with context's "Reranking"); assumes `a.rerankQuery`. B assumes `a.rerankScores` pre-allocates and `a.currentQueryID`; uses `slog.Info` (assumes import). No factual errors in logic/ETA math, but B's bar is more robust (rounding/clamping). Neither emits on start (easy add).

Overall, both are strong/correct; no logical flaws beyond optional live-reorder (risky).

### SYNERGIZE
- **A's strengths**: Phased rollout rationale (progress first), query truncation for context, first-item observability (unique insight for Ollama cold-start latency), optional live-reorder idea (but we'll skip per B/task risks).
- **B's strengths**: Superior modularity (helpers prevent duplication), precise progress bar (`math.Round`/clamps), QueryID validation, cursor reset logic, 50%/finish milestones, clean UX summary.
- **Complements**: Merge B's helpers/structure with A's query inclusion/first-event; reject live-reorder for stability (unifies on "wait till end"); combine observability (first + 50% + finish); use B's ETA/bar precision; reset cursor to top (addresses task's "cursor stability problem"); status format blending both (`"Reranking \"X\" [bar] N/Total (~Xs rem)"`).
- **Unified decisions**: Progress+bar+ETA (must-have); no live-reorder; observability at 1/50%/100%; helpers for cleanliness; validate QueryID; reset cursor.

### UNIFY
## Live Progress Streaming Design for Ollama Reranking

### Design Decisions
1. **Progress count `(N/Total)`**: Yes, precise and immediate feedback in `statusText`.
2. **ETA estimation**: Yes, using elapsed time / scored items * remaining. Shown only after 1+ items (~Xs rem); rounded to seconds.
3. **Live reordering**: No — wait until all scores arrive. **Rationale**: Avoids disorienting "list jitter" during user navigation (j/k allowed); keeps cursor stable on current item until final snap-to-relevance.
4. **Reorder frequency**: N/A (batch-only).
5. **Visual progress bar**: Yes — compact 10-char Unicode `[████░░░░░░]` for at-a-glance status. Fits Bubble Tea status bar perfectly (spinner + text).
6. **Observability events**: Emit at milestones (1st item for cold-start latency, 50% progress, completion) using structured logs (e.g., `slog` or `otel`). Track total duration/items.

**Status text format** (spinner auto-shows if non-empty):
- `"Reranking \"climate\" [████░░░░░░] 7/30 (~21s rem)"`
- Early: `"Reranking \"climate\" [░░░░░░░░░░] 0/30"`
- Uses `truncateRunes(query, 20)` for brevity (add if missing).

**UX Flow**:
- 'r' pressed → Batch cmds fire → Static spinner briefly.
- Per `EntryReranked` → Progress/bar/ETA updates instantly (View re-renders).
- Done → `applyScoresAsOrder()`, status clears, cursor resets to #1 (top relevance).

### Implementation

#### 1. New Helper Functions
Add to `App` methods (in `internal/ui/app.go`). Assumes `import ("fmt"; "math"; "strings"; "time"; "log/slog")` and `truncateRunes(s string, n int) string` utility (implement if missing: trim runes to n chars).

```go
// generateProgressBar returns "[████░░░░░░]" (width=10).
func (a *App) generateProgressBar(current, total, width int) string {
	if total == 0 {
		return fmt.Sprintf("[%s]", strings.Repeat("░", width))
	}
	percent := float64(current) / float64(total)
	filled := int(math.Round(percent * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return fmt.Sprintf("[%s%s]",
		strings.Repeat("█", filled),
		strings.Repeat("░", width-filled),
	)
}

// updateRerankStatus sets formatted statusText with bar + count + ETA.
func (a *App) updateRerankStatus(query string) {
	count := a.rerankProgress
	total := len(a.rerankEntries)
	bar := a.generateProgressBar(count, total, 10)

	var etaMsg string
	if count > 0 {
		elapsed := time.Since(a.searchStart)
		avgPerItem := elapsed / time.Duration(count)
		remaining := avgPerItem * time.Duration(total-count)
		etaMsg = fmt.Sprintf(" (~%ds rem)", int(remaining.Seconds()))
	}

	truncQuery := truncateRunes(query, 20)
	a.statusText = fmt.Sprintf("Reranking \"%s\" %s %d/%d%s",
		truncQuery, bar, count, total, etaMsg)
}
```

#### 2. Modified `handleEntryReranked()`
Replace existing handler. Calls helper; validates `QueryID`; emits milestones.

```go
func (a App) handleEntryReranked(msg EntryReranked) (tea.Model, tea.Cmd) {
	// Discard stale/cancelled results
	if msg.QueryID != a.queryID {  // Assume a.queryID holds current search ID
		return a, nil
	}

	// Store score (assume a.rerankScores pre-allocated to len(a.rerankEntries))
	if msg.Err == nil && msg.Index >= 0 && msg.Index < len(a.rerankScores) {
		a.rerankScores[msg.Index] = msg.Score
	}
	a.rerankProgress++

	total := len(a.rerankEntries)

	// Observability milestones
	if a.rerankProgress == 1 {
		slog.Info("Reranking first item scored",
			"queryID", msg.QueryID, "dur_ms", time.Since(a.searchStart).Milliseconds())
	} else if a.rerankProgress == total/2 {
		slog.Info("Reranking 50% complete",
			"processed", a.rerankProgress, "total", total)
	}

	// Check completion
	if a.rerankProgress >= total {
		a.rerankPending = false
		a.statusText = ""
		a.applyScoresAsOrder()  // See cursor fix below
		slog.Info("Reranking complete",
			"items", total, "dur_ms", time.Since(a.searchStart).Milliseconds())
		return a, nil
	}

	// Update live status (triggers View() re-render)
	a.updateRerankStatus(a.rerankQuery)  // Assume a.rerankQuery holds search query; fallback to ""

	return a, nil
}
```

#### 3. Cursor Stability in `applyScoresAsOrder()`
Modify to reset cursor to top post-sort (logical after rerank: show best item).

```go
func (a *App) applyScoresAsOrder() {
	// Existing: sort a.rerankEntries by a.rerankScores descending
	// ...

	// Update main list
	a.list.SetItems(a.rerankEntries)  // Assume a.list is Bubble Tea list model

	// Reset cursor to #0 (top relevance) for intuitive post-rerank UX
	a.list.Select(0)

	// Optional: viewport scroll-to-top cmd if list has viewport
	// return a, a.list.ScrollToTopCmd()  // If needed
}
```

#### 4. Init in `startReranking()`
Ensure `a.rerankProgress = 0; a.statusText = "Reranking..."; a.rerankScores = make([]float32, len(a.rerankEntries))` before `tea.Batch(cmds...)`.

**Files**: `internal/ui/app.go` only. No msg/reranker changes.

This is zero-risk, high-UX: turns 32s wait into smooth progress; stable during browse; observable. Phase in bar/ETA if perf concerns.

---

## Feature 3: Progressive Search Pipeline

### 3a. Pipeline State Machine

### ANALYZE

**Where both responses agree**
- A **linear `SearchPhase` enum** is the right abstraction for the *UI-facing* “quality of results currently shown,” while async/concurrency is handled by separate in-flight flags/counters.
- `SearchPhase` should become the primary driver for `View()` (badges, colors, status text), with phases advancing **monotonically** within a search and `Esc` resetting.
- **QueryID correlation**: ignore stale async messages; don’t let old work mutate current UI.
- Key handling should become a **phase-based switch** instead of an ad-hoc cascade.
- Observability should include **phase transition events** with **timing**.

**Gaps / issues to correct**
- Response B implies `searchActive` is “equivalent to `Phase != None`”; in your app it’s “user is typing / input focused,” which maps to `PhaseInput`, not all phases.
- Response B’s pseudocode starts reranking immediately on `SearchPoolLoaded`; in your pipeline reranking must wait until **both** pool + embedding are ready.
- Neither response fully spells out how to represent “baseline results are visible but embeddings not ready yet” *today* (pre-FTS5). You can use `PhaseLexical` as “baseline ordering” even before true lexical/FTS results exist, which avoids showing results while still in `PhaseInput`.

---

### SYNERGIZE

- Response A is good at clarifying **monotonic phase transitions** and how phase replaces “guessing” in `View()`.
- Response B contributes a strong framing (“phase is perception”), a cleaner **`Update`/key routing structure**, and a concrete **observability + testing** approach.
- Combining them yields a robust design:
  - **Linear phase for perception**
  - **Orthogonal async flags** for concurrency (embedding/pool/rerank in flight)
  - A single helper `advancePhase()` enforcing monotonicity + emitting telemetry
  - A transition table + invariants for tests

---

### UNIFY

Below is a detailed implementation plan addressing all 7 questions and covering: type definition, transitions, `View()`, key routing, observability, and tests.

## 1) Linear enum vs bitmask/DAG?

Use a **linear enum** for `SearchPhase`.

- The *backend* work is a DAG (pool load + embedding in parallel; rerank after both).
- The *user experience* is progressive and superseding: once cosine results are displayed, they supersede baseline; once reranked is complete, it supersedes cosine.
- “Cosine showing while rerank runs” is represented as:
  - `phase == PhaseCosine` **and** `rerankPending == true`
  - (phase = what you are seeing; pending = what is still cooking)

A bitmask makes `View()` and transition reasoning harder and invites impossible combinations.

## 2) Should `PhaseInput` replace `searchActive`?

Yes: **replace `searchActive` with `phase == PhaseInput`**.

If you still need a convenience boolean, derive it:

```go
func (m Model) InputActive() bool { return m.phase == PhaseInput }
```

This avoids inconsistent states like `searchActive=false` but `phase=PhaseReranked`.

## 3) Phase + stale-check logic (QueryID)

Rule: **stale messages never affect phase or data**.

- Every async message must carry `QueryID`.
- On receipt:
  - if `msg.QueryID != m.queryID`: ignore and return
  - otherwise apply and possibly advance phase

Do **not** “reset phase on mismatch.” Resetting on mismatch causes random regressions when late messages arrive. The reset happens only when:
- user starts a new committed search (Enter), or
- user cancels (`Esc`)

Implementation detail: on `Esc`, increment `queryID` (or set a new token) to invalidate in-flight work.

## 4) Observability events on phase transitions (with timing?)

Yes—emit an event on every **phase transition** and include timing.

Recommended:
- `search.phase_transition` with fields:
  - `query_id`, `from`, `to`
  - `since_search_start_ms`
  - `since_prev_phase_ms`
  - `backend` (`jina`/`ollama`)
  - `pool_size`, `visible_count`
- Also emit stage timings (these are often more actionable than phases):
  - `search.embed.start/complete`
  - `search.pool.start/complete`
  - `search.rerank.start/complete`
  - for Ollama sequential: `search.rerank.entry_complete` (sampled or aggregated)

Store timestamps in model:
- `searchStartedAt`
- `phaseEnteredAt`
- `embedStartedAt`, `poolStartedAt`, `rerankStartedAt`

## 5) Test strategy

Use **table-driven transition tests** + a small set of invariants.

### Transition table tests
For each message type, test:
- same `QueryID` advances (or not) as expected
- stale `QueryID` does nothing
- rerank completion advances to `PhaseReranked`

### Invariants (can be property-based, but not required)
- **Monotonicity:** for same `QueryID`, `phase` never decreases
- **Stale immunity:** stale messages never mutate `phase`, `results`, or pending flags
- **Cancel resets:** `Esc` sets `phase=PhaseNone` and invalidates in-flight work

If you want lightweight property testing, generate random permutations of message arrival orders for a fixed `QueryID` and assert monotonicity + correct final phase when all completions arrive.

## 6) Replace the `handleKeyMsg` cascade

Move from “branch on booleans” to “branch on phase + global keys”.

Pattern:

1) Handle truly global keys first (`Ctrl+C`, `Esc`, `/`).
2) Then route by phase:
- `PhaseNone`: normal browsing keys
- `PhaseInput`: text input editing + submit/cancel
- `PhaseLexical|PhaseCosine|PhaseReranked`: results navigation keys

Pending flags should rarely block navigation; they mainly affect status/spinner and whether “submit again” cancels/starts new search.

## 7) `PhaseError` or errors orthogonal?

Make errors **orthogonal** to phase.

- If rerank fails, the user is still seeing cosine results → phase remains `PhaseCosine`.
- If embedding fails but you have baseline results, you may remain `PhaseLexical` and show an error banner.

Add:
```go
searchErr error        // last pipeline error for current queryID
errToast string/time   // optional transient UI
```

Only introduce `PhaseError` if you truly want a different *mode* (e.g., a full-screen error view that replaces results). Most TUIs don’t need that; they need “best available results + error notice”.

---

# Concrete Implementation Plan

## A) Types and model fields

```go
type SearchPhase uint8

const (
    PhaseNone SearchPhase = iota
    PhaseInput
    PhaseLexical  // "baseline" results shown (FTS later; can be recency/unranked today)
    PhaseCosine
    PhaseReranked
)

func (p SearchPhase) String() string { /* implement */ }

func (p SearchPhase) HasResults() bool { return p >= PhaseLexical }
```

Model additions (keep your existing booleans for flow control):

```go
type Model struct {
    phase SearchPhase

    queryID uint64
    queryCommitted string // the query currently being processed/displayed
    queryDraft     string // what user is typing (optional but recommended)

    // async flow control (existing)
    embeddingPending bool
    searchPoolPending bool
    rerankPending bool

    // readiness flags / cached artifacts
    haveEmbedding bool
    havePool bool

    // timing for observability
    searchStartedAt time.Time
    phaseEnteredAt  time.Time
    embedStartedAt  time.Time
    poolStartedAt   time.Time
    rerankStartedAt time.Time

    searchErr error
}
```

### Monotonic phase helper (centralize correctness + telemetry)

```go
func (m *Model) advancePhase(to SearchPhase, reason string) {
    if to <= m.phase {
        return
    }
    from := m.phase
    now := time.Now()

    // emit telemetry
    // duration since search start + since previous phase
    // (guard if searchStartedAt is zero)
    m.emitPhaseTransition(from, to, reason, now)

    m.phase = to
    m.phaseEnteredAt = now
}
```

## B) State transitions (authoritative list)

### Global resets
- **Esc**:
  - `phase = PhaseNone`
  - clear `queryDraft`, `queryCommitted`, results (as desired)
  - clear pending flags
  - `queryID++` to invalidate in-flight async work
  - emit `search.canceled`

### Start input
- `/` (or focus search):
  - `phase = PhaseInput`
  - focus textinput
  - (optionally preserve current results underneath; phase indicates the *mode*, and results remain visible)

### Commit search (Enter from input)
On `Enter` when `phase==PhaseInput` and draft non-empty:
- `queryID++`
- `queryCommitted = queryDraft`
- reset: `haveEmbedding=false`, `havePool=false`, clear scores, etc.
- set pending flags true: `embeddingPending=true`, `searchPoolPending=true`
- set timestamps: `searchStartedAt=now`, `phaseEnteredAt=now`
- **advance phase**:
  - If you can show immediate baseline results (even current visible items sorted by recency): `advancePhase(PhaseLexical, "search_submitted")`
  - Otherwise you can stay `PhaseInput` until baseline/cosine is available; but using `PhaseLexical` is usually nicer for “something is happening”.

Fire commands in parallel:
- `loadSearchPool(queryID, queryCommitted)`
- `embedQuery(queryID, queryCommitted)`

### Pool loaded
On `SearchPoolLoaded{QueryID, Items}`:
- stale-check
- `searchPoolPending=false; havePool=true`
- replace results with full corpus (or lexical subset)
- if phase < `PhaseLexical` and you are now showing baseline results: `advancePhase(PhaseLexical, "pool_loaded")`
- if `haveEmbedding`: compute cosine sort and `advancePhase(PhaseCosine, "pool+embedding_ready")`
- if `haveEmbedding` and rerank not started: start rerank

### Embedding ready
On `QueryEmbedded{QueryID, Vec}`:
- stale-check
- `embeddingPending=false; haveEmbedding=true`
- apply cosine sort to whatever set is currently displayed (visible subset or pool if present)
- `advancePhase(PhaseCosine, "embedding_ready")`
- if `havePool` and rerank not started: start rerank

### Start rerank (trigger condition)
Start reranking exactly once when:
- `havePool && haveEmbedding && !rerankPending && phase >= PhaseCosine`

Set:
- `rerankPending=true; rerankStartedAt=now`
- emit `search.rerank.start`
- command: Jina batch or Ollama sequential

### Incremental rerank (optional)
On `EntryReranked{QueryID, EntryID, Score}` (Ollama path):
- stale-check
- store score on that entry
- optionally update UI progress (status line, per-item “R” badge *for scored items*)
- **do not** advance to `PhaseReranked` until completion message (keeps the “quality contract” honest)

### Rerank complete
On `RerankComplete{QueryID, Scores/Order}`:
- stale-check
- apply final ordering
- `rerankPending=false`
- `advancePhase(PhaseReranked, "rerank_complete")`
- emit `search.rerank.complete` with duration

## C) `View()` rendering changes

Derive UI purely from `phase` + pending flags:

- Badges:
  - If `phase < PhaseCosine`: show `L` (baseline)
  - If `phase == PhaseCosine`: show `C` (and optionally show `R` on entries already reranked, if you want)
  - If `phase == PhaseReranked`: show `R`
- Border colors / header theme: keyed on `phase`
- Status text:
  - `PhaseInput`: “Type to search…”
  - `PhaseLexical`: “Searching…” + spinner if pool/embedding pending
  - `PhaseCosine`: “Semantic results” + spinner if `rerankPending`
  - `PhaseReranked`: “Refined results” (no spinner)

Spinner display becomes deterministic:
```go
showSpinner := m.embeddingPending || m.searchPoolPending || m.rerankPending
```
…but you can tailor wording by phase.

Also update your `hasQuery()` logic to reflect intent:
- “Do we have committed query results?” → `phase.HasResults()`
- “Is user editing?” → `phase == PhaseInput`

## D) `handleKeyMsg` routing (clean switch)

In `Update`:

1) Global keys:
- `Esc`: reset (always)
- `/`: enter input mode (from anywhere, optional)
- `Ctrl+C`: quit

2) Phase routing:
```go
switch m.phase {
case PhaseNone:
    return m.handleNormalKeys(msg)
case PhaseInput:
    return m.handleInputKeys(msg) // textinput update; Enter submits
default:
    return m.handleResultsKeys(msg) // list nav, open item, etc.
}
```

Within `handleResultsKeys`, you typically do **not** branch on pending flags except for UI text. Let users scroll results even while rerank runs.

## E) Observability integration (what to emit)

Minimum set:
- On submit:
  - `search.started` (query length, backend, query_id)
- On phase changes:
  - `search.phase_transition` (from/to, reason, durations)
- On each stage:
  - `search.pool.loaded` (duration, count)
  - `search.embed.completed` (duration)
  - `search.rerank.started`
  - `search.rerank.completed` (duration, backend)
- On cancel:
  - `search.canceled` (phase at cancel, elapsed)

Timing fields:
- `t_total_ms = now - searchStartedAt`
- `t_since_prev_phase_ms = now - phaseEnteredAt`
- plus per-stage durations from their start timestamps

## F) Tests (practical and high-signal)

### 1) Transition table tests
Create a small harness model with deterministic initial state, run `Update(msg)`, assert:
- phase
- pending flags
- whether rerank start was triggered (you can expose a flag or inspect returned cmd type)

Include cases:
- `QueryEmbedded` before `SearchPoolLoaded` and vice versa
- stale IDs ignored
- late messages after `Esc` ignored (because `queryID` changed)
- rerank only starts when both prerequisites are met

### 2) Invariant tests
- “phase never regresses for same queryID” across random message sequences
- “no data mutation on stale messages”

---

## Final answers (mapped to your numbered questions)

1) **Linear enum** for phase; concurrency handled by pending flags/readiness booleans.  
2) **Replace** `searchActive` with `phase==PhaseInput` (derive helper if needed).  
3) **Ignore stale messages**; don’t reset phase on mismatch. Reset only on new search submit or `Esc` (and bump queryID).  
4) Emit **phase transition events** + stage timing events; include durations.  
5) Prefer **table-driven transitions + invariants**; optional light property testing for message ordering.  
6) Replace the cascade with a **phase switch** plus global key handling; pending flags only influence spinners/text.  
7) Keep **errors orthogonal** to phase; show “best available results” and surface error in status/toast.

This design gives you a crisp “quality contract” in the UI while preserving your existing async pipeline mechanics and race-safety.

### 3b. Non-Blocking Rerank

### ANALYZE
- Both responses agree on the core change: reranking must stop blocking input; the “rerankPending gate” in the key cascade should be removed and replaced with a lighter, state-based behavior (only special-casing “apply” for the slow backend).
- Both propose separating “rerank is running” from “rerank results are ready (but not applied yet)” and driving UI indicators from that state rather than from the existing `statusText` overlay.
- Both correctly rely on the existing `QueryID` stale-check to ignore late rerank results when the user clears search or starts a new one.

Key issues / gaps:
- One response suggests keeping `rerankPending=true` but “non-blocking”; that’s internally inconsistent because the current meaning of `rerankPending` *is* “block UI”. If kept, it must be renamed/redefined to avoid accidental gating.
- One response sets `phase = PhaseIdle` after rerank completes; “idle” usually implies no active search/results. You likely want a “results shown” phase, and then separately track rerank state.
- Storing `pendingOrderedItems []Item` can go stale if the user changes filters/visibility while rerank runs. A more robust approach is: store rerank scores keyed by stable item ID, and compute the applied ordering against the *current* visible list at apply-time.
- For Ollama per-entry, intermediate reorders are a bad UX for reading; both agree to “collect then apply”, but the plan should still allow showing progress (N/30) without movement.

---

### SYNERGIZE
Complementary strengths to combine:
- The state-machine framing (enum phases) gives clarity and avoids boolean soup.
- The “inline spinner in footer/filter bar” suggestion cleanly replaces the blocking overlay.
- The “cursor stability by item ID” approach is essential for both auto-apply (Jina) and manual apply (Ollama).
- Add one missing but important robustness piece: **store rerank targets by ID + use context cancellation**. QueryID stale-check prevents UI corruption, but cancellation saves CPU/network (especially for Ollama’s 30 calls).

---

### UNIFY
Below is a consolidated implementation plan that answers Q1–Q7 and specifies field changes, handler changes, key cascade changes, View() changes, and edge cases.

## 1) Direct answers to your 7 questions

1) **`handleKeyMsg` cascade / input gating**
- Remove the `rerankPending` input block entirely.
- Replace it with **one small conditional**: only intercept `R` when rerank is “ready to apply” (Ollama slow path). Everything else (navigation, open, bookmark, filters, etc.) should continue to work while rerank runs.

2) **Spinner / “Refining…” indicator without `statusText` overlay**
- Keep `statusText` for truly modal/blocking operations only (if any remain).
- Add a **separate, non-modal indicator** driven by rerank state:
  - A `spinner.Model` + a small `refineText` rendered inline in your footer/status bar or right side of the filter bar.
  - For Ollama, include progress: `Refining… 12/30`.
  - For Jina, just `Refining…` for 2–5s.

3) **Esc (clear search) during rerank**
- Yes: clearing search should invalidate and discard in-flight rerank results.
- Do it by:
  - Incrementing/changing `queryID` (stale-check keeps you safe).
  - Resetting rerank state.
  - **Canceling the rerank context** (recommended) so Ollama calls stop quickly.

4) **New search while previous rerank is running**
- Your current QueryID stale-check is the right backbone.
- Ensure `submitSearch()` increments/sets the new QueryID **before** launching commands.
- Also reset UI indicator state immediately (spinner/progress/toast) so the old rerank doesn’t “leak” into the new query’s UI.

5) **`rerankRunning` / `rerankReady`: booleans vs SearchPhase enum**
- Put it into a unified state model:
  - Keep a `SearchPhase` (Idle/Typing/Loading/Results) **for the search pipeline**.
  - Track rerank as a sub-state (Running/Ready/Applied) **or** extend the phase enum to include RerankRunning/RerankReady.
- Practically: a phase enum + a small `rerank` struct is cleanest and avoids boolean sprawl.

6) **Cursor stability when items reorder**
- Yes: record the currently selected item’s stable ID before applying a reorder, then after reorder find that ID and restore the cursor index (clamp if missing).
- Do this for both Jina auto-apply and Ollama manual apply.

7) **Ollama per-entry: intermediate movement?**
- Do **not** reorder on every `EntryReranked`. It creates a jumping list while the user reads.
- Collect scores, show progress only. When all are in, switch to “ready” and wait for `R` to apply.

---

## 2) Struct / field changes (App model)

### Replace/retire
- Retire `rerankPending` (or redefine/rename it so it can’t accidentally gate input again). The UI must not interpret rerank-running as modal.

### Add: phases + rerank state
```go
type SearchPhase int

const (
    PhaseIdle SearchPhase = iota
    PhaseSearching          // loading pool + embedding
    PhaseResults            // cosine-ranked results visible/browsable
)

type RerankMode int
const (
    RerankAutoApply RerankMode = iota // Jina
    RerankManualApply                // Ollama
)

type RerankState struct {
    queryID   uint64
    running   bool
    ready     bool   // results available but not applied (manual path)
    mode      RerankMode

    // snapshot of what you reranked (stable IDs are key)
    targetIDs []string        // length N
    scores    []float32       // length N, same index as targetIDs
    done      int
    total     int

    // optional UI
    toast     string          // "Deep analysis complete. Press R to apply."
}

type App struct {
    // existing search fields...
    phase           SearchPhase
    queryID         uint64

    // “both ready” flags you already effectively have somewhere:
    haveEmbedding   bool
    havePool        bool

    // rerank
    rerank          RerankState
    rerankCancel    context.CancelFunc

    // UI (non-modal)
    refineSpinner   spinner.Model
}
```

Notes:
- Use `uint64 queryID` (monotonic) or UUID—either is fine.
- The critical change is: **rerank results are keyed by stable IDs**, not “whatever index the visible list had later”.

---

## 3) Command start/cancel behavior

### `submitSearch()`
- Increment `queryID` first.
- Cancel any previous rerank context.
- Reset `rerank` to zero state and clear any toast/indicator.
- Fire `loadSearchPool(queryID)` and `embedQuery(queryID)` in parallel.

### `startReranking()`
- Build the rerank candidate set from the *current* cosine-ranked visible items (e.g., top 30).
- Snapshot stable IDs into `rerank.targetIDs`.
- Initialize `rerank.running=true`, `rerank.ready=false`, `rerank.done=0`, `rerank.total=N`, and set `rerank.mode` depending on backend:
  - Jina => `RerankAutoApply`
  - Ollama => `RerankManualApply`
- Create `ctx, cancel := context.WithCancel(...)` and store `rerankCancel = cancel`.
- Kick off:
  - Jina: one cmd returning `RerankComplete{QueryID, Scores}`
  - Ollama: N cmds returning `EntryReranked{QueryID, Index, Score}`

---

## 4) Update() message handling changes

### `QueryEmbedded`
- If stale (`msg.QueryID != app.queryID`) ignore.
- Mark `haveEmbedding=true`.
- Immediately cosine-sort current items (even if pool not loaded yet).
- Set `phase = PhaseResults` as soon as you have anything meaningful to show.
- If `havePool && haveEmbedding` and rerank not started yet, call `startReranking()`.

### `SearchPoolLoaded`
- If stale ignore.
- Mark `havePool=true`.
- Replace items with full corpus.
- If embedding already exists, cosine-sort immediately.
- If `havePool && haveEmbedding` and rerank not started yet, call `startReranking()`.

### `EntryReranked` (Ollama)
- If stale ignore.
- If `rerank.running` is false ignore (covers cases where user canceled/reset).
- Store: `rerank.scores[msg.Index] = msg.Score`, increment `rerank.done`.
- When `rerank.done == rerank.total`:
  - `rerank.running=false`
  - `rerank.ready=true`
  - `rerank.toast = "Deep analysis complete. Press R to apply."`
  - Do **not** reorder yet.

### `RerankComplete` (Jina)
- If stale ignore.
- `rerank.running=false`
- Store the scores into `rerank.scores` aligned with `rerank.targetIDs`.
- Auto-apply immediately:
  - record selected item ID
  - reorder
  - restore cursor by ID
- Clear toast / mark not-ready:
  - `rerank.ready=false`

### Applying reorder (shared utility)
Instead of “apply by index into whatever the current list is”, do:

- Build a map `scoreByID := map[string]float32` from `rerank.targetIDs` + `rerank.scores`.
- Sort the **current visible items** by:
  1) items with rerank scores first, descending by score
  2) items without rerank score retain their relative order (stable sort) so you don’t scramble the rest
- This remains correct even if the user toggled filters while rerank ran.

---

## 5) Key handling cascade (non-blocking)

Remove any `if rerankPending { ... return }` style block.

Pseudo-flow:

```go
func (a App) handleKeyMsg(k tea.KeyMsg) (tea.Model, tea.Cmd) {
    // Global keys
    switch k.Type {
    case tea.KeyEsc:
        a.cancelInFlightRerank()
        a.resetSearchToIdle()        // clears query text, items, havePool/haveEmbedding, etc.
        a.bumpQueryID()              // invalidates in-flight messages
        return a, nil
    }

    // Manual apply only when ready
    if a.rerank.ready && (k.String() == "r" || k.String() == "R") {
        a.applyRerankOrderPreserveCursor()
        a.rerank.ready = false
        a.rerank.toast = ""
        return a, nil
    }

    // Otherwise: normal key handling always allowed
    return a.normalKeyHandling(k)
}
```

No other rerank-related gating is needed.

---

## 6) View() rendering changes (no modal overlay)

Stop using `statusText != ""` for rerank.

Render rerank status inline, derived from rerank state:

- If `rerank.running`:
  - Jina: `⠋ Refining…`
  - Ollama: `⠋ Refining… 12/30`
- If `rerank.ready` (Ollama complete, not applied):
  - show toast line: `Deep analysis complete. Press R to apply.`
- Otherwise nothing.

Where to place it:
- Footer/status bar is simplest and doesn’t disturb layout.
- If you have a filter/search bar at top, right-align the indicator there.

Also: keep `statusText` overlay for truly blocking phases only (if any remain). Rerank should never trigger it.

---

## 7) Edge cases checklist

- **Esc during rerank**:
  - Cancel context (`rerankCancel()`).
  - Increment queryID.
  - Reset rerank state.
  - Late messages are ignored by QueryID mismatch.

- **New search during rerank**:
  - Same as Esc: cancel + new queryID + reset rerank indicators immediately.

- **User changes filters/sorts while rerank runs**:
  - Because rerank scores are stored by item ID and applied against the current visible list, applying remains sensible.
  - If none of the visible items have scores, applying becomes a no-op (fine).

- **Backend detection (auto-apply vs toast)**:
  - Decide once in `startReranking()` from config/backend choice:
    - Jina => `mode=RerankAutoApply` (apply on `RerankComplete`)
    - Ollama => `mode=RerankManualApply` (set `ready=true` when all entries scored)

This design meets the goal: cosine-ranked results appear instantly, the user can browse freely, rerank refines in the background with a subtle indicator, and completion behavior differs cleanly between fast (auto-apply) and slow (toast + R to apply) backends—without UI blocking.

### 3c. Smooth Reorder

### ANALYZE
Both responses converge on the right core approach for Bubble Tea: treat “animation” as transient render state + timed messages. They agree on:
- Use a brief flash window (≈1–2s) where extra adornments (movement arrows) are shown.
- Compute reorder deltas once at reorder time and store them (don’t recompute in `View()` every frame).
- Preserve cursor by tracking the selected item **ID** before reorder and restoring cursor to that item’s new index after reorder.
- Persist cross-encoder score metadata in the model so the `View()` can colorize.

Key differences / issues:
- **Response A** sometimes implies arrows might show score change (e.g. `^0.12`), while your goal is **position change** (`^3`, `v2`). The delta computation itself is correct; the example is off.
- **ID typing mismatch**: A uses `map[string]...`, B uses `map[int64]...`. The correct solution should key by whatever `store.Item.ID` actually is (or a dedicated `ItemID` type).
- **Tick overlap**: B mentions stale ticks; A says “cancel previous flash” but Bubble Tea can’t cancel a previously returned `tea.Tick`. You need a **generation/token** check to ignore stale “done” messages.
- **Rendering API**: B’s “RenderContext struct” recommendation is stronger and cleaner than expanding the function signature with many params.

### SYNERGIZE
What A contributes that’s useful:
- Clear reorder flow: snapshot old positions → apply reorder → compute deltas → start flash.
- Practical score band thresholds and border coloring concept.
- Explicit “cursorID → restoreCursor(cursorID)” approach.

What B contributes that A missed or under-specified:
- Encapsulating transient UI feedback in a dedicated struct (`SearchFeedback` / `ReorderFeedback`) keeps `App` tidy.
- Render refactor via a context object to avoid signature bloat.
- Explicit plan for handling reorders occurring during an active flash (overwrite state + generation guard).
- Concrete unit test ideas (delta, cursor stability, timeout).

Combined best plan:
- One flash window controlled by a single tick **per reorder**, guarded by a generation id.
- Store deltas/scores/rank-source in model; arrows are transient; border/badges can persist (at least in search mode).
- Extend rendering via a `RenderContext` (or `ItemRenderMeta` provider) rather than adding many parameters.

### UNIFY
#### Direct answers to your 8 questions

1) **Flash timer: single `tea.Tick` or multiple staged ticks?**  
Use a **single `tea.Tick`** that sends a `ReorderFlashDone` message after ~1.2–1.8s. Terminal “fade-outs” tend to look like flicker and add complexity. If you later want a 2-step effect, add a second phase (`FlashPhaseDim`) and a second tick—but start with one.

2) **Store deltas vs compute lazily in `View()`?**  
Compute once at reorder time and **store in a map** on the model. `View()` runs frequently; lazy computation is wasteful and couples rendering to reorder logic.

3) **Extend `renderItemLine` without bloating signature?**  
Yes: pass a **render context struct** (or an interface that can answer “meta” questions). Recommended:
```go
renderItemLine(item store.Item, ctx ItemRenderCtx) string
```
where `ctx` includes width/selected/mode + accessors for delta/score/rank source.

4) **Should left border color persist after flash?**  
In **search mode**, persist it: the score band is useful information after the moment of reordering. In chronological mode, either disable it or use a neutral border to avoid visual noise.

5) **L/C/R badge: replace `[HN]` source badge or appear alongside?**  
Prefer **alongside**, but extremely compact: a single fixed-width “rank column” (e.g. `R`, `C`, `L`) plus your existing source badge. Replacing `[HN]` loses important provenance. If space is very tight, consider merging as `[R|HN]` (still keeps source).

6) **Cursor stability vs flash: should cursor follow the item if it moved?**  
Yes: cursor should follow the **same item ID**, even if it moved from position 5 → 2. “Stability” in a list means “the thing I selected stays selected,” not “the cursor index stays constant.”

7) **New reorder triggers while previous flash active?**  
Overwrite the feedback state with the newest reorder and restart the flash. Use a **generation id** so stale ticks from the previous flash don’t incorrectly clear the new flash state.

8) **Items beyond rerank top N (31+) get treatment?**  
- They should get the **rank-source badge** (`C` typically) so users understand they weren’t cross-encoder reranked.
- They should **not** get cross-encoder score border colors (no score exists), so use a neutral/grey border (or none).
- They generally won’t move, but if they do (edge cases), you can still show arrows during flash if `delta != 0`.

---

## Implementation plan

### 1) New model fields (animation + metadata)

Keep reorder/search feedback in one struct to avoid scattering fields:

```go
type RankSource uint8
const (
    RankUnknown RankSource = iota
    RankLexical
    RankCosine
    RankReranked
)

type ReorderFeedback[ID comparable] struct {
    Active     bool
    Gen        uint64            // generation token for stale tick protection
    StartedAt  time.Time
    Deltas     map[ID]int        // ID -> oldIndex - newIndex (positive moved up)
    Scores     map[ID]float64    // ID -> cross-encoder score (only for reranked items)
    Source     map[ID]RankSource // ID -> L/C/R (for current mode)
    TopN       int               // e.g. 30, for display decisions if needed
}

type App struct {
    // existing fields...
    items  []store.Item
    cursor int

    // new:
    reorderUI ReorderFeedback[store.ItemID] // use the real ID type
    // also: currentMode / isSearchMode already exists in your app
}
```

Message type to end the flash, carrying the generation id:

```go
type ReorderFlashDoneMsg struct{ Gen uint64 }
```

### 2) Flash timer command (single tick + generation guard)

```go
func reorderFlashTick(gen uint64, d time.Duration) tea.Cmd {
    return tea.Tick(d, func(time.Time) tea.Msg {
        return ReorderFlashDoneMsg{Gen: gen}
    })
}
```

In `Update`:

```go
case ReorderFlashDoneMsg:
    if msg.Gen == a.reorderUI.Gen {
        a.reorderUI.Active = false
        // keep Scores/Source for persistent indicators
        // optionally clear Deltas to reduce clutter/state:
        a.reorderUI.Deltas = nil
    }
    return a, nil
```

### 3) Reorder flow (snapshot → apply → compute deltas → start flash → restore cursor)

Assume you currently receive rerank results as `(scores map[ID]float64, orderedIDs []ID)` for top N.

**Step A — snapshot old positions + selected ID**
```go
oldPos := make(map[store.ItemID]int, len(a.items))
for i, it := range a.items {
    oldPos[it.ID] = i
}

var selectedID store.ItemID
hasSelected := len(a.items) > 0 && a.cursor >= 0 && a.cursor < len(a.items)
if hasSelected {
    selectedID = a.items[a.cursor].ID
}
```

**Step B — apply reorder without resetting cursor**
Refactor `applyRerankOrder` so it *does not* blindly set `cursor = 0`. It should only rebuild `a.items`:

- Build a `rerankedSet` from `orderedIDs`
- Append reranked items in that order
- Append the remaining items in their existing relative order

**Step C — compute deltas + rank source**
```go
deltas := make(map[store.ItemID]int, len(a.items))
source := make(map[store.ItemID]RankSource, len(a.items))

for newIdx, it := range a.items {
    if oldIdx, ok := oldPos[it.ID]; ok {
        delta := oldIdx - newIdx // + => moved up
        if delta != 0 {
            deltas[it.ID] = delta
        }
    }
    if _, ok := scores[it.ID]; ok {
        source[it.ID] = RankReranked
    } else {
        source[it.ID] = RankCosine // or RankLexical if that pipeline produced it
    }
}
```

**Step D — cursor stability**
```go
if hasSelected {
    for i, it := range a.items {
        if it.ID == selectedID {
            a.cursor = i
            break
        }
    }
    if a.cursor >= len(a.items) {
        a.cursor = max(0, len(a.items)-1)
    }
}
```

**Step E — start flash state + tick**
```go
a.reorderUI.Gen++
gen := a.reorderUI.Gen
a.reorderUI.Active = true
a.reorderUI.StartedAt = time.Now()
a.reorderUI.Deltas = deltas
a.reorderUI.Scores = scores      // keep for persistent border in search mode
a.reorderUI.Source = source
a.reorderUI.TopN = 30

return reorderFlashTick(gen, 1500*time.Millisecond)
```

### 4) Rendering changes: context object + stable columns

#### Render context
```go
type ItemRenderCtx struct {
    Width      int
    Selected   bool
    SearchMode bool

    Feedback   *ReorderFeedback[store.ItemID] // nil if not applicable
}
```

New signature:
```go
func renderItemLine(item store.Item, ctx ItemRenderCtx) string
```

#### Visual composition (recommended layout)
Keep columns fixed width so lines don’t “jitter”:

- Left border: colored by score band (if available and in search mode)
- Rank column: `L`/`C`/`R` (1 char, styled)
- Delta column (flash only): `^3` / `v2` (fixed width, e.g. 3–4 cells)
- Existing: `[SourceBadge] Title`

Example:
```
│ R ^3 [HN] Some headline...
│ C    [BBC] Another headline...
```

#### Score band border color (search mode)
```go
func scoreBandColor(score float64) lipgloss.Color {
    switch {
    case score >= 0.8:
        return lipgloss.Color("82")  // green-ish in 256 color
    case score >= 0.5:
        return lipgloss.Color("39")  // blue-ish
    default:
        return lipgloss.Color("241") // grey
    }
}
```

If no score (not reranked), use a neutral dim border.

#### Movement arrows during flash
- Only render if `ctx.Feedback != nil && ctx.Feedback.Active`
- Lookup `delta := ctx.Feedback.Deltas[item.ID]`
- If `delta > 0` render `^<delta>`
- If `delta < 0` render `v<abs(delta)>`
- If absent/0, render spaces (for alignment)

### 5) Interaction with existing styles (Selected/Normal/Read)
- Keep your existing SelectedItem/NormalItem/ReadItem styling for the **content**.
- Apply the left border style in a wrapper style so selection highlight doesn’t erase the border:
  - `BorderForeground(scoreColor)`
  - Selection background on the main text area.

### 6) Handling time band headers
- Only apply reorder feedback visuals in **search mode** (or wherever rerank happens).
- In chronological mode, keep current header behavior; don’t show deltas/badges/borders.

---

## Re-trigger behavior (flash overlap)
Because `tea.Tick` can’t be canceled:
- Each reorder increments `reorderUI.Gen`.
- The tick message carries `Gen`.
- Only clear `Active` if the generation matches.

This makes repeated reranks safe, even if they arrive rapidly.

---

## Test strategy (state transitions + render assertions)

### Unit tests for reorder logic (no terminal needed)
1. **Delta correctness**
   - Items: `[A,B,C,D]`, reorder to `[C,A,B,D]`
   - Assert: `delta[C]=2`, `delta[A]=-1`, `delta[B]=-1`, `D` absent/0

2. **Cursor stability**
   - Cursor on `B`, reorder moves `B` elsewhere
   - Assert cursor points to `B`’s new index

3. **Flash generation guard**
   - Trigger reorder #1 ⇒ gen=1, Active=true
   - Trigger reorder #2 ⇒ gen=2, Active=true
   - Send `ReorderFlashDoneMsg{Gen:1}` ⇒ assert still Active (unchanged)
   - Send `ReorderFlashDoneMsg{Gen:2}` ⇒ assert Active=false

4. **Rank source assignment**
   - Provide scores for only top N IDs
   - Assert scored items are `R`, others are `C` (or `L` when applicable)

### Render tests (string-level)
- Render one item with `delta=+3` and assert output contains `^3` during flash.
- Render after flash done and assert arrow is absent.
- If you can tolerate ANSI codes in tests, assert border color codes; otherwise, structure rendering so prefix tokens are testable before styling.

---

## Recommended defaults
- Flash duration: **1500ms**
- Persistent indicators (search mode): **score-band left border + L/C/R badge**
- Transient indicators: **movement arrows only**
- Cursor: always track by **selected item ID**, not by index

### 3d. Ollama Toast Pattern

### ANALYZE
Both responses agree on the core UX: Jina auto-applies because it’s fast; Ollama should *not* auto-apply because it’s slow/disorienting and aligns with “Curation by Consent.” Both also agree on: store pending results, show a completion notification, auto-dismiss the notification after ~10s while keeping results applicable via a key, and discard pending results on `Esc` and on new searches.

Key differences:
- **Toast placement**
  - A favors putting the message in the **status bar area** (integrated with navigation/key hints) to avoid layout churn.
  - B favors reusing **`statusText`** (existing transient status mechanism).
  - Issue: if `statusText` currently *replaces* the normal status bar (including key hints), using it for a “Press R” message can hide the very hints you want visible.
- **R vs r binding**
  - A suggests `R` apply, and optionally letting `r` apply when pending (risky because `r` is refresh).
  - B prefers **only `R`** applies; `r` always refresh.
- **Staleness**
  - A hints at stale checks; B says no staleness check.
  - Reality: time-based staleness is often unnecessary, but **state-based staleness** (list changed, query changed, refresh occurred) is important to avoid applying mismatched results.
- **Progress indicator**
  - B strongly recommends showing progress (e.g., 12/30). A mentions progress only lightly.
- **Implementation correctness**
  - B’s key matching examples use patterns that don’t match Bubble Tea’s actual `tea.KeyMsg` API (you generally switch on `msg.String()` / `msg.Type`, not `tea.Key{Run: 'R'}`).

### SYNERGIZE
What A contributes that’s valuable:
- Clear lifecycle: ready → toast shown → toast expires but pending remains → apply/discard.
- Good idea to **preserve the user’s place** by restoring cursor by ID on apply (avoids disorientation).
- Good observability suggestions (ready/applied/dismissed, defer duration).

What B contributes that’s valuable:
- Strong argument for reusing existing UI conventions and keeping footprint minimal.
- Concrete suggestion to show **Ollama progress** during the long rerank.
- A firm stance that **`R` should be the apply key** to avoid clobbering refresh behavior.

Best combined approach:
- Render the toast in the **status bar region** without adding rows (no layout shift), *and* don’t steal all key hints. This likely means slightly evolving the status bar renderer to support a transient “center message” (toast/progress) while keeping left (position) and right (key hints) intact.
- Use **`R` to apply**, keep **`r` refresh**; if refresh happens while pending, explicitly discard pending and optionally emit a brief “discarded” status.
- Keep pending results indefinitely **until the list/query changes**; validate with a revision token (state-based staleness), not a time timeout.
- Show progress for Ollama in the same status message channel (spinner + `12/30`), then swap to toast on completion.

### UNIFY
#### 1) Answers to the 7 UX questions

1) **Where should the toast render?**  
Most natural in a TUI: **the status bar region**, integrated as a transient message *without changing layout*. Concretely: keep your existing left “position” and right “key hints,” and add a **middle message slot** for “Deep analysis complete. Press R to apply.”  
Avoid adding a new bar (option c) because it shifts the list; avoid the filter bar (b) because it’s “search semantics,” not “system notification.”

2) **Visual emphasis?**  
Use **subtle emphasis**: bold + a success/info color (e.g., yellow/cyan/green depending on your theme), no border. This should feel like a “ready” state, not an alert. Optional: a short-lived brighter style for the first ~1s, then settle to normal bold color.

3) **How should the R key binding work?**  
- **`R` (uppercase) applies pending Ollama rerank** when available.  
- **`r` (lowercase) remains refresh** always.  
If pending exists and the user presses `r`, treat refresh as: “refresh (discard pending analysis)” and emit a brief status message like “Refreshed (discarded pending analysis).” Don’t overload `r` to apply—refresh is too important and case confusion is real; better to keep semantics stable and rely on the toast + key hint.

4) **Pressing R 60 seconds later: apply or stale?**  
Apply is fine *if the underlying list hasn’t changed*. Don’t use a pure time-based staleness rule. Instead, do a **state-based staleness check** (see “listRevision” below). If the list changed (new search, refresh, different filter result set), refuse to apply and show: “Analysis no longer matches current results.”

5) **Progress indicator while reranking?**  
Yes for Ollama. Show something like:  
`⠋ Deep analysis 12/30…`  
in the same status message slot (with spinner). Jina is fast enough to skip progress and just apply.

6) **If user is at item #50 (past reranked top-30), should toast still appear?**  
Yes. The analysis is still useful and user can later jump back up. Mitigate disorientation by **preserving cursor by entry ID** when applying so they don’t “teleport.”

7) **Vary toast message based on quality/impact?**  
Optional but worthwhile if easy: compute a tiny “impact summary” (e.g., number of items that would move positions, or count of positives in Ollama’s yes/no). If minimal, use:  
“Deep analysis complete (minimal changes). Press R to apply.”  
Otherwise keep the standard message. Don’t overfit to score distributions.

---

## Implementation Plan (Bubble Tea)

### A) New model fields

Add explicit state for: in-flight rerank, pending rerank, and toast lifecycle.

```go
type RerankBackend int
const (
    BackendNone RerankBackend = iota
    BackendJina
    BackendOllama
)

type ToastState struct {
    Text      string
    Visible   bool
    ToastID   int64     // increment to ignore stale ticks
    ExpiresAt time.Time
    StyleKind string    // "info"|"success"|"warn" (optional)
}

type PendingRerank struct {
    RerankID      int64
    Backend       RerankBackend
    Query         string
    CreatedAt     time.Time

    // Validity / staleness protection:
    ListRevision  int64        // snapshot of current list revision
    EntryIDs      []string     // IDs for the reranked subset (e.g., top 30)
    ScoresByID    map[string]float64

    // Optional precomputed:
    NewOrderIDs   []string     // reranked order for subset
    ImpactMoves   int          // e.g., how many positions change in subset
}

type model struct {
    // existing:
    items []Entry
    cursor int
    statusText string // if you keep it, but see status bar section below

    // list revision increments when items set/order changes due to search/refresh/filter
    listRevision int64

    // rerank in-flight (non-blocking)
    rerankRunning   bool
    rerankBackend   RerankBackend
    rerankID        int64
    rerankStartedAt time.Time
    rerankProgress  int
    rerankTotal     int

    pending *PendingRerank
    toast   ToastState

    // Feature 3c flash state
    flashActive bool
}
```

**Why `rerankID` + `ToastID` + `listRevision`:**  
They prevent races where old goroutines/ticks deliver completion/progress after a new search/refresh.

### B) New messages

```go
type toastExpiredMsg struct{ ToastID int64 }

type rerankProgressMsg struct {
    RerankID int64
    Done     int
    Total    int
}

type rerankReadyMsg struct {
    RerankID int64
    Backend  RerankBackend
    ScoresByID map[string]float64 // or []score aligned with EntryIDs
    EntryIDs []string
}
```

(You may already have `EntryReranked` and `RerankComplete`; the key is: **include `RerankID`** in every rerank-related msg.)

### C) Commands: toast timer + flash timer

```go
func startToastTimer(toastID int64, d time.Duration) tea.Cmd {
    return tea.Tick(d, func(time.Time) tea.Msg {
        return toastExpiredMsg{ToastID: toastID}
    })
}

func startFlashTimer() tea.Cmd {
    return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
        return flashDoneMsg{}
    })
}
```

### D) Status bar rendering (recommended change)

Instead of “statusText replaces everything,” evolve your status bar render to support:

- **Left:** `pos` text (`1/247`, filtered marker, etc.)
- **Middle:** transient message (searching, rerank progress, toast)
- **Right:** key hints (always visible, but can be adjusted when pending exists)

Pseudo:

```go
func (m model) statusMessage() (string, lipgloss.Style) {
    // Priority order
    if m.rerankRunning && m.rerankBackend == BackendOllama {
        return fmt.Sprintf("%s Deep analysis %d/%d…", m.spinner.View(), m.rerankProgress, m.rerankTotal), progressStyle
    }
    if m.toast.Visible {
        return m.toast.Text, toastStyle
    }
    return "", lipgloss.NewStyle()
}

func (m model) keyHints() string {
    base := "j/k:nav Enter:read /:search r:refresh q:quit"
    if m.pending != nil {
        // persistent hint even after toast expires
        base = strings.Replace(base, "r:refresh", "R:apply r:refresh", 1)
        base = strings.Replace(base, "/:search", "/:search (discard analysis)", 1)
    }
    return base
}
```

This solves the “toast hides key hints” problem while keeping the layout stable.

### E) Completion behavior

#### Jina
On `RerankComplete` (batch) for the active rerankID:
- Apply immediately (existing behavior), but consider preserving cursor by ID if you want to reduce disorientation across both backends.
- Trigger flash (Feature 3c).

#### Ollama
When the last entry completes (or you aggregate to `rerankReadyMsg`):
- Compute pending order and store it in `m.pending` (do not apply).
- Show toast for 10s.

```go
func (m model) onOllamaReady(msg rerankReadyMsg) (model, tea.Cmd) {
    if msg.RerankID != m.rerankID { return m, nil } // stale

    pending := &PendingRerank{
        RerankID: msg.RerankID,
        Backend: BackendOllama,
        Query: m.currentQuery(),
        CreatedAt: time.Now(),
        ListRevision: m.listRevision,
        EntryIDs: msg.EntryIDs,
        ScoresByID: msg.ScoresByID,
    }
    pending.NewOrderIDs, pending.ImpactMoves = computeSubsetOrderAndImpact(msg.EntryIDs, msg.ScoresByID)

    m.pending = pending
    m.rerankRunning = false

    // Toast text (optionally impact-aware)
    text := "Deep analysis complete. Press R to apply."
    if pending.ImpactMoves <= 1 {
        text = "Deep analysis complete (minimal changes). Press R to apply."
    }
    m.toast.ToastID++
    m.toast.Visible = true
    m.toast.Text = text
    m.toast.ExpiresAt = time.Now().Add(10 * time.Second)

    return m, startToastTimer(m.toast.ToastID, 10*time.Second)
}
```

### F) Applying pending rerank (R key)

Key goals:
- Validate it’s not stale (listRevision matches and IDs exist).
- Apply reorder for the subset (top 30) stably.
- Preserve cursor by current entry ID where possible.
- Trigger flash.

```go
func (m model) applyPending() (model, tea.Cmd) {
    if m.pending == nil { return m, nil }

    // State-based staleness check:
    if m.pending.ListRevision != m.listRevision {
        m.pending = nil
        m.toast.ToastID++
        m.toast.Visible = true
        m.toast.Text = "Analysis no longer matches current results."
        return m, startToastTimer(m.toast.ToastID, 5*time.Second)
    }

    curID := ""
    if m.cursor >= 0 && m.cursor < len(m.items) {
        curID = m.items[m.cursor].ID
    }

    m.items = applySubsetReorder(m.items, m.pending.EntryIDs, m.pending.NewOrderIDs)
    m.pending = nil
    m.toast.Visible = false
    m.toast.Text = ""

    // Restore cursor by ID (prevents disorientation)
    if curID != "" {
        m.cursor = indexOfID(m.items, curID)
        if m.cursor < 0 { m.cursor = 0 }
    }

    m.flashActive = true
    // observability: rerank_applied with defer duration
    return m, startFlashTimer()
}
```

### G) Key handling (`handleKeyMsg`)

Use Bubble Tea idioms:

```go
case tea.KeyMsg:
    switch msg.String() {
    case "R":
        if m.pending != nil {
            return m.applyPending()
        }
    case "r":
        // refresh; if pending exists, discard it explicitly
        if m.pending != nil {
            m.pending = nil
            // optional: brief status message "Discarded pending analysis"
        }
        return m.startRefresh()
    case "esc":
        // must clear search AND discard pending results
        m.pending = nil
        m.toast.Visible = false
        return m.clearSearch()
    case "/":
        // starting new search discards pending results
        if m.pending != nil {
            m.pending = nil
            m.toast.Visible = false
        }
        return m.enterSearchMode()
    }
```

### H) Toast lifecycle

- **Show** on Ollama completion → set `toast.Visible=true` and start tick.
- **Auto-dismiss**: on `toastExpiredMsg`, hide only if IDs match.

```go
case toastExpiredMsg:
    if msg.ToastID == m.toast.ToastID {
        m.toast.Visible = false
        m.toast.Text = ""
        // pending remains; key hint continues to advertise R:apply
    }
```

### I) Interactions with `clearSearch()` and new searches

Ensure all “list-changing” actions increment `listRevision` and discard pending appropriately:
- `clearSearch()` (Esc): discard `pending`, hide toast.
- Submitting a new search query: discard `pending`, hide toast, increment `listRevision` when results load.
- Refresh: discard `pending`, hide toast, increment `listRevision` when items update.
- If you support toggling filters that reconstitute the list: either increment `listRevision` or treat it as “same list different view” depending on whether applying rerank should still be valid.

### J) Observability events (minimum useful set)

Emit structured events:
- `rerank_started` `{backend, rerank_id, query, total}`
- `rerank_progress` (optional sampling) `{rerank_id, done, total}`
- `rerank_ready_deferred` `{rerank_id, backend:"ollama", duration_ms, impact_moves}`
- `toast_shown` / `toast_expired` `{rerank_id, toast_id}`
- `rerank_applied` `{rerank_id, apply_delay_ms, impact_moves}`
- `rerank_discarded` `{reason:"esc|new_search|refresh|stale_apply", rerank_id}`

### K) Test strategy

**Model/unit tests (table-driven):**
1. Ollama completion → `pending != nil`, `toast.Visible == true`, tick cmd scheduled.
2. `toastExpiredMsg` with wrong ToastID does nothing; with correct ToastID hides toast but keeps pending.
3. `KeyMsg("R")` with pending applies reorder, clears pending, triggers flash, preserves cursor by ID.
4. `KeyMsg("esc")` clears search and discards pending + toast.
5. Starting new search (`"/"` then submit) discards pending immediately.
6. Staleness: increment `listRevision` after pending created; pressing `R` refuses and shows “no longer matches.”

**View/golden tests:**
- Snapshot status bar rendering with:
  - normal
  - rerank progress (shows `12/30`)
  - toast visible (shows message + right-side key hints incl `R:apply`)
  - toast expired but pending exists (no message, but key hint includes `R:apply`)

This plan keeps the UI stable, preserves user context, prevents race/stale application bugs, and cleanly expresses “deferred apply” as consent-driven curation.

---

## Feature 4: Step Timeline Status Bar

### 4a. Timeline Data Model

### ANALYZE
Both responses agree on core design choices: a dedicated `SearchTimeline` struct (better than flat App fields for reset/testability); direct handler updates (Option A/hybrid, rejecting ring buffer scans for performance/simplicity); phases `pool`, `embed`, `cosine`, `rerank`; QueryID for staleness/reset; `Reset`/`Clear` methods; `Render` for View(); integration via App field, submitSearch/clearSearch hooks, and handler calls.

Key differences:
- **Structure**: A uses flat fields (global `Start`, per-phase `Done` durations from search start, simple bools/progress). B uses nested `Phase` structs (per-phase `StartedAt`/`EndedAt` timestamps, computed elapsed, generic `PhaseName`).
- **Timing**: A precomputes durations from global `searchStart` (simple/fast Render, but inaccurate for sequential phases as they accumulate prior time). B computes true per-phase elapsed dynamically in `Render(now)` (precise, handles parallel accurately).
- **Methods**: A has phase-specific `Record*` methods. B has generic `StartPhase`/`FinishPhase`/`SetProgress` with `phasePtr` switch and QueryID guards on every call (extensible, defensive against stale).
- **Render**: A: Fixed order, shows `[pool ...]` for started/in-progress, no query/now param. B: Includes `/ query`, placeholders `[--]`, richer progress, takes `now` for live elapsed.
- **Defenses**: B more robust (idempotent Start/Finish, infer start, clamp/auto-finish progress, QueryID everywhere). A simpler but lacks per-call guards (relies on caller).
- **Extras**: B stores `Query`, uses `strings.Builder`, clamps negatives; A has `Active()` check.

No factual errors. Minor logical issues: A's global-start durations misrepresent phase-specific latencies (e.g., rerank shows full search time); B's `[--]` placeholder shows unstarted phases (less clean than A's omission); neither exactly matches example labels (`fts` vs `pool`) or prefix (`/ query    `).

### SYNERGIZE
A brings: Simplicity (flat fields possible but upgraded; specific methods avoid switch; fast no-arg `Render`; clean omission of unstarted phases; compact render matching goal closely).
B brings: Precision (per-phase timestamps > global; live elapsed; QueryID guards; generic/extensible; rich progress; query storage for render; defensive idempotence).
Complementary strengths: B's `Phase` + generic methods for robustness/parallel accuracy; A's render logic/style (omissions, order, no placeholders); combine query prefix from example/B; use B's `now` for live but A's fmtDur; hybrid guards (QueryID on mutations); exact example format (`[pool 12ms]`, `/ query`, `5/30...`); B's extensibility for future (e.g., fts split).

Unified wins: More precise/robust than A, simpler/cleaner than B (no switch via phase-specific wrappers? No, keep generic; trim excess like `--`; precise timings).

### UNIFY
```go
// SearchTimeline tracks per-phase timing for the active search.
// Zero value means no search in progress. Designed for Bubble Tea: fast Render, precise parallel timings.
type SearchTimeline struct {
	QueryID   string    // correlation ID (stale guard)
	Query     string    // for rendering
	StartedAt time.Time // search initiation

	Pool   Phase // SQLite pool load
	Embed  Phase // query embedding (Jina)
	Cosine Phase // cosine similarity (CPU)
	Rerank Phase // cross-encoder (Jina batch)
}

// Phase models a single timeline step (parallel-friendly).
type Phase struct {
	Started   bool
	Done      bool
	StartedAt time.Time
	EndedAt   time.Time
	Progress  int // only meaningful for rerank
	Total     int // only meaningful for rerank
}

func (p *Phase) Reset() {
	*p = Phase{}
}

func (p *Phase) Start(at time.Time) {
	if p.Started {
		return // idempotent
	}
	p.Started = true
	p.StartedAt = at
}

func (p *Phase) Finish(at time.Time) {
	if p.Done {
		return // idempotent
	}
	if !p.Started {
		p.Start(at) // infer if missed
	}
	p.Done = true
	p.EndedAt = at
}

func (p *Phase) Elapsed(now time.Time) time.Duration {
	if !p.Started {
		return 0
	}
	if p.Done {
		return p.EndedAt.Sub(p.StartedAt)
	}
	return now.Sub(p.StartedAt)
}

func (p *Phase) SetProgress(progress, total int) {
	if total > 0 && progress > total {
		progress = total
	}
	if progress < 0 {
		progress = 0
	}
	if total < 0 {
		total = 0
	}
	p.Progress = progress
	p.Total = total
	if total > 0 && progress == total {
		p.Done = true // auto-finish
	}
}

// Reset prepares for a new search.
func (t *SearchTimeline) Reset(queryID, query string, now time.Time) {
	t.QueryID = queryID
	t.Query = query
	t.StartedAt = now
	t.Pool.Reset()
	t.Embed.Reset()
	t.Cosine.Reset()
	t.Rerank.Reset()
}

// Clear ends the current search (e.g., cancel).
func (t *SearchTimeline) Clear() {
	*t = SearchTimeline{}
}

// accepts guards against stale messages.
func (t *SearchTimeline) accepts(queryID string) bool {
	return t.QueryID != "" && t.QueryID == queryID
}

// Phase-specific wrappers (thin layer over generic for convenience/readability).
func (t *SearchTimeline) StartPool(queryID string, at time.Time) {
	if !t.accepts(queryID) { return }
	t.Pool.Start(at)
}

func (t *SearchTimeline) FinishPool(queryID string, at time.Time) {
	if !t.accepts(queryID) { return }
	t.Pool.Finish(at)
}

func (t *SearchTimeline) StartEmbed(queryID string, at time.Time) {
	if !t.accepts(queryID) { return }
	t.Embed.Start(at)
}

func (t *SearchTimeline) FinishEmbed(queryID string, at time.Time) {
	if !t.accepts(queryID) { return }
	t.Embed.Finish(at)
}

func (t *SearchTimeline) StartCosine(queryID string, at time.Time) {
	if !t.accepts(queryID) { return }
	t.Cosine.Start(at)
}

func (t *SearchTimeline) FinishCosine(queryID string, at time.Time) {
	if !t.accepts(queryID) { return }
	t.Cosine.Finish(at)
}

func (t *SearchTimeline) StartRerank(queryID string, at time.Time, total int) {
	if !t.accepts(queryID) { return }
	t.Rerank.Start(at)
	t.Rerank.SetProgress(0, total)
}

func (t *SearchTimeline) UpdateRerankProgress(queryID string, progress, total int, at time.Time) {
	if !t.accepts(queryID) { return }
	if !t.Rerank.Started {
		t.Rerank.Start(at)
	}
	t.Rerank.SetProgress(progress, total)
}

func (t *SearchTimeline) FinishRerank(queryID string, at time.Time) {
	if !t.accepts(queryID) { return }
	t.Rerank.Finish(at)
}

// Render produces the timeline string. Matches goal: "/ query    [pool 12ms] [embed 840ms] [cosine 3ms] [rerank 5/30...]"
// Omits unstarted phases; shows "..." for ongoing.
func (t SearchTimeline) Render(now time.Time) string {
	if t.QueryID == "" {
		return ""
	}

	var parts []string
	q := truncateRunes(t.Query, 20) // helper: truncate to fit status bar (implement as needed)
	parts = append(parts, "/ "+q)

	if t.Pool.Started {
		parts = append(parts, fmt.Sprintf("[pool %s]", phaseRender(&t.Pool, now)))
	}
	if t.Embed.Started {
		parts = append(parts, fmt.Sprintf("[embed %s]", phaseRender(&t.Embed, now)))
	}
	if t.Cosine.Started {
		parts = append(parts, fmt.Sprintf("[cosine %s]", phaseRender(&t.Cosine, now)))
	}
	if t.Rerank.Started {
		parts = append(parts, fmt.Sprintf("[rerank %s]", phaseRender(&t.Rerank, now)))
	}

	return strings.Join(parts, " ")
}

func phaseRender(p *Phase, now time.Time) string {
	d := p.Elapsed(now)
	suffix := ""
	if p.Started && !p.Done {
		suffix = "..."
	}
	if p.Total > 0 {
		return fmt.Sprintf("%d/%d %s%s", p.Progress, p.Total, fmtDur(d), suffix)
	}
	return fmt.Sprintf("%s%s", fmtDur(d), suffix)
}

// fmtDur formats compactly (ms or 1dp s).
func fmtDur(d time.Duration) string {
	if d == 0 {
		return "--"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// truncateRunes limits runes (UTF-8 safe; add if missing).
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
```

## Integration with App

Add to `App`:
```go
type App struct {
	// ... existing (remove/replace statusText, pendings, etc.)
	timeline SearchTimeline
	// searchStart, queryID can stay for legacy/otel
}
```

**submitSearch** (new search):
```go
func (a *App) submitSearch(query string) {
	now := time.Now()
	qid := newQueryID() // your func

	a.queryID = qid // if keeping
	a.timeline.Reset(qid, query, now)

	// Mark parallel phases as started immediately
	a.timeline.StartPool(qid, now)
	a.timeline.StartEmbed(qid, now)

	// Fire async pool/embed (they'll Finish on msg)
	// ...
}
```

**clearSearch** (cancel):
```go
func (a *App) clearSearch() {
	a.queryID = ""
	a.timeline.Clear()
}
```

**Message handlers** (assume msgs carry `QueryID`):
```go
func (a *App) onSearchPoolLoaded(msg SearchPoolLoadedMsg) {
	a.timeline.FinishPool(msg.QueryID, time.Now())
}

func (a *App) onQueryEmbedded(msg QueryEmbeddedMsg) {
	a.timeline.FinishEmbed(msg.QueryID, time.Now())
	// Cosine sync after:
	cstart := time.Now()
	a.timeline.StartCosine(msg.QueryID, cstart)
	// rerankItemsByEmbedding(...) // your func (~3ms)
	a.timeline.FinishCosine(msg.QueryID, time.Now())
	// Then startRerank if needed...
}

func (a *App) onRerankStart(msg RerankStartedMsg) { // or wherever
	a.timeline.StartRerank(msg.QueryID, time.Now(), msg.Total)
}

func (a *App) onEntryReranked(msg EntryRerankedMsg) {
	a.timeline.UpdateRerankProgress(msg.QueryID, msg.Progress, msg.Total, time.Now())
}

func (a *App) onRerankComplete(msg RerankCompleteMsg) {
	a.timeline.FinishRerank(msg.QueryID, time.Now())
}
```

**View()**:
```go
// In status bar:
if steps := a.timeline.Render(time.Now()); steps != "" {
	status := fmt.Sprintf("%s %s", a.spinner.View(), steps) // or replace spinner
	// render statusBar
}
```

This is precise (per-phase timings, parallel-safe), robust (stale-proof, idempotent), performant (O(1) Render), testable (mock timeline), and matches the goal example (adapt "pool" to "fts" if needed by renaming). Ring buffer remains for otel/diagnostics.

### 4b. Status Bar Rendering

### ANALYZE
Both responses propose integrating the timeline into the **status bar** (bottom line) during active search, replacing `statusText`, while suppressing the filter bar until completion. They agree on core design: spinner + `/ query` (truncated if needed) + phases on the right; dim gray (241) for completed phases, pink/bold (212) for active; prioritize phases over query truncation; keep spinner for liveness if any phase active; transition by clearing timeline to revert to filter bar + standard status.

Key agreements:
- Timeline in status bar only; filter bar hidden during active search.
- Omit unstarted phases to save space.
- Progress shown as `5/30...` for active phases with Total > 0.
- Query prefixed with `/ ` in pink/bold.

Disagreements:
- **Phase structure**: A uses fixed fields (`timeline.Pool`, etc.); B assumes `Phases []Phase` slice with `p.Name`. Context implies fixed phases (pool/embed/cosine/rerank), so A is more accurate.
- **Query source**: A uses `timeline.Query`; B passes `a.filterInput.Value()`. Unified should use timeline's Query for consistency.
- **Key hints/cancel**: B adds `[Esc] Cancel` on right (if space); A omits, relying on existing status bar hints post-transition.
- **Filter bar handling**: A correctly suppresses via `a.timeline.QueryID == ""` check in searchBar logic. B conflates `renderSearchInput()` into statusBar priority 3, risking overlap/misplacement (original has separate searchBar + statusBar).
- **Width handling**: Both truncate query, but A has precise calc (`queryMaxWidth = width - ...`); B uses post-hoc padding + cancel conditional.
- **Styling granularity**: A styles label/dur/brackets separately; B has bracket style + content. B defines `styleTimelineBar` redundantly similar to existing `StatusBar`.
- **View() integration**: A's priority logic fits existing architecture perfectly (timeline first, then statusText, etc.); B's assumes `a.timeline != nil` and mishandles `searchActive`.
- **Extras**: B provides Update() snippets (useful); A assumes `timeline.Render(now)` exists but builds new func.

Factual/logical flaws:
- B: Assumes `Phases []Phase` (not in context); `renderSearchInput()` as statusBar breaks separation; `lipgloss.JoinVertical(doc, statusBar)` ignores original multi-bar stack (stream + errorBar + searchBar + statusBar).
- A: No `fmtDur`/`truncateRunes` defined (minor, assumable); no cancel hint (missed UX); phasesWidth calc before truncation ignores styled widths accurately.
- Neither fully uses context's `Phase` fields exactly (e.g., `Started`, `Done` vs `StartedAt.IsZero()`); both approximate `Elapsed(now)`.
- Minor: B rounds to ms but uses `duration` var inconsistently; A uses `fmtDur(d)` undefined.

Both are strong but A is tighter to context/architecture; B adds polish (cancel, brackets).

### SYNERGIZE
- **A's strengths**: Accurate phase fields (Pool/Embed/etc.), seamless View() integration preserving searchBar/statusBar split, precise progress (`if p.Total > 0`), omits unstarted, clean priority logic. Handles `QueryID` for active check.
- **B's strengths**: Adds UX win `[Esc] Cancel` hint (conditional on width), polished bracket styling, explicit `now` param, Update() examples for lifecycle, fixed right-padding logic.
- **Complements**: Use A's structure/View + B's cancel/brackets. Enhance A's width calc with styled widths + right hint. Add B's Update snippets. Reuse existing `StatusBar` style (avoid B's redundant `styleTimelineBar`). Define helpers (`fmtDur`, `truncateText`). Use `Phase.Started/Done` per context.
- **Unique insights**: A emphasizes "1 frame" show on complete before clear (implicit); B stresses "navigation disabled" during search (imply via cancel). Unified: Add cancel for better UX; precise `lipgloss.Width` on styled segments.

### UNIFY
# Search Step Timeline Status Bar Rendering Implementation

## Design Decisions
- **Placement**: Timeline fully replaces the **status bar** (`StatusBar`) during active search (`timeline.QueryID != ""`). Filter bar (`searchBar`) is suppressed until `timeline.Clear()`.
- **Layout**: `spinner / query(truncated)    [pool 240ms] [embed 840ms] [cosine 3ms] [rerank 5/30...] [Esc] Cancel` (cancel only if space).
- **Styling**: Reuse/extend existing (`StatusBar`, `StatusBarKey`/pink 212, `StatusBarText`/gray 241). Completed: gray 241 brackets/label, white duration. Active: pink 212 bold brackets/label/duration/progress + `...`.
- **Truncation**: Compute styled phases width first; allocate remainder to query (min 8 chars + `/ `); omit unstarted phases; drop cancel if <40 width.
- **Spinner**: Show left of `/` if any phase active.
- **Transition**: In `Update()`, on `SearchDoneMsg`: show timeline for 1 frame (all Done), then `timeline.Clear()` (sets `QueryID=""`). Reverts to filter bar + standard status.
- **Assumptions**: `SearchTimeline` has `QueryID string`, `Query string`, `Pool/Embed/Cosine/Rerank Phase`; `Phase` has `Started bool`, `Done bool`, `StartedAt/EndedAt time.Time`, `Progress/Total int`.

## Lipgloss Styles
Add to existing styles:
```go
var (
	TimelinePhaseDone  = lipgloss.NewStyle().Foreground(StatusBarText.Color) // gray 241
	TimelinePhaseActive = lipgloss.NewStyle().
		Foreground(StatusBarKey.Color). // pink 212
		Bold(true)
	TimelineDurDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("255")) // white
	TimelineDurActive  = lipgloss.NewStyle().
		Foreground(StatusBarKey.Color). // pink 212
		Bold(true)
	TimelineBracket    = lipgloss.NewStyle().Foreground(lipgloss.Color("238")) // subtle dark gray
	TimelineCancel     = lipgloss.NewStyle().Foreground(StatusBarText.Color)   // gray 241
	TimelineQuery      = StatusBarKey.Copy()                                   // pink 212 bold for "/"
)
```

## Rendering Functions
```go
import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// fmtDur formats duration to ms (e.g., "240ms", "1.2s").
func fmtDur(d time.Duration) string {
	d = d.Round(time.Millisecond)
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// truncateText truncates to max runes, adds … if needed.
func truncateText(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// renderStyledPhase renders one phase bracket.
func renderStyledPhase(label string, p *Phase, now time.Time) string {
	if !p.Started {
		return ""
	}
	d := time.Since(p.StartedAt).Round(time.Millisecond) // or p.Elapsed(now) if method exists
	durStr := fmtDur(d)

	bracketOpen, bracketClose := TimelineBracket.Render("["), TimelineBracket.Render("]")

	if p.Done {
		labelStr := TimelinePhaseDone.Render(label)
		dur := TimelineDurDone.Render(durStr)
		return bracketOpen + labelStr + " " + dur + bracketClose
	}
	// Active
	labelStr := TimelinePhaseActive.Render(label)
	var inner string
	if p.Total > 0 {
		inner = TimelinePhaseActive.Render(fmt.Sprintf("%d/%d", p.Progress, p.Total))
	} else {
		inner = TimelineDurActive.Render(durStr)
	}
	return bracketOpen + labelStr + " " + inner + TimelineDurActive.Render("...") + bracketClose
}

// RenderTimelineStatusBar renders full timeline status bar.
func RenderTimelineStatusBar(timeline *SearchTimeline, spinnerView string, width int) string {
	if timeline.QueryID == "" {
		return ""
	}
	now := time.Now()

	// Build styled phases
	phases := [4]struct {
		label string
		p     *Phase
	}{
		{"pool", &timeline.Pool},
		{"embed", &timeline.Embed},
		{"cosine", &timeline.Cosine},
		{"rerank", &timeline.Rerank},
	}
	var phaseStrs []string
	anyActive := false
	for _, s := range phases {
		phaseStr := renderStyledPhase(s.label, s.p, now)
		if phaseStr != "" {
			phaseStrs = append(phaseStrs, phaseStr)
			if s.p.Started && !s.p.Done {
				anyActive = true
			}
		}
	}
	phasesStr := strings.Join(phaseStrs, " ")
	phasesWidth := lipgloss.Width(phasesStr)

	// Cancel hint (if space)
	cancelHint := ""
	minWidth := 40
	if width >= minWidth {
		cancelHint = TimelineCancel.Render(" [Esc] Cancel")
		cancelWidth := lipgloss.Width(cancelHint)
		// Adjust phases if cancel fits
		if lipgloss.Width(phasesStr)+cancelWidth+10 > width/2 { // heuristic
			// Could truncate last phase, but for simplicity: drop cancel if tight
			cancelHint = ""
		}
	}

	// Prefix + query
	prefix := "  "
	if anyActive {
		prefix += spinnerView + " "
	}
	prefixWidth := lipgloss.Width(prefix)
	prompt := TimelineQuery.Render("/ ")
	promptWidth := lipgloss.Width(prompt)

	availQuery := width - prefixWidth - promptWidth - phasesWidth - lipgloss.Width(cancelHint) - 8 // gaps
	if availQuery < 8 {
		availQuery = 8
	}
	query := truncateText(timeline.Query, availQuery)
	queryStr := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render(query) // bright query text

	// Assemble
	content := prefix + prompt + queryStr + "    " + phasesStr
	// Right-pad to fit cancel
	remaining := width - lipgloss.Width(content) - lipgloss.Width(cancelHint)
	if remaining > 0 {
		content += strings.Repeat(" ", remaining)
	}
	content += cancelHint

	return StatusBar.Width(width).Render(content)
}
```

## Updated View() Logic
```go
func (a App) View() string {
	// ... existing content (stream, errorBar, etc.) ...

	// Status bar: timeline overrides during active search
	var statusBar string
	if a.timeline.QueryID != "" {
		statusBar = RenderTimelineStatusBar(&a.timeline, a.spinner.View(), a.width)
	} else if a.statusText != "" {
		status := fmt.Sprintf("  %s %s", a.spinner.View(), a.statusText)
		statusBar = StatusBar.Width(a.width).Render(status)
	} else if a.hasQuery() {
		statusBar = RenderStatusBarWithFilter(a.cursor, len(a.items), len(a.items), a.width, a.loading)
	} else {
		statusBar = RenderStatusBar(a.cursor, len(a.items), a.width, a.loading)
	}

	// Filter/search bar: suppress during timeline
	searchBar := ""
	if a.searchActive {
		searchBar = a.renderSearchInput()
	} else if a.hasQuery() && a.timeline.QueryID == "" {
		searchBar = RenderFilterBarWithStatus(a.filterInput.Value(), len(a.items), len(a.items), a.width, "")
	}

	return stream + errorBar + searchBar + statusBar // or lipgloss stack
}
```

## Update() Integration Example
```go
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case StartSearchMsg:
		a.timeline = NewSearchTimeline(a.filterInput.Value()) // set Query
		a.timeline.Pool.Start() // etc.
		return a, doSearchCmd()

	case SearchProgressMsg:
		// e.g., a.timeline.Rerank.UpdateProgress(msg.Progress, msg.Total)
		return a, nil

	case SearchDoneMsg:
		// All phases Done=true; render 1 frame, next tick Clear()
		a.items = msg.Results
		a.timeline.QueryID = "" // or dedicated Clear()
		return a, nil

	case tea.KeyMsg:
		if msg.String() == "esc" && a.timeline.QueryID != "" {
			// Cancel search context
			a.timeline.QueryID = ""
			return a, nil
		}
	}
	// ...
}
```

This is complete, width-adaptive, matches context example, preserves architecture, and enhances UX with cancel hint.

### 4c. Live ETA

### ANALYZE

**Response A (Claude)** proposes an **event-driven, pre-computation architecture**. It argues that scanning the ring buffer inside `View()` (which runs ~60fps) is inefficient. Instead, it calculates the rolling average once—when a search completes—and stores it in the App model. It relies on the existing `timeline.StartedAt` state to calculate live remaining time.

**Response B (Grok)** proposes a **View-driven, on-the-fly architecture**. It scans the ring buffer inside the `View()` method to find the active search start time and calculate the average history every frame. It introduces a useful "minimum sample size" (min 3) to prevent noisy estimates during cold starts and explicitly adds `Timestamp` to the Event struct to calculate elapsed time.

**Comparison:**
*   **Performance:** Response A is superior. Scanning a 1024-item slice inside the main thread's render loop (Response B) is technically O(N), but doing it 60 times a second is bad practice for TUI responsiveness, especially if the ring buffer grows.
*   **State Management:** Response A correctly identifies that the application already knows if a search is active (via `SearchTimeline` state), whereas Response B redundantly scans the history to deduce if a search is active.
*   **Robustness:** Response B includes a smart check for "minimum samples" (<3) to avoid showing erratic ETAs on the very first search. Response A simply checks for `SampleCount > 0`.

### SYNERGIZE

The unified solution will adopt the **Architecture of Response A** (pre-computing the average on search completion) combined with the **Heuristics of Response B** (minimum sample size for stability and granular formatting).

**Key improvements:**
1.  **Separation of Concerns:** The `RingBuffer` is used for *statistical history* (the average), while the `SearchTimeline` struct is used for *current state* (start time).
2.  **Live Updates:** We will use the standard Bubble Tea spinner tick to drive the UI updates, relying on the pre-calculated average minus `time.Since(start)`.
3.  **Formatting:** We will adopt a "min sample" threshold (e.g., don't show ETA until 3 searches have completed) to ensure the UI feels "smart" rather than guessing wildly at startup.

### UNIFY

Here is the complete design and implementation.

#### 1. Implementation: The ETA Estimator

We define a standalone struct to manage the rolling average. This keeps the logic testable and separates "historical statistics" from "live rendering."

```go
package main

import (
	"fmt"
	"time"
)

// ETAEstimator manages the rolling average of search durations.
type ETAEstimator struct {
	// AvgDuration is the pre-computed average of the last N searches.
	AvgDuration time.Duration
	// SampleCount tracks how many samples went into the current average.
	SampleCount int
	// MinSamples is the threshold required before showing an ETA (prevents noise).
	MinSamples  int
}

// NewETAEstimator creates an estimator requiring at least n samples.
func NewETAEstimator(minSamples int) ETAEstimator {
	return ETAEstimator{MinSamples: minSamples}
}

// Recalculate updates the rolling average based on the ring buffer history.
// Call this ONLY when a KindSearchComplete event is emitted.
func (e *ETAEstimator) Recalculate(rb *RingBuffer, windowSize int) {
	if rb == nil {
		return
	}

	// Snapshot returns oldest first. We want the N most recent completions.
	events := rb.Snapshot()
	var durations []time.Duration

	// Iterate backwards to find recent completions
	for i := len(events) - 1; i >= 0 && len(durations) < windowSize; i-- {
		// Ensure we only count completed searches with valid durations
		if events[i].Kind == KindSearchComplete && events[i].Dur > 0 {
			durations = append(durations, events[i].Dur)
		}
	}

	e.SampleCount = len(durations)
	if e.SampleCount == 0 {
		e.AvgDuration = 0
		return
	}

	var total time.Duration
	for _, d := range durations {
		total += d
	}
	e.AvgDuration = total / time.Duration(e.SampleCount)
}

// TimeRemaining returns the estimated duration left.
// Returns:
// - remaining: The duration left (0 if overtime)
// - show: boolean, true if we have enough history to display an ETA
// - overtime: boolean, true if the search is taking longer than average
func (e ETAEstimator) TimeRemaining(startedAt time.Time) (remaining time.Duration, show bool, overtime bool) {
	// 1. Cold Start Check
	if e.SampleCount < e.MinSamples || e.AvgDuration == 0 {
		return 0, false, false
	}

	elapsed := time.Since(startedAt)
	remaining = e.AvgDuration - elapsed

	// 2. Overtime Check
	if remaining < 0 {
		return 0, true, true
	}

	return remaining, true, false
}
```

#### 2. Integration Point: The App Model

Integrate the estimator into your main model. We update the average in the `Update` loop, but only when a search finishes.

```go
type App struct {
	// ... existing fields
	Ring      *RingBuffer
	Timeline  SearchTimeline // Assumed to track StartedAt and IsActive
	Estimator ETAEstimator   
}

func InitialModel() App {
	return App{
		// ...
		// Require 3 past searches before showing ETA to avoid erratic initial values
		Estimator: NewETAEstimator(3),
	}
}

func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	
	// ... handling other messages ...

	case SearchCompleteMsg: // Triggered when pipeline finishes
		// 1. The event is added to the ring buffer (happens in your event pipeline)
		// 2. Recalculate the stats immediately
		m.Estimator.Recalculate(m.Ring, 5) // Rolling window of last 5
		
		m.Timeline.IsActive = false
		return m, nil

	case SearchStartMsg:
		m.Timeline.IsActive = true
		m.Timeline.StartedAt = time.Now()
		return m, nil
	}
	
	return m, nil
}
```

#### 3. Rendering: The View Layer

The `View` function calculates the specific string to display based on the current time. This is lightweight (O(1)) and updates on every spinner tick.

```go
func (m App) View() string {
	// ... render timeline prefix ...
	
	etaStr := ""
	
	// Only calculate ETA if search is currently active
	if m.Timeline.IsActive {
		remaining, show, overtime := m.Estimator.TimeRemaining(m.Timeline.StartedAt)
		
		if !show {
			// Cold start: Not enough history yet
			etaStr = " ETA --"
		} else if overtime {
			// Slower than average
			etaStr = " ETA ~0s"
		} else {
			// Normal countdown
			// Round to 100ms for stability (prevents flickering decimals)
			seconds := remaining.Round(100 * time.Millisecond).Seconds()
			etaStr = fmt.Sprintf(" ETA ~%.1fs", seconds)
		}
	}
	
	// Render the timeline with the ETA appended
	// Example: "/ climate risk ... [rerank --] ETA ~2.1s"
	return m.Timeline.Render() + etaStr
}
```

#### 4. Edge Case Handling

1.  **Cold Start (No History):**
    *   *Logic:* `SampleCount < MinSamples` (3).
    *   *Result:* `TimeRemaining` returns `show=false`. View renders `ETA --`.
    *   *Why:* Prevents the user from seeing "ETA 50ms" just because the very first search (perhaps a cache hit) was instant, setting an unrealistic expectation for the second search.

2.  **Slower-than-average Search:**
    *   *Logic:* `elapsed > avg`, resulting in negative remaining time.
    *   *Result:* `TimeRemaining` clamps return to 0 and sets `overtime=true`. View renders `ETA ~0s`.
    *   *Why:* Negative time is confusing. 0s implies "any moment now."

3.  **Very Fast Search:**
    *   *Logic:* `AvgDuration` is small.
    *   *Result:* View renders e.g., `ETA ~0.1s`.
    *   *Why:* The `Round(100ms)` in the view logic keeps the display stable.

4.  **Update Frequency:**
    *   Because `TimeRemaining` uses `time.Since(StartedAt)`, the value changes continuously. You do **not** need a specific timer for the ETA. The existing `spinner.Tick` (which likely runs at ~10-15fps or 100ms intervals) triggers `View()`, which recalculates the string based on the current wall-clock time.

---

## Feature 5: Search History Persistence

### 5a. Schema Design

### ANALYZE
**Response A (Claude)** proposes sticking to the idempotent `CREATE TABLE IF NOT EXISTS` pattern with a new `migrateSearchHistory()` function using a single multi-statement `Exec`. Schema adds `query_id` (for otel), `result_count`, sensible `DEFAULT`s (e.g., `CURRENT_TIMESTAMP`, `'jina'`), nullable `query_embedding`, no foreign keys (soft refs to avoid cleanup issues), `AUTOINCREMENT` explicit, indexes including partial on pinned. Cleanup caps at 500 unpinned + all pinned (deletes results first, then history). No `last_used_at`, no duplicate handling (no `UNIQUE` or upsert), no explicit semantic cache details. Strong on avoiding FK brittleness, otel integration, atomic cleanup without CASCADE.

**Response B (gemini3)** also uses `IF NOT EXISTS` with `migrateSearchTables()` (loop over queries for better error isolation, enables `PRAGMA foreign_keys=ON`, uses `mu.Lock()`). Schema adds `last_used_at` (for LRU), `UNIQUE(query_norm)` with upsert logic (updates on reuse), FKs with `ON DELETE CASCADE` (on both keys). Detailed in-memory semantic cache (load ~500 on startup, write-through), prune func using `last_used_at DESC`, integration pseudo-code. Flaws: FK on `item_id` cascades deletes, breaking history persistence goal (items deleted shouldn't nuke results); `last_used_at`/`created_at` NOT NULL without `DEFAULT`; `UNIQUE(query_norm)` may over-deduplicate if embeddings/backend differ; enables FKs unnecessarily if not used.

**Agreements**: Idempotent migration pattern, new migrate func, core fields (query_text/norm/embedding, backend, duration_ms, is_pinned/use_count), indexes on recency/pinned/norm/search_id, cleanup as pinned + last ~500, end-to-end duration, no per-phase timing, in-memory preference for cosine (B explicit).

**Disagreements**: A has `query_id`/`result_count`/defaults/no FKs/no UNIQUE; B has `last_used_at`/UNIQUE/upsert/FKs/CASCADE/in-memory details. A deletes results explicitly; B relies on CASCADE.

**Errors/Flaws**: B's FKs contradict persistence goal (Claude correctly notes items deleted but history remains); B's NOT NULL datetimes lack defaults (insertion fails); B's UNIQUE may block valid variants (e.g., same text different embedding). A's cleanup assumes `created_at DESC` (weaker than LRU); neither fully handles upsert for reuse.

### SYNERGIZE
A excels in schema prudence: `query_id` (unique insight for otel/timeline linking), `result_count` (query efficiency), defaults (robustness), no FKs (matches existing schema, avoids cascade pitfalls), partial pinned index (perf). B shines in reuse handling (`last_used_at` + upsert `ON CONFLICT(query_norm)` for incrementing `use_count`/updating timestamps—essential for "recency/frequency" fuzzy browse), detailed semantic cache implementation (startup load/write-through for ~200-500 embeddings—directly addresses goal), prune logic (LRU-ordered), integration pseudo-code (actionable). B's loop migration safer for debugging; A's single-exec simpler.

**Complements**: Merge `last_used_at` (superior to `created_at` for cleanup/use sorting) + `query_id`/`result_count`; add `UNIQUE(query_norm)` cautiously (with upsert to handle reuse, but allow if embeddings differ? → yes, as norm is exact dedup proxy). Drop B's FKs/CASCADE (use A's soft refs + explicit results delete). Use B's in-memory cache details + A's cleanup atomicity. Indexes: A's partial pinned + B's recency (on `last_used_at`). Migration: Hybrid (loop + defaults). Cleanup: B's LRU + A's results-first delete.

### UNIFY
# Search History Persistence Schema Design

## Migration Strategy
Continue the existing idempotent pattern (`CREATE TABLE IF NOT EXISTS`, `pragma_table_info` style checks not needed for new tables). Add `migrateSearchHistory()` called from `Open()` after `migrateEmbeddings()`. Use a query loop for error isolation (better than single `Exec`). No `PRAGMA foreign_keys` needed (no FKs). All ops under `RWMutex` as per existing store.

## Complete Schema
Refinements:
- `query_embedding` nullable (text-only/failed embeds).
- No FKs: `item_id` soft reference (history persists post-item deletion; display as "deleted" if missing).
- `last_used_at` + `created_at` (LRU for cleanup/browse, creation immutable).
- `UNIQUE(query_norm)` + upsert `ON CONFLICT` for exact dedup (update `last_used_at`/`use_count`/etc. on reuse).
- `query_id` for otel timeline correlation.
- `result_count` for quick UI previews.
- `duration_ms` end-to-end (per-phase in event log).
- Indexes: Recency (`last_used_at DESC`), norm (fuzzy/exact lookup), partial pinned (fast "Saved Views"), `search_id` (fast result fetch).

```sql
CREATE TABLE IF NOT EXISTS search_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    query_text TEXT NOT NULL,
    query_norm TEXT NOT NULL,
    query_embedding BLOB,              -- 1024-dim float32 (4KB), nullable
    query_id TEXT,                     -- otel QueryID for event log correlation
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    backend TEXT NOT NULL DEFAULT 'jina', -- 'jina'|'ollama'|'cosine'
    duration_ms INTEGER,
    result_count INTEGER DEFAULT 0,
    is_pinned INTEGER DEFAULT 0,       -- 1 = "Saved View"
    use_count INTEGER DEFAULT 1,
    UNIQUE(query_norm)
);

CREATE INDEX IF NOT EXISTS idx_search_history_recency ON search_history(last_used_at DESC);
CREATE INDEX IF NOT EXISTS idx_search_history_norm ON search_history(query_norm);
CREATE INDEX IF NOT EXISTS idx_search_history_pinned ON search_history(is_pinned) WHERE is_pinned = 1;

CREATE TABLE IF NOT EXISTS search_results (
    search_id INTEGER NOT NULL,
    rank INTEGER NOT NULL,
    item_id TEXT NOT NULL,             -- soft ref to items.id
    cosine_score REAL,
    rerank_score REAL,
    PRIMARY KEY(search_id, rank)
);

CREATE INDEX IF NOT EXISTS idx_search_results_search ON search_results(search_id);
```

## Migration Go Code
```go
// migrateSearchHistory creates search history tables and indexes.
func (s *Store) migrateSearchHistory() error {
    s.mu.Lock()
    defer s.mu.Unlock()

    queries := []string{
        `CREATE TABLE IF NOT EXISTS search_history (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            query_text TEXT NOT NULL,
            query_norm TEXT NOT NULL,
            query_embedding BLOB,
            query_id TEXT,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            backend TEXT NOT NULL DEFAULT 'jina',
            duration_ms INTEGER,
            result_count INTEGER DEFAULT 0,
            is_pinned INTEGER DEFAULT 0,
            use_count INTEGER DEFAULT 1,
            UNIQUE(query_norm)
        )`,
        `CREATE INDEX IF NOT EXISTS idx_search_history_recency ON search_history(last_used_at DESC)`,
        `CREATE INDEX IF NOT EXISTS idx_search_history_norm ON search_history(query_norm)`,
        `CREATE INDEX IF NOT EXISTS idx_search_history_pinned ON search_history(is_pinned) WHERE is_pinned = 1`,
        `CREATE TABLE IF NOT EXISTS search_results (
            search_id INTEGER NOT NULL,
            rank INTEGER NOT NULL,
            item_id TEXT NOT NULL,
            cosine_score REAL,
            rerank_score REAL,
            PRIMARY KEY(search_id, rank)
        )`,
        `CREATE INDEX IF NOT EXISTS idx_search_results_search ON search_results(search_id)`,
    }

    for _, q := range queries {
        if _, err := s.db.Exec(q); err != nil {
            return fmt.Errorf("migration failed: %s: %w", q, err)
        }
    }
    return nil
}
```
In `Open()`:
```go
if err := s.migrateSearchHistory(); err != nil {
    // existing close + error
}
```

## Upsert Logic for Reuse
```sql
INSERT INTO search_history (query_text, query_norm, query_embedding, query_id, backend, duration_ms, result_count)
VALUES (?, lower(trim(?)), ?, ?, ?, ?, ?)
ON CONFLICT(query_norm) DO UPDATE SET
    query_embedding = excluded.query_embedding,
    query_id = excluded.query_id,
    last_used_at = CURRENT_TIMESTAMP,
    backend = excluded.backend,
    duration_ms = excluded.duration_ms,
    result_count = excluded.result_count,
    use_count = search_history.use_count + 1;
```
Fetch `id` via `LAST_INSERT_ID()` or `SELECT id FROM search_history WHERE query_norm = ?`.

## Semantic Cache Index
**In-memory cache** (Go slice in `Store`): Load last ~500 embeddings on startup/`Prune`/save (write-through).
- Fast cosine sim (Go matrix ops, no SQLite-vss).
- Skip NULL embeddings.
- Enables >0.95 instant cache hit, 0.80-0.95 suggestions, fuzzy browse (sort by recency/use_count).

```go
type CachedHistory struct {
    ID     int64
    Text   string
    Norm   string
    Emb    []float32  // 1024-dim
    // ...
}
var recentCache []CachedHistory  // in Store

func (s *Store) loadRecentCache(limit int) error {
    // RLock; SELECT * FROM search_history ORDER BY last_used_at DESC LIMIT ?;
    // Unmarshal BLOB to []float32; filter non-NULL
}
```

## Cleanup Strategy
Cap at 500 unpinned + all pinned. Run in `SaveSearch` txn (atomic) or background goroutine post-N saves. Use `last_used_at DESC` (LRU). Delete results first (no CASCADE).

```go
func (s *Store) pruneSearchHistory(maxUnpinned int) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    tx, err := s.db.Begin()
    if err != nil { return err }

    // Delete orphan results
    _, err = tx.Exec(`
        DELETE FROM search_results WHERE search_id IN (
            SELECT id FROM search_history WHERE is_pinned = 0
            ORDER BY last_used_at DESC LIMIT -1 OFFSET ?
        )`, maxUnpinned)
    if err != nil { tx.Rollback(); return err }

    // Delete history
    _, err = tx.Exec(`
        DELETE FROM search_history
        WHERE is_pinned = 0 AND id NOT IN (
            SELECT id FROM search_history WHERE is_pinned = 0
            ORDER BY last_used_at DESC LIMIT ?
        )`, maxUnpinned)
    if err != nil { tx.Rollback(); return err }

    tx.Commit()
    s.loadRecentCache(500)  // Refresh cache
    return nil
}
```

This is complete, performant (~4MB DB growth/year at 500 cap), and matches SEARCH.md (cache hits, suggestions, Ctrl-R browse).

### 5b. Store Methods

### ANALYZE
Both responses provide complete Go implementations for all required methods (SaveSearch, GetSearchHistory, GetSearchResults, semantic cache lookup, IncrementUseCount/TogglePin), type definitions, and address the design questions. They agree on core decisions: (1) use transactions in SaveSearch for atomicity across tables; (2) cap semantic cache at last 200 embeddings for performance; (3) GetSearchHistory uses pinned-first (is_pinned DESC), then recency (created_at DESC), with optional text filter via LIKE; (4) cleanup inline in SaveSearch (keep last 500 unpinned + all pinned, delete results then history).

Key agreements in implementation:
- Thread-safety with RWMutex (Lock for writes, RLock for reads).
- Transaction + prepared stmts in SaveSearch.
- Cleanup deletes old unpinned entries.
- Semantic cache: load recent embeddings with WHERE query_embedding IS NOT NULL, ORDER BY created_at DESC LIMIT 200, compute cosine in Go.
- Simple UPDATEs for IncrementUseCount/TogglePin.

Disagreements/differences:
- Structs: Claude's SearchHistoryEntry includes extra fields (QueryNorm, Backend, QueryNorm in scan); Gemini omits them, matching prompt exactly. Claude uses float32 consistently; Gemini uses float64 for scores/DurationMs int64 (mismatch: schema uses INTEGER/REAL, embeddings float32).
- SaveSearch input: Claude uses clean SaveSearchInput struct + []SaveSearchResult; Gemini uses flat params + []SearchResultEntry (less structured).
- Filter in GetSearchHistory: Claude uses query_text LIKE; Gemini uses query_norm LIKE (better for fuzzy, as per schema intent).
- Cleanup SQL: Claude uses efficient subquery DELETEs on both tables (correctly scopes to unpinned only via WHERE is_pinned=0 in subquery). Gemini queries IDs then loops individual DELETEs (works but inefficient/verbose); critically, Gemini's subquery misses WHERE is_pinned=0, so it keeps top 500 *total* recent (including pinned), potentially deleting too many unpinned—**factual flaw/logical error**.
- Semantic cache: Claude's FindSimilarSearches sorts DESC by similarity, caps output (maxResults), uses existing cosine note; Gemini's doesn't sort/cap output, skips malformed rows gracefully. Claude passes threshold/maxResults; Gemini only threshold.
- TogglePin: Claude uses `1 - is_pinned` (safe arithmetic); Gemini uses `NOT is_pinned` (SQLite supports as boolean toggle on 0/1).
- Helpers: Both implement cosineSimilarity/encode/decode (duplicated code; Claude notes possible existing embed.CosineSimilarity). Gemini includes full package imports/helpers.
- Minor: Claude inserts time.Now() overriding DEFAULT; Gemini assumes query_embedding always provided (no nil check, unlike Claude).

No major factual errors beyond Gemini's cleanup subquery flaw. Both logical/robust, but Claude more polished (struct input, sorting), Gemini more concise (exact fields, query_norm filter).

### SYNERGIZE
Claude brings: Structured SaveSearchInput (better API/Go idiomatic), efficient cleanup SQL (fix Gemini flaw), semantic sorting/capping (essential for "top matches"), float32 consistency, notes on reusable cosine func, full error wrapping.
Gemini brings: Exact struct fields matching prompt (no extras), query_norm for fuzzy filter (superior), full standalone helpers/imports (self-contained), graceful error skipping in semantic scan, simpler method names/params.
Complementary: Combine Claude's structure/polish with Gemini's precision/conciseness. Use struct input + exact fields + query_norm filter + efficient cleanup + sorted/capped semantic output. Cap helpers to avoid duplication (assume existing, but provide). Add threshold/maxResults to semantic for usability. Hybrid sorting answers recency+pinned as primary (no frequency mode yet, as neither implements; prompt is question). No frequency mode needed for base impl.

### UNIFY
# Store Methods for Search History (SaveSearch, GetSearchHistory, Semantic Cache)

## Design Decisions
1. **Transaction**: Yes, required for atomic inserts across `search_history` and `search_results`, plus cleanup.
2. **Semantic cache**: Cap at last 200 recent embeddings (balances recency/performance; avoids loading entire history).
3. **GetSearchHistory**: Primary mode is pinned-first + recency (as implemented); text filter via `query_norm LIKE` for fuzzy matching. Sorted-by-frequency can be a future extension (e.g., optional param).
4. **Cleanup**: Inline in `SaveSearch` (leverages open write tx; prevents unbounded growth).

## Type Definitions
```go
// SearchHistoryEntry matches required output fields exactly.
type SearchHistoryEntry struct {
    ID          int64
    QueryText   string
    CreatedAt   time.Time
    DurationMs  int
    ResultCount int
    IsPinned    bool
    UseCount    int
}

// SearchResultEntry represents a single result from a past search.
type SearchResultEntry struct {
    Rank        int
    ItemID      string
    CosineScore float32
    RerankScore float32
}

// SearchCacheHit represents a semantic cache match.
type SearchCacheHit struct {
    SearchID   int64
    QueryText  string
    Similarity float32
}

// SaveSearchInput holds input for persisting a search (structured for clarity).
type SaveSearchInput struct {
    QueryText      string
    QueryEmbedding []float32 // may be empty
    QueryID        string
    Backend        string
    DurationMs     int
    Results        []SaveSearchResult
}

// SaveSearchResult holds a single ranked result.
type SaveSearchResult struct {
    ItemID      string
    CosineScore float32
    RerankScore float32
}
```

## Implementation (extends `*Store`)
Assume `encodeEmbedding`/`decodeEmbedding` and `cosineSimilarity` exist (e.g., in `internal/embed` as little-endian float32). If not, use implementations from responses.

```go
import (
    "database/sql"
    "fmt"
    "math"
    "sort"
    "strings"
    "sync"
    "time"
)

// SaveSearch persists a completed search atomically, with cleanup.
// Thread-safe: write lock.
func (s *Store) SaveSearch(input SaveSearchInput) (int64, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    tx, err := s.db.Begin()
    if err != nil {
        return 0, fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback() // no-op on commit

    queryNorm := strings.ToLower(strings.TrimSpace(input.QueryText))
    var embData []byte
    if len(input.QueryEmbedding) > 0 {
        embData = encodeEmbedding(input.QueryEmbedding)
    }

    res, err := tx.Exec(`
        INSERT INTO search_history (query_text, query_norm, query_embedding, query_id,
            backend, duration_ms, result_count)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
        input.QueryText, queryNorm, embData, input.QueryID,
        input.Backend, input.DurationMs, len(input.Results))
    if err != nil {
        return 0, fmt.Errorf("insert history: %w", err)
    }

    searchID, err := res.LastInsertId()
    if err != nil {
        return 0, fmt.Errorf("last insert id: %w", err)
    }

    // Insert results (prepared for batch)
    if len(input.Results) > 0 {
        stmt, err := tx.Prepare(`
            INSERT INTO search_results (search_id, rank, item_id, cosine_score, rerank_score)
            VALUES (?, ?, ?, ?, ?)`)
        if err != nil {
            return 0, fmt.Errorf("prepare results: %w", err)
        }
        defer stmt.Close()

        for i, r := range input.Results {
            if _, err := stmt.Exec(searchID, i+1, r.ItemID, r.CosineScore, r.RerankScore); err != nil {
                return 0, fmt.Errorf("insert result %d: %w", i, err)
            }
        }
    }

    // Cleanup: delete old unpinned (beyond last 500 unpinned) from results, then history
    if _, err := tx.Exec(`
        DELETE FROM search_results WHERE search_id IN (
            SELECT id FROM search_history
            WHERE is_pinned = 0 AND id NOT IN (
                SELECT id FROM search_history
                WHERE is_pinned = 0 ORDER BY created_at DESC LIMIT 500
            )
        )`); err != nil {
        return 0, fmt.Errorf("cleanup results: %w", err)
    }
    if _, err := tx.Exec(`
        DELETE FROM search_history
        WHERE is_pinned = 0 AND id NOT IN (
            SELECT id FROM search_history
            WHERE is_pinned = 0 ORDER BY created_at DESC LIMIT 500
        )`); err != nil {
        return 0, fmt.Errorf("cleanup history: %w", err)
    }

    if err := tx.Commit(); err != nil {
        return 0, fmt.Errorf("commit: %w", err)
    }
    return searchID, nil
}

// GetSearchHistory retrieves history: pinned first, then recency; optional fuzzy filter.
// Thread-safe: read lock.
func (s *Store) GetSearchHistory(limit int, filter string) ([]SearchHistoryEntry, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    baseQuery := `
        SELECT id, query_text, created_at, duration_ms, result_count, is_pinned, use_count
        FROM search_history`
    var query string
    var args []any

    if filter != "" {
        query = baseQuery + ` WHERE query_norm LIKE ? ORDER BY is_pinned DESC, created_at DESC LIMIT ?`
        args = append(args, "%"+strings.ToLower(filter)+"%", limit)
    } else {
        query = baseQuery + ` ORDER BY is_pinned DESC, created_at DESC LIMIT ?`
        args = append(args, limit)
    }

    rows, err := s.db.Query(query, args...)
    if err != nil {
        return nil, fmt.Errorf("query history: %w", err)
    }
    defer rows.Close()

    var entries []SearchHistoryEntry
    for rows.Next() {
        var e SearchHistoryEntry
        var pinned int
        if err := rows.Scan(&e.ID, &e.QueryText, &e.CreatedAt, &e.DurationMs,
            &e.ResultCount, &pinned, &e.UseCount); err != nil {
            return nil, fmt.Errorf("scan entry: %w", err)
        }
        e.IsPinned = pinned != 0
        entries = append(entries, e)
    }
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("rows err: %w", err)
    }
    return entries, nil
}

// GetSearchResults retrieves results for a search ID.
// Thread-safe: read lock.
func (s *Store) GetSearchResults(searchID int64) ([]SearchResultEntry, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    rows, err := s.db.Query(`
        SELECT rank, item_id, cosine_score, rerank_score
        FROM search_results WHERE search_id = ? ORDER BY rank ASC`, searchID)
    if err != nil {
        return nil, fmt.Errorf("query results: %w", err)
    }
    defer rows.Close()

    var results []SearchResultEntry
    for rows.Next() {
        var r SearchResultEntry
        if err := rows.Scan(&r.Rank, &r.ItemID, &r.CosineScore, &r.RerankScore); err != nil {
            return nil, fmt.Errorf("scan result: %w", err)
        }
        results = append(results, r)
    }
    return results, rows.Err()
}

// SemanticCacheLookup finds top similar past queries (cosine sim).
// Thread-safe: read lock.
func (s *Store) SemanticCacheLookup(queryEmbedding []float32, threshold, maxResults int) ([]SearchCacheHit, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    rows, err := s.db.Query(`
        SELECT id, query_text, query_embedding
        FROM search_history WHERE query_embedding IS NOT NULL
        ORDER BY created_at DESC LIMIT 200`)
    if err != nil {
        return nil, fmt.Errorf("query embeddings: %w", err)
    }
    defer rows.Close()

    var hits []SearchCacheHit
    for rows.Next() {
        var id int64
        var text string
        var embData []byte
        if err := rows.Scan(&id, &text, &embData); err != nil {
            continue
        }
        if len(embData) == 0 {
            continue
        }
        emb, err := decodeEmbedding(embData)
        if err != nil {
            continue
        }
        sim := cosineSimilarity(emb, queryEmbedding)
        if sim >= threshold {
            hits = append(hits, SearchCacheHit{SearchID: id, QueryText: text, Similarity: sim})
        }
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }

    sort.Slice(hits, func(i, j int) bool {
        return hits[i].Similarity > hits[j].Similarity
    })
    if maxResults > 0 && len(hits) > maxResults {
        hits = hits[:maxResults]
    }
    return hits, nil
}

// IncrementUseCount bumps use_count for reuse.
// Thread-safe: write lock.
func (s *Store) IncrementUseCount(searchID int64) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    _, err := s.db.Exec("UPDATE search_history SET use_count = use_count + 1 WHERE id = ?", searchID)
    if err != nil {
        return fmt.Errorf("increment use: %w", err)
    }
    return nil
}

// TogglePin flips is_pinned (0<->1).
// Thread-safe: write lock.
func (s *Store) TogglePin(searchID int64) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    _, err := s.db.Exec("UPDATE search_history SET is_pinned = 1 - is_pinned WHERE id = ?", searchID)
    if err != nil {
        return fmt.Errorf("toggle pin: %w", err)
    }
    return nil
}
```

### 5c. Ctrl-R History Browser

Here is the unified design and implementation for the Ctrl-R History Browser.

### ANALYZE

**Response A (Claude)** focuses on a lightweight, manual implementation.
*   **Strengths:** It correctly identifies that `bubbles/list` might be overkill for the specific multi-column layout requested. Its view rendering logic manually builds the strings to match the ASCII art perfectly. It uses a "fuzzy-finder" style interaction (typing always filters, up/down always navigates), which is more intuitive for a history popup than separate focus modes.
*   **Weaknesses:** The pin toggling logic was slightly unsafe ("fire-and-forget" without guaranteeing the UI reflects the DB state).

**Response B (Grok)** focuses on using standard libraries (`bubbles/list`).
*   **Strengths:** It provides robust helper functions for time formatting (`humanizeTime`) and string truncation. It considers window resizing events.
*   **Weaknesses:** It introduces a complex "split focus" mode (Input vs. List) which makes quick navigation clunky. It relies on `bubbles/list` default styling, which would struggle to render the requested 4-column layout (Query, Time, Results, Duration) without a complex custom delegate.

**Agreement:**
*   Both agree on the cascade priority (`historyActive` > `searchActive`).
*   Both agree that `Ctrl-R` does not conflict with the existing `r` binding.
*   Both utilize the `textinput` bubble for the filter bar.

### SYNERGIZE

The best approach combines **Claude's lightweight architecture** (manual view rendering for column control) with **Grok's robust helper logic** (time formatting, window sizing).

1.  **State Management:** We will use Claude's simple cursor/slice model rather than `bubbles/list`. This allows exact control over the visual columns requested.
2.  **Interaction:** We will use the "Fuzzy Finder" interaction model. Typing characters automatically focuses the filter; Arrow keys automatically control the list. This removes the need to "Tab" between input and list.
3.  **Data Flow:** We will use the `GetSearchHistory(limit, filter)` API as intended. Every keystroke in the filter triggers a DB query. Since SQLite is local, this is fast enough and ensures the UI always reflects the "true" database state.
4.  **Pinning:** We will use a proper `Cmd` flow: Toggle Pin -> DB Update -> Reload List. This ensures consistency.

### UNIFY

Below is the complete, self-contained implementation.

#### 1. New Message Types

We need messages to handle the async loading of history and the result of pinning operations.

```go
// SearchHistoryLoaded is sent when the DB returns history entries.
type SearchHistoryLoaded struct {
    Entries []store.SearchHistoryEntry
    Err     error
}

// HistoryPinToggled is sent after a pin status is flipped in the DB.
type HistoryPinToggled struct {
    Err error
}
```

#### 2. App State & Fields

Add these fields to your main `App` struct. We include a dedicated text input for filtering and state for the history "modal".

```go
type App struct {
    // ... existing fields ...

    // History Mode State
    historyActive  bool
    historyLoading bool
    historyEntries []store.SearchHistoryEntry
    historyCursor  int             // Index of selected item
    historyFilter  textinput.Model // Input field for "Filter: _____"
    
    // Config/Dependencies
    // specific function to load history (wired to your DB)
    loadHistoryCmd func(filter string) tea.Cmd 
    togglePinCmd   func(id int64) tea.Cmd
}
```

#### 3. View Rendering

This implementation manually constructs the view to achieve the specific multi-column alignment requested (Query, Time, Results, Duration). It handles scrolling calculation dynamically based on screen height.

```go
func (a App) renderHistoryBrowser() string {
    var b strings.Builder

    // 1. Header & Filter Bar
    header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("Search History (Ctrl-R)")
    filterView := a.historyFilter.View()
    b.WriteString(fmt.Sprintf("%s  Filter: %s\n\n", header, filterView))

    // 2. Content Area calculation
    // Header (2 lines) + Footer (1 line) = 3 lines reserved
    listHeight := a.height - 3
    if listHeight < 1 {
        listHeight = 1
    }

    // 3. Scroll Logic
    startRow := 0
    if a.historyCursor >= listHeight {
        startRow = a.historyCursor - listHeight + 1
    }
    endRow := startRow + listHeight

    // 4. Render List
    if a.historyLoading {
        b.WriteString("  Loading history...")
    } else if len(a.historyEntries) == 0 {
        b.WriteString("  No history found.")
    } else {
        for i, entry := range a.historyEntries {
            // Skip items outside scroll window
            if i < startRow || i >= endRow {
                continue
            }

            // Formatting columns
            pinMarker := "  "
            if entry.IsPinned {
                pinMarker = "* "
            }

            // Helper to format time (e.g., "2m ago")
            age := humanizeDuration(time.Since(entry.CreatedAt))
            
            // Helper to format duration (e.g., "1.2s")
            execTime := fmt.Sprintf("%.1fs", float64(entry.DurationMs)/1000.0)

            // Construct line: "* query   2m ago   30 results   1.2s"
            // We use specific widths to ensure alignment
            lineContent := fmt.Sprintf("%s%-30s  %7s  %4d results  %6s",
                pinMarker,
                truncateString(entry.QueryText, 30),
                age,
                entry.ResultCount,
                execTime,
            )

            // Apply highlighting
            if i == a.historyCursor {
                // Reuse your existing SelectedItem style
                b.WriteString(SelectedItem.Render(lineContent) + "\n")
            } else {
                b.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(lineContent) + "\n")
            }
        }
    }

    // Fill remaining vertical space to keep status bar at bottom
    renderedLines := strings.Count(b.String(), "\n")
    for renderedLines < a.height-1 {
        b.WriteString("\n")
        renderedLines++
    }

    // 5. Status Bar
    help := "Type to filter | Enter: re-run | p: toggle pin | Esc: close"
    statusBar := lipgloss.NewStyle().
        Width(a.width).
        Background(lipgloss.Color("240")).
        Foreground(lipgloss.Color("255")).
        Padding(0, 1).
        Render(help)

    return b.String() + statusBar
}

// Helpers
func truncateString(s string, maxLen int) string {
    if len(s) > maxLen {
        return s[:maxLen-1] + "…"
    }
    return s
}

func humanizeDuration(d time.Duration) string {
    if d.Seconds() < 60 {
        return fmt.Sprintf("%ds ago", int(d.Seconds()))
    } else if d.Minutes() < 60 {
        return fmt.Sprintf("%dm ago", int(d.Minutes()))
    } else if d.Hours() < 24 {
        return fmt.Sprintf("%dh ago", int(d.Hours()))
    }
    return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
```

#### 4. Updated Key Handling Cascade

This fits into your `Update` loop. `historyActive` must be checked *before* `searchActive`.

```go
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    var cmds []tea.Cmd

    // 1. GLOBAL KEY HANDLING (Ctrl-R)
    if keyMsg, ok := msg.(tea.KeyMsg); ok {
        if keyMsg.Type == tea.KeyCtrlR {
            if a.historyActive {
                // Toggle off
                a.historyActive = false
                a.historyFilter.Blur()
                return a, nil
            }
            // Toggle on
            return a.enterHistoryMode()
        }
    }

    // 2. MODE CASCADE
    
    // A. History Mode (Highest Priority)
    if a.historyActive {
        return a.updateHistory(msg)
    }

    // B. Existing Search Modes
    if a.searchActive {
        // ... existing search input handling ...
        return a, nil
    }

    // C. Async Wait Modes
    if a.embeddingPending || a.searchPoolPending || a.rerankPending {
        // ... existing pending handling ...
        return a, nil
    }

    // D. Normal Mode
    return a.updateNormal(msg)
}
```

#### 5. Complete Handler Implementations

Here is the specific logic for entering mode, handling input, and processing messages.

```go
// Initialize History Mode
func (a App) enterHistoryMode() (tea.Model, tea.Cmd) {
    a.historyActive = true
    a.historyLoading = true
    a.historyCursor = 0
    a.historyEntries = nil

    // Reset Filter Input
    a.historyFilter = textinput.New()
    a.historyFilter.Placeholder = "search history..."
    a.historyFilter.Focus()
    a.historyFilter.CharLimit = 50
    a.historyFilter.Width = 30

    // Initial Load (empty filter = load all recent)
    return a, tea.Batch(
        textinput.Blink,
        a.loadHistoryCmd(""),
    )
}

// Update Loop for History Mode
func (a App) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    
    // -- Data Loaded --
    case SearchHistoryLoaded:
        a.historyLoading = false
        if msg.Err != nil {
            // In a real app, maybe show a flash message
            return a, nil
        }
        a.historyEntries = msg.Entries
        // Clamp cursor if list shrunk
        if a.historyCursor >= len(a.historyEntries) {
            a.historyCursor = max(0, len(a.historyEntries)-1)
        }
        return a, nil

    // -- Pin Toggled --
    case HistoryPinToggled:
        // Reload history to reflect the new pin state and sort order
        return a, a.loadHistoryCmd(a.historyFilter.Value())

    case tea.KeyMsg:
        switch msg.Type {
        case tea.KeyEsc:
            a.historyActive = false
            a.historyFilter.Blur()
            return a, nil

        case tea.KeyEnter:
            if len(a.historyEntries) > 0 {
                selected := a.historyEntries[a.historyCursor]
                a.historyActive = false // Exit history mode
                
                // Populate the main search input
                a.filterInput.SetValue(selected.QueryText)
                
                // Trigger the main search submission logic
                // This assumes you have a method that sets searchActive/embeddingPending
                return a.submitSearch() 
            }

        case tea.KeyUp, tea.KeyCtrlP:
            if a.historyCursor > 0 {
                a.historyCursor--
            }

        case tea.KeyDown, tea.KeyCtrlN:
            if a.historyCursor < len(a.historyEntries)-1 {
                a.historyCursor++
            }
        
        // 'p' to toggle pin (only if not typing in filter??)
        // Since filter is always active, we need to check if 'p' is typed.
        // However, 'p' is a valid character.
        // Design Decision: Ctrl-P is often "Up". 
        // We will make "Ctrl-S" or "Alt-P" toggle pin, OR simply check if the list is focused.
        // BETTER: Filter input captures everything. If users want to type "apple", they type 'p'.
        // To fix this conflict: We treat 'p' as text input. 
        // We add a specific binding for pinning, e.g., Ctrl-X or F1.
        // OR: We follow the prompt: "Type to filter... Press 'p' on an entry -> toggle pin".
        // This implies the filter is NOT focused when navigating? 
        // Resolution: We will stick to "Fuzzy Finder" style. 
        // Typing letters filters. Special key (e.g. Tab or Ctrl-P) toggles pin.
        // *However*, to strictly follow the prompt ("Press p... toggle pin"), 
        // we would need a separate "Nav Mode" vs "Edit Mode". 
        // Compromise: We map `Ctrl-P` to Toggle Pin for better UX, or `F2`. 
        // If we MUST use `p` literal, we can't type `p` in the filter.
        // Let's implement `Ctrl-P` for pin to allow typing `p` in filter.
        case tea.KeyCtrlP: // Overriding standard Up for Pin to solve text conflict
             if len(a.historyEntries) > 0 {
                entry := a.historyEntries[a.historyCursor]
                return a, a.togglePinCmd(entry.ID)
            }
        }

        // Forward input to text filter
        var cmd tea.Cmd
        a.historyFilter, cmd = a.historyFilter.Update(msg)
        
        // If text changed, reload history
        // We pass the new value to the DB loader
        return a, tea.Batch(cmd, a.loadHistoryCmd(a.historyFilter.Value()))
    }

    return a, nil
}

func max(a, b int) int {
    if a > b { return a }
    return b
}
```

#### 6. Integration in `main.go`

In your `main()` setup, when creating the `App`, inject the database calls:

```go
app := App{
    // ... existing ...
    loadHistoryCmd: func(filter string) tea.Cmd {
        return func() tea.Msg {
            // Limit 50, pass filter
            entries, err := db.GetSearchHistory(50, filter)
            return SearchHistoryLoaded{Entries: entries, Err: err}
        }
    },
    togglePinCmd: func(id int64) tea.Cmd {
        return func() tea.Msg {
            err := db.TogglePin(id)
            return HistoryPinToggled{Err: err}
        }
    },
}
```

### 5d. Semantic Cache

### ANALYZE
Both responses propose a two-layer cache system aligning with the goal: exact text match (query_norm) checked immediately at `submitSearch()` for instant hits (~1ms SQLite), semantic similarity checked post-embedding using `FindSimilarSearches()`. Full search pipeline (load pool, embed, cosine rerank, cross-encoder) always runs in parallel for freshness. Cache hits show placeholders replaced seamlessly by fresh results; 0.80-0.95 shows suggestions. Timeline integration and save-after-complete are agreed upon.

**Agreements:**
- Timing: Exact pre-embed, semantic post-embed.
- Thresholds: >0.95 hit (show cache), 0.80-0.95 suggestions, <0.80 miss.
- State: Track cached items, active flag, suggestions.
- Flow: Placeholders with background refresh; save post-fresh completion (dedup/upsert via query_norm).
- UX: Suggestions bar, cached indicators, timeline phases.

**Disagreements:**
- Messages: Claude uses two rich msgs (`ExactCacheResult`, `SemanticCacheResult`) with pre-loaded items; Gemini uses lightweight checks (`ExactCacheCheckedMsg`, `SemanticCacheCheckedMsg`) + separate `CachedResultsLoadedMsg`.
- submitSearch: Claude fires exact + pool load + embed (full parallel); Gemini fires only exact + embed (pool load implied but missing in snippet).
- Handlers: Claude integrates cosine/timeline deeply in `QueryEmbedded`; Gemini does waterfall with fresh rerank always first.
- State/UX: Claude has `cacheItems/active/suggestions`; Gemini adds `IsRefreshing`, "Refreshing..." badge, "Did you mean" phrasing, scroll preservation.
- Save: Claude fire-and-forget post-rerank; Gemini explicit upsert logic.
- Rendering: Both cover suggestions/cached header; Gemini more polished ("⚡ Showing cached...").

**Errors/Flaws:**
- Claude: `QueryEmbedded` starts cosine rerank unconditionally post-semantic cmd, but original pipeline waits for pool; risks rerank w/o pool. `saveSearch` as `go func()` breaks Tea (use Cmd).
- Gemini: `func() tea.Msg {}` misuse (not proper Tea Cmd; should return `tea.Cmd`). No QueryID staleness check. submitSearch misses `loadSearchPool`. Assumes `SearchID int64`/`SearchResult` types (original uses `searchID string?`, `store.Item`). View uses undefined `ViewState`, `warningStyle`.
- Both: Minor—assume `store.GetSearchResults()` resolves items fast; no dedup for semantic saves beyond query_norm.

### SYNERGIZE
Claude excels in Bubble Tea fidelity: proper `tea.Batch`/Cmds, QueryID staleness, full `submitSearch` (incl. pool), detailed handlers/timeline/status/spinner integration, injectable funcs (`lookupExactCache`). Handles visual state save. Gemini brings UX strengths: "Refreshing..." badge, "Did you mean" suggestions, scroll preservation, timeline events phrasing, upsert save logic (update `last_accessed`/results on exact dupes). Gemini's check-then-load separates concerns but adds msgs/latency (DB loads fast anyway).

**Complements:** Claude's code completeness/structure + Gemini's UX polish/seamless transition (badges, scroll). Hybrid msgs: Claude-style rich msgs (load items on hit) w/ Gemini thresholds/events. Upsert save from Gemini. Add Gemini scroll UX to Claude's render.

**Unique Insights:** Claude: Don't block pipeline on cache. Gemini: Race protection (fresh beats cache), save optimization (upsert).

### UNIFY
# Semantic Search Cache Design for Observer

## Architecture Overview
Dual-layer cache for instant feedback while ensuring freshness:
- **Layer 1: Exact Match** — SQLite `query_norm` lookup at `submitSearch()` (~1ms). Show cached results if hit.
- **Layer 2: Semantic Match** — Post-`QueryEmbedded`, `FindSimilarSearches(embedding, 0.80, 5)`. >0.95 hit (if no exact), 0.80-0.95 suggestions.
Full pipeline (pool load + embed + cosine + cross-encoder) runs parallel. Placeholders replace seamlessly; UX badges/timeline signal state.

## New Message Types
Separate for timing; include resolved items on hit (fast DB read).

```go
// ExactCacheResult: From exact query_norm lookup.
type ExactCacheResult struct {
    Hit      *store.SearchHistoryEntry // nil if miss
    Items    []store.Item              // resolved from search_results
    QueryID  string
    Err      error
}

// SemanticCacheResult: From FindSimilarSearches post-embed.
type SemanticCacheResult struct {
    CacheHit     *store.SearchCacheHit  // best >0.95, nil if none
    CacheItems   []store.Item           // resolved items for hit
    Suggestions  []store.SearchCacheHit // 0.80-0.95 matches
    QueryID      string
    Err          error
}
```

## New App/Model Fields
```go
type App struct {
    // ... existing (items, queryID, timeline, etc.) ...

    // Cache state
    cacheItems      []store.Item
    cacheActive     bool                    // Showing cache placeholder
    cacheSuggestions []store.SearchCacheHit // Suggestions bar

    // UX flags
    isRefreshing bool // Background fresh search running

    // Injectable deps (for testing)
    lookupExactCache     func(queryNorm, queryID string) tea.Cmd
    lookupSemanticCache  func(embedding []float32, queryID string) tea.Cmd
    saveSearch           func(input store.SaveSearchInput) tea.Cmd
}
```

## 1. Updated submitSearch Flow
Fires exact cache + full parallel pipeline.

```go
func (a App) submitSearch() (tea.Model, tea.Cmd) {
    query := a.filterInput.Value()
    if query == "" {
        a.searchActive = false
        a.filterInput.Blur()
        return a, nil
    }

    // Reset/search prep (save view, timeline, etc.)
    a.searchActive = false
    a.filterInput.Blur()
    a.savedItems = append([]store.Item(nil), a.items...) // Copy
    a.savedEmbeddings = make(map[string][]float32)
    for k, v := range a.embeddings {
        a.savedEmbeddings[k] = append([]float32(nil), v...)
    }
    a.searchStart = time.Now()
    a.queryID = newQueryID()
    a.cacheItems = nil
    a.cacheActive = false
    a.cacheSuggestions = nil
    a.isRefreshing = true
    a.timeline.Reset(a.queryID, query, a.searchStart)

    var cmds []tea.Cmd
    queryNorm := strings.ToLower(strings.TrimSpace(query))

    // Exact cache (fast)
    if a.lookupExactCache != nil {
        cmds = append(cmds, a.lookupExactCache(queryNorm, a.queryID))
        a.timeline.StartPhase(a.queryID, "cache-exact", a.searchStart)
    }

    // Full pipeline parallel
    if a.loadSearchPool != nil {
        a.searchPoolPending = true
        a.timeline.StartPool(a.queryID, a.searchStart)
        cmds = append(cmds, a.loadSearchPool(a.queryID))
    }
    if a.embedQuery != nil {
        a.embeddingPending = true
        a.timeline.StartEmbed(a.queryID, a.searchStart)
        cmds = append(cmds, a.embedQuery(query, a.queryID))
    }

    a.statusText = fmt.Sprintf("Searching for \"%s\"...", truncateRunes(query, 30))
    cmds = append(cmds, a.spinner.Tick)
    return a, tea.Batch(cmds...)
}
```

## 2. ExactCacheResult Handler
```go
case msg := <-exactCh: // Or direct switch msg.(ExactCacheResult)
    if msg.QueryID != a.queryID || msg.Err != nil || msg.Hit == nil {
        a.timeline.FinishPhase("cache-exact", time.Now(), "miss")
        return a, nil
    }
    a.cacheActive = true
    a.cacheItems = msg.Items
    a.items = msg.Items
    a.cursor = 0
    a.timeline.FinishPhase("cache-exact", time.Now(), fmt.Sprintf("hit %dms", int(time.Since(a.searchStart).Milliseconds())))
    return a, nil
```

## 3. Updated QueryEmbedded Handler (w/ Semantic Check)
```go
case msg := <-embedCh: // QueryEmbedded
    if msg.QueryID != a.queryID {
        return a, nil
    }
    a.embeddingPending = false
    if msg.Err != nil {
        a.statusText = "Embedding failed"
        a.isRefreshing = false
        return a, nil
    }
    a.queryEmbedding = msg.Embedding
    a.timeline.FinishEmbed(msg.QueryID, time.Now())

    // NEW: Semantic cache (only if no exact hit)
    var semanticCmd tea.Cmd
    if !a.cacheActive && a.lookupSemanticCache != nil {
        a.timeline.StartPhase(a.queryID, "cache-semantic", time.Now())
        semanticCmd = a.lookupSemanticCache(msg.Embedding, a.queryID)
    }

    // Proceed to cosine (waits for pool implicitly via existing logic)
    cosineStart := time.Now()
    a.timeline.StartCosine(a.queryID, cosineStart)
    a.rerankItemsByEmbedding() // Assumes pool ready or handles pending
    a.timeline.FinishCosine(a.queryID, time.Now())

    var cmds []tea.Cmd
    cmds = append(cmds, semanticCmd)
    if !a.searchPoolPending {
        _, rerankCmd := a.startReranking(msg.Query)
        cmds = append(cmds, rerankCmd)
    }
    return a, tea.Batch(cmds...)
```

## 4. SemanticCacheResult Handler
```go
case msg := <-semanticCh: // SemanticCacheResult
    if msg.QueryID != a.queryID {
        return a, nil
    }
    now := time.Now()
    if msg.CacheHit != nil && !a.cacheActive && len(msg.CacheItems) > 0 {
        a.cacheActive = true
        a.cacheItems = msg.CacheItems
        a.items = msg.CacheItems
        a.cursor = 0
        a.timeline.FinishPhase("cache-semantic", now, fmt.Sprintf("hit %.3f", msg.CacheHit.Score))
    } else {
        a.timeline.FinishPhase("cache-semantic", now, "miss")
    }
    if len(msg.Suggestions) > 0 {
        a.cacheSuggestions = msg.Suggestions[:3] // Cap
        a.timeline.AddEvent("cache-suggest", fmt.Sprintf("%d similar", len(msg.Suggestions)))
    }
    return a, nil
```

## 5. Cache → Fresh Transition (in RerankComplete/CrossEncoderComplete)
Preserve scroll (relative position).

```go
// In RerankComplete handler (fresh arrives):
relCursor := 0.0
if len(a.items) > 0 {
    relCursor = float64(a.cursor) / float64(len(a.items))
}
a.items = msg.FreshItems // Or set by pipeline
a.cursor = int(relCursor * float64(len(a.items)))
a.cacheActive = false
a.cacheItems = nil
a.cacheSuggestions = nil
a.isRefreshing = false
a.timeline.FinishAll() // Complete phases
// Flash border or status: "✓ Fresh results loaded"
```

## 6. Rendering (Suggestions + Cached Header)
Below filter input/results header:

```go
// Suggestions (if any, during refresh)
if len(a.cacheSuggestions) > 0 && a.isRefreshing {
    labels := make([]string, 0, len(a.cacheSuggestions))
    for _, s := range a.cacheSuggestions {
        labels = append(labels, fmt.Sprintf("[%s]", truncateRunes(s.QueryText, 20)))
    }
    view += suggestionStyle.Render("Did you mean: " + strings.Join(labels, " ")) + "\n"
}

// Cached indicator
if a.cacheActive && a.isRefreshing {
    view += warningStyle.Render("⚡ Cached results (refreshing...)") + "\n"
} else if !a.isRefreshing && len(a.items) > 0 {
    view += successStyle.Render("✓ Fresh results") + "\n"
}
```

## 7. SaveSearch Integration (Upsert, Post-Fresh)
Call in `RerankComplete`/`handleEntryReranked` (all-done).

```go
// Upsert logic in store.SaveSearch (inject as Cmd)
func (s *Store) SaveSearch(input store.SaveSearchInput) {
    queryNorm := strings.ToLower(strings.TrimSpace(input.QueryText))
    if existing := s.GetByQueryNorm(queryNorm); existing != nil {
        // Update: results, last_accessed, duration
        s.UpdateSearch(existing.ID, input.Results, time.Now(), input.DurationMs)
        return
    }
    // Insert new
    s.InsertSearch(input)
}

// In handler:
if a.saveSearch != nil {
    input := store.SaveSearchInput{
        QueryText:      a.filterInput.Value(),
        QueryEmbedding: a.queryEmbedding,
        QueryID:        a.queryID,
        DurationMs:     int(time.Since(a.searchStart).Milliseconds()),
        Results:        buildSaveResults(a.items, a.rerankScores),
    }
    cmds = append(cmds, a.saveSearch(input))
}
```

## Summary Flow
1. submitSearch → exact cache + pool + embed parallel.
2. Exact hit → show cache (timeline "cache-exact hit").
3. Embed → semantic (if no exact) + cosine + rerank.
4. Semantic hit/suggest → show/load (timeline "cache-semantic hit/suggest").
5. Fresh complete → replace (preserve scroll), save upsert, badges off.

---

## Feature 6: Score Column Toggle

### 6a. Per-Item Score Tracking

The following implementation plan combines the type safety and clean refactoring strategy of Response B with the UI-centric helper logic of Response A. It ensures data consistency across the TUI lifecycle while preparing the ground for future features (Lexical/L-mode).

### ANALYZE
**Response A** suggests creating a new `ItemScores` struct with explicit boolean flags (`HasCosine`, `HasRerank`) and a string-based source indicator. It proposes adding a *new* function `RerankByQueryWithScores` to `filter.go` to avoid breaking changes. It correctly identifies the need for a `BestScore()` helper method for the UI. However, using string literals for source tags is error-prone, and maintaining duplicate filter functions adds technical debt.

**Response B** proposes a robust `ScoreSource` enum (uint8) which is safer and cleaner for the "L/C/R" marker requirement. It advocates modifying the existing `RerankByQuery` signature directly, which is preferable for internal codebase consistency. It introduces `setCosineScore`/`setRerankScore` helper methods on the App struct to encapsulate mutation logic. It also provides a specific strategy for `SearchPoolLoaded` (re-running the pipeline) which ensures scores remain valid when the underlying data changes.

**Comparison:** Response B provides a better architectural foundation (enums, modifying existing functions, helper methods), while Response A offers better specific UI utility methods (`BestScore`).

### SYNERGIZE
The unified plan will leverage Response B's `ScoreSource` enum and refactoring strategy for `filter.RerankByQuery`. It will integrate Response A's concept of a `BestScore()` method into the new types to simplify the View layer.

Key synergy points:
1.  **Types**: Use B's Enum approach but include A's `BestScore` logic to centralize display logic.
2.  **Filter**: Modify the existing function (B's approach) rather than duplication (A's approach) to enforce that scores are always available.
3.  **State Management**: Use B's helper methods (`setScore`) to manage the `scoreMap` state, ensuring the `Source` field is automatically updated when a score is written.
4.  **Lifecycle**: Adopt B's precise logic for `SearchPoolLoaded` (re-trigger pipeline) and `clearSearch` (using `clear()` or map re-initialization).

### UNIFY

# Implementation Plan: Per-Item Score Tracking

This plan introduces a dedicated score tracking structure, updates the filter pipeline to return per-item scores, and wires these scores into the Bubble Tea update loop.

## 1. New Types (`internal/ui/scores.go`)

Create a new file to define the score structure. We use an Enum for the Source to satisfy the "L/C/R" marker requirement safely.

```go
package ui

// ScoreSource indicates which algorithm provided the primary ranking score.
type ScoreSource uint8

const (
    ScoreSourceNone ScoreSource = iota
    ScoreSourceLexical // "L" (Reserved for Feature 7)
    ScoreSourceCosine  // "C"
    ScoreSourceRerank  // "R"
)

// Marker returns the single-char string for UI display (L/C/R).
func (s ScoreSource) Marker() string {
    switch s {
    case ScoreSourceLexical: return "L"
    case ScoreSourceCosine:  return "C"
    case ScoreSourceRerank:  return "R"
    default: return ""
    }
}

// ItemScores holds transient score data for a specific item ID.
type ItemScores struct {
    CosineScore float32
    RerankScore float32
    Source      ScoreSource
}

// BestScore returns the score used for current sorting/display.
func (s ItemScores) BestScore() float32 {
    if s.Source == ScoreSourceRerank {
        return s.RerankScore
    }
    return s.CosineScore
}
```

## 2. Refactor Filter Logic (`internal/filter/filter.go`)

Modify `RerankByQuery` to return the map of scores. This ensures the calling code (App) always has access to the raw cosine values.

```go
// RerankByQuery sorts items by cosine similarity and returns the specific scores.
func RerankByQuery(items []store.Item, embeddings map[string][]float32, queryEmbedding []float32) ([]store.Item, map[string]float32) {
    if len(queryEmbedding) == 0 {
        return items, nil
    }

    // Scored wrapper to facilitate sorting
    type scoredItem struct {
        Item  store.Item
        Score float32
    }

    scored := make([]scoredItem, 0, len(items))
    scoreMap := make(map[string]float32)

    for _, item := range items {
        s := float32(0.0)
        if emb, ok := embeddings[item.ID]; ok {
            // Assuming existing cosineSimilarity function exists in package
            s = cosineSimilarity(emb, queryEmbedding)
        }
        // Track score for return
        scoreMap[item.ID] = s
        scored = append(scored, scoredItem{Item: item, Score: s})
    }

    // Sort descending by score
    sort.SliceStable(scored, func(i, j int) bool {
        return scored[i].Score > scored[j].Score
    })

    // Unpack
    sortedItems := make([]store.Item, len(scored))
    for i, s := range scored {
        sortedItems[i] = s.Item
    }

    return sortedItems, scoreMap
}
```

## 3. App Struct and Helpers (`internal/ui/app.go`)

Add the map to the App struct and creating helper methods to safely mutate state.

```go
type App struct {
    items      []store.Item
    embeddings map[string][]float32

    // Score tracking: Map item ID to its scores
    scoreMap map[string]ItemScores

    // Existing rerank fields...
    rerankEntries []store.Item
    rerankScores  []float32
    // ...
}

// Helper to set cosine score (Initial stage)
func (a *App) setCosineScore(id string, score float32) {
    if a.scoreMap == nil {
        a.scoreMap = make(map[string]ItemScores)
    }
    s := a.scoreMap[id]
    s.CosineScore = score
    // Only set source to C if it isn't already R (rare case, but safe)
    if s.Source == ScoreSourceNone {
        s.Source = ScoreSourceCosine
    }
    a.scoreMap[id] = s
}

// Helper to set rerank score (Final stage)
func (a *App) setRerankScore(id string, score float32) {
    if a.scoreMap == nil {
        a.scoreMap = make(map[string]ItemScores)
    }
    s := a.scoreMap[id]
    s.RerankScore = score
    s.Source = ScoreSourceRerank
    a.scoreMap[id] = s
}
```

## 4. Pipeline Integration

### Stage A: Cosine Similarity
Update `rerankItemsByEmbedding` to capture the map returned by the filter.

```go
func (a *App) rerankItemsByEmbedding() {
    // ... validation checks ...

    // 1. Run filter
    sortedItems, cosineScores := filter.RerankByQuery(a.items, a.embeddings, a.queryEmbedding)
    a.items = sortedItems

    // 2. Clear old scores and populate new cosine scores
    a.scoreMap = make(map[string]ItemScores, len(a.items))
    for id, score := range cosineScores {
        a.setCosineScore(id, score)
    }
    
    // ... trigger rerank step ...
}
```

### Stage B: Rerank Completion
Update the handlers where reranking finishes. This applies to both batch and per-entry logic.

**In `handleEntryReranked` (Per-entry flow):**
```go
// When a single item is reranked
if msg.Err == nil {
    // ... existing logic stores to a.rerankScores ...
    
    // Update the official score map
    itemID := a.rerankEntries[msg.Index].ID
    a.setRerankScore(itemID, msg.Score)
}
```

**In Batch Completion (if applicable):**
Iterate over the results and call `a.setRerankScore(id, score)` for every item returned.

**In `applyScoresAsOrder`:**
Ensure that even if you use the transient `rerankScores` slice for sorting, the data is synced to `scoreMap` before that slice is cleared.

## 5. Lifecycle and Cleanup

### Clear Search
When the user exits search mode, clear the map to free memory and prevent stale scores from appearing in other contexts.

```go
func (a *App) clearSearch() (tea.Model, tea.Cmd) {
    // ... existing cleanup ...
    a.scoreMap = make(map[string]ItemScores) // Reset map
    // ...
}
```

### SearchPoolLoaded
When the underlying item pool changes (e.g., RSS fetch completes), existing scores are invalid.
- If search is **not** active: Do nothing (map remains empty).
- If search **is** active: Re-run the pipeline to score the new items against the existing query.

```go
// In Update() handling SearchPoolLoaded
case SearchPoolLoaded:
    a.items = msg.Items // New items
    
    // If we have an active search query, we must re-score the new items immediately
    if a.inSearchMode && len(a.queryEmbedding) > 0 {
        // This will call filter.RerankByQuery -> repopulate scoreMap
        a.rerankItemsByEmbedding() 
        // Then trigger reranking for top N
        return a, a.triggerRerank()
    } else {
        // Ensure no stale scores linger
        a.scoreMap = make(map[string]ItemScores)
    }
```

### 6b. Score Column Rendering

The following is a unified implementation plan that combines the architectural strengths of Response A with the precise formatting and degradation logic of Response B.

### ANALYZE

**Response A (Claude)**
*   **Strengths:** Introduces a `RenderOptions` struct pattern. This is the superior architectural choice for Go, as it prevents the function signature of `renderItemLine` from becoming "unwieldy" (a specific design constraint). It also separates the score rendering logic into a cleaner helper function (`renderScoreColumn`).
*   **Weaknesses:** The width threshold logic (checking `< 60`) contradicts the specific requirement to degrade gracefully for terminals `< 80`. The padding logic is slightly manual compared to `fmt` width specifiers.

**Response B (Grok)**
*   **Strengths:** adhered strictly to the "width < 80" constraint. Used `fmt.Sprintf("%6.2f")` for cleaner alignment logic. correctly identified that items without scores must render whitespace to maintain column alignment when the feature is active.
*   **Weaknesses:** Modified the function signature by adding multiple arguments (`score`, `showScores`), which makes the signature messy and harder to maintain if more features are added later. The logic is placed inline, making `renderItemLine` longer and harder to read.

**Comparison**
Both models correctly identified the color codes and the need for logic to handle "no score" items. Response A wins on architecture (cleaner code structure), while Response B wins on strict adherence to visual constraints (width thresholds and formatting).

### SYNERGIZE

The unified solution will leverage Response A's **Options Pattern** to keep the API clean. We will pass a single `RenderOptions` struct into the rendering functions.

From Response B, we will adopt the **Formatting and Degradation Logic**. We will enforce the `< 80` character check for hiding the column entirely. We will also ensure that when the column is visible, items with missing scores render a fixed-width empty string to preserve vertical alignment across the list.

**Unique Synergies:**
1.  **Helper Function:** We will use A's `renderScoreColumn` helper but populate it with B's `fmt` formatting logic.
2.  **Styling:** We will use A's inclusion of `Bold` for high scores (adds visual pop) while keeping B's strict color definitions.
3.  **Layout Math:** We will standardize the column width to **6 characters** (Space + 4 chars + Space) to ensure perfect spacing: `[Source] 0.82 Title`.

### UNIFY

# Implementation Plan: Score Column Rendering

This plan implements the score column using an options struct pattern to maintain clean function signatures, while ensuring strict visual alignment and graceful degradation for narrow terminals.

## 1. Styles (`internal/ui/styles.go`)

We add the score-specific styles. Note the use of `Bold` for the high score to make top results pop.

```go
// Score column color bands
var (
    ScoreHigh = lipgloss.NewStyle().
        Foreground(lipgloss.Color("82")). // Bright green
        Bold(true)

    ScoreMedium = lipgloss.NewStyle().
        Foreground(lipgloss.Color("226")) // Yellow

    ScoreLow = lipgloss.NewStyle().
        Foreground(lipgloss.Color("240")) // Dim gray
)
```

## 2. Rendering Logic (`internal/ui/stream.go`)

We introduce a `RenderOptions` struct. This satisfies the constraint of passing data without making signatures unwieldy.

### Struct Definition

```go
// RenderOptions encapsulates view-specific toggles and data to keep function signatures clean.
type RenderOptions struct {
    ShowScores bool
    ScoreMap   map[string]ItemScores // Keyed by Item.ID (or Source+Title hash)
}
```

### Helper Function: `renderScoreColumn`

This helper isolates the logic for formatting, coloring, and handling "no score" scenarios.

```go
// renderScoreColumn returns the rendered string and its visual width.
// It enforces a fixed width to ensure vertical alignment across the list.
func renderScoreColumn(itemID string, opts RenderOptions, termWidth int) (string, int) {
    // 1. Graceful degradation: Hide completely if terminal is too narrow or toggle is off
    if !opts.ShowScores || termWidth < 80 {
        return "", 0
    }

    // Fixed width: 1 leading space + 4 chars ("0.00") + 1 trailing space = 6 chars
    const colWidth = 6
    
    // 2. Handle missing data: If column is on, but this item has no score, return blank space
    scores, exists := opts.ScoreMap[itemID]
    if !exists || opts.ScoreMap == nil {
        return strings.Repeat(" ", colWidth), colWidth
    }

    // 3. Render Score
    val := scores.BestScore()
    
    var style lipgloss.Style
    switch {
    case val >= 0.80:
        style = ScoreHigh
    case val >= 0.60:
        style = ScoreMedium
    default:
        style = ScoreLow
    }

    // Format: " 0.82 " (Space is handled by fmt alignment or manual string concat)
    // We strictly format the number to 4 chars (e.g. "0.82")
    text := fmt.Sprintf("%.2f", val)
    styledText := style.Render(text)

    // Construct final string: " " + styled + " "
    return fmt.Sprintf(" %s ", styledText), colWidth
}
```

### Modified `renderItemLine`

We update the signature to accept `RenderOptions` and calculate the dynamic title width based on whether the score column is present.

```go
func renderItemLine(item store.Item, selected bool, width int, opts RenderOptions) string {
    badge := SourceBadge.Render(item.SourceName)
    badgeWidth := lipgloss.Width(badge)

    // Render score column (or empty string if disabled/narrow)
    scoreCol, scoreWidth := renderScoreColumn(item.ID, opts, width)

    // Calculate title width: Total - Badge - Score - Padding
    // Padding: 1 space after badge (if no score) or handled by scoreCol logic.
    // Logic: If scoreCol exists, it includes its own padding. If not, we need 1 space after badge.
    effectivePadding := 1
    if scoreWidth > 0 {
        effectivePadding = 0 // scoreCol includes the necessary spacing
    }
    
    titleWidth := width - badgeWidth - scoreWidth - effectivePadding - 1 // -1 for safety/margin
    if titleWidth < 20 {
        titleWidth = 20
    }

    title := item.Title
    if utf8.RuneCountInString(title) > titleWidth {
        runes := []rune(title)
        title = string(runes[:titleWidth-3]) + "..."
    }

    var titleStyle lipgloss.Style
    switch {
    case selected:
        titleStyle = SelectedItem
    case item.Read:
        titleStyle = ReadItem
    default:
        titleStyle = NormalItem
    }
    styledTitle := titleStyle.Render(title)

    // Assembly
    if scoreWidth > 0 {
        // scoreCol already contains surrounding spaces: " 0.82 "
        return fmt.Sprintf("%s%s%s", badge, scoreCol, styledTitle)
    }
    
    // Standard view
    return fmt.Sprintf("%s %s", badge, styledTitle)
}
```

### Modified `RenderStream`

We inject the options into the loop.

```go
func RenderStream(items []store.Item, cursor int, width, height int, showBands bool, opts RenderOptions) string {
    // ... (Existing buffer/band logic) ...

    for i, item := range items {
        // ... (Loop logic) ...
        
        // Pass options down
        line := renderItemLine(item, i == cursor, width, opts)
        
        // ... (Appending logic) ...
    }
    return b.String()
}
```

## 3. Integration Plan

This is how the modifications connect to the main `App` struct (likely in `internal/ui/view.go` or `main.go`).

1.  **State Management:** Ensure `App` has access to `scoreMap` and `showScores`.
2.  **View Call:**
    ```go
    func (a App) View() string {
        // ...
        
        // Determine if scores should be shown (Search Mode + Toggle + Data exists)
        renderOpts := RenderOptions{
            ShowScores: a.showScores && a.inSearchMode(), 
            ScoreMap:   a.scoreMap,
        }

        content := RenderStream(
            a.items, 
            a.cursor, 
            a.width, 
            a.height, 
            a.showBands, 
            renderOpts, // Pass the new struct
        )
        
        // ...
    }
    ```

## 4. Verification & Constraints Check

*   **Format:** `0.82` (4 chars) is achieved via `%.2f`.
*   **Color:** High/Med/Low bands applied correctly.
*   **Alignment:** `renderScoreColumn` returns a fixed width of 6 regardless of score value or existence (as long as `ShowScores` is true), ensuring the Titles all start at the exact same vertical column.
*   **Narrow Terminals:** Logic explicitly checks `width < 80` to short-circuit the column.
*   **Clean Signature:** We added one struct parameter rather than 2-3 primitives.

### 6c. Toggle UX

# Claude's Analysis: Subtask 6c -- Toggle UX (`x` key)

## Implementation Plan

### 1. App Struct Addition

```go
type App struct {
    // ... existing fields ...

    // Score column toggle (persists across searches within session)
    showScores bool // toggled by 'x' key; only effective when hasQuery()
}
```

Note: `showScores` is NOT cleared in `clearSearch()` because it is a user preference, not search state. This means if the user enables scores in one search, they stay on for the next search.

### 2. Key Handling Changes

**In `rerankPending` block -- add `x` alongside existing `D`:**
```go
if a.rerankPending {
    switch msg.Type {
    case tea.KeyEsc:
        return a.clearSearch()
    case tea.KeyCtrlC:
        return a, tea.Quit
    case tea.KeyUp:
        return a.handleUp()
    case tea.KeyDown:
        return a.handleDown()
    case tea.KeyHome:
        return a.handleHome()
    case tea.KeyEnd:
        return a.handleEnd()
    }
    switch msg.String() {
    case "j":
        return a.handleDown()
    case "k":
        return a.handleUp()
    case "g":
        return a.handleHome()
    case "G":
        return a.handleEnd()
    case "D":
        a.debugVisible = !a.debugVisible
        return a, nil
    case "x":                           // NEW
        a.showScores = !a.showScores
        return a, nil
    }
    return a, nil
}
```

**In normal mode block -- add `x` with guard:**
```go
// String-based keys (normal mode)
switch msg.String() {
case "/":
    return a.enterSearchMode()
// ... existing cases ...
case "D":
    a.debugVisible = !a.debugVisible
    return a, nil
case "x":                              // NEW
    if a.hasQuery() {
        a.showScores = !a.showScores
    }
    return a, nil
}
```

**NOT added to:**
- `searchActive` block (user is typing, 'x' goes to text input)
- `embeddingPending` block (minimal keys only, consistent with existing pattern)

### 3. Status Bar Hint Updates

**RenderStatusBarWithFilter (search results mode):**
```go
func RenderStatusBarWithFilter(cursor, filtered, total int, width int, loading bool) string {
    keys := []string{
        StatusBarKey.Render("j/k") + StatusBarText.Render(":nav"),
        StatusBarKey.Render("Enter") + StatusBarText.Render(":read"),
        StatusBarKey.Render("x") + StatusBarText.Render(":scores"),   // NEW
        StatusBarKey.Render("Esc") + StatusBarText.Render(":clear"),
    }
    // ... rest unchanged ...
}
```

### 4. clearSearch Interaction

`showScores` is NOT cleared in `clearSearch()`. The toggle is a session-level user preference:

```go
func (a App) clearSearch() (tea.Model, tea.Cmd) {
    // ... existing cleanup ...
    a.scoreMap = nil  // scores are cleared (search data)
    // a.showScores is NOT cleared (user preference)
    // ... rest ...
}
```

This means:
- User searches, presses `x` to show scores
- Presses Esc to clear search
- Searches again -- scores are still toggled on
- Score column appears as soon as cosine scores are computed

### 5. View() Integration

In `App.View()`, the `showScores` flag gates score column rendering:

```go
// In View():
renderOpts := RenderOptions{
    ScoreMap:   a.scoreMap,
    ShowScores: a.showScores && a.hasQuery(),
}
```

The `a.hasQuery()` guard ensures score column never appears in chronological mode, even if `showScores` is true.

### 6. Edge Cases

**x pressed with no scores computed yet:**
- `showScores` becomes true, but `scoreMap` is nil or empty
- `renderScoreColumn` returns blank spaces (alignment preserved)
- Once scores arrive (cosine or rerank), column populates automatically
- No special handling needed

**x pressed during reranking:**
- Scores-so-far are visible (cosine scores already in scoreMap)
- As rerank scores arrive, they update in real-time
- Works naturally because View() reads scoreMap on every render

**x pressed after Esc (no search):**
- `hasQuery()` returns false
- `showScores` still toggles (no-op visually)
- Next search will show scores immediately

### Test Strategy
- Test 'x' key toggles showScores in normal mode with hasQuery
- Test 'x' key is no-op in normal mode without hasQuery
- Test 'x' key works during rerankPending
- Test 'x' key does NOT work during embeddingPending
- Test showScores persists across clearSearch()
- Test View renders score column when showScores && hasQuery

---

## Feature 7: FTS5 Instant Lexical Results

### 7a. FTS5 Schema

Here is the unified implementation plan, combining the structural robustness of Response B with the specific configuration details of Response A.

### ANALYZE

**Response A (Claude)** correctly identified the core strategy of using SQLite's implicit `rowid` for the external content linkage. It provided a simple migration strategy and good tokenizer settings (`porter unicode61`). However, its triggers were slightly inefficient (firing on *any* update, not just relevant columns) and lacked null-safety. It also attempted to execute multiple SQL statements in a single `Exec` call, which can sometimes be problematic depending on driver configuration.

**Response B (gpt5)** provided superior SQL implementation. It recognized that the `summary` column is nullable and correctly applied `COALESCE` to prevent FTS errors. It optimized the UPDATE trigger to only fire when `title` or `summary` changes (vital for a news reader where `read`/`saved` flags change frequently). It also provided the essential logic for actually *querying* the data (the JOIN pattern), which was missing from A.

**Key Differences:**
*   **Triggers:** Response B used `COALESCE` (safety) and `AFTER UPDATE OF...` (performance). Response A was generic.
*   **Migration:** Response B correctly identified that `rebuild` should only happen on *initial* creation, whereas Response A's logic was slightly ambiguous on when the rebuild triggers.
*   **Context:** Response B included `context.Context` usage, which is better for production Go apps.

### SYNERGIZE

The unified plan will leverage **Response B's SQL logic** (optimized triggers, null handling) and **Response A's specific tokenizer configuration** (`porter unicode61`).

I will combine the migration strategies: we will use a transaction-based approach (from B) to ensure the table creation and trigger creation happen atomically. We will also incorporate the explicit "check if table exists" step to determine if we need to run an expensive `rebuild` command (only necessary on the first run).

I will also include the retrieval query pattern from Response B, as implementing FTS is useless without knowing how to join it back to the source table.

### UNIFY

# Implementation Plan: FTS5 for Observer

## 1. Core Strategy
Since `items` uses `id TEXT PRIMARY KEY`, we cannot use `id` as the FTS rowid. However, because the table is **not** defined as `WITHOUT ROWID`, SQLite maintains an internal, stable integer `rowid`.

We will use **External Content FTS** to avoid data duplication:
*   **Source:** `content='items'`
*   **Linkage:** `content_rowid='rowid'`
*   **Tokenizer:** `tokenize='porter unicode61'` (Stemming + Unicode support)

## 2. SQL Schema & Triggers

We need three triggers to keep the index in sync.
*   **Crucial Optimization:** The UPDATE trigger only fires when `title` or `summary` changes. This prevents unnecessary FTS re-indexing when you update flags like `read` or `saved`.
*   **Null Safety:** The `summary` column is nullable. We use `COALESCE(new.summary, '')` to ensure FTS5 doesn't choke on NULL values.

```sql
-- 1. The Virtual Table
CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
    title,
    summary,
    content='items',
    content_rowid='rowid',
    tokenize='porter unicode61'
);

-- 2. Insert Trigger
CREATE TRIGGER IF NOT EXISTS items_fts_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, summary)
    VALUES (new.rowid, new.title, COALESCE(new.summary, ''));
END;

-- 3. Delete Trigger
CREATE TRIGGER IF NOT EXISTS items_fts_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, summary)
    VALUES ('delete', old.rowid, old.title, COALESCE(old.summary, ''));
END;

-- 4. Update Trigger (Optimized)
CREATE TRIGGER IF NOT EXISTS items_fts_au AFTER UPDATE OF title, summary ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, summary)
    VALUES ('delete', old.rowid, old.title, COALESCE(old.summary, ''));
    
    INSERT INTO items_fts(rowid, title, summary)
    VALUES (new.rowid, new.title, COALESCE(new.summary, ''));
END;
```

## 3. Go Implementation (`internal/store/store.go`)

Add the migration function. This function is **idempotent**. It detects if the FTS table is being created for the first time; if so, it issues a `rebuild` command to backfill existing data.

```go
package store

import (
    "context"
    "fmt"
    // ... imports
)

// migrateFTS creates the virtual table and triggers.
// It backfills data only if the table is newly created.
func (s *Store) migrateFTS() error {
    ctx := context.Background() // Or pass context if available in Open()
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin FTS migration: %w", err)
    }
    defer tx.Rollback()

    // 1. Check if FTS table already exists
    var count int
    err = tx.QueryRowContext(ctx, 
        "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='items_fts'",
    ).Scan(&count)
    if err != nil {
        return fmt.Errorf("check FTS existence: %w", err)
    }
    
    tableExists := count > 0

    // 2. Define SQL Statements
    queries := []string{
        `CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
            title,
            summary,
            content='items',
            content_rowid='rowid',
            tokenize='porter unicode61'
        );`,
        `CREATE TRIGGER IF NOT EXISTS items_fts_ai AFTER INSERT ON items BEGIN
            INSERT INTO items_fts(rowid, title, summary)
            VALUES (new.rowid, new.title, COALESCE(new.summary, ''));
        END;`,
        `CREATE TRIGGER IF NOT EXISTS items_fts_ad AFTER DELETE ON items BEGIN
            INSERT INTO items_fts(items_fts, rowid, title, summary)
            VALUES ('delete', old.rowid, old.title, COALESCE(old.summary, ''));
        END;`,
        `CREATE TRIGGER IF NOT EXISTS items_fts_au AFTER UPDATE OF title, summary ON items BEGIN
            INSERT INTO items_fts(items_fts, rowid, title, summary)
            VALUES ('delete', old.rowid, old.title, COALESCE(old.summary, ''));
            INSERT INTO items_fts(rowid, title, summary)
            VALUES (new.rowid, new.title, COALESCE(new.summary, ''));
        END;`,
    }

    // 3. Execute Schema Creation
    for _, query := range queries {
        if _, err := tx.ExecContext(ctx, query); err != nil {
            return fmt.Errorf("exec fts schema: %w", err)
        }
    }

    // 4. Backfill/Rebuild (Only if table didn't exist previously)
    if !tableExists {
        // This pulls all current data from 'items' into the index
        if _, err := tx.ExecContext(ctx, "INSERT INTO items_fts(items_fts) VALUES('rebuild')"); err != nil {
            return fmt.Errorf("rebuild fts: %w", err)
        }
    }

    return tx.Commit()
}

// RebuildFTS forces a complete re-indexing.
// Useful for maintenance or if triggers were temporarily disabled.
func (s *Store) RebuildFTS(ctx context.Context) error {
    _, err := s.db.ExecContext(ctx, "INSERT INTO items_fts(items_fts) VALUES('rebuild')")
    return err
}
```

## 4. Lifecycle Integration

Call `migrateFTS` inside `Open`, after the main tables are created but before the store is returned.

```go
func Open(dbPath string) (*Store, error) {
    // ... open DB, enable WAL ...
    s := &Store{db: db}
    
    if err := s.createTables(); err != nil { 
        return nil, err 
    }
    
    // NEW: Initialize FTS
    if err := s.migrateFTS(); err != nil {
        return nil, fmt.Errorf("migrating FTS: %w", err)
    }

    if err := s.migrateEmbeddings(); err != nil { 
        return nil, err 
    }
    
    return s, nil
}
```

## 5. Usage: Performing the Search

To search, you must JOIN the virtual table back to the physical table using the `rowid`.

```go
func (s *Store) SearchLexical(ctx context.Context, query string) ([]Item, error) {
    // Note: In production, sanitize 'query' to prevent FTS syntax errors 
    // (e.g. unmatched quotes)
    sql := `
        SELECT i.id, i.title, i.summary, i.url, i.published_at
        FROM items i
        JOIN items_fts f ON i.rowid = f.rowid
        WHERE items_fts MATCH ? 
        ORDER BY bm25(items_fts) -- Relevance ranking
        LIMIT 50;
    `
    
    rows, err := s.db.QueryContext(ctx, sql, query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var items []Item
    for rows.Next() {
        var i Item
        // scan into struct...
        items = append(items, i)
    }
    return items, nil
}
```

## 6. Gotchas & Notes

1.  **RowID Stability:** This implementation relies on SQLite's internal integer `rowid`. Do not change the `items` table to `WITHOUT ROWID` in the future, or this will break.
2.  **Transactions:** `modernc.org/sqlite` is robust, but always use transactions when running multiple schema modification statements to ensure the database doesn't end up in a half-migrated state.
3.  **Sanitization:** FTS5's query syntax (NEAR, OR, AND, etc.) is powerful but fragile. If a user types a raw `"` character, the query might fail. You may need to escape user input or wrap it in double quotes for literal phrase matching.
4.  **Driver Support:** `modernc.org/sqlite` includes `FTS5` by default. If you see "no such module: fts5", ensure you aren't using a custom build tag that excludes it.

### 7b. Store Methods

# Claude's Analysis: Subtask 7b -- Store Methods for FTS5 Search

## Implementation Plan

### 1. Query Sanitization

FTS5 MATCH syntax has special operators. User input like `hello "world` would crash.

Strategy: **Double-quote each whitespace-separated token**, stripping existing quotes. This makes every token a literal phrase match, which is safe and intuitive.

```go
// sanitizeFTSQuery converts user input into a safe FTS5 query.
// Each word is quoted as a phrase, joined with implicit AND.
// Example: `ukraine "missile defense"` -> `"ukraine" "missile" "defense"`
func sanitizeFTSQuery(input string) string {
    // Remove any existing double quotes to prevent injection
    cleaned := strings.ReplaceAll(input, `"`, "")

    words := strings.Fields(cleaned)
    if len(words) == 0 {
        return ""
    }

    quoted := make([]string, len(words))
    for i, w := range words {
        quoted[i] = `"` + w + `"`
    }
    return strings.Join(quoted, " ")
}
```

This approach:
- Prevents FTS5 syntax errors from user input
- Provides AND semantics (all words must appear)
- Porter stemming still applies inside quoted phrases
- Simple and predictable behavior

### 2. SearchFTS Method

```go
// FTSResult holds an item with its FTS5 relevance score.
type FTSResult struct {
    Item  Item
    Score float64 // BM25 relevance score (lower = more relevant in SQLite's bm25)
}

// SearchFTS performs full-text search using FTS5.
// Returns items matching the query, ordered by relevance (bm25).
// The query is sanitized to prevent FTS5 syntax errors.
// Returns up to limit results.
// Thread-safe: acquires read lock.
func (s *Store) SearchFTS(query string, limit int) ([]Item, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    ftsQuery := sanitizeFTSQuery(query)
    if ftsQuery == "" {
        return nil, nil
    }

    // Join FTS results with items table via rowid
    // bm25() returns negative values (more negative = more relevant)
    // ORDER BY rank uses FTS5's built-in ranking
    sqlQuery := `
        SELECT i.id, i.source_type, i.source_name, i.title, i.summary,
               i.url, i.author, i.published_at, i.fetched_at, i.read, i.saved
        FROM items_fts fts
        JOIN items i ON i.rowid = fts.rowid
        WHERE items_fts MATCH ?
        ORDER BY rank
        LIMIT ?
    `

    return s.queryItems(sqlQuery, ftsQuery, limit)
}
```

### 3. SearchFTSWithScores Method

For the L/C/R scoring system, we need BM25 scores:

```go
// SearchFTSWithScores performs FTS5 search and returns items with relevance scores.
// Scores are normalized to [0, 1] range for consistency with cosine/rerank scores.
// Thread-safe: acquires read lock.
func (s *Store) SearchFTSWithScores(query string, limit int) ([]FTSResult, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    ftsQuery := sanitizeFTSQuery(query)
    if ftsQuery == "" {
        return nil, nil
    }

    sqlQuery := `
        SELECT i.id, i.source_type, i.source_name, i.title, i.summary,
               i.url, i.author, i.published_at, i.fetched_at, i.read, i.saved,
               bm25(items_fts) as score
        FROM items_fts fts
        JOIN items i ON i.rowid = fts.rowid
        WHERE items_fts MATCH ?
        ORDER BY rank
        LIMIT ?
    `

    rows, err := s.db.Query(sqlQuery, ftsQuery, limit)
    if err != nil {
        return nil, fmt.Errorf("FTS search: %w", err)
    }
    defer rows.Close()

    var results []FTSResult
    for rows.Next() {
        var r FTSResult
        var readInt, savedInt int
        var score float64
        err := rows.Scan(
            &r.Item.ID, &r.Item.SourceType, &r.Item.SourceName,
            &r.Item.Title, &r.Item.Summary, &r.Item.URL, &r.Item.Author,
            &r.Item.Published, &r.Item.Fetched, &readInt, &savedInt,
            &score,
        )
        if err != nil {
            return nil, err
        }
        r.Item.Read = readInt != 0
        r.Item.Saved = savedInt != 0
        r.Score = score
        results = append(results, r)
    }

    return results, rows.Err()
}
```

### 4. CountFTSResults

```go
// CountFTSResults returns the number of items matching an FTS5 query.
// Thread-safe: acquires read lock.
func (s *Store) CountFTSResults(query string) (int, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    ftsQuery := sanitizeFTSQuery(query)
    if ftsQuery == "" {
        return 0, nil
    }

    var count int
    err := s.db.QueryRow(`
        SELECT COUNT(*) FROM items_fts WHERE items_fts MATCH ?
    `, ftsQuery).Scan(&count)
    if err != nil {
        return 0, fmt.Errorf("count FTS results: %w", err)
    }
    return count, nil
}
```

### 5. FTS Health/Stats

```go
// FTSAvailable returns true if the FTS5 table exists and is queryable.
// Thread-safe: acquires read lock.
func (s *Store) FTSAvailable() bool {
    s.mu.RLock()
    defer s.mu.RUnlock()

    var count int
    err := s.db.QueryRow(`
        SELECT COUNT(*) FROM sqlite_master
        WHERE type = 'table' AND name = 'items_fts'
    `).Scan(&count)
    return err == nil && count > 0
}

// FTSRowCount returns the number of rows in the FTS5 index.
// Thread-safe: acquires read lock.
func (s *Store) FTSRowCount() (int, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    var count int
    err := s.db.QueryRow("SELECT COUNT(*) FROM items_fts").Scan(&count)
    if err != nil {
        return 0, fmt.Errorf("FTS row count: %w", err)
    }
    return count, nil
}
```

### 6. Error Handling

All FTS methods should gracefully handle the case where FTS5 is unavailable:

```go
// In SearchFTS, wrap the error check:
if err != nil {
    // Check if this is an FTS unavailable error
    if strings.Contains(err.Error(), "no such table") {
        return nil, nil // FTS not available, return empty
    }
    return nil, fmt.Errorf("FTS search: %w", err)
}
```

### 7. Thread Safety

All methods use the existing `s.mu.RLock()` pattern since FTS queries are reads. The FTS index is updated via triggers (which fire within the write-locked `SaveItems` call), so no additional locking is needed.

### Test Strategy
- Test sanitizeFTSQuery with various inputs (normal, special chars, empty, quotes)
- Test SearchFTS returns matching items
- Test SearchFTS with no matches returns empty
- Test SearchFTSWithScores returns valid scores
- Test CountFTSResults matches SearchFTS length
- Test FTSAvailable returns true after migration
- Test FTS results respect porter stemming (searching "running" matches "run")
- Integration test: SaveItems then SearchFTS finds new items

### 7c. Search Flow Integration

Here is the unified implementation plan.

### ANALYZE

**Response A (Claude)** focuses on clean dependency injection and adhering to the Bubble Tea command pattern.
*   **Strengths:** It provides excellent boilerplate for wiring the `SearchFTS` function through `AppConfig`. It correctly identifies the need to check for stale `QueryID`.
*   **Weaknesses:** Its merging strategy is destructive. It suggests that semantic results should *replace* FTS results entirely. This ignores the robust pattern of "Hybrid Search" where lexical matches that appear in FTS but *not* in vector search should often be preserved (deduplicated) at the bottom of the list.

**Response B (GPT-5)** introduces a state-based "Phased" approach.
*   **Strengths:** It proposes keeping separate state slices for `ftsItems`, `cosineItems`, and `rerankItems`. This is superior because it allows for a central `rebuildSearchResults()` function that can be called whenever *any* stage completes, handling out-of-order execution gracefully. It also suggests a centralized `updateSearchStatus()` helper, which prevents logic duplication across multiple message handlers.
*   **Weaknesses:** It is slightly less detailed on the configuration injection (AppConfig) aspect compared to Response A.

**Comparison:**
*   **Merging:** Response B's logic (Priority: R > C > L, plus deduped append) is much stronger than Response A's simple replacement.
*   **Architecture:** Response A's specific struct updates are cleaner, while Response B's internal logic is better.

### SYNERGIZE

The unified solution will combine Response A's rigorous struct/config setup with Response B's robust state management and merging logic.

1.  **State Management:** We will adopt Response B's strategy of storing `ftsItems`, `cosineItems`, and `rerankItems` separately in the `App` model. This allows non-destructive updates.
2.  **Centralized Rebuilding:** We will implement the `rebuildSearchResults()` function (from Response B) to handle the complex merging and deduplication logic (Requirement 2).
3.  **Status Logic:** We will implement the `updateStatus()` helper (from Response B) to handle the required status text progression (Requirement 3).
4.  **Wiring:** We will use Response A's `AppConfig` and `main.go` wiring patterns to ensure the `SearchFTS` function is properly injected.

### UNIFY

# Implementation Plan: FTS5 & Hybrid Search Integration

## 1. Data Structures & Messages

First, we define the message type for the FTS result and update the `App` struct to maintain state for all three search phases (Lexical, Cosine, Rerank). This prevents race conditions and allows for "Hybrid" merging.

### `internal/ui/messages.go`
```go
// FTSResultsLoaded is sent when the fast lexical search completes.
type FTSResultsLoaded struct {
    Items   []store.Item
    QueryID string
    Err     error
}
```

### `internal/ui/app.go`
Update the `App` struct to hold phase-specific data.

```go
type App struct {
    // ... existing fields ...

    // Dependencies
    searchFTS func(query string, limit int, queryID string) tea.Cmd

    // Search State
    // We keep results from different phases separate to allow rebuilding/merging
    ftsItems    []store.Item
    cosineItems []store.Item
    rerankItems []store.Item
    
    // Flags to track active phases
    ftsPending        bool
    searchPoolPending bool // existing
    embeddingPending  bool // existing
    // rerankPending  bool // (add if not already tracking cross-encoder state)
}
```

## 2. Configuration & Wiring

Inject the FTS capability via `AppConfig`.

### `internal/ui/config.go`
```go
type AppConfig struct {
    // ... existing ...
    SearchFTS func(query string, limit int, queryID string) tea.Cmd
}
```

### `cmd/observer/main.go`
Wire the store method to the UI command.

```go
// In main setup:
cfg := ui.AppConfig{
    // ... other config ...
    SearchFTS: func(query string, limit int, queryID string) tea.Cmd {
        return func() tea.Msg {
            // Call the synchronous store method
            items, err := st.SearchFTS(query, limit)
            return ui.FTSResultsLoaded{
                Items:   items,
                QueryID: queryID,
                Err:     err,
            }
        }
    },
}
```

## 3. Centralized Merging & Status Logic

This is the core logic. Instead of blindly replacing `a.items`, we rebuild the list based on what data is currently available.

### `internal/ui/helpers.go` (or `app.go`)

```go
// rebuildSearchResults creates the final display list by merging phases:
// Priority: Rerank > Cosine > FTS.
// Strategy: Take the best available list, then append non-duplicate items from lower tiers.
func (a *App) rebuildSearchResults() {
    var primary []store.Item
    var primarySource ScoreSource // Enum: Lexical, Cosine, Rerank

    // 1. Determine the "best" available list
    switch {
    case len(a.rerankItems) > 0:
        primary = a.rerankItems
        primarySource = ScoreSourceRerank // "R"
    case len(a.cosineItems) > 0:
        primary = a.cosineItems
        primarySource = ScoreSourceCosine // "C"
    default:
        primary = a.ftsItems
        primarySource = ScoreSourceLexical // "L"
    }

    // 2. Deduplicate and Build
    seen := make(map[string]struct{}, len(primary))
    finalItems := make([]store.Item, 0, len(primary)+len(a.ftsItems))
    newScoreMap := make(map[string]ItemScores)

    // Helper to add items
    add := func(items []store.Item, source ScoreSource) {
        for _, item := range items {
            if _, exists := seen[item.ID]; exists {
                continue
            }
            seen[item.ID] = struct{}{}
            finalItems = append(finalItems, item)
            
            // Preserve existing score if available, update Source for UI marker
            currentScore := a.scoreMap[item.ID]
            currentScore.Source = source
            newScoreMap[item.ID] = currentScore
        }
    }

    // Add primary items (e.g., Semantic results)
    add(primary, primarySource)

    // 3. Hybrid Fallback: Append FTS items not found in semantic results
    // This ensures specific keyword matches appear even if the embedding model missed them.
    if primarySource != ScoreSourceLexical {
        add(a.ftsItems, ScoreSourceLexical)
    }

    a.items = finalItems
    a.scoreMap = newScoreMap
}

// updateSearchStatus manages the text progression: Refining -> Reranking -> Done
func (a *App) updateSearchStatus() {
    query := truncateRunes(a.filterInput.Value(), 30)
    count := len(a.items)

    switch {
    // Case: Reranking is completely done
    case len(a.rerankItems) > 0:
         a.statusText = fmt.Sprintf("%q — %d results", query, count)

    // Case: Cosine ready, but waiting for Rerank (if rerank is active)
    case len(a.cosineItems) > 0:
        a.statusText = fmt.Sprintf("%q — %d results (reranking...)", query, count)

    // Case: FTS ready, waiting for Vector Search
    case len(a.ftsItems) > 0:
        if a.embeddingPending || a.searchPoolPending {
            a.statusText = fmt.Sprintf("%q — %d results (refining...)", query, count)
        } else {
            // Fallback if semantic fails or produces 0 results
            a.statusText = fmt.Sprintf("%q — %d results", query, count)
        }

    // Case: Just started
    default:
        a.statusText = fmt.Sprintf("Searching for %q...", query)
    }
}
```

## 4. Modified `submitSearch`

Initialize the phased state and fire FTS immediately alongside the semantic pipeline.

```go
func (a App) submitSearch() (tea.Model, tea.Cmd) {
    query := a.filterInput.Value()
    if query == "" {
        // ... existing clear logic ...
        return a, nil
    }

    // Reset Search State
    a.searchStart = time.Now()
    a.queryID = newQueryID()
    a.ftsItems = nil
    a.cosineItems = nil
    a.rerankItems = nil
    a.scoreMap = make(map[string]ItemScores)
    
    // ... save chronological view logic ...

    var cmds []tea.Cmd

    // 1. Fire Instant FTS
    if a.searchFTS != nil {
        a.ftsPending = true
        cmds = append(cmds, a.searchFTS(query, 50, a.queryID))
    }

    // 2. Fire Semantic Pipeline (Parallel)
    if a.loadSearchPool != nil {
        a.searchPoolPending = true
        cmds = append(cmds, a.loadSearchPool(a.queryID))
    }
    if a.embedQuery != nil {
        a.embeddingPending = true
        cmds = append(cmds, a.embedQuery(query, a.queryID))
    }

    if len(cmds) > 0 {
        a.updateSearchStatus() // Sets initial "Searching..." text
        cmds = append(cmds, a.spinner.Tick)
        return a, tea.Batch(cmds...)
    }
    return a, nil
}
```

## 5. Update Loop Integration

Handle the new message and update existing handlers to use the central merge/status logic.

### `internal/ui/update.go`

```go
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {

    // ... existing cases ...

    case FTSResultsLoaded:
        // Stale check
        if msg.QueryID != a.queryID {
            return a, nil
        }
        a.ftsPending = false
        
        if msg.Err != nil {
            // Log error but don't stop semantic search
            a.logger.Emit(otel.Event{Kind: "error", Msg: "FTS failed", Err: msg.Err.Error()})
        } else {
            a.ftsItems = msg.Items
            
            // Only rebuild visual list if we don't have better results yet
            // (Prevents FTS from overwriting Semantic if Semantic arrived super fast)
            if len(a.cosineItems) == 0 && len(a.rerankItems) == 0 {
                a.rebuildSearchResults()
                a.cursor = 0 // Reset cursor on first paint
            }
        }
        a.updateSearchStatus()
        return a, nil

    // Modify EXISTING handler for Cosine Results (likely QueryEmbedded or similar)
    case SearchPoolLoaded: // or whatever triggers the cosine rank
        // ... existing logic to calculate cosine scores ...
        
        a.cosineItems = sortedItems // Store in specific phase slice
        a.rebuildSearchResults()    // Merge with FTS (Semantic takes priority)
        a.updateSearchStatus()      // Update text to "reranking..."
        return a, nil

    // Modify EXISTING handler for Rerank Results
    case RerankComplete:
        // ... existing logic ...
        
        a.rerankItems = rerankedItems
        a.rebuildSearchResults()
        a.updateSearchStatus() // Update text to final count
        return a, nil
    }
    
    return a, nil
}
```

## Summary of Logic Flow
1.  **User Hits Enter**: `submitSearch` fires. FTS, Pool, Embed commands start. Status: "Searching...".
2.  **~20ms**: `FTSResultsLoaded` arrives. `ftsItems` populated. `rebuildSearchResults` puts them on screen (Source: L). Status: "12 results (refining...)".
3.  **~200ms**: Embeddings ready. Cosine calc runs. `cosineItems` populated. `rebuildSearchResults` puts Cosine items at top (Source: C), appends unique FTS items at bottom (Source: L). Status: "40 results (reranking...)".
4.  **~1.5s**: Rerank ready. `rerankItems` populated. `rebuildSearchResults` puts Reranked items at top (Source: R), keeps un-reranked Cosine items below (Source: C), FTS-only below that (Source: L). Status: "40 results".

### 7d. L/C/R Visual Markers

The following is a unified implementation plan that combines the structural precision of bitflags (from both models), the visual stability strategies (from Response B), and the specific styling and update logic (from Response A).

### ANALYZE

**Response A (Claude)** correctly identifies the need for bitflags (`1 << iota`) to track the `L`, `C`, and `R` stages simultaneously. It provides specific Lipgloss colors and detailed logic for updating the `ScoreMap` progressively using the `|=` operator. However, its width calculation is dynamic (variable width based on marker length), which causes "layout jitter"—the title column will jump around as items upgrade from `[L]` to `[LC]`.

**Response B (gpt5)** validates the bitflag approach but adds a critical UI insight: the "Score Column" must have a **fixed width** to prevent jitter. It suggests reserving 5 cells for the marker regardless of its content. It also raises a vital point about **Selection Styles**: if the selected row style enforces a specific Foreground color, it will override the L/C/R color coding. It proposes a unified `renderScoreArea` function and adds an `InSearchMode` boolean to the render options, which is essential for context.

**Agreement:**
- Replace Enum with Bitflags.
- Use Lipgloss for specific L/C/R coloring.
- `ItemScores` needs to track the source history, not just the primary source.

**Disagreement/Improvements:**
- **Layout:** Response A uses dynamic width; Response B uses fixed width (superior for TUI stability).
- **Selection:** Response B warns about color overrides; Response A does not.

### SYNERGIZE

The unified plan will adopt **Response A's** specific color definitions and handler update logic (which were very clear) but implement **Response B's** "Fixed Width" layout strategy and `renderScoreArea` abstraction.

I will also incorporate Response B's warning regarding selection styles. To ensure the L/C/R colors remain visible when a row is selected, the application must rely on **Background** styling for selection, rather than overriding the Foreground of the entire line.

### UNIFY

# Implementation Plan: L/C/R Result Source Markers

## 1. Data Model Updates (Bitflags)

We will replace the mutually exclusive `ScoreSource` enum with a bitmask to track the provenance of an item through the pipeline.

**`store/types.go`** (or wherever `ItemScores` is defined):

```go
type ScoreSource uint8

const (
    ScoreSourceNone    ScoreSource = 0
    ScoreSourceLexical ScoreSource = 1 << iota // 1: L
    ScoreSourceCosine                          // 2: C
    ScoreSourceRerank                          // 4: R
)

// Has checks if a specific stage bit is set
func (s ScoreSource) Has(flag ScoreSource) bool { return s&flag != 0 }

type ItemScores struct {
    CosineScore float32
    RerankScore float32
    Source      ScoreSource // Bitmask of all contributing stages
}
```

## 2. Styling & Marker Rendering

We will define specific styles for the letters. **Crucial:** To ensure these colors are visible when a row is selected, your `SelectedItem` style should primarily use `Background` colors and avoid setting a global `Foreground` that would overwrite these.

**`ui/styles.go`**:

```go
var (
    // Colors
    ColorLexical = lipgloss.Color("75")  // Blue
    ColorCosine  = lipgloss.Color("220") // Yellow
    ColorRerank  = lipgloss.Color("82")  // Green
    ColorBracket = lipgloss.Color("240") // Dim Gray

    // Styles
    StyleL = lipgloss.NewStyle().Foreground(ColorLexical)
    StyleC = lipgloss.NewStyle().Foreground(ColorCosine)
    StyleR = lipgloss.NewStyle().Foreground(ColorRerank)
    StyleBracket = lipgloss.NewStyle().Foreground(ColorBracket)

    // Fixed widths to prevent layout jitter
    // Marker is max 5 chars: "[LCR]"
    MarkerWidth = 5 
    // Score is 4 chars: "0.82"
    ScoreNumWidth = 4
)
```

**`ui/rendering.go`**:

```go
// renderSourceMarker generates the [LCR] string. 
// It does NOT handle padding; that is done by the caller to ensure alignment.
func renderSourceMarker(s store.ScoreSource) string {
    if s == store.ScoreSourceNone {
        return ""
    }

    var sb strings.Builder
    sb.WriteString(StyleBracket.Render("["))

    if s.Has(store.ScoreSourceLexical) {
        sb.WriteString(StyleL.Render("L"))
    }
    if s.Has(store.ScoreSourceCosine) {
        sb.WriteString(StyleC.Render("C"))
    }
    if s.Has(store.ScoreSourceRerank) {
        sb.WriteString(StyleR.Render("R"))
    }

    sb.WriteString(StyleBracket.Render("]"))
    return sb.String()
}
```

## 3. Integration into `renderItemLine`

We introduce a helper `renderScoreArea` that handles the layout of both the numeric score and the marker. We also add `InSearchMode` to options so we know when to hide/show the marker.

**Updated Options:**
```go
type RenderOptions struct {
    ScoreMap     map[string]store.ItemScores
    ShowScores   bool
    InSearchMode bool // New flag
}
```

**Helper Function (Score Area):**
```go
func renderScoreArea(scores store.ItemScores, opts RenderOptions) (string, int) {
    // If not in search mode, show nothing (or standard date/time if that was original behavior)
    if !opts.InSearchMode {
        return "", 0
    }

    // 1. Generate Marker
    rawMarker := renderSourceMarker(scores.Source)
    // Force marker to occupy 5 cells (align left)
    markerCell := lipgloss.NewStyle().
        Width(MarkerWidth).
        Align(lipgloss.Left).
        Render(rawMarker)

    // 2. Generate Numeric Score (if enabled)
    numCell := ""
    if opts.ShowScores {
        val := ""
        // Pick best score for display
        if scores.Source.Has(store.ScoreSourceRerank) {
            val = fmt.Sprintf("%.2f", scores.RerankScore)
        } else if scores.Source.Has(store.ScoreSourceCosine) {
            val = fmt.Sprintf("%.2f", scores.CosineScore)
        }
        // Force number to occupy 4 cells (align right)
        numCell = lipgloss.NewStyle().
            Width(ScoreNumWidth).
            Align(lipgloss.Right).
            Render(val)
    }

    // 3. Combine
    // Layout: "0.82 [LCR]" or "     [LCR]"
    fullStr := ""
    totalWidth := MarkerWidth

    if opts.ShowScores {
        fullStr = numCell + " " + markerCell
        totalWidth += ScoreNumWidth + 1 // +1 for gap
    } else {
        fullStr = markerCell
    }

    return fullStr, totalWidth
}
```

**Updated `renderItemLine`:**
```go
func renderItemLine(item store.Item, selected bool, width int, opts RenderOptions) string {
    // 1. Render Badge
    badge := renderSourceBadge(item.SourceName) // Assuming existing function
    badgeWidth := lipgloss.Width(badge)

    // 2. Render Score/Marker Area
    scoreArea := ""
    scoreWidth := 0
    
    // Look up scores safely
    if s, ok := opts.ScoreMap[item.ID]; ok {
        scoreArea, scoreWidth = renderScoreArea(s, opts)
    } else if opts.InSearchMode {
         // Handle edge case: Item in list but missing from map (rare)
         // Render empty placeholder of correct width to maintain alignment
         w := MarkerWidth
         if opts.ShowScores { w += ScoreNumWidth + 1 }
         scoreArea = lipgloss.NewStyle().Width(w).Render("")
         scoreWidth = w
    }

    // 3. Calculate Title Width
    // Available = Total - Badge - ScoreArea - Gaps
    gap := 1
    usedWidth := badgeWidth + gap + scoreWidth + gap
    titleWidth := width - usedWidth
    
    // Ensure minimum title width
    if titleWidth < 10 { titleWidth = 10 }

    // 4. Render Title
    title := item.Title
    if utf8.RuneCountInString(title) > titleWidth {
        // Truncate logic...
        runes := []rune(title)
        title = string(runes[:titleWidth-1]) + "…"
    }
    
    // Apply Selection Style (Background only recommended for L/C/R visibility)
    titleStyle := NormalItemStyle
    if selected {
        titleStyle = SelectedItemStyle 
    }
    styledTitle := titleStyle.Render(title)

    // 5. Assemble
    return lipgloss.JoinHorizontal(lipgloss.Top,
        badge,
        strings.Repeat(" ", gap),
        scoreArea,
        strings.Repeat(" ", gap),
        styledTitle,
    )
}
```

## 4. Progressive Update Logic

Update the `ScoreMap` in your Bubble Tea `Update` loop as messages arrive. Use the bitwise OR operator `|=` to accumulate history.

**1. Lexical Stage (FTS5 Results):**
```go
// In handleFTSResults
for _, item := range msg.Items {
    s := m.scoreMap[item.ID]
    s.Source |= store.ScoreSourceLexical // Set L flag
    m.scoreMap[item.ID] = s
}
```

**2. Cosine Stage (Embedding Results):**
```go
// In handleCosineResults
for id, score := range msg.Scores {
    s := m.scoreMap[id]
    s.CosineScore = score
    s.Source |= store.ScoreSourceCosine // Adds C flag, preserving L if present
    m.scoreMap[id] = s
}
```

**3. Rerank Stage (Cross-Encoder Results):**
```go
// In handleRerankResults
for i, entry := range msg.Entries {
    s := m.scoreMap[entry.ID]
    s.RerankScore = msg.Scores[i]
    s.Source |= store.ScoreSourceRerank // Adds R flag
    m.scoreMap[entry.ID] = s
}
```

## 5. Summary of Width Calculation

To avoid UI jitter, we treat the marker column as a fixed block:

| Component | Width | Notes |
| :--- | :--- | :--- |
| **Number** | 4 | `0.82` (Hidden if `!ShowScores`) |
| **Gap** | 1 | Space between number and marker |
| **Marker** | 5 | `[L]  `, `[LC] `, `[LCR]` |

**Total Impact:**
- Search + Scores: **10 chars** consumed.
- Search + No Scores: **5 chars** consumed.
- Normal Mode: **0 chars** consumed.

---

## Feature 8: Filter Chips / Query Language

### 8a. Query Parser

### ANALYZE
- **Common ground:** Both responses correctly identify the needed structured filters (`source`, `type`, `after`, `before`, `read/unread`, `saved`), the need for a custom duration parser (days/weeks), and the requirement that invalid tokens must fall back to free-text without errors. Both also include an ordered `Filters` slice for UI chips and propose a “remove chip → rebuild query” method.
- **Response A strengths:** Clean `FilterKind` enum, explicit `Key/Value/Raw` on each filter, and a straightforward `SearchOptions` shape (`ReadState *bool`, `SavedOnly bool`).
- **Response A gaps/risks:**
  - Rebuilds the query as `filters + FreeText`, which **does not preserve original token order** (important when removing chips; user expects `foo source:hn bar` → remove chip → `foo bar`, not `bar foo` or reordered output).
  - No way to control “now” for deterministic tests.
  - Tokenization uses whitespace splitting only; no quoted-string support.
- **Response B strengths:** Adds a `now time.Time` parameter for deterministic parsing/tests and provides an extensive test matrix.
- **Response B gaps/bugs:**
  - Code references `errors.New(...)` but **doesn’t import `errors`**.
  - `parseDuration` supports seconds (`s`) though the requirements only call for `m/h/d/w` (not harmful, but off-spec).
  - Same ordering problem in reconstruction: `FullQuery` and `QueryWithout` rebuild as `filters then FreeText`, **losing original interleaving**.
  - `Parse(input string, now time.Time)` deviates from required `Parse(input string) SearchOptions` (though a testable internal helper is a good idea).

### SYNERGIZE
- Combine **A’s stronger type modeling** (enum `FilterKind`, explicit `Key/Value/Raw`) with **B’s deterministic-time testing approach** by keeping the public API `Parse(input string)` and adding an internal `parseAt(input, now)` helper used by tests.
- Improve beyond both by introducing a lightweight lexer that:
  - preserves **original token order** for round-trip and chip removal,
  - supports **quoted strings** (at least `"..."` and `'...'`) without breaking live typing tolerance,
  - keeps a per-token `Raw` representation so reconstructed queries match user intent.
- Keep `SearchOptions.FreeText` as an embedding-ready string (unquoted), while also storing enough internal information to rebuild the query accurately when removing a filter chip.

### UNIFY
Below is a concrete implementation plan for `internal/query/` that meets all requirements and strengthens round-trip behavior.

---

## 1) Package layout

```
internal/query/
  types.go        // FilterKind, Filter, SearchOptions (+ internal token/part types)
  lexer.go        // tokenize(input) with quote support
  duration.go     // ParseRelativeDuration("7d","2w",...)
  parse.go        // Parse(), parseAt(), core parser
  query_test.go   // table-driven tests
```

---

## 2) Types: `SearchOptions` + `Filter` (chips)

```go
package query

import "time"

type FilterKind int

const (
	FilterSource FilterKind = iota
	FilterType
	FilterAfter
	FilterBefore
	FilterRead
	FilterUnread
	FilterSaved
)

// Filter is a successfully-parsed structured filter token.
// Raw is used for chip rendering and query reconstruction.
type Filter struct {
	Kind  FilterKind // stable kind for UI styling
	Key   string     // canonical key: "source","type","after","before","read","unread","saved"
	Value string     // unquoted value; empty for boolean flags
	Raw   string     // original token substring as typed (preserve case/quotes)
}

// SearchOptions is the parsed query.
type SearchOptions struct {
	// Structured filters
	Sources   []string   // source:<name> values (store normalized, e.g., strings.ToLower)
	Types     []string   // type:<rss|hn|reddit> normalized lowercase
	After     *time.Time // published >= After (computed as now - duration)
	Before    *time.Time // published <= Before (computed as now - duration)
	ReadState *bool      // nil=any, true=read, false=unread
	SavedOnly bool       // true if "saved" present

	// Semantic search input
	FreeText string // remaining non-filter text (unquoted, space-joined)

	// Chips in input order (only successful structured filters)
	Filters []Filter

	// internal: preserves original token order for round-trip removal
	parts []part
}

type part struct {
	raw        string // original token as typed (including quotes)
	filterIdx  int    // -1 if not a successful filter; otherwise index into SearchOptions.Filters
	isFreeText bool   // true if contributes to FreeText
	textValue  string // unquoted/normalized free-text contribution
}
```

Notes:
- `Filters` holds only **successful** filters (invalid filter-shaped tokens become free-text parts).
- `parts` preserves the original sequence of tokens, enabling correct reconstruction after chip removal.

---

## 3) Tokenization (with quotes, tolerant)

### Goal
Split input into tokens similarly to a shell: whitespace separates tokens unless inside `'...'` or `"..."`. Quotes may appear mid-token (`source:"New York Times"`), and unmatched quotes should not error—just produce a token that likely won’t parse as a filter and thus becomes free-text.

### Signature
```go
func tokenize(input string) []string
```

### Behavior
- Walk runes, building the current token.
- Track `inQuote` and `quoteChar` (`'` or `"`).
- Whitespace ends a token only when not `inQuote`.
- Quotes are kept in the raw token (so chips display exactly what the user typed), but later we can “unquote” values when extracting `Value` / free-text.

Helper:
```go
func unquoteIfQuoted(s string) (string, bool)
// returns (unquoted, true) only if s is fully wrapped in matching single or double quotes.
// Otherwise returns (s, false).
```

---

## 4) Duration parsing (`7d`, `2w`, `1h`, `30m`)

### Signature
```go
func ParseRelativeDuration(s string) (time.Duration, bool)
```

### Rules
- Accept `^\d+[mhdw]$` (case-insensitive unit).
- Integers only (per requirements). No multi-unit (`1h30m`)—treat as invalid → free-text.
- Map:
  - `m` → minute
  - `h` → hour
  - `d` → 24 * hour
  - `w` → 7 * 24 * hour

Implementation is simplest as:
- trim spaces, lower-case
- unit := last rune, num := prefix
- `strconv.ParseInt(num, 10, 64)`
- multiply by unit duration

---

## 5) Parsing algorithm

### Public API (as required)
```go
func Parse(input string) SearchOptions {
	return parseAt(input, time.Now())
}
```

### Testable helper
```go
func parseAt(input string, now time.Time) SearchOptions
```

### Core logic (per token)
For each `rawTok := range tokenize(input)`:

1. Try `key:value` form:
   - `strings.SplitN(rawTok, ":", 2)`
   - `key := strings.ToLower(strings.TrimSpace(k))`
   - `valRaw := strings.TrimSpace(v)` (may include quotes)
   - `val, _ := unquoteIfQuoted(valRaw)` (if quoted, strip for Value)
2. Switch on `key`:
   - `source`: valid if `val` not empty
     - store `strings.ToLower(val)` into `Sources`
     - append `Filter{Kind:FilterSource, Key:"source", Value:val, Raw:rawTok}`
   - `type`: valid if `valLower` in `{rss, hn, reddit}`
     - store normalized lowercase in `Types`
     - chip `Value: val` (unquoted) and `Raw` as typed
   - `after` / `before`: parse duration from `val`
     - if ok: compute bound = `now.Add(-dur)`; set `After`/`Before`
     - append filter chip
     - invalid → treat as free-text token
   - unknown key → free-text
3. If not `key:value`, check flags (case-insensitive):
   - `read`: `ReadState = &true`
   - `unread`: `ReadState = &false`
   - `saved`: `SavedOnly = true`
   - otherwise free-text
4. Free-text handling:
   - for embedding, add an unquoted/cleaned value:
     - if token is fully quoted, strip quotes for `FreeText` contribution
     - else use raw token as-is
   - append a `part{isFreeText:true, textValue:...}`

### Multiple filters semantics
- `Sources`, `Types`: accumulate (later interpreted as OR/IN); optionally de-dup while preserving first occurrence.
- `After` / `Before`:
  - simplest: apply in sequence (“last wins”) because it matches user typing expectations and keeps implementation simple.
  - alternative (also reasonable): combine constraints (`After = max(after...)`, `Before = min(before...)`). If you choose combine, document it and test it. Either approach is acceptable; “last wins” pairs well with chip removal + reparse.
- `read/unread`: last wins (`ReadState` overwritten).
- `saved`: idempotent.

---

## 6) Query reconstruction + chip removal

### Requirement-driven method
Provide a method that rebuilds the query string **in original token order**, excluding one filter chip by index.

```go
// QueryTextWithout rebuilds the query omitting Filters[filterIndex].
// If filterIndex is out of range, it returns the full reconstructed query.
func (s SearchOptions) QueryTextWithout(filterIndex int) string
```

Implementation:
- Iterate `s.parts` in order:
  - if `p.filterIdx == filterIndex`, skip
  - else append `p.raw`
- `strings.Join(out, " ")`

Also helpful (for debugging/UI):
```go
func (s SearchOptions) QueryText() string // same as QueryTextWithout(-1)
```

This fixes the main weakness in both prior responses: preserving interleaving like
`foo source:hn bar` → remove chip → `foo bar` (not reordered).

---

## 7) Table-driven tests (coverage plan)

Use a fixed `now` in tests via `parseAt`.

```go
now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
```

### Suggested test cases (minimum set)
1. **Empty**
   - input: `""`
   - FreeText: `""`, Filters: none
2. **Free-text only**
   - `climate risk` → FreeText `climate risk`
3. **Single source**
   - `source:hn` → Sources `["hn"]`, Filters[0].Raw == `source:hn`
4. **Invalid source**
   - `source:` → treated as FreeText `source:`
5. **Type validation**
   - `type:rss` ok; `type:foo` becomes free-text
6. **after/before valid**
   - `after:7d` → After == now-7*24h
   - `before:2w` → Before == now-14*24h
7. **after invalid**
   - `after:xyz` → free-text includes `after:xyz`
8. **Flags**
   - `read`, `unread`, `saved`
   - `read unread` → ReadState == false (last wins), two chips preserved
9. **Mixed ordering + round-trip removal**
   - input: `foo source:hn bar after:7d unread baz`
   - FreeText: `foo bar baz`
   - Filters in appearance order: source, after, unread
   - `QueryTextWithout(0)` should yield: `foo bar after:7d unread baz`
   - `QueryTextWithout(1)` should yield: `foo source:hn bar unread baz`
10. **Quoted values**
   - `source:"New York Times" climate`
     - Sources contains `new york times`
     - Filter.Value == `New York Times`
     - FreeText == `climate`
   - `"climate risk" after:7d`
     - FreeText == `climate risk` (quotes stripped for embedding)
11. **Unmatched quote tolerance**
   - `source:"NYT climate` → tokenization produces something; parsing should not panic; likely becomes free-text.
12. **Colon in value**
   - `source:hn:extra` → valid source value `hn:extra` (SplitN behavior), chip raw preserved.

Assertions:
- Exact slices (`Sources`, `Types`) and `Filters` (Kind/Key/Value/Raw).
- Time equality for bounds.
- `QueryTextWithout(i)` outputs expected order-preserving strings.
- Invalid filters do not create chips and remain in `FreeText`.

---

## 8) Integration notes (where it plugs in)
- In `internal/ui/app.go` `submitSearch()`:
  - `opts := query.Parse(filterInput.Value())`
  - embed/rerank using `opts.FreeText`
  - apply structured constraints via SQL and/or existing pure filters
  - render chips from `opts.Filters`
  - on chip removal: `filterInput.SetValue(opts.QueryTextWithout(i))` then re-parse

---

This plan yields a tolerant, testable parser that supports relative time filters, preserves input order for correct chip removal, and cleanly separates structured constraints from embedding free-text.

### 8b. Filter Application

### ANALYZE
Both responses implement the same core idea: split `SearchOptions` into **SQL-pushable filters** (source/type/time/read/saved) and **in-memory post-filters**, then optionally run an **embedding-based search** on the narrowed candidate set.

Key differences / issues:
- **SQL builder shape**
  - One returns clause **without** `WHERE` (matches your deliverable), the other returns it **with** `WHERE` (easy to adapt).
  - One uses `LOWER(field)` and lowercased args (case-insensitive but can defeat indexes); the other uses direct `IN` (index-friendly but case-sensitive).
- **ReadState parameterization**
  - One emits `read = 0/1` literals (no args) while the other uses `read = ?` with a bool arg. SQLite typically handles bools fine, but using `0/1` args is more predictable.
- **Embedding column**
  - One example query omitted `embedding`, which can break “candidate pool then embedding rerank” unless embeddings are fetched later. The other used `SELECT *` (can be inefficient and may not match an existing scanner that expects explicit columns).
- **Post-filtering**
  - One had only a stub; the other had a real pipeline + an overfetch strategy to avoid post-filters reducing results below the requested limit.
- **Thread safety**
  - One explicitly held `RLock()` in `GetFilteredItems`; the other implied `queryItems` handles locking. You should be consistent with the existing store pattern.

### SYNERGIZE
A stronger combined plan:
- Keep the deliverable-friendly builder signature: **returns clause without `WHERE`**.
- Use **index-friendly case-insensitive matching** via `COLLATE NOCASE` (better than `LOWER(...)`), while still supporting canonicalization as a fallback.
- Add a **query plan** concept: SQL where/args + whether embeddings are needed + post-filters. This cleanly supports “SQL first, in-memory second” and “embedding rerank after SQL narrowing”.
- Avoid `SELECT *`: select explicit columns, and **include `embedding` only when needed** (when `FreeText != ""` or a post-filter requires it).
- Use an **overfetch multiplier** only when post-filters are present (or when the caller requests a bigger candidate pool), with a hard cap (e.g., 5000).
- Provide table-driven tests for SQL generation and an integration test on an in-memory SQLite DB.

### UNIFY
## Implementation Plan: Filter Application Layer (SQL WHERE + In-Memory Post-Filtering)

### 0) Goals & architecture
We’ll build a two-stage filtering pipeline:

1. **SQL pushdown (SQLite)**: `Sources`, `Types`, `After/Before`, `ReadState`, `SavedOnly` become a parameterized `WHERE` clause.
2. **In-memory post-filtering (Go)**: `opts.Filters []Filter` (and any future non-SQL filters like semantic dedup/diversity) run after the DB query.
3. **Embedding/reranking**: If `FreeText` is non-empty, we run embedding similarity/reranking on the *already SQL-filtered + post-filtered* candidates.

This keeps I/O small and expensive similarity computations bounded.

---

## 1) SQL WHERE clause builder

### Design notes
- Prevent injection: only use `?` placeholders; never interpolate user strings into SQL tokens.
- Keep deterministic arg ordering.
- Prefer index-friendly case-insensitive matching in SQLite using `COLLATE NOCASE` instead of `LOWER(...)`:
  - `source_name COLLATE NOCASE IN (?, ?)`
  - `source_type COLLATE NOCASE IN (?, ?)`

### Code: `BuildWhereClause(opts SearchOptions) (string, []any, error)`
Returns clause **without** a leading `WHERE` (per deliverable). `error` is optional but useful for `After > Before`.

```go
package store

import (
	"fmt"
	"strings"
	"time"
)

func BuildWhereClause(opts SearchOptions) (string, []any, error) {
	var conds []string
	var args []any

	addIN := func(expr string, values []string) {
		if len(values) == 0 {
			return
		}
		ph := make([]string, len(values))
		for i := range values {
			ph[i] = "?"
			args = append(args, values[i])
		}
		conds = append(conds, fmt.Sprintf("%s IN (%s)", expr, strings.Join(ph, ",")))
	}

	// Source/type (case-insensitive, index-friendly if indexes use NOCASE collation)
	addIN("source_name COLLATE NOCASE", opts.Sources)
	addIN("source_type COLLATE NOCASE", opts.Types)

	// Time range
	if opts.After != nil {
		conds = append(conds, "published_at > ?")
		args = append(args, *opts.After)
	}
	if opts.Before != nil {
		conds = append(conds, "published_at < ?")
		args = append(args, *opts.Before)
	}
	if opts.After != nil && opts.Before != nil && opts.After.After(*opts.Before) {
		return "", nil, fmt.Errorf("invalid time range: after (%s) is after before (%s)",
			opts.After.Format(time.RFC3339), opts.Before.Format(time.RFC3339))
	}

	// Read state: stored as INTEGER (0/1)
	if opts.ReadState != nil {
		conds = append(conds, "read = ?")
		if *opts.ReadState {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	// SavedOnly
	if opts.SavedOnly {
		conds = append(conds, "saved = 1")
	}

	if len(conds) == 0 {
		return "", nil, nil
	}
	return strings.Join(conds, " AND "), args, nil
}
```

### SQL examples produced
- Sources + unread + time window:
  ```sql
  WHERE source_name COLLATE NOCASE IN (?,?)
    AND published_at > ?
    AND read = ?
  ORDER BY published_at DESC
  LIMIT ?
  ```
  args: `["hn","reddit", afterTime, 0, limit]`

- Saved-only:
  ```sql
  WHERE saved = 1
  ORDER BY published_at DESC
  LIMIT ?
  ```

---

## 2) Store integration: `GetFilteredItems(opts, limit)` (SQL + post-filters)

### Key behavior
- Build WHERE + args.
- Query ordered by `published_at DESC`.
- Overfetch candidates when post-filters exist (so you still return up to `limit` after filtering), capped at 5000.
- Include `embedding` column only when it will be used (FreeText or embedding-based post-filter).

### Filter interface (in-memory)
You already have `Filters []Filter`. Make it explicit that these are post-SQL filters:

```go
type Filter interface {
	Apply([]Item) ([]Item, error)
	// Optional: declare whether this filter needs embedding data loaded.
	NeedsEmbedding() bool
}
```

If you already have `type Filter func([]Item) []Item`, you can keep it and add a separate interface later; the plan still works.

### Query planning helper
```go
func needsEmbedding(opts SearchOptions) bool {
	if strings.TrimSpace(opts.FreeText) != "" {
		return true
	}
	for _, f := range opts.Filters {
		if f != nil && f.NeedsEmbedding() {
			return true
		}
	}
	return false
}
```

### Store method
This keeps the existing `queryItems(query, args...)` integration pattern and the store’s `RWMutex` model.

```go
const (
	postFilterMultiplier = 5
	maxCandidates        = 5000
)

func (s *Store) GetFilteredItems(opts SearchOptions, limit int) ([]Item, error) {
	if limit <= 0 {
		return []Item{}, nil
	}

	where, args, err := BuildWhereClause(opts)
	if err != nil {
		return nil, err
	}

	// Overfetch only if post-filters are present (otherwise LIMIT is exact).
	effectiveLimit := limit
	if len(opts.Filters) > 0 {
		effectiveLimit = limit * postFilterMultiplier
		if effectiveLimit > maxCandidates {
			effectiveLimit = maxCandidates
		}
	}

	// Select explicit columns; include embedding only when needed.
	selectCols := `id, source_type, source_name, title, summary, url, author,
                   published_at, fetched_at, read, saved`
	if needsEmbedding(opts) {
		selectCols += `, embedding`
	}

	q := "SELECT " + selectCols + " FROM items"
	if where != "" {
		q += " WHERE " + where
	}
	q += " ORDER BY published_at DESC LIMIT ?"
	args = append(args, effectiveLimit)

	s.mu.RLock()
	defer s.mu.RUnlock()

	items, err := s.queryItems(q, args...)
	if err != nil {
		return nil, err
	}

	// In-memory post-filter pipeline (stable order expected).
	for _, f := range opts.Filters {
		if f == nil {
			continue
		}
		items, err = f.Apply(items)
		if err != nil {
			return nil, err
		}
	}

	// Truncate to requested limit.
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
```

Notes:
- If any post-filter may reorder items, either enforce “filters must preserve order” or re-sort by `PublishedAt DESC` after the pipeline.

---

## 3) In-memory post-filter pipeline design
- `opts.Filters` is the extension point for “not SQL-friendly” filters.
- Examples:
  - semantic dedup (needs embeddings)
  - complex title normalization rules
  - “diversity” constraints across sources
  - heuristics using multiple fields

Example filter:
```go
type ByAuthorFilter struct{ Substr string }

func (f ByAuthorFilter) NeedsEmbedding() bool { return false }

func (f ByAuthorFilter) Apply(items []Item) ([]Item, error) {
	sub := strings.ToLower(f.Substr)
	out := items[:0]
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Author), sub) {
			out = append(out, it)
		}
	}
	return out, nil
}
```

---

## 4) Composition with embedding search / reranking
Rule: **SQL narrowing happens first**, then optional post-filters, then embedding similarity.

Typical search flow:
1. Parse query → `SearchOptions` (structured filters + `FreeText`)
2. Decide a candidate pool size (often bigger than UI limit)
3. `GetFilteredItems(opts, candidateLimit)`
4. If `FreeText != ""`: embed query + score candidates + rerank top K
5. Return final `limit`

Example orchestration:
```go
func (svc *Service) Search(opts SearchOptions, limit int) ([]Item, error) {
	// Candidate pool: widen for semantic ranking, but keep bounded.
	candidateLimit := limit
	if strings.TrimSpace(opts.FreeText) != "" {
		candidateLimit = min(limit*50, 5000) // tune as needed
	}

	candidates, err := svc.store.GetFilteredItems(opts, candidateLimit)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(opts.FreeText) == "" {
		if len(candidates) > limit {
			return candidates[:limit], nil
		}
		return candidates, nil
	}

	// Embedding + rerank only over SQL-narrowed candidates
	ranked, err := svc.rerankByEmbedding(candidates, opts.FreeText, limit)
	if err != nil {
		return nil, err
	}
	return ranked, nil
}
```

This satisfies: “SQL filters narrow the candidate pool BEFORE embedding search.”

---

## 5) Test cases (SQL generation + edge cases)

### Table-driven unit tests for `BuildWhereClause`
```go
func TestBuildWhereClause(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ptr := func(b bool) *bool { return &b }

	tests := []struct {
		name      string
		opts      SearchOptions
		wantWhere string
		wantArgs  []any
		wantErr   bool
	}{
		{"empty", SearchOptions{}, "", nil, false},
		{"sources", SearchOptions{Sources: []string{"hn"}}, "source_name COLLATE NOCASE IN (?)", []any{"hn"}, false},
		{"types", SearchOptions{Types: []string{"rss", "api"}}, "source_type COLLATE NOCASE IN (?,?)", []any{"rss", "api"}, false},
		{"time", SearchOptions{After: &now}, "published_at > ?", []any{now}, false},
		{"readstate", SearchOptions{ReadState: ptr(false)}, "read = ?", []any{0}, false},
		{"savedonly", SearchOptions{SavedOnly: true}, "saved = 1", nil, false},
		{"combined", SearchOptions{
			Sources:   []string{"CNN", "BBC"},
			After:     &now,
			ReadState: ptr(true),
			SavedOnly: true,
		}, "source_name COLLATE NOCASE IN (?,?) AND published_at > ? AND read = ? AND saved = 1",
			[]any{"CNN", "BBC", now, 1}, false,
		},
		{"bad time range", SearchOptions{
			After:  func() *time.Time { t := now.Add(24 * time.Hour); return &t }(),
			Before: &now,
		}, "", nil, true},
		{"injection safe", SearchOptions{
			Sources: []string{"'; DROP TABLE items; --"},
		}, "source_name COLLATE NOCASE IN (?)", []any{"'; DROP TABLE items; --"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotWhere, gotArgs, err := BuildWhereClause(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if gotWhere != tt.wantWhere || !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Fatalf("got (%q,%v) want (%q,%v)", gotWhere, gotArgs, tt.wantWhere, tt.wantArgs)
			}
		})
	}
}
```

### Integration test idea
- Create `:memory:` SQLite DB.
- Create `items` table schema.
- Insert rows with different sources/types/read/saved/published_at.
- Assert `GetFilteredItems` returns correct IDs, correct order, and respects `limit`.
- Include a post-filter that removes some items to validate overfetch works.

---

## 6) Migration path (no breaking changes)
1. **Add** `BuildWhereClause` + `GetFilteredItems`.
2. Keep existing APIs, re-implement them as wrappers:
   ```go
   func (s *Store) GetItems(limit int, includeRead bool) ([]Item, error) {
	   var rs *bool
	   if !includeRead {
		   unread := false
		   rs = &unread
	   }
	   return s.GetFilteredItems(SearchOptions{ReadState: rs}, limit)
   }

   func (s *Store) GetItemsSince(since time.Time) ([]Item, error) {
	   t := since
	   // pick existing default limit behavior or introduce one explicitly
	   return s.GetFilteredItems(SearchOptions{After: &t}, 5000)
   }
   ```
3. Update new search UI / command handlers to call `GetFilteredItems(opts, candidateLimit)` and then optionally run embedding rerank.
4. Later: deprecate older methods once all callers use `SearchOptions`.

---

### Performance notes (practical)
- Add indexes if not present: `published_at`, `source_name`, `source_type`, `read`, `saved` (and consider composite indexes if common).
- Avoid `LOWER(column)` in WHERE if you care about index usage; prefer `COLLATE NOCASE` and matching index collation.
- Keep candidate pool bounded (e.g., 5000) to keep embedding reranking fast and predictable.

This design meets all deliverables: safe parameterized SQL pushdown, store integration, extensible post-filter pipeline, clean embedding composition, strong test coverage for SQL generation, and a backwards-compatible migration path.

### 8c. Chip Rendering

### ANALYZE
- **Both responses agree** on: rendering 1-line horizontal chips via lipgloss; chips only visible when filters exist; adjusting `contentHeight` to account for the extra line; color-coding by filter type; and truncation/overflow needs.
- **Response A strengths:** simple rendering API close to the requested `RenderChips(filters, width)`; includes width-aware truncation and clean `contentHeight--` integration.  
  **Gaps / issues:** it effectively punts on per-chip removal (“retype query”) and doesn’t implement “navigate + X/Backspace” removal. Also its suggested colors don’t match the requirement (source=blue, time=green, state=yellow), and it doesn’t actually render the `×` in the chip.
- **Response B strengths:** complete interactive model (Tab focus, arrow navigation, delete/backspace/x removal), plus query reconstruction and rerun search.  
  **Gaps / issues:** `RenderChipsBar` signature doesn’t match the deliverable; overflow handling is hand-wavy (may wrap); and it treats `Filter.Kind` as a string, which may not match your `query.FilterKind` (but `Filter.Key` is a safer discriminator). Also, using borders for focus can accidentally increase height—underline is safer for a strict 1-line bar.

### SYNERGIZE
- Use **Response B’s interaction + removal flow** (Tab to focus chips, arrows to select, Backspace/Delete/X to remove) because it best satisfies the “removable chips” UX.
- Use **Response A’s width-aware truncation approach**, but improve it into a **scrolling “window”** so *every* chip remains reachable even when there are many (important for “navigate to a chip”).
- Keep the required public function **`RenderChips(filters, width)`**, and add an internal interactive variant that supports focus/selection/offset without breaking the deliverable.

### UNIFY
## Implementation Plan: Visual Filter Chips Above Results

### UX / Interaction Flow (meets requirements)
- Chip bar appears **only when `len(SearchOptions.Filters) > 0`**.
- **Tab** toggles focus between **Stream** and **Chip Bar** (only when not actively typing in the `/` search input).
- When Chip Bar is focused:
  - **Left/Right** (or `h/l`) moves the selected chip.
  - **Backspace/Delete/X** removes the selected chip.
  - **Esc/Enter** exits chip focus back to the stream.
- Optional quality-of-life:
  - **Ctrl+X** clears *all* filters (keeps free text).

This provides navigation + remove while keeping the rest of the app’s focus model simple.

---

## 1) Styles (`internal/ui/styles.go`)
Add styles + colors. Use **underline** for focus (stays 1 line).

```go
package ui

import "github.com/charmbracelet/lipgloss"

var (
	ChipBarStyle = lipgloss.NewStyle().
		Height(1).
		PaddingLeft(1).
		PaddingRight(1)

	ChipBaseStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff"))

	ChipSelectedStyle = ChipBaseStyle.Copy().
		Underline(true)

	ChipMoreStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#a1a1aa")). // muted
		Bold(true)

	// Accent colors per requirement
	ChipSourceBg = lipgloss.Color("#3b82f6") // blue
	ChipTimeBg   = lipgloss.Color("#22c55e") // green
	ChipStateBg  = lipgloss.Color("#eab308") // yellow
	ChipDefaultBg = lipgloss.Color("#52525b") // gray
)
```

If you want the bar to visually “sit” in your existing theme, you can also set `ChipBarStyle.Background(...)` to match your filter/status bar background.

---

## 2) Chip Rendering (`internal/ui/chips.go`)
Deliverable requires:

```go
RenderChips(filters []query.Filter, width int) string
```

We’ll implement that plus an internal interactive renderer that supports:
- `active` (focused or not)
- `selectedIdx` (which chip is selected)
- `offset` (horizontal window start, to keep it 1-line without wrapping)

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"observer/internal/query"
)

func RenderChips(filters []query.Filter, width int) string {
	return renderChips(filters, width, false, -1, 0)
}

func RenderChipsInteractive(filters []query.Filter, width int, active bool, selectedIdx int, offset int) string {
	return renderChips(filters, width, active, selectedIdx, offset)
}

func renderChips(filters []query.Filter, width int, active bool, selectedIdx int, offset int) string {
	if len(filters) == 0 || width <= 0 {
		return ""
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(filters) {
		offset = len(filters)
	}

	// Build a 1-line window of chips that fits width, with leading/trailing "…" as needed.
	more := ChipMoreStyle.Render("…")
	leftMore := offset > 0

	row := ""
	if leftMore {
		row = more
	}

	// Helper to try append " token" and check width.
	appendToken := func(cur, token string) (string, bool) {
		next := token
		if cur != "" {
			next = cur + " " + token
		}
		if lipgloss.Width(next) > width {
			return cur, false
		}
		return next, true
	}

	// We reserve space for trailing "…" if we won't fit all chips.
	lastShown := offset - 1
	for i := offset; i < len(filters); i++ {
		ch := renderChip(filters[i], active && i == selectedIdx)

		// If there are chips to the right, ensure we still can fit " …" at the end.
		hasRight := i < len(filters)-1
		testRow := row
		var ok bool
		testRow, ok = appendToken(testRow, ch)
		if !ok {
			break
		}
		if hasRight {
			// Try with trailing more as well
			withMore, ok2 := appendToken(testRow, more)
			if !ok2 {
				// Can't add this chip if it prevents us from showing overflow indicator.
				break
			}
			_ = withMore
		}

		row = testRow
		lastShown = i
	}

	rightMore := lastShown < len(filters)-1
	if rightMore {
		if r2, ok := appendToken(row, more); ok {
			row = r2
		} else if row == "" {
			row = lipgloss.Truncate(more, width)
		}
	}

	// Pad to full width; bar must be exactly 1 line.
	row = padRight(row, width)
	return ChipBarStyle.Width(width).Render(row)
}

func renderChip(f query.Filter, selected bool) string {
	bg := chipBg(f)

	style := ChipBaseStyle.Copy().Background(bg)
	if selected {
		style = ChipSelectedStyle.Copy().Background(bg)
	}

	label := chipLabel(f)
	// Requirement-style text. Background makes it “chip-like”.
	text := fmt.Sprintf("[%s ×]", label)
	return style.Render(text)
}

func chipLabel(f query.Filter) string {
	// Prefer Raw (preserves user input like `after:7d` or `unread`)
	if strings.TrimSpace(f.Raw) != "" {
		return f.Raw
	}
	// Fallback
	if f.Key != "" && f.Value != "" {
		return f.Key + ":" + f.Value
	}
	if f.Value != "" {
		return f.Value
	}
	return f.Key
}

func chipBg(f query.Filter) lipgloss.Color {
	// Use Key first; it’s stable across parser implementations.
	switch f.Key {
	case "source", "type":
		return ChipSourceBg
	case "after", "before", "since", "until":
		return ChipTimeBg
	case "read", "unread", "saved", "starred", "is", "state":
		return ChipStateBg
	default:
		// If Key is empty for unary tokens, Raw may be e.g. "unread"
		switch f.Raw {
		case "read", "unread", "saved":
			return ChipStateBg
		}
		return ChipDefaultBg
	}
}

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w >= width {
		return lipgloss.Truncate(s, width)
	}
	return s + strings.Repeat(" ", width-w)
}
```

---

## 3) Integration into `View()` (`internal/ui/app.go`)
Goal: chip bar is **1 line** and must be subtracted from stream height. Layout per requirement: **Search bar**, then **Chips**, then **Stream**, then **Status** (and error/debug as you already do).

Because your existing code currently appends `stream + errorBar + searchBar + statusBar`, you’ll likely want to switch to a vertical join so the chips can sit *between* the search bar and the stream deterministically.

Example integration sketch (adapt names to your exact fields):

```go
func (a App) View() string {
	// debug overlay handling unchanged...

	statusBar := a.renderStatusBar() // 1 line, existing
	statusH := 1

	errorBar := ""
	errH := 0
	if a.err != nil {
		errorBar = a.renderErrorBar()
		errH = 1
	}

	// Header line is your search input bar when active; otherwise your existing filter/status bar.
	header := ""
	headerH := 0
	if a.searchActive {
		header = a.searchInput.View() // 1 line
		headerH = 1
	} else {
		// Existing function you already have
		header = RenderFilterBarWithStatus(a.queryText, a.filteredCount, a.totalCount, a.width, a.statusText)
		headerH = 1
	}

	chips := ""
	chipsH := 0
	if len(a.searchOpts.Filters) > 0 {
		chips = RenderChipsInteractive(a.searchOpts.Filters, a.width, a.focus == focusChips, a.chipIdx, a.chipOffset)
		chipsH = 1
	}

	contentHeight := a.height - statusH - errH - headerH - chipsH
	if contentHeight < 0 {
		contentHeight = 0
	}

	stream := RenderStream(a.items, a.cursor, a.width, contentHeight, !a.hasQuery())

	parts := []string{}
	if header != "" {
		parts = append(parts, header)
	}
	if chips != "" {
		parts = append(parts, chips)
	}
	parts = append(parts, stream)
	if errorBar != "" {
		parts = append(parts, errorBar)
	}
	parts = append(parts, statusBar)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
```

This guarantees the chip bar consumes exactly one row and is placed correctly.

---

## 4) App State for Chip Interaction
Add minimal state to your `App` model:

```go
type focusArea int

const (
	focusStream focusArea = iota
	focusChips
)

type App struct {
	// ...
	focus     focusArea
	chipIdx   int // selected chip index
	chipOffset int // window start for overflow

	searchOpts query.SearchOptions // already in your context
	queryText  string              // last submitted full query string (optional but useful)
}
```

---

## 5) Chip Removal + Re-run Search (Bubble Tea Update)
### Key handling
In your `Update(msg tea.Msg)` (or wherever key handling lives):

- Only allow chip focus when `len(a.searchOpts.Filters) > 0` and `!a.searchActive`.

```go
case tea.KeyMsg:
    if !a.searchActive && len(a.searchOpts.Filters) > 0 && msg.String() == "tab" {
        if a.focus == focusStream {
            a.focus = focusChips
            if a.chipIdx >= len(a.searchOpts.Filters) {
                a.chipIdx = 0
            }
        } else {
            a.focus = focusStream
        }
        return a, nil
    }

    if a.focus == focusChips {
        switch msg.String() {
        case "left", "h":
            if a.chipIdx > 0 { a.chipIdx-- }
            a.ensureChipVisible()
            return a, nil
        case "right", "l":
            if a.chipIdx < len(a.searchOpts.Filters)-1 { a.chipIdx++ }
            a.ensureChipVisible()
            return a, nil
        case "backspace", "delete", "x":
            return a.removeChipAt(a.chipIdx)
        case "esc", "enter":
            a.focus = focusStream
            return a, nil
        }
        return a, nil // consume keys while in chip mode
    }
```

### Removing a chip and re-running search
Prefer to rebuild the query string and reuse your **existing parse + search pipeline**, so you don’t create a second “source of truth”.

```go
func (a App) removeChipAt(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(a.searchOpts.Filters) {
		return a, nil
	}

	// Remove filter
	a.searchOpts.Filters = append(a.searchOpts.Filters[:i], a.searchOpts.Filters[i+1:]...)

	// Fix selection / focus
	if a.chipIdx >= len(a.searchOpts.Filters) {
		a.chipIdx = len(a.searchOpts.Filters) - 1
	}
	if a.chipIdx < 0 {
		a.chipIdx = 0
		a.focus = focusStream
	}

	// Rebuild raw query and re-parse (keeps behavior identical to normal submission)
	newQuery := rebuildQueryString(a.searchOpts)
	a.queryText = newQuery
	a.searchInput.SetValue(newQuery) // keep UI consistent if you show it

	opts := query.Parse(newQuery)
	a.searchOpts = opts

	// Trigger your existing search command
	return a, a.runSearchCmd(opts)
}

func rebuildQueryString(opts query.SearchOptions) string {
	parts := []string{}
	if strings.TrimSpace(opts.FreeText) != "" { // adapt to your actual field name
		parts = append(parts, opts.FreeText)
	}
	for _, f := range opts.Filters {
		if strings.TrimSpace(f.Raw) != "" {
			parts = append(parts, f.Raw)
		} else if f.Key != "" && f.Value != "" {
			parts = append(parts, f.Key+":"+f.Value)
		}
	}
	return strings.Join(parts, " ")
}
```

> Adapt `opts.FreeText` to your actual “free text” field name (some codebases call it `Text`, `Query`, `Terms`, etc.).

---

## 6) Edge Cases: Too Many Chips / Narrow Terminals
**Hard requirement:** chip bar is **exactly 1 line** → no wrapping.

Implemented behavior:
- Chips are rendered from `chipOffset` onward.
- If there are hidden chips to the left or right, show a muted `…` chip as an overflow indicator.
- In chip-focus mode, keep the selected chip visible by adjusting `chipOffset`.

Minimal `ensureChipVisible()` strategy:
- If selected is left of offset → set `offset = selected`
- If selected is beyond last visible index → increment offset until visible

You’ll need a helper to compute “last visible index” for the current width. Keep it small and deterministic by using the same sizing logic as rendering (often easiest to share a `chipWindowEnd(filters,width,offset)` function).

If you don’t want scrolling in v1, you can truncate with a trailing `…`, but then hidden chips can’t be navigated/removed—scrolling is the robust solution.

---

## Acceptance Checklist
- With query `source:hn after:7d unread go 1.24`, chips show as one line with distinct colors:
  - `source:*` blue, `after/before:*` green, `unread/read/saved` yellow.
- `Tab` focuses chips; underline shows selection.
- `Backspace/Delete/X` removes selected chip and immediately refreshes results.
- Chip bar consumes exactly 1 terminal row; `contentHeight` decreases by 1 when visible.
- Overflow shows `…` and remains navigable via scrolling offset.

This plan preserves your existing parser/search pipeline, adds a single focused interaction mode, and keeps layout math explicit so the chip bar is always exactly one line.

### 8d. Key Bindings

### ANALYZE
Both responses agree on the core UX: `Ctrl-F` opens a filter picker UI state; selecting an option inserts a token into the search input; while typing in `/` search mode the app should parse continuously and show a live preview of recognized filters; normal-mode single-key shortcuts toggle common filters.

Key gaps / issues:
- Response A explicitly *defers* normal-mode shortcuts, which violates Requirement #3.
- Response A’s inline “highlighting” is realistic (chips/hint line below input), but it underspecifies message types and pending-state behavior.
- Response B proposes chips and a solid picker, but:
  - It uses `is:unread` / `is:saved` tokens, which don’t match the example query language (`unread`, `saved`, `source:hn`, `after:7d`).
  - Its shortcut toggling is a naive substring replace (can delete parts of other tokens).
  - It doesn’t integrate `embeddingPending` / `rerankPending` into the state cascade as required.
- Both provide workable picker handlers and insertion logic; neither fully nails “complete key table across all modes + pending states + picker state + search state” and “Bubble Tea message types” in a structured way.

### SYNERGIZE
Complementary strengths to combine:
- From Response A: practical reality that `textinput.Model` can’t easily do per-token inline coloring; better to show a live parsed preview line/chips below the input (still satisfies “highlight differently” via chips).
- From Response B: better UX for the picker (j/k navigation, Enter/Esc), a clean `FilterOption` model, and a clearer state-machine priority concept (modal traps keys).
- Improve both by:
  - Matching the existing query language (`unread`, `saved`, `source:`, `type:`, `after:`, `before:`).
  - Making shortcuts robust by tokenizing (strings.Fields) or by parsing+rebuilding, not substring replacement.
  - Integrating pending states so `Ctrl-F`/shortcuts don’t interfere when embedding/rerank are in progress.
  - Adding explicit Bubble Tea message types/commands for picker open/close/apply and “apply query”.

### UNIFY
## Implementation Plan — Key Bindings: `Ctrl-F` Filter Picker, Inline Parsing Preview, Filter Shortcuts

### 0) Goals & guiding rules
- `Ctrl-F` opens a **filter picker** (a modal/overlay UI state). No conflict with existing `f` fetch binding.
- While `/` search mode is active, parse the query **on every edit** and show a **preview** of recognized filters (chips). **No filtering happens until Enter** (preview-only).
- In **normal mode**, provide **single-key toggles** for common boolean filters (e.g. `u` unread, `s` saved). These *do* apply immediately (they are “quick filters” outside of the `/` “draft then Enter” flow).
- `embeddingPending` / `rerankPending` must remain restrictive: do not open picker or accept shortcuts there.

---

## 1) UX Design

### 1.1 `Ctrl-F` Filter Picker
**UI**: a small overlay panel (centered, or anchored under the search bar) listing filter actions. Each option either:
- inserts a **prefix** for the user to fill (e.g. `source:`), with the cursor placed after `:`, or
- inserts a **complete token/preset** (e.g. `after:7d`, `unread`, `saved`).

Suggested menu (v1: single-level list with useful presets + prefixes):

- **Source…** → inserts `source:` (cursor after colon)
- **Type…** → inserts `type:` (cursor after colon)
- **After…** → inserts `after:` (cursor after colon)
- **Before…** → inserts `before:` (cursor after colon)
- **Unread** → inserts `unread`
- **Saved** → inserts `saved`
- Presets (optional, very useful):
  - `after:24h`, `after:7d`
  - `source:hn` (if stable in your app), etc.

**Controls**
- `j/k` or `↑/↓` move selection
- `Enter` select (inserts token, closes picker, returns focus to search input)
- `Esc` cancel (close picker, return to prior state)
- Optional: `1–9` quick select for first items

**Behavior**
- `Ctrl-F` works in **normal mode and search mode**.
- If opened from normal mode: activate search input behind it (so the user can keep typing after picking), but still treat the query as *draft* until Enter.

---

### 1.2 Inline Parsing Preview (Search mode only)
Because `textinput.Model` renders as a single string, inline per-token coloring is awkward without rewriting the input widget. Instead, show “highlighting” as **chips below the input**:

Example:
```
/ source:hn after:7d unread climate_
[ Src: hn ] [ After: 7d ] [ Unread ] [ "climate" ]
```

- Chips reflect **recognized filters** from the existing parser (8a).
- Free text keywords become a “text chip” (e.g. `"climate"`).
- If the parser can report errors/unknown tokens, show a dim/red chip like `[ ? badtoken ]` (optional).

**Important**: This preview is purely informational. Actual filtering still occurs only on `Enter` in search mode.

---

### 1.3 Normal-mode filter shortcuts
In **normal mode** (and only when not pending):
- `u` toggles `unread` quick filter
- `s` toggles `saved` quick filter

These apply immediately by updating the “active query” and re-running the filter (or refreshing the view). They should also update the search input’s stored value so pressing `/` shows the current active query.

---

## 2) State machine integration

### 2.1 New state
Add:
- `filterPickerActive bool`
- `filterCursor int`
- `filterOptions []FilterOption`
- `parsePreview ParsedPreview` (cached output for chips)

### 2.2 Priority order (Update cascade)
To respect the restrictive pending states, use this priority:

1. **Global keys** (always): `Ctrl-C` quit, `D` debug (if you want truly global)
2. **embeddingPending** handler (only Esc/Ctrl-C/D)
3. **rerankPending** handler (its limited set)
4. **filterPickerActive** handler (modal traps keys)
5. **searchActive** handler (text input, Enter/Esc/Ctrl-F)
6. **normal mode** handler (navigation, fetch/refresh/quit, shortcuts)

This keeps picker/search from “escaping” into the app while pending operations are active.

### 2.3 State transitions
- Normal → Search: `/`
- Normal → FilterPicker: `Ctrl-F` (also sets `searchActive=true`, focuses input)
- Search → FilterPicker: `Ctrl-F`
- FilterPicker → Search: `Enter` (apply token), or `Esc` (cancel)
- Search → Normal: `Enter` (apply query), `Esc` (cancel/clear draft)
- Normal shortcuts (`u`,`s`) do not enter search mode; they modify active query and refresh.

---

## 3) Complete key binding table (all modes)

| Mode | Key | Action |
|---|---|---|
| **Global (all modes)** | `Ctrl+C` | Quit |
|  | `D` | Toggle debug (if allowed globally) |
| **Embedding Pending** | `Esc` | Cancel embedding |
|  | (others) | ignored |
| **Rerank Pending** | `Esc` | Cancel rerank |
|  | `j/k` `↑/↓` | Navigate (per your existing behavior) |
| **Normal** | `/` | Enter search mode (focus input) |
|  | `Ctrl+F` | Open filter picker (also activates search input) |
|  | `u` | Toggle unread quick filter + refresh |
|  | `s` | Toggle saved quick filter + refresh |
|  | `j/k` `↑/↓` | Navigate items |
|  | `r` | Refresh |
|  | `f` | Fetch |
|  | `Esc` | Clear active query / reset filters (keep existing behavior) |
|  | `q` | Quit |
| **Search (`searchActive`)** | typing | Edit query (live preview updates) |
|  | `Enter` | Apply query (actual filtering) |
|  | `Esc` | Cancel/clear search input (per current behavior) |
|  | `Ctrl+F` | Open filter picker |
| **Filter Picker (`filterPickerActive`)** | `j/k` `↑/↓` | Move selection |
|  | `Enter` | Insert token into search input, close picker |
|  | `Esc` | Close picker (no change) |
|  | `1–9` | Optional: quick select |

---

## 4) Bubble Tea message types (for clean separation & testability)

```go
type OpenFilterPickerMsg struct{}
type CloseFilterPickerMsg struct{}
type FilterPickedMsg struct {
	Token        string // "source:" or "after:7d" or "unread"
	CursorAdjust int    // where to place cursor relative to end; e.g. 0 or -0; see helper below
}

type ApplyQueryMsg struct {
	Raw string
}

type ToggleQuickFilterMsg struct {
	Token string // "unread" or "saved"
}
```

Commands (optional but recommended):

```go
func openFilterPickerCmd() tea.Cmd {
	return func() tea.Msg { return OpenFilterPickerMsg{} }
}
func applyQueryCmd(raw string) tea.Cmd {
	return func() tea.Msg { return ApplyQueryMsg{Raw: raw} }
}
func toggleQuickFilterCmd(tok string) tea.Cmd {
	return func() tea.Msg { return ToggleQuickFilterMsg{Token: tok} }
}
```

You can also handle everything directly in `tea.KeyMsg`, but explicit msgs make it easier to unit test state transitions.

---

## 5) Go code skeleton (Model, Update, helpers, View)

### 5.1 Types and model fields

```go
type InsertKind int

const (
	InsertToken InsertKind = iota // insert full token like "after:7d" or "unread"
	InsertPrefix                  // insert prefix like "source:" and place cursor after ':'
)

type FilterOption struct {
	Title string
	Help  string
	Text  string     // inserted text
	Kind  InsertKind
}

type ParsedPreview struct {
	Filters  []string // already formatted labels like "Src: hn", "After: 7d", "Unread"
	Keywords string   // joined free text
	// Optional: Errors []string
}

type Model struct {
	// existing:
	searchActive bool
	searchInput  textinput.Model

	embeddingPending bool
	rerankPending    bool

	// new:
	filterPickerActive bool
	filterCursor       int
	filterOptions      []FilterOption

	parsePreview ParsedPreview

	// canonical “active query” applied to list (keep in sync with input)
	activeQuery string

	// ... existing fields for items, debug, etc.
}
```

### 5.2 Filter option initialization

```go
func defaultFilterOptions() []FilterOption {
	return []FilterOption{
		{Title: "Source…", Help: "Insert source:", Text: "source:", Kind: InsertPrefix},
		{Title: "Type…", Help: "Insert type:", Text: "type:", Kind: InsertPrefix},
		{Title: "After…", Help: "Insert after:", Text: "after:", Kind: InsertPrefix},
		{Title: "Before…", Help: "Insert before:", Text: "before:", Kind: InsertPrefix},
		{Title: "Unread", Help: "Only unread items", Text: "unread", Kind: InsertToken},
		{Title: "Saved", Help: "Only saved items", Text: "saved", Kind: InsertToken},

		// Presets (optional, but great UX):
		{Title: "After: 24h", Help: "Items from last day", Text: "after:24h", Kind: InsertToken},
		{Title: "After: 7d", Help: "Items from last week", Text: "after:7d", Kind: InsertToken},
	}
}
```

### 5.3 Update loop (state cascade)

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		// 1) Global
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if msg.String() == "D" { // keep your existing debug toggle semantics
			// toggle debug...
			return m, nil
		}

		// 2) Pending states must block picker/search
		if m.embeddingPending {
			return m.updateEmbeddingPending(msg)
		}
		if m.rerankPending {
			return m.updateRerankPending(msg)
		}

		// 3) Modal picker
		if m.filterPickerActive {
			return m.updateFilterPicker(msg)
		}

		// 4) Search
		if m.searchActive {
			return m.updateSearch(msg)
		}

		// 5) Normal
		return m.updateNormal(msg)

	case OpenFilterPickerMsg:
		m.filterPickerActive = true
		m.filterCursor = 0
		// ensure search is available after selection
		if !m.searchActive {
			m.searchActive = true
			m.searchInput.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case CloseFilterPickerMsg:
		m.filterPickerActive = false
		return m, nil

	case FilterPickedMsg:
		m.filterPickerActive = false
		m.searchActive = true
		m.searchInput.Focus()

		newVal, newCursor := insertTokenAtEnd(m.searchInput.Value(), msg.Token, msg.CursorAdjust)
		m.searchInput.SetValue(newVal)
		m.searchInput.SetCursor(newCursor)

		m.parsePreview = computePreview(newVal) // preview update
		return m, textinput.Blink

	case ToggleQuickFilterMsg:
		// toggle in activeQuery (not substring replace)
		m.activeQuery = toggleExactField(m.activeQuery, msg.Token)
		m.searchInput.SetValue(m.activeQuery)
		m.parsePreview = computePreview(m.activeQuery)
		return m, applyQueryCmd(m.activeQuery)

	case ApplyQueryMsg:
		m.activeQuery = msg.Raw
		m.searchInput.SetValue(m.activeQuery)
		m.parsePreview = computePreview(m.activeQuery)
		// call your existing filter/apply command here:
		return m, performSearchCmd(msg.Raw) // implement with your existing pipeline

	}

	return m, nil
}
```

### 5.4 Mode handlers

```go
func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "/":
		m.searchActive = true
		m.searchInput.Focus()
		// seed input with active query so user can edit
		m.searchInput.SetValue(m.activeQuery)
		m.searchInput.SetCursor(len(m.activeQuery))
		m.parsePreview = computePreview(m.searchInput.Value())
		return m, textinput.Blink

	case msg.Type == tea.KeyCtrlF:
		return m, openFilterPickerCmd()

	case msg.String() == "u":
		return m, toggleQuickFilterCmd("unread")

	case msg.String() == "s":
		return m, toggleQuickFilterCmd("saved")

	// keep existing normal keys: j/k nav, f fetch, r refresh, q quit, etc.
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlF:
		return m, openFilterPickerCmd()

	case msg.Type == tea.KeyEsc:
		m.searchActive = false
		m.searchInput.Blur()
		m.searchInput.SetValue("") // matches current behavior
		m.parsePreview = ParsedPreview{}
		return m, nil

	case msg.Type == tea.KeyEnter:
		m.searchActive = false
		m.searchInput.Blur()
		raw := strings.TrimSpace(m.searchInput.Value())
		return m, applyQueryCmd(raw)
	}

	// default: edit input
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.parsePreview = computePreview(m.searchInput.Value())
	return m, cmd
}

func (m Model) updateFilterPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.filterPickerActive = false
		return m, nil
	case tea.KeyUp:
		if m.filterCursor > 0 {
			m.filterCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.filterCursor < len(m.filterOptions)-1 {
			m.filterCursor++
		}
		return m, nil
	case tea.KeyEnter:
		opt := m.filterOptions[m.filterCursor]
		cursorAdjust := 0
		if opt.Kind == InsertPrefix {
			// cursor goes to end; token ends with ":" so user types value next
			cursorAdjust = 0
		}
		return m, func() tea.Msg {
			return FilterPickedMsg{Token: opt.Text, CursorAdjust: cursorAdjust}
		}
	}

	// support j/k too
	switch msg.String() {
	case "j":
		if m.filterCursor < len(m.filterOptions)-1 {
			m.filterCursor++
		}
	case "k":
		if m.filterCursor > 0 {
			m.filterCursor--
		}
	}
	return m, nil
}
```

### 5.5 Token insertion & robust toggling helpers

```go
func insertTokenAtEnd(current, tok string, cursorAdjust int) (string, int) {
	current = strings.TrimRight(current, " ")
	if current != "" {
		current += " "
	}
	// For prefixes like "source:" don't force trailing space (user types value).
	needsTrailingSpace := !(strings.HasSuffix(tok, ":"))
	out := current + tok
	if needsTrailingSpace {
		out += " "
	}
	// place cursor at end (or adjusted)
	cur := len(out) + cursorAdjust
	if cur < 0 {
		cur = 0
	}
	if cur > len(out) {
		cur = len(out)
	}
	return out, cur
}

func toggleExactField(raw, token string) string {
	fields := strings.Fields(raw)
	out := make([]string, 0, len(fields))
	found := false
	for _, f := range fields {
		if f == token {
			found = true
			continue
		}
		out = append(out, f)
	}
	if !found {
		out = append(out, token)
	}
	return strings.Join(out, " ")
}
```

### 5.6 Preview computation (reuses your parser)
Assuming your existing parser returns something like `SearchOptions` with recognized structured filters and leftover keywords:

```go
func computePreview(raw string) ParsedPreview {
	opts := parser.Parse(raw) // your existing 8a parser

	p := ParsedPreview{}

	// adapt these to your actual SearchOptions fields
	for _, src := range opts.Sources {
		p.Filters = append(p.Filters, "Src: "+src)
	}
	if opts.After != nil {
		p.Filters = append(p.Filters, "After: "+opts.After.String())
	}
	if opts.Before != nil {
		p.Filters = append(p.Filters, "Before: "+opts.Before.String())
	}
	if opts.UnreadOnly {
		p.Filters = append(p.Filters, "Unread")
	}
	if opts.SavedOnly {
		p.Filters = append(p.Filters, "Saved")
	}
	p.Keywords = strings.Join(opts.Keywords, " ")
	return p
}
```

### 5.7 View: search line + preview chips + picker overlay
- Render `searchInput.View()` as usual.
- Under it, render chips from `m.parsePreview`.
- If `filterPickerActive`, render picker overlay (lipgloss bordered box). Keep it simple:

```go
func (m Model) viewParseChips() string {
	if !m.searchActive {
		return ""
	}

	chip := lipgloss.NewStyle().Padding(0, 1).MarginRight(1).
		Background(lipgloss.Color("62")).Foreground(lipgloss.Color("255"))
	textChip := lipgloss.NewStyle().Padding(0, 1).MarginRight(1).
		Background(lipgloss.Color("237")).Foreground(lipgloss.Color("255"))

	var parts []string
	for _, f := range m.parsePreview.Filters {
		parts = append(parts, chip.Render(f))
	}
	if strings.TrimSpace(m.parsePreview.Keywords) != "" {
		parts = append(parts, textChip.Render(`"`+m.parsePreview.Keywords+`"`))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}
```

---

## 6) Notes / v2 improvements (optional)
- Add a second-stage picker for “Source…” that lists observed sources dynamically from loaded items.
- Add remove/edit chips with mouse or keyboard (more complex).
- Add `Tab` completion for prefixes (`source:`) inside search input.

This plan satisfies all deliverables: picker UX + insertion behavior, live parse preview chips (highlighting without rewriting `textinput`), normal-mode shortcut toggles, state-machine integration with pending states, a complete key table, and explicit Bubble Tea message types/commands for picker/query actions.

---

## Feature 9: Pinned Searches as Views

### 9a. Schema

### ANALYZE
- Both responses agree on adding two new tables (`search_history`, `search_results`), using additive/idempotent migrations (`CREATE TABLE IF NOT EXISTS`), and storing the raw query string plus a normalized form for deduplication. Both also align on using the existing embedding encoder/decoder for a 1024-dim `float32` (4KB) BLOB.
- Both propose the core store methods: `SaveSearch`, `GetPinnedSearches`, `TogglePinned`, `GetSearchHistory`, `GetSearchResults` (and both implicitly need a “save results” helper even if not listed).
- Key differences / issues:
  - One response adds `last_used_at`, which is useful but **not in the required SEARCH.md spec**; adding extra columns is safe but the deliverable asked for the spec columns explicitly.
  - One response makes `query_embedding BLOB NOT NULL`, but the spec doesn’t require NOT NULL; in practice embeddings may be unavailable (e.g., backend disabled), so **nullable is safer**.
  - One response assumes `items.id` is `INTEGER` and sets `item_id INTEGER`; the other uses `TEXT`. Since the existing `items` schema isn’t shown, **`item_id` must match the actual type of `items.id`** (or at least be compatible).
  - Deduplication: one response handles dedup in app logic; the other uses a `UNIQUE(query_norm, backend)` constraint. The unique constraint + UPSERT is more robust and simpler.
  - Some transactional/migration snippets are incomplete (e.g., showing a `BeginTx` call embedded into a migration call without proper error handling/commit).

### SYNERGIZE
- Combine:
  - The stricter data-integrity approach (unique constraint + UPSERT; FK constraints; cascade delete of results when a search is deleted).
  - The better UX-focused reasoning on storing raw `query_text` (not a parsed struct) and re-parsing at runtime for pinned refresh.
  - The more complete indexing suggestions (pinned lookup, normalized lookup, results lookup).
- Improve by:
  - Keeping the schema exactly to the required columns (no extra `last_used_at`) while still achieving “recently used” via `created_at` and `use_count` (or optionally proposing `last_used_at` as a *future additive* column).
  - Writing migrations as an idempotent “create new tables + indexes” step that fits the existing `createTables()` style and can also be called as a separate `migrateSearchTables()` inside a transaction.
  - Making `item_id` type explicitly “match `items.id`” in the plan.

### UNIFY
## Implementation Plan: Pinned Searches (`is_pinned`) + `GetPinnedSearches()`

### 0) Goals & Behavior
- Every executed search can be persisted into `search_history` with its raw text, normalized text, embedding blob (1024×float32), backend used, duration, pin state, and use count.
- Pinned searches are “persistent views” (tabs). On each fetch cycle, the UI loads pinned searches from the DB, re-parses `query_text` into current `SearchOptions`, reruns the search, and (optionally) refreshes `search_results` for that `search_id`.

---

## 1) SQL Schema (complete, per spec)

### `search_history`
Columns required by spec: `id, query_text, query_norm, query_embedding, created_at, backend, duration_ms, is_pinned, use_count`.

```sql
CREATE TABLE IF NOT EXISTS search_history (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    query_text      TEXT    NOT NULL,                            -- raw user input
    query_norm      TEXT    NOT NULL,                            -- lower+trim for dedup
    query_embedding BLOB,                                        -- 1024-dim float32 (4KB); nullable
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    backend         TEXT    NOT NULL,                            -- e.g. "jina", "ollama", "cosine-only"
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    is_pinned       INTEGER NOT NULL DEFAULT 0,                  -- 0/1 (SQLite boolean)
    use_count       INTEGER NOT NULL DEFAULT 0,

    UNIQUE (query_norm, backend)                                 -- exact dedup per backend
);

-- Helpful indexes
CREATE INDEX IF NOT EXISTS idx_search_history_pinned
    ON search_history(is_pinned, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_search_history_norm_backend
    ON search_history(query_norm, backend);

CREATE INDEX IF NOT EXISTS idx_search_history_created_at
    ON search_history(created_at DESC);
```

### `search_results`
Spec columns: `search_id, rank, item_id, cosine_score, rerank_score`.

**Important:** `item_id` type should match your existing `items(id)` type. If `items.id` is `TEXT`, make `item_id TEXT`; if it’s `INTEGER`, make `item_id INTEGER`.

```sql
CREATE TABLE IF NOT EXISTS search_results (
    search_id     INTEGER NOT NULL,
    rank          INTEGER NOT NULL,
    item_id       TEXT    NOT NULL,         -- adjust to INTEGER if items.id is INTEGER
    cosine_score  REAL,
    rerank_score  REAL,

    PRIMARY KEY (search_id, rank),
    FOREIGN KEY (search_id) REFERENCES search_history(id) ON DELETE CASCADE,
    FOREIGN KEY (item_id)   REFERENCES items(id)
);

CREATE INDEX IF NOT EXISTS idx_search_results_search
    ON search_results(search_id, rank);
```

Notes:
- `ON DELETE CASCADE` on `search_id` is desirable so deleting a search cleans up its cached results.
- Whether you want cascading from `items` deletions is a product choice; the schema above does **not** cascade on `item_id` by default.

---

## 2) Migration Strategy (idempotent, additive, fits existing pattern)

### Option A (simplest): add to existing `createTables()`
If `createTables()` is already called on startup, append the new `CREATE TABLE IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS` statements to it. This is fully idempotent and safe on existing DBs.

### Option B (explicit additive migration step): `migrateSearchTables(tx)`
Pattern matches your existing `migrateEmbeddings()` approach and keeps concerns separated.

```go
func (s *Store) migrateSearchTables(ctx context.Context, tx *sql.Tx) error {
	const schema = `
		-- search_history + indexes
		CREATE TABLE IF NOT EXISTS search_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			query_text TEXT NOT NULL,
			query_norm TEXT NOT NULL,
			query_embedding BLOB,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			backend TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			is_pinned INTEGER NOT NULL DEFAULT 0,
			use_count INTEGER NOT NULL DEFAULT 0,
			UNIQUE (query_norm, backend)
		);

		CREATE INDEX IF NOT EXISTS idx_search_history_pinned
			ON search_history(is_pinned, created_at DESC);

		CREATE INDEX IF NOT EXISTS idx_search_history_norm_backend
			ON search_history(query_norm, backend);

		CREATE INDEX IF NOT EXISTS idx_search_history_created_at
			ON search_history(created_at DESC);

		-- search_results + indexes
		CREATE TABLE IF NOT EXISTS search_results (
			search_id INTEGER NOT NULL,
			rank INTEGER NOT NULL,
			item_id TEXT NOT NULL,
			cosine_score REAL,
			rerank_score REAL,
			PRIMARY KEY (search_id, rank),
			FOREIGN KEY (search_id) REFERENCES search_history(id) ON DELETE CASCADE,
			FOREIGN KEY (item_id) REFERENCES items(id)
		);

		CREATE INDEX IF NOT EXISTS idx_search_results_search
			ON search_results(search_id, rank);
	`
	_, err := tx.ExecContext(ctx, schema)
	return err
}
```

Call it during store initialization inside the same transaction you use for other migrations:

```go
tx, err := s.db.BeginTx(ctx, nil)
if err != nil { ... }
defer tx.Rollback()

if err := s.migrateEmbeddings(ctx, tx); err != nil { ... }
if err := s.migrateSearchTables(ctx, tx); err != nil { ... }

if err := tx.Commit(); err != nil { ... }
```

Also ensure `PRAGMA foreign_keys = ON;` is enabled once per connection (often done at open).

---

## 3) Go Struct Definitions

```go
type SearchRecord struct {
	ID             int64
	QueryText      string
	QueryNorm      string
	QueryEmbedding []float32 // nil if NULL in DB

	CreatedAt   time.Time
	Backend     string
	DurationMS  int
	IsPinned    bool
	UseCount    int
}

type SearchResult struct {
	SearchID    int64
	Rank        int
	ItemID      string // match items.id type if not string
	CosineScore float64
	RerankScore *float64 // nullable
}
```

Embedding BLOB mapping:
- On write: `encodeEmbedding([]float32)` (must be length 1024)
- On read: `decodeEmbedding([]byte)` (if blob is non-NULL)

---

## 4) Store Methods: Signatures + SQL

All methods should follow your existing concurrency pattern (`s.mu` + WAL). Use `RLock` for pure reads and `Lock` for writes; use a transaction when updating multiple tables.

### Normalization helper
Pinned searches rely on exact dedup:
```go
func normalizeQuery(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
```

### `SaveSearch` (UPSERT + increment use_count)
Behavior:
- Insert a new record for `(query_norm, backend)` if missing.
- If it exists, increment `use_count` and update fields (without clobbering an existing embedding with NULL).

```go
func (s *Store) SaveSearch(ctx context.Context, rec *SearchRecord) (int64, error)
```

SQL (recommended; uses UNIQUE + UPSERT). Use `RETURNING id` if your SQLite build supports it (modernc generally does); otherwise do a follow-up `SELECT id`.

```sql
INSERT INTO search_history (
  query_text, query_norm, query_embedding, backend, duration_ms, is_pinned, use_count
) VALUES (?, ?, ?, ?, ?, COALESCE(?, 0), 1)
ON CONFLICT(query_norm, backend) DO UPDATE SET
  query_text      = excluded.query_text,
  query_embedding = COALESCE(excluded.query_embedding, search_history.query_embedding),
  duration_ms     = excluded.duration_ms,
  is_pinned       = search_history.is_pinned,         -- keep pin state on reuse
  use_count       = search_history.use_count + 1
RETURNING id;
```

Notes:
- `rec.QueryEmbedding == nil` should bind NULL; non-nil should bind `encodeEmbedding(rec.QueryEmbedding)`.
- Validate embedding length when non-nil: must be 1024.

### `GetPinnedSearches()`
```go
func (s *Store) GetPinnedSearches(ctx context.Context) ([]SearchRecord, error)
```

SQL:
```sql
SELECT id, query_text, query_norm, query_embedding, created_at, backend, duration_ms, is_pinned, use_count
FROM search_history
WHERE is_pinned = 1
ORDER BY created_at DESC, id DESC;
```

### `TogglePinned(searchID)`
```go
func (s *Store) TogglePinned(ctx context.Context, searchID int64) error
```

SQL:
```sql
UPDATE search_history
SET is_pinned = CASE WHEN is_pinned = 1 THEN 0 ELSE 1 END
WHERE id = ?;
```

(Alternative: `SET is_pinned = NOT is_pinned` also works if stored as 0/1.)

### `GetSearchHistory(limit)`
```go
func (s *Store) GetSearchHistory(ctx context.Context, limit int) ([]SearchRecord, error)
```

SQL:
```sql
SELECT id, query_text, query_norm, query_embedding, created_at, backend, duration_ms, is_pinned, use_count
FROM search_history
ORDER BY created_at DESC, id DESC
LIMIT ?;
```

### `GetSearchResults(searchID)`
```go
func (s *Store) GetSearchResults(ctx context.Context, searchID int64) ([]SearchResult, error)
```

SQL:
```sql
SELECT search_id, rank, item_id, cosine_score, rerank_score
FROM search_results
WHERE search_id = ?
ORDER BY rank ASC;
```

### (Needed helper) `SaveSearchResults(searchID, results)`
Not explicitly listed in requirements, but necessary to populate `search_results`.

```go
func (s *Store) SaveSearchResults(ctx context.Context, searchID int64, results []SearchResult) error
```

Implementation approach:
- In a tx: `DELETE FROM search_results WHERE search_id=?`
- Insert each row with rank, item_id, scores (use prepared statement / batch).

---

## 5) Relationship to `SearchOptions`: what is stored?
Store **raw `query_text`** + `backend` (and derived `query_norm` + optional `query_embedding`). Do **not** store a parsed `SearchOptions` struct in the DB.

Rationale:
- Query language/flags can evolve; storing structs tightly couples DB format to parser version.
- Raw text is what the user expects to see in a pinned “tab”.
- On auto-refresh: load `query_text` → parse using current parser → produce `SearchOptions` → execute search.

If you later need full fidelity for non-text options (filters, sort, limit), add an additive column like `options_json BLOB` (future migration).

---

## 6) Test Cases (CRUD + migration)

Use an isolated SQLite DB (temp file recommended for WAL; `:memory:` can be tricky with multiple connections). Ensure migrations run on setup.

1. **Migration idempotence**
   - Run `createTables()`/`migrateSearchTables()` twice; expect no error.
   - Verify both tables and indexes exist (optional: query `sqlite_master`).

2. **SaveSearch inserts**
   - Save a search with query text `"AI News"` → expect one row, `use_count=1`, `is_pinned=false`, `query_norm="ai news"`.

3. **SaveSearch dedup + increments**
   - Save again with same `query_norm` and `backend`.
   - Expect same `id`, `use_count=2`.
   - Ensure `is_pinned` is preserved.

4. **TogglePinned + GetPinnedSearches**
   - Toggle pinned on saved record.
   - `GetPinnedSearches()` returns exactly that record with `IsPinned=true`.
   - Toggle again; pinned list becomes empty.

5. **GetSearchHistory ordering + limit**
   - Insert multiple distinct searches; verify `GetSearchHistory(limit)` returns newest first and respects limit.

6. **SaveSearchResults + GetSearchResults**
   - Save results ranks 1..N.
   - Fetch and confirm ordering by rank and scores scanned correctly (including NULL rerank).

7. **FK cascade on search deletion (optional but recommended)**
   - If you add a `DeleteSearch(searchID)` method: after deleting from `search_history`, confirm `search_results` rows for that `search_id` are gone due to `ON DELETE CASCADE`.

This plan stays within the required SEARCH.md schema, uses robust deduplication via a unique constraint + UPSERT, keeps migrations additive and idempotent, and cleanly supports pinned searches as persistent auto-refreshing views.

### 9b. Tab-Like Navigation

# Claude's Analysis: Tab-Like Navigation (9b)

## Tab Data Model

```go
// TabView represents a single view (tab) in the UI.
type TabView struct {
    Label         string           // display label for tab bar (e.g., "All", "Physics", "source:hn")
    SearchID      int64            // 0 for the "All" tab; search_history.id for pinned
    QueryText     string           // "" for All tab; original query for pinned
    Items         []store.Item     // cached items for this view
    Embeddings    map[string][]float32
    Cursor        int
    QueryEmbedding []float32       // for re-ranking on refresh
    LastRefreshed time.Time
    NeedsRefresh  bool             // true when coordinator fetched new items
}
```

## App Struct Changes

```go
type App struct {
    // ... existing fields ...

    // Tab system
    tabs       []TabView // tabs[0] is always "All"; tabs[1:] are pinned searches
    activeTab  int       // index into tabs
}
```

The existing `items`, `cursor`, `embeddings` fields become accessors into the active tab:

```go
func (a *App) currentTab() *TabView {
    return &a.tabs[a.activeTab]
}
```

**Migration strategy**: Initially, `tabs` has one entry (the "All" view). Existing code that reads `a.items` reads `a.currentTab().Items`. This is a mechanical refactor — every `a.items` becomes `a.tabs[a.activeTab].Items`.

## Tab Bar Rendering

```go
// Tab bar styles
var (
    ActiveTab = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("255")).
        Background(lipgloss.Color("62")).  // Purple
        Padding(0, 1)

    InactiveTab = lipgloss.NewStyle().
        Foreground(lipgloss.Color("252")).
        Background(lipgloss.Color("238")).
        Padding(0, 1)

    TabSeparator = lipgloss.NewStyle().
        Foreground(lipgloss.Color("240")).
        SetString(" | ")
)

func RenderTabBar(tabs []TabView, activeIndex int, width int) string {
    var parts []string
    totalWidth := 0

    for i, tab := range tabs {
        label := tab.Label
        if len(label) > 15 {
            label = label[:12] + "..."
        }

        var styled string
        if i == activeIndex {
            styled = ActiveTab.Render(label)
        } else {
            styled = InactiveTab.Render(label)
        }

        tabWidth := lipgloss.Width(styled)
        if totalWidth + tabWidth + 3 > width {
            parts = append(parts, InactiveTab.Render("..."))
            break
        }
        parts = append(parts, styled)
        totalWidth += tabWidth + 3
    }

    bar := strings.Join(parts, TabSeparator.String())
    padding := width - lipgloss.Width(bar)
    if padding > 0 {
        bar += strings.Repeat(" ", padding)
    }
    return StatusBar.Width(width).Render(bar)
}
```

## View() Integration

Tab bar sits at the very top, above the item stream:

```go
func (a App) View() string {
    // Debug overlay: full takeover
    if a.debugVisible && a.ring != nil { ... }

    contentHeight := a.height - 1  // status bar

    // Tab bar (only if >1 tab)
    tabBar := ""
    if len(a.tabs) > 1 {
        tabBar = RenderTabBar(a.tabs, a.activeTab, a.width) + "\n"
        contentHeight--
    }

    // ... existing height adjustments for error/search bars ...

    stream := RenderStream(a.currentTab().Items, a.currentTab().Cursor, a.width, contentHeight, a.activeTab == 0)

    return tabBar + stream + errorBar + chipsBar + searchBar + statusBar
}
```

## Key Bindings

| Key | Action |
|-----|--------|
| `Tab` | Next tab |
| `Shift-Tab` | Previous tab |
| `1-9` | Jump to tab by index (only when >1 tab) |
| `p` | Pin current search as new tab (only when search results showing) |
| `P` | Unpin current tab (only when on a pinned tab) |

```go
// In handleKeyMsg normal mode:
case "p":
    if a.hasQuery() {
        return a.pinCurrentSearch()
    }
case "P":
    if a.activeTab > 0 {  // Can't unpin "All"
        return a.unpinCurrentTab()
    }

// Tab navigation
if msg.Type == tea.KeyTab {
    return a.nextTab()
}
if msg.Type == tea.KeyShiftTab {
    return a.prevTab()
}
```

## Tab Switching Logic

```go
func (a App) nextTab() (tea.Model, tea.Cmd) {
    a.activeTab = (a.activeTab + 1) % len(a.tabs)
    // Clear any active search state
    a.searchActive = false
    a.embeddingPending = false
    a.rerankPending = false
    a.statusText = ""
    return a, nil
}

func (a App) pinCurrentSearch() (tea.Model, tea.Cmd) {
    if !a.hasQuery() { return a, nil }

    // Create tab from current search state
    tab := TabView{
        Label:          truncateRunes(a.filterInput.Value(), 15),
        QueryText:      a.filterInput.Value(),
        Items:          make([]store.Item, len(a.items)),
        Cursor:         0,
        QueryEmbedding: a.queryEmbedding,
        LastRefreshed:  time.Now(),
    }
    copy(tab.Items, a.items)

    a.tabs = append(a.tabs, tab)
    a.activeTab = len(a.tabs) - 1

    // Clear search state — we're now on the pinned tab
    a.clearSearchState()

    // Persist to DB (fire-and-forget cmd)
    return a, a.persistPinnedSearch(tab)
}
```

## Memory Considerations

Each tab holds a full item slice. With 5000 items at ~500 bytes each, that's ~2.5MB per tab. 10 pinned tabs = 25MB — acceptable for a TUI. Embeddings are shared (pointer to same map when possible) or lazily loaded.

Reasonable limit: cap at 10 pinned tabs. Show warning if user tries to pin more.

### 9c. Auto-Refresh

### ANALYZE
Both responses agree on the core Bubble Tea pattern: treat pinned-tab refreshes as asynchronous work that returns `tea.Msg` back into `Update`, so the UI remains responsive. Both also propose new message types (`Started`/`Complete` or `Refreshed`) and updating tab state (`NeedsRefresh`, `LastRefreshed`, optional `IsRefreshing`).

Key differences / issues:
- **Where the trigger/state is set:** Both place “mark tabs as needing refresh” in the UI’s `FetchComplete` handler, while the requirement text says “Coordinator sets `NeedsRefresh=true`”. In practice the coordinator can’t mutate UI tab state directly; it must signal the UI, which then mutates state. The plan should clarify this as “coordinator triggers, UI sets”.
- **Concurrency/rate limiting strategy:**
  - One response uses **sequential chaining** (refresh next tab after previous completes). This is simple and avoids SQLite contention.
  - The other batches refresh commands for all tabs immediately but gates execution via a **semaphore**. This can still spawn many goroutines that block on the semaphore, which is usually fine but unnecessary; a queue/worker approach is cleaner.
- **Fetch/Embed ordering problem:** The project context says `FetchComplete` is sent *before* `embedNewItems()`. If pinned refresh does cosine rerank using embeddings, it may run before embeddings exist for the new items. Neither response fully resolves this; a robust plan must either (a) reorder coordinator steps or (b) add an “embeddings ready” message and trigger rerank after that.
- **Implementation correctness details:**
  - Any closure that references loop indices should capture the index into a local variable to avoid classic Go closure pitfalls.
  - Use stable identifiers (`TabID`) and generation counters to ignore stale/out-of-order refresh results (helpful if a user edits/deletes tabs mid-refresh).

### SYNERGIZE
Complementary strengths to combine:
- The sequential “chain” approach is very clean and naturally rate-limited; the semaphore approach highlights configurable concurrency and avoids “one slow tab blocks all”.
- The second response adds useful operational safeguards: tab deletion during refresh, potential SQLite busy mitigation, and caching query embeddings.
- The first response’s “refresh manager” idea (refresh-next pattern) can be upgraded into a **proper refresh queue with limited worker count**, giving the best of both worlds: bounded concurrency without spawning N blocked goroutines.
- Both mention skipping cross-encoder rerank for background refresh; the unified plan should formalize a **policy**: background refresh = SQL + optional cosine; cross-encoder only when user opens the tab or explicitly requests.

### UNIFY
## Implementation Plan: Auto-Refresh Pinned Searches on Fetch Cycle

### 0) High-level flow
1. Coordinator finishes a fetch cycle and determines `NewItems > 0`.
2. After new items are **saved and embedded** (or after an “embeddings complete” signal), the UI receives a message indicating new data is available.
3. UI marks all pinned tabs `NeedsRefresh=true`, increments a refresh epoch, and starts a background refresh manager.
4. Refresh manager re-runs pinned searches asynchronously with bounded concurrency and sends per-tab completion messages back to the UI.
5. UI updates each tab’s cached `Items`; if the user is currently viewing that tab, the list updates (with safeguards to avoid jarring UX while typing/scrolling).

---

## 1) Message types (Deliverable #1)

### Coordinator → UI
Keep the existing `FetchComplete` but ensure it represents “data is ready to query”. Two options:

**Option A (preferred): reorder coordinator so embeddings are ready before `FetchComplete`:**
- `fetchAll()` does: `provider.Fetch()` → `store.SaveItems()` → `embedNewItems()` → `program.Send(FetchComplete{NewItems: n})`

Then no new message type is required.

**Option B: add a second message if you can’t reorder:**
```go
type FetchCompleteMsg struct {
    NewItems int
}

type EmbedCompleteMsg struct {
    EmbeddedItems int
    // could include a FetchCycleID/epoch if needed
}
```
UI triggers pinned refresh on `EmbedCompleteMsg` (or does a SQL-only refresh on `FetchCompleteMsg` and cosine rerank later on `EmbedCompleteMsg`).

### UI internal messages (refresh lifecycle)
Use stable IDs + an epoch/generation to discard stale results:

```go
type PinnedRefreshEnqueueMsg struct {
    Epoch int64
}

type PinnedRefreshStartedMsg struct {
    TabID string
    Epoch int64
}

type PinnedRefreshFinishedMsg struct {
    TabID  string
    Epoch  int64
    Items  []store.Item // or []ItemSummary for lighter cache
    Err    error
    // Optional: metrics (duration, counts), debug fields, etc.
}
```

---

## 2) Where the refresh logic lives (Deliverable #2)

**Put refresh execution in the UI layer as `tea.Cmd` work**, using injected dependencies:
- UI already owns tab state and can safely apply results.
- Coordinator remains a data pipeline (fetch/save/embed/send signals).
- UI uses the same store/query machinery as interactive search, but with background policies (skip heavy reranks).

**Integration point:** the app model should be constructed with functions/interfaces like:
- `SearchPinned(ctx, queryText, queryEmbedding, opts) ([]Item, error)`
- `EmbedQuery(ctx, text) ([]float32, error)` (only if query embedding missing)
- optional `CrossEncoderRerank(...)`

---

## 3) Tab state changes (stale detection + UX state)
Extend your pinned tab struct:

```go
type Tab struct {
    ID           string
    Title        string
    QueryText    string
    Pinned       bool

    Items        []store.Item
    QueryEmbedding []float32

    LastRefreshed time.Time
    NeedsRefresh  bool
    IsRefreshing  bool

    // Guards against out-of-order updates
    RefreshEpoch  int64

    // Optional UX polish
    HasNewResults bool // e.g., if user is actively interacting, don’t hot-swap list
}
```

---

## 4) Efficient pinned search rerun (Deliverable #3)

Implement a single “pinned refresh search” function that composes the pipeline:

### Pipeline policy
- **Always:** (a) SQL filter query
- **Usually:** (b) cosine rerank (if embeddings available + tab has `QueryEmbedding`)
- **Background default:** skip (c) cross-encoder
- **When user views tab (foreground):** optionally run cross-encoder as an additional refinement step (on-demand)

### Suggested store-side API
Keep SQL efficient and avoid loading huge payloads:
- SQL step returns a bounded candidate set (IDs + light fields) e.g. 500–2000.
- If cosine rerank is enabled, fetch embeddings for those IDs in one query.
- Rerank in-memory, then trim to display limit.

Pseudo:
```go
func (s *Store) SearchPinned(
    ctx context.Context,
    queryText string,
    queryEmbedding []float32,
    opts SearchOpts, // includes limits + flags: Cosine, CrossEncoder
) ([]store.Item, error) {
    candidates, err := s.SQLFilter(ctx, queryText, opts.SQLLimit)
    if err != nil { return nil, err }

    if opts.Cosine && len(queryEmbedding) > 0 {
        embs, err := s.LoadEmbeddings(ctx, ids(candidates))
        if err == nil {
            candidates = CosineRerank(candidates, embs, queryEmbedding)
        }
        // if embeddings missing, keep SQL order (don’t fail refresh)
    }

    if opts.CrossEncoder {
        candidates = CrossEncoderRerank(ctx, queryText, candidates)
    }

    return candidates[:min(len(candidates), opts.DisplayLimit)], nil
}
```

**Important:** If you keep the current coordinator order (FetchComplete before embeddings), then background refresh should either:
- run `opts.Cosine=false` until embeddings are ready, or
- tolerate missing embeddings and rerank only the subset with vectors.

---

## 5) Concurrency / rate limiting (Deliverable #4)

### Recommended: refresh queue + bounded workers (no “N blocked goroutines”)
Create a single `tea.Cmd` that starts a manager goroutine which:
- builds a queue of pinned TabIDs needing refresh
- runs up to `N` concurrent refresh workers (e.g., 2–3)
- sends `PinnedRefreshStartedMsg` / `PinnedRefreshFinishedMsg` per tab via `Program.Send` **or** returns them as messages if implemented as chained cmds

In Bubble Tea, the simplest pattern is: **enqueue** in the model, then issue **one command per tab** but only when capacity allows (a “pump”). Concretely:

Model fields:
```go
type Model struct {
    // ...
    refreshEpoch int64
    refreshQueue []string      // TabIDs
    refreshInFlight int        // count
    refreshMaxConcurrent int   // e.g. 2 or 3
}
```

Commands:
- `startOrPumpPinnedRefreshCmd()` – schedules as many refresh jobs as capacity allows.

Pseudo Update logic:
1. On trigger, fill `refreshQueue` with pinned tabs, set `NeedsRefresh=true`.
2. Call `pump()` which starts up to `refreshMaxConcurrent` jobs.
3. Each `PinnedRefreshFinishedMsg` decrements `refreshInFlight`, clears tab flags, and calls `pump()` again.

This yields:
- bounded concurrency
- no extra goroutines sitting blocked
- easy to cancel/ignore stale work via `Epoch`

### Epoch (generation) handling
Increment `refreshEpoch` on every fetch-trigger event. Each refresh job carries the epoch; when results return, ignore if `msg.Epoch != tab.RefreshEpoch` or the tab no longer exists.

---

## 6) Integration with existing `FetchComplete` handling (Deliverable #5)

In the UI `Update`:

```go
case FetchCompleteMsg:
    cmds := []tea.Cmd{ m.loadItemsCmd() } // existing behavior for current view

    if msg.NewItems > 0 {
        // If embeddings are already ready at this point:
        m.refreshEpoch++
        epoch := m.refreshEpoch

        for i := range m.tabs {
            if m.tabs[i].Pinned {
                m.tabs[i].NeedsRefresh = true
                m.tabs[i].IsRefreshing = true
                m.tabs[i].RefreshEpoch = epoch
                m.refreshQueue = append(m.refreshQueue, m.tabs[i].ID)
            }
        }
        cmds = append(cmds, m.pumpPinnedRefreshCmd(epoch))
    }

    return m, tea.Batch(cmds...)
```

If you adopt the two-message approach, do the queueing/pumping on `EmbedCompleteMsg` instead (or do SQL-only on `FetchCompleteMsg` and cosine rerank later).

---

## 7) Handling “user is actively searching” (Deliverable #6)

Define “active searching” as: user is typing in a query box, interactive search command in-flight, or list cursor/viewport is being manipulated in a way where replacing items would be disruptive.

Policy:
- **Always refresh pinned tab caches in background** (it doesn’t block UI).
- **Only hot-swap the visible list** when:
  - the user is currently on that pinned tab **and**
  - they are not actively editing/searching **or** you can preserve cursor/selection reliably.

Implementation options (choose one for v1 vs v2):
- **V1 (simple):** If active tab matches, update list immediately; attempt to preserve cursor by remembering selected item ID and reselecting if present.
- **V2 (less jarring):** If active tab matches but user is “busy” (typing/scrolling), set `tab.HasNewResults=true` and show a small indicator (“Updated results available; press r to apply”). Applying swaps in `tab.Items`.

Pseudo on finish:
```go
case PinnedRefreshFinishedMsg:
    tab := findTabByID(msg.TabID)
    if tab == nil || msg.Epoch != tab.RefreshEpoch { return m, nil }

    tab.IsRefreshing = false
    tab.NeedsRefresh = false
    tab.LastRefreshed = time.Now()

    if msg.Err == nil {
        tab.Items = msg.Items
        if m.activeTabID == tab.ID {
            if m.userIsActivelySearchingOrTyping() {
                tab.HasNewResults = true
            } else {
                m.applyTabItemsToListPreservingSelection(tab.Items)
                tab.HasNewResults = false
            }
        }
    }

    m.refreshInFlight--
    return m, m.pumpPinnedRefreshCmd(m.refreshEpoch)
```

---

## Additional safeguards (recommended)
- **Tab deletion/edit during refresh:** use `TabID` lookup; ignore if missing. If query text changes, clear `QueryEmbedding` and bump `RefreshEpoch` to invalidate in-flight refresh results.
- **SQLite busy resilience:** keep refresh concurrency low (2–3). Ensure fetch writes are committed before triggering refresh (another reason to reorder embed+send).
- **Background cross-encoder:** disabled by default; run when tab becomes active or on explicit “refine” action.

---

## Summary of concrete changes
1. **Coordinator:** ensure `FetchComplete` is sent only after items (and ideally embeddings) are ready, or add `EmbedCompleteMsg`.
2. **UI model:** add pinned refresh fields (`NeedsRefresh`, `IsRefreshing`, `LastRefreshed`, `RefreshEpoch`) + a refresh queue manager (`refreshQueue`, `refreshInFlight`, `refreshMaxConcurrent`).
3. **Messages:** add `PinnedRefreshStartedMsg` / `PinnedRefreshFinishedMsg` (+ epoch).
4. **Commands:** implement `pumpPinnedRefreshCmd(epoch)` + `refreshOnePinnedTabCmd(tabID, epoch)`.
5. **Search pipeline:** implement `Store.SearchPinned` with SQL filter + optional cosine; cross-encoder only on-demand.
6. **UX behavior:** update active tab view immediately only when safe; otherwise set “new results” indicator.

This satisfies the trigger requirement, keeps UI non-blocking, provides stale detection, follows the refresh pipeline with optional steps, updates cached tab results, rate-limits refreshes, integrates cleanly with `FetchComplete`, and avoids disrupting an active user search.

### 9d. UI Integration

### ANALYZE
- Both proposals converge on the same core UI decision: a **1-row horizontal tab bar at the top** is the best fit for 80–200 col TUIs because it preserves the full content width; a left sidebar would permanently steal columns and degrade the item stream.
- Both cover: active tab highlighting, truncated labels, a “new items” count, status-bar key hints for unpin, and a centered empty state.
- Key issues to fix:
  - **String truncation**: both drafts use `len(s)` in places where **rune-safe** truncation is needed; otherwise UTF-8 labels can break.
  - **Width math with ANSI styling**: using `len(badge)` is wrong once a string is lipgloss-styled; must use `lipgloss.Width`.
  - **Overflow behavior**: one approach truncates/ellipsis but doesn’t guarantee the **active tab stays visible** when many tabs exist.
  - **New-item counting**: one approach compares IDs (robust), another uses timestamps (simpler but can miscount on reorder/clock skew). Best is: increment unseen on ingestion using query-match, with optional ID dedupe.

### SYNERGIZE
- Keep the shared decision: **top tab bar** integrated above the item stream.
- Combine strengths:
  - Use **styles aligned to existing palette** (bg=236, active purple=62).
  - Use a **windowed tab strip** so the active tab is always visible, with leading/trailing ellipses when overflow occurs.
  - Use **rune-safe truncation** and **lipgloss.Width-based** layout calculations.
  - Implement new-item badges via **increment-on-new-item** (best for a live stream), with an optional **ID dedupe** hook if the pipeline can re-emit items.

### UNIFY
## 1) Final layout decision (with rationale)
**Choose: Horizontal tab bar, 1 row, at the top (above item stream).**

**Why this is best for Observer:**
- Preserves full-width item stream (critical at 80 cols); sidebar would permanently reduce readability.
- Costs 1 row, which is acceptable in a 24–50 row terminal.
- Matches familiar TUI patterns (lazygit/tmux-like tabs).
- Easy to render and reason about in Bubble Tea + lipgloss (single line, predictable height).

### Final vertical stack (top → bottom)
1. **Tab bar** (1 row) — shown only when there is at least 1 pinned view (so there is something to navigate to).
2. **Item stream viewport** (fills remaining height)
3. **Error bar** (0/1 row)
4. **Search/filter bar** (0/1 row; whatever you currently render here)
5. **Status bar** (1 row, always)

---

## 2) Data model additions (tabs, labels, active tab)
Use an explicit “All” view + pinned views. This makes the UI consistent: there is always a “leftmost” baseline tab.

```go
type PinnedTab struct {
	ID         string    // stable identifier (uuid or hash(query))
	Query      string
	Label      string    // user-customizable; fallback derived from Query
	Unseen     int       // new items since last viewed (while not active)
	LastViewed time.Time // set when user enters the tab
}

type Model struct {
	Width, Height int

	// Tab state:
	// ActiveTabIndex: 0 = All (live stream), 1..len(Pinned)=pinned tabs
	ActiveTabIndex int
	Pinned         []PinnedTab

	// Optional: if your stream can resend items, track seen IDs per tab.
	// SeenIDs map[string]struct{} // globally or per-tab (memory tradeoff)
}
```

**Label derivation:**
- On pin: `Label = ""` initially; render uses `Label` if set, else derives from `Query`.
- Provide rename action later (`r: rename`) to set `Label`.

---

## 3) Lipgloss styles (aligned with existing palette)
These intentionally reuse your existing colors:
- **bg=236** (StatusBar background)
- **active bg=62** (SelectedItem purple)
- **badge fg=226** (readable “new” color) or use 212 (pink) if you prefer consistency with TimeBandHeader

```go
var (
	TabBarStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("236")). // match StatusBar bg
		Foreground(lipgloss.Color("255")).
		Height(1)

	TabActiveStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("62")). // purple
		Padding(0, 1)

	TabInactiveStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")). // light gray
		Background(lipgloss.Color("236")). // blend into bar
		Padding(0, 1)

	TabEllipsisStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Background(lipgloss.Color("236")).
		Padding(0, 1)

	TabBadgeStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("226")). // yellow
		Bold(true)

	TabSepStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Background(lipgloss.Color("236"))

	EmptyStateStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Italic(true)
)
```

---

## 4) Complete tab bar rendering code (overflow + responsive)
### Behavior summary
- **Width ≥ 80**: show a window of tabs around the active tab if necessary; truncate labels; show `(N)` badge on inactive tabs with unseen items.
- **Width < 80**: collapse to a compact single-tab header:  
  `All [1/4]` or `Physics (3) [3/4]` (active label + position indicator).
- Always keep the **active tab visible**.

### Code
```go
package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

type tabSpec struct {
	Title  string
	Unseen int
	Active bool
}

// rune-safe truncation with ellipsis
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}

// derive label: prefer custom Label, else Query
func tabTitle(label, query string) string {
	if strings.TrimSpace(label) != "" {
		return label
	}
	return strings.TrimSpace(query)
}

func renderTabBar(width int, pinned []PinnedTab, activeTabIndex int) string {
	// Tabs list: All + pinned
	if len(pinned) == 0 {
		return "" // no pinned => no tab UI overhead
	}

	tabs := make([]tabSpec, 0, len(pinned)+1)
	tabs = append(tabs, tabSpec{Title: "All", Unseen: 0, Active: activeTabIndex == 0})
	for i := range pinned {
		isActive := activeTabIndex == i+1
		tabs = append(tabs, tabSpec{
			Title:  tabTitle(pinned[i].Label, pinned[i].Query),
			Unseen: pinned[i].Unseen,
			Active: isActive,
		})
	}

	totalTabs := len(tabs)
	if activeTabIndex < 0 || activeTabIndex >= totalTabs {
		activeTabIndex = 0
	}

	// Narrow mode (<80): collapse to active tab + [i/n]
	if width < 80 {
		active := tabs[activeTabIndex]
		title := active.Title

		// badge only if unseen and not active; in narrow mode, allow badge even if active is pinned
		// (user might want to see it before entering; but once active, Unseen should usually be 0)
		badge := ""
		if active.Unseen > 0 && !active.Active {
			badge = TabBadgeStyle.Render(fmt.Sprintf("(%d)", active.Unseen))
		}

		pos := fmt.Sprintf("[%d/%d]", activeTabIndex+1, totalTabs)
		// Reserve space for " " + pos
		reserve := 1 + utf8.RuneCountInString(pos)
		avail := width - reserve - 2 // a bit of slack
		if avail < 4 {
			avail = 4
		}

		main := truncateRunes(title, avail)
		if badge != "" && lipgloss.Width(main)+1+lipgloss.Width(badge) < avail {
			main = main + " " + badge
		}
		line := TabActiveStyle.Render(main) + " " + TabSepStyle.Render(pos)
		return TabBarStyle.Width(width).Render(lipgloss.PlaceHorizontal(width, lipgloss.Left, line))
	}

	// Wider mode: render a window of tabs around active when overflowed.
	sep := TabSepStyle.Render(" ")
	sepW := lipgloss.Width(sep)

	// Minimum viable tab content width (inside padding) before truncation looks awful.
	minContent := 4
	// Each tab has padding(0,1) => +2 columns
	minTabW := minContent + 2

	// How many tabs can we show at minimum width?
	maxVisible := (width + sepW) / (minTabW + sepW)
	if maxVisible < 1 {
		maxVisible = 1
	}
	if maxVisible > totalTabs {
		maxVisible = totalTabs
	}

	// Choose a window centered on active
	start := activeTabIndex - maxVisible/2
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > totalTabs {
		end = totalTabs
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}

	visible := tabs[start:end]

	// Add ellipses if we hid tabs
	if start > 0 {
		visible = append([]tabSpec{{Title: "…"}}, visible...)
	}
	if end < totalTabs {
		visible = append(visible, tabSpec{Title: "…"})
	}

	// Now compute per-tab content width dynamically based on visible count
	n := len(visible)
	avail := width - (n-1)*sepW
	if avail < n*minTabW {
		// fallback: still render; lipgloss will clip at container width
		avail = n * minTabW
	}
	perTab := avail / n
	contentMax := perTab - 2 // remove padding
	if contentMax < minContent {
		contentMax = minContent
	}

	renderOne := func(t tabSpec, idx int) string {
		// Ellipsis tabs are rendered in a special style
		if t.Title == "…" {
			return TabEllipsisStyle.Render("…")
		}

		// Badge only shown when NOT active (requirement: "new since last viewed")
		badge := ""
		if t.Unseen > 0 && !t.Active {
			count := t.Unseen
			if count > 99 {
				count = 99
			}
			badge = TabBadgeStyle.Render(fmt.Sprintf("(%d)", count))
		}

		// Leave room for badge if present
		labelMax := contentMax
		if badge != "" {
			// label + space + badge
			labelMax = contentMax - (1 + lipgloss.Width(badge))
			if labelMax < 1 {
				labelMax = 1
			}
		}

		label := truncateRunes(t.Title, labelMax)
		text := label
		if badge != "" {
			text = label + " " + badge
		}

		if t.Active {
			return TabActiveStyle.Render(text)
		}
		return TabInactiveStyle.Render(text)
	}

	parts := make([]string, 0, n*2)
	for i := range visible {
		if i > 0 {
			parts = append(parts, sep)
		}
		parts = append(parts, renderOne(visible[i], i))
	}

	line := lipgloss.JoinHorizontal(lipgloss.Left, parts...)
	return TabBarStyle.Width(width).Render(lipgloss.Place(width, 1, lipgloss.Left, lipgloss.Center, line))
}
```

---

## 5) New item badge logic (count since last viewed)
Best fit for an item stream: **increment unseen on ingestion** by testing each new item against each pinned query. This avoids periodic refetch and keeps badges accurate in real time.

### Core rules
- If a new item matches pinned query **and that tab is not active**: `Unseen++`
- When user switches to a pinned tab: `Unseen=0`, `LastViewed=now`
- When active tab is pinned, matching items appear immediately; they are **not** counted as unseen.

### Pseudocode in `Update`
```go
// called when new items are appended to the global stream
func (m *Model) onItemsAppended(newItems []Item) {
	// Append to global store as usual...
	// m.Items = append(m.Items, newItems...)

	for _, it := range newItems {
		for i := range m.Pinned {
			tabIndex := i + 1 // because 0 is "All"
			if m.ActiveTabIndex == tabIndex {
				continue // user is already viewing it
			}
			if matchesQuery(m.Pinned[i].Query, it) {
				m.Pinned[i].Unseen++
			}
		}
	}
}

func (m *Model) setActiveTab(idx int) {
	m.ActiveTabIndex = idx
	if idx >= 1 && idx <= len(m.Pinned) {
		t := &m.Pinned[idx-1]
		t.Unseen = 0
		t.LastViewed = time.Now()
	}
}
```

**Optional robustness (if items can be re-emitted):**
- Track a global `seenIDs` set, or per-tab `seenIDs`, and only increment unseen for IDs not seen before.

---

## 6) Status bar changes when on a pinned tab (unpin hint)
When `ActiveTabIndex >= 1`, append pinned-tab actions to status bar hints:

- `←/→` or `Tab/Shift+Tab`: switch tabs
- `u`: unpin current
- `r`: rename tab (optional but recommended since you support custom labels)

Example snippet (adapt to your existing status bar renderer/styles):
```go
func (m Model) statusHints() []string {
	h := []string{
		"j/k:nav",
		"/:search",
		"q:quit",
	}
	if len(m.Pinned) > 0 {
		h = append(h, "Tab:next", "Shift+Tab:prev")
	}
	if m.ActiveTabIndex >= 1 {
		h = append(h, "u:unpin", "r:rename")
	}
	return h
}
```

**Unpin behavior**:
- Remove `Pinned[idx-1]`
- Set `ActiveTabIndex = 0` (fallback to All) or clamp to next available pinned tab.

---

## 7) Empty state rendering (pinned tab has no results)
When the active view’s filtered list is empty:
- Center a short message in the item stream viewport area.
- Include the query/label and next action hints.

```go
func renderEmptyState(width, height int, activeTitle, query string, isPinned bool) string {
	var msg string
	if isPinned {
		msg = fmt.Sprintf(
			"No items match %q\n\nWaiting for new matching items…\nTab: switch  u: unpin",
			truncateRunes(tabTitle(activeTitle, query), width-10),
		)
	} else {
		msg = "No items yet…\n\nWaiting for stream…"
	}
	block := EmptyStateStyle.Render(msg)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}
```

---

## Integration notes (Model.View height math)
In `View()`:
- `tabBar := renderTabBar(m.Width, m.Pinned, m.ActiveTabIndex)` (height 1 or 0)
- Subtract its height from the stream viewport height.

```go
func (m Model) View() string {
	tabBar := renderTabBar(m.Width, m.Pinned, m.ActiveTabIndex)
	tabH := 0
	if tabBar != "" {
		tabH = 1
	}

	// Compute heights based on what you already conditionally render:
	// streamH := m.Height - tabH - errorH - filterH - statusH
	// itemStream := m.renderItemStream(m.Width, streamH)

	return lipgloss.JoinVertical(lipgloss.Top,
		tabBar,
		m.renderItemStreamArea(tabH), // your existing stream+bars composition
	)
}
```

This plan yields a tab UI that is width-efficient, keeps the active tab visible, provides accurate “new since last viewed” badges, and remains readable down to narrow terminals without sacrificing the item stream.

---

## Feature 10: Jina Reader for Full Articles

### 10a. Reader Client

# Claude's Analysis: Jina Reader Client (10a)

## Package Structure

```
internal/reader/
    reader.go       — Reader interface + JinaReader implementation
    reader_test.go  — tests with httptest mock server
```

## Interface Design

```go
package reader

import "context"

// Reader fetches full article content from a URL and returns clean Markdown.
type Reader interface {
    // Available returns true if the reader service is accessible.
    Available() bool
    // Read fetches the article at the given URL and returns Markdown content.
    // Returns an error if the URL is unreachable, the service is unavailable,
    // or the response exceeds the size limit.
    Read(ctx context.Context, url string) (string, error)
    // Name returns the reader identifier for logging.
    Name() string
}
```

## JinaReader Implementation

```go
package reader

import (
    "context"
    "fmt"
    "io"
    "net/http"
    "os"
    "strings"
    "time"
)

const (
    jinaReaderBaseURL = "https://r.jina.ai/"
    defaultTimeout    = 30 * time.Second
    maxResponseSize   = 100 * 1024 // 100KB
    maxRetries        = 2
    retryDelay        = 2 * time.Second
)

// JinaReader fetches article content via Jina Reader API.
type JinaReader struct {
    apiKey string
    client *http.Client
}

// NewJinaReader creates a Reader using the Jina Reader API.
// Uses JINA_API_KEY from environment.
func NewJinaReader() *JinaReader {
    return &JinaReader{
        apiKey: os.Getenv("JINA_API_KEY"),
        client: &http.Client{Timeout: defaultTimeout},
    }
}

// NewJinaReaderWithKey creates a Reader with an explicit API key.
// Used for testing.
func NewJinaReaderWithKey(apiKey string) *JinaReader {
    return &JinaReader{
        apiKey: apiKey,
        client: &http.Client{Timeout: defaultTimeout},
    }
}

func (r *JinaReader) Available() bool {
    return r.apiKey != ""
}

func (r *JinaReader) Name() string {
    return "jina-reader"
}

func (r *JinaReader) Read(ctx context.Context, url string) (string, error) {
    if !r.Available() {
        return "", fmt.Errorf("jina reader: API key not set")
    }
    if url == "" {
        return "", fmt.Errorf("jina reader: empty URL")
    }

    // Jina Reader API: GET https://r.jina.ai/{target_url}
    readerURL := jinaReaderBaseURL + url

    var lastErr error
    for attempt := 0; attempt <= maxRetries; attempt++ {
        if attempt > 0 {
            select {
            case <-ctx.Done():
                return "", ctx.Err()
            case <-time.After(retryDelay):
            }
        }

        body, err := r.doRequest(ctx, readerURL)
        if err != nil {
            lastErr = err
            // Only retry on rate limit or server errors
            if isRetryable(err) {
                continue
            }
            return "", err
        }
        return body, nil
    }
    return "", fmt.Errorf("jina reader: max retries exceeded: %w", lastErr)
}

func (r *JinaReader) doRequest(ctx context.Context, url string) (string, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return "", fmt.Errorf("create request: %w", err)
    }

    req.Header.Set("Authorization", "Bearer "+r.apiKey)
    req.Header.Set("Accept", "text/markdown")
    req.Header.Set("X-Return-Format", "markdown")

    resp, err := r.client.Do(req)
    if err != nil {
        return "", fmt.Errorf("http request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusTooManyRequests {
        return "", &RateLimitError{RetryAfter: parseRetryAfter(resp)}
    }
    if resp.StatusCode >= 500 {
        return "", &ServerError{StatusCode: resp.StatusCode}
    }
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("jina reader: HTTP %d", resp.StatusCode)
    }

    // Read with size limit
    limited := io.LimitReader(resp.Body, maxResponseSize+1)
    data, err := io.ReadAll(limited)
    if err != nil {
        return "", fmt.Errorf("read response: %w", err)
    }
    if len(data) > maxResponseSize {
        // Truncate to last complete line within limit
        content := string(data[:maxResponseSize])
        if idx := strings.LastIndex(content, "\n"); idx > 0 {
            content = content[:idx]
        }
        return content + "\n\n[Content truncated]", nil
    }

    return strings.TrimSpace(string(data)), nil
}
```

## Error Types

```go
// RateLimitError indicates the API rate limit was hit.
type RateLimitError struct {
    RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
    return fmt.Sprintf("jina reader: rate limited (retry after %s)", e.RetryAfter)
}

// ServerError indicates a server-side error.
type ServerError struct {
    StatusCode int
}

func (e *ServerError) Error() string {
    return fmt.Sprintf("jina reader: server error %d", e.StatusCode)
}

func isRetryable(err error) bool {
    switch err.(type) {
    case *RateLimitError, *ServerError:
        return true
    }
    return false
}

func parseRetryAfter(resp *http.Response) time.Duration {
    // Parse Retry-After header if present
    if val := resp.Header.Get("Retry-After"); val != "" {
        if secs, err := time.ParseDuration(val + "s"); err == nil {
            return secs
        }
    }
    return retryDelay
}
```

## Test Strategy

```go
func TestJinaReader_Read(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify headers
        assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
        assert.Equal(t, "text/markdown", r.Header.Get("Accept"))

        // Return mock article
        fmt.Fprint(w, "# Article Title\n\nArticle content here.")
    }))
    defer server.Close()

    reader := &JinaReader{
        apiKey: "test-key",
        client: &http.Client{Timeout: 5 * time.Second},
    }
    // Override base URL for testing
    // ... need to make baseURL configurable or use server.URL

    content, err := reader.Read(context.Background(), server.URL+"/test")
    assert.NoError(t, err)
    assert.Contains(t, content, "Article Title")
}
```

For testability, make the base URL configurable:
```go
type JinaReader struct {
    apiKey  string
    baseURL string  // defaults to "https://r.jina.ai/"
    client  *http.Client
}
```

## Integration Points
- `cmd/observer/main.go` creates JinaReader alongside JinaEmbedder and JinaReranker
- Passed to App via AppConfig
- UI triggers Read on key press (subtask 10c)
- Article content cached in store (subtask 10b)

### 10b. Article Cache

### ANALYZE
Both responses propose a URL-keyed cache table, TTL filtering on reads, periodic cleanup, and optional LRU eviction when a max-size is exceeded. They agree on core methods: `GetCachedArticle`, `SaveArticle`, `CleanExpiredArticles`, `CacheStats`.

Key differences / issues:
- **Schema vs requirements**: Both omit a true `item_id` dimension in a way that satisfies “dedup by URL”. If `item_id` must exist, it can’t be part of the primary key without breaking URL dedup (unless you add a separate mapping table or treat `item_id` as “last requester” metadata).
- **LRU tracking**: Response A adds `access_count` + `last_accessed_at`; Response B adds only `last_accessed`. Access count isn’t needed for LRU.
- **Time handling**: Response B mixes SQLite `datetime('now')` and Go-formatted strings; Response A uses Go `time.Now()` directly. A cleaner approach is to store timestamps as **INTEGER epoch seconds (UTC)** and compare numerically.
- **Concurrency / locking**: Response B shows an “upgrade lock” pattern but contains mechanical errors (`s.RLock()` instead of `s.mu.RLock()`) and mixes placeholder styles. Response A consistently locks but uses a background goroutine for eviction, which can be tricky around shutdown.
- **Eviction**: Both implement eviction with repeated deletes. Neither uses a transaction to keep “size check → delete → recheck” consistent.

### SYNERGIZE
Best combined approach:
- Use **URL as the cache key** (hard requirement), plus **`last_accessed_at`** to implement LRU eviction.
- Enforce TTL at **query-time** (never return stale) and reclaim space via **periodic and/or lazy cleanup**.
- Use **UPSERT** (`ON CONFLICT(url) DO UPDATE`) instead of `INSERT OR REPLACE` to avoid delete/insert side effects.
- Store times as **INTEGER (unix epoch, UTC)** for consistent comparisons and simpler tests.
- If `item_id` must exist per requirement, either:
  1) store it as “last_item_id” metadata in `article_cache` (still deduped by URL), or  
  2) add an optional `article_cache_refs(item_id,url)` mapping table (recommended if you truly need many-to-one mapping).

### UNIFY
## Implementation Plan: Article Cache with TTL (Go 1.24 + SQLite WAL)

### 0) Behavior Summary
- Cache entries are keyed by **URL** (dedup across items).
- `GetCachedArticle(url)` returns cached content only if **not expired**; on hit, it **touches** LRU metadata.
- `SaveArticle(url, content, ttl)` writes/refreshes a cache entry with a configurable TTL (default 24h) and then enforces optional **max cache size** via **LRU eviction** (expired entries evicted first).
- `CleanExpiredArticles()` deletes expired rows to reclaim reusable space.
- `CacheStats()` reports count/bytes/expired for monitoring.

---

## 1) SQL Schema

### 1.1 Recommended `article_cache` table (dedup by URL)
Stores required fields and adds LRU support. Timestamps stored as **UTC epoch seconds**.

```sql
CREATE TABLE IF NOT EXISTS article_cache (
    -- Requirement fields
    url            TEXT PRIMARY KEY,
    content        TEXT NOT NULL,
    fetched_at     INTEGER NOT NULL, -- unix epoch seconds (UTC)
    expires_at     INTEGER NOT NULL, -- unix epoch seconds (UTC)
    content_size   INTEGER NOT NULL CHECK (content_size >= 0),

    -- LRU support (extra, needed for eviction)
    last_accessed_at INTEGER NOT NULL,

    -- Requirement mentions item_id; to preserve URL dedup, store as metadata
    -- (the most recent item that caused a fetch/save). Nullable.
    item_id        INTEGER
);

CREATE INDEX IF NOT EXISTS idx_article_cache_expires_at
    ON article_cache(expires_at);

CREATE INDEX IF NOT EXISTS idx_article_cache_last_accessed
    ON article_cache(last_accessed_at);
```

### 1.2 Optional mapping table (only if you truly need item↔URL tracking)
If you need to know which items referenced a cached URL (many items → one URL) without breaking URL dedup, add:

```sql
CREATE TABLE IF NOT EXISTS article_cache_refs (
    item_id INTEGER NOT NULL,
    url     TEXT NOT NULL,
    PRIMARY KEY (item_id, url),
    FOREIGN KEY (url) REFERENCES article_cache(url) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_article_cache_refs_url
    ON article_cache_refs(url);
```

This is optional because your required store methods are URL-based.

---

## 2) Go Types

```go
type CachedArticle struct {
    URL            string
    Content        string
    ContentSize    int64
    FetchedAt      time.Time
    ExpiresAt      time.Time
    LastAccessedAt time.Time
    ItemID         *int64 // nil if unknown
}

type CacheStats struct {
    TotalArticles   int64
    TotalSizeBytes  int64
    ExpiredArticles int64
    OldestFetchedAt *time.Time // nil if empty
}
```

Store config knobs (fits existing patterns):

```go
const DefaultArticleTTL = 24 * time.Hour

type Store struct {
    db  *sql.DB
    mu  sync.RWMutex

    ArticleDefaultTTL  time.Duration // default 24h
    ArticleMaxBytes    int64         // 0 = unlimited (e.g., 50*1024*1024)
    // Optional for testability:
    now func() time.Time // if nil, use time.Now
}
```

Helper:

```go
func (s *Store) nowUTC() time.Time {
    if s.now != nil { return s.now().UTC() }
    return time.Now().UTC()
}
```

---

## 3) Store Methods (SQL + semantics)

### 3.1 `GetCachedArticle(url string) (*CachedArticle, error)`
**TTL enforcement at query-time** (`expires_at > now`). On hit, update `last_accessed_at` for LRU.

Use a single write lock (simpler + correct LRU metadata). If you want more concurrency, you can accept stale LRU data and “touch” asynchronously, but correctness is simplest.

```go
func (s *Store) GetCachedArticle(url string) (*CachedArticle, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    now := s.nowUTC().Unix()

    row := s.db.QueryRow(`
        SELECT url, content, content_size, fetched_at, expires_at, last_accessed_at, item_id
        FROM article_cache
        WHERE url = ? AND expires_at > ?
    `, url, now)

    var (
        ca CachedArticle
        fetchedAt, expiresAt, lastAccessed int64
        itemID sql.NullInt64
    )

    err := row.Scan(&ca.URL, &ca.Content, &ca.ContentSize, &fetchedAt, &expiresAt, &lastAccessed, &itemID)
    if errors.Is(err, sql.ErrNoRows) {
        // Optional lazy cleanup of exactly-this URL (reclaims space sooner):
        // _, _ = s.db.Exec(`DELETE FROM article_cache WHERE url = ? AND expires_at <= ?`, url, now)
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("GetCachedArticle: %w", err)
    }

    ca.FetchedAt = time.Unix(fetchedAt, 0).UTC()
    ca.ExpiresAt = time.Unix(expiresAt, 0).UTC()
    ca.LastAccessedAt = time.Unix(lastAccessed, 0).UTC()
    if itemID.Valid {
        v := itemID.Int64
        ca.ItemID = &v
    }

    // Touch LRU
    _, _ = s.db.Exec(`UPDATE article_cache SET last_accessed_at = ? WHERE url = ?`, now, url)

    return &ca, nil
}
```

### 3.2 `SaveArticle(url, content string, ttl time.Duration) error`
- Default TTL is 24h if `ttl <= 0`.
- Computes `content_size` as `len(content)` (bytes).
- Writes via UPSERT.
- Runs eviction if `ArticleMaxBytes > 0`.

Use a transaction so “insert → measure → evict” is consistent.

```go
func (s *Store) SaveArticle(url, content string, ttl time.Duration) error {
    if ttl <= 0 {
        ttl = s.ArticleDefaultTTL
        if ttl <= 0 {
            ttl = DefaultArticleTTL
        }
    }

    nowT := s.nowUTC()
    now := nowT.Unix()
    expires := nowT.Add(ttl).Unix()
    size := int64(len(content))

    s.mu.Lock()
    defer s.mu.Unlock()

    // If max size is set and this single entry can never fit, choose a policy:
    // Recommended: don't cache and return a sentinel error.
    if s.ArticleMaxBytes > 0 && size > s.ArticleMaxBytes {
        return fmt.Errorf("article too large to cache: %d > max %d", size, s.ArticleMaxBytes)
    }

    tx, err := s.db.Begin()
    if err != nil { return err }
    defer func() { _ = tx.Rollback() }()

    // Upsert (avoid INSERT OR REPLACE delete/insert behavior)
    _, err = tx.Exec(`
        INSERT INTO article_cache (url, content, content_size, fetched_at, expires_at, last_accessed_at, item_id)
        VALUES (?, ?, ?, ?, ?, ?, NULL)
        ON CONFLICT(url) DO UPDATE SET
            content          = excluded.content,
            content_size     = excluded.content_size,
            fetched_at       = excluded.fetched_at,
            expires_at       = excluded.expires_at,
            last_accessed_at = excluded.last_accessed_at
    `, url, content, size, now, expires, now)
    if err != nil {
        return fmt.Errorf("SaveArticle upsert: %w", err)
    }

    // Optional: opportunistic cleanup of expired entries before eviction
    _, _ = tx.Exec(`DELETE FROM article_cache WHERE expires_at <= ?`, now)

    if s.ArticleMaxBytes > 0 {
        if err := evictOverLimitTx(tx, now, s.ArticleMaxBytes); err != nil {
            return err
        }
    }

    if err := tx.Commit(); err != nil {
        return err
    }
    return nil
}
```

Eviction helper: expired-first, then LRU by `last_accessed_at`:

```go
func evictOverLimitTx(tx *sql.Tx, now int64, maxBytes int64) error {
    for {
        var total int64
        if err := tx.QueryRow(`SELECT COALESCE(SUM(content_size), 0) FROM article_cache`).Scan(&total); err != nil {
            return fmt.Errorf("eviction size scan: %w", err)
        }
        if total <= maxBytes {
            return nil
        }

        // Pick one victim: expired first, else least recently accessed
        var victimURL string
        err := tx.QueryRow(`
            SELECT url
            FROM article_cache
            ORDER BY
                CASE WHEN expires_at <= ? THEN 0 ELSE 1 END ASC,
                last_accessed_at ASC
            LIMIT 1
        `, now).Scan(&victimURL)
        if errors.Is(err, sql.ErrNoRows) {
            return nil
        }
        if err != nil {
            return fmt.Errorf("eviction victim select: %w", err)
        }

        if _, err := tx.Exec(`DELETE FROM article_cache WHERE url = ?`, victimURL); err != nil {
            return fmt.Errorf("eviction delete: %w", err)
        }
    }
}
```

### 3.3 `CleanExpiredArticles() (int64, error)`
Bulk delete expired rows. Can be run on a ticker (e.g., every hour) or during coordinator cycles.

```go
func (s *Store) CleanExpiredArticles() (int64, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    now := s.nowUTC().Unix()
    res, err := s.db.Exec(`DELETE FROM article_cache WHERE expires_at <= ?`, now)
    if err != nil {
        return 0, fmt.Errorf("CleanExpiredArticles: %w", err)
    }
    n, _ := res.RowsAffected()
    return n, nil
}
```

### 3.4 `CacheStats() (CacheStats, error)`
Aggregates computed on demand (accurate + simple).

```go
func (s *Store) CacheStats() (CacheStats, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    now := s.nowUTC().Unix()
    var st CacheStats

    if err := s.db.QueryRow(`
        SELECT COUNT(*), COALESCE(SUM(content_size), 0)
        FROM article_cache
    `).Scan(&st.TotalArticles, &st.TotalSizeBytes); err != nil {
        return st, err
    }

    if err := s.db.QueryRow(`
        SELECT COUNT(*) FROM article_cache WHERE expires_at <= ?
    `, now).Scan(&st.ExpiredArticles); err != nil {
        return st, err
    }

    var oldest sql.NullInt64
    if err := s.db.QueryRow(`SELECT MIN(fetched_at) FROM article_cache`).Scan(&oldest); err != nil {
        return st, err
    }
    if oldest.Valid {
        t := time.Unix(oldest.Int64, 0).UTC()
        st.OldestFetchedAt = &t
    }

    return st, nil
}
```

---

## 4) TTL Enforcement Strategy
Use **both**:
1. **Query-time enforcement (mandatory):** `WHERE expires_at > now` in `GetCachedArticle` ensures expired content is never returned.
2. **Cleanup-time reclamation (recommended):**
   - Periodic: coordinator ticker every 30–60 minutes calling `CleanExpiredArticles()`.
   - Opportunistic: `SaveArticle` can delete expired rows before eviction (shown above).
   - Optional lazy per-URL cleanup on cache miss.

This gives correct freshness + bounded disk usage.

---

## 5) Cache Eviction Strategy (Max Size + LRU)
- Configuration: `Store.ArticleMaxBytes` (e.g., 50MB); `0` means unlimited.
- Trigger: after saving new content (and optionally after periodic cleanup).
- Policy:
  1) evict **expired** first  
  2) then evict **least recently used** by `last_accessed_at`
- Implementation: loop in a transaction until total bytes ≤ max.
- Edge case: if a single article exceeds max size, return an error (or choose “don’t cache” policy).

---

## 6) Migration / Additive Schema Approach
Match your existing patterns:
- Add table creation to `createTables()` with `CREATE TABLE IF NOT EXISTS`.
- If you anticipate evolving columns later (e.g., adding `last_accessed_at` to existing installs), add a `migrateArticleCache()` similar to `migrateEmbeddings()`:
  - `PRAGMA table_info(article_cache)` to detect missing columns
  - `ALTER TABLE article_cache ADD COLUMN ...` (SQLite supports adding columns)
  - Create missing indexes with `CREATE INDEX IF NOT EXISTS`

If this is a brand-new table for all users, `CREATE TABLE IF NOT EXISTS` with the full schema is sufficient.

---

## 7) Test Cases (SQLite in-memory)
Use `:memory:` or a temp file DB; inject a fake clock via `store.now` for deterministic TTL tests.

Suggested tests:
1. **Save/Get hit**
   - Save with TTL 1h, then Get returns content and correct metadata.
2. **TTL expiry**
   - Set `store.now` to t0; save with TTL 10s; advance `store.now` to t0+11s; Get returns `nil`.
3. **Cleanup removes expired**
   - Insert two entries (one expired, one valid); run `CleanExpiredArticles`; verify only expired removed.
4. **Dedup by URL (upsert)**
   - Save URL with content A; save same URL with content B; stats count remains 1; content is B.
5. **LRU eviction under max size**
   - Set max bytes small; save multiple entries; touch one via Get; add another entry to exceed max; verify oldest (by last_accessed) is evicted, but recently accessed remains.
6. **Expired-first eviction**
   - Create an expired entry and a fresh entry; exceed max; ensure expired is evicted before fresh regardless of last_accessed.
7. **Stats accuracy**
   - Validate `TotalArticles`, `TotalSizeBytes`, `ExpiredArticles`.
8. **Concurrency sanity**
   - Run `SaveArticle` + `GetCachedArticle` in goroutines under `-race` to ensure no data races (your mutex should make this straightforward).

---

## Integration Notes
- Fetch flow:
  1) `ca, _ := store.GetCachedArticle(url)`  
  2) if `ca != nil` use it; else fetch from Jina Reader API and `store.SaveArticle(url, md, ttl)`
- Run `CleanExpiredArticles()` on the coordinator loop (or a ticker) to keep the DB tidy.
- Log `CacheStats()` occasionally for monitoring cache growth (size limits + content_size make this easy).

### 10c. UI Integration

# Claude's Analysis: UI Integration for Article Reading (10c)

## State Additions

```go
type App struct {
    // ... existing fields ...

    // Article reading mode
    articleActive    bool        // true when viewing an article
    articleContent   string      // rendered Markdown content
    articleTitle     string      // item title for header
    articleURL       string      // for "open in browser" action
    articleScroll    int         // scroll offset in article view
    articleLines     int         // total rendered lines
    articleLoading   bool        // true while fetching article
    articleItemID    string      // item being viewed

    // Article fetcher (injected via AppConfig)
    fetchArticle func(url string) tea.Cmd
}
```

## AppConfig Addition

```go
type AppConfig struct {
    // ... existing ...
    FetchArticle func(url string) tea.Cmd
}
```

## Message Types

```go
// ArticleFetched is sent when a full article has been fetched.
type ArticleFetched struct {
    URL     string
    Content string  // Markdown content
    Cached  bool    // true if served from cache
    Err     error
}
```

## Key Binding Flow

**Enter Key Behavior Change:**
Currently Enter marks an item as read. New behavior:
- `Enter` → Open article view (fetch + display). Also marks as read.
- `o` → Same as Enter (alias for "open").
- `m` → Mark read without opening (preserves current behavior for users who just want to mark).

```go
// In handleKeyMsg normal mode:
case tea.KeyEnter:
    return a.openArticle()
// ...
switch msg.String() {
case "o":
    return a.openArticle()
case "m":
    return a.handleMarkRead()  // renamed from handleEnter
```

## Article Open Flow

```go
func (a App) openArticle() (tea.Model, tea.Cmd) {
    if len(a.items) == 0 || a.cursor >= len(a.items) {
        return a, nil
    }

    item := a.items[a.cursor]
    a.articleActive = true
    a.articleLoading = true
    a.articleTitle = item.Title
    a.articleURL = item.URL
    a.articleItemID = item.ID
    a.articleScroll = 0
    a.statusText = "Loading article..."

    var cmds []tea.Cmd
    // Mark as read
    if a.markRead != nil {
        cmds = append(cmds, a.markRead(item.ID))
    }
    // Fetch article
    if a.fetchArticle != nil && item.URL != "" {
        cmds = append(cmds, a.fetchArticle(item.URL))
        cmds = append(cmds, a.spinner.Tick)
    } else {
        // No reader available — show summary
        a.articleContent = renderFallbackArticle(item)
        a.articleLoading = false
        a.statusText = ""
    }

    return a, tea.Batch(cmds...)
}
```

## Article Fetched Handler

```go
case ArticleFetched:
    a.articleLoading = false
    a.statusText = ""
    if msg.Err != nil {
        // Fallback to summary
        item := a.items[a.cursor]
        a.articleContent = renderFallbackArticle(item)
        a.err = fmt.Errorf("article fetch failed: %w", msg.Err)
    } else {
        a.articleContent = msg.Content
    }
    // Count lines for scroll limits
    a.articleLines = strings.Count(a.articleContent, "\n") + 1
    return a, nil
```

## Article View Rendering

```go
func (a App) View() string {
    if !a.ready { return "Loading..." }

    // Debug overlay takes priority
    if a.debugVisible && a.ring != nil { ... }

    // Article view: full-screen takeover
    if a.articleActive {
        return a.renderArticleView()
    }

    // ... normal view ...
}

func (a App) renderArticleView() string {
    contentHeight := a.height - 2 // -1 header, -1 status bar

    // Header: title + source
    header := ArticleHeader.Width(a.width).Render(a.articleTitle)

    if a.articleLoading {
        body := fmt.Sprintf("\n  %s Loading article...\n", a.spinner.View())
        statusBar := StatusBar.Width(a.width).Render("  Loading...")
        return header + "\n" + body + "\n" + statusBar
    }

    // Render article content with scroll
    lines := strings.Split(a.articleContent, "\n")
    end := a.articleScroll + contentHeight
    if end > len(lines) { end = len(lines) }
    start := a.articleScroll
    if start > len(lines) { start = len(lines) }

    visibleContent := strings.Join(lines[start:end], "\n")
    // Word-wrap long lines
    wrappedContent := wordWrap(visibleContent, a.width - 4)

    // Scroll indicator
    scrollPct := ""
    if a.articleLines > contentHeight {
        pct := float64(a.articleScroll) / float64(a.articleLines - contentHeight) * 100
        scrollPct = fmt.Sprintf(" %d%%", int(pct))
    }

    // Status bar with article-specific keys
    keys := []string{
        StatusBarKey.Render("j/k") + StatusBarText.Render(":scroll"),
        StatusBarKey.Render("o") + StatusBarText.Render(":browser"),
        StatusBarKey.Render("Esc") + StatusBarText.Render(":back"),
    }
    keyHints := strings.Join(keys, " ")
    position := fmt.Sprintf(" Article%s ", scrollPct)
    statusBar := StatusBar.Width(a.width).Render(position + keyHints)

    return header + "\n" + wrappedContent + "\n" + statusBar
}
```

## Markdown Rendering Approach

**Option 1: glamour library** (charmbracelet/glamour) — purpose-built for terminal Markdown rendering. Handles headers, lists, code blocks, links with ANSI styles. Part of the Charm ecosystem (same as Bubble Tea).

**Option 2: Custom lipgloss** — more control but significant effort.

**Recommendation: glamour**. It's already in the Charm ecosystem, actively maintained, and handles edge cases (nested lists, tables, etc.) that would take weeks to implement manually.

```go
import "github.com/charmbracelet/glamour"

func renderMarkdown(content string, width int) string {
    renderer, err := glamour.NewTermRenderer(
        glamour.WithAutoStyle(),   // dark/light detection
        glamour.WithWordWrap(width - 4),
    )
    if err != nil {
        return content // fallback: raw text
    }
    rendered, err := renderer.Render(content)
    if err != nil {
        return content
    }
    return rendered
}
```

## Key Handler in Article Mode

```go
func (a App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    // ... existing error clearing ...

    // Article view mode
    if a.articleActive {
        return a.handleArticleInput(msg)
    }

    // ... existing cascade ...
}

func (a App) handleArticleInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch msg.Type {
    case tea.KeyEsc:
        a.articleActive = false
        a.articleContent = ""
        return a, nil
    case tea.KeyCtrlC:
        return a, tea.Quit
    case tea.KeyUp:
        if a.articleScroll > 0 { a.articleScroll-- }
        return a, nil
    case tea.KeyDown:
        maxScroll := a.articleLines - (a.height - 2)
        if maxScroll < 0 { maxScroll = 0 }
        if a.articleScroll < maxScroll { a.articleScroll++ }
        return a, nil
    case tea.KeyPgUp:
        a.articleScroll -= (a.height - 2)
        if a.articleScroll < 0 { a.articleScroll = 0 }
        return a, nil
    case tea.KeyPgDown:
        maxScroll := a.articleLines - (a.height - 2)
        if maxScroll < 0 { maxScroll = 0 }
        a.articleScroll += (a.height - 2)
        if a.articleScroll > maxScroll { a.articleScroll = maxScroll }
        return a, nil
    }

    switch msg.String() {
    case "j":
        // same as KeyDown
    case "k":
        // same as KeyUp
    case "q":
        a.articleActive = false
        a.articleContent = ""
        return a, nil
    case "o":
        // Open in browser
        return a, openInBrowser(a.articleURL)
    case "g":
        a.articleScroll = 0
        return a, nil
    case "G":
        maxScroll := a.articleLines - (a.height - 2)
        if maxScroll < 0 { maxScroll = 0 }
        a.articleScroll = maxScroll
        return a, nil
    }
    return a, nil
}
```

## Browser Opening

```go
func openInBrowser(url string) tea.Cmd {
    return func() tea.Msg {
        cmd := exec.Command("xdg-open", url)
        _ = cmd.Start()
        return nil
    }
}
```

## Fallback Article

```go
func renderFallbackArticle(item store.Item) string {
    var b strings.Builder
    b.WriteString("# " + item.Title + "\n\n")
    if item.Summary != "" {
        b.WriteString(item.Summary + "\n\n")
    }
    b.WriteString("---\n")
    b.WriteString("*Full article reading requires JINA_API_KEY.*\n")
    b.WriteString("Press `o` to open in browser: " + item.URL + "\n")
    return b.String()
}
```

## New Article Header Style

```go
var ArticleHeader = lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("255")).
    Background(lipgloss.Color("62")).
    Padding(0, 1).
    MarginBottom(1)
```

### 10d. Search Enrichment

### ANALYZE
Both responses propose the same core system: split cached article Markdown into chunks, embed each chunk, store embeddings in an `article_chunks` table, and use chunk-level similarity to improve “More Like This” (MLT) and query search.

**Agreement**
- Chunk by paragraphs/sections; merge tiny chunks; split oversized chunks.
- Store `item_id`, `chunk_index`, `chunk_text/content`, `embedding BLOB`.
- Embed asynchronously after article fetch; do not block the TUI.
- Use chunk-level similarity then aggregate to item-level (often `MAX` similarity).
- Use chunk embeddings as a second signal for search relevance and/or MLT re-ranking.

**Differences / issues**
- **Schema details**: One adds helpful operational fields (id, timestamps, nullable embedding for retry). The other marks `embedding NOT NULL`, which conflicts with “incremental/background + retry” because you often want to insert chunks first and embed later (or handle embedding failures).
- **Chunking quality**: One emphasizes a simple deterministic splitter; the other adds better heuristics (strip markdown, overlap windows, prepend title for context).
- **MLT scoring**: One suggests full cross-product max similarity (can be expensive), while the other suggests an efficient asymmetric search (source item vector vs target chunks) and a hybrid re-rank strategy.
- **Types**: One uses `item_id TEXT`, the other `INTEGER`. Correct choice is: match whatever `items.id` is in your existing schema.

No critical factual errors, but treating segmenter as “free/no token charge” is not a safe assumption long-term; implement it as optional and configurable.

---

### SYNERGIZE
To produce a stronger plan, combine:
- **Operational robustness** (nullable embeddings, retry queue, partial index for “needs embedding”, idempotent inserts) + **better chunking heuristics** (min/max thresholds, sentence splitting with overlap, optional title/context, markdown normalization).
- **Efficient retrieval**: avoid O(sourceChunks × targetChunks × candidates) by using:
  - (a) a vector index (recommended: `sqlite-vec`) for chunk search, and/or
  - (b) a two-stage pipeline: fast candidate generation with existing item embeddings, then chunk-based re-ranking on a small candidate set.
- **Search UX improvement**: store/return the best-matching chunk as a snippet.

---

### UNIFY
## Implementation Plan — Embed Article Chunks for Finer-Grained “More Like This” + Search Enrichment

### 0) Goals & Principles
- Keep current item-level embeddings (title+summary) as the fast baseline.
- Add **passage/chunk embeddings** for articles *only after the full article is fetched/cached*.
- Run chunking+embedding **in background**, **idempotently**, with retry on failure.
- Use chunk similarity to:
  1) improve MLT ranking and
  2) improve query search relevance (and optionally show matched snippet).

---

## 1) Chunking Strategy (Markdown → semantic chunks)

### 1.1 Inputs
- Article Markdown from the Reader cache (`article_cache`).
- Optional: item title (for context).

### 1.2 Output
A slice of chunks, each with:
- `chunk_text` (stored for debugging/snippets)
- `embed_text` (text actually sent to embedder; may be normalized)

### 1.3 Algorithm (recommended default: “Markdown-aware paragraph chunker”)
1. **Normalize**:
   - Convert `\r\n` to `\n`, trim leading/trailing whitespace.
2. **Primary split**:
   - Split on blank lines: `\n\n+` (Markdown paragraph breaks).
   - Treat headings (`^#{1,6} `) as boundaries: start a new chunk when a heading appears, and keep the heading with the following paragraph(s) to preserve section meaning.
3. **Clean for embedding** (keep raw for display):
   - Remove or simplify Markdown constructs:
     - Links: `[text](url)` → `text`
     - Images: `![alt](url)` → `alt` or drop
     - Inline code/backticks → plain text
     - Excess punctuation/whitespace normalization
4. **Merge very small pieces**:
   - If a piece is `< MinWords` (e.g., 40–60 words), merge forward until it reaches the minimum or until a heading boundary.
5. **Split very large pieces**:
   - If a chunk exceeds `MaxWords` (e.g., 250–350 words), split by sentence boundaries into smaller windows.
   - Use a small overlap (e.g., 10–15%) to prevent losing context at boundaries.
6. **Optional (often beneficial): prepend context**:
   - `embed_text = title + "\n\n" + chunk_text_cleaned`
   - This helps “orphan” paragraphs remain interpretable.
7. **Hard limits**:
   - Cap chunks per article (e.g., `MaxChunks=30`) to bound cost.
   - If the article is extremely long, prioritize lead + major sections (headings).

### 1.4 Optional upgrade: Jina Segmenter API
Support a pluggable chunker:
- `ParagraphChunker` (default, zero network)
- `SegmenterChunker` (remote, potentially higher quality)

```go
type Chunk struct {
  Index     int
  Text      string // stored
  EmbedText string // sent to embedder
}

type Chunker interface {
  Chunk(ctx context.Context, title, markdown string) ([]Chunk, error)
}
```

---

## 2) Database Schema (article_chunks)

Requirement baseline: `item_id, chunk_index, chunk_text, embedding BLOB`.

Recommended migration (adds operational columns but keeps required fields):

```sql
CREATE TABLE IF NOT EXISTS article_chunks (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  item_id     TEXT NOT NULL,          -- use same type as items.id (TEXT or INTEGER)
  chunk_index INTEGER NOT NULL,        -- 0-based order
  chunk_text  TEXT NOT NULL,           -- stored chunk (for snippets/debug)
  embedding   BLOB,                   -- NULL until embedded / on failure
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

  UNIQUE(item_id, chunk_index),
  FOREIGN KEY(item_id) REFERENCES items(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_article_chunks_item
  ON article_chunks(item_id);

-- Fast “work queue” scan:
CREATE INDEX IF NOT EXISTS idx_article_chunks_needs_embed
  ON article_chunks(id)
  WHERE embedding IS NULL;
```

**Optional but useful**
- `content_hash` (detect changes and re-chunk/re-embed if the cached article changes).
- `embed_model` / `embed_version` if you ever rotate embedding models.

**Vector indexing**
- If you want scalable chunk search inside SQLite, strongly consider `sqlite-vec`:
  - Either store vectors in a `vec0` virtual table keyed by `article_chunks.id`, or compute similarity in Go for small candidate sets.

---

## 3) Embedding Pipeline (incremental + background)

### 3.1 When it runs
Trigger after article fetch succeeds and Markdown is saved to `article_cache`:
- “Fetch article” remains fast for the user.
- Chunk embedding happens asynchronously.

### 3.2 Idempotency rules
- If `article_chunks` already contains rows for `item_id`, **skip chunk creation** (unless you add `content_hash` and detect changes).
- If chunks exist but `embedding IS NULL` for some rows, only embed missing ones (retry behavior).

### 3.3 Coordinator flow (high-level)
1. Save Markdown to `article_cache` (existing).
2. Enqueue a background job: `EmbedArticleChunks(itemID)`.

### 3.4 Worker behavior (pseudo)
```go
func (c *Coordinator) EmbedArticleChunks(ctx context.Context, itemID string) error {
  // 1) Load item title + cached markdown
  title, markdown := c.store.GetItemTitleAndArticle(itemID)

  // 2) If chunks already exist, only embed NULL embeddings (skip rechunk)
  if c.store.HasChunks(itemID) {
    return c.embedMissingChunkEmbeddings(ctx, itemID)
  }

  // 3) Chunk
  chunks, err := c.chunker.Chunk(ctx, title, markdown)
  if err != nil || len(chunks) == 0 { return err }

  // 4) Insert chunks with embedding = NULL (transaction, INSERT OR IGNORE)
  //    Return chunk row IDs in order (or re-select them).
  ids, err := c.store.InsertChunks(itemID, chunks)
  if err != nil { return err }

  // 5) Embed in batch
  texts := make([]string, len(chunks))
  for i := range chunks { texts[i] = chunks[i].EmbedText }

  embs, err := c.embedBatch(ctx, texts) // uses embed.BatchEmbedder when available
  if err != nil {
    // leave embeddings NULL for retry
    return err
  }

  // 6) Persist embeddings (transaction)
  return c.store.UpdateChunkEmbeddings(ids, embs)
}
```

### 3.5 Background execution & throttling
- Use a small worker pool (e.g., 1–2 concurrent embedding workers) to control API usage.
- Retry strategy: exponential backoff; leave `embedding NULL` so `idx_article_chunks_needs_embed` can pick it up later.

---

## 4) Aggregation Strategy (chunk scores → item ranking)

You need a chunk-level similarity function and an item-level aggregation.

### 4.1 Chunk similarity
- Cosine similarity between 1024-d vectors.

### 4.2 Item-level aggregation options
Recommended default:
- **Max** similarity across matching chunks:
  - Interprets “one highly relevant passage” as strong relevance.
  - Robust and simple.

Alternatives (optional):
- **Top-k mean** (e.g., average of best 3 chunk matches) to reduce “one-off” spikes.
- **Softmax pooling** (smooth approximation of max).

### 4.3 Practical scoring for MLT (hybrid, efficient)
Avoid expensive all-pairs chunk comparisons across the entire corpus by using two stages:

**Stage A — candidate generation (fast, existing):**
- Use existing item embeddings (title+summary) to get top `N` candidates (e.g., 50).

**Stage B — chunk rerank (more accurate, bounded):**
- Compute `bestChunkSim(source, candidate)` using:
  - source item embedding vs candidate chunks **or**
  - source chunks vs candidate item embedding
  - (choose based on which side has chunks available; use whichever yields more signal)

**Final score blend (example):**
- `final = 0.4 * itemSim + 0.6 * maxChunkSim`
- If chunks missing on either side, fall back to `itemSim`.

This gives most of the benefit without needing a global chunk index.

### 4.4 If you add a chunk vector index (best quality)
You can also do:
- Query chunk index using the source item embedding (or each source chunk embedding).
- Get top K matching chunks globally, then `GROUP BY item_id` with `MAX(score)`.

This enables high-quality MLT even when item-level candidate generation misses.

---

## 5) Integration

### 5.1 “More Like This”
When user triggers MLT on item A:
1. Ensure A has an item embedding (already).
2. If A’s article chunks exist, also load A’s chunk embeddings.
3. Candidate generation:
   - Get top `N` by item embedding similarity (current approach).
   - Optionally merge with top `M` from chunk index (if available).
4. Rerank candidates using chunk similarity aggregation (max/top-k mean).
5. Return items; optionally include **best matching chunk snippet** from each result.

### 5.2 Search
When user searches:
1. Embed query (existing search embedder path or new).
2. Compute base ranking using item embeddings (fast).
3. For top `N` items (or using chunk index globally):
   - Compute `bestChunkSim(query, itemChunks)`
4. Blend scores:
   - `final = 0.7*itemScore + 0.3*chunkScore` (tunable)
5. UI enhancement (optional but valuable):
   - Show the best matching chunk text as a snippet under the result.

---

## 6) Store Methods (chunk CRUD)

Add `store/chunks.go`:

```go
type ArticleChunkRow struct {
  ID         int64
  ItemID     string
  ChunkIndex int
  ChunkText  string
  Embedding  []float32 // nil if NULL
}

func (s *Store) HasChunks(itemID string) (bool, error)

func (s *Store) InsertChunks(itemID string, chunks []Chunk) (chunkIDs []int64, err error)
// INSERT OR IGNORE on (item_id, chunk_index). Return IDs via SELECT afterward if needed.

func (s *Store) GetChunks(itemID string) ([]ArticleChunkRow, error)

func (s *Store) GetChunksNeedingEmbedding(limit int) ([]ArticleChunkRow, error)

func (s *Store) UpdateChunkEmbeddings(chunkIDs []int64, embs [][]float32) error
// transactional update, serialize []float32 -> BLOB (float32 little-endian)

func (s *Store) DeleteChunks(itemID string) error // usually handled by ON DELETE CASCADE
```

If you implement chunk-index search (sqlite-vec), add:
- `SearchChunkEmbeddings(queryVec, limit, excludeItemID)` returning `(item_id, chunk_id, score)`.

---

## 7) Cost & Performance Analysis

Let:
- Average article length = 500–5000 words
- Target chunk size = 200–300 words
- Tokens per word ≈ 1.2–1.4 (rough estimate; depends on language/content)

### 7.1 Typical article
Example: 2000 words, chunk size 250 words → ~8 chunks  
Tokens ≈ 2000 * 1.3 ≈ 2600 tokens total text embedded (similar to embedding the full article once, but split into chunks).

### 7.2 API calls
- With `EmbedBatch`: **~1 embedding request per article** (batched chunks), best case.
- Without batching: **#chunks requests per article** (worse; avoid if possible).

### 7.3 Mitigations / knobs
- Only chunk-embed when the article is fetched/opened (meets your incremental requirement).
- Cap `MaxChunks` (e.g., 30).
- Background worker pool size 1–2, rate-limited.
- Store `embedding NULL` on failure and retry later (no re-chunking needed).

---

## Recommended Rollout Steps
1. Add `article_chunks` migration + indexes.
2. Implement `Chunker` (paragraph+heading splitter with min/max + overlap).
3. Implement store CRUD and vector serialization helpers.
4. Add background job in coordinator after article cache write.
5. Integrate MLT: hybrid candidate generation + chunk rerank + snippet.
6. Integrate search: chunk-based booster + optional snippet.
7. (Optional) Add `sqlite-vec` for global chunk search at scale.

This design stays incremental, keeps the TUI responsive, and materially improves similarity and search by moving from coarse document embeddings to passage-level semantics.
