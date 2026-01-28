package stream

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/correlation"
	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/selection"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"
)

// Category colors for visual differentiation
var categoryColors = map[string]lipgloss.Color{
	"wire":           lipgloss.Color("#f85149"), // red - breaking
	"tv-us":          lipgloss.Color("#8b949e"), // gray
	"newspaper-us":   lipgloss.Color("#c9d1d9"), // white
	"newspaper-intl": lipgloss.Color("#a5d6ff"), // light blue
	"tech":           lipgloss.Color("#58a6ff"), // blue
	"ai":             lipgloss.Color("#d2a8ff"), // purple
	"science":        lipgloss.Color("#7ee787"), // green
	"finance":        lipgloss.Color("#ffa657"), // orange
	"politics":       lipgloss.Color("#ff7b72"), // coral
	"security":       lipgloss.Color("#f85149"), // red
	"crypto":         lipgloss.Color("#ffa657"), // orange
	"longform":       lipgloss.Color("#d29922"), // amber
	"aggregator":     lipgloss.Color("#8b949e"), // gray
	"reddit":         lipgloss.Color("#ff7b72"), // reddit orange-red
	"predictions":    lipgloss.Color("#3fb950"), // green - money
	"events":         lipgloss.Color("#f85149"), // red - alerts
	"arxiv":          lipgloss.Color("#d2a8ff"), // purple - academic
	"sec":            lipgloss.Color("#ffa657"), // orange - finance
	"bluesky":        lipgloss.Color("#58a6ff"), // blue
	"viral":          lipgloss.Color("#f778ba"), // pink - viral/memes
}

// Source abbreviations - shorter, more recognizable than truncation
var sourceAbbrevs = map[string]string{
	"Hacker News":        "HN",
	"r/MachineLearning":  "r/ML",
	"r/LocalLLaMA":       "r/LocalLLM",
	"r/programming":      "r/prog",
	"r/technology":       "r/tech",
	"r/worldnews":        "r/world",
	"r/singularity":      "r/singul",
	"r/Futurology":       "r/future",
	"r/geopolitics":      "r/geopol",
	"r/Economics":        "r/econ",
	"South China MP":     "SCMP",
	"Sydney Morning Herald": "SMH",
	"Washington Post":    "WaPo",
	"Wall St Journal":    "WSJ",
	"NY Times":           "NYT",
	"NY Times World":     "NYT World",
	"Financial Times":    "FT",
	"Google News Top":    "GNews",
	"Google News World":  "GN World",
	"Google News Tech":   "GN Tech",
	"Google News Sci":    "GN Sci",
	"Scientific American": "SciAm",
	"MIT AI News":        "MIT AI",
	"Krebs on Security":  "Krebs",
	"Schneier on Security": "Schneier",
	"The Hacker News":    "THN",
	"Bleeping Computer":  "BleepCo",
	"Hollywood Reporter": "THR",
	"Rolling Stone":      "RollingS",
	"USGS Significant":   "USGS",
	"USGS M4.5+":         "USGS 4.5",
	// Viral / Internet Culture
	"Daily Dot":          "DDot",
	"Daily Dot Viral":    "DDot 🔥",
	"Daily Dot Social":   "DDot Social",
	"BuzzFeed Internet":  "BF Viral",
	"Know Your Meme":     "KYM",
	"Mashable":           "Mash",
	"Input Mag":          "Input",
}

// interleaveBySource spreads sources evenly for diversity.
// Instead of: [A1, A2, A3, B1, B2, C1] (chronological clusters)
// Produces:   [A1, B1, C1, A2, B2, A3] (interleaved by source)
func interleaveBySource(items []feeds.Item, maxPerSource int) []feeds.Item {
	if len(items) == 0 {
		return items
	}

	// Group by source
	sourceItems := make(map[string][]feeds.Item)
	var sourceOrder []string // preserve first-seen order
	for _, item := range items {
		if _, exists := sourceItems[item.SourceName]; !exists {
			sourceOrder = append(sourceOrder, item.SourceName)
		}
		sourceItems[item.SourceName] = append(sourceItems[item.SourceName], item)
	}

	// Round-robin through sources
	result := make([]feeds.Item, 0, len(items))
	sourceIdx := make(map[string]int)
	moreToAdd := true
	for moreToAdd {
		moreToAdd = false
		for _, source := range sourceOrder {
			idx := sourceIdx[source]
			if idx < len(sourceItems[source]) && idx < maxPerSource {
				result = append(result, sourceItems[source][idx])
				sourceIdx[source]++
				moreToAdd = true
			}
		}
	}

	return result
}

// sourceActivity tracks recent activity for a source
type sourceActivity struct {
	recentCount int       // items in last hour
	lastSeen    time.Time // most recent item
}

// TopStory represents an AI-identified important story
type TopStory struct {
	Item      *feeds.Item
	Label     string    // "BREAKING", "DEVELOPING", "TOP STORY"
	Reason    string    // Why AI flagged this
	Zinger    string    // Punchy one-liner from local LLM
	Loading   bool
	HitCount  int       // How many times identified as top story
	FirstSeen time.Time // When first identified
	Streak    bool      // True if identified in consecutive analyses
	Status    string    // Lifecycle: "breaking", "developing", "persistent", "fading"
	MissCount int       // How many consecutive analyses missed this
}

// TopStoryLabel types (conservative - local LLM can be inconsistent)
const (
	LabelNew      = "● NEW"
	LabelEmerging = "◐ EMERGING"
	LabelOngoing  = "◉ ONGOING"
	LabelMajor    = "★ MAJOR"
	LabelSustained = "◑ SUSTAINED"
	LabelFading   = "○ FADING"
	LabelNoted    = "◦ NOTED"
)

// Sparkline characters (8 levels)
// renderActivityIndicator shows source activity as a heartbeat
func renderActivityIndicator(recentCount int) string {
	// More activity = more bars
	switch {
	case recentCount >= 10:
		return "▇▇▇" // Very active
	case recentCount >= 5:
		return "▅▅▅" // Active
	case recentCount >= 2:
		return "▃▃▃" // Moderate
	case recentCount >= 1:
		return "▁▁▁" // Low
	default:
		return "···" // Inactive
	}
}

// DensityMode controls how much space items take
type DensityMode int

const (
	DensityComfortable DensityMode = iota // Default - expanded selected items
	DensityCompact                        // Single line per item, minimal
)

// TopStoriesRefreshInterval is how often top stories auto-refresh
const TopStoriesRefreshInterval = 30 * time.Second

