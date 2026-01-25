package feeds

import (
	"regexp"
	"strings"
)

// Filter determines if an item should be excluded
type Filter struct {
	// URL patterns to block
	BlockURLPatterns []*regexp.Regexp

	// Title patterns to block
	BlockTitlePatterns []*regexp.Regexp

	// Keywords in title/summary that indicate ads
	BlockKeywords []string

	// Source-specific URL patterns (source name -> patterns)
	SourceBlockPatterns map[string][]*regexp.Regexp
}

// DefaultFilter returns a filter configured to block common ad patterns
func DefaultFilter() *Filter {
	f := &Filter{
		BlockKeywords: []string{
			// Ads
			"sponsored",
			"advertisement",
			"paid content",
			"paid post",
			"partner content",
			"branded content",
			"promoted",
			"presented by",
			"brought to you by",
			"underwritten by",
			"[ad]",
			"[sponsored]",
			// Financial spam
			"credit card",
			"cash back card",
			"0% apr",
			"0% intro",
			"home equity",
			"balance transfer",
			"best cash back",
			"avoid credit card interest",
			"charging 0% interest",
			"cash out of your home",
		},
		SourceBlockPatterns: make(map[string][]*regexp.Regexp),
	}

	// Common ad URL patterns across sites
	f.BlockURLPatterns = compilePatterns([]string{
		`/sponsored/`,
		`/native/`,
		`/branded-content/`,
		`/partner/`,
		`/advertisement/`,
		`doubleclick\.net`,
		`googlesyndication\.com`,
		`/paid-post/`,
		`/commercial/`,
		`/promo/`,
		`utm_source=paid`,
		`/underwriter/`,
	})

	// Title patterns that indicate ads/promos
	f.BlockTitlePatterns = compilePatterns([]string{
		`(?i)^sponsored:`,
		`(?i)^ad:`,
		`(?i)\[sponsored\]`,
		`(?i)\[ad\]`,
		`(?i)paid post:`,
		`(?i)partner content:`,
		`(?i)^promo:`,
	})

	// CNN-specific patterns
	f.SourceBlockPatterns["CNN Top"] = compilePatterns([]string{
		`/cnn-underscored/`,
		`/specials/`,
		`cnn\.com/sponsored`,
		`cnn\.com/partners`,
		`/deals/`,
		`/coupons/`,
		`/reviews/`, // CNN Underscored reviews are basically ads
	})
	f.SourceBlockPatterns["CNN World"] = f.SourceBlockPatterns["CNN Top"]
	f.SourceBlockPatterns["CNN Politics"] = f.SourceBlockPatterns["CNN Top"]

	// Fox News patterns
	f.SourceBlockPatterns["Fox News"] = compilePatterns([]string{
		`/lifestyle/`,
		`/deals/`,
		`foxnews\.com/category/sponsored`,
	})

	// NBC/MSNBC patterns
	f.SourceBlockPatterns["NBC News"] = compilePatterns([]string{
		`/select/`,
		`/shopping/`,
		`nbcnews\.com/shopping`,
	})

	// USA Today
	f.SourceBlockPatterns["USA Today"] = compilePatterns([]string{
		`/reviewed/`,
		`/blueprint/`,
		`usatoday\.com/deals`,
	})

	// Forbes - lots of "contributor" spam
	f.SourceBlockPatterns["Forbes"] = compilePatterns([]string{
		`/sites/`, // Most contributor content
		`/advisor/`,
		`forbes\.com/advisor`,
	})

	return f
}

func compilePatterns(patterns []string) []*regexp.Regexp {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			result = append(result, re)
		}
	}
	return result
}

// ShouldBlock returns true if the item should be filtered out
func (f *Filter) ShouldBlock(item Item) bool {
	// Block empty titles
	title := strings.TrimSpace(item.Title)
	if title == "" {
		return true
	}

	// Check URL patterns
	for _, re := range f.BlockURLPatterns {
		if re.MatchString(item.URL) {
			return true
		}
	}

	// Check source-specific URL patterns
	if patterns, ok := f.SourceBlockPatterns[item.SourceName]; ok {
		for _, re := range patterns {
			if re.MatchString(item.URL) {
				return true
			}
		}
	}

	// Check title patterns
	for _, re := range f.BlockTitlePatterns {
		if re.MatchString(item.Title) {
			return true
		}
	}

	// Check keywords in title and summary
	titleLower := strings.ToLower(item.Title)
	summaryLower := strings.ToLower(item.Summary)
	for _, kw := range f.BlockKeywords {
		if strings.Contains(titleLower, kw) || strings.Contains(summaryLower, kw) {
			return true
		}
	}

	return false
}

// FilterItems returns items with blocked content removed
func (f *Filter) FilterItems(items []Item) []Item {
	result := make([]Item, 0, len(items))
	for _, item := range items {
		if !f.ShouldBlock(item) {
			result = append(result, item)
		}
	}
	return result
}

// BlockedCount returns how many items would be blocked
func (f *Filter) BlockedCount(items []Item) int {
	count := 0
	for _, item := range items {
		if f.ShouldBlock(item) {
			count++
		}
	}
	return count
}
