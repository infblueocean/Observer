package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/abelbrown/observer/internal/fetch"
	"github.com/abelbrown/observer/internal/logging"
	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// FetchController manages periodic fetching of all sources.
//
// It submits fetch jobs to the work pool and saves results to the store.
//
// # Thread Safety
//
// FetchController is safe for concurrent use. Multiple goroutines can call
// FetchAll(), FetchSource(), etc. concurrently.
//
// # Lifecycle
//
// Call Start() to begin periodic fetching, Stop() to halt. Stop() blocks until
// all in-progress fetches complete.
type FetchController struct {
	sources []fetch.SourceConfig
	store   *model.Store
	pool    *work.Pool

	// Track last fetch time per source (protected by mu)
	mu          sync.RWMutex
	lastFetched map[string]time.Time

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Prevent double-start
	started bool
	startMu sync.Mutex
}

// NewFetchController creates a fetch controller.
//
// Does not start fetching - call Start() to begin.
func NewFetchController(sources []fetch.SourceConfig, store *model.Store, pool *work.Pool) *FetchController {
	return &FetchController{
		sources:     sources,
		store:       store,
		pool:        pool,
		lastFetched: make(map[string]time.Time),
	}
}

// Start begins periodic fetching.
//
// This method is idempotent - calling it multiple times has no effect after
// the first call.
//
// The provided context controls the lifetime of the fetch controller. When
// ctx is cancelled, fetching stops (same as calling Stop()).
func (c *FetchController) Start(ctx context.Context) {
	c.startMu.Lock()
	if c.started {
		c.startMu.Unlock()
		return
	}
	c.started = true
	c.startMu.Unlock()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Initial fetch of all sources
	c.fetchDue()

	// Periodic check for due sources
	c.wg.Add(1)
	go c.pollLoop()

	logging.Info("Fetch controller started", "sources", len(c.sources))
}

// Stop halts fetching and waits for in-progress fetches to complete.
//
// This method is idempotent and safe to call multiple times.
func (c *FetchController) Stop() {
	c.startMu.Lock()
	if !c.started {
		c.startMu.Unlock()
		return
	}
	c.startMu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	logging.Info("Fetch controller stopped")
}

func (c *FetchController) pollLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.fetchDue()
		}
	}
}

// fetchDue submits fetch jobs for all sources that are due.
func (c *FetchController) fetchDue() {
	for _, src := range c.sources {
		// Check context before each source
		if c.ctx.Err() != nil {
			return
		}
		if c.isDue(src) {
			c.submitFetch(src)
		}
	}
}

func (c *FetchController) isDue(src fetch.SourceConfig) bool {
	c.mu.RLock()
	lastFetch := c.lastFetched[src.Name]
	c.mu.RUnlock()

	if lastFetch.IsZero() {
		return true
	}

	interval := time.Duration(src.RefreshMinutes) * time.Minute
	return time.Since(lastFetch) >= interval
}

func (c *FetchController) submitFetch(src fetch.SourceConfig) {
	fetcher := fetch.CreateFetcher(src)

	c.pool.SubmitWithData(
		work.TypeFetch,
		fmt.Sprintf("Fetching %s", src.Name),
		src.Name,
		src.Category,
		func() (string, any, error) {
			// Check context before starting fetch
			if c.ctx.Err() != nil {
				return "", nil, c.ctx.Err()
			}

			items, err := fetcher.Fetch()
			if err != nil {
				// Update source status with error
				if updateErr := c.store.UpdateSourceStatus(src.Name, 0, err.Error()); updateErr != nil {
					logging.Error("Failed to update source status", "source", src.Name, "error", updateErr)
				}
				return "", nil, err
			}

			// Save items to store
			newCount, saveErr := c.store.SaveItems(items)
			if saveErr != nil {
				logging.Error("Failed to save items", "source", src.Name, "error", saveErr)
				return "", nil, saveErr
			}

			// Update source status (success)
			if updateErr := c.store.UpdateSourceStatus(src.Name, len(items), ""); updateErr != nil {
				logging.Error("Failed to update source status", "source", src.Name, "error", updateErr)
			}

			// Update last fetched time
			c.mu.Lock()
			c.lastFetched[src.Name] = time.Now()
			c.mu.Unlock()

			return fmt.Sprintf("%d items (%d new)", len(items), newCount), items, nil
		},
	)
}

// FetchAll triggers an immediate fetch of all sources.
//
// Jobs are submitted asynchronously - this method returns immediately.
func (c *FetchController) FetchAll() {
	for _, src := range c.sources {
		c.submitFetch(src)
	}
}

// FetchSource triggers an immediate fetch of a specific source.
//
// Returns false if the source name is not found.
func (c *FetchController) FetchSource(name string) bool {
	for _, src := range c.sources {
		if src.Name == name {
			c.submitFetch(src)
			return true
		}
	}
	logging.Warn("FetchSource: source not found", "name", name)
	return false
}

// SourceCount returns the number of configured sources.
func (c *FetchController) SourceCount() int {
	return len(c.sources)
}

// Sources returns the configured sources.
func (c *FetchController) Sources() []fetch.SourceConfig {
	return c.sources
}
