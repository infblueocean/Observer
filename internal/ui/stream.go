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
// When showBands is false (e.g. during search results), time band headers are suppressed.
// Returns the rendered string for display.
func RenderStream(items []store.Item, cursor int, width, height int, showBands bool, aligned bool, shimmerOffset int) string {
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

	// Calculate scroll offset to keep cursor visible, accounting for band headers.
	scrollOffset := calcScrollOffset(items, cursor, availableHeight, showBands)

	for i, item := range items {
		if renderedLines >= availableHeight {
			break
		}

		// Track band state for all items (including skipped) so headers
		// render correctly when we reach the visible region.
		if showBands {
			band := TimeBand(item.Published)
			if band != currentBand {
				currentBand = band
				if i >= scrollOffset && renderedLines < availableHeight {
					header := TimeBandHeader.Render(band)
					b.WriteString(header)
					b.WriteString("\n")
					renderedLines++
				}
			}
		}

		if i < scrollOffset {
			continue
		}

		if renderedLines >= availableHeight {
			break
		}

		line := renderItemLine(item, i == cursor, width, aligned, shimmerOffset)
		b.WriteString(line)
		b.WriteString("\n")
		renderedLines++
	}

	return b.String()
}

// calcScrollOffset finds the smallest item index such that all visible lines
// from that index through the cursor (including band headers) fit within
// availableHeight. Without bands this is a simple subtraction; with bands
// we iterate to account for header lines that consume viewport space.
func calcScrollOffset(items []store.Item, cursor, availableHeight int, showBands bool) int {
	if len(items) == 0 || cursor < 0 {
		return 0
	}
	if cursor >= len(items) {
		cursor = len(items) - 1
	}

	if !showBands {
		if cursor >= availableHeight {
			return cursor - availableHeight + 1
		}
		return 0
	}

	// Start with optimistic offset (ignoring headers), then adjust upward
	// until cursor fits. Converges in at most ~5 iterations (one per band).
	offset := 0
	if cursor >= availableHeight {
		offset = cursor - availableHeight + 1
	}

	for offset <= cursor {
		lines := visibleLineCount(items, offset, cursor, showBands)
		if lines <= availableHeight {
			return offset
		}
		offset++
	}

	return cursor
}

// visibleLineCount counts how many rendered lines items[from..to] would
// produce, including any band headers that appear within that range.
func visibleLineCount(items []store.Item, from, to int, showBands bool) int {
	lines := 0
	currentBand := ""
	// Initialize band from predecessor so we know if items[from] starts a new band.
	if from > 0 {
		currentBand = TimeBand(items[from-1].Published)
	}
	for i := from; i <= to && i < len(items); i++ {
		if showBands {
			band := TimeBand(items[i].Published)
			if band != currentBand {
				currentBand = band
				lines++
			}
		}
		lines++
	}
	return lines
}

// renderItemLine renders a single item line.
func renderItemLine(item store.Item, selected bool, width int, aligned bool, shimmerOffset int) string {
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
		if item.Read {
			// Dim the text for read items even when selected, to provide visual feedback
			titleStyle = titleStyle.Foreground(lipgloss.Color("250")).Bold(false)
		}
	case item.Read:
		titleStyle = ReadItem
	default:
		titleStyle = NormalItem
	}

	// Compose the line
	styledTitle := titleStyle.Render(title)

	if !aligned {
		line := fmt.Sprintf("%s %s", badge, styledTitle)
		if !selected {
			return line
		}
		return shimmerLine(stripANSI(line), width, shimmerOffset)
	}

	sourceColWidth := 16
	sourcePlain := item.SourceName
	if utf8.RuneCountInString(sourcePlain) > sourceColWidth {
		runes := []rune(sourcePlain)
		sourcePlain = string(runes[:sourceColWidth-1]) + "…"
	}
	sourcePad := sourceColWidth - utf8.RuneCountInString(sourcePlain)
	if sourcePad < 0 {
		sourcePad = 0
	}
	sourceStyle := lipgloss.NewStyle().Foreground(sourcePaletteColor(item.SourceName))
	sourceText := sourceStyle.Render(sourcePlain)
	sourceDots := MetaItem.Render(fadeDots(sourcePad))
	sourceField := sourceText + sourceDots + " "

	age := formatAgeShort(item.Published)
	ageWidth := 6
	agePad := ageWidth - utf8.RuneCountInString(age)
	if agePad < 0 {
		agePad = 0
	}
	ageText := strings.Repeat(" ", agePad) + age

	left := sourceField + styledTitle
	leftWidth := lipgloss.Width(left)
	dotCount := width - leftWidth - ageWidth - 1
	if dotCount < 0 {
		dotCount = 0
	}
	dots := fadeDots(dotCount)
	if !selected {
		return left + dots + " " + MetaItem.Render(ageText)
	}

	plainSourceDots := strings.Repeat(".", sourcePad)
	plainSourceField := sourcePlain + plainSourceDots + " "
	plainLeft := plainSourceField + title
	plainLeftWidth := lipgloss.Width(plainLeft)
	plainDotCount := width - plainLeftWidth - ageWidth - 1
	if plainDotCount < 0 {
		plainDotCount = 0
	}
	plainDots := fadeDots(plainDotCount)
	plainLine := plainLeft + plainDots + " " + ageText
	return shimmerLine(plainLine, width, shimmerOffset)
}

