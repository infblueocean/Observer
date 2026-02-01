# GPT-5 Response — Engineered vs Raw View

**Model:** gpt5
**Date:** 2026-01-31

---

## 1) Architecture recommendation (Bubble Tea model hierarchy + message flow)

### Root `appModel` (orchestrator)
Owns:
- global UI state: `mode` (Raw/Engineered), `focus` (list vs sidebar), `width/height`, `loading/error`, `tick` (for glitch animation)
- child components:
  - `feedModel` (headlines, selection, transparency card expanded state, sorting + rank deltas)
  - `sliderPanel` (weights; emits “weights changed” effects)
  - optional `spinner/help/status` components

Flow:
- `appModel.Update(msg)`:
  - handles app-level keys (`r/e/tab/q`, window resize)
  - delegates to focused child component
  - when weights change: call `feedModel.Recompute(weights)` (pure, deterministic)
  - schedules animation ticks (`tea.Tick`) for glitch flicker

### `feedModel` (testable domain component)
Owns:
- headline objects (prefer pointers so raw/engineered views share the same instances)
- two orderings:
  - `rawOrder []*Headline` (chronological)
  - `engOrder []*Headline` (sorted by engineered score)
- selection + transparency expansion state
- ranking diff metadata (`RankDelta`) updated on re-sort

Responsibilities:
- `MoveSelection(+/-1)`
- `ToggleCard()`
- `SetMode(mode)` while preserving selection by ID
- `Recompute(weights)`:
  - recompute engineered scores (client-side)
  - sort `engOrder`
  - compute per-item `RankDelta` (↑/↓ indicators)

### `sliderPanel` (UI component)
Owns:
- current weights (for now only `ArousalWeight` 0..1)
- rendering of a “slider bar” (you can use `bubbles/progress` to render it nicely)
- key handling for left/right when focused

### Transparency card
Not necessarily a full model early-on: a view function rendering the selected headline’s breakdown. Later you can make it a component with its own viewport, collapse/expand animations, etc.

---

## 2) Key technical challenges + how to solve

1) **Stable, explainable re-ranking with live sliders**
- Challenge: frequent resorting can feel “jumpy”; users need to understand *why*.
- Solution:
  - Keep a stable sort tie-break (e.g., recency) so small score changes don’t randomize.
  - Cache per-item score components; recompute only the weighted sum on slider change.
  - Track previous ranks to show diff arrows (↑/↓) and optionally animate them.

2) **Glitch/cyber-noir effects without killing performance**
- Challenge: per-frame per-char styling can get expensive with 4k items.
- Solution:
  - Apply glitch only to visible rows (when you add a viewport/list).
  - Use deterministic pseudo-random based on `(tick, headlineID)` to avoid storing random state.
  - Keep the effect subtle (a few characters max) and throttle ticks (~80–150ms).

3) **Responsive layout (split views + sidebar)**
- Challenge: terminal sizes vary; must not overflow or wrap badly.
- Solution:
  - On `tea.WindowSizeMsg`, recompute panel widths and pass them down.
  - Use Lip Gloss width constraints and truncate titles with ellipsis.

4) **Testability (teatest-friendly)**
- Challenge: Bubble Tea UIs often tangle domain logic into rendering.
- Solution:
  - Make `feedModel.Recompute(weights)` pure and deterministic.
  - Keep “random” glitch deterministic and injectable (tick-driven).
  - Avoid I/O in models; represent loading via messages.

---

## 3) Concrete implementation plan + file structure

**Phase 0 (minimal working, in code below)**
- Raw/Engineered toggle (`r` / `e`)
- Headline list navigation
- Transparency card (Enter)
- One slider: Arousal weight (Left/Right, focus with Tab)
- Live resort + rank delta indicators
- Resize handling
- Loading state + animated glitch tick

**Suggested structure**
```
.
├── main.go                 // program entry
├── app_model.go            // root model/update/view
├── feed_model.go           // sorting, selection, score recompute
├── slider_panel.go         // arousal slider (uses bubbles/progress)
├── styles.go               // lipgloss styles + colors
├── glitch.go               // deterministic glitch rendering
└── headline.go             // types: Headline, ScoreBreakdown, Weights
```

---

## 4) Minimal working implementation (toggle, 1 transparency card, 1 slider re-sorts)

