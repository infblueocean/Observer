package ui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// mockCmd tracks whether a command function was called.
type mockCmd struct {
	called   bool
	calledID string
}

func (m *mockCmd) loadItems() tea.Cmd {
	m.called = true
	return func() tea.Msg {
		return ItemsLoaded{
			Items: []store.Item{
				{ID: "1", Title: "Test Item 1", Published: time.Now()},
				{ID: "2", Title: "Test Item 2", Published: time.Now()},
				{ID: "3", Title: "Test Item 3", Published: time.Now()},
			},
		}
	}
}

func (m *mockCmd) markRead(id string) tea.Cmd {
	m.called = true
	m.calledID = id
	return func() tea.Msg {
		return ItemMarkedRead{ID: id}
	}
}

func (m *mockCmd) triggerFetch() tea.Cmd {
	m.called = true
	return func() tea.Msg {
		return FetchComplete{Source: "test", NewItems: 1}
	}
}

func TestAppInit(t *testing.T) {
	mock := &mockCmd{}
	app := NewApp(mock.loadItems, mock.markRead, mock.triggerFetch)

	cmd := app.Init()

	if cmd == nil {
		t.Fatal("Init should return a command")
	}

	if !mock.called {
		t.Error("Init should call loadItems")
	}
}

func TestAppInitNilLoadItems(t *testing.T) {
	app := NewApp(nil, nil, nil)

	cmd := app.Init()

	if cmd != nil {
		t.Error("Init should return nil when loadItems is nil")
	}
}

func TestAppNavigation(t *testing.T) {
	mock := &mockCmd{}
	app := NewApp(mock.loadItems, mock.markRead, mock.triggerFetch)

	// Load some items first
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}

	// Test j (down)
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated := model.(App)
	if updated.Cursor() != 1 {
		t.Errorf("j should move cursor to 1, got %d", updated.Cursor())
	}

	// Test k (up)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	updated = model.(App)
	if updated.Cursor() != 0 {
		t.Errorf("k should move cursor to 0, got %d", updated.Cursor())
	}

	// Test k at top (should stay at 0)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	updated = model.(App)
	if updated.Cursor() != 0 {
		t.Errorf("k at top should keep cursor at 0, got %d", updated.Cursor())
	}

	// Test G (end)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	updated = model.(App)
	if updated.Cursor() != 2 {
		t.Errorf("G should move cursor to 2, got %d", updated.Cursor())
	}

	// Test g (home)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	updated = model.(App)
	if updated.Cursor() != 0 {
		t.Errorf("g should move cursor to 0, got %d", updated.Cursor())
	}

	// Test down arrow
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = model.(App)
	if updated.Cursor() != 1 {
		t.Errorf("down arrow should move cursor to 1, got %d", updated.Cursor())
	}

	// Test up arrow
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated = model.(App)
	if updated.Cursor() != 0 {
		t.Errorf("up arrow should move cursor to 0, got %d", updated.Cursor())
	}
}

func TestAppNavigationBounds(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.cursor = 1

	// Test j at bottom (should stay at 1)
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated := model.(App)
	if updated.Cursor() != 1 {
		t.Errorf("j at bottom should keep cursor at 1, got %d", updated.Cursor())
	}
}

func TestAppMarkRead(t *testing.T) {
	mock := &mockCmd{}
	app := NewApp(mock.loadItems, mock.markRead, mock.triggerFetch)

	app.items = []store.Item{
		{ID: "item-1", Title: "Item 1"},
		{ID: "item-2", Title: "Item 2"},
	}
	app.cursor = 0

	// Press Enter to mark read
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = model.(App)

	if !mock.called {
		t.Error("Enter should call markRead")
	}

	if mock.calledID != "item-1" {
		t.Errorf("markRead should be called with item ID 'item-1', got '%s'", mock.calledID)
	}

	if cmd == nil {
		t.Error("Enter should return a command")
	}
}

func TestAppMarkReadEmpty(t *testing.T) {
	mock := &mockCmd{}
	app := NewApp(mock.loadItems, mock.markRead, mock.triggerFetch)

	// No items
	app.items = []store.Item{}

	// Press Enter with no items
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if mock.called {
		t.Error("Enter should not call markRead when no items")
	}

	if cmd != nil {
		t.Error("Enter should return nil command when no items")
	}
}

func TestAppRefresh(t *testing.T) {
	mock := &mockCmd{}
	app := NewApp(mock.loadItems, mock.markRead, mock.triggerFetch)

	// Reset mock
	mock.called = false

	// Press r to refresh
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	if !mock.called {
		t.Error("r should call loadItems")
	}

	if cmd == nil {
		t.Error("r should return a command")
	}
}

func TestAppQuit(t *testing.T) {
	app := NewApp(nil, nil, nil)

	// Test q
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if cmd == nil {
		t.Fatal("q should return a command")
	}

	// Execute the command and check for quit
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Error("q should return tea.Quit")
	}
}

func TestAppQuitCtrlC(t *testing.T) {
	app := NewApp(nil, nil, nil)

	// Test ctrl+c
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if cmd == nil {
		t.Fatal("ctrl+c should return a command")
	}

	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Error("ctrl+c should return tea.Quit")
	}
}

func TestAppWindowSize(t *testing.T) {
	app := NewApp(nil, nil, nil)

	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	updated := model.(App)

	if updated.width != 100 {
		t.Errorf("width should be 100, got %d", updated.width)
	}

	if updated.height != 50 {
		t.Errorf("height should be 50, got %d", updated.height)
	}

	if !updated.ready {
		t.Error("app should be ready after WindowSizeMsg")
	}
}

func TestAppItemsLoaded(t *testing.T) {
	app := NewApp(nil, nil, nil)

	items := []store.Item{
		{ID: "1", Title: "Test 1"},
		{ID: "2", Title: "Test 2"},
	}

	model, _ := app.Update(ItemsLoaded{Items: items})
	updated := model.(App)

	if len(updated.Items()) != 2 {
		t.Errorf("should have 2 items, got %d", len(updated.Items()))
	}
}

func TestAppItemsLoadedError(t *testing.T) {
	app := NewApp(nil, nil, nil)

	model, _ := app.Update(ItemsLoaded{Err: tea.ErrProgramKilled})
	updated := model.(App)

	if updated.err == nil {
		t.Error("err should be set on error")
	}
}

