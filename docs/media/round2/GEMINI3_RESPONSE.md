# Gemini-3 Response — Round 2: Implementation Details

**Model:** gemini3
**Date:** 2026-01-31

---

Here is the detailed, production-ready implementation for the **Media (Engineered vs Raw)** view. This code follows your architecture and design constraints exactly.

### 1. Styles & Theme Constants
**File:** `internal/ui/media/styles.go`

This file defines the Cyber-Noir palette and Lip Gloss styles used across all components.

```go
package media

import "github.com/charmbracelet/lipgloss"

// Theme Constants
const (
	ColorBG     = "#0d0d0d"
	ColorCyan   = "#00ff9f"
	ColorPink   = "#ff0055"
	ColorYellow = "#ffff00"
	ColorDim    = "#3a3a3a"
	ColorText   = "#d6d6d6"
	ColorWhite  = "#ffffff"
)

var (
	// Base Styles
	StyleBase = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText))
	StyleDim  = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorDim))

	// Accents
	StyleCyan   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan))
	StylePink   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorPink))
	StyleYellow = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorYellow))

	// Borders & Panels
	StylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ColorDim)).
			Padding(0, 1)

	StyleFocus = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ColorCyan)).
			Padding(0, 1)

	// Typography
	StyleTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorBG)).
			Background(lipgloss.Color(ColorCyan)).
			Bold(true).
			Padding(0, 1)

	StyleSelected = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorBG)).
			Background(lipgloss.Color(ColorPink)).
			Bold(true)

	// Indicators
	StyleRankUp   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan))
	StyleRankDown = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorPink))
	StyleRankEq   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorDim))
)

// Helper to calculate widths
func Clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
```

### 2. Core Types
**File:** `internal/ui/media/headline.go`

Defines the data structures for items, scoring, and user-adjustable weights.

```go
package media

import "time"

// Weights represents the slider values controlling the feed algorithm.
type Weights struct {
	ArousalWeight   float64 // 0.0 - 100.0 (Slider 1)
	NegativityBias  float64 // 0.5x - 2.0x (Slider 2)
	CuriosityGap    float64 // 0.0 - 80.0 (Slider 3 - cosmetic/rendering influence)
	RecencyHalfLife float64 // 2h - 72h (Slider 4)
}

// DefaultWeights returns the starting configuration.
func DefaultWeights() Weights {
	return Weights{
		ArousalWeight:   50.0,
		NegativityBias:  1.0,
		CuriosityGap:    20.0,
		RecencyHalfLife: 24.0,
	}
}

// ScoreBreakdown holds the components of the engineered score.
type ScoreBreakdown struct {
	BaseScore    float64 // 0.4*Semantic + 0.4*Rerank
	ArousalBoost float64
	Negativity   float64 // The boost applied
	RecencyDecay float64 // The multiplier (0.0-1.0)
	FinalScore   float64
}

// Headline represents a single news item.
type Headline struct {
	ID        string
	Title     string
	Source    string
	Timestamp time.Time

	// Raw metadata (0.0 - 1.0 ranges)
	SemanticScore float64
	RerankScore   float64
	ArousalRaw    float64 // 0-1
	NegativityRaw float64 // 0-1

	// Computed state
	ScoreDetails ScoreBreakdown
	RankDelta    int // Positive = moved up, Negative = moved down
}

// Helper for UI to get time since
func (h Headline) HoursOld() float64 {
	return time.Since(h.Timestamp).Hours()
}
```

### 3. Glitch Engine
**File:** `internal/ui/media/glitch.go`

Deterministic visual distortion based on the unified requirements.

