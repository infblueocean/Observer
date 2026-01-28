package embedding

import (
	"context"
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/feeds"
	"github.com/abelbrown/observer/internal/logging"
	"github.com/coder/hnsw"
)

// DedupIndex uses embeddings + HNSW for fast semantic deduplication
type DedupIndex struct {
	mu        sync.RWMutex
	embedder  Embedder
	graph     *hnsw.Graph[string]          // HNSW index: itemID -> vector
	groups    map[string][]string          // groupID -> itemIDs (first item is primary)
	itemGroup map[string]string            // itemID -> groupID
	threshold float32                      // similarity threshold (0.85 = 85% similar)
}

// NewDedupIndex creates a new embedding-based dedup index with HNSW
func NewDedupIndex(embedder Embedder, threshold float64) *DedupIndex {
	if threshold <= 0 {
		threshold = 0.85 // Default: 85% similarity = duplicate
	}

	// Create HNSW graph with cosine distance
	g := hnsw.NewGraph[string]()
	g.Distance = hnsw.CosineDistance
	g.M = 16        // Max neighbors per node
	g.EfSearch = 32 // Search quality parameter

	return &DedupIndex{
		embedder:  embedder,
		graph:     g,
		groups:    make(map[string][]string),
		itemGroup: make(map[string]string),
		threshold: float32(threshold),
	}
}

// toFloat32 converts float64 vector to float32 (HNSW uses float32)
func toFloat32(v Vector) []float32 {
	result := make([]float32, len(v))
	for i, f := range v {
		result[i] = float32(f)
	}
	return result
}

// IndexItem generates embedding for an item and checks for duplicates.
// Returns (isDuplicate, primaryID, groupSize).
func (d *DedupIndex) IndexItem(ctx context.Context, item *feeds.Item) (isDup bool, primary string, size int) {
	if d.embedder == nil || !d.embedder.Available() {
		return false, "", 0
	}

	// Recover from any HNSW panics
	defer func() {
		if r := recover(); r != nil {
			logging.Error("HNSW panic recovered in IndexItem", "error", r, "item", item.ID)
			isDup, primary, size = false, "", 0
		}
	}()

	// Generate embedding from title
	text := item.Title
	if len(text) > 200 {
		text = text[:200]
	}

	vec, err := d.embedder.Embed(ctx, text)
	if err != nil {
		logging.Debug("Failed to embed item", "id", item.ID, "error", err)
		return false, "", 0
	}

	// Skip empty vectors
	if len(vec) == 0 {
		logging.Warn("Empty embedding returned", "item", item.ID)
		return false, "", 0
	}

	vec32 := toFloat32(vec)

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if already indexed
	if _, exists := d.graph.Lookup(item.ID); exists {
		groupID := d.itemGroup[item.ID]
		if groupID != "" {
			return true, groupID, len(d.groups[groupID])
		}
		return false, "", 0
	}

	// Search for similar items using HNSW (O(log n))
	var bestMatch string
	var bestSim float32

	if d.graph.Len() > 0 {
		// CosineDistance returns distance (0 = identical, 2 = opposite)
		// Convert to similarity: sim = 1 - (distance / 2)
		neighbors := d.graph.Search(vec32, 5) // Get top 5 candidates
		for _, n := range neighbors {
			// Validate dimensions match
			if len(n.Value) != len(vec32) {
				continue
			}
			distance := hnsw.CosineDistance(vec32, n.Value)
			sim := 1.0 - (distance / 2.0)
			if sim >= d.threshold && sim > bestSim {
				bestSim = sim
				bestMatch = n.Key
			}
		}
	}

	// Add to HNSW index
	d.graph.Add(hnsw.MakeNode(item.ID, vec32))

	if bestMatch == "" {
		// No duplicate found
		return false, "", 0
	}

	// Found a duplicate - add to existing group or create new one
	groupID := d.itemGroup[bestMatch]
	if groupID == "" {
		// Create new group with bestMatch as primary
		groupID = bestMatch
		d.groups[groupID] = []string{bestMatch}
		d.itemGroup[bestMatch] = groupID
	}

	// Add this item to the group
	d.groups[groupID] = append(d.groups[groupID], item.ID)
	d.itemGroup[item.ID] = groupID

	return true, groupID, len(d.groups[groupID])
}

