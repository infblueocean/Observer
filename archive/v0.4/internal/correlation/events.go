package correlation

// CorrelationEvent is the interface for all events flowing from the pipeline to Bubble Tea.
// Events are non-blocking - the UI reads from a channel and re-renders on each event.
type CorrelationEvent interface {
	correlationEvent() // marker method
}

// DuplicateFound is emitted when an item is detected as a duplicate.
type DuplicateFound struct {
	ItemID    string
	PrimaryID string
	GroupSize int
}

func (DuplicateFound) correlationEvent() {}

// EntitiesExtracted is emitted when entities are extracted from an item.
type EntitiesExtracted struct {
	ItemID   string
	Entities []Entity
}

func (EntitiesExtracted) correlationEvent() {}

// ClusterUpdated is emitted when an item joins or creates a cluster.
type ClusterUpdated struct {
	ClusterID string
	ItemID    string
	Size      int
	Velocity  float64
	Sparkline []float64
	IsNew     bool
}

func (ClusterUpdated) correlationEvent() {}

// DisagreementFound is emitted when sources conflict within a cluster.
type DisagreementFound struct {
	ClusterID string
	ItemIDs   []string
	Reason    string
}

func (DisagreementFound) correlationEvent() {}

// BatchComplete is emitted after processing a batch of items.
type BatchComplete struct {
	ItemsProcessed  int
	DuplicatesFound int
	EntitiesFound   int
	ClustersUpdated int
}

func (BatchComplete) correlationEvent() {}
