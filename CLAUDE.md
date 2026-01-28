# CLAUDE.md - Observer Development Notes

This file documents the thinking, decisions, and architecture of Observer as it's being built.

## What is Observer?

Observer is an ambient news aggregation TUI (Terminal User Interface) built with Go and the Charm libraries. It's designed to let you "watch the world go by" - aggregating content from many sources and presenting it in a calm, contemplative interface.

### Core Philosophy (Ethos)

**The Problem:** "I'm so fucking tired of the fucking algorithm behind the curtain deciding what I see."

**The Solution:** Radical transparency and user control.

1. **You Own Your Attention** - No algorithm stands between you and information
2. **Curation By Consent** - Every filter is one YOU chose, visible and adjustable
3. **The Whole Firehose** - You can see everything your sources publish
4. **AI as Tool, Never Master** - AI assists when asked, never decides secretly
5. **Transparency Is Non-Negotiable** - You always know why you're seeing something

### The Vibe

- Calm, not anxious
- Deliberate reading, not passive scrolling
- Completable feeds (infinity is a design flaw)
- Close the app feeling satisfied, not torn away

---

## Architecture

### Project Structure

```
observer/
├── cmd/observer/main.go           # Entry point
├── internal/
│   ├── app/
│   │   ├── app.go                 # Root Bubble Tea model
│   │   └── messages.go            # Message types
│   ├── config/
│   │   └── config.go              # Persistent configuration
│   ├── correlation/               # Story correlation engine
│   │   ├── engine.go              # Core correlation logic
│   │   ├── types.go               # Entity, Cluster, Claim types
│   │   └── cheap.go               # Regex-based extractors
│   ├── curation/
│   │   └── filter.go              # Filter engine (pattern + semantic)
│   ├── embedding/                 # Vector embeddings for semantic dedup (v0.3)
│   │   ├── embedder.go            # Embedder interface + Ollama implementation
│   │   └── dedup.go               # HNSW-based duplicate detection
│   ├── feeds/
│   │   ├── types.go               # Core types (Item, Source)
│   │   ├── sources.go             # 94 RSS feed configs
│   │   ├── aggregator.go          # Smart refresh coordinator
│   │   ├── filter.go              # Ad/spam filtering
│   │   ├── rss/source.go          # RSS fetcher
│   │   ├── hackernews/source.go   # HN API client
│   │   └── usgs/source.go         # Earthquake data
│   ├── rerank/                    # ML-based headline reranking (v0.3)
│   │   ├── reranker.go            # Reranker interface + helpers
│   │   ├── ollama.go              # Ollama reranker implementation
│   │   ├── rubric.go              # Ranking rubrics (topstories, breaking, etc.)
│   │   └── factory.go             # Reranker factory
│   ├── sampling/                  # Pluggable sampling strategies
│   │   ├── queue.go               # SourceQueue with adaptive polling
│   │   ├── manager.go             # QueueManager coordinates sources
│   │   └── round_robin.go         # All sampler implementations
│   ├── store/
│   │   └── sqlite.go              # Persistence layer
│   ├── work/                      # Unified async work pool (v0.3)
│   │   ├── pool.go                # Work pool with event subscription
│   │   ├── types.go               # Work item types (fetch, rerank, analyze)
│   │   └── ring.go                # Ring buffer for history
│   └── ui/
│       ├── stream/model.go        # Main stream view
│       ├── filters/
│       │   ├── model.go           # Filter list UI
│       │   └── workshop.go        # Interactive filter builder
│       ├── workview/model.go      # Work queue visualization (/w)
│       ├── configview/model.go    # Settings UI
│       └── styles/theme.go        # Lip Gloss styles
├── go.mod
├── go.sum
├── CLAUDE.md                      # This file
├── SAMPLING_ARCHITECTURE.md       # Sampling strategies documentation
├── CORRELATION_ENGINE.md          # Story clustering documentation
└── V03_RANKING_PLAN.md            # v0.3 ranking system design
```

### Tech Stack