// Model is the stream view showing feed items flowing by
type Model struct {
	items        []feeds.Item
	categories   map[string]string         // item ID -> category lookup
	cursor       int
	selectedID   string                    // Track selected item ID for stability during updates
	width        int
	height       int
	loading      bool
	spinner      spinner.Model
	density      DensityMode
	sourceStats  map[string]sourceActivity // source name -> recent activity
	topStories   []TopStory                // AI-identified top stories
	topStoriesLoading bool                 // Whether top stories are being analyzed
	topStoriesLastRefresh time.Time        // When top stories were last refreshed

	// Correlation engine for duplicate/cluster indicators
	correlationEngine *correlation.Engine

	// Selection (time filter, source filter, etc.)
	activeSelector  selection.Selector // Current view filter (nil = all)
	selectorIndex   int                // Which preset is active (0=all, 1=just now, etc.)

	// Filtering by entity/cluster (from correlation)
	filterEntityID  string // Filter to show only items with this entity
	filterClusterID string // Filter to show only items in this cluster
	filterLabel     string // Human-readable filter description

	// Smooth scrolling with harmonica spring physics
	scrollSpring   harmonica.Spring
	scrollPos      float64 // Current animated scroll position
	scrollVelocity float64 // Current scroll velocity
	scrollTarget   float64 // Target scroll position (cursor)
}

// New creates a new stream model
func New() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))

	// Create a smooth spring for scrolling (frequency, damping)
	// Higher frequency = faster response, higher damping = less bounce
	spring := harmonica.NewSpring(harmonica.FPS(60), 6.0, 0.8)

	return Model{
		items:        make([]feeds.Item, 0),
		categories:   make(map[string]string),
		loading:      true,
		spinner:      s,
		density:      DensityComfortable,
		sourceStats:  make(map[string]sourceActivity),
		scrollSpring: spring,
	}
}

// SetSelector sets the active time/source selector
func (m *Model) SetSelector(sel selection.Selector, index int) {
	m.activeSelector = sel
	m.selectorIndex = index
}

// GetSelectorIndex returns the current selector index
func (m Model) GetSelectorIndex() int {
	return m.selectorIndex
}

// CycleSelector cycles through built-in time selectors
func (m *Model) CycleSelector() {
	selectors := []selection.Selector{nil} // nil = All
	selectors = append(selectors, selection.TimeSelectors()...)

	m.selectorIndex = (m.selectorIndex + 1) % len(selectors)
	m.activeSelector = selectors[m.selectorIndex]
}

// GetSelectorName returns the current selector name
func (m Model) GetSelectorName() string {
	if m.activeSelector == nil {
		return "All"
	}
	return m.activeSelector.Name()
}

// ToggleDensity switches between compact and comfortable modes
func (m *Model) ToggleDensity() {
	if m.density == DensityComfortable {
		m.density = DensityCompact
	} else {
		m.density = DensityComfortable
	}
}

// SetDensity sets the density mode
func (m *Model) SetDensity(d DensityMode) {
	m.density = d
}

// Density returns the current density mode
func (m Model) Density() DensityMode {
	return m.density
}

// DensityLabel returns a human-readable density mode name
func (m Model) DensityLabel() string {
	if m.density == DensityCompact {
		return "Compact"
	}
	return "Comfortable"
}

// SetSize updates the viewport dimensions
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetItems replaces the current items
func (m *Model) SetItems(items []feeds.Item) {
	// Remember currently selected item ID for stability
	var previousSelectedID string
	if m.cursor >= 0 && m.cursor < len(m.items) {
		previousSelectedID = m.items[m.cursor].ID
	}
	if m.selectedID != "" {
		previousSelectedID = m.selectedID
	}

	// Interleave sources for diversity (no time band grouping)
	m.items = interleaveBySource(items, 50) // Max 50 items per source total
	m.loading = false

	// Try to restore cursor to the same item (by ID) after update
	foundPrevious := false
	if previousSelectedID != "" {
		for i, item := range m.items {
			if item.ID == previousSelectedID {
				m.cursor = i
				m.selectedID = previousSelectedID
				foundPrevious = true
				break
			}
		}
	}

	// Item not found in new list, clamp cursor to valid range
	if !foundPrevious && m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}

	// Calculate source activity stats
	m.sourceStats = make(map[string]sourceActivity)
	oneHourAgo := time.Now().Add(-time.Hour)
	for _, item := range m.items {
		stats := m.sourceStats[item.SourceName]
		if item.Published.After(oneHourAgo) {
			stats.recentCount++
		}
		if stats.lastSeen.IsZero() || item.Published.After(stats.lastSeen) {
			stats.lastSeen = item.Published
		}
		m.sourceStats[item.SourceName] = stats
	}
}

// SetTopStories sets the AI-identified top stories with deduplication and source balancing
func (m *Model) SetTopStories(stories []TopStory) {
	if stories == nil {
		m.topStories = nil
		m.topStoriesLoading = false
		m.topStoriesLastRefresh = time.Now()
		return
	}

	// Deduplicate by title prefix (catches same story from different sources)
	seen := make(map[string]bool)
	var dedupedStories []TopStory

	for _, story := range stories {
		if story.Item == nil {
			continue
		}

		// Use first 40 chars of title as key for deduplication
		titleKey := strings.ToLower(story.Item.Title)
		if len(titleKey) > 40 {
			titleKey = titleKey[:40]
		}

		if seen[titleKey] {
			continue // Skip duplicate
		}
		seen[titleKey] = true
		dedupedStories = append(dedupedStories, story)
	}

	// Balance sources - no single source should dominate
	m.topStories = balanceTopStoriesBySources(dedupedStories, 2) // max 2 per source
	m.topStoriesLoading = false
	m.topStoriesLastRefresh = time.Now()
}

// balanceTopStoriesBySources limits stories per source for uniform representation
// Prioritizes higher-confidence stories (by status/hit count) while ensuring diversity
func balanceTopStoriesBySources(stories []TopStory, maxPerSource int) []TopStory {
	if len(stories) <= maxPerSource {
		return stories
	}

	// Group by source, preserving order (which is already priority-sorted)
	sourceStories := make(map[string][]TopStory)
	var sourceOrder []string
	for _, story := range stories {
		if story.Item == nil {
			continue
		}
		source := story.Item.SourceName
		if _, exists := sourceStories[source]; !exists {
			sourceOrder = append(sourceOrder, source)
		}
		sourceStories[source] = append(sourceStories[source], story)
	}

	// Round-robin through sources, taking one at a time up to maxPerSource each
	var balanced []TopStory
	sourceIdx := make(map[string]int)

	// Keep going until we've exhausted all sources or hit the original count
	for len(balanced) < len(stories) {
		added := false
		for _, source := range sourceOrder {
			idx := sourceIdx[source]
			if idx < len(sourceStories[source]) && idx < maxPerSource {
				balanced = append(balanced, sourceStories[source][idx])
				sourceIdx[source]++
				added = true
			}
		}
		if !added {
			break // All sources exhausted within their limits
		}
	}

	return balanced
}

