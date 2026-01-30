package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/abelbrown/observer/internal/embed"
	"github.com/abelbrown/observer/internal/filter"
	"github.com/abelbrown/observer/internal/store"
)

func runSearch() {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	topN := fs.Int("top", 30, "Number of cross-encoder candidates")
	cosineOnly := fs.Bool("cosine-only", false, "Skip cross-encoder reranking")
	fs.Parse(os.Args[1:])

	queries := fs.Args()
	if len(queries) == 0 {
		fmt.Fprintln(os.Stderr, "usage: obs search [--top N] [--cosine-only] <query> [query...]")
		os.Exit(1)
	}

	apiKey := requireJinaKey()

	st := openDB()
	defer st.Close()

	// Load items (same pipeline as main.go)
	allItems, err := st.GetItems(10000, true)
	if err != nil {
		log.Fatalf("get items: %v", err)
	}
	allItems = filter.ByAge(allItems, 24*time.Hour)

	ids := make([]string, len(allItems))
	for i, item := range allItems {
		ids[i] = item.ID
	}
	allEmbeddings, err := st.GetItemsWithEmbeddings(ids)
	if err != nil {
		log.Fatalf("get embeddings: %v", err)
	}

	allItems = filter.SemanticDedup(allItems, allEmbeddings, 0.85)
	allItems = filter.LimitPerSource(allItems, 50)

	// Rebuild embeddings for filtered items
	filteredEmb := make(map[string][]float32)
	for _, item := range allItems {
		if emb, ok := allEmbeddings[item.ID]; ok {
			filteredEmb[item.ID] = emb
		}
	}

	withEmb := 0
	for _, item := range allItems {
		if _, ok := filteredEmb[item.ID]; ok {
			withEmb++
		}
	}
	fmt.Printf("Items: %d total, %d with embeddings\n", len(allItems), withEmb)
	fmt.Println(strings.Repeat("=", 80))

	ctx := context.Background()
	embedder := newJinaEmbedder(apiKey)
	reranker := newJinaReranker(apiKey)

	for _, query := range queries {
		fmt.Printf("\n\n>>> QUERY: %q\n", query)
		fmt.Println(strings.Repeat("-", 80))

		// Embed query
		t0 := time.Now()
		queryEmb, err := embedder.EmbedQuery(ctx, query)
		embedDur := time.Since(t0)
		if err != nil {
			fmt.Printf("  ERROR embedding query: %v\n", err)
			continue
		}
		fmt.Printf("  Query embedded in %v\n", embedDur.Round(time.Millisecond))

		// Stage 1: cosine similarity
		reranked := filter.RerankByQuery(allItems, filteredEmb, queryEmb)

		fmt.Println("\n  STAGE 1 — Cosine Similarity (Top 10):")
		for i := 0; i < 10 && i < len(reranked); i++ {
			item := reranked[i]
			sim := float32(0)
			hasEmb := false
			if emb, ok := filteredEmb[item.ID]; ok {
				sim = embed.CosineSimilarity(emb, queryEmb)
				hasEmb = true
			}
			embTag := "NO-EMB"
			if hasEmb {
				embTag = fmt.Sprintf("%.4f", sim)
			}
			fmt.Printf("  %2d. [%s] %s — %s\n", i+1, embTag, item.SourceName, truncate(item.Title, 70))
		}

		if *cosineOnly {
			continue
		}

		// Stage 2: cross-encoder reranking
		n := *topN
		if n > len(reranked) {
			n = len(reranked)
		}
		candidates := reranked[:n]

		docs := make([]string, n)
		for i, item := range candidates {
			docs[i] = item.Title
			if item.Summary != "" {
				docs[i] += " - " + item.Summary
			}
		}

		t1 := time.Now()
		scores, err := reranker.Rerank(ctx, query, docs)
		rerankDur := time.Since(t1)
		if err != nil {
			fmt.Printf("  ERROR reranking: %v\n", err)
			continue
		}

		type scored struct {
			index int
			score float32
			item  store.Item
		}
		scoredItems := make([]scored, len(candidates))
		for i, item := range candidates {
			scoredItems[i] = scored{index: i, score: scores[i].Score, item: item}
		}
		sort.SliceStable(scoredItems, func(i, j int) bool {
			return scoredItems[i].score > scoredItems[j].score
		})

		fmt.Printf("\n  STAGE 2 — Cross-Encoder Reranking (Top 10) [%v]:\n", rerankDur.Round(time.Millisecond))
		for i := 0; i < 10 && i < len(scoredItems); i++ {
			s := scoredItems[i]
			fmt.Printf("  %2d. [%.4f] %s — %s\n", i+1, s.score, s.item.SourceName, truncate(s.item.Title, 70))
		}
	}

	fmt.Println()
}
