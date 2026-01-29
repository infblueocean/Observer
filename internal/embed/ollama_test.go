package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAvailable(t *testing.T) {
	// Mock server that returns our model in the tags list
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		resp := ollamaTagsResponse{
			Models: []ollamaModel{
				{Name: "nomic-embed-text"},
				{Name: "llama2"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	if !embedder.Available() {
		t.Error("Available() returned false, want true")
	}
}

func TestNotAvailable(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		model   string
	}{
		{
			name: "model not in list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				resp := ollamaTagsResponse{
					Models: []ollamaModel{
						{Name: "llama2"},
						{Name: "mistral"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			model: "nomic-embed-text",
		},
		{
			name: "empty model list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				resp := ollamaTagsResponse{
					Models: []ollamaModel{},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			model: "nomic-embed-text",
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			},
			model: "nomic-embed-text",
		},
		{
			name: "invalid json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("not json"))
			},
			model: "nomic-embed-text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			embedder := NewOllamaEmbedder(server.URL, tt.model)
			if embedder.Available() {
				t.Error("Available() returned true, want false")
			}
		})
	}
}

func TestEmbed(t *testing.T) {
	expectedEmbedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	inputText := "test text for embedding"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected content-type: %s", ct)
		}

		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.Model != "nomic-embed-text" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if req.Input != inputText {
			t.Errorf("unexpected input: %s", req.Input)
		}

		resp := ollamaEmbedResponse{
			Embeddings: [][]float32{expectedEmbedding},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	result, err := embedder.Embed(context.Background(), inputText)
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

func TestEmbedTimeout(t *testing.T) {
	// Server that delays response longer than context timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		resp := ollamaEmbedResponse{
			Embeddings: [][]float32{{0.1, 0.2, 0.3}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := embedder.Embed(ctx, "test")
	if err == nil {
		t.Error("Embed() expected error for timeout, got nil")
	}

	// Error should indicate cancellation
	if !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("Embed() error = %v, want context cancellation error", err)
	}
}

func TestEmbedOllamaDown(t *testing.T) {
	// Start a server and immediately close it to get a guaranteed-closed port
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := server.URL
	server.Close()

	embedder := NewOllamaEmbedder(endpoint, "nomic-embed-text")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := embedder.Embed(ctx, "test")
	if err == nil {
		t.Error("Embed() expected error when server is down, got nil")
	}

	// Error should indicate connection failure
	if !strings.Contains(err.Error(), "request failed") {
		t.Errorf("Embed() error = %v, want connection error", err)
	}
}

func TestEmbedEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaEmbedResponse{
			Embeddings: [][]float32{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	_, err := embedder.Embed(context.Background(), "test")
	if err == nil {
		t.Error("Embed() expected error for empty embeddings, got nil")
	}

	if !strings.Contains(err.Error(), "no embeddings") {
		t.Errorf("Embed() error = %v, want 'no embeddings' error", err)
	}
}

func TestEmbedServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	_, err := embedder.Embed(context.Background(), "test")
	if err == nil {
		t.Error("Embed() expected error for server error, got nil")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("Embed() error = %v, want status code in error", err)
	}
}

func TestEmbedInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	_, err := embedder.Embed(context.Background(), "test")
	if err == nil {
		t.Error("Embed() expected error for invalid JSON, got nil")
	}

	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("Embed() error = %v, want parse error", err)
	}
}
