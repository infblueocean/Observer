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

	// Jina API configuration
	jinaKey := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	embedModel := envOrDefault("JINA_EMBED_MODEL", "jina-embeddings-v3")
	rerankModel := envOrDefault("JINA_RERANK_MODEL", "jina-reranker-v3")

	// Embedder and reranker: Jina API required
	if jinaKey == "" {
		log.Fatal("JINA_API_KEY environment variable is required")
	}
	embedder := embed.NewJinaEmbedder(jinaKey, embedModel)
	jinaReranker := rerank.NewJinaReranker(jinaKey, rerankModel)

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
				// Dedup only (no source limiting for search)
				items = filter.SemanticDedup(items, embeddings, 0.85)

				filteredEmbeddings := make(map[string][]float32)
				for _, item := range items {
					if emb, ok := embeddings[item.ID]; ok {
						filteredEmbeddings[item.ID] = emb
					}
				}
				return ui.SearchPoolLoaded{Items: items, Embeddings: filteredEmbeddings, QueryID: queryID}
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
		// embedQuery: embed a query string for semantic search
		EmbedQuery: func(ctx context.Context, query string, queryID string) tea.Cmd {
			return func() tea.Msg {
				emb, err := embedder.EmbedQuery(ctx, query)
				return ui.QueryEmbedded{Query: query, Embedding: emb, Err: err, QueryID: queryID}
			}
		},
		// batchRerank: single Jina API call for all docs
		BatchRerank: func(ctx context.Context, query string, docs []string, queryID string) tea.Cmd {
			return func() tea.Msg {
				scores, err := jinaReranker.Rerank(ctx, query, docs)
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
		},
		Obs: ui.ObsConfig{
			Logger: logger,
			Ring:   ring,
		},
		AutoReranks: true,
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
