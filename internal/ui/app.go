package ui

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/abelbrown/observer/internal/filter"
	"github.com/abelbrown/observer/internal/otel"
	"github.com/abelbrown/observer/internal/store"
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

// App is the root Bubble Tea model.
// IMPORTANT: App does NOT hold *store.Store. It receives items via messages.
type App struct {
	loadItems       func() tea.Cmd
	loadRecentItems func() tea.Cmd // loads last 1h (fast first paint)
	loadSearchPool  func(queryID string) tea.Cmd // loads all items for search
	markRead        func(id string) tea.Cmd
	triggerFetch    func() tea.Cmd
	embedQuery      func(query string, queryID string) tea.Cmd
	scoreEntry  func(query string, doc string, index int, queryID string) tea.Cmd // Ollama per-entry path (not wired in production; Jina batch path used instead)
	batchRerank func(query string, docs []string, queryID string) tea.Cmd         // Jina batch rerank — single API call for all docs

	items      []store.Item
	embeddings map[string][]float32 // item ID -> embedding
	cursor     int
	err        error
	width      int
	height     int
	ready      bool
	loading    bool
	statusText  string    // activity status for status bar; empty = no activity
	searchStart time.Time // when current search was initiated

	// Two-stage loading
	fullLoaded bool // true after Stage 2 completes

	// Search mode: press "/" to activate, type query, Enter to submit
	searchActive bool // true when typing in search input
	filterInput  textinput.Model

	// Full-history search: save/restore chronological view
	savedItems      []store.Item         // chronological items saved before search
	savedEmbeddings map[string][]float32 // embeddings saved before search

	// Search pool loading
	searchPoolPending bool // true while loading search pool from DB

	// Query state
	queryEmbedding    []float32 // current query's embedding
	embeddingPending  bool      // true while waiting for query embedding
	lastEmbeddedQuery string    // the query that was last embedded

	// Search correlation
	queryID string // current search correlation ID; empty when no search active

	// Rerank progress (package-manager style)
	rerankPending  bool         // true during reranking
	rerankEntries  []store.Item // entries being reranked
	rerankScores   []float32    // scores per entry
	rerankProgress int          // entries scored so far
	rerankQuery    string       // the query that started the current rerank

	// UI components
	spinner  spinner.Model

	// Observability
	logger       *otel.Logger
	ring         *otel.RingBuffer
	debugVisible bool
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
	LoadSearchPool  func(queryID string) tea.Cmd
	MarkRead        func(id string) tea.Cmd
	TriggerFetch    func() tea.Cmd
	EmbedQuery      func(query string, queryID string) tea.Cmd
	ScoreEntry      func(query string, doc string, index int, queryID string) tea.Cmd
	BatchRerank     func(query string, docs []string, queryID string) tea.Cmd
	Embeddings      map[string][]float32
	Obs             ObsConfig
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
		cursor:          0,
		filterInput:     ti,
		embeddings:      embeddings,
		spinner:         s,
		logger:          logger,
		ring:            cfg.Obs.Ring,
	}
}

// Init initializes the App by loading items.
// Uses loadRecentItems (Stage 1) for fast first paint if available,
// otherwise falls back to loadItems (full load).
func (a App) Init() tea.Cmd {
	if a.loadRecentItems != nil {
		return a.loadRecentItems()
	}
	if a.loadItems != nil {
		return a.loadItems()
	}
	return nil
}