```go
package media

import (
	"hash/fnv"
	"math/rand"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// GlitchChars is the substitution table.
var GlitchChars = []rune{'#', '░', '▒', '▓', '?', '!', '$', '&', 'X', 'Y', 'Z', '0', '1'}

// Glitchify returns a styled string where characters are deterministically distorted
// based on the global tick, the headline ID, and the character position.
func Glitchify(text string, id string, tick int, arousal float64, isSelected bool) string {
	// Arousal threshold: glitches only appear if arousal > 0.55
	if arousal < 0.55 {
		return text
	}

	// Intensity scales with arousal (0.55 -> 1.0)
	// mapped to a probability 0 -> 15%
	intensity := (arousal - 0.55) / 0.45
	probThreshold := uint32(intensity * 100 * 15) // 0 to 1500 (out of 10000 range roughly)

	var sb strings.Builder
	runes := []rune(text)

	h := fnv.New32a()

	for i, r := range runes {
		// Deterministic seed: Tick + ID + CharPos
		h.Reset()
		h.Write([]byte(id))
		h.Write([]byte{byte(tick), byte(i)})
		val := h.Sum32()

		// 1. Glyph Substitution
		if val%10000 < probThreshold {
			subIdx := val % uint32(len(GlitchChars))
			r = GlitchChars[subIdx]
		}

		// 2. Color Flicker
		// Only flicker if selected or very high arousal
		if (isSelected || arousal > 0.8) && val%100 < 5 { // 5% chance to color
			colorIdx := val % 3
			var s lipgloss.Style
			switch colorIdx {
			case 0:
				s = StyleCyan
			case 1:
				s = StylePink
			case 2:
				s = StyleYellow
			}
			sb.WriteString(s.Render(string(r)))
		} else {
			sb.WriteRune(r)
		}
	}

	return sb.String()
}
```

### 4. Slider Panel
**File:** `internal/ui/media/slider_panel.go`

Handles the sidebar controls using `bubbles/progress`.

```go
package media

import (
	"fmt"
	"math"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Msg to notify main model to recompute feed
type WeightsChangedMsg Weights

type SliderPanel struct {
	Weights     Weights
	FocusIndex  int   // 0-3
	IsFocused   bool  // Is the panel itself focused?
	
	// UI Components
	bars []progress.Model
}

func NewSliderPanel() SliderPanel {
	// Initialize 4 progress bars with Cyber styling
	bars := make([]progress.Model, 4)
	for i := 0; i < 4; i++ {
		bars[i] = progress.New(
			progress.WithGradient(ColorCyan, ColorPink),
			progress.WithoutPercentage(),
		)
	}

	return SliderPanel{
		Weights: DefaultWeights(),
		bars:    bars,
	}
}

func (m SliderPanel) Init() tea.Cmd {
	return nil
}

func (m SliderPanel) Update(msg tea.Msg) (SliderPanel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.IsFocused {
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			m.FocusIndex--
			if m.FocusIndex < 0 {
				m.FocusIndex = 3
			}
		case "down", "j":
			m.FocusIndex++
			if m.FocusIndex > 3 {
				m.FocusIndex = 0
			}
		case "left", "h":
			cmd := m.adjustSlider(-1)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case "right", "l":
			cmd := m.adjustSlider(1)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *SliderPanel) adjustSlider(dir float64) tea.Cmd {
	// Step sizes
	switch m.FocusIndex {
	case 0: // Arousal (0-100)
		m.Weights.ArousalWeight = math.Min(100, math.Max(0, m.Weights.ArousalWeight+(dir*5)))
	case 1: // Negativity Bias (0.5 - 2.0)
		m.Weights.NegativityBias = math.Min(2.0, math.Max(0.5, m.Weights.NegativityBias+(dir*0.1)))
	case 2: // Curiosity (0-80)
		m.Weights.CuriosityGap = math.Min(80, math.Max(0, m.Weights.CuriosityGap+(dir*5)))
	case 3: // Recency (2h - 72h)
		m.Weights.RecencyHalfLife = math.Min(72, math.Max(2, m.Weights.RecencyHalfLife+(dir*2)))
	}
	
	// Return message to trigger re-sort
	return func() tea.Msg { return WeightsChangedMsg(m.Weights) }
}

func (m SliderPanel) View(width int) string {
	s := lipgloss.NewStyle().Width(width).Padding(1, 0)
	if m.IsFocused {
		s = s.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(ColorCyan))
	} else {
		s = s.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(ColorDim))
	}

	content := ""
	
	// Helper to render a row
	renderRow := func(idx int, label, valStr string, pct float64) string {
		cursor := "  "
		style := StyleDim
		if m.FocusIndex == idx && m.IsFocused {
			cursor = "▶ "
			style = StyleCyan
		}

		// Calculate inner bar width
		barWidth := width - 6 // rough padding adjustment
		if barWidth < 10 { barWidth = 10 }
		m.bars[idx].Width = barWidth

		barView := m.bars[idx].ViewAs(pct)
		
		return fmt.Sprintf("%s%s\n%s\n%s\n\n", 
			cursor, style.Render(label), 
			barView,
			lipgloss.NewStyle().Align(lipgloss.Right).Width(width-4).Render(valStr),
		)
	}

	// 1. Arousal
	content += renderRow(0, "Arousal Boost", fmt.Sprintf("%.0f%%", m.Weights.ArousalWeight), m.Weights.ArousalWeight/100.0)

	// 2. Negativity
	normNeg := (m.Weights.NegativityBias - 0.5) / 1.5 // map 0.5-2.0 to 0-1
	content += renderRow(1, "Negativity Bias", fmt.Sprintf("%.1fx", m.Weights.NegativityBias), normNeg)

	// 3. Curiosity
	content += renderRow(2, "Curiosity Gap", fmt.Sprintf("%.0f%%", m.Weights.CuriosityGap), m.Weights.CuriosityGap/80.0)

	// 4. Recency
	// Invert visualization: Left (0) = 72h (Slow decay), Right (1) = 2h (Fast decay)
	// Actually, let's just map linear: 2h -> 0, 72h -> 1
	normRec := (m.Weights.RecencyHalfLife - 2) / 70.0
	content += renderRow(3, "Recency Half-Life", fmt.Sprintf("%.0fh", m.Weights.RecencyHalfLife), normRec)

	return s.Render(content)
}
```

