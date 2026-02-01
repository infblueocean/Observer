# GPT-5 Response — Round 2: Implementation Details

**Model:** gpt5
**Date:** 2026-01-31

---

Below is **complete, compilable Go code** for the decided architecture under `internal/ui/media/`. It implements the dual “Engineered vs Raw” feed, slider sidebar, transparency card, deterministic glitch engine, responsive layout, and mock data.

> Package name is `media`. It depends only on Bubble Tea + Lip Gloss + Bubbles Progress (+ runewidth for precise truncation).

---

## `internal/ui/media/styles.go`
```go
package media

import "github.com/charmbracelet/lipgloss"

// Theme constants (decided)
const (
	ColorBG     = "#0d0d0d"
	ColorCyan   = "#00ff9f"
	ColorPink   = "#ff0055"
	ColorYellow = "#ffff00"
	ColorDim    = "#3a3a3a"
	ColorText   = "#d6d6d6"
)

type Styles struct {
	// Global surfaces
	AppBG lipgloss.Style
	Frame lipgloss.Style

	// Header / footer
	Header      lipgloss.Style
	HeaderTitle lipgloss.Style
	Badge       lipgloss.Style
	BadgeAlt    lipgloss.Style
	Help        lipgloss.Style
	HelpKey     lipgloss.Style
	HelpDim     lipgloss.Style

	// Feed
	FeedBox          lipgloss.Style
	FeedRow          lipgloss.Style
	FeedRowSelected  lipgloss.Style
	FeedMeta         lipgloss.Style
	FeedRank         lipgloss.Style
	FeedDeltaUp      lipgloss.Style
	FeedDeltaDown    lipgloss.Style
	FeedDeltaFlat    lipgloss.Style
	FeedModeRaw      lipgloss.Style
	FeedModeEng      lipgloss.Style
	FeedFocusOutline lipgloss.Style
	SideFocusOutline lipgloss.Style

	// Sidebar
	SidebarBox    lipgloss.Style
	SidebarTitle  lipgloss.Style
	SliderLabel   lipgloss.Style
	SliderValue   lipgloss.Style
	SliderActive  lipgloss.Style
	SliderInactive lipgloss.Style

	// Card
	CardBox        lipgloss.Style
	CardTitle      lipgloss.Style
	CardDim        lipgloss.Style
	CardMetricName lipgloss.Style
	CardMetricVal  lipgloss.Style
	BarFillCyan    lipgloss.Style
	BarFillPink    lipgloss.Style
	BarFillYellow  lipgloss.Style
	BarEmpty       lipgloss.Style
}

func DefaultStyles() Styles {
	bg := lipgloss.Color(ColorBG)
	cyan := lipgloss.Color(ColorCyan)
	pink := lipgloss.Color(ColorPink)
	yellow := lipgloss.Color(ColorYellow)
	dim := lipgloss.Color(ColorDim)
	text := lipgloss.Color(ColorText)

	frame := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dim).
		Padding(0, 1)

	return Styles{
		AppBG: lipgloss.NewStyle().Background(bg).Foreground(text),
		Frame: frame,

		Header: lipgloss.NewStyle().
			Background(bg).
			Foreground(text).
			Padding(0, 1),

		HeaderTitle: lipgloss.NewStyle().Foreground(cyan).Bold(true),
		Badge:       lipgloss.NewStyle().Foreground(bg).Background(cyan).Padding(0, 1).Bold(true),
		BadgeAlt:    lipgloss.NewStyle().Foreground(bg).Background(pink).Padding(0, 1).Bold(true),

		Help:    lipgloss.NewStyle().Foreground(text).Background(bg).Padding(0, 1),
		HelpKey: lipgloss.NewStyle().Foreground(cyan).Bold(true),
		HelpDim: lipgloss.NewStyle().Foreground(dim),

		FeedBox: frame.Copy(),
		FeedRow: lipgloss.NewStyle().
			Foreground(text),
		FeedRowSelected: lipgloss.NewStyle().
			Foreground(text).
			Background(lipgloss.Color("#141414")).
			Bold(true),

		FeedMeta: lipgloss.NewStyle().Foreground(dim),
		FeedRank: lipgloss.NewStyle().Foreground(yellow).Bold(true),

		FeedDeltaUp:   lipgloss.NewStyle().Foreground(cyan).Bold(true),
		FeedDeltaDown: lipgloss.NewStyle().Foreground(pink).Bold(true),
		FeedDeltaFlat: lipgloss.NewStyle().Foreground(dim),

		FeedModeRaw: lipgloss.NewStyle().Foreground(yellow).Bold(true),
		FeedModeEng: lipgloss.NewStyle().Foreground(cyan).Bold(true),

		FeedFocusOutline: frame.Copy().BorderForeground(cyan),
		SideFocusOutline: frame.Copy().BorderForeground(pink),

		SidebarBox:   frame.Copy(),
		SidebarTitle: lipgloss.NewStyle().Foreground(pink).Bold(true),

		SliderLabel:    lipgloss.NewStyle().Foreground(text),
		SliderValue:    lipgloss.NewStyle().Foreground(cyan).Bold(true),
		SliderActive:   lipgloss.NewStyle().Foreground(cyan).Bold(true),
		SliderInactive: lipgloss.NewStyle().Foreground(dim),

		CardBox:   frame.Copy(),
		CardTitle: lipgloss.NewStyle().Foreground(yellow).Bold(true),
		CardDim:   lipgloss.NewStyle().Foreground(dim),

		CardMetricName: lipgloss.NewStyle().Foreground(text),
		CardMetricVal:  lipgloss.NewStyle().Foreground(cyan).Bold(true),

		BarFillCyan:   lipgloss.NewStyle().Foreground(cyan),
		BarFillPink:   lipgloss.NewStyle().Foreground(pink),
		BarFillYellow: lipgloss.NewStyle().Foreground(yellow),
		BarEmpty:      lipgloss.NewStyle().Foreground(dim),
	}
}
```

---

