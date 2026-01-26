package feeds

import (
	"testing"
)

func TestDefaultFilter(t *testing.T) {
	f := DefaultFilter()

	if f == nil {
		t.Fatal("DefaultFilter() returned nil")
	}

	if len(f.BlockKeywords) == 0 {
		t.Error("BlockKeywords should not be empty")
	}

	if len(f.BlockURLPatterns) == 0 {
		t.Error("BlockURLPatterns should not be empty")
	}

	if len(f.BlockTitlePatterns) == 0 {
		t.Error("BlockTitlePatterns should not be empty")
	}
}

func TestShouldBlockEmptyTitle(t *testing.T) {
	f := DefaultFilter()

	item := Item{
		Title: "",
		URL:   "https://example.com/article",
	}

	if !f.ShouldBlock(item) {
		t.Error("Should block items with empty titles")
	}

	item.Title = "   " // whitespace only
	if !f.ShouldBlock(item) {
		t.Error("Should block items with whitespace-only titles")
	}
}

func TestShouldBlockKeywords(t *testing.T) {
	f := DefaultFilter()

	tests := []struct {
		title   string
		blocked bool
	}{
		{"Normal article title", false},
		{"SPONSORED: Check out this product", true},
		{"Get a $200 Cash Back Bonus today!", true},
		{"Best credit card for travel", true},
		{"0% APR until 2025", true},
		{"Home equity loan options", true},
		{"Flash sale: 50% off", true},
		{"Breaking: Major news event", false},
		{"Act now - limited time offer", true},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			item := Item{Title: tt.title, URL: "https://example.com"}
			got := f.ShouldBlock(item)
			if got != tt.blocked {
				t.Errorf("ShouldBlock(%q) = %v, want %v", tt.title, got, tt.blocked)
			}
		})
	}
}

func TestShouldBlockURLPatterns(t *testing.T) {
	f := DefaultFilter()

	tests := []struct {
		url     string
		blocked bool
	}{
		{"https://example.com/news/article", false},
		{"https://example.com/sponsored/deal", true},
		{"https://cnn.com/cnn-underscored/review", true},
		{"https://example.com/native/content", true},
		{"https://news.site.com/world/breaking", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			item := Item{Title: "Test Article", URL: tt.url}
			got := f.ShouldBlock(item)
			if got != tt.blocked {
				t.Errorf("ShouldBlock(url=%q) = %v, want %v", tt.url, got, tt.blocked)
			}
		})
	}
}

func TestShouldBlockTitlePatterns(t *testing.T) {
	f := DefaultFilter()

	tests := []struct {
		title   string
		blocked bool
	}{
		{"Normal news headline", false},
		{"$500 Cash Back Bonus on purchases", true},
		{"Save $200 on your next order", true},
		{"15% interest rate changes", true},
		{"Best card for 2024", true},
		{"Top 10 cards for rewards", true},
		{"Record-low price on gadget", true},
		{"50% off everything", true},
		{"Government announces new policy", false},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			item := Item{Title: tt.title, URL: "https://example.com"}
			got := f.ShouldBlock(item)
			if got != tt.blocked {
				t.Errorf("ShouldBlock(title=%q) = %v, want %v", tt.title, got, tt.blocked)
			}
		})
	}
}

func TestSourceSpecificPatterns(t *testing.T) {
	f := DefaultFilter()

	tests := []struct {
		sourceName string
		url        string
		blocked    bool
	}{
		{"CNN Top", "https://cnn.com/news/world", false},
		{"CNN Top", "https://cnn.com/cnn-underscored/deals", true},
		{"CNN Top", "https://cnn.com/specials/promo", true},
		{"Fox News", "https://foxnews.com/news/politics", false},
		{"Fox News", "https://foxnews.com/deals/shopping", true},
		{"NBC News", "https://nbcnews.com/world", false},
		{"NBC News", "https://nbcnews.com/select/shopping", true},
	}

	for _, tt := range tests {
		t.Run(tt.sourceName+"/"+tt.url, func(t *testing.T) {
			item := Item{
				Title:      "Test Article",
				URL:        tt.url,
				SourceName: tt.sourceName,
			}
			got := f.ShouldBlock(item)
			if got != tt.blocked {
				t.Errorf("ShouldBlock(source=%q, url=%q) = %v, want %v",
					tt.sourceName, tt.url, got, tt.blocked)
			}
		})
	}
}

func TestFilterItems(t *testing.T) {
	f := DefaultFilter()

	items := []Item{
		{Title: "Good article 1", URL: "https://example.com/1"},
		{Title: "", URL: "https://example.com/2"}, // empty title
		{Title: "Good article 2", URL: "https://example.com/3"},
		{Title: "SPONSORED: Bad article", URL: "https://example.com/4"},
		{Title: "Good article 3", URL: "https://example.com/5"},
		{Title: "$200 Cash Back", URL: "https://example.com/6"},
	}

	filtered := f.FilterItems(items)

	if len(filtered) != 3 {
		t.Errorf("FilterItems returned %d items, want 3", len(filtered))
	}

	// Verify the right items were kept
	for _, item := range filtered {
		if item.Title == "" || item.Title == "SPONSORED: Bad article" || item.Title == "$200 Cash Back" {
			t.Errorf("FilterItems should have removed %q", item.Title)
		}
	}
}

func TestBlockedCount(t *testing.T) {
	f := DefaultFilter()

	items := []Item{
		{Title: "Good article", URL: "https://example.com/1"},
		{Title: "", URL: "https://example.com/2"},
		{Title: "SPONSORED: Ad", URL: "https://example.com/3"},
	}

	count := f.BlockedCount(items)

	if count != 2 {
		t.Errorf("BlockedCount = %d, want 2", count)
	}
}

func TestConferencePromoFiltering(t *testing.T) {
	f := DefaultFilter()

	// These should be blocked
	promoItems := []Item{
		{Title: "Secure Your Spot at RSAC 2026 Conference", URL: "https://example.com/1"},
		{Title: "Register Now for Black Hat 2026", URL: "https://example.com/2"},
		{Title: "Join Us at DEF CON for Exclusive Training", URL: "https://example.com/3"},
		{Title: "Webinar: Top Security Trends for 2026", URL: "https://example.com/4"},
		{Title: "Free Webinar on Cloud Security", URL: "https://example.com/5"},
		{Title: "Early Bird Registration Now Open", URL: "https://example.com/6"},
		{Title: "Reserve Your Seat at Our Virtual Summit", URL: "https://example.com/7"},
	}

	for _, item := range promoItems {
		if !f.ShouldBlock(item) {
			t.Errorf("ShouldBlock should block promotional item: %q", item.Title)
		}
	}

	// These should NOT be blocked (actual news about conferences)
	newsItems := []Item{
		{Title: "RSA Conference Announces New Security Research", URL: "https://example.com/8"},
		{Title: "Black Hat Reveals Major Vulnerabilities", URL: "https://example.com/9"},
		{Title: "DEF CON Hackers Discover Critical Flaw", URL: "https://example.com/10"},
	}

	for _, item := range newsItems {
		if f.ShouldBlock(item) {
			t.Errorf("ShouldBlock should NOT block news item: %q", item.Title)
		}
	}
}
