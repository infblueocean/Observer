package ui

import "github.com/charmbracelet/lipgloss"

// Colors used in the application.
var (
	colorPrimary   = lipgloss.Color("62")  // Purple
	colorSecondary = lipgloss.Color("241") // Gray
	colorMuted     = lipgloss.Color("240") // Darker gray
	colorHighlight = lipgloss.Color("212") // Pink
	colorSpinner   = lipgloss.Color("205") // Magenta-pink
)

// SelectedItem style for the currently highlighted item.
var SelectedItem = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("255")).
	Background(colorPrimary).
	Padding(0, 1)

// NormalItem style for unselected, unread items.
var NormalItem = lipgloss.NewStyle().
	Foreground(lipgloss.Color("255")).
	Padding(0, 1)

// ReadItem style for items that have been read.
var ReadItem = lipgloss.NewStyle().
	Foreground(colorSecondary).
	Padding(0, 1)

// TimeBandHeader style for time band labels (e.g., "Just Now", "Today").
var TimeBandHeader = lipgloss.NewStyle().
	Bold(true).
	Foreground(colorHighlight).
	MarginTop(1).
	MarginBottom(0).
	Padding(0, 1)

// SourceBadge style for source name badges.
var SourceBadge = lipgloss.NewStyle().
	Foreground(colorPrimary).
	Background(lipgloss.Color("236")).
	Padding(0, 1).
	MarginRight(1)

// StatusBar style for the bottom status bar.
var StatusBar = lipgloss.NewStyle().
	Foreground(lipgloss.Color("255")).
	Background(lipgloss.Color("236")).
	Padding(0, 1)

// StatusBarKey style for key hints in status bar.
var StatusBarKey = lipgloss.NewStyle().
	Foreground(colorHighlight).
	Bold(true)

// StatusBarText style for descriptive text in status bar.
var StatusBarText = lipgloss.NewStyle().
	Foreground(colorSecondary)

// ErrorStyle for displaying errors.
var ErrorStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("196")).
	Bold(true).
	Padding(0, 1)

// HelpStyle for help text.
var HelpStyle = lipgloss.NewStyle().
	Foreground(colorMuted).
	Padding(1, 2)

// FilterBar style for the filter input bar.
var FilterBar = lipgloss.NewStyle().
	Foreground(lipgloss.Color("255")).
	Background(lipgloss.Color("240")).
	Padding(0, 1)

// FilterBarPrompt style for the "/" prompt.
var FilterBarPrompt = lipgloss.NewStyle().
	Foreground(colorHighlight).
	Bold(true)

// FilterBarText style for the filter input text.
var FilterBarText = lipgloss.NewStyle().
	Foreground(lipgloss.Color("255"))

// FilterBarCount style for the filtered count.
var FilterBarCount = lipgloss.NewStyle().
	Foreground(colorSecondary)

// DebugPanel style for the debug overlay.
var DebugPanel = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("63")).
	Foreground(lipgloss.Color("252")).
	Background(lipgloss.Color("235")).
	Padding(1, 2)

// DebugHeaderStyle for section headers in the debug panel.
var DebugHeaderStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(colorHighlight).
	MarginBottom(0)

