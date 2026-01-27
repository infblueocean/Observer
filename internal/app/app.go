package app

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/brain"
	"github.com/abelbrown/observer/internal/config"
	"github.com/abelbrown/observer/internal/correlation"
	"github.com/abelbrown/observer/internal/curation"
	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/feeds/hackernews"
	"github.com/abelbrown/observer/internal/feeds/manifold"
	"github.com/abelbrown/observer/internal/feeds/polymarket"
	"github.com/abelbrown/observer/internal/feeds/rss"
	"github.com/abelbrown/observer/internal/feeds/usgs"
	"github.com/abelbrown/observer/internal/logging"
	"github.com/abelbrown/observer/internal/sampling"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui/braintrust"
	"github.com/abelbrown/observer/internal/ui/briefing"
	"github.com/abelbrown/observer/internal/ui/command"
	"github.com/abelbrown/observer/internal/ui/configview"
	"github.com/abelbrown/observer/internal/ui/filters"
	"github.com/abelbrown/observer/internal/ui/radar"
	"github.com/abelbrown/observer/internal/ui/sources"
	"github.com/abelbrown/observer/internal/ui/stream"
	"github.com/charmbracelet/bubbles/spinner"
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
	modeRadar
	modeBriefing
	modeHelp
)

// RecentError holds an error with its timestamp
type RecentError struct {
	Err  error
	Time time.Time
}

