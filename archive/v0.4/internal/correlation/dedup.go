package correlation

import (
	"sync"

	"github.com/abelbrown/observer/internal/feeds"
)

// DedupResult is the output of duplicate detection.
type DedupResult struct {
	Item        *feeds.Item
	IsDuplicate bool
	PrimaryID   string
	GroupSize   int
}

// DedupIndex maintains SimHash data structures for O(1) duplicate detection.
// Uses LSH (Locality Sensitive Hashing) buckets for fast candidate lookup.
type DedupIndex struct {
	mu        sync.RWMutex
	hashes    map[string]uint64   // itemID → simhash
	buckets   map[uint16][]string // LSH bucket → itemIDs
	groups    map[string][]string // groupID → itemIDs
	itemGroup map[string]string   // itemID → groupID
}

// NewDedupIndex creates an empty deduplication index.
func NewDedupIndex() *DedupIndex {
	return &DedupIndex{
		hashes:    make(map[string]uint64),
		buckets:   make(map[uint16][]string),
		groups:    make(map[string][]string),
		itemGroup: make(map[string]string),
	}
}

// Check determines if an item is a duplicate.
// Returns (isDuplicate, primaryID, groupSize).
// This runs INLINE - must be <1ms.
func (d *DedupIndex) Check(item *feeds.Item) (bool, string, int) {
	hash := SimHash(item.Title)

	// Get LSH bucket for fast candidate lookup
	bucket := uint16(hash >> 48) // Top 16 bits as bucket key

	d.mu.RLock()
	candidates := d.buckets[bucket]
	d.mu.RUnlock()

	// Check candidates for actual duplicates
	for _, candidateID := range candidates {
		d.mu.RLock()
		candidateHash := d.hashes[candidateID]
		d.mu.RUnlock()

		if HammingDistance(hash, candidateHash) <= 3 { // ~90% similar
			// Found duplicate
			d.mu.Lock()
			defer d.mu.Unlock()

			groupID := d.itemGroup[candidateID]
			if groupID == "" {
				// Create new group
				groupID = candidateID
				d.groups[groupID] = []string{candidateID}
				d.itemGroup[candidateID] = groupID
			}

			// Add to group
			d.groups[groupID] = append(d.groups[groupID], item.ID)
			d.itemGroup[item.ID] = groupID
			d.hashes[item.ID] = hash
			size := len(d.groups[groupID])

			return true, groupID, size
		}
	}

	// Not a duplicate - add to index
	d.mu.Lock()
	d.hashes[item.ID] = hash
	d.buckets[bucket] = append(d.buckets[bucket], item.ID)
	d.mu.Unlock()

	return false, "", 0
}

// GetGroupSize returns the size of an item's duplicate group.
func (d *DedupIndex) GetGroupSize(itemID string) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if groupID, ok := d.itemGroup[itemID]; ok {
		return len(d.groups[groupID])
	}
	return 0
}

// IsPrimary returns true if this item is the primary (first) in its group.
func (d *DedupIndex) IsPrimary(itemID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	groupID := d.itemGroup[itemID]
	if groupID == "" {
		return true // Not in a group = primary by default
	}
	items := d.groups[groupID]
	return len(items) > 0 && items[0] == itemID
}

// GetDuplicates returns all item IDs in the same group (excluding the queried item).
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

// HammingDistance counts the number of differing bits between two hashes.
func HammingDistance(a, b uint64) int {
	xor := a ^ b
	count := 0
	for xor != 0 {
		count++
		xor &= xor - 1
	}
	return count
}

// Stats returns statistics about the dedup index.
func (d *DedupIndex) Stats() (items, groups int) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.hashes), len(d.groups)
}