// TopStoriesNeedsRefresh returns true if top stories should be refreshed
// Triggers 5 seconds early for smooth transition (fetches while countdown shows 5...4...3...)
func (m Model) TopStoriesNeedsRefresh() bool {
	if m.topStoriesLoading {
		return false
	}
	if m.topStoriesLastRefresh.IsZero() {
		return true
	}
	// Trigger 5 seconds early so new data arrives smoothly
	earlyTrigger := TopStoriesRefreshInterval - 5*time.Second
	return time.Since(m.topStoriesLastRefresh) >= earlyTrigger
}

// TopStoriesRefreshProgress returns progress 0.0-1.0 until next refresh
func (m Model) TopStoriesRefreshProgress() float64 {
	if m.topStoriesLastRefresh.IsZero() || m.topStoriesLoading {
		return 0
	}
	elapsed := time.Since(m.topStoriesLastRefresh)
	progress := float64(elapsed) / float64(TopStoriesRefreshInterval)
	if progress > 1 {
		progress = 1
	}
	return progress
}

// TopStoriesTimeUntilRefresh returns seconds until next refresh
func (m Model) TopStoriesTimeUntilRefresh() int {
	if m.topStoriesLastRefresh.IsZero() || m.topStoriesLoading {
		return 0
	}
	remaining := TopStoriesRefreshInterval - time.Since(m.topStoriesLastRefresh)
	if remaining < 0 {
		return 0
	}
	return int(remaining.Seconds())
}

// SetTopStoriesLoading sets the loading state for top stories
func (m *Model) SetTopStoriesLoading(loading bool) {
	m.topStoriesLoading = loading
}

// ResetTopStoriesRefresh resets the refresh timer without clearing stories
// Use this when analysis fails but we want to keep showing old stories
func (m *Model) ResetTopStoriesRefresh() {
	m.topStoriesLoading = false
	m.topStoriesLastRefresh = time.Now()
}

// GetTopStories returns the current top stories
func (m Model) GetTopStories() []TopStory {
	return m.topStories
}

// GetRecentItems returns items from the last N hours for AI analysis
func (m Model) GetRecentItems(hours int) []feeds.Item {
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	var recent []feeds.Item
	for _, item := range m.items {
		if item.Published.After(cutoff) {
			recent = append(recent, item)
		}
	}
	return recent
}

// SetItemCategory sets the category for an item (for coloring)
func (m *Model) SetItemCategory(itemID, category string) {
	m.categories[itemID] = category
}

// SetCorrelationEngine sets the correlation engine for duplicate/cluster display
func (m *Model) SetCorrelationEngine(engine *correlation.Engine) {
	m.correlationEngine = engine
}

// SetFilterByEntity filters to show only items containing the given entity
func (m *Model) SetFilterByEntity(entityID, label string) {
	m.filterEntityID = entityID
	m.filterClusterID = ""
	m.filterLabel = label
	m.cursor = 0
}

// SetFilterByCluster filters to show only items in the given cluster
func (m *Model) SetFilterByCluster(clusterID, label string) {
	m.filterClusterID = clusterID
	m.filterEntityID = ""
	m.filterLabel = label
	m.cursor = 0
}

// ClearFilter removes any active filter
func (m *Model) ClearFilter() {
	m.filterEntityID = ""
	m.filterClusterID = ""
	m.filterLabel = ""
}

// HasFilter returns true if a filter is active
func (m Model) HasFilter() bool {
	return m.filterEntityID != "" || m.filterClusterID != ""
}

// FilterLabel returns the current filter description
func (m Model) FilterLabel() string {
	return m.filterLabel
}

// getFilteredItems returns items matching the current filter
func (m Model) getFilteredItems() []feeds.Item {
	items := m.items

	// Apply time/source selector first
	if m.activeSelector != nil {
		items = selection.Apply(items, m.activeSelector)
	}

	// Apply entity/cluster filter if set
	if m.HasFilter() && m.correlationEngine != nil {
		var filtered []feeds.Item
		for _, item := range items {
			if m.filterEntityID != "" {
				// Check if item has this entity
				entities := m.correlationEngine.GetItemEntities(item.ID)
				for _, e := range entities {
					if e.ID == m.filterEntityID {
						filtered = append(filtered, item)
						break
					}
				}
			} else if m.filterClusterID != "" {
				// Check if item is in this cluster
				cluster := m.correlationEngine.GetClusterInfo(item.ID)
				if cluster != nil && cluster.ID == m.filterClusterID {
					filtered = append(filtered, item)
				}
			}
		}
		items = filtered
	}

	return items
}

// SetLoading sets the loading state
func (m *Model) SetLoading(loading bool) {
	m.loading = loading
}

// MoveUp moves cursor up with smooth scrolling
func (m *Model) MoveUp() {
	if m.cursor > 0 {
		m.cursor--
		m.scrollTarget = float64(m.cursor)
		// Update selectedID for stability during feed updates
		items := m.getFilteredItems()
		if m.cursor >= 0 && m.cursor < len(items) {
			m.selectedID = items[m.cursor].ID
		}
	}
}

// MoveDown moves cursor down with smooth scrolling
func (m *Model) MoveDown() {
	items := m.getFilteredItems()
	if m.cursor < len(items)-1 {
		m.cursor++
		m.scrollTarget = float64(m.cursor)
		// Update selectedID for stability during feed updates
		if m.cursor >= 0 && m.cursor < len(items) {
			m.selectedID = items[m.cursor].ID
		}
	}
}

// UpdateScroll updates the smooth scroll animation (call on each frame)
func (m *Model) UpdateScroll() {
	// Update spring physics (position, velocity, target)
	m.scrollPos, m.scrollVelocity = m.scrollSpring.Update(m.scrollPos, m.scrollVelocity, m.scrollTarget)
}

// ScrollOffset returns the current animated scroll offset for smooth rendering
func (m Model) ScrollOffset() float64 {
	return m.scrollPos
}

// IsScrolling returns true if scroll animation is in progress
func (m Model) IsScrolling() bool {
	return math.Abs(m.scrollPos-m.scrollTarget) > 0.01
}

