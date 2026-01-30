package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/otel"
)

// debugPanelChrome is the number of terminal lines consumed by DebugPanel's
// border (top + bottom = 2) and vertical padding (top + bottom = 2).
// Must be updated if DebugPanel style changes.
const debugPanelChrome = 4

// debugOverlay renders the debug panel showing pipeline stats and recent events.
// Pure function with no side effects. Returns empty string if ring is nil.
func debugOverlay(ring *otel.RingBuffer, width, height int) string {
	if ring == nil {
		return ""
	}

	stats := ring.Stats()
	recent := ring.Last(20)

	// --- Stats section (keyed lookups, not map iteration) ---
	var lines []string
	lines = append(lines, DebugHeaderStyle.Render("Pipeline Stats"))
	lines = append(lines, fmt.Sprintf("  Fetches:    %d complete, %d errors",
		stats[otel.KindFetchComplete], stats[otel.KindFetchError]))
	lines = append(lines, fmt.Sprintf("  Embeds:     %d complete, %d batch, %d errors",
		stats[otel.KindEmbedComplete], stats[otel.KindEmbedBatch], stats[otel.KindEmbedError]))
	lines = append(lines, fmt.Sprintf("  Searches:   %d started, %d complete, %d cancelled",
		stats[otel.KindSearchStart], stats[otel.KindSearchComplete], stats[otel.KindSearchCancel]))
	lines = append(lines, fmt.Sprintf("  Reranks:    %d cosine, %d cross-encoder",
		stats[otel.KindCosineRerank], stats[otel.KindCrossEncoder]))
	lines = append(lines, fmt.Sprintf("  Buffer:     %d / %d events", ring.Len(), ring.Cap()))
	lines = append(lines, "")

	// --- Recent events section ---
	lines = append(lines, DebugHeaderStyle.Render("Recent Events"))
	for _, e := range recent {
		age := time.Since(e.Time)
		ageStr := formatAge(age)

		line := fmt.Sprintf("  %6s  %-22s", ageStr, string(e.Kind))
		if e.Msg != "" {
			line += "  " + truncateRunes(e.Msg, 40)
		}
		if e.Err != "" {
			line += "  ERR:" + truncateRunes(e.Err, 30)
		}
		if e.QueryID != "" {
			qidDisplay := e.QueryID
			if len(qidDisplay) > 8 {
				qidDisplay = qidDisplay[:8]
			}
			line += fmt.Sprintf("  qid:%s", qidDisplay)
		}
		lines = append(lines, line)
	}

	// Truncate to fit terminal height (subtract chrome added by DebugPanel border/padding)
	maxHeight := height - debugPanelChrome
	if maxHeight < 1 {
		maxHeight = 1
	}
	if len(lines) > maxHeight {
		lines = lines[:maxHeight]
	}

	panelWidth := 76
	if panelWidth > width-4 {
		panelWidth = width - 4
	}
	if panelWidth < 20 {
		panelWidth = 20
	}

	content := strings.Join(lines, "\n")
	return DebugPanel.Width(panelWidth).Render(content)
}

// formatAge formats a duration as a compact human string.
// Handles negative durations from clock skew by clamping to "0ms".
func formatAge(d time.Duration) string {
	if d < 0 {
		return "0ms"
	}
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
}

// debugStatusBar renders the status bar for the debug overlay.
func debugStatusBar(width int) string {
	keys := StatusBarKey.Render("D") + StatusBarText.Render(":close")
	return StatusBar.Width(width).Render("  [DEBUG]  " + keys)
}
