package brain

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/logging"
)

// Analysis is the result of analyzing an item
type Analysis struct {
	Content  string
	Error    error
	Loading  bool
	Provider string // Which AI model provided this analysis
	Stage    string // Current stage: "starting", "searching", "analyzing", "complete"
}

// AnalysisStore is the interface for persisting analyses
type AnalysisStore interface {
	SaveAnalysis(itemID, provider, model, prompt, rawResponse, content, errMsg string) error
}

// Analyzer coordinates AI analysis of content
type Analyzer struct {
	providers        []Provider // Multiple providers for random selection
	store            AnalysisStore
	mu               sync.RWMutex
	analyses         map[string]*Analysis // item ID -> analysis
	callbacks        []func(itemID string, analysis Analysis)
	preferLocal      bool // Prefer local models for analysis
	localForQuickOps bool // Use local for quick operations
	topStoriesCache  *TopStoriesCache
}

// NewAnalyzer creates a new AI Analyzer
func NewAnalyzer(provider Provider) *Analyzer {
	a := &Analyzer{
		analyses: make(map[string]*Analysis),
		topStoriesCache: &TopStoriesCache{
			entries: make(map[string]*CachedTopStory),
		},
	}
	if provider != nil {
		a.providers = []Provider{provider}
	}
	return a
}

// SetStore sets the persistence store for saving analyses
func (a *Analyzer) SetStore(store AnalysisStore) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.store = store
}

// SetPreferences configures analysis behavior
func (a *Analyzer) SetPreferences(preferLocal, localForQuickOps bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.preferLocal = preferLocal
	a.localForQuickOps = localForQuickOps
	logging.Info("Analyzer preferences set", "preferLocal", preferLocal, "localForQuickOps", localForQuickOps)
}

// SetProvider updates the AI provider (replaces all providers with this one)
func (a *Analyzer) SetProvider(provider Provider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if provider != nil {
		a.providers = []Provider{provider}
	} else {
		a.providers = nil
	}
}

// AddProvider adds an additional AI provider
func (a *Analyzer) AddProvider(provider Provider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if provider != nil {
		a.providers = append(a.providers, provider)
	}
}

// getRandomProvider returns a random available provider
func (a *Analyzer) getRandomProvider() Provider {
	var available []Provider
	for _, p := range a.providers {
		if p != nil && p.Available() {
			available = append(available, p)
		}
	}
	if len(available) == 0 {
		return nil
	}
	return available[rand.Intn(len(available))]
}

// getLocalProvider returns the local (ollama) provider if available
func (a *Analyzer) getLocalProvider() Provider {
	for _, p := range a.providers {
		if p != nil && p.Name() == "ollama" && p.Available() {
			return p
		}
	}
	return nil
}

// getCloudProvider returns a cloud provider (non-ollama) if available
func (a *Analyzer) getCloudProvider() Provider {
	var cloud []Provider
	for _, p := range a.providers {
		if p != nil && p.Name() != "ollama" && p.Available() {
			cloud = append(cloud, p)
		}
	}
	if len(cloud) == 0 {
		return nil
	}
	return cloud[rand.Intn(len(cloud))]
}

// OnAnalysis registers a callback for when analysis completes
func (a *Analyzer) OnAnalysis(callback func(itemID string, analysis Analysis)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.callbacks = append(a.callbacks, callback)
}

// Analyze triggers unified AI analysis of an item
// Uses two-phase approach: fast local model first (if available and preferred), then cloud model
// AnalyzeWithContext analyzes an item with additional context (like top stories)
func (a *Analyzer) AnalyzeWithContext(ctx context.Context, item feeds.Item, topStoriesContext string) {
	a.analyzeInternal(ctx, item, topStoriesContext)
}

// Analyze analyzes an item (legacy method without context)
func (a *Analyzer) Analyze(ctx context.Context, item feeds.Item) {
	a.analyzeInternal(ctx, item, "")
}

