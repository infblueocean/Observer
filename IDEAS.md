# IDEAS.md - Observer Feature Ideas & Future Directions

This document captures ideas as they emerge. Some are implemented, some are planned, some are wild explorations.

---

## Implemented

### Core
- [x] 97+ RSS feed sources with per-source refresh intervals
- [x] Hacker News API (richer than RSS)
- [x] USGS Earthquake data (real-time world events)
- [x] SQLite persistence
- [x] Pattern-based ad/spam filtering
- [x] Dark ambient theme

### Prediction Markets
- [x] Polymarket integration (real-money markets)
- [x] Manifold integration (play-money markets)

### Configuration
- [x] Config UI for AI model API keys
- [x] Auto-populate keys from environment/keys.sh

---

## In Progress

### Semantic Filters
- [x] Filter engine with pattern + semantic types
- [x] Interactive filter workshop (conversational creation)
- [ ] Wire up AI evaluation for semantic filters

### Semantic Alerts
- [x] Alert engine with pattern, threshold, correlation types
- [ ] UI for managing alerts
- [ ] Visual indicators for triggered alerts
- [ ] Sound notifications

---

## Planned

### Brain Trust (AI Personas)
When you select an item, the "brain trust" analyzes it:

| Persona | Perspective |
|---------|-------------|
| **The Historian** | Historical context, patterns, precedents |
| **The Skeptic** | What's missing? Other side? Questions to ask? |
| **The Optimist** | Silver linings, opportunities |
| **The Connector** | Related events, trends, connections |

**Flow:**
1. User dwells on item for 500ms
2. Background: parallel requests to AI (one per persona)
3. Responses stream into brain trust panel
4. User can expand any perspective
5. Deep-dive: full chat with that persona's angle

### Shared Sessions
Token-based collaborative viewing:

```
obs_own_xxx   - Full control (creator)
obs_edit_xxx  - Can curate and annotate
obs_view_xxx  - Read-only observer
obs_tmp_xxx   - 24h expiry (quick shares)
```

**Features:**
- Shared feed curation
- Shared filters (toggle share/private)
- Shared bookmarks
- Presence indicators ("3 observers online")
- "Follow me" mode (one person drives)
- Annotations on articles
- Emoji reactions

**Sync Strategy:**
1. MVP: S3/R2 polling (simple, works offline)
2. V1: WebSocket for presence + reactions
3. V2: CRDT for annotations (Yjs)

### News ↔ Prediction Market Matching
Automatically link news stories to relevant prediction markets:

```
┌─────────────────────────────────────────────────────────────┐
│ Reuters: "Fed signals rate cuts coming in Q2"               │
├─────────────────────────────────────────────────────────────┤
│ 📊 Related Markets:                                         │
│   • Fed rate cut by March?     67% (+12% today) [Kalshi]   │
│   • Recession in 2025?         23% (-5% week)   [Polymarket]│
└─────────────────────────────────────────────────────────────┘
```

**Approaches:**
1. Adjacent News API (already does this!)
2. Entity extraction + market search
3. Embedding similarity search

### User Prediction Tracking
Let users make their own predictions:

```go
type UserPrediction struct {
    UserID      string
    NewsItemID  string
    MarketID    string    // optional - linked market
    Probability float64   // user's prediction (0-100)
    CreatedAt   time.Time
    ResolvedAt  time.Time // when we know outcome
    Outcome     bool      // what actually happened
    MarketProb  float64   // what market said at time of prediction
}
```

**Features:**
- Track your forecasting accuracy (Brier score)
- Compare yourself to markets
- Calibration charts
- Prediction history
- Gamification: streaks, badges

---

## Role-Based Personas (Curation Lens)

Different from Brain Trust (which analyzes selected items). This is about **who you are** shaping what you see.

**Concept:**
```
"I'm a project manager. Show me what matters to ME."
"I'm a security researcher. Flag vulnerabilities."
"I'm a startup founder. Boost funding/VC news."
```

**Implementation Approaches:**

1. **Pre-built Role Personas**
   - Project Manager: deadlines, team dynamics, productivity tools
   - Security Researcher: CVEs, breaches, vulnerabilities, threat intel
   - Startup Founder: funding rounds, VC moves, market trends
   - Data Scientist: ML papers, tools, datasets, benchmarks
   - Policy Wonk: legislation, regulations, political analysis

2. **Custom Role Definition**
   ```
   /persona create "Hardware Engineer"
   > "I design embedded systems. Highlight news about:
      semiconductors, chip manufacturing, RISC-V, embedded
      Linux, IoT security, supply chain issues."
   ```