func TestAppItemMarkedRead(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Test 1", Read: false},
		{ID: "2", Title: "Test 2", Read: false},
	}

	model, _ := app.Update(ItemMarkedRead{ID: "1"})
	updated := model.(App)

	if !updated.items[0].Read {
		t.Error("item 1 should be marked as read")
	}

	if updated.items[1].Read {
		t.Error("item 2 should not be marked as read")
	}
}

func TestAppView(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = true
	app.width = 80
	app.height = 24
	app.items = []store.Item{
		{ID: "1", Title: "Test Item", SourceName: "test", Published: time.Now()},
	}

	view := app.View()

	if view == "" {
		t.Error("View should not be empty")
	}

	// Verify item title appears in output
	if !strings.Contains(view, "Test Item") {
		t.Errorf("View should contain item title 'Test Item', got: %s", view)
	}

	// Verify status bar is present (shows position info like "1/1")
	if !strings.Contains(view, "1/1") {
		t.Errorf("View should contain status bar with position '1/1', got: %s", view)
	}

	// Verify status bar key hints are present
	if !strings.Contains(view, "j/k") {
		t.Errorf("View should contain key hint 'j/k', got: %s", view)
	}
}

func TestAppViewNotReady(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = false

	view := app.View()

	if view != "Loading..." {
		t.Errorf("View should show 'Loading...' when not ready, got: %s", view)
	}
}

func TestAppViewWithError(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = true
	app.width = 80
	app.height = 24
	app.err = tea.ErrProgramKilled

	view := app.View()

	if view == "" {
		t.Error("View should not be empty even with error")
	}

	// Verify the error message appears in output
	if !strings.Contains(view, "Error:") {
		t.Errorf("View should contain 'Error:' prefix, got: %s", view)
	}

	// Verify the actual error message appears
	if !strings.Contains(view, tea.ErrProgramKilled.Error()) {
		t.Errorf("View should contain error message '%s', got: %s", tea.ErrProgramKilled.Error(), view)
	}

	// Verify dismissal hint is shown
	if !strings.Contains(view, "press any key to dismiss") {
		t.Errorf("View should contain dismissal hint, got: %s", view)
	}
}

func TestAppCursorResetOnItemsLoaded(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.cursor = 10

	items := []store.Item{
		{ID: "1", Title: "Test 1"},
		{ID: "2", Title: "Test 2"},
	}

	model, _ := app.Update(ItemsLoaded{Items: items})
	updated := model.(App)

	if updated.Cursor() != 1 {
		t.Errorf("cursor should be reset to last item (1) when out of bounds, got %d", updated.Cursor())
	}
}

func TestAppViewShowsSelectedItem(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = true
	app.width = 80
	app.height = 24
	app.items = []store.Item{
		{ID: "1", Title: "First Item", SourceName: "source1", Published: time.Now()},
		{ID: "2", Title: "Second Item", SourceName: "source2", Published: time.Now()},
		{ID: "3", Title: "Third Item", SourceName: "source3", Published: time.Now()},
	}

	// Test with cursor at position 0 (first item selected)
	app.cursor = 0
	view := app.View()

	// Both items should appear
	if !strings.Contains(view, "First Item") {
		t.Errorf("View should contain 'First Item', got: %s", view)
	}
	if !strings.Contains(view, "Second Item") {
		t.Errorf("View should contain 'Second Item', got: %s", view)
	}

	// Status bar should show position 1/3 (cursor+1 / total)
	if !strings.Contains(view, "1/3") {
		t.Errorf("View should show position '1/3' when cursor is at first item, got: %s", view)
	}

	// Move cursor to second item
	app.cursor = 1
	view = app.View()

	// Status bar should now show position 2/3
	if !strings.Contains(view, "2/3") {
		t.Errorf("View should show position '2/3' when cursor is at second item, got: %s", view)
	}
}

func TestAppViewShowsSourceName(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = true
	app.width = 80
	app.height = 24
	app.items = []store.Item{
		{ID: "1", Title: "Item from HackerNews", SourceName: "hackernews", Published: time.Now()},
		{ID: "2", Title: "Item from Reddit", SourceName: "reddit", Published: time.Now()},
	}

	view := app.View()

	// Verify source names appear in the rendered output
	if !strings.Contains(view, "hackernews") {
		t.Errorf("View should contain source name 'hackernews', got: %s", view)
	}
	if !strings.Contains(view, "reddit") {
		t.Errorf("View should contain source name 'reddit', got: %s", view)
	}

	// Verify item titles also appear
	if !strings.Contains(view, "Item from HackerNews") {
		t.Errorf("View should contain title 'Item from HackerNews', got: %s", view)
	}
	if !strings.Contains(view, "Item from Reddit") {
		t.Errorf("View should contain title 'Item from Reddit', got: %s", view)
	}
}

// --- Search mode tests ---

func TestAppSlashEntersSearchMode(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Test Item"},
	}

	// Press "/" to enter search mode
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)

	if !updated.searchActive {
		t.Error("/ should activate search mode")
	}
}

func TestAppSearchInputAcceptsText(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Test Item"},
	}

	// Enter search mode
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)

	// Type "hello"
	for _, ch := range "hello" {
		model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		updated = model.(App)
	}

	if updated.filterInput.Value() != "hello" {
		t.Errorf("Search input should contain 'hello', got '%s'", updated.filterInput.Value())
	}

	// Should still be in search mode
	if !updated.searchActive {
		t.Error("Should still be in search mode while typing")
	}
}

func TestAppSearchEscCancels(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Test Item"},
	}

	// Enter search mode
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)

	// Press Esc to cancel
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEscape})
	updated = model.(App)

	if updated.searchActive {
		t.Error("Esc should exit search mode")
	}
}

func TestAppSearchEnterSubmits(t *testing.T) {
	var embedCalled bool
	var embedQuery string

	app := NewAppWithConfig(AppConfig{
		EmbedQuery: func(query string, queryID string) tea.Cmd {
			embedCalled = true
			embedQuery = query
			return func() tea.Msg {
				return QueryEmbedded{Query: query, Embedding: []float32{1, 2, 3}}
			}
		},
	})
	app.items = []store.Item{
		{ID: "1", Title: "Test Item"},
	}

	// Enter search mode
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)

	// Type query
	for _, ch := range "test" {
		model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		updated = model.(App)
	}

	// Press Enter to submit
	model, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(App)

	if updated.searchActive {
		t.Error("Enter should exit search input mode")
	}

	if !updated.embeddingPending {
		t.Error("Enter should set embeddingPending")
	}

	if cmd == nil {
		t.Error("Enter should return commands (spinner + embed)")
	}

	// Execute commands to check embed was called
	if cmd != nil {
		// The cmd is a Batch; we need to execute it
		// For the test, just verify the state is correct
		_ = embedCalled
		_ = embedQuery
	}
}