func (a *Analyzer) analyzeInternal(ctx context.Context, item feeds.Item, topStoriesContext string) {
	a.mu.RLock()
	// Check if analysis already in progress
	if existing, ok := a.analyses[item.ID]; ok && existing != nil && existing.Loading {
		logging.Debug("AI analysis already in progress", "item", item.ID)
		a.mu.RUnlock()
		return
	}

	// Get local (fast) and cloud providers
	localProvider := a.getLocalProvider()
	cloudProvider := a.getCloudProvider()
	preferLocal := a.preferLocal
	a.mu.RUnlock()

	// Need at least one provider
	if localProvider == nil && cloudProvider == nil {
		logging.Debug("AI analysis skipped - no provider available")
		return
	}

	// If not preferring local, skip the two-phase approach
	if !preferLocal {
		localProvider = nil
	}

	// Determine which provider to show initially
	initialProvider := localProvider
	if initialProvider == nil {
		initialProvider = cloudProvider
	}

	providerName := initialProvider.Name()
	logging.Info("AI analysis started", "item", item.Title, "provider", providerName,
		"has_local", localProvider != nil, "has_cloud", cloudProvider != nil)

	// Build the item summary for analysis
	itemSummary := fmt.Sprintf("Title: %s\n", item.Title)
	if item.Summary != "" {
		itemSummary += fmt.Sprintf("Summary: %s\n", item.Summary)
	}
	if item.SourceName != "" {
		itemSummary += fmt.Sprintf("Source: %s\n", item.SourceName)
	}
	if item.URL != "" {
		itemSummary += fmt.Sprintf("URL: %s\n", item.URL)
	}

	// Check if top stories context was provided
	hasTopStories := topStoriesContext != ""

	var systemPrompt string
	if hasTopStories {
		systemPrompt = `You are a thoughtful news analyst. Provide insightful analysis of the given news item.

Your analysis should include:

ANALYSIS (2-3 paragraphs):
- What this news likely means based on the headline and context
- Historical context or precedents
- What questions this raises or what's missing
- Potential implications

CONNECTIONS (1 paragraph):
- How this story relates to the current top stories listed below
- Common themes, causes, or implications across stories
- If no meaningful connection exists, briefly note why this story stands apart

Be direct and analytical. No bullet points, use flowing prose.`
	} else {
		systemPrompt = `You are a thoughtful news analyst. Provide insightful analysis of the given news item.

Your analysis should weave together:
- What this news likely means based on the headline and context
- Historical context or precedents
- What questions this raises or what's missing
- Potential implications and how this connects to broader trends

Write 2-3 substantive paragraphs. Be direct and analytical. No fluff, no bullet points, no section headers.`
	}

	var userPrompt string
	if hasTopStories {
		userPrompt = fmt.Sprintf("Analyze this news item:\n\n%s\n%s", itemSummary, topStoriesContext)
	} else {
		userPrompt = fmt.Sprintf("Analyze this news item:\n\n%s", itemSummary)
	}

	// Initialize loading state
	a.mu.Lock()
	a.analyses[item.ID] = &Analysis{Loading: true, Provider: providerName, Stage: "starting"}
	a.mu.Unlock()

	// Track if cloud result already arrived (to avoid overwriting with slower local)
	cloudDone := make(chan struct{})

	// If we have both local and cloud, run local first for quick feedback
	if localProvider != nil && cloudProvider != nil {
		// Start local analysis (fast)
		go func() {
			a.mu.Lock()
			if existing, ok := a.analyses[item.ID]; ok {
				existing.Stage = "local"
				existing.Provider = "ollama (quick)"
			}
			a.mu.Unlock()

			resp, err := localProvider.Generate(ctx, Request{
				SystemPrompt: systemPrompt,
				UserPrompt:   userPrompt,
				MaxTokens:    500, // Shorter for speed
			})

			// Only update if cloud hasn't finished yet
			select {
			case <-cloudDone:
				logging.Debug("Local analysis finished but cloud already done, skipping")
				return
			default:
			}

			if err != nil {
				logging.Debug("Local analysis failed, waiting for cloud", "error", err)
				return
			}

			a.mu.Lock()
			// Only update if still loading (cloud hasn't replaced it)
			if existing, ok := a.analyses[item.ID]; ok && existing.Loading {
				existing.Content = strings.TrimSpace(resp.Content)
				existing.Loading = false
				existing.Provider = "ollama (quick)"
				existing.Stage = "interim"
				logging.Info("Local analysis ready (interim)", "item", item.Title)
			}
			callbacks := a.callbacks
			a.mu.Unlock()

			// Notify callbacks of interim result
			for _, cb := range callbacks {
				cb(item.ID, Analysis{
					Content:  strings.TrimSpace(resp.Content),
					Provider: "ollama (quick)",
					Stage:    "interim",
				})
			}
		}()

		// Start cloud analysis (slower but better)
		go func() {
			defer close(cloudDone)

			a.mu.Lock()
			if existing, ok := a.analyses[item.ID]; ok && existing.Stage == "starting" {
				existing.Stage = "analyzing"
			}
			a.mu.Unlock()

			resp, err := cloudProvider.Generate(ctx, Request{
				SystemPrompt: systemPrompt,
				UserPrompt:   userPrompt,
				MaxTokens:    800,
			})

			a.runAnalysisComplete(item, cloudProvider.Name(), resp, err, userPrompt)
		}()
	} else {
		// Only one provider available, use it directly
		provider := localProvider
		if provider == nil {
			provider = cloudProvider
		}

		go func() {
			a.mu.Lock()
			if existing, ok := a.analyses[item.ID]; ok {
				existing.Stage = "analyzing"
			}
			a.mu.Unlock()

			resp, err := provider.Generate(ctx, Request{
				SystemPrompt: systemPrompt,
				UserPrompt:   userPrompt,
				MaxTokens:    800,
			})

			a.runAnalysisComplete(item, provider.Name(), resp, err, userPrompt)
		}()
	}
}

