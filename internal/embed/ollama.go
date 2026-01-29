package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaEmbedder generates embeddings via local Ollama server.
type OllamaEmbedder struct {
	endpoint string       // e.g., "http://localhost:11434"
	model    string       // e.g., "nomic-embed-text"
	client   *http.Client // HTTP client for requests
}

// ollamaTagsResponse represents the response from GET /api/tags.
type ollamaTagsResponse struct {
	Models []ollamaModel `json:"models"`
}

// ollamaModel represents a model in the tags response.
type ollamaModel struct {
	Name string `json:"name"`
}

// ollamaEmbedRequest represents the request body for POST /api/embed.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// ollamaEmbedResponse represents the response from POST /api/embed.
type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// NewOllamaEmbedder creates a new OllamaEmbedder with the given endpoint and model.
func NewOllamaEmbedder(endpoint, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		endpoint: endpoint,
		model:    model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Available returns true if the Ollama server is accessible and the model exists.
// Uses a 3-second timeout for the availability check.
func (e *OllamaEmbedder) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.endpoint+"/api/tags", nil)
	if err != nil {
		return false
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	var tagsResp ollamaTagsResponse
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return false
	}

	// Check if the configured model exists
	// Handle both exact matches and matches without the tag suffix
	// e.g., "mxbai-embed-large" should match "mxbai-embed-large:latest"
	for _, model := range tagsResp.Models {
		if model.Name == e.model {
			return true
		}
		// Check if model name without tag matches (e.g., "model:latest" matches "model")
		if model.Name == e.model+":latest" {
			return true
		}
	}

	return false
}

// Embed generates a vector embedding for the given text using Ollama.
// Respects context cancellation and returns meaningful errors.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model: e.model,
		Input: text,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embed: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/api/embed", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("embed: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		// Check if context was cancelled
		if ctx.Err() != nil {
			return nil, fmt.Errorf("embed: request cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("embed: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("embed: ollama returned status %d (failed to read body: %v)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("embed: ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embed: failed to read response: %w", err)
	}

	var embedResp ollamaEmbedResponse
	if err := json.Unmarshal(body, &embedResp); err != nil {
		return nil, fmt.Errorf("embed: failed to parse response: %w", err)
	}

	if len(embedResp.Embeddings) == 0 {
		return nil, fmt.Errorf("embed: no embeddings returned")
	}

	return embedResp.Embeddings[0], nil
}