// Update handles messages and returns the updated model and any commands.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if otel.TraceEnabled() {
		a.traceMsg(msg)
		defer a.traceHandled(time.Now())
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		return a, nil

	case spinner.TickMsg:
		if a.statusText != "" {
			var cmd tea.Cmd
			a.spinner, cmd = a.spinner.Update(msg)
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
		// Stale check first: don't clear state for old queries
		if msg.QueryID != "" && msg.QueryID != a.queryID {
			return a, nil
		}
		a.embeddingPending = false
		if msg.Err != nil {
			a.statusText = ""
			return a, nil
		}
		if msg.Query == a.filterInput.Value() {
			a.queryEmbedding = msg.Embedding
			a.logger.Emit(otel.Event{Kind: otel.KindQueryEmbed, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Dims: len(msg.Embedding), Query: msg.Query})
			a.lastEmbeddedQuery = msg.Query
			// Always apply fast cosine reranking for immediate feedback
			a.rerankItemsByEmbedding()
			a.logger.Emit(otel.Event{Kind: otel.KindCosineRerank, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Count: len(a.items), Query: a.filterInput.Value()})
			// Only start cross-encoder if search pool has already arrived
			if !a.searchPoolPending {
				return a.startReranking(msg.Query)
			}
		}
		return a, nil

	case SearchPoolLoaded:
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
			a.statusText = ""
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
		a.items = msg.Items
		a.embeddings = msg.Embeddings
		a.logger.Emit(otel.Event{Kind: otel.KindSearchPool, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Count: len(msg.Items), Query: a.filterInput.Value(), Extra: map[string]any{"embeddings": len(msg.Embeddings)}})
		// If query embedding already arrived, apply full reranking now
		if len(a.queryEmbedding) > 0 {
			a.rerankItemsByEmbedding()
			return a.startReranking(a.filterInput.Value())
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
		} else if msg.Query != a.filterInput.Value() {
			// Fallback: text comparison when no QueryID
			return a, nil
		}
		a.rerankPending = false
		a.statusText = ""
		a.logger.Emit(otel.Event{Kind: otel.KindSearchComplete, Level: otel.LevelInfo, Comp: "ui", Dur: time.Since(a.searchStart), Query: a.filterInput.Value()})
		if msg.Err != nil {
			a.err = msg.Err
			a.rerankEntries = nil
			a.rerankScores = nil
			a.rerankProgress = 0
			return a, nil
		}
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
	return a.filterInput.Value() != "" && !a.searchActive
}

// handleKeyMsg processes keyboard input.
func (a App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Clear any existing error on key press
	if a.err != nil {
		a.err = nil
	}

	// In search input mode, route everything to search handler
	if a.searchActive {
		return a.handleSearchInput(msg)
	}

	// During embedding, only allow Esc to cancel and D to toggle debug
	if a.embeddingPending {
		switch msg.Type {
		case tea.KeyEsc:
			return a.clearSearch()
		case tea.KeyCtrlC:
			return a, tea.Quit
		}
		if msg.String() == "D" {
			a.debugVisible = !a.debugVisible
			return a, nil
		}
		return a, nil
	}

	// During reranking, allow navigation + Esc to cancel
	if a.rerankPending {
		switch msg.Type {
		case tea.KeyEsc:
			return a.clearSearch()
		case tea.KeyCtrlC:
			return a, tea.Quit
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
		case "j":
			return a.handleDown()
		case "k":
			return a.handleUp()
		case "g":
			return a.handleHome()
		case "G":
			return a.handleEnd()
		case "D":
			a.debugVisible = !a.debugVisible
			return a, nil
		}
		return a, nil
	}

	// Normal mode key handling
	switch msg.Type {
	case tea.KeyCtrlC:
		return a, tea.Quit
	case tea.KeyEsc:
		if a.hasQuery() {
			return a.clearSearch()
		}
		return a, nil
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

	// String-based keys
	switch msg.String() {
	case "/":
		return a.enterSearchMode()
	case "j":
		return a.handleDown()
	case "k":
		return a.handleUp()
	case "q":
		return a, tea.Quit
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
	case "g":
		return a.handleHome()
	case "G":
		return a.handleEnd()
	case "D":
		a.debugVisible = !a.debugVisible
		return a, nil
	}

	return a, nil
}

// handleSearchInput routes keys when in search input mode.
func (a App) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return a.submitSearch()
	case tea.KeyEsc:
		a.searchActive = false
		a.filterInput.Blur()
		// If no query was submitted, this is just canceling the input
		if a.filterInput.Value() == "" {
			return a, nil
		}
		// Keep previous results showing
		return a, nil
	case tea.KeyCtrlC:
		return a, tea.Quit
	}

	// Forward all other keys to text input
	var cmd tea.Cmd
	a.filterInput, cmd = a.filterInput.Update(msg)
	return a, cmd
}

// enterSearchMode activates the search input.
func (a App) enterSearchMode() (tea.Model, tea.Cmd) {
	a.searchActive = true
	a.filterInput.SetValue("")
	a.filterInput.Focus()
	return a, a.filterInput.Cursor.BlinkCmd()
}