// runAnalysisComplete handles the completion of an analysis
func (a *Analyzer) runAnalysisComplete(item feeds.Item, providerName string, resp Response, err error, userPrompt string) {
	var analysis Analysis
	var errMsg string
	if err != nil {
		logging.Error("AI analysis failed", "error", err, "provider", providerName)
		analysis = Analysis{Error: err, Loading: false, Provider: providerName, Stage: "error"}
		errMsg = err.Error()
	} else {
		logging.Info("AI analysis raw response",
			"item_id", item.ID,
			"item_title", item.Title,
			"provider", providerName,
			"model", resp.Model,
			"content_len", len(resp.Content),
			"raw_response", resp.RawResponse)
		analysis = Analysis{Content: strings.TrimSpace(resp.Content), Loading: false, Provider: providerName, Stage: "complete"}
	}

	a.mu.Lock()
	a.analyses[item.ID] = &analysis
	store := a.store
	callbacks := a.callbacks
	a.mu.Unlock()

	// Persist to database
	if store != nil {
		store.SaveAnalysis(
			item.ID,
			providerName,
			resp.Model,
			userPrompt,
			resp.RawResponse,
			strings.TrimSpace(resp.Content),
			errMsg,
		)
	}

	// Notify callbacks
	for _, cb := range callbacks {
		cb(item.ID, analysis)
	}
}

// GetAnalysis returns the current analysis for an item
func (a *Analyzer) GetAnalysis(itemID string) *Analysis {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if analysis, ok := a.analyses[itemID]; ok {
		// Return a copy
		return &Analysis{
			Content:  analysis.Content,
			Error:    analysis.Error,
			Loading:  analysis.Loading,
			Provider: analysis.Provider,
			Stage:    analysis.Stage,
		}
	}
	return nil
}

// HasAnalysis returns true if we have analysis for an item
func (a *Analyzer) HasAnalysis(itemID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.analyses[itemID]
	return ok
}

// ClearAnalysis removes analysis for an item
func (a *Analyzer) ClearAnalysis(itemID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.analyses, itemID)
}

// TopStoryResult represents an AI-identified important story
type TopStoryResult struct {
	ItemID    string
	Label     string    // "BREAKING", "DEVELOPING", "TOP STORY"
	Reason    string
	Zinger    string    // One-line punchy summary from local LLM
	HitCount  int       // How many times this story was identified (1 = first time)
	FirstSeen time.Time // When first identified as top story
	Streak    bool      // True if identified in consecutive analyses
	Status    TopStoryStatus // Lifecycle status
	MissCount int       // How many consecutive analyses missed this story
}

// TopStoryStatus indicates where a story is in its lifecycle
type TopStoryStatus string

const (
	StatusBreaking   TopStoryStatus = "breaking"   // New, first 1-2 identifications
	StatusDeveloping TopStoryStatus = "developing" // Hit count 2-3, gaining traction
	StatusPersistent TopStoryStatus = "persistent" // Hit count 4+, major ongoing story
	StatusFading     TopStoryStatus = "fading"     // Not identified recently, cooling off
)

// CachedTopStory tracks a story's importance over time
type CachedTopStory struct {
	ItemID         string
	Title          string    // For display when original item may be gone
	Label          string    // Latest label assigned
	Reason         string    // Latest reason
	Zinger         string    // One-line punchy summary from local LLM
	FirstSeen      time.Time // When first identified as top story
	LastSeen       time.Time // Most recent identification
	HitCount       int       // How many times identified
	MissCount      int       // Consecutive analyses that missed this story
	ConsecutiveHit bool      // Was this story in the previous analysis?
}

// TopStoriesCache tracks stories identified as "top" over time
type TopStoriesCache struct {
	mu             sync.RWMutex
	entries        map[string]*CachedTopStory // itemID -> cached entry
	lastAnalysisAt time.Time                  // When the last analysis ran
	lastTopIDs     []string                   // Item IDs from last analysis (for streak detection)
}