3. **Role as Semantic Filter**
   - Each role is essentially a boost/dim filter
   - "Relevant to my role" → boost
   - "Irrelevant" → dim (not hide - you still see everything)

4. **Multi-Role Support**
   - People wear many hats
   - "Project Manager" during work hours
   - "Space Enthusiast" evenings/weekends
   - Time-based persona switching?

**Tie-in with everything-claude-code:**
- Could import persona definitions from that repo
- Battle-tested prompts for different roles
- Community-contributed personas

**Key Insight:**
This is curation by consent - you explicitly say "I am X, show me X-relevant things" rather than an algorithm inferring it from your behavior.

---

## Wild Ideas

### Serendipity Engine
Connect strangers reading the same article:

```
You're reading about AI regulation
Someone else is reading about AI regulation
           ↓
"Someone else is curious about this too"
           ↓
       [Merge sessions?]
           ↓
Strangers become co-observers
```

Opt-in serendipitous connection based on reading overlap.

### Controversy Radar
Detect when different communities see the same story differently:

```
Session A (liberal-leaning) annotates article one way
Session B (conservative-leaning) annotates differently
                    ↓
    "This story is divisive"
    [See the other perspective?]
```

Bridge bubbles without forcing interaction.

### Dead Drops
Leave notes "pinned" to articles:
- Anyone who reads that article later finds them
- Decays over time (graffiti on the news)
- Creates asynchronous conversation with past/future readers

### AI Participants in Sessions
Personas join as "participants":
- Proactively surface: "This relates to what you discussed yesterday"
- Can be @mentioned: "@Skeptic what do you think of Alice's point?"
- Blurs line between tool and collaborator

### Time-Shifted Watch Parties
- Record a session's "journey" through the news
- Play it back later
- "Watch election night WITH the Observer team" (async)
- Commentary track over news exploration

### Slow News Mode
Anti-doomscroll:
- Updates only once per day
- Enforced delay on everything
- Collaborative curation of "what actually mattered today"
- The infinite scroll detox

### Voice/Audio Rooms
```
Same article on everyone's screen
Voice chat for discussion
AI personas as "call-in experts"
```
Clubhouse meets news.

### Physical Artifact Integration
- Print a "daily digest" of session highlights
- QR code links back to shared session
- Bring digital river into physical morning routine

### The Infinite Scroll Detox
```
[Ambient Mode: ON]

You've been reading for 45 minutes.
Your session companions have moved on.

       [Take a break?]
[See what your friends bookmarked]
```
Social accountability for healthier consumption.

---

## Correlation Engine (First-Class Citizen)

**Core Insight:** The persistence layer isn't just storage - it's **memory**. Connect the now to the past.

### Entity Extraction
Pull entities from every item:
- People (Elon Musk, Biden)
- Organizations (OpenAI, Fed)
- Locations (Ukraine, Silicon Valley)
- Topics (AI regulation, climate)
- Tickers ($TSLA, $BTC)
- Events (2024 Election, COP28)

### Correlation Types
| Type | Description |
|------|-------------|
| **Entity** | Same person/company appearing over time |
| **Topic** | Same theme evolving |
| **Prediction** | Market predicted → outcome happened |
| **Coverage** | Multiple sources on same story |
| **Causal** | Cause → effect relationships |
| **Historical** | "This is like what happened in 2008" |

### Threads
Ongoing stories that span multiple items:
```
Thread: "OpenAI Leadership Drama"
├── Nov 17: "Board fires Sam Altman"
├── Nov 18: "Employees threaten to quit"
├── Nov 19: "Microsoft offers jobs"
├── Nov 21: "Altman returns as CEO"
└── Ongoing: Related coverage
```

### UI Concept
When viewing an item:
```
┌─────────────────────────────────────────────────┐
│ Fed announces rate hold                         │
├─────────────────────────────────────────────────┤
│ 🔗 Related (from your history):                 │
│   • 3 weeks ago: "Fed hints at cuts"            │
│   • 2 months ago: "Inflation drops to 3%"       │
│   • 6 months ago: "Markets bet on rate pause"   │
│                                                 │
│ 📊 Prediction Markets:                          │
│   • "Rate cut by March" jumped 67% → 45%        │
│                                                 │
│ 📈 Entity Timeline: [Federal Reserve]           │
│   47 mentions over 3 months                     │
└─────────────────────────────────────────────────┘
```

### The Historian Persona
Brain Trust's Historian is powered by correlation engine:
- "This is similar to the 2008 financial crisis because..."
- "The last time this company had leadership issues was..."
- "This topic has been building for 3 months..."

---

## Translation & Normalization

