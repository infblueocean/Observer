// Package coord provides background fetch coordination for Observer.
package coord

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/otel"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui"
)

// fetchInterval is the time between fetch cycles.
const fetchInterval = 5 * time.Minute

// embedBatchSize is the max items to embed per cycle.
const embedBatchSize = 100

// embedWorkerInterval is the time between embedding worker cycles.
const embedWorkerInterval = 2 * time.Second

// Provider fetches items from external sources.
type Provider interface {
	Fetch(ctx context.Context) ([]store.Item, error)
}

// Coordinator manages background fetching and embedding.
// Uses context cancellation as the ONLY stop mechanism.
type Coordinator struct {
	store    *store.Store
	provider Provider
	embedder embed.Embedder // optional: nil to disable embedding
	logger   *otel.Logger
	wg       sync.WaitGroup
}

// NewCoordinator creates a Coordinator with the given provider.
// The embedder is optional (nil to disable embedding).
func NewCoordinator(s *store.Store, p Provider, e embed.Embedder, l *otel.Logger) *Coordinator {
	if l == nil {
		l = otel.NewNullLogger()
	}
	return &Coordinator{
		store:    s,
		provider: p,
		embedder: e,
		logger:   l,
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
// to avoid overwhelming the API. Use this for backfilling existing items.
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
				c.embedBatch(ctx, embedBatchSize)
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
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) > maxChars {
		return string(runes[:maxChars])
	}
	return s
}

// embedTextForItem builds the text to embed for a single item.
// Title + sanitized summary, capped at ~2000 chars (~500 tokens).
func embedTextForItem(item store.Item) string {
	text := item.Title
	if item.Summary != "" {
		remaining := 2000 - len([]rune(text))
		if remaining > 0 {
			clean := sanitizeForEmbedding(item.Summary, remaining)
			if clean != "" {
				text += " " + clean
			}
		}
	}
	return text
}

// embedItems generates and saves embeddings for the given items.
// Uses batch embedding if available, otherwise falls back to sequential.
// If batch embedding fails, falls back to sequential to avoid discarding the entire batch.
func (c *Coordinator) embedItems(ctx context.Context, items []store.Item) {
	if len(items) == 0 {
		return
	}

	// Build texts for all items, filtering out empty ones
	type itemText struct {
		item store.Item
		text string
	}
	var pairs []itemText
	for _, item := range items {
		text := embedTextForItem(item)
		if strings.TrimSpace(text) == "" {
			c.logger.Emit(otel.Event{Kind: otel.KindEmbedError, Level: otel.LevelWarn, Comp: "coord", Source: item.ID, Msg: "skipping embedding: empty text"})
			continue
		}
		pairs = append(pairs, itemText{item: item, text: text})
	}
	if len(pairs) == 0 {
		return
	}

	// Batch path: single API call for all items
	if batcher, ok := c.embedder.(embed.BatchEmbedder); ok {
		texts := make([]string, len(pairs))
		for i, p := range pairs {
			texts[i] = p.text
		}
		embeddings, err := batcher.EmbedBatch(ctx, texts)
		if err != nil {
			c.logger.Emit(otel.Event{Kind: otel.KindEmbedError, Level: otel.LevelError, Comp: "coord", Msg: "batch embedding failed, falling back to sequential", Err: err.Error()})
			// Fall through to sequential path below
		} else {
			for i, emb := range embeddings {
				if ctx.Err() != nil {
					return
				}
				if i < len(pairs) {
					if err := c.store.SaveEmbedding(pairs[i].item.ID, emb); err != nil {
						c.logger.Emit(otel.Event{Kind: otel.KindError, Level: otel.LevelError, Comp: "coord", Source: pairs[i].item.ID, Msg: "failed to save embedding", Err: err.Error()})
					}
				}
			}
			return
		}
	}

	// Sequential fallback (Ollama, or batch failure)
	for _, p := range pairs {
		if ctx.Err() != nil {
			return
		}
		if !c.embedder.Available() {
			return
		}

		embedding, err := c.embedder.Embed(ctx, p.text)
		if err != nil {
			c.logger.Emit(otel.Event{Kind: otel.KindEmbedError, Level: otel.LevelError, Comp: "coord", Source: p.item.ID, Err: err.Error()})
			continue
		}

		if err := c.store.SaveEmbedding(p.item.ID, embedding); err != nil {
			c.logger.Emit(otel.Event{Kind: otel.KindError, Level: otel.LevelError, Comp: "coord", Source: p.item.ID, Msg: "failed to save embedding", Err: err.Error()})
		}
	}
}

// fetchAll fetches from the provider, saves items, sends completion, then embeds.
func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
	if ctx.Err() != nil {
		return
	}

	c.logger.Emit(otel.Event{Kind: otel.KindFetchStart, Level: otel.LevelInfo, Comp: "coord"})
	start := time.Now()

	items, err := c.provider.Fetch(ctx)

	newItems := 0
	if err == nil && len(items) > 0 {
		var saveErr error
		newItems, saveErr = c.store.SaveItems(items)
		if saveErr != nil {
			c.logger.Emit(otel.Event{Kind: otel.KindError, Level: otel.LevelError, Comp: "coord", Msg: "failed to save items", Err: saveErr.Error()})
		}
	}

	if program != nil {
		program.Send(ui.FetchComplete{
			Source:   "all",
			NewItems: newItems,
			Err:      err,
		})
	}

	c.logger.Emit(otel.Event{Kind: otel.KindFetchComplete, Level: otel.LevelInfo, Comp: "coord", Dur: time.Since(start), Count: newItems})

	// After fetch, embed new items (if embedder available)
	c.embedNewItems(ctx)
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
