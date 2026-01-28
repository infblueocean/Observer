// Package stream provides the main feed view for Observer v0.5.
package stream

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/view/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model is the stream view model.
type Model struct {
	items    []model.Item
	cursor   int
	width    int
	height   int
	viewport int // Index of first visible item

	// Time band tracking
	timeBands map[model.TimeBand]int // First index of each time band
}

// New creates a new stream model.
func New() Model {
	return Model{
		timeBands: make(map[model.TimeBand]int),
	}
}

// SetItems updates the items displayed.
func (m *Model) SetItems(items []model.Item) {
	m.items = items
	m.computeTimeBands()

	// Reset cursor if out of bounds
	if m.cursor >= len(items) {
		m.cursor = max(0, len(items)-1)
	}
}

// SetSize updates the view dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Items returns the current items.
func (m Model) Items() []model.Item {
	return m.items
}

// SelectedItem returns the currently selected item.
func (m Model) SelectedItem() (model.Item, bool) {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor], true
	}
	return model.Item{}, false
}

// Cursor returns the current cursor position.
func (m Model) Cursor() int {
	return m.cursor
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Up):
			m.moveUp()
		case key.Matches(msg, keys.Down):
			m.moveDown()
		case key.Matches(msg, keys.PageUp):
			m.pageUp()
		case key.Matches(msg, keys.PageDown):
			m.pageDown()
		case key.Matches(msg, keys.Home):
			m.cursor = 0
			m.viewport = 0
		case key.Matches(msg, keys.End):
			if len(m.items) > 0 {
				m.cursor = len(m.items) - 1
			}
		}
	}

	// Keep viewport in sync with cursor
	m.ensureCursorVisible()

	return m, nil
}

func (m *Model) moveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *Model) moveDown() {
	if m.cursor < len(m.items)-1 {
		m.cursor++
	}
}

func (m *Model) pageUp() {
	visible := m.visibleLines()
	m.cursor -= visible
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) pageDown() {
	visible := m.visibleLines()
	m.cursor += visible
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) ensureCursorVisible() {
	visible := m.visibleLines()
	if visible <= 0 {
		return
	}

	if m.cursor < m.viewport {
		m.viewport = m.cursor
	}
	if m.cursor >= m.viewport+visible {
		m.viewport = m.cursor - visible + 1
	}
}

func (m Model) visibleLines() int {
	// Reserve lines for header/footer
	return max(1, m.height-4)
}

// View implements tea.Model.
func (m Model) View() string {
	if len(m.items) == 0 {
		return styles.Help.Render("  No items yet. Fetching feeds...")
	}

	var b strings.Builder
	visible := m.visibleLines()
	currentBand := model.TimeBand(-1)

	end := min(m.viewport+visible, len(m.items))
	for i := m.viewport; i < end; i++ {
		item := m.items[i]

		// Time band divider
		band := item.TimeBand()
		if band != currentBand {
			currentBand = band
			// Only show divider if not first visible item
			if i > m.viewport {
				b.WriteString("\n")
			}
			b.WriteString(styles.TimeBandDivider.Render(fmt.Sprintf("─── %s ───", band.String())))
			b.WriteString("\n")
		}

		// Render item
		line := m.renderItem(item, i == m.cursor)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderItem(item model.Item, selected bool) string {
	// Source abbreviation
	src := abbreviateSource(item.SourceName)
	srcStyle := styles.SourceBadge
	if color, ok := getCategoryColor(item.Source); ok {
		srcStyle = srcStyle.Foreground(color)
	}
	srcBadge := srcStyle.Render(fmt.Sprintf("[%s]", src))

	// Age
	age := formatAge(item.Published)
	ageStr := styles.TimeStamp.Render(age)

	// Title
	maxTitleLen := m.width - len(src) - len(age) - 12
	if maxTitleLen < 20 {
		maxTitleLen = 20
	}
	title := styles.Truncate(item.Title, maxTitleLen)

	// Apply style based on state
	var line string
	if selected {
		line = styles.ItemSelected.Render(fmt.Sprintf("%s %s  %s", srcBadge, title, ageStr))
	} else if item.Read {
		line = styles.ItemRead.Render(fmt.Sprintf("%s %s  %s", srcBadge, title, ageStr))
	} else {
		line = styles.ItemNormal.Render(fmt.Sprintf("%s %s  %s", srcBadge, title, ageStr))
	}

	return line
}

func (m *Model) computeTimeBands() {
	m.timeBands = make(map[model.TimeBand]int)
	for i, item := range m.items {
		band := item.TimeBand()
		if _, exists := m.timeBands[band]; !exists {
			m.timeBands[band] = i
		}
	}
}

// Key bindings
var keys = struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding
}{
	Up:       key.NewBinding(key.WithKeys("up", "k")),
	Down:     key.NewBinding(key.WithKeys("down", "j")),
	PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
	PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
	Home:     key.NewBinding(key.WithKeys("home", "g")),
	End:      key.NewBinding(key.WithKeys("end", "G")),
}

// abbreviateSource returns a short form of the source name.
func abbreviateSource(name string) string {
	abbrevs := map[string]string{
		"Hacker News":       "HN",
		"AP News":           "AP",
		"BBC World":         "BBC",
		"BBC Top":           "BBC",
		"Ars Technica":      "Ars",
		"The Verge":         "Verge",
		"Techmeme":          "TM",
		"Nature":            "Nature",
		"Quanta Magazine":   "Quanta",
		"Bloomberg":         "BBG",
		"Krebs on Security": "Krebs",
		"Lobsters":          "Lob",
	}

	if abbr, ok := abbrevs[name]; ok {
		return abbr
	}

	// Default: first 5 chars
	if len(name) > 5 {
		return name[:5]
	}
	return name
}

// getCategoryColor returns the color for a source type.
func getCategoryColor(src model.SourceType) (lipgloss.Color, bool) {
	colors := map[model.SourceType]lipgloss.Color{
		model.SourceRSS:        styles.ColorTextMuted,
		model.SourceHN:         styles.ColorAccentAmber,
		model.SourceReddit:     styles.ColorAccentRed,
		model.SourceAggregator: styles.ColorAccentBlue,
	}
	c, ok := colors[src]
	return c, ok
}

// formatAge returns a human-readable age string.
func formatAge(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		return fmt.Sprintf("%dh ago", hours)
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
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
