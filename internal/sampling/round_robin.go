package sampling

import (
	"sort"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
)

// RoundRobinSampler takes items from each source in rotation
// This ensures every source gets representation regardless of chattiness
type RoundRobinSampler struct {
	// MaxPerSource limits how many items to take from each source
	// 0 means no limit
	MaxPerSource int
}

// NewRoundRobinSampler creates a new round-robin sampler
func NewRoundRobinSampler() *RoundRobinSampler {
	return &RoundRobinSampler{
		MaxPerSource: 0, // no limit by default
	}
}

// Sample takes items from each queue in rotation
// Returns up to n items, interleaved across sources
func (s *RoundRobinSampler) Sample(queues []*SourceQueue, n int) []feeds.Item {
	if len(queues) == 0 || n <= 0 {
		return nil
	}

	result := make([]feeds.Item, 0, n)

	// Track position in each queue
	idx := make([]int, len(queues))

	// Keep going until we have enough items or exhaust all queues
	for len(result) < n {
		added := false

		for i, q := range queues {
			// Check if we've hit per-source limit
			if s.MaxPerSource > 0 && idx[i] >= s.MaxPerSource {
				continue
			}

			// Try to get next item from this queue
			item := q.Peek(idx[i])
			if item != nil {
				result = append(result, *item)
				idx[i]++
				added = true

				if len(result) >= n {
					break
				}
			}
		}

		// If we couldn't add any items, all queues are exhausted
		if !added {
			break
		}
	}

	return result
}

// WeightedRoundRobinSampler takes more items from higher-weighted sources
type WeightedRoundRobinSampler struct {
	// MinWeight is the minimum weight to consider (sources below this are skipped)
	MinWeight float64
}

// NewWeightedRoundRobinSampler creates a new weighted round-robin sampler
func NewWeightedRoundRobinSampler() *WeightedRoundRobinSampler {
	return &WeightedRoundRobinSampler{
		MinWeight: 0.1,
	}
}

// Sample takes items proportional to source weight
// A source with weight 2.0 gets ~2x items as weight 1.0
func (s *WeightedRoundRobinSampler) Sample(queues []*SourceQueue, n int) []feeds.Item {
	if len(queues) == 0 || n <= 0 {
		return nil
	}

	result := make([]feeds.Item, 0, n)

	// Track position and "credit" for each queue
	// Credit accumulates based on weight; when >= 1.0, take an item
	idx := make([]int, len(queues))
	credit := make([]float64, len(queues))

	// Normalize weights so average is 1.0
	var totalWeight float64
	for _, q := range queues {
		if q.Weight >= s.MinWeight && q.Len() > 0 {
			totalWeight += q.Weight
		}
	}
	if totalWeight == 0 {
		return nil
	}
	avgWeight := totalWeight / float64(len(queues))

	// Keep going until we have enough items or exhaust all queues
	for len(result) < n {
		added := false

		for i, q := range queues {
			if q.Weight < s.MinWeight || idx[i] >= q.Len() {
				continue
			}

			// Add credit based on normalized weight
			credit[i] += q.Weight / avgWeight

			// Take items while we have credit
			for credit[i] >= 1.0 && idx[i] < q.Len() && len(result) < n {
				item := q.Peek(idx[i])
				if item != nil {
					result = append(result, *item)
					idx[i]++
					credit[i] -= 1.0
					added = true
				}
			}
		}

		// If we couldn't add any items, all queues are exhausted
		if !added {
			break
		}
	}

	return result
}

// RecencyMergeSampler ignores source boundaries, takes N most recent globally
type RecencyMergeSampler struct{}

// NewRecencyMergeSampler creates a sampler that merges by recency
func NewRecencyMergeSampler() *RecencyMergeSampler {
	return &RecencyMergeSampler{}
}