func TestAppSearchEmptyEnterCancels(t *testing.T) {
	app := NewApp(nil, nil, nil)

	// Enter search mode
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)

	// Press Enter with empty query
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(App)

	if updated.searchActive {
		t.Error("Enter with empty query should exit search mode")
	}

	if updated.embeddingPending {
		t.Error("Enter with empty query should not start embedding")
	}
}

func TestAppSearchKeysDoNotNavInSearchMode(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}

	// Enter search mode
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)

	// Press 'j' - should type 'j' not navigate
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated = model.(App)

	if updated.Cursor() != 0 {
		t.Errorf("j in search mode should not navigate, cursor was %d", updated.Cursor())
	}

	if updated.filterInput.Value() != "j" {
		t.Errorf("j should be typed into search input, got '%s'", updated.filterInput.Value())
	}
}

func TestAppSearchQDoesNotQuit(t *testing.T) {
	app := NewApp(nil, nil, nil)

	// Enter search mode
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)

	// Press 'q' - should type 'q', not quit
	model, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	updated = model.(App)

	// Should not quit
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Error("q in search mode should not quit")
		}
	}

	if updated.filterInput.Value() != "q" {
		t.Errorf("q should be typed into search input, got '%s'", updated.filterInput.Value())
	}
}

func TestAppSearchViewShowsInput(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = true
	app.width = 80
	app.height = 24
	app.items = []store.Item{
		{ID: "1", Title: "Test Item", SourceName: "source", Published: time.Now()},
	}

	// Enter search mode
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)

	view := updated.View()

	// Should show the "/" prompt
	if !strings.Contains(view, "/") {
		t.Errorf("View should contain search prompt '/', got: %s", view)
	}
}

func TestAppEscClearsResults(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Apple News", Published: time.Now()},
		{ID: "2", Title: "Banana Report", Published: time.Now().Add(-time.Hour)},
	}
	app.filterInput.SetValue("test query")

	// Press Esc to clear
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEscape})
	updated := model.(App)

	if updated.filterInput.Value() != "" {
		t.Errorf("Esc should clear filter, got '%s'", updated.filterInput.Value())
	}

	if updated.searchActive {
		t.Error("Esc should not leave search mode active")
	}
}

// --- Rerank progress tests ---

func TestAppEntryRerankedProgress(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app.rerankPending = true
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app.rerankScores = make([]float32, 3)
	app.rerankProgress = 0
	app.filterInput.SetValue("test")
	app.rerankQuery = "test"
	app.statusText = `Reranking "test"...`

	// Process first entry (parallel — no chaining, so no cmd returned)
	model, cmd := app.Update(EntryReranked{Index: 0, Score: 0.9})
	updated := model.(App)

	if updated.rerankProgress != 1 {
		t.Errorf("Progress should be 1 after first entry, got %d", updated.rerankProgress)
	}

	if math.Abs(float64(updated.rerankScores[0]-0.9)) > 0.01 {
		t.Errorf("Score for entry 0 should be 0.9, got %f", updated.rerankScores[0])
	}

	// Parallel mode: no chaining command returned
	if cmd != nil {
		t.Error("Should return nil command (parallel, no chaining)")
	}

	// Still pending
	if !updated.rerankPending {
		t.Error("Should still be pending after first entry")
	}
}

func TestAppEntryRerankedComplete(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.rerankPending = true
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.rerankScores = []float32{0.3, 0} // First already scored
	app.rerankProgress = 1
	app.filterInput.SetValue("test")
	app.rerankQuery = "test"
	app.statusText = `Reranking "test"...`

	// Process second (final) entry
	model, _ := app.Update(EntryReranked{Index: 1, Score: 0.8})
	updated := model.(App)

	if updated.rerankPending {
		t.Error("Should not be pending after all entries scored")
	}

	// Item 2 (score 0.8) should be first, Item 1 (score 0.3) second
	if updated.items[0].ID != "2" {
		t.Errorf("Item with higher score should be first, got ID '%s'", updated.items[0].ID)
	}

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared after all entries scored, got %q", updated.statusText)
	}
}

func TestAppRerankProgressView(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = true
	app.width = 80
	app.height = 24
	app.items = []store.Item{
		{ID: "1", Title: "Test Item", SourceName: "source", Published: time.Now()},
	}
	app.filterInput.SetValue("query")
	app.rerankPending = true
	app.statusText = "Reranking \"query\"..."
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "First Entry"},
		{ID: "2", Title: "Second Entry"},
		{ID: "3", Title: "Third Entry"},
	}
	app.rerankScores = []float32{0.9, 0.2, 0}
	app.rerankProgress = 2

	view := app.View()

	// Should show item stream
	if !strings.Contains(view, "Test Item") {
		t.Errorf("View should show items, got: %s", view)
	}

	// Should show reranking status in status bar
	if !strings.Contains(view, "Reranking") {
		t.Errorf("Status bar should contain 'Reranking', got: %s", view)
	}

	// statusText-based view shows spinner + status text, not detailed panel
	if strings.Contains(view, "✓") {
		t.Errorf("Status bar view should not show checkmarks, got: %s", view)
	}
}

func TestAppNavigationDuringRerank(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app.rerankPending = true
	app.rerankEntries = app.items
	app.rerankScores = make([]float32, 3)
	app.rerankProgress = 1
	app.statusText = `Reranking "test"...`

	// Should allow j/k navigation during reranking
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated := model.(App)
	if updated.Cursor() != 1 {
		t.Errorf("j should navigate during reranking, got cursor %d", updated.Cursor())
	}

	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	updated = model.(App)
	if updated.Cursor() != 0 {
		t.Errorf("k should navigate during reranking, got cursor %d", updated.Cursor())
	}

	// Arrow keys should also work
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = model.(App)
	if updated.Cursor() != 1 {
		t.Errorf("down arrow should navigate during reranking, got cursor %d", updated.Cursor())
	}
}