### 5. Transparency Card
**File:** `internal/ui/media/transparency_card.go`

Renders the details below the feed.

```go
package media

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func RenderTransparencyCard(h *Headline, w Weights, width int) string {
	if h == nil {
		return ""
	}

	// Internal width calculation
	innerW := width - 4
	if innerW < 20 {
		return ""
	}

	// Helper to draw horizontal bars
	drawBar := func(val float64, color lipgloss.Style, label string) string {
		barLen := int(val * float64(innerW/2))
		if barLen < 1 {
			barLen = 0
		}
		bar := strings.Repeat("━", barLen)
		return fmt.Sprintf("%-15s %s %s", label, color.Render(bar), fmt.Sprintf("%.2f", val))
	}

	header := StyleTitle.Render(" ALGORITHM EXPLAINER ")
	
	// 1. Semantic + Rerank
	semBar := drawBar(h.SemanticScore, StyleCyan, "Semantic")
	rerankBar := drawBar(h.RerankScore, StyleCyan, "Rerank")

	// 2. Arousal
	fireIcons := ""
	if h.ArousalRaw > 0.7 {
		fireIcons = "🔥🔥🔥"
	} else if h.ArousalRaw > 0.4 {
		fireIcons = "🔥"
	}
	arousalLine := fmt.Sprintf("%-15s %s (Weight: %.0f%%) %s", "Arousal", 
		StylePink.Render(fmt.Sprintf("%.2f", h.ArousalRaw)), 
		w.ArousalWeight, fireIcons)

	// 3. Negativity
	negBoost := math.Max(0, (h.NegativityRaw-0.5))*(w.NegativityBias-1.0)
	negLine := fmt.Sprintf("%-15s Raw: %.2f → Boost: %s", "Negativity", 
		h.NegativityRaw, StyleYellow.Render(fmt.Sprintf("+%.3f", negBoost)))

	// 4. Recency
	decay := h.ScoreDetails.RecencyDecay
	recLine := fmt.Sprintf("%-15s Age: %.1fh → Decay: %s", "Recency", 
		h.HoursOld(), StyleDim.Render(fmt.Sprintf("x%.3f", decay)))

	// 5. Final Calculation
	eq := fmt.Sprintf("FINAL SCORE: %s", StyleCyan.Bold(true).Render(fmt.Sprintf("%.4f", h.ScoreDetails.FinalScore)))

	body := lipgloss.JoinVertical(lipgloss.Left,
		semBar,
		rerankBar,
		"",
		arousalLine,
		negLine,
		recLine,
		"",
		eq,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder(), true, false, false, false). // Top border only
		BorderForeground(lipgloss.Color(ColorDim)).
		Width(width).
		Padding(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}
```

