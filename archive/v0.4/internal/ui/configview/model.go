package configview

import (
	"fmt"
	"strings"

	"github.com/abelbrown/observer/internal/config"
	"github.com/abelbrown/observer/internal/ui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Section of the config
type section int

const (
	sectionModels section = iota
	sectionMCP
	sectionUI
)

// Model is the config view
type Model struct {
	config    *config.Config
	width     int
	height    int
	section   section
	cursor    int
	editing   bool
	input     textinput.Model
	quitting  bool
	saved     bool

	// Model items (expanded)
	modelItems []modelItem
	mcpItems   []mcpItem
}

type modelItem struct {
	name     string
	provider string // claude, openai, gemini, grok, ollama
	settings *config.ModelSettings
}

type mcpItem struct {
	server *config.MCPServerConfig
	isAdd  bool // "Add new server" option
}

// New creates a new config view
func New(cfg *config.Config) Model {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Width = 60

	m := Model{
		config:  cfg,
		input:   ti,
		section: sectionModels,
	}
	m.buildModelItems()
	return m
}

func (m *Model) buildModelItems() {
	m.modelItems = []modelItem{
		{name: "Claude (Anthropic)", provider: "claude", settings: &m.config.Models.Claude},
		{name: "GPT (OpenAI)", provider: "openai", settings: &m.config.Models.OpenAI},
		{name: "Gemini (Google)", provider: "gemini", settings: &m.config.Models.Gemini},
		{name: "Grok (xAI)", provider: "grok", settings: &m.config.Models.Grok},
		{name: "Ollama (Local)", provider: "ollama", settings: &m.config.Models.Ollama},
	}
}

// SetSize updates dimensions
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m.editing {
		return m.updateEditing(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			m.quitting = true
			return m, nil

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			maxCursor := m.maxCursor()
			if m.cursor < maxCursor {
				m.cursor++
			}

		case "tab":
			// Switch sections
			m.section = (m.section + 1) % 3
			m.cursor = 0

		case "enter", " ":
			return m.handleSelect()

		case "e":
			// Edit API key
			if m.section == sectionModels && m.cursor < len(m.modelItems) {
				item := m.modelItems[m.cursor]
				m.editing = true
				m.input.SetValue(item.settings.APIKey)
				m.input.Focus()
				m.input.Placeholder = "Enter API key for " + item.name
				return m, textinput.Blink
			}

		case "s", "ctrl+s":
			// Save config
			if err := m.config.Save(); err == nil {
				m.saved = true
			}
		}

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
	}

	return m, nil
}

func (m Model) updateEditing(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.editing = false
			m.input.Reset()
			return m, nil

		case "enter":
			// Save the value
			if m.section == sectionModels && m.cursor < len(m.modelItems) {
				m.modelItems[m.cursor].settings.APIKey = m.input.Value()
			}
			m.editing = false
			m.input.Reset()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleSelect() (Model, tea.Cmd) {
	switch m.section {
	case sectionModels:
		if m.cursor < len(m.modelItems) {
			// Toggle enabled
			item := m.modelItems[m.cursor]
			item.settings.Enabled = !item.settings.Enabled
		}
	case sectionMCP:
		// Toggle MCP server or add new
		if m.cursor < len(m.config.MCPServers) {
			m.config.MCPServers[m.cursor].Enabled = !m.config.MCPServers[m.cursor].Enabled
		}
	}
	return m, nil
}

func (m Model) maxCursor() int {
	switch m.section {
	case sectionModels:
		return len(m.modelItems) - 1
	case sectionMCP:
		return len(m.config.MCPServers) // +1 for "Add new" but 0-indexed
	case sectionUI:
		return 2 // Theme, source panel, item limit
	}
	return 0
}

// View renders the config UI
func (m Model) View() string {
	// Tabs
	tabs := m.renderTabs()

	// Content based on section
	var content string
	switch m.section {
	case sectionModels:
		content = m.renderModels()
	case sectionMCP:
		content = m.renderMCP()
	case sectionUI:
		content = m.renderUI()
	}

	// Status
	status := ""
	if m.saved {
		status = styles.SystemMessage.Render("  ✓ Config saved!")
	}

	// Help
	help := styles.Help.Render("  [↑↓] navigate · [enter/space] toggle · [e] edit key · [tab] section · [s] save · [esc] close")

	// Compose
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorBorder).
		Padding(1, 2).
		Width(m.width - 4).
		Height(m.height - 4)

	inner := lipgloss.JoinVertical(lipgloss.Left,
		tabs,
		"",
		content,
		"",
		status,
		help,
	)

	return box.Render(inner)
}

