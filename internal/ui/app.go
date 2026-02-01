package ui

import (
	"context"
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/abelbrown/observer/internal/filter"
	"github.com/abelbrown/observer/internal/otel"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui/media"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// newQueryID generates a short random hex string for search correlation.
// 8 bytes = 16 hex chars. Unique within and across sessions.
func newQueryID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b[:])
}

// AppMode determines which input handler and view layout are active.
type AppMode int

const (
	ModeList    AppMode = iota // chronological feed (default)
	ModeSearch                 // typing in search input
	ModeResults                // viewing search/MLT results
	ModeHistory                // browsing search history (future)
	ModeArticle                // reading full article (future)
	ModeMedia                  // "Engineered" cyber-noir view
)

// App is the root Bubble Tea model.
// IMPORTANT: App does NOT hold *store.Store. It receives items via messages.
type App struct {
	loadItems       func() tea.Cmd
	loadRecentItems func() tea.Cmd                                    // loads last 1h (fast first paint)
	loadSearchPool  func(ctx context.Context, queryID string) tea.Cmd // loads all items for search
	markRead        func(id string) tea.Cmd
	triggerFetch    func() tea.Cmd
	embedQuery      func(ctx context.Context, query string, queryID string) tea.Cmd
	scoreEntry      func(ctx context.Context, query string, doc string, itemID string, queryID string) tea.Cmd // Ollama per-entry path (not wired in production; Jina batch path used instead)
	batchRerank     func(ctx context.Context, query string, docs []string, queryID string) tea.Cmd             // Jina batch rerank — single API call for all docs
	searchFTS       func(query string, limit int) ([]store.Item, error)                                        // FTS5 instant search

	items       []store.Item
	embeddings  map[string][]float32 // item ID -> embedding
	cursor      int
	err         error
	width       int
	height      int
	ready       bool
	loading     bool
	statusText  string    // activity status for status bar; empty = no activity
	searchStart time.Time // when current search was initiated

	// Media View (ModeMedia)
	mediaView media.MainModel

	// Two-stage loading
	fullLoaded bool // true after Stage 2 completes

	// Search mode: press "/" to activate, type query, Enter to submit
	mode         AppMode
	modeStack    []AppMode
	filterInput  textinput.Model
	activeQuery  string // query stored at submit time (independent of live input)
	mltSeedID    string // when set, results are seeded by this item ID
	mltSeedTitle string // cached seed title for render

	// Full-history search: save/restore chronological view
	savedItems      []store.Item         // chronological items saved before search
	savedEmbeddings map[string][]float32 // embeddings saved before search

	// Search pool loading
	searchPoolPending bool                 // true while loading search pool from DB
	poolItems         []store.Item         // buffered pool; merged into items when embedding arrives
	poolEmbeddings    map[string][]float32 // buffered pool embeddings

	// Query state
	queryEmbedding    []float32 // current query's embedding
	embeddingPending  bool      // true while waiting for query embedding
	lastEmbeddedQuery string    // the query that was last embedded

	// Search correlation
	queryID string // current search correlation ID; empty when no search active

	// Per-query cancellation
	searchCtx    context.Context
	searchCancel context.CancelFunc

	// Rerank policy
	autoReranks bool

	// Rerank progress (package-manager style)
	rerankPending  bool         // true during reranking
	rerankEntries  []store.Item // entries being reranked
	rerankScores   []float32    // scores per entry
	rerankProgress int          // entries scored so far
	rerankQuery    string       // the query that started the current rerank

	// UI components
	spinner spinner.Model

	// Observability
	logger       *otel.Logger
	ring         *otel.RingBuffer
	debugVisible bool

	// Feature flags
	features Features

	// Layout
	alignedList bool

	// Animation
	shimmerOffset int
}

// ObsConfig groups observability dependencies to prevent AppConfig god-object growth.
type ObsConfig struct {
	Logger *otel.Logger
	Ring   *otel.RingBuffer
}

// AppConfig holds the configuration for creating a new App.
type AppConfig struct {
	LoadItems       func() tea.Cmd
	LoadRecentItems func() tea.Cmd
	LoadSearchPool  func(ctx context.Context, queryID string) tea.Cmd
	MarkRead        func(id string) tea.Cmd
	TriggerFetch    func() tea.Cmd
	EmbedQuery      func(ctx context.Context, query string, queryID string) tea.Cmd
	ScoreEntry      func(ctx context.Context, query string, doc string, itemID string, queryID string) tea.Cmd
	BatchRerank     func(ctx context.Context, query string, docs []string, queryID string) tea.Cmd
	SearchFTS       func(query string, limit int) ([]store.Item, error)
	Embeddings      map[string][]float32
	Obs             ObsConfig
	AutoReranks     bool
	Features        Features
}

// NewApp creates a new App with the given command functions.
func NewApp(loadItems func() tea.Cmd, markRead func(id string) tea.Cmd, triggerFetch func() tea.Cmd) App {
	return newApp(AppConfig{
		LoadItems:    loadItems,
		MarkRead:     markRead,
		TriggerFetch: triggerFetch,
	})
}

// NewAppWithConfig creates a new App with the given configuration.
func NewAppWithConfig(cfg AppConfig) App {
	return newApp(cfg)
}

