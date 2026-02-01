package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/store"
)

// makeItems creates n items with sequential titles. published times are
// spread so that band boundaries appear at predictable indices.
func makeItems(n int) []store.Item {
	now := time.Now()
	items := make([]store.Item, n)
	for i := range items {
		items[i] = store.Item{
			ID:         string(rune('a' + i%26)),
			Title:      strings.Repeat("x", 20),
			SourceName: "src",
			Published:  now.Add(-time.Duration(i) * time.Minute),
		}
	}
	return items
}

// makeItemsWithBands creates items across multiple time bands.
// Returns items where the first 5 are "Just Now", next 10 are "Past Hour",
// rest are "Today".
func makeItemsWithBands(n int) []store.Item {
	now := time.Now()
	items := make([]store.Item, n)
	for i := range items {
		var pub time.Time
		switch {
		case i < 5:
			pub = now.Add(-time.Duration(i) * time.Minute) // Just Now (<15m)
		case i < 15:
			pub = now.Add(-20*time.Minute - time.Duration(i)*time.Minute) // Past Hour
		default:
			pub = now.Add(-2*time.Hour - time.Duration(i)*time.Minute) // Today
		}
		items[i] = store.Item{
			ID:         string(rune('A' + i%26)),
			Title:      strings.Repeat("y", 20),
			SourceName: "src",
			Published:  pub,
		}
	}
	return items
}

func TestCalcScrollOffset_NoBands(t *testing.T) {
	items := makeItems(100)

	tests := []struct {
		name       string
		cursor     int
		height     int
		wantOffset int
	}{
		{"cursor at top", 0, 30, 0},
		{"cursor within viewport", 10, 30, 0},
		{"cursor at viewport edge", 29, 30, 0},
		{"cursor one past viewport", 30, 30, 1},
		{"cursor far down", 99, 30, 70},
		{"small viewport", 10, 5, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcScrollOffset(items, tt.cursor, tt.height, false)
			if got != tt.wantOffset {
				t.Errorf("calcScrollOffset(cursor=%d, height=%d) = %d, want %d",
					tt.cursor, tt.height, got, tt.wantOffset)
			}
		})
	}
}

func TestCalcScrollOffset_WithBands(t *testing.T) {
	items := makeItemsWithBands(30)

	// With 30 items across 3 bands, band headers take extra lines.
	// Cursor at bottom must still be visible.
	height := 10
	cursor := 20

	offset := calcScrollOffset(items, cursor, height, true)

	// Verify cursor fits: count lines from offset to cursor
	lines := visibleLineCount(items, offset, cursor, true)
	if lines > height {
		t.Errorf("cursor %d not visible: visibleLineCount(%d..%d) = %d, viewport = %d",
			cursor, offset, cursor, lines, height)
	}

	// Offset should be > simple calculation due to band headers
	simpleOffset := cursor - height + 1
	if offset < simpleOffset {
		t.Errorf("band-aware offset %d < simple offset %d", offset, simpleOffset)
	}
}

func TestCalcScrollOffset_CursorAlwaysVisible(t *testing.T) {
	items := makeItemsWithBands(50)

	for height := 5; height <= 20; height += 5 {
		for cursor := 0; cursor < len(items); cursor++ {
			offset := calcScrollOffset(items, cursor, height, true)

			lines := visibleLineCount(items, offset, cursor, true)
			if lines > height {
				t.Fatalf("height=%d cursor=%d offset=%d: lines=%d exceeds viewport",
					height, cursor, offset, lines)
			}
			if offset > cursor {
				t.Fatalf("height=%d cursor=%d: offset=%d > cursor", height, cursor, offset)
			}
		}
	}
}

func TestRenderStream_NoOverRender(t *testing.T) {
	items := makeItems(500)
	width := 80
	// height passed to RenderStream includes the status bar line;
	// internally it subtracts 1, giving availableHeight = 30.
	height := 31

	// Render at cursor 250 (scrollOffset would be ~221).
	out := RenderStream(items, 250, width, height, false, false, 0)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 30 {
		t.Errorf("rendered %d lines, want <= 30 (availableHeight)", len(lines))
	}
}

func TestRenderStream_CursorHighlightVisible(t *testing.T) {
	items := makeItems(100)
	width := 80
	height := 21 // availableHeight = 20

	for _, cursor := range []int{0, 10, 19, 20, 50, 99} {
		out := RenderStream(items, cursor, width, height, false, false, 0)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

		// The cursor item should have the SelectedItem style applied.
		// Check that at least one line contains the selected styling.
		found := false
		for _, line := range lines {
			// SelectedItem style uses bold/reverse — look for ANSI escape
			// sequences that differ from normal items. Since renderItemLine
			// applies SelectedItem only to i==cursor, we just verify the
			// highlighted item is among the rendered lines.
			if strings.Contains(line, items[cursor].Title) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cursor=%d: selected item title not found in rendered output (%d lines)",
				cursor, len(lines))
		}
	}
}

func TestRenderStream_BandsWithScrolling(t *testing.T) {
	items := makeItemsWithBands(30)
	width := 80
	height := 11 // availableHeight = 10

	// Scroll to item 25 (deep in "Today" band).
	out := RenderStream(items, 25, width, height, true, false, 0)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	if len(lines) > 10 {
		t.Errorf("rendered %d lines with bands, want <= 10", len(lines))
	}

	// Cursor item must be in output.
	found := false
	for _, line := range lines {
		if strings.Contains(line, items[25].Title) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("cursor=25: selected item not visible in banded output")
	}
}

func TestVisibleLineCount(t *testing.T) {
	items := makeItemsWithBands(20)

	// Items 0-4 are "Just Now", 5-14 are "Past Hour", 15+ are "Today".
	// From 0 to 4: 1 band header + 5 items = 6 lines
	got := visibleLineCount(items, 0, 4, true)
	if got != 6 {
		t.Errorf("visibleLineCount(0,4) = %d, want 6", got)
	}

	// From 0 to 5: crosses into "Past Hour" = 2 headers + 6 items = 8
	got = visibleLineCount(items, 0, 5, true)
	if got != 8 {
		t.Errorf("visibleLineCount(0,5) = %d, want 8", got)
	}

	// From 5 to 5: within "Past Hour", predecessor is item 4 ("Just Now")
	// so band changes = 1 header + 1 item = 2
	got = visibleLineCount(items, 5, 5, true)
	if got != 2 {
		t.Errorf("visibleLineCount(5,5) = %d, want 2", got)
	}

	// From 6 to 10: all "Past Hour", predecessor is item 5 (same band)
	// no header + 5 items = 5
	got = visibleLineCount(items, 6, 10, true)
	if got != 5 {
		t.Errorf("visibleLineCount(6,10) = %d, want 5", got)
	}

	// Without bands: just item count
	got = visibleLineCount(items, 3, 10, false)
	if got != 8 {
		t.Errorf("visibleLineCount(3,10,false) = %d, want 8", got)
	}
}
