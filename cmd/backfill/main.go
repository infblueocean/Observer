package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/store"
)

func main() {
	// Handle graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Check for API key
	apiKey := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	if apiKey == "" {
		log.Fatal("JINA_API_KEY environment variable is required")
	}

	model := os.Getenv("JINA_EMBED_MODEL")
	if model == "" {
		model = "jina-embeddings-v3"
	}

	// Open database
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	dbPath := filepath.Join(homeDir, ".observer", "observer.db")

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer st.Close()

	// Count existing embeddings and total items
	totalItems, err := st.CountAllItems()
	if err != nil {
		log.Fatalf("Failed to count items: %v", err)
	}
	needingEmbedding, err := st.CountItemsNeedingEmbedding()
	if err != nil {
		log.Fatalf("Failed to count items needing embedding: %v", err)
	}
	existingEmbeddings := totalItems - needingEmbedding

	fmt.Printf("Database: %s\n", dbPath)
	fmt.Printf("Total items: %d\n", totalItems)
	fmt.Printf("Existing embeddings: %d\n", existingEmbeddings)
	fmt.Printf("Needing embedding: %d\n", needingEmbedding)
	fmt.Println()

	// If there are existing embeddings, prompt to clear them
	// (old mxbai embeddings are incompatible with Jina)
	if existingEmbeddings > 0 {
		fmt.Printf("Clear %d existing embeddings and re-embed with Jina? [y/N] ", existingEmbeddings)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return
		}

		cleared, err := st.ClearAllEmbeddings()
		if err != nil {
			log.Fatalf("Failed to clear embeddings: %v", err)
		}
		fmt.Printf("Cleared %d embeddings.\n\n", cleared)
	}

	// Create Jina embedder
	embedder := embed.NewJinaEmbedder(apiKey, model)
	fmt.Printf("Using model: %s\n", model)
	fmt.Println("Starting backfill... (Ctrl+C to stop, re-run to resume)")
	fmt.Println()

	// Process in batches of 100
	batchSize := 100
	embedded := 0

	for {
		if ctx.Err() != nil {
			fmt.Printf("\nInterrupted. Embedded %d items. Re-run to continue.\n", embedded)
			return
		}

		items, err := st.GetItemsNeedingEmbedding(batchSize)
		if err != nil {
			log.Fatalf("Failed to get items: %v", err)
		}
		if len(items) == 0 {
			break
		}

		// Build texts
		texts := make([]string, len(items))
		for i, item := range items {
			texts[i] = item.Title
			if item.Summary != "" {
				texts[i] += " " + item.Summary
			}
		}

		// Embed batch
		embeddings, err := embedder.EmbedBatch(ctx, texts)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Printf("\nInterrupted. Embedded %d items. Re-run to continue.\n", embedded)
				return
			}
			log.Fatalf("Embedding failed: %v", err)
		}

		// Save embeddings
		saved := 0
		for i, emb := range embeddings {
			if ctx.Err() != nil {
				break
			}
			if i < len(items) {
				if err := st.SaveEmbedding(items[i].ID, emb); err != nil {
					log.Printf("Warning: failed to save embedding for %s: %v", items[i].ID, err)
				} else {
					saved++
				}
			}
		}

		embedded += saved
		remaining, _ := st.CountItemsNeedingEmbedding()
		fmt.Printf("Embedded %d items (%d remaining)\n", embedded, remaining)
	}

	fmt.Printf("\nDone! Embedded %d items total.\n", embedded)
}
