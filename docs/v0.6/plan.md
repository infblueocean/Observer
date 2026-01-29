# Observer v0.6 Implementation Plan: Embeddings and Semantic Features

## Executive Summary

v0.6 adds semantic features using local embeddings via Ollama. The core principle is **graceful degradation** - everything must work without Ollama, with embeddings providing enhanced functionality when available.

**Key Components:**
1. OllamaEmbedder - Local embedding generation via Ollama
2. Embedding Storage - SQLite persistence for embeddings
3. Background Embedding Worker - Async embedding of new items
4. Semantic Dedup Filter - Cosine similarity duplicate detection
5. Parallel Fetch - errgroup for faster updates

---

## Phase Overview

| Phase | Name | Key Deliverables |
|-------|------|------------------|
| 1 | OllamaEmbedder | Ollama client, availability check, Embed() |
| 2 | Embedding Storage | Schema migration, Save/Get embedding methods |
| 3 | Background Worker | Embed new items after fetch |
| 4 | Semantic Dedup | Cosine similarity filter |
| 5 | Parallel Fetch | errgroup for concurrent fetching |

---

## Phase 1: OllamaEmbedder (Foundation)

**Goal:** Connect to local Ollama server and generate embeddings.

### 1.1 Interface and Types

**File:** `internal/embed/ollama.go`

```go
package embed

// OllamaEmbedder generates embeddings via local Ollama server.
type OllamaEmbedder struct {
    endpoint string        // Default: "http://localhost:11434"
    model    string        // Default: "nomic-embed-text"
    client   *http.Client
}

// NewOllamaEmbedder creates an embedder.
func NewOllamaEmbedder(endpoint, model string) *OllamaEmbedder

// Available returns true if Ollama is running and the model is available.
func (e *OllamaEmbedder) Available() bool

// Embed generates a float32 embedding for the given text.
// Returns error if Ollama is unavailable or request fails.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error)

// Dimension returns the embedding dimension (e.g., 768 for nomic-embed-text).
func (e *OllamaEmbedder) Dimension() int

// CosineSimilarity computes similarity between two embeddings.
// Returns 1.0 for identical, 0.0 for orthogonal.
func CosineSimilarity(a, b []float32) float32
```

### 1.2 Ollama API

```json
// POST /api/embed
// Request:
{"model": "nomic-embed-text", "input": "text to embed"}

// Response:
{"embeddings": [[0.123, -0.456, ...]]}
```

### 1.3 Testing Strategy

```go
// Use httptest.Server to mock Ollama
func TestAvailable(t *testing.T)           // Mock returns model list
func TestNotAvailable(t *testing.T)        // Mock returns empty/error
func TestEmbed(t *testing.T)               // Mock returns embedding
func TestEmbedTimeout(t *testing.T)        // Context timeout
func TestCosineSimilarity(t *testing.T)    // Pure math
func TestCosineSimilarityEdgeCases(t *testing.T) // Zero vectors, etc.
```

### 1.4 Phase 1 Success Criteria

- [ ] OllamaEmbedder connects to Ollama
- [ ] Available() correctly detects Ollama presence
- [ ] Embed() returns []float32 vectors
- [ ] Graceful error when Ollama unavailable
- [ ] Context timeout respected
- [ ] All tests pass (no real Ollama needed)

---

## Phase 2: Embedding Storage

**Goal:** Persist embeddings in SQLite alongside items.

### 2.1 Schema Migration

```sql
-- Add embedding column (idempotent migration)
ALTER TABLE items ADD COLUMN embedding BLOB DEFAULT NULL;

-- Index for finding items without embeddings
CREATE INDEX IF NOT EXISTS idx_items_no_embedding
    ON items(id) WHERE embedding IS NULL;
```

### 2.2 New Store Methods

```go
// SaveEmbedding stores an embedding for an item.
// Embedding encoded as binary (4 bytes per float32, little-endian).
func (s *Store) SaveEmbedding(id string, embedding []float32) error

// GetItemsNeedingEmbedding returns items with NULL embedding.
// Returns oldest items first (by fetched_at) up to limit.
func (s *Store) GetItemsNeedingEmbedding(limit int) ([]Item, error)

// GetEmbedding returns the embedding for an item, or nil if not set.
func (s *Store) GetEmbedding(id string) ([]float32, error)

// GetItemsWithEmbeddings returns embeddings for given item IDs.
// Items without embeddings are omitted from result.
func (s *Store) GetItemsWithEmbeddings(ids []string) (map[string][]float32, error)
```

### 2.3 Binary Encoding

