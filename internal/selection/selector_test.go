package selection

import (
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

func TestTimeSelector(t *testing.T) {
	now := time.Now()

	items := []feeds.Item{
		{ID: "1", Title: "5 min ago", Published: now.Add(-5 * time.Minute)},
		{ID: "2", Title: "30 min ago", Published: now.Add(-30 * time.Minute)},
		{ID: "3", Title: "2 hours ago", Published: now.Add(-2 * time.Hour)},
		{ID: "4", Title: "1 day ago", Published: now.Add(-25 * time.Hour)},
		{ID: "5", Title: "3 days ago", Published: now.Add(-72 * time.Hour)},
	}

	tests := []struct {
		name     string
		selector Selector
		expected []string // item IDs that should match
	}{
		{
			name:     "JustNow matches <15min",
			selector: JustNow,
			expected: []string{"1"},
		},
		{
			name:     "PastHour matches <1hr",
			selector: PastHour,
			expected: []string{"1", "2"},
		},
		{
			name:     "Today matches <24hr",
			selector: Today,
			expected: []string{"1", "2", "3"},
		},
		{
			name:     "Yesterday matches 24-48hr",
			selector: Yesterday,
			expected: []string{"4"},
		},
		{
			name:     "ThisWeek matches <7days",
			selector: ThisWeek,
			expected: []string{"1", "2", "3", "4", "5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Apply(items, tt.selector)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d items, got %d", len(tt.expected), len(result))
				return
			}
			for i, item := range result {
				if item.ID != tt.expected[i] {
					t.Errorf("item %d: expected ID %s, got %s", i, tt.expected[i], item.ID)
				}
			}
		})
	}
}

func TestSourceSelector(t *testing.T) {
	items := []feeds.Item{
		{ID: "1", SourceName: "Reuters"},
		{ID: "2", SourceName: "AP"},
		{ID: "3", SourceName: "BBC"},
		{ID: "4", SourceName: "Reuters"},
	}

	selector := NewSourceSelector("Wire Services", "Reuters", "AP")
	result := Apply(items, selector)

	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}

	// Should include Reuters and AP, not BBC
	for _, item := range result {
		if item.SourceName == "BBC" {
			t.Error("BBC should not be included")
		}
	}
}

func TestAndSelector(t *testing.T) {
	now := time.Now()

	items := []feeds.Item{
		{ID: "1", SourceName: "Reuters", Published: now.Add(-5 * time.Minute)},
		{ID: "2", SourceName: "Reuters", Published: now.Add(-2 * time.Hour)},
		{ID: "3", SourceName: "BBC", Published: now.Add(-5 * time.Minute)},
	}

	// Past hour AND Reuters
	selector := And("Recent Reuters",
		PastHour,
		NewSourceSelector("Reuters", "Reuters"),
	)

	result := Apply(items, selector)

	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
		return
	}
	if result[0].ID != "1" {
		t.Errorf("expected item 1, got %s", result[0].ID)
	}
}

func TestOrSelector(t *testing.T) {
	now := time.Now()

	items := []feeds.Item{
		{ID: "1", SourceName: "Reuters", Published: now.Add(-5 * time.Minute)},
		{ID: "2", SourceName: "BBC", Published: now.Add(-2 * time.Hour)},
		{ID: "3", SourceName: "CNN", Published: now.Add(-5 * time.Minute)},
	}

	// Reuters OR Just Now
	selector := Or("Reuters or Recent",
		NewSourceSelector("Reuters", "Reuters"),
		JustNow,
	)

	result := Apply(items, selector)

	// Should get item 1 (Reuters + recent), item 3 (recent)
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestNotSelector(t *testing.T) {
	items := []feeds.Item{
		{ID: "1", SourceName: "Reuters"},
		{ID: "2", SourceName: "BBC"},
		{ID: "3", SourceName: "Reuters"},
	}

	// Not Reuters
	selector := Not(NewSourceSelector("Reuters", "Reuters"))

	result := Apply(items, selector)

	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
		return
	}
	if result[0].SourceName != "BBC" {
		t.Errorf("expected BBC, got %s", result[0].SourceName)
	}
}

func TestApplyNilSelector(t *testing.T) {
	items := []feeds.Item{
		{ID: "1"},
		{ID: "2"},
		{ID: "3"},
	}

	result := Apply(items, nil)

	if len(result) != 3 {
		t.Errorf("nil selector should return all items, got %d", len(result))
	}
}
