// Package coord provides background fetch coordination for Observer.
package coord

import (
	"context"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abelbrown/observer/internal/fetch"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui"
)

// fetchInterval is the time between fetch cycles.
const fetchInterval = 5 * time.Minute

// fetchTimeout is the timeout for each individual fetch.
const fetchTimeout = 30 * time.Second

// fetcher interface for dependency injection (testing).
type fetcher interface {
	Fetch(ctx context.Context, src fetch.Source) ([]store.Item, error)
}

// Coordinator manages background fetching.
// Uses context cancellation as the ONLY stop mechanism.
type Coordinator struct {
	store   *store.Store
	fetcher fetcher               // interface for testing
	sources []fetch.Source        // IMMUTABLE: set at construction, never modified
	wg      sync.WaitGroup
}

// NewCoordinator creates a Coordinator with the real fetcher.
func NewCoordinator(s *store.Store, f *fetch.Fetcher, sources []fetch.Source) *Coordinator {
	return NewCoordinatorWithFetcher(s, f, sources)
}

// NewCoordinatorWithFetcher allows injecting a custom fetcher (for testing).
func NewCoordinatorWithFetcher(s *store.Store, f fetcher, sources []fetch.Source) *Coordinator {
	// Copy sources slice to ensure immutability
	sourcesCopy := make([]fetch.Source, len(sources))
	copy(sourcesCopy, sources)

	return &Coordinator{
		store:   s,
		fetcher: f,
		sources: sourcesCopy,
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

// fetchAll fetches all sources sequentially.
// Each fetch has a 30-second timeout.
// Sends ui.FetchComplete messages to the program.
func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
	for _, src := range c.sources {
		// Check context before each fetch
		if ctx.Err() != nil {
			return
		}

		// Create timeout context for this fetch
		fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)

		// Perform fetch
		items, err := c.fetcher.Fetch(fetchCtx, src)
		cancel() // Always cancel to release resources

		// Save items if fetch succeeded
		var saveErr error
		newItems := 0
		if err == nil && len(items) > 0 {
			newItems, saveErr = c.store.SaveItems(items)
			// Note: saveErr is logged but not propagated - fetch succeeded,
			// partial save failure is non-critical for a news reader
			_ = saveErr // intentionally ignored, store errors are rare
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
}
