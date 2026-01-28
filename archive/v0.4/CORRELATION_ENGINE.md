# Correlation Engine: Design Document

*"Don't curate. Illuminate."*

---

## Table of Contents

1. [Philosophy](#philosophy)
2. [The Core Insight](#the-core-insight)
3. [Architecture](#architecture)
4. [Data Model](#data-model)
5. [UI/UX Vision](#uiux-vision)
6. [Execution Plan](#execution-plan)
7. [Technical Implementation](#technical-implementation)
8. [Future Horizons](#future-horizons)

---

## Philosophy

### The Problem We're Solving

Traditional news consumption is fragmented. You see individual items, disconnected atoms of information. To understand what's actually happening in the world, you must do the correlation work yourself:

- "Wait, is this the same Boeing story from yesterday?"
- "Who is this person they keep mentioning?"
- "Didn't another outlet say something different?"
- "How long has this been developing?"

Algorithms "solve" this by hiding the complexity - but that violates our core principle. We don't hide. We illuminate.

### The Observer Way

The correlation engine makes the **shape of information visible** without deciding what matters. Think of it as a librarian who:

- Knows where everything is (entity extraction, indexing)
- Notices patterns (velocity, clustering, geography)
- Answers when asked ("What did I miss?" "Who is this?")
- **Never hides the stacks**

Every correlation we surface is:
- **Transparent**: User can see why items are linked
- **Inspectable**: User can view the raw data
- **Dismissable**: User can say "these aren't related" and we learn
- **Optional**: Core reading experience works without it

---

## The Core Insight

**Correlation is the primitive. Everything else is a view.**

```
                    ┌─────────────────────────┐
                    │   CORRELATION ENGINE    │
                    │                         │
                    │  Entities + Events +    │
                    │  Claims + Relations     │
                    │                         │
                    └───────────┬─────────────┘
                                │
            ┌───────────────────┼───────────────────┐
            │                   │                   │
            ▼                   ▼                   ▼
    ┌───────────────┐   ┌───────────────┐   ┌───────────────┐
    │ Story Threads │   │ Entity Pages  │   │  Disagreement │
    │               │   │               │   │   Detection   │
    │ "5 sources    │   │ "Everything   │   │               │
    │  covering     │   │  about NVDA"  │   │ "Reuters says │
    │  this event"  │   │               │   │  X, Fox says  │
    └───────────────┘   └───────────────┘   │  Y"           │
            │                   │           └───────────────┘
            │                   │                   │
            ▼                   ▼                   ▼
    ┌───────────────┐   ┌───────────────┐   ┌───────────────┐
    │   Velocity    │   │  Lifecycle    │   │   Geography   │
    │               │   │               │   │               │
    │ "AI coverage  │   │ "Developing   │   │ "40% US,      │
    │  spiking"     │   │  for 3 days"  │   │  30% Europe"  │
    └───────────────┘   └───────────────┘   └───────────────┘
```

Build the engine once. Get all these features as natural consequences.

---

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                              OBSERVER                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─────────────┐      ┌─────────────────────────────────────────┐  │
│  │             │      │         CORRELATION ENGINE               │  │
│  │   FEEDS     │─────▶│                                         │  │
│  │ AGGREGATOR  │      │  ┌─────────┐  ┌─────────┐  ┌─────────┐ │  │
│  │             │      │  │ Extract │─▶│ Correlate│─▶│  Store  │ │  │
│  └─────────────┘      │  └─────────┘  └─────────┘  └─────────┘ │  │
│                       │       │                          │      │  │
│                       │       │      ┌───────────────────┘      │  │
│                       │       ▼      ▼                          │  │
│                       │  ┌─────────────────┐                    │  │
│                       │  │  Entity Index   │                    │  │
│                       │  │  Story Clusters │                    │  │
│                       │  │  Claim Graph    │                    │  │
│                       │  │  Velocity Stats │                    │  │
│                       │  └─────────────────┘                    │  │
│                       └─────────────────────────────────────────┘  │
│                                      │                              │
│                                      ▼                              │
│                       ┌─────────────────────────────────────────┐  │
│                       │                 UI                       │  │
│                       │                                         │  │
│                       │  Stream ← enriched with correlation     │  │
│                       │  Entity Cards ← on hover/focus          │  │
│                       │  Cluster View ← expandable threads      │  │
│                       │  Radar ← velocity, geography, etc       │  │
│                       └─────────────────────────────────────────┘  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Processing Pipeline

```
Item arrives from feed
        │
        ▼
┌─────────────────┐
│ CHEAP EXTRACTION │ ◀── Runs on EVERY item, instant
│                  │
│ • Title hash     │     (duplicate detection)
│ • Ticker regex   │     ($AAPL, $TSLA)
│ • Country names  │     (Ukraine, China, etc)
│ • Source attr.   │     ("Reuters reports...")
└────────┬─────────┘
         │
         ▼
┌─────────────────┐
│ QUEUE FOR LLM   │ ◀── Prioritized by source weight
│                  │
│ • High-weight    │     (wire services first)
│ • Breaking       │     (temporal signals)
│ • Uncorrelated   │     (orphan items need context)
└────────┬─────────┘
         │
         ▼
┌─────────────────┐
│ LLM EXTRACTION  │ ◀── Background worker, async
│                  │
│ • Entities       │     (people, orgs, places, products)
│ • Event type     │     (statement, action, announcement)
│ • Event summary  │     (what happened, one line)
│ • Claims         │     (who said what)
│ • Sentiment      │     (positive, negative, neutral)
│ • Temporal       │     (breaking, developing, analysis)
└────────┬─────────┘
         │
         ▼
┌─────────────────┐
│ CORRELATION     │ ◀── Runs after extraction
│                  │
│ • Entity overlap │     (items sharing entities)
│ • Title simil.   │     (same story, diff source)
│ • Event matching │     (same event, diff framing)
│ • Claim conflict │     (contradictory statements)
└────────┬─────────┘
         │
         ▼
┌─────────────────┐
│ STORE + INDEX   │ ◀── SQLite, queryable
│                  │
│ • entities       │     (canonical entity records)
│ • item_entities  │     (item ↔ entity links)
│ • clusters       │     (story cluster records)
│ • cluster_items  │     (cluster ↔ item links)
│ • claims         │     (extracted claims)
│ • velocity       │     (entity/cluster activity)
└─────────────────┘
```

---

## Data Model

### Core Tables

```sql
-- Canonical entities (normalized)
CREATE TABLE entities (
    id TEXT PRIMARY KEY,           -- normalized slug: "vladimir_putin"
    type TEXT NOT NULL,            -- person, org, place, product, ticker
    display_name TEXT NOT NULL,    -- "Vladimir Putin"
    aliases TEXT,                  -- JSON array: ["Putin", "Russian President"]
    first_seen TIMESTAMP,
    last_seen TIMESTAMP,
    mention_count INTEGER DEFAULT 0,
    metadata TEXT                  -- JSON blob for type-specific data
);

-- Links items to entities
CREATE TABLE item_entities (
    item_id TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    role TEXT,                     -- subject, object, mentioned, author
    confidence REAL DEFAULT 1.0,
    extracted_at TIMESTAMP,
    PRIMARY KEY (item_id, entity_id)
);

-- Story clusters (groups of items about same event)
CREATE TABLE clusters (
    id TEXT PRIMARY KEY,
    event_summary TEXT,            -- "Boeing 737 MAX grounding extends"
    event_type TEXT,               -- announcement, incident, statement, etc
    first_item_at TIMESTAMP,
    last_item_at TIMESTAMP,
    item_count INTEGER DEFAULT 0,
    source_count INTEGER DEFAULT 0,
    status TEXT DEFAULT 'active',  -- active, stale, resolved
    velocity REAL DEFAULT 0.0      -- items per hour
);

-- Links items to clusters
CREATE TABLE cluster_items (
    cluster_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    added_at TIMESTAMP,
    confidence REAL DEFAULT 1.0,
    PRIMARY KEY (cluster_id, item_id)
);

-- Extracted claims (for disagreement detection, prediction tracking)
CREATE TABLE claims (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL,
    entity_id TEXT,                -- who made the claim
    claim_text TEXT NOT NULL,      -- what they said
    claim_type TEXT,               -- statement, prediction, denial
    sentiment TEXT,
    extracted_at TIMESTAMP,
    -- For predictions
    prediction_date DATE,          -- when claim says X will happen
    prediction_resolved BOOLEAN,
    prediction_outcome TEXT
);

-- Disagreements (when sources conflict)
CREATE TABLE disagreements (
    id TEXT PRIMARY KEY,
    cluster_id TEXT,               -- which story cluster
    claim_a_id TEXT NOT NULL,
    claim_b_id TEXT NOT NULL,
    disagreement_type TEXT,        -- factual, framing, omission
    description TEXT,
    detected_at TIMESTAMP
);

-- Velocity tracking (for "spiking" indicators)
CREATE TABLE velocity_snapshots (
    entity_id TEXT,
    cluster_id TEXT,
    snapshot_at TIMESTAMP,
    mentions_1h INTEGER,
    mentions_24h INTEGER,
    velocity REAL,                 -- mentions per hour trend
    PRIMARY KEY (entity_id, cluster_id, snapshot_at)
);
```

### LLM Extraction Output Schema

```json
{
  "item_id": "feed_item_abc123",
  "extracted_at": "2025-01-25T10:30:00Z",
  "extractor": "ollama/llama3.2",

  "entities": [
    {
      "text": "Vladimir Putin",
      "type": "person",
      "normalized": "vladimir_putin",
      "role": "subject",
      "confidence": 0.95
    },
    {
      "text": "Kremlin",
      "type": "org",
      "normalized": "kremlin",
      "role": "mentioned",
      "confidence": 0.90
    },
    {
      "text": "Ukraine",
      "type": "place",
      "normalized": "ukraine",
      "role": "object",
      "confidence": 0.98
    }
  ],

  "event": {
    "type": "statement",
    "summary": "Putin signals openness to Ukraine negotiations",
    "temporal": "breaking",
    "fingerprint": "hash_for_deduplication"
  },

  "claims": [
    {
      "speaker": "vladimir_putin",
      "text": "Russia is ready for negotiations without preconditions",
      "type": "statement",
      "sentiment": "neutral"
    }
  ],

  "source_attribution": {
    "original_source": "Reuters",
    "is_aggregation": false
  }
}
```

---

## UI/UX Vision

### Design Principles

**1. Ambient, Not Aggressive**

Correlation data should feel like gentle context, not shouty notifications. The stream remains primary. Correlation enriches without demanding attention.

```
Bad:  🚨 RELATED STORY DETECTED! 5 SOURCES!
Good: ◐ 5 sources
```

**2. Progressive Disclosure**

Surface the minimum useful signal. Let users drill deeper on demand.

```
Level 0: Stream item (no visible correlation)
Level 1: Subtle indicator "◐ 5" (5 sources on this story)
Level 2: Hover/focus shows cluster preview
Level 3: Expand to see full cluster with all items
Level 4: Entity page with complete history
```

**3. Information Scent**

Use visual weight to indicate richness. Items with more correlation context feel slightly "heavier" - not visually louder, but denser. Users develop intuition for where depth exists.

**4. The Ambient Radar**

A subtle, persistent awareness layer. Not a dashboard demanding attention - more like peripheral vision. You notice movement, investigate if curious.

### Visual Language

#### Color Palette (Correlation-Specific)

```
Cluster Indicators:
  ◐ Cluster exists      #58a6ff (calm blue)
  ◑ Active/developing   #3fb950 (growth green)
  ◉ High velocity       #f85149 (attention red)
  ○ Stale               #8b949e (muted gray)

Entity Type Colors:
  Person                #d2a8ff (soft purple)
  Organization          #79c0ff (sky blue)
  Place                 #56d364 (geo green)
  Ticker/Product        #ffa657 (finance orange)

Disagreement:
  Conflict detected     #f85149 border/accent

Velocity:
  Spiking ↑             #3fb950
  Steady →              #8b949e
  Fading ↓              #6e7681
```

#### Iconography

```
Cluster:       ◐ ◑ ◉ ○  (fill indicates activity)
Entity types:  👤 🏢 🌍 📈  (person, org, place, ticker)
Velocity:      ▁▂▃▄▅▆▇█  (sparkline)
Agreement:     ✓         (sources align)
Disagreement:  ⚡        (sources conflict)
Source count:  ×5        (multiplier notation)
Duration:      3d        (days as story)
```

### UI Components

#### 1. The Enriched Stream Item

```
┌─────────────────────────────────────────────────────────────┐
│ ● Reuters · 3m                             ◐ 5  ⚡  3d     │
│                                            ↑   ↑   ↑       │
│ Boeing 737 MAX Grounding Extended          │   │   └─ duration
│ FAA announces continued safety review      │   └─ disagreement exists
│                                            └─ 5 sources in cluster
│ 👤 Boeing CEO  🏢 FAA  📈 $BA                              │
│ └─ entity pills (clickable)                                │
└─────────────────────────────────────────────────────────────┘
```

**States:**
- Default: Entity pills hidden, only cluster indicator visible
- Focused: Entity pills appear, cluster info expands
- Expanded: Full cluster view opens below

#### 2. Entity Pills

Small, clickable indicators showing key entities. Appear on focus/hover.

```
┌────────────────┐ ┌─────────────┐ ┌──────────────┐
│ 👤 Elon Musk   │ │ 🏢 Tesla    │ │ 📈 $TSLA     │
│    ×12 today   │ │   ×8 today  │ │   ×15 today  │
└────────────────┘ └─────────────┘ └──────────────┘
       │
       ▼ click
┌─────────────────────────────────────────────────┐
│ 👤 Elon Musk                           ▁▃▅▇▅▃▁ │
│                                                 │
│ Mentioned in 47 items today                    │
│ Peak activity: 2h ago                          │
│ Related: 🏢 Tesla, SpaceX, X Corp              │
│                                                 │
│ Recent:                                         │
│ ├─ Tesla announces... (Reuters, 10m)           │
│ ├─ Musk responds to... (CNN, 25m)              │
│ └─ SpaceX launch... (Space.com, 1h)            │
│                                                 │
│ [View all 47 items →]                          │
└─────────────────────────────────────────────────┘
```

#### 3. Cluster Expansion

When user focuses on cluster indicator, expand inline:

```
┌─────────────────────────────────────────────────────────────┐
│ ● Reuters · 3m                                 ◉ 5  3d     │
│ Boeing 737 MAX Grounding Extended                          │
├─────────────────────────────────────────────────────────────┤
│ ┌─ CLUSTER: Boeing 737 MAX Safety Review ────────────────┐ │
│ │                                                         │ │
│ │  5 sources · 12 items · developing for 3 days         │ │
│ │  ▁▂▃▅▇▅▃▂▁ velocity                                    │ │
│ │                                                         │ │
│ │  ⚡ DISAGREEMENT DETECTED                              │ │
│ │  Reuters: "FAA extends indefinitely"                   │ │
│ │  Fox Biz: "Sources say weeks, not months"             │ │
│ │                                                         │ │
│ │  Sources covering:                                      │ │
│ │  ├─ Reuters (3 items)                                  │ │
│ │  ├─ Bloomberg (2 items)                                │ │
│ │  ├─ CNN (2 items)                                      │ │
│ │  ├─ Fox Business (3 items)                             │ │
│ │  └─ WSJ (2 items)                                      │ │
│ │                                                         │ │
│ │  [Expand full cluster →]  [Compare sources →]          │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

#### 4. The Radar (Ambient Awareness)

A subtle status area showing correlation engine vitals. Not a dashboard - more like a car's instrument cluster in peripheral vision.

```
┌─────────────────────────────────────────────────────────────┐
│ RADAR                                                 ─ × ○ │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  VELOCITY                     GEOGRAPHY                     │
│  ▲ AI/ML        ████████░░    US      ████████████         │
│  ▲ Boeing       ██████░░░░    Europe  ██████░░░░░░         │
│  → Markets      ████░░░░░░    Asia    ████░░░░░░░░         │
│  ▼ Crypto       ██░░░░░░░░    Other   ██░░░░░░░░░░         │
│                                                             │
│  ACTIVE CLUSTERS                                            │
│  ◉ Boeing 737 MAX (5 src, 3d)                              │
│  ◑ Fed Rate Decision (8 src, developing)                   │
│  ◐ Ukraine Negotiations (12 src, 2d)                       │
│                                                             │
│  ENTITIES TRENDING                                          │
│  👤 Jerome Powell ↑↑    🏢 Boeing ↑    📈 $NVDA →         │
│                                                             │
│  ENGINE: 847 items indexed · 124 entities · 23 clusters    │
└─────────────────────────────────────────────────────────────┘
```

**Behavior:**
- Toggled with `Ctrl+R` or `/radar`
- Can be docked left or floating
- Updates live but subtly (no jarring refreshes)
- Click any element to filter stream

#### 5. Entity Page (Full View)

When user clicks through to an entity:

```
┌─────────────────────────────────────────────────────────────┐
│ ← Back to Stream                                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  👤 JEROME POWELL                                          │
│  ══════════════════════════════════════════════════════════ │
│                                                             │
│  Chair, Federal Reserve                                     │
│  Tracking since: Jan 15, 2025                              │
│  Total mentions: 234                                        │
│                                                             │
│  ACTIVITY ════════════════════════════════════════════════ │
│                                                             │
│  ▁▁▂▂▃▃▂▂▁▁▂▃▅▇██▇▅▃▃▄▅▃▂▁▁                              │
│  └─ Jan 1 ─────────────────────────────────── Today ──┘   │
│                                                             │
│  RELATED ENTITIES ════════════════════════════════════════ │
│                                                             │
│  🏢 Federal Reserve (187 co-mentions)                      │
│  🏢 Treasury Dept (45 co-mentions)                         │
│  👤 Janet Yellen (34 co-mentions)                          │
│  📈 Interest Rates (89 co-mentions)                        │
│                                                             │
│  STORY CLUSTERS ══════════════════════════════════════════ │
│                                                             │
│  ◉ Fed Rate Decision (active)           8 items, 4 sources │
│  ○ Jackson Hole Speech (Aug)           12 items, 7 sources │
│  ○ Inflation Testimony (Jul)            9 items, 5 sources │
│                                                             │
│  ALL MENTIONS ════════════════════════════════════════════ │
│                                                             │
│  ● Bloomberg · 15m                                         │
│    Powell Signals Fed Ready to Cut Rates                   │
│                                                             │
│  ● Reuters · 1h                                            │
│    Fed Chair Testimony: Key Takeaways                      │
│                                                             │
│  ● WSJ · 2h                                                │
│    Markets Rally on Powell Comments                        │
│                                                             │
│  [Load more...]                                            │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

#### 6. Disagreement View

When disagreements are detected:

```
┌─────────────────────────────────────────────────────────────┐
│ ⚡ SOURCES DISAGREE                                         │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Topic: Boeing 737 MAX Grounding Duration                  │
│  Detected: 2 hours ago                                      │
│                                                             │
│  ┌─────────────────────┐   ┌─────────────────────┐         │
│  │ REUTERS             │   │ FOX BUSINESS        │         │
│  │                     │   │                     │         │
│  │ "The FAA has        │   │ "Sources familiar   │         │
│  │ extended the        │   │ with the matter     │         │
│  │ grounding           │   │ suggest the         │         │
│  │ indefinitely        │   │ grounding will      │         │
│  │ pending safety      │   │ lift within weeks,  │         │
│  │ review."            │   │ not months."        │         │
│  │                     │   │                     │         │
│  │ [Read full →]       │   │ [Read full →]       │         │
│  └─────────────────────┘   └─────────────────────┘         │
│                    VS                                       │
│                                                             │
│  Other sources on this story:                               │
│  • Bloomberg: Aligns with Reuters                          │
│  • WSJ: No timeline stated                                 │
│  • CNN: Aligns with Reuters                                │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

#### 7. The "Catch Me Up" Flow

User returns after time away:

```
┌─────────────────────────────────────────────────────────────┐
│ CATCH ME UP                                      ─ × ○      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  You've been away for 6 hours.                             │
│  Here's what developed:                                     │
│                                                             │
│  ══════════════════════════════════════════════════════════ │
│                                                             │
│  🔴 NEW CLUSTER                                            │
│  Fed Announces Rate Cut                                     │
│  8 sources · Started 4h ago · High velocity                │
│  [View cluster →]                                           │
│                                                             │
│  ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── │
│                                                             │
│  🟡 DEVELOPING                                              │
│  Boeing 737 MAX Grounding                                   │
│  +3 items since you left · Sources now disagree            │
│  [View updates →]                                           │
│                                                             │
│  ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── ── │
│                                                             │
│  📊 TRENDING ENTITIES                                       │
│  👤 Jerome Powell (+45 mentions)                           │
│  🏢 Federal Reserve (+38 mentions)                         │
│  📈 $SPY (+22 mentions)                                    │
│                                                             │
│  ══════════════════════════════════════════════════════════ │
│                                                             │
│  [Dismiss and browse] [Generate full briefing]             │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## Execution Plan

### Phase 0: Foundation (This Sprint)
*Zero LLM cost, immediate value, builds data structures*

**Week 1-2: Duplicate Detection + Basic Entities**

```
Goals:
├── Detect duplicate/near-duplicate items
├── Extract tickers via regex ($AAPL, $TSLA)
├── Extract country names via dictionary
├── Store in SQLite, create indexes
└── Add subtle "×3" indicator in UI when dupes exist

Implementation:
├── internal/correlation/
│   ├── engine.go        # Background worker coordinator
│   ├── duplicates.go    # SimHash or Levenshtein on titles
│   ├── tickers.go       # Regex extraction: \$[A-Z]{1,5}
│   ├── countries.go     # Dictionary lookup
│   └── store.go         # SQLite schema + queries
├── Add entity tables to store/sqlite.go
└── Add "×N" indicator to stream item rendering

No LLM required. Pure algorithmic.
```

**Deliverables:**
- [ ] Duplicate detection working (simhash on titles)
- [ ] Ticker extraction working ($AAPL → entity)
- [ ] Country extraction working (Ukraine → entity)
- [ ] SQLite tables created and populated
- [ ] Stream shows "×3" when 3+ items are duplicates

---

### Phase 1: Entity Index (Next Sprint)
*LLM extraction begins, entity pages emerge*

**Week 3-4: LLM Extraction Pipeline**

```
Goals:
├── Background worker processes items async
├── LLM extracts people, orgs, places, products
├── Entity normalization (fuzzy matching)
├── Entity hover cards in UI
└── Basic entity page (click to see all mentions)

Implementation:
├── internal/correlation/
│   ├── extractor.go     # LLM prompt + parsing
│   ├── normalizer.go    # Entity deduplication
│   └── queue.go         # Priority queue for processing
├── Prompt engineering for consistent extraction
├── Entity hover card component
└── Entity page view

LLM cost: ~1 extraction per item, background
```

**Deliverables:**
- [ ] Extraction worker running in background
- [ ] Entity normalization reducing duplicates
- [ ] Hover over item shows entity pills
- [ ] Click entity shows entity page with all mentions
- [ ] Entity sparkline shows activity over time

---

### Phase 2: Story Clusters (Month 2)
*The "5 sources" indicator, cluster expansion*

**Week 5-6: Clustering Logic**

```
Goals:
├── Group items about same event/story
├── Use entity overlap + title similarity + time proximity
├── LLM as tiebreaker for ambiguous cases
├── Cluster indicator in stream
└── Cluster expansion view

Implementation:
├── internal/correlation/
│   ├── clusters.go      # Clustering algorithm
│   └── cluster_ui.go    # Expansion rendering
├── Cluster indicator: ◐ N (N sources)
├── Focus on cluster → inline expansion
└── Full cluster page

Clustering heuristics:
- Same entities (>50% overlap)
- Similar titles (>0.7 similarity)
- Within 24h of each other
- LLM confirms if uncertain
```

**Deliverables:**
- [ ] Items automatically grouped into clusters
- [ ] Cluster indicator (◐ 5) in stream
- [ ] Focus on indicator expands inline
- [ ] Full cluster view shows all items, sources
- [ ] Cluster velocity tracking (items/hour)

---

### Phase 3: Disagreement Detection (Month 2-3)
*The ⚡ indicator, source comparison*

**Week 7-8: Claim Extraction + Conflict Detection**

```
Goals:
├── Extract claims from items ("X says Y")
├── Detect conflicting claims in same cluster
├── Surface disagreements with ⚡ indicator
└── Side-by-side comparison view

Implementation:
├── internal/correlation/
│   ├── claims.go        # Claim extraction
│   └── conflicts.go     # Conflict detection
├── Claim storage in SQLite
├── ⚡ indicator when conflict exists
└── Comparison view component

LLM prompts:
- "Extract factual claims from this text"
- "Do these claims contradict each other?"
```

**Deliverables:**
- [ ] Claims extracted and stored
- [ ] Conflicts detected automatically
- [ ] ⚡ indicator on items with conflicts
- [ ] Side-by-side comparison view
- [ ] Confidence scoring on conflicts

---

### Phase 4: The Radar (Month 3)
*Ambient awareness, velocity, geography*

**Week 9-10: Radar View**

```
Goals:
├── Velocity tracking (what's spiking)
├── Geographic distribution
├── Active clusters overview
├── Trending entities
└── Toggleable radar panel

Implementation:
├── internal/correlation/
│   ├── velocity.go      # Trend calculation
│   └── geography.go     # Place entity aggregation
├── internal/ui/radar/
│   ├── model.go
│   └── view.go
├── Keyboard: Ctrl+R toggles radar
└── Click-through to filter stream

Velocity calculation:
- Snapshot mentions every 5 minutes
- Compare to baseline (7-day average)
- Flag as ▲ spiking, → steady, ▼ fading
```

**Deliverables:**
- [ ] Velocity tracking for entities/clusters
- [ ] Geographic distribution calculation
- [ ] Radar panel UI
- [ ] Toggle with Ctrl+R
- [ ] Click radar item filters stream

---

### Phase 5: Polish + Advanced (Month 4+)
*Catch-up flow, prediction tracking, collaboration*

```
Advanced features:
├── "Catch Me Up" - Personalized briefing after absence
├── Prediction tracking - Log claims, check outcomes
├── Shared session correlation - Collaborative entity annotation
├── Living graph - Force-directed entity visualization
└── API export - Entity/cluster data for external tools

Each is a mini-project. Prioritize based on user feedback.
```

---

## Technical Implementation

### Project Structure

```
internal/correlation/
├── engine.go           # Main coordinator, background workers
├── types.go            # Entity, Cluster, Claim types
├── store.go            # SQLite persistence layer
│
├── extraction/
│   ├── cheap.go        # Regex, dictionary extraction (no LLM)
│   ├── llm.go          # LLM-based extraction
│   ├── prompts.go      # Extraction prompt templates
│   └── queue.go        # Priority queue for LLM processing
│
├── correlation/
│   ├── duplicates.go   # Duplicate detection (simhash)
│   ├── clusters.go     # Story clustering logic
│   ├── normalizer.go   # Entity normalization/dedup
│   └── conflicts.go    # Claim conflict detection
│
├── analysis/
│   ├── velocity.go     # Trend calculation
│   ├── geography.go    # Geographic distribution
│   └── lifecycle.go    # Story lifecycle tracking
│
└── testdata/           # Test fixtures
```

### Engine Interface

```go
// Engine coordinates all correlation activities
type Engine struct {
    store       *Store
    extractor   *Extractor
    clusterer   *Clusterer
    analyzer    *Analyzer

    itemQueue   chan feeds.Item
    resultsChan chan ExtractionResult

    mu          sync.RWMutex
    running     bool
}

// Start begins background processing
func (e *Engine) Start(ctx context.Context) error

// Stop gracefully shuts down workers
func (e *Engine) Stop() error

// ProcessItem queues an item for extraction
func (e *Engine) ProcessItem(item feeds.Item)

// Queries
func (e *Engine) GetEntity(id string) (*Entity, error)
func (e *Engine) GetEntitiesForItem(itemID string) ([]Entity, error)
func (e *Engine) GetCluster(id string) (*Cluster, error)
func (e *Engine) GetClusterForItem(itemID string) (*Cluster, error)
func (e *Engine) GetDuplicates(itemID string) ([]feeds.Item, error)
func (e *Engine) GetDisagreements(clusterID string) ([]Disagreement, error)
func (e *Engine) GetVelocity(entityID string) (*Velocity, error)
func (e *Engine) GetTrendingEntities(limit int) ([]Entity, error)
func (e *Engine) GetActiveClsuters(limit int) ([]Cluster, error)
```

### Extraction Prompts

```go
const EntityExtractionPrompt = `Extract entities from this news item.

For each entity found, provide:
- text: The exact text as it appears
- type: One of [person, org, place, product, ticker]
- role: One of [subject, object, mentioned]

Return JSON only:
{
  "entities": [
    {"text": "...", "type": "...", "role": "..."}
  ]
}

News item:
Title: %s
Summary: %s
Source: %s`

const ClaimExtractionPrompt = `Extract factual claims from this text.

A claim is a statement that could be true or false.
Include who made the claim if stated.

Return JSON only:
{
  "claims": [
    {"speaker": "...", "claim": "...", "type": "statement|prediction|denial"}
  ]
}

Text:
%s`

const ConflictDetectionPrompt = `Do these two claims contradict each other?

Claim A: %s
Claim B: %s

Return JSON only:
{
  "conflicts": true|false,
  "explanation": "brief explanation"
}`
```

---

## Future Horizons

### The Living Graph

A force-directed visualization where entities are nodes and stories are edges:

```
            Putin ●━━━━━━━━━━━━━● Ukraine
                  ╲            ╱
                   ╲          ╱
              NATO ●━━━━━━━━━●━━━━━━━━━● EU
                             ╲
                              ╲
                       Zelensky ●
```

Nodes pulse with activity. Edges thicken with connection strength. You watch the world breathe.

### Collaborative Correlation

In shared sessions, correlation becomes collaborative:

- **Shared entity definitions**: User A defines "the Boeing situation", User B inherits it
- **Divergence detection**: "You read the NYT take, they read WSJ. Compare?"
- **Collective attention**: "47 Observer users tracking this cluster"
- **Expert annotations**: Users deep on a topic can add context

### Prediction Markets Integration

Claims that are predictions can be tracked:

```
CLAIM: "Fed will cut rates in March" (Powell, Jan 15)
       └─ Source: Reuters
       └─ Outcome: PENDING
       └─ Resolve date: March 31

Later:

CLAIM: "Fed will cut rates in March"
       └─ Outcome: CORRECT ✓
       └─ Resolved: March 19 (Fed cut 25bp)
```

Build accountability for analysts and sources over time.

### API Export

Make correlation data available for external tools:

```bash
# Get all entities mentioned today
curl localhost:8080/api/entities?since=today

# Get cluster details
curl localhost:8080/api/clusters/boeing-737-max

# Subscribe to new clusters (WebSocket)
wscat -c ws://localhost:8080/api/stream/clusters
```

Power user workflows: pipe to scripts, build custom visualizations, integrate with note-taking apps.

---

## Summary

The correlation engine is not a feature - it's infrastructure that enables features. Build it once, thoughtfully, and an entire universe of capabilities emerges:

| You Build | You Get For Free |
|-----------|------------------|
| Entity extraction | Entity pages, hover cards, search |
| Duplicate detection | "×3" indicators, deduplication |
| Story clustering | "5 sources" badges, cluster expansion |
| Claim extraction | Disagreement detection, prediction tracking |
| Velocity tracking | Trending indicators, spike alerts |
| Geography tracking | Map view, region filtering |

The UI remains calm and ambient. The complexity is there for those who seek it, invisible to those who don't.

That's the Observer way: **all the power, none of the manipulation.**

---

*Document version: 1.0*
*Last updated: January 2025*
*Authors: The Brain Trust*