// Sample merges all queues and returns the n most recent items
func (s *RecencyMergeSampler) Sample(queues []*SourceQueue, n int) []feeds.Item {
	if len(queues) == 0 || n <= 0 {
		return nil
	}

	// Collect all items
	var all []feeds.Item
	for _, q := range queues {
		all = append(all, q.All()...)
	}

	// Sort by published time (newest first) - O(n log n)
	sort.Slice(all, func(i, j int) bool {
		return all[i].Published.After(all[j].Published)
	})

	// Return top n
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// --- Advanced Samplers (from Brain Trust recommendations) ---

// DeficitRoundRobinSampler provides strict long-run fairness even with bursty sources.
// Each source accumulates "credit" (deficit) and emits items when credit >= cost.
// Better than plain round-robin when some sources are empty then explode.
// Recommended by GPT-5 as the best default "balanced" sampler.
type DeficitRoundRobinSampler struct {
	// Quantum is the credit added per round (can be weighted per-source)
	// Higher quantum = source can emit more items per round
	Quantum float64

	// MaxPerSource caps items per source per sample (0 = no limit)
	MaxPerSource int

	// deficits tracks accumulated credit per source (by name)
	deficits map[string]float64
}

// NewDeficitRoundRobinSampler creates a DRR sampler
func NewDeficitRoundRobinSampler() *DeficitRoundRobinSampler {
	return &DeficitRoundRobinSampler{
		Quantum:      1.0,
		MaxPerSource: 0,
		deficits:     make(map[string]float64),
	}
}

// Sample selects items using deficit round-robin for strict fairness
func (s *DeficitRoundRobinSampler) Sample(queues []*SourceQueue, n int) []feeds.Item {
	if len(queues) == 0 || n <= 0 {
		return nil
	}

	result := make([]feeds.Item, 0, n)
	idx := make(map[string]int) // position in each queue
	taken := make(map[string]int) // items taken per source this sample

	// Initialize deficits for new sources
	for _, q := range queues {
		if _, exists := s.deficits[q.Name]; !exists {
			s.deficits[q.Name] = 0
		}
	}

	// Keep sampling until we have enough items or exhaust all queues
	for len(result) < n {
		added := false

		for _, q := range queues {
			// Check per-source cap
			if s.MaxPerSource > 0 && taken[q.Name] >= s.MaxPerSource {
				continue
			}

			// Check if queue has more items
			pos := idx[q.Name]
			if pos >= q.Len() {
				continue
			}

			// Add credit based on source weight
			quantum := s.Quantum * q.Weight
			s.deficits[q.Name] += quantum

			// Emit items while we have credit
			for s.deficits[q.Name] >= 1.0 && pos < q.Len() && len(result) < n {
				item := q.Peek(pos)
				if item != nil {
					result = append(result, *item)
					pos++
					idx[q.Name] = pos
					taken[q.Name]++
					s.deficits[q.Name] -= 1.0
					added = true

					// Check per-source cap
					if s.MaxPerSource > 0 && taken[q.Name] >= s.MaxPerSource {
						break
					}
				}
			}

			if len(result) >= n {
				break
			}
		}

		// No progress = all queues exhausted or capped
		if !added {
			break
		}
	}

	return result
}

// FairRecentSampler combines fairness across sources with recency preference.
// Takes up to quota items per source from the recent window, then sorts by recency.
// Recommended by Grok-4 as the best default for "balanced + fresh" view.
type FairRecentSampler struct {
	// QuotaPerSource limits items per source (default 20)
	QuotaPerSource int

	// MaxAge filters out items older than this (default 24 hours)
	MaxAge time.Duration

	// PerSourceCooldown prevents same source appearing consecutively
	// 0 = no cooldown, >0 = minimum items between same source
	PerSourceCooldown int
}

// NewFairRecentSampler creates a fair+recent sampler
func NewFairRecentSampler() *FairRecentSampler {
	return &FairRecentSampler{
		QuotaPerSource:    20,
		MaxAge:            24 * time.Hour,
		PerSourceCooldown: 0,
	}
}

// Sample selects items with fairness quotas then sorts by recency
func (s *FairRecentSampler) Sample(queues []*SourceQueue, n int) []feeds.Item {
	if len(queues) == 0 || n <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-s.MaxAge)
	var candidates []feeds.Item

	// Collect up to quota items per source (recent only)
	for _, q := range queues {
		count := 0
		for i := 0; i < q.Len() && count < s.QuotaPerSource; i++ {
			item := q.Peek(i)
			if item != nil && item.Published.After(cutoff) {
				candidates = append(candidates, *item)
				count++
			}
		}
	}

	// Sort by recency (newest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Published.After(candidates[j].Published)
	})

	// Apply per-source cooldown if configured
	if s.PerSourceCooldown > 0 {
		candidates = applySourceCooldown(candidates, s.PerSourceCooldown)
	}

	// Return top n
	if n > len(candidates) {
		n = len(candidates)
	}
	return candidates[:n]
}

// applySourceCooldown reorders items to ensure minimum spacing between same source
func applySourceCooldown(items []feeds.Item, cooldown int) []feeds.Item {
	if len(items) <= 1 || cooldown <= 0 {
		return items
	}

	result := make([]feeds.Item, 0, len(items))
	remaining := make([]feeds.Item, len(items))
	copy(remaining, items)

	lastSource := make(map[string]int) // source name -> position of last appearance

	for len(remaining) > 0 && len(result) < len(items) {
		// Find best candidate that satisfies cooldown
		bestIdx := -1
		for i, item := range remaining {
			pos := len(result)
			lastPos, seen := lastSource[item.SourceName]
			if !seen || pos-lastPos > cooldown {
				bestIdx = i
				break
			}
		}

		// If no candidate satisfies cooldown, take first anyway
		if bestIdx < 0 {
			bestIdx = 0
		}

		// Add to result and remove from remaining
		item := remaining[bestIdx]
		result = append(result, item)
		lastSource[item.SourceName] = len(result) - 1
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return result
}

// ThrottledRecencySampler is a recency-first sampler with per-source caps.
// Prevents any single source from dominating breaking news view.
// Recommended by GPT-5 for "Recent" view.
type ThrottledRecencySampler struct {
	// MaxPerSource caps items per source in the result
	MaxPerSource int
}

// NewThrottledRecencySampler creates a throttled recency sampler
func NewThrottledRecencySampler() *ThrottledRecencySampler {
	return &ThrottledRecencySampler{
		MaxPerSource: 3, // reasonable default
	}
}

// Sample returns most recent items with per-source caps
func (s *ThrottledRecencySampler) Sample(queues []*SourceQueue, n int) []feeds.Item {
	if len(queues) == 0 || n <= 0 {
		return nil
	}

	// Collect all items
	var all []feeds.Item
	for _, q := range queues {
		all = append(all, q.All()...)
	}

	// Sort by recency
	sort.Slice(all, func(i, j int) bool {
		return all[i].Published.After(all[j].Published)
	})

	// Apply per-source cap
	result := make([]feeds.Item, 0, n)
	sourceCounts := make(map[string]int)

	for _, item := range all {
		if sourceCounts[item.SourceName] < s.MaxPerSource {
			result = append(result, item)
			sourceCounts[item.SourceName]++
			if len(result) >= n {
				break
			}
		}
	}

	return result
}
