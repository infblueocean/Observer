package media

import (
	"fmt"
	"strings"
)

// TransparencyCard renders the score breakdown for a single headline.
type TransparencyCard struct {
	Styles Styles
}

// NewTransparencyCard creates a card with default styles.
func NewTransparencyCard() TransparencyCard {
	return TransparencyCard{Styles: DefaultStyles()}
}

// View renders the card for the given headline.
// expanded: if true, show full metric bars; if false, show 1-line summary.
func (c TransparencyCard) View(h Headline, expanded bool, width int) string {
	if !expanded {
		return c.renderCollapsed(h, width)
	}
	return c.renderExpanded(h, width)
}

func (c TransparencyCard) renderCollapsed(h Headline, width int) string {
	// 1-line summary: Sem 0.62 Rer 0.58 Final 0.437
	summary := fmt.Sprintf("Sem %.2f Rer %.2f Final %.3f", 
		h.Breakdown.Semantic, h.Breakdown.Rerank, h.Breakdown.FinalScore)
	
	return c.Styles.CardBorder.Width(width - 2).Render(summary)
}

func (c TransparencyCard) renderExpanded(h Headline, width int) string {
	var b strings.Builder

	// Header: Fire Meter
	fire := c.renderFireMeter(h.Breakdown.Arousal)
	b.WriteString(fmt.Sprintf("RELEVANCE BREAKDOWN  %s\n\n", fire))

	// Metrics
	metrics := []struct {
		label string
		val   float64
	}{
		{"Semantic", h.Breakdown.Semantic},
		{"Reranker", h.Breakdown.Rerank},
		{"Arousal ", h.Breakdown.Arousal},
		{"Recency ", h.Breakdown.RecencyDecay},
		{"Negivity", h.Breakdown.NegBoost},
		{"Final   ", h.Breakdown.FinalScore},
	}

	barWidth := width - 16 // Account for label and value
	if barWidth < 10 { barWidth = 10 }

	for _, m := range metrics {
		label := c.Styles.CardMetricLabel.Render(m.label)
		bar := c.renderBar(m.val, barWidth)
		val := c.Styles.CardMetricValue.Render(fmt.Sprintf("%.2f", m.val))
		
		b.WriteString(fmt.Sprintf("%s %s %s\n", label, bar, val))
	}

	return c.Styles.CardBorder.Width(width - 2).Render(b.String())
}

func (c TransparencyCard) renderBar(val float64, width int) string {
	filledWidth := int(val * float64(width))
	if filledWidth < 0 { filledWidth = 0 }
	if filledWidth > width { filledWidth = width }

	filled := strings.Repeat("█", filledWidth)
	empty := strings.Repeat("░", width-filledWidth)

	return c.Styles.CardBarFill.Render(filled) + c.Styles.CardBarEmpty.Render(empty)
}

func (c TransparencyCard) renderFireMeter(arousal float64) string {
	numFlames := int(arousal * 5)
	if numFlames < 1 && arousal > 0.1 { numFlames = 1 }
	if numFlames > 5 { numFlames = 5 }

	return c.Styles.FireIcon.Render(strings.Repeat("🔥", numFlames))
}
