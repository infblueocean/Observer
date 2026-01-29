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
		EmbedQuery: func(query string) tea.Cmd {
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
	app.rerankEntries = []store.Item{
		{ID: "1", Title: "First Entry"},
		{ID: "2", Title: "Second Entry"},
		{ID: "3", Title: "Third Entry"},
	}
	app.rerankScores = []float32{0.9, 0.2, 0}
	app.rerankProgress = 2
	app.rerankStart = time.Now()

	view := app.View()

	// Should show item stream above progress panel
	if !strings.Contains(view, "Test Item") {
		t.Errorf("View should show items above progress panel, got: %s", view)
	}

	// Should show reranking progress in bottom panel
	if !strings.Contains(view, "Reranking") {
		t.Errorf("Progress view should contain 'Reranking', got: %s", view)
	}

	// Should show completed entries in panel
	if !strings.Contains(view, "First Entry") {
		t.Errorf("Progress view should show completed entry 'First Entry', got: %s", view)
	}

	// Should show checkmarks
	if !strings.Contains(view, "✓") {
		t.Errorf("Progress view should contain checkmarks, got: %s", view)
	}

	// Should show count
	if !strings.Contains(view, "2/3") {
		t.Errorf("Progress view should show count '2/3', got: %s", view)
	}

	// Should show separator
	if !strings.Contains(view, "───") {
		t.Errorf("Progress view should contain separator, got: %s", view)
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
	if updated.rerankProgress != 3 {
		t.Errorf("Progress should be 3, got %d", updated.rerankProgress)
	}
	if updated.rerankPending {
		t.Error("Should not be pending after all entries scored")
	}

	// Item 1 (score 0.9) should be first
	if updated.items[0].ID != "1" {
		t.Errorf("Item with highest score should be first, got ID '%s'", updated.items[0].ID)
	}
}

// --- Batch rerank tests ---

func TestAppBatchRerankPath(t *testing.T) {
	var batchCalled bool
	var batchQuery string
	var batchDocs []string

	app := NewAppWithConfig(AppConfig{
		EmbedQuery: func(query string) tea.Cmd {
			return func() tea.Msg {
				return QueryEmbedded{Query: query, Embedding: []float32{1, 2, 3}}
			}
		},
		BatchRerank: func(query string, docs []string) tea.Cmd {
			batchCalled = true
			batchQuery = query
			batchDocs = docs
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

	// Simulate query embedded → triggers startReranking
	model, cmd := app.Update(QueryEmbedded{Query: "test", Embedding: []float32{1, 2, 3}})
	updated := model.(App)

	// Note: the QueryEmbedded handler calls startReranking which fires batchRerank.
	// But we also need to set the filterInput value first.
	// Let's set up properly:
	app2 := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string) tea.Cmd {
			batchCalled = true
			batchQuery = query
			batchDocs = docs
			return func() tea.Msg {
				scores := make([]float32, len(docs))
				for i := range scores {
					scores[i] = float32(len(docs)-i) / float32(len(docs))
				}
				return RerankComplete{Query: query, Scores: scores}
			}
		},
	})
	app2.items = []store.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	app2.filterInput.SetValue("test")
	app2.queryEmbedding = []float32{1, 2, 3}
	app2.lastEmbeddedQuery = "test"

	// Directly call startReranking (which is what QueryEmbedded handler does)
	model, cmd = app2.Update(QueryEmbedded{Query: "test", Embedding: []float32{1, 2, 3}})
	updated = model.(App)
	_ = cmd

	if !updated.rerankPending {
		t.Error("Should be rerank pending after startReranking")
	}

	if !batchCalled {
		t.Error("BatchRerank should have been called")
	}
	_ = batchQuery
	_ = batchDocs
}

func TestAppRerankCompleteAppliesScores(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string) tea.Cmd {
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
}

func TestAppRerankCompleteStaleQuery(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string) tea.Cmd {
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
}

func TestAppBatchRerankView(t *testing.T) {
	app := NewAppWithConfig(AppConfig{
		BatchRerank: func(query string, docs []string) tea.Cmd {
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
	app.rerankEntries = []store.Item{{ID: "1", Title: "Test"}}
	app.rerankScores = make([]float32, 1)

	view := app.View()

	// Batch mode should show spinner, not progress panel
	if !strings.Contains(view, "Reranking") {
		t.Errorf("View should show 'Reranking' during batch rerank, got: %s", view)
	}

	// Should NOT show the progress panel checkmarks (that's per-entry mode)
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
		BatchRerank: func(query string, docs []string) tea.Cmd { return nil },
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
}

func TestAppCtrlCDuringRerankQuits(t *testing.T) {
	app := NewApp(nil, nil, nil)
	app.items = []store.Item{{ID: "1", Title: "Item 1"}}
	app.rerankPending = true
	app.rerankEntries = []store.Item{{ID: "1", Title: "Item 1"}}
	app.rerankScores = make([]float32, 1)

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