// Model is the root Bubble Tea model
type Model struct {
	stream            stream.Model
	filterView        filters.Model
	filterWorkshop    filters.WorkshopModel
	configView        configview.Model
	sourcesView       sources.Model
	radarView         radar.Model
	briefingView      briefing.Model
	brainTrustPanel   braintrust.Model
	brainTrust        *brain.BrainTrust
	cmdPalette        command.Palette
	filterEngine      *curation.FilterEngine
	sourceManager     *curation.SourceManager
	aggregator        *feeds.Aggregator
	queueManager      *sampling.QueueManager // New: per-source queues with adaptive polling
	store             *store.Store
	config            *config.Config
	correlationEngine *correlation.Engine
	itemCategories    map[string]string // item ID -> category
	width             int
	height            int
	recentErrors      []RecentError // Last 2 errors
	lastErrorTime     time.Time     // When to hide error pane (10s after last error)
	mode              viewMode
	showSources       bool
	showBrainTrust    bool
	topStoriesAnalyzed   bool
	feedsLoadedCount     int

	// Mouse tracking for analysis pane scrolling
	analysisPaneTop    int  // Y coordinate where analysis pane starts
	analysisPaneBottom int  // Y coordinate where analysis pane ends
	mouseOverAnalysis  bool // True when mouse is hovering over analysis pane
	analysisFocused    bool // True when analysis pane has keyboard focus (scroll goes to analysis)

	// Streaming analysis state
	activeStreamItemID   string                   // Item ID for active stream
	activeStreamChan     <-chan brain.StreamChunk // Active streaming channel
	streamBuffer         *strings.Builder         // Buffer for accumulating chunks (pointer to avoid copy issues)
	lastStreamRenderTime time.Time                // When we last rendered stream content

	// Session tracking
	sessionID         int64     // Current session ID for tracking
	lastSessionTime   time.Time // When user was last active
	showBriefingOnStart bool    // Whether to show briefing on first render
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
		logging.Error("Failed to initialize SQLite store - correlation disabled", "path", dbPath, "error", err)
		st = nil
	} else {
		logging.Info("SQLite store initialized", "path", dbPath)
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

	// Initialize AI Analysis with unified provider system
	brainTrustInstance := brain.NewBrainTrust(nil)

	// Add all available providers (reads API keys from env vars)
	for _, p := range brain.CreateAllProviders() {
		brainTrustInstance.AddProvider(p)
		logging.Info("Provider added", "name", p.Name())
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

	// Initialize correlation engine (uses cheap extraction by default)
	var correlationEngine *correlation.Engine
	if st != nil {
		var err error
		correlationEngine, err = correlation.NewEngineSimple(st.DB())
		if err != nil {
			logging.Error("CORRELATION: Failed to initialize engine", "error", err)
		} else {
			logging.Info("CORRELATION: Engine initialized successfully - radar should work now")
		}
	} else {
		logging.Warn("CORRELATION: No store available - correlation disabled")
	}

	// Initialize stream with persisted density
	streamModel := stream.New()
	if cfg.UI.DensityMode == "compact" {
		streamModel.SetDensity(stream.DensityCompact)
	}
	if correlationEngine != nil {
		streamModel.SetCorrelationEngine(correlationEngine)
	}

	// Initialize radar
	radarModel := radar.New()
	if correlationEngine != nil {
		radarModel.SetEngine(correlationEngine)
	}

	// Initialize briefing
	briefingModel := briefing.New()
	if correlationEngine != nil {
		briefingModel.SetEngine(correlationEngine)
	}

	// Session tracking
	var sessionID int64
	var lastSessionTime time.Time
	var showBriefingOnStart bool
	if st != nil {
		// Get last session time
		lastSessionTime, _ = st.GetLastSession()

		// Start new session
		sessionID, _ = st.StartSession()

		// Set up briefing
		briefingModel.SetLastSession(lastSessionTime)
		showBriefingOnStart = briefingModel.NeedsBriefing()

		logging.Info("Session started", "session_id", sessionID, "last_session", lastSessionTime, "needs_briefing", showBriefingOnStart)
	}

	// Initialize QueueManager with round-robin sampler for balanced source representation
	qm := sampling.NewQueueManager(sampling.NewRoundRobinSampler())

	// Register all sources with QueueManager (using their configured refresh intervals)
	for _, cfg := range feeds.DefaultRSSFeeds {
		basePoll := time.Duration(cfg.RefreshMinutes) * time.Minute
		qm.RegisterSource(cfg.Name, feeds.SourceRSS, basePoll)
	}
	// Register non-RSS sources
	qm.RegisterSource("HN Top", feeds.SourceHN, time.Duration(feeds.RefreshFast)*time.Minute)
	qm.RegisterSource("USGS Significant", feeds.SourceUSGS, time.Duration(feeds.RefreshRealtime)*time.Minute)
	qm.RegisterSource("USGS M4.5+", feeds.SourceUSGS, time.Duration(feeds.RefreshNormal)*time.Minute)
	qm.RegisterSource("Polymarket", feeds.SourcePolymarket, time.Duration(feeds.RefreshNormal)*time.Minute)
	qm.RegisterSource("Manifold", feeds.SourceManifold, time.Duration(feeds.RefreshNormal)*time.Minute)

	return Model{
		stream:              streamModel,
		filterView:          filters.New(filterEngine),
		configView:          configview.New(cfg),
		sourcesView:         sources.New(sourceManager, feeds.DefaultRSSFeeds),
		radarView:           radarModel,
		briefingView:        briefingModel,
		brainTrustPanel:     braintrust.New(),
		brainTrust:          brainTrustInstance,
		cmdPalette:          cmdPalette,
		filterEngine:        filterEngine,
		sourceManager:       sourceManager,
		aggregator:          agg,
		queueManager:        qm,
		store:               st,
		config:              cfg,
		correlationEngine:   correlationEngine,
		itemCategories:      make(map[string]string),
		mode:                modeStream,
		sessionID:           sessionID,
		lastSessionTime:     lastSessionTime,
		showBriefingOnStart: showBriefingOnStart,
		streamBuffer:        &strings.Builder{},
	}
}

// Init initializes the app
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.refreshDueSources(),
		m.tickRefresh(),
		m.brainTrustPanel.Spinner().Tick,
	}

	// Show briefing on startup if needed (after a delay to let window size be set)
	if m.showBriefingOnStart {
		cmds = append(cmds, func() tea.Msg {
			return ShowBriefingMsg{}
		})
	}

	return tea.Batch(cmds...)
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
	case modeRadar:
		return m.updateRadarView(msg)
	case modeBriefing:
		return m.updateBriefingView(msg)
	case modeHelp:
		return m.updateHelpView(msg)
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

			// Merge new items into aggregator (for backward compat)
			m.aggregator.MergeItems(msg.Items)

			// Add to QueueManager (new sampling architecture)
			m.queueManager.AddItems(msg.SourceName, msg.Items)
			m.queueManager.MarkPolled(msg.SourceName)

			// Save to store
			if m.store != nil {
				m.store.SaveItems(msg.Items)
			}

			// Process items through correlation engine
			// Note: This is synchronous but ProcessItems is fast for small batches
			// TODO: Add background worker queue for large batches
			if m.correlationEngine != nil && len(msg.Items) <= 20 {
				m.correlationEngine.ProcessItems(msg.Items)
			}

			// Update aggregator state
			m.aggregator.UpdateSourceState(msg.SourceName, len(msg.Items), msg.Err)

			// Refresh stream view (with correlation data)
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
		// Use ItemID and ItemTitle from the message (captured at request time)
		// to avoid issues when feed updates change the cursor position
		if m.brainTrustPanel.GetItemID() == msg.ItemID {
			analysis := m.brainTrust.GetAnalysis(msg.ItemID)
			m.brainTrustPanel.SetAnalysis(msg.ItemID, msg.ItemTitle, analysis)
			m.showBrainTrust = true
		}
		return m, nil

	case BrainTrustStreamStartMsg:
		// Stream has started - store the channel and begin reading
		m.activeStreamItemID = msg.ItemID
		m.activeStreamChan = msg.Chunks
		m.streamBuffer.Reset()
		m.lastStreamRenderTime = time.Now()
		logging.Info("STREAMING: Stream started, beginning to read chunks",
			"item", msg.ItemID, "provider", msg.ProviderName)

		// Don't set provider name here - wait for real model ID from first chunk

		// Start reading chunks
		return m, readNextStreamChunk(msg.ItemID, msg.Chunks)

	case BrainTrustStreamChunkMsg:
		// Handle streaming analysis chunks
		if m.brainTrustPanel.GetItemID() != msg.ItemID {
			logging.Debug("STREAMING: Ignoring chunk for different item")
			return m, nil
		}

		if msg.Error != nil {
			logging.Error("STREAMING: Stream error", "error", msg.Error, "item", msg.ItemID)
			m.brainTrustPanel.SetStreamComplete(msg.Model)
			m.activeStreamItemID = ""
			m.activeStreamChan = nil
			m.streamBuffer.Reset()
			return m, nil
		}

		// Update model name from chunk if we have it (this is the real model ID)
		if msg.Model != "" {
			m.brainTrustPanel.SetStreamingProvider(msg.Model)
		}

		// Buffer the content
		if msg.Content != "" {
			m.streamBuffer.WriteString(msg.Content)
		}

		if msg.Done {
			// Flush any remaining buffer
			logging.Info("STREAMING: Done signal received",
				"buffer_len", m.streamBuffer.Len(),
				"time", time.Now().Format("15:04:05.000"))
			if m.streamBuffer.Len() > 0 {
				m.brainTrustPanel.AppendStreamContent(m.streamBuffer.String())
				m.streamBuffer.Reset()
			}
			m.brainTrustPanel.SetStreamComplete(msg.Model)
			m.activeStreamItemID = ""
			m.activeStreamChan = nil
			return m, nil
		}

		// Adaptive flush interval - fast at start, slower as content grows
		// This keeps initial UX snappy while reducing "blippiness" later
		tokenCount := m.brainTrustPanel.GetTokenCount()
		var flushInterval time.Duration
		switch {
		case tokenCount < 50:
			flushInterval = 60 * time.Millisecond // Fast initial updates
		case tokenCount < 150:
			flushInterval = 150 * time.Millisecond
		case tokenCount < 300:
			flushInterval = 300 * time.Millisecond
		default:
			flushInterval = 500 * time.Millisecond // Calm updates for long content
		}

		now := time.Now()
		sinceLastRender := now.Sub(m.lastStreamRenderTime)
		if sinceLastRender >= flushInterval && m.streamBuffer.Len() > 0 {
			m.brainTrustPanel.AppendStreamContent(m.streamBuffer.String())
			m.streamBuffer.Reset()
			m.lastStreamRenderTime = now

			// Return a tick to force re-render, then continue reading
			if m.activeStreamChan != nil {
				return m, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
					return streamTickMsg{itemID: msg.ItemID}
				})
			}
		}

		// Continue reading chunks without forcing render
		if m.activeStreamChan != nil {
			return m, readNextStreamChunk(msg.ItemID, m.activeStreamChan)
		}
		return m, nil

	case streamTickMsg:
		// After forced render tick, continue reading stream
		if m.activeStreamChan != nil && m.activeStreamItemID == msg.itemID {
			return m, readNextStreamChunk(msg.itemID, m.activeStreamChan)
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

	case ShowBriefingMsg:
		// Show briefing on startup
		if m.showBriefingOnStart && m.width > 0 {
			m.mode = modeBriefing
			m.briefingView.SetSize(m.width, m.height)
			m.briefingView.SetVisible(true)
			m.showBriefingOnStart = false
		}
		return m, nil

	case TopStoriesMsg:
		// Update stream with AI-identified top stories
		logging.Info("TopStoriesMsg received", "stories_count", len(msg.Stories), "has_error", msg.Err != nil)
		if msg.Err != nil {
			logging.Error("Top stories analysis failed", "error", msg.Err)
			// Reset timer but keep any existing stories visible
			m.stream.ResetTopStoriesRefresh()
		} else {
			// Get the "breathing" list - merges current results with persistent cache entries
			breathingStories := m.brainTrust.GetBreathingTopStories(msg.Stories, 8)

			if len(breathingStories) == 0 {
				logging.Info("No top stories to display (slow news day)")
				// Clear stories and reset timer
				m.stream.SetTopStories(nil)
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
			m.brainTrustPanel.SetAnalysis(item.ID, item.Title, analysis)
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
	mouseInPane := m.showBrainTrust && msg.Y >= m.analysisPaneTop && msg.Y <= m.analysisPaneBottom
	m.mouseOverAnalysis = mouseInPane

	// Scroll goes to analysis pane if: mouse is over it OR analysis has keyboard focus
	scrollToAnalysis := m.showBrainTrust && (mouseInPane || m.analysisFocused)

	if scrollToAnalysis {
		// Handle scroll wheel events for analysis pane
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.brainTrustPanel.ScrollUp(3)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.brainTrustPanel.ScrollDown(3)
			return m, nil
		}
	} else {
		// Scroll wheel over main content area scrolls the feed (one item at a time)
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.stream.MoveUp()
			m.analysisFocused = false // Scrolling feed clears analysis focus
			m.updateBrainTrustForSelectedItem()
			return m, nil
		case tea.MouseButtonWheelDown:
			m.stream.MoveDown()
			m.analysisFocused = false // Scrolling feed clears analysis focus
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

	case "esc", "backspace":
		// Clear analysis focus first, then filter, then hide panel
		if m.analysisFocused {
			m.analysisFocused = false
			return m, nil
		}
		if m.stream.HasFilter() {
			m.stream.ClearFilter()
			return m, nil
		}
		if m.showBrainTrust {
			m.showBrainTrust = false
			return m, nil
		}

	case "up", "k":
		m.stream.MoveUp()
		m.analysisFocused = false // Return focus to feed when navigating
		m.updateBrainTrustForSelectedItem()

	case "down", "j":
		m.stream.MoveDown()
		m.analysisFocused = false // Return focus to feed when navigating
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

	case "ctrl+x":
		// Toggle Story Radar
		m.mode = modeRadar
		m.radarView.SetSize(m.width, m.height)
		m.radarView.SetVisible(true)

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
		// Trigger AI analysis on selected item with cloud provider (streaming)
		if item := m.stream.SelectedItem(); item != nil {
			// Don't trigger if analysis already in progress (for any item)
			// This prevents issues when feed updates change cursor position
			if m.brainTrustPanel.IsLoading() {
				logging.Debug("Analysis already in progress, ignoring 'a' key",
					"requested_item", item.ID,
					"loading_item", m.brainTrustPanel.GetItemID())
				return m, nil
			}

			m.showBrainTrust = true
			m.analysisFocused = true // Focus shifts to analysis pane for scrolling
			m.brainTrustPanel.Clear() // Clear any previous display (but keeps DB history)
			m.brainTrustPanel.SetVisible(true)
			// AI panel gets max 33% of screen
			aiHeight := min(m.height/2, 20)
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
			// Use streaming for cloud analysis - tokens appear as generated
			return m, tea.Batch(
				m.analyzeBrainTrustCloudStreaming(*item),
				m.brainTrustPanel.Spinner().Tick,
			)
		}

	case "A": // Shift+A for local-only analysis with streaming (tokens appear as generated)
		if item := m.stream.SelectedItem(); item != nil {
			// Don't trigger if analysis already in progress
			if m.brainTrustPanel.IsLoading() {
				logging.Debug("Analysis already in progress, ignoring 'A' key",
					"requested_item", item.ID,
					"loading_item", m.brainTrustPanel.GetItemID())
				return m, nil
			}
			m.showBrainTrust = true
			m.analysisFocused = true // Focus shifts to analysis pane for scrolling
			m.brainTrustPanel.Clear()
			m.brainTrustPanel.SetVisible(true)
			aiHeight := min(m.height/2, 20)
			if aiHeight < 6 {
				aiHeight = 6
			}
			m.brainTrustPanel.SetSize(m.width, aiHeight)
			m.brainTrustPanel.SetLoading(item.ID, item.Title)
			streamHeight := m.height - 4 - aiHeight
			if m.showSources {
				streamHeight -= 3
			}
			m.stream.SetSize(m.width, streamHeight)
			// Use streaming for local analysis - tokens appear as they're generated
			return m, tea.Batch(
				m.analyzeBrainTrustStreaming(*item),
				m.brainTrustPanel.Spinner().Tick,
			)
		}

	case "tab":
		// Toggle AI Analysis panel visibility OR toggle focus when panel is visible
		if m.showBrainTrust {
			// Panel is visible - toggle focus between feed and analysis
			m.analysisFocused = !m.analysisFocused
		} else {
			// Panel is hidden - show it
			m.showBrainTrust = true
			// Update panel size (max 33% of screen)
			aiHeight := min(m.height/2, 20)
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
		// Show briefing (Catch Me Up)
		m.mode = modeBriefing
		m.briefingView.SetSize(m.width, m.height)
		m.briefingView.SetVisible(true)
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
			aiHeight := min(m.height/2, 20)
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
		m.mode = modeHelp
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

	// End the session
	if m.sessionID > 0 {
		if err := m.store.EndSession(m.sessionID); err != nil {
			logging.Error("Failed to end session", "error", err)
		} else {
			logging.Info("Session ended", "session_id", m.sessionID)
		}
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

func (m Model) updateRadarView(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+x":
			m.mode = modeStream
			m.radarView.SetVisible(false)
			return m, nil
		case "up", "k":
			m.radarView.MoveUp()
		case "down", "j":
			m.radarView.MoveDown()
		case "tab":
			m.radarView.SwitchSection()
		case "l":
			m.radarView.ToggleLogFilter()
		case "enter":
			// Filter by selected cluster/entity
			if m.radarView.FocusSection() == 0 {
				// Cluster selected
				clusterID := m.radarView.SelectedClusterID()
				if clusterID != "" && m.correlationEngine != nil {
					clusters := m.correlationEngine.GetActiveClusters(10)
					for _, c := range clusters {
						if c.ID == clusterID {
							label := c.Summary
							if len(label) > 30 {
								label = label[:27] + "..."
							}
							m.stream.SetFilterByCluster(clusterID, "Cluster: "+label)
							break
						}
					}
				}
			} else {
				// Entity selected
				entityID := m.radarView.SelectedEntityID()
				if entityID != "" {
					m.stream.SetFilterByEntity(entityID, "Entity: "+entityID)
				}
			}
			m.mode = modeStream
			m.radarView.SetVisible(false)
		case "backspace", "delete":
			// Clear filter and return
			m.stream.ClearFilter()
			m.mode = modeStream
			m.radarView.SetVisible(false)
		}
	case tea.WindowSizeMsg:
		m.radarView.SetSize(msg.Width, msg.Height)
	}
	return m, nil
}

func (m Model) updateBriefingView(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "enter":
			m.mode = modeStream
			m.briefingView.SetVisible(false)
			return m, nil
		case "up", "k":
			m.briefingView.ScrollUp()
		case "down", "j":
			m.briefingView.ScrollDown()
		}
	case tea.WindowSizeMsg:
		m.briefingView.SetSize(msg.Width, msg.Height)
	}
	return m, nil
}

func (m Model) updateHelpView(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Any key closes help
		m.mode = modeStream
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

func (m Model) renderHelpView() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#58a6ff"))

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#f0883e"))

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7ee787"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b949e"))

	help := []string{
		titleStyle.Render("  OBSERVER - Keyboard Shortcuts"),
		"",
		sectionStyle.Render("  Navigation"),
		fmt.Sprintf("  %s / %s    Navigate items", keyStyle.Render("↑↓"), keyStyle.Render("jk")),
		fmt.Sprintf("  %s        Mark item read / open URL", keyStyle.Render("Enter")),
		fmt.Sprintf("  %s            Scroll up a page", keyStyle.Render("PgUp")),
		fmt.Sprintf("  %s          Scroll down a page", keyStyle.Render("PgDn")),
		"",
		sectionStyle.Render("  AI Features"),
		fmt.Sprintf("  %s            Analyze selected item", keyStyle.Render("a")),
		fmt.Sprintf("  %s            Analyze top stories", keyStyle.Render("T")),
		fmt.Sprintf("  %s          Toggle AI analysis panel", keyStyle.Render("Tab")),
		"",
		sectionStyle.Render("  Views"),
		fmt.Sprintf("  %s       Story Radar (correlations)", keyStyle.Render("Ctrl+X")),
		fmt.Sprintf("  %s            Catch Me Up briefing", keyStyle.Render("?")),
		fmt.Sprintf("  %s            Open filter manager", keyStyle.Render("f")),
		fmt.Sprintf("  %s            Open source manager", keyStyle.Render("S")),
		fmt.Sprintf("  %s            Open config", keyStyle.Render("c")),
		fmt.Sprintf("  %s            Toggle source panel", keyStyle.Render("t")),
		"",
		sectionStyle.Render("  Display"),
		fmt.Sprintf("  %s            Toggle density (compact/comfortable)", keyStyle.Render("v")),
		fmt.Sprintf("  %s            Shuffle items", keyStyle.Render("s")),
		"",
		sectionStyle.Render("  Actions"),
		fmt.Sprintf("  %s            Refresh due sources", keyStyle.Render("r")),
		fmt.Sprintf("  %s            Force refresh all", keyStyle.Render("R")),
		fmt.Sprintf("  %s            Command mode", keyStyle.Render("/")),
		fmt.Sprintf("  %s            Quit", keyStyle.Render("q")),
		"",
		sectionStyle.Render("  Commands"),
		descStyle.Render("  /help /shuffle /refresh /density /filters /sources"),
		descStyle.Render("  /config /panel /clearcache"),
		"",
		descStyle.Render("  Press any key to close"),
	}

	content := strings.Join(help, "\n")

	// Center the help text
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#58a6ff")).
		Padding(1, 2).
		Width(60)

	box := boxStyle.Render(content)

	// Center in terminal
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) shuffleItems() {
	items := m.aggregator.GetItems()
	rand.Shuffle(len(items), func(i, j int) {
		items[i], items[j] = items[j], items[i]
	})
	m.stream.SetItems(items)
}