// IndexBatch indexes multiple items efficiently
func (d *DedupIndex) IndexBatch(ctx context.Context, items []feeds.Item) {
	if d.embedder == nil || !d.embedder.Available() {
		return
	}

	// Recover from any HNSW panics
	defer func() {
		if r := recover(); r != nil {
			logging.Error("HNSW panic recovered in IndexBatch", "error", r)
		}
	}()

	start := time.Now()

	// Filter items we haven't seen (check under lock)
	d.mu.Lock()
	var toEmbed []feeds.Item
	for _, item := range items {
		if _, exists := d.graph.Lookup(item.ID); !exists {
			toEmbed = append(toEmbed, item)
		}
	}
	d.mu.Unlock()

	if len(toEmbed) == 0 {
		return
	}

	// Prepare texts
	texts := make([]string, len(toEmbed))
	for i, item := range toEmbed {
		text := item.Title
		if len(text) > 200 {
			text = text[:200]
		}
		texts[i] = text
	}

	// Get embeddings
	vectors, err := d.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		logging.Error("Batch embedding failed", "error", err)
		return
	}

	// Validate we got the right number of vectors
	if len(vectors) != len(toEmbed) {
		logging.Error("Embedding count mismatch", "expected", len(toEmbed), "got", len(vectors))
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Track expected dimensions (from first valid vector)
	var expectedDims int

	// Add to HNSW and find duplicates
	dupCount := 0
	for i, item := range toEmbed {
		vec := vectors[i]

		// Skip empty vectors
		if len(vec) == 0 {
			logging.Warn("Skipping empty embedding", "item", item.ID)
			continue
		}

		// Track/validate dimensions
		if expectedDims == 0 {
			expectedDims = len(vec)
		} else if len(vec) != expectedDims {
			logging.Warn("Dimension mismatch, skipping", "item", item.ID, "expected", expectedDims, "got", len(vec))
			continue
		}

		vec32 := toFloat32(vec)

		// Search for similar items before adding
		var bestMatch string
		var bestSim float32

		if d.graph.Len() > 0 {
			neighbors := d.graph.Search(vec32, 5)
			for _, n := range neighbors {
				// Validate neighbor vector dimensions
				if len(n.Value) != len(vec32) {
					continue
				}
				distance := hnsw.CosineDistance(vec32, n.Value)
				sim := 1.0 - (distance / 2.0)
				if sim >= d.threshold && sim > bestSim {
					bestSim = sim
					bestMatch = n.Key
				}
			}
		}

		// Add to HNSW index
		d.graph.Add(hnsw.MakeNode(item.ID, vec32))

		if bestMatch != "" {
			// Found duplicate
			dupCount++
			groupID := d.itemGroup[bestMatch]
			if groupID == "" {
				groupID = bestMatch
				d.groups[groupID] = []string{bestMatch}
				d.itemGroup[bestMatch] = groupID
			}
			d.groups[groupID] = append(d.groups[groupID], item.ID)
			d.itemGroup[item.ID] = groupID
		}
	}

	logging.Info("Batch embedding complete",
		"items", len(toEmbed),
		"duplicates", dupCount,
		"dims", expectedDims,
		"duration", time.Since(start).Round(time.Millisecond),
		"groups", len(d.groups))
}

// IsPrimary returns true if this item is the primary (first) in its duplicate group
func (d *DedupIndex) IsPrimary(itemID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	groupID := d.itemGroup[itemID]
	if groupID == "" {
		return true // Not in any group = unique = primary
	}

	items := d.groups[groupID]
	return len(items) > 0 && items[0] == itemID
}

// GetPrimaryItems filters a list to only include primary (non-duplicate) items
func (d *DedupIndex) GetPrimaryItems(items []feeds.Item) []feeds.Item {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []feeds.Item
	for _, item := range items {
		groupID := d.itemGroup[item.ID]
		if groupID == "" {
			// Not in any group = unique
			result = append(result, item)
		} else if d.groups[groupID][0] == item.ID {
			// Is the primary of its group
			result = append(result, item)
		}
	}
	return result
}

// GetGroupSize returns the number of items in an item's duplicate group
func (d *DedupIndex) GetGroupSize(itemID string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	groupID := d.itemGroup[itemID]
	if groupID == "" {
		return 1 // Just itself
	}
	return len(d.groups[groupID])
}

// GetDuplicates returns all duplicate item IDs for a given item
func (d *DedupIndex) GetDuplicates(itemID string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	groupID := d.itemGroup[itemID]
	if groupID == "" {
		return nil
	}

	var result []string
	for _, id := range d.groups[groupID] {
		if id != itemID {
			result = append(result, id)
		}
	}
	return result
}

// GetSimilarity returns the cosine similarity between two items (if both are indexed)
func (d *DedupIndex) GetSimilarity(itemA, itemB string) float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	vecA, okA := d.graph.Lookup(itemA)
	vecB, okB := d.graph.Lookup(itemB)
	if !okA || !okB {
		return 0
	}
	distance := hnsw.CosineDistance(vecA, vecB)
	return float64(1.0 - (distance / 2.0))
}

// Stats returns index statistics
func (d *DedupIndex) Stats() (indexed, groups, duplicates int) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	indexed = d.graph.Len()
	groups = len(d.groups)
	for _, members := range d.groups {
		duplicates += len(members) - 1 // All but primary
	}
	return
}

// HasEmbedding returns true if we have an embedding for this item
func (d *DedupIndex) HasEmbedding(itemID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.graph.Lookup(itemID)
	return ok
}