> This compiles as-is as a multi-file `package main`. Mock data included.  
> Keys: `r/e` toggle modes, `tab` focus list/sidebar, `↑↓` move, `enter` toggle transparency, `←→` adjust slider, `q` quit.

### `main.go`
```go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	p := tea.NewProgram(
		newAppModel(),
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

### `headline.go`
```go
package main

import "time"

type ViewMode int

const (
	ViewRaw ViewMode = iota
	ViewEngineered
)

type Weights struct {
	// 0..1
	ArousalWeight float64

	// placeholders for later expansion:
	// NegativityMultiplier float64
	// CuriosityBoost       float64
	// RecencyHalfLife      time.Duration
}

type ScoreBreakdown struct {
	Semantic float64 // 0..1
	Rerank   float64 // 0..1
	Arousal  float64 // 0..100

	// placeholders for later:
	RecencyWeight float64 // 0..1
	Diversity     float64 // 0..1
}

type Headline struct {
	ID          int
	Title       string
	Source      string
	PublishedAt time.Time

	Scores ScoreBreakdown

	Engineered float64 // computed
	RankDelta  int     // + => moved up, - => moved down (engineered only)
}
```

### `feed_model.go`
```go
package main

import (
	"sort"
	"time"
)

type feedModel struct {
	items []*Headline

	rawOrder []*Headline
	engOrder []*Headline

	selected int
	cardOpen bool
}

func newFeedModel(now time.Time) feedModel {
	items := []*Headline{
		{
			ID:          1,
			Title:       "Blackout spreads across port cities as undersea cable alarms spike",
			Source:      "NightWire",
			PublishedAt: now.Add(-18 * time.Minute),
			Scores: ScoreBreakdown{
				Semantic:      0.62,
				Rerank:        0.58,
				Arousal:       88,
				RecencyWeight: 0.92,
				Diversity:     0.40,
			},
		},
		{
			ID:          2,
			Title:       "Central bank signals pause; markets exhale, cautiously",
			Source:      "The Ledger",
			PublishedAt: now.Add(-55 * time.Minute),
			Scores: ScoreBreakdown{
				Semantic:      0.44,
				Rerank:        0.51,
				Arousal:       22,
				RecencyWeight: 0.78,
				Diversity:     0.65,
			},
		},
		{
			ID:          3,
			Title:       "Leaked memo hints at sweeping surveillance reform after tribunal ruling",
			Source:      "CipherPost",
			PublishedAt: now.Add(-2 * time.Hour),
			Scores: ScoreBreakdown{
				Semantic:      0.71,
				Rerank:        0.66,
				Arousal:       64,
				RecencyWeight: 0.55,
				Diversity:     0.52,
			},
		},
		{
			ID:          4,
			Title:       "Wildcat strike freezes logistics corridor; officials deny shortages",
			Source:      "UnionSignal",
			PublishedAt: now.Add(-35 * time.Minute),
			Scores: ScoreBreakdown{
				Semantic:      0.53,
				Rerank:        0.49,
				Arousal:       73,
				RecencyWeight: 0.84,
				Diversity:     0.33,
			},
		},
		{
			ID:          5,
			Title:       "Researchers demo battery breakthrough—skeptics ask: at what cost?",
			Source:      "ArcScience",
			PublishedAt: now.Add(-4 * time.Hour),
			Scores: ScoreBreakdown{
				Semantic:      0.68,
				Rerank:        0.61,
				Arousal:       47,
				RecencyWeight: 0.42,
				Diversity:     0.71,
			},
		},
	}

	raw := append([]*Headline{}, items...)
	sort.SliceStable(raw, func(i, j int) bool {
		return raw[i].PublishedAt.After(raw[j].PublishedAt)
	})

	eng := append([]*Headline{}, items...)

	f := feedModel{
		items:    items,
		rawOrder: raw,
		engOrder: eng,
		selected: 0,
		cardOpen: false,
	}

	// initial scoring with defaults
	f.Recompute(Weights{ArousalWeight: 0.35})
	return f
}

func (f *feedModel) currentSlice(mode ViewMode) []*Headline {
	if mode == ViewEngineered {
		return f.engOrder
	}
	return f.rawOrder
}

func (f *feedModel) selectedHeadline(mode ViewMode) *Headline {
	s := f.currentSlice(mode)
	if len(s) == 0 {
		return nil
	}
	if f.selected < 0 {
		f.selected = 0
	}
	if f.selected >= len(s) {
		f.selected = len(s) - 1
	}
	return s[f.selected]
}