func newApp(cfg AppConfig) App {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 100
	ti.Width = 40

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Spinner.FPS = 100 * time.Millisecond
	s.Style = lipgloss.NewStyle().Foreground(colorSpinner)

	embeddings := cfg.Embeddings
	if embeddings == nil {
		embeddings = make(map[string][]float32)
	}

	logger := cfg.Obs.Logger
	if logger == nil {
		logger = otel.NewNullLogger()
	}

	return App{
		loadItems:       cfg.LoadItems,
		loadRecentItems: cfg.LoadRecentItems,
		loadSearchPool:  cfg.LoadSearchPool,
		markRead:        cfg.MarkRead,
		triggerFetch:    cfg.TriggerFetch,
		embedQuery:      cfg.EmbedQuery,
		scoreEntry:      cfg.ScoreEntry,
		batchRerank:     cfg.BatchRerank,
		searchFTS:       cfg.SearchFTS,
		cursor:          0,
		filterInput:     ti,
		embeddings:      embeddings,
		spinner:         s,
		logger:          logger,
		ring:            cfg.Obs.Ring,
		mode:            ModeList,
		searchCtx:       context.Background(),
		autoReranks:     cfg.AutoReranks,
		features:        cfg.Features,
		width:           80,
		height:          24,
		ready:           true,
	}
}

// Init initializes the App by loading items.
// Uses loadRecentItems (Stage 1) for fast first paint if available,
// otherwise falls back to loadItems (full load).
func (a App) Init() tea.Cmd {
	var cmds []tea.Cmd
	if a.loadRecentItems != nil {
		cmds = append(cmds, a.loadRecentItems())
	} else if a.loadItems != nil {
		cmds = append(cmds, a.loadItems())
	}
	
	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

func (a *App) pushMode(m AppMode) {
	a.modeStack = append(a.modeStack, a.mode)
	a.mode = m
}

func (a *App) popMode(defaultMode AppMode) {
	// Cancel any in-flight search work when leaving Results or Search.
	if a.mode == ModeResults || a.mode == ModeSearch {
		a.cancelSearch()
	}
	n := len(a.modeStack)
	if n == 0 {
		a.mode = defaultMode
		return
	}
	a.mode = a.modeStack[n-1]
	a.modeStack = a.modeStack[:n-1]
}

func (a *App) replaceMode(m AppMode) {
	a.mode = m
}

func shimmerCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return ShimmerTick{}
	})
}

func (a App) shimmerSpan() int {
	if len(a.items) == 0 {
		return a.width
	}
	start := a.cursor - 7
	if start < 0 {
		start = 0
	}
	end := a.cursor + 7
	if end >= len(a.items) {
		end = len(a.items) - 1
	}
	if end < start {
		return a.width
	}
	total := 0
	count := 0
	for i := start; i <= end; i++ {
		total += measureItemLineWidth(a.items[i], a.width, a.alignedList)
		count++
	}
	if count == 0 {
		return a.width
	}
	avg := total / count
	if avg < 20 {
		avg = 20
	}
	if avg > a.width {
		avg = a.width
	}
	return avg
}