```go
func encodeEmbedding(embedding []float32) []byte {
    buf := make([]byte, len(embedding)*4)
    for i, f := range embedding {
        binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
    }
    return buf
}

func decodeEmbedding(data []byte) []float32 {
    if len(data)%4 != 0 {
        return nil
    }
    embedding := make([]float32, len(data)/4)
    for i := range embedding {
        embedding[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
    }
    return embedding
}
```

### 2.4 Testing Strategy

```go
func TestSaveEmbedding(t *testing.T)            // Save and retrieve
func TestGetItemsNeedingEmbedding(t *testing.T) // Returns unembedded
func TestGetItemsWithEmbeddings(t *testing.T)   // Batch retrieval
func TestEmbeddingRoundTrip(t *testing.T)       // Encode/decode
func TestSchemaMigration(t *testing.T)          // Migration idempotent
```

### 2.5 Phase 2 Success Criteria

- [ ] Schema migration adds embedding column
- [ ] SaveEmbedding stores binary blob
- [ ] GetEmbedding returns correct []float32
- [ ] GetItemsNeedingEmbedding returns oldest unembedded
- [ ] Binary encoding round-trips correctly
- [ ] All tests pass with `-race`

---

## Phase 3: Background Embedding Worker

**Goal:** Embed new items after fetch completes.

### 3.1 Coordinator Extension

```go
type Coordinator struct {
    store    *store.Store
    fetcher  fetcher
    embedder embedder  // NEW: optional embedder
    sources  []fetch.Source
    wg       sync.WaitGroup
}

// embedder interface for dependency injection
type embedder interface {
    Available() bool
    Embed(ctx context.Context, text string) ([]float32, error)
}

// NewCoordinator now accepts optional embedder (nil to disable)
func NewCoordinator(s *store.Store, f *fetch.Fetcher, e embedder, sources []fetch.Source) *Coordinator
```

### 3.2 Embedding Worker Logic

```go
const (
    embedBatchSize = 10
    embedDelay     = 100 * time.Millisecond
    embedTimeout   = 10 * time.Second
)

func (c *Coordinator) embedNewItems(ctx context.Context, program *tea.Program) {
    if c.embedder == nil || !c.embedder.Available() {
        return
    }

    for {
        if ctx.Err() != nil {
            return
        }

        items, _ := c.store.GetItemsNeedingEmbedding(embedBatchSize)
        if len(items) == 0 {
            return
        }

        for _, item := range items {
            if ctx.Err() != nil {
                return
            }

            text := item.Title
            if item.Summary != "" {
                text += " " + item.Summary
            }

            embedCtx, cancel := context.WithTimeout(ctx, embedTimeout)
            embedding, err := c.embedder.Embed(embedCtx, text)
            cancel()

            if err == nil {
                c.store.SaveEmbedding(item.ID, embedding)
            }

            time.Sleep(embedDelay) // Rate limit
        }
    }
}
```

### 3.3 Integration Point

```go
func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
    // ... existing fetch logic ...

    // After all fetches complete, embed new items
    c.embedNewItems(ctx, program)
}
```

### 3.4 Testing Strategy

```go
type mockEmbedder struct {
    available  bool
    embedFunc  func(ctx context.Context, text string) ([]float32, error)
}

func TestCoordinatorEmbedsAfterFetch(t *testing.T)
func TestCoordinatorSkipsWhenUnavailable(t *testing.T)
func TestCoordinatorRespectsCancellation(t *testing.T)
func TestCoordinatorHandlesEmbedErrors(t *testing.T)
```

### 3.5 Phase 3 Success Criteria

- [ ] Embedding runs after fetch when Ollama available
- [ ] Embedding skipped when Ollama unavailable
- [ ] Context cancellation stops embedding promptly
- [ ] Rate limiting prevents Ollama overload
- [ ] Errors don't stop the process
- [ ] All tests pass with `-race`

---

## Phase 4: Semantic Dedup Filter

**Goal:** Use embeddings for smarter duplicate detection.

### 4.1 Filter Function

```go
// SemanticDedup removes semantically similar items.
// Uses cosine similarity with threshold (e.g., 0.85).
// Falls back to URL dedup if embeddings unavailable.
func SemanticDedup(items []store.Item, embeddings map[string][]float32, threshold float32) []store.Item
```

### 4.2 Implementation

