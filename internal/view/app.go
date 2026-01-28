// Package view provides the UI layer for Observer v0.5.
//
// The view layer renders data from controllers and handles user input.
// Views are "dumb" - they don't make data decisions, they just render
// what controllers provide.
//
// # Architecture
//
// The root Model (this file) orchestrates multiple sub-views:
//   - StreamView: Main feed display
//   - WorkView: Work queue visualization
//   - HelpView: Keyboard shortcuts
//
// Each view receives items from its controller via event channels.
package view

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/controller"
	"github.com/abelbrown/observer/internal/controller/controllers"
	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/view/stream"
	"github.com/abelbrown/observer/internal/view/styles"
	workview "github.com/abelbrown/observer/internal/view/work"
	"github.com/abelbrown/observer/internal/work"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// ViewMode represents the current view.
type ViewMode int

const (
	ModeStream ViewMode = iota
	ModeWork
	ModeHelp
)

// Model is the root Bubble Tea model for Observer.
type Model struct {
	// Core components
	store          *model.Store
	pool           *work.Pool
	fetchCtrl      *controllers.FetchController
	mainFeedCtrl   *controllers.MainFeedController
	controllerChan <-chan controller.Event
	workEventChan  <-chan work.Event

	// Views
	streamView stream.Model
	workView   workview.Model

	// UI state
	mode      ViewMode
	width     int
	height    int
	spinner   spinner.Model
	loading   bool
	statusMsg string

	// Command mode
	commandMode  bool
	commandInput string

	// Context for async operations
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a new app model.
func New(store *model.Store, pool *work.Pool, fetchCtrl *controllers.FetchController, mainFeedCtrl *controllers.MainFeedController) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = styles.WorkActive

	ctx, cancel := context.WithCancel(context.Background())

	return Model{
		store:          store,
		pool:           pool,
		fetchCtrl:      fetchCtrl,
		mainFeedCtrl:   mainFeedCtrl,
		controllerChan: mainFeedCtrl.Subscribe(),
		workEventChan:  pool.Subscribe(),
		streamView:     stream.New(),
		workView:       workview.New(pool),
		mode:           ModeStream,
		spinner:        s,
		loading:        true,
		statusMsg:      "Starting...",
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.listenForControllerEvents(),
		m.listenForWorkEvents(),
		m.triggerRefresh(),
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.streamView.SetSize(msg.Width, msg.Height-4) // Reserve for header/status
		m.workView.SetSize(msg.Width, msg.Height-4)

	case tea.KeyMsg:
		// Handle command mode
		if m.commandMode {
			return m.handleCommandInput(msg)
		}

		// Global keys
		switch {
		case key.Matches(msg, keys.Quit):
			m.cancel() // Cancel context on quit
			return m, tea.Quit
		case key.Matches(msg, keys.Help):
			m.mode = ModeHelp
		case key.Matches(msg, keys.Escape):
			m.mode = ModeStream
		case key.Matches(msg, keys.Command):
			m.commandMode = true
			m.commandInput = ""
		case key.Matches(msg, keys.WorkView):
			m.mode = ModeWork
		case key.Matches(msg, keys.Refresh):
			cmds = append(cmds, m.triggerRefresh())
		case key.Matches(msg, keys.Enter):
			if item, ok := m.streamView.SelectedItem(); ok {
				if err := m.store.MarkRead(item.ID); err == nil {
					items := m.streamView.Items()
					for i := range items {
						if items[i].ID == item.ID {
							items[i].Read = true
							break
						}
					}
					m.streamView.SetItems(items)
				}
			}
		}

		// View-specific keys
		switch m.mode {
		case ModeStream:
			var cmd tea.Cmd
			m.streamView, cmd = m.streamView.Update(msg)
			cmds = append(cmds, cmd)
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case controllerEventMsg:
		m.handleControllerEvent(controller.Event(msg))
		cmds = append(cmds, m.listenForControllerEvents())

	case workEventMsg:
		m.handleWorkEvent(work.Event(msg))
		cmds = append(cmds, m.listenForWorkEvents())

	case refreshCompleteMsg:
		// Refresh triggered in background completed
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleControllerEvent(event controller.Event) {
	switch event.Type {
	case controller.EventStarted:
		m.loading = true
		m.statusMsg = "Loading..."
	case controller.EventCompleted:
		m.loading = false
		m.streamView.SetItems(event.Items)
		m.statusMsg = fmt.Sprintf("%d items", len(event.Items))
	case controller.EventError:
		m.loading = false
		m.statusMsg = fmt.Sprintf("Error: %v", event.Err)
	}
}

func (m *Model) handleWorkEvent(event work.Event) {
	// Update status message for significant events
	if event.Change == "started" && event.Item.Type == work.TypeFetch {
		m.statusMsg = fmt.Sprintf("Fetching %s...", event.Item.Source)
	}
	if event.Change == "completed" && event.Item.Type == work.TypeFetch {
		// Trigger a refresh after fetch completes (in background)
		go func() {
			m.mainFeedCtrl.Refresh(m.ctx, m.store, m.pool)
		}()
	}
}

func (m Model) handleCommandInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.commandMode = false
		return m.executeCommand(m.commandInput)
	case "esc":
		m.commandMode = false
		m.commandInput = ""
	case "backspace":
		if len(m.commandInput) > 0 {
			m.commandInput = m.commandInput[:len(m.commandInput)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.commandInput += msg.String()
		}
	}
	return m, nil
}

func (m Model) executeCommand(cmd string) (tea.Model, tea.Cmd) {
	cmd = strings.TrimSpace(cmd)
	switch cmd {
	case "w", "work":
		m.mode = ModeWork
	case "q", "quit":
		m.cancel()
		return m, tea.Quit
	case "r", "refresh":
		return m, m.triggerRefresh()
	case "help", "h":
		m.mode = ModeHelp
	default:
		m.statusMsg = fmt.Sprintf("Unknown command: %s", cmd)
	}
	return m, nil
}

// refreshCompleteMsg signals that a background refresh completed.
type refreshCompleteMsg struct{}

func (m Model) triggerRefresh() tea.Cmd {
	return func() tea.Msg {
		m.mainFeedCtrl.Refresh(m.ctx, m.store, m.pool)
		return refreshCompleteMsg{}
	}
}

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Main content
	switch m.mode {
	case ModeStream:
		b.WriteString(m.streamView.View())
	case ModeWork:
		b.WriteString(m.workView.View())
	case ModeHelp:
		b.WriteString(m.renderHelp())
	}

	// Status bar
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())

	return b.String()
}

