package media

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Config holds the initial setup for the Media View.
type Config struct {
	Now       func() time.Time
	Headlines []Headline
	Styles    *Styles
}

// TickMsg is sent every 120ms to animate the glitch effect.
type TickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

// WeightsChangedMsg is sent when user adjusts sliders.
type WeightsChangedMsg struct {
	Weights Weights
}

// MainModel is the root model for the "Engineered" Media View.
type MainModel struct {
	Config  Config
	Feed    FeedModel
	Sliders SliderPanel
	Card    TransparencyCard
	Styles  Styles
	
	Width  int
	Height int
	Active bool // Only tick when active
	
	lastTick uint32
}

// NewMainModel creates a new media view model.
func NewMainModel(cfg Config) MainModel {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	
	var styles Styles
	if cfg.Styles == nil {
		styles = DefaultStyles()
	} else {
		styles = *cfg.Styles
	}

	return MainModel{
		Config:  cfg,
		Feed:    NewFeedModel(cfg.Headlines),
		Sliders: NewSliderPanel(),
		Card:    NewTransparencyCard(),
		Styles:  styles,
		Active:  true,
	}
}

// Init starts the glitch animation ticker.
func (m MainModel) Init() tea.Cmd {
	return tick()
}

// Update handles resizing, ticks, and navigation.
func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case TickMsg:
		if m.Active {
			m.lastTick++
			m.Feed.Tick = m.lastTick
			return m, tick()
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			// If focused on feed, move feed cursor. If on sliders, move slider focus.
			// For now, let's keep it simple: j/k always feed, arrows always sliders?
			// No, let's follow the standard pattern: Tab to switch panels?
			// GPT-5 suggested j/k for feed, up/down for sliders. 
			// Let's use j/k for feed and up/down for sliders.
			m.Feed.MoveCursor(-1, m.Height-10) // Reserve space for header/card
		case "down", "j":
			m.Feed.MoveCursor(1, m.Height-10)
		
		// Sliders
		case "ctrl+up", "ctrl+down", "h", "l":
			var cmd tea.Cmd
			m.Sliders, cmd = m.Sliders.Update(msg)
			m.Feed.Recompute(m.Sliders.Weights, m.Config.Now())
			return m, cmd
		}
	}

	return m, nil
}

// View implements the responsive layout logic.
func (m MainModel) View() string {
	if m.Width == 0 || m.Height == 0 {
		return "Initialising..."
	}

	// Calculate widths
	sidebarWidth := 36
	feedWidth := m.Width
	showSidebar := m.Width >= 92

	if showSidebar {
		feedWidth = m.Width - sidebarWidth - 2
	}

	// Render Feed
	feedHeight := m.Height - 8 // Reserve space for Card
	if !showSidebar {
		feedHeight = m.Height - 12
	}
	
	feedView := m.Feed.View(feedWidth, feedHeight)

	// Render Sidebar
	var sidebarView string
	if showSidebar {
		sidebarView = m.Sliders.View(sidebarWidth)
	}

	// Render Card (focused on current headline)
	cardView := ""
	if len(m.Feed.Headlines) > 0 {
		h := m.Feed.Headlines[m.Feed.Cursor]
		cardView = m.Card.View(h, showSidebar, m.Width)
	}

	if showSidebar {
		// Side-by-side: Feed | Sidebar
		mainContent := lipgloss.JoinHorizontal(lipgloss.Top, 
			lipgloss.NewStyle().Width(feedWidth).Render(feedView),
			lipgloss.NewStyle().Width(sidebarWidth).MarginLeft(2).Render(sidebarView),
		)
		return lipgloss.JoinVertical(lipgloss.Left, mainContent, cardView)
	}

	// Stacked: Feed then Card (sliders hidden)
	return lipgloss.JoinVertical(lipgloss.Left, feedView, cardView)
}