- **Go** - Language
- **Bubble Tea** - TUI framework (Elm architecture)
- **Lip Gloss** - Styling
- **Bubbles** - UI components (list, textinput, viewport, textarea)
- **gofeed** - RSS parsing
- **SQLite** - Persistence

### Data Flow

```
Sources (RSS, HN API, USGS)
         │
         ▼
   Aggregator (per-source refresh intervals)
         │
         ▼
   Filter Engine (ads, spam, user-defined)
         │
         ▼
   SQLite Store (persistence)
         │
         ▼
   Sampling Layer (balanced exposure)
         │
         ▼
   Stream View (Bubble Tea)
```

### Sampling Architecture (NEW - See SAMPLING_ARCHITECTURE.md)

**Philosophy**: "Firehose to DB, curated to UI"
- Polling stores everything (complete record)
- Sampling controls what appears (balanced exposure)

**Implemented Samplers**:
| Sampler | Purpose | When to Use |
|---------|---------|-------------|
| `RoundRobinSampler` | Simple rotation | Testing, equal representation |
| `DeficitRoundRobinSampler` | Strict long-run fairness | Front page, balanced view |
| `FairRecentSampler` | Quotas + recency | Default stream |
| `ThrottledRecencySampler` | Recency with caps | Breaking news |
| `WeightedRoundRobinSampler` | Editorial control | Curated views |
| `RecencyMergeSampler` | Pure chronological | Firehose view |

**Adaptive Polling**:
- Sources adjust polling interval based on activity
- Found new content → speed up (×0.7, floor 30s)
- No new content → slow down (×1.5, ceiling 15m)

**Brain Trust Consultations** (2026-01-26):
- GPT-5: Deficit Round Robin, MMR re-ranking, exposure caps
- Grok-4: FairRecent pattern, per-source queuing architecture

### Database Schema

SQLite database at `~/.observer/observer.db`:

```sql
-- Feed items
items (id, source_type, source_name, title, summary, url, published_at, read, saved, hidden)

-- Source tracking
sources (name, last_fetched_at, item_count, error_count, last_error)

-- AI analyses
analyses (id, item_id, provider, model, prompt, raw_response, content, error, created_at)

-- Top stories cache (NEW)
top_stories_cache (item_id, title, label, reason, zinger, first_seen, last_seen, hit_count, miss_count)
```

---

## Key Decisions

### 1. Per-Source Refresh Intervals

Not all sources need the same polling frequency:

| Interval | Sources |
|----------|---------|
| 1 min | USGS earthquakes (real-time events) |
| 2 min | Hacker News (fast-moving) |
| 5 min | Wire services (breaking news) |
| 15 min | Tech blogs |
| 30 min | Newspapers |
| 60 min | Longform, academic |

This reduces unnecessary API calls while keeping important sources fresh.

### 2. Two Types of Filters

**Pattern Filters** (instant, no AI cost)
- Regex/keyword matching
- Good for: ads, shopping, known spam
- Run on every item immediately

**Semantic Filters** (AI-powered, batched)
- Natural language criteria
- Good for: clickbait detection, opinion tagging
- Run periodically in background

**Built-in Filter Categories:**

The default filter blocks common promotional content:

| Category | Examples |
|----------|----------|
| Ads | "sponsored", "paid content", "partner content" |
| Financial spam | Credit cards, mortgages, "0% APR" |
| Shopping | "best deals", "flash sale", "% off" |
| Conference promo | "secure your spot", "register now", "webinar:" |
| Event spam | "join us at", "early bird", "call for papers" |

**Design Principle:** Filter promotional content while preserving news ABOUT those topics.
- ❌ Blocks: "Secure Your Spot at RSAC 2026 Conference" (promo)
- ✅ Allows: "RSA Conference Announces New Security Research" (news)

### 3. Source Weights

Each source has a weight (default 1.0):
- Wire services: 1.5 (high signal)
- Regional news: 0.8 (lower priority)

Used for importance scoring and future curated views.

### 4. Interactive Filter Workshop

Users can create filters conversationally:
1. Describe what you want to filter in plain English
2. See a preview of what would be filtered
3. Refine with back-and-forth
4. Commit when happy

