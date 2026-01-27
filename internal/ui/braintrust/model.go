package braintrust

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/abelbrown/observer/internal/brain"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

// Available spinner styles for variety
var spinnerStyles = []spinner.Spinner{
	spinner.Dot,
	spinner.Globe,
	spinner.Moon,
	spinner.Monkey,
	spinner.Line,
	spinner.Points,
	spinner.Meter,
	spinner.Hamburger,
	spinner.Ellipsis,
	spinner.MiniDot,
	spinner.Jump,
	spinner.Pulse,
}

// Model is the AI Analysis panel UI
type Model struct {
	analysis   *brain.Analysis
	width      int
	height     int
	spinner    spinner.Model
	itemID     string
	itemTitle  string // Title of the item being analyzed
	visible    bool
	scrollPos  int    // Scroll position for content
	totalLines int    // Total lines of content (for scroll limits)
}

// New creates a new AI Analysis panel
func New() Model {
	s := spinner.New()
	s.Spinner = randomSpinner()
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))

	return Model{
		spinner: s,
	}
}

// randomSpinner picks a random spinner style
func randomSpinner() spinner.Spinner {
	return spinnerStyles[rand.Intn(len(spinnerStyles))]
}

// RandomizeSpinner changes to a new random spinner style
func (m *Model) RandomizeSpinner() {
	m.spinner.Spinner = randomSpinner()
}

// SetSize updates the panel dimensions
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetAnalysis updates the displayed analysis
func (m *Model) SetAnalysis(itemID string, itemTitle string, analysis *brain.Analysis) {
	m.itemID = itemID
	m.itemTitle = itemTitle
	m.analysis = analysis
	m.scrollPos = 0 // Reset scroll when content changes

	// Calculate total lines for scroll limits
	if analysis != nil && analysis.Content != "" {
		wrapped := wrapText(analysis.Content, m.width-10)
		m.totalLines = len(strings.Split(wrapped, "\n"))
	} else {
		m.totalLines = 0
	}
}

// SetLoading initializes the panel with loading state
func (m *Model) SetLoading(itemID string, itemTitle string) {
	m.itemID = itemID
	m.itemTitle = itemTitle
	m.visible = true
	m.analysis = &brain.Analysis{Loading: true}
	m.spinner.Spinner = randomSpinner() // Fresh spinner each time
}

// SetVisible shows/hides the panel
func (m *Model) SetVisible(visible bool) {
	m.visible = visible
}

// IsVisible returns whether the panel is showing
func (m Model) IsVisible() bool {
	return m.visible
}

// Clear resets the panel to empty state
func (m *Model) Clear() {
	m.analysis = nil
	m.itemID = ""
	m.scrollPos = 0
}

// ScrollUp scrolls the content up (shows earlier content)
func (m *Model) ScrollUp(lines int) {
	m.scrollPos -= lines
	if m.scrollPos < 0 {
		m.scrollPos = 0
	}
}

// ScrollDown scrolls the content down (shows later content)
func (m *Model) ScrollDown(lines int) {
	m.scrollPos += lines
	maxScroll := m.totalLines - (m.height - 5) // Leave room for header/footer
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollPos > maxScroll {
		m.scrollPos = maxScroll
	}
}

// CanScroll returns true if there's more content to scroll
func (m Model) CanScroll() bool {
	return m.totalLines > (m.height - 5)
}

// Spinner returns the spinner model
func (m Model) Spinner() spinner.Model {
	return m.spinner
}

// UpdateSpinner updates the spinner
func (m *Model) UpdateSpinner(s spinner.Model) {
	m.spinner = s
}