func TestAppEntryRerankedOutOfOrder(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app.rerankPending = true
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app.rerankScores = make([]float32, 3)
	app.rerankProgress = 0
	app.filterInput.SetValue("test")
	app.rerankQuery = "test"
	app.statusText = `Reranking "test"...`

	// Scores arrive out of order (parallel execution)
	model, _ := app.Update(EntryReranked{Index: 2, Score: 0.5})
	updated := model.(App)
	if updated.rerankProgress != 1 {
		t.Errorf("Progress should be 1, got %d", updated.rerankProgress)
	}
	if math.Abs(float64(updated.rerankScores[2]-0.5)) > 0.01 {
		t.Errorf("Score for index 2 should be 0.5, got %f", updated.rerankScores[2])
	}

	model, _ = updated.Update(EntryReranked{Index: 0, Score: 0.9})
	updated = model.(App)
	if updated.rerankProgress != 2 {
		t.Errorf("Progress should be 2, got %d", updated.rerankProgress)
	}

	model, _ = updated.Update(EntryReranked{Index: 1, Score: 0.1})
	updated = model.(App)
	if updated.rerankPending {
		t.Error("Should not be pending after all entries scored")
	}

	// Rerank state should be cleared on completion
	if updated.rerankEntries != nil {
		t.Error("rerankEntries should be cleared after completion")
	}
	if updated.rerankScores != nil {
		t.Error("rerankScores should be cleared after completion")
	}
	if updated.rerankProgress != 0 {
		t.Errorf("rerankProgress should be 0 after completion, got %d", updated.rerankProgress)
	}

	// Item 1 (score 0.9) should be first
	if updated.items[0].ID != "1" {
		t.Errorf("Item with highest score should be first, got ID '%s'", updated.items[0].ID)
	}

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared after all entries scored, got %q", updated.statusText)
	}
}

// --- Batch rerank tests ---

func TestAppBatchRerankPath(t *testing.T) {
	var batchCalled bool

	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string, queryID string) tea.Cmd {
			batchCalled = true
			return func() tea.Msg {
				scores := make([]float32, len(docs))
				for i := range scores {
					scores[i] = float32(len(docs)-i) / float32(len(docs))
				}
				return RerankComplete{Query: query, Scores: scores}
			}
		},
	})
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app.filterInput.SetValue("test")
	app.queryEmbedding = []float32{1, 2, 3}
	app.lastEmbeddedQuery = "test"

	// QueryEmbedded handler calls startReranking which fires batchRerank
	model, _ := app.Update(QueryEmbedded{Query: "test", Embedding: []float32{1, 2, 3}})
	updated := model.(App)

	if !updated.rerankPending {
		t.Error("Should be rerank pending after startReranking")
	}
	if !batchCalled {
		t.Error("BatchRerank should have been called")
	}

	// Simulate the RerankComplete that the batch rerank would produce.
	// Scores: Item 2 highest (0.9), Item 3 middle (0.5), Item 1 lowest (0.3).
	model, _ = updated.Update(RerankComplete{
		Query:  "test",
		Scores: []float32{0.3, 0.9, 0.5},
	})
	updated = model.(App)

	if updated.rerankPending {
		t.Error("Should not be pending after RerankComplete")
	}
	if updated.items[0].ID != "2" {
		t.Errorf("Item with highest score should be first, got ID '%s'", updated.items[0].ID)
	}
	if updated.statusText != "" {
		t.Errorf("statusText should be cleared after RerankComplete, got %q", updated.statusText)
	}
	if updated.rerankEntries != nil {
		t.Error("rerankEntries should be cleared after RerankComplete")
	}
}

func TestAppRerankCompleteAppliesScores(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string, queryID string) tea.Cmd {
			return nil
		},
	})
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app.rerankPending = true
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app.rerankScores = make([]float32, 3)
	app.rerankProgress = 0
	app.filterInput.SetValue("test")
	app.statusText = `Reranking "test"...`

	// Send RerankComplete
	model, _ := app.Update(RerankComplete{
		Query:  "test",
		Scores: []float32{0.1, 0.9, 0.5},
	})
	updated := model.(App)

	if updated.rerankPending {
		t.Error("Should not be pending after RerankComplete")
	}

	// Item 2 (score 0.9) should be first
	if updated.items[0].ID != "2" {
		t.Errorf("Item with highest score should be first, got ID '%s'", updated.items[0].ID)
	}

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared after RerankComplete, got %q", updated.statusText)
	}
}

func TestAppRerankCompleteStaleQuery(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string, queryID string) tea.Cmd {
			return nil
		},
	})
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.rerankPending = true
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.rerankScores = make([]float32, 2)
	app.filterInput.SetValue("current query")
	app.statusText = `Reranking "current query"...`

	// Send RerankComplete for a stale query
	model, _ := app.Update(RerankComplete{
		Query:  "old query",
		Scores: []float32{0.1, 0.9},
	})
	updated := model.(App)

	// Should still be pending (stale result ignored)
	if !updated.rerankPending {
		t.Error("Should still be pending after stale RerankComplete")
	}

	if updated.statusText != `Reranking "current query"...` {
		t.Errorf("statusText should be unchanged after stale RerankComplete, got %q", updated.statusText)
	}
}

func TestAppBatchRerankView(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string, queryID string) tea.Cmd {
			return nil
		},
	})
	app.ready = true
	app.width = 80
	app.height = 24
	app.items = []store.Item{
		{ID: "1", Title: "Test Item", SourceName: "source", Published: time.Now()},
	}
	app.filterInput.SetValue("query")
	app.rerankPending = true
	app.statusText = "Reranking \"query\"..."
	app.rerankEntries = []store.Item{{ID: "1", Title: "Test"}}
	app.rerankScores = make([]float32, 1)

	view := app.View()

	// Should show spinner with status text
	if !strings.Contains(view, "Reranking") {
		t.Errorf("View should show 'Reranking' during batch rerank, got: %s", view)
	}

	// Should NOT show checkmarks
	if strings.Contains(view, "✓") {
		t.Errorf("Batch rerank view should not show checkmarks, got: %s", view)
	}
}

func TestAppStatusBarShowsSlash(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = true
	app.width = 80
	app.height = 24
	app.items = []store.Item{
		{ID: "1", Title: "Test Item", SourceName: "test", Published: time.Now()},
	}

	view := app.View()

	// Status bar should show "/" as a key hint
	if !strings.Contains(view, "/") {
		t.Errorf("Status bar should contain '/' key hint, got: %s", view)
	}
}

