package braintrust

import (
	"testing"

	"github.com/abelbrown/observer/internal/brain"
)

func TestNewModel(t *testing.T) {
	m := New()

	if m.visible {
		t.Error("New model should not be visible by default")
	}

	if m.scrollPos != 0 {
		t.Errorf("Expected scrollPos 0, got %d", m.scrollPos)
	}
}

func TestSetVisible(t *testing.T) {
	m := New()

	m.SetVisible(true)
	if !m.IsVisible() {
		t.Error("Expected IsVisible to return true")
	}

	m.SetVisible(false)
	if m.IsVisible() {
		t.Error("Expected IsVisible to return false")
	}
}

func TestScrolling(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.totalLines = 50 // Simulate long content

	// Test scroll down
	m.ScrollDown(5)
	if m.scrollPos != 5 {
		t.Errorf("Expected scrollPos 5, got %d", m.scrollPos)
	}

	// Test scroll up
	m.ScrollUp(3)
	if m.scrollPos != 2 {
		t.Errorf("Expected scrollPos 2, got %d", m.scrollPos)
	}

	// Test scroll up past 0
	m.ScrollUp(10)
	if m.scrollPos != 0 {
		t.Errorf("Expected scrollPos 0 (clamped), got %d", m.scrollPos)
	}
}

func TestClear(t *testing.T) {
	m := New()
	m.SetAnalysis("item-1", &brain.Analysis{Content: "test"})
	m.scrollPos = 10

	m.Clear()

	if m.analysis != nil {
		t.Error("Expected analysis to be nil after Clear")
	}
	if m.scrollPos != 0 {
		t.Errorf("Expected scrollPos 0 after Clear, got %d", m.scrollPos)
	}
}

func TestCanScroll(t *testing.T) {
	m := New()
	m.SetSize(80, 20) // height 20, leaves ~15 lines for content

	m.totalLines = 10 // Less than visible
	if m.CanScroll() {
		t.Error("Should not be able to scroll with short content")
	}

	m.totalLines = 50 // More than visible
	if !m.CanScroll() {
		t.Error("Should be able to scroll with long content")
	}
}

func TestSetLoading(t *testing.T) {
	m := New()

	m.SetLoading("item-1", "Test Title")

	if m.itemID != "item-1" {
		t.Errorf("Expected itemID 'item-1', got %s", m.itemID)
	}
	if m.itemTitle != "Test Title" {
		t.Errorf("Expected itemTitle 'Test Title', got %s", m.itemTitle)
	}
	if !m.visible {
		t.Error("Expected visible to be true after SetLoading")
	}
	if m.analysis == nil || !m.analysis.Loading {
		t.Error("Expected analysis.Loading to be true")
	}
}
