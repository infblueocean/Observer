package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/infblueocean/clarion"

	"github.com/abelbrown/observer/internal/coord"
	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/fetch"
	"github.com/abelbrown/observer/internal/filter"
	"github.com/abelbrown/observer/internal/otel"
	"github.com/abelbrown/observer/internal/rerank"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui"
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type e2eEmbedder struct{}

func (e e2eEmbedder) Available() bool { return true }

func (e e2eEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return e.embedText(text), nil
}

func (e e2eEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return e.embedText(text), nil
}

func (e e2eEmbedder) embedText(text string) []float32 {
	var sum int
	for i := 0; i < len(text); i++ {
		sum += int(text[i])
	}
	base := float32(sum%97) / 97.0
	return []float32{base + 0.1, base + 0.2, base + 0.3}
}

type noopProvider struct{}

func (n noopProvider) Fetch(ctx context.Context) ([]store.Item, error) {
	return nil, nil
}

func main() {
	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())

	// Data directory: ~/.observer/
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	dataDir := filepath.Join(homeDir, ".observer")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}
	dbPath := filepath.Join(dataDir, "observer.db")

	// Open store
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer st.Close()

	// Structured event log (JSONL) — separate from Bubble Tea's log output
	eventLogPath := filepath.Join(dataDir, "observer.events.jsonl")
	eventFile, err := os.OpenFile(eventLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatalf("Failed to open event log: %v", err)
	}
	defer eventFile.Close()

	logger := otel.NewLogger(eventFile)
	defer logger.Close()

	ring := otel.NewRingBuffer(otel.DefaultRingSize)
	logger.SetRingBuffer(ring)

	logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelInfo, Comp: "main", Msg: "observer starting"})

	// Backend selection: Jina when JINA_API_KEY is set, otherwise no AI backend.
	// Ollama can be enabled explicitly via OLLAMA_HOST.
	e2eMode := os.Getenv("OBSERVER_E2E") != ""
	jinaKey := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	embedModel := envOrDefault("JINA_EMBED_MODEL", "jina-embeddings-v3")
	rerankModel := envOrDefault("JINA_RERANK_MODEL", "jina-reranker-v3")

	var embedder embed.Embedder
	var reranker rerank.Reranker

	switch {
	case e2eMode:
		embedder = e2eEmbedder{}
		logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelInfo, Comp: "main", Msg: "using e2e mock embedder"})
	case jinaKey != "":
		embedder = embed.NewJinaEmbedder(jinaKey, embedModel)
		reranker = rerank.NewJinaReranker(jinaKey, rerankModel)
		logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelInfo, Comp: "main", Msg: "using Jina API backend"})
	case os.Getenv("OLLAMA_HOST") != "":
		ollamaEndpoint := os.Getenv("OLLAMA_HOST")
		ollamaEmbedModel := envOrDefault("OLLAMA_EMBED_MODEL", "mxbai-embed-large")
		ollamaRerankModel := os.Getenv("OLLAMA_RERANK_MODEL")
		embedder = embed.NewOllamaEmbedder(ollamaEndpoint, ollamaEmbedModel)
		reranker = rerank.NewOllamaReranker(ollamaEndpoint, ollamaRerankModel)
		logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelInfo, Comp: "main", Msg: "using Ollama backend"})
	default:
		logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelWarn, Comp: "main", Msg: "no AI backend (set JINA_API_KEY or OLLAMA_HOST)"})
	}

	// Create provider using Clarion
	provider := fetch.NewClarionProvider(nil, clarion.FetchOptions{
		MaxConcurrency: 10,
		Timeout:        30 * time.Second,
		MaxItems:       50,
	}, logger)

	// Create UI app with dependency injection
	cfg := ui.AppConfig{
		// LoadRecentItems: Stage 1 — fast first paint (last 1h, unread only)
		LoadRecentItems: func() tea.Cmd {
			return func() tea.Msg {
				since := time.Now().Add(-1 * time.Hour)
				items, err := st.GetItemsSince(since)
				if err != nil {
					return ui.ItemsLoaded{Err: err}
				}

				// Filter out read items (GetItemsSince has no read filter)
				unread := items[:0]
				for _, item := range items {
					if !item.Read {
						unread = append(unread, item)
					}
				}
				items = unread

				// Same filter pipeline as LoadItems
				ids := make([]string, len(items))
				for i, item := range items {
					ids[i] = item.ID
				}
				embeddings, err := st.GetItemsWithEmbeddings(ids)
				if err != nil {
					logger.Emit(otel.Event{Kind: otel.KindStoreError, Level: otel.LevelWarn, Comp: "main", Msg: "failed to get embeddings (recent)", Err: err.Error()})
					embeddings = make(map[string][]float32)
				}

				items = filter.SemanticDedup(items, embeddings, 0.85)
				items = filter.LimitPerSource(items, 50)

				filteredEmbeddings := make(map[string][]float32)
				for _, item := range items {
					if emb, ok := embeddings[item.ID]; ok {
						filteredEmbeddings[item.ID] = emb
					}
				}

				return ui.ItemsLoaded{Items: items, Embeddings: filteredEmbeddings}
			}
		},
		// LoadItems: Stage 2 — full 24h corpus (also used by refresh/fetch)
		LoadItems: func() tea.Cmd {
			return func() tea.Msg {
				items, err := st.GetItems(10000, false)
				if err != nil {
					return ui.ItemsLoaded{Err: err}
				}
				items = filter.ByAge(items, 24*time.Hour)

				// Get embeddings for semantic dedup and search
				ids := make([]string, len(items))
				for i, item := range items {
					ids[i] = item.ID
				}
				embeddings, err := st.GetItemsWithEmbeddings(ids)
				if err != nil {
					// Log but continue - semantic dedup will fall back to URL dedup
					logger.Emit(otel.Event{Kind: otel.KindStoreError, Level: otel.LevelWarn, Comp: "main", Msg: "failed to get embeddings (full)", Err: err.Error()})
					embeddings = make(map[string][]float32)
				}

				// Use semantic dedup (falls back to URL dedup if no embeddings)
				// Threshold 0.85 means items with >85% cosine similarity are considered duplicates
				items = filter.SemanticDedup(items, embeddings, 0.85)
				items = filter.LimitPerSource(items, 50)

				// Rebuild embeddings map for filtered items only
				filteredEmbeddings := make(map[string][]float32)
				for _, item := range items {
					if emb, ok := embeddings[item.ID]; ok {
						filteredEmbeddings[item.ID] = emb
					}
				}

				return ui.ItemsLoaded{Items: items, Embeddings: filteredEmbeddings}
			}
		},
		// LoadSearchPool: load all items for full-history search
		LoadSearchPool: func(ctx context.Context, queryID string) tea.Cmd {
			return func() tea.Msg {
				if err := ctx.Err(); err != nil {
					return ui.SearchPoolLoaded{Err: err, QueryID: queryID}
				}
				items, err := st.GetItems(10000, true) // include read items
				if err != nil {
					return ui.SearchPoolLoaded{Err: err, QueryID: queryID}
				}
				// No age filter, no LimitPerSource — search needs everything
				ids := make([]string, len(items))
				for i, item := range items {
					ids[i] = item.ID
				}
				embeddings, err := st.GetItemsWithEmbeddings(ids)
				if err != nil {
					logger.Emit(otel.Event{Kind: otel.KindStoreError, Level: otel.LevelWarn, Comp: "main", Msg: "failed to get embeddings (search pool)", Err: err.Error()})
					embeddings = make(map[string][]float32)
				}
				// No dedup for search: SemanticDedup is O(n^2) and dominates latency
				// for large pools. The reranker handles relevance; dupes cluster naturally.
				return ui.SearchPoolLoaded{Items: items, Embeddings: embeddings, QueryID: queryID}
			}
		},
		// markRead
		MarkRead: func(id string) tea.Cmd {
			return func() tea.Msg {
				if err := st.MarkRead(id); err != nil {
					logger.Error(otel.KindStoreError, "main", err)
				}
				return ui.ItemMarkedRead{ID: id}
			}
		},
		// triggerFetch (manual refresh triggers reload after fetch)
		TriggerFetch: func() tea.Cmd {
			return func() tea.Msg {
				return ui.RefreshTick{}
			}
		},
		// SearchFTS: instant lexical search (synchronous)
		SearchFTS: func(query string, limit int) ([]store.Item, error) {
			return st.SearchFTS(query, limit)
		},
		Obs: ui.ObsConfig{
			Logger: logger,
			Ring:   ring,
		},
		Features: ui.Features{
			MLT:  true,
			FTS5: true,
		},
	}

	// Wire embedding closures only when an AI backend is available.
	// Interactive search gets its own embedder — no rate limiter,
	// no contention with the background embedding worker.
	if embedder != nil {
		if e2eMode || jinaKey == "" {
			cfg.EmbedQuery = func(ctx context.Context, query string, queryID string) tea.Cmd {
				return func() tea.Msg {
					emb, err := embed.EmbedQuery(ctx, embedder, query)
					return ui.QueryEmbedded{Query: query, Embedding: emb, Err: err, QueryID: queryID}
				}
			}
		} else {
			queryEmbedder := embed.NewJinaEmbedder(jinaKey, embedModel)
			queryEmbedder.SetRateLimit(0) // no throttle for interactive queries
			cfg.EmbedQuery = func(ctx context.Context, query string, queryID string) tea.Cmd {
				return func() tea.Msg {
					emb, err := embed.EmbedQuery(ctx, queryEmbedder, query)
					return ui.QueryEmbedded{Query: query, Embedding: emb, Err: err, QueryID: queryID}
				}
			}
		}
	}

	// Wire reranker based on backend type.
	// Jina: fast batch API → auto-rerank after every search.
	// Ollama: slow per-item scoring → user presses R to opt in.
	autoReranks := false
	if ar, ok := reranker.(rerank.AutoReranker); ok {
		autoReranks = ar.AutoReranks()
	}
	cfg.AutoReranks = autoReranks

	if autoReranks {
		// Batch path: single API call for all docs (Jina).
		// 15s timeout prevents indefinite hangs from retries/rate limiting.
		cfg.BatchRerank = func(ctx context.Context, query string, docs []string, queryID string) tea.Cmd {
			return func() tea.Msg {
				rerankCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				scores, err := reranker.Rerank(rerankCtx, query, docs)
				if err != nil {
					return ui.RerankComplete{Query: query, Err: err, QueryID: queryID}
				}
				result := make([]float32, len(docs))
				for _, s := range scores {
					if s.Index < len(result) {
						result[s.Index] = s.Score
					}
				}
				return ui.RerankComplete{Query: query, Scores: result, QueryID: queryID}
			}
		}
		logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelInfo, Comp: "main", Msg: "reranker: auto (batch)", Extra: map[string]any{"name": reranker.Name()}})
	} else if reranker != nil && reranker.Available() {
		// Per-entry path: one HTTP call per document (Ollama)
		cfg.ScoreEntry = func(ctx context.Context, query string, doc string, itemID string, queryID string) tea.Cmd {
			return func() tea.Msg {
				if err := ctx.Err(); err != nil {
					return ui.EntryReranked{ItemID: itemID, Score: 0, QueryID: queryID, Err: err}
				}
				score, err := reranker.Rerank(ctx, query, []string{doc})
				if err != nil || len(score) == 0 {
					return ui.EntryReranked{ItemID: itemID, Score: 0.5, QueryID: queryID, Err: err}
				}
				return ui.EntryReranked{ItemID: itemID, Score: score[0].Score, QueryID: queryID}
			}
		}
		logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelInfo, Comp: "main", Msg: "reranker: manual (per-entry)", Extra: map[string]any{"name": reranker.Name()}})
	} else {
		logger.Emit(otel.Event{Kind: otel.KindStartup, Level: otel.LevelWarn, Comp: "main", Msg: "no reranker available — cosine only"})
	}

	app := ui.NewAppWithConfig(cfg)

	// Redirect log output to file so it doesn't corrupt the TUI
	logPath := filepath.Join(dataDir, "observer.log")
	if f, err := tea.LogToFile(logPath, "observer"); err == nil {
		defer f.Close()
	}

	// Create program
	program := tea.NewProgram(app, tea.WithAltScreen())

	// Create and start coordinator
	coordinator := coord.NewCoordinator(st, provider, embedder, logger)
	coordinator.Start(ctx, program)

	// Start background embedding worker (continuously embeds items without embeddings)
	coordinator.StartEmbeddingWorker(ctx)

	// Run UI (blocks until quit)
	if _, err := program.Run(); err != nil {
		logger.Emit(otel.Event{Kind: otel.KindError, Level: otel.LevelError, Comp: "main", Msg: "program error", Err: err.Error()})
	}

	logger.Emit(otel.Event{Kind: otel.KindShutdown, Level: otel.LevelInfo, Comp: "main", Msg: "observer stopping"})

	// Graceful shutdown
	cancel()
	coordinator.Wait()
}