## `internal/ui/media/headline.go`
```go
package media

import (
	"fmt"
	"hash/fnv"
	"math"
	"time"
)

type Headline struct {
	ID    string
	Title string
	Source string
	URL   string

	PublishedAt time.Time

	// Model inputs (0..1 unless noted)
	Semantic   float64 // 0..1
	Rerank     float64 // 0..1
	Arousal    float64 // 0..1
	Negativity float64 // 0..1
	Curiosity  float64 // 0..1
	Diversity  float64 // 0..1 (per-item proxy from upstream; displayed in card)

	// Derived each recompute
	TitleHash  uint32
	FinalScore float64
	Breakdown  ScoreBreakdown

	// Engineered-only ranking metadata
	EngRank   int
	RankDelta int // oldIndex - newIndex (positive means moved up)
}

type ScoreBreakdown struct {
	// Inputs
	Semantic   float64
	Rerank     float64
	Arousal    float64
	Negativity float64
	Curiosity  float64
	Diversity  float64

	// Time
	HoursOld     float64
	RecencyDecay float64

	// Components
	Base          float64
	ArousalBoost  float64
	NegBoost      float64
	CuriousBoost  float64
	PreRecency    float64
	FinalScore    float64

	// Weights snapshot
	Weights Weights
}

type Weights struct {
	// Slider 1: 0..100 (%)
	ArousalWeight float64

	// Slider 2: 0.5x..2.0x
	NegativityBias float64

	// Slider 3: 0..80 (%)
	CuriosityGap float64

	// Slider 4: 2h..72h
	RecencyHalfLifeHours float64
}

func DefaultWeights() Weights {
	return Weights{
		ArousalWeight:        55,   // %
		NegativityBias:       1.20, // x
		CuriosityGap:         35,   // %
		RecencyHalfLifeHours: 18,   // hours
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (h *Headline) EnsureHash() {
	if h.TitleHash != 0 {
		return
	}
	h.TitleHash = HashTitle(h.Title)
}

func HashTitle(title string) uint32 {
	f := fnv.New32a()
	_, _ = f.Write([]byte(title))
	return f.Sum32()
}

// ComputeFinal applies the decided exponential-decay scoring, plus the decided
// Curiosity slider as an additive boost term (kept separate in Breakdown).
//
// Decided core:
// base := 0.4*Semantic + 0.4*Rerank
// arousalBoost := Arousal * (ArousalWeight / 100.0)
// negBoost := max(0, (Negativity - 0.5)) * (NegativityBias - 1.0)
// recencyDecay := math.Pow(0.5, hoursOld / RecencyHalfLife)
// FinalScore = (base + arousalBoost + negBoost) * recencyDecay
//
// Added (because a 4th content-weight slider was decided):
// curiousBoost := Curiosity * (CuriosityGap / 100.0) * 0.35
// FinalScore = (base + arousalBoost + negBoost + curiousBoost) * recencyDecay
func (h *Headline) ComputeFinal(w Weights, now time.Time) ScoreBreakdown {
	h.EnsureHash()

	semantic := clamp(h.Semantic, 0, 1)
	rerank := clamp(h.Rerank, 0, 1)
	arousal := clamp(h.Arousal, 0, 1)
	neg := clamp(h.Negativity, 0, 1)
	cur := clamp(h.Curiosity, 0, 1)

	halfLife := clamp(w.RecencyHalfLifeHours, 2, 72)
	hoursOld := now.Sub(h.PublishedAt).Hours()
	if hoursOld < 0 {
		hoursOld = 0
	}

	base := 0.4*semantic + 0.4*rerank
	arousalBoost := arousal * (clamp(w.ArousalWeight, 0, 100) / 100.0)
	negBoost := math.Max(0, (neg-0.5)) * (clamp(w.NegativityBias, 0.5, 2.0) - 1.0)

	// Curiosity gap: kept deliberately smaller than arousal by scaling.
	curiousBoost := cur * (clamp(w.CuriosityGap, 0, 80) / 100.0) * 0.35

	recencyDecay := math.Pow(0.5, hoursOld/halfLife)
	preRecency := base + arousalBoost + negBoost + curiousBoost
	final := preRecency * recencyDecay

	return ScoreBreakdown{
		Semantic: semantic, Rerank: rerank, Arousal: arousal, Negativity: neg,
		Curiosity: cur, Diversity: clamp(h.Diversity, 0, 1),

		HoursOld: hoursOld, RecencyDecay: recencyDecay,

		Base: base, ArousalBoost: arousalBoost, NegBoost: negBoost,
		CuriousBoost: curiousBoost,

		PreRecency: preRecency,
		FinalScore: final,
		Weights:    w,
	}
}

func (w Weights) String() string {
	return fmt.Sprintf("Arousal %.0f%%  Neg %.2fx  Cur %.0f%%  HL %.0fh",
		w.ArousalWeight, w.NegativityBias, w.CuriosityGap, w.RecencyHalfLifeHours)
}

// MockHeadlines returns 29 cyber-noir themed headlines with clustered score distributions
// and timestamps spanning 0..72 hours old.
func MockHeadlines(now time.Time) []*Headline {
	ago := func(h float64) time.Time { return now.Add(-time.Duration(h * float64(time.Hour))) }

	// Clusters:
	// - High arousal/neg: raids, leaks, violence
	// - Mid: corporate intrigue, policy
	// - Low: research, standards, quiet patches
	return []*Headline{
		{ID: "hx-0001", Title: "Neon district blackout traced to a misfired mesh update", Source: "ArcLight Wire", URL: "https://example.test/0001", PublishedAt: ago(1.2),
			Semantic: 0.71, Rerank: 0.69, Arousal: 0.62, Negativity: 0.58, Curiosity: 0.44, Diversity: 0.72},
		{ID: "hx-0002", Title: "Ghost-wallet syndicate drains commuter cards via ‘silent tap’ exploit", Source: "Night Market Ledger", URL: "https://example.test/0002", PublishedAt: ago(3.8),
			Semantic: 0.77, Rerank: 0.81, Arousal: 0.86, Negativity: 0.72, Curiosity: 0.66, Diversity: 0.63},
		{ID: "hx-0003", Title: "City council votes to license drones; activists call it airborne probation", Source: "Civic Hex", URL: "https://example.test/0003", PublishedAt: ago(11.0),
			Semantic: 0.64, Rerank: 0.62, Arousal: 0.41, Negativity: 0.53, Curiosity: 0.38, Diversity: 0.55},
		{ID: "hx-0004", Title: "Leaked: ‘Project Glassline’ vendor memo details emotion-targeting ad slots", Source: "Spillbyte", URL: "https://example.test/0004", PublishedAt: ago(6.5),
			Semantic: 0.83, Rerank: 0.79, Arousal: 0.74, Negativity: 0.61, Curiosity: 0.71, Diversity: 0.68},
		{ID: "hx-0005", Title: "Kernel patch closes side-channel in arcade cabinets; speedrunners furious", Source: "Patch Ritual", URL: "https://example.test/0005", PublishedAt: ago(0.6),
			Semantic: 0.58, Rerank: 0.55, Arousal: 0.30, Negativity: 0.28, Curiosity: 0.49, Diversity: 0.61},
		{ID: "hx-0006", Title: "Corporate shrine defaced with QR curses; employees report ‘dream spam’", Source: "Neon Courier", URL: "https://example.test/0006", PublishedAt: ago(9.2),
			Semantic: 0.69, Rerank: 0.73, Arousal: 0.78, Negativity: 0.65, Curiosity: 0.59, Diversity: 0.77},
		{ID: "hx-0007", Title: "Data-broker consortium proposes ‘privacy amnesty’—for a monthly fee", Source: "Signal & Soot", URL: "https://example.test/0007", PublishedAt: ago(21.0),
			Semantic: 0.74, Rerank: 0.70, Arousal: 0.48, Negativity: 0.57, Curiosity: 0.42, Diversity: 0.46},
		{ID: "hx-0008", Title: "Underpass clinic uses open-source diagnostic model; regulator calls it ‘unlicensed medicine’", Source: "Open Alley", URL: "https://example.test/0008", PublishedAt: ago(33.0),
			Semantic: 0.67, Rerank: 0.63, Arousal: 0.36, Negativity: 0.46, Curiosity: 0.33, Diversity: 0.74},
		{ID: "hx-0009", Title: "Reranker wars: two labs accuse each other of training on stolen query logs", Source: "Modelwatch", URL: "https://example.test/0009", PublishedAt: ago(14.5),
			Semantic: 0.81, Rerank: 0.77, Arousal: 0.52, Negativity: 0.55, Curiosity: 0.47, Diversity: 0.64},
		{ID: "hx-0010", Title: "Metro AI ‘optimizes’ patrol routes; residents map the bias in real time", Source: "Civic Hex", URL: "https://example.test/0010", PublishedAt: ago(4.1),
			Semantic: 0.79, Rerank: 0.76, Arousal: 0.57, Negativity: 0.60, Curiosity: 0.39, Diversity: 0.51},
		{ID: "hx-0011", Title: "Someone is selling ‘consent tokens’ that expire mid-conversation", Source: "Night Market Ledger", URL: "https://example.test/0011", PublishedAt: ago(2.3),
			Semantic: 0.72, Rerank: 0.75, Arousal: 0.70, Negativity: 0.63, Curiosity: 0.62, Diversity: 0.58},
		{ID: "hx-0012", Title: "Transit cameras get firmware with ‘behavioral anomaly’ flags; union demands audit", Source: "ArcLight Wire", URL: "https://example.test/0012", PublishedAt: ago(18.2),
			Semantic: 0.76, Rerank: 0.72, Arousal: 0.49, Negativity: 0.57, Curiosity: 0.41, Diversity: 0.59},
		{ID: "hx-0013", Title: "Rain-slicked dev bar hosts ‘zero-day poetry’ night; prizes paid in bug bounties", Source: "Neon Courier", URL: "https://example.test/0013", PublishedAt: ago(27.0),
			Semantic: 0.54, Rerank: 0.52, Arousal: 0.33, Negativity: 0.22, Curiosity: 0.56, Diversity: 0.82},
		{ID: "hx-0014", Title: "Grid operator confirms ransomware note contained a working load-shed script", Source: "Spillbyte", URL: "https://example.test/0014", PublishedAt: ago(7.7),
			Semantic: 0.84, Rerank: 0.83, Arousal: 0.90, Negativity: 0.80, Curiosity: 0.61, Diversity: 0.66},
		{ID: "hx-0015", Title: "New TLS draft proposes ‘deniable handshake’ mode; cryptographers split", Source: "Patch Ritual", URL: "https://example.test/0015", PublishedAt: ago(44.0),
			Semantic: 0.73, Rerank: 0.70, Arousal: 0.28, Negativity: 0.20, Curiosity: 0.31, Diversity: 0.69},
		{ID: "hx-0016", Title: "Influencer’s ‘clean-room’ stream reveals ad-injection inside smart mirrors", Source: "Signal & Soot", URL: "https://example.test/0016", PublishedAt: ago(5.4),
			Semantic: 0.68, Rerank: 0.71, Arousal: 0.66, Negativity: 0.52, Curiosity: 0.73, Diversity: 0.47},
		{ID: "hx-0017", Title: "Courier cooperative routes around corporate geofences using analog maps", Source: "Open Alley", URL: "https://example.test/0017", PublishedAt: ago(12.4),
			Semantic: 0.61, Rerank: 0.59, Arousal: 0.37, Negativity: 0.33, Curiosity: 0.29, Diversity: 0.76},
		{ID: "hx-0018", Title: "Court filing: ‘predictive eviction’ model mislabeled thousands as high-risk", Source: "Civic Hex", URL: "https://example.test/0018", PublishedAt: ago(29.0),
			Semantic: 0.82, Rerank: 0.78, Arousal: 0.55, Negativity: 0.69, Curiosity: 0.35, Diversity: 0.50},
		{ID: "hx-0019", Title: "Firmware update bricks knockoff respirators; blame falls on unsigned drivers", Source: "ArcLight Wire", URL: "https://example.test/0019", PublishedAt: ago(8.9),
			Semantic: 0.66, Rerank: 0.64, Arousal: 0.59, Negativity: 0.62, Curiosity: 0.40, Diversity: 0.62},
		{ID: "hx-0020", Title: "Black-bag raid hits ‘packet chapel’ hackerspace; servers seized, cats unharmed", Source: "Night Market Ledger", URL: "https://example.test/0020", PublishedAt: ago(1.9),
			Semantic: 0.79, Rerank: 0.82, Arousal: 0.92, Negativity: 0.77, Curiosity: 0.58, Diversity: 0.70},
		{ID: "hx-0021", Title: "Startup claims ‘anti-glitch font’ reduces phishing success by 12%", Source: "Modelwatch", URL: "https://example.test/0021", PublishedAt: ago(52.0),
			Semantic: 0.57, Rerank: 0.56, Arousal: 0.25, Negativity: 0.24, Curiosity: 0.37, Diversity: 0.65},
		{ID: "hx-0022", Title: "Underground ISP offers ‘fog routing’ through laundromat routers", Source: "Signal & Soot", URL: "https://example.test/0022", PublishedAt: ago(16.1),
			Semantic: 0.63, Rerank: 0.61, Arousal: 0.47, Negativity: 0.39, Curiosity: 0.60, Diversity: 0.79},
		{ID: "hx-0023", Title: "Museum exhibit lets visitors ‘hear’ encryption keys; audiophiles debate ethics", Source: "Open Alley", URL: "https://example.test/0023", PublishedAt: ago(36.0),
			Semantic: 0.55, Rerank: 0.53, Arousal: 0.29, Negativity: 0.18, Curiosity: 0.64, Diversity: 0.83},
		{ID: "hx-0024", Title: "Report: recruiter bots quietly lower offers when your wearable shows stress", Source: "Spillbyte", URL: "https://example.test/0024", PublishedAt: ago(10.7),
			Semantic: 0.80, Rerank: 0.79, Arousal: 0.63, Negativity: 0.66, Curiosity: 0.52, Diversity: 0.60},
		{ID: "hx-0025", Title: "Patch Tuesday: the boring fixes that stopped a spectacular chain reaction", Source: "Patch Ritual", URL: "https://example.test/0025", PublishedAt: ago(22.5),
			Semantic: 0.62, Rerank: 0.60, Arousal: 0.21, Negativity: 0.19, Curiosity: 0.26, Diversity: 0.57},
		{ID: "hx-0026", Title: "Pentest crew finds ‘admin’ backdoor in streetlight controller; vendor calls it a feature", Source: "ArcLight Wire", URL: "https://example.test/0026", PublishedAt: ago(5.9),
			Semantic: 0.75, Rerank: 0.76, Arousal: 0.71, Negativity: 0.64, Curiosity: 0.55, Diversity: 0.62},
		{ID: "hx-0027", Title: "Deepfake hotline flooded after scandal: ‘The voice was mine, the words weren’t’", Source: "Neon Courier", URL: "https://example.test/0027", PublishedAt: ago(2.8),
			Semantic: 0.78, Rerank: 0.74, Arousal: 0.81, Negativity: 0.73, Curiosity: 0.49, Diversity: 0.67},
		{ID: "hx-0028", Title: "Researchers publish ‘lattice lantern’ proof; nobody understands it, but it’s beautiful", Source: "Modelwatch", URL: "https://example.test/0028", PublishedAt: ago(61.0),
			Semantic: 0.69, Rerank: 0.66, Arousal: 0.18, Negativity: 0.12, Curiosity: 0.35, Diversity: 0.71},
		{ID: "hx-0029", Title: "Street medics swap to e-ink protocols after RF jammers return to the docks", Source: "Open Alley", URL: "https://example.test/0029", PublishedAt: ago(70.0),
			Semantic: 0.60, Rerank: 0.58, Arousal: 0.42, Negativity: 0.49, Curiosity: 0.32, Diversity: 0.78},
	}
}
```