// measureItemLineWidth returns the plain (no ANSI) line width for shimmer span.
func measureItemLineWidth(item store.Item, width int, aligned bool) int {
	// Reuse the same layout logic, but produce a plain string.
	if !aligned {
		badgeWidth := lipgloss.Width(SourceBadge.Render(item.SourceName))
		titleWidth := width - badgeWidth - 4
		if titleWidth < 20 {
			titleWidth = 20
		}
		title := item.Title
		if utf8.RuneCountInString(title) > titleWidth {
			runes := []rune(title)
			title = string(runes[:titleWidth-3]) + "..."
		}
		line := fmt.Sprintf("%s %s", item.SourceName, title)
		return lipgloss.Width(line)
	}

	sourceColWidth := 16
	sourcePlain := item.SourceName
	if utf8.RuneCountInString(sourcePlain) > sourceColWidth {
		runes := []rune(sourcePlain)
		sourcePlain = string(runes[:sourceColWidth-1]) + "…"
	}
	sourcePad := sourceColWidth - utf8.RuneCountInString(sourcePlain)
	if sourcePad < 0 {
		sourcePad = 0
	}
	sourceField := sourcePlain + strings.Repeat(".", sourcePad) + " "

	// Title width is computed similarly to renderItemLine.
	badgeWidth := lipgloss.Width(SourceBadge.Render(item.SourceName))
	titleWidth := width - badgeWidth - 4
	if titleWidth < 20 {
		titleWidth = 20
	}
	title := item.Title
	if utf8.RuneCountInString(title) > titleWidth {
		runes := []rune(title)
		title = string(runes[:titleWidth-3]) + "..."
	}

	ageWidth := 6
	left := sourceField + title
	leftWidth := lipgloss.Width(left)
	dotCount := width - leftWidth - ageWidth - 1
	if dotCount < 0 {
		dotCount = 0
	}
	dots := strings.Repeat(".", dotCount)
	line := left + dots + " " + strings.Repeat(" ", ageWidth)
	return lipgloss.Width(line)
}

func formatAgeShort(published time.Time) string {
	age := time.Since(published)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	}
}

func fadeDots(count int) string {
	if count <= 0 {
		return ""
	}
	// Use simple dots for the leader to avoid rendering issues
	// Fill most of the space with dots, leave a small gap at the end
	dotCount := count - 1
	if dotCount < 0 {
		dotCount = 0
	}
	
	var b strings.Builder
	b.Grow(count)
	for i := 0; i < dotCount; i++ {
		b.WriteString(".")
	}
	// Pad remaining with space
	for i := 0; i < count-dotCount; i++ {
		b.WriteString(" ")
	}
	return b.String()
}

