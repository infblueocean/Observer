package correlation

import (
	"sync"
	"time"
)

// Cluster groups related items about the same event/story.
type Cluster struct {
	ID          string
	PrimaryID   string    // representative item
	ItemIDs     []string
	Title       string    // from primary item
	Size        int
	Velocity    float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ClusterResult is the output of cluster assignment.
type ClusterResult struct {
	ItemID    string
	Item      *EntityResult
	Cluster   *Cluster
	IsNew     bool
	Merged    []string // IDs of clusters that were merged
}

// ClusterEngine handles incremental clustering by entity overlap.
type ClusterEngine struct {
	mu          sync.RWMutex
	clusters    map[string]*Cluster
	itemCluster map[string]string

	// For incremental clustering
	entityIndex map[string][]string // entityID → clusterIDs
}

// NewClusterEngine creates a new cluster engine.
func NewClusterEngine() *ClusterEngine {
	return &ClusterEngine{
		clusters:    make(map[string]*Cluster),
		itemCluster: make(map[string]string),
		entityIndex: make(map[string][]string),
	}
}

// AssignToCluster finds or creates a cluster for an item.
// Uses entity overlap for instant clustering.
// Budget: <10ms per item.
func (c *ClusterEngine) AssignToCluster(er *EntityResult) *ClusterResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find candidate clusters by entity overlap
	candidates := make(map[string]int) // clusterID → overlap count
	for _, e := range er.Entities {
		for _, clusterID := range c.entityIndex[e.ID] {
			candidates[clusterID]++
		}
	}

	// Find best match (>50% entity overlap)
	var bestCluster string
	var bestOverlap int
	threshold := len(er.Entities) / 2
	if threshold < 1 {
		threshold = 1
	}

	for clusterID, overlap := range candidates {
		if overlap >= threshold && overlap > bestOverlap {
			// Check temporal decay - skip if cluster is stale (>48h old)
			cluster := c.clusters[clusterID]
			if time.Since(cluster.UpdatedAt) > 48*time.Hour {
				continue
			}
			bestCluster = clusterID
			bestOverlap = overlap
		}
	}

	if bestCluster != "" {
		// Add to existing cluster
		cluster := c.clusters[bestCluster]
		cluster.ItemIDs = append(cluster.ItemIDs, er.ItemID)
		cluster.Size = len(cluster.ItemIDs)
		cluster.UpdatedAt = time.Now()
		c.itemCluster[er.ItemID] = bestCluster

		// Update entity index
		for _, e := range er.Entities {
			c.entityIndex[e.ID] = appendUnique(c.entityIndex[e.ID], bestCluster)
		}

		return &ClusterResult{
			ItemID:  er.ItemID,
			Item:    er,
			Cluster: cluster,
			IsNew:   false,
		}
	}

	// Create new cluster
	cluster := &Cluster{
		ID:        er.ItemID, // Use first item ID as cluster ID
		PrimaryID: er.ItemID,
		ItemIDs:   []string{er.ItemID},
		Title:     er.Item.Title,
		Size:      1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	c.clusters[cluster.ID] = cluster
	c.itemCluster[er.ItemID] = cluster.ID

	// Index entities
	for _, e := range er.Entities {
		c.entityIndex[e.ID] = append(c.entityIndex[e.ID], cluster.ID)
	}

	return &ClusterResult{
		ItemID:  er.ItemID,
		Item:    er,
		Cluster: cluster,
		IsNew:   true,
	}
}

// GetCluster returns the cluster for an item.
func (c *ClusterEngine) GetCluster(itemID string) *Cluster {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if clusterID, ok := c.itemCluster[itemID]; ok {
		return c.clusters[clusterID]
	}
	return nil
}

// GetClusterByID returns a cluster by its ID.
func (c *ClusterEngine) GetClusterByID(clusterID string) *Cluster {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clusters[clusterID]
}

// IsClusterPrimary returns true if the item is the primary of its cluster.
func (c *ClusterEngine) IsClusterPrimary(itemID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	clusterID, ok := c.itemCluster[itemID]
	if !ok {
		return false
	}
	cluster := c.clusters[clusterID]
	return cluster != nil && cluster.PrimaryID == itemID
}

// GetAllClusters returns all clusters.
func (c *ClusterEngine) GetAllClusters() []*Cluster {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*Cluster, 0, len(c.clusters))
	for _, cluster := range c.clusters {
		result = append(result, cluster)
	}
	return result
}

// Stats returns statistics about clusters.
func (c *ClusterEngine) Stats() (clusters, items int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.clusters), len(c.itemCluster)
}

// appendUnique appends a value to a slice if it's not already present.
func appendUnique(slice []string, val string) []string {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}
