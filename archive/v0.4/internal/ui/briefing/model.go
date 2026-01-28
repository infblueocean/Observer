// Package briefing provides the "Catch Me Up" briefing view
// Shows what happened since the user's last session
package briefing

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/correlation"
	"github.com/charmbracelet/lipgloss"
)

// Model represents the briefing view
type Model struct {
	width       int
	height      int
	visible     bool
	engine      *correlation.Engine
	lastSession time.Time
	items       []BriefingItem
	scrollPos   int
}

// BriefingItem represents a single item in the briefing
type BriefingItem struct {
	Type        string // "cluster", "entity", "disagreement"
	Title       string
	Description string
	Importance  int // 1-5
	Time        time.Time
}

// New creates a new briefing model
func New() Model {
	return Model{}
}

// SetEngine sets the correlation engine
func (m *Model) SetEngine(engine *correlation.Engine) {
	m.engine = engine
}

// SetSize sets the dimensions
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetLastSession sets when the user was last active
func (m *Model) SetLastSession(t time.Time) {
	m.lastSession = t
}

// SetVisible shows/hides the briefing
func (m *Model) SetVisible(visible bool) {
	m.visible = visible
	if visible {
		m.generateBriefing()
	}
}

// IsVisible returns whether briefing is visible
func (m Model) IsVisible() bool {
	return m.visible
}

// NeedsBriefing returns true if user should see a briefing
// (more than 4 hours since last session)
func (m Model) NeedsBriefing() bool {
	if m.lastSession.IsZero() {
		return false // First time, no need for briefing
	}
	return time.Since(m.lastSession) > 4*time.Hour
}

// Dismiss closes the briefing
func (m *Model) Dismiss() {
	m.visible = false
}

// ScrollUp scrolls the briefing up
func (m *Model) ScrollUp() {
	if m.scrollPos > 0 {
		m.scrollPos--
	}
}

// ScrollDown scrolls the briefing down
func (m *Model) ScrollDown() {
	maxScroll := len(m.items) - (m.height / 3)
	if m.scrollPos < maxScroll {
		m.scrollPos++
	}
}

// generateBriefing creates the briefing content
func (m *Model) generateBriefing() {
	m.items = nil
	if m.engine == nil {
		return
	}

	// Get top clusters since last session
	clusters := m.engine.GetActiveClusters(10)
	for _, c := range clusters {
		if c.FirstItemAt.After(m.lastSession) || c.ItemCount > 3 {
			importance := 3
			if c.Velocity > 5 {
				importance = 5
			} else if c.Velocity > 2 {
				importance = 4
			}

			item := BriefingItem{
				Type:        "cluster",
				Title:       c.Summary,
				Description: fmt.Sprintf("%d sources covering • Velocity: %.1f/hr", c.ItemCount, c.Velocity),
				Importance:  importance,
				Time:        c.FirstItemAt,
			}

			if c.HasConflict {
				item.Description += " • ⚡ Sources disagree"
			}

			m.items = append(m.items, item)
		}
	}

	// Get top entities
	entities, _ := m.engine.GetTopEntities(m.lastSession, 10)
	for _, e := range entities {
		if e.Mentions >= 3 {
			m.items = append(m.items, BriefingItem{
				Type:        "entity",
				Title:       formatEntityName(e.Name),
				Description: fmt.Sprintf("Mentioned %d times since you left", e.Mentions),
				Importance:  min(e.Mentions/2, 4),
				Time:        e.LastSeen,
			})
		}
	}

	// Sort by importance
	for i := 0; i < len(m.items)-1; i++ {
		for j := i + 1; j < len(m.items); j++ {
			if m.items[j].Importance > m.items[i].Importance {
				m.items[i], m.items[j] = m.items[j], m.items[i]
			}
		}
	}

	// Limit to top items
	if len(m.items) > 15 {
		m.items = m.items[:15]
	}

	m.scrollPos = 0
}

// View renders the briefing
func (m Model) View() string {
	if !m.visible || m.width == 0 {
		return ""
	}

	// Styles
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#58a6ff")).
		Bold(true)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#c9d1d9")).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b949e"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6e7681"))

	importantStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f85149")).
		Bold(true)

	clusterIcon := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#d29922")).
		Render("◉")

	entityIcon := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3fb950")).
		Render("●")

	var lines []string

	// Header
	duration := time.Since(m.lastSession)
	lines = append(lines, "")
	lines = append(lines, headerStyle.Render("  ━━━ CATCH ME UP ━━━"))
	lines = append(lines, descStyle.Render(fmt.Sprintf("  You've been away for %s. Here's what happened:", formatDuration(duration))))
	lines = append(lines, "")

	if len(m.items) == 0 {
		lines = append(lines, dimStyle.Render("  📭 Nothing major happened while you were away."))
		lines = append(lines, dimStyle.Render("  It was a slow news day."))
	} else {
		// Render items with scrolling
		visibleItems := m.items
		if m.scrollPos > 0 && m.scrollPos < len(m.items) {
			visibleItems = m.items[m.scrollPos:]
		}

		for i, item := range visibleItems {
			if i >= 10 { // Limit visible items
				break
			}

			// Type icon
			var icon string
			switch item.Type {
			case "cluster":
				icon = clusterIcon
			case "entity":
				icon = entityIcon
			default:
				icon = "•"
			}

			// Importance indicator
			importanceStr := ""
			if item.Importance >= 4 {
				importanceStr = importantStyle.Render(" ★")
			}

			// Title
			title := item.Title
			if len(title) > m.width-20 {
				title = title[:m.width-23] + "..."
			}

			lines = append(lines, fmt.Sprintf("  %s %s%s", icon, titleStyle.Render(title), importanceStr))
			lines = append(lines, descStyle.Render("    "+item.Description))
			lines = append(lines, "")
		}

		// Scroll indicator
		if len(m.items) > 10 {
			remaining := len(m.items) - m.scrollPos - 10
			if remaining > 0 {
				lines = append(lines, dimStyle.Render(fmt.Sprintf("    ↓ %d more items (scroll down)", remaining)))
			}
		}
	}

	// Footer
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  Press Enter or Esc to dismiss • ↑↓ to scroll"))

	// Center on screen
	content := strings.Join(lines, "\n")

	// Add border
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#30363d")).
		Padding(1, 2).
		Width(min(m.width-4, 80))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

// formatDuration formats a duration in a friendly way
func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// formatEntityName formats an entity name for display
func formatEntityName(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	words := strings.Fields(name)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(string(w[0])) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
