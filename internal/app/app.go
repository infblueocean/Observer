package app

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/abelbrown/observer/internal/config"
	"github.com/abelbrown/observer/internal/curation"
	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/feeds/hackernews"
	"github.com/abelbrown/observer/internal/feeds/manifold"
	"github.com/abelbrown/observer/internal/feeds/polymarket"
	"github.com/abelbrown/observer/internal/feeds/rss"
	"github.com/abelbrown/observer/internal/feeds/usgs"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui/configview"
	"github.com/abelbrown/observer/internal/ui/filters"
	"github.com/abelbrown/observer/internal/ui/sources"
	"github.com/abelbrown/observer/internal/ui/stream"
	"github.com/abelbrown/observer/internal/ui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// View mode
type viewMode int

const (
	modeStream viewMode = iota
	modeFilters
	modeFilterWorkshop
	modeConfig
	modeSources
)

// Model is the root Bubble Tea model
type Model struct {
	stream         stream.Model
	filterView     filters.Model
	filterWorkshop filters.WorkshopModel
	configView     configview.Model
	sourcesView    sources.Model
	filterEngine   *curation.FilterEngine
	sourceManager  *curation.SourceManager
	aggregator     *feeds.Aggregator
	store          *store.Store
	config         *config.Config
	width          int
	height         int
	err            error
	mode           viewMode // Current view mode
	showSources    bool     // Toggle source panel
	commandMode    bool     // For /commands
	commandBuf     string
}

