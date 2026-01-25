package stream

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/charmbracelet/bubbles/spinner"
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
}

// Time bands for grouping
type timeBand int

const (
	bandJustNow   timeBand = iota // < 10 minutes
	bandPastHour                  // < 1 hour
	bandToday                     // < 24 hours
	bandYesterday                 // < 48 hours
	bandOlder                     // everything else
)

func getTimeBand(published time.Time) timeBand {
	age := time.Since(published)
	switch {
	case age < 10*time.Minute:
		return bandJustNow
	case age < time.Hour:
		return bandPastHour
	case age < 24*time.Hour:
		return bandToday
	case age < 48*time.Hour:
		return bandYesterday
	default:
		return bandOlder
	}
}

func bandLabel(band timeBand) string {
	switch band {
	case bandJustNow:
		return "Just Now"
	case bandPastHour:
		return "Past Hour"
	case bandToday:
		return "Earlier Today"
	case bandYesterday:
		return "Yesterday"
	case bandOlder:
		return "Older"
	}
	return ""
}

// Model is the stream view showing feed items flowing by
type Model struct {
	items      []feeds.Item
	categories map[string]string // item ID -> category lookup
	cursor     int
	width      int
	height     int
	loading    bool
	spinner    spinner.Model
}

// New creates a new stream model
func New() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))

	return Model{
		items:      make([]feeds.Item, 0),
		categories: make(map[string]string),
		loading:    true,
		spinner:    s,
	}
}

// SetSize updates the viewport dimensions
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetItems replaces the current items
func (m *Model) SetItems(items []feeds.Item) {
	m.items = items
	m.loading = false
	if m.cursor >= len(items) {
		m.cursor = max(0, len(items)-1)
	}
}

// SetItemCategory sets the category for an item (for coloring)
func (m *Model) SetItemCategory(itemID, category string) {
	m.categories[itemID] = category
}

// SetLoading sets the loading state
func (m *Model) SetLoading(loading bool) {
	m.loading = loading
}

// MoveUp moves cursor up
func (m *Model) MoveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

// MoveDown moves cursor down
func (m *Model) MoveDown() {
	if m.cursor < len(m.items)-1 {
		m.cursor++
	}
}

// SelectedItem returns the currently selected item, if any
func (m *Model) SelectedItem() *feeds.Item {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return &m.items[m.cursor]
	}
	return nil
}

// MarkSelectedRead marks the selected item as read
func (m *Model) MarkSelectedRead() {
	if item := m.SelectedItem(); item != nil {
		item.Read = true
	}
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

	return m.renderItems()
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

func (m Model) renderItems() string {
	var lines []string

	// Calculate how many lines we can show
	availableHeight := m.height - 2 // Leave room for scroll indicator

	// Build all renderable lines with their item indices
	type renderedLine struct {
		content   string
		itemIndex int // -1 for dividers/spacing
	}
	var allLines []renderedLine

	currentBand := timeBand(-1)
	for i, item := range m.items {
		band := getTimeBand(item.Published)

		// Add time band divider if band changed
		if band != currentBand {
			if currentBand != -1 {
				// Blank line before new band (breathing room)
				allLines = append(allLines, renderedLine{"", -1})
			}
			// Time band divider
			divider := m.renderTimeBandDivider(band)
			allLines = append(allLines, renderedLine{divider, -1})
			currentBand = band
		}

		// Render the item
		selected := i == m.cursor
		line := m.renderItem(item, selected)
		allLines = append(allLines, renderedLine{line, i})
	}

	// Find the line index where cursor item is
	cursorLineIdx := 0
	for i, rl := range allLines {
		if rl.itemIndex == m.cursor {
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

	// Scroll indicator
	scrollInfo := ""
	if len(m.items) > 0 {
		pct := float64(m.cursor) / float64(max(1, len(m.items)-1)) * 100
		scrollInfo = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#484f58")).
			Render(fmt.Sprintf(" %d/%d (%.0f%%)", m.cursor+1, len(m.items), pct))
	}

	content := strings.Join(lines, "\n")
	if scrollInfo != "" {
		content += "\n" + scrollInfo
	}

	return content
}

func (m Model) renderTimeBandDivider(band timeBand) string {
	label := bandLabel(band)

	// Style: muted, unobtrusive
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#484f58"))

	lineStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#30363d"))

	// Calculate line widths
	labelWidth := len(label) + 2 // padding
	totalWidth := m.width - 4
	leftLineWidth := 3
	rightLineWidth := totalWidth - leftLineWidth - labelWidth
	if rightLineWidth < 0 {
		rightLineWidth = 0
	}

	leftLine := lineStyle.Render(strings.Repeat("─", leftLineWidth))
	rightLine := lineStyle.Render(strings.Repeat("─", rightLineWidth))
	labelText := labelStyle.Render(" " + label + " ")

	return fmt.Sprintf("  %s%s%s", leftLine, labelText, rightLine)
}

func (m Model) renderItem(item feeds.Item, selected bool) string {
	// Get category color
	category := m.categories[item.ID]
	if category == "" {
		// Try to derive from source type or name
		category = deriveCategoryFromSource(item.SourceName, string(item.Source))
	}
	catColor, ok := categoryColors[category]
	if !ok {
		catColor = lipgloss.Color("#8b949e")
	}

	// Time formatting
	age := time.Since(item.Published)
	timeStr := formatAge(age)

	// Source name - use abbreviation if available, otherwise smart truncate
	sourceName := getSourceAbbrev(item.SourceName)

	// Source badge with category color
	sourceBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0d1117")).
		Background(catColor).
		Padding(0, 1).
		Render(sourceName)

	// Time stamp - right side, muted
	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#484f58"))

	// Fresh indicator - only for < 10 minutes (make it meaningful)
	freshIndicator := ""
	if age < 10*time.Minute {
		freshIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3fb950")).
			Bold(true).
			Render(" ●")
	}

	// Title
	badgeWidth := lipgloss.Width(sourceBadge)
	timeWidth := len(timeStr) + 2
	freshWidth := 0
	if freshIndicator != "" {
		freshWidth = 3
	}
	maxTitleWidth := m.width - badgeWidth - timeWidth - freshWidth - 8
	if maxTitleWidth < 20 {
		maxTitleWidth = 20
	}
	title := truncate(item.Title, maxTitleWidth)

	// Build the line based on state
	if selected {
		// Selected: highlighted background, accent border, brighter text
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)

		// Container with left border in category color
		line := fmt.Sprintf("%s  %s%s",
			sourceBadge,
			titleStyle.Render(title),
			freshIndicator)

		// Pad and add time on right
		lineWidth := lipgloss.Width(line)
		padding := m.width - lineWidth - len(timeStr) - 6
		if padding < 1 {
			padding = 1
		}
		line += strings.Repeat(" ", padding) + timeStyle.Render(timeStr)

		containerStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#1c2128")).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(catColor).
			Width(m.width - 2)

		return containerStyle.Render(line)
	}

	if item.Read {
		// Read: dimmed everything
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#484f58"))

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

	// Normal: clean, readable
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#c9d1d9"))

	line := fmt.Sprintf("  %s  %s%s",
		sourceBadge,
		titleStyle.Render(title),
		freshIndicator)

	lineWidth := lipgloss.Width(line)
	padding := m.width - lineWidth - len(timeStr) - 4
	if padding < 1 {
		padding = 1
	}
	return line + strings.Repeat(" ", padding) + timeStyle.Render(timeStr)
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
