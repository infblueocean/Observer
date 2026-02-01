# Brain Trust Prompt — Engineered vs Raw View

Sent to GPT-5, Grok-4, and Gemini-3 on 2026-01-31.

---

You are an expert Go developer specializing in Bubble Tea (charmbracelet/bubbletea) and Lip Gloss for rich terminal UIs. Your goal is to build a beautiful, cyber-noir styled news aggregator TUI with full transparency features.

Project context:
- The app ingests ~4k RSS headlines/day from global sources.
- It uses Jina API for embeddings (semantic clustering) and reranking (jina-reranker-v3).
- Current features: raw chronological feed, basic glitchy headline display.
- New goal: Add two parallel views users can toggle/split:
  1. "Raw" view → pure chronological RSS (no ranking, no weighting).
  2. "Engineered" view → current pipeline (embed → cluster → rerank) with transparency.

Requirements:
- Use Bubble Tea + Lip Gloss + bubbles (for sliders, lists, etc.).
- Cyber-noir aesthetic: dark theme (#0d0d0d bg, neon accents: #00ff9f cyan, #ff0055 pink, #ffff00 yellow), glitch effects on high-arousal headlines.
- Split-screen or tab-toggle between Raw and Engineered views (use tea.KeyMsg for 'r'/'e' toggle or bubbles/tab).
- In Engineered view: every headline has an expandable transparency card (on focus/hover/enter) showing:
  - Semantic match score to query
  - Reranker score
  - Arousal proxy (0–100, 🔥 emoji scale)
  - Recency weight
  - Source diversity boost
  - Final engineered score
- Add a sidebar/collapsible panel with sliders to tweak weighting live:
  - Arousal weight (0–100%)
  - Negativity bias multiplier (0.5×–2.0×)
  - Curiosity-gap boost (0–80%)
  - Recency half-life (2h–72h)
  - When sliders change → instantly re-rank the feed client-side (cache scores, apply weighted sum).
  - Show subtle diff indicators (↑/↓ glow) on reordered items.
- Make it responsive to terminal resize (use tea.WindowSizeMsg).
- Include basic error handling and loading states.
- Use fake/mock data (hardcode 20–30 sample headlines with mock scores) for now — structure so real RSS/Jina integration can plug in later.

Deliverables:
1. Complete main.go with Bubble Tea program.
2. Separate model files if needed (e.g., headline.go, transparency_card.go, sliders_panel.go).
3. Use teatest-friendly structure (testable models).
4. Clean, commented code with good separation of concerns.
5. Cyber-noir glitch effect on headlines (color flicker, scanlines, subtle distortion) — intensity tied to arousal score.

Start by creating a minimal working version with:
- Toggle between Raw/Engineered
- One sample headline with transparency card
- One slider (arousal weight) that re-sorts on change

Then iterate toward full polish.

Please provide:
1. Your architectural recommendation for structuring this Bubble Tea app (model hierarchy, message flow, component boundaries).
2. The key technical challenges you foresee and how to solve them.
3. A concrete implementation plan with file structure.
4. Code for the core model and view logic (main.go + key components).
5. Specific advice on the glitch/cyber-noir visual effects in a terminal context — what's actually achievable vs. gimmicky.
