//go:build ignore

// Package styles provides Lip Gloss styling for Observer v0.5.
package styles

import "github.com/charmbracelet/lipgloss"

// Colors - Dark Ambient palette
// Calm, contemplative, easy to watch all day
var (
	ColorBackground  = lipgloss.Color("#0d1117") // deep night
	ColorSurface     = lipgloss.Color("#161b22") // slightly lifted
	ColorBorder      = lipgloss.Color("#30363d") // subtle separation
	ColorTextPrimary = lipgloss.Color("#c9d1d9") // soft white
	ColorTextMuted   = lipgloss.Color("#8b949e") // gentle gray
	ColorAccentBlue  = lipgloss.Color("#58a6ff") // calm highlight
	ColorAccentGreen = lipgloss.Color("#3fb950") // positive
	ColorAccentAmber = lipgloss.Color("#d29922") // warm
	ColorAccentRed   = lipgloss.Color("#f85149") // alert
	ColorHot         = lipgloss.Color("#ff7b72") // big happening
)

// Category colors for source badges
var CategoryColors = map[string]lipgloss.Color{
	"wire":       ColorAccentRed,  // Breaking news, urgent
	"tech":       ColorAccentBlue, // Technology
	"science":    ColorAccentGreen,
	"finance":    ColorAccentAmber,
	"aggregator": ColorTextMuted,
	"security":   ColorAccentRed,
}

// Base styles
var (
	// App container
	App = lipgloss.NewStyle().
		Background(ColorBackground)

	// Header bar
	Header = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Background(ColorSurface).
		Padding(0, 2).
		Bold(true)

	// Stream item - normal
	ItemNormal = lipgloss.NewStyle().
		Foreground(ColorTextPrimary).
		Padding(0, 2)

	// Stream item - selected
	ItemSelected = lipgloss.NewStyle().
		Foreground(ColorTextPrimary).
		Background(ColorSurface).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(ColorAccentBlue).
		Padding(0, 2)

	// Stream item - read (dimmed)
	ItemRead = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Padding(0, 2)

	// Source badge
	SourceBadge = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Background(ColorSurface).
		Padding(0, 1).
		MarginRight(1)

	// Time stamp
	TimeStamp = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Italic(true)

	// Time band divider
	TimeBandDivider = lipgloss.NewStyle().
		Foreground(ColorBorder).
		Padding(0, 2)

	// Status bar at bottom
	StatusBar = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Background(ColorSurface).
		Padding(0, 2)

	// Command input
	CommandInput = lipgloss.NewStyle().
		Foreground(ColorAccentBlue)

	// Help text
	Help = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	// Hot/important indicator
	HotBadge = lipgloss.NewStyle().
		Foreground(ColorHot).
		Bold(true)

	// Work item styles
	WorkPending = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	WorkActive = lipgloss.NewStyle().
		Foreground(ColorAccentBlue).
		Bold(true)

	WorkComplete = lipgloss.NewStyle().
		Foreground(ColorAccentGreen)

	WorkFailed = lipgloss.NewStyle().
		Foreground(ColorAccentRed)
)

// Truncate truncates a string to maxLen characters with ellipsis.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