func shimmerLine(line string, width int, shimmerOffset int) string {
	base := SelectedItem.Copy().Padding(0)
	if width <= 0 {
		return base.Render(line)
	}
	runes := []rune(line)
	if len(runes) < width {
		runes = append(runes, []rune(strings.Repeat(" ", width-len(runes)))...)
	} else if len(runes) > width {
		runes = runes[:width]
	}
	pos := 0
	if width > 0 {
		pos = shimmerOffset % width
	}
	var b strings.Builder
	for i, r := range runes {
		if i == pos {
			b.WriteString(ShimmerHead.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sourcePaletteColor(name string) lipgloss.Color {
	palette := []lipgloss.Color{
		lipgloss.Color("62"),
		lipgloss.Color("69"),
		lipgloss.Color("39"),
		lipgloss.Color("141"),
		lipgloss.Color("208"),
		lipgloss.Color("75"),
		lipgloss.Color("99"),
		lipgloss.Color("212"),
	}
	sum := 0
	for i := 0; i < len(name); i++ {
		sum += int(name[i])
	}
	return palette[sum%len(palette)]
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
		StatusBarKey.Render("/") + StatusBarText.Render(":search"),
		StatusBarKey.Render("r") + StatusBarText.Render(":refresh"),
		StatusBarKey.Render("f") + StatusBarText.Render(":fetch"),
		StatusBarKey.Render("t") + StatusBarText.Render(":layout"),
		StatusBarKey.Render("?") + StatusBarText.Render(":debug"),
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

// RenderStatusBarWithFilter renders the status bar when filter is active.
func RenderStatusBarWithFilter(cursor, filtered, total int, width int, loading bool, statusText string, poolPending, embedPending, rerankPending bool) string {
	// Left side: position info or status text
	var leftSide string
	if statusText != "" {
		leftSide = " " + statusText + " "
	} else if loading {
		leftSide = " Loading... "
	} else {
		leftSide = fmt.Sprintf(" %d/%d ", cursor+1, filtered)
	}

	// Pipeline strip
	var strip string
	if poolPending || embedPending || rerankPending {
		pMark := "✓"
		if poolPending {
			pMark = "."
		}
		eMark := "✓"
		if embedPending {
			eMark = "."
		}
		rMark := "✓"
		if rerankPending {
			rMark = "."
		}
		// Compact strip: P:. E:. R:.
		// Use subtle coloring? relying on plain text for now, maybe dim styles.
		strip = fmt.Sprintf(" P:%s E:%s R:%s ", pMark, eMark, rMark)
	}

	// Right side: filter-specific key hints
	keys := []string{
		StatusBarKey.Render("j/k") + StatusBarText.Render(":nav"),
		StatusBarKey.Render("Enter") + StatusBarText.Render(":read"),
		StatusBarKey.Render("Esc") + StatusBarText.Render(":clear"),
		StatusBarKey.Render("t") + StatusBarText.Render(":layout"),
	}
	keyHints := strings.Join(keys, " ")

	// Calculate padding
	leftWidth := lipgloss.Width(leftSide)
	stripWidth := lipgloss.Width(strip)
	rightWidth := lipgloss.Width(keyHints)

	// Center the strip if possible, or append to left
	padding := width - leftWidth - stripWidth - rightWidth
	if padding < 0 {
		padding = 0
	}

	// Simple layout: Left + Padding + Strip + Hints
	// Or: Left + Strip + Padding + Hints?
	// Let's do: Left + Strip + Spacer + Hints
	bar := leftSide + StatusBarText.Render(strip) + strings.Repeat(" ", padding) + keyHints
	return StatusBar.Width(width).Render(bar)
}

// RenderFilterBarWithStatus renders the filter input bar with a custom status indicator.
// status can be empty (no indicator), "embedding", "reranking", etc.
func RenderFilterBarWithStatus(filterText string, filtered, total int, width int, status string) string {
	// Show filter prompt with current text
	prompt := FilterBarPrompt.Render("/")
	text := FilterBarText.Render(filterText)

	// Show status indicator if in progress
	var statusIndicator string
	switch status {
	case "embedding":
		statusIndicator = FilterBarCount.Render(" ...")
	case "reranking":
		statusIndicator = FilterBarCount.Render(" (reranking...)")
	}

	count := FilterBarCount.Render(fmt.Sprintf(" %d/%d", filtered, total))

	content := prompt + text + statusIndicator + count
	contentWidth := lipgloss.Width(content)
	padding := width - contentWidth - 2 // -2 for bar padding
	if padding < 0 {
		padding = 0
	}

	bar := content + strings.Repeat(" ", padding)
	return FilterBar.Width(width).Render(bar)
}
