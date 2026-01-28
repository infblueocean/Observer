# IDEAS.md - Observer Feature Ideas & Future Directions

This document captures ideas as they emerge. Some are implemented, some are planned, some are wild explorations.

---

## Beautiful Living Feed - Design Research & Vision

### The Problem (Current State)
The current stream is a "wall of text" - high information density but no visual hierarchy, no breathing room, no rhythm. Every item looks equally important. The eye has nowhere to rest.

### Research Foundation

#### Edward Tufte's Data Visualization Principles
Source: [Tufte's Principles](https://thedoublethink.com/tuftes-principles-for-visualizing-quantitative-information/)

- **"Above all else, show the data"** - Don't let decoration obscure information
- **Data-ink ratio** - Maximize the ink devoted to actual data vs decoration
- **High data density is OK** - "Our eyes and brains are capable of processing large amounts of information if presented clearly"
- **Chartjunk** - Eliminate useless, non-informative visual elements
- **Small multiples** - Miniature illustrations arrayed as single figure
- **Sparklines** - "Data-intense, design-simple, word-sized graphics"

**Application to Observer:** We CAN show dense information, but it must be organized. Every pixel should serve a purpose. No decoration for decoration's sake.

#### Cognitive Load & Eye Tracking Research
Source: [Eye Tracking for Cognitive Load](https://dl.acm.org/doi/10.1145/2993901.2993908), [NN/g Eye Tracking](https://www.interaction-design.org/literature/topics/eye-tracking)

- **Pupil dilation** indicates cognitive effort - complex UIs cause measurable strain
- **Fixations** - where eyes stop and focus; good design guides fixations
- **Saccades** - rapid eye movements between fixations; chaotic saccades = poor hierarchy
- **Key finding:** "Well-designed interfaces feature high visual contrast, intuitive iconography, clear typography, and well-structured interactive elements"
- **Goal:** "Efficiently guide user attention, reduce cognitive load, support task completion"

**Application to Observer:** Create clear visual hierarchy so eyes know where to go. Use contrast and whitespace to guide attention. Reduce saccade chaos.

#### Gestalt Principles (Proximity & Grouping)
Source: [NN/g Proximity Principle](https://www.nngroup.com/articles/gestalt-proximity/), [Figma Gestalt Principles](https://www.figma.com/resource-library/gestalt-principles/)

- **Proximity principle** - "Items close together are perceived as part of the same group"
- **Proximity overpowers color/shape** - Grouping by space is stronger than grouping by appearance
- **No borders needed** - "Proximity can create implicit relationships without explicit borders"
- **Whitespace is structural** - "Using varying amounts of whitespace to unite or separate elements is key to communicating meaningful groupings"
- **Users scan quickly** - "Making groupings visually obvious increases usability"

**Application to Observer:** Group items by time period, category, or importance using whitespace - not borders. Let proximity do the heavy lifting.

#### Information Scent Theory
Source: [NN/g Information Scent](https://www.nngroup.com/articles/information-scent/), [News Cues Research](https://www.bellisario.psu.edu/medialab/research-article/news-cues-information-scent-and-cognitive-heuristics)

- **Information foraging** - Users behave like animals hunting for food, following "scent"
- **Three key news cues:** (1) Source name, (2) Recency/time, (3) Related article count
- **Heuristic processing** - Cues are processed as mental shortcuts, not deeply analyzed
- **Context early** - "Too often landing pages don't provide enough context soon enough"
- **Attention-getting potential** - Placement, layout, and color direct traffic

**Application to Observer:** Optimize the three cues: source (badge), recency (time), corroboration (multiple sources). Make them scannable heuristics.

#### TUI-Specific Design Principles
Source: [Brandur on Terminal Interfaces](https://brandur.org/interfaces), [Charm Libraries](https://charm.sh)

- **Speed over aesthetics** - "A successful interface maximizes productivity and lets us keep moving"
- **Legibility first** - "Legibility and whitespace are great, but vanishing importance compared to speed"
- **Purpose-fit layouts** - TUIs can "situate themselves in purpose-fit layouts and controls"
- **Modern terminals** - Support 16.7 million colors, mouse, smooth animation
- **Theme separation** - Centralized theme class separates presentation from styling

**Application to Observer:** Embrace terminal constraints as features. Speed is paramount. Colors should be meaningful, not decorative.

---

### Design Principles for Observer Stream

Based on the research, here are our guiding principles:

#### 1. Meaningful Density (Tufte)
- High information density is good IF organized
- Every visual element must earn its place
- Source badges, timestamps, titles = data. Decorative borders = chartjunk.
- Consider sparklines for trends (prediction market probabilities over time)

#### 2. Visual Hierarchy (Cognitive Load)
- Three levels: **Urgent** (breaking, alerts) → **Fresh** (< 1hr) → **Archive** (older)
- Selected item should expand slightly, show summary
- Unread vs read must be clearly distinct
- Category colors provide quick classification without reading

#### 3. Proximity Grouping (Gestalt)
- Group by time bands: "Just now", "Past hour", "Earlier today", "Yesterday"
- Whitespace between groups, not borders
- Related items (same story, multiple sources) cluster together
- No need for heavy dividers - space implies structure

#### 4. Strong Scent (Information Foraging)
- **Source** - Colored badge, recognizable abbreviation
- **Recency** - Relative time, prominent for fresh items
- **Corroboration** - "3 sources" indicator for multi-source stories
- All three visible at a glance, processed heuristically

#### 5. Speed & Responsiveness (TUI)
- Sub-frame response to keystrokes
- No animation that blocks interaction
- Instant scroll, instant selection feedback
- Loading states should be brief and informative

---

### Concrete Improvements (Prioritized)

#### Phase 1: Breathing Room (Quick Wins) ✓ DONE
- [x] Time band dividers: "─── Past Hour ───" with muted styling
- [x] 1 blank line between time bands (proximity grouping)
- [x] Fresh indicator only for < 10 minutes (make it meaningful)
- [x] Better source abbreviations: HN, NYT, WaPo, WSJ, SCMP, SMH, r/ML, etc.

#### Phase 2: Hierarchy & Expansion ✓ DONE
- [x] Selected item shows 1-line summary below title (with HTML entity decoding)
- [ ] Importance indicator for multi-source stories (needs correlation engine)
- [x] "Breaking" visual treatment: ⚡ indicator + red badge for wire < 30min
- [x] Subtle dimming for items > 24 hours old (title + timestamp)

#### Phase 3: Sparklines & Trends ✓ DONE
- [x] Prediction market: probability bar visualization (extracted from title/summary)
- [x] Source activity: heartbeat indicator (▁▂▃▅▇) for sources with 3+ items in last hour
- [ ] Entity timeline: "▁▂▃▅▇ 47 mentions" inline (requires correlation engine)

#### Phase 4: Adaptive Density ✓ DONE
- [x] "Compact" vs "Comfortable" view toggle (`v` key)
- [x] Auto-adjust: terminals < 30 lines auto-switch to compact
- [x] Compact mode: read items collapsed to minimal "· title" format
- [x] Density indicator in status bar: ◉ comfortable, ◎ compact

---

### Visual Mockup (ASCII)

```
◉ OBSERVER │ 97 sources │ 2,847 items │ 82 blocked
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  ─── Just Now ───────────────────────────────────

┃ Reuters      Fed announces emergency rate cut      ● 2m
│              Markets react with sharp rally...
│              ◆ 4 sources covering this story

  AP News      Breaking: Fed cuts rates by 50bp       3m
  BBC World    Federal Reserve makes surprise move    4m

  ─── Past Hour ──────────────────────────────────

  r/MachineLearning  Claude 4 released, benchmarks... 15m
  Hacker News        Show HN: I built a feed reader   23m
  TechCrunch         OpenAI responds to Anthropic...  31m

  ─── Earlier Today ──────────────────────────────

  NY Times     Analysis: What the Fed move means     2h
  The Atlantic The Age of Instant Information       3h

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
↑↓ navigate  /commands  enter expand  q quit   1/2847
```

**Design notes:**
- Time bands use proximity (whitespace) not heavy borders
- Selected item (Fed) has left border + expanded summary + corroboration
- Fresh items have ● indicator
- Multi-source stories show "◆ N sources"
- Timestamps right-aligned, muted
- Status bar shows position

---

### References

- Tufte, E. (1983). *The Visual Display of Quantitative Information*
- Card, S. & Pirolli, P. - Information Foraging Theory (PARC)
- Sundar, S.S. - "News cues: Information scent and cognitive heuristics"
- Nielsen Norman Group - Gestalt Principles, Information Scent
- Interaction Design Foundation - Eye Tracking in UX

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

## Collaboration Philosophy (Design Decision)

### The Problem with Naive Collaboration
"I add a friend, suddenly my feed is 50% rock climbing. WTF?"

Traditional sharing = firehose of noise. This violates our core ethos:
- "You own your attention" → Now someone else owns it
- "Curation by consent" → You didn't ask for rock climbing
- "Calm is a feature" → Now you're overwhelmed

### The Thoughtful Collaboration Model

**Core Insight:** Collaboration should increase CLARITY, not VOLUME.

What's valuable about a collaborator isn't their sources - it's their JUDGMENT.
- What do they think is important?
- What patterns do they see?
- What should I not miss?

**Principle: Collaborator Sources Start Quiet**
```
When Alice adds Bob as collaborator:
├── Bob's sources → Alice's "Auto" mode (not in stream)
├── Bob's filters → Available but not active
├── Bob's bookmarks → Visible in "Collaborator Picks" section
└── Bob's "Live" sources → Suggested for promotion
```

Alice's feed doesn't change overnight. She can CHOOSE to:
1. See what Bob bookmarked (his judgment)
2. Promote Bob's sources she finds valuable
3. Adopt Bob's filters
4. Ignore Bob's rock climbing obsession

**Principle: Signal Amplification > Source Addition**
```
┌─────────────────────────────────────────────────────────────┐
│ 🔥 3 collaborators flagged this                            │
│ Reuters: Major breakthrough in fusion energy                │
│                                                             │
│ 👀 Alice bookmarked · Bob reading now · Carol shared        │
└─────────────────────────────────────────────────────────────┘
```

When multiple collaborators signal something matters, it surfaces.
This is CLARITY - the group helps you see what's important.

**Principle: Trust is Earned, Not Assumed**

| Trust Level | What You See |
|-------------|--------------|
| New collaborator | Their bookmarks only |
| Trusted | Their bookmarks + their "hot" items |
| Inner circle | Their full Live sources available |

Trust grows through: time, overlap in interests, helpful signals.

**Principle: Asymmetric Collaboration**
- I can follow Alice's curation without her following mine
- I can adopt Bob's "security" sources but not his "sports"
- Collaboration is granular, not all-or-nothing

### Collaboration = Collective Intelligence

The goal isn't "more data" - it's "smarter curation":

| Anti-Pattern | Pattern |
|--------------|---------|
| Firehose their sources | Surface their judgment |
| Add 500 items | Highlight 5 important ones |
| Duplicate effort | Divide attention efficiently |
| Echo chamber | Diverse perspectives, unified signal |

**The Dream State:**
```
"My 4 collaborators and I collectively monitor 500 sources.
 Each of us sees ~50 items/day (our Live sources).
 But when something BIG happens, we all see it instantly
 because someone in the group flagged it."
```

This is Observer's collaboration value proposition:
**Together we see more clearly, not just more.**

---

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

## Source Modes (Design Decision)

**Problem:** With 120+ sources, users need control without complexity. Also, some sources are too noisy for daily reading but valuable for AI personas (e.g., arXiv papers, SEC filings).

**Solution: Four modes with probabilistic exposure**

| Mode | Fetched | Shown | AI Access | Use Case |
|------|---------|-------|-----------|----------|
| **Live** | Always | Always | Yes | Core news you want to see |
| **Sample** | Always | Probabilistically | Yes | "Sometimes" sources |
| **Auto** | On-demand | Never | Yes | Reference material for AI |
| **Off** | Never | Never | No | Disabled entirely |

**Key Insight: Auto Mode**
The Auto mode is the clever bit. ArXiv cs.AI papers are too noisy for your daily stream, but when The Historian persona analyzes a news item about AI, it should be able to pull from academic sources. Auto mode = "in the library, not on my desk."

**Exposure for Sample Mode**
Instead of binary on/off, Sample mode uses a 0.0-1.0 probability:
- 1.0 = always show (same as Live)
- 0.5 = show ~half the time
- 0.1 = show occasionally ("I don't want to miss this but don't flood me")

**80/20 Decision:** Kept the UI minimal - just mode toggles and exposure slider. Skipped: category filtering, batch operations. Can add later if needed.

**Retention Semantics (Design Decision):**
- **Feed items** → Always persist. Your feed history is yours forever. Source mode affects *visibility*, not *storage*.
- **Persona analysis** → Ephemeral by default. AI responses are regenerable on-demand. Don't bloat the DB with cached analysis.
- **User annotations** → Persist. Your highlights, notes, bookmarks are permanent.
- **Correlations** → Persist. The connection graph is valuable and hard to recreate.

This means Off mode doesn't delete old items - it just stops fetching new ones and hides existing ones from stream.

**Collaboration Value:** Source configs persist to `~/.observer/sources.json`. Future: shared sessions can merge configs (your local settings take precedence).

---

## Anonymous Feed Principle

**Core Design Decision:** Observer should only use anonymous, public feeds that require no login or tracking.

### Why This Matters

1. **No behavioral exhaust** - No account = no tracking of what you read
2. **No dependency** - No API keys to revoke, no ToS changes
3. **Portable** - Anyone can run Observer without signing up for anything
4. **Aligned with ethos** - "You own your attention" means no one knows what you're reading

### What Qualifies as "Anonymous"

| OK | Not OK |
|----|--------|
| Public RSS feeds | OAuth-gated APIs |
| `.rss` suffix on Reddit | Reddit API (needs OAuth) |
| Unauthenticated JSON APIs | Twitter/X API (paywalled) |
| Public firehose feeds | Personalized feeds |

### Current Anonymous Aggregators

- **Techmeme** - Human+algo tech curation, firehose RSS available
- **Memeorandum** - Same creator, politics aggregation
- **AllSides** - Balanced news with bias ratings
- **Google News** - Public RSS (though links route through Google)
- **Reddit** - Any public subreddit via `.rss` suffix
- **Lobsters** - Tech, invite-only community but public RSS

### Aggregators That Require Accounts (Avoid)

- Ground News (now paywalled)
- Feedly (account-based)
- Flipboard (account for full features)
- Inoreader (account-based)

---

## Data Sources to Add

### High Priority (Anonymous)
- [x] Reddit public subreddits (via .rss - no OAuth!)
- [x] Bluesky native RSS (profile/rss - no auth!)
- [x] ArXiv (cs.AI, cs.LG, cs.CL, cs.CV, cs.CR, econ, physics)
- [x] SEC EDGAR (Latest, 8-K, 10-K filings - public atom feeds)
- [x] Techmeme + Memeorandum + AllSides aggregators
- [x] Google News RSS (topics)
- [x] X/Twitter content via aggregators (see below)
- [ ] Mastodon public timeline (via Open RSS or native)
- [ ] Wikipedia EventStreams (real-time edits - public)

### X/Twitter Content (Via Aggregators)

Since X's API is paywalled ($100+/mo) and requires auth, we surface X content through sites that cover trending tweets and viral content:

| Source | RSS URL | Content |
|--------|---------|---------|
| **Daily Dot** | dailydot.com/feed/ | Internet culture, viral tweets |
| **Daily Dot Viral** | dailydot.com/tags/viral/feed/ | Specifically viral content |
| **Daily Dot Social** | dailydot.com/tags/social-media/feed/ | Social media coverage |
| **BuzzFeed Internet** | buzzfeed.com/bestoftheinternet.xml | "Best of the Internet" - viral tweets, memes |
| **Know Your Meme** | knowyourmeme.com/newsfeed.rss | Meme documentation, trending memes |
| **Mashable** | mashable.com/feeds/rss/all | Tech & culture with social media coverage |
| **Input Mag** | inputmag.com/rss | Tech culture and social trends |

**Why This Works:**
- These sites employ journalists who monitor X/Twitter trends
- They write about viral tweets, embed them, provide context
- We get the signal without the API cost or tracking
- Aggregators like Daily Dot are "hometown newspapers of the web"

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
