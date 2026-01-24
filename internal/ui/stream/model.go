package stream

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/ui/styles"
	"github.com/charmbracelet/lipgloss"
)

// Model is the stream view showing feed items flowing by
type Model struct {
	items    []feeds.Item
	cursor   int
	width    int
	height   int
	loading  bool
}

// New creates a new stream model
func New() Model {
	return Model{
		items:   make([]feeds.Item, 0),
		loading: true,
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
	loading := styles.Help.Render("  Loading feeds...")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, loading)
}

func (m Model) renderEmpty() string {
	empty := styles.Help.Render("  No items yet. Add some feeds!")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, empty)
}

func (m Model) renderItems() string {
	var b strings.Builder

	// Calculate visible range (simple scrolling)
	visibleLines := m.height - 2 // Leave room for padding
	startIdx := 0
	if m.cursor > visibleLines/2 {
		startIdx = m.cursor - visibleLines/2
	}
	endIdx := min(startIdx+visibleLines, len(m.items))
	if endIdx-startIdx < visibleLines && startIdx > 0 {
		startIdx = max(0, endIdx-visibleLines)
	}

	for i := startIdx; i < endIdx; i++ {
		item := m.items[i]
		line := m.renderItem(item, i == m.cursor)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderItem(item feeds.Item, selected bool) string {
	// Time formatting
	age := time.Since(item.Published)
	timeStr := formatAge(age)

	// Source badge
	source := styles.SourceBadge.Render(item.SourceName)

	// Title (truncate if needed)
	maxTitleLen := m.width - len(item.SourceName) - len(timeStr) - 15
	title := item.Title
	if len(title) > maxTitleLen && maxTitleLen > 0 {
		title = title[:maxTitleLen-3] + "..."
	}

	// Build the line
	var style lipgloss.Style
	switch {
	case selected:
		style = styles.ItemSelected
	case item.Read:
		style = styles.ItemRead
	default:
		style = styles.ItemNormal
	}

	// Indicator
	indicator := "  "
	if selected {
		indicator = styles.ItemSelected.Render(">")
	}

	timeRendered := styles.TimeStamp.Render(timeStr)

	content := fmt.Sprintf("%s %s %s  %s", indicator, source, title, timeRendered)

	return style.Width(m.width).Render(content)
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
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
