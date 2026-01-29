package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestJinaRerankerAvailable(t *testing.T) {
	r := NewJinaReranker("test-key", "")
	if !r.Available() {
		t.Error("Expected Available() = true when API key is set")
	}

	r2 := NewJinaReranker("", "")
	if r2.Available() {
		t.Error("Expected Available() = false when API key is empty")
	}
}

func TestJinaRerankerName(t *testing.T) {
	r := NewJinaReranker("key", "jina-reranker-v3")
	if got := r.Name(); got != "jina/jina-reranker-v3" {
		t.Errorf("Name() = %q, want %q", got, "jina/jina-reranker-v3")
	}

	r2 := NewJinaReranker("key", "custom-model")
	if got := r2.Name(); got != "jina/custom-model" {
		t.Errorf("Name() = %q, want %q", got, "jina/custom-model")
	}

	// Default model when empty.
	r3 := NewJinaReranker("key", "")
	if got := r3.Name(); got != "jina/jina-reranker-v3" {
		t.Errorf("Name() = %q, want %q", got, "jina/jina-reranker-v3")
	}
}

func TestJinaRerankerRerank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Verify request format.
		if req.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", req.Method)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var body jinaRerankRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "jina-reranker-v3" {
			t.Errorf("model = %q, want %q", body.Model, "jina-reranker-v3")
		}
		if body.Query != "climate change" {
			t.Errorf("query = %q, want %q", body.Query, "climate change")
		}
		if len(body.Documents) != 3 {
			t.Errorf("documents count = %d, want 3", len(body.Documents))
		}
		if body.TopN != 3 {
			t.Errorf("top_n = %d, want 3", body.TopN)
		}

		resp := jinaRerankResponse{
			Results: []jinaRerankResult{
				{Index: 0, RelevanceScore: 0.9},
				{Index: 1, RelevanceScore: 0.1},
				{Index: 2, RelevanceScore: 0.7},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	r := NewJinaReranker("test-key", "jina-reranker-v3")
	r.endpoint = server.URL
	r.limiter = rate.NewLimiter(rate.Inf, 1)

	docs := []string{
		"Global warming accelerates ice melt",
		"New smartphone released today",
		"Carbon emissions hit record high",
	}

	scores, err := r.Rerank(context.Background(), "climate change", docs)
	if err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}

	if len(scores) != 3 {
		t.Fatalf("got %d scores, want 3", len(scores))
	}

	// Verify scores match expected values.
	expected := []float32{0.9, 0.1, 0.7}
	for i, s := range scores {
		if s.Index != i {
			t.Errorf("scores[%d].Index = %d, want %d", i, s.Index, i)
		}
		if s.Score != expected[i] {
			t.Errorf("scores[%d].Score = %v, want %v", i, s.Score, expected[i])
		}
	}
}

func TestJinaRerankerEmptyDocs(t *testing.T) {
	r := NewJinaReranker("test-key", "jina-reranker-v3")
	r.limiter = rate.NewLimiter(rate.Inf, 1)

	scores, err := r.Rerank(context.Background(), "query", nil)
	if err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}
	if scores != nil {
		t.Errorf("expected nil scores for empty docs, got %v", scores)
	}

	scores2, err := r.Rerank(context.Background(), "query", []string{})
	if err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}
	if scores2 != nil {
		t.Errorf("expected nil scores for empty docs, got %v", scores2)
	}
}

func TestJinaRerankerRetryOn429(t *testing.T) {
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"detail":"rate limited"}`))
			return
		}
		resp := jinaRerankResponse{
			Results: []jinaRerankResult{
				{Index: 0, RelevanceScore: 0.8},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	r := NewJinaReranker("test-key", "jina-reranker-v3")
	r.endpoint = server.URL
	r.limiter = rate.NewLimiter(rate.Inf, 1)

	scores, err := r.Rerank(context.Background(), "query", []string{"doc1"})
	if err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}

	finalCalls := atomic.LoadInt64(&calls)
	if finalCalls != 2 {
		t.Errorf("expected 2 API calls (1 retry), got %d", finalCalls)
	}

	if len(scores) != 1 {
		t.Fatalf("got %d scores, want 1", len(scores))
	}
	if scores[0].Score != 0.8 {
		t.Errorf("scores[0].Score = %v, want 0.8", scores[0].Score)
	}
}

func TestJinaRerankerRetryOn500(t *testing.T) {
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"detail":"internal error"}`))
			return
		}
		resp := jinaRerankResponse{
			Results: []jinaRerankResult{
				{Index: 0, RelevanceScore: 0.6},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	r := NewJinaReranker("test-key", "jina-reranker-v3")
	r.endpoint = server.URL
	r.limiter = rate.NewLimiter(rate.Inf, 1)

	scores, err := r.Rerank(context.Background(), "query", []string{"doc1"})
	if err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}

	finalCalls := atomic.LoadInt64(&calls)
	if finalCalls != 2 {
		t.Errorf("expected 2 API calls (1 retry), got %d", finalCalls)
	}

	if len(scores) != 1 {
		t.Fatalf("got %d scores, want 1", len(scores))
	}
	if scores[0].Score != 0.6 {
		t.Errorf("scores[0].Score = %v, want 0.6", scores[0].Score)
	}
}

func TestJinaRerankerServerError(t *testing.T) {
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"detail":"bad request"}`))
	}))
	defer server.Close()

	r := NewJinaReranker("test-key", "jina-reranker-v3")
	r.endpoint = server.URL
	r.limiter = rate.NewLimiter(rate.Inf, 1)

	_, err := r.Rerank(context.Background(), "query", []string{"doc1"})
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}

	finalCalls := atomic.LoadInt64(&calls)
	if finalCalls != 1 {
		t.Errorf("expected 1 API call (no retry for 400), got %d", finalCalls)
	}
}

func TestJinaRerankerContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Delay long enough for context to be cancelled.
		time.Sleep(5 * time.Second)
		resp := jinaRerankResponse{
			Results: []jinaRerankResult{
				{Index: 0, RelevanceScore: 0.5},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	r := NewJinaReranker("test-key", "jina-reranker-v3")
	r.endpoint = server.URL
	r.limiter = rate.NewLimiter(rate.Inf, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := r.Rerank(ctx, "query", []string{"doc1"})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
