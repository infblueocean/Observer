package rerank

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestScoreFromText(t *testing.T) {
	r := &OllamaReranker{}

	tests := []struct {
		response string
		want     float32
	}{
		{"yes", 1.0},
		{"Yes", 1.0},
		{"YES", 1.0},
		{"yes.", 1.0},
		{"yes, the document is relevant", 1.0},
		{"y", 1.0},
		{"true", 1.0},
		{"1", 1.0},
		{"no", 0.0},
		{"No", 0.0},
		{"NO", 0.0},
		{"no.", 0.0},
		{"no, the document is not relevant", 0.0},
		{"n", 0.0},
		{"false", 0.0},
		{"0", 0.0},
		{"maybe", 0.5},
		{"", 0.5},
		{"  ", 0.5},
		{"unclear", 0.5},
		// Numeric scores - the critical bug case
		{"0.0", 0.0},
		{"0.1", 0.1},
		{"0.5", 0.5},
		{"0.85", 0.85},
		{"1.0", 1.0},
		{" 0.0", 0.0},
		{"0.0\n\nThe document is not relevant", 0.0},
		{"0.9 - highly relevant", 0.9},
		{"The relevance score is 0.7", 0.7},
		// Think-wrapped responses (Qwen3-Reranker format)
		{"<think>\nSome reasoning here.\n</think>\n\nyes", 1.0},
		{"<think>\nNot relevant.\n</think>\n\nNo.", 0.0},
		{"<think>\n\n</think>\nyes", 1.0},
		{"<think>\nThe document discusses the Super Bowl which matches the query.\n</think>\n\nyes", 1.0},
		{"<think>\nThis is about cooking, not sports.\n</think>\n\nno", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.response, func(t *testing.T) {
			got := r.scoreFromText(tt.response)
			if math.Abs(float64(got-tt.want)) > 0.01 {
				t.Errorf("scoreFromText(%q) = %v, want %v", tt.response, got, tt.want)
			}
		})
	}
}

func TestNewOllamaReranker_Defaults(t *testing.T) {
	r := NewOllamaReranker("", "test-model")

	if r.endpoint != "http://localhost:11434" {
		t.Errorf("endpoint = %s, want http://localhost:11434", r.endpoint)
	}
	if r.model != "test-model" {
		t.Errorf("model = %s, want test-model", r.model)
	}
	if r.concurrency != 32 {
		t.Errorf("concurrency = %d, want 32", r.concurrency)
	}
}

func TestOllamaReranker_Name(t *testing.T) {
	r := NewOllamaReranker("", "qwen3-reranker:4b")
	if r.Name() != "ollama/qwen3-reranker:4b" {
		t.Errorf("Name() = %s, want ollama/qwen3-reranker:4b", r.Name())
	}

	r2 := &OllamaReranker{}
	if r2.Name() != "ollama/none" {
		t.Errorf("Name() = %s, want ollama/none", r2.Name())
	}
}

// --- Available() tests ---

func TestOllamaReranker_Available_ServerUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	r := &OllamaReranker{
		endpoint: srv.URL,
		model:    "test-model",
		client:   srv.Client(),
	}
	if !r.Available() {
		t.Error("Available() = false, want true when server is up and model is set")
	}
}

func TestOllamaReranker_Available_NoModel(t *testing.T) {
	r := &OllamaReranker{
		endpoint: "http://localhost:11434",
		model:    "",
		client:   http.DefaultClient,
	}
	if r.Available() {
		t.Error("Available() = true, want false when model is empty")
	}
}

func TestOllamaReranker_Available_ServerDown(t *testing.T) {
	r := &OllamaReranker{
		endpoint: "http://127.0.0.1:1", // unreachable port
		model:    "test-model",
		client:   http.DefaultClient,
	}
	if r.Available() {
		t.Error("Available() = true, want false when server is unreachable")
	}
}

