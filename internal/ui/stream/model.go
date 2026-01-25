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

// Spinner returns a command to tick the spinner
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

	// Calculate visible range with some padding
	linesPerItem := 2 // title + spacing
	visibleItems := (m.height - 2) / linesPerItem
	if visibleItems < 1 {
		visibleItems = 1
	}

	startIdx := 0
	if m.cursor > visibleItems/2 {
		startIdx = m.cursor - visibleItems/2
	}
	endIdx := min(startIdx+visibleItems, len(m.items))
	if endIdx-startIdx < visibleItems && startIdx > 0 {
		startIdx = max(0, endIdx-visibleItems)
	}

	for i := startIdx; i < endIdx; i++ {
		item := m.items[i]
		selected := i == m.cursor
		lines = append(lines, m.renderItem(item, selected))
	}

	// Scroll indicator
	scrollInfo := ""
	if len(m.items) > visibleItems {
		pct := float64(m.cursor) / float64(len(m.items)-1) * 100
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

func (m Model) renderItem(item feeds.Item, selected bool) string {
	// Get category color
	category := m.categories[item.ID]
	if category == "" {
		category = string(item.Source)
	}
	catColor, ok := categoryColors[category]
	if !ok {
		catColor = lipgloss.Color("#8b949e")
	}

	// Time formatting
	age := time.Since(item.Published)
	timeStr := formatAge(age)

	// Source badge with category color
	sourceBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0d1117")).
		Background(catColor).
		Padding(0, 1).
		Bold(true).
		Render(truncate(item.SourceName, 12))

	// Time stamp
	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#484f58"))

	// "Fresh" indicator for items < 30min old
	freshIndicator := ""
	if age < 30*time.Minute {
		freshIndicator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3fb950")).
			Render(" ●")
	}

	// Title
	maxTitleWidth := m.width - lipgloss.Width(sourceBadge) - 15
	title := truncate(item.Title, maxTitleWidth)

	// Build the line based on state
	if selected {
		// Selected: highlighted background, accent border
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)

		containerStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#21262d")).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(catColor).
			Padding(0, 1).
			Width(m.width - 2)

		line := fmt.Sprintf("%s  %s%s  %s",
			sourceBadge,
			titleStyle.Render(title),
			freshIndicator,
			timeStyle.Render(timeStr))

		return containerStyle.Render(line)
	}

	if item.Read {
		// Read: dimmed
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#484f58"))

		dimBadge := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#484f58")).
			Background(lipgloss.Color("#21262d")).
			Padding(0, 1).
			Render(truncate(item.SourceName, 12))

		line := fmt.Sprintf("  %s  %s  %s",
			dimBadge,
			titleStyle.Render(title),
			timeStyle.Render(timeStr))

		return line
	}

	// Normal: clean, readable
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#c9d1d9"))

	line := fmt.Sprintf("  %s  %s%s  %s",
		sourceBadge,
		titleStyle.Render(title),
		freshIndicator,
		timeStyle.Render(timeStr))

	return line
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
	return s[:maxLen-3] + "..."
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