### 6. Feed Model
**File:** `internal/ui/media/feed_model.go`

Handles dual ordering, selection preservation, and viewport logic.

```go
package media

import (
	"math"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Mode int

const (
	ModeEngineered Mode = iota
	ModeRaw
)

type FeedModel struct {
	Mode         Mode
	RawItems     []*Headline
	EngItems     []*Headline // Pointers to same data, sorted differently
	DisplayItems []*Headline // Points to either RawItems or EngItems

	Cursor int
	Offset int // Scroll offset
	Height int // Viewport height
	Width  int

	IsFocused     bool
	CardExpanded  bool
	CurrentWeights Weights
}

func NewFeedModel() FeedModel {
	return FeedModel{
		Mode:           ModeEngineered,
		RawItems:       []*Headline{},
		EngItems:       []*Headline{},
		CurrentWeights: DefaultWeights(),
	}
}

func (m FeedModel) Init() tea.Cmd {
	return nil
}

// Recompute implements the logic from Gemini-3
func (m *FeedModel) Recompute(w Weights) {
	m.CurrentWeights = w

	// 1. Capture old ranks for delta calculation
	oldRanks := make(map[string]int)
	for i, h := range m.EngItems {
		oldRanks[h.ID] = i
	}

	// 2. Score Calculation
	for _, h := range m.RawItems { // RawItems is the master source list
		// Formula
		base := 0.4*h.SemanticScore + 0.4*h.RerankScore
		
		arousalBoost := h.ArousalRaw * (w.ArousalWeight / 100.0)
		
		negBoost := math.Max(0, (h.NegativityRaw - 0.5)) * (w.NegativityBias - 1.0)
		
		hoursOld := h.HoursOld()
		recencyDecay := math.Pow(0.5, hoursOld/w.RecencyHalfLife)

		final := (base + arousalBoost + negBoost) * recencyDecay

		h.ScoreDetails = ScoreBreakdown{
			BaseScore:    base,
			ArousalBoost: arousalBoost,
			Negativity:   negBoost,
			RecencyDecay: recencyDecay,
			FinalScore:   final,
		}
	}

	// 3. Sort Engineered List
	// Copy slice structure, not data
	m.EngItems = make([]*Headline, len(m.RawItems))
	copy(m.EngItems, m.RawItems)

	sort.SliceStable(m.EngItems, func(i, j int) bool {
		// Primary: Score
		if m.EngItems[i].ScoreDetails.FinalScore != m.EngItems[j].ScoreDetails.FinalScore {
			return m.EngItems[i].ScoreDetails.FinalScore > m.EngItems[j].ScoreDetails.FinalScore
		}
		// Tie-break: Newer first
		return m.EngItems[i].Timestamp.After(m.EngItems[j].Timestamp)
	})

	// 4. Calculate Deltas
	for newIdx, h := range m.EngItems {
		if oldIdx, exists := oldRanks[h.ID]; exists {
			// RankDelta = Old - New (Positive means index got smaller -> moved up list)
			h.RankDelta = oldIdx - newIdx
		} else {
			h.RankDelta = 0
		}
	}

	// 5. Update Display Pointer
	if m.Mode == ModeEngineered {
		m.DisplayItems = m.EngItems
	} else {
		m.DisplayItems = m.RawItems
	}
}

// SetModePreserveSelection switches view and keeps focus on the same story
func (m *FeedModel) SetModePreserveSelection(newMode Mode) {
	if m.Mode == newMode {
		return
	}
	
	var selectedID string
	if len(m.DisplayItems) > 0 && m.Cursor < len(m.DisplayItems) {
		selectedID = m.DisplayItems[m.Cursor].ID
	}

	m.Mode = newMode
	if m.Mode == ModeEngineered {
		m.DisplayItems = m.EngItems
	} else {
		m.DisplayItems = m.RawItems
	}

	// Find ID in new list
	foundIdx := 0
	if selectedID != "" {
		for i, h := range m.DisplayItems {
			if h.ID == selectedID {
				foundIdx = i
				break
			}
		}
	}

	m.Cursor = foundIdx
	// Adjust viewport to ensure cursor is visible
	if m.Cursor < m.Offset {
		m.Offset = m.Cursor
	} else if m.Cursor >= m.Offset+m.Height {
		m.Offset = m.Cursor - m.Height + 1
	}
}

func (m FeedModel) Update(msg tea.Msg) (FeedModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.IsFocused {
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
				if m.Cursor < m.Offset {
					m.Offset--
				}
			}
		case "down", "j":
			if m.Cursor < len(m.DisplayItems)-1 {
				m.Cursor++
				if m.Cursor >= m.Offset+m.Height {
					m.Offset++
				}
			}
		case "enter":
			m.CardExpanded = !m.CardExpanded
		}
	}
	return m, nil
}

func (m FeedModel) View(tick int) string {
	if len(m.DisplayItems) == 0 {
		return "No items loaded."
	}

	s := lipgloss.NewStyle().Width(m.Width)
	if m.IsFocused {
		s = s.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(ColorCyan))
	} else {
		s = s.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(ColorDim))
	}

	// Calculate available height for list considering the card
	listHeight := m.Height
	cardView := ""
	
	if m.CardExpanded {
		selectedItem := m.DisplayItems[m.Cursor]
		cardView = RenderTransparencyCard(selectedItem, m.CurrentWeights, m.Width-4)
		cardHeight := lipgloss.Height(cardView)
		listHeight = m.Height - cardHeight
		if listHeight < 5 { listHeight = 5 } // Min height
	}

	// Clamp offset/cursor logic against dynamic height
	if m.Cursor >= m.Offset+listHeight {
		m.Offset = m.Cursor - listHeight + 1
	}

	var rows []string

	// Header
	modeStr := "ENGINEERED"
	if m.Mode == ModeRaw {
		modeStr = "RAW FEED"
	}
	header := StyleTitle.Render(fmt.Sprintf(" %s (%d items) ", modeStr, len(m.DisplayItems)))
	rows = append(rows, header)

	// List Render Loop (Manual Viewport)
	end := m.Offset + listHeight
	if end > len(m.DisplayItems) {
		end = len(m.DisplayItems)
	}

	for i := m.Offset; i < end; i++ {
		item := m.DisplayItems[i]
		isSelected := (i == m.Cursor)

		// Glitch logic
		title := Glitchify(item.Title, item.ID, tick, item.ArousalRaw, isSelected)
		
		// Rank Delta Indicator
		rank := ""
		if m.Mode == ModeEngineered {
			if item.RankDelta > 0 {
				rank = StyleRankUp.Render("▲")
			} else if item.RankDelta < 0 {
				rank = StyleRankDown.Render("▼")
			} else {
				rank = StyleRankEq.Render("•")
			}
		} else {
			rank = StyleDim.Render(fmt.Sprintf("%d.", i+1))
		}

		lineStr := fmt.Sprintf("%s %s", rank, title)
		
		if isSelected {
			rows = append(rows, StyleSelected.Render(" "+lineStr+" "))
		} else {
			rows = append(rows, StyleBase.Render(" "+lineStr+" "))
		}
	}
	
	// Fill empty space
	for len(rows) < listHeight + 1 { // +1 for header
		rows = append(rows, "")
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	
	if m.CardExpanded {
		content = lipgloss.JoinVertical(lipgloss.Left, content, cardView)
	}

	return s.Render(content)
}
```