// submitSearch submits the current search query.
func (a App) submitSearch() (tea.Model, tea.Cmd) {
	query := a.filterInput.Value()
	if query == "" {
		a.searchActive = false
		a.filterInput.Blur()
		return a, nil
	}

	a.searchActive = false
	a.filterInput.Blur()

	// Save current chronological view for restore on Esc
	a.savedItems = make([]store.Item, len(a.items))
	copy(a.savedItems, a.items)
	a.savedEmbeddings = make(map[string][]float32, len(a.embeddings))
	for k, v := range a.embeddings {
		a.savedEmbeddings[k] = v
	}

	a.searchStart = time.Now()
	a.queryID = newQueryID()

	a.logger.Emit(otel.Event{
		Kind:    otel.KindSearchStart,
		Level:   otel.LevelInfo,
		Comp:    "ui",
		QueryID: a.queryID,
		Query:   query,
	})

	// Load full search pool + embed query in parallel
	var cmds []tea.Cmd
	if a.loadSearchPool != nil {
		a.searchPoolPending = true
		cmds = append(cmds, a.loadSearchPool(a.queryID))
	}
	if a.embedQuery != nil {
		a.embeddingPending = true
		cmds = append(cmds, a.embedQuery(query, a.queryID))
	}
	if len(cmds) > 0 {
		a.statusText = fmt.Sprintf("Searching for \"%s\"...", truncateRunes(query, 30))
		cmds = append(cmds, a.spinner.Tick)
		return a, tea.Batch(cmds...)
	}

	// No search pool or embedding available; try cross-encoder reranking directly
	if a.scoreEntry != nil {
		return a.startReranking(query)
	}

	return a, nil
}

// clearSearch clears the search state and restores original order.
func (a App) clearSearch() (tea.Model, tea.Cmd) {
	a.logger.Emit(otel.Event{Kind: otel.KindSearchCancel, Level: otel.LevelInfo, Comp: "ui", QueryID: a.queryID})

	a.searchActive = false
	a.filterInput.SetValue("")
	a.filterInput.Blur()
	a.queryEmbedding = nil
	a.lastEmbeddedQuery = ""
	a.embeddingPending = false
	a.searchPoolPending = false
	a.rerankPending = false
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

	topN := 30
	if len(a.items) < topN {
		topN = len(a.items)
	}

	a.rerankPending = true
	a.rerankQuery = query
	a.statusText = fmt.Sprintf("Reranking \"%s\"...", truncateRunes(query, 30))
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
		return a, tea.Batch(a.spinner.Tick, a.batchRerank(query, docs, a.queryID))
	}

	// Per-entry path: fire ALL entries in parallel (Ollama)
	cmds := make([]tea.Cmd, topN+1)
	cmds[0] = a.spinner.Tick
	for i := 0; i < topN; i++ {
		doc := entryText(a.rerankEntries[i])
		cmds[i+1] = a.scoreEntry(query, doc, i, a.queryID)
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
	if a.rerankQuery != a.filterInput.Value() {
		return a, nil
	}

	// Store score
	if msg.Err == nil && msg.Index < len(a.rerankScores) {
		a.rerankScores[msg.Index] = msg.Score
	}
	a.rerankProgress++

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
	if a.searchActive || (a.hasQuery() && a.statusText == "") {
		contentHeight--
	}

	stream := RenderStream(a.items, a.cursor, a.width, contentHeight, !a.hasQuery())

	errorBar := ""
	if a.err != nil {
		errorBar = ErrorStyle.Width(a.width).Render("Error: " + a.err.Error() + " (press any key to dismiss)")
	}

	// Search input bar or results bar (suppress filter bar during active status)
	searchBar := ""
	if a.searchActive {
		searchBar = a.renderSearchInput()
	} else if a.hasQuery() && a.statusText == "" {
		searchBar = RenderFilterBarWithStatus(a.filterInput.Value(), len(a.items), len(a.items), a.width, "")
	}

	// Status bar
	var statusBar string
	if a.statusText != "" {
		status := fmt.Sprintf("  %s %s", a.spinner.View(), a.statusText)
		statusBar = StatusBar.Width(a.width).Render(status)
	} else if a.hasQuery() {
		statusBar = RenderStatusBarWithFilter(a.cursor, len(a.items), len(a.items), a.width, a.loading)
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
		e.Extra = map[string]any{"index": m.Index, "score": m.Score}
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