**Goal:** Digest sources from around the world, normalize to user's language.

### Why This Matters
- Most news happens outside English-speaking world
- Direct sources > translated summaries
- See how same story is covered differently globally

### Sources Unlocked
| Region | Sources |
|--------|---------|
| China | Xinhua, Global Times, SCMP |
| Russia | TASS, Interfax |
| Middle East | Al Jazeera Arabic, Haaretz |
| Europe | Le Monde, Der Spiegel, Corriere |
| Japan | NHK, Asahi, Nikkei |
| Brazil | Folha, O Globo |
| India | Hindi outlets |

### Implementation Options

1. **MCP Translation Server**
   - Local: Ollama with translation model
   - API: DeepL, Google Translate, Azure
   - Hybrid: Local for speed, API for quality

2. **On-Demand vs Pre-Translate**
   - Pre-translate: Everything indexed in English
   - On-demand: Translate when user views
   - Hybrid: Pre-translate titles, on-demand for content

3. **Normalization**
   - Dates/times to user's timezone
   - Currency to user's preferred
   - Measurements (metric/imperial)
   - Cultural context notes

### Correlation Across Languages
Same event covered in different languages:
```
Thread: "Taiwan Strait Tensions"
├── Reuters (EN): "US warship transits Taiwan Strait"
├── Xinhua (ZH→EN): "China condemns provocation"
├── NHK (JA→EN): "Japan monitoring situation"
└── Taiwan News (EN): "Coast guard on alert"
```

See how different regions frame the same story.

---

## Data Sources to Add

### High Priority
- [ ] Reddit API (OAuth required, 100 req/min)
- [ ] Mastodon public timeline (no auth!)
- [ ] Bluesky public API
- [ ] ArXiv (academic papers)
- [ ] SEC EDGAR (corporate filings)
- [ ] Wikipedia EventStreams (real-time edits)

### Medium Priority
- [ ] Kalshi prediction markets (CFTC-regulated)
- [ ] Metaculus (long-term forecasts)
- [ ] Congress.gov (legislation)
- [ ] NOAA weather alerts
- [ ] NASA APIs

### Lower Priority / Specialized
- [ ] CoinGecko (crypto prices)
- [ ] Alpha Vantage (stock data)
- [ ] OpenSky (flight tracking)
- [ ] SpaceX API (launches)

---

## Technical Ideas

### MCP Server Integration
Connect to Model Context Protocol servers for:
- File system access
- Database connections
- Custom tools for AI
- External service integrations

### Corroboration Scoring
Track when multiple sources cover the same story:

```go
type Item struct {
    // ...
    SeenIn        []string // ["Reuters", "CNN", "BBC"]
    Corroboration int      // Number of sources
}
```

If Reuters + BBC + CNN all cover it → higher importance.

### Importance Scoring
Combine signals for "big happenings" detection:
- Source weight (wire services > blogs)
- Corroboration (multiple sources)
- Engagement (HN score, Reddit upvotes)
- Velocity (how fast it's spreading)
- Prediction market movement (related markets spiking)

### Completable Feeds
"Infinity is a design flaw"

- Track read state across sessions
- "You've seen everything from the last 4 hours"
- Clear "done" state
- No anxiety about missing things

---

## Monetization (If Ever)

### Freemium Tiers
```
FREE: Solo Observer
- Personal use, local persistence
- Limited AI persona calls/day
- No shared sessions

$5/mo: Observer+
- Unlimited AI personas
- Shared sessions (up to 5 participants)
- 30-day history

$15/mo: Observer Team
- Unlimited participants
- Admin tools, moderation
- API access
- Unlimited history, export

Enterprise: Observer Newsroom
- SSO, compliance, audit logs
- Custom AI persona training
- On-prem option
```

### Creator Monetization
- Curators with great taste charge for access
- "Subscribe to @techmeme's curated Observer for $3/mo"
- Platform takes 20%

### Data Insights (Aggregated, Anonymized)
- "What's trending across all Observer sessions?"
- Sell trend data to researchers
- Strict privacy: patterns only, no individual data

---

## Philosophy Reminders

From the ethos:

1. **You own your attention** - No algorithm without consent
2. **Curation by consent** - Every filter is visible and adjustable
3. **AI as tool, never master** - Assists when asked, never decides secretly
4. **Transparency is non-negotiable** - Always know why you're seeing something
5. **Calm is a feature** - No anxiety-inducing patterns
6. **Feeds should be completable** - Infinity is a design flaw
7. **The goal is to close the app satisfied** - Not to keep it open forever

---

*Last updated: Session with Claude, building Observer*