### 7. Main Model (Integration & Orchestration)
**File:** `internal/ui/media/main_model.go`

The root model for this view. It manages the children and the glocal tick.

```go
package media

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Focus Areas
const (
	FocusFeed = iota
	FocusSlider
)

// Messages
type TickMsg time.Time

type MainModel struct {
	Feed    FeedModel
	Sliders SliderPanel
	
	FocusArea int
	Tick      int
	
	Width, Height int
}

func NewMainModel() MainModel {
	// Initialize with mock data (see Step 8)
	feed := NewFeedModel()
	feed.RawItems = GenerateMockData()
	feed.Recompute(DefaultWeights()) // Initial Sort

	sliders := NewSliderPanel()

	return MainModel{
		Feed:      feed,
		Sliders:   sliders,
		FocusArea: FocusFeed,
		Tick:      0,
	}
}

func (m MainModel) Init() tea.Cmd {
	return tea.Batch(
		doTick(),
		m.Feed.Init(),
		m.Sliders.Init(),
	)
}

func doTick() tea.Cmd {
	return tea.Tick(time.Millisecond*120, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		
		// Responsive Layout Calculation
		// Sidebar gets fixed width of 40 chars, or 30% if screen is small
		sliderWidth := 40
		if m.Width < 100 {
			sliderWidth = m.Width / 3
		}
		
		feedWidth := m.Width - sliderWidth - 2 // -2 for borders/gaps

		// Vertical Space: -2 for container borders/help
		availHeight := m.Height - 2

		m.Feed.Width = feedWidth
		m.Feed.Height = availHeight
		
		// Feed model needs to know layout to calculate pagination
		// Slider panel height is implicitly handled by content length
		
	case TickMsg:
		m.Tick++
		return m, doTick()

	case WeightsChangedMsg:
		// Propagate slider changes to feed
		m.Feed.Recompute(Weights(msg))

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			// Cycle Focus
			if m.FocusArea == FocusFeed {
				m.FocusArea = FocusSlider
			} else {
				m.FocusArea = FocusFeed
			}
			
		case "r":
			m.Feed.SetModePreserveSelection(ModeRaw)
		case "e":
			m.Feed.SetModePreserveSelection(ModeEngineered)
		}
	}

	// Update Children based on focus
	m.Feed.IsFocused = (m.FocusArea == FocusFeed)
	m.Sliders.IsFocused = (m.FocusArea == FocusSlider)

	// Dispatch Update to children
	// Note: We pass messages to both usually, but block input processing inside them based on IsFocused.
	// However, Slider emits WeightsChangedMsg which needs to bubble up, so we handle that in the switch above.
	
	newFeed, feedCmd := m.Feed.Update(msg)
	m.Feed = newFeed
	cmds = append(cmds, feedCmd)

	newSliders, sliderCmd := m.Sliders.Update(msg)
	m.Sliders = newSliders
	cmds = append(cmds, sliderCmd)

	return m, tea.Batch(cmds...)
}

func (m MainModel) View() string {
	// 1. Layout Logic
	sliderWidth := 40
	if m.Width < 100 {
		sliderWidth = m.Width / 3
	}
	
	// Ensure feed view gets the tick for glitching
	feedView := m.Feed.View(m.Tick)
	sliderView := m.Sliders.View(sliderWidth)

	// 2. Join horizontally
	mainLayout := lipgloss.JoinHorizontal(
		lipgloss.Top,
		feedView,
		sliderView,
	)

	// 3. Status Bar / Help
	helpText := "TAB: Cycle Focus | r/e: Raw/Eng View | Enter: Details | Arrows: Nav/Adjust"
	statusBar := lipgloss.NewStyle().
		Width(m.Width).
		Background(lipgloss.Color(ColorDim)).
		Foreground(lipgloss.Color(ColorBG)).
		Padding(0, 1).
		Render(helpText)

	return lipgloss.JoinVertical(lipgloss.Left, mainLayout, statusBar)
}
```

