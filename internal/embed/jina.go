package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// JinaEmbedder generates embeddings via the Jina AI API.
type JinaEmbedder struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
	limiter  *rate.Limiter
}

// jinaEmbedRequest represents the request body for the Jina embeddings API.
type jinaEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Task       string   `json:"task"`
	Dimensions int      `json:"dimensions"`
	Truncate   bool     `json:"truncate"`
}

// jinaEmbedResponse represents the response from the Jina embeddings API.
type jinaEmbedResponse struct {
	Data []jinaEmbedding `json:"data"`
}

// jinaEmbedding represents a single embedding in the Jina response.
type jinaEmbedding struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// NewJinaEmbedder creates a new JinaEmbedder with the given API key and model.
func NewJinaEmbedder(apiKey, model string) *JinaEmbedder {
	if model == "" {
		model = "jina-embeddings-v3"
	}
	return &JinaEmbedder{
		apiKey:   apiKey,
		model:    model,
		endpoint: "https://api.jina.ai/v1/embeddings",
		client:   &http.Client{Timeout: 60 * time.Second},
		limiter:  rate.NewLimiter(rate.Every(750*time.Millisecond), 1), // ~80 RPM
	}
}

// Available returns true if the Jina API key is configured.
func (e *JinaEmbedder) Available() bool {
	return e.apiKey != ""
}

// Embed generates a vector embedding for the given text using the Jina API.
// Uses task type "retrieval.passage" for document embeddings.
func (e *JinaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.embed(ctx, []string{text}, "retrieval.passage")
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embed: jina returned no embeddings")
	}
	return resp.Data[0].Embedding, nil
}

// EmbedBatch generates vector embeddings for multiple texts in batch.
// Splits inputs into chunks of 100 to respect API limits.
// Uses task type "retrieval.passage" for document embeddings.
func (e *JinaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))

	// Process in chunks of 25 (smaller chunks = more reliable JSON responses)
	const chunkSize = 25
	for chunkStart := 0; chunkStart < len(texts); chunkStart += chunkSize {
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(texts) {
			chunkEnd = len(texts)
		}
		chunk := texts[chunkStart:chunkEnd]

		resp, err := e.embed(ctx, chunk, "retrieval.passage")
		if err != nil {
			return nil, fmt.Errorf("embed: batch chunk starting at %d failed: %w", chunkStart, err)
		}

		// Use the Index field to place embeddings in correct order within the chunk
		for _, item := range resp.Data {
			if item.Index < 0 || item.Index >= len(chunk) {
				return nil, fmt.Errorf("embed: jina returned out-of-range index %d for chunk of size %d starting at %d", item.Index, len(chunk), chunkStart)
			}
			globalIndex := chunkStart + item.Index
			results[globalIndex] = item.Embedding
		}
	}

	// Verify all results were populated
	for i, r := range results {
		if r == nil {
			return nil, fmt.Errorf("embed: missing embedding for index %d", i)
		}
	}

	return results, nil
}

// EmbedQuery generates a vector embedding for a query text using the Jina API.
// Uses task type "retrieval.query" for better search quality when embedding queries.
func (e *JinaEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.embed(ctx, []string{text}, "retrieval.query")
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embed: jina returned no embeddings")
	}
	return resp.Data[0].Embedding, nil
}

// embed is the internal method that calls the Jina API with the given inputs and task type.
func (e *JinaEmbedder) embed(ctx context.Context, input []string, task string) (*jinaEmbedResponse, error) {
	reqBody := jinaEmbedRequest{
		Model:      e.model,
		Input:      input,
		Task:       task,
		Dimensions: 1024,
		Truncate:   true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embed: failed to marshal request: %w", err)
	}

	return e.doWithRetry(ctx, jsonBody)
}

// doWithRetry executes the API request with retry logic for transient errors.
// Retries up to 3 times on HTTP 429 or 5xx status codes with exponential backoff.
// On 429, honors the Retry-After header if present.
func (e *JinaEmbedder) doWithRetry(ctx context.Context, reqBody []byte) (*jinaEmbedResponse, error) {
	maxRetries := 3
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Wait for rate limiter
		if err := e.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("embed: rate limiter wait failed: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("embed: failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.apiKey)

		resp, err := e.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("embed: request cancelled: %w", ctx.Err())
			}
			return nil, fmt.Errorf("embed: request failed: %w", err)
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("embed: failed to read response: %w", err)
		}

		// Success
		if resp.StatusCode == http.StatusOK {
			var embedResp jinaEmbedResponse
			if err := json.Unmarshal(body, &embedResp); err != nil {
				// Truncated/malformed response — treat as retryable
				lastErr = fmt.Errorf("embed: failed to parse response: %w", err)
				if attempt < maxRetries {
					select {
					case <-ctx.Done():
						return nil, fmt.Errorf("embed: request cancelled during retry: %w", ctx.Err())
					case <-time.After(backoffs[attempt]):
					}
				}
				continue
			}
			return &embedResp, nil
		}

		// Determine if we should retry
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if !retryable {
			return nil, fmt.Errorf("embed: jina returned status %d: %s", resp.StatusCode, string(body))
		}

		lastErr = fmt.Errorf("embed: jina returned status %d: %s", resp.StatusCode, string(body))

		// Don't sleep after the last attempt
		if attempt < maxRetries {
			delay := backoffs[attempt]

			// Honor Retry-After header on 429
			if resp.StatusCode == http.StatusTooManyRequests {
				if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
					if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
						delay = time.Duration(seconds) * time.Second
						if delay > 30*time.Second {
							delay = 30 * time.Second
						}
					}
				}
			}

			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("embed: request cancelled during retry: %w", ctx.Err())
			case <-time.After(delay):
			}
		}
	}

	return nil, fmt.Errorf("embed: all retries exhausted: %w", lastErr)
}
