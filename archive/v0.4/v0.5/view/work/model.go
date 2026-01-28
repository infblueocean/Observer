//go:build ignore

// Package work provides the work queue visualization for Observer v0.5.
package work

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/view/styles"
	"github.com/abelbrown/observer/internal/work"
	tea "github.com/charmbracelet/bubbletea"
)

// Model is the work view model.
type Model struct {
	pool   *work.Pool
	width  int
	height int
}

// New creates a new work view model.
func New(pool *work.Pool) Model {
	return Model{pool: pool}
}

// SetSize updates the view dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// Work view is mostly passive - it renders the pool state
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.pool == nil {
		return styles.Help.Render("  Work pool not available")
	}

	snapshot := m.pool.Snapshot()
	var b strings.Builder

	// Header
	b.WriteString(styles.Header.Render("WORK QUEUE"))
	b.WriteString("\n\n")

	// Stats summary
	stats := snapshot.Stats
	statsLine := fmt.Sprintf("Active: %d  Pending: %d  Completed: %d  Failed: %d",
		stats.WorkersActive, stats.PendingCount, stats.TotalCompleted, stats.TotalFailed)
	b.WriteString(styles.StatusBar.Render(statsLine))
	b.WriteString("\n\n")

	// Active items
	if len(snapshot.Active) > 0 {
		b.WriteString(styles.WorkActive.Render("● Active"))
		b.WriteString("\n")
		for _, item := range snapshot.Active {
			b.WriteString(m.renderWorkItem(item))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Pending items
	if len(snapshot.Pending) > 0 {
		b.WriteString(styles.WorkPending.Render("○ Pending"))
		b.WriteString("\n")
		for _, item := range snapshot.Pending {
			b.WriteString(m.renderWorkItem(item))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Recent completed (last 10)
	if len(snapshot.Completed) > 0 {
		b.WriteString(styles.WorkComplete.Render("✓ Recent"))
		b.WriteString("\n")
		limit := 10
		if len(snapshot.Completed) < limit {
			limit = len(snapshot.Completed)
		}
		for i := 0; i < limit; i++ {
			item := snapshot.Completed[i]
			b.WriteString(m.renderWorkItem(item))
			b.WriteString("\n")
		}
	}

	// Help
	b.WriteString("\n")
	b.WriteString(styles.Help.Render("Press q or Esc to return"))

	return b.String()
}

func (m Model) renderWorkItem(item *work.Item) string {
	// Status icon
	icon := item.StatusIcon()

	// Type icon
	typeIcon := item.Type.Icon()

	// Description (truncated)
	desc := item.Description
	if len(desc) > 40 {
		desc = desc[:37] + "..."
	}

	// Result or duration
	var extra string
	switch item.Status {
	case work.StatusActive:
		extra = fmt.Sprintf("[%s]", formatDuration(item.Duration()))
	case work.StatusComplete:
		extra = item.Result
		if age := item.Age(); age < 3*time.Second {
			// Highlight fresh completions
			extra = styles.WorkComplete.Bold(true).Render(extra + " just now")
		} else {
			extra = fmt.Sprintf("%s  %s ago", item.Result, formatDuration(age))
		}
	case work.StatusFailed:
		if item.Error != nil {
			extra = item.Error.Error()
			if len(extra) > 30 {
				extra = extra[:27] + "..."
			}
		}
	case work.StatusPending:
		extra = fmt.Sprintf("waiting %s", formatDuration(time.Since(item.CreatedAt)))
	}

	// Apply style based on status
	var style = styles.WorkPending
	switch item.Status {
	case work.StatusActive:
		style = styles.WorkActive
	case work.StatusComplete:
		style = styles.WorkComplete
	case work.StatusFailed:
		style = styles.WorkFailed
	}

	return style.Render(fmt.Sprintf("  [%s] %s %s  %s", icon, typeIcon, desc, extra))
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	default:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}
