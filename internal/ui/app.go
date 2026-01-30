package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/abelbrown/observer/internal/filter"
	"github.com/abelbrown/observer/internal/store"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// App is the root Bubble Tea model.
// IMPORTANT: App does NOT hold *store.Store. It receives items via messages.
type App struct {
	loadItems       func() tea.Cmd
	loadRecentItems func() tea.Cmd // loads last 1h (fast first paint)
	loadSearchPool  func() tea.Cmd // loads all items for search
	markRead        func(id string) tea.Cmd
	triggerFetch    func() tea.Cmd
	embedQuery      func(query string) tea.Cmd
	scoreEntry      func(query string, doc string, index int) tea.Cmd // score single entry
	batchRerank     func(query string, docs []string) tea.Cmd         // batch rerank all docs at once

	items      []store.Item
	embeddings map[string][]float32 // item ID -> embedding
	cursor     int
	err        error
	width      int
	height     int
	ready      bool
	loading    bool

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

	// Rerank progress (package-manager style)
	rerankPending  bool         // true during reranking
	rerankEntries  []store.Item // entries being reranked
	rerankScores   []float32    // scores per entry
	rerankProgress int          // entries scored so far
	rerankStart    time.Time    // when reranking started
	rerankQuery    string       // the query that started the current rerank

	// UI components
	spinner  spinner.Model
	progress progress.Model
}