// AnalyzeTopStories uses AI to identify the most important/breaking stories
// Prefers local model for speed since this is a quick classification task (if configured)
func (a *Analyzer) AnalyzeTopStories(ctx context.Context, items []feeds.Item) ([]TopStoryResult, error) {
	a.mu.RLock()
	localForQuickOps := a.localForQuickOps
	// Prefer local model for speed if configured, fall back to cloud
	var provider Provider
	if localForQuickOps {
		provider = a.getLocalProvider()
	}
	if provider == nil {
		provider = a.getRandomProvider()
	}
	a.mu.RUnlock()

	if provider == nil {
		logging.Debug("Top stories analysis skipped - no provider available")
		return nil, nil
	}

	if len(items) == 0 {
		return nil, nil
	}

	logging.Info("Analyzing top stories", "items", len(items), "provider", provider.Name())

	// Build a list of headlines for analysis
	var headlines strings.Builder
	headlines.WriteString("Here are recent news headlines. Identify ALL important, breaking, or developing stories (typically 3-6).\n\n")

	// Limit to 50 most recent items to avoid token limits
	maxItems := 50
	if len(items) < maxItems {
		maxItems = len(items)
	}

	for i := 0; i < maxItems; i++ {
		item := items[i]
		headlines.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, item.SourceName, item.Title))
	}

	systemPrompt := `You are a news editor. Identify the most important stories from the list.

OUTPUT FORMAT - Use EXACTLY this format, one story per line:
BREAKING|<number>|<short reason>
DEVELOPING|<number>|<short reason>
TOP|<number>|<short reason>

EXAMPLE OUTPUT (your entire response should look like this):
BREAKING|5|Earthquake with casualties
DEVELOPING|12|Vote counting continues
TOP|3|Historic climate deal

STRICT RULES:
- Output 3-6 lines maximum, nothing else
- <number> = the headline number (1, 2, 3, etc.)
- <short reason> = 3-8 words explaining why (NOT the headline text)
- BREAKING = urgent news (deaths, disasters, emergencies)
- DEVELOPING = ongoing situation with updates
- TOP = important but not urgent
- NO markdown, NO bullets, NO extra text
- NEVER repeat headline text in the reason`

	resp, err := provider.Generate(ctx, Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   headlines.String(),
		MaxTokens:    300,
	})

	if err != nil {
		logging.Error("Top stories analysis failed", "error", err)
		return nil, err
	}

	logging.Info("Top stories LLM response", "content", resp.Content, "content_len", len(resp.Content))

	// Parse the response - try pipe format first, then fallback to markdown
	results := parseTopStoriesPipeFormat(resp.Content, items, maxItems)
	if len(results) == 0 {
		logging.Debug("Pipe format parsing failed, trying markdown fallback")
		results = parseTopStoriesMarkdown(resp.Content, items, maxItems)
	}

	// Build a map of item titles for caching
	itemTitles := make(map[string]string)
	for _, item := range items {
		itemTitles[item.ID] = item.Title
	}

	// Update cache and enrich results with historical data
	results = a.updateTopStoriesCache(results, itemTitles)

	// Generate zingers for stories that don't have one yet (async, local LLM only)
	go a.generateZingersAsync(ctx, results, items)

	logging.Info("Top stories identified", "count", len(results))
	return results, nil
}

// generateZingersAsync generates punchy one-liners for top stories using local LLM
func (a *Analyzer) generateZingersAsync(ctx context.Context, results []TopStoryResult, items []feeds.Item) {
	a.mu.RLock()
	localProvider := a.getLocalProvider()
	a.mu.RUnlock()

	if localProvider == nil {
		logging.Debug("Skipping zinger generation - no local provider")
		return
	}

	// Build item lookup
	itemMap := make(map[string]*feeds.Item)
	for i := range items {
		itemMap[items[i].ID] = &items[i]
	}

	// Generate zingers for stories that need them
	for _, result := range results {
		// Check if we already have a zinger cached
		a.topStoriesCache.mu.RLock()
		cached, exists := a.topStoriesCache.entries[result.ItemID]
		hasZinger := exists && cached.Zinger != ""
		a.topStoriesCache.mu.RUnlock()

		if hasZinger {
			continue // Already have a zinger
		}

		item, ok := itemMap[result.ItemID]
		if !ok {
			continue
		}

		// Generate zinger
		zinger := a.generateSingleZinger(ctx, localProvider, item, result.Label)
		if zinger == "" {
			continue
		}

		// Store in cache
		a.topStoriesCache.mu.Lock()
		if cached, ok := a.topStoriesCache.entries[result.ItemID]; ok {
			cached.Zinger = zinger
			logging.Debug("Generated zinger", "item", item.Title[:min(len(item.Title), 40)], "zinger", zinger)
		}
		a.topStoriesCache.mu.Unlock()
	}
}

// generateSingleZinger creates a punchy one-liner for a story
func (a *Analyzer) generateSingleZinger(ctx context.Context, provider Provider, item *feeds.Item, label string) string {
	prompt := fmt.Sprintf(`Create ONE punchy, informative sentence (max 15 words) that captures why this story matters.

Headline: %s
Source: %s
Category: %s

Rules:
- One sentence only, no quotes
- Be specific, not generic
- Include the key insight or implication
- No clickbait, no questions
- Start with the most important word

Example good zingers:
- "Fed rate cut signals recession fears despite strong jobs data"
- "Tesla's robotaxi delay threatens $500B valuation premise"
- "Ukraine's drone strike hits Russian oil refinery 500 miles from front"

Your zinger:`, item.Title, item.SourceName, label)

	resp, err := provider.Generate(ctx, Request{
		SystemPrompt: "You create brief, informative news summaries. Output only the zinger, nothing else.",
		UserPrompt:   prompt,
		MaxTokens:    50,
	})

	if err != nil {
		logging.Debug("Zinger generation failed", "error", err)
		return ""
	}

	// Clean up response
	zinger := strings.TrimSpace(resp.Content)
	zinger = strings.Trim(zinger, `"'`)
	zinger = strings.TrimPrefix(zinger, "- ")

	// Reject if too long or looks wrong
	if len(zinger) > 120 || len(zinger) < 10 {
		return ""
	}
	if strings.Contains(zinger, "\n") {
		zinger = strings.Split(zinger, "\n")[0]
	}

	return zinger
}

