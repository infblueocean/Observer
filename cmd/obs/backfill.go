package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
)

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var whitespaceRe = regexp.MustCompile(`\s+`)

func sanitizeForEmbedding(s string, maxChars int) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = whitespaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if len(s) > maxChars {
		s = s[:maxChars]
	}
	return s
}

func runBackfill() {
	fs := flag.NewFlagSet("backfill", flag.ExitOnError)
	clear := fs.Bool("clear", false, "Clear all existing embeddings before backfilling")
	batchSize := fs.Int("batch-size", 50, "Items per batch")
	dryRun := fs.Bool("dry-run", false, "Show counts without embedding")
	fs.Parse(os.Args[1:])

	apiKey := requireJinaKey()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	st := openDB()
	defer st.Close()

	// Count existing embeddings and total items
	totalItems, err := st.CountAllItems()
	if err != nil {
		log.Fatalf("failed to count items: %v", err)
	}
	needingEmbedding, err := st.CountItemsNeedingEmbedding()
	if err != nil {
		log.Fatalf("failed to count items needing embedding: %v", err)
	}
	existingEmbeddings := totalItems - needingEmbedding

	fmt.Printf("Database: %s\n", dbPath())
	fmt.Printf("Total items: %d\n", totalItems)
	fmt.Printf("Existing embeddings: %d\n", existingEmbeddings)
	fmt.Printf("Needing embedding: %d\n", needingEmbedding)
	fmt.Println()

	if *dryRun {
		fmt.Println("(dry run — no changes made)")
		return
	}

	// Clear existing embeddings if requested
	if *clear && existingEmbeddings > 0 {
		fmt.Printf("Clear %d existing embeddings and re-embed? [y/N] ", existingEmbeddings)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return
		}

		cleared, err := st.ClearAllEmbeddings()
		if err != nil {
			log.Fatalf("failed to clear embeddings: %v", err)
		}
		fmt.Printf("Cleared %d embeddings.\n\n", cleared)
	}

	// Create Jina embedder
	embedder := newJinaEmbedder(apiKey)
	fmt.Printf("Using model: %s\n", envOrDefault("JINA_EMBED_MODEL", "jina-embeddings-v3"))
	fmt.Println("Starting backfill... (Ctrl+C to stop, re-run to resume)")
	fmt.Println()

	embedded := 0

	for {
		if ctx.Err() != nil {
			fmt.Printf("\nInterrupted. Embedded %d items. Re-run to continue.\n", embedded)
			return
		}

		items, err := st.GetItemsNeedingEmbedding(*batchSize)
		if err != nil {
			log.Fatalf("failed to get items: %v", err)
		}
		if len(items) == 0 {
			break
		}

		// Build texts (strip HTML, cap length)
		texts := make([]string, len(items))
		for i, item := range items {
			texts[i] = item.Title
			if item.Summary != "" {
				clean := sanitizeForEmbedding(item.Summary, 2000-len(item.Title))
				if clean != "" {
					texts[i] += " " + clean
				}
			}
		}

		// Embed batch with retries
		var embeddings [][]float32
		for attempt := 0; attempt < 3; attempt++ {
			if ctx.Err() != nil {
				fmt.Printf("\nInterrupted. Embedded %d items. Re-run to continue.\n", embedded)
				return
			}
			embeddings, err = embedder.EmbedBatch(ctx, texts)
			if err == nil {
				break
			}
			log.Printf("Batch attempt %d failed: %v", attempt+1, err)
		}
		if err != nil {
			log.Printf("Skipping batch of %d items after 3 failures: %v", len(items), err)
			continue
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