// Update handles messages and returns the updated model and any commands.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if otel.TraceEnabled() {
		a.traceMsg(msg)
		defer a.traceHandled(time.Now())
	}

	switch msg := msg.(type) {
	case ShimmerTick:
		span := a.shimmerSpan()
		if span <= 0 {
			span = a.width
		}
		if span <= 0 {
			span = 80
		}
		a.shimmerOffset = (a.shimmerOffset + 1) % span
		return a, shimmerCmd()

	case tea.KeyMsg:
		if a.mode == ModeMedia {
			var cmd tea.Cmd
			m, cmd := a.mediaView.Update(msg)
			a.mediaView = m.(media.MainModel)
			return a, cmd
		}
		return a.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		if a.mode == ModeMedia {
			m, _ := a.mediaView.Update(msg)
			a.mediaView = m.(media.MainModel)
		}
		return a, nil

	case spinner.TickMsg:
		if a.statusText != "" {
			var cmd tea.Cmd
			a.spinner, cmd = a.spinner.Update(msg)
			return a, cmd
		}
		return a, nil

	case media.TickMsg:
		if a.mode == ModeMedia {
			m, cmd := a.mediaView.Update(msg)
			a.mediaView = m.(media.MainModel)
			return a, cmd
		}
		return a, nil

	case ItemsLoaded:
		a.loading = false
		if msg.Err != nil {
			a.err = msg.Err
			return a, nil
		}

		// If search is active, update savedItems instead of live view
		if a.savedItems != nil {
			a.savedItems = msg.Items
			if msg.Embeddings != nil {
				a.savedEmbeddings = msg.Embeddings
			}
			// Still chain Stage 2 if needed
			if !a.fullLoaded && a.loadItems != nil {
				a.fullLoaded = true
				return a, a.loadItems()
			}
			return a, nil
		}

		// Cursor stability: record current item ID
		cursorID := ""
		if a.cursor < len(a.items) && len(a.items) > 0 {
			cursorID = a.items[a.cursor].ID
		}

		a.items = msg.Items
		a.err = nil
		if msg.Embeddings != nil {
			a.embeddings = msg.Embeddings
		}

		// Restore cursor by ID (not index)
		a.restoreCursor(cursorID)

		// Cancel in-flight reranking - items have changed
		if a.rerankPending {
			a.rerankPending = false
			a.rerankEntries = nil
			a.rerankScores = nil
			a.rerankProgress = 0
			a.statusText = ""
		}
		// Re-apply reranking if we have an active query
		if a.hasQuery() && len(a.queryEmbedding) > 0 {
			a.rerankItemsByEmbedding()
		}

		// Chain Stage 2: load full corpus after first paint
		if !a.fullLoaded && a.loadItems != nil {
			a.fullLoaded = true
			return a, a.loadItems()
		}
		return a, nil

	case QueryEmbedded:
		if !a.embeddingPending {
			return a, nil
		}
		// Stale check first: don't clear state for old queries
		if msg.QueryID != "" && msg.QueryID != a.queryID {
			return a, nil
		}
		a.embeddingPending = false
		if msg.Err != nil {
			// Embedding failed — discard buffered pool (can't rank without embedding).
			// Keep showing FTS results if we have them.
			a.err = msg.Err
			a.poolItems = nil
			a.poolEmbeddings = nil
			a.statusText = fmt.Sprintf("Search failed: %v", msg.Err)
			return a, nil
		}
		if msg.Query == a.activeQuery {
			a.queryEmbedding = msg.Embedding
			a.logger.Emit(otel.Event{Kind: otel.KindQueryEmbed, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Dims: len(msg.Embedding), Query: msg.Query})
			a.lastEmbeddedQuery = msg.Query
			// Merge buffered pool if it arrived while we were waiting for embedding
			if a.poolItems != nil {
				a.items = a.poolItems
				a.embeddings = a.poolEmbeddings
				a.poolItems = nil
				a.poolEmbeddings = nil
			}
			// Always apply fast cosine reranking for immediate feedback
			a.rerankItemsByEmbedding()
			a.logger.Emit(otel.Event{Kind: otel.KindCosineRerank, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Count: len(a.items), Query: a.activeQuery})
			// Only start cross-encoder if search pool has already arrived
			if !a.searchPoolPending {
				if a.autoReranks && a.rerankerAvailable() {
					return a.startReranking(a.activeQuery)
				}
				a.statusText = a.cosineCompleteHint()
			} else {
				a.statusText = a.searchStage()
			}
		}
		return a, nil

	case SearchPoolLoaded:
		if !a.searchPoolPending {
			return a, nil
		}
		a.searchPoolPending = false
		// Stale check: QueryID
		if msg.QueryID != "" && msg.QueryID != a.queryID {
			return a, nil
		}
		// Discard if no active query (user pressed Esc before pool arrived)
		if !a.hasQuery() {
			return a, nil
		}
		if msg.Err != nil {
			a.err = msg.Err
			a.statusText = fmt.Sprintf("Search failed: %v", msg.Err)
			return a, nil
		}
		// Cancel in-flight reranking — items are about to change
		if a.rerankPending {
			a.rerankPending = false
			a.rerankEntries = nil
			a.rerankScores = nil
			a.rerankProgress = 0
			a.statusText = ""
		}
		a.logger.Emit(otel.Event{Kind: otel.KindSearchPool, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Count: len(msg.Items), Query: a.activeQuery, Extra: map[string]any{"embeddings": len(msg.Embeddings)}})
		// If query embedding already arrived, merge pool into live view and rank.
		// Otherwise, buffer pool — keep showing FTS results until embedding arrives.
		if len(a.queryEmbedding) > 0 {
			a.items = msg.Items
			a.embeddings = msg.Embeddings
			a.poolItems = nil
			a.poolEmbeddings = nil
			a.rerankItemsByEmbedding()
			if a.mltSeedID != "" {
				a.excludeItem(a.mltSeedID)
			}
			if a.autoReranks && a.rerankerAvailable() {
				return a.startReranking(a.activeQuery)
			}
			a.statusText = a.cosineCompleteHint()
		} else if a.embeddingPending {
			// Embedding in flight — buffer pool until it arrives
			a.poolItems = msg.Items
			a.poolEmbeddings = msg.Embeddings
			a.statusText = a.searchStage()
		} else {
			// No embedding coming (no AI backend or embed already failed).
			// Show pool as-is — FTS results are already visible, this is the full corpus.
			a.items = msg.Items
			a.embeddings = msg.Embeddings
			a.statusText = ""
		}
		return a, nil

	case EntryReranked:
		return a.handleEntryReranked(msg)

	case RerankComplete:
		if !a.rerankPending {
			return a, nil
		}
		// Stale check: QueryID is authoritative when present
		if msg.QueryID != "" {
			if msg.QueryID != a.queryID {
				return a, nil
			}
		} else if msg.Query != a.activeQuery {
			// Fallback: text comparison when no QueryID
			return a, nil
		}
		a.rerankPending = false
		if msg.Err != nil {
			// Graceful degradation: keep cosine-ranked results, don't show error modal.
			// Log the error for diagnostics but let the user work with what they have.
			a.logger.Emit(otel.Event{Kind: otel.KindSearchComplete, Level: otel.LevelWarn, Comp: "ui", Dur: time.Since(a.searchStart), Query: a.activeQuery, Err: msg.Err.Error()})
			a.statusText = "Rerank failed -- showing cosine results"
			a.rerankEntries = nil
			a.rerankScores = nil
			a.rerankProgress = 0
			return a, nil
		}
		a.statusText = ""
		a.logger.Emit(otel.Event{Kind: otel.KindSearchComplete, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Query: a.activeQuery})
		if len(msg.Scores) > 0 {
			for i, score := range msg.Scores {
				if i < len(a.rerankScores) {
					a.rerankScores[i] = score
				}
			}
			a.rerankProgress = len(a.rerankEntries)
			a.applyScoresAsOrder()
		}
		// D9: clear rerank state after success
		a.rerankEntries = nil
		a.rerankScores = nil
		a.rerankProgress = 0
		return a, nil

	case ItemMarkedRead:
		for i := range a.items {
			if a.items[i].ID == msg.ID {
				a.items[i].Read = true
				break
			}
		}
		// Also update savedItems if we are in a search/view that has snapshotted the list
		if a.savedItems != nil {
			for i := range a.savedItems {
				if a.savedItems[i].ID == msg.ID {
					a.savedItems[i].Read = true
					break
				}
			}
		}
		return a, nil

	case FetchComplete:
		a.loading = false
		if msg.Err != nil {
			a.err = msg.Err
		} else if msg.NewItems > 0 {
			if a.loadItems != nil {
				a.loading = true
				return a, a.loadItems()
			}
		}
		return a, nil

	case RefreshTick:
		if a.loadItems != nil {
			return a, a.loadItems()
		}
		return a, nil
	}

	return a, nil
}

