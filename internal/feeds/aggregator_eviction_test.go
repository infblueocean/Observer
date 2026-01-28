package feeds

import (
	"fmt"
	"testing"
	"time"
)

func TestAggregatorMemoryCap(t *testing.T) {
	// Create aggregator with small cap for testing
	agg := NewAggregatorWithCap(100)

	// Add 150 items
	items := make([]Item, 150)
	baseTime := time.Now()
	for i := range items {
		items[i] = Item{
			ID:        fmt.Sprintf("item-%d", i),
			Title:     fmt.Sprintf("Test Article %d", i),
			URL:       fmt.Sprintf("https://example.com/article/%d", i),
			Published: baseTime.Add(time.Duration(i) * time.Minute),
		}
	}
	agg.MergeItems(items)

	// Verify capped at 100
	count := agg.ItemCount()
	if count > 100 {
		t.Errorf("expected <= 100 items, got %d", count)
	}
	if count != 100 {
		t.Errorf("expected exactly 100 items (at cap), got %d", count)
	}
}

func TestAggregatorEvictsOldest(t *testing.T) {
	// Create aggregator with small cap
	agg := NewAggregatorWithCap(50)

	// Add 100 items with sequential timestamps
	// Items 0-49 are older, items 50-99 are newer
	items := make([]Item, 100)
	baseTime := time.Now()
	for i := range items {
		items[i] = Item{
			ID:        fmt.Sprintf("item-%d", i),
			Title:     fmt.Sprintf("Test Article %d", i),
			URL:       fmt.Sprintf("https://example.com/article/%d", i),
			Published: baseTime.Add(time.Duration(i) * time.Minute),
		}
	}
	agg.MergeItems(items)

	// Get remaining items
	remaining := agg.GetItems()

	if len(remaining) != 50 {
		t.Errorf("expected 50 remaining items, got %d", len(remaining))
	}

	// Verify all remaining items are from the newer half (IDs 50-99)
	for _, item := range remaining {
		var id int
		fmt.Sscanf(item.ID, "item-%d", &id)
		if id < 50 {
			t.Errorf("found old item %q that should have been evicted", item.ID)
		}
	}
}

func TestAggregatorEvictionCount(t *testing.T) {
	agg := NewAggregatorWithCap(100)

	// Initial evicted count should be zero
	if agg.EvictedCount() != 0 {
		t.Errorf("expected initial evicted count to be 0, got %d", agg.EvictedCount())
	}

	// Add 150 items (should evict 50)
	items := make([]Item, 150)
	baseTime := time.Now()
	for i := range items {
		items[i] = Item{
			ID:        fmt.Sprintf("item-%d", i),
			Title:     fmt.Sprintf("Test Article %d", i),
			URL:       fmt.Sprintf("https://example.com/article/%d", i),
			Published: baseTime.Add(time.Duration(i) * time.Minute),
		}
	}
	agg.MergeItems(items)

	if agg.EvictedCount() != 50 {
		t.Errorf("expected evicted count to be 50, got %d", agg.EvictedCount())
	}

	// Add another 75 items (should evict 75 more)
	moreItems := make([]Item, 75)
	for i := range moreItems {
		moreItems[i] = Item{
			ID:        fmt.Sprintf("more-item-%d", i),
			Title:     fmt.Sprintf("More Article %d", i),
			URL:       fmt.Sprintf("https://example.com/more/%d", i),
			Published: baseTime.Add(time.Duration(200+i) * time.Minute), // newer than first batch
		}
	}
	agg.MergeItems(moreItems)

	if agg.EvictedCount() != 125 {
		t.Errorf("expected evicted count to be 125, got %d", agg.EvictedCount())
	}

	// Verify total items is still at cap
	if agg.ItemCount() != 100 {
		t.Errorf("expected 100 items at cap, got %d", agg.ItemCount())
	}
}

func TestAggregatorEvictionPreservesMostRecent(t *testing.T) {
	agg := NewAggregatorWithCap(10)

	// Add items in random timestamp order, not sequential
	baseTime := time.Now()
	items := []Item{
		{ID: "old-1", Title: "Old 1", URL: "https://example.com/old1", Published: baseTime.Add(-10 * time.Hour)},
		{ID: "new-1", Title: "New 1", URL: "https://example.com/new1", Published: baseTime.Add(-1 * time.Hour)},
		{ID: "old-2", Title: "Old 2", URL: "https://example.com/old2", Published: baseTime.Add(-9 * time.Hour)},
		{ID: "new-2", Title: "New 2", URL: "https://example.com/new2", Published: baseTime.Add(-2 * time.Hour)},
		{ID: "old-3", Title: "Old 3", URL: "https://example.com/old3", Published: baseTime.Add(-8 * time.Hour)},
		{ID: "new-3", Title: "New 3", URL: "https://example.com/new3", Published: baseTime.Add(-3 * time.Hour)},
		{ID: "old-4", Title: "Old 4", URL: "https://example.com/old4", Published: baseTime.Add(-7 * time.Hour)},
		{ID: "new-4", Title: "New 4", URL: "https://example.com/new4", Published: baseTime.Add(-4 * time.Hour)},
		{ID: "old-5", Title: "Old 5", URL: "https://example.com/old5", Published: baseTime.Add(-6 * time.Hour)},
		{ID: "new-5", Title: "New 5", URL: "https://example.com/new5", Published: baseTime.Add(-5 * time.Hour)},
		// Extra items that should cause eviction
		{ID: "newest-1", Title: "Newest 1", URL: "https://example.com/newest1", Published: baseTime},
		{ID: "newest-2", Title: "Newest 2", URL: "https://example.com/newest2", Published: baseTime.Add(1 * time.Minute)},
	}
	agg.MergeItems(items)

	// Should have evicted 2 oldest items
	if agg.ItemCount() != 10 {
		t.Errorf("expected 10 items, got %d", agg.ItemCount())
	}

	remaining := agg.GetItems()
	idMap := make(map[string]bool)
	for _, item := range remaining {
		idMap[item.ID] = true
	}

	// Oldest items (old-1, old-2) should be gone
	if idMap["old-1"] {
		t.Error("old-1 should have been evicted (oldest)")
	}
	if idMap["old-2"] {
		t.Error("old-2 should have been evicted (second oldest)")
	}

	// Newest items should be present
	if !idMap["newest-1"] {
		t.Error("newest-1 should be present")
	}
	if !idMap["newest-2"] {
		t.Error("newest-2 should be present")
	}
}

