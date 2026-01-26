package app

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/abelbrown/observer/internal/brain"
	"github.com/abelbrown/observer/internal/config"
	"github.com/abelbrown/observer/internal/logging"
	"github.com/abelbrown/observer/internal/curation"
	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/feeds/hackernews"
	"github.com/abelbrown/observer/internal/feeds/manifold"
	"github.com/abelbrown/observer/internal/feeds/polymarket"
	"github.com/abelbrown/observer/internal/feeds/rss"
	"github.com/abelbrown/observer/internal/feeds/usgs"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui/braintrust"
	"github.com/abelbrown/observer/internal/ui/command"
	"github.com/charmbracelet/bubbles/spinner"
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

// RecentError holds an error with its timestamp
type RecentError struct {
	Err  error
	Time time.Time
}

// Model is the root Bubble Tea model
type Model struct {
	stream          stream.Model
	filterView      filters.Model
	filterWorkshop  filters.WorkshopModel
	configView      configview.Model
	sourcesView     sources.Model
	brainTrustPanel braintrust.Model
	brainTrust      *brain.BrainTrust
	cmdPalette      command.Palette
	filterEngine    *curation.FilterEngine
	sourceManager   *curation.SourceManager
	aggregator      *feeds.Aggregator
	store           *store.Store
	config          *config.Config
	itemCategories  map[string]string // item ID -> category
	width           int
	height          int
	recentErrors    []RecentError // Last 2 errors
	lastErrorTime   time.Time     // When to hide error pane (10s after last error)
	mode                 viewMode
	showSources          bool
	showBrainTrust       bool
	topStoriesAnalyzed   bool
	feedsLoadedCount     int

	// Mouse tracking for analysis pane scrolling
	analysisPaneTop    int  // Y coordinate where analysis pane starts
	analysisPaneBottom int  // Y coordinate where analysis pane ends
	mouseOverAnalysis  bool // True when mouse is hovering over analysis pane
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

	// Initialize AI Analysis with providers from config
	brainTrustInstance := brain.NewBrainTrust(nil)

	// Add Ollama provider (local, fast)
	if cfg.Models.Ollama.Enabled {
		ollamaProvider := brain.NewOllamaProvider(
			cfg.Models.Ollama.Endpoint,
			cfg.Models.Ollama.Model,
		)
		if ollamaProvider.Available() {
			brainTrustInstance.AddProvider(ollamaProvider)
			logging.Info("Ollama provider added", "endpoint", cfg.Models.Ollama.Endpoint, "model", cfg.Models.Ollama.Model)
		} else {
			logging.Warn("Ollama enabled but not available", "endpoint", cfg.Models.Ollama.Endpoint)
		}
	}

	// Add Claude provider (cloud)
	if cfg.Models.Claude.Enabled && cfg.Models.Claude.APIKey != "" {
		claudeProvider := brain.NewClaudeProvider(cfg.Models.Claude.APIKey, cfg.Models.Claude.Model)
		brainTrustInstance.AddProvider(claudeProvider)
		logging.Info("Claude provider added", "model", cfg.Models.Claude.Model)
	}

	// Connect store to analyzer for persistence
	if st != nil {
		brainTrustInstance.SetStore(st)

		// Load persisted top stories cache
		if entries, err := st.LoadTopStoriesCache(); err == nil && len(entries) > 0 {
			// Convert store entries to brain entries
			brainEntries := make([]brain.TopStoryCacheEntry, len(entries))
			for i, e := range entries {
				brainEntries[i] = brain.TopStoryCacheEntry{
					ItemID:    e.ItemID,
					Title:     e.Title,
					Label:     e.Label,
					Reason:    e.Reason,
					Zinger:    e.Zinger,
					FirstSeen: e.FirstSeen,
					LastSeen:  e.LastSeen,
					HitCount:  e.HitCount,
					MissCount: e.MissCount,
				}
			}
			brainTrustInstance.ImportTopStoriesCache(brainEntries)
		}
	}

	// Set analysis preferences from config
	brainTrustInstance.SetPreferences(cfg.Analysis.PreferLocal, cfg.Analysis.LocalForQuickOps)

	// Initialize stream with persisted density
	streamModel := stream.New()
	if cfg.UI.DensityMode == "compact" {
		streamModel.SetDensity(stream.DensityCompact)
	}

	return Model{
		stream:          streamModel,
		filterView:      filters.New(filterEngine),
		configView:      configview.New(cfg),
		sourcesView:     sources.New(sourceManager, feeds.DefaultRSSFeeds),
		brainTrustPanel: braintrust.New(),
		brainTrust:      brainTrustInstance,
		cmdPalette:      cmdPalette,
		filterEngine:    filterEngine,
		sourceManager:   sourceManager,
		aggregator:      agg,
		store:           st,
		config:          cfg,
		itemCategories:  make(map[string]string),
		mode:            modeStream,
	}
}

