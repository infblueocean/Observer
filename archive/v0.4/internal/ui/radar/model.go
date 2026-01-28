// Package radar provides the Story Radar panel for ambient awareness
// Shows velocity-ranked clusters, top entities, and enables quick filtering
package radar

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/correlation"
	"github.com/abelbrown/observer/internal/logging"
	"github.com/charmbracelet/lipgloss"
)

// Model represents the Story Radar panel
type Model struct {
	width             int
	height            int
	visible           bool
	engine            *correlation.Engine
	selectedCluster   int
	selectedEntity    int
	focusSection      int // 0=clusters, 1=entities
	lastUpdate        time.Time
	filterLogs        bool // Show only correlation-related logs
}

// ClusterInfo holds display info for a cluster
type ClusterInfo struct {
	ID          string
	Summary     string
	ItemCount   int
	Velocity    float64
	Trend       correlation.VelocityTrend
	HasConflict bool
	FirstSeen   time.Time
}

// EntityInfo holds display info for an entity
type EntityInfo struct {
	ID       string
	Name     string
	Type     string
	Mentions int
}

// New creates a new radar model
func New() Model {
	return Model{
		lastUpdate: time.Now(),
	}
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

// SetVisible shows/hides the radar
func (m *Model) SetVisible(visible bool) {
	m.visible = visible
}

// IsVisible returns whether radar is visible
func (m Model) IsVisible() bool {
	return m.visible
}

// Toggle toggles visibility
func (m *Model) Toggle() {
	m.visible = !m.visible
}

// MoveUp moves selection up
func (m *Model) MoveUp() {
	if m.focusSection == 0 && m.selectedCluster > 0 {
		m.selectedCluster--
	} else if m.focusSection == 1 && m.selectedEntity > 0 {
		m.selectedEntity--
	}
}

// MoveDown moves selection down
func (m *Model) MoveDown() {
	// Bounds checking happens in render
	if m.focusSection == 0 {
		m.selectedCluster++
	} else {
		m.selectedEntity++
	}
}

// SwitchSection switches between clusters and entities
func (m *Model) SwitchSection() {
	m.focusSection = (m.focusSection + 1) % 2
}

// ToggleLogFilter toggles between all logs and correlation-only logs
func (m *Model) ToggleLogFilter() {
	m.filterLogs = !m.filterLogs
}

// IsLogFiltered returns whether logs are filtered
func (m Model) IsLogFiltered() bool {
	return m.filterLogs
}

// FocusSection returns which section is focused (0=clusters, 1=entities)
func (m Model) FocusSection() int {
	return m.focusSection
}

// SelectedClusterID returns the currently selected cluster ID, if any
func (m Model) SelectedClusterID() string {
	clusters := m.getTopClusters()
	if m.selectedCluster < len(clusters) {
		return clusters[m.selectedCluster].ID
	}
	return ""
}

// SelectedEntityID returns the currently selected entity ID, if any
func (m Model) SelectedEntityID() string {
	entities := m.getTopEntities()
	if m.selectedEntity < len(entities) {
		return entities[m.selectedEntity].ID
	}
	return ""
}

// getTopClusters returns clusters sorted by velocity
func (m Model) getTopClusters() []ClusterInfo {
	if m.engine == nil {
		return nil
	}

	// Get active clusters from engine
	summaries := m.engine.GetActiveClusters(10)

	var clusters []ClusterInfo
	for _, s := range summaries {
		clusters = append(clusters, ClusterInfo{
			ID:          s.ID,
			Summary:     s.Summary,
			ItemCount:   s.ItemCount,
			Velocity:    s.Velocity,
			Trend:       s.Trend,
			HasConflict: s.HasConflict,
			FirstSeen:   s.FirstItemAt,
		})
	}

	return clusters
}

// getTopEntities returns entities sorted by recent mentions
func (m Model) getTopEntities() []EntityInfo {
	if m.engine == nil {
		return nil
	}

	// Get top entities from engine
	entities, _ := m.engine.GetTopEntities(time.Now().Add(-6*time.Hour), 20)

	var infos []EntityInfo
	for _, e := range entities {
		parts := strings.SplitN(e.ID, ":", 2)
		entityType := ""
		name := e.Name
		if len(parts) == 2 {
			entityType = parts[0]
			name = parts[1]
		}

		infos = append(infos, EntityInfo{
			ID:       e.ID,
			Name:     name,
			Type:     entityType,
			Mentions: e.Mentions,
		})
	}

	return infos
}

// View renders the radar panel
func (m Model) View() string {
	if !m.visible || m.width == 0 {
		return ""
	}

	// Fixed log panel height at bottom
	const logPanelHeight = 12

	// Styles
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#58a6ff")).
		Bold(true)

	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b949e"))

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#1f3a5f"))

	hotStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f85149")).
		Bold(true)

	warmStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#d29922"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6e7681"))

	conflictStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#d29922")).
		Bold(true)

	greenStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3fb950"))

	cyanStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#39c5cf"))

	// === TOP SECTION ===
	var topLines []string

	// Header - CORRELATION HQ
	topLines = append(topLines, headerStyle.Render("━━━ CORRELATION HQ ━━━"))

	// Stats line
	var statsStr string
	if m.engine != nil {
		stats := m.engine.GetStats()
		if stats.StartTime.IsZero() {
			statsStr = dimStyle.Render("Waiting for items...")
		} else {
			runtime := time.Since(stats.StartTime)
			statsStr = fmt.Sprintf("%s processed • %s extracted • %s clusters • running %s",
				greenStyle.Render(fmt.Sprintf("%d items", stats.ItemsProcessed)),
				cyanStyle.Render(fmt.Sprintf("%d entities", stats.EntitiesFound)),
				warmStyle.Render(fmt.Sprintf("%d", stats.ClustersFormed)),
				dimStyle.Render(formatDuration(runtime)))
		}
	} else {
		statsStr = dimStyle.Render("Engine not initialized")
	}
	topLines = append(topLines, statsStr)
	topLines = append(topLines, dimStyle.Render("Enter=filter • Tab=switch • Esc=return"))
	topLines = append(topLines, "")

	// Split into two columns
	leftWidth := m.width / 2
	rightWidth := m.width - leftWidth

	// Left column: Live Activity
	var leftLines []string
	leftLines = append(leftLines, sectionStyle.Render("⚡ LIVE ACTIVITY"))
	leftLines = append(leftLines, "")

	// Get recent activities from engine
	var activities []correlation.Activity
	if m.engine != nil {
		activities = m.engine.GetRecentActivity(12)
	}

	if len(activities) == 0 {
		leftLines = append(leftLines, dimStyle.Render("  Waiting for feed items..."))
		leftLines = append(leftLines, dimStyle.Render("  The correlation engine extracts"))
		leftLines = append(leftLines, dimStyle.Render("  entities and clusters from news."))
	} else {
		for i, act := range activities {
			if i >= 12 { // Limit display
				break
			}

			// Time ago
			timeAgo := formatDuration(time.Since(act.Time))

			// Icon based on activity type
			var icon string
			var style lipgloss.Style
			switch act.Type {
			case correlation.ActivityExtract:
				icon = "📍"
				style = cyanStyle
			case correlation.ActivityCluster:
				icon = "🔗"
				style = greenStyle
			case correlation.ActivityDuplicate:
				icon = "♊"
				style = warmStyle
			case correlation.ActivityDisagree:
				icon = "⚡"
				style = hotStyle
			default:
				icon = "•"
				style = dimStyle
			}

			// Truncate title
			title := act.ItemTitle
			maxTitleLen := leftWidth - 30
			if len(title) > maxTitleLen {
				title = title[:maxTitleLen-3] + "..."
			}

			line := fmt.Sprintf("  %s %s %s", icon, dimStyle.Render(timeAgo), title)
			if act.Details != "" {
				detailLine := fmt.Sprintf("      %s", style.Render(act.Details))
				leftLines = append(leftLines, line)
				leftLines = append(leftLines, detailLine)
			} else {
				leftLines = append(leftLines, line)
			}
		}
	}

	// Also show hot clusters below activity if there's room
	clusters := m.getTopClusters()
	if len(clusters) > 0 && len(leftLines) < 20 {
		leftLines = append(leftLines, "")
		leftLines = append(leftLines, sectionStyle.Render("🔥 HOT CLUSTERS"))
		for i, c := range clusters {
			if i >= 5 || len(leftLines) >= 22 { // Limit
				break
			}
			var trendStr string
			switch c.Trend {
			case correlation.TrendSpiking:
				trendStr = "🔥"
			case correlation.TrendSteady:
				trendStr = "→"
			default:
				trendStr = "↓"
			}
			conflictStr := ""
			if c.HasConflict {
				conflictStr = conflictStyle.Render(" ⚡")
			}
			summary := c.Summary
			if len(summary) > leftWidth-20 {
				summary = summary[:leftWidth-23] + "..."
			}
			line := fmt.Sprintf("  %s %s (%d)%s", trendStr, summary, c.ItemCount, conflictStr)
			if m.focusSection == 0 && i == m.selectedCluster {
				line = selectedStyle.Width(leftWidth - 2).Render(line)
			} else {
				line = dimStyle.Render(line)
			}
			leftLines = append(leftLines, line)
		}
	}

	// Right column: Top Entities
	var rightLines []string
	rightLines = append(rightLines, sectionStyle.Render("📍 TOP ENTITIES"))
	rightLines = append(rightLines, "")

	entities := m.getTopEntities()
	if len(entities) == 0 {
		rightLines = append(rightLines, dimStyle.Render("  No active entities"))
	} else {
		// Ensure selection is in bounds
		if m.selectedEntity >= len(entities) {
			m.selectedEntity = len(entities) - 1
		}

		for i, e := range entities {
			if i >= 10 { // Limit display
				break
			}

			// Type indicator
			var typeIcon string
			var style lipgloss.Style
			switch e.Type {
			case "ticker":
				typeIcon = "💰"
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
			case "country":
				typeIcon = "🌍"
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
			default:
				typeIcon = "•"
				style = dimStyle
			}

			name := formatEntityName(e.Name)
			line := fmt.Sprintf("  %s %s (%d)", typeIcon, name, e.Mentions)

			if m.focusSection == 1 && i == m.selectedEntity {
				line = selectedStyle.Width(rightWidth - 2).Render(line)
			} else {
				line = style.Render(line)
			}

			rightLines = append(rightLines, line)
		}
	}

	// Combine columns
	maxRows := max(len(leftLines), len(rightLines))
	for i := 0; i < maxRows; i++ {
		left := ""
		right := ""
		if i < len(leftLines) {
			left = leftLines[i]
		}
		if i < len(rightLines) {
			right = rightLines[i]
		}

		// Pad left column (ensure non-negative)
		padCount := leftWidth - lipgloss.Width(left)
		if padCount < 0 {
			padCount = 0
		}
		leftPadded := left + strings.Repeat(" ", padCount)

		topLines = append(topLines, leftPadded+right)
	}

	// === BOTTOM SECTION (log panel - fixed height) ===
	var logLines []string

	logHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#a371f7")).
		Bold(true)

	borderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#30363d"))

	filterIndicator := ""
	if m.filterLogs {
		filterIndicator = " [correlation only]"
	}

	// Separator line
	logLines = append(logLines, borderStyle.Render(strings.Repeat("─", m.width)))
	logLines = append(logLines, logHeaderStyle.Render(fmt.Sprintf("📋 LIVE LOG%s  (l=toggle filter)", filterIndicator)))

	// Get recent log entries
	logInfoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
	logWarnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
	logErrorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
	logDebugStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681"))

	availableLogLines := logPanelHeight - 2 // Account for header and separator

	// Get more logs than we need so we can filter
	recentLogs := logging.GetRecentLogs(availableLogLines * 3)

	// Filter if enabled
	var filteredLogs []logging.LogEntry
	for _, entry := range recentLogs {
		if m.filterLogs {
			// Only show correlation-related logs
			msg := strings.ToLower(entry.Message)
			if !strings.Contains(msg, "correlation") &&
				!strings.Contains(msg, "cluster") &&
				!strings.Contains(msg, "entit") &&
				!strings.Contains(msg, "duplicate") {
				continue
			}
		}
		filteredLogs = append(filteredLogs, entry)
		if len(filteredLogs) >= availableLogLines {
			break
		}
	}

	if len(filteredLogs) == 0 {
		if m.filterLogs {
			logLines = append(logLines, dimStyle.Render("  No correlation logs yet... (press l to show all)"))
		} else {
			logLines = append(logLines, dimStyle.Render("  Waiting for log entries..."))
		}
	} else {
		// Show logs in reverse order (newest at bottom for terminal feel)
		for i := len(filteredLogs) - 1; i >= 0; i-- {
			entry := filteredLogs[i]
			formatted := entry.Format()
			if formatted == "" {
				continue
			}

			// Truncate to fit width
			maxWidth := m.width - 4
			if len(formatted) > maxWidth {
				formatted = formatted[:maxWidth-3] + "..."
			}

			// Color based on level
			var style lipgloss.Style
			switch entry.Level {
			case "INFO":
				style = logInfoStyle
			case "WARN":
				style = logWarnStyle
			case "ERROR":
				style = logErrorStyle
			case "DEBUG":
				style = logDebugStyle
			default:
				style = dimStyle
			}

			logLines = append(logLines, "  "+style.Render(formatted))
		}
	}

	// Pad log lines to fixed height
	for len(logLines) < logPanelHeight {
		logLines = append(logLines, "")
	}

	// === COMBINE: top section + padding + bottom log panel ===
	topHeight := m.height - logPanelHeight

	// Pad top section to fill available space
	for len(topLines) < topHeight {
		topLines = append(topLines, "")
	}

	// Truncate if too many top lines
	if len(topLines) > topHeight {
		topLines = topLines[:topHeight]
	}

	// Combine all lines
	allLines := append(topLines, logLines...)

	return strings.Join(allLines, "\n")
}

// formatDuration formats a duration nicely
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// formatEntityName formats an entity name for display
func formatEntityName(name string) string {
	// Remove underscores and title case
	name = strings.ReplaceAll(name, "_", " ")
	words := strings.Fields(name)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(string(w[0])) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