---

## `internal/ui/media/glitch.go`
```go
package media

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Deterministic per-character distortion:
// - Decision uses FNV-32a over (titleHash, tick, charPos)
// - Arousal threshold: 55%
// - Intensity scales linearly above threshold
// - Only call for visible rows (handled by feed renderer)

const (
	glitchThreshold = 0.55
)

var (
	// Glyph pool: techno-noir terminal noise, box drawing, math symbols.
	glitchGlyphs = []rune("░▒▓█▌▐▀▄▖▗▘▙▚▛▜▝▞▟─━┄┅┈┉╌╍╎╏┆┇┊┋⎯⎽⎼⎻≋≈≣≡⋯⋰⋱⟂⟄⟟⟐⟡◇◆○●◌◎◍◐◑◒◓◔◕")
)

// GlitchText returns a styled string. It applies deterministic per-character
// substitution and color flicker (cyan/pink/yellow) when arousal is high.
func GlitchText(text string, tick uint64, titleHash uint32, arousal float64, st Styles) string {
	if arousal < glitchThreshold || len(text) == 0 {
		return text
	}

	intensity := (arousal - glitchThreshold) / (1.0 - glitchThreshold)
	intensity = clamp(intensity, 0, 1)

	// Probability of glitch per character.
	// Kept low to avoid destroying readability; increases with intensity.
	pGlitch := 0.03 + 0.14*intensity

	// Probability of color flicker even without substitution (slight shimmer).
	pFlicker := 0.02 + 0.10*intensity

	cCyan := lipgloss.Color(ColorCyan)
	cPink := lipgloss.Color(ColorPink)
	cYellow := lipgloss.Color(ColorYellow)

	// Prebuild styles (avoid allocating per character).
	sCyan := lipgloss.NewStyle().Foreground(cCyan)
	sPink := lipgloss.NewStyle().Foreground(cPink)
	sYellow := lipgloss.NewStyle().Foreground(cYellow)

	var b strings.Builder
	b.Grow(len(text) + 16)

	// Iterate by rune; charPos is rune index.
	pos := 0
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		ch := r
		h := fnv32aTriplet(titleHash, uint32(tick), uint32(pos))

		// Decide substitution.
		if u01(h) < pGlitch {
			ch = selectGlitchRune(h, r)
		}

		// Decide flicker color.
		if u01(rotl32(h, 11)) < pFlicker || ch != r {
			switch colorPick(rotl32(h, 19)) {
			case 0:
				b.WriteString(sCyan.Render(string(ch)))
			case 1:
				b.WriteString(sPink.Render(string(ch)))
			default:
				b.WriteString(sYellow.Render(string(ch)))
			}
		} else {
			b.WriteRune(ch)
		}

		text = text[size:]
		pos++
	}

	return b.String()
}

func selectGlitchRune(h uint32, orig rune) rune {
	// Sometimes keep alphanumerics as-is to preserve legibility.
	// Otherwise pick from pool.
	if (orig >= 'a' && orig <= 'z') || (orig >= 'A' && orig <= 'Z') || (orig >= '0' && orig <= '9') {
		// 30% chance to keep.
		if u01(h) < 0.30 {
			return orig
		}
	}
	return glitchGlyphs[int(rotl32(h, 7))%len(glitchGlyphs)]
}

// colorPick chooses cyan/pink/yellow with a fixed distribution.
func colorPick(h uint32) int {
	// 0..99
	n := int(h % 100)
	switch {
	case n < 55:
		return 0 // cyan most common
	case n < 80:
		return 1 // pink
	default:
		return 2 // yellow
	}
}

func rotl32(x uint32, r uint) uint32 { return (x << r) | (x >> (32 - r)) }

// u01 maps uint32 -> [0,1)
func u01(x uint32) float64 {
	// 2^-32
	return float64(x) * (1.0 / 4294967296.0)
}

func fnv32aTriplet(a, b, c uint32) uint32 {
	// FNV-1a over 12 bytes.
	var buf [12]byte
	binary.LittleEndian.PutUint32(buf[0:4], a)
	binary.LittleEndian.PutUint32(buf[4:8], b)
	binary.LittleEndian.PutUint32(buf[8:12], c)

	h := fnv.New32a()
	_, _ = h.Write(buf[:])
	return h.Sum32()
}

// (Optional) utility used by card rendering for mapping to 0..1
func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}
```