// Init initializes the app
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshDueSources(),
		m.tickRefresh(),
		m.brainTrustPanel.Spinner().Tick,
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

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		streamHeight := msg.Height - 4
		if m.showSources {
			streamHeight -= 3
		}
		// AI Analysis panel gets max 33% of screen height
		aiPanelHeight := msg.Height / 3
		if aiPanelHeight > 12 {
			aiPanelHeight = 12 // Cap at 12 lines max
		}
		if aiPanelHeight < 6 {
			aiPanelHeight = 6 // Min 6 lines
		}
		if m.showBrainTrust {
			streamHeight -= aiPanelHeight
			// Track analysis pane position for mouse detection
			// Analysis pane is above the status bar (last 1-2 lines)
			statusBarHeight := 1
			if len(m.recentErrors) > 0 {
				statusBarHeight += len(m.recentErrors)
			}
			m.analysisPaneBottom = msg.Height - statusBarHeight
			m.analysisPaneTop = m.analysisPaneBottom - aiPanelHeight
		}
		m.stream.SetSize(msg.Width, streamHeight)
		m.filterView.SetSize(msg.Width, msg.Height)
		m.brainTrustPanel.SetSize(msg.Width, aiPanelHeight)
		m.cmdPalette.SetWidth(min(60, msg.Width-4))
		return m, nil

	case ItemsLoadedMsg:
		if msg.Err != nil {
			logging.Error("Feed fetch failed", "source", msg.SourceName, "error", msg.Err)
			// Add to recent errors (keep last 2)
			m.recentErrors = append(m.recentErrors, RecentError{Err: msg.Err, Time: time.Now()})
			if len(m.recentErrors) > 2 {
				m.recentErrors = m.recentErrors[len(m.recentErrors)-2:]
			}
			m.lastErrorTime = time.Now() // Reset 10s timer
		} else {
			logging.Debug("Feed fetched", "source", msg.SourceName, "items", len(msg.Items), "category", msg.Category)
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

			// Track feeds loaded and trigger top stories after enough feeds loaded
			m.feedsLoadedCount++
			if !m.topStoriesAnalyzed && m.feedsLoadedCount >= 5 && m.aggregator.ItemCount() >= 20 {
				m.topStoriesAnalyzed = true
				m.stream.SetTopStoriesLoading(true)
				return m, tea.Batch(m.analyzeTopStories(), m.stream.Spinner().Tick)
			}
		}
		return m, nil

	case TickMsg:
		// Clear error pane after 10 seconds of no new errors
		if len(m.recentErrors) > 0 && time.Since(m.lastErrorTime) > 10*time.Second {
			m.recentErrors = nil
		}

		// Auto-refresh top stories every 30 seconds
		var topStoriesCmd tea.Cmd
		if m.stream.TopStoriesNeedsRefresh() && m.aggregator.ItemCount() > 10 {
			logging.Info("Auto-refreshing top stories")
			m.stream.SetTopStoriesLoading(true)
			topStoriesCmd = tea.Batch(m.analyzeTopStories(), m.stream.Spinner().Tick)
		}

		// Update smooth scroll animation
		m.stream.UpdateScroll()

		return m, tea.Batch(
			m.refreshDueSources(),
			m.tickRefresh(),
			topStoriesCmd,
		)

	case BrainTrustAnalysisMsg:
		// Update AI Analysis panel with new analysis
		if item := m.stream.SelectedItem(); item != nil && item.ID == msg.ItemID {
			analysis := m.brainTrust.GetAnalysis(msg.ItemID)
			m.brainTrustPanel.SetAnalysis(msg.ItemID, analysis)
			m.showBrainTrust = true
		}
		return m, nil

	case spinner.TickMsg:
		// Update spinner animation for AI Analysis panel and top stories
		var cmd tea.Cmd
		newSpinner, cmd := m.stream.Spinner().Update(msg)
		m.stream.UpdateSpinner(newSpinner)
		if m.showBrainTrust {
			btSpinner, btCmd := m.brainTrustPanel.Spinner().Update(msg)
			m.brainTrustPanel.UpdateSpinner(btSpinner)
			return m, tea.Batch(cmd, btCmd)
		}
		return m, cmd

	case TopStoriesMsg:
		// Update stream with AI-identified top stories
		logging.Info("TopStoriesMsg received", "stories_count", len(msg.Stories), "has_error", msg.Err != nil)
		if msg.Err != nil {
			logging.Error("Top stories analysis failed", "error", msg.Err)
			m.stream.SetTopStoriesLoading(false)
		} else {
			// Get the "breathing" list - merges current results with persistent cache entries
			breathingStories := m.brainTrust.GetBreathingTopStories(msg.Stories, 8)

			if len(breathingStories) == 0 {
				logging.Info("No top stories to display (slow news day)")
				m.stream.SetTopStoriesLoading(false)
			} else {
				// Convert results to TopStory structs
				var topStories []stream.TopStory
				items := m.aggregator.GetItems()
				itemMap := make(map[string]*feeds.Item)
				for i := range items {
					itemMap[items[i].ID] = &items[i]
				}

				for _, result := range breathingStories {
					logging.Debug("Processing breathing top story",
						"item_id", result.ItemID,
						"label", result.Label,
						"status", result.Status,
						"hit_count", result.HitCount,
						"miss_count", result.MissCount,
						"streak", result.Streak)

					if item, ok := itemMap[result.ItemID]; ok {
						topStories = append(topStories, stream.TopStory{
							Item:      item,
							Label:     result.Label,
							Reason:    result.Reason,
							Zinger:    result.Zinger,
							HitCount:  result.HitCount,
							FirstSeen: result.FirstSeen,
							Streak:    result.Streak,
							Status:    string(result.Status),
							MissCount: result.MissCount,
						})
					}
				}
				m.stream.SetTopStories(topStories)
				logging.Info("Breathing top stories updated", "count", len(topStories))
			}
		}
		return m, nil
	}

	return m, nil
}

