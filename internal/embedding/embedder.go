package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/abelbrown/observer/internal/httpclient"
	"github.com/abelbrown/observer/internal/logging"
)

// Vector is a dense embedding vector
type Vector []float64

// Embedder generates embedding vectors from text
type Embedder interface {
	// Embed generates a vector for a single text
	Embed(ctx context.Context, text string) (Vector, error)

	// EmbedBatch generates vectors for multiple texts
	EmbedBatch(ctx context.Context, texts []string) ([]Vector, error)

	// Name returns the model name
	Name() string

	// Available checks if the embedder is ready
	Available() bool
}

// CosineSimilarity computes similarity between two vectors (1.0 = identical, 0.0 = orthogonal)
func CosineSimilarity(a, b Vector) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// OllamaEmbedder uses Ollama for local embeddings
type OllamaEmbedder struct {
	endpoint  string
	model     string
	client    *http.Client
	available bool
}

// NewOllamaEmbedder creates an embedder using Ollama
func NewOllamaEmbedder(endpoint, model string) *OllamaEmbedder {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if model == "" {
		model = "mxbai-embed-large"
	}

	e := &OllamaEmbedder{
		endpoint: endpoint,
		model:    model,
		client:   httpclient.Default(),
	}

	// Check availability
	e.checkAvailable()

	return e
}

func (e *OllamaEmbedder) checkAvailable() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to get model info
	req, err := http.NewRequestWithContext(ctx, "GET", e.endpoint+"/api/tags", nil)
	if err != nil {
		return
	}

	resp, err := e.client.Do(req)
	if err != nil {
		logging.Warn("Ollama embedder not available", "error", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}

	// Check if our model is available
	for _, m := range result.Models {
		if m.Name == e.model || m.Name == e.model+":latest" {
			e.available = true
			logging.Info("Ollama embedder available", "model", e.model)
			return
		}
	}

	logging.Warn("Embedding model not found in Ollama", "model", e.model)
}

func (e *OllamaEmbedder) Name() string {
	return e.model
}

func (e *OllamaEmbedder) Available() bool {
	return e.available
}

// ollamaEmbedRequest is the request format for Ollama embed API
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// ollamaEmbedResponse is the response format from Ollama embed API
type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) (Vector, error) {
	if !e.available {
		return nil, fmt.Errorf("embedder not available")
	}

	reqBody := ollamaEmbedRequest{
		Model: e.model,
		Input: text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.endpoint+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	return Vector(result.Embeddings[0]), nil
}

func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([]Vector, error) {
	if !e.available {
		return nil, fmt.Errorf("embedder not available")
	}

	// Ollama doesn't have native batch embedding, so we do sequential calls
	// Could parallelize but keeping simple for now
	vectors := make([]Vector, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding text %d: %w", i, err)
		}
		vectors[i] = vec
	}

	return vectors, nil
}