func TestAggregatorUnlimitedCap(t *testing.T) {
	// Cap of 0 means unlimited
	agg := NewAggregatorWithCap(0)

	// Add 1000 items
	items := make([]Item, 1000)
	baseTime := time.Now()
	for i := range items {
		items[i] = Item{
			ID:        fmt.Sprintf("item-%d", i),
			Title:     fmt.Sprintf("Test Article %d", i),
			URL:       fmt.Sprintf("https://example.com/article/%d", i),
			Published: baseTime.Add(time.Duration(i) * time.Minute),
		}
	}
	agg.MergeItems(items)

	// All items should be present (no eviction with 0 cap)
	if agg.ItemCount() != 1000 {
		t.Errorf("expected 1000 items with unlimited cap, got %d", agg.ItemCount())
	}
	if agg.EvictedCount() != 0 {
		t.Errorf("expected 0 evictions with unlimited cap, got %d", agg.EvictedCount())
	}
}

func TestAggregatorMaxItemsGetter(t *testing.T) {
	agg := NewAggregatorWithCap(500)

	if agg.MaxItems() != 500 {
		t.Errorf("expected MaxItems() to return 500, got %d", agg.MaxItems())
	}
}

func TestAggregatorSetMaxItems(t *testing.T) {
	agg := NewAggregatorWithCap(100)

	// Add 100 items
	items := make([]Item, 100)
	baseTime := time.Now()
	for i := range items {
		items[i] = Item{
			ID:        fmt.Sprintf("item-%d", i),
			Title:     fmt.Sprintf("Test Article %d", i),
			URL:       fmt.Sprintf("https://example.com/article/%d", i),
			Published: baseTime.Add(time.Duration(i) * time.Minute),
		}
	}
	agg.MergeItems(items)

	// Verify at 100
	if agg.ItemCount() != 100 {
		t.Errorf("expected 100 items, got %d", agg.ItemCount())
	}

	// Lower the cap to 50 - should trigger immediate eviction
	agg.SetMaxItems(50)

	if agg.ItemCount() != 50 {
		t.Errorf("expected 50 items after SetMaxItems(50), got %d", agg.ItemCount())
	}
	if agg.EvictedCount() != 50 {
		t.Errorf("expected 50 evictions after SetMaxItems(50), got %d", agg.EvictedCount())
	}
	if agg.MaxItems() != 50 {
		t.Errorf("expected MaxItems() to return 50, got %d", agg.MaxItems())
	}
}

func TestAggregatorEvictionWithFetchedTimestamp(t *testing.T) {
	// Test that items with Fetched (but no Published) timestamp are sorted correctly
	agg := NewAggregatorWithCap(5)

	baseTime := time.Now()
	items := []Item{
		{ID: "pub-old", Title: "Published Old", URL: "https://example.com/1", Published: baseTime.Add(-5 * time.Hour)},
		{ID: "fetch-old", Title: "Fetched Old", URL: "https://example.com/2", Fetched: baseTime.Add(-4 * time.Hour)},
		{ID: "pub-new", Title: "Published New", URL: "https://example.com/3", Published: baseTime.Add(-1 * time.Hour)},
		{ID: "fetch-new", Title: "Fetched New", URL: "https://example.com/4", Fetched: baseTime.Add(-30 * time.Minute)},
		{ID: "pub-newest", Title: "Published Newest", URL: "https://example.com/5", Published: baseTime},
		// This one should cause eviction of oldest
		{ID: "newest", Title: "Newest", URL: "https://example.com/6", Published: baseTime.Add(1 * time.Minute)},
	}
	agg.MergeItems(items)

	remaining := agg.GetItems()
	if len(remaining) != 5 {
		t.Errorf("expected 5 items, got %d", len(remaining))
	}

	// pub-old should be evicted (oldest by Published time)
	for _, item := range remaining {
		if item.ID == "pub-old" {
			t.Error("pub-old should have been evicted as the oldest item")
		}
	}
}

func TestAggregatorDefaultMaxItems(t *testing.T) {
	// Test that default aggregator uses DefaultMaxItems constant
	agg := NewAggregator()

	if agg.MaxItems() != DefaultMaxItems {
		t.Errorf("expected default MaxItems to be %d, got %d", DefaultMaxItems, agg.MaxItems())
	}
}

func TestDefaultMaxItemsConstant(t *testing.T) {
	// Verify the constant is set to expected value
	if DefaultMaxItems != 10000 {
		t.Errorf("expected DefaultMaxItems to be 10000, got %d", DefaultMaxItems)
	}
}