// SelectedItem returns the currently selected item, if any
func (m *Model) SelectedItem() *feeds.Item {
	items := m.getFilteredItems()
	if m.cursor >= 0 && m.cursor < len(items) {
		// Need to find the item in the original slice to return a stable pointer
		for i := range m.items {
			if m.items[i].ID == items[m.cursor].ID {
				return &m.items[i]
			}
		}
	}
	return nil
}

// MarkSelectedRead marks the selected item as read
func (m *Model) MarkSelectedRead() {
	if item := m.SelectedItem(); item != nil {
		item.Read = true
	}
}

// ItemCount returns the number of items currently displayed in the stream
func (m *Model) ItemCount() int {
	return len(m.items)
}

// Spinner returns the spinner model
func (m Model) Spinner() spinner.Model {
	return m.spinner
}

// UpdateSpinner updates the spinner state
func (m *Model) UpdateSpinner(s spinner.Model) {
	m.spinner = s
}

// View renders the stream
func (m Model) View() string {
	if m.loading {
		return m.renderLoading()
	}

	if len(m.items) == 0 {
		return m.renderEmpty()
	}

	// Render top stories as fixed header (ALWAYS visible)
	topLines := m.renderTopStoriesSection()

	// Add filter indicator if active
	if m.HasFilter() {
		filterStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#a371f7")).
			Background(lipgloss.Color("#1f1d2e")).
			Padding(0, 1)
		clearHint := lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681")).Render(" (Esc to clear)")
		topLines = append(topLines, filterStyle.Render("🔍 "+m.filterLabel)+clearHint)
	}

	topSection := strings.Join(topLines, "\n")
	topSectionHeight := len(topLines) + 2 // +2 for padding
	topSection = topSection + "\n\n" // Add spacing after

	// Render scrollable items (with reduced height to account for fixed header)
	items := m.renderItemsOnly(topSectionHeight)

	return topSection + items
}

func (m Model) renderLoading() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b949e"))

	content := fmt.Sprintf("%s Loading feeds...", m.spinner.View())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, style.Render(content))
}

func (m Model) renderEmpty() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b949e"))

	content := "No items yet. Press R to refresh."
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, style.Render(content))
}

func (m Model) renderTopStoriesSection() []string {
	var lines []string

	// Section header with refresh timer
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f85149")).
		Bold(true)
	dividerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f85149"))

	// Build header with refresh progress bar
	headerText := "━━━ TOP STORIES ━━━"
	if len(m.topStories) > 0 && !m.topStoriesLoading {
		// Add a cool refresh timer widget
		timerWidget := m.renderRefreshTimer()
		headerText = fmt.Sprintf("━━━ TOP STORIES %s ━━━", timerWidget)
	}
	lines = append(lines, headerStyle.Render(headerText))

	if m.topStoriesLoading {
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e")).
			Italic(true)
		lines = append(lines, loadingStyle.Render(fmt.Sprintf("  %s Analyzing headlines...", m.spinner.View())))
		return lines
	}

	if len(m.topStories) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e"))
		lines = append(lines, dimStyle.Render("  Press T to analyze headlines for breaking news"))
		lines = append(lines, dividerStyle.Render(strings.Repeat("─", min(m.width-4, 60))))
		return lines
	}

	// Render each top story
	for _, story := range m.topStories {
		if story.Item == nil {
			continue
		}

		// Determine if this story is fading (dimmed styling)
		// Note: sustained stories (missCount==1) are NOT fading - they're still important
		isFading := story.Status == "fading" || story.MissCount >= 2

		// Label with appropriate color based on status
		// Conservative styling - only use bold/prominent colors for high-confidence stories
		var labelStyle lipgloss.Style
		switch {
		case strings.Contains(story.Label, "NEW"):
			// Single hit - neutral styling, might be noise
			labelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c9d1d9")).
				Background(lipgloss.Color("#30363d")).
				Padding(0, 1)
		case strings.Contains(story.Label, "EMERGING"):
			// 2-3 hits - starting to look real, slightly more prominent
			labelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#0d1117")).
				Background(lipgloss.Color("#d29922")).
				Padding(0, 1)
		case strings.Contains(story.Label, "MAJOR"):
			// High confidence (6+ hits) - use prominent styling
			labelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#0d1117")).
				Background(lipgloss.Color("#f85149")).
				Bold(true).
				Padding(0, 1)
		case strings.Contains(story.Label, "ONGOING"):
			// Persistent (4+ hits) - confirmed important
			labelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#0d1117")).
				Background(lipgloss.Color("#f0883e")).
				Padding(0, 1)
		case strings.Contains(story.Label, "SUSTAINED"):
			// Was persistent, missed once - still tracking
			labelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#0d1117")).
				Background(lipgloss.Color("#58a6ff")).
				Padding(0, 1)
		case strings.Contains(story.Label, "FADING"):
			// Missed 2+ times - dimmed
			labelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b949e")).
				Background(lipgloss.Color("#21262d")).
				Padding(0, 1)
		default:
			// NOTED or unknown - neutral
			labelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b949e")).
				Background(lipgloss.Color("#30363d")).
				Padding(0, 1)
		}

		// Title and source styling - dimmed for fading stories
		var titleStyle, sourceStyle lipgloss.Style
		if isFading {
			titleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6e7681"))
			sourceStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#484f58"))
		} else {
			titleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c9d1d9")).
				Bold(true)
			sourceStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b949e"))
		}

		// Build streak/hit count indicator
		streakIndicator := ""
		if story.HitCount > 1 {
			var streakStyle lipgloss.Style
			if story.HitCount >= 5 {
				// High confidence - bright fire
				streakStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149")).Bold(true)
				streakIndicator = streakStyle.Render(fmt.Sprintf(" 🔥×%d", story.HitCount))
			} else if story.Streak {
				// Consecutive identification - show fire
				streakStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff7b72")).Bold(true)
				streakIndicator = streakStyle.Render(fmt.Sprintf(" 🔥×%d", story.HitCount))
			} else {
				// Non-consecutive - just show count, dimmer if fading
				if isFading {
					streakStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681"))
				} else {
					streakStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e"))
				}
				streakIndicator = streakStyle.Render(fmt.Sprintf(" ×%d", story.HitCount))
			}
		}

		// Format: [LABEL] Title · Source [streak]
		label := labelStyle.Render(story.Label)
		title := titleStyle.Render(truncate(story.Item.Title, m.width-35))
		source := sourceStyle.Render(" · " + story.Item.SourceName)

		lines = append(lines, fmt.Sprintf("  %s %s%s%s", label, title, source, streakIndicator))

		// Show zinger (preferred) or reason, skip for fading stories
		displayText := story.Zinger
		if displayText == "" {
			displayText = story.Reason
		}
		if displayText != "" && !isFading {
			zingerStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#58a6ff")). // Blue for zingers - more prominent
				Italic(true)
			// If high hit count, show how long story has been top
			extraInfo := ""
			if story.HitCount > 1 && !story.FirstSeen.IsZero() {
				duration := time.Since(story.FirstSeen)
				if duration > time.Hour {
					extraInfo = fmt.Sprintf(" (top for %dh)", int(duration.Hours()))
				} else if duration > time.Minute {
					extraInfo = fmt.Sprintf(" (top for %dm)", int(duration.Minutes()))
				}
			}
			lines = append(lines, zingerStyle.Render("    "+truncate(displayText, m.width-20)+extraInfo))
		}
	}

	lines = append(lines, dividerStyle.Render(strings.Repeat("─", min(m.width-4, 60))))

	return lines
}