// AppConfig holds the configuration for creating a new App.
type AppConfig struct {
	LoadItems       func() tea.Cmd
	LoadRecentItems func() tea.Cmd
	LoadSearchPool  func() tea.Cmd
	MarkRead        func(id string) tea.Cmd
	TriggerFetch    func() tea.Cmd
	EmbedQuery      func(query string) tea.Cmd
	ScoreEntry      func(query string, doc string, index int) tea.Cmd
	BatchRerank     func(query string, docs []string) tea.Cmd
	Embeddings      map[string][]float32
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
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	p := progress.New(progress.WithDefaultGradient())

	embeddings := cfg.Embeddings
	if embeddings == nil {
		embeddings = make(map[string][]float32)
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
		progress:        p,
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.progress.Width = msg.Width - 6 // padding for progress bar
		if a.progress.Width < 20 {
			a.progress.Width = 20
		}
		a.ready = true
		return a, nil

	case spinner.TickMsg:
		if a.embeddingPending || a.rerankPending {
			var cmd tea.Cmd
			a.spinner, cmd = a.spinner.Update(msg)
			return a, cmd
		}
		return a, nil

	case progress.FrameMsg:
		progressModel, cmd := a.progress.Update(msg)
		a.progress = progressModel.(progress.Model)
		return a, cmd

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
		a.embeddingPending = false
		if msg.Err != nil {
			return a, nil
		}
		if msg.Query == a.filterInput.Value() {
			a.queryEmbedding = msg.Embedding
			a.lastEmbeddedQuery = msg.Query
			// Always apply fast cosine reranking for immediate feedback
			a.rerankItemsByEmbedding()
			// Only start cross-encoder if search pool has already arrived
			if !a.searchPoolPending {
				return a.startReranking(msg.Query)
			}
		}
		return a, nil

	case SearchPoolLoaded:
		a.searchPoolPending = false
		// Discard if no active query (user pressed Esc before pool arrived)
		if !a.hasQuery() {
			return a, nil
		}
		if msg.Err != nil {
			a.err = msg.Err
			return a, nil
		}
		// Cancel in-flight reranking — items are about to change
		if a.rerankPending {
			a.rerankPending = false
			a.rerankEntries = nil
			a.rerankScores = nil
			a.rerankProgress = 0
		}
		a.items = msg.Items
		a.embeddings = msg.Embeddings
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
		// Stale check: ignore results from old queries
		if msg.Query != a.filterInput.Value() {
			return a, nil
		}
		a.rerankPending = false
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

	// During embedding, only allow Esc to cancel
	if a.embeddingPending {
		switch msg.Type {
		case tea.KeyEsc:
			return a.clearSearch()
		case tea.KeyCtrlC:
			return a, tea.Quit
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

	// Load full search pool + embed query in parallel
	var cmds []tea.Cmd
	if a.loadSearchPool != nil {
		a.searchPoolPending = true
		cmds = append(cmds, a.loadSearchPool())
	}
	if a.embedQuery != nil {
		a.embeddingPending = true
		cmds = append(cmds, a.embedQuery(query))
	}
	if len(cmds) > 0 {
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
		return a, nil
	}

	// Need either batch or per-entry scoring
	if a.batchRerank == nil && a.scoreEntry == nil {
		return a, nil
	}

	topN := 30
	if len(a.items) < topN {
		topN = len(a.items)
	}

	a.rerankPending = true
	a.rerankQuery = query
	a.rerankEntries = make([]store.Item, topN)
	copy(a.rerankEntries, a.items[:topN])
	a.rerankScores = make([]float32, topN)
	a.rerankProgress = 0
	a.rerankStart = time.Now()

	// Batch path: single API call (Jina)
	if a.batchRerank != nil {
		docs := make([]string, topN)
		for i := 0; i < topN; i++ {
			docs[i] = entryText(a.rerankEntries[i])
		}
		return a, tea.Batch(a.spinner.Tick, a.batchRerank(query, docs))
	}

	// Per-entry path: fire ALL entries in parallel (Ollama)
	cmds := make([]tea.Cmd, topN+1)
	cmds[0] = a.spinner.Tick
	for i := 0; i < topN; i++ {
		doc := entryText(a.rerankEntries[i])
		cmds[i+1] = a.scoreEntry(query, doc, i)
	}
	return a, tea.Batch(cmds...)
}

// handleEntryReranked processes a single entry's rerank score.
func (a App) handleEntryReranked(msg EntryReranked) (tea.Model, tea.Cmd) {
	if !a.rerankPending {
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
		a.applyScoresAsOrder()
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

	// Show embedding spinner
	if a.embeddingPending {
		return a.viewEmbedding()
	}

	// During batch reranking (Jina): show spinner like embedding view
	if a.rerankPending && a.batchRerank != nil {
		return a.viewBatchReranking()
	}

	// During per-entry reranking (Ollama): show progress panel in bottom 25%
	if a.rerankPending && len(a.rerankEntries) > 0 {
		return a.viewWithRerankPanel()
	}

	// Normal view
	contentHeight := a.height - 1
	if a.err != nil {
		contentHeight--
	}
	if a.searchActive || a.hasQuery() {
		contentHeight--
	}

	stream := RenderStream(a.items, a.cursor, a.width, contentHeight, !a.hasQuery())

	errorBar := ""
	if a.err != nil {
		errorBar = ErrorStyle.Width(a.width).Render("Error: " + a.err.Error() + " (press any key to dismiss)")
	}

	// Search input bar or results bar
	searchBar := ""
	if a.searchActive {
		searchBar = a.renderSearchInput()
	} else if a.hasQuery() {
		searchBar = RenderFilterBarWithStatus(a.filterInput.Value(), len(a.items), len(a.items), a.width, "")
	}

	// Status bar
	var statusBar string
	if a.hasQuery() {
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

// viewEmbedding shows a spinner while embedding the query.
func (a App) viewEmbedding() string {
	query := a.filterInput.Value()

	// Show the item list behind the status (no time bands during search)
	contentHeight := a.height - 2
	stream := RenderStream(a.items, a.cursor, a.width, contentHeight, false)

	status := fmt.Sprintf("  %s Searching for \"%s\"...", a.spinner.View(), query)
	statusBar := StatusBar.Width(a.width).Render(status)

	return stream + statusBar
}

// viewBatchReranking shows a spinner while batch reranking is in progress.
func (a App) viewBatchReranking() string {
	query := a.filterInput.Value()

	contentHeight := a.height - 2
	stream := RenderStream(a.items, a.cursor, a.width, contentHeight, false)

	status := fmt.Sprintf("  %s Reranking \"%s\"...", a.spinner.View(), truncateRunes(query, 30))
	statusBar := StatusBar.Width(a.width).Render(status)

	return stream + statusBar
}

// viewWithRerankPanel renders items in the top 75% and progress panel in the bottom 25%.
func (a App) viewWithRerankPanel() string {
	// Layout: top 75% = item stream, bottom 25% = compact progress panel
	progressHeight := a.height / 4
	if progressHeight < 5 {
		progressHeight = 5
	}
	contentHeight := a.height - progressHeight

	// Render item stream in the top portion (no time bands during search)
	stream := RenderStream(a.items, a.cursor, a.width, contentHeight, false)

	// Render compact progress panel in the bottom portion
	panel := a.renderRerankPanel(progressHeight)

	return stream + panel
}

// renderRerankPanel renders a compact reranking progress panel.
func (a App) renderRerankPanel(height int) string {
	var b strings.Builder
	query := a.filterInput.Value()
	total := len(a.rerankEntries)
	done := a.rerankProgress
	pct := float64(done) / float64(total)
	pctInt := int(pct * 100)

	// Separator line
	b.WriteString(strings.Repeat("─", a.width))
	b.WriteString("\n")

	// Header with inline counter
	elapsed := time.Since(a.rerankStart).Round(time.Millisecond * 100)
	header := fmt.Sprintf("  %s Reranking \"%s\"  %d/%d  %d%%  %s",
		a.spinner.View(), truncateRunes(query, 30), done, total, pctInt, elapsed)
	b.WriteString(FilterBarText.Render(header))
	b.WriteString("\n")

	// Show last few scored entries (fill remaining height minus 2 for header + progress bar)
	entryLines := height - 3 // separator + header + progress bar
	if entryLines < 1 {
		entryLines = 1
	}

	startFrom := 0
	if done > entryLines {
		startFrom = done - entryLines
	}

	for i := startFrom; i < done && i < total && (i-startFrom) < entryLines; i++ {
		title := truncateRunes(a.rerankEntries[i].Title, a.width-16)
		score := a.rerankScores[i]
		check := ProgressCheckmark.Render("✓")
		titleText := ProgressTitle.Render(title)
		scoreText := ProgressCount.Render(fmt.Sprintf(" [%.1f]", score))
		b.WriteString(fmt.Sprintf("  %s %s%s\n", check, titleText, scoreText))
	}

	// Progress bar
	b.WriteString("  ")
	b.WriteString(a.progress.ViewAs(pct))
	b.WriteString("\n")

	return b.String()
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