// updateTopStoriesCache updates the cache with new results and enriches them with history
func (a *Analyzer) updateTopStoriesCache(results []TopStoryResult, itemTitles map[string]string) []TopStoryResult {
	a.topStoriesCache.mu.Lock()
	defer a.topStoriesCache.mu.Unlock()

	now := time.Now()
	currentIDs := make(map[string]bool)

	// Build set of IDs that were in last analysis for streak detection
	lastIDSet := make(map[string]bool)
	for _, id := range a.topStoriesCache.lastTopIDs {
		lastIDSet[id] = true
	}

	// Update cache and enrich results
	for i := range results {
		result := &results[i]
		currentIDs[result.ItemID] = true

		if cached, ok := a.topStoriesCache.entries[result.ItemID]; ok {
			// Story was previously identified - update cache
			cached.HitCount++
			cached.MissCount = 0 // Reset miss count
			cached.LastSeen = now
			cached.Label = result.Label
			cached.Reason = result.Reason
			cached.ConsecutiveHit = lastIDSet[result.ItemID]

			// Enrich result with cache data (including existing zinger)
			result.HitCount = cached.HitCount
			result.FirstSeen = cached.FirstSeen
			result.Streak = cached.ConsecutiveHit
			result.MissCount = 0
			result.Status = calculateStatus(cached.HitCount, 0)
			result.Zinger = cached.Zinger // Preserve existing zinger

			logging.Debug("Top story cache hit",
				"item", result.ItemID,
				"hit_count", cached.HitCount,
				"first_seen", cached.FirstSeen,
				"streak", cached.ConsecutiveHit,
				"status", result.Status,
				"has_zinger", cached.Zinger != "")
		} else {
			// New top story - add to cache
			title := itemTitles[result.ItemID]
			a.topStoriesCache.entries[result.ItemID] = &CachedTopStory{
				ItemID:         result.ItemID,
				Title:          title,
				Label:          result.Label,
				Reason:         result.Reason,
				FirstSeen:      now,
				LastSeen:       now,
				HitCount:       1,
				MissCount:      0,
				ConsecutiveHit: false,
			}
			result.HitCount = 1
			result.FirstSeen = now
			result.Streak = false
			result.MissCount = 0
			result.Status = StatusBreaking

			logging.Debug("Top story cache miss (new)",
				"item", result.ItemID,
				"title", truncateStr(title, 40),
				"status", result.Status)
		}
	}

	// Update miss count for stories NOT in current results
	for id, cached := range a.topStoriesCache.entries {
		if !currentIDs[id] {
			cached.MissCount++
			cached.ConsecutiveHit = false
			logging.Debug("Top story cache miss increment",
				"item", id,
				"miss_count", cached.MissCount,
				"hit_count", cached.HitCount)
		}
	}

	// Update tracking for next analysis
	a.topStoriesCache.lastAnalysisAt = now
	newLastTopIDs := make([]string, 0, len(currentIDs))
	for id := range currentIDs {
		newLastTopIDs = append(newLastTopIDs, id)
	}
	a.topStoriesCache.lastTopIDs = newLastTopIDs

	return results
}

// calculateStatus determines the lifecycle status of a story
func calculateStatus(hitCount, missCount int) TopStoryStatus {
	if missCount >= 2 {
		return StatusFading
	}
	if hitCount >= 4 {
		return StatusPersistent
	}
	if hitCount >= 2 {
		return StatusDeveloping
	}
	return StatusBreaking
}

// truncateStr safely truncates a string
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ClearTopStoriesCache clears all cached top stories
func (a *Analyzer) ClearTopStoriesCache() {
	a.topStoriesCache.mu.Lock()
	defer a.topStoriesCache.mu.Unlock()
	a.topStoriesCache.entries = make(map[string]*CachedTopStory)
	a.topStoriesCache.lastTopIDs = nil
	logging.Info("Top stories cache cleared")
}

// TopStoryCacheEntry is the format for persistence (matches store package)
type TopStoryCacheEntry struct {
	ItemID    string
	Title     string
	Label     string
	Reason    string
	Zinger    string
	FirstSeen time.Time
	LastSeen  time.Time
	HitCount  int
	MissCount int
}

// ExportTopStoriesCache exports cache entries for persistence
func (a *Analyzer) ExportTopStoriesCache() []TopStoryCacheEntry {
	a.topStoriesCache.mu.RLock()
	defer a.topStoriesCache.mu.RUnlock()

	entries := make([]TopStoryCacheEntry, 0, len(a.topStoriesCache.entries))
	for _, cached := range a.topStoriesCache.entries {
		entries = append(entries, TopStoryCacheEntry{
			ItemID:    cached.ItemID,
			Title:     cached.Title,
			Label:     cached.Label,
			Reason:    cached.Reason,
			Zinger:    cached.Zinger,
			FirstSeen: cached.FirstSeen,
			LastSeen:  cached.LastSeen,
			HitCount:  cached.HitCount,
			MissCount: cached.MissCount,
		})
	}
	return entries
}