// renderRefreshTimer creates a cool animated progress bar for refresh countdown
func (m Model) renderRefreshTimer() string {
	progress := m.TopStoriesRefreshProgress()
	remaining := m.TopStoriesTimeUntilRefresh()

	// Progress bar width
	barWidth := 10

	// Use block characters for smooth progress: ▓░ or ━╺
	filled := int(float64(barWidth) * progress)
	empty := barWidth - filled

	// Color gradient from green (fresh) to yellow to red (stale)
	var barColor lipgloss.Color
	if progress < 0.5 {
		barColor = lipgloss.Color("#3fb950") // Green - fresh
	} else if progress < 0.8 {
		barColor = lipgloss.Color("#d29922") // Yellow - getting stale
	} else {
		barColor = lipgloss.Color("#f85149") // Red - about to refresh
	}

	filledStyle := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#30363d"))
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e"))

	// Build the bar with cool characters
	bar := filledStyle.Render(strings.Repeat("━", filled)) +
		emptyStyle.Render(strings.Repeat("╌", empty))

	// Format: [━━━━━━╌╌╌╌ 15s]
	return fmt.Sprintf("[%s %s]", bar, timeStyle.Render(fmt.Sprintf("%ds", remaining)))
}

// renderItemsOnly renders just the scrollable items list (without top stories header)
func (m Model) renderItemsOnly(headerHeight int) string {
	var lines []string

	// Calculate how many lines we can show (minus fixed header)
	availableHeight := m.height - 2 - headerHeight // Leave room for scroll indicator and header

	// Get items (filtered if filter is active)
	items := m.getFilteredItems()

	// Build all renderable content with item indices
	// Selected items may have multiple lines (title + summary) in comfortable mode
	type renderedBlock struct {
		lines     []string
		itemIndex int // -1 for dividers/spacing
	}
	var allBlocks []renderedBlock

	for i, item := range items {
		// In compact mode, skip rendering read items fully - just show minimal
		if m.density == DensityCompact && item.Read && i != m.cursor {
			// Ultra-minimal read item
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#30363d"))
			shortTitle := truncate(item.Title, 40)
			allBlocks = append(allBlocks, renderedBlock{[]string{dimStyle.Render("  · " + shortTitle)}, i})
			continue
		}

		// Render the item
		selected := i == m.cursor
		rendered := m.renderItem(item, selected)

		// Split into lines (selected items may have multiple)
		itemLines := strings.Split(rendered, "\n")
		allBlocks = append(allBlocks, renderedBlock{itemLines, i})
	}

	// Flatten blocks to lines with tracking
	type lineInfo struct {
		content   string
		itemIndex int
	}
	var allLines []lineInfo
	for _, block := range allBlocks {
		for _, line := range block.lines {
			allLines = append(allLines, lineInfo{line, block.itemIndex})
		}
	}

	// Find the line index where cursor item starts
	cursorLineIdx := 0
	for i, li := range allLines {
		if li.itemIndex == m.cursor {
			cursorLineIdx = i
			break
		}
	}

	// Calculate visible range centered on cursor
	startLine := cursorLineIdx - availableHeight/2
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine + availableHeight
	if endLine > len(allLines) {
		endLine = len(allLines)
		startLine = max(0, endLine-availableHeight)
	}

	// Collect visible lines
	for i := startLine; i < endLine; i++ {
		lines = append(lines, allLines[i].content)
	}

	// Scroll indicator with density mode and active selector
	scrollInfo := ""
	if len(m.items) > 0 {
		pct := float64(m.cursor) / float64(max(1, len(m.items)-1)) * 100
		densityIndicator := "◉" // comfortable
		if m.density == DensityCompact {
			densityIndicator = "◎" // compact
		}
		// Show active selector if one is set
		selectorInfo := ""
		if m.activeSelector != nil {
			selectorInfo = fmt.Sprintf(" [%s]", m.activeSelector.Name())
		}
		scrollInfo = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#484f58")).
			Render(fmt.Sprintf(" %s %d/%d (%.0f%%)%s", densityIndicator, m.cursor+1, len(m.items), pct, selectorInfo))
	}

	content := strings.Join(lines, "\n")
	if scrollInfo != "" {
		content += "\n" + scrollInfo
	}

	return content
}

