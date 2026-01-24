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
│   ├── curation/
│   │   └── filter.go              # Filter engine (pattern + semantic)
│   ├── feeds/
│   │   ├── types.go               # Core types (Item, Source)
│   │   ├── sources.go             # 94 RSS feed configs
│   │   ├── aggregator.go          # Smart refresh coordinator
│   │   ├── filter.go              # Ad/spam filtering
│   │   ├── rss/source.go          # RSS fetcher
│   │   ├── hackernews/source.go   # HN API client
│   │   └── usgs/source.go         # Earthquake data
│   ├── store/
│   │   └── sqlite.go              # Persistence layer
│   └── ui/
│       ├── stream/model.go        # Main stream view
│       ├── filters/
│       │   ├── model.go           # Filter list UI
│       │   └── workshop.go        # Interactive filter builder
│       ├── configview/model.go    # Settings UI
│       └── styles/theme.go        # Lip Gloss styles
├── go.mod
├── go.sum
└── CLAUDE.md                      # This file
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
   Stream View (Bubble Tea)
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
2. keys.sh file if available

---

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑↓` / `jk` | Navigate |
| `enter` | Mark read |
| `s` | Shuffle items |
| `f` | Open filter manager |
| `c` | Open config |
| `r` | Refresh due sources |
| `R` | Force refresh all |
| `t` | Toggle source panel |
| `/` | Command mode |
| `q` | Quit |

### Commands

| Command | Action |
|---------|--------|
| `/shuffle` | Randomize order |
| `/refresh` | Force refresh |
| `/filter` | Open filter manager |
| `/config` | Open settings |
| `/sources` | Toggle source panel |

---

## Future Work

### Phase 2: Multi-Source + Persistence (mostly done)
- [x] Feed aggregator with per-source intervals
- [x] SQLite persistence
- [ ] Reddit API
- [ ] Mastodon/Bluesky

### Phase 3: Brain Trust
- [ ] AI provider interface
- [ ] Ollama + Claude integration
- [ ] Persona system (Historian, Skeptic, Optimist, Connector)
- [ ] Brain trust panel UI
- [ ] Background analysis on item selection

### Phase 4: Shared Sessions
- [ ] Token generation & validation
- [ ] S3/R2 sync layer
- [ ] Shared filters & bookmarks
- [ ] Presence indicators
- [ ] WebSocket for real-time

### Phase 5: Polish
- [ ] Animations (Harmonica)
- [ ] MCP server integration
- [ ] Better error handling
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
