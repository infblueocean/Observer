package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/otel"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDebugOverlayNilRing(t *testing.T) {
	result := debugOverlay(nil, 80, 24)
	if result != "" {
		t.Errorf("debugOverlay(nil) should return empty string, got %q", result)
	}
}

func TestDebugOverlayRendersStats(t *testing.T) {
	ring := otel.NewRingBuffer(64)
	ring.Push(otel.Event{Kind: otel.KindFetchComplete, Time: time.Now()})
	ring.Push(otel.Event{Kind: otel.KindFetchComplete, Time: time.Now()})
	ring.Push(otel.Event{Kind: otel.KindFetchError, Time: time.Now()})
	ring.Push(otel.Event{Kind: otel.KindSearchStart, Time: time.Now()})
	ring.Push(otel.Event{Kind: otel.KindSearchComplete, Time: time.Now()})

	result := debugOverlay(ring, 80, 40)

	if !strings.Contains(result, "Pipeline Stats") {
		t.Error("overlay should contain 'Pipeline Stats' header")
	}
	if !strings.Contains(result, "2 complete, 1 errors") {
		t.Errorf("overlay should show fetch stats, got:\n%s", result)
	}
	if !strings.Contains(result, "1 started, 1 complete") {
		t.Errorf("overlay should show search stats, got:\n%s", result)
	}
	if !strings.Contains(result, "5 / 64 events") {
		t.Errorf("overlay should show buffer stats, got:\n%s", result)
	}
}

func TestDebugOverlayRecentEvents(t *testing.T) {
	ring := otel.NewRingBuffer(64)
	ring.Push(otel.Event{Kind: otel.KindFetchStart, Time: time.Now(), Msg: "hello world"})
	ring.Push(otel.Event{Kind: otel.KindFetchError, Time: time.Now(), Err: "timeout"})
	ring.Push(otel.Event{Kind: otel.KindSearchStart, Time: time.Now(), QueryID: "abcdef1234567890"})

	result := debugOverlay(ring, 80, 40)

	if !strings.Contains(result, "Recent Events") {
		t.Error("overlay should contain 'Recent Events' header")
	}
	if !strings.Contains(result, "hello world") {
		t.Errorf("overlay should show event message, got:\n%s", result)
	}
	if !strings.Contains(result, "ERR:timeout") {
		t.Errorf("overlay should show error, got:\n%s", result)
	}
	if !strings.Contains(result, "qid:abcdef12") {
		t.Errorf("overlay should show truncated query ID, got:\n%s", result)
	}
}

func TestDebugOverlayTruncation(t *testing.T) {
	ring := otel.NewRingBuffer(64)
	// Push 30 events
	for i := 0; i < 30; i++ {
		ring.Push(otel.Event{Kind: otel.KindFetchStart, Time: time.Now()})
	}

	// Very small height should still render without panic
	result := debugOverlay(ring, 80, 10)
	if result == "" {
		t.Error("overlay should still render with small height")
	}

	// Count the lines (approximately) — should be limited
	lines := strings.Count(result, "\n")
	// With height=10, maxHeight=6, so at most ~6 content lines (plus border/padding)
	if lines > 20 { // generous bound accounting for lipgloss borders
		t.Errorf("overlay should be truncated, got %d lines", lines)
	}
}

func TestDebugToggle(t *testing.T) {
	ring := otel.NewRingBuffer(16)
	app := NewAppWithConfig(AppConfig{
		Obs: ObsConfig{Ring: ring},
	})
	app.ready = true
	app.width = 80
	app.height = 24

	// Verify initially not visible
	if app.debugVisible {
		t.Error("debug should be hidden initially")
	}

	// Press ? to show
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	updated := model.(App)
	if !updated.debugVisible {
		t.Error("? should show debug overlay")
	}

	// Verify debug view renders
	view := updated.View()
	if !strings.Contains(view, "[DEBUG]") {
		t.Errorf("debug view should contain '[DEBUG]', got:\n%s", view)
	}

	// Press ? again to hide
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	updated = model.(App)
	if updated.debugVisible {
		t.Error("second ? should hide debug overlay")
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		dur  time.Duration
		want string
	}{
		{0, "0ms"},
		{50 * time.Millisecond, "50ms"},
		{999 * time.Millisecond, "999ms"},
		{1500 * time.Millisecond, "1.5s"},
		{30 * time.Second, "30.0s"},
		{90 * time.Second, "2m"}, // 1.5 minutes rounds to 2 with %.0f
		{5 * time.Minute, "5m"},
	}
	for _, tt := range tests {
		got := formatAge(tt.dur)
		if got != tt.want {
			t.Errorf("formatAge(%v) = %q, want %q", tt.dur, got, tt.want)
		}
	}
}

func TestFormatAgeNegative(t *testing.T) {
	got := formatAge(-5 * time.Second)
	if got != "0ms" {
		t.Errorf("formatAge(-5s) = %q, want \"0ms\"", got)
	}
}
