package media

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// FeedModel manages the scrollable list of headlines.
type FeedModel struct {
	Headlines []Headline
	Cursor    int
	Offset    int // Scroll offset
	Tick      uint32 // Animation tick
	Styles    Styles
	Glitch    GlitchEngine
}

// NewFeedModel creates a feed model with default settings.
func NewFeedModel(h []Headline) FeedModel {
	return FeedModel{
		Headlines: h,
		Styles:    DefaultStyles(),
		Glitch:    GlitchEngine{Styles: DefaultStyles()},
	}
}

// VisibleRange returns the headlines that should be rendered on screen.
func (m *FeedModel) VisibleRange(height int) []Headline {
	if len(m.Headlines) == 0 {
		return nil
	}
	end := m.Offset + height
	if end > len(m.Headlines) {
		end = len(m.Headlines)
	}
	return m.Headlines[m.Offset:end]
}

// MoveCursor adjusts the cursor and handles scrolling.
func (m *FeedModel) MoveCursor(delta int, height int) {
	m.Cursor += delta
	if m.Cursor < 0 { m.Cursor = 0 }
	if m.Cursor >= len(m.Headlines) { m.Cursor = len(m.Headlines) - 1 }

	// Scroll into view
	if m.Cursor < m.Offset {
		m.Offset = m.Cursor
	}
	if m.Cursor >= m.Offset+height {
		m.Offset = m.Cursor - height + 1
	}
}

// Recompute re-sorts headlines based on weights.
func (m *FeedModel) Recompute(w Weights, now time.Time) {
	for i := range m.Headlines {
		h := &m.Headlines[i]
		
		// Recency Decay: 1.0 for new, decreases over time
		age := now.Sub(h.Published).Hours()
		halfLife := 24.0 // 24 hours
		h.Breakdown.RecencyDecay = 1.0 / (1.0 + (age / halfLife))

		// Calculation logic based on weights
		// FinalScore = (Factors weighted by user sliders)
		score := (h.Breakdown.Semantic * 0.3) +
			(h.Breakdown.Rerank * 0.2) +
			(h.Breakdown.Arousal * w.Arousal) +
			(h.Breakdown.NegBoost * w.Negavity) +
			(h.Breakdown.Diversity * w.Curiosity)
		
		// Modulate decay based on Recency weight
		// If w.Recency is 1.0, use full decay.
		// If w.Recency is 0.0, use no decay (1.0).
		effectiveDecay := 1.0 - ((1.0 - h.Breakdown.RecencyDecay) * w.Recency)
		
		h.Breakdown.FinalScore = score * effectiveDecay
	}

	// Sort stable to keep original order for equal scores
	sort.SliceStable(m.Headlines, func(i, j int) bool {
		return m.Headlines[i].Breakdown.FinalScore > m.Headlines[j].Breakdown.FinalScore
	})
}

// View renders the visible part of the feed.
func (m *FeedModel) View(width, height int) string {
	if len(m.Headlines) == 0 {
		return "No data loaded."
	}

	visible := m.VisibleRange(height)
	var b strings.Builder

	for i, h := range visible {
		idx := m.Offset + i
		b.WriteString(m.renderRow(h, idx == m.Cursor, width))
		b.WriteString("\n")
	}

	return b.String()
}

func (m *FeedModel) renderRow(h Headline, selected bool, width int) string {
	// 1. Rank & Delta (placeholder delta for now)
	rankStr := m.Styles.RankNumber.Render(fmt.Sprintf("%2d.", m.Offset+1)) // should be absolute rank
	deltaStr := m.Styles.DeltaNeutral.Render("  -  ")

	// 2. Meta (Source + Age)
	source := m.Styles.SourceMeta.Render(runewidth.Truncate(h.Source, 14, "…"))
	age := m.Styles.AgeMeta.Render(formatAge(h.Published))

	// 3. Title (Glitchify)
	intensity := GetIntensity(h.Breakdown.Arousal, 0.55)
	glitchedTitle := m.Glitch.Glitchify(h.Title, intensity, m.Tick)

	// Calculate title width
	metaWidth := runewidth.StringWidth(rankStr) + runewidth.StringWidth(deltaStr) + 
		runewidth.StringWidth(source) + runewidth.StringWidth(age) + 4 // spacing
	
	titleWidth := width - metaWidth
	if titleWidth < 10 { titleWidth = 10 }

	// Truncate title
	displayTitle := runewidth.Truncate(glitchedTitle, titleWidth, "…")
	
	// Composite
	row := fmt.Sprintf("%s %s %s %s %s", rankStr, deltaStr, displayTitle, source, age)
	
	if selected {
		return m.Styles.SelectedItem.Width(width).Render(row)
	}
	return m.Styles.FeedItem.Width(width).Render(row)
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
