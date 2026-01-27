package command

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Command represents an available command
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Key         string // shortcut key if any
}

// DefaultCommands returns the built-in commands
func DefaultCommands() []Command {
	return []Command{
		{Name: "sources", Description: "Manage feed sources", Key: "S"},
		{Name: "filters", Aliases: []string{"filter"}, Description: "Manage filters", Key: "f"},
		{Name: "config", Aliases: []string{"settings"}, Description: "Configure API keys & settings", Key: "c"},
		{Name: "density", Aliases: []string{"compact", "comfortable"}, Description: "Toggle compact/comfortable view", Key: "v"},
		{Name: "refresh", Description: "Refresh all feeds", Key: "R"},
		{Name: "shuffle", Description: "Randomize item order", Key: "s"},
		{Name: "panel", Description: "Toggle source panel"},
		{Name: "analyze", Aliases: []string{"ai", "brain", "braintrust"}, Description: "AI analysis of selected item", Key: "a"},
		{Name: "top", Aliases: []string{"breaking", "headlines"}, Description: "Refresh top/breaking stories", Key: "T"},
		{Name: "help", Description: "Show help", Key: "?"},
		{Name: "quit", Aliases: []string{"exit", "q"}, Description: "Exit Observer", Key: "q"},
	}
}

// Palette is a command palette with fuzzy matching
type Palette struct {
	input    textinput.Model
	commands []Command
	filtered []Command
	cursor   int
	width    int
	active   bool
}

// New creates a new command palette
func New() Palette {
	ti := textinput.New()
	ti.Placeholder = "Type a command..."
	ti.Prompt = "/ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff")).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#c9d1d9"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
	ti.CharLimit = 32

	return Palette{
		input:    ti,
		commands: DefaultCommands(),
		filtered: DefaultCommands(),
	}
}

// Activate shows the palette
func (p *Palette) Activate() tea.Cmd {
	p.active = true
	p.input.SetValue("")
	p.input.Focus()
	p.filtered = p.commands
	p.cursor = 0
	return textinput.Blink
}

// Deactivate hides the palette
func (p *Palette) Deactivate() {
	p.active = false
	p.input.Blur()
}

// IsActive returns whether palette is showing
func (p Palette) IsActive() bool {
	return p.active
}

// SetWidth sets the palette width
func (p *Palette) SetWidth(w int) {
	p.width = w
	p.input.Width = w - 10
}

// SelectedCommand returns the currently selected command name
func (p Palette) SelectedCommand() string {
	if p.cursor >= 0 && p.cursor < len(p.filtered) {
		return p.filtered[p.cursor].Name
	}
	return ""
}

// Update handles input
func (p Palette) Update(msg tea.Msg) (Palette, tea.Cmd, string) {
	if !p.active {
		return p, nil, ""
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			p.Deactivate()
			return p, nil, ""

		case "enter":
			cmd := p.SelectedCommand()
			p.Deactivate()
			return p, nil, cmd

		case "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil, ""

		case "down", "ctrl+n":
			if p.cursor < len(p.filtered)-1 {
				p.cursor++
			}
			return p, nil, ""

		case "tab":
			// Tab completion
			if len(p.filtered) > 0 {
				p.input.SetValue(p.filtered[p.cursor].Name)
				p.input.CursorEnd()
			}
			return p, nil, ""
		}
	}

	// Track if input value changed
	oldValue := p.input.Value()

	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)

	// Only filter when input actually changes
	if p.input.Value() != oldValue {
		p.filter()
	}

	return p, cmd, ""
}

func (p *Palette) filter() {
	query := strings.ToLower(p.input.Value())
	if query == "" {
		p.filtered = p.commands
		p.cursor = 0
		return
	}

	var matches []Command
	for _, c := range p.commands {
		// Match name or aliases
		if fuzzyMatch(c.Name, query) {
			matches = append(matches, c)
			continue
		}
		for _, alias := range c.Aliases {
			if fuzzyMatch(alias, query) {
				matches = append(matches, c)
				break
			}
		}
	}

	p.filtered = matches
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

// Simple fuzzy matching - contains substring
func fuzzyMatch(s, query string) bool {
	return strings.Contains(strings.ToLower(s), query)
}

// View renders the palette
func (p Palette) View() string {
	if !p.active {
		return ""
	}

	// Styles
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#30363d")).
		Background(lipgloss.Color("#161b22")).
		Padding(0, 1).
		Width(p.width - 4)

	itemStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#c9d1d9")).
		Padding(0, 1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#58a6ff")).
		Background(lipgloss.Color("#21262d")).
		Bold(true).
		Padding(0, 1).
		Width(p.width - 8)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b949e"))

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#484f58")).
		Background(lipgloss.Color("#21262d")).
		Padding(0, 1)

	var b strings.Builder

	// Input
	b.WriteString(p.input.View())
	b.WriteString("\n")

	// Divider
	dividerWidth := p.width - 8
	if dividerWidth < 0 {
		dividerWidth = 0
	}
	b.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("#30363d")).
		Render(strings.Repeat("─", dividerWidth)))
	b.WriteString("\n")

	// Commands (max 8 visible, scrolls with cursor)
	maxVisible := min(8, len(p.filtered))

	// Calculate scroll offset to keep cursor visible
	start := 0
	if p.cursor >= maxVisible {
		start = p.cursor - maxVisible + 1
	}
	end := min(start+maxVisible, len(p.filtered))

	// Show scroll indicator if there are items above
	if start > 0 {
		b.WriteString(descStyle.Render("  ↑ more above"))
		b.WriteString("\n")
		maxVisible-- // Account for the indicator line
		end = min(start+maxVisible, len(p.filtered))
	}

	for i := start; i < end; i++ {
		cmd := p.filtered[i]

		// Command name and description
		var line string
		if i == p.cursor {
			name := selectedStyle.Render("› " + cmd.Name)
			desc := descStyle.Render(" " + cmd.Description)
			line = name + desc
		} else {
			name := itemStyle.Render("  " + cmd.Name)
			desc := descStyle.Render(" " + cmd.Description)
			line = name + desc
		}

		// Key hint on the right
		if cmd.Key != "" {
			keyHint := keyStyle.Render(cmd.Key)
			// Right-align the key hint
			padding := p.width - 10 - lipgloss.Width(line) - lipgloss.Width(keyHint)
			if padding > 0 {
				line += strings.Repeat(" ", padding) + keyHint
			}
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Show scroll indicator if there are items below
	if end < len(p.filtered) {
		b.WriteString(descStyle.Render("  ↓ more below"))
		b.WriteString("\n")
	}

	// Help hint
	if len(p.filtered) == 0 {
		b.WriteString(descStyle.Render("  No matching commands"))
		b.WriteString("\n")
	}

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#484f58")).
		Render("↑↓ navigate  enter select  tab complete  esc cancel")
	b.WriteString(help)

	return containerStyle.Render(b.String())
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