```go
func SemanticDedup(items []store.Item, embeddings map[string][]float32, threshold float32) []store.Item {
    if len(items) == 0 {
        return []store.Item{}
    }

    seenURLs := make(map[string]bool)
    var seenEmbeddings [][]float32
    var seenIDs []string

    result := make([]store.Item, 0, len(items))

    for _, item := range items {
        // URL dedup (always)
        if item.URL != "" && seenURLs[item.URL] {
            continue
        }

        // Semantic dedup (if embedding available)
        if emb, ok := embeddings[item.ID]; ok {
            isDup := false
            for _, seen := range seenEmbeddings {
                if embed.CosineSimilarity(emb, seen) > threshold {
                    isDup = true
                    break
                }
            }
            if isDup {
                continue
            }
            seenEmbeddings = append(seenEmbeddings, emb)
        }

        if item.URL != "" {
            seenURLs[item.URL] = true
        }
        result = append(result, item)
    }

    return result
}
```

### 4.3 Integration in main.go

```go
// In loadItems closure
items = filter.ByAge(items, 24*time.Hour)

// Get embeddings for semantic dedup
ids := make([]string, len(items))
for i, item := range items {
    ids[i] = item.ID
}
embeddings, _ := st.GetItemsWithEmbeddings(ids)

items = filter.SemanticDedup(items, embeddings, 0.85)
items = filter.LimitPerSource(items, 20)
```

### 4.4 Performance Note

O(N^2) for N items. Acceptable for <10k items:
- 1000 items: ~500k comparisons < 50ms
- 5000 items: ~12.5M comparisons < 500ms

HNSW deferred to v0.7 if needed.

### 4.5 Testing Strategy

```go
func TestSemanticDedupWithEmbeddings(t *testing.T)
func TestSemanticDedupWithoutEmbeddings(t *testing.T)  // Falls back to URL
func TestSemanticDedupMixed(t *testing.T)              // Some have, some don't
func TestSemanticDedupThreshold(t *testing.T)
func TestSemanticDedupPreservesOrder(t *testing.T)
```

### 4.6 Phase 4 Success Criteria

- [ ] SemanticDedup removes similar items
- [ ] Falls back to URL dedup when no embeddings
- [ ] Threshold controls sensitivity
- [ ] First occurrence preserved
- [ ] Performance acceptable for 5000 items
- [ ] All tests pass

---

## Phase 5: Parallel Fetch with errgroup

**Goal:** Fetch sources in parallel for faster updates.

### 5.1 Update Coordinator

```go
import "golang.org/x/sync/errgroup"

const maxConcurrentFetches = 5

func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
    g, gCtx := errgroup.WithContext(ctx)
    g.SetLimit(maxConcurrentFetches)

    results := make(chan fetchResult, len(c.sources))

    for _, src := range c.sources {
        src := src
        g.Go(func() error {
            fetchCtx, cancel := context.WithTimeout(gCtx, fetchTimeout)
            defer cancel()

            items, err := c.fetcher.Fetch(fetchCtx, src)
            results <- fetchResult{src: src, items: items, err: err}
            return nil
        })
    }

    go func() {
        g.Wait()
        close(results)
    }()

    for res := range results {
        // Process results...
    }

    // Then embed
    c.embedNewItems(ctx, program)
}
```

### 5.2 Dependencies

```
require golang.org/x/sync v0.10.0
```

### 5.3 Phase 5 Success Criteria

- [ ] Sources fetched in parallel (up to 5 concurrent)
- [ ] Context cancellation stops all fetches
- [ ] No race conditions
- [ ] Tests pass with `-race`

---

## Graceful Degradation Matrix

| Component | Ollama Available | Ollama Unavailable |
|-----------|------------------|-------------------|
| OllamaEmbedder | Returns embeddings | Returns error |
| Coordinator | Embeds new items | Skips embedding |
| SemanticDedup | Uses cosine similarity | Falls back to URL dedup |

---

## Concurrency Summary

| Structure | Protection | Notes |
|-----------|------------|-------|
| Store.db | sync.RWMutex | Existing |
| OllamaEmbedder | Stateless | No internal state |
| embeddings map | Per-request | Created fresh each load |
| Coordinator | Context + WaitGroup | Existing pattern |

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Ollama slow | 10s timeout per embed, rate limiting |
| DB bloat | ~3KB per item acceptable |
| O(N^2) similarity | Acceptable <10k, HNSW later |
| Parallel fetch races | errgroup + channel |

---

## Success Criteria (Full v0.6)

- [ ] OllamaEmbedder connects to local Ollama
- [ ] Embeddings stored in SQLite
- [ ] Background worker embeds new items
- [ ] SemanticDedup uses embeddings when available
- [ ] Everything works without Ollama
- [ ] Parallel fetch with errgroup
- [ ] All tests pass with `-race`
