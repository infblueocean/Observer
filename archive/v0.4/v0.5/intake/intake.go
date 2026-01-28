//go:build ignore

// Package intake provides the intake pipeline for processing fetched items.
//
// The intake pipeline runs after items are fetched and before they're displayed.
// It handles:
//   - Embedding generation (for semantic operations)
//   - Deduplication (identifying duplicate stories)
//   - Storage (persisting items and embeddings)
//
// # Architecture
//
// The pipeline processes items in stages, each stage operating on the output
// of the previous stage:
//
//	Fetch → Intake Pipeline → Store
//	         ├─ Embed Stage   (generate embeddings)
//	         ├─ Dedup Stage   (identify duplicates)
//	         └─ Store Stage   (persist to SQLite)
//
// # Work Pool Integration
//
// The intake pipeline integrates with the work pool for async processing.
// Embedding generation is submitted as TypeEmbed work items, allowing the UI
// to show progress and the system to remain responsive.
//
// # Usage
//
//	pipeline := intake.NewPipeline(embedder, store, pool)
//	result := pipeline.Process(ctx, items)
//	// result.Unique contains deduplicated items
//	// result.Stored is the count of items saved
package intake

import (
	"context"
	"fmt"
	"time"

	"github.com/abelbrown/observer/internal/embedding"
	"github.com/abelbrown/observer/internal/logging"
	"github.com/abelbrown/observer/internal/model"
	"github.com/abelbrown/observer/internal/work"
)

// Pipeline processes fetched items through embed, dedup, and store stages.
type Pipeline struct {
	embedder   embedding.Embedder
	dedupIndex *embedding.DedupIndex
	store      *model.Store
	pool       *work.Pool

	// Configuration
	embedTimeout    time.Duration
	dedupThreshold  float32
	embedBatchSize  int
	skipDuplicates  bool // If true, duplicates are not stored
	intakePriority  int  // Priority for async work pool submissions
}

// Option configures the pipeline.
type Option func(*Pipeline)

// WithEmbedTimeout sets the timeout for embedding operations.
func WithEmbedTimeout(d time.Duration) Option {
	return func(p *Pipeline) {
		if d > 0 {
			p.embedTimeout = d
		}
	}
}

// WithDedupThreshold sets the similarity threshold for deduplication.
// Default is 0.85 (85% similar = duplicate).
func WithDedupThreshold(threshold float32) Option {
	return func(p *Pipeline) {
		if threshold > 0 && threshold <= 1.0 {
			p.dedupThreshold = threshold
		}
	}
}

// WithEmbedBatchSize sets the batch size for embedding operations.
func WithEmbedBatchSize(size int) Option {
	return func(p *Pipeline) {
		if size > 0 {
			p.embedBatchSize = size
		}
	}
}

// WithSkipDuplicates configures whether to skip storing duplicate items.
func WithSkipDuplicates(skip bool) Option {
	return func(p *Pipeline) {
		p.skipDuplicates = skip
	}
}

// WithIntakePriority sets the priority for async work pool submissions.
// Use work.PriorityLow, work.PriorityNormal, or work.PriorityHigh.
func WithIntakePriority(priority int) Option {
	return func(p *Pipeline) {
		p.intakePriority = priority
	}
}

// NewPipeline creates a new intake pipeline.
//
// If embedder is nil, embedding and dedup stages are skipped.
// If store is nil, items are not persisted.
// If pool is nil, operations run synchronously.
func NewPipeline(embedder embedding.Embedder, store *model.Store, pool *work.Pool, opts ...Option) *Pipeline {
	p := &Pipeline{
		embedder:       embedder,
		store:          store,
		pool:           pool,
		embedTimeout:   30 * time.Second,
		dedupThreshold: 0.85,
		embedBatchSize: 50,
		skipDuplicates: false,
		intakePriority: work.PriorityNormal,
	}

	// Apply options FIRST so dedupThreshold is set before creating the index
	for _, opt := range opts {
		opt(p)
	}

	// Create dedup index if embedder is available (uses configured threshold)
	if embedder != nil && embedder.Available() {
		p.dedupIndex = embedding.NewDedupIndex(embedder, float64(p.dedupThreshold))
	}

	return p
}

// Result contains the outcome of processing items through the pipeline.
type Result struct {
	// Input counts
	Total int // Total items received

	// Embedding results
	Embedded int // Items that got embeddings
	EmbedErr int // Items that failed embedding

	// Dedup results
	Unique     int           // Unique items after dedup
	Duplicates int           // Duplicate items found
	UniqueItems []model.Item // The actual unique items

	// Storage results
	Stored   int // Items successfully stored
	StoreErr int // Items that failed to store

	// Timing
	EmbedTime time.Duration
	DedupTime time.Duration
	StoreTime time.Duration
	TotalTime time.Duration

	// Errors
	Errors []error
}

