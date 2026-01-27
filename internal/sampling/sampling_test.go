package sampling

import (
	"os"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/logging"
)

func TestMain(m *testing.M) {
	// Initialize logging for tests
	logging.Init()
	os.Exit(m.Run())
}

func TestSourceQueueAdd(t *testing.T) {
	q := NewSourceQueue("test", feeds.SourceRSS, 5*time.Minute)

	items := []feeds.Item{
		{ID: "1", Title: "First"},
		{ID: "2", Title: "Second"},
		{ID: "3", Title: "Third"},
	}

	added := q.Add(items)
	if added != 3 {
		t.Errorf("Expected 3 items added, got %d", added)
	}

	if q.Len() != 3 {
		t.Errorf("Expected queue length 3, got %d", q.Len())
	}

	// Adding duplicates should not increase count
	added = q.Add(items)
	if added != 0 {
		t.Errorf("Expected 0 items added (duplicates), got %d", added)
	}

	if q.Len() != 3 {
		t.Errorf("Expected queue length still 3, got %d", q.Len())
	}
}

func TestSourceQueuePeek(t *testing.T) {
	q := NewSourceQueue("test", feeds.SourceRSS, 5*time.Minute)

	q.Add([]feeds.Item{
		{ID: "1", Title: "First"},
		{ID: "2", Title: "Second"},
	})

	// Peek should return item without removing
	item := q.Peek(0)
	if item == nil || item.ID != "1" {
		t.Errorf("Expected first item, got %v", item)
	}

	item = q.Peek(1)
	if item == nil || item.ID != "2" {
		t.Errorf("Expected second item, got %v", item)
	}

	// Out of bounds should return nil
	item = q.Peek(2)
	if item != nil {
		t.Errorf("Expected nil for out of bounds, got %v", item)
	}

	// Queue should still have both items
	if q.Len() != 2 {
		t.Errorf("Queue should still have 2 items, got %d", q.Len())
	}
}

func TestAdaptivePolling(t *testing.T) {
	q := NewSourceQueue("test", feeds.SourceRSS, 5*time.Minute)
	q.SetPollLimits(30*time.Second, 15*time.Minute)

	// Initially should poll immediately
	if !q.ShouldPoll() {
		t.Error("Should poll initially (never polled)")
	}

	// Add items (simulates finding new content)
	q.Add([]feeds.Item{{ID: "1", Title: "New"}})

	// Adjust should speed up
	initialInterval := q.PollInterval()
	q.AdjustInterval()
	if q.PollInterval() >= initialInterval {
		t.Error("Should speed up polling after finding content")
	}

	// Simulate no new content
	q.Add([]feeds.Item{}) // empty add sets lastNewCount to 0
	q.AdjustInterval()
	afterSlowdown := q.PollInterval()
	if afterSlowdown <= q.minPoll {
		// Might have hit floor, that's ok
	}

	// Multiple empty polls should slow down toward ceiling
	for i := 0; i < 10; i++ {
		q.Add([]feeds.Item{})
		q.AdjustInterval()
	}
	if q.PollInterval() > q.maxPoll {
		t.Errorf("Poll interval %v should not exceed max %v", q.PollInterval(), q.maxPoll)
	}
}

func TestRoundRobinSampler(t *testing.T) {
	sampler := NewRoundRobinSampler()

	q1 := NewSourceQueue("A", feeds.SourceRSS, time.Minute)
	q2 := NewSourceQueue("B", feeds.SourceRSS, time.Minute)
	q3 := NewSourceQueue("C", feeds.SourceRSS, time.Minute)

	q1.Add([]feeds.Item{{ID: "A1"}, {ID: "A2"}, {ID: "A3"}})
	q2.Add([]feeds.Item{{ID: "B1"}, {ID: "B2"}})
	q3.Add([]feeds.Item{{ID: "C1"}})

	queues := []*SourceQueue{q1, q2, q3}
	result := sampler.Sample(queues, 6)

	// Should get items interleaved: A1, B1, C1, A2, B2, A3
	expected := []string{"A1", "B1", "C1", "A2", "B2", "A3"}
	if len(result) != len(expected) {
		t.Fatalf("Expected %d items, got %d", len(expected), len(result))
	}

	for i, item := range result {
		if item.ID != expected[i] {
			t.Errorf("Position %d: expected %s, got %s", i, expected[i], item.ID)
		}
	}
}

func TestRoundRobinSamplerWithLimit(t *testing.T) {
	sampler := &RoundRobinSampler{MaxPerSource: 2}

	q1 := NewSourceQueue("chatty", feeds.SourceRSS, time.Minute)
	q2 := NewSourceQueue("quiet", feeds.SourceRSS, time.Minute)

	// Chatty source has many items
	q1.Add([]feeds.Item{{ID: "C1"}, {ID: "C2"}, {ID: "C3"}, {ID: "C4"}, {ID: "C5"}})
	// Quiet source has few
	q2.Add([]feeds.Item{{ID: "Q1"}})

	queues := []*SourceQueue{q1, q2}
	result := sampler.Sample(queues, 10)

	// Should get max 2 from chatty + 1 from quiet = 3 items
	if len(result) != 3 {
		t.Errorf("Expected 3 items (2 chatty + 1 quiet), got %d", len(result))
	}
}

func TestRecencyMergeSampler(t *testing.T) {
	sampler := NewRecencyMergeSampler()

	now := time.Now()
	q1 := NewSourceQueue("A", feeds.SourceRSS, time.Minute)
	q2 := NewSourceQueue("B", feeds.SourceRSS, time.Minute)

	q1.Add([]feeds.Item{
		{ID: "A1", Published: now.Add(-1 * time.Hour)},
		{ID: "A2", Published: now.Add(-3 * time.Hour)},
	})
	q2.Add([]feeds.Item{
		{ID: "B1", Published: now.Add(-30 * time.Minute)}, // most recent
		{ID: "B2", Published: now.Add(-2 * time.Hour)},
	})

	queues := []*SourceQueue{q1, q2}
	result := sampler.Sample(queues, 4)

	// Should be sorted by recency: B1 (30m), A1 (1h), B2 (2h), A2 (3h)
	if len(result) != 4 {
		t.Fatalf("Expected 4 items, got %d", len(result))
	}

	expected := []string{"B1", "A1", "B2", "A2"}
	for i, item := range result {
		if item.ID != expected[i] {
			t.Errorf("Position %d: expected %s, got %s", i, expected[i], item.ID)
		}
	}
}

func TestQueueManager(t *testing.T) {
	manager := NewQueueManager(NewRoundRobinSampler())

	// Register sources
	manager.RegisterSource("Reuters", feeds.SourceRSS, time.Minute)
	manager.RegisterSource("HN", feeds.SourceHN, 2*time.Minute)

	// Add items
	manager.AddItems("Reuters", []feeds.Item{{ID: "R1"}, {ID: "R2"}})
	manager.AddItems("HN", []feeds.Item{{ID: "H1"}})

	if manager.TotalItems() != 3 {
		t.Errorf("Expected 3 total items, got %d", manager.TotalItems())
	}

	// Sample
	items := manager.Sample(3)
	if len(items) != 3 {
		t.Errorf("Expected 3 sampled items, got %d", len(items))
	}

	// Stats
	stats := manager.Stats()
	if stats.SourceCount != 2 {
		t.Errorf("Expected 2 sources, got %d", stats.SourceCount)
	}
}