// hasQuery returns true if there is a submitted query with results showing.
func (a App) hasQuery() bool {
	return a.activeQuery != "" || a.mltSeedID != ""
}

// handleKeyMsg processes keyboard input.
func (a App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Clear any existing error on key press
	if a.err != nil {
		a.err = nil
	}

	// Global keys — handled inline to keep mutations on the same value copy
	switch msg.Type {
	case tea.KeyCtrlC:
		a.cancelSearch()
		return a, tea.Quit
	case tea.KeyEsc:
		if a.debugVisible {
			a.debugVisible = false
			return a, nil
		}
	}
	if a.mode != ModeSearch {
		switch msg.String() {
		case "q":
			a.cancelSearch()
			return a, tea.Quit
		case "?":
			a.debugVisible = !a.debugVisible
			return a, nil
		}
	}

	switch a.mode {
	case ModeSearch:
		return a.handleSearchKeys(msg)
	case ModeResults:
		return a.handleResultsKeys(msg)
	case ModeHistory:
		return a.handleHistoryKeys(msg)
	case ModeArticle:
		return a.handleArticleKeys(msg)
	default:
		return a.handleListKeys(msg)
	}
}

func (a App) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return a.handleEnter()
	case tea.KeyUp:
		return a.handleUp()
	case tea.KeyDown:
		return a.handleDown()
	case tea.KeyHome:
		return a.handleHome()
	case tea.KeyEnd:
		return a.handleEnd()
	}

	switch msg.String() {
	case "/":
		return a.enterSearchMode()
	case "j":
		return a.handleDown()
	case "k":
		return a.handleUp()
	case "g":
		return a.handleHome()
	case "G":
		return a.handleEnd()
	case "r":
		if a.loadRecentItems != nil {
			a.fullLoaded = false
			a.loading = true
			return a, a.loadRecentItems()
		}
		if a.loadItems != nil {
			a.loading = true
			return a, a.loadItems()
		}
	case "f":
		if a.triggerFetch != nil {
			a.loading = true
			return a, a.triggerFetch()
		}
	case "m":
		if a.features.MLT {
			return a.handleMoreLikeThis()
		}
	case "M":
		a.mode = ModeMedia
		a.mediaView = media.NewMainModel(media.Config{
			Headlines: a.convertToHeadlines(),
		})
		a.mediaView.Width = a.width
		a.mediaView.Height = a.height
		return a, a.mediaView.Init()
	case "x":
		if a.features.ScoreColumn {
			return a, nil
		}
	case "t":
		a.alignedList = !a.alignedList
		return a, nil
	case "ctrl+r":
		if a.features.SearchHistory {
			return a.openHistory()
		}
	}

	return a, nil
}

func (a App) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return a.submitSearch()
	case tea.KeyEsc:
		if a.hasQuery() {
			return a.clearSearch()
		}
		a.filterInput.SetValue("")
		a.filterInput.Blur()
		a.mode = ModeList
		return a, nil
	case tea.KeyCtrlR:
		if a.features.SearchHistory {
			return a.openHistory()
		}
	}

	var cmd tea.Cmd
	a.filterInput, cmd = a.filterInput.Update(msg)
	return a, cmd
}

func (a App) handleResultsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return a.handleEnter()
	case tea.KeyUp:
		return a.handleUp()
	case tea.KeyDown:
		return a.handleDown()
	case tea.KeyHome:
		return a.handleHome()
	case tea.KeyEnd:
		return a.handleEnd()
	case tea.KeyEsc:
		return a.handleResultsEsc()
	}

	switch msg.String() {
	case "/":
		return a.enterSearchMode()
	case "j":
		return a.handleDown()
	case "k":
		return a.handleUp()
	case "g":
		return a.handleHome()
	case "G":
		return a.handleEnd()
	case "m":
		if a.features.MLT {
			return a.handleMoreLikeThis()
		}
	case "M":
		a.mode = ModeMedia
		a.mediaView = media.NewMainModel(media.Config{
			Headlines: a.convertToHeadlines(),
		})
		a.mediaView.Width = a.width
		a.mediaView.Height = a.height
		return a, a.mediaView.Init()
	case "R":
		if !a.autoReranks && !a.rerankPending && a.activeQuery != "" && a.rerankerAvailable() {
			return a.startReranking(a.activeQuery)
		}
	case "x":
		if a.features.ScoreColumn {
			return a, nil
		}
	case "t":
		a.alignedList = !a.alignedList
		return a, nil
	case "ctrl+r":
		if a.features.SearchHistory {
			return a.openHistory()
		}
	}

	return a, nil
}

func (a App) handleHistoryKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		a.popMode(ModeList)
		return a, nil
	}
	return a, nil
}

func (a App) handleArticleKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		a.popMode(ModeList)
		return a, nil
	}
	return a, nil
}

func (a App) handleResultsEsc() (tea.Model, tea.Cmd) {
	if a.embeddingPending || a.rerankPending || a.searchPoolPending {
		a.cancelSearch()
		// Re-sort by cosine if we have an embedding (undo partial rerank reordering)
		if len(a.queryEmbedding) > 0 {
			a.rerankItemsByEmbedding()
		}
		a.statusText = a.cosineCompleteHint()
		if a.statusText == "" {
			a.statusText = "Cancelled -- Esc again to exit"
		}
		return a, nil
	}
	return a.clearSearch()
}

