package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/abelbrown/observer/internal/store"
	"github.com/charmbracelet/lipgloss"
)

// TimeBand returns a display string for grouping items by age.
func TimeBand(published time.Time) string {
	age := time.Since(published)
	switch {
	case age < 15*time.Minute:
		return "Just Now"
	case age < 1*time.Hour:
		return "Past Hour"
	case age < 24*time.Hour:
		return "Today"
	case age < 48*time.Hour:
		return "Yesterday"
	default:
		return "Older"
	}
}

// RenderStream renders the item list with time bands.
// Returns the rendered string for display.
func RenderStream(items []store.Item, cursor int, width, height int) string {
	if len(items) == 0 {
		return HelpStyle.Render("No items to display. Press 'r' to refresh.")
	}

	var b strings.Builder
	currentBand := ""
	renderedLines := 0

	// Calculate available height for items (reserve 1 line for status bar)
	availableHeight := height - 1
	if availableHeight < 1 {
		availableHeight = 1
	}

	// Calculate scroll offset to keep cursor visible
	scrollOffset := 0
	if cursor >= availableHeight {
		scrollOffset = cursor - availableHeight + 1
	}

	for i, item := range items {
		// Check if we've rendered enough lines
		if renderedLines >= availableHeight+scrollOffset {
			break
		}

		// Render time band header if band changes
		band := TimeBand(item.Published)
		if band != currentBand {
			currentBand = band
			if i >= scrollOffset {
				header := TimeBandHeader.Render(band)
				b.WriteString(header)
				b.WriteString("\n")
				renderedLines++
			}
		}

		// Skip items before scroll offset
		if i < scrollOffset {
			continue
		}

		// Render item line
		line := renderItemLine(item, i == cursor, width)
		b.WriteString(line)
		b.WriteString("\n")
		renderedLines++
	}

	return b.String()
}

// renderItemLine renders a single item line.
func renderItemLine(item store.Item, selected bool, width int) string {
	// Build the source badge
	badge := SourceBadge.Render(item.SourceName)
	badgeWidth := lipgloss.Width(badge)

	// Calculate available width for title
	// Account for badge and padding
	titleWidth := width - badgeWidth - 4
	if titleWidth < 20 {
		titleWidth = 20
	}

	// Truncate title if needed (use rune count, not byte count for Unicode support)
	title := item.Title
	if utf8.RuneCountInString(title) > titleWidth {
		runes := []rune(title)
		title = string(runes[:titleWidth-3]) + "..."
	}

	// Apply style based on state
	var titleStyle lipgloss.Style
	switch {
	case selected:
		titleStyle = SelectedItem
	case item.Read:
		titleStyle = ReadItem
	default:
		titleStyle = NormalItem
	}

	// Compose the line
	styledTitle := titleStyle.Render(title)

	// Format: [Source] Title
	return fmt.Sprintf("%s %s", badge, styledTitle)
}

// RenderStatusBar renders the bottom status bar with key hints and item count.
func RenderStatusBar(cursor, total int, width int, loading bool) string {
	// Left side: position info or loading indicator
	var position string
	if loading {
		position = " Loading... "
	} else {
		position = fmt.Sprintf(" %d/%d ", cursor+1, total)
	}

	// Right side: key hints
	keys := []string{
		StatusBarKey.Render("j/k") + StatusBarText.Render(":nav"),
		StatusBarKey.Render("Enter") + StatusBarText.Render(":read"),
		StatusBarKey.Render("r") + StatusBarText.Render(":refresh"),
		StatusBarKey.Render("f") + StatusBarText.Render(":fetch"),
		StatusBarKey.Render("q") + StatusBarText.Render(":quit"),
	}
	keyHints := strings.Join(keys, " ")

	// Calculate padding to fill width
	leftWidth := lipgloss.Width(position)
	rightWidth := lipgloss.Width(keyHints)
	padding := width - leftWidth - rightWidth
	if padding < 0 {
		padding = 0
	}

	bar := position + strings.Repeat(" ", padding) + keyHints
	return StatusBar.Width(width).Render(bar)
}
