package embed

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestJinaEmbedQueryLatency measures a single query embedding call.
// Skipped unless JINA_API_KEY is set.
func TestJinaEmbedQueryLatency(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	if key == "" {
		t.Skip("JINA_API_KEY not set")
	}

	e := NewJinaEmbedder(key, "jina-embeddings-v3")

	ctx := context.Background()
	start := time.Now()
	emb, err := e.EmbedQuery(ctx, "NFL football")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	t.Logf("Latency: %dms", elapsed.Milliseconds())
	t.Logf("Dimensions: %d", len(emb))

	fmt.Fprintf(os.Stderr, "\n=== JINA EMBED QUERY: %dms, %d dims ===\n", elapsed.Milliseconds(), len(emb))
}
