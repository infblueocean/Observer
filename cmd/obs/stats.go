package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/abelbrown/observer/internal/filter"
)

func runStats() {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	dbHealth := fs.Bool("db", false, "Include DB health section (timestamps, embedding coverage)")
	fs.Parse(os.Args[1:])

	st := openDB()
	defer st.Close()

	// --- Pipeline statistics ---

	all, _ := st.GetItems(99999, true)
	fmt.Printf("Total in DB:           %d\n", len(all))

	unread, _ := st.GetItems(99999, false)
	fmt.Printf("Unread in DB:          %d\n", len(unread))

	recent := filter.ByAge(all, 24*time.Hour)
	fmt.Printf("Last 24h (all):        %d\n", len(recent))

	recentUnread := filter.ByAge(unread, 24*time.Hour)
	fmt.Printf("Last 24h (unread):     %d\n", len(recentUnread))

	// Simulate the actual pipeline from main.go
	items, _ := st.GetItems(500, false)
	fmt.Printf("\nGetItems(500, false):   %d\n", len(items))

	items = filter.ByAge(items, 24*time.Hour)
	fmt.Printf("After ByAge(24h):      %d\n", len(items))

	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	embeddings, _ := st.GetItemsWithEmbeddings(ids)
	fmt.Printf("With embeddings:       %d\n", len(embeddings))

	items = filter.SemanticDedup(items, embeddings, 0.85)
	fmt.Printf("After SemanticDedup:   %d\n", len(items))

	items = filter.LimitPerSource(items, 50)
	fmt.Printf("After LimitPerSource:  %d\n", len(items))

	// Count sources
	sources := map[string]int{}
	for _, item := range items {
		sources[item.SourceName]++
	}
	fmt.Printf("\nSources (%d):\n", len(sources))
	for name, count := range sources {
		fmt.Printf("  %-35s %d\n", name, count)
	}

	// --- DB health section ---
	if !*dbHealth {
		return
	}

	fmt.Println()
	fmt.Println("=== DB Health ===")

	totalItems, _ := st.CountAllItems()
	needingEmbedding, _ := st.CountItemsNeedingEmbedding()
	existingEmbeddings := totalItems - needingEmbedding

	fmt.Printf("Total items:           %d\n", totalItems)
	fmt.Printf("With embeddings:       %d\n", existingEmbeddings)
	if totalItems > 0 {
		fmt.Printf("Embedding coverage:    %.1f%%\n", float64(existingEmbeddings)/float64(totalItems)*100)
	}
	fmt.Printf("Needing embedding:     %d\n", needingEmbedding)

	// Timestamp analysis
	sample, _ := st.GetItems(5000, true)
	if len(sample) == 0 {
		return
	}

	now := time.Now()
	fmt.Printf("\nSample size: %d items\n", len(sample))

	buckets := []time.Duration{
		1 * time.Hour, 6 * time.Hour, 24 * time.Hour,
		7 * 24 * time.Hour, 30 * 24 * time.Hour,
	}
	labels := []string{"<1h", "<6h", "<24h", "<7d", "<30d"}

	fmt.Println("\nBy published_at:")
	for i, d := range buckets {
		count := 0
		for _, item := range sample {
			if now.Sub(item.Published) < d {
				count++
			}
		}
		fmt.Printf("  %-8s %d\n", labels[i], count)
	}

	fmt.Println("\nBy fetched_at:")
	for i, d := range buckets {
		count := 0
		for _, item := range sample {
			if now.Sub(item.Fetched) < d {
				count++
			}
		}
		fmt.Printf("  %-8s %d\n", labels[i], count)
	}

	fmt.Printf("\nNewest published: %s (%.0fh ago)\n",
		sample[0].Published.Format(time.RFC3339),
		now.Sub(sample[0].Published).Hours())
	last := len(sample) - 1
	fmt.Printf("Oldest in sample: %s (%.0fh ago)\n",
		sample[last].Published.Format(time.RFC3339),
		now.Sub(sample[last].Published).Hours())

	// Bogus dates
	zeroCount := 0
	futureCount := 0
	for _, item := range sample {
		if item.Published.IsZero() || item.Published.Year() < 2020 {
			zeroCount++
		}
		if item.Published.After(now.Add(1 * time.Hour)) {
			futureCount++
		}
	}
	fmt.Printf("\nBogus published_at (zero/pre-2020): %d\n", zeroCount)
	fmt.Printf("Future published_at (>1h ahead):    %d\n", futureCount)
}
