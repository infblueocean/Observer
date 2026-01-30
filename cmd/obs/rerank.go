package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/abelbrown/observer/internal/rerank"
)

// testHeadlines is a fixed set of headlines for reranker validation.
// Mix of sports, politics, tech, and random topics.
var testHeadlines = []string{
	// Sports - should rank HIGH for "super bowl" query
	"Chiefs defeat 49ers in overtime thriller to win Super Bowl LVIII",
	"Patrick Mahomes named Super Bowl MVP after historic performance",
	"Taylor Swift celebrates with Travis Kelce after Super Bowl victory",
	"Super Bowl halftime show draws record 120 million viewers",
	"NFL playoffs bracket: Road to the Super Bowl",
	"Tom Brady reflects on his Super Bowl legacy in retirement interview",

	// Semi-related
	"Kansas City celebrates with Super Bowl parade downtown",
	"Football fans arrested after post-game celebration turns violent",
	"Sports betting sites crash during championship game",

	// Unrelated - should rank LOW
	"Trump squares off with Biden in heated debate exchange",
	"Chelsea fan stabbed outside stadium in London",
	"Stock market rallies on Fed interest rate decision",
	"SpaceX launches 40 Starlink satellites into orbit",
	"OpenAI announces GPT-5 release date",
	"Ukraine claims major gains in eastern offensive",
	"California wildfire evacuations expand to 50,000 residents",
	"Amazon raises Prime membership prices by 20%",
	"Bitcoin surges past $60,000 amid ETF approval rumors",
	"Scientists discover high-speed winds deep below Jupiter's clouds",
}

func runRerank() {
	fs := flag.NewFlagSet("rerank", flag.ExitOnError)
	query := fs.String("query", "super bowl", "Query to test")
	model := fs.String("model", "", "Ollama model name (auto-detects if empty)")
	endpoint := fs.String("endpoint", "", "Ollama endpoint (default: http://localhost:11434)")
	fs.Parse(os.Args[1:])

	fmt.Println("=== Reranker Validation ===")
	fmt.Println()

	// Create reranker
	modelName := *model
	if modelName == "" {
		modelName = "dengcao/Qwen3-Reranker-4B:Q5_K_M"
	}

	reranker := rerank.NewOllamaReranker(*endpoint, modelName)

	fmt.Printf("Endpoint: %s\n", func() string {
		if *endpoint == "" {
			return "http://localhost:11434 (default)"
		}
		return *endpoint
	}())
	fmt.Printf("Model: %s\n", reranker.Name())
	fmt.Printf("Available: %v\n", reranker.Available())
	fmt.Println()

	// Auto-detect if specified model unavailable
	if !reranker.Available() && *model == "" {
		fmt.Println("Specified model not available, trying auto-detection...")
		reranker = rerank.NewOllamaReranker(*endpoint, "")
		fmt.Printf("Auto-detected model: %s\n", reranker.Name())
		fmt.Printf("Available: %v\n", reranker.Available())
		fmt.Println()
	}

	if !reranker.Available() {
		fmt.Println("ERROR: No reranker model available!")
		fmt.Println()
		fmt.Println("To fix this:")
		fmt.Println("  1. Make sure Ollama is running: ollama serve")
		fmt.Println("  2. Pull a reranker model:")
		fmt.Println("     ollama pull dengcao/Qwen3-Reranker-4B:Q5_K_M")
		os.Exit(1)
	}

	fmt.Printf("Query: %q\n", *query)
	fmt.Printf("Documents: %d headlines\n", len(testHeadlines))
	fmt.Println()

	// Run reranking
	fmt.Println("Reranking...")
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	scores, err := reranker.Rerank(ctx, *query, testHeadlines)
	elapsed := time.Since(start)

	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Completed in %v (%.1f docs/sec)\n", elapsed, float64(len(testHeadlines))/elapsed.Seconds())
	fmt.Println()

	// Sort and display
	sorted := rerank.SortByScore(scores)

	fmt.Println("=== Results (sorted by relevance) ===")
	fmt.Println()
	for i, s := range sorted {
		headline := testHeadlines[s.Index]
		if len(headline) > 65 {
			headline = headline[:62] + "..."
		}
		marker := "  "
		if s.Score >= 0.7 {
			marker = "**"
		} else if s.Score <= 0.3 {
			marker = "--"
		}
		fmt.Printf("%s %2d. [%.3f] %s\n", marker, i+1, s.Score, headline)
	}

	fmt.Println()
	fmt.Println("Legend: ** = highly relevant (>=0.7), -- = not relevant (<=0.3)")

	// Statistics
	fmt.Println()
	fmt.Println("=== Statistics ===")
	relevant := rerank.FilterAboveThreshold(scores, 0.7)
	var irrelCount int
	for _, s := range scores {
		if s.Score <= 0.3 {
			irrelCount++
		}
	}
	fmt.Printf("Highly relevant (>=0.7): %d\n", len(relevant))
	fmt.Printf("Not relevant (<=0.3): %d\n", irrelCount)
	fmt.Printf("Neutral/unclear: %d\n", len(scores)-len(relevant)-irrelCount)

	fmt.Println()
	if len(relevant) >= 4 {
		fmt.Println("SUCCESS: Reranker correctly identified relevant headlines!")
	} else {
		fmt.Println("NOTE: Fewer than expected relevant headlines found.")
	}
}