func (m *Model) updateStreamItems() {
	// Use QueueManager for balanced sampling across sources
	// The sampler ensures diversity - chatty sources don't dominate
	maxItems := 500 // configurable limit
	if m.config != nil && m.config.UI.ItemLimit > 0 {
		maxItems = m.config.UI.ItemLimit
	}

	items := m.queueManager.Sample(maxItems)

	// Sort by published time, newest first
	// (sampler returns interleaved items, we re-sort for chronological display)
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
	case modeRadar:
		return m.radarView.View()
	case modeBriefing:
		return m.briefingView.View()
	case modeHelp:
		return m.renderHelpView()
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

	// Header with source health indicator
	blocked := m.aggregator.BlockedCount()
	health := m.aggregator.GetSourceHealth()

	// Show healthy/total sources with percentage if we have data
	var sourceIndicator string
	if health.Total > 0 && health.Healthy > 0 {
		pct := float64(health.Healthy) / float64(health.Total) * 100
		sourceIndicator = fmt.Sprintf("%d/%d sources (%.0f%%)", health.Healthy, health.Total, pct)
	} else {
		sourceIndicator = fmt.Sprintf("%d sources", m.aggregator.SourceCount())
	}

	// Show sampled/total items (sampled = what's displayed, total = in DB)
	sampledCount := m.stream.ItemCount()
	totalCount := m.aggregator.ItemCount()
	var itemsIndicator string
	if sampledCount < totalCount {
		itemsIndicator = fmt.Sprintf("%d/%d items", sampledCount, totalCount)
	} else {
		itemsIndicator = fmt.Sprintf("%d items", totalCount)
	}

	headerText := fmt.Sprintf("◉ OBSERVER  │  %s  │  %s",
		sourceIndicator,
		itemsIndicator)
	if blocked > 0 {
		headerText += fmt.Sprintf("  │  %d blocked", blocked)
	}
	if health.Failing > 0 {
		headerText += fmt.Sprintf("  │  %d failing", health.Failing)
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
						ItemID:    item.ID,
						ItemTitle: item.Title,
						Analysis:  *analysis,
					}
				}
			case <-timeout:
				logging.Warn("AI analysis timed out", "item", item.Title)
				return BrainTrustAnalysisMsg{
					ItemID:    item.ID,
					ItemTitle: item.Title,
					Analysis:  brain.Analysis{Error: fmt.Errorf("analysis timed out")},
				}
			}
		}
	}
}