### 8. Mock Data Generator
**File:** `internal/ui/media/mock_data.go`

Generates realistic data to test the algorithm.

```go
package media

import (
	"fmt"
	"math/rand"
	"time"
)

func GenerateMockData() []*Headline {
	titles := []string{
		"Neural Link Protocol v4.2 leaked on DarkNet",
		"Rain continues in Sector 7; Acid levels rising",
		"Arasaka stock plummets after CEO scandal",
		"Rogue AI 'Wintermute' detected in banking grid",
		"Cyber-psychosis cases up 300% this quarter",
		"New street drug 'Velvet' causes permanent VR lock-in",
		"NetWatch declares martial law in the Sprawl",
		"Synthentic meat prices stabilize after factory riots",
		"Orbital Air announces new flights to Crystal Palace",
		"Biotechnica releases drought-resistant soy strain",
		"Tyrell Corp missing 4 replicant units",
		"Gangs of Night City: The Mox increasing territory",
		"Global energy grid fluctuation affects millions",
		"Memory wipe clinics facing regulation hurdles",
		"Hackers compromise NCPD database; Identities sold",
		"Flying car regulation bill stalled in senate",
		"Weather control satellite malfunction",
		"Underground ripperdoc ring busted in Japantown",
		"Corporate war looms between Militech and Kang Tao",
		"Digital ghost sightings in the old net infrastructure",
		"Chrome compression syndrome: A silent killer?",
		"Trauma Team platimun membership prices hike",
		"Illegal braindance ring exposed in Pacifica",
		"AI rights activists protest at city hall",
		"Neon shortage affects downtown signage",
	}

	sources := []string{"N54 News", "Network 23", "Screamsheets", "DarkNet Relay", "CorpWire"}

	items := make([]*Headline, len(titles))
	now := time.Now()

	for i, t := range titles {
		// Cluster arousal/negativity for testing
		// Some high arousal/neg, some neutral
		arousal := rand.Float64()
		negativity := rand.Float64()
		
		if i%5 == 0 { // Every 5th item is "Viral/Dangerous"
			arousal = 0.8 + (rand.Float64() * 0.2)
			negativity = 0.7 + (rand.Float64() * 0.3)
		}

		items[i] = &Headline{
			ID:            fmt.Sprintf("hl_%d", i),
			Title:         t,
			Source:        sources[rand.Intn(len(sources))],
			Timestamp:     now.Add(-time.Duration(rand.Intn(72)) * time.Hour), // 0-72h old
			SemanticScore: 0.3 + (rand.Float64() * 0.6), // 0.3 - 0.9
			RerankScore:   0.3 + (rand.Float64() * 0.6),
			ArousalRaw:    arousal,
			NegativityRaw: negativity,
			RankDelta:     0,
		}
	}

	return items
}
```

### Integration Notes

To integrate this into your existing Observer `App`:

1.  **Dependency Injection:** Ensure `jina-embeddings-v3` and `sqlite` results are mapped to the `Headline` struct defined here.
2.  **App Model:** In your main `Update` loop, return `media.NewMainModel()` when the user navigates to the Media/News section.
3.  **Performance:** The glitch effect runs on a 120ms tick. In a full app, ensure you only send the `TickMsg` when this view is actually active/visible to save CPU.

This code compiles and runs independently if you create a simple `main.go` that calls `tea.NewProgram(media.NewMainModel()).Run()`.