// updateBrainTrustForSelectedItem shows/hides AI analysis panel based on selected item
func (m *Model) updateBrainTrustForSelectedItem() {
	if item := m.stream.SelectedItem(); item != nil {
		if m.brainTrust.HasAnalysis(item.ID) {
			analysis := m.brainTrust.GetAnalysis(item.ID)
			m.brainTrustPanel.SetAnalysis(item.ID, analysis)
			m.showBrainTrust = true
			m.updateAnalysisPaneBounds() // Ensure mouse detection works
		} else {
			m.showBrainTrust = false
			m.brainTrustPanel.SetVisible(false)
		}
	}
}

// updateAnalysisPaneBounds recalculates the Y coordinates for the analysis pane
// This needs to be called whenever the panel is shown or window is resized
func (m *Model) updateAnalysisPaneBounds() {
	if m.height == 0 {
		return
	}

	// Analysis panel takes up to 33% of height, capped at 12 lines
	aiPanelHeight := m.height / 3
	if aiPanelHeight > 12 {
		aiPanelHeight = 12
	}
	if aiPanelHeight < 6 {
		aiPanelHeight = 6 // Match WindowSizeMsg logic
	}

	// Status bar height
	statusBarHeight := 1
	if len(m.recentErrors) > 0 {
		statusBarHeight += len(m.recentErrors)
	}

	m.analysisPaneBottom = m.height - statusBarHeight
	m.analysisPaneTop = m.analysisPaneBottom - aiPanelHeight

	logging.Debug("Analysis pane bounds updated",
		"top", m.analysisPaneTop,
		"bottom", m.analysisPaneBottom,
		"height", m.height)
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Ensure bounds are up to date
	if m.showBrainTrust && m.analysisPaneTop == 0 {
		m.updateAnalysisPaneBounds()
	}

	// Debug logging for mouse events
	isScrollEvent := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown
	if isScrollEvent {
		logging.Debug("Mouse scroll event",
			"y", msg.Y,
			"button", msg.Button,
			"showBrainTrust", m.showBrainTrust,
			"paneTop", m.analysisPaneTop,
			"paneBottom", m.analysisPaneBottom,
			"inPane", msg.Y >= m.analysisPaneTop && msg.Y <= m.analysisPaneBottom)
	}

	// Check if mouse is over the analysis pane
	if m.showBrainTrust && msg.Y >= m.analysisPaneTop && msg.Y <= m.analysisPaneBottom {
		m.mouseOverAnalysis = true

		// Handle scroll wheel events
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.brainTrustPanel.ScrollUp(3)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.brainTrustPanel.ScrollDown(3)
			return m, nil
		}
	} else {
		m.mouseOverAnalysis = false

		// Scroll wheel over main content area scrolls the feed
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.stream.MoveUp()
			m.stream.MoveUp()
			m.stream.MoveUp()
			m.updateBrainTrustForSelectedItem()
			return m, nil
		case tea.MouseButtonWheelDown:
			m.stream.MoveDown()
			m.stream.MoveDown()
			m.stream.MoveDown()
			m.updateBrainTrustForSelectedItem()
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	logging.Debug("Key pressed", "key", msg.String(), "type", msg.Type)
	switch msg.String() {
	case "q", "ctrl+c":
		m.saveAndClose()
		return m, tea.Quit

	case "up", "k":
		m.stream.MoveUp()
		m.updateBrainTrustForSelectedItem()

	case "down", "j":
		m.stream.MoveDown()
		m.updateBrainTrustForSelectedItem()

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

	case "T":
		// Trigger top stories analysis
		logging.Info("T pressed - triggering top stories analysis")
		m.topStoriesAnalyzed = true
		m.stream.SetTopStoriesLoading(true)
		return m, tea.Batch(m.analyzeTopStories(), m.stream.Spinner().Tick)

	case "v":
		// Toggle density mode (compact/comfortable)
		m.stream.ToggleDensity()
		// Persist the setting
		if m.stream.Density() == stream.DensityCompact {
			m.config.UI.DensityMode = "compact"
		} else {
			m.config.UI.DensityMode = "comfortable"
		}
		m.config.Save()

	case "a":
		// Trigger AI analysis on selected item
		if item := m.stream.SelectedItem(); item != nil {
			m.showBrainTrust = true
			m.brainTrustPanel.Clear() // Clear any previous content
			m.brainTrustPanel.SetVisible(true)
			// AI panel gets max 33% of screen
			aiHeight := min(m.height/3, 12)
			if aiHeight < 6 {
				aiHeight = 6
			}
			m.brainTrustPanel.SetSize(m.width, aiHeight)
			m.brainTrustPanel.SetLoading(item.ID, item.Title) // Show loading state with title
			// Recalculate stream height to make room for panel
			streamHeight := m.height - 4 - aiHeight
			if m.showSources {
				streamHeight -= 3
			}
			m.stream.SetSize(m.width, streamHeight)
			// Start spinner animation and analysis
			return m, tea.Batch(
				m.analyzeBrainTrust(*item),
				m.brainTrustPanel.Spinner().Tick,
			)
		}

	case "tab":
		// Toggle AI Analysis panel visibility
		m.showBrainTrust = !m.showBrainTrust
		if m.showBrainTrust {
			// Update panel size (max 33% of screen)
			aiHeight := min(m.height/3, 12)
			if aiHeight < 6 {
				aiHeight = 6
			}
			m.brainTrustPanel.SetSize(m.width, aiHeight)
		}

	case "left", "h":
		if m.showBrainTrust {
			m.brainTrustPanel.MoveLeft()
		}

	case "right", "l":
		if m.showBrainTrust {
			m.brainTrustPanel.MoveRight()
		}

	case "/", ":":
		// Open command palette
		return m, m.cmdPalette.Activate()

	case "?":
		// TODO: Help overlay
	}

	return m, nil
}