// analyzeBrainTrustRandom triggers analysis with a randomly selected provider
// Each press of 'a' may use a different provider for variety
func (m Model) analyzeBrainTrustRandom(item feeds.Item) tea.Cmd {
	topStoriesContext := m.brainTrust.GetTopStoriesContext()

	return func() tea.Msg {
		// Clear in-memory cache to start fresh (DB history preserved)
		m.brainTrust.ClearAnalysis(item.ID)

		ctx := context.Background()
		m.brainTrust.AnalyzeRandomProvider(ctx, item, topStoriesContext)

		// Poll until analysis is complete or timeout
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()

		timeout := time.After(120 * time.Second)
		for {
			select {
			case <-ticker.C:
				analysis := m.brainTrust.GetAnalysis(item.ID)
				if analysis != nil && !analysis.Loading {
					logging.Debug("AI analysis complete (random provider)", "item", item.ID)
					return BrainTrustAnalysisMsg{
						ItemID:    item.ID,
						ItemTitle: item.Title,
						Analysis:  *analysis,
					}
				}
			case <-timeout:
				logging.Warn("AI analysis timed out", "item", item.Title)
				return BrainTrustAnalysisMsg{
					ItemID:    item.ID,
					ItemTitle: item.Title,
					Analysis:  brain.Analysis{Error: fmt.Errorf("analysis timed out")},
				}
			}
		}
	}
}

