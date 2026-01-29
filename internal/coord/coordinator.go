// Package coord provides background fetch coordination for Observer.
package coord

import (
	"context"
	"log"
	"regexp"
	"strings"
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

// embedWorkerInterval is the time between embedding worker cycles.
const embedWorkerInterval = 2 * time.Second

// embedWorkerBatchSize is the number of items to embed per worker cycle.
const embedWorkerBatchSize = 10

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

// StartEmbeddingWorker starts a background worker that continuously embeds
// items without embeddings. Processes items in small batches with delays
// to avoid overwhelming Ollama. Use this for backfilling existing items.
func (c *Coordinator) StartEmbeddingWorker(ctx context.Context) {
	if c.embedder == nil {
		return
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		ticker := time.NewTicker(embedWorkerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.embedBatch(ctx, embedWorkerBatchSize)
			}
		}
	}()
}

// embedBatch embeds up to limit items that need embeddings.
// Returns early if embedder unavailable or context cancelled.
func (c *Coordinator) embedBatch(ctx context.Context, limit int) {
	if !c.embedder.Available() {
		return
	}

	items, err := c.store.GetItemsNeedingEmbedding(limit)
	if err != nil || len(items) == 0 {
		return
	}

	c.embedItems(ctx, items)
}

// htmlTagRe matches HTML tags.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// whitespaceRe matches runs of whitespace.
var whitespaceRe = regexp.MustCompile(`\s+`)

// sanitizeForEmbedding strips HTML tags, collapses whitespace, and caps at maxChars.
func sanitizeForEmbedding(s string, maxChars int) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = whitespaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if len(s) > maxChars {
		s = s[:maxChars]
	}
	return s
}

// embedTextForItem builds the text to embed for a single item.
// Title + sanitized summary, capped at ~2000 chars (~500 tokens).
func embedTextForItem(item store.Item) string {
	text := item.Title
	if item.Summary != "" {
		clean := sanitizeForEmbedding(item.Summary, 2000-len(text))
		if clean != "" {
			text += " " + clean
		}
	}
	return text
}

// embedItems generates and saves embeddings for the given items.
// Uses batch embedding if available, otherwise falls back to sequential.
func (c *Coordinator) embedItems(ctx context.Context, items []store.Item) {
	if len(items) == 0 {
		return
	}

	// Batch path: single API call for all items
	if batcher, ok := c.embedder.(embed.BatchEmbedder); ok {
		texts := make([]string, len(items))
		for i, item := range items {
			texts[i] = embedTextForItem(item)
		}
		embeddings, err := batcher.EmbedBatch(ctx, texts)
		if err != nil {
			log.Printf("coord: batch embedding failed: %v", err)
			return
		}
		for i, emb := range embeddings {
			if ctx.Err() != nil {
				return
			}
			if i < len(items) {
				if err := c.store.SaveEmbedding(items[i].ID, emb); err != nil {
					log.Printf("coord: failed to save embedding for %s: %v", items[i].ID, err)
				}
			}
		}
		return
	}

	// Sequential fallback (Ollama)
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		if !c.embedder.Available() {
			return
		}

		text := embedTextForItem(item)

		embedding, err := c.embedder.Embed(ctx, text)
		if err != nil {
			continue
		}

		if err := c.store.SaveEmbedding(item.ID, embedding); err != nil {
			log.Printf("coord: failed to save embedding for %s: %v", item.ID, err)
		}
	}
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

	c.embedItems(ctx, items)
}
