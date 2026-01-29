package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/abelbrown/observer/internal/coord"
	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/fetch"
	"github.com/abelbrown/observer/internal/filter"
	"github.com/abelbrown/observer/internal/store"
	"github.com/abelbrown/observer/internal/ui"
)

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

	// Create embedder (optional - gracefully degrades if Ollama unavailable)
	embedder := embed.NewOllamaEmbedder("http://localhost:11434", "mxbai-embed-large")

	// Default sources (can be made configurable later)
	sources := []fetch.Source{
		{Type: "rss", Name: "Hacker News", URL: "https://news.ycombinator.com/rss"},
		{Type: "rss", Name: "Lobsters", URL: "https://lobste.rs/rss"},
	}

	// Create UI app with dependency injection
	app := ui.NewApp(
		// loadItems: load from store with filters
		func() tea.Cmd {
			return func() tea.Msg {
				items, err := st.GetItems(500, false)
				if err != nil {
					return ui.ItemsLoaded{Err: err}
				}
				items = filter.ByAge(items, 24*time.Hour)

				// Get embeddings for semantic dedup
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
				return ui.ItemsLoaded{Items: items}
			}
		},
		// markRead
		func(id string) tea.Cmd {
			return func() tea.Msg {
				st.MarkRead(id)
				return ui.ItemMarkedRead{ID: id}
			}
		},
		// triggerFetch (manual refresh triggers reload after fetch)
		func() tea.Cmd {
			return func() tea.Msg {
				return ui.RefreshTick{}
			}
		},
	)

	// Create program
	program := tea.NewProgram(app, tea.WithAltScreen())

	// Create and start coordinator
	coordinator := coord.NewCoordinator(st, fetcher, embedder, sources)
	coordinator.Start(ctx, program)

	// Run UI (blocks until quit)
	if _, err := program.Run(); err != nil {
		log.Printf("Error running program: %v", err)
	}

	// Graceful shutdown
	cancel()
	coordinator.Wait()
}