// ImportTopStoriesCache imports cache entries from persistence
func (a *Analyzer) ImportTopStoriesCache(entries []TopStoryCacheEntry) {
	a.topStoriesCache.mu.Lock()
	defer a.topStoriesCache.mu.Unlock()

	for _, e := range entries {
		a.topStoriesCache.entries[e.ItemID] = &CachedTopStory{
			ItemID:    e.ItemID,
			Title:     e.Title,
			Label:     e.Label,
			Reason:    e.Reason,
			Zinger:    e.Zinger,
			FirstSeen: e.FirstSeen,
			LastSeen:  e.LastSeen,
			HitCount:  e.HitCount,
			MissCount: e.MissCount,
		}
	}
	logging.Info("Top stories cache imported", "count", len(entries))
}

// GetBreathingTopStories returns the dynamic list of top stories
// This merges current LLM results with persistent high-confidence stories from cache
// The list grows and contracts based on what's actually happening
func (a *Analyzer) GetBreathingTopStories(currentResults []TopStoryResult, maxStories int) []TopStoryResult {
	a.topStoriesCache.mu.RLock()
	defer a.topStoriesCache.mu.RUnlock()

	if maxStories <= 0 {
		maxStories = 8 // Sensible default max
	}

	// Build maps for deduplication - by ID and by title prefix (to catch same story from different sources)
	currentMap := make(map[string]*TopStoryResult)
	seenTitles := make(map[string]bool)

	for i := range currentResults {
		currentMap[currentResults[i].ItemID] = &currentResults[i]
		// Track title prefix for deduplication
		if cached, ok := a.topStoriesCache.entries[currentResults[i].ItemID]; ok && cached.Title != "" {
			titleKey := strings.ToLower(cached.Title)
			if len(titleKey) > 40 {
				titleKey = titleKey[:40]
			}
			seenTitles[titleKey] = true
		}
	}

	// Collect all stories to consider
	var allStories []TopStoryResult

	// Add all current results first
	allStories = append(allStories, currentResults...)

	// Add high-confidence cached stories not in current results
	// These are stories that were consistently identified but might have been
	// missed in this particular analysis
	for id, cached := range a.topStoriesCache.entries {
		if _, inCurrent := currentMap[id]; inCurrent {
			continue // Already included by ID
		}

		// Check for title-based duplicate
		if cached.Title != "" {
			titleKey := strings.ToLower(cached.Title)
			if len(titleKey) > 40 {
				titleKey = titleKey[:40]
			}
			if seenTitles[titleKey] {
				logging.Debug("Skipping duplicate title from cache", "title", cached.Title[:min(len(cached.Title), 40)])
				continue
			}
		}

		// Include if: high hit count AND not too many misses
		// This keeps persistent stories visible even if LLM misses them once
		if cached.HitCount >= 3 && cached.MissCount <= 2 {
			status := calculateStatus(cached.HitCount, cached.MissCount)
			allStories = append(allStories, TopStoryResult{
				ItemID:    cached.ItemID,
				Label:     cached.Label,
				Reason:    cached.Reason,
				Zinger:    cached.Zinger,
				HitCount:  cached.HitCount,
				FirstSeen: cached.FirstSeen,
				Streak:    false,
				Status:    status,
				MissCount: cached.MissCount,
			})

			// Mark this title as seen
			if cached.Title != "" {
				titleKey := strings.ToLower(cached.Title)
				if len(titleKey) > 40 {
					titleKey = titleKey[:40]
				}
				seenTitles[titleKey] = true
			}

			logging.Debug("Including persistent story from cache",
				"item", id,
				"hit_count", cached.HitCount,
				"miss_count", cached.MissCount,
				"status", status)
		}
	}

	// Sort by importance: Status priority, then hit count, then recency
	sortTopStories(allStories)

	// Cap at max
	if len(allStories) > maxStories {
		allStories = allStories[:maxStories]
	}

	// Update labels based on status for display
	for i := range allStories {
		allStories[i].Label = labelForStatus(allStories[i].Status, allStories[i].HitCount)
	}

	return allStories
}

// sortTopStories sorts stories by importance
func sortTopStories(stories []TopStoryResult) {
	// Simple bubble sort for small lists
	for i := 0; i < len(stories)-1; i++ {
		for j := i + 1; j < len(stories); j++ {
			if storyLess(stories[j], stories[i]) {
				stories[i], stories[j] = stories[j], stories[i]
			}
		}
	}
}

// storyLess returns true if a should come before b
func storyLess(a, b TopStoryResult) bool {
	// Priority order: Breaking > Persistent > Developing > Fading
	statusPriority := map[TopStoryStatus]int{
		StatusBreaking:   0,
		StatusPersistent: 1,
		StatusDeveloping: 2,
		StatusFading:     3,
	}

	aPri := statusPriority[a.Status]
	bPri := statusPriority[b.Status]

	if aPri != bPri {
		return aPri < bPri
	}

	// Within same status, higher hit count wins
	if a.HitCount != b.HitCount {
		return a.HitCount > b.HitCount
	}

	// Tie-breaker: more recent first
	return a.FirstSeen.After(b.FirstSeen)
}

