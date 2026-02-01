# Round 3: Architecture Validation & Implementation Roadmap

**Date:** 2026-01-31
**Author:** Observer AI Agent
**Status:** VALIDATED

## 1. Findings: Architecture Alignment

The current state of the `observer` codebase (post-Phase 0 and F7 implementation) provides a superior foundation for the Media View than what was originally envisioned in the Brain Trust Synthesis.

### Key Synergies Identified:
1.  **AppMode State Machine:** The `AppMode` enum (`ModeList`, `ModeSearch`, `ModeResults`) is already implemented. Adding `ModeMedia` is a 1-line change that safely isolates the experimental Media View without impacting core aggregation.
2.  **Structured Telemetry (OTEL):** The existing `internal/otel` package and the `SearchFTS` / `QueryEmbed` events provide the exact data needed for the **Transparency Card**. We can pipe these events directly into the card to show real-time scores (BM25 vs Cosine).
3.  **PTY-E2E Harness:** The newly added `test/e2e` suite (using `creack/pty`) is capable of validating "Engineered" rendering quirks (like the Glitch Engine) by asserting on ANSI escape patterns, which standard unit tests cannot do.

## 2. Updated Critical Path

Based on Round 2 code reviews, the implementation will follow this refined sequence:

### Step 1: Headless Core (`internal/ui/media/`)
-   Implement the `GlitchEngine` using the **FNV-32a** hashing algorithm.
-   **Finding:** Use `0-1` float ranges for all weights to simplify the `Recompute()` math.
-   Create `mock_data.go` with "Arousal Clusters" to prove the visual feedback loop works.

### Step 2: Component Hardening
-   Implement the **Transparency Card** with its dual-state (collapsed/expanded).
-   Implement the **Slider Panel** using `bubbles/progress` with the **Cyan-to-Pink gradient**.

### Step 3: Responsive Orchestration
-   Build `MainModel` with the `92-character` collapse threshold.
-   **Finding:** The sidebar must be completely omitted (not just hidden) when collapsed to prevent grid alignment jitter in small terminals.

### Step 4: Observer Integration
-   Mount `media.MainModel` into `internal/ui/app.go`.
-   Add `M` keybinding to toggle between `ModeList` and `ModeMedia`.

## 3. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| **CPU Spikes** | The 120ms glitch tick will be gated by `a.mode == ModeMedia`. It will never run in the background. |
| **Grid Jitter** | Use `mattn/go-runewidth` for all column calculations to handle double-width Unicode glyphs in the glitch set. |
| **Logic Drift** | The `Weights` struct will be the "Single Source of Truth." Both the Sliders and the Feed will read from the same instance. |

## 4. Conclusion

The implementation is ready to proceed. The combination of GPT-5's structural integrity and Gemini-3's visual polish provides a high-confidence path to a "Cyber-Noir" interface that remains functional and transparent.
