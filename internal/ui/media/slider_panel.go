package media

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

// SliderID identifies which weight a slider controls.
type SliderID int

const (
	SliderArousal SliderID = iota
	SliderNegativity
	SliderCuriosity
	SliderRecency
)

// SliderPanel contains the interactive ranking weights sliders.
type SliderPanel struct {
	Weights Weights
	Focused SliderID
	Styles  Styles
	
	prog progress.Model
}

// NewSliderPanel creates a panel with default weights and styles.
func NewSliderPanel() SliderPanel {
	p := progress.New(
		progress.WithGradient(string(ColorCyan), string(ColorPink)),
		progress.WithoutPercentage(),
	)
	
	return SliderPanel{
		Weights: DefaultWeights(),
		Focused: SliderArousal,
		Styles:  DefaultStyles(),
		prog:    p,
	}
}

// Update handles key messages for moving between and adjusting sliders.
func (p SliderPanel) Update(msg tea.Msg) (SliderPanel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.Focused > 0 {
				p.Focused--
			}
		case "down", "j":
			if p.Focused < 3 {
				p.Focused++
			}
		case "left", "h":
			p.adjustFocused(-0.05)
		case "right", "l":
			p.adjustFocused(0.05)
		}
	}
	return p, nil
}

func (p *SliderPanel) adjustFocused(delta float64) {
	switch p.Focused {
	case SliderArousal:
		p.Weights.Arousal = clamp(p.Weights.Arousal + delta)
	case SliderNegativity:
		p.Weights.Negavity = clamp(p.Weights.Negavity + delta)
	case SliderCuriosity:
		p.Weights.Curiosity = clamp(p.Weights.Curiosity + delta)
	case SliderRecency:
		p.Weights.Recency = clamp(p.Weights.Recency + delta)
	}
}

func clamp(v float64) float64 {
	if v < 0 { return 0 }
	if v > 1 { return 1 }
	return v
}

// View renders the sidebar panel with all 4 sliders.
func (p SliderPanel) View(width int) string {
	var b strings.Builder

	b.WriteString(p.Styles.SidebarTitle.Render("RANKING WEIGHTS"))
	b.WriteString("\n\n")

	sliders := []struct {
		id    SliderID
		label string
		val   float64
	}{
		{SliderArousal, "Arousal", p.Weights.Arousal},
		{SliderNegativity, "Negativity", p.Weights.Negavity},
		{SliderCuriosity, "Curiosity", p.Weights.Curiosity},
		{SliderRecency, "Recency", p.Weights.Recency},
	}

	for _, s := range sliders {
		b.WriteString(p.renderSlider(s.label, s.val, s.id == p.Focused, width))
		b.WriteString("\n\n")
	}

	return b.String()
}

func (p SliderPanel) renderSlider(label string, val float64, focused bool, width int) string {
	indicator := "▷"
	labelStyle := p.Styles.SliderLabel
	if focused {
		indicator = "▶"
		labelStyle = p.Styles.SliderActive
	}

	// Calculate bar width (total width - label width - margin)
	barWidth := width - 12
	if barWidth < 10 { barWidth = 10 }
	p.prog.Width = barWidth

	bar := p.prog.ViewAs(val)
	valStr := fmt.Sprintf("%.0f%%", val*100)

	return fmt.Sprintf("%s %-10s\n  %s %s", 
		indicator, labelStyle.Render(label), bar, p.Styles.SliderDim.Render(valStr)) 
}
