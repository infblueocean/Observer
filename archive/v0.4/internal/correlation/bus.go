package correlation

import (
	"context"
	"sync"

	"github.com/abelbrown/observer/internal/feeds"
)

// Bus is the event bus for the correlation pipeline.
// All communication flows through buffered channels with non-blocking sends.
type Bus struct {
	// Pipeline channels (buffered, non-blocking sends)
	items    chan *feeds.Item  // Input
	deduped  chan *DedupResult // Stage 1 → 2
	enriched chan *EntityResult // Stage 2 → 3
	clustered chan *ClusterResult // Stage 3 → 4

	// Output to Bubble Tea
	Results chan CorrelationEvent

	// Control
	ctx      context.Context
	cancel   context.CancelFunc
	closeOnce sync.Once
}

// NewBus creates a new event bus with the specified buffer size.
func NewBus(bufferSize int) *Bus {
	return &Bus{
		items:     make(chan *feeds.Item, bufferSize),
		deduped:   make(chan *DedupResult, bufferSize),
		enriched:  make(chan *EntityResult, bufferSize),
		clustered: make(chan *ClusterResult, bufferSize),
		Results:   make(chan CorrelationEvent, bufferSize),
	}
}

// Start initializes the bus with a context for cancellation.
func (b *Bus) Start(ctx context.Context) {
	b.ctx, b.cancel = context.WithCancel(ctx)
}

// Stop cancels all bus operations and closes all channels.
// Safe to call multiple times.
func (b *Bus) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.closeOnce.Do(func() {
		// Close internal pipeline channels first
		close(b.items)
		close(b.deduped)
		close(b.enriched)
		close(b.clustered)
		// Close output channel last
		close(b.Results)
	})
}

// Send is non-blocking - drops if the results channel is full.
// This implements backpressure without blocking the pipeline.
func (b *Bus) Send(event CorrelationEvent) {
	select {
	case b.Results <- event:
	default:
		// Drop - UI will catch up on next render
	}
}

// Context returns the bus context for checking cancellation.
func (b *Bus) Context() context.Context {
	return b.ctx
}