func TestAppRerankCompleteError(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string, queryID string) tea.Cmd { return nil },
	})
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.rerankPending = true
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.rerankScores = make([]float32, 2)
	app.filterInput.SetValue("test")
	app.statusText = `Reranking "test"...`

	testErr := fmt.Errorf("rerank failed")
	model, _ := app.Update(RerankComplete{
		Query: "test",
		Err:   testErr,
	})
	updated := model.(App)

	if updated.rerankPending {
		t.Error("Should not be pending after error")
	}
	if updated.err == nil {
		t.Error("Error should be set")
	}
	if updated.rerankEntries != nil {
		t.Error("rerankEntries should be cleared on error")
	}

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared on error, got %q", updated.statusText)
	}
}

func TestAppCtrlCDuringRerankQuits(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{{ID: "1", Title: "Item 1"}}
	app.rerankPending = true
	app.rerankEntries = []store.Item{{ID: "1", Title: "Item 1"}}
	app.rerankScores = make([]float32, 1)
	app.statusText = `Reranking "test"...`

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl+C during rerank should return a command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Error("Ctrl+C during rerank should quit")
	}
}

func TestAppCtrlCDuringEmbeddingQuits(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.embeddingPending = true
	app.statusText = `Searching for "test"...`

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl+C during embedding should return a command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Error("Ctrl+C during embedding should quit")
	}
}

func TestAppItemsLoadedCancelsReranking(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.rerankPending = true
	app.rerankEntries = []store.Item{{ID: "1", Title: "Old Item"}}
	app.rerankScores = make([]float32, 1)
	app.rerankProgress = 0
	app.statusText = `Reranking "test"...`

	model, _ := app.Update(ItemsLoaded{Items: []store.Item{
		{ID: "2", Title: "New Item"},
	}})
	updated := model.(App)

	if updated.rerankPending {
		t.Error("ItemsLoaded should cancel in-flight reranking")
	}
	if updated.rerankEntries != nil {
		t.Error("rerankEntries should be cleared")
	}

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared when reranking cancelled, got %q", updated.statusText)
	}
}

// --- Two-stage loading tests ---

func TestTwoStageLoading(t *testing.T) {
	// Track which functions were called
	var recentCalled, fullCalled bool

	stage1Items := []store.Item{
		{ID: "a", Title: "Recent Item A", Published: time.Now()},
		{ID: "b", Title: "Recent Item B", Published: time.Now().Add(-30 * time.Minute)},
	}
	stage2Items := []store.Item{
		{ID: "a", Title: "Recent Item A", Published: time.Now()},
		{ID: "b", Title: "Recent Item B", Published: time.Now().Add(-30 * time.Minute)},
		{ID: "c", Title: "Older Item C", Published: time.Now().Add(-12 * time.Hour)},
		{ID: "d", Title: "Older Item D", Published: time.Now().Add(-20 * time.Hour)},
	}

	app := NewAppWithConfig(AppConfig{
		LoadRecentItems: func() tea.Cmd {
			recentCalled = true
			return func() tea.Msg {
				return ItemsLoaded{Items: stage1Items}
			}
		},
		LoadItems: func() tea.Cmd {
			fullCalled = true
			return func() tea.Msg {
				return ItemsLoaded{Items: stage2Items}
			}
		},
	})

	// Init should call loadRecentItems, not loadItems
	cmd := app.Init()
	if !recentCalled {
		t.Error("Init should call loadRecentItems")
	}
	if fullCalled {
		t.Error("Init should not call loadItems directly")
	}
	if cmd == nil {
		t.Fatal("Init should return a command")
	}

	// Stage 1 arrives: items set, Stage 2 chained
	model, cmd := app.Update(ItemsLoaded{Items: stage1Items})
	updated := model.(App)

	if len(updated.Items()) != 2 {
		t.Errorf("Stage 1 should set 2 items, got %d", len(updated.Items()))
	}
	if !updated.fullLoaded {
		t.Error("fullLoaded should be true after Stage 1 chains Stage 2")
	}
	if !fullCalled {
		t.Error("Stage 1 should chain loadItems (Stage 2)")
	}
	if cmd == nil {
		t.Error("Stage 1 should return a command for Stage 2")
	}

	// Stage 2 arrives: items replaced, no further chaining
	model, cmd = updated.Update(ItemsLoaded{Items: stage2Items})
	updated = model.(App)

	if len(updated.Items()) != 4 {
		t.Errorf("Stage 2 should set 4 items, got %d", len(updated.Items()))
	}
	if cmd != nil {
		t.Error("Stage 2 should not chain further loads")
	}
}

func TestCursorRestoredByID(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		LoadItems: func() tea.Cmd {
			return func() tea.Msg {
				return ItemsLoaded{Items: []store.Item{
					{ID: "x", Title: "Item X"},
					{ID: "y", Title: "Item Y"},
					{ID: "z", Title: "Item Z"},
				}}
			}
		},
	})

	// Set initial items with cursor on item "b"
	app.items = []store.Item{
		{ID: "a", Title: "Item A"},
		{ID: "b", Title: "Item B"},
		{ID: "c", Title: "Item C"},
	}
	app.cursor = 1 // pointing at "b"
	app.fullLoaded = true // skip Stage 2 chaining

	// Load new items that include "b" at a different index
	newItems := []store.Item{
		{ID: "x", Title: "Item X"},
		{ID: "b", Title: "Item B"},
		{ID: "y", Title: "Item Y"},
		{ID: "z", Title: "Item Z"},
	}
	model, _ := app.Update(ItemsLoaded{Items: newItems})
	updated := model.(App)

	if updated.Cursor() != 1 {
		t.Errorf("Cursor should point to item 'b' at index 1, got %d", updated.Cursor())
	}
	if updated.Items()[updated.Cursor()].ID != "b" {
		t.Errorf("Cursor should be on item 'b', got '%s'", updated.Items()[updated.Cursor()].ID)
	}
}

func TestCursorClampedWhenIDNotFound(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "old", Title: "Old Item"},
	}
	app.cursor = 0
	app.fullLoaded = true

	// Load items that don't contain "old"
	newItems := []store.Item{
		{ID: "new1", Title: "New 1"},
		{ID: "new2", Title: "New 2"},
	}
	model, _ := app.Update(ItemsLoaded{Items: newItems})
	updated := model.(App)

	// Cursor was 0, which is still valid, so it stays at 0
	if updated.Cursor() != 0 {
		t.Errorf("Cursor should be clamped to 0, got %d", updated.Cursor())
	}
}