func (m Model) renderItem(item feeds.Item, selected bool) string {
	// Get category color
	category := m.categories[item.ID]
	if category == "" {
		category = deriveCategoryFromSource(item.SourceName, string(item.Source))
	}
	catColor, ok := categoryColors[category]
	if !ok {
		catColor = lipgloss.Color("#8b949e")
	}

	// Time formatting
	age := time.Since(item.Published)
	timeStr := formatAge(age)

	// Source name - use abbreviation if available
	sourceName := getSourceAbbrev(item.SourceName)

	// Determine if this is a "breaking" wire service item
	isBreaking := isBreakingNews(item, category, age)

	// In compact mode, simplify everything
	if m.density == DensityCompact && !selected {
		return m.renderCompactItem(item, sourceName, timeStr, catColor, isBreaking, age)
	}

	// Source badge with category color
	badgeBg := catColor
	badgeFg := lipgloss.Color("#0d1117")
	if isBreaking {
		// Breaking news gets pulsing red treatment
		badgeBg = lipgloss.Color("#f85149")
	}
	sourceBadge := lipgloss.NewStyle().
		Foreground(badgeFg).
		Background(badgeBg).
		Padding(0, 1).
		Render(sourceName)

	// Time stamp style - dimmer for older items
	timeColor := lipgloss.Color("#484f58")
	if age > 24*time.Hour {
		timeColor = lipgloss.Color("#30363d") // Extra dim for old items
	}
	timeStyle := lipgloss.NewStyle().Foreground(timeColor)

	// Fresh indicator - only for < 10 minutes
	freshIndicator := ""
	if age < 10*time.Minute {
		freshIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3fb950")).
			Bold(true).
			Render(" ●")
	}

	// Breaking indicator
	breakingIndicator := ""
	if isBreaking {
		breakingIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f85149")).
			Bold(true).
			Render(" ⚡")
	}

	// Duplicate indicator (×N)
	duplicateIndicator := ""
	if m.correlationEngine != nil {
		dupCount := m.correlationEngine.GetDuplicateCount(item.ID)
		if dupCount > 0 {
			// Only show if this is the primary item in the group
			if m.correlationEngine.IsPrimaryInGroup(item.ID) {
				duplicateIndicator = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#8b949e")).
					Render(fmt.Sprintf(" ×%d", dupCount+1)) // +1 to include this item
			}
		}
	}

	// Cluster indicator (◐ N) - shows how many sources cover this story
	clusterIndicator := ""
	if m.correlationEngine != nil {
		if cluster := m.correlationEngine.GetClusterInfo(item.ID); cluster != nil {
			if m.correlationEngine.IsClusterPrimary(item.ID) && cluster.Size > 1 {
				indicatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
				clusterIndicator = indicatorStyle.Render(fmt.Sprintf(" ◐%d", cluster.Size))
			}
		}
	}

	// Disagreement indicator (⚡) - shown when sources conflict
	disagreementIndicator := ""
	if m.correlationEngine != nil {
		if m.correlationEngine.ItemHasDisagreement(item.ID) {
			disagreementIndicator = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d29922")). // Yellow/orange for attention
				Bold(true).
				Render(" ⚡")
		}
	}

	// Title width calculation
	badgeWidth := lipgloss.Width(sourceBadge)
	timeWidth := len(timeStr) + 2
	indicatorWidth := 0
	if freshIndicator != "" {
		indicatorWidth += 3
	}
	if breakingIndicator != "" {
		indicatorWidth += 3
	}
	if duplicateIndicator != "" {
		indicatorWidth += 5 // " ×N" is roughly 4-5 chars
	}
	if clusterIndicator != "" {
		indicatorWidth += 5 // " ◐N" is roughly 4-5 chars
	}
	if disagreementIndicator != "" {
		indicatorWidth += 3 // " ⚡"
	}
	maxTitleWidth := m.width - badgeWidth - timeWidth - indicatorWidth - 8
	if maxTitleWidth < 20 {
		maxTitleWidth = 20
	}
	title := truncate(item.Title, maxTitleWidth)

	// Build the line based on state
	if selected {
		return m.renderSelectedItem(item, sourceBadge, title, timeStr, freshIndicator, breakingIndicator, catColor, age, category)
	}

	// Determine title color based on age
	titleColor := lipgloss.Color("#c9d1d9")
	if age > 24*time.Hour {
		titleColor = lipgloss.Color("#8b949e") // Dimmed for old
	}

	if item.Read {
		// Read: dimmed everything
		titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#484f58"))
		dimBadge := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#484f58")).
			Background(lipgloss.Color("#21262d")).
			Padding(0, 1).
			Render(sourceName)

		line := fmt.Sprintf("  %s  %s", dimBadge, titleStyle.Render(title))
		lineWidth := lipgloss.Width(line)
		padding := m.width - lineWidth - len(timeStr) - 4
		if padding < 1 {
			padding = 1
		}
		return line + strings.Repeat(" ", padding) + timeStyle.Render(timeStr)
	}

	// Normal item
	indicator := freshIndicator
	if breakingIndicator != "" {
		indicator = breakingIndicator
	}

	// Build indicators string
	indicators := indicator + duplicateIndicator + clusterIndicator + disagreementIndicator

	titleStyle := lipgloss.NewStyle().Foreground(titleColor)
	line := fmt.Sprintf("  %s  %s%s", sourceBadge, titleStyle.Render(title), indicators)

	lineWidth := lipgloss.Width(line)
	padding := m.width - lineWidth - len(timeStr) - 4
	if padding < 1 {
		padding = 1
	}
	return line + strings.Repeat(" ", padding) + timeStyle.Render(timeStr)
}

// renderCompactItem renders a minimal single-line item for compact mode
func (m Model) renderCompactItem(item feeds.Item, sourceName, timeStr string, catColor lipgloss.Color, isBreaking bool, age time.Duration) string {
	// Minimal badge - just colored text, no background
	sourceStyle := lipgloss.NewStyle().Foreground(catColor)
	if isBreaking {
		sourceStyle = sourceStyle.Foreground(lipgloss.Color("#f85149")).Bold(true)
	}

	// Shorter title in compact mode
	maxTitleWidth := m.width - len(sourceName) - len(timeStr) - 10
	if maxTitleWidth < 20 {
		maxTitleWidth = 20
	}
	title := truncate(item.Title, maxTitleWidth)

	// Determine title color
	titleColor := lipgloss.Color("#c9d1d9")
	if age > 24*time.Hour {
		titleColor = lipgloss.Color("#6e7681")
	}
	if item.Read {
		titleColor = lipgloss.Color("#484f58")
	}
	titleStyle := lipgloss.NewStyle().Foreground(titleColor)

	// Time style
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#30363d"))

	// Single compact line
	line := fmt.Sprintf("  %s %s", sourceStyle.Render(sourceName), titleStyle.Render(title))
	lineWidth := lipgloss.Width(line)
	padding := m.width - lineWidth - len(timeStr) - 4
	if padding < 1 {
		padding = 1
	}

	return line + strings.Repeat(" ", padding) + timeStyle.Render(timeStr)
}