// New creates a new app model
func New() Model {
	// Load configuration
	cfg, _ := config.Load()

	// Try to load keys from keys.sh if available
	keysPath := filepath.Join(os.Getenv("HOME"), "src", "claude", "keys.sh")
	if _, err := os.Stat(keysPath); err == nil {
		cfg.LoadKeysFromFile(keysPath)
	}
	cfg.AutoPopulateFromEnv()
	cfg.Save() // Persist loaded keys

	// Initialize store
	homeDir, _ := os.UserHomeDir()
	dbPath := filepath.Join(homeDir, ".observer", "observer.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	st, err := store.New(dbPath)
	if err != nil {
		// Continue without persistence
		st = nil
	}

	// Build aggregator with all sources
	agg := feeds.NewAggregator()

	// Add RSS feeds
	for _, cfg := range feeds.DefaultRSSFeeds {
		source := rss.New(cfg.Name, cfg.URL)
		agg.AddSource(source, cfg)
	}

	// Add HN API
	agg.AddSource(hackernews.NewTop(), feeds.RSSFeedConfig{
		Name:           "HN Top",
		Category:       "tech",
		RefreshMinutes: feeds.RefreshFast,
		Weight:         1.3,
	})

	// Add USGS
	agg.AddSource(usgs.NewSignificant(), feeds.RSSFeedConfig{
		Name:           "USGS Significant",
		Category:       "events",
		RefreshMinutes: feeds.RefreshRealtime,
		Weight:         1.5,
	})
	agg.AddSource(usgs.NewM45Week(), feeds.RSSFeedConfig{
		Name:           "USGS M4.5+",
		Category:       "events",
		RefreshMinutes: feeds.RefreshNormal,
		Weight:         1.2,
	})

	// Add Prediction Markets
	agg.AddSource(polymarket.New(), feeds.RSSFeedConfig{
		Name:           "Polymarket",
		Category:       "predictions",
		RefreshMinutes: feeds.RefreshNormal,
		Weight:         1.3,
	})
	agg.AddSource(manifold.New(), feeds.RSSFeedConfig{
		Name:           "Manifold",
		Category:       "predictions",
		RefreshMinutes: feeds.RefreshNormal,
		Weight:         1.2,
	})

	// Initialize filter engine (no AI evaluator yet - can add later)
	filterEngine := curation.NewFilterEngine(nil)

	// Initialize source manager
	configDir := filepath.Join(homeDir, ".observer")
	sourceManager := curation.NewSourceManager(configDir)

	return Model{
		stream:        stream.New(),
		filterView:    filters.New(filterEngine),
		configView:    configview.New(cfg),
		sourcesView:   sources.New(sourceManager, feeds.DefaultRSSFeeds),
		filterEngine:  filterEngine,
		sourceManager: sourceManager,
		aggregator:    agg,
		store:         st,
		config:        cfg,
		mode:          modeStream,
	}
}

// Init initializes the app
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshDueSources(),
		m.tickRefresh(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle modal views
	switch m.mode {
	case modeFilters:
		return m.updateFilterView(msg)
	case modeConfig:
		return m.updateConfigView(msg)
	case modeSources:
		return m.updateSourcesView(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		streamHeight := msg.Height - 4 // Header + status
		if m.showSources {
			streamHeight -= 3 // Source bar
		}
		m.stream.SetSize(msg.Width, streamHeight)
		m.filterView.SetSize(msg.Width, msg.Height)
		return m, nil

	case ItemsLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			// Merge new items
			m.aggregator.MergeItems(msg.Items)

			// Save to store
			if m.store != nil {
				m.store.SaveItems(msg.Items)
			}

			// Update aggregator state
			m.aggregator.UpdateSourceState(msg.SourceName, len(msg.Items), msg.Err)

			// Refresh stream view
			m.updateStreamItems()
		}
		return m, nil

	case TickMsg:
		return m, tea.Batch(
			m.refreshDueSources(),
			m.tickRefresh(),
		)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Command mode
	if m.commandMode {
		return m.handleCommandKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		if m.store != nil {
			m.store.Close()
		}
		return m, tea.Quit

	case "up", "k":
		m.stream.MoveUp()

	case "down", "j":
		m.stream.MoveDown()

	case "enter":
		if item := m.stream.SelectedItem(); item != nil {
			item.Read = true
			if m.store != nil {
				m.store.MarkRead(item.ID)
			}
		}

	case "r":
		// Manual refresh all due sources
		return m, m.refreshDueSources()

	case "R":
		// Force refresh ALL sources
		return m, m.refreshAllSources()

	case "s":
		// Shuffle
		m.shuffleItems()

	case "t":
		// Toggle source panel
		m.showSources = !m.showSources
		streamHeight := m.height - 4
		if m.showSources {
			streamHeight -= 3
		}
		m.stream.SetSize(m.width, streamHeight)

	case "f":
		// Open filter view
		m.mode = modeFilters
		m.filterView.SetSize(m.width, m.height)

	case "c":
		// Open config view
		m.mode = modeConfig
		m.configView.SetSize(m.width, m.height)

	case "S":
		// Open sources view (capital S to avoid conflict with shuffle)
		m.mode = modeSources
		m.sourcesView.SetSize(m.width, m.height)

	case "/":
		// Enter command mode
		m.commandMode = true
		m.commandBuf = ""

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Quick sort by category (future)
	}

	return m, nil
}

func (m *Model) handleCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		cmd := m.executeCommand(m.commandBuf)
		m.commandMode = false
		m.commandBuf = ""
		return m, cmd

	case "esc":
		m.commandMode = false
		m.commandBuf = ""

	case "backspace":
		if len(m.commandBuf) > 0 {
			m.commandBuf = m.commandBuf[:len(m.commandBuf)-1]
		}

	default:
		if len(msg.String()) == 1 {
			m.commandBuf += msg.String()
		}
	}
	return m, nil
}

func (m *Model) executeCommand(cmd string) tea.Cmd {
	switch cmd {
	case "shuffle":
		m.shuffleItems()
	case "refresh":
		return m.refreshAllSources()
	case "sources":
		// /sources opens the source manager (not just toggle panel)
		m.mode = modeSources
		m.sourcesView.SetSize(m.width, m.height)
	case "panel":
		// /panel toggles the source panel in stream view
		m.showSources = !m.showSources
	case "filter", "filters":
		m.mode = modeFilters
		m.filterView.SetSize(m.width, m.height)
	case "config", "settings":
		m.mode = modeConfig
		m.configView.SetSize(m.width, m.height)
	}
	return nil
}

func (m Model) updateFilterView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.filterView, cmd = m.filterView.Update(msg)

	// Check if user closed the filter view
	if m.filterView.IsQuitting() {
		m.mode = modeStream
		m.filterView.ResetQuitting()
	}

	return m, cmd
}

func (m Model) updateConfigView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.configView, cmd = m.configView.Update(msg)

	// Check if user closed the config view
	if m.configView.IsQuitting() {
		m.mode = modeStream
		m.configView.ResetQuitting()
	}

	return m, cmd
}