// enterSearchMode activates the search input.
func (a App) enterSearchMode() (tea.Model, tea.Cmd) {
	a.mode = ModeSearch
	a.statusText = ""
	a.filterInput.SetValue("")
	a.filterInput.Focus()
	return a, a.filterInput.Cursor.BlinkCmd()
}

// ensureSnapshot saves the current chronological view if no snapshot exists.
func (a *App) ensureSnapshot() {
	if a.savedItems != nil {
		return // already snapshotted
	}
	a.savedItems = make([]store.Item, len(a.items))
	copy(a.savedItems, a.items)
	a.savedEmbeddings = make(map[string][]float32, len(a.embeddings))
	for k, v := range a.embeddings {
		a.savedEmbeddings[k] = v
	}
}

// submitSearch submits the current search query.
func (a App) submitSearch() (tea.Model, tea.Cmd) {
	query := a.filterInput.Value()
	if query == "" {
		a.mode = ModeList
		a.filterInput.Blur()
		return a, nil
	}

	a.mode = ModeResults
	a.filterInput.Blur()
	a.activeQuery = query

	// Save current chronological view for restore on Esc
	a.ensureSnapshot()
	a.mltSeedID = ""
	a.mltSeedTitle = ""

	ctx := a.newSearchContext()
	a.searchStart = time.Now()
	a.queryID = newQueryID()

	a.logger.Emit(otel.Event{
		Kind:    otel.KindSearchStart,
		Level:   otel.LevelInfo,
		Comp:    "ui",
		QueryID: a.queryID,
		Query:   query,
	})

	// === FTS5: instant lexical results ===
	// Always clear items on search submit — never show stale feed as "results".
	a.items = nil
	a.cursor = 0
	if a.features.FTS5 && a.searchFTS != nil {
		ftsItems, err := a.searchFTS(query, 50)
		if err != nil {
			a.logger.Emit(otel.Event{
				Kind:    otel.KindSearchFTS,
				Level:   otel.LevelWarn,
				Comp:    "ui",
				QueryID: a.queryID,
				Msg:     fmt.Sprintf("FTS error: %v", err),
			})
			// FTS failure is non-fatal — fall through to embedding search
		} else if len(ftsItems) > 0 {
			a.items = ftsItems
			a.logger.Emit(otel.Event{
				Kind:    otel.KindSearchFTS,
				Level:   otel.LevelInfo,
				Comp:    "ui",
				QueryID: a.queryID,
				Msg:     fmt.Sprintf("FTS returned %d results", len(ftsItems)),
			})
		}
	}

	// Load full search pool + embed query in parallel
	var cmds []tea.Cmd
	if a.loadSearchPool != nil {
		a.searchPoolPending = true
		cmds = append(cmds, a.loadSearchPool(ctx, a.queryID))
	}
	if a.embedQuery != nil {
		a.embeddingPending = true
		cmds = append(cmds, a.embedQuery(ctx, query, a.queryID))
	}
	if len(cmds) > 0 {
		a.statusText = a.searchStage()
		cmds = append(cmds, a.spinner.Tick)
		return a, tea.Batch(cmds...)
	}

	// No search pool, no embedder — FTS-only mode.
	if a.embedQuery == nil {
		a.statusText = "FTS only -- set JINA_API_KEY for semantic search"
	}

	// No search pool or embedding available; try cross-encoder reranking directly
	if a.scoreEntry != nil {
		return a.startReranking(query)
	}

	return a, nil
}

// cancelSearch cancels any in-flight search work.
// Safe to call multiple times or when searchCancel is nil.
func (a *App) cancelSearch() {
	if a.searchCancel != nil {
		a.searchCancel()
	}
	a.searchCancel = nil
	a.searchCtx = context.Background()

	// Reset pipeline flags to avoid stuck spinners
	a.embeddingPending = false
	a.rerankPending = false
	a.searchPoolPending = false
	a.poolItems = nil
	a.poolEmbeddings = nil
	a.rerankEntries = nil
	a.rerankScores = nil
	a.rerankProgress = 0
}

// newSearchContext creates a fresh context for a new search.
// Cancels any existing search first (but preserves queryID).
func (a *App) newSearchContext() context.Context {
	a.cancelSearch()
	a.searchCtx, a.searchCancel = context.WithCancel(context.Background())
	return a.searchCtx
}

func (a App) rerankerAvailable() bool {
	return a.batchRerank != nil || a.scoreEntry != nil
}

// searchStage returns a human-readable string for the current search pipeline stage.
func (a App) searchStage() string {
	query := a.activeQuery
	if a.mltSeedID != "" {
		query = fmt.Sprintf("similar to %q", truncateRunes(a.mltSeedTitle, 20))
	} else {
		query = fmt.Sprintf("%q", truncateRunes(query, 20))
	}

	if a.rerankPending {
		if a.rerankEntries != nil {
			return fmt.Sprintf("Reranking %s (%d/%d)...", query, a.rerankProgress, len(a.rerankEntries))
		}
		return fmt.Sprintf("Reranking %s...", query)
	}
	if a.searchPoolPending || a.embeddingPending {
		return fmt.Sprintf("Searching %s...", query)
	}
	return ""
}

// cosineCompleteHint returns the status text to show after cosine ranking completes.
// Shows "press R to rerank" when manual reranking is available, otherwise empty.
func (a App) cosineCompleteHint() string {
	if !a.autoReranks && a.rerankerAvailable() {
		return "Cosine results -- press R to rerank"
	}
	return ""
}

