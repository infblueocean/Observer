package filters

import (
	"fmt"
	"strings"

	"github.com/abelbrown/observer/internal/curation"
	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/ui/styles"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WorkshopModel is the interactive filter creation workshop
type WorkshopModel struct {
	width      int
	height     int
	input      textarea.Model
	viewport   viewport.Model
	messages   []workshopMessage
	items      []feeds.Item // Current feed items to preview against
	engine     *curation.FilterEngine
	evaluator  FilterWorkshopEvaluator

	// Current filter being crafted
	filterName     string
	filterCriteria string
	filterAction   curation.FilterAction
	previewResults []previewResult

	committed bool
	cancelled bool
}

type workshopMessage struct {
	role    string // "user" or "ai"
	content string
}

type previewResult struct {
	item    feeds.Item
	matched bool
	reason  string
}

// FilterWorkshopEvaluator is the AI interface for the workshop
type FilterWorkshopEvaluator interface {
	// RefineFilter takes user input and current criteria, returns refined criteria and explanation
	RefineFilter(userInput string, currentCriteria string, sampleItems []feeds.Item) (newCriteria string, explanation string, err error)

	// PreviewFilter evaluates items against criteria, returns which would be filtered
	PreviewFilter(criteria string, items []feeds.Item) ([]previewResult, error)

	// SuggestName suggests a filter name based on criteria
	SuggestName(criteria string) (string, error)
}

// NewWorkshop creates a new filter workshop
func NewWorkshop(engine *curation.FilterEngine, items []feeds.Item, evaluator FilterWorkshopEvaluator) WorkshopModel {
	// Setup text input
	ti := textarea.New()
	ti.Placeholder = "Describe what you want to filter..."
	ti.Focus()
	ti.SetWidth(60)
	ti.SetHeight(2)
	ti.ShowLineNumbers = false

	// Setup viewport for conversation history
	vp := viewport.New(60, 10)

	return WorkshopModel{
		input:        ti,
		viewport:     vp,
		items:        items,
		engine:       engine,
		evaluator:    evaluator,
		filterAction: curation.ActionHide, // Default
		messages: []workshopMessage{
			{
				role: "ai",
				content: `Welcome to the Filter Workshop!

Describe what you want to filter in plain English. I'll show you what would be affected, and we can refine it together until it's just right.

Examples:
• "Hide clickbait headlines"
• "Dim articles about celebrity gossip"
• "Boost breaking news about technology"
• "Tag opinion pieces so I know what's editorial"

What would you like to filter?`,
			},
		},
	}
}

// SetSize updates dimensions
func (m *WorkshopModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.input.SetWidth(width - 10)
	m.viewport.Width = width - 10
	m.viewport.Height = height - 15 // Leave room for input and chrome
}

// Init initializes the model
func (m WorkshopModel) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles messages
func (m WorkshopModel) Update(msg tea.Msg) (WorkshopModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.filterCriteria == "" {
				m.cancelled = true
				return m, nil
			}
			// If we have criteria, ask for confirmation
			m.addMessage("ai", "Discard this filter? Type 'yes' to confirm, or continue refining.")

		case "ctrl+s":
			// Quick save
			if m.filterCriteria != "" {
				return m.commitFilter()
			}

		case "enter":
			// Check for special commands
			input := strings.TrimSpace(m.input.Value())

			if input == "" {
				return m, nil
			}

			// Handle commands
			switch strings.ToLower(input) {
			case "save", "commit", "done", "yes":
				if m.filterCriteria != "" {
					return m.commitFilter()
				}
			case "cancel", "quit", "exit":
				m.cancelled = true
				return m, nil
			case "hide", "dim", "boost", "tag":
				m.filterAction = curation.FilterAction(strings.ToLower(input))
				m.addMessage("user", input)
				m.addMessage("ai", fmt.Sprintf("Action set to '%s'. Matching items will be %s.\n\nContinue refining or type 'save' when ready.", input, actionDescription(m.filterAction)))
				m.input.Reset()
				m.updateViewport()
				return m, nil
			}

			// Regular refinement input
			m.addMessage("user", input)
			m.input.Reset()

			// Process with AI (or mock for now)
			return m.processInput(input)
		}

	case filterPreviewMsg:
		m.previewResults = msg.results
		m.filterCriteria = msg.criteria
		m.addMessage("ai", m.formatPreview(msg))
		m.updateViewport()
		return m, nil

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
	}

	// Update textarea
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	// Update viewport
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *WorkshopModel) addMessage(role, content string) {
	m.messages = append(m.messages, workshopMessage{role: role, content: content})
}

func (m *WorkshopModel) updateViewport() {
	var content strings.Builder

	for _, msg := range m.messages {
		if msg.role == "user" {
			content.WriteString(styles.UserMessage.Render("You: " + msg.content))
		} else {
			content.WriteString(styles.AIMessage.Render(msg.content))
		}
		content.WriteString("\n\n")
	}

	m.viewport.SetContent(content.String())
	m.viewport.GotoBottom()
}

func (m WorkshopModel) processInput(input string) (WorkshopModel, tea.Cmd) {
	// If we have an evaluator, use it
	if m.evaluator != nil {
		return m, func() tea.Msg {
			newCriteria, _, _ := m.evaluator.RefineFilter(input, m.filterCriteria, m.items)
			results, _ := m.evaluator.PreviewFilter(newCriteria, m.items)
			return filterPreviewMsg{
				criteria: newCriteria,
				results:  results,
			}
		}
	}

	// Mock response for now (no AI connected)
	mockResults := m.mockPreview(input)
	m.previewResults = mockResults
	m.filterCriteria = input
	m.addMessage("ai", m.formatPreview(filterPreviewMsg{criteria: input, results: mockResults}))
	m.updateViewport()

	return m, nil
}

