# Synthesis — Engineered vs Raw View

**Date:** 2026-01-31
**Synthesizer:** Claude Opus 4.5
**Method:** ANALYZE → SYNERGIZE → UNIFY across GPT-5, Grok-4, Gemini-3

---

## ANALYZE: Agreements & Disagreements

### All Three Agree On

1. **Model hierarchy**: Root app model composing child models (feed, sidebar/sliders, transparency card). Pure separation of state, update, and view.
2. **Message-driven re-ranking**: Slider changes emit messages → recompute scores → re-sort → update list. No direct coupling.
3. **File structure**: 5-7 files — `main.go`, `app_model.go`, `headline.go`, `styles.go`, `sidebar/sliders.go`, plus view/glitch helpers.
4. **Glitch effects should be subtle**: Tied to arousal score, applied only to visible/focused items, using deterministic pseudo-random per tick. Cap at 2-4 characters distorted per headline.
5. **Animation tick ~100-500ms** to avoid CPU churn. All recommend `tea.Tick`.
6. **`tea.WindowSizeMsg`** for responsive layout with percentage-based splits.
7. **Rank delta tracking** (↑/↓ indicators) by capturing old positions before re-sort.
8. **Mock data first**, structured so real RSS/Jina plugs in later via messages.

### Key Disagreements

| Topic | GPT-5 | Grok-4 | Gemini-3 |
|-------|-------|--------|----------|
| **View toggle** | `r`/`e` keys | `r`/`e` keys | `TAB` key |
| **Sidebar focus** | `TAB` cycles focus | `s` toggles sidebar | `ENTER` cycles focus |
| **List component** | Custom list (no `bubbles/list`) | Uses `bubbles/list` | Custom list with paginator |
| **Slider component** | Uses `bubbles/progress` as visual bar | Uses `bubbles/slider` | Custom Unicode bar (`█░`) |
| **Transparency card placement** | Below the feed list (expandable) | Below focused item in list | Sidebar panel (always visible for selected) |
| **Score formula** | `0.70*Semantic + 0.30*Rerank + arousal*weight` | `0.30*Semantic + 0.30*Rerank + arousal*weight + 0.10*Recency` | `(0.4*Semantic + 0.4*Rerank + arousalBoost + negBoost) * recencyDecay` |
| **Glitch impl** | Full `glitch.go` with per-char FNV hash, glyph substitution + color flicker | Inline style changes + unicode prefix chars | Boolean tick toggle on focused/high-arousal rows |
| **Code completeness** | Most complete — 6 compilable files with proper types | Pseudocode/skeleton — imports don't match, some syntax issues | Single-file, runnable but less modular |

---

## SYNERGIZE: Complementary Strengths

### GPT-5's Strengths

- **Best code quality**: 6 properly structured files that would actually compile. Clean type system (`Headline`, `ScoreBreakdown`, `Weights`). Proper `feedModel` with `SetModePreserveSelection()` (preserves cursor across view switches by ID — nobody else did this).
- **Best glitch implementation**: `glitch.go` with deterministic FNV hashing per `(tick, position, title)`, glyph substitution from a curated set (`/\█▓░⟟⟊⟡`), and per-character color flicker between cyan/pink/yellow. Intensity scales with arousal.
- **Best testability**: Pure `feedModel.Recompute(weights)`, separated `clamp01`, no I/O in models. Explicitly calls out teatest-friendly patterns.
- **Best UX detail**: Loading spinner, `SetModePreserveSelection()`, headline sources like "NightWire" and "CipherPost" that match the cyber-noir aesthetic.

### Grok-4's Strengths

