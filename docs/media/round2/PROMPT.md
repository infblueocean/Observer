# Brain Trust Round 2 Prompt — Implementation Details

Sent to GPT-5, Grok-4, and Gemini-3 on 2026-01-31.

This round provided the unified architecture decisions from Round 1 and asked for detailed, production-ready, compilable Go code for all 7 files in the media view.

---

See full prompt text in the scratchpad: `braintrust_round2.txt`

Key sections requested:
1. Complete Go type definitions for ALL structs
2. Full message type catalog
3. Init() → Update() → View() flow
4. Integration with existing Observer App
5. Feed model internals (dual ordering, Recompute, viewport, selection preservation)
6. Slider panel (all 4 sliders, key handling, message propagation)
7. Transparency card (metrics, bar charts, expand/collapse)
8. Glitch engine (FNV-32a, glyph substitution, color flicker)
9. Responsive layout (sidebar collapse, width thresholds)
10. Mock data (25-30 cyber-noir headlines with realistic score distributions)