// labelForStatus returns the display label based on status
func labelForStatus(status TopStoryStatus, hitCount int) string {
	switch status {
	case StatusBreaking:
		return "🔴 BREAKING"
	case StatusDeveloping:
		return "🟡 DEVELOPING"
	case StatusPersistent:
		if hitCount >= 6 {
			return "🔥 MAJOR"
		}
		return "🟠 ONGOING"
	case StatusFading:
		return "⚪ FADING"
	default:
		return "📌 TOP STORY"
	}
}

// GetTopStoriesContext returns a formatted string of current top stories for use in analysis prompts
// Returns empty string if no top stories are available
func (a *Analyzer) GetTopStoriesContext() string {
	a.topStoriesCache.mu.RLock()
	defer a.topStoriesCache.mu.RUnlock()

	if len(a.topStoriesCache.entries) == 0 {
		return ""
	}

	// Get active top stories (not fading, recent)
	var activeStories []*CachedTopStory
	for _, entry := range a.topStoriesCache.entries {
		status := calculateStatus(entry.HitCount, entry.MissCount)
		if status != StatusFading && entry.Title != "" {
			activeStories = append(activeStories, entry)
		}
	}

	if len(activeStories) == 0 {
		return ""
	}

	// Sort by importance (hit count desc)
	for i := 0; i < len(activeStories)-1; i++ {
		for j := i + 1; j < len(activeStories); j++ {
			if activeStories[j].HitCount > activeStories[i].HitCount {
				activeStories[i], activeStories[j] = activeStories[j], activeStories[i]
			}
		}
	}

	// Limit to top 5
	if len(activeStories) > 5 {
		activeStories = activeStories[:5]
	}

	// Format as context string
	var sb strings.Builder
	sb.WriteString("CURRENT TOP STORIES:\n")
	for i, story := range activeStories {
		status := calculateStatus(story.HitCount, story.MissCount)
		label := labelForStatus(status, story.HitCount)
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, label, story.Title))
	}
	return sb.String()
}

// GetTopStoriesCache returns cached top stories sorted by hit count
func (a *Analyzer) GetTopStoriesCache() []*CachedTopStory {
	a.topStoriesCache.mu.RLock()
	defer a.topStoriesCache.mu.RUnlock()

	// Convert map to slice
	result := make([]*CachedTopStory, 0, len(a.topStoriesCache.entries))
	for _, entry := range a.topStoriesCache.entries {
		// Make a copy to avoid race conditions
		copy := *entry
		result = append(result, &copy)
	}

	// Sort by hit count (descending), then by last seen (descending)
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			swap := false
			if result[j].HitCount > result[i].HitCount {
				swap = true
			} else if result[j].HitCount == result[i].HitCount &&
				result[j].LastSeen.After(result[i].LastSeen) {
				swap = true
			}
			if swap {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// PruneTopStoriesCache removes entries older than the given duration
func (a *Analyzer) PruneTopStoriesCache(maxAge time.Duration) int {
	a.topStoriesCache.mu.Lock()
	defer a.topStoriesCache.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	pruned := 0

	for id, entry := range a.topStoriesCache.entries {
		if entry.LastSeen.Before(cutoff) {
			delete(a.topStoriesCache.entries, id)
			pruned++
		}
	}

	if pruned > 0 {
		logging.Info("Pruned top stories cache", "removed", pruned, "remaining", len(a.topStoriesCache.entries))
	}

	return pruned
}

// GetTopStoriesCacheSize returns the number of entries in the cache
func (a *Analyzer) GetTopStoriesCacheSize() int {
	a.topStoriesCache.mu.RLock()
	defer a.topStoriesCache.mu.RUnlock()
	return len(a.topStoriesCache.entries)
}

// parseTopStoriesPipeFormat parses LABEL|number|reason format
func parseTopStoriesPipeFormat(content string, items []feeds.Item, maxItems int) []TopStoryResult {
	var results []TopStoryResult
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}

		label := strings.TrimSpace(parts[0])
		var itemNum int
		fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &itemNum)
		if itemNum < 1 || itemNum > maxItems {
			continue
		}

		reason := strings.TrimSpace(parts[2])
		fullLabel := mapLabel(label)
		if fullLabel == "" {
			continue
		}

		// Validate and clean the reason
		cleanedReason := cleanReason(reason, items, maxItems)

		results = append(results, TopStoryResult{
			ItemID: items[itemNum-1].ID,
			Label:  fullLabel,
			Reason: cleanedReason,
		})
		logging.Debug("Parsed top story (pipe)", "itemNum", itemNum, "label", fullLabel, "reason", cleanedReason)
	}
	return results
}