// analyzeBrainTrustLocal forces local-only analysis (LFM-instruct → LFM-transcript pipeline)
func (m Model) analyzeBrainTrustLocal(item feeds.Item) tea.Cmd {
	topStoriesContext := m.brainTrust.GetTopStoriesContext()

	return func() tea.Msg {
		m.brainTrust.ClearAnalysis(item.ID)

		ctx := context.Background()
		m.brainTrust.AnalyzeLocalWithContext(ctx, item, topStoriesContext)

		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()

		timeout := time.After(60 * time.Second) // Local should be faster
		for {
			select {
			case <-ticker.C:
				analysis := m.brainTrust.GetAnalysis(item.ID)
				if analysis != nil && !analysis.Loading {
					logging.Debug("Local AI analysis complete", "item", item.ID)
					return BrainTrustAnalysisMsg{
						ItemID:    item.ID,
						ItemTitle: item.Title,
						Analysis:  *analysis,
					}
				}
			case <-timeout:
				logging.Warn("Local AI analysis timed out", "item", item.Title)
				return BrainTrustAnalysisMsg{
					ItemID:    item.ID,
					ItemTitle: item.Title,
					Analysis:  brain.Analysis{Error: fmt.Errorf("local analysis timed out")},
				}
			}
		}
	}
}

