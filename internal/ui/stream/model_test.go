package stream

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

func TestGetTimeBand(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		published time.Time
		expected  timeBand
	}{
		{"just now", now.Add(-5 * time.Minute), bandJustNow},
		{"9 minutes ago", now.Add(-9 * time.Minute), bandJustNow},
		{"11 minutes ago", now.Add(-11 * time.Minute), bandPastHour},
		{"30 minutes ago", now.Add(-30 * time.Minute), bandPastHour},
		{"2 hours ago", now.Add(-2 * time.Hour), bandToday},
		{"12 hours ago", now.Add(-12 * time.Hour), bandToday},
		{"30 hours ago", now.Add(-30 * time.Hour), bandYesterday},
		{"3 days ago", now.Add(-72 * time.Hour), bandOlder},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTimeBand(tt.published)
			if got != tt.expected {
				t.Errorf("getTimeBand(%v) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestBandLabel(t *testing.T) {
	tests := []struct {
		band     timeBand
		expected string
	}{
		{bandJustNow, "Just Now"},
		{bandPastHour, "Past Hour"},
		{bandToday, "Earlier Today"},
		{bandYesterday, "Yesterday"},
		{bandOlder, "Older"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := bandLabel(tt.band)
			if got != tt.expected {
				t.Errorf("bandLabel(%v) = %q, want %q", tt.band, got, tt.expected)
			}
		})
	}
}

func TestGetSourceAbbrev(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"Hacker News", "HN"},
		{"r/MachineLearning", "r/ML"},
		{"Sydney Morning Herald", "SMH"},
		{"Washington Post", "WaPo"},
		{"NY Times", "NYT"},
		{"Unknown Source", "Unknown"},        // finds natural break at space
		{"Short", "Short"},                   // unchanged
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSourceAbbrev(tt.name)
			if got != tt.expected {
				t.Errorf("getSourceAbbrev(%q) = %q, want %q", tt.name, got, tt.expected)
			}
		})
	}
}