This makes AI-powered filtering accessible and transparent.

### 5. Shared Sessions (Future)

Token-based sharing:
- `obs_own_xxx` - Full control (creator)
- `obs_edit_xxx` - Can curate and annotate
- `obs_view_xxx` - Read-only observer
- `obs_tmp_xxx` - 24h expiry

Filters can be shared within sessions.

### 6. Visual Hierarchy (Phases 1-4)

Based on research (Tufte, Gestalt, Information Scent theory), the stream view uses:

**Time Bands**: Items grouped by recency (Just Now, Past Hour, Earlier Today, Yesterday, Older)
- Whitespace between groups, not borders (Gestalt proximity principle)
- Dividers are muted, unobtrusive

**Source Abbreviations**: Recognizable short names (HN, NYT, WaPo, r/ML)
- Better than truncation for scannability

**Breaking News Treatment**: ⚡ indicator + red badge for wire services < 30 min
- Keywords like "BREAKING", "URGENT" also trigger this

**Age-Based Dimming**: Items > 24 hours get muted styling
- Helps focus on fresh content

### 7. Adaptive Density (Phase 4)

Two view modes:
- **Comfortable** (default): Expanded selection with summary, time band dividers, breathing room
- **Compact**: Single line per item, minimal chrome, collapsed read items

Auto-adjusts to compact mode on small terminals (< 30 lines).

Toggle with `v` key or `/density` command.

Status bar shows current mode: ◉ comfortable, ◎ compact

### 8. Source Activity Tracking (Phase 3)

Each source tracks recent activity:
- Count of items in last hour
- Most recent item timestamp

Displayed as heartbeat indicator for active sources (▁▂▃▅▆▇).
Only shows if source has 3+ items in last hour (avoids noise).

### 9. Prediction Market Sparklines (Phase 3)

For prediction market items (Polymarket, Manifold):
- Extracts probability from title/summary (looks for `NN%` pattern)
- Displays as probability bar: `████████████░░░░░░░░ 67%`

Provides at-a-glance market sentiment without clicking through.

### 10. AI Analysis (Brain Trust)

AI-powered news analysis with multiple providers:

**Providers:**
- Ollama (local, fast) - auto-detects available model
- Claude (cloud, quality) - requires API key

**Two-Phase Analysis:**
1. Local model provides quick interim analysis
2. Cloud model replaces with higher quality (if available)

**Top Stories:**
- Press `T` or auto-triggers after feeds load
- Auto-refreshes every 30 seconds
- Progress bar shows countdown: `[━━━━━━╌╌╌╌ 15s]`
- Color gradient: green (fresh) → yellow → red (stale)
- Triggers 5s early for smooth transition

**Breathing Top Stories (Dynamic Section):**

The top stories section expands and contracts based on what's actually happening.
Instead of always showing exactly 3, it shows 2-8 stories based on the news cycle.

**Story Lifecycle (Conservative Labels):**

Local LLMs can be inconsistent, so we use conservative indicators that require confidence:

- ● **NEW**: Single hit - might be noise, neutral styling
- ◐ **EMERGING**: Hit count 2-3, starting to look real
- ◉ **ONGOING**: Hit count 4+, confirmed persistent story
- ★ **MAJOR**: Hit count 6+, high confidence major story (only one with bold)
- ◑ **SUSTAINED**: Was ongoing, missed once, still tracking
- ○ **FADING**: Missed 2+ times, cooling off (dimmed)

**Smart Merging:**
- Current LLM results merged with high-confidence cached stories
- Stories with hit count >= 3 persist even if LLM misses them once
- Fading stories auto-removed after 3 consecutive misses

**Display:**
- `[◉ ONGOING] Title · Source ×5` - Hit count shown for tracked stories
- NEW/EMERGING stories use neutral styling until confidence established
- MAJOR stories (6+ hits) are the only ones with bold styling
- Fading stories are dimmed, no reason line shown
- Duration shown for ongoing stories: `(top for 2h)`