func TestRefreshResetsTwoStage(t *testing.T) {
	var recentCalled bool
	app := NewAppWithConfig(AppConfig{
		LoadRecentItems: func() tea.Cmd {
			recentCalled = true
			return func() tea.Msg {
				return ItemsLoaded{Items: []store.Item{}}
			}
		},
		LoadItems: func() tea.Cmd {
			return func() tea.Msg {
				return ItemsLoaded{Items: []store.Item{}}
			}
		},
	})
	app.fullLoaded = true

	// Press "r" to refresh
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	updated := model.(App)

	if !recentCalled {
		t.Error("r should call loadRecentItems")
	}
	if updated.fullLoaded {
		t.Error("r should reset fullLoaded to false")
	}
	if !updated.loading {
		t.Error("r should set loading to true")
	}
	if cmd == nil {
		t.Error("r should return a command")
	}
}

// --- Full-history search tests ---

func TestSearchSavesAndRestores(t *testing.T) {
	chronoItems := []store.Item{
		{ID: "1", Title: "Chrono Item 1", Published: time.Now()},
		{ID: "2", Title: "Chrono Item 2", Published: time.Now().Add(-time.Hour)},
	}
	chronoEmbeddings := map[string][]float32{
		"1": {0.1, 0.2},
		"2": {0.3, 0.4},
	}

	app := NewAppWithConfig(AppConfig{
		LoadSearchPool: func(queryID string) tea.Cmd {
			return func() tea.Msg {
				return SearchPoolLoaded{
					Items:      []store.Item{{ID: "s1", Title: "Search Pool Item"}},
					Embeddings: map[string][]float32{"s1": {0.5, 0.6}},
				}
			}
		},
		EmbedQuery: func(query string, queryID string) tea.Cmd {
			return func() tea.Msg {
				return QueryEmbedded{Query: query, Embedding: []float32{1, 2, 3}}
			}
		},
	})
	app.items = chronoItems
	app.embeddings = chronoEmbeddings

	// Enter search mode and submit
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)
	for _, ch := range "test" {
		model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		updated = model.(App)
	}
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(App)

	// Verify items were saved
	if updated.savedItems == nil {
		t.Fatal("submitSearch should save items")
	}
	if len(updated.savedItems) != 2 {
		t.Errorf("savedItems should have 2 items, got %d", len(updated.savedItems))
	}
	if updated.savedEmbeddings == nil {
		t.Fatal("submitSearch should save embeddings")
	}

	// Now press Esc to restore
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEscape})
	updated = model.(App)

	if len(updated.Items()) != 2 {
		t.Errorf("Esc should restore 2 chronological items, got %d", len(updated.Items()))
	}
	if updated.Items()[0].ID != "1" {
		t.Errorf("Restored items should start with ID '1', got '%s'", updated.Items()[0].ID)
	}
	if updated.savedItems != nil {
		t.Error("savedItems should be nil after restore")
	}
	if updated.savedEmbeddings != nil {
		t.Error("savedEmbeddings should be nil after restore")
	}
}

func TestSearchPoolLoaded(t *testing.T) {
	poolItems := []store.Item{
		{ID: "p1", Title: "Pool 1"},
		{ID: "p2", Title: "Pool 2"},
		{ID: "p3", Title: "Pool 3"},
	}
	poolEmb := map[string][]float32{
		"p1": {0.1}, "p2": {0.2}, "p3": {0.3},
	}

	app := NewApp(nil, nil, nil)
	app.items = []store.Item{{ID: "old", Title: "Old"}}
	app.filterInput.SetValue("test query")
	// Simulate: search submitted, not in search input mode
	app.searchActive = false

	model, _ := app.Update(SearchPoolLoaded{Items: poolItems, Embeddings: poolEmb})
	updated := model.(App)

	if len(updated.Items()) != 3 {
		t.Errorf("SearchPoolLoaded should replace items, got %d", len(updated.Items()))
	}
	if updated.Items()[0].ID != "p1" {
		t.Errorf("First item should be p1, got '%s'", updated.Items()[0].ID)
	}
}

func TestSearchPoolAndQueryRace_QueryFirst(t *testing.T) {
	// QueryEmbedded arrives before SearchPoolLoaded
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string, queryID string) tea.Cmd {
			return func() tea.Msg {
				return RerankComplete{Query: query, Scores: make([]float32, len(docs))}
			}
		},
	})
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
	}
	app.filterInput.SetValue("test")
	app.searchPoolPending = true // search pool still loading

	// QueryEmbedded arrives first
	model, _ := app.Update(QueryEmbedded{Query: "test", Embedding: []float32{1, 2, 3}})
	updated := model.(App)

	// Should NOT start reranking (pool not ready)
	if updated.rerankPending {
		t.Error("Should not start reranking before search pool arrives")
	}
	// But embedding should be stored
	if updated.queryEmbedding == nil {
		t.Error("queryEmbedding should be stored even when pool is pending")
	}

	// Now SearchPoolLoaded arrives
	poolItems := []store.Item{
		{ID: "p1", Title: "Pool 1"},
		{ID: "p2", Title: "Pool 2"},
	}
	model, _ = updated.Update(SearchPoolLoaded{
		Items:      poolItems,
		Embeddings: map[string][]float32{"p1": {0.5}, "p2": {0.6}},
	})
	updated = model.(App)

	// Now reranking should start
	if !updated.rerankPending {
		t.Error("SearchPoolLoaded should trigger reranking when query embedding is ready")
	}
	if len(updated.Items()) != 2 {
		t.Errorf("Items should be pool items, got %d", len(updated.Items()))
	}
}

