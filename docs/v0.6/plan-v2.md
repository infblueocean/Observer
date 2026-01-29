# Observer v0.6 Implementation Plan: Embeddings and Semantic Features (v2)

## Executive Summary

v0.6 adds semantic features using local embeddings via Ollama. The core principle is **graceful degradation** - everything must work without Ollama, with embeddings providing enhanced functionality when available.

**Key Components:**
1. Embedder Interface + OllamaEmbedder - Local embedding generation via Ollama
2. Embedding Storage - SQLite persistence for embeddings
3. Background Embedding Worker - Async embedding of new items
4. Semantic Dedup Filter - Cosine similarity duplicate detection

---

## Phase Overview

| Phase | Name | Key Deliverables |
|-------|------|------------------|
| 1 | Embedder + Storage | embed.Embedder interface, OllamaEmbedder, schema migration, Store methods |
| 2 | Worker + SemanticDedup | Background embedding after fetch, cosine similarity filter |

---

## Phase 1: Embedder + Storage (Foundation)

**Goal:** Define embedding interface, implement Ollama client, and persist embeddings in SQLite.

### 1.1 Embedder Interface

**File:** `internal/embed/embed.go`

```go
package embed

import "context"

// Embedder generates vector embeddings from text.
type Embedder interface {
    // Available returns true if the embedding service is running and ready.
    Available() bool

    // Embed generates a float32 embedding for the given text.
    // Returns error if service is unavailable or request fails.
    Embed(ctx context.Context, text string) ([]float32, error)
}

// CosineSimilarity computes similarity between two embeddings.
// Returns 1.0 for identical, 0.0 for orthogonal, -1.0 for opposite.
func CosineSimilarity(a, b []float32) float32 {
    if len(a) != len(b) || len(a) == 0 {
        return 0
    }

    var dot, normA, normB float32
    for i := range a {
        dot += a[i] * b[i]
        normA += a[i] * a[i]
        normB += b[i] * b[i]
    }

    if normA == 0 || normB == 0 {
        return 0
    }

    return dot / (sqrt(normA) * sqrt(normB))
}
```

### 1.2 OllamaEmbedder

**File:** `internal/embed/ollama.go`

```go
package embed

// OllamaEmbedder generates embeddings via local Ollama server.
// Implements embed.Embedder.
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
```

### 1.3 Ollama API

```json
// POST /api/embed
// Request:
{"model": "nomic-embed-text", "input": "text to embed"}

// Response:
{"embeddings": [[0.123, -0.456, ...]]}
```

### 1.4 Schema Migration with Versioning

```go
// In store initialization, use pragma_table_info to check before ALTER
func (s *Store) migrateEmbeddings() error {
    // Check if column exists
    var count int
    err := s.db.QueryRow(`
        SELECT COUNT(*) FROM pragma_table_info('items')
        WHERE name = 'embedding'
    `).Scan(&count)
    if err != nil {
        return err
    }

    if count == 0 {
        // Column doesn't exist, add it
        _, err = s.db.Exec(`ALTER TABLE items ADD COLUMN embedding BLOB DEFAULT NULL`)
        if err != nil {
            return err
        }
    }

    // Create index (IF NOT EXISTS is idempotent)
    _, err = s.db.Exec(`
        CREATE INDEX IF NOT EXISTS idx_items_no_embedding
        ON items(id) WHERE embedding IS NULL
    `)
    return err
}
```

### 1.5 New Store Methods

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

### 1.6 Binary Encoding

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

### 1.7 Testing Strategy

```go
// embed/embed_test.go
func TestCosineSimilarityIdentical(t *testing.T)     // Same vectors -> 1.0
func TestCosineSimilarityOrthogonal(t *testing.T)    // Perpendicular -> 0.0
func TestCosineSimilarityZeroVector(t *testing.T)    // Zero vector -> 0.0
func TestCosineSimilarityDifferentLengths(t *testing.T) // Mismatch -> 0.0

// embed/ollama_test.go (use httptest.Server to mock Ollama)
func TestAvailable(t *testing.T)           // Mock returns model list
func TestNotAvailable(t *testing.T)        // Mock returns empty/error
func TestEmbed(t *testing.T)               // Mock returns embedding
func TestEmbedTimeout(t *testing.T)        // Context timeout

// store/embedding_test.go
func TestSaveEmbedding(t *testing.T)            // Save and retrieve
func TestGetItemsNeedingEmbedding(t *testing.T) // Returns unembedded
func TestGetItemsWithEmbeddings(t *testing.T)   // Batch retrieval
func TestEmbeddingRoundTrip(t *testing.T)       // Encode/decode
func TestMigrationIdempotent(t *testing.T)      // Run twice, no error
```

### 1.8 Phase 1 Success Criteria