func (a App) handleMoreLikeThis() (tea.Model, tea.Cmd) {
	if len(a.items) == 0 || a.cursor >= len(a.items) {
		return a, nil
	}

	seed := a.items[a.cursor]
	seedEmb, ok := a.embeddings[seed.ID]
	if !ok || len(seedEmb) == 0 {
		a.logger.Emit(otel.Event{
			Kind:  otel.KindSearchCancel,
			Level: otel.LevelWarn,
			Comp:  "ui",
			Extra: map[string]any{"seed": seed.ID, "reason": "no_embedding"},
		})
		return a, nil
	}

	ctx := a.newSearchContext()
	a.mode = ModeResults
	a.ensureSnapshot()

	a.mltSeedID = seed.ID
	a.mltSeedTitle = seed.Title
	a.activeQuery = entryText(seed)
	a.filterInput.SetValue("")
	a.filterInput.Blur()
	a.queryEmbedding = seedEmb
	a.embeddingPending = false
	a.searchStart = time.Now()
	a.queryID = newQueryID()

	a.logger.Emit(otel.Event{
		Kind:    otel.KindSearchStart,
		Level:   otel.LevelInfo,
		Comp:    "ui",
		QueryID: a.queryID,
		Extra:   map[string]any{"seed": seed.ID, "title": truncateRunes(seed.Title, 60)},
	})

	var cmds []tea.Cmd
	if a.loadSearchPool != nil {
		a.searchPoolPending = true
		cmds = append(cmds, a.loadSearchPool(ctx, a.queryID))
	}

	a.rerankItemsByEmbedding()
	a.excludeItem(seed.ID)

	a.statusText = a.searchStage()
	cmds = append(cmds, a.spinner.Tick)

	if !a.searchPoolPending {
		return a.startMLTReranking(seed)
	}

	return a, tea.Batch(cmds...)
}

func (a *App) startMLTReranking(seed store.Item) (tea.Model, tea.Cmd) {
	return a.startReranking(entryText(seed))
}

func (a *App) excludeItem(id string) {
	for i, item := range a.items {
		if item.ID == id {
			a.items = append(a.items[:i], a.items[i+1:]...)
			if a.cursor >= len(a.items) && len(a.items) > 0 {
				a.cursor = len(a.items) - 1
			} else {
				a.cursor = 0
			}
			return
		}
	}
}

func (a App) openHistory() (tea.Model, tea.Cmd) {
	a.pushMode(ModeHistory)
	return a, nil
}

// clearSearch clears the search state and restores original order.
func (a App) clearSearch() (tea.Model, tea.Cmd) {
	a.logger.Emit(otel.Event{Kind: otel.KindSearchCancel, Level: otel.LevelInfo, Comp: "ui", QueryID: a.queryID})

	a.cancelSearch()
	a.mode = ModeList
	a.modeStack = nil
	a.filterInput.SetValue("")
	a.filterInput.Blur()
	a.queryEmbedding = nil
	a.lastEmbeddedQuery = ""
	a.activeQuery = ""
	a.mltSeedID = ""
	a.mltSeedTitle = ""
	a.rerankQuery = ""
	a.rerankEntries = nil
	a.rerankScores = nil
	a.rerankProgress = 0
	a.statusText = ""
	a.searchStart = time.Time{}
	a.queryID = ""

	// Restore chronological view
	if a.savedItems != nil {
		a.items = a.savedItems
		a.embeddings = a.savedEmbeddings
		a.savedItems = nil
		a.savedEmbeddings = nil
	} else {
		a.sortByFetchTime()
	}
	return a, nil
}

// startReranking begins parallel cross-encoder scoring of top entries.
func (a App) startReranking(query string) (tea.Model, tea.Cmd) {
	if len(a.items) == 0 {
		a.statusText = ""
		return a, nil
	}

	// Need either batch or per-entry scoring
	if a.batchRerank == nil && a.scoreEntry == nil {
		a.statusText = ""
		return a, nil
	}

	topN := a.height + 10 // Visible stories + hedge
	if topN < 30 {
		topN = 30 // Quality baseline
	}
	if len(a.items) < topN {
		topN = len(a.items)
	}

	a.rerankPending = true
	a.rerankQuery = query
	a.statusText = a.searchStage()
	a.logger.Emit(otel.Event{Kind: otel.KindCrossEncoder, Level: otel.LevelInfo, Comp: "ui", Count: topN, Query: query, Extra: map[string]any{"batch": a.batchRerank != nil}})
	a.rerankEntries = make([]store.Item, topN)
	copy(a.rerankEntries, a.items[:topN])
	a.rerankScores = make([]float32, topN)
	a.rerankProgress = 0

	// Batch path: single API call (Jina)
	if a.batchRerank != nil {
		docs := make([]string, topN)
		for i := 0; i < topN; i++ {
			docs[i] = entryText(a.rerankEntries[i])
		}
		return a, tea.Batch(a.spinner.Tick, a.batchRerank(a.searchCtx, query, docs, a.queryID))
	}

	// Per-entry path: fire ALL entries in parallel (Ollama)
	cmds := make([]tea.Cmd, topN+1)
	cmds[0] = a.spinner.Tick
	for i := 0; i < topN; i++ {
		doc := entryText(a.rerankEntries[i])
		cmds[i+1] = a.scoreEntry(a.searchCtx, query, doc, a.rerankEntries[i].ID, a.queryID)
	}
	return a, tea.Batch(cmds...)
}