---

## `internal/ui/media/feed_model.go`
```go
package media

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type ViewMode int

const (
	ModeRaw ViewMode = iota
	ModeEngineered
)

func (m ViewMode) String() string {
	switch m {
	case ModeEngineered:
		return "ENGINEERED"
	default:
		return "RAW"
	}
}

type FeedModel struct {
	Items    []*Headline // master set (shared pointers)
	rawOrder []*Headline
	engOrder []*Headline

	mode ViewMode

	// Cursor is index within current order.
	cursor int

	// Scroll offset for viewport.
	offset int

	// Layout
	width      int
	listHeight int // number of rows visible in list

	// Ranking metadata (engineered recompute)
	lastWeights Weights
	lastNow     time.Time
}

func NewFeedModel(headlines []*Headline) FeedModel {
	items := make([]*Headline, 0, len(headlines))
	for _, h := range headlines {
		if h == nil {
			continue
		}
		h.EnsureHash()
		items = append(items, h)
	}

	fm := FeedModel{
		Items: items,
		mode:  ModeEngineered,
	}
	fm.buildRawOrder()
	// Start engineered order same as raw until first recompute.
	fm.engOrder = append([]*Headline(nil), fm.rawOrder...)
	return fm
}

func (m *FeedModel) SetModePreserveSelection(mode ViewMode) {
	if m.mode == mode {
		return
	}
	selectedID := ""
	if cur := m.CursorHeadline(); cur != nil {
		selectedID = cur.ID
	}
	m.mode = mode

	order := m.currentOrder()
	if len(order) == 0 {
		m.cursor, m.offset = 0, 0
		return
	}

	// Preserve selection by headline ID.
	if selectedID != "" {
		for i, h := range order {
			if h != nil && h.ID == selectedID {
				m.cursor = i
				m.ensureVisible()
				return
			}
		}
	}

	// Fallback: clamp cursor.
	m.cursor = clampInt(m.cursor, 0, len(order)-1)
	m.ensureVisible()
}

func (m *FeedModel) Mode() ViewMode { return m.mode }

func (m *FeedModel) SetSize(width, listHeight int) {
	if width < 0 {
		width = 0
	}
	if listHeight < 0 {
		listHeight = 0
	}
	m.width = width
	m.listHeight = listHeight
	m.ensureVisible()
}

func (m *FeedModel) CursorHeadline() *Headline {
	order := m.currentOrder()
	if len(order) == 0 {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(order) {
		return nil
	}
	return order[m.cursor]
}

func (m *FeedModel) MoveCursor(delta int) {
	order := m.currentOrder()
	if len(order) == 0 {
		return
	}
	m.cursor = clampInt(m.cursor+delta, 0, len(order)-1)
	m.ensureVisible()
}

func (m *FeedModel) Page(deltaPages int) {
	order := m.currentOrder()
	if len(order) == 0 {
		return
	}
	step := maxInt(1, m.listHeight)
	m.cursor = clampInt(m.cursor+deltaPages*step, 0, len(order)-1)
	m.ensureVisible()
}

func (m *FeedModel) JumpHome() {
	if len(m.currentOrder()) == 0 {
		return
	}
	m.cursor = 0
	m.ensureVisible()
}

func (m *FeedModel) JumpEnd() {
	order := m.currentOrder()
	if len(order) == 0 {
		return
	}
	m.cursor = len(order) - 1
	m.ensureVisible()
}

func (m *FeedModel) buildRawOrder() {
	m.rawOrder = append([]*Headline(nil), m.Items...)
	// Raw = newest first.
	sort.SliceStable(m.rawOrder, func(i, j int) bool {
		ai := m.rawOrder[i].PublishedAt
		aj := m.rawOrder[j].PublishedAt
		if !ai.Equal(aj) {
			return ai.After(aj)
		}
		return m.rawOrder[i].ID < m.rawOrder[j].ID
	})
}

func (m *FeedModel) Recompute(weights Weights, now time.Time) {
	m.lastWeights = weights
	m.lastNow = now

	// Track old ranks before we sort engineered order.
	oldRank := make(map[string]int, len(m.engOrder))
	for i, h := range m.engOrder {
		if h != nil {
			oldRank[h.ID] = i
		}
	}

	// Ensure engOrder has all items as pointers.
	if len(m.engOrder) != len(m.Items) {
		m.engOrder = append([]*Headline(nil), m.Items...)
	}

	for _, h := range m.engOrder {
		if h == nil {
			continue
		}
		bd := h.ComputeFinal(weights, now)
		h.Breakdown = bd
		h.FinalScore = bd.FinalScore
	}

	// Stable sort by final score desc; tie-break by recency (newer first).
	sort.SliceStable(m.engOrder, func(i, j int) bool {
		a := m.engOrder[i]
		b := m.engOrder[j]
		if a == nil || b == nil {
			return a != nil
		}
		da := a.FinalScore
		db := b.FinalScore
		if math.Abs(da-db) > 1e-12 {
			return da > db
		}
		// Tie-break: newer first.
		if !a.PublishedAt.Equal(b.PublishedAt) {
			return a.PublishedAt.After(b.PublishedAt)
		}
		return a.ID < b.ID
	})

	// Recompute rank + delta.
	for newIdx, h := range m.engOrder {
		if h == nil {
			continue
		}
		h.EngRank = newIdx + 1
		if oldIdx, ok := oldRank[h.ID]; ok {
			h.RankDelta = oldIdx - newIdx
		} else {
			h.RankDelta = 0
		}
	}

	// Raw order should reflect any changes in Items (if upstream replaces it).
	m.buildRawOrder()

	// Keep cursor stable by ID in current mode.
	curID := ""
	if cur := m.CursorHeadline(); cur != nil {
		curID = cur.ID
	}
	if curID != "" {
		order := m.currentOrder()
		for i, h := range order {
			if h != nil && h.ID == curID {
				m.cursor = i
				break
			}
		}
	}
	m.cursor = clampInt(m.cursor, 0, maxInt(0, len(m.currentOrder())-1))
	m.ensureVisible()
}

func (m *FeedModel) currentOrder() []*Headline {
	if m.mode == ModeRaw {
		return m.rawOrder
	}
	return m.engOrder
}

func (m *FeedModel) ensureVisible() {
	order := m.currentOrder()
	n := len(order)
	if n == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	m.cursor = clampInt(m.cursor, 0, n-1)

	h := maxInt(1, m.listHeight)
	// Clamp offset range
	maxOffset := maxInt(0, n-h)
	m.offset = clampInt(m.offset, 0, maxOffset)

	if m.cursor < m.offset {
		m.offset = m.cursor
	} else if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = clampInt(m.offset, 0, maxOffset)
}

func (m *FeedModel) VisibleRange() (start, end int) {
	order := m.currentOrder()
	n := len(order)
	if n == 0 || m.listHeight <= 0 {
		return 0, 0
	}
	start = clampInt(m.offset, 0, n)
	end = clampInt(m.offset+m.listHeight, 0, n)
	return start, end
}

func (m *FeedModel) View(tick uint64, st Styles) string {
	order := m.currentOrder()
	if len(order) == 0 {
		return st.FeedMeta.Render("no items")
	}
	start, end := m.VisibleRange()
	if start == end {
		return st.FeedMeta.Render("…")
	}

	var lines []string
	lines = make([]string, 0, end-start)

	// Column widths (approx): rank+delta+space = 10; meta = 14
	rankW := 4
	deltaW := 6
	metaW := 14
	gap := 1

	titleW := m.width - (rankW + deltaW + metaW + 2*gap)
	if titleW < 10 {
		titleW = maxInt(10, m.width-2) // degrade gracefully
		metaW = 0
	}

	for idx := start; idx < end; idx++ {
		h := order[idx]
		if h == nil {
			continue
		}
		selected := idx == m.cursor

		// Rank column
		rankStr := fmt.Sprintf("%3d", idx+1)
		rankStr = st.FeedRank.Render(rankStr)

		// Delta column (engineered only; raw shows blanks)
		deltaStr := "   ·  "
		if m.mode == ModeEngineered {
			switch {
			case h.RankDelta > 0:
				deltaStr = st.FeedDeltaUp.Render(fmt.Sprintf(" ↑%2d ", h.RankDelta))
			case h.RankDelta < 0:
				deltaStr = st.FeedDeltaDown.Render(fmt.Sprintf(" ↓%2d ", -h.RankDelta))
			default:
				deltaStr = st.FeedDeltaFlat.Render("  ·   ")
			}
		} else {
			deltaStr = st.FeedDeltaFlat.Render("      ")
		}

		// Title (glitched only in engineered mode; visible rows only)
		title := h.Title
		if m.mode == ModeEngineered {
			title = GlitchText(title, tick, h.TitleHash, h.Arousal, st)
		}
		title = runewidth.Truncate(title, titleW, "…")

		// Meta: "SOURCE · 3.2h"
		meta := ""
		if metaW > 0 {
			ageH := math.Max(0, m.lastNow.Sub(h.PublishedAt).Hours())
			var age string
			if ageH < 1 {
				age = fmt.Sprintf("%.0fm", ageH*60)
			} else if ageH < 24 {
				age = fmt.Sprintf("%.1fh", ageH)
			} else {
				age = fmt.Sprintf("%.0fd", ageH/24)
			}
			meta = fmt.Sprintf("%s · %s", h.Source, age)
			meta = runewidth.Truncate(meta, metaW, "…")
			meta = st.FeedMeta.Render(meta)
		}

		row := joinFixed(
			m.width,
			[]string{
				padRight(rankStr, rankW),
				padRight(deltaStr, deltaW),
				padRight(title, titleW),
				padRight(meta, metaW),
			},
			" ",
		)

		if selected {
			row = st.FeedRowSelected.Render(row)
		} else {
			row = st.FeedRow.Render(row)
		}
		lines = append(lines, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
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
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func padRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := lipgloss.Width(s)
	if sw >= w {
		return s
	}
	return s + spaces(w-sw)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%*s", n, "")
}

// joinFixed joins columns and then truncates/pads to width.
func joinFixed(width int, cols []string, sep string) string {
	if width <= 0 {
		return ""
	}
	out := cols[0]
	for i := 1; i < len(cols); i++ {
		if cols[i] == "" {
			continue
		}
		out += sep + cols[i]
	}
	ow := lipgloss.Width(out)
	if ow == width {
		return out
	}
	if ow < width {
		return out + spaces(width-ow)
	}
	return runewidth.Truncate(out, width, "…")
}
```