// analyzeBrainTrustStreaming triggers streaming analysis using local Ollama
// Tokens are displayed as they arrive for better UX during long generations
func (m Model) analyzeBrainTrustStreaming(item feeds.Item) tea.Cmd {
	topStoriesContext := m.brainTrust.GetTopStoriesContext()
	startTime := time.Now()

	return func() tea.Msg {
		// Check if we have a streaming provider
		streamingProvider := m.brainTrust.GetStreamingProvider()
		if streamingProvider == nil {
			// Fall back to non-streaming
			logging.Debug("No streaming provider available, using non-streaming")
			return m.analyzeBrainTrustLocal(item)()
		}

		logging.Info("STREAMING: Using provider",
			"provider", streamingProvider.Name(),
			"elapsed_since_keypress_ms", time.Since(startTime).Milliseconds())

		// Build the analysis prompt
		systemPrompt, userPrompt := m.brainTrust.BuildAnalysisPrompt(item, topStoriesContext)

		ctx := context.Background()
		logging.Info("STREAMING: Calling GenerateStream", "provider", streamingProvider.Name())

		chunks, err := streamingProvider.GenerateStream(ctx, brain.Request{
			SystemPrompt: systemPrompt,
			UserPrompt:   userPrompt,
			MaxTokens:    1000,
		})
		if err != nil {
			logging.Error("Failed to start streaming analysis", "error", err,
				"elapsed_ms", time.Since(startTime).Milliseconds())
			return BrainTrustAnalysisMsg{
				ItemID:    item.ID,
				ItemTitle: item.Title,
				Analysis:  brain.Analysis{Error: err},
			}
		}

		logging.Info("STREAMING: GenerateStream returned channel",
			"elapsed_ms", time.Since(startTime).Milliseconds())

		// Return the stream start message with the channel
		// The Update handler will store it and start reading
		return BrainTrustStreamStartMsg{
			ItemID:       item.ID,
			ItemTitle:    item.Title,
			ProviderName: streamingProvider.Name(),
			Chunks:       chunks,
		}
	}
}