func TestCleanSummary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"HTML tags removed",
			"<p>Hello <b>world</b></p>",
			"Hello world",
		},
		{
			"HTML entities decoded",
			"It&#39;s a &quot;test&quot; &amp; more",
			"It's a \"test\" & more",
		},
		{
			"Extra whitespace collapsed",
			"  too   many    spaces  ",
			"too many spaces",
		},
		{
			"Comments prefix removed",
			"Comments This is the actual summary",
			"This is the actual summary",
		},
		{
			"Mixed HTML and entities",
			"<div>We&#39;ve got &amp; <span>stuff</span></div>",
			"We've got & stuff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanSummary(tt.input)
			if got != tt.expected {
				t.Errorf("cleanSummary(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.example.com/path", "example.com"},
		{"http://news.ycombinator.com/item?id=123", "news.ycombinator.com"},
		{"https://bbc.co.uk/news/world", "bbc.co.uk"},
		{"https://www.nytimes.com", "nytimes.com"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractDomain(tt.url)
			if got != tt.expected {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestIsBreakingNews(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		item     feeds.Item
		category string
		age      time.Duration
		expected bool
	}{
		{
			"wire service recent",
			feeds.Item{Title: "Normal headline"},
			"wire",
			5 * time.Minute,
			true,
		},
		{
			"wire service old",
			feeds.Item{Title: "Normal headline"},
			"wire",
			2 * time.Hour,
			false,
		},
		{
			"breaking keyword",
			feeds.Item{Title: "BREAKING: Major event"},
			"tech",
			15 * time.Minute,
			true,
		},
		{
			"urgent keyword",
			feeds.Item{Title: "URGENT: Action needed"},
			"tech",
			10 * time.Minute,
			true,
		},
		{
			"normal item",
			feeds.Item{Title: "Regular news story"},
			"tech",
			5 * time.Minute,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.item.Published = now.Add(-tt.age)
			got := isBreakingNews(tt.item, tt.category, tt.age)
			if got != tt.expected {
				t.Errorf("isBreakingNews() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{45 * time.Minute, "45m"},
		{2 * time.Hour, "2h"},
		{25 * time.Hour, "1d"},
		{5 * 24 * time.Hour, "5d"},
		{14 * 24 * time.Hour, "2w"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := formatAge(tt.duration)
			if got != tt.expected {
				t.Errorf("formatAge(%v) = %q, want %q", tt.duration, got, tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is .."},
		{"ab", 5, "ab"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

func TestDeriveCategoryFromSource(t *testing.T) {
	tests := []struct {
		sourceName string
		sourceType string
		expected   string
	}{
		{"r/MachineLearning", "rss", "reddit"},
		{"r/golang", "rss", "reddit"},
		{"arXiv cs.AI", "rss", "arxiv"},
		{"Polymarket", "polymarket", "predictions"},
		{"USGS Significant", "usgs", "events"},
		{"Google News Tech", "rss", "aggregator"},
		{"Techmeme", "rss", "aggregator"},
		{"HN Top", "hn", "tech"},
		{"Random Blog", "rss", "rss"},
	}

	for _, tt := range tests {
		t.Run(tt.sourceName, func(t *testing.T) {
			got := deriveCategoryFromSource(tt.sourceName, tt.sourceType)
			if got != tt.expected {
				t.Errorf("deriveCategoryFromSource(%q, %q) = %q, want %q",
					tt.sourceName, tt.sourceType, got, tt.expected)
			}
		})
	}
}

func TestModelNavigation(t *testing.T) {
	m := New()
	items := []feeds.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
		{ID: "3", Title: "Item 3"},
	}
	m.SetItems(items)

	// Start at 0
	if m.cursor != 0 {
		t.Errorf("Initial cursor = %d, want 0", m.cursor)
	}

	// Move down
	m.MoveDown()
	if m.cursor != 1 {
		t.Errorf("After MoveDown cursor = %d, want 1", m.cursor)
	}

	// Move down again
	m.MoveDown()
	if m.cursor != 2 {
		t.Errorf("After second MoveDown cursor = %d, want 2", m.cursor)
	}

	// Can't move past end
	m.MoveDown()
	if m.cursor != 2 {
		t.Errorf("After MoveDown at end cursor = %d, want 2", m.cursor)
	}

	// Move up
	m.MoveUp()
	if m.cursor != 1 {
		t.Errorf("After MoveUp cursor = %d, want 1", m.cursor)
	}

	// Move up to start
	m.MoveUp()
	if m.cursor != 0 {
		t.Errorf("After second MoveUp cursor = %d, want 0", m.cursor)
	}

	// Can't move before start
	m.MoveUp()
	if m.cursor != 0 {
		t.Errorf("After MoveUp at start cursor = %d, want 0", m.cursor)
	}
}

func TestSelectedItem(t *testing.T) {
	m := New()

	// No items
	if m.SelectedItem() != nil {
		t.Error("SelectedItem should be nil with no items")
	}

	// Add items
	items := []feeds.Item{
		{ID: "1", Title: "Item 1"},
		{ID: "2", Title: "Item 2"},
	}
	m.SetItems(items)

	// Check selected
	selected := m.SelectedItem()
	if selected == nil {
		t.Fatal("SelectedItem should not be nil")
	}
	if selected.ID != "1" {
		t.Errorf("SelectedItem ID = %q, want %q", selected.ID, "1")
	}

	// Move and check
	m.MoveDown()
	selected = m.SelectedItem()
	if selected.ID != "2" {
		t.Errorf("After MoveDown, SelectedItem ID = %q, want %q", selected.ID, "2")
	}
}

// Phase 3 & 4 Tests

func TestRenderSparkline(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		width    int
		expected int // expected rune count
	}{
		{"empty", []float64{}, 10, 0},
		{"single value", []float64{0.5}, 5, 5},
		{"multiple values", []float64{0.0, 0.25, 0.5, 0.75, 1.0}, 5, 5},
		{"zero width", []float64{0.5}, 0, 0},
		{"clamped high", []float64{1.5, 2.0}, 2, 2},
		{"clamped low", []float64{-0.5, -1.0}, 2, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderSparkline(tt.values, tt.width)
			runeCount := len([]rune(got))
			if runeCount != tt.expected {
				t.Errorf("renderSparkline rune count = %d, want %d", runeCount, tt.expected)
			}
		})
	}
}

func TestRenderSparklineValues(t *testing.T) {
	// Test that different values produce different characters
	low := renderSparkline([]float64{0.0}, 1)
	mid := renderSparkline([]float64{0.5}, 1)
	high := renderSparkline([]float64{1.0}, 1)

	if low == high {
		t.Error("low and high sparkline should be different")
	}
	if low == mid || mid == high {
		t.Log("Note: mid sparkline may or may not differ based on quantization")
	}
}

func TestRenderActivityIndicator(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, "···"},
		{1, "▁▁▁"},
		{2, "▃▃▃"},
		{5, "▅▅▅"},
		{10, "▇▇▇"},
		{100, "▇▇▇"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("count_%d", tt.count), func(t *testing.T) {
			got := renderActivityIndicator(tt.count)
			if got != tt.expected {
				t.Errorf("renderActivityIndicator(%d) = %q, want %q", tt.count, got, tt.expected)
			}
		})
	}
}

func TestDensityModes(t *testing.T) {
	m := New()

	// Default is comfortable
	if m.Density() != DensityComfortable {
		t.Errorf("Default density = %v, want DensityComfortable", m.Density())
	}
	if m.DensityLabel() != "Comfortable" {
		t.Errorf("Default density label = %q, want %q", m.DensityLabel(), "Comfortable")
	}

	// Toggle to compact
	m.ToggleDensity()
	if m.Density() != DensityCompact {
		t.Errorf("After toggle density = %v, want DensityCompact", m.Density())
	}
	if m.DensityLabel() != "Compact" {
		t.Errorf("After toggle density label = %q, want %q", m.DensityLabel(), "Compact")
	}

	// Toggle back to comfortable
	m.ToggleDensity()
	if m.Density() != DensityComfortable {
		t.Errorf("After second toggle density = %v, want DensityComfortable", m.Density())
	}

	// Set directly
	m.SetDensity(DensityCompact)
	if m.Density() != DensityCompact {
		t.Errorf("After SetDensity = %v, want DensityCompact", m.Density())
	}
}

func TestSourceActivityTracking(t *testing.T) {
	m := New()
	m.SetSize(80, 40)

	now := time.Now()
	items := []feeds.Item{
		{ID: "1", Title: "Item 1", SourceName: "HN", Published: now.Add(-5 * time.Minute)},
		{ID: "2", Title: "Item 2", SourceName: "HN", Published: now.Add(-10 * time.Minute)},
		{ID: "3", Title: "Item 3", SourceName: "HN", Published: now.Add(-30 * time.Minute)},
		{ID: "4", Title: "Item 4", SourceName: "BBC", Published: now.Add(-2 * time.Hour)},
	}

	m.SetItems(items)

	// Check that source stats were calculated
	if stats, ok := m.sourceStats["HN"]; !ok {
		t.Error("Expected sourceStats for HN")
	} else {
		if stats.recentCount != 3 {
			t.Errorf("HN recentCount = %d, want 3", stats.recentCount)
		}
	}

	if stats, ok := m.sourceStats["BBC"]; !ok {
		t.Error("Expected sourceStats for BBC")
	} else {
		if stats.recentCount != 0 {
			t.Errorf("BBC recentCount = %d, want 0 (item is > 1 hour old)", stats.recentCount)
		}
	}
}

func TestAutoCompactOnSmallTerminal(t *testing.T) {
	m := New()

	// Set small terminal size
	m.SetSize(80, 20) // < 30 lines

	// Add items - this triggers auto-density adjustment
	items := []feeds.Item{
		{ID: "1", Title: "Item 1", Published: time.Now()},
	}
	m.SetItems(items)

	// Should auto-switch to compact
	if m.Density() != DensityCompact {
		t.Errorf("Small terminal should auto-set DensityCompact, got %v", m.Density())
	}
}

func TestViewRendersDensityIndicator(t *testing.T) {
	m := New()
	m.SetSize(80, 40)

	items := []feeds.Item{
		{ID: "1", Title: "Test Item", Published: time.Now()},
	}
	m.SetItems(items)

	// Comfortable mode should show ◉
	view := m.View()
	if !strings.Contains(view, "◉") {
		t.Error("Comfortable mode should show ◉ indicator")
	}

	// Compact mode should show ◎
	m.SetDensity(DensityCompact)
	view = m.View()
	if !strings.Contains(view, "◎") {
		t.Error("Compact mode should show ◎ indicator")
	}
}

func TestExtractProbability(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"Will AI replace programmers? 67%", 67},
		{"Bitcoin to $100k: 45% chance", 45},
		{"Market at 85%", 85},
		{"0% probability", 0},
		{"100% certain", 100},
		{"No probability here", -1},
		{"Just 50 people", -1},        // no % sign
		{"150% increase", -1},         // > 100
		{"The year 2025", -1},         // large number no %
		{"23% and 45% both", 23},      // returns first match
		{"Will X happen? Odds: 75%", 75},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := extractProbability(tt.text)
			if got != tt.expected {
				t.Errorf("extractProbability(%q) = %d, want %d", tt.text, got, tt.expected)
			}
		})
	}
}