---

## `internal/ui/media/slider_panel.go`
```go
package media

import (
	"fmt"
	"math"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

type SliderPanel struct {
	Weights  Weights
	Selected int

	width  int
	height int

	bar progress.Model
}

func NewSliderPanel(w Weights) SliderPanel {
	// progress.WithScaledGradient is pretty but costs; keep solid + theme.
	bar := progress.New(
		progress.WithDefaultGradient(),
	)
	return SliderPanel{
		Weights:  w,
		Selected: 0,
		bar:      bar,
	}
}

func (s SliderPanel) SetSize(width, height int) SliderPanel {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	s.width, s.height = width, height
	return s
}

func (s SliderPanel) Update(msg tea.Msg) (SliderPanel, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}

	before := s.Weights
	switch k.String() {
	case "up":
		s.Selected = clampInt(s.Selected-1, 0, 3)
	case "down":
		s.Selected = clampInt(s.Selected+1, 0, 3)
	case "left":
		s.Weights = adjustWeight(s.Weights, s.Selected, -1)
	case "right":
		s.Weights = adjustWeight(s.Weights, s.Selected, +1)
	}

	// Only emit change msg if weights changed.
	if before != s.Weights {
		w := s.Weights
		return s, func() tea.Msg { return weightsChangedMsg{Weights: w} }
	}
	return s, nil
}

func adjustWeight(w Weights, which int, dir int) Weights {
	switch which {
	case 0: // Arousal 0..100 step 5
		w.ArousalWeight = clamp(w.ArousalWeight+float64(dir)*5, 0, 100)
	case 1: // Neg bias 0.5..2.0 step 0.1
		w.NegativityBias = clamp(roundTo(w.NegativityBias+float64(dir)*0.10, 2), 0.5, 2.0)
	case 2: // Curiosity gap 0..80 step 5
		w.CuriosityGap = clamp(w.CuriosityGap+float64(dir)*5, 0, 80)
	case 3: // Half-life 2..72 step 2h
		w.RecencyHalfLifeHours = clamp(w.RecencyHalfLifeHours+float64(dir)*2, 2, 72)
	}
	return w
}

func roundTo(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}

func (s SliderPanel) View(st Styles, focused bool) string {
	title := st.SidebarTitle.Render("WEIGHTS")
	if !focused {
		title = st.CardDim.Render("WEIGHTS")
	}

	w := s.width
	if w <= 0 {
		return ""
	}

	// Leave room for borders/padding; actual bar width should be conservative.
	barW := maxInt(10, w-16)

	lines := []string{title, st.CardDim.Render(""), s.renderSlider(st, 0, barW), s.renderSlider(st, 1, barW), s.renderSlider(st, 2, barW), s.renderSlider(st, 3, barW)}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	box := st.SidebarBox
	if focused {
		box = st.SideFocusOutline
	}
	return box.Width(w).Render(content)
}

func (s SliderPanel) renderSlider(st Styles, idx int, barW int) string {
	active := idx == s.Selected
	prefix := "▷"
	labelStyle := st.SliderLabel
	valStyle := st.SliderValue
	if active {
		prefix = "▶"
		labelStyle = st.SliderActive
	} else {
		labelStyle = st.SliderInactive
		valStyle = st.SliderInactive
	}

	var label, val string
	var p float64 // 0..1

	switch idx {
	case 0:
		label = "Arousal boost"
		val = fmt.Sprintf("%.0f%%", s.Weights.ArousalWeight)
		p = clamp(s.Weights.ArousalWeight/100.0, 0, 1)
	case 1:
		label = "Negativity bias"
		val = fmt.Sprintf("%.2fx", s.Weights.NegativityBias)
		p = clamp((s.Weights.NegativityBias-0.5)/(2.0-0.5), 0, 1)
	case 2:
		label = "Curiosity gap"
		val = fmt.Sprintf("%.0f%%", s.Weights.CuriosityGap)
		p = clamp(s.Weights.CuriosityGap/80.0, 0, 1)
	case 3:
		label = "Recency half-life"
		val = fmt.Sprintf("%.0fh", s.Weights.RecencyHalfLifeHours)
		p = clamp((s.Weights.RecencyHalfLifeHours-2.0)/(72.0-2.0), 0, 1)
	}

	bar := s.bar.ViewAs(p)
	// Constrain bar width by styling (progress renders to its own width; we pad/truncate visually).
	// Bubbles/progress doesn't expose width in ViewAs directly; we do a quick truncate/pad here.
	bar = padRight(bar, barW)
	if lipgloss.Width(bar) > barW {
		bar = bar[:barW]
	}

	left := fmt.Sprintf("%s %s", prefix, labelStyle.Render(label))
	right := valStyle.Render(val)

	// Layout: left + bar + right (best-effort)
	row := fmt.Sprintf("%-16s %s %s", left, bar, right)
	return row
}
```