// View renders the AI Analysis panel
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	// Container style with max height
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#30363d")).
		Background(lipgloss.Color("#161b22")).
		Padding(0, 1).
		Width(m.width - 4).
		MaxHeight(m.height)

	// Title style
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#58a6ff")).
		Bold(true)

	// Content style
	contentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#c9d1d9"))

	mutedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8b949e")).
		Italic(true)

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f85149"))

	// Header with pipeline info
	var header string
	if m.analysis != nil && !m.analysis.Loading {
		providerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e")).
			Italic(true)
		pipelineStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3fb950"))

		// Show pipeline if available, otherwise just provider
		if len(m.analysis.Pipeline) > 0 {
			pipelineStr := strings.Join(m.analysis.Pipeline, " │ ")
			header = titleStyle.Render("AI Analysis") + "  " + pipelineStyle.Render("["+pipelineStr+"]")
		} else if m.analysis.Provider != "" {
			header = titleStyle.Render("AI Analysis") + "  " + providerStyle.Render("via "+m.analysis.Provider)
		} else {
			header = titleStyle.Render("AI Analysis")
		}
	} else {
		header = titleStyle.Render("AI Analysis")
	}

	// Show what item is being analyzed
	itemHeader := ""
	if m.itemTitle != "" {
		// Truncate title if too long
		displayTitle := m.itemTitle
		maxTitleLen := m.width - 20
		if len(displayTitle) > maxTitleLen && maxTitleLen > 10 {
			displayTitle = displayTitle[:maxTitleLen-3] + "..."
		}
		itemTitleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c9d1d9")).
			Bold(true)
		itemHeader = itemTitleStyle.Render(displayTitle)
	}

	// Divider
	dividerWidth := m.width - 8
	if dividerWidth < 0 {
		dividerWidth = 0
	}
	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#30363d")).
		Render(strings.Repeat("─", dividerWidth))

	// Content
	var content string
	if m.analysis == nil {
		content = mutedStyle.Render(m.spinner.View() + " Initializing...")
	} else if m.analysis.Loading {
		// Show stage-specific loading message
		stageMsg := "Analyzing..."
		switch m.analysis.Stage {
		case "starting":
			stageMsg = "Starting analysis..."
		case "searching":
			stageMsg = "Preparing request..."
		case "analyzing":
			stageMsg = "Generating analysis..."
		}
		// Show provider if known
		if m.analysis.Provider != "" {
			stageMsg = fmt.Sprintf("%s (via %s)", stageMsg, m.analysis.Provider)
		}
		content = mutedStyle.Render(m.spinner.View() + " " + stageMsg)
	} else if m.analysis.Error != nil {
		content = errorStyle.Render(fmt.Sprintf("Error: %v", m.analysis.Error))
	} else if m.analysis.Content == "" {
		content = mutedStyle.Render("No analysis available.")
	} else {
		// Wrap content to fit panel width
		wrapped := wrapText(m.analysis.Content, m.width-10)
		allLines := strings.Split(wrapped, "\n")

		// Calculate visible lines (leave room for header/divider)
		maxLines := m.height - 5
		if maxLines < 3 {
			maxLines = 3
		}

		// Apply scroll offset
		startLine := m.scrollPos
		if startLine >= len(allLines) {
			startLine = max(0, len(allLines)-1)
		}
		endLine := startLine + maxLines
		if endLine > len(allLines) {
			endLine = len(allLines)
		}

		visibleLines := allLines[startLine:endLine]

		// Add scroll indicators
		if startLine > 0 {
			scrollUpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
			visibleLines = append([]string{scrollUpStyle.Render("▲ scroll up for more")}, visibleLines...)
			if len(visibleLines) > maxLines {
				visibleLines = visibleLines[:maxLines]
			}
		}
		if endLine < len(allLines) {
			scrollDownStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
			if len(visibleLines) >= maxLines {
				visibleLines = visibleLines[:maxLines-1]
			}
			visibleLines = append(visibleLines, scrollDownStyle.Render("▼ scroll down for more"))
		}

		content = contentStyle.Render(strings.Join(visibleLines, "\n"))
	}

	var body string
	if itemHeader != "" {
		body = lipgloss.JoinVertical(lipgloss.Left,
			header,
			itemHeader,
			divider,
			content,
		)
	} else {
		body = lipgloss.JoinVertical(lipgloss.Left,
			header,
			divider,
			content,
		)
	}

	return containerStyle.Render(body)
}

// wrapText wraps text to a given width, preserving paragraph breaks
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	// Split by double newlines to preserve paragraphs
	paragraphs := strings.Split(text, "\n\n")
	var wrappedParagraphs []string

	for _, para := range paragraphs {
		// Replace single newlines with spaces within paragraph
		para = strings.ReplaceAll(para, "\n", " ")
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		var lines []string
		words := strings.Fields(para)
		currentLine := ""

		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}

		wrappedParagraphs = append(wrappedParagraphs, strings.Join(lines, "\n"))
	}

	return strings.Join(wrappedParagraphs, "\n\n")
}

// Legacy compatibility methods
func (m *Model) SetAnalyses(itemID string, itemTitle string, analyses []brain.Analysis) {
	if len(analyses) > 0 {
		m.SetAnalysis(itemID, itemTitle, &analyses[0])
	}
}

func (m *Model) MoveLeft()  {}
func (m *Model) MoveRight() {}
