package ui

import (
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
	app.err = tea.ErrProgramKilled

	view := app.View()

	if view == "" {
		t.Error("View should not be empty even with error")
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
