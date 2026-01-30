package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/rerank"
	"github.com/abelbrown/observer/internal/store"
)

// dataDir returns ~/.observer/, creating it if needed.
func dataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home directory: %v", err)
	}
	dir := filepath.Join(home, ".observer")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("failed to create data directory: %v", err)
	}
	return dir
}

// dbPath returns the path to observer.db.
func dbPath() string {
	return filepath.Join(dataDir(), "observer.db")
}

// eventLogPath returns the path to observer.events.jsonl.
func eventLogPath() string {
	return filepath.Join(dataDir(), "observer.events.jsonl")
}

// openDB opens the store or fatals.
func openDB() *store.Store {
	st, err := store.Open(dbPath())
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	return st
}

// requireJinaKey returns the JINA_API_KEY or fatals.
func requireJinaKey() string {
	key := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	if key == "" {
		fmt.Fprintln(os.Stderr, "error: JINA_API_KEY environment variable is required")
		fmt.Fprintln(os.Stderr, "  export JINA_API_KEY=... or source ~/src/claude/keys.sh")
		os.Exit(1)
	}
	return key
}

// envOrDefault returns the environment variable value or a fallback.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// newJinaEmbedder creates a Jina embedder with the configured model.
func newJinaEmbedder(apiKey string) *embed.JinaEmbedder {
	model := envOrDefault("JINA_EMBED_MODEL", "jina-embeddings-v3")
	return embed.NewJinaEmbedder(apiKey, model)
}

// newJinaReranker creates a Jina reranker with the configured model.
func newJinaReranker(apiKey string) *rerank.JinaReranker {
	model := envOrDefault("JINA_RERANK_MODEL", "jina-reranker-v3")
	return rerank.NewJinaReranker(apiKey, model)
}

// truncate shortens a string to max runes, appending "..." if truncated.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}
