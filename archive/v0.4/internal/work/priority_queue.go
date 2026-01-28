package work

import "container/heap"

// priorityQueue implements heap.Interface for work items.
// Higher priority items are popped first (max-heap by priority).
// For equal priority, earlier items (by CreatedAt) are popped first (FIFO within priority).
type priorityQueue []*Item

// Len returns the number of items in the queue.
func (pq priorityQueue) Len() int { return len(pq) }

// Less reports whether item i should be popped before item j.
// Higher priority first; for equal priority, earlier creation time first.
func (pq priorityQueue) Less(i, j int) bool {
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority > pq[j].Priority // Higher priority first
	}
	return pq[i].CreatedAt.Before(pq[j].CreatedAt) // Earlier first (FIFO)
}

// Swap swaps the items at indices i and j.
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].heapIndex = i
	pq[j].heapIndex = j
}

// Push adds an item to the queue.
func (pq *priorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*Item)
	item.heapIndex = n
	*pq = append(*pq, item)
}

// Pop removes and returns the highest priority item.
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil      // avoid memory leak
	item.heapIndex = -1 // mark as removed
	*pq = old[0 : n-1]
	return item
}

// update modifies the priority of an item in the queue.
// Returns true if the update succeeded, false if the item is not in the queue
// (e.g., it was already popped or never added).
func (pq *priorityQueue) update(item *Item, priority int) bool {
	// Validate that item is still in the queue
	if item == nil || item.heapIndex < 0 || item.heapIndex >= len(*pq) {
		return false
	}
	// Double-check the item at this index is actually the one we want to update
	if (*pq)[item.heapIndex] != item {
		return false
	}
	item.Priority = priority
	heap.Fix(pq, item.heapIndex)
	return true
}

// peek returns the highest priority item without removing it.
// WARNING: The returned item is a pointer to internal queue data.
// The caller MUST NOT modify the returned item's fields (especially Priority,
// CreatedAt, or heapIndex) as this would corrupt the heap invariant.
// Use update() to safely modify an item's priority.
func (pq priorityQueue) peek() *Item {
	if len(pq) == 0 {
		return nil
	}
	return pq[0]
}