// Process runs items through the intake pipeline.
//
// The pipeline stages are:
// 1. Embed: Generate embeddings for items
// 2. Dedup: Identify and mark duplicates
// 3. Store: Persist items and embeddings to database
//
// If context is cancelled, the pipeline stops and returns partial results.
func (p *Pipeline) Process(ctx context.Context, items []model.Item) Result {
	start := time.Now()
	result := Result{
		Total:       len(items),
		UniqueItems: items, // Start with all items
	}

	if len(items) == 0 {
		result.TotalTime = time.Since(start)
		return result
	}

	// Stage 1: Embed
	if p.embedder != nil && p.embedder.Available() {
		embedStart := time.Now()
		embedded, errors := p.embedItems(ctx, items)
		result.Embedded = embedded
		result.EmbedErr = len(errors)
		result.Errors = append(result.Errors, errors...)
		result.EmbedTime = time.Since(embedStart)

		logging.Info("Intake: embed stage complete",
			"total", len(items),
			"embedded", embedded,
			"errors", len(errors),
			"duration", result.EmbedTime)
	}

	// Stage 2: Dedup
	if p.dedupIndex != nil {
		if ctx.Err() != nil {
			result.TotalTime = time.Since(start)
			return result
		}

		dedupStart := time.Now()
		unique := p.dedupItems(ctx, items)
		result.Unique = len(unique)
		result.Duplicates = len(items) - len(unique)
		result.UniqueItems = unique
		result.DedupTime = time.Since(dedupStart)

		logging.Info("Intake: dedup stage complete",
			"total", len(items),
			"unique", len(unique),
			"duplicates", result.Duplicates,
			"duration", result.DedupTime)
	} else {
		result.Unique = len(items)
	}

	// Stage 3: Store
	if p.store != nil {
		if ctx.Err() != nil {
			result.TotalTime = time.Since(start)
			return result
		}

		storeStart := time.Now()
		itemsToStore := items
		if p.skipDuplicates {
			itemsToStore = result.UniqueItems
		}

		stored, err := p.storeItems(ctx, itemsToStore)
		result.Stored = stored
		if err != nil {
			result.StoreErr = len(itemsToStore) - stored
			result.Errors = append(result.Errors, err)
		}
		result.StoreTime = time.Since(storeStart)

		logging.Info("Intake: store stage complete",
			"stored", stored,
			"duration", result.StoreTime)
	}

	result.TotalTime = time.Since(start)
	return result
}

// ProcessAsync submits intake processing to the work pool.
// Returns a channel that receives the result when complete.
//
// The returned channel is guaranteed to be closed when processing completes.
// The work function owns the channel lifecycle - it sends the result and closes
// the channel, avoiding race conditions with coordination goroutines.
func (p *Pipeline) ProcessAsync(ctx context.Context, items []model.Item, source string) <-chan Result {
	resultCh := make(chan Result, 1)

	if p.pool == nil {
		// No pool, process synchronously
		go func() {
			defer close(resultCh)
			resultCh <- p.Process(ctx, items)
		}()
		return resultCh
	}

	// Work pool handles execution. Work function is responsible for result.
	p.pool.SubmitFuncWithPriority(work.TypeIntake, fmt.Sprintf("Intake %s (%d items)", source, len(items)), p.intakePriority, func() (string, error) {
		defer close(resultCh)
		result := p.Process(ctx, items)
		resultCh <- result
		return fmt.Sprintf("stored %d, unique %d of %d", result.Stored, result.Unique, result.Total), nil
	})

	return resultCh
}

// embedItems generates embeddings for items.
//
// NOTE: This function modifies the input slice in place, setting the Embedding
// field on each item that is successfully embedded. Callers should be aware
// that the original items slice will be mutated.
func (p *Pipeline) embedItems(ctx context.Context, items []model.Item) (int, []error) {
	if len(items) == 0 {
		return 0, nil
	}

	ctx, cancel := context.WithTimeout(ctx, p.embedTimeout)
	defer cancel()

	// Extract texts for embedding
	texts := make([]string, len(items))
	for i, item := range items {
		// Use title for embedding (truncate to reasonable length)
		text := item.Title
		if len(text) > 200 {
			text = text[:200]
		}
		texts[i] = text
	}

	// Batch embed
	vectors, err := p.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, []error{fmt.Errorf("batch embed failed: %w", err)}
	}

	// Convert vectors from float64 to float32 and attach to items
	var errors []error
	embedded := 0
	for i, vec := range vectors {
		if vec == nil {
			errors = append(errors, fmt.Errorf("nil embedding for item %s", items[i].ID))
			continue
		}
		// Convert float64 to float32
		items[i].Embedding = make([]float32, len(vec))
		for j, v := range vec {
			items[i].Embedding[j] = float32(v)
		}
		embedded++
	}

	return embedded, errors
}

// dedupItems identifies duplicates and returns unique items.
func (p *Pipeline) dedupItems(ctx context.Context, items []model.Item) []model.Item {
	if len(items) == 0 {
		return items
	}

	// Index items and update their embeddings
	p.dedupIndex.IndexModelBatch(ctx, items)

	// Get primary items only
	return p.dedupIndex.GetPrimaryModelItems(items)
}

// storeItems saves items and their embeddings to the database.
func (p *Pipeline) storeItems(ctx context.Context, items []model.Item) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	// Save items
	stored, err := p.store.SaveItems(items)
	if err != nil {
		return stored, fmt.Errorf("save items failed: %w", err)
	}

	// Save embeddings for items that have them
	embeddings := make(map[string][]float32)
	for _, item := range items {
		if item.Embedding != nil {
			embeddings[item.ID] = item.Embedding
		}
	}

	if len(embeddings) > 0 {
		if err := p.store.SaveEmbeddings(embeddings); err != nil {
			return stored, fmt.Errorf("save embeddings failed: %w", err)
		}
	}

	return stored, nil
}

// Stats returns current pipeline statistics.
type Stats struct {
	EmbedderAvailable bool
	DedupIndexed      int
	DedupGroups       int
	DedupDuplicates   int
}

// Stats returns current pipeline statistics.
func (p *Pipeline) Stats() Stats {
	s := Stats{}

	if p.embedder != nil {
		s.EmbedderAvailable = p.embedder.Available()
	}

	if p.dedupIndex != nil {
		s.DedupIndexed, s.DedupGroups, s.DedupDuplicates = p.dedupIndex.Stats()
	}

	return s
}
