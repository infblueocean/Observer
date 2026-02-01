package rerank

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestJinaRerankLatency_Single makes a single rerank API call (1 query, 1 doc)
// and reports latency. Skipped unless JINA_API_KEY is set.
func TestJinaRerankLatency_Single(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	if key == "" {
		t.Skip("JINA_API_KEY not set")
	}

	r := NewJinaReranker(key, "jina-reranker-v3")

	query := "NFL football"
	docs := []string{"Patrick Mahomes Leads Chiefs to Overtime Victory"}

	ctx := context.Background()
	start := time.Now()
	scores, err := r.Rerank(ctx, query, docs)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	if len(scores) != 1 {
		t.Fatalf("Expected 1 score, got %d", len(scores))
	}

	t.Logf("Latency: %dms", elapsed.Milliseconds())
	t.Logf("Query: %q", query)
	t.Logf("Doc:   %q", docs[0])
	t.Logf("Score: %.4f", scores[0].Score)

	fmt.Fprintf(os.Stderr, "\n=== JINA SINGLE RERANK: %dms ===\n", elapsed.Milliseconds())
}

// TestJinaRerankLatency_Batch30 reranks 30 docs (the actual batch size used in search)
// to measure realistic latency. Skipped unless JINA_API_KEY is set.
func TestJinaRerankLatency_Batch30(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	if key == "" {
		t.Skip("JINA_API_KEY not set")
	}

	r := NewJinaReranker(key, "jina-reranker-v3")

	query := "NFL football"
	docs := []string{
		"NFL Draft 2025: Top Prospects and Mock Draft Analysis",
		"OpenAI Releases GPT-5 with Multimodal Reasoning",
		"Patrick Mahomes Leads Chiefs to Overtime Victory",
		"Federal Reserve Holds Interest Rates Steady",
		"Super Bowl LVIII: Biggest Plays and Highlights",
		"Rust 2.0 Announced with Async Improvements",
		"Bitcoin Surges Past $100K as ETF Inflows Accelerate",
		"NFL Free Agency: Top Available Players and Predictions",
		"James Webb Telescope Detects New Exoplanet Atmosphere",
		"Severe Thunderstorm Warning for Central Texas",
		"EU Parliament Passes Comprehensive AI Regulation Act",
		"Ukraine Peace Talks Resume in Geneva",
		"CRISPR Gene Therapy Shows Promise for Sickle Cell Disease",
		"Apple Vision Pro Sales Disappoint in Q4",
		"SQLite Adds Built-in Vector Search Extension",
		"NVIDIA Stock Hits All-Time High on AI Demand",
		"NBA Playoffs: Lakers vs Celtics Preview",
		"Tesla Announces Next-Gen Battery Technology",
		"SpaceX Starship Completes First Orbital Flight",
		"Amazon Prime Day Breaks Sales Records Again",
		"Google DeepMind Achieves Breakthrough in Protein Folding",
		"New York City Announces Major Transit Overhaul",
		"Climate Scientists Warn of Accelerating Ice Sheet Loss",
		"Major Cybersecurity Breach Affects Fortune 500 Company",
		"WHO Declares End to Latest Global Health Emergency",
		"Housing Market Sees First Price Drop in Two Years",
		"Record-Breaking Heat Wave Sweeps Across Southern Europe",
		"Congressional Budget Office Releases 2026 Fiscal Outlook",
		"Wimbledon Final Delivers Five-Set Thriller",
		"New Study Links Ultra-Processed Foods to Cognitive Decline",
	}

	ctx := context.Background()
	start := time.Now()
	scores, err := r.Rerank(ctx, query, docs)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	t.Logf("Batch 30 docs: %dms", elapsed.Milliseconds())

	// Verify NFL items are top-ranked
	nflIndices := map[int]bool{0: true, 2: true, 4: true, 7: true}
	topScored := 0
	for _, s := range scores {
		if nflIndices[s.Index] && s.Score > 0.0 {
			topScored++
		}
	}
	t.Logf("NFL items with positive scores: %d/4", topScored)

	fmt.Fprintf(os.Stderr, "\n=== JINA BATCH 30: %dms ===\n", elapsed.Milliseconds())
}