func (m WorkshopModel) mockPreview(criteria string) []previewResult {
	// Simple mock - just check if title contains keywords from criteria
	keywords := strings.Fields(strings.ToLower(criteria))
	var results []previewResult

	matchCount := 0
	for _, item := range m.items {
		if len(results) >= 10 {
			break // Only show first 10
		}

		titleLower := strings.ToLower(item.Title)
		matched := false

		for _, kw := range keywords {
			if len(kw) > 3 && strings.Contains(titleLower, kw) {
				matched = true
				break
			}
		}

		// Also match on common patterns based on keywords
		if strings.Contains(criteria, "clickbait") {
			if strings.Contains(titleLower, "won't believe") ||
				strings.Contains(titleLower, "shocking") ||
				strings.Contains(titleLower, "you need to") ||
				strings.HasSuffix(item.Title, "...") {
				matched = true
			}
		}

		if matched {
			matchCount++
		}

		results = append(results, previewResult{
			item:    item,
			matched: matched,
			reason:  "keyword match",
		})
	}

	return results
}

func (m WorkshopModel) formatPreview(msg filterPreviewMsg) string {
	var b strings.Builder

	matchedCount := 0
	for _, r := range msg.results {
		if r.matched {
			matchedCount++
		}
	}

	totalItems := len(m.items)
	percentage := float64(matchedCount) / float64(totalItems) * 100

	b.WriteString(fmt.Sprintf("Based on: \"%s\"\n\n", msg.criteria))
	b.WriteString("Preview (sample of your feed):\n\n")

	for _, r := range msg.results {
		if r.matched {
			b.WriteString(fmt.Sprintf("  ✗ %s\n", truncate(r.item.Title, 50)))
		} else {
			b.WriteString(fmt.Sprintf("  ✓ %s (kept)\n", truncate(r.item.Title, 45)))
		}
	}

	b.WriteString(fmt.Sprintf("\nWould %s: %d of %d items (%.1f%%)\n\n",
		m.filterAction, matchedCount, totalItems, percentage))

	b.WriteString("Refine further, change action (hide/dim/boost/tag), or type 'save' when ready.")

	return b.String()
}

func (m WorkshopModel) commitFilter() (WorkshopModel, tea.Cmd) {
	// Generate a name if we don't have one
	name := m.filterName
	if name == "" {
		// Simple name generation from criteria
		words := strings.Fields(m.filterCriteria)
		if len(words) > 3 {
			words = words[:3]
		}
		name = strings.Title(strings.Join(words, " "))
	}

	// Create the filter
	m.engine.CreateFilter(name, m.filterCriteria, m.filterAction)

	m.addMessage("ai", fmt.Sprintf("✓ Filter '%s' saved!\n\nAction: %s\nCriteria: %s\n\nPress any key to return.",
		name, m.filterAction, m.filterCriteria))
	m.updateViewport()
	m.committed = true

	return m, nil
}

// View renders the workshop
func (m WorkshopModel) View() string {
	// Title bar
	title := lipgloss.NewStyle().
		Foreground(styles.ColorAccentBlue).
		Bold(true).
		Render("Filter Workshop")

	subtitle := styles.Help.Render("Interactive filter creation")

	// Action indicator
	actionStyle := lipgloss.NewStyle().
		Foreground(styles.ColorTextMuted).
		Background(styles.ColorSurface).
		Padding(0, 1)
	actionBar := actionStyle.Render(fmt.Sprintf("Action: %s", m.filterAction))

	// Conversation viewport
	conversationBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorBorder).
		Padding(1).
		Width(m.width - 6).
		Height(m.height - 12)

	conversation := conversationBox.Render(m.viewport.View())

	// Input area
	inputBox := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(styles.ColorBorder).
		Padding(0, 1).
		Width(m.width - 6)

	inputArea := inputBox.Render(m.input.View())

	// Help text
	help := styles.Help.Render("  [enter] send · [ctrl+s] save · [esc] cancel · commands: save, hide, dim, boost, tag")

	// Compose
	return lipgloss.JoinVertical(
		lipgloss.Left,
		"",
		"  "+title+"  "+subtitle+"  "+actionBar,
		"",
		conversation,
		inputArea,
		help,
	)
}

// IsCommitted returns true if filter was saved
func (m WorkshopModel) IsCommitted() bool {
	return m.committed
}

// IsCancelled returns true if user cancelled
func (m WorkshopModel) IsCancelled() bool {
	return m.cancelled
}

// Messages

type filterPreviewMsg struct {
	criteria string
	results  []previewResult
}

// Helpers

func actionDescription(action curation.FilterAction) string {
	switch action {
	case curation.ActionHide:
		return "hidden from your stream"
	case curation.ActionDim:
		return "shown but visually dimmed"
	case curation.ActionBoost:
		return "highlighted and boosted"
	case curation.ActionTag:
		return "tagged with a label"
	default:
		return "processed"
	}
}

// Styles for messages
var (
	_ = styles.ColorAccentBlue // Ensure styles is used
)

func init() {
	// Add message styles to the styles package
}