func (m Model) renderHeader() string {
	// Left: app name + stats
	itemCount, _ := m.store.ItemCount()
	sourceCount := m.fetchCtrl.SourceCount()

	left := fmt.Sprintf("OBSERVER │ %d sources │ %d items", sourceCount, itemCount)

	// Right: spinner if loading
	right := ""
	if m.loading {
		right = m.spinner.View() + " " + m.statusMsg
	}

	// Pad to width
	padding := m.width - len(left) - len(right) - 4
	if padding < 0 {
		padding = 0
	}

	return styles.Header.Render(left + strings.Repeat(" ", padding) + right)
}

func (m Model) renderStatusBar() string {
	if m.commandMode {
		return styles.StatusBar.Render(fmt.Sprintf(":%s█", m.commandInput))
	}

	// Mode indicator
	modeStr := "stream"
	switch m.mode {
	case ModeWork:
		modeStr = "work"
	case ModeHelp:
		modeStr = "help"
	}

	help := "j/k: navigate  enter: read  /: command  q: quit"
	status := fmt.Sprintf("[%s] %s │ %s", modeStr, m.statusMsg, help)

	return styles.StatusBar.Render(status)
}

func (m Model) renderHelp() string {
	help := `
  OBSERVER v0.5 - MVC Architecture

  NAVIGATION
    j/k, ↑/↓     Move cursor
    enter        Mark as read
    g/G          Jump to top/bottom

  VIEWS
    /w           Work queue
    /help        This help

  COMMANDS
    /            Enter command mode
    /refresh     Refresh feeds
    /quit        Exit

  Press q or Esc to return
`
	return styles.Help.Render(help)
}

// Message types for tea.Cmd
type controllerEventMsg controller.Event
type workEventMsg work.Event

func (m Model) listenForControllerEvents() tea.Cmd {
	return func() tea.Msg {
		// Use select with timeout to avoid blocking forever
		select {
		case event := <-m.controllerChan:
			return controllerEventMsg(event)
		case <-m.ctx.Done():
			// Context cancelled, return empty event
			return controllerEventMsg(controller.Event{Type: controller.EventError, Err: m.ctx.Err()})
		case <-time.After(5 * time.Second):
			// Timeout - return nil to re-poll
			// This prevents UI freeze if controller stops sending events
			return nil
		}
	}
}

func (m Model) listenForWorkEvents() tea.Cmd {
	return func() tea.Msg {
		select {
		case event := <-m.workEventChan:
			return workEventMsg(event)
		case <-m.ctx.Done():
			return workEventMsg(work.Event{})
		case <-time.After(5 * time.Second):
			return nil
		}
	}
}

// Key bindings
var keys = struct {
	Quit     key.Binding
	Help     key.Binding
	Escape   key.Binding
	Command  key.Binding
	WorkView key.Binding
	Refresh  key.Binding
	Enter    key.Binding
}{
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c")),
	Help:     key.NewBinding(key.WithKeys("?")),
	Escape:   key.NewBinding(key.WithKeys("esc")),
	Command:  key.NewBinding(key.WithKeys("/")),
	WorkView: key.NewBinding(key.WithKeys("w")),
	Refresh:  key.NewBinding(key.WithKeys("r")),
	Enter:    key.NewBinding(key.WithKeys("enter")),
}