func (m *Model) executeCommand(cmd string) (tea.Model, tea.Cmd) {
	logging.Debug("Command executed", "command", cmd)
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
	case "density", "compact", "comfortable":
		m.stream.ToggleDensity()
		// Persist the setting
		if m.stream.Density() == stream.DensityCompact {
			m.config.UI.DensityMode = "compact"
		} else {
			m.config.UI.DensityMode = "comfortable"
		}
		m.config.Save()
	case "analyze", "ai", "brain", "braintrust":
		// Trigger AI analysis on selected item
		if item := m.stream.SelectedItem(); item != nil {
			m.showBrainTrust = true
			m.brainTrustPanel.Clear() // Clear any previous content
			m.brainTrustPanel.SetVisible(true)
			// AI panel gets max 33% of screen
			aiHeight := min(m.height/3, 12)
			if aiHeight < 6 {
				aiHeight = 6
			}
			m.brainTrustPanel.SetSize(m.width, aiHeight)
			m.brainTrustPanel.SetLoading(item.ID, item.Title) // Show loading state with title
			// Recalculate stream height
			streamHeight := m.height - 4 - aiHeight
			if m.showSources {
				streamHeight -= 3
			}
			m.stream.SetSize(m.width, streamHeight)
			return m, tea.Batch(m.analyzeBrainTrust(*item), m.brainTrustPanel.Spinner().Tick)
		}
	case "top", "breaking", "headlines":
		// Trigger top stories analysis
		m.topStoriesAnalyzed = true
		m.stream.SetTopStoriesLoading(true)
		return m, tea.Batch(m.analyzeTopStories(), m.stream.Spinner().Tick)
	case "clearcache":
		// Clear the top stories cache
		m.brainTrust.ClearTopStoriesCache()
		m.stream.SetTopStories(nil)
		logging.Info("Top stories cache cleared via /clearcache command")
	case "help":
		// TODO: help overlay
	case "quit", "exit", "q":
		m.saveAndClose()
		return m, tea.Quit
	}
	return m, nil
}

