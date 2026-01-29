// Package coord provides background fetch coordination for Observer.
package coord

import (
	"context"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sync/errgroup"

	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/fetch"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui"
)

// fetchInterval is the time between fetch cycles.
const fetchInterval = 5 * time.Minute

// fetchTimeout is the timeout for each individual fetch.
const fetchTimeout = 30 * time.Second

// maxConcurrentFetches limits parallel fetch operations.
const maxConcurrentFetches = 5

// embedBatchSize is the max items to embed per fetch cycle.
const embedBatchSize = 100

// fetcher interface for dependency injection (testing).
type fetcher interface {
	Fetch(ctx context.Context, src fetch.Source) ([]store.Item, error)
}

// Coordinator manages background fetching and embedding.
// Uses context cancellation as the ONLY stop mechanism.
type Coordinator struct {
	store    *store.Store
	fetcher  fetcher               // interface for testing
	embedder embed.Embedder        // optional: nil to disable embedding
	sources  []fetch.Source        // IMMUTABLE: set at construction, never modified
	wg       sync.WaitGroup
}

// NewCoordinator creates a Coordinator with the real fetcher.
// The embedder is optional (nil to disable embedding).
func NewCoordinator(s *store.Store, f *fetch.Fetcher, e embed.Embedder, sources []fetch.Source) *Coordinator {
	return NewCoordinatorWithFetcher(s, f, e, sources)
}

// NewCoordinatorWithFetcher allows injecting a custom fetcher (for testing).
// The embedder is optional (nil to disable embedding).
func NewCoordinatorWithFetcher(s *store.Store, f fetcher, e embed.Embedder, sources []fetch.Source) *Coordinator {
	// Copy sources slice to ensure immutability
	sourcesCopy := make([]fetch.Source, len(sources))
	copy(sourcesCopy, sources)

	return &Coordinator{
		store:    s,
		fetcher:  f,
		embedder: e,
		sources:  sourcesCopy,
	}
}

// Start begins background fetching. Call with a cancellable context.
// Performs initial fetch immediately, then every 5 minutes.
func (c *Coordinator) Start(ctx context.Context, program *tea.Program) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		// Perform initial fetch immediately
		c.fetchAll(ctx, program)

		// Create ticker for periodic fetches
		ticker := time.NewTicker(fetchInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.fetchAll(ctx, program)
			}
		}
	}()
}

// Wait blocks until the background goroutine exits.
// Call after canceling the context passed to Start.
func (c *Coordinator) Wait() {
	c.wg.Wait()
}

// fetchAll fetches all sources in parallel, then embeds new items.
// Each fetch has a 30-second timeout.
// Sends ui.FetchComplete messages to the program (order non-deterministic).
func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
	var g errgroup.Group
	g.SetLimit(maxConcurrentFetches)

	for _, src := range c.sources {
		g.Go(func() error {
			// Early exit if context cancelled
			if ctx.Err() != nil {
				return nil
			}
			c.fetchSource(ctx, src, program)
			return nil // never fail the group - errors reported per-source
		})
	}

	_ = g.Wait() // All goroutines return nil, but explicit discard for clarity

	// After all fetches, embed new items (if embedder available)
	c.embedNewItems(ctx)
}

// fetchSource fetches a single source with timeout.
// Sends ui.FetchComplete message when done.
func (c *Coordinator) fetchSource(ctx context.Context, src fetch.Source, program *tea.Program) {
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	items, err := c.fetcher.Fetch(fetchCtx, src)

	// Save items if fetch succeeded
	newItems := 0
	if err == nil && len(items) > 0 {
		var saveErr error
		newItems, saveErr = c.store.SaveItems(items)
		_ = saveErr // intentionally ignored - fetch succeeded, save errors are rare
	}

	// Send completion message (handle nil program gracefully for testing)
	if program != nil {
		program.Send(ui.FetchComplete{
			Source:   src.Name,
			NewItems: newItems,
			Err:      err,
		})
	}
}

// embedNewItems generates embeddings for items that don't have one.
// Skips silently if embedder is nil or unavailable.
func (c *Coordinator) embedNewItems(ctx context.Context) {
	if c.embedder == nil || !c.embedder.Available() {
		return
	}

	items, err := c.store.GetItemsNeedingEmbedding(embedBatchSize)
	if err != nil || len(items) == 0 {
		return
	}

	for _, item := range items {
		if ctx.Err() != nil {
			return
		}

		// Re-check availability (Ollama may have stopped)
		if !c.embedder.Available() {
			return
		}

		text := item.Title
		if item.Summary != "" {
			text += " " + item.Summary
		}

		embedding, err := c.embedder.Embed(ctx, text)
		if err != nil {
			// Skip failed embeds - don't stop the whole process
			continue
		}

		// Ignore save errors - embedding is non-critical functionality
		_ = c.store.SaveEmbedding(item.ID, embedding)
	}
}