- [ ] embed.Embedder interface defined in internal/embed/embed.go
- [ ] CosineSimilarity function in embed package
- [ ] OllamaEmbedder implements embed.Embedder
- [ ] Available() correctly detects Ollama presence
- [ ] Embed() returns []float32 vectors
- [ ] Graceful error when Ollama unavailable
- [ ] Context timeout respected
- [ ] Migration checks column existence before ALTER TABLE
- [ ] SaveEmbedding stores binary blob
- [ ] GetEmbedding returns correct []float32
- [ ] GetItemsNeedingEmbedding returns oldest unembedded
- [ ] All tests pass with `-race`

---

## Phase 2: Worker + SemanticDedup (Feature)

**Goal:** Embed new items after fetch and use embeddings for smarter duplicate detection.

### 2.1 Coordinator Extension

```go
type Coordinator struct {
    store    *store.Store
    fetcher  fetcher
    embedder embed.Embedder  // NEW: optional embedder (nil to disable)
    sources  []fetch.Source
    wg       sync.WaitGroup
}

// NewCoordinator now accepts optional embedder (nil to disable)
func NewCoordinator(s *store.Store, f *fetch.Fetcher, e embed.Embedder, sources []fetch.Source) *Coordinator
```

### 2.2 Embedding Worker Logic

Simple implementation: embed items one by one. No premature optimization.

```go
func (c *Coordinator) embedNewItems(ctx context.Context) {
    if c.embedder == nil || !c.embedder.Available() {
        return
    }

    items, err := c.store.GetItemsNeedingEmbedding(100)
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
            // Log and continue - don't stop the whole process
            continue
        }

        c.store.SaveEmbedding(item.ID, embedding)
    }
}
```

### 2.3 Integration Point

```go
func (c *Coordinator) fetchAll(ctx context.Context, program *tea.Program) {
    // ... existing fetch logic ...

    // After all fetches complete, embed new items
    c.embedNewItems(ctx)
}
```

### 2.4 Semantic Dedup Filter

**File:** `internal/filter/semantic.go`

```go
// SemanticDedup removes semantically similar items.
// Uses cosine similarity with threshold (e.g., 0.85).
// Falls back to URL dedup if embeddings unavailable.
func SemanticDedup(items []store.Item, embeddings map[string][]float32, threshold float32) []store.Item {
    if len(items) == 0 {
        return []store.Item{}
    }

    seenURLs := make(map[string]bool)
    var seenEmbeddings [][]float32

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

### 2.5 Integration in main.go

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

### 2.6 Performance Note

O(N^2) for N items. Acceptable for <10k items:
- 1000 items: ~500k comparisons < 50ms
- 5000 items: ~12.5M comparisons < 500ms

Add batching or HNSW in future version if profiling shows need.

### 2.7 Testing Strategy

```go
// coord/embed_test.go
type mockEmbedder struct {
    available  bool
    embedFunc  func(ctx context.Context, text string) ([]float32, error)
}

func TestCoordinatorEmbedsAfterFetch(t *testing.T)
func TestCoordinatorSkipsWhenUnavailable(t *testing.T)
func TestCoordinatorRespectsCancellation(t *testing.T)
func TestCoordinatorHandlesEmbedErrors(t *testing.T)
func TestCoordinatorStopsWhenOllamaDisappears(t *testing.T)

// filter/semantic_test.go
func TestSemanticDedupWithEmbeddings(t *testing.T)
func TestSemanticDedupWithoutEmbeddings(t *testing.T)  // Falls back to URL
func TestSemanticDedupMixed(t *testing.T)              // Some have, some don't
func TestSemanticDedupThreshold(t *testing.T)
func TestSemanticDedupPreservesOrder(t *testing.T)
```

### 2.8 Phase 2 Success Criteria

- [ ] Embedding runs after fetch when Ollama available
- [ ] Embedding skipped when Ollama unavailable
- [ ] Embedding stops if Ollama disappears mid-process
- [ ] Context cancellation stops embedding promptly
- [ ] Errors don't stop the process
- [ ] SemanticDedup removes similar items
- [ ] Falls back to URL dedup when no embeddings
- [ ] Threshold controls sensitivity
- [ ] First occurrence preserved
- [ ] All tests pass with `-race`

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

## Deferred to Future Versions

| Feature | Reason | When to Add |
|---------|--------|-------------|
| Parallel fetch (errgroup) | Premature optimization | v0.7 if fetch latency becomes a problem |
| Batch embedding | Premature optimization | When profiling shows Ollama is bottleneck |
| Rate limiting | Premature optimization | When Ollama actually chokes |
| HNSW indexing | O(N^2) is fine for <10k | When item count exceeds 10k |
| Dimension validation | Single model assumption | When supporting multiple models |

---

## Success Criteria (Full v0.6)

- [ ] embed.Embedder interface in internal/embed/embed.go
- [ ] OllamaEmbedder implements embed.Embedder
- [ ] CosineSimilarity in embed package
- [ ] Migration checks column existence before ALTER
- [ ] Embeddings stored in SQLite
- [ ] Background worker embeds new items
- [ ] Worker re-checks Ollama availability during processing
- [ ] SemanticDedup uses embeddings when available
- [ ] Everything works without Ollama
- [ ] All tests pass with `-race`