// saveAndClose saves state and closes the store
func (m *Model) saveAndClose() {
	if m.store == nil {
		return
	}

	// Save top stories cache
	brainEntries := m.brainTrust.ExportTopStoriesCache()
	if len(brainEntries) > 0 {
		storeEntries := make([]store.TopStoryCacheEntry, len(brainEntries))
		for i, e := range brainEntries {
			storeEntries[i] = store.TopStoryCacheEntry{
				ItemID:    e.ItemID,
				Title:     e.Title,
				Label:     e.Label,
				Reason:    e.Reason,
				Zinger:    e.Zinger,
				FirstSeen: e.FirstSeen,
				LastSeen:  e.LastSeen,
				HitCount:  e.HitCount,
				MissCount: e.MissCount,
			}
		}
		if err := m.store.SaveTopStoriesCache(storeEntries); err != nil {
			logging.Error("Failed to save top stories cache", "error", err)
		}
	}

	m.store.Close()
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

	// AI Analysis panel (if visible)
	analysisView := ""
	if m.showBrainTrust {
		m.brainTrustPanel.SetVisible(true)
		analysisView = m.brainTrustPanel.View()
	}

	// Command bar (always visible at very bottom)
	brainHint := "a analyze"
	if m.showBrainTrust {
		brainHint = "tab hide"
	}
	densityHint := "v " + m.stream.DensityLabel()
	helpText := statusStyle.Render(fmt.Sprintf("↑↓ navigate  %s  %s  T top stories  /help  q quit", brainHint, densityHint))

	// Error log pane (shows above command bar, fades after 10s of no errors)
	var statusText string
	if len(m.recentErrors) > 0 {
		var errorLines []string
		for _, re := range m.recentErrors {
			errorLines = append(errorLines, errorStyle.Render("⚠ "+truncateError(re.Err.Error(), m.width-10)))
		}
		errorPane := lipgloss.JoinVertical(lipgloss.Left, errorLines...)
		statusText = lipgloss.JoinVertical(lipgloss.Left, errorPane, helpText)
	} else {
		statusText = helpText
	}

	// Compose with AI Analysis panel if visible
	var content string
	if analysisView != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, header, streamView, analysisView, statusText)
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left, header, streamView, statusText)
	}

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
	// Tick every 5 seconds for responsive top stories refresh
	// The actual refresh interval is controlled by TopStoriesNeedsRefresh()
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return TickMsg{}
	})
}