func TestOllamaReranker_Available_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := &OllamaReranker{
		endpoint: srv.URL,
		model:    "test-model",
		client:   srv.Client(),
	}
	if r.Available() {
		t.Error("Available() = true, want false when server returns 500")
	}
}

// --- detectModel() tests ---

func TestOllamaReranker_DetectModel(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantModel  string
	}{
		{
			name:   "prefer_qwen3",
			status: 200,
			body:   `{"models":[{"name":"dengcao/Qwen3-Reranker-4B:Q5_K_M"},{"name":"llama3:8b"}]}`,
			wantModel: "dengcao/Qwen3-Reranker-4B:Q5_K_M",
		},
		{
			name:   "fallback_to_rerank",
			status: 200,
			body:   `{"models":[{"name":"llama3:8b"},{"name":"bge-reranker-v2:latest"}]}`,
			wantModel: "bge-reranker-v2:latest",
		},
		{
			name:   "qwen3_over_generic",
			status: 200,
			body:   `{"models":[{"name":"bge-reranker:latest"},{"name":"Qwen3-Reranker-0.6B:latest"}]}`,
			wantModel: "Qwen3-Reranker-0.6B:latest",
		},
		{
			name:      "no_reranker",
			status:    200,
			body:      `{"models":[{"name":"llama3:8b"},{"name":"mistral:7b"}]}`,
			wantModel: "",
		},
		{
			name:      "empty_models",
			status:    200,
			body:      `{"models":[]}`,
			wantModel: "",
		},
		{
			name:      "server_error",
			status:    500,
			body:      `internal server error`,
			wantModel: "",
		},
		{
			name:      "invalid_json",
			status:    200,
			body:      `not-json`,
			wantModel: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			r := &OllamaReranker{
				endpoint: srv.URL,
				client:   srv.Client(),
			}
			got := r.detectModel()
			if got != tt.wantModel {
				t.Errorf("detectModel() = %q, want %q", got, tt.wantModel)
			}
		})
	}
}

// --- parseResponse() tests ---