func TestSearchPoolAndQueryRace_PoolFirst(t *testing.T) {
	// SearchPoolLoaded arrives before QueryEmbedded
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string, queryID string) tea.Cmd {
			return func() tea.Msg {
				return RerankComplete{Query: query, Scores: make([]float32, len(docs))}
			}
		},
	})
	app.items = []store.Item{{ID: "1", Title: "Item 1"}}
	app.filterInput.SetValue("test")
	app.searchPoolPending = true
	app.embeddingPending = true

	// SearchPoolLoaded arrives first (no query embedding yet)
	model, _ := app.Update(SearchPoolLoaded{
		Items:      []store.Item{{ID: "p1", Title: "Pool 1"}},
		Embeddings: map[string][]float32{"p1": {0.5}},
	})
	updated := model.(App)

	// Should not rerank (no query embedding yet)
	if updated.rerankPending {
		t.Error("Should not start reranking before query embedding arrives")
	}
	if updated.searchPoolPending {
		t.Error("searchPoolPending should be cleared")
	}

	// Now QueryEmbedded arrives
	model, _ = updated.Update(QueryEmbedded{Query: "test", Embedding: []float32{1, 2, 3}})
	updated = model.(App)

	// Now reranking should start
	if !updated.rerankPending {
		t.Error("QueryEmbedded should trigger reranking when pool is ready")
	}
}

func TestStaleSearchPoolIgnored(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Chrono Item", Published: time.Now()},
	}
	// No active query (user pressed Esc)
	app.filterInput.SetValue("")
	app.searchActive = false

	// Stale SearchPoolLoaded arrives after Esc
	model, _ := app.Update(SearchPoolLoaded{
		Items:      []store.Item{{ID: "stale", Title: "Stale Pool Item"}},
		Embeddings: map[string][]float32{"stale": {0.1}},
	})
	updated := model.(App)

	// Items should NOT be replaced
	if updated.Items()[0].ID != "1" {
		t.Errorf("Stale SearchPoolLoaded should be ignored, items[0].ID = '%s'", updated.Items()[0].ID)
	}
}

func TestStage2DuringSearch(t *testing.T) {
	// Stage 2 ItemsLoaded arrives while search is active
	stage2Items := []store.Item{
		{ID: "a", Title: "Full A"},
		{ID: "b", Title: "Full B"},
		{ID: "c", Title: "Full C"},
	}

	app := NewApp(nil, nil, nil)
	app.items = []store.Item{{ID: "search-result", Title: "Search Result"}}
	// Simulate active search with saved items
	app.savedItems = []store.Item{{ID: "s1", Title: "Saved Stage 1"}}
	app.savedEmbeddings = map[string][]float32{"s1": {0.1}}
	app.fullLoaded = true

	// Stage 2 arrives during search
	model, _ := app.Update(ItemsLoaded{
		Items:      stage2Items,
		Embeddings: map[string][]float32{"a": {1}, "b": {2}, "c": {3}},
	})
	updated := model.(App)

	// Live search items should NOT be replaced
	if updated.Items()[0].ID != "search-result" {
		t.Errorf("Live items should not be replaced during search, got '%s'", updated.Items()[0].ID)
	}

	// savedItems SHOULD be updated with Stage 2 data
	if len(updated.savedItems) != 3 {
		t.Errorf("savedItems should be updated to Stage 2 items, got %d", len(updated.savedItems))
	}
	if updated.savedItems[0].ID != "a" {
		t.Errorf("savedItems[0] should be 'a', got '%s'", updated.savedItems[0].ID)
	}

	// savedEmbeddings should be updated
	if len(updated.savedEmbeddings) != 3 {
		t.Errorf("savedEmbeddings should have 3 entries, got %d", len(updated.savedEmbeddings))
	}
}

func TestSearchPoolLoadedError(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.filterInput.SetValue("test")
	app.searchActive = false
	app.searchPoolPending = true

	testErr := fmt.Errorf("search pool failed")
	model, _ := app.Update(SearchPoolLoaded{Err: testErr})
	updated := model.(App)

	if updated.searchPoolPending {
		t.Error("searchPoolPending should be cleared on error")
	}
	if updated.err == nil {
		t.Error("Error should be set")
	}
}

func TestInitFallsBackToLoadItems(t *testing.T) {
	var fullCalled bool
	app := NewAppWithConfig(AppConfig{
		LoadItems: func() tea.Cmd {
			fullCalled = true
			return func() tea.Msg {
				return ItemsLoaded{Items: []store.Item{}}
			}
		},
	})

	cmd := app.Init()
	if !fullCalled {
		t.Error("Init should fall back to loadItems when loadRecentItems is nil")
	}
	if cmd == nil {
		t.Error("Init should return a command")
	}
}

func TestTruncateRunesEdgeCases(t *testing.T) {
	// maxRunes = 0
	if got := truncateRunes("hello", 0); got != "" {
		t.Errorf("truncateRunes(\"hello\", 0) = %q, want \"\"", got)
	}
	// maxRunes = 1
	if got := truncateRunes("hello", 1); got != "h" {
		t.Errorf("truncateRunes(\"hello\", 1) = %q, want \"h\"", got)
	}
	// maxRunes = 3
	if got := truncateRunes("hello", 3); got != "hel" {
		t.Errorf("truncateRunes(\"hello\", 3) = %q, want \"hel\"", got)
	}
	// maxRunes = 4 with truncation
	if got := truncateRunes("hello", 4); got != "h..." {
		t.Errorf("truncateRunes(\"hello\", 4) = %q, want \"h...\"", got)
	}
	// No truncation needed
	if got := truncateRunes("hi", 5); got != "hi" {
		t.Errorf("truncateRunes(\"hi\", 5) = %q, want \"hi\"", got)
	}
}

// --- statusText lifecycle tests ---

func TestStatusTextSetOnSearch(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		EmbedQuery: func(query string, queryID string) tea.Cmd {
			return func() tea.Msg {
				return QueryEmbedded{Query: query, Embedding: []float32{1, 2, 3}}
			}
		},
	})
	app.items = []store.Item{{ID: "1", Title: "Item 1"}}

	// Enter search, type, submit
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(App)
	for _, ch := range "climate" {
		model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		updated = model.(App)
	}
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(App)

	if updated.statusText == "" {
		t.Error("statusText should be set after submitSearch")
	}
	if !strings.Contains(updated.statusText, "climate") {
		t.Errorf("statusText should contain the query, got %q", updated.statusText)
	}
}

func TestStatusTextClearedOnRerankComplete(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{{ID: "1", Title: "Item 1"}}
	app.rerankPending = true
	app.statusText = `Reranking "test"...`
	app.rerankEntries = []store.Item{{ID: "1", Title: "Item 1"}}
	app.rerankScores = make([]float32, 1)
	app.filterInput.SetValue("test")

	model, _ := app.Update(RerankComplete{
		Query:  "test",
		Scores: []float32{0.5},
	})
	updated := model.(App)

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared after RerankComplete, got %q", updated.statusText)
	}
}

