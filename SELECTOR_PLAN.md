# Selector System Plan

## Overview

Replace hardcoded time band rendering with composable **selectors** - predicates that narrow the view to matching items.

**Philosophy:** Selectors are generic, composable functions. The stream view just renders whatever passes the active selector.

---

## Current State (Problems)

```go
// Hardcoded in stream/model.go
type timeBand int
const (
    bandJustNow timeBand = iota
    bandPastHour
    bandToday
    bandYesterday
    bandOlder
)

func interleaveWithinBands(items []feeds.Item) []feeds.Item { ... }
func renderTimeBandDivider(band timeBand) string { ... }
func getTimeBand(published time.Time) timeBand { ... }
```

**Issues:**
1. Time logic baked into rendering
2. Auto-compact hides/shows dividers inconsistently
3. Can't combine with other filters (source, entity)
4. Not reusable

---

## Proposed Architecture

### Two Filter Types (Separation of Concerns)

| Type | Purpose | Example | Application |
|------|---------|---------|-------------|
| **Exclusion** | Remove unwanted items | "Hide ads" | Applied globally, always |
| **Selection** | Narrow current view | "Show past hour" | User switches between |

### Selector Interface

```go
// internal/selection/selector.go

package selection

import (
    "time"
    "github.com/abelbrown/observer/internal/feeds"
)

// Selector narrows the view to matching items
type Selector interface {
    Name() string
    Match(item *feeds.Item) bool
}

// TimeSelector filters by age
type TimeSelector struct {
    name   string
    maxAge time.Duration
    minAge time.Duration // 0 = no minimum
}

func (s TimeSelector) Name() string { return s.name }

func (s TimeSelector) Match(item *feeds.Item) bool {
    age := time.Since(item.Published)
    if s.minAge > 0 && age < s.minAge {
        return false
    }
    if s.maxAge > 0 && age > s.maxAge {
        return false
    }
    return true
}
```

### Built-in Time Selectors

```go
var (
    All          Selector = nil // No filter = show all
    JustNow      = TimeSelector{"Just Now", 15 * time.Minute, 0}
    PastHour     = TimeSelector{"Past Hour", time.Hour, 0}
    Today        = TimeSelector{"Today", 24 * time.Hour, 0}
    Yesterday    = TimeSelector{"Yesterday", 48 * time.Hour, 24 * time.Hour}
    ThisWeek     = TimeSelector{"This Week", 7 * 24 * time.Hour, 0}
)
```

### Other Selector Types

```go
// SourceSelector filters by source name
type SourceSelector struct {
    sources map[string]bool
}

func (s SourceSelector) Match(item *feeds.Item) bool {
    return s.sources[item.SourceName]
}

// EntitySelector filters by entity (uses correlation engine)
type EntitySelector struct {
    entityID string
    engine   *correlation.Engine
}

func (s EntitySelector) Match(item *feeds.Item) bool {
    entities := s.engine.GetItemEntities(item.ID)
    for _, e := range entities {
        if e.ID == s.entityID {
            return true
        }
    }
    return false
}

// ClusterSelector filters by cluster
type ClusterSelector struct {
    clusterID string
    engine    *correlation.Engine
}

func (s ClusterSelector) Match(item *feeds.Item) bool {
    cluster := s.engine.GetClusterInfo(item.ID)
    return cluster != nil && cluster.ID == s.clusterID
}
```

### Composite Selectors

```go
// AndSelector requires all selectors to match
type AndSelector struct {
    selectors []Selector
}

func (s AndSelector) Match(item *feeds.Item) bool {
    for _, sel := range s.selectors {
        if !sel.Match(item) {
            return false
        }
    }
    return true
}

// OrSelector requires any selector to match
type OrSelector struct {
    selectors []Selector
}

func (s OrSelector) Match(item *feeds.Item) bool {
    for _, sel := range s.selectors {
        if sel.Match(item) {
            return true
        }
    }
    return false
}
```

---

## Stream Model Changes

### New Fields

```go
type Model struct {
    // ... existing fields ...

    activeSelector selection.Selector  // Current view (nil = all)
    selectorIndex  int                  // Which preset is active (for UI)
}
```

### New Method: getVisibleItems()

```go
func (m Model) getVisibleItems() []feeds.Item {
    items := m.items

    // 1. Apply exclusion filters (ads, spam) - always
    for i := len(items) - 1; i >= 0; i-- {
        if m.filterEngine.ShouldHide(items[i]) {
            items = append(items[:i], items[i+1:]...)
        }
    }

    // 2. Apply active selector (time, source, etc.)
    if m.activeSelector != nil {
        var selected []feeds.Item
        for _, item := range items {
            if m.activeSelector.Match(&item) {
                selected = append(selected, item)
            }
        }
        items = selected
    }

    // 3. Apply diversity (interleave sources)
    items = interleaveBySource(items)

    // 4. Apply limit
    if len(items) > m.maxItems {
        items = items[:m.maxItems]
    }

    return items
}
```

### Simplified Interleave (No Time Bands)