**Breathing Behavior:**
- Slow news day: 2-3 stories
- Active news day: 5-6 stories
- Major breaking event: Can expand to 8

LLM now asked for "all important stories (3-6)" instead of exactly 3.

**Analysis Panel:**
- Shows AI analysis of selected item
- Scrollable content (mouse wheel or indicators)
- Max 33% screen height, capped at 12 lines
- Shows provider name and loading stages
- **Connections section**: When analyzing an item, shows how it connects to current top stories

**Zingers (Local LLM One-Liners):**

After top stories are identified, the local LLM generates punchy one-line summaries:
- Generated asynchronously in background (doesn't block UI)
- Cached and persisted across sessions
- Displayed in blue, more prominent than generic reasons
- Max 15 words, focuses on "why this matters"

Example zingers:
- "Fed rate cut signals recession fears despite strong jobs data"
- "Tesla's robotaxi delay threatens $500B valuation premise"
- "Ukraine's drone strike hits Russian oil refinery 500 miles from front"

**Top Stories Cache Persistence:**

The top stories cache is persisted to SQLite, so the app "picks up where it left off":
- Saved on quit (`q` or `ctrl+c`)
- Loaded on startup
- Auto-prunes entries older than 48 hours
- Only loads entries from last 24 hours

What's persisted:
- ItemID, Title, Label, Reason, Zinger
- FirstSeen, LastSeen timestamps
- HitCount, MissCount

Benefits:
- Zingers don't need regeneration
- Hit counts preserved across sessions
- "Persistent" stories stay persistent
- Quick startup with pre-populated context

**MVC Pattern (Controller Orchestrates Data Flow):**

The analysis feature follows proper MVC:
```
Controller (app.go):
  1. Gets top stories context from brain trust
  2. Passes it to AnalyzeWithContext()
  3. Model doesn't reach into itself for context
```

This keeps the code testable and decoupled.

**Config Options (`~/.observer/config.json`):**
```json
{
  "brain_trust": {
    "enabled": true,
    "auto_analyze": false,
    "dwell_time_ms": 1000,
    "prefer_local": true,
    "local_for_quick_ops": true
  }
}
```

### 11. Smooth Scrolling (Harmonica)

Spring-based physics for smooth cursor movement:
- Frequency: 6.0 (fast response)
- Damping: 0.8 (minimal bounce)
- Updates on each frame tick

### 12. Error Log Pane

Transient error display:
- Shows last 2 errors above command bar
- Fades after 10 seconds of no new errors
- All errors logged to `~/.observer/logs/observer-YYYY-MM-DD.log`

### 13. ML-Based Ranking (v0.3)

Replaces LLM classification with fast ML reranking for top stories and feed ordering.

**Architecture:**
```
Items → Embed (mxbai-embed-large) → HNSW Dedup → Rerank (Qwen3-Reranker) → Feed
```

**Reranker (`internal/rerank/`):**
- Uses Ollama with Qwen3-Reranker-4B model
- Scores headlines against rubric queries (0-10 scale)
- Rubrics: `topstories`, `breaking`, `tech`, `frontpage`
- O(n) scoring, deterministic results

**Rubric Example (topstories):**
```
Most important breaking news and major world developments that informed
citizens should know about right now. Prioritize: geopolitical events,
economic policy, major tech/science breakthroughs, significant legal rulings...
```

**Feed Ordering:**
- Reranker scores all recent items by relevance
- Top 8 go to "Top Stories" section
- Full ranked list orders the main feed
- Time bands (Just Now, Past Hour, etc.) are visual groupings
- Items appear in relevance order within each time band

### 14. Embedding-Based Deduplication (v0.3)

Semantic duplicate detection using vector embeddings.

**Architecture:**
```
Headlines → mxbai-embed-large → 1024-dim vectors → HNSW Index → Cosine Similarity
```

**Components:**
- `internal/embedding/embedder.go` - Ollama embedding client
- `internal/embedding/dedup.go` - HNSW-based dedup index
- Uses `github.com/coder/hnsw` for O(log n) similarity search
- 85% similarity threshold = duplicate

**Flow:**
1. Batch embed new items via Ollama
2. HNSW finds nearest neighbors in O(log n)
3. Group duplicates, track primary (first-seen)
4. Filter to unique items before reranking

**Why not SimHash?**
- SimHash is lexical (misses "Fed raises rates" vs "Federal Reserve increases interest rates")
- Embeddings are semantic (catches same story, different wording)

### 15. Unified Work Pool (v0.3)

All async operations flow through a single work pool for visibility and control.

**Work Types:**
- `TypeFetch` - Feed fetching
- `TypeRerank` - ML reranking
- `TypeAnalyze` - AI analysis
- `TypeEmbed` - Embedding generation

**Features:**
- Event subscription for Bubble Tea integration
- Ring buffer history (last 100 items)
- Filter by type, status
- `/w` command to view work queue

**UI (`/w`):**
```
┌─ WORK QUEUE ─────────────────────────────┐
│ ● Reranking 150 headlines    topstories  │
│ ✓ Fetched Hacker News        12 items    │
│ ✓ Fetched Reuters            8 items     │
│ ○ Pending: Embed batch       45 items    │
└──────────────────────────────────────────┘
```

### 16. Parallel Reranker (v0.4)

Replaced "fake batching" (stuffing N headlines in one prompt) with proper parallel requests.

**Why parallel is better:**
- Ollama's `/api/generate` is one prompt → one response
- Parallel requests let Ollama batch internally on GPU
- Simpler prompts = more reliable score parsing
- One failure doesn't break the whole batch

**Architecture:**
```
200 docs → 200 goroutines → semaphore (32 concurrent) → Ollama GPU batching
```

**Implementation (`internal/rerank/ollama.go`):**
```go
// Semaphore for concurrency control
sem := make(chan struct{}, 32)  // 32 parallel requests

for i, doc := range docs {
    go func(idx int, document string) {
        sem <- struct{}{}        // Acquire
        defer func() { <-sem }() // Release

        score := scoreOne(ctx, query, document)
        scores[idx] = score
    }(i, doc)
}
```

**Per-document prompt:**
```
Rate the relevance of this headline to the topic.
Topic: Most important breaking news...

Headline: Fed Raises Interest Rates by 0.25%

Reply with ONLY a number from 0-10.
Score:
```

**Settings:**
| Setting | Value |
|---------|-------|
| Concurrency | 32 parallel requests |
| Per-request timeout | 30s |
| num_predict | 10 tokens |

### 17. Status Bar Reranking Display (v0.4)

Shows reranking progress in the top status bar with animated spinner, count, and elapsed time.

**Display format:**
```
◉ OBSERVER │ 45 sources │ 12,847 items │ ● ranking 150 [3s]
```

**Components:**
- `●` - Animated spinner (from bubbles)
- `150` - Number of items being reranked
- `[3s]` - Elapsed time since reranking started

**Implementation:**
- `stream.Model.rerankingCount` - Item count
- `stream.Model.rerankingStartTime` - Start timestamp
- `stream.GetRerankingElapsed()` - Returns seconds elapsed
- Count set via work event when `TypeRerank` starts
- Cleared when reranking completes

### 18. Work View Fresh Completions (v0.4)

Fast jobs now stay visible with highlighting for 3 seconds after completion.

**Problem:** Reranking jobs complete so fast you can't see them in `/w`.

**Solution:** Highlight recently completed items (< 3 seconds) in bright green bold.

**Styles:**
```go
// Fresh completion - bright green, stands out
freshCompleteStyle = lipgloss.NewStyle().
    Foreground(lipgloss.Color("#3fb950")).
    Bold(true)
```

**Display:**
- Fresh (< 3s): `[✓] ▲ Reranking 150 headlines  ranked 150 items  just now`
- Older: `[✓] ▲ Reranking 150 headlines  ranked 150 items  5s ago`

---

## Configuration

Config stored at `~/.observer/config.json`:

```json
{
  "models": {
    "claude": { "enabled": true, "api_key": "sk-...", "model": "claude-sonnet-4-20250514" },
    "openai": { "enabled": false, "api_key": "", "model": "gpt-4o" },
    "gemini": { "enabled": false, "api_key": "", "model": "gemini-2.0-flash" },
    "grok": { "enabled": false, "api_key": "", "model": "grok-beta" },
    "ollama": { "enabled": false, "endpoint": "http://localhost:11434", "model": "llama3.2" }
  },
  "mcp_servers": [],
  "ui": {
    "theme": "dark",
    "show_source_panel": false,
    "item_limit": 500
  }
}
```

Keys are auto-populated from:
1. Environment variables (CLAUDE_API_KEY, OPENAI_API_KEY, etc.)
2. `~/src/claude/keys.sh` - source this file before running Observer

**API Keys Location:** `~/src/claude/keys.sh`
```bash
# Expected format in keys.sh:
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
export GOOGLE_API_KEY="..."
export XAI_API_KEY="..."
```

---

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑↓` / `jk` | Navigate |
| `enter` | Mark read |
| `a` | AI analysis of selected item |
| `T` | Analyze top stories (auto-refreshes every 30s) |
| `tab` | Toggle AI analysis panel |
| `s` | Shuffle items |
| `v` | Toggle density (compact/comfortable) |
| `1` | All items (clear time filter) |
| `2` | Just Now (<15 min) |
| `3` | Past Hour |
| `4` | Today (<24h) |
| `5` | Yesterday (24-48h) |
| `f` | Open filter manager |
| `S` | Open source manager |
| `c` | Open config |
| `r` | Refresh due sources |
| `R` | Force refresh all |
| `t` | Toggle source panel |
| `/` | Command mode |
| `q` | Quit |

### Mouse Support

| Action | Result |
|--------|--------|
| Scroll wheel over feed | Navigate up/down (3 items at a time) |
| Scroll wheel over AI panel | Scroll analysis content |

### Commands

| Command | Action |
|---------|--------|
| `/help` | Show help |
| `/shuffle` | Randomize order |
| `/refresh` | Force refresh |
| `/density` | Toggle compact/comfortable view |
| `/filters` | Open filter manager |
| `/sources` | Open source manager |
| `/config` | Open settings |
| `/panel` | Toggle source panel |
| `/w` | View async work queue (fetches, reranking, analysis) |
| `/clearcache` | Clear top stories cache (if data gets corrupted) |

---

## Future Work

### Beautiful Living Feed (DONE)
- [x] Phase 1: Time bands, breathing room, source abbreviations
- [x] Phase 2: Selected item expansion, breaking news, age dimming
- [x] Phase 3: Prediction market sparklines, source activity indicators
- [x] Phase 4: Compact/Comfortable density toggle, auto-adjust on small terminals
- [ ] Entity timelines (requires correlation engine)

### Multi-Source + Persistence (mostly done)
- [x] Feed aggregator with per-source intervals
- [x] SQLite persistence
- [x] Reddit (via public .rss feeds)
- [x] Prediction markets (Polymarket, Manifold)
- [x] Top stories cache persistence (survives restarts)
- [ ] Mastodon/Bluesky

### Brain Trust / AI Analysis (DONE)
- [x] AI provider interface (Provider, Request, Response)
- [x] Ollama integration (local LLM, auto-detects model)
- [x] Claude integration (cloud API)
- [x] Two-phase analysis: fast local → quality cloud
- [x] AI Analysis panel UI with scroll support
- [x] Top Stories auto-analysis (30s refresh with progress bar)
- [x] Mouse scroll over analysis panel
- [x] Breathing top stories (dynamic 2-8 based on news cycle)
- [x] Zingers: local LLM punchy one-liners for top stories
- [x] Analysis includes "Connections" section linking to top stories
- [x] Reason validation (rejects garbage LLM output)
- [ ] Persona system (Historian, Skeptic, Optimist, Connector)

### v0.3 Ranking System (DONE)
- [x] Unified work pool for async operations
- [x] Work queue visualization (`/w` command)
- [x] ML reranker using Qwen3-Reranker-4B via Ollama
- [x] Rubric-based scoring (topstories, breaking, tech, frontpage)
- [x] Embedding-based deduplication (mxbai-embed-large)
- [x] HNSW index for O(log n) similarity search
- [x] Feed ordering by relevance (reranked items first)
- [x] Top stories refresh runs in background (any view)
- [x] Smooth UX during refresh (stories stay visible)
- [x] Status bar indicator for background work

### v0.4 Parallel Reranker (DONE)
- [x] Parallel single-pair requests (32 concurrent goroutines)
- [x] Ollama batches internally on GPU for efficiency
- [x] Simpler per-doc prompt, reliable score parsing
- [x] Status bar shows spinner + count + elapsed time
- [x] Work view highlights fresh completions (< 3s)
- [x] Spinner ticks forwarded properly in work view

### v0.5 Reranking Feed Selection (TODO)
Fix feedback loop bug where reranking selects from already-ranked items.

**Current (broken):**
```
m.stream.GetRecentItems(6)  →  items sorted by PREVIOUS ranking
items[:200]                 →  same "top 200" re-ranked repeatedly
```

**Fixed:**
```
m.aggregator.GetItems()     →  raw items from all sources
filter to last 6 hours      →  recency filter
sort by Published time      →  newest first
items[:200]                 →  NEWEST 200 get ranked
```

- [ ] Get items from aggregator (not pre-sorted stream)
- [ ] Sort by publish time before selecting
- [ ] Ensure new items always get chance to rank
- [ ] Consider: pure Go reranker via hugot/ONNX (no Ollama dependency)

### Correlation Engine (NEW - See CORRELATION_ENGINE.md)
*"Don't curate. Illuminate."*

The correlation engine makes the shape of information visible without deciding what matters.
Full design document: `CORRELATION_ENGINE.md`

**Phase 0: Foundation (No LLM)**
- [ ] Duplicate detection (simhash on titles)
- [ ] Ticker extraction via regex ($AAPL, $TSLA)
- [ ] Country extraction via dictionary
- [ ] Basic "×3" indicator for duplicates

**Phase 1: Entity Index**
- [ ] LLM extraction pipeline (background worker)
- [ ] Entity normalization/deduplication
- [ ] Entity hover cards
- [ ] Entity pages (all mentions of X)

**Phase 2: Story Clusters**
- [ ] Clustering algorithm (entity overlap + similarity)
- [ ] "◐ 5 sources" indicator
- [ ] Inline cluster expansion
- [ ] Cluster velocity tracking

**Phase 3: Disagreement Detection**
- [ ] Claim extraction from text
- [ ] Conflict detection between sources
- [ ] ⚡ indicator for disagreements
- [ ] Side-by-side comparison view

**Phase 4: The Radar**
- [ ] Velocity tracking (what's spiking)
- [ ] Geographic distribution
- [ ] Ambient awareness panel (Ctrl+R)
- [ ] Click-to-filter from radar

**Phase 5: Advanced**
- [ ] "Catch Me Up" briefing flow
- [ ] Prediction tracking (log claims, check outcomes)
- [ ] Living graph visualization
- [ ] Collaborative entity annotation (shared sessions)

### Shared Sessions
- [ ] Token generation & validation
- [ ] S3/R2 sync layer
- [ ] Shared filters & bookmarks
- [ ] Presence indicators
- [ ] WebSocket for real-time
- [ ] Correlation data sharing between users

### Polish (Partial)
- [x] Harmonica smooth scrolling (spring physics)
- [x] Mouse support (scroll wheel)
- [x] Error log pane (fades after 10s)
- [ ] MCP server integration
- [ ] Rate limiting

---

## Running

```bash
# Build
go build -o observer ./cmd/observer

# Run
./observer

# With keys from environment
source ~/src/claude/keys.sh && ./observer
```

---

## Design Principles

1. **Transparency over magic** - Always show why something happened
2. **User control** - Every automatic behavior can be overridden
3. **Calm over urgent** - No anxiety-inducing patterns
4. **Completable** - You can reach the end, be done
5. **AI assists, never decides** - Tools extend your will, not impose their own