// renderSelectedItem renders an expanded selected item with summary
func (m Model) renderSelectedItem(item feeds.Item, sourceBadge, title, timeStr, freshIndicator, breakingIndicator string, catColor lipgloss.Color, age time.Duration, category string) string {
	// Selection highlight style - clear visual indicator
	selectionBg := lipgloss.Color("#1f3a5f") // Deep blue background
	selectionStyle := lipgloss.NewStyle().
		Background(selectionBg).
		Foreground(lipgloss.Color("#ffffff"))

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6e7681")).
		Background(selectionBg)

	// In compact mode, just highlight but don't expand
	if m.density == DensityCompact {
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Background(selectionBg).
			Bold(true)

		indicator := freshIndicator
		if breakingIndicator != "" {
			indicator = breakingIndicator
		}

		line := fmt.Sprintf("▶ %s  %s%s", sourceBadge, titleStyle.Render(title), indicator)
		lineWidth := lipgloss.Width(line)
		padding := m.width - lineWidth - len(timeStr) - 4
		if padding < 1 {
			padding = 1
		}

		fullLine := line + strings.Repeat(" ", padding) + timeStyle.Render(timeStr)
		// Pad to full width with selection background
		fullLine = selectionStyle.Width(m.width - 2).Render(fullLine)
		return fullLine
	}

	// Title line - bright and bold with selection background
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Background(selectionBg).
		Bold(true)

	indicator := freshIndicator
	if breakingIndicator != "" {
		indicator = breakingIndicator
	}

	// Selection marker
	markerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#58a6ff")).
		Background(selectionBg).
		Bold(true)

	titleLine := fmt.Sprintf("%s %s  %s%s", markerStyle.Render("▶"), sourceBadge, titleStyle.Render(title), indicator)
	titleLineWidth := lipgloss.Width(titleLine)
	padding := m.width - titleLineWidth - len(timeStr) - 6
	if padding < 1 {
		padding = 1
	}
	titleLine += strings.Repeat(" ", padding) + timeStyle.Render(timeStr)
	// Pad title line to full width
	titleLine = selectionStyle.Width(m.width - 2).Render(titleLine)

	// Summary line - extract and display if available
	summaryLine := ""
	if item.Summary != "" {
		// Clean and truncate summary
		summary := cleanSummary(item.Summary)
		maxSummaryWidth := m.width - 12
		if len(summary) > maxSummaryWidth {
			summary = summary[:maxSummaryWidth-2] + ".."
		}
		if summary != "" {
			summaryStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#a8b1bb")).
				Background(selectionBg).
				Italic(true)
			summaryLine = selectionStyle.Width(m.width - 2).Render("     " + summaryStyle.Render(summary))
		}
	}

	// Sparkline for prediction markets - extract probability from title/summary
	sparklineLine := ""
	if category == "predictions" {
		prob := extractProbability(item.Title + " " + item.Summary)
		if prob >= 0 {
			sparklineStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3fb950")).
				Background(selectionBg)
			probStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7ee787")).
				Background(selectionBg).
				Bold(true)

			// Show probability bar
			barWidth := 20
			filled := int(float64(barWidth) * (float64(prob) / 100.0))
			if filled > barWidth {
				filled = barWidth
			}
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

			sparklineLine = selectionStyle.Width(m.width - 2).Render(fmt.Sprintf("     %s %s",
				sparklineStyle.Render(bar),
				probStyle.Render(fmt.Sprintf("%d%%", prob))))
		}
	}

	// Source activity indicator (if active source)
	activityLine := ""
	if stats, ok := m.sourceStats[item.SourceName]; ok && stats.recentCount >= 3 {
		activityStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#58a6ff")).
			Background(selectionBg)
		activityIndicator := renderActivityIndicator(stats.recentCount)
		activityLine = selectionStyle.Width(m.width - 2).Render(fmt.Sprintf("     %s %d items in last hour",
			activityStyle.Render(activityIndicator),
			stats.recentCount))
	}

	// Entity pills line (tickers, countries)
	entityLine := ""
	if m.correlationEngine != nil {
		entities := m.correlationEngine.GetItemEntities(item.ID)
		if len(entities) > 0 {
			var pills []string
			seen := make(map[string]bool)
			for _, ent := range entities {
				if seen[ent.ID] || len(pills) >= 5 {
					continue
				}
				seen[ent.ID] = true

				pill := m.renderEntityPill(ent.ID, selectionBg)
				if pill != "" {
					pills = append(pills, pill)
				}
			}
			if len(pills) > 0 {
				entityLine = selectionStyle.Width(m.width - 2).Render("     " + strings.Join(pills, " "))
			}
		}
	}

	// URL hint
	urlHint := ""
	if item.URL != "" {
		domain := extractDomain(item.URL)
		if domain != "" {
			urlStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#58a6ff")).
				Background(selectionBg).
				Underline(true)
			urlHint = selectionStyle.Width(m.width - 2).Render("     " + urlStyle.Render(domain))
		}
	}

	// Combine lines - each line already has the selection background
	var lines []string
	lines = append(lines, titleLine)
	if entityLine != "" {
		lines = append(lines, entityLine)
	}
	if sparklineLine != "" {
		lines = append(lines, sparklineLine)
	}
	if summaryLine != "" {
		lines = append(lines, summaryLine)
	}
	if activityLine != "" {
		lines = append(lines, activityLine)
	}
	if urlHint != "" && summaryLine == "" && sparklineLine == "" {
		lines = append(lines, urlHint)
	}

	// Join with newlines - container provides left border
	content := strings.Join(lines, "\n")

	// Container with category-colored left border and selection background
	containerStyle := lipgloss.NewStyle().
		Background(selectionBg).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(catColor).
		Width(m.width - 2)

	return containerStyle.Render(content)
}

// isBreakingNews determines if an item should get "breaking" treatment
func isBreakingNews(item feeds.Item, category string, age time.Duration) bool {
	// Only recent items can be "breaking"
	if age > 30*time.Minute {
		return false
	}

	// Wire services get breaking treatment
	if category == "wire" {
		return true
	}

	// Check for breaking keywords in title
	titleLower := strings.ToLower(item.Title)
	breakingKeywords := []string{"breaking", "just in", "urgent", "alert", "developing"}
	for _, kw := range breakingKeywords {
		if strings.Contains(titleLower, kw) {
			return true
		}
	}

	return false
}

// cleanSummary removes HTML and cleans up summary text
func cleanSummary(s string) string {
	// Remove HTML tags (simple approach)
	result := s
	for {
		start := strings.Index(result, "<")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], ">")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}

	// Decode common HTML entities
	result = strings.ReplaceAll(result, "&#39;", "'")
	result = strings.ReplaceAll(result, "&#34;", "\"")
	result = strings.ReplaceAll(result, "&quot;", "\"")
	result = strings.ReplaceAll(result, "&apos;", "'")
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&nbsp;", " ")
	result = strings.ReplaceAll(result, "&#x27;", "'")
	result = strings.ReplaceAll(result, "&#x22;", "\"")
	result = strings.ReplaceAll(result, "&mdash;", "-")
	result = strings.ReplaceAll(result, "&ndash;", "-")
	result = strings.ReplaceAll(result, "&hellip;", "...")
	result = strings.ReplaceAll(result, "&ldquo;", "\"")
	result = strings.ReplaceAll(result, "&rdquo;", "\"")
	result = strings.ReplaceAll(result, "&lsquo;", "'")
	result = strings.ReplaceAll(result, "&rsquo;", "'")

	// Remove extra whitespace
	result = strings.Join(strings.Fields(result), " ")

	// Remove common RSS cruft
	result = strings.TrimPrefix(result, "Comments")
	result = strings.TrimSpace(result)

	return result
}