func TestOllamaReranker_ParseResponse(t *testing.T) {
	r := &OllamaReranker{}

	tests := []struct {
		name    string
		input   string
		want    float32
		wantErr bool
	}{
		{
			name:  "yes_response",
			input: `{"response":"yes","done":true}`,
			want:  1.0,
		},
		{
			name:  "no_response",
			input: `{"response":"no","done":true}`,
			want:  0.0,
		},
		{
			name:  "numeric_response",
			input: `{"response":"0.75","done":true}`,
			want:  0.75,
		},
		{
			name:  "think_wrapped_yes",
			input: `{"response":"<think>Some reasoning</think>\nyes","done":true}`,
			want:  1.0,
		},
		{
			name:  "empty_response",
			input: `{"response":"","done":true}`,
			want:  0.5,
		},
		{
			name:    "invalid_json",
			input:   `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.parseResponse([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(float64(got-tt.want)) > 0.01 {
				t.Errorf("parseResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- ScoreOne() tests ---

func TestOllamaReranker_ScoreOne(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		status    int
		want      float32
		wantErr   bool
		checkBody func(t *testing.T, body map[string]any)
	}{
		{
			name:     "yes_response",
			response: `{"response":"yes","done":true}`,
			status:   200,
			want:     1.0,
			checkBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["model"] != "test-model" {
					t.Errorf("model = %v, want test-model", body["model"])
				}
				if body["raw"] != true {
					t.Errorf("raw = %v, want true", body["raw"])
				}
				if body["stream"] != false {
					t.Errorf("stream = %v, want false", body["stream"])
				}
				prompt, _ := body["prompt"].(string)
				if !strings.Contains(prompt, "test query") {
					t.Errorf("prompt missing query, got: %s", prompt)
				}
				if !strings.Contains(prompt, "test document") {
					t.Errorf("prompt missing document, got: %s", prompt)
				}
			},
		},
		{
			name:     "no_response",
			response: `{"response":"no","done":true}`,
			status:   200,
			want:     0.0,
		},
		{
			name:    "server_error",
			status:  500,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.checkBody != nil {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode request body: %v", err)
					}
					tt.checkBody(t, body)
				}
				w.WriteHeader(tt.status)
				if tt.response != "" {
					w.Write([]byte(tt.response))
				} else {
					w.Write([]byte(`{"error":"server error"}`))
				}
			}))
			defer srv.Close()

			r := &OllamaReranker{
				endpoint: srv.URL,
				model:    "test-model",
				client:   srv.Client(),
			}

			got, err := r.ScoreOne(context.Background(), "test query", "test document")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(float64(got-tt.want)) > 0.01 {
				t.Errorf("ScoreOne() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOllamaReranker_ScoreOne_LongDocTruncated(t *testing.T) {
	var receivedPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		receivedPrompt, _ = body["prompt"].(string)
		w.WriteHeader(200)
		w.Write([]byte(`{"response":"yes","done":true}`))
	}))
	defer srv.Close()

	r := &OllamaReranker{
		endpoint: srv.URL,
		model:    "test-model",
		client:   srv.Client(),
	}

	longDoc := strings.Repeat("a", 600)
	_, err := r.ScoreOne(context.Background(), "query", longDoc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The document in the prompt should be truncated to 500 chars + "..."
	if strings.Contains(receivedPrompt, longDoc) {
		t.Error("expected long document to be truncated, but full doc found in prompt")
	}
	if !strings.Contains(receivedPrompt, "...") {
		t.Error("expected truncation marker '...' in prompt")
	}
}

func TestOllamaReranker_ScoreOne_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"response":"yes","done":true}`))
	}))
	defer srv.Close()

	r := &OllamaReranker{
		endpoint: srv.URL,
		model:    "test-model",
		client:   srv.Client(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := r.ScoreOne(ctx, "query", "doc")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// --- Rerank() tests ---

// ollamaTestServer creates an httptest server that handles both /api/tags and /api/generate.
func ollamaTestServer(t *testing.T, tagsBody string, generateHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(200)
			w.Write([]byte(tagsBody))
		case "/api/generate":
			if generateHandler != nil {
				generateHandler(w, r)
			} else {
				w.WriteHeader(200)
				w.Write([]byte(`{"response":"yes","done":true}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestOllamaReranker_Rerank_Success(t *testing.T) {
	var generateCalls int64
	srv := ollamaTestServer(t,
		`{"models":[{"name":"test-model"}]}`,
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&generateCalls, 1)
			w.WriteHeader(200)
			w.Write([]byte(`{"response":"yes","done":true}`))
		},
	)
	defer srv.Close()

	r := &OllamaReranker{
		endpoint:    srv.URL,
		model:       "test-model",
		client:      srv.Client(),
		concurrency: 32,
	}

	docs := []string{"doc1", "doc2", "doc3"}
	scores, err := r.Rerank(context.Background(), "query", docs)
	if err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("got %d scores, want 3", len(scores))
	}
	for i, s := range scores {
		if s.Index != i {
			t.Errorf("scores[%d].Index = %d, want %d", i, s.Index, i)
		}
		if math.Abs(float64(s.Score-1.0)) > 0.01 {
			t.Errorf("scores[%d].Score = %v, want 1.0", i, s.Score)
		}
	}
	if calls := atomic.LoadInt64(&generateCalls); calls != 3 {
		t.Errorf("generate calls = %d, want 3", calls)
	}
}

func TestOllamaReranker_Rerank_EmptyDocs(t *testing.T) {
	r := &OllamaReranker{
		endpoint:    "http://localhost:11434",
		model:       "test-model",
		client:      http.DefaultClient,
		concurrency: 32,
	}

	// nil docs
	scores, err := r.Rerank(context.Background(), "query", nil)
	if err != nil {
		t.Fatalf("Rerank(nil) error: %v", err)
	}
	if scores != nil {
		t.Errorf("expected nil scores for nil docs, got %v", scores)
	}

	// empty slice
	scores2, err := r.Rerank(context.Background(), "query", []string{})
	if err != nil {
		t.Fatalf("Rerank([]) error: %v", err)
	}
	if scores2 != nil {
		t.Errorf("expected nil scores for empty docs, got %v", scores2)
	}
}

func TestOllamaReranker_Rerank_NotAvailable(t *testing.T) {
	r := &OllamaReranker{
		endpoint:    "http://127.0.0.1:1",
		model:       "", // No model => not available
		client:      http.DefaultClient,
		concurrency: 32,
	}

	_, err := r.Rerank(context.Background(), "query", []string{"doc"})
	if err == nil {
		t.Fatal("expected error when reranker not available, got nil")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error = %q, want it to contain 'not available'", err.Error())
	}
}

func TestOllamaReranker_Rerank_AllFail(t *testing.T) {
	srv := ollamaTestServer(t,
		`{"models":[{"name":"test-model"}]}`,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"internal error"}`))
		},
	)
	defer srv.Close()

	r := &OllamaReranker{
		endpoint:    srv.URL,
		model:       "test-model",
		client:      srv.Client(),
		concurrency: 32,
	}

	_, err := r.Rerank(context.Background(), "query", []string{"doc1", "doc2", "doc3"})
	if err == nil {
		t.Fatal("expected error when all rerank requests fail, got nil")
	}
	if !strings.Contains(err.Error(), "all 3 rerank requests failed") {
		t.Errorf("error = %q, want it to contain 'all 3 rerank requests failed'", err.Error())
	}
}

func TestOllamaReranker_Rerank_PartialFailure(t *testing.T) {
	var callIdx int64
	srv := ollamaTestServer(t,
		`{"models":[{"name":"test-model"}]}`,
		func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt64(&callIdx, 1)
			if n%2 == 0 {
				// Even calls fail
				w.WriteHeader(500)
				w.Write([]byte(`{"error":"fail"}`))
				return
			}
			w.WriteHeader(200)
			w.Write([]byte(`{"response":"yes","done":true}`))
		},
	)
	defer srv.Close()

	r := &OllamaReranker{
		endpoint:    srv.URL,
		model:       "test-model",
		client:      srv.Client(),
		concurrency: 1, // Sequential to make alternating predictable
	}

	docs := []string{"doc1", "doc2", "doc3"}
	scores, err := r.Rerank(context.Background(), "query", docs)
	if err != nil {
		t.Fatalf("Rerank() with partial failure should not error, got: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("got %d scores, want 3", len(scores))
	}

	// At least one should be 1.0 (success) and at least one 0.5 (default/failure)
	var hasSuccess, hasDefault bool
	for _, s := range scores {
		if math.Abs(float64(s.Score-1.0)) < 0.01 {
			hasSuccess = true
		}
		if math.Abs(float64(s.Score-0.5)) < 0.01 {
			hasDefault = true
		}
	}
	if !hasSuccess {
		t.Error("expected at least one successful score (1.0)")
	}
	if !hasDefault {
		t.Error("expected at least one default score (0.5) from failed request")
	}
}

// --- NewOllamaReranker auto-detect test ---

func TestNewOllamaReranker_AutoDetect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"models":[{"name":"dengcao/Qwen3-Reranker-4B:Q5_K_M"},{"name":"llama3:8b"}]}`))
	}))
	defer srv.Close()

	r := NewOllamaReranker(srv.URL, "")
	if r.model != "dengcao/Qwen3-Reranker-4B:Q5_K_M" {
		t.Errorf("model = %q, want %q", r.model, "dengcao/Qwen3-Reranker-4B:Q5_K_M")
	}
}
