// Package workview provides a Bubble Tea view for the work queue.
// Toggle with /w to see all async operations in real-time.
package workview

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/work"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#58a6ff"))

	statsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e"))

	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#30363d"))

	activeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3fb950"))

	pendingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e"))

	completeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#58a6ff"))

	failedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f85149"))

	progressBarFilled = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3fb950"))

	progressBarEmpty = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#30363d"))

	typeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d2a8ff"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#484f58"))
)

// Model is the Bubble Tea model for the work queue view.
type Model struct {
	pool     *work.Pool
	snapshot work.Snapshot
	spinner  spinner.Model

	width  int
	height int
	scroll int

	// Display filters
	showPending   bool
	showActive    bool
	showCompleted bool
	showFailed    bool
	filterType    work.Type // empty = all types

	// Max items to show in completed history
	maxCompleted int
}

// New creates a new work view model.
func New(pool *work.Pool) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))

	return Model{
		pool:          pool,
		spinner:       s,
		showPending:   true,
		showActive:    true,
		showCompleted: true,
		showFailed:    true,
		maxCompleted:  20,
	}
}

// SetSize updates the view dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Refresh updates the snapshot from the pool.
func (m *Model) Refresh() {
	if m.pool != nil {
		m.snapshot = m.pool.Snapshot()
	}
}

// Spinner returns the spinner for tick forwarding.
func (m *Model) Spinner() spinner.Model {
	return m.spinner
}

// SetSpinner updates the spinner state.
func (m *Model) SetSpinner(s spinner.Model) {
	m.spinner = s
}

// View renders the work queue.
func (m Model) View() string {
	if m.pool == nil {
		return "Work pool not initialized"
	}

	var b strings.Builder

	// Header with stats
	stats := m.snapshot.Stats
	header := fmt.Sprintf("WORK QUEUE %s", m.spinner.View())
	b.WriteString(titleStyle.Render(header))
	b.WriteString("  ")
	b.WriteString(statsStyle.Render(stats.String()))
	b.WriteString("\n\n")

	// Active work (always on top, with progress)
	if m.showActive && len(m.snapshot.Active) > 0 {
		for _, item := range m.snapshot.Active {
			if m.filterType != "" && item.Type != m.filterType {
				continue
			}
			b.WriteString(m.renderItem(item))
			b.WriteString("\n")
		}
	}

	// Pending work
	if m.showPending && len(m.snapshot.Pending) > 0 {
		count := 0
		for _, item := range m.snapshot.Pending {
			if m.filterType != "" && item.Type != m.filterType {
				continue
			}
			if count >= 5 {
				remaining := len(m.snapshot.Pending) - count
				b.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more pending\n", remaining)))
				break
			}
			b.WriteString(m.renderItem(item))
			b.WriteString("\n")
			count++
		}
	}

	// Divider between active/pending and completed
	if (m.showActive && len(m.snapshot.Active) > 0) || (m.showPending && len(m.snapshot.Pending) > 0) {
		divider := strings.Repeat("─", min(m.width-4, 60))
		b.WriteString(dividerStyle.Render(divider))
		b.WriteString("\n")
	}

	// Recent completed/failed
	if m.showCompleted || m.showFailed {
		count := 0
		for _, item := range m.snapshot.Completed {
			if count >= m.maxCompleted {
				break
			}
			if item.Status == work.StatusFailed && !m.showFailed {
				continue
			}
			if item.Status == work.StatusComplete && !m.showCompleted {
				continue
			}
			if m.filterType != "" && item.Type != m.filterType {
				continue
			}
			b.WriteString(m.renderItem(item))
			b.WriteString("\n")
			count++
		}
	}

	// Footer with keyboard shortcuts
	b.WriteString("\n")
	shortcuts := dimStyle.Render("p:pending  a:active  c:complete  f:failed  x:clear  esc:close")
	b.WriteString(shortcuts)

	return b.String()
}

// renderItem renders a single work item.
func (m Model) renderItem(item *work.Item) string {
	var parts []string

	// Status icon
	icon := item.StatusIcon()
	switch item.Status {
	case work.StatusActive:
		parts = append(parts, activeStyle.Render("["+icon+"]"))
	case work.StatusPending:
		parts = append(parts, pendingStyle.Render("["+icon+"]"))
	case work.StatusComplete:
		parts = append(parts, completeStyle.Render("["+icon+"]"))
	case work.StatusFailed:
		parts = append(parts, failedStyle.Render("["+icon+"]"))
	}

	// Type icon
	parts = append(parts, typeStyle.Render(string(item.Type.Icon())))

	// Description (truncated)
	desc := truncate(item.Description, 40)
	parts = append(parts, desc)

	// Status-specific info
	switch item.Status {
	case work.StatusActive:
		// Progress bar or elapsed time
		if item.Progress > 0 {
			parts = append(parts, m.renderProgress(item.Progress, 10))
		}
		parts = append(parts, dimStyle.Render(formatDuration(item.Duration())))

	case work.StatusPending:
		// Nothing extra for pending

	case work.StatusComplete:
		// Result and age
		if item.Result != "" {
			parts = append(parts, completeStyle.Render(truncate(item.Result, 15)))
		}
		parts = append(parts, dimStyle.Render(formatAge(item.Age())))

	case work.StatusFailed:
		// Error and age
		if item.Error != nil {
			errStr := truncate(item.Error.Error(), 20)
			parts = append(parts, failedStyle.Render(errStr))
		}
		parts = append(parts, dimStyle.Render(formatAge(item.Age())))
	}

	return strings.Join(parts, " ")
}

// renderProgress renders a progress bar.
func (m Model) renderProgress(pct float64, width int) string {
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := progressBarFilled.Render(strings.Repeat("█", filled)) +
		progressBarEmpty.Render(strings.Repeat("░", empty))

	return fmt.Sprintf("%s %2.0f%%", bar, pct*100)
}

// TogglePending toggles pending visibility.
func (m *Model) TogglePending() {
	m.showPending = !m.showPending
}

// ToggleActive toggles active visibility.
func (m *Model) ToggleActive() {
	m.showActive = !m.showActive
}

// ToggleCompleted toggles completed visibility.
func (m *Model) ToggleCompleted() {
	m.showCompleted = !m.showCompleted
}

// ToggleFailed toggles failed visibility.
func (m *Model) ToggleFailed() {
	m.showFailed = !m.showFailed
}

// SetFilterType sets a type filter (empty = all).
func (m *Model) SetFilterType(t work.Type) {
	m.filterType = t
}

// ClearFilter removes any type filter.
func (m *Model) ClearFilter() {
	m.filterType = ""
}

// ClearHistory clears completed work history.
func (m *Model) ClearHistory() {
	if m.pool != nil {
		m.pool.ClearHistory()
		m.Refresh()
	}
}

// Helper functions

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

func formatAge(d time.Duration) string {
	if d < time.Second {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
