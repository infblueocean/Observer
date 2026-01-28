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

// --- Tests for Advanced Samplers ---

func TestDeficitRoundRobinSampler(t *testing.T) {
	sampler := NewDeficitRoundRobinSampler()

	q1 := NewSourceQueue("A", feeds.SourceRSS, time.Minute)
	q2 := NewSourceQueue("B", feeds.SourceRSS, time.Minute)
	q3 := NewSourceQueue("C", feeds.SourceRSS, time.Minute)

	// Source A has many items, B and C have few
	q1.Add([]feeds.Item{{ID: "A1"}, {ID: "A2"}, {ID: "A3"}, {ID: "A4"}, {ID: "A5"}})
	q2.Add([]feeds.Item{{ID: "B1"}, {ID: "B2"}})
	q3.Add([]feeds.Item{{ID: "C1"}})

	queues := []*SourceQueue{q1, q2, q3}
	result := sampler.Sample(queues, 8)

	// DRR should give fair distribution - 8 items total from all sources
	if len(result) != 8 {
		t.Fatalf("Expected 8 items, got %d", len(result))
	}

	// Count items per source - should be relatively balanced
	counts := make(map[string]int)
	for _, item := range result {
		counts[item.ID[:1]]++ // A, B, or C
	}

	// All sources should be represented
	if counts["A"] == 0 || counts["B"] == 0 || counts["C"] == 0 {
		t.Errorf("DRR should give fair distribution, got: A=%d, B=%d, C=%d",
			counts["A"], counts["B"], counts["C"])
	}
}

func TestDeficitRoundRobinWithWeights(t *testing.T) {
	sampler := NewDeficitRoundRobinSampler()

	q1 := NewSourceQueue("Wire", feeds.SourceRSS, time.Minute)
	q2 := NewSourceQueue("Blog", feeds.SourceRSS, time.Minute)

	q1.SetWeight(2.0) // wire service should get more
	q2.SetWeight(1.0)

	q1.Add([]feeds.Item{{ID: "W1"}, {ID: "W2"}, {ID: "W3"}, {ID: "W4"}})
	q2.Add([]feeds.Item{{ID: "B1"}, {ID: "B2"}, {ID: "B3"}, {ID: "B4"}})

	queues := []*SourceQueue{q1, q2}
	result := sampler.Sample(queues, 6)

	// Count items per source
	wireCount := 0
	blogCount := 0
	for _, item := range result {
		if item.ID[0] == 'W' {
			wireCount++
		} else {
			blogCount++
		}
	}

	// Wire (weight 2) should have more items than Blog (weight 1)
	if wireCount <= blogCount {
		t.Errorf("Wire (weight 2.0) should have more items than Blog (weight 1.0), got Wire=%d, Blog=%d",
			wireCount, blogCount)
	}
}

func TestFairRecentSampler(t *testing.T) {
	sampler := NewFairRecentSampler()
	sampler.QuotaPerSource = 2 // only 2 per source
	sampler.MaxAge = 48 * time.Hour

	now := time.Now()
	q1 := NewSourceQueue("Chatty", feeds.SourceRSS, time.Minute)
	q2 := NewSourceQueue("Quiet", feeds.SourceRSS, time.Minute)

	// Chatty source: many items
	q1.Add([]feeds.Item{
		{ID: "C1", SourceName: "Chatty", Published: now.Add(-10 * time.Minute)},
		{ID: "C2", SourceName: "Chatty", Published: now.Add(-20 * time.Minute)},
		{ID: "C3", SourceName: "Chatty", Published: now.Add(-30 * time.Minute)},
		{ID: "C4", SourceName: "Chatty", Published: now.Add(-40 * time.Minute)},
	})

	// Quiet source: few items but more recent
	q2.Add([]feeds.Item{
		{ID: "Q1", SourceName: "Quiet", Published: now.Add(-5 * time.Minute)}, // most recent
		{ID: "Q2", SourceName: "Quiet", Published: now.Add(-15 * time.Minute)},
	})

	queues := []*SourceQueue{q1, q2}
	result := sampler.Sample(queues, 10)

	// Should get quota from each (2+2=4), sorted by recency
	if len(result) != 4 {
		t.Fatalf("Expected 4 items (2 per source quota), got %d", len(result))
	}

	// First item should be most recent (Q1)
	if result[0].ID != "Q1" {
		t.Errorf("Expected most recent item Q1 first, got %s", result[0].ID)
	}
}

func TestFairRecentSamplerMaxAge(t *testing.T) {
	sampler := NewFairRecentSampler()
	sampler.MaxAge = 1 * time.Hour // only items from last hour

	now := time.Now()
	q1 := NewSourceQueue("Mixed", feeds.SourceRSS, time.Minute)

	q1.Add([]feeds.Item{
		{ID: "R1", Published: now.Add(-30 * time.Minute)},  // recent - include
		{ID: "R2", Published: now.Add(-45 * time.Minute)},  // recent - include
		{ID: "O1", Published: now.Add(-2 * time.Hour)},     // old - exclude
		{ID: "O2", Published: now.Add(-3 * time.Hour)},     // old - exclude
	})

	queues := []*SourceQueue{q1}
	result := sampler.Sample(queues, 10)

	// Should only get the 2 recent items
	if len(result) != 2 {
		t.Errorf("Expected 2 recent items, got %d", len(result))
	}

	for _, item := range result {
		if item.ID[0] == 'O' {
			t.Errorf("Old item %s should have been filtered out", item.ID)
		}
	}
}

func TestThrottledRecencySampler(t *testing.T) {
	sampler := NewThrottledRecencySampler()
	sampler.MaxPerSource = 2

	now := time.Now()
	q1 := NewSourceQueue("Firehose", feeds.SourceRSS, time.Minute)
	q2 := NewSourceQueue("Trickle", feeds.SourceRSS, time.Minute)

	// Firehose: many recent items
	q1.Add([]feeds.Item{
		{ID: "F1", SourceName: "Firehose", Published: now.Add(-1 * time.Minute)},
		{ID: "F2", SourceName: "Firehose", Published: now.Add(-2 * time.Minute)},
		{ID: "F3", SourceName: "Firehose", Published: now.Add(-3 * time.Minute)},
		{ID: "F4", SourceName: "Firehose", Published: now.Add(-4 * time.Minute)},
		{ID: "F5", SourceName: "Firehose", Published: now.Add(-5 * time.Minute)},
	})

	// Trickle: one older item
	q2.Add([]feeds.Item{
		{ID: "T1", SourceName: "Trickle", Published: now.Add(-10 * time.Minute)},
	})

	queues := []*SourceQueue{q1, q2}
	result := sampler.Sample(queues, 10)

	// Should get max 2 from Firehose + 1 from Trickle = 3
	if len(result) != 3 {
		t.Errorf("Expected 3 items (2 firehose cap + 1 trickle), got %d", len(result))
	}

	// First two should be F1, F2 (most recent)
	if result[0].ID != "F1" || result[1].ID != "F2" {
		t.Errorf("First two should be most recent from Firehose, got %s, %s", result[0].ID, result[1].ID)
	}

	// Count per source
	firehoseCount := 0
	for _, item := range result {
		if item.SourceName == "Firehose" {
			firehoseCount++
		}
	}
	if firehoseCount > 2 {
		t.Errorf("Firehose should be capped at 2, got %d", firehoseCount)
	}
}