func (m Model) updateSourcesView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.sourcesView, cmd = m.sourcesView.Update(msg)

	// Check if user closed the sources view
	if m.sourcesView.IsQuitting() {
		m.mode = modeStream
		m.sourcesView.ResetQuitting()
		// Refresh stream with new source settings
		m.updateStreamItems()
	}

	return m, cmd
}

func (m *Model) shuffleItems() {
	items := m.aggregator.GetItems()
	rand.Shuffle(len(items), func(i, j int) {
		items[i], items[j] = items[j], items[i]
	})
	m.stream.SetItems(items)
}

func (m *Model) updateStreamItems() {
	items := m.aggregator.GetItems()

	// Sort by published time, newest first
	sort.Slice(items, func(i, j int) bool {
		return items[i].Published.After(items[j].Published)
	})

	m.stream.SetItems(items)
}

// View renders the UI
func (m Model) View() string {
	// Modal views take over when active
	switch m.mode {
	case modeFilters:
		return m.filterView.View()
	case modeConfig:
		return m.configView.View()
	case modeSources:
		return m.sourcesView.View()
	}

	var sections []string

	// Header
	blocked := m.aggregator.BlockedCount()
	headerText := fmt.Sprintf("  OBSERVER  ·  %d sources  ·  %d items",
		m.aggregator.SourceCount(),
		m.aggregator.ItemCount())
	if blocked > 0 {
		headerText += fmt.Sprintf("  ·  %d ads blocked", blocked)
	}
	header := styles.Header.Width(m.width).Render(headerText)
	sections = append(sections, header)

	// Source refresh indicators (if enabled)
	if m.showSources {
		sections = append(sections, m.renderSourceBar())
	}

	// Stream content
	sections = append(sections, m.stream.View())

	// Status bar
	statusText := m.renderStatusBar()
	status := styles.StatusBar.Width(m.width).Render(statusText)
	sections = append(sections, status)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) renderSourceBar() string {
	states := m.aggregator.GetSourceStates()

	// Group by category and show refresh progress
	var indicators []string
	categories := make(map[string]int)

	for _, s := range states {
		categories[s.Config.Category]++
	}

	// Show counts per category with mini progress
	for cat, count := range categories {
		indicator := fmt.Sprintf("%s:%d", cat, count)
		indicators = append(indicators, indicator)
	}

	// Limit width
	barContent := ""
	for i, ind := range indicators {
		if i > 0 {
			barContent += "  "
		}
		barContent += ind
		if len(barContent) > m.width-10 {
			barContent += "..."
			break
		}
	}

	return styles.SourceBadge.Width(m.width).Render("  " + barContent)
}

func (m Model) renderStatusBar() string {
	if m.commandMode {
		return fmt.Sprintf("  /%-20s  [esc] cancel", m.commandBuf)
	}
	if m.err != nil {
		return "  Error: " + m.err.Error()
	}
	return "  [↑↓] navigate  [s] shuffle  [f] filters  [S] sources  [c] config  [r] refresh  [q] quit"
}

// Commands

func (m Model) refreshDueSources() tea.Cmd {
	due := m.aggregator.GetSourcesDueForRefresh()
	if len(due) == 0 {
		return nil
	}

	var cmds []tea.Cmd
	for _, s := range due {
		state := s // Capture
		m.aggregator.MarkFetching(state.Config.Name, true)

		cmds = append(cmds, func() tea.Msg {
			items, err := state.Source.Fetch()
			return ItemsLoadedMsg{
				Items:      items,
				SourceName: state.Config.Name,
				Err:        err,
			}
		})
	}

	return tea.Batch(cmds...)
}

func (m Model) refreshAllSources() tea.Cmd {
	states := m.aggregator.GetSourceStates()

	var cmds []tea.Cmd
	for _, s := range states {
		state := s
		m.aggregator.MarkFetching(state.Config.Name, true)

		cmds = append(cmds, func() tea.Msg {
			items, err := state.Source.Fetch()
			return ItemsLoadedMsg{
				Items:      items,
				SourceName: state.Config.Name,
				Err:        err,
			}
		})
	}

	return tea.Batch(cmds...)
}

func (m Model) tickRefresh() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
		return TickMsg{}
	})
}