func (m Model) renderTabs() string {
	tabStyle := lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(styles.ColorTextMuted)

	activeStyle := tabStyle.Copy().
		Foreground(styles.ColorAccentBlue).
		Bold(true).
		Underline(true)

	tabs := []string{"Models", "MCP Servers", "UI"}
	var rendered []string

	for i, tab := range tabs {
		if section(i) == m.section {
			rendered = append(rendered, activeStyle.Render(tab))
		} else {
			rendered = append(rendered, tabStyle.Render(tab))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func (m Model) renderModels() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("AI Models"))
	b.WriteString("\n")
	b.WriteString(styles.Help.Render("Configure AI providers for Brain Trust and filters"))
	b.WriteString("\n\n")

	for i, item := range m.modelItems {
		cursor := "  "
		if i == m.cursor {
			cursor = "▶ "
		}

		checkbox := "[ ]"
		checkStyle := styles.Help
		if item.settings.Enabled {
			checkbox = "[✓]"
			checkStyle = lipgloss.NewStyle().Foreground(styles.ColorAccentGreen)
		}

		// API key status
		keyStatus := styles.Help.Render("no key")
		if item.settings.APIKey != "" {
			keyStatus = lipgloss.NewStyle().Foreground(styles.ColorAccentGreen).Render("key set")
		}
		if item.provider == "ollama" {
			keyStatus = styles.Help.Render(item.settings.Endpoint)
		}

		// Model name
		modelName := styles.Help.Render(item.settings.Model)

		line := fmt.Sprintf("%s%s %s  %s  %s",
			cursor,
			checkStyle.Render(checkbox),
			item.name,
			keyStatus,
			modelName,
		)

		if i == m.cursor {
			line = lipgloss.NewStyle().Foreground(styles.ColorTextPrimary).Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// If editing
	if m.editing {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorAccentBlue).Render("Enter API Key:"))
		b.WriteString("\n")
		b.WriteString(m.input.View())
	}

	return b.String()
}

func (m Model) renderMCP() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("MCP Servers"))
	b.WriteString("\n")
	b.WriteString(styles.Help.Render("Model Context Protocol servers for extended capabilities"))
	b.WriteString("\n\n")

	if len(m.config.MCPServers) == 0 {
		b.WriteString(styles.Help.Render("  No MCP servers configured"))
		b.WriteString("\n\n")
		b.WriteString(styles.Help.Render("  MCP servers can provide:"))
		b.WriteString("\n")
		b.WriteString(styles.Help.Render("  • File system access"))
		b.WriteString("\n")
		b.WriteString(styles.Help.Render("  • Database connections"))
		b.WriteString("\n")
		b.WriteString(styles.Help.Render("  • Custom tools for AI"))
		b.WriteString("\n")
	}

	for i, server := range m.config.MCPServers {
		cursor := "  "
		if i == m.cursor {
			cursor = "▶ "
		}

		checkbox := "[ ]"
		if server.Enabled {
			checkbox = "[✓]"
		}

		b.WriteString(fmt.Sprintf("%s%s %s (%s)\n", cursor, checkbox, server.Name, server.Command))
	}

	// Add new option
	addCursor := "  "
	if m.cursor == len(m.config.MCPServers) {
		addCursor = "▶ "
	}
	b.WriteString(fmt.Sprintf("\n%s+ Add MCP Server\n", addCursor))

	return b.String()
}

func (m Model) renderUI() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("UI Preferences"))
	b.WriteString("\n\n")

	items := []struct {
		name  string
		value string
	}{
		{"Theme", m.config.UI.Theme},
		{"Show Source Panel", fmt.Sprintf("%v", m.config.UI.ShowSourcePanel)},
		{"Item Limit", fmt.Sprintf("%d", m.config.UI.ItemLimit)},
	}

	for i, item := range items {
		cursor := "  "
		if i == m.cursor {
			cursor = "▶ "
		}
		b.WriteString(fmt.Sprintf("%s%s: %s\n", cursor, item.name, item.value))
	}

	return b.String()
}

// IsQuitting returns true if user wants to close
func (m Model) IsQuitting() bool {
	return m.quitting
}

// ResetQuitting resets the quitting state
func (m *Model) ResetQuitting() {
	m.quitting = false
	m.saved = false
}
