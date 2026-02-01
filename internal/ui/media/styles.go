package media

import "github.com/charmbracelet/lipgloss"

// Colors used in the cyber-noir media view.
var (
	ColorCyan   = lipgloss.Color("86")  // Glitch Primary
	ColorPink   = lipgloss.Color("212") // Glitch Secondary
	ColorYellow = lipgloss.Color("227") // Glitch Accent
	ColorWhite  = lipgloss.Color("255")
	ColorDim    = lipgloss.Color("242")
	ColorDark   = lipgloss.Color("234")
	ColorBlack  = lipgloss.Color("0")
	ColorFire   = lipgloss.Color("202")
)

// Styles holds all Lip Gloss style definitions for the media view.
// This allows for dependency injection and testing.
type Styles struct {
	// Feed Styles
	FeedItem       lipgloss.Style
	SelectedItem   lipgloss.Style
	RankNumber     lipgloss.Style
	DeltaNeutral   lipgloss.Style
	DeltaUp        lipgloss.Style
	DeltaDown      lipgloss.Style
	SourceMeta     lipgloss.Style
	AgeMeta        lipgloss.Style

	// Glitch Colors
	GlitchCyan     lipgloss.Style
	GlitchPink     lipgloss.Style
	GlitchYellow   lipgloss.Style

	// Sidebar Styles
	SidebarTitle   lipgloss.Style
	SliderLabel    lipgloss.Style
	SliderActive   lipgloss.Style
	SliderDim      lipgloss.Style

	// Card Styles
	CardBorder     lipgloss.Style
	CardMetricLabel lipgloss.Style
	CardMetricValue lipgloss.Style
	CardBarEmpty   lipgloss.Style
	CardBarFill    lipgloss.Style
	FireIcon       lipgloss.Style
}

// DefaultStyles returns the default cyber-noir look.
func DefaultStyles() Styles {
	s := Styles{}

	// Feed
	s.FeedItem = lipgloss.NewStyle().Padding(0, 1)
	s.SelectedItem = lipgloss.NewStyle().
		Bold(true).
		Background(lipgloss.Color("62")). // Purple background for selection
		Foreground(ColorWhite)
	
	s.RankNumber = lipgloss.NewStyle().Foreground(ColorDim).Width(4)
	s.SourceMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Width(14).Align(lipgloss.Right)
	s.AgeMeta = lipgloss.NewStyle().Foreground(ColorDim).Width(6).Align(lipgloss.Right)

	// Glitch
	s.GlitchCyan = lipgloss.NewStyle().Foreground(ColorCyan)
	s.GlitchPink = lipgloss.NewStyle().Foreground(ColorPink)
	s.GlitchYellow = lipgloss.NewStyle().Foreground(ColorYellow)

	// Sidebar
	s.SidebarTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorCyan).
		MarginBottom(1).
		Padding(0, 1)
	
	s.SliderLabel = lipgloss.NewStyle().Foreground(ColorWhite)
	s.SliderActive = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	s.SliderDim = lipgloss.NewStyle().Foreground(ColorDim)

	// Card
	s.CardBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1).
		MarginTop(1)
	
	s.CardMetricLabel = lipgloss.NewStyle().Foreground(ColorDim)
	s.CardMetricValue = lipgloss.NewStyle().Foreground(ColorWhite).Bold(true)
	s.CardBarEmpty = lipgloss.NewStyle().Foreground(lipgloss.Color("236"))
	s.CardBarFill = lipgloss.NewStyle().Foreground(ColorCyan)
	s.FireIcon = lipgloss.NewStyle().Foreground(ColorFire)

	return s
}
