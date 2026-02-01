# Round 2 Synthesis — Implementation Details

**Date:** 2026-01-31
**Synthesizer:** Claude Opus 4.5
**Method:** ANALYZE → SYNERGIZE → UNIFY across GPT-5, Grok-4 (truncated), Gemini-3

**Note:** Grok-4's response was truncated at 431 chars (only partial `headline.go` types received). Synthesis is based primarily on GPT-5 and Gemini-3.

---

## ANALYZE: Comparing Implementations

### Response Completeness

| Model | Files Delivered | Lines of Code | Status |
|-------|----------------|---------------|--------|
| GPT-5 | 7 files (all requested) | ~1,200 | Complete, compilable |
| Grok-4 | 1 partial file | ~15 | Truncated — unusable |
| Gemini-3 | 8 files (7 + mock_data.go) | ~800 | Complete, compilable |

### Type System Comparison

| Aspect | GPT-5 | Gemini-3 |
|--------|-------|----------|
| Headline ID | `string` | `string` |
| Arousal range | `0-100 float64` | `0-1 float64` |
| Breakdown struct | 7 fields (Semantic, Rerank, Arousal, RecencyDecay, Diversity, NegBoost, FinalScore) | 5 fields (BaseScore, ArousalBoost, Negativity, RecencyDecay, FinalScore) |
| Weights struct | 4 fields with `RecencyHalfLifeHours` | 4 fields with `RecencyHalfLife` |
| Title hash | Pre-computed `TitleHash uint32` via `EnsureHash()` | Hash computed inline in `Glitchify()` |

### Feed Model Comparison

| Feature | GPT-5 | Gemini-3 |
|---------|-------|----------|
| Viewport | Full viewport with `VisibleRange()`, `Page()`, `JumpHome()`, `JumpEnd()` | Basic offset tracking, cursor-follows-viewport |
| Column layout | Fixed columns: rank(4) + delta(6) + title(dynamic) + meta(14) | Simple concatenation |
| Age display | Smart: `3m` / `2.1h` / `3d` based on magnitude | Raw `hours` display |
| Row rendering | Uses `go-runewidth` for proper Unicode width | Basic string formatting |
| Performance note | Only renders `VisibleRange()` rows | Same approach via offset/height |

### Slider Panel Comparison

| Feature | GPT-5 | Gemini-3 |
|---------|-------|----------|
| Step sizes | Arousal ±5, Neg ±0.10, Curiosity ±5, Recency ±2h | Same |
| Visual | `▶`/`▷` indicators, `bubbles/progress` bar | `▶` indicator, `bubbles/progress` bar with gradient |
| Rounding | `roundTo()` for float precision | Direct math.Min/Max |
| Message type | `weightsChangedMsg` (unexported) | `WeightsChangedMsg` (exported) |

### Glitch Engine Comparison

| Feature | GPT-5 | Gemini-3 |
|---------|-------|----------|
| Hash input | `fnv32aTriplet(tick, position, titleHash)` — pre-computed hash | `fnv.New32a()` with `id + tick + position` bytes |
| Glyph set | `/ \ █ ▓ ░ ⟟ ⟊ ⟡` (8 glyphs) | `# ░ ▒ ▓ ? ! $ & X Y Z 0 1` (13 glyphs) |
| Color distribution | 55% cyan, 25% pink, 20% yellow | Equal 33% each, but only 5% chance to color |
| Arousal threshold | 0.55 (on 0-1 scale) | 0.55 (on 0-1 scale) |
| Intensity formula | `(arousal - 0.55) / 0.45` → distortion chance | `(arousal - 0.55) / 0.45` → probability threshold |
| Per-char or per-row | Per-character with legibility preservation (30% keep alphanumerics) | Per-character with probability-based substitution |

### Transparency Card Comparison

| Feature | GPT-5 | Gemini-3 |
|---------|-------|----------|
| Collapsed state | Shows summary line: "Semantic 0.62 Rerank 0.58 Final 0.437" | Not implemented (always expanded when open) |
| Bar rendering | Custom `renderBar()` with `█` fill + `░` empty | Custom bars with `━` character |
| Fire meter | `🔥` × (arousal × 5) mapped to 0-5 flames | `🔥🔥🔥` / `🔥` based on thresholds |
| Metrics shown | 6: Semantic, Reranker, Arousal, Recency, Diversity, Final | 6: Semantic, Rerank, Arousal, Negativity, Recency, Final (with equation) |

### Layout & Integration Comparison

