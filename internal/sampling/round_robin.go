package sampling

import (
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

	// Sort by published time (newest first)
	// Using simple bubble sort for small collections, replace with sort.Slice for larger
	for i := 0; i < len(all)-1; i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].Published.After(all[i].Published) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	// Return top n
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}