func (f *feedModel) MoveSelection(delta int, mode ViewMode) {
	s := f.currentSlice(mode)
	if len(s) == 0 {
		return
	}
	f.selected += delta
	if f.selected < 0 {
		f.selected = 0
	}
	if f.selected >= len(s) {
		f.selected = len(s) - 1
	}
}

func (f *feedModel) ToggleCard() { f.cardOpen = !f.cardOpen }

func (f *feedModel) SetModePreserveSelection(from, to ViewMode) {
	h := f.selectedHeadline(from)
	if h == nil {
		f.selected = 0
		return
	}
	target := f.currentSlice(to)
	for i, hh := range target {
		if hh.ID == h.ID {
			f.selected = i
			return
		}
	}
	f.selected = 0
}

func (f *feedModel) Recompute(w Weights) {
	// Capture old ranks for diff indicators.
	oldRank := map[int]int{}
	for i, h := range f.engOrder {
		oldRank[h.ID] = i
	}

	// Compute engineered score (minimal version: base + arousal term).
	for _, h := range f.items {
		base := 0.70*h.Scores.Semantic + 0.30*h.Scores.Rerank
		ar := (h.Scores.Arousal / 100.0) * clamp01(w.ArousalWeight)
		h.Engineered = base + ar
	}

	// Stable sort: engineered desc; tie-break by recency.
	sort.SliceStable(f.engOrder, func(i, j int) bool {
		a, b := f.engOrder[i], f.engOrder[j]
		if a.Engineered == b.Engineered {
			return a.PublishedAt.After(b.PublishedAt)
		}
		return a.Engineered > b.Engineered
	})

	// Rank delta (oldIndex - newIndex): positive means moved up.
	for i, h := range f.engOrder {
		if old, ok := oldRank[h.ID]; ok {
			h.RankDelta = old - i
		} else {
			h.RankDelta = 0
		}
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
```

### `slider_panel.go`
```go
package main

import (
	"fmt"
	"math"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

type sliderPanel struct {
	weights Weights
	bar     progress.Model
	width   int
	focus   bool
}

func newSliderPanel() sliderPanel {
	bar := progress.New(
		progress.WithDefaultGradient(),
	)
	return sliderPanel{
		weights: Weights{ArousalWeight: 0.35},
		bar:     bar,
		width:   28,
		focus:   false,
	}
}

func (s *sliderPanel) SetWidth(w int) {
	s.width = w
	// progress width is the inner bar width (approx)
	s.bar.Width = max(10, w-8)
}

func (s *sliderPanel) SetFocus(on bool) { s.focus = on }

func (s *sliderPanel) AdjustArousal(delta float64) (changed bool) {
	before := s.weights.ArousalWeight
	s.weights.ArousalWeight = clamp01(s.weights.ArousalWeight + delta)
	return s.weights.ArousalWeight != before
}

func (s sliderPanel) View() string {
	title := neonTitleStyle.Render("WEIGHTS")
	if s.focus {
		title = neonTitleActiveStyle.Render("WEIGHTS")
	}

	val := fmt.Sprintf("%3.0f%%", math.Round(s.weights.ArousalWeight*100))
	label := lipgloss.JoinHorizontal(lipgloss.Left,
		dimLabelStyle.Render("Arousal"),
		lipgloss.NewStyle().Width(max(0, s.width-12)).Render(""),
		valueStyle.Render(val),
	)

	bar := s.bar.ViewAs(s.weights.ArousalWeight)

	hint := dimHintStyle.Render("tab: focus  ←/→: adjust")
	if s.focus {
		hint = hintActiveStyle.Render("tab: focus  ←/→: adjust")
	}

	box := panelStyle.Width(s.width).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			label,
			bar,
			"",
			hint,
		),
	)

	return box
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

### `glitch.go`
```go
package main

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func glitchTitle(title string, arousal float64, tick int, selected bool, width int) string {
	intensity := arousal / 100.0
	if intensity < 0.55 {
		// Minimal styling only.
		txt := truncate(title, width)
		if selected {
			return selectedTitleStyle.Render(txt)
		}
		return titleStyle.Render(txt)
	}

	txt := truncate(title, width)
	runes := []rune(txt)

	// Deterministic pseudo-random per tick+position+title.
	// Keep it subtle: only a few chars get distorted.
	var b strings.Builder
	for i, r := range runes {
		h := fnv.New32a()
		_, _ = h.Write([]byte(fmt.Sprintf("%d:%d:%d", tick, i, len(runes))))
		x := h.Sum32()

		distortChance := int(20 + intensity*120) // 20..140 (out of 1000 below)
		distort := int(x%1000) < distortChance

		ch := r
		if distort && r != ' ' {
			alts := []rune{'/', '\\', '█', '▓', '░', '⟟', '⟊', '⟡'}
			ch = alts[int(x)%len(alts)]
		}

		// Flicker between cyan/pink; occasionally yellow.
		var st lipgloss.Style
		switch x % 12 {
		case 0:
			st = glitchYellowStyle
		case 1, 2, 3, 4:
			st = glitchPinkStyle
		default:
			st = glitchCyanStyle
		}
		if selected {
			st = st.Copy().Background(colorBG).Bold(true)
		}
		b.WriteString(st.Render(string(ch)))
	}

	return b.String()
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}
```

### `styles.go`
```go
package main

import "github.com/charmbracelet/lipgloss"

var (
	// Theme
	colorBG    = lipgloss.Color("#0d0d0d")
	colorCyan  = lipgloss.Color("#00ff9f")
	colorPink  = lipgloss.Color("#ff0055")
	colorYellow = lipgloss.Color("#ffff00")
	colorDim   = lipgloss.Color("#3a3a3a")
	colorText  = lipgloss.Color("#d6d6d6")
)

var (
	appStyle = lipgloss.NewStyle().
		Background(colorBG).
		Foreground(colorText).
		Padding(1, 2)

	topBarStyle = lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorBG).
		Bold(true)

	tabInactiveStyle = lipgloss.NewStyle().
		Foreground(colorDim)

	tabActiveRawStyle = lipgloss.NewStyle().
		Foreground(colorYellow).
		Bold(true)

	tabActiveEngStyle = lipgloss.NewStyle().
		Foreground(colorCyan).
		Bold(true)

	panelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Padding(1, 1).
		Background(colorBG)

	neonTitleStyle = lipgloss.NewStyle().
		Foreground(colorDim).
		Bold(true)

	neonTitleActiveStyle = lipgloss.NewStyle().
		Foreground(colorCyan).
		Bold(true)

	titleStyle = lipgloss.NewStyle().
		Foreground(colorText)

	selectedTitleStyle = lipgloss.NewStyle().
		Foreground(colorYellow).
		Background(colorBG).
		Bold(true)

	valueStyle = lipgloss.NewStyle().
		Foreground(colorCyan).
		Bold(true)

	dimLabelStyle = lipgloss.NewStyle().
		Foreground(colorDim)

	dimHintStyle = lipgloss.NewStyle().
		Foreground(colorDim)

	hintActiveStyle = lipgloss.NewStyle().
		Foreground(colorPink)

	diffUpStyle = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	diffDownStyle = lipgloss.NewStyle().Foreground(colorPink).Bold(true)

	// Glitch styles
	glitchCyanStyle = lipgloss.NewStyle().Foreground(colorCyan)
	glitchPinkStyle = lipgloss.NewStyle().Foreground(colorPink)
	glitchYellowStyle = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)

	cardStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Padding(1, 1).
		Background(colorBG)
)
```

### `app_model.go`
```go
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

type focusArea int

const (
	focusList focusArea = iota
	focusSidebar
)

type tickMsg struct{}
type loadedMsg struct{}

type appModel struct {
	mode  ViewMode
	focus focusArea

	w, h int

	loading bool
	err     error

	tick int

	spin spinner.Model

	feed   feedModel
	slider sliderPanel
}

func newAppModel() appModel {
	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(colorCyan)

	f := newFeedModel(time.Now())
	s := newSliderPanel()

	return appModel{
		mode:    ViewEngineered,
		focus:   focusList,
		loading: true,
		spin:    sp,
		feed:    f,
		slider:  s,
	}
}

func (m appModel) Init() tea.Cmd {
	return tea.Batch(
		m.spin.Tick,
		tea.Tick(450*time.Millisecond, func(time.Time) tea.Msg { return loadedMsg{} }),
		tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} }),
	)
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		sideW := clampInt(m.w/3, 26, 38)
		m.slider.SetWidth(sideW - 4)
		return m, nil

	case loadedMsg:
		m.loading = false
		// initial compute already done, but ensure consistency with slider weights.
		m.feed.Recompute(m.slider.weights)
		return m, nil

	case tickMsg:
		m.tick++
		return m, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })

	case tea.KeyMsg:
		if m.loading {
			// allow quit during loading
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				return m, tea.Quit
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit

		case "tab":
			if m.focus == focusList {
				m.focus = focusSidebar
				m.slider.SetFocus(true)
			} else {
				m.focus = focusList
				m.slider.SetFocus(false)
			}
			return m, nil

		case "r":
			if m.mode != ViewRaw {
				m.feed.SetModePreserveSelection(m.mode, ViewRaw)
				m.mode = ViewRaw
			}
			return m, nil

		case "e":
			if m.mode != ViewEngineered {
				m.feed.SetModePreserveSelection(m.mode, ViewEngineered)
				m.mode = ViewEngineered
			}
			return m, nil

		case "up", "k":
			if m.focus == focusList {
				m.feed.MoveSelection(-1, m.mode)
			}
			return m, nil

		case "down", "j":
			if m.focus == focusList {
				m.feed.MoveSelection(+1, m.mode)
			}
			return m, nil

		case "enter", " ":
			if m.focus == focusList && m.mode == ViewEngineered {
				m.feed.ToggleCard()
			}
			return m, nil

		case "left", "h":
			if m.focus == focusSidebar && m.mode == ViewEngineered {
				if m.slider.AdjustArousal(-0.05) {
					m.feed.Recompute(m.slider.weights)
				}
			}
			return m, nil

		case "right", "l":
			if m.focus == focusSidebar && m.mode == ViewEngineered {
				if m.slider.AdjustArousal(+0.05) {
					m.feed.Recompute(m.slider.weights)
				}
			}
			return m, nil
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m appModel) View() string {
	if m.w == 0 {
		// initial render before first WindowSizeMsg
		m.w, m.h = 100, 30
	}

	if m.loading {
		return appStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left,
				topBarStyle.Render("NEON FEED  //  initializing"),
				"",
				fmt.Sprintf("%s %s", m.spin.View(), "Syncing headlines…"),
			),
		)
	}
	if m.err != nil {
		return appStyle.Render("error: " + m.err.Error())
	}

	sideW := clampInt(m.w/3, 26, 38)
	mainW := m.w - sideW - 4 // padding/gaps

	header := m.renderTopBar()
	left := m.renderFeed(mainW)
	right := m.renderSidebar(sideW)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	help := dimHintStyle.Render("r: raw  e: engineered  tab: focus  ↑↓: move  enter: transparency  q: quit")

	return appStyle.Width(m.w).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			header,
			"",
			body,
			"",
			help,
		),
	)
}

func (m appModel) renderTopBar() string {
	raw := tabInactiveStyle.Render("[r] RAW")
	eng := tabInactiveStyle.Render("[e] ENGINEERED")

	if m.mode == ViewRaw {
		raw = tabActiveRawStyle.Render("[r] RAW")
	} else {
		eng = tabActiveEngStyle.Render("[e] ENGINEERED")
	}

	line := lipgloss.JoinHorizontal(lipgloss.Left,
		"NEON FEED",
		"  ",
		raw,
		"  ",
		eng,
		"  ",
		dimLabelStyle.Render("// cyber-noir transparent ranking"),
	)

	return topBarStyle.Render(line)
}

func (m appModel) renderFeed(width int) string {
	var b strings.Builder

	listPanel := panelStyle.Width(width).Render(m.renderFeedInner(width - 4))
	b.WriteString(listPanel)

	// Transparency card below list in engineered mode (minimal).
	if m.mode == ViewEngineered && m.feed.cardOpen {
		h := m.feed.selectedHeadline(m.mode)
		if h != nil {
			b.WriteString("\n\n")
			b.WriteString(m.renderTransparencyCard(width, h))
		}
	}

	return b.String()
}

func (m appModel) renderFeedInner(width int) string {
	items := m.feed.currentSlice(m.mode)
	if len(items) == 0 {
		return dimHintStyle.Render("no headlines")
	}

	var out strings.Builder
	title := neonTitleStyle.Render("FEED")
	if m.focus == focusList {
		title = neonTitleActiveStyle.Render("FEED")
	}
	out.WriteString(title + "\n\n")

	for i, h := range items {
		sel := (i == m.feed.selected)
		prefix := "  "
		if sel {
			prefix = selectedTitleStyle.Render("> ")
		}

		diff := ""
		if m.mode == ViewEngineered {
			if h.RankDelta > 0 {
				diff = diffUpStyle.Render(fmt.Sprintf(" ↑%d", h.RankDelta))
			} else if h.RankDelta < 0 {
				diff = diffDownStyle.Render(fmt.Sprintf(" ↓%d", -h.RankDelta))
			}
		}

		// allocate room for prefix + diff + small source tag
		src := dimLabelStyle.Render("  " + h.Source)
		avail := max(10, width-2-6) // rough, keep simple for minimal version

		var title string
		if m.mode == ViewEngineered {
			title = glitchTitle(h.Title, h.Scores.Arousal, m.tick, sel, avail)
		} else {
			tt := truncate(h.Title, avail)
			if sel {
				title = selectedTitleStyle.Render(tt)
			} else {
				title = titleStyle.Render(tt)
			}
		}

		out.WriteString(prefix + title + src + diff + "\n")
	}

	return out.String()
}

func (m appModel) renderSidebar(width int) string {
	// In raw mode, sidebar is informational for now.
	if m.mode == ViewRaw {
		box := panelStyle.Width(width).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				neonTitleStyle.Render("RAW MODE"),
				"",
				dimHintStyle.Render("Chronological RSS.\nNo ranking.\nNo weighting."),
				"",
				dimHintStyle.Render("press e to switch"),
			),
		)
		return box
	}

	return m.slider.View()
}

func (m appModel) renderTransparencyCard(width int, h *Headline) string {
	// Minimal “transparency card” fields; expand later with all weights.
	arousal := h.Scores.Arousal
	fire := arousalToFire(arousal)

	body := fmt.Sprintf(
		"%s\n\n%s %0.2f\n%s %0.2f\n%s %0.0f/100  %s\n%s %0.2f\n\n%s %0.3f",
		neonTitleActiveStyle.Render("TRANSPARENCY"),
		dimLabelStyle.Render("Semantic match:"),
		h.Scores.Semantic,
		dimLabelStyle.Render("Reranker score:"),
		h.Scores.Rerank,
		dimLabelStyle.Render("Arousal proxy:"),
		arousal,
		valueStyle.Render(fire),
		dimLabelStyle.Render("Arousal weight:"),
		m.slider.weights.ArousalWeight,
		valueStyle.Render("Final engineered:"),
		h.Engineered,
	)

	return cardStyle.Width(width).Render(body)
}

func arousalToFire(a float64) string {
	switch {
	case a >= 85:
		return "🔥🔥🔥🔥"
	case a >= 70:
		return "🔥🔥🔥"
	case a >= 50:
		return "🔥🔥"
	case a >= 30:
		return "🔥"
	default:
		return "·"
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
```

---

## 5) Glitch / cyber-noir effects: achievable vs gimmicky

### Actually achievable (looks good in terminals)
- **Neon palette + high contrast** (cyan/pink/yellow on near-black) via Lip Gloss.
- **Deterministic flicker**: alternate cyan/pink emphasis per character and per tick.
- **Subtle distortion**: replace a *few* glyphs with block characters (`░▓█`) or slashes.
- **Scanline feel**: occasional dim horizontal rules / faint separators, or alternating dim background rows (very subtle).
- **Glow illusion**: bold + bright foreground + occasional secondary outline (render same text twice with offset is usually too messy, but you can “hint” glow with borders and bright accents).

### Gimmicky / risky (often hurts UX)
- Heavy full-line randomization (destroys readability).
- Fast animation ticks (<50ms) causing flicker and CPU usage.
- Overusing background colors (some terminals render them harshly, and selection becomes unreadable).
- True “transparency” (terminal can’t alpha-blend; you can only *fake* it with darker shades).

### Practical recommendation
- Tie glitch intensity to arousal, but cap it: only distort up to ~2–4 characters per headline per frame.
- Keep animation tick ~100–150ms.
- Apply glitch only in Engineered mode and only for “high arousal” items; keep Raw mode clean.

---

If you want the next iteration, I’d add (in order): a real list viewport (only render visible rows), 20–30 mock headlines, the full slider set, and a proper right-side collapsible “Weights” panel + “Transparency” panel (two stacked boxes) with smooth resize behavior.