func (m Model) analyzeBrainTrust(item feeds.Item) tea.Cmd {
	// Get top stories context from the controller's view of current state
	// This is the proper MVC pattern - controller orchestrates data flow
	topStoriesContext := m.brainTrust.GetTopStoriesContext()

	return func() tea.Msg {
		// Clear any existing analysis for this item to start fresh
		m.brainTrust.ClearAnalysis(item.ID)

		// Use a long-lived context for API calls (don't cancel on return)
		ctx := context.Background()
		m.brainTrust.AnalyzeWithContext(ctx, item, topStoriesContext)

		// Poll until analysis is complete or timeout
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()

		timeout := time.After(120 * time.Second) // Allow time for web search
		for {
			select {
			case <-ticker.C:
				analysis := m.brainTrust.GetAnalysis(item.ID)
				if analysis != nil && !analysis.Loading {
					logging.Debug("AI analysis complete", "item", item.ID)
					return BrainTrustAnalysisMsg{
						ItemID:   item.ID,
						Analysis: *analysis,
					}
				}
			case <-timeout:
				logging.Warn("AI analysis timed out", "item", item.Title)
				return BrainTrustAnalysisMsg{
					ItemID:   item.ID,
					Analysis: brain.Analysis{Error: fmt.Errorf("analysis timed out")},
				}
			}
		}
	}
}

func (m Model) analyzeTopStories() tea.Cmd {
	return func() tea.Msg {
		// Get recent items (last 6 hours)
		items := m.stream.GetRecentItems(6)
		logging.Debug("analyzeTopStories - recent items", "count", len(items))
		if len(items) == 0 {
			items = m.aggregator.GetItems()
			logging.Debug("analyzeTopStories - using aggregator items", "count", len(items))
		}

		// Limit to recent items
		if len(items) > 100 {
			items = items[:100]
		}

		logging.Info("analyzeTopStories - calling AnalyzeTopStories", "items", len(items))
		ctx := context.Background()
		results, err := m.brainTrust.AnalyzeTopStories(ctx, items)
		logging.Info("analyzeTopStories - got results", "results", len(results), "err", err)

		return TopStoriesMsg{
			Stories: results,
			Err:     err,
		}
	}
}