// handleEntryReranked processes a single entry's rerank score.
func (a App) handleEntryReranked(msg EntryReranked) (tea.Model, tea.Cmd) {
	if !a.rerankPending {
		return a, nil
	}

	// Stale check: QueryID takes precedence if available
	if msg.QueryID != "" && msg.QueryID != a.queryID {
		return a, nil
	}

	// Stale check: ignore results from queries that don't match current rerank
	if a.rerankQuery != a.activeQuery {
		return a, nil
	}

	// Store score by ID lookup
	if msg.Err == nil {
		if idx := a.rerankIndexForID(msg.ItemID); idx >= 0 && idx < len(a.rerankScores) {
			a.rerankScores[idx] = msg.Score
		}
	}
	a.rerankProgress++
	a.statusText = a.searchStage()

	// All done?
	if a.rerankProgress >= len(a.rerankEntries) {
		a.rerankPending = false
		a.statusText = ""
		a.logger.Emit(otel.Event{Kind: otel.KindSearchComplete, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Count: len(a.rerankEntries)})
		a.applyScoresAsOrder()
		a.rerankEntries = nil
		a.rerankScores = nil
		a.rerankProgress = 0
		return a, nil
	}

	// No chaining — all entries fired in parallel, just wait for more results
	return a, nil
}