| Feature | GPT-5 | Gemini-3 |
|---------|-------|----------|
| Sidebar width | Fixed 36 chars, collapses below 92 total | Fixed 40 chars, or 30% on small screens |
| Min feed width | 44 chars | Not specified |
| Integration design | Documented as mountable `tea.Model` with `Config` struct + `Now()` callback | Notes on dependency injection, suggests wrapping |
| Styles | Full `Styles` struct with 20+ named styles, `DefaultStyles()` factory | Global `var` styles |
| Mock data | 28 headlines with curated cyber-noir sources (NightWire, CipherPost, etc.) | 25 headlines with cyber-noir titles |

---

## SYNERGIZE: Best of Each

### From GPT-5 (Primary Implementation):
- **Superior feed model**: `VisibleRange()`, `Page()`, `JumpHome()`, `JumpEnd()`, smart age display, `go-runewidth` for Unicode
- **Better integration design**: `Config` struct with `Now()` callback and optional `Styles` override — clean dependency injection for Observer
- **Styles architecture**: Named `Styles` struct instead of global vars — testable and overridable
- **Collapsed card state**: Summary line when collapsed is much better UX than binary show/hide
- **Glitch legibility**: 30% chance to keep alphanumeric characters preserves readability
- **Pre-computed title hash**: `EnsureHash()` avoids re-hashing every tick

### From Gemini-3 (Supplementary):
- **Cleaner type naming**: `ArousalRaw` (0-1) vs GPT-5's `Arousal` (0-100) — 0-1 is better for internal computation, display can multiply by 100
- **Progress bar gradient**: `progress.WithGradient(ColorCyan, ColorPink)` — more visually appealing than default
- **Mock data clustering**: Every 5th item has high arousal+negativity — creates realistic clusters for testing
- **Simpler glitch glyph set**: More characters gives more visual variety
- **Explicit mock_data.go separation**: Clean file boundary

---

## UNIFY: Recommended Implementation

### Use GPT-5's code as the primary base, with these Gemini-3 enhancements:

1. **Arousal scale**: Use 0-1 internally (Gemini-3), multiply by 100 only for display. Avoids `/100.0` scattered through code.

2. **File structure** (7 + 1 files):
   ```
   internal/ui/media/
   ├── main_model.go        // GPT-5's version (Config struct, mountable tea.Model)
   ├── feed_model.go        // GPT-5's version (VisibleRange, Page, JumpHome/End)
   ├── slider_panel.go      // GPT-5's version + Gemini-3's gradient styling
   ├── transparency_card.go // GPT-5's version (collapsed summary + expanded detail)
   ├── headline.go          // GPT-5's types with Gemini-3's 0-1 arousal scale
   ├── glitch.go            // GPT-5's version (pre-computed hash, legibility preservation)
   ├── styles.go            // GPT-5's Styles struct approach
   └── mock_data.go         // Gemini-3's file + GPT-5's curated headlines/sources
   ```

3. **Specific merges**:
   - `headline.go`: Use GPT-5's `Breakdown` struct (7 fields including Diversity) but with 0-1 arousal range
   - `glitch.go`: Use GPT-5's `fnv32aTriplet` + `selectGlitchRune` with the larger glyph set from Gemini-3: `# / \ █ ▓ ░ ⟟ ⟊ ⟡ ? ! $ &`
   - `slider_panel.go`: Use GPT-5's `▶`/`▷` rendering + Gemini-3's `progress.WithGradient(ColorCyan, ColorPink)`
   - `mock_data.go`: Combine GPT-5's curated headlines/sources with Gemini-3's clustering pattern (every 5th item high-arousal)
   - `main_model.go`: GPT-5's version is strictly superior (Config, applyLayout, sidebar collapse logic, help bar)

4. **Integration with Observer**: Mount as a sub-model per GPT-5's documentation:
   ```go
   // In Observer's App model:
   type App struct {
       // existing fields...
       mediaView media.MainModel
       showMedia bool
   }

   // Switch to media view:
   app.mediaView = media.NewMainModel(media.Config{
       Now:       time.Now,
       Headlines: convertItems(store.LoadItems()),
   })
   app.showMedia = true
   ```

### Key Implementation Notes:
- **Messages are unexported** (`tickMsg`, `weightsChangedMsg`) — they don't leak outside the package
- **120ms tick** only runs when media view is active — parent should stop/start it
- **Sidebar collapses** at width < 92 chars, feed gets full width
- **Card has two states**: collapsed (1-line summary) and expanded (6 metric bars)
- **`go-runewidth`** dependency required for proper terminal width calculation