---

## `internal/ui/media/transparency_card.go`
```go
package media

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type TransparencyCard struct {
	Expanded bool
	width    int
}

func NewTransparencyCard() TransparencyCard {
	return TransparencyCard{Expanded: false}
}

func (c TransparencyCard) SetWidth(width int) TransparencyCard {
	if width < 0 {
		width = 0
	}
	c.width = width
	return c
}

func (c TransparencyCard) Toggle() TransparencyCard {
	c.Expanded = !c.Expanded
	return c
}

func (c TransparencyCard) Height() int {
	if !c.Expanded {
		// title + hint line
		return 2
	}
	// title + 6 metrics + hint
	return 8
}

func (c TransparencyCard) View(st Styles, selected *Headline) string {
	w := c.width
	if w <= 0 {
		return ""
	}

	title := st.CardTitle.Render("TRANSPARENCY")
	if selected == nil {
		body := st.CardDim.Render("no selection")
		return st.CardBox.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left, title, body))
	}

	// Collapsed view: single-line summary
	if !c.Expanded {
		sum := fmt.Sprintf("Enter: expand · Semantic %.2f  Rerank %.2f  Final %.3f",
			selected.Breakdown.Semantic,
			selected.Breakdown.Rerank,
			selected.FinalScore,
		)
		sum = runewidth.Truncate(sum, maxInt(1, w-2), "…")
		return st.CardBox.Width(w).Render(lipgloss.JoinVertical(lipgloss.Left, title, st.CardDim.Render(sum)))
	}

	// Expanded: show metrics + bars
	bd := selected.Breakdown
	barW := maxInt(10, w-28)

	lines := []string{title}

	lines = append(lines,
		c.metricLine(st, "Semantic", bd.Semantic, barW, st.BarFillCyan),
		c.metricLine(st, "Reranker", bd.Rerank, barW, st.BarFillCyan),
		c.metricLine(st, "Arousal "+fireMeter(bd.Arousal), bd.Arousal, barW, st.BarFillPink),
		c.metricLine(st, "Recency wt", clamp(bd.RecencyDecay, 0, 1), barW, st.BarFillYellow),
		c.metricLine(st, "Src diversity", bd.Diversity, barW, st.BarFillCyan),
		c.metricLine(st, "Final score", clamp(bd.FinalScore, 0, 1), barW, st.BarFillYellow),
	)

	hint := st.CardDim.Render("Enter: collapse")
	lines = append(lines, hint)

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return st.CardBox.Width(w).Render(content)
}

func (c TransparencyCard) metricLine(st Styles, name string, v float64, barW int, fill lipgloss.Style) string {
	name = runewidth.Truncate(name, 14, "…")
	val := fmt.Sprintf("%.3f", v)

	bar := renderBar(st, v, barW, fill)
	left := st.CardMetricName.Render(fmt.Sprintf("%-14s", name))
	right := st.CardMetricVal.Render(val)

	// Keep stable layout: name + bar + value
	return strings.TrimRight(fmt.Sprintf("%s %s %s", left, bar, right), " ")
}

func renderBar(st Styles, v float64, width int, fill lipgloss.Style) string {
	width = maxInt(1, width)
	v = clamp(v, 0, 1)
	filled := int(math.Round(v * float64(width)))
	if filled > width {
		filled = width
	}
	empty := width - filled
	if empty < 0 {
		empty = 0
	}
	return fill.Render(strings.Repeat("█", filled)) + st.BarEmpty.Render(strings.Repeat("░", empty))
}

func fireMeter(arousal float64) string {
	// Requirement: 🔥 scale.
	// 0..1 mapped to 0..5 flames.
	n := int(math.Round(clamp(arousal, 0, 1) * 5))
	if n <= 0 {
		return ""
	}
	return strings.Repeat("🔥", n)
}
```

