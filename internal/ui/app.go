package ui

import (
	"github.com/abelbrown/observer/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// App is the root Bubble Tea model.
// IMPORTANT: App does NOT hold *store.Store. It receives items via messages.
type App struct {
	loadItems    func() tea.Cmd
	markRead     func(id string) tea.Cmd
	triggerFetch func() tea.Cmd

	items   []store.Item
	cursor  int
	err     error
	width   int
	height  int
	ready   bool
	loading bool
}

// NewApp creates a new App with the given command functions.
// loadItems: returns a Cmd that fetches items from the store
// markRead: returns a Cmd that marks an item as read
// triggerFetch: returns a Cmd that triggers a background fetch
func NewApp(loadItems func() tea.Cmd, markRead func(id string) tea.Cmd, triggerFetch func() tea.Cmd) App {
	return App{
		loadItems:    loadItems,
		markRead:     markRead,
		triggerFetch: triggerFetch,
		cursor:       0,
	}
}

// Init initializes the App by loading items.
func (a App) Init() tea.Cmd {
	if a.loadItems != nil {
		a.loading = true
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
		a.ready = true
		return a, nil

	case ItemsLoaded:
		a.loading = false
		if msg.Err != nil {
			a.err = msg.Err
		} else {
			a.items = msg.Items
			a.err = nil
			// Reset cursor if it's out of bounds
			if a.cursor >= len(a.items) && len(a.items) > 0 {
				a.cursor = len(a.items) - 1
			}
		}
		return a, nil

	case ItemMarkedRead:
		// Find the item and mark it read locally
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
			// Reload items to show new content
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

// handleKeyMsg processes keyboard input.
func (a App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Clear any existing error on key press
	if a.err != nil {
		a.err = nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return a, tea.Quit

	case "j", "down":
		if a.cursor < len(a.items)-1 {
			a.cursor++
		}
		return a, nil

	case "k", "up":
		if a.cursor > 0 {
			a.cursor--
		}
		return a, nil

	case "g", "home":
		a.cursor = 0
		return a, nil

	case "G", "end":
		if len(a.items) > 0 {
			a.cursor = len(a.items) - 1
		}
		return a, nil

	case "enter":
		if len(a.items) > 0 && a.cursor < len(a.items) {
			item := a.items[a.cursor]
			if a.markRead != nil {
				return a, a.markRead(item.ID)
			}
		}
		return a, nil

	case "r":
		if a.loadItems != nil {
			a.loading = true
			return a, a.loadItems()
		}
		return a, nil

	case "f":
		if a.triggerFetch != nil {
			a.loading = true
			return a, a.triggerFetch()
		}
		return a, nil
	}

	return a, nil
}

// View renders the UI.
func (a App) View() string {
	if !a.ready {
		return "Loading..."
	}

	// Calculate height for content: subtract status bar (1 line) and error bar if present (1 line)
	contentHeight := a.height - 1
	if a.err != nil {
		contentHeight--
	}

	// Render the stream
	stream := RenderStream(a.items, a.cursor, a.width, contentHeight)

	// Render error bar if there's an error (shown above status bar)
	errorBar := ""
	if a.err != nil {
		errorBar = ErrorStyle.Width(a.width).Render("Error: " + a.err.Error() + " (press any key to dismiss)")
	}

	// Render the status bar
	statusBar := RenderStatusBar(a.cursor, len(a.items), a.width, a.loading)

	return stream + errorBar + statusBar
}

// Cursor returns the current cursor position (for testing).
func (a App) Cursor() int {
	return a.cursor
}

// Items returns the current items (for testing).
func (a App) Items() []store.Item {
	return a.items
}
