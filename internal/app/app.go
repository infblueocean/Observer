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
	"github.com/abelbrown/observer/internal/ui/command"
	"github.com/abelbrown/observer/internal/ui/configview"
	"github.com/abelbrown/observer/internal/ui/filters"
	"github.com/abelbrown/observer/internal/ui/sources"
	"github.com/abelbrown/observer/internal/ui/stream"
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
	cmdPalette     command.Palette
	filterEngine   *curation.FilterEngine
	sourceManager  *curation.SourceManager
	aggregator     *feeds.Aggregator
	store          *store.Store
	config         *config.Config
	itemCategories map[string]string // item ID -> category
	width          int
	height         int
	lastError      error
	errorTime      time.Time
	mode           viewMode
	showSources    bool
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
	cfg.Save()

	// Initialize store
	homeDir, _ := os.UserHomeDir()
	dbPath := filepath.Join(homeDir, ".observer", "observer.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	st, err := store.New(dbPath)
	if err != nil {
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

	// Initialize filter engine
	filterEngine := curation.NewFilterEngine(nil)

	// Initialize source manager
	configDir := filepath.Join(homeDir, ".observer")
	sourceManager := curation.NewSourceManager(configDir)

	// Initialize command palette
	cmdPalette := command.New()

	return Model{
		stream:         stream.New(),
		filterView:     filters.New(filterEngine),
		configView:     configview.New(cfg),
		sourcesView:    sources.New(sourceManager, feeds.DefaultRSSFeeds),
		cmdPalette:     cmdPalette,
		filterEngine:   filterEngine,
		sourceManager:  sourceManager,
		aggregator:     agg,
		store:          st,
		config:         cfg,
		itemCategories: make(map[string]string),
		mode:           modeStream,
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

	// Command palette takes priority when active
	if m.cmdPalette.IsActive() {
		var cmd tea.Cmd
		var executed string
		m.cmdPalette, cmd, executed = m.cmdPalette.Update(msg)
		if executed != "" {
			return m.executeCommand(executed)
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		streamHeight := msg.Height - 4
		if m.showSources {
			streamHeight -= 3
		}
		m.stream.SetSize(msg.Width, streamHeight)
		m.filterView.SetSize(msg.Width, msg.Height)
		m.cmdPalette.SetWidth(min(60, msg.Width-4))
		return m, nil

	case ItemsLoadedMsg:
		if msg.Err != nil {
			m.lastError = msg.Err
			m.errorTime = time.Now()
		} else {
			// Track categories for coloring
			for _, item := range msg.Items {
				m.itemCategories[item.ID] = msg.Category
			}

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
		// Clear old errors
		if m.lastError != nil && time.Since(m.errorTime) > 10*time.Second {
			m.lastError = nil
		}
		return m, tea.Batch(
			m.refreshDueSources(),
			m.tickRefresh(),
		)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m, m.refreshDueSources()

	case "R":
		return m, m.refreshAllSources()

	case "s":
		m.shuffleItems()

	case "t":
		m.showSources = !m.showSources
		streamHeight := m.height - 4
		if m.showSources {
			streamHeight -= 3
		}
		m.stream.SetSize(m.width, streamHeight)

	case "f":
		m.mode = modeFilters
		m.filterView.SetSize(m.width, m.height)

	case "c":
		m.mode = modeConfig
		m.configView.SetSize(m.width, m.height)

	case "S":
		m.mode = modeSources
		m.sourcesView.SetSize(m.width, m.height)

	case "/", ":":
		// Open command palette
		return m, m.cmdPalette.Activate()

	case "?":
		// TODO: Help overlay
	}

	return m, nil
}

func (m *Model) executeCommand(cmd string) (tea.Model, tea.Cmd) {
	switch cmd {
	case "shuffle":
		m.shuffleItems()
	case "refresh":
		return m, m.refreshAllSources()
	case "sources":
		m.mode = modeSources
		m.sourcesView.SetSize(m.width, m.height)
	case "panel":
		m.showSources = !m.showSources
	case "filters", "filter":
		m.mode = modeFilters
		m.filterView.SetSize(m.width, m.height)
	case "config", "settings":
		m.mode = modeConfig
		m.configView.SetSize(m.width, m.height)
	case "help":
		// TODO: help overlay
	case "quit", "exit", "q":
		if m.store != nil {
			m.store.Close()
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateFilterView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.filterView, cmd = m.filterView.Update(msg)
	if m.filterView.IsQuitting() {
		m.mode = modeStream
		m.filterView.ResetQuitting()
	}
	return m, cmd
}

func (m Model) updateConfigView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.configView, cmd = m.configView.Update(msg)
	if m.configView.IsQuitting() {
		m.mode = modeStream
		m.configView.ResetQuitting()
	}
	return m, cmd
}

func (m Model) updateSourcesView(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.sourcesView, cmd = m.sourcesView.Update(msg)
	if m.sourcesView.IsQuitting() {
		m.mode = modeStream
		m.sourcesView.ResetQuitting()
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

	// Set categories for coloring
	for _, item := range items {
		if cat, ok := m.itemCategories[item.ID]; ok {
			m.stream.SetItemCategory(item.ID, cat)
		}
	}

	m.stream.SetItems(items)
}

// View renders the UI
func (m Model) View() string {
	// Modal views take over
	switch m.mode {
	case modeFilters:
		return m.filterView.View()
	case modeConfig:
		return m.configView.View()
	case modeSources:
		return m.sourcesView.View()
	}

	// Styles
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#c9d1d9")).
		Background(lipgloss.Color("#161b22")).
		Bold(true).
		Padding(0, 2).
		Width(m.width)

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b949e")).
		Background(lipgloss.Color("#0d1117")).
		Padding(0, 2).
		Width(m.width)

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f85149")).
		Background(lipgloss.Color("#0d1117")).
		Padding(0, 2).
		Width(m.width)

	// Header
	blocked := m.aggregator.BlockedCount()
	headerText := fmt.Sprintf("◉ OBSERVER  │  %d sources  │  %d items",
		m.aggregator.SourceCount(),
		m.aggregator.ItemCount())
	if blocked > 0 {
		headerText += fmt.Sprintf("  │  %d blocked", blocked)
	}
	header := headerStyle.Render(headerText)

	// Stream
	streamView := m.stream.View()

	// Status bar
	var statusText string
	if m.lastError != nil {
		statusText = errorStyle.Render("⚠ " + truncateError(m.lastError.Error(), m.width-10))
	} else {
		statusText = statusStyle.Render("↑↓ navigate  /commands  s shuffle  f filters  S sources  c config  r refresh  q quit")
	}

	// Compose with command palette overlay if active
	content := lipgloss.JoinVertical(lipgloss.Left, header, streamView, statusText)

	if m.cmdPalette.IsActive() {
		// Overlay the command palette
		paletteView := m.cmdPalette.View()
		// Center horizontally, position near top
		content = overlayCenter(content, paletteView, m.width, 3)
	}

	return content
}

func overlayCenter(base, overlay string, width, yOffset int) string {
	baseLines := splitLines(base)
	overlayLines := splitLines(overlay)

	overlayWidth := 0
	for _, line := range overlayLines {
		if lipgloss.Width(line) > overlayWidth {
			overlayWidth = lipgloss.Width(line)
		}
	}

	xOffset := (width - overlayWidth) / 2
	if xOffset < 0 {
		xOffset = 0
	}

	// Overlay the lines
	for i, overlayLine := range overlayLines {
		targetLine := yOffset + i
		if targetLine < len(baseLines) {
			baseLines[targetLine] = insertAt(baseLines[targetLine], overlayLine, xOffset)
		}
	}

	return joinLines(baseLines)
}

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

func insertAt(base, insert string, x int) string {
	// Simple insertion - just replace characters
	baseRunes := []rune(base)
	insertRunes := []rune(insert)

	// Pad base if needed
	for len(baseRunes) < x+len(insertRunes) {
		baseRunes = append(baseRunes, ' ')
	}

	// Insert
	for i, r := range insertRunes {
		if x+i < len(baseRunes) {
			baseRunes[x+i] = r
		}
	}

	return string(baseRunes)
}

func truncateError(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Commands

func (m Model) refreshDueSources() tea.Cmd {
	due := m.aggregator.GetSourcesDueForRefresh()
	if len(due) == 0 {
		return nil
	}

	var cmds []tea.Cmd
	for _, s := range due {
		state := s
		m.aggregator.MarkFetching(state.Config.Name, true)

		cmds = append(cmds, func() tea.Msg {
			items, err := state.Source.Fetch()
			return ItemsLoadedMsg{
				Items:      items,
				SourceName: state.Config.Name,
				Category:   state.Config.Category,
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
				Category:   state.Config.Category,
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