// cleanReason validates and cleans a reason string
// Removes reasons that look like headlines, source attributions, or are too long
func cleanReason(reason string, items []feeds.Item, maxItems int) string {
	if reason == "" {
		return ""
	}

	// Reject if it looks like a headline (contains source name)
	reasonLower := strings.ToLower(reason)
	for i, item := range items {
		if i >= maxItems {
			break
		}
		sourceLower := strings.ToLower(item.SourceName)
		if strings.Contains(reasonLower, sourceLower) {
			logging.Debug("Rejecting reason containing source name", "reason", reason, "source", item.SourceName)
			return ""
		}
		// Also reject if it contains significant portion of a headline
		titleLower := strings.ToLower(item.Title)
		if len(titleLower) > 20 {
			titlePrefix := titleLower[:20]
			if strings.Contains(reasonLower, titlePrefix) {
				logging.Debug("Rejecting reason containing headline text", "reason", reason)
				return ""
			}
		}
	}

	// Reject if it contains markdown formatting
	if strings.Contains(reason, "**") || strings.Contains(reason, "*") {
		logging.Debug("Rejecting reason with markdown", "reason", reason)
		return ""
	}

	// Reject if too long (likely a headline or full sentence)
	if len(reason) > 80 {
		logging.Debug("Rejecting overly long reason", "reason", reason)
		return ""
	}

	// Reject if it starts with certain patterns that indicate it's not a reason
	badPrefixes := []string{"according to", "reports say", "the ", "a ", "an "}
	for _, prefix := range badPrefixes {
		if strings.HasPrefix(reasonLower, prefix) && len(reason) > 40 {
			logging.Debug("Rejecting reason with bad prefix", "reason", reason, "prefix", prefix)
			return ""
		}
	}

	return reason
}

// parseTopStoriesMarkdown parses various markdown formats from LLM responses
// Handles: "1. **[Source] Title**", "1. **Title - Source**", "1. Title (Source)", etc.
func parseTopStoriesMarkdown(content string, items []feeds.Item, maxItems int) []TopStoryResult {
	var results []TopStoryResult
	lines := strings.Split(content, "\n")

	// Build a list of source names for matching
	sourceNames := make(map[string]bool)
	for i, item := range items {
		if i >= maxItems {
			break
		}
		sourceNames[strings.ToLower(item.SourceName)] = true
	}

	foundCount := 0
	for _, line := range lines {
		if foundCount >= 3 {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "1.") && !strings.HasPrefix(line, "2.") && !strings.HasPrefix(line, "3.") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
			// Skip lines that don't look like list items
			if !strings.Contains(line, "**") {
				continue
			}
		}

		// Clean up the line - remove markdown
		cleanLine := strings.ReplaceAll(line, "**", "")
		cleanLine = strings.ReplaceAll(cleanLine, "*", "")
		cleanLine = strings.TrimLeft(cleanLine, "0123456789.-) ")

		// Try to match against our items
		for i, item := range items {
			if i >= maxItems {
				break
			}

			// Check if this line mentions this item's title (fuzzy match)
			itemTitleLower := strings.ToLower(item.Title)
			lineLower := strings.ToLower(cleanLine)

			// Get first 40 chars of title for matching
			titlePrefix := itemTitleLower
			if len(titlePrefix) > 40 {
				titlePrefix = titlePrefix[:40]
			}

			// Check for title match
			titleMatch := strings.Contains(lineLower, titlePrefix) ||
				strings.Contains(itemTitleLower, lineLower[:min(len(lineLower), 40)])

			// Also check if source is mentioned
			sourceMatch := strings.Contains(lineLower, strings.ToLower(item.SourceName))

			if titleMatch || (sourceMatch && len(cleanLine) > 20) {
				// Avoid duplicates
				isDupe := false
				for _, r := range results {
					if r.ItemID == item.ID {
						isDupe = true
						break
					}
				}
				if isDupe {
					continue
				}

				// Determine label based on position
				label := "📌 TOP STORY"
				if foundCount == 0 {
					label = "🔴 BREAKING"
				} else if foundCount == 1 {
					label = "🟡 DEVELOPING"
				}

				// Extract reason (text after colon or dash)
				reason := ""
				for _, sep := range []string{": ", " - ", "– "} {
					if idx := strings.Index(line, sep); idx > 0 && idx < len(line)-3 {
						reason = strings.TrimSpace(line[idx+len(sep):])
						break
					}
				}

				// Clean and validate the reason
				reason = cleanReason(reason, items, maxItems)

				results = append(results, TopStoryResult{
					ItemID: item.ID,
					Label:  label,
					Reason: reason,
				})
				logging.Debug("Parsed top story (markdown)", "item", item.Title[:min(len(item.Title), 40)], "label", label, "reason", reason)
				foundCount++
				break
			}
		}
	}
	return results
}

// mapLabel converts label string to emoji label
func mapLabel(label string) string {
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "BREAKING":
		return "🔴 BREAKING"
	case "DEVELOPING":
		return "🟡 DEVELOPING"
	case "TOP", "TOP STORY":
		return "📌 TOP STORY"
	default:
		return ""
	}
}

// Legacy alias for compatibility
type BrainTrust = Analyzer

func NewBrainTrust(provider Provider) *BrainTrust {
	return NewAnalyzer(provider)
}

// Legacy method aliases
func (a *Analyzer) GetAnalyses(itemID string) []Analysis {
	if analysis := a.GetAnalysis(itemID); analysis != nil {
		return []Analysis{*analysis}
	}
	return nil
}

func (a *Analyzer) SetPersonas(personaIDs []string) {
	// No-op for compatibility
}
