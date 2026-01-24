package filters

import (
	"fmt"
	"strings"

	"github.com/abelbrown/observer/internal/curation"
	"github.com/abelbrown/observer/internal/ui/styles"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// View states
type viewState int

const (
	stateList viewState = iota
	stateCreateName
	stateCreateDesc
	stateCreateAction
)

// FilterItem implements list.Item for display in the list
type FilterItem struct {
	filter *curation.Filter
}

func (f FilterItem) Title() string {
	checkbox := "[ ]"
	if f.filter.Enabled {
		checkbox = "[✓]"
	}
	return fmt.Sprintf("%s %s", checkbox, f.filter.Name)
}

func (f FilterItem) Description() string {
	stats := ""
	if f.filter.MatchCount > 0 {
		stats = fmt.Sprintf(" · %d matched", f.filter.MatchCount)
	}
	typeLabel := "pattern"
	if f.filter.Type == curation.FilterTypeSemantic {
		typeLabel = "AI"
	}
	return fmt.Sprintf("%s [%s]%s", truncate(f.filter.Description, 50), typeLabel, stats)
}

func (f FilterItem) FilterValue() string {
	return f.filter.Name
}

// Model is the filter management view
type Model struct {
	list        list.Model
	engine      *curation.FilterEngine
	width       int
	height      int
	state       viewState
	nameInput   textinput.Model
	descInput   textinput.Model
	actionIndex int
	actions     []curation.FilterAction
	quitting    bool
}

// New creates a new filter management view
func New(engine *curation.FilterEngine) Model {
	// Setup list
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(styles.ColorAccentBlue).
		BorderForeground(styles.ColorAccentBlue)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(styles.ColorTextMuted)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Filters"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(styles.ColorTextPrimary).
		Background(styles.ColorSurface).
		Padding(0, 1).
		Bold(true)

	// Setup text inputs for create flow
	nameInput := textinput.New()
	nameInput.Placeholder = "Filter name (e.g., 'No Crypto Hype')"
	nameInput.CharLimit = 50
	nameInput.Width = 50

	descInput := textinput.New()
	descInput.Placeholder = "Describe what to filter in plain English..."
	descInput.CharLimit = 200
	descInput.Width = 60

	m := Model{
		list:      l,
		engine:    engine,
		nameInput: nameInput,
		descInput: descInput,
		actions: []curation.FilterAction{
			curation.ActionHide,
			curation.ActionDim,
			curation.ActionBoost,
			curation.ActionTag,
		},
	}

	m.refreshList()
	return m
}

func (m *Model) refreshList() {
	filters := m.engine.GetFilters()

	items := make([]list.Item, 0, len(filters)+1)

	// Add "Create New" as first item
	items = append(items, createNewItem{})

	// Add existing filters
	for _, f := range filters {
		items = append(items, FilterItem{filter: f})
	}

	m.list.SetItems(items)
}

// createNewItem is the "Add New Filter" option
type createNewItem struct{}

func (c createNewItem) Title() string       { return "+ Create New Filter" }
func (c createNewItem) Description() string { return "Define a new filter in plain English" }
func (c createNewItem) FilterValue() string { return "create" }

// SetSize updates the view dimensions
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.list.SetSize(width-4, height-6)
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch m.state {
	case stateList:
		return m.updateList(msg)
	case stateCreateName:
		return m.updateCreateName(msg)
	case stateCreateDesc:
		return m.updateCreateDesc(msg)
	case stateCreateAction:
		return m.updateCreateAction(msg)
	}
	return m, nil
}

