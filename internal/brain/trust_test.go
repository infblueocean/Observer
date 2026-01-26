package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

func TestParseTopStoriesPipeFormat(t *testing.T) {
	items := []feeds.Item{
		{ID: "1", Title: "Story 1", SourceName: "BBC"},
		{ID: "2", Title: "Story 2", SourceName: "CNN"},
		{ID: "3", Title: "Story 3", SourceName: "Reuters"},
	}

	// Test valid pipe format
	content := `BREAKING|1|Major earthquake
DEVELOPING|2|Election updates
TOP|3|Climate agreement`

	results := parseTopStoriesPipeFormat(content, items, 3)

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	if results[0].Label != "● NEW" {
		t.Errorf("Expected NEW label, got %s", results[0].Label)
	}
	if results[0].ItemID != "1" {
		t.Errorf("Expected item ID 1, got %s", results[0].ItemID)
	}
}

func TestParseTopStoriesMarkdown(t *testing.T) {
	items := []feeds.Item{
		{ID: "1", Title: "Major earthquake hits Japan killing dozens", SourceName: "BBC"},
		{ID: "2", Title: "Election results are finally in after long count", SourceName: "CNN"},
		{ID: "3", Title: "Climate summit reaches historic deal on emissions", SourceName: "Reuters"},
	}

	// Test markdown format with [Source] Title style
	content1 := `Here are the top stories:

1. **[BBC] Major earthquake hits Japan killing dozens**: This is significant...
2. **[CNN] Election results are finally in**: Important political development...
3. **[Reuters] Climate summit reaches historic deal**: Historic agreement...`

	results := parseTopStoriesMarkdown(content1, items, 3)
	if len(results) < 1 {
		t.Errorf("Expected at least 1 result from [Source] format, got %d", len(results))
	}

	// Test markdown format with Title - Source style (common from local models)
	content2 := `Based on the headlines, here are the top 3:

1. **Major earthquake hits Japan killing dozens - BBC**
   - This is a breaking news event with casualties

2. **Election results are finally in after long count - CNN**
   - Important political development

3. **Climate summit reaches historic deal on emissions - Reuters**
   - Significant international agreement`

	results2 := parseTopStoriesMarkdown(content2, items, 3)
	if len(results2) < 1 {
		t.Errorf("Expected at least 1 result from Title - Source format, got %d", len(results2))
	}
}

func TestMapLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"BREAKING", "● NEW"},
		{"breaking", "● NEW"},
		{"DEVELOPING", "◐ EMERGING"},
		{"TOP", "◦ NOTED"},
		{"TOP STORY", "◦ NOTED"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		result := mapLabel(tt.input)
		if result != tt.expected {
			t.Errorf("mapLabel(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestAnalyzerProviderManagement(t *testing.T) {
	a := NewAnalyzer(nil)

	if a.getRandomProvider() != nil {
		t.Error("Expected nil provider when none added")
	}

	// Test that nil providers are handled gracefully
	a.AddProvider(nil)
	if a.getRandomProvider() != nil {
		t.Error("Expected nil provider after adding nil")
	}
}

func TestTopStoriesCache(t *testing.T) {
	a := NewAnalyzer(nil)

	// Initial cache should be empty
	if a.GetTopStoriesCacheSize() != 0 {
		t.Errorf("Expected empty cache, got size %d", a.GetTopStoriesCacheSize())
	}

	// Simulate first analysis results
	results1 := []TopStoryResult{
		{ItemID: "story-1", Label: "● NEW", Reason: "Major event"},
		{ItemID: "story-2", Label: "◐ EMERGING", Reason: "Ongoing"},
	}
	itemTitles := map[string]string{
		"story-1": "Major earthquake hits",
		"story-2": "Election count continues",
	}

	enriched1 := a.updateTopStoriesCache(results1, itemTitles)

	// First time, hit count should be 1
	if enriched1[0].HitCount != 1 {
		t.Errorf("Expected HitCount 1, got %d", enriched1[0].HitCount)
	}
	if enriched1[0].Streak {
		t.Error("Expected Streak false for first identification")
	}
	if enriched1[0].FirstSeen.IsZero() {
		t.Error("Expected FirstSeen to be set")
	}

	// Cache should now have 2 entries
	if a.GetTopStoriesCacheSize() != 2 {
		t.Errorf("Expected cache size 2, got %d", a.GetTopStoriesCacheSize())
	}

	// Simulate second analysis - story-1 appears again (streak), story-3 is new
	results2 := []TopStoryResult{
		{ItemID: "story-1", Label: "● NEW", Reason: "Still major"},
		{ItemID: "story-3", Label: "◦ NOTED", Reason: "New story"},
	}
	itemTitles["story-3"] = "Climate summit begins"

	enriched2 := a.updateTopStoriesCache(results2, itemTitles)

	// story-1 should have hit count 2 and streak true
	if enriched2[0].HitCount != 2 {
		t.Errorf("Expected HitCount 2 for story-1, got %d", enriched2[0].HitCount)
	}
	if !enriched2[0].Streak {
		t.Error("Expected Streak true for consecutive identification")
	}

	// story-3 should be new (hit count 1, no streak)
	if enriched2[1].HitCount != 1 {
		t.Errorf("Expected HitCount 1 for story-3, got %d", enriched2[1].HitCount)
	}
	if enriched2[1].Streak {
		t.Error("Expected Streak false for new story")
	}

	// Cache should now have 3 entries
	if a.GetTopStoriesCacheSize() != 3 {
		t.Errorf("Expected cache size 3, got %d", a.GetTopStoriesCacheSize())
	}
}

func TestTopStoriesCachePrune(t *testing.T) {
	a := NewAnalyzer(nil)

	// Add some cached entries directly
	a.topStoriesCache.entries["old-story"] = &CachedTopStory{
		ItemID:   "old-story",
		LastSeen: time.Now().Add(-2 * time.Hour), // 2 hours ago
		HitCount: 1,
	}
	a.topStoriesCache.entries["recent-story"] = &CachedTopStory{
		ItemID:   "recent-story",
		LastSeen: time.Now().Add(-30 * time.Minute), // 30 mins ago
		HitCount: 3,
	}

	// Prune entries older than 1 hour
	pruned := a.PruneTopStoriesCache(1 * time.Hour)

	if pruned != 1 {
		t.Errorf("Expected 1 pruned, got %d", pruned)
	}
	if a.GetTopStoriesCacheSize() != 1 {
		t.Errorf("Expected cache size 1 after prune, got %d", a.GetTopStoriesCacheSize())
	}
}

func TestGetTopStoriesCache(t *testing.T) {
	a := NewAnalyzer(nil)

	// Add entries with different hit counts
	now := time.Now()
	a.topStoriesCache.entries["low"] = &CachedTopStory{
		ItemID:   "low",
		HitCount: 1,
		LastSeen: now,
	}
	a.topStoriesCache.entries["high"] = &CachedTopStory{
		ItemID:   "high",
		HitCount: 5,
		LastSeen: now,
	}
	a.topStoriesCache.entries["mid"] = &CachedTopStory{
		ItemID:   "mid",
		HitCount: 3,
		LastSeen: now,
	}

	cached := a.GetTopStoriesCache()

	// Should be sorted by hit count descending
	if len(cached) != 3 {
		t.Fatalf("Expected 3 cached entries, got %d", len(cached))
	}
	if cached[0].HitCount != 5 {
		t.Errorf("Expected first entry to have HitCount 5, got %d", cached[0].HitCount)
	}
	if cached[1].HitCount != 3 {
		t.Errorf("Expected second entry to have HitCount 3, got %d", cached[1].HitCount)
	}
	if cached[2].HitCount != 1 {
		t.Errorf("Expected third entry to have HitCount 1, got %d", cached[2].HitCount)
	}
}

func TestCalculateStatus(t *testing.T) {
	tests := []struct {
		hitCount  int
		missCount int
		expected  TopStoryStatus
	}{
		{1, 0, StatusBreaking},
		{2, 0, StatusDeveloping},
		{3, 0, StatusDeveloping},
		{4, 0, StatusPersistent},
		{5, 0, StatusPersistent},
		{10, 0, StatusPersistent},
		{4, 1, StatusSustained},  // High hit, missed once = sustained
		{5, 1, StatusSustained},  // Still sustained
		{10, 1, StatusSustained}, // High confidence sustained
		{5, 2, StatusFading},     // High hit but many misses = fading
		{1, 3, StatusFading},     // Even new stories fade if missed
		{3, 1, StatusDeveloping}, // Not enough hits for sustained
	}

	for _, tt := range tests {
		result := calculateStatus(tt.hitCount, tt.missCount)
		if result != tt.expected {
			t.Errorf("calculateStatus(%d, %d) = %s, want %s",
				tt.hitCount, tt.missCount, result, tt.expected)
		}
	}
}

func TestGetBreathingTopStories(t *testing.T) {
	a := NewAnalyzer(nil)

	// Seed the cache with some stories
	now := time.Now()
	a.topStoriesCache.entries["persistent-story"] = &CachedTopStory{
		ItemID:    "persistent-story",
		Title:     "Major Ongoing Event",
		Label:     "◉ ONGOING",
		Reason:    "Still developing",
		FirstSeen: now.Add(-2 * time.Hour),
		LastSeen:  now.Add(-1 * time.Minute),
		HitCount:  5,
		MissCount: 1, // Missed once but still high confidence
	}
	a.topStoriesCache.entries["fading-story"] = &CachedTopStory{
		ItemID:    "fading-story",
		Title:     "Old Story",
		Label:     "○ FADING",
		Reason:    "Was important",
		FirstSeen: now.Add(-4 * time.Hour),
		LastSeen:  now.Add(-30 * time.Minute),
		HitCount:  3,
		MissCount: 3, // Missed too many times
	}

	// Current results from LLM
	currentResults := []TopStoryResult{
		{ItemID: "new-breaking", Label: "● NEW", Reason: "Just happened", HitCount: 1, Status: StatusBreaking},
		{ItemID: "current-developing", Label: "◐ EMERGING", Reason: "Ongoing", HitCount: 2, Status: StatusDeveloping},
	}

	// Get breathing list
	breathing := a.GetBreathingTopStories(currentResults, 8)

	// Should include: new-breaking, current-developing, persistent-story
	// Should NOT include: fading-story (too many misses)
	if len(breathing) < 2 {
		t.Errorf("Expected at least 2 stories, got %d", len(breathing))
	}

	// Check that persistent story is included
	foundPersistent := false
	for _, s := range breathing {
		if s.ItemID == "persistent-story" {
			foundPersistent = true
		}
	}
	if !foundPersistent {
		t.Error("Expected persistent story to be included in breathing list")
	}

	// Check ordering: breaking should come first
	if len(breathing) > 0 && breathing[0].Status != StatusBreaking {
		t.Errorf("Expected first story to be breaking, got %s", breathing[0].Status)
	}
}

func TestStoryLess(t *testing.T) {
	breaking := TopStoryResult{Status: StatusBreaking, HitCount: 1}
	persistent := TopStoryResult{Status: StatusPersistent, HitCount: 5}
	sustained := TopStoryResult{Status: StatusSustained, HitCount: 4}
	developing := TopStoryResult{Status: StatusDeveloping, HitCount: 2}
	fading := TopStoryResult{Status: StatusFading, HitCount: 3}

	// Breaking should come before everything
	if !storyLess(breaking, persistent) {
		t.Error("Breaking should come before persistent")
	}
	if !storyLess(breaking, sustained) {
		t.Error("Breaking should come before sustained")
	}
	if !storyLess(breaking, developing) {
		t.Error("Breaking should come before developing")
	}
	if !storyLess(breaking, fading) {
		t.Error("Breaking should come before fading")
	}

	// Persistent should come before sustained, developing and fading
	if !storyLess(persistent, sustained) {
		t.Error("Persistent should come before sustained")
	}
	if !storyLess(persistent, developing) {
		t.Error("Persistent should come before developing")
	}
	if !storyLess(persistent, fading) {
		t.Error("Persistent should come before fading")
	}

	// Sustained should come before developing and fading
	if !storyLess(sustained, developing) {
		t.Error("Sustained should come before developing")
	}
	if !storyLess(sustained, fading) {
		t.Error("Sustained should come before fading")
	}

	// Within same status, higher hit count wins
	highHit := TopStoryResult{Status: StatusDeveloping, HitCount: 5}
	lowHit := TopStoryResult{Status: StatusDeveloping, HitCount: 2}
	if !storyLess(highHit, lowHit) {
		t.Error("Higher hit count should come before lower")
	}
}

func TestGetTopStoriesContext(t *testing.T) {
	a := NewAnalyzer(nil)

	// Empty cache should return empty string
	ctx := a.GetTopStoriesContext()
	if ctx != "" {
		t.Errorf("Expected empty context for empty cache, got %q", ctx)
	}

	// Add some cached stories
	now := time.Now()
	a.topStoriesCache.entries["story-1"] = &CachedTopStory{
		ItemID:    "story-1",
		Title:     "Major Breaking News Event",
		Label:     "● NEW",
		HitCount:  3,
		MissCount: 0,
		LastSeen:  now,
	}
	a.topStoriesCache.entries["story-2"] = &CachedTopStory{
		ItemID:    "story-2",
		Title:     "Developing Story Updates",
		Label:     "◐ EMERGING",
		HitCount:  2,
		MissCount: 0,
		LastSeen:  now,
	}
	// Fading story should be excluded
	a.topStoriesCache.entries["story-3"] = &CachedTopStory{
		ItemID:    "story-3",
		Title:     "Old Fading Story",
		Label:     "○ FADING",
		HitCount:  1,
		MissCount: 3, // Too many misses = fading
		LastSeen:  now.Add(-1 * time.Hour),
	}

	ctx = a.GetTopStoriesContext()

	// Should contain the header
	if !strings.Contains(ctx, "CURRENT TOP STORIES:") {
		t.Error("Context should contain header")
	}

	// Should contain the active stories
	if !strings.Contains(ctx, "Major Breaking News Event") {
		t.Error("Context should contain story-1")
	}
	if !strings.Contains(ctx, "Developing Story Updates") {
		t.Error("Context should contain story-2")
	}

	// Should NOT contain the fading story
	if strings.Contains(ctx, "Old Fading Story") {
		t.Error("Context should NOT contain fading story")
	}

	// Higher hit count should come first
	idx1 := strings.Index(ctx, "Major Breaking")
	idx2 := strings.Index(ctx, "Developing Story")
	if idx1 > idx2 {
		t.Error("Higher hit count story should come first")
	}
}

func TestCleanReason(t *testing.T) {
	items := []feeds.Item{
		{ID: "1", Title: "Seahawks, Patriots set to meet in Super Bowl rematch", SourceName: "CBS News"},
		{ID: "2", Title: "Major earthquake hits Japan killing dozens", SourceName: "BBC"},
		{ID: "3", Title: "Climate summit reaches historic deal on emissions", SourceName: "Reuters"},
	}

	tests := []struct {
		name     string
		reason   string
		expected string // empty means should be rejected
	}{
		{
			name:     "valid short reason",
			reason:   "Major earthquake with casualties",
			expected: "Major earthquake with casualties",
		},
		{
			name:     "reason with source name should be rejected",
			reason:   "CBS News reports major event",
			expected: "",
		},
		{
			name:     "reason with headline text should be rejected",
			reason:   "Seahawks, Patriots set to meet",
			expected: "",
		},
		{
			name:     "reason with markdown should be rejected",
			reason:   "**Breaking news** about event",
			expected: "",
		},
		{
			name:     "reason with asterisks should be rejected",
			reason:   "*Seahawks, Patriots set to meet in Super Bowl rematch* (CBS News)",
			expected: "",
		},
		{
			name:     "overly long reason should be rejected",
			reason:   "This is a very long reason that goes on and on and on about the story and contains way too much information to be useful as a short summary",
			expected: "",
		},
		{
			name:     "empty reason should be preserved",
			reason:   "",
			expected: "",
		},
		{
			name:     "valid medium reason",
			reason:   "Historic climate agreement signed by world leaders",
			expected: "Historic climate agreement signed by world leaders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanReason(tt.reason, items, 50)
			if result != tt.expected {
				t.Errorf("cleanReason(%q) = %q, want %q", tt.reason, result, tt.expected)
			}
		})
	}
}