// extractDomain pulls the domain from a URL
func extractDomain(url string) string {
	// Remove protocol
	domain := url
	if idx := strings.Index(domain, "://"); idx != -1 {
		domain = domain[idx+3:]
	}
	// Remove path
	if idx := strings.Index(domain, "/"); idx != -1 {
		domain = domain[:idx]
	}
	// Remove www.
	domain = strings.TrimPrefix(domain, "www.")
	return domain
}

func getSourceAbbrev(name string) string {
	if abbrev, ok := sourceAbbrevs[name]; ok {
		return abbrev
	}
	// Smart truncation: keep it readable
	if len(name) > 12 {
		// Try to find a natural break point
		if idx := strings.Index(name, " "); idx > 0 && idx < 10 {
			return name[:idx]
		}
		return name[:10] + ".."
	}
	return name
}

func deriveCategoryFromSource(sourceName, sourceType string) string {
	// Try to derive category from source name patterns
	nameLower := strings.ToLower(sourceName)

	switch {
	case strings.HasPrefix(nameLower, "r/"):
		return "reddit"
	case strings.Contains(nameLower, "arxiv"):
		return "arxiv"
	case strings.Contains(nameLower, "sec ") || strings.Contains(nameLower, "edgar"):
		return "sec"
	case strings.Contains(nameLower, "polymarket") || strings.Contains(nameLower, "manifold"):
		return "predictions"
	case strings.Contains(nameLower, "usgs"):
		return "events"
	case strings.Contains(nameLower, "google news"):
		return "aggregator"
	case strings.Contains(nameLower, "techmeme") || strings.Contains(nameLower, "memeorandum"):
		return "aggregator"
	case strings.Contains(nameLower, "daily dot") || strings.Contains(nameLower, "buzzfeed") ||
		strings.Contains(nameLower, "know your meme") || strings.Contains(nameLower, "mashable") ||
		strings.Contains(nameLower, "input mag"):
		return "viral"
	case sourceType == "hn":
		return "tech"
	}

	// Default based on source type
	return sourceType
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-2] + ".."
}

// extractProbability tries to find a percentage in text (for prediction markets)
// Returns -1 if no probability found
func extractProbability(text string) int {
	// Look for patterns like "67%", "67 %", "67 percent"
	// Simple approach: find digits followed by %
	for i := 0; i < len(text)-1; i++ {
		if text[i] >= '0' && text[i] <= '9' {
			// Found a digit, scan forward
			start := i
			for i < len(text) && text[i] >= '0' && text[i] <= '9' {
				i++
			}
			// Check if followed by %
			if i < len(text) && text[i] == '%' {
				num := 0
				for j := start; j < i; j++ {
					num = num*10 + int(text[j]-'0')
				}
				if num >= 0 && num <= 100 {
					return num
				}
			}
		}
	}
	return -1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderEntityPill renders an entity as a colored pill
// Entity IDs are prefixed: ticker:AAPL, country:china, source:reuters
func (m Model) renderEntityPill(entityID string, bg lipgloss.Color) string {
	parts := strings.SplitN(entityID, ":", 2)
	if len(parts) != 2 {
		return ""
	}

	entityType := parts[0]
	entityValue := parts[1]

	switch entityType {
	case "ticker":
		// Stock ticker - blue pill with $ prefix
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#58a6ff")).
			Background(bg).
			Bold(true)
		return style.Render("$" + entityValue)

	case "country":
		// Country - show flag emoji + name
		flag := getCountryFlag(entityValue)
		name := formatCountryName(entityValue)
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7ee787")).
			Background(bg)
		return style.Render(flag + " " + name)

	case "source":
		// Source attribution - gray pill
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e")).
			Background(bg).
			Italic(true)
		return style.Render("via " + entityValue)

	default:
		return ""
	}
}

// getCountryFlag returns emoji flag for a country
var countryFlags = map[string]string{
	"united_states":   "🇺🇸",
	"china":           "🇨🇳",
	"russia":          "🇷🇺",
	"united_kingdom":  "🇬🇧",
	"germany":         "🇩🇪",
	"france":          "🇫🇷",
	"japan":           "🇯🇵",
	"india":           "🇮🇳",
	"ukraine":         "🇺🇦",
	"israel":          "🇮🇱",
	"palestine":       "🇵🇸",
	"iran":            "🇮🇷",
	"north_korea":     "🇰🇵",
	"south_korea":     "🇰🇷",
	"taiwan":          "🇹🇼",
	"canada":          "🇨🇦",
	"australia":       "🇦🇺",
	"brazil":          "🇧🇷",
	"mexico":          "🇲🇽",
	"italy":           "🇮🇹",
	"spain":           "🇪🇸",
	"european_union":  "🇪🇺",
	"nato":            "🏛️",
	"saudi_arabia":    "🇸🇦",
	"uae":             "🇦🇪",
	"turkey":          "🇹🇷",
	"egypt":           "🇪🇬",
	"south_africa":    "🇿🇦",
	"nigeria":         "🇳🇬",
	"indonesia":       "🇮🇩",
	"singapore":       "🇸🇬",
	"hong_kong":       "🇭🇰",
	"vietnam":         "🇻🇳",
	"thailand":        "🇹🇭",
	"philippines":     "🇵🇭",
	"argentina":       "🇦🇷",
	"syria":           "🇸🇾",
	"afghanistan":     "🇦🇫",
	"iraq":            "🇮🇶",
	"netherlands":     "🇳🇱",
	"switzerland":     "🇨🇭",
	"sweden":          "🇸🇪",
	"norway":          "🇳🇴",
	"poland":          "🇵🇱",
}

func getCountryFlag(id string) string {
	if flag, ok := countryFlags[id]; ok {
		return flag
	}
	return "🌍"
}

// formatCountryName formats country ID as display name
func formatCountryName(id string) string {
	// Convert underscores to spaces and title case
	name := strings.ReplaceAll(id, "_", " ")
	// Simple title case
	words := strings.Fields(name)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(string(w[0])) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