func TestStatusTextClearedOnQueryEmbeddedError(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.embeddingPending = true
	app.statusText = `Searching for "test"...`

	model, _ := app.Update(QueryEmbedded{
		Query: "test",
		Err:   fmt.Errorf("embed failed"),
	})
	updated := model.(App)

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared on QueryEmbedded error, got %q", updated.statusText)
	}
}

func TestStatusTextClearedOnEsc(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{{ID: "1", Title: "Item 1"}}
	app.filterInput.SetValue("test")
	app.embeddingPending = true
	app.statusText = `Searching for "test"...`

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEscape})
	updated := model.(App)

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared on Esc, got %q", updated.statusText)
	}
}

func TestViewRendersStatusText(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.ready = true
	app.width = 80
	app.height = 24
	app.items = []store.Item{
		{ID: "1", Title: "Test Item", SourceName: "source", Published: time.Now()},
	}
	app.statusText = `Searching for "climate"...`

	view := app.View()

	if !strings.Contains(view, "Searching") {
		t.Errorf("View should render statusText, got: %s", view)
	}
	if !strings.Contains(view, "climate") {
		t.Errorf("View should contain query in statusText, got: %s", view)
	}
	// Should NOT show normal status bar hints
	if strings.Contains(view, "j/k") {
		t.Errorf("View should not show normal key hints when statusText is set, got: %s", view)
	}
}

func TestStatusTextClearedOnItemsLoadedCancelRerank(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.rerankPending = true
	app.statusText = `Reranking "test"...`
	app.rerankEntries = []store.Item{{ID: "1", Title: "Old Item"}}
	app.rerankScores = make([]float32, 1)
	app.rerankProgress = 0

	model, _ := app.Update(ItemsLoaded{Items: []store.Item{
		{ID: "2", Title: "New Item"},
	}})
	updated := model.(App)

	if updated.statusText != "" {
		t.Errorf("statusText should be cleared when ItemsLoaded cancels reranking, got %q", updated.statusText)
	}
}

// ---------------------------------------------------------------------------
// P4: Query ID tests
// ---------------------------------------------------------------------------

func TestNewQueryIDUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := newQueryID()
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate queryID %q on iteration %d", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestNewQueryIDFormat(t *testing.T) {
	id := newQueryID()
	if len(id) != 16 {
		t.Fatalf("expected queryID length 16, got %d (%q)", len(id), id)
	}
	for i, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("unexpected character %q at position %d in queryID %q", string(c), i, id)
		}
	}
}

func TestStaleQueryIDCheck(t *testing.T) {
	app := NewApp(nil, nil, nil)

	// Simulate a search submission: set the query, queryID, and embeddingPending.
	originalID := newQueryID()
	app.queryID = originalID
	app.embeddingPending = true
	app.filterInput.SetValue("test")

	// Now simulate the user starting a new search (queryID changes).
	app.queryID = newQueryID()

	// Deliver a QueryEmbedded with the OLD queryID — should be discarded.
	model, _ := app.Update(QueryEmbedded{
		Query:     "test",
		Embedding: []float32{0.1, 0.2, 0.3},
		QueryID:   originalID,
	})
	updated := model.(App)

	if updated.queryEmbedding != nil {
		t.Errorf("stale QueryEmbedded should have been discarded, but queryEmbedding is %v", updated.queryEmbedding)
	}
}

func TestSameQueryDifferentID(t *testing.T) {
	app := NewApp(nil, nil, nil)

	// First search for "test" with id1.
	app.queryID = "id1"
	app.embeddingPending = true
	app.filterInput.SetValue("test")

	// Deliver QueryEmbedded with matching id1 — should be accepted.
	model, _ := app.Update(QueryEmbedded{
		Query:     "test",
		Embedding: []float32{0.1, 0.2, 0.3},
		QueryID:   "id1",
	})
	updated := model.(App)

	if updated.queryEmbedding == nil {
		t.Fatal("QueryEmbedded with matching queryID should have been accepted, but queryEmbedding is nil")
	}

	// Second search for "test" — same text, new ID.
	updated.queryID = "id2"
	updated.embeddingPending = true
	updated.queryEmbedding = nil // reset for the new search

	// Deliver QueryEmbedded with OLD id1 — should be discarded even though query text matches.
	model2, _ := updated.Update(QueryEmbedded{
		Query:     "test",
		Embedding: []float32{0.4, 0.5, 0.6},
		QueryID:   "id1",
	})
	updated2 := model2.(App)

	if updated2.queryEmbedding != nil {
		t.Errorf("QueryEmbedded with stale queryID should have been discarded even though query text matches, but queryEmbedding is %v", updated2.queryEmbedding)
	}
}

// --- S10: QueryID stale tests for SearchPoolLoaded and RerankComplete ---

func TestStaleQueryIDSearchPoolLoaded(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{{ID: "1", Title: "Current"}}
	app.filterInput.SetValue("test")
	app.searchActive = false
	app.searchPoolPending = true
	app.queryID = "current-id"

	// Deliver SearchPoolLoaded with a stale QueryID
	model, _ := app.Update(SearchPoolLoaded{
		Items:      []store.Item{{ID: "stale", Title: "Stale Pool"}},
		Embeddings: map[string][]float32{"stale": {0.1}},
		QueryID:    "old-id",
	})
	updated := model.(App)

	// Items should NOT be replaced
	if updated.Items()[0].ID != "1" {
		t.Errorf("stale SearchPoolLoaded should be ignored, got items[0].ID = %q", updated.Items()[0].ID)
	}
}

func TestStaleQueryIDRerankComplete(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.rerankPending = true
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	app.rerankScores = make([]float32, 2)
	app.filterInput.SetValue("test")
	app.queryID = "current-id"
	app.statusText = `Reranking "test"...`

	// Deliver RerankComplete with a stale QueryID
	model, _ := app.Update(RerankComplete{
		Query:   "test",
		Scores:  []float32{0.9, 0.1},
		QueryID: "old-id",
	})
	updated := model.(App)

	// Should still be pending (stale result ignored)
	if !updated.rerankPending {
		t.Error("stale RerankComplete should be ignored, rerankPending should still be true")
	}
	if updated.statusText != `Reranking "test"...` {
		t.Errorf("statusText should be unchanged, got %q", updated.statusText)
	}
}
