package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/abelbrown/observer/internal/coord"
	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/fetch"
	"github.com/abelbrown/observer/internal/filter"
	rerankpkg "github.com/abelbrown/observer/internal/rerank"
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

	// Create fetcher
	fetcher := fetch.NewFetcher(30 * time.Second)

	// Jina API configuration
	jinaKey := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	embedModel := envOrDefault("JINA_EMBED_MODEL", "jina-embeddings-v3")
	rerankModel := envOrDefault("JINA_RERANK_MODEL", "jina-reranker-v3")

	// Embedder: prefer Jina API, fall back to Ollama
	var embedder embed.Embedder
	if jinaKey != "" {
		embedder = embed.NewJinaEmbedder(jinaKey, embedModel)
	} else {
		embedder = embed.NewOllamaEmbedder("http://localhost:11434", "mxbai-embed-large")
	}

	// Reranker: Jina for batch reranking, Ollama for per-entry scoring
	ollamaReranker := rerankpkg.NewOllamaReranker("http://localhost:11434", "")
	var jinaReranker *rerankpkg.JinaReranker
	if jinaKey != "" {
		jinaReranker = rerankpkg.NewJinaReranker(jinaKey, rerankModel)
	}

	// Default sources (can be made configurable later)
	sources := []fetch.Source{
		{Type: "rss", Name: "Hacker News", URL: "https://news.ycombinator.com/rss"},
		{Type: "rss", Name: "Lobsters", URL: "https://lobste.rs/rss"},
	}

	// Create UI app with dependency injection
	cfg := ui.AppConfig{
		// loadItems: load from store with filters
		LoadItems: func() tea.Cmd {
			return func() tea.Msg {
				items, err := st.GetItems(500, false)
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
					log.Printf("Warning: failed to get embeddings: %v", err)
					embeddings = make(map[string][]float32)
				}

				// Use semantic dedup (falls back to URL dedup if no embeddings)
				// Threshold 0.85 means items with >85% cosine similarity are considered duplicates
				items = filter.SemanticDedup(items, embeddings, 0.85)
				items = filter.LimitPerSource(items, 20)

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
		// markRead
		MarkRead: func(id string) tea.Cmd {
			return func() tea.Msg {
				st.MarkRead(id)
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
		// Uses EmbedQuery() for Jina (retrieval.query task type) for better search quality
		EmbedQuery: func(query string) tea.Cmd {
			return func() tea.Msg {
				if !embedder.Available() {
					return ui.QueryEmbedded{Query: query, Err: nil}
				}
				type queryEmbedder interface {
					EmbedQuery(ctx context.Context, query string) ([]float32, error)
				}
				if qe, ok := embedder.(queryEmbedder); ok {
					emb, err := qe.EmbedQuery(ctx, query)
					return ui.QueryEmbedded{Query: query, Embedding: emb, Err: err}
				}
				embedding, err := embedder.Embed(ctx, query)
				return ui.QueryEmbedded{Query: query, Embedding: embedding, Err: err}
			}
		},
		// scoreEntry: score a single entry using cross-encoder (Ollama path)
		ScoreEntry: func(query string, doc string, index int) tea.Cmd {
			return func() tea.Msg {
				if !ollamaReranker.Available() {
					return ui.EntryReranked{Index: index, Score: 0.5, Err: nil}
				}
				score, err := ollamaReranker.ScoreOne(ctx, query, doc)
				return ui.EntryReranked{Index: index, Score: score, Err: err}
			}
		},
	}

	// BatchRerank: single API call for all docs (Jina only)
	if jinaReranker != nil {
		cfg.BatchRerank = func(query string, docs []string) tea.Cmd {
			return func() tea.Msg {
				scores, err := jinaReranker.Rerank(ctx, query, docs)
				if err != nil {
					return ui.RerankComplete{Query: query, Err: err}
				}
				result := make([]float32, len(docs))
				for _, s := range scores {
					if s.Index < len(result) {
						result[s.Index] = s.Score
					}
				}
				return ui.RerankComplete{Query: query, Scores: result}
			}
		}
	}

	app := ui.NewAppWithConfig(cfg)

	// Create program
	program := tea.NewProgram(app, tea.WithAltScreen())

	// Create and start coordinator
	coordinator := coord.NewCoordinator(st, fetcher, embedder, sources)
	coordinator.Start(ctx, program)

	// Start background embedding worker (continuously embeds items without embeddings)
	coordinator.StartEmbeddingWorker(ctx)

	// Run UI (blocks until quit)
	if _, err := program.Run(); err != nil {
		log.Printf("Error running program: %v", err)
	}

	// Graceful shutdown
	cancel()
	coordinator.Wait()
}