---

## `internal/ui/media/main_model.go`
```go
package media

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ----- Message catalog -----

// tickMsg drives deterministic glitch animation. Interval: 120ms.
type tickMsg struct{}

// weightsChangedMsg emitted by SliderPanel when user adjusts weights.
type weightsChangedMsg struct {
	Weights Weights
}

// ----- Focus -----

type FocusArea int

const (
	FocusFeed FocusArea = iota
	FocusSidebar
)

func (f FocusArea) String() string {
	switch f {
	case FocusSidebar:
		return "SIDEBAR"
	default:
		return "FEED"
	}
}

// ----- Integration context (Observer app) -----
//
// This media view is a self-contained tea.Model you can mount inside your existing
// Observer App model in one of two common ways:
//
// 1) As a sub-model inside a "screen router":
//    - AppModel holds `activeScreen tea.Model`
//    - Switching to Media sets `activeScreen = media.NewMainModel(...)`
//    - AppModel.Update delegates msgs to activeScreen.Update and replaces it
//
// 2) As a field on AppModel with a view mode enum:
//    - AppModel has `mode AppMode` and `media media.MainModel`
//    - When mode==Media, AppModel.Update delegates to media.Update and returns cmd
//    - When mode switches away, keep media state alive for fast return
//
// This package does not import Observer internals; it stays portable.
// Provide your real headlines via Config.Headlines and a Now() callback.

// ----- Root model -----

type Config struct {
	Now func() time.Time
	// Provide real headlines from store/coordinator; if nil, MockHeadlines is used.
	Headlines []*Headline
	Styles    *Styles
}

type MainModel struct {
	width  int
	height int

	mode  ViewMode
	focus FocusArea

	tick uint64

	now func() time.Time

	weights Weights

	feed   FeedModel
	sliders SliderPanel
	card   TransparencyCard

	styles Styles

	sidebarCollapsed bool
}

func NewMainModel(cfg Config) MainModel {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	st := DefaultStyles()
	if cfg.Styles != nil {
		st = *cfg.Styles
	}

	h := cfg.Headlines
	if len(h) == 0 {
		h = MockHeadlines(nowFn())
	}

	w := DefaultWeights()

	m := MainModel{
		mode:    ModeEngineered,
		focus:   FocusFeed,
		now:     nowFn,
		weights: w,
		feed:    NewFeedModel(h),
		sliders: NewSliderPanel(w),
		card:    NewTransparencyCard(),
		styles:  st,
	}

	// Initial compute.
	m.feed.SetModePreserveSelection(m.mode)
	m.feed.Recompute(m.weights, m.now())
	return m
}

func (m MainModel) Init() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applyLayout()
		return m, nil

	case tickMsg:
		m.tick++
		// Keep ticking.
		return m, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })

	case weightsChangedMsg:
		m.weights = msg.Weights
		m.feed.Recompute(m.weights, m.now())
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "tab":
			if m.sidebarCollapsed {
				m.focus = FocusFeed
				return m, nil
			}
			if m.focus == FocusFeed {
				m.focus = FocusSidebar
			} else {
				m.focus = FocusFeed
			}
			return m, nil

		case "r":
			m.mode = ModeRaw
			m.feed.SetModePreserveSelection(m.mode)
			return m, nil
		case "e":
			m.mode = ModeEngineered
			m.feed.SetModePreserveSelection(m.mode)
			// Recompute ensures deltas stay meaningful when weights changed off-screen.
			m.feed.Recompute(m.weights, m.now())
			return m, nil

		case "enter":
			if m.focus == FocusFeed {
				m.card = m.card.Toggle()
				m.applyLayout() // adjust listHeight without losing scroll position
			}
			return m, nil
		}

		// Delegate navigation based on focus.
		if m.focus == FocusFeed {
			switch msg.String() {
			case "up":
				m.feed.MoveCursor(-1)
			case "down":
				m.feed.MoveCursor(+1)
			case "pgup":
				m.feed.Page(-1)
			case "pgdown":
				m.feed.Page(+1)
			case "home":
				m.feed.JumpHome()
			case "end":
				m.feed.JumpEnd()
			}
			return m, nil
		}

		if m.focus == FocusSidebar && !m.sidebarCollapsed {
			var cmd tea.Cmd
			m.sliders, cmd = m.sliders.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m *MainModel) applyLayout() {
	// Responsive layout rules:
	// - Split horizontally: feed | sidebar (gap 1)
	// - Sidebar collapses under width threshold
	// - Vertical: header (1) + help (1) + body (rest)
	w, h := m.width, m.height
	if w <= 0 || h <= 0 {
		return
	}

	const (
		minTotalForSidebar = 92
		sidebarW           = 36
		gapW               = 1
		minFeedW           = 44
	)

	m.sidebarCollapsed = w < minTotalForSidebar || (w-sidebarW-gapW) < minFeedW
	if m.sidebarCollapsed && m.focus == FocusSidebar {
		m.focus = FocusFeed
	}

	headerH := 1
	helpH := 1
	bodyH := h - headerH - helpH
	if bodyH < 1 {
		bodyH = 1
	}

	cardW := w
	feedW := w
	sideW := 0
	if !m.sidebarCollapsed {
		sideW = sidebarW
		feedW = w - sidebarW - gapW
		cardW = feedW
	}
	m.card = m.card.SetWidth(cardW)

	// Allocate list height inside feed column:
	// feed box contains list + card, but both are rendered within feed column view.
	cardH := m.card.Height()
	listH := bodyH - cardH
	if listH < 3 {
		// If extremely small, shrink card first (still keeps a minimal hint).
		// But card Height() is fixed; we instead clamp list to 3 and allow body to overflow minimally.
		listH = 3
	}
	m.feed.SetSize(feedW-2, listH) // -2 to account for the feed box padding/border

	m.sliders = m.sliders.SetSize(sideW-2, bodyH) // sidebar box padding/border
}

func (m MainModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	st := m.styles
	m.applyLayout()

	// Header (mode + focus)
	modeBadge := st.Badge.Render("RAW")
	if m.mode == ModeEngineered {
		modeBadge = st.Badge.Render("ENG")
	}
	focusBadge := st.BadgeAlt.Render(m.focus.String())
	title := st.HeaderTitle.Render("OBSERVER / MEDIA")

	header := lipgloss.JoinHorizontal(lipgloss.Left,
		title,
		" ",
		modeBadge,
		" ",
		focusBadge,
	)

	header = st.Header.Width(m.width).Render(header)

	// Body split
	body := m.renderBody()

	help := m.renderHelp()

	// Compose vertically
	return lipgloss.JoinVertical(lipgloss.Left, header, body, help)
}

func (m MainModel) renderBody() string {
	st := m.styles
	w, h := m.width, m.height
	bodyH := h - 2 // header + help

	// Recompute column widths consistent with applyLayout().
	const (
		minTotalForSidebar = 92
		sidebarW           = 36
		gapW               = 1
		minFeedW           = 44
	)

	sideCollapsed := w < minTotalForSidebar || (w-sidebarW-gapW) < minFeedW

	feedW := w
	sideW := 0
	gap := ""
	if !sideCollapsed {
		feedW = w - sidebarW - gapW
		sideW = sidebarW
		gap = " "
	}

	// Feed: list + card inside a box
	feedInner := lipgloss.JoinVertical(lipgloss.Left,
		m.feed.View(m.tick, st),
		m.card.View(st, m.feed.CursorHeadline()),
	)

	feedBox := st.FeedBox
	if m.focus == FocusFeed {
		feedBox = st.FeedFocusOutline
	}

	feedRendered := feedBox.Width(feedW).Height(bodyH).Render(feedInner)

	if sideCollapsed {
		return feedRendered
	}

	sideRendered := st.SidebarBox
	if m.focus == FocusSidebar {
		sideRendered = st.SideFocusOutline
	}
	side := m.sliders.View(st, m.focus == FocusSidebar)
	side = sideRendered.Width(sideW).Height(bodyH).Render(side)

	return lipgloss.JoinHorizontal(lipgloss.Top, feedRendered, gap, side)
}

func (m MainModel) renderHelp() string {
	st := m.styles

	// Keys:
	// r/e toggle; tab focus; arrows; enter card; q quit
	keys := func(k, desc string) string {
		return st.HelpKey.Render(k) + st.HelpDim.Render(" "+desc)
	}

	parts := []string{
		keys("r", "raw"),
		keys("e", "engineered"),
		keys("tab", "focus"),
		keys("↑/↓", "move"),
		keys("pgup/pgdn", "page"),
		keys("enter", "transparency"),
		keys("q", "quit"),
	}

	line := parts[0]
	for i := 1; i < len(parts); i++ {
		line += st.HelpDim.Render("  ·  ") + parts[i]
	}
	return st.Help.Width(m.width).Render(line)
}

func (m MainModel) DebugString() string {
	return fmt.Sprintf("w=%d h=%d mode=%s focus=%s tick=%d weights=%s",
		m.width, m.height, m.mode.String(), m.focus.String(), m.tick, m.weights.String())
}
```

---

## Notes on performance and correctness (implemented as requested)

- **4k items**: feed renders only the **visible range** (`VisibleRange()`), and glitching is applied only to those rows.
- **Dual ordering**: `rawOrder` and `engOrder` are slices of `*Headline` pointing into the same `Items` slice. Recompute updates per-headline derived fields.
- **Rank delta**: computed from an `oldRank` map prior to engineered sort; `RankDelta = oldIndex - newIndex`.
- **Selection preservation**: `SetModePreserveSelection()` switches modes by matching selected headline `ID`.
- **Tick**: `tickMsg` at **120ms**, increments `tick`, and view uses it for deterministic glitch variation.
- **Slider propagation**: slider sends `weightsChangedMsg` to root; root calls `feed.Recompute()` with `now()`.

---

If you want, I can also provide a tiny `cmd/demo-media/main.go` that runs this model standalone for local iteration, but I kept to your decided file structure.