- **Best architecture documentation**: Clear message flow diagrams, component boundary descriptions, and phased implementation plan.
- **Most pragmatic slider design**: Directly references `bubbles/slider` (though note: `bubbles` doesn't actually have a slider component — would need custom or `bubbles/progress`).
- **Good extensibility plan**: Explicit hooks for adding new sliders (`FinalScore()` method on headline) and real data integration.
- **Best challenge/solution table**: Concise mapping of every technical challenge to a concrete solution.

### Gemini-3's Strengths

- **Best scoring algorithm**: Only model to implement exponential recency decay (`math.Pow(0.5, hoursOld/halfLife)`) and negativity bias as a multiplicative factor. Most realistic production ranking formula.
- **Best sidebar rendering**: Custom Unicode bar chart (`█░`) with active indicator (`▶`/`▷`), proper focus highlighting. Visual rendering is the most polished.
- **Best transparency card design**: Horizontal bar charts per metric with color coding (VECTORS section in cyan, PSYCH section in pink/yellow). Final score rendered as an inverted badge. The most visually informative.
- **Only one with proper debounce acknowledgment**: Notes slider debouncing as important for production with 4k items.
- **Simplest to run**: Single file, `go run main.go`.

---

## UNIFY: Authoritative Recommendation

### Architecture (adopt GPT-5's structure with Gemini-3's scoring)

**File structure:**
```
observer-ui/
├── main.go              // Entry point: tea.NewProgram
├── app_model.go         // Root model: mode, focus, tick, window size
├── feed_model.go        // Headlines, dual orderings, selection, recompute
├── slider_panel.go      // Weight sliders using bubbles/progress for bar rendering
├── transparency_card.go // Expandable card view (GPT-5 placement: below feed)
├── headline.go          // Types: Headline, ScoreBreakdown, Weights
├── glitch.go            // Deterministic per-char distortion (GPT-5's FNV approach)
└── styles.go            // Lip Gloss cyber-noir theme
```

### Key Design Decisions

1. **View toggle: `r`/`e` keys** (GPT-5 + Grok-4). TAB is better reserved for focus cycling between feed and sidebar, which is more frequent.

2. **Focus cycling: `TAB`** (GPT-5). Clean and standard — TAB moves between feed ↔ sidebar. Arrow keys navigate within the focused panel.

3. **Transparency card: Below feed, expandable on Enter** (GPT-5). Better than sidebar placement (Gemini-3) because:
   - Sidebar space is already consumed by sliders
   - Card can use full feed width for detailed metrics
   - Toggle on/off avoids constant visual noise

4. **Scoring formula: Gemini-3's approach**, extended:
   ```go
   base := 0.4*Semantic + 0.4*Rerank
   arousalBoost := Arousal * (ArousalWeight / 100.0)
   negBoost := max(0, (Negativity - 0.5)) * (NegativityBias - 1.0)
   recencyDecay := math.Pow(0.5, hoursOld / RecencyHalfLife)
   FinalScore = (base + arousalBoost + negBoost) * recencyDecay
   ```
   This is the most production-realistic formula. GPT-5's linear formula is too simple; Grok-4's is slightly better but lacks decay.

5. **Glitch rendering: GPT-5's `glitch.go`**, which is the only implementation that actually works at per-character granularity with proper determinism. Adopt the FNV hash approach, glyph substitution set, and arousal-scaled intensity.

6. **Slider rendering: Hybrid** — Use `bubbles/progress` for the bar visual (GPT-5), but adopt Gemini-3's sidebar layout with the `▶`/`▷` active indicator and `█░` Unicode aesthetic.

7. **Rank delta: GPT-5's implementation** — Track `oldRank` map before sort, compute `RankDelta = oldIndex - newIndex`. Display with colored ↑/↓ indicators.

8. **Selection preservation: GPT-5's `SetModePreserveSelection()`** — When switching between Raw/Engineered, find the same headline by ID in the new ordering. Nobody else implemented this and it's critical for UX.

9. **Responsiveness: All agree** — `tea.WindowSizeMsg` → recalculate widths. Main feed gets ~70%, sidebar ~30%. Collapse sidebar below threshold width.

### Implementation Order

**Phase 1 — Minimal working:**
- GPT-5's 6-file structure as the skeleton
- 5 mock headlines with cyber-noir themed sources
- `r`/`e` toggle between Raw (chronological) and Engineered (scored)
- One slider (arousal weight) with `bubbles/progress` bar
- Basic transparency card on Enter
- Rank delta ↑/↓ indicators
- Loading state with spinner

**Phase 2 — Full sliders + scoring:**
- Add all 4 sliders: arousal, negativity bias, curiosity gap, recency half-life
- Implement Gemini-3's exponential recency decay formula
- 30 mock headlines
- Gemini-3's sidebar visual polish (active indicators, section dividers)

**Phase 3 — Glitch + polish:**
- GPT-5's `glitch.go` with FNV-based per-char distortion
- Animation tick at 120ms
- Scanline effect (alternating subtle background rows)
- Terminal-size collapse behavior

**Phase 4 — Real integration:**
- Replace mock data with Observer's `LoadItems` / `LoadSearchPool` callbacks
- Wire Jina embedding/reranking scores into `ScoreBreakdown`
- Connect to existing `internal/filter` for dedup/reranking

### What to Avoid

- Don't use `bubbles/list` (Grok-4) — it adds complexity for custom rendering (glitch effects, transparency cards, rank deltas) without benefit. A simple slice + manual viewport is better.
- Don't use `bubbles/slider` (Grok-4) — it doesn't exist in bubbles. Use `bubbles/progress` for the bar visual with custom key handling.
- Don't put the transparency card in the sidebar (Gemini-3) — it competes with sliders for limited space.
- Don't animate the entire list (Grok-4 mentions matrix rain) — only glitch visible high-arousal rows and the focused item.

### Cyber-Noir Theme Constants

```go
ColorBG     = "#0d0d0d"  // Near-black (not pure black — softer CRT feel)
ColorCyan   = "#00ff9f"  // Neon cyan (primary accent)
ColorPink   = "#ff0055"  // Neon pink (warnings, high arousal)
ColorYellow = "#ffff00"  // Neon yellow (active selection, alerts)
ColorDim    = "#3a3a3a"  // Dim grey (inactive, hints)
ColorText   = "#d6d6d6"  // Light grey (body text)
```

### Glitch Effect Rules

1. Only in Engineered mode
2. Only on high-arousal items (>55%) and the focused item
3. Deterministic per `(tick, charPosition, titleHash)` via FNV-32a
4. Max 2-4 characters distorted per headline per frame
5. Glyph substitution set: `/\█▓░⟟⟊⟡`
6. Color flicker between cyan/pink/yellow per character
7. Tick interval: 120ms
8. Intensity scales linearly with arousal (55% threshold, full at 100%)