func (m Model) updateList(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			m.quitting = true
			return m, nil

		case "enter", " ":
			// Check what's selected
			if item, ok := m.list.SelectedItem().(createNewItem); ok {
				_ = item
				// Start create flow
				m.state = stateCreateName
				m.nameInput.Focus()
				return m, textinput.Blink
			}

			if item, ok := m.list.SelectedItem().(FilterItem); ok {
				// Toggle filter
				m.engine.ToggleFilter(item.filter.ID)
				m.refreshList()
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) updateCreateName(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.state = stateList
			m.nameInput.Reset()
			return m, nil

		case "enter":
			if m.nameInput.Value() != "" {
				m.state = stateCreateDesc
				m.descInput.Focus()
				return m, textinput.Blink
			}
		}
	}

	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m Model) updateCreateDesc(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.state = stateCreateName
			m.descInput.Reset()
			return m, nil

		case "enter":
			if m.descInput.Value() != "" {
				m.state = stateCreateAction
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.descInput, cmd = m.descInput.Update(msg)
	return m, cmd
}

func (m Model) updateCreateAction(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.state = stateCreateDesc
			return m, nil

		case "up", "k":
			if m.actionIndex > 0 {
				m.actionIndex--
			}

		case "down", "j":
			if m.actionIndex < len(m.actions)-1 {
				m.actionIndex++
			}

		case "enter":
			// Create the filter
			m.engine.CreateFilter(
				m.nameInput.Value(),
				m.descInput.Value(),
				m.actions[m.actionIndex],
			)

			// Reset and go back to list
			m.nameInput.Reset()
			m.descInput.Reset()
			m.actionIndex = 0
			m.state = stateList
			m.refreshList()
			return m, nil
		}
	}

	return m, nil
}

// View renders the filter management UI
func (m Model) View() string {
	var content string

	switch m.state {
	case stateList:
		content = m.renderList()
	case stateCreateName:
		content = m.renderCreateName()
	case stateCreateDesc:
		content = m.renderCreateDesc()
	case stateCreateAction:
		content = m.renderCreateAction()
	}

	// Wrap in a styled box
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorBorder).
		Padding(1, 2).
		Width(m.width - 4).
		Height(m.height - 4)

	return box.Render(content)
}

func (m Model) renderList() string {
	help := styles.Help.Render("\n  [↑↓] navigate · [enter/space] toggle · [esc] close")
	return m.list.View() + help
}

func (m Model) renderCreateName() string {
	title := lipgloss.NewStyle().
		Foreground(styles.ColorAccentBlue).
		Bold(true).
		Render("Create New Filter")

	subtitle := styles.Help.Render("Step 1/3: Name your filter")

	return fmt.Sprintf("%s\n%s\n\n%s\n\n%s",
		title,
		subtitle,
		m.nameInput.View(),
		styles.Help.Render("[enter] next · [esc] cancel"),
	)
}

func (m Model) renderCreateDesc() string {
	title := lipgloss.NewStyle().
		Foreground(styles.ColorAccentBlue).
		Bold(true).
		Render("Create New Filter: " + m.nameInput.Value())

	subtitle := styles.Help.Render("Step 2/3: Describe what to filter (plain English)")

	example := styles.Help.Render(`
Examples:
  "Hide articles about celebrity gossip and entertainment drama"
  "Dim clickbait titles that use emotional manipulation"
  "Boost breaking news about technology and science"
`)

	return fmt.Sprintf("%s\n%s\n\n%s\n%s\n%s",
		title,
		subtitle,
		m.descInput.View(),
		example,
		styles.Help.Render("[enter] next · [esc] back"),
	)
}

func (m Model) renderCreateAction() string {
	title := lipgloss.NewStyle().
		Foreground(styles.ColorAccentBlue).
		Bold(true).
		Render("Create New Filter: " + m.nameInput.Value())

	subtitle := styles.Help.Render("Step 3/3: What should happen when this filter matches?")

	var options strings.Builder
	actionDescs := map[curation.FilterAction]string{
		curation.ActionHide:  "Remove from stream entirely",
		curation.ActionDim:   "Show but visually muted",
		curation.ActionBoost: "Highlight and float higher",
		curation.ActionTag:   "Add a label, keep normal",
	}

	for i, action := range m.actions {
		cursor := "  "
		style := styles.Help
		if i == m.actionIndex {
			cursor = "▶ "
			style = lipgloss.NewStyle().Foreground(styles.ColorAccentBlue)
		}
		options.WriteString(fmt.Sprintf("%s%s%s - %s\n",
			cursor,
			style.Render(string(action)),
			strings.Repeat(" ", 8-len(action)),
			actionDescs[action],
		))
	}

	return fmt.Sprintf("%s\n%s\n\n%s\n%s",
		title,
		subtitle,
		options.String(),
		styles.Help.Render("[enter] create · [esc] back"),
	)
}

// IsQuitting returns true if user wants to close the filter view
func (m Model) IsQuitting() bool {
	return m.quitting
}

// ResetQuitting resets the quitting state
func (m *Model) ResetQuitting() {
	m.quitting = false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