```go
// interleaveBySource spreads sources evenly without time band grouping
func interleaveBySource(items []feeds.Item, maxPerSource int) []feeds.Item {
    // Group by source
    bySource := make(map[string][]feeds.Item)
    var sourceOrder []string
    for _, item := range items {
        if _, exists := bySource[item.SourceName]; !exists {
            sourceOrder = append(sourceOrder, item.SourceName)
        }
        bySource[item.SourceName] = append(bySource[item.SourceName], item)
    }

    // Round-robin through sources
    var result []feeds.Item
    sourceIdx := make(map[string]int)
    moreToAdd := true
    for moreToAdd {
        moreToAdd = false
        for _, source := range sourceOrder {
            idx := sourceIdx[source]
            if idx < len(bySource[source]) && idx < maxPerSource {
                result = append(result, bySource[source][idx])
                sourceIdx[source]++
                moreToAdd = true
            }
        }
    }
    return result
}
```

---

## UI Changes

### Selector Bar (Top of Stream)

```
┌─────────────────────────────────────────────────────────────┐
│ [All] [Just Now] [Past Hour] [Today] [Yesterday]  │ 456 items │
├─────────────────────────────────────────────────────────────┤
│  HN   Some headline about something...              5m  │
│  NYT  Another headline here...                      12m │
│  ...                                                     │
```

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `1` | All items |
| `2` | Just Now (<15min) |
| `3` | Past Hour |
| `4` | Today |
| `5` | Yesterday |
| `/filter` | Open filter picker |

### Active Selector Display

Show in status bar: `◉ Past Hour (127 items)`

---

## Code to Remove

| File | Remove |
|------|--------|
| `stream/model.go` | `timeBand` type and constants |
| `stream/model.go` | `getTimeBand()` function |
| `stream/model.go` | `bandLabel()` function |
| `stream/model.go` | `interleaveWithinBands()` - replace with `interleaveBySource()` |
| `stream/model.go` | `renderTimeBandDivider()` |
| `stream/model.go` | `itemBands` cache field |
| `stream/model.go` | Time band divider rendering in View() |
| `stream/model.go` | Auto-compact divider logic |

---

## Code to Add

| File | Add |
|------|-----|
| `internal/selection/selector.go` | **NEW** - Selector interface and types |
| `internal/selection/time.go` | **NEW** - TimeSelector and presets |
| `internal/selection/source.go` | **NEW** - SourceSelector |
| `internal/selection/entity.go` | **NEW** - EntitySelector (uses correlation) |
| `internal/selection/cluster.go` | **NEW** - ClusterSelector (uses correlation) |
| `internal/selection/composite.go` | **NEW** - AndSelector, OrSelector |
| `stream/model.go` | `activeSelector` field |
| `stream/model.go` | `getVisibleItems()` method |
| `stream/model.go` | `interleaveBySource()` function |
| `stream/model.go` | Selector switching keybindings |
| `stream/model.go` | Selector bar rendering |

---

## Implementation Order

### Phase 1: Create Selection Package
1. Create `internal/selection/selector.go` with interface
2. Create `internal/selection/time.go` with TimeSelector
3. Add unit tests for selectors

### Phase 2: Integrate into Stream Model
1. Add `activeSelector` field to Model
2. Create `getVisibleItems()` method
3. Create `interleaveBySource()` (simplified interleave)
4. Wire `getVisibleItems()` into rendering

### Phase 3: Remove Time Band Code
1. Remove `timeBand` type and functions
2. Remove `interleaveWithinBands()`
3. Remove `renderTimeBandDivider()`
4. Remove `itemBands` cache
5. Remove auto-compact divider logic

### Phase 4: Add UI
1. Add selector bar at top of stream
2. Add keyboard shortcuts (1-5)
3. Show active selector in status bar

### Phase 5: Add Advanced Selectors
1. SourceSelector
2. EntitySelector (correlation)
3. ClusterSelector (correlation)
4. Composite selectors (And, Or)

---

## Testing

### Unit Tests

```go
func TestTimeSelector(t *testing.T) {
    now := time.Now()

    item5min := feeds.Item{Published: now.Add(-5 * time.Minute)}
    item30min := feeds.Item{Published: now.Add(-30 * time.Minute)}
    item2hr := feeds.Item{Published: now.Add(-2 * time.Hour)}

    justNow := TimeSelector{"Just Now", 15 * time.Minute, 0}

    assert.True(t, justNow.Match(&item5min))
    assert.False(t, justNow.Match(&item30min))
    assert.False(t, justNow.Match(&item2hr))
}
```

### Integration Tests

1. Start app, verify default view shows all items
2. Press `2`, verify only items <15min shown
3. Press `3`, verify items <1hr shown
4. Press `1`, verify all items shown again

---

## Success Criteria

- [x] Time bands replaced with selectors
- [x] No more flickering dividers on resize
- [x] Keyboard shortcuts work (1-5)
- [x] Status bar shows active filter
- [x] Item count updates when switching
- [x] Source diversity preserved (interleaving)
- [ ] Can combine with correlation filters later