// analyzeBrainTrustCloudStreaming triggers streaming analysis using cloud provider
// Tokens are displayed as they arrive for better UX during API calls
func (m Model) analyzeBrainTrustCloudStreaming(item feeds.Item) tea.Cmd {
	topStoriesContext := m.brainTrust.GetTopStoriesContext()

	return func() tea.Msg {
		// Check if we have a cloud streaming provider
		streamingProvider := m.brainTrust.GetCloudStreamingProvider()
		if streamingProvider == nil {
			// Fall back to non-streaming random provider
			logging.Info("STREAMING: No cloud streaming provider available, falling back to non-streaming")
			return m.analyzeBrainTrustRandom(item)()
		}

		providerName := streamingProvider.(brain.Provider).Name()
		logging.Info("STREAMING: Starting cloud streaming analysis", "provider", providerName)

		// Build the analysis prompt
		systemPrompt, userPrompt := m.brainTrust.BuildAnalysisPrompt(item, topStoriesContext)

		ctx := context.Background()
		chunks, err := streamingProvider.GenerateStream(ctx, brain.Request{
			SystemPrompt: systemPrompt,
			UserPrompt:   userPrompt,
			MaxTokens:    1500, // Cloud can handle more tokens
		})
		if err != nil {
			logging.Error("Failed to start cloud streaming analysis", "error", err)
			return BrainTrustAnalysisMsg{
				ItemID:    item.ID,
				ItemTitle: item.Title,
				Analysis:  brain.Analysis{Error: err},
			}
		}

		// Return the stream start message with the channel
		return BrainTrustStreamStartMsg{
			ItemID:       item.ID,
			ItemTitle:    item.Title,
			ProviderName: providerName,
			Chunks:       chunks,
		}
	}
}

// readNextStreamChunk creates a command to read the next chunk from a stream
func readNextStreamChunk(itemID string, chunks <-chan brain.StreamChunk) tea.Cmd {
	logging.Info("STREAMING: readNextStreamChunk called", "item", itemID)
	return func() tea.Msg {
		logging.Info("STREAMING: readNextStreamChunk executing, waiting on channel")
		chunk, ok := <-chunks
		logging.Info("STREAMING: Got from channel", "ok", ok, "len", len(chunk.Content), "done", chunk.Done)
		if !ok {
			return BrainTrustStreamChunkMsg{
				ItemID: itemID,
				Done:   true,
			}
		}

		return BrainTrustStreamChunkMsg{
			ItemID:  itemID,
			Content: chunk.Content,
			Done:    chunk.Done,
			Model:   chunk.Model,
			Error:   chunk.Error,
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
