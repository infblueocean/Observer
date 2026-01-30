package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestJinaAvailable(t *testing.T) {
	t.Run("available with API key", func(t *testing.T) {
		e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
		if !e.Available() {
			t.Error("Available() returned false, want true")
		}
	})

	t.Run("not available without API key", func(t *testing.T) {
		e := NewJinaEmbedder("", "jina-embeddings-v3")
		if e.Available() {
			t.Error("Available() returned true, want false")
		}
	})
}

func TestJinaEmbed(t *testing.T) {
	expectedEmbedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	inputText := "test text for embedding"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected content-type: %s", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("unexpected authorization: %s", auth)
		}

		var req jinaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.Model != "jina-embeddings-v3" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if len(req.Input) != 1 || req.Input[0] != inputText {
			t.Errorf("unexpected input: %v", req.Input)
		}
		if req.Task != "retrieval.passage" {
			t.Errorf("unexpected task: %s, want retrieval.passage", req.Task)
		}
		if req.Dimensions != 1024 {
			t.Errorf("unexpected dimensions: %d, want 1024", req.Dimensions)
		}

		resp := jinaEmbedResponse{
			Data: []jinaEmbedding{
				{Embedding: expectedEmbedding, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	result, err := e.Embed(context.Background(), inputText)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if len(result) != len(expectedEmbedding) {
		t.Fatalf("Embed() returned %d elements, want %d", len(result), len(expectedEmbedding))
	}
	for i, v := range result {
		if v != expectedEmbedding[i] {
			t.Errorf("Embed()[%d] = %v, want %v", i, v, expectedEmbedding[i])
		}
	}
}

func TestJinaEmbedBatch(t *testing.T) {
	texts := []string{"hello world", "foo bar", "baz qux"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jinaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.Task != "retrieval.passage" {
			t.Errorf("unexpected task: %s, want retrieval.passage", req.Task)
		}

		// Return embeddings in reverse order to test reordering via Index field
		resp := jinaEmbedResponse{
			Data: make([]jinaEmbedding, len(req.Input)),
		}
		for i := range req.Input {
			embedding := make([]float32, 3)
			embedding[0] = float32(i) * 0.1
			embedding[1] = float32(i) * 0.2
			embedding[2] = float32(i) * 0.3
			// Return in reverse order but with correct Index
			resp.Data[len(req.Input)-1-i] = jinaEmbedding{
				Embedding: embedding,
				Index:     i,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	results, err := e.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}

	if len(results) != len(texts) {
		t.Fatalf("EmbedBatch() returned %d results, want %d", len(results), len(texts))
	}

	// Verify embeddings are in correct order (index 0 should have 0.0, 0.0, 0.0)
	for i, emb := range results {
		expected0 := float32(i) * 0.1
		if emb[0] != expected0 {
			t.Errorf("EmbedBatch()[%d][0] = %v, want %v", i, emb[0], expected0)
		}
	}
}

func TestJinaEmbedBatchChunking(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)

		var req jinaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Verify each chunk is at most 25 items
		if len(req.Input) > 25 {
			t.Errorf("chunk size %d exceeds maximum of 25", len(req.Input))
		}

		resp := jinaEmbedResponse{
			Data: make([]jinaEmbedding, len(req.Input)),
		}
		for i := range req.Input {
			resp.Data[i] = jinaEmbedding{
				Embedding: []float32{float32(i)},
				Index:     i,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	// Create 75 texts, should result in 3 API calls (25 + 25 + 25)
	texts := make([]string, 75)
	for i := range texts {
		texts[i] = fmt.Sprintf("text %d", i)
	}

	results, err := e.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}

	if len(results) != 75 {
		t.Fatalf("EmbedBatch() returned %d results, want 75", len(results))
	}

	// Verify 3 API calls were made (75 / 25 = 3 chunks)
	if count := callCount.Load(); count != 3 {
		t.Errorf("expected 3 API calls, got %d", count)
	}
}

func TestJinaEmbedQuery(t *testing.T) {
	var receivedTask string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jinaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		receivedTask = req.Task

		resp := jinaEmbedResponse{
			Data: []jinaEmbedding{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	result, err := e.EmbedQuery(context.Background(), "search query")
	if err != nil {
		t.Fatalf("EmbedQuery() error = %v", err)
	}

	if receivedTask != "retrieval.query" {
		t.Errorf("task = %q, want %q", receivedTask, "retrieval.query")
	}

	if len(result) != 3 {
		t.Fatalf("EmbedQuery() returned %d elements, want 3", len(result))
	}
}

func TestJinaRetryOn429(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)

		if count == 1 {
			// First call: return 429 with Retry-After header
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}

		// Second call: succeed
		resp := jinaEmbedResponse{
			Data: []jinaEmbedding{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	result, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("Embed() returned %d elements, want 3", len(result))
	}

	if count := callCount.Load(); count != 2 {
		t.Errorf("expected 2 API calls (1 retry), got %d", count)
	}
}

func TestJinaRetryOn500(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)

		if count == 1 {
			// First call: return 500
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Second call: succeed
		resp := jinaEmbedResponse{
			Data: []jinaEmbedding{
				{Embedding: []float32{0.4, 0.5, 0.6}, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	result, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("Embed() returned %d elements, want 3", len(result))
	}

	if count := callCount.Load(); count != 2 {
		t.Errorf("expected 2 API calls (1 retry), got %d", count)
	}
}

func TestJinaRetryExhausted(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		// Always return 500
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("Embed() expected error after retries exhausted, got nil")
	}

	if !strings.Contains(err.Error(), "retries exhausted") {
		t.Errorf("Embed() error = %v, want 'retries exhausted' error", err)
	}

	// Should have made 4 calls: 1 initial + 3 retries
	if count := callCount.Load(); count != 4 {
		t.Errorf("expected 4 API calls (1 initial + 3 retries), got %d", count)
	}
}

func TestJinaEmbedServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 400 Bad Request - not retryable
		http.Error(w, "bad request: invalid input", http.StatusBadRequest)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("Embed() expected error for 400 status, got nil")
	}

	if !strings.Contains(err.Error(), "400") {
		t.Errorf("Embed() error = %v, want status 400 in error", err)
	}
}

func TestJinaEmbedContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay response to allow cancellation
		time.Sleep(500 * time.Millisecond)
		resp := jinaEmbedResponse{
			Data: []jinaEmbedding{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := e.Embed(ctx, "test")
	if err == nil {
		t.Fatal("Embed() expected error for cancelled context, got nil")
	}

	if !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("Embed() error = %v, want context cancellation error", err)
	}
}

func TestJinaEmbedBatchOutOfRangeIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jinaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Return an embedding with an out-of-range index (150 for a chunk of size 3)
		resp := jinaEmbedResponse{
			Data: []jinaEmbedding{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
				{Embedding: []float32{0.4, 0.5, 0.6}, Index: 150},
				{Embedding: []float32{0.7, 0.8, 0.9}, Index: 2},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	_, err := e.EmbedBatch(context.Background(), []string{"hello", "world", "foo"})
	if err == nil {
		t.Fatal("EmbedBatch() expected error for out-of-range index, got nil")
	}

	if !strings.Contains(err.Error(), "out-of-range index") {
		t.Errorf("EmbedBatch() error = %v, want 'out-of-range index' error", err)
	}
}

func TestJinaEmbedBatchPartialResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jinaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Return fewer embeddings than inputs (only 2 out of 3)
		resp := jinaEmbedResponse{
			Data: []jinaEmbedding{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
				{Embedding: []float32{0.7, 0.8, 0.9}, Index: 2},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e := NewJinaEmbedder("test-key", "jina-embeddings-v3")
	e.endpoint = server.URL
	e.limiter = rate.NewLimiter(rate.Inf, 1)

	_, err := e.EmbedBatch(context.Background(), []string{"hello", "world", "foo"})
	if err == nil {
		t.Fatal("EmbedBatch() expected error for partial results, got nil")
	}

	if !strings.Contains(err.Error(), "missing embedding") {
		t.Errorf("EmbedBatch() error = %v, want 'missing embedding' error", err)
	}
}
