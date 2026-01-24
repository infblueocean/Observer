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
	ColorAccentGreen = lipgloss.Color("#3fb950") // positive/optimist
	ColorAccentAmber = lipgloss.Color("#d29922") // historian/warm
	ColorAccentRed   = lipgloss.Color("#f85149") // skeptic/alert
	ColorHot         = lipgloss.Color("#ff7b72") // big happening
)

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

	// Stream item - selected (the glow)
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

	// Status bar at bottom
	StatusBar = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Background(ColorSurface).
		Padding(0, 2)

	// Help text
	Help = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	// Hot/important indicator
	HotBadge = lipgloss.NewStyle().
		Foreground(ColorHot).
		Bold(true)
)

// Persona colors for Brain Trust (future)
var (
	PersonaHistorian = lipgloss.NewStyle().Foreground(ColorAccentAmber)
	PersonaSkeptic   = lipgloss.NewStyle().Foreground(ColorAccentRed)
	PersonaOptimist  = lipgloss.NewStyle().Foreground(ColorAccentGreen)
	PersonaConnector = lipgloss.NewStyle().Foreground(ColorAccentBlue)
)

// Chat/Workshop message styles
var (
	UserMessage = lipgloss.NewStyle().
		Foreground(ColorAccentBlue).
		Bold(true)

	AIMessage = lipgloss.NewStyle().
		Foreground(ColorTextPrimary)

	SystemMessage = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Italic(true)
)

// Shared/collaboration indicators
var (
	SharedBadge = lipgloss.NewStyle().
		Foreground(ColorAccentGreen).
		Background(ColorSurface).
		Padding(0, 1).
		Bold(true)

	PrivateBadge = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Background(ColorSurface).
		Padding(0, 1)
)