// applyScoresAsOrder sorts reranked entries by score and applies the order.
func (a *App) applyScoresAsOrder() {
	if len(a.rerankEntries) == 0 {
		return
	}

	// Build index-score pairs and sort by score descending
	type scored struct {
		index int
		score float32
	}
	pairs := make([]scored, len(a.rerankEntries))
	for i := range pairs {
		pairs[i] = scored{index: i, score: a.rerankScores[i]}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	// Extract ordered IDs
	order := make([]string, len(pairs))
	for i, p := range pairs {
		order[i] = a.rerankEntries[p.index].ID
	}

	a.applyRerankOrder(order)
}

func (a *App) rerankIndexForID(id string) int {
	for i, item := range a.rerankEntries {
		if item.ID == id {
			return i
		}
	}
	return -1
}

// handleEnter processes the Enter key.
func (a App) handleEnter() (tea.Model, tea.Cmd) {
	if len(a.items) > 0 && a.cursor < len(a.items) {
		item := a.items[a.cursor]
		if a.markRead != nil {
			return a, a.markRead(item.ID)
		}
	}
	return a, nil
}

// handleUp moves cursor up.
func (a App) handleUp() (tea.Model, tea.Cmd) {
	if a.cursor > 0 {
		a.cursor--
	}
	return a, nil
}

// handleDown moves cursor down.
func (a App) handleDown() (tea.Model, tea.Cmd) {
	if a.cursor < len(a.items)-1 {
		a.cursor++
	}
	return a, nil
}

// handleHome moves cursor to start.
func (a App) handleHome() (tea.Model, tea.Cmd) {
	a.cursor = 0
	return a, nil
}

// handleEnd moves cursor to end.
func (a App) handleEnd() (tea.Model, tea.Cmd) {
	if len(a.items) > 0 {
		a.cursor = len(a.items) - 1
	}
	return a, nil
}

// rerankItemsByEmbedding reranks items in place by cosine similarity to the query embedding.
func (a *App) rerankItemsByEmbedding() {
	if len(a.queryEmbedding) == 0 || len(a.items) == 0 {
		return
	}
	a.items = filter.RerankByQuery(a.items, a.embeddings, a.queryEmbedding)
	a.cursor = 0
}

// applyRerankOrder applies the cross-encoder reranking order to items.
func (a *App) applyRerankOrder(order []string) {
	if len(order) == 0 || len(a.items) == 0 {
		return
	}

	itemMap := make(map[string]store.Item, len(a.items))
	for _, item := range a.items {
		itemMap[item.ID] = item
	}

	rerankedSet := make(map[string]bool, len(order))
	for _, id := range order {
		rerankedSet[id] = true
	}

	result := make([]store.Item, 0, len(a.items))
	for _, id := range order {
		if item, ok := itemMap[id]; ok {
			result = append(result, item)
		}
	}
	for _, item := range a.items {
		if !rerankedSet[item.ID] {
			result = append(result, item)
		}
	}

	a.items = result
	a.cursor = 0
}

// sortByFetchTime restores items to their original order.
func (a *App) sortByFetchTime() {
	if len(a.items) == 0 {
		return
	}
	sort.SliceStable(a.items, func(i, j int) bool {
		return a.items[i].Published.After(a.items[j].Published)
	})
}

// restoreCursor finds the item with targetID and sets the cursor to its index.
// Falls back to clamping the cursor to the last item if not found.
func (a *App) restoreCursor(targetID string) {
	if targetID != "" {
		for i, item := range a.items {
			if item.ID == targetID {
				a.cursor = i
				return
			}
		}
	}
	// Clamp cursor to valid range
	if a.cursor >= len(a.items) && len(a.items) > 0 {
		a.cursor = len(a.items) - 1
	}
}

// View renders the UI.
func (a App) View() string {
	if !a.ready {
		return "Loading..."
	}

	if a.mode == ModeMedia {
		return a.mediaView.View()
	}

	// Debug overlay: full takeover
	if a.debugVisible && a.ring != nil {
		contentHeight := a.height - 2 // -1 status bar, -1 newline separator
		overlay := debugOverlay(a.ring, a.width, contentHeight)
		statusBar := debugStatusBar(a.width)
		return overlay + "\n" + statusBar
	}

	contentHeight := a.height - 1
	if a.err != nil {
		contentHeight--
	}
	if a.mode == ModeSearch || (a.hasQuery() && a.statusText == "") {
		contentHeight--
	}

	stream := RenderStream(a.items, a.cursor, a.width, contentHeight, !a.hasQuery(), a.alignedList, a.shimmerOffset)

	errorBar := ""
	if a.err != nil {
		errorBar = ErrorStyle.Width(a.width).Render("Error: " + a.err.Error() + " (press any key to dismiss)")
	}

	// Search input bar or results bar (suppress filter bar during active status)
	searchBar := ""
	if a.mode == ModeSearch {
		searchBar = a.renderSearchInput()
	} else if a.mltSeedID != "" && a.statusText == "" {
		searchBar = RenderFilterBarWithStatus(fmt.Sprintf("Similar to: %s", truncateRunes(a.mltSeedTitle, 40)), len(a.items), len(a.items), a.width, "")
	} else if a.hasQuery() && a.statusText == "" {
		searchBar = RenderFilterBarWithStatus(a.activeQuery, len(a.items), len(a.items), a.width, "")
	}

	// Status bar
	var statusBar string
	if a.hasQuery() {
		statusBar = RenderStatusBarWithFilter(
			a.cursor, len(a.items), len(a.items), a.width, a.loading,
			a.statusText, a.searchPoolPending, a.embeddingPending, a.rerankPending,
		)
	} else if a.statusText != "" {
		elapsed := ""
		if !a.searchStart.IsZero() {
			elapsed = fmt.Sprintf(" (%ds)", int(time.Since(a.searchStart).Seconds()))
		}
		status := fmt.Sprintf("  %s %s%s", a.spinner.View(), a.statusText, elapsed)
		statusBar = StatusBar.Width(a.width).Render(status)
	} else {
		statusBar = RenderStatusBar(a.cursor, len(a.items), a.width, a.loading)
	}

	return stream + errorBar + searchBar + statusBar
}

// renderSearchInput renders the search input bar.
func (a App) renderSearchInput() string {
	prompt := FilterBarPrompt.Render("/")
	text := a.filterInput.View()
	content := prompt + text
	contentWidth := lipgloss.Width(content)
	padding := a.width - contentWidth - 2
	if padding < 0 {
		padding = 0
	}
	bar := content + strings.Repeat(" ", padding)
	return FilterBar.Width(a.width).Render(bar)
}

// entryText returns the title and summary of an item for scoring.
func entryText(item store.Item) string {
	if item.Summary != "" {
		return item.Title + " - " + item.Summary
	}
	return item.Title
}

// truncateRunes truncates a string to maxRunes, adding "..." if truncated.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes < 1 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

// Cursor returns the current cursor position (for testing).
func (a App) Cursor() int {
	return a.cursor
}

// Items returns the current items (for testing).
func (a App) Items() []store.Item {
	return a.items
}

// traceMsg logs a trace event for the incoming message.
// Only called when OBSERVER_TRACE is set.
// Uses exhaustive type switch with %T for unknown types (never %+v).
func (a App) traceMsg(msg tea.Msg) {
	e := otel.Event{
		Kind:  otel.KindMsgReceived,
		Level: otel.LevelDebug,
		Comp:  "trace",
	}

	var typeName string
	switch m := msg.(type) {
	case tea.KeyMsg:
		typeName = "KeyMsg"
		e.Extra = map[string]any{"key": m.String()}
	case tea.WindowSizeMsg:
		typeName = "WindowSizeMsg"
		e.Extra = map[string]any{"w": m.Width, "h": m.Height}
	case spinner.TickMsg:
		typeName = "spinner.TickMsg"
	case ItemsLoaded:
		typeName = "ItemsLoaded"
		e.Count = len(m.Items)
		if m.Err != nil {
			e.Err = m.Err.Error()
		}
	case FetchComplete:
		typeName = "FetchComplete"
		e.Source = m.Source
		e.Count = m.NewItems
		if m.Err != nil {
			e.Err = m.Err.Error()
		}
	case QueryEmbedded:
		typeName = "QueryEmbedded"
		e.Query = m.Query
		e.QueryID = m.QueryID
		e.Dims = len(m.Embedding)
	case SearchPoolLoaded:
		typeName = "SearchPoolLoaded"
		e.QueryID = m.QueryID
		e.Count = len(m.Items)
	case RerankComplete:
		typeName = "RerankComplete"
		e.QueryID = m.QueryID
		e.Query = m.Query
	case EntryReranked:
		typeName = "EntryReranked"
		e.QueryID = m.QueryID
		e.Extra = map[string]any{"item_id": m.ItemID, "score": m.Score}
	case ItemMarkedRead:
		typeName = "ItemMarkedRead"
		e.Source = m.ID
	case RefreshTick:
		typeName = "RefreshTick"
	default:
		// %T gives the type name without allocating the full value string.
		// NEVER use %+v here — it triggers full reflection and can allocate
		// megabytes on large message types (e.g., SearchPoolLoaded with 2000 items).
		typeName = fmt.Sprintf("%T", msg)
	}

	e.Msg = typeName
	a.logger.Emit(e)
}

// convertToHeadlines transforms standard Items into Media Headlines.
func (a App) convertToHeadlines() []media.Headline {
	headlines := make([]media.Headline, len(a.items))
	for i, item := range a.items {
		// Calculate base scores if available, else defaults
		sem := 0.5
		if emb, ok := a.embeddings[item.ID]; ok && len(a.queryEmbedding) > 0 {
			sem = float64(filter.CosineSimilarity(a.queryEmbedding, emb))
		}

		headlines[i] = media.Headline{
			ID:        item.ID,
			Title:     item.Title,
			Source:    item.SourceName,
			Published: item.Published,
			Breakdown: media.Breakdown{
				Semantic:  sem,
				Rerank:    0.5, // placeholder
				Arousal:   0.5, // placeholder
				Diversity: 0.5,
				NegBoost:  0.5,
			},
		}
		headlines[i].EnsureHash()
	}
	return headlines
}

// traceHandled logs a trace event recording how long Update() took.
// Only called via defer when OBSERVER_TRACE is set.
func (a App) traceHandled(startTime time.Time) {
	a.logger.Emit(otel.Event{
		Kind:  otel.KindMsgHandled,
		Level: otel.LevelDebug,
		Comp:  "trace",
		Dur:   time.Since(startTime),
	})
}
