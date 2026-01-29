package rerank

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

// JinaReranker scores documents against queries using the Jina AI rerank API.
type JinaReranker struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
	limiter  *rate.Limiter
}

// NewJinaReranker creates a reranker using the Jina AI rerank API.
// If model is empty, defaults to "jina-reranker-v3".
func NewJinaReranker(apiKey, model string) *JinaReranker {
	if model == "" {
		model = "jina-reranker-v3"
	}
	return &JinaReranker{
		apiKey:   apiKey,
		model:    model,
		endpoint: "https://api.jina.ai/v1/rerank",
		client:   &http.Client{Timeout: 30 * time.Second},
		limiter:  rate.NewLimiter(rate.Every(750*time.Millisecond), 1),
	}
}

// Available returns true if the Jina API key is configured.
func (r *JinaReranker) Available() bool {
	return r.apiKey != ""
}

// Name returns the reranker identifier for logging.
func (r *JinaReranker) Name() string {
	return "jina/" + r.model
}

// Rerank scores documents against the query using the Jina rerank API.
// Returns a Score slice with the same length as documents, in corresponding order.
func (r *JinaReranker) Rerank(ctx context.Context, query string, documents []string) ([]Score, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	if err := r.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	reqBody := jinaRerankRequest{
		Model:     r.model,
		Query:     query,
		Documents: documents,
		TopN:      len(documents),
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respBody, err := r.doWithRetry(ctx, jsonBody)
	if err != nil {
		return nil, err
	}

	var jinaResp jinaRerankResponse
	if err := json.Unmarshal(respBody, &jinaResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Initialize all scores to 0.
	scores := make([]Score, len(documents))
	for i := range scores {
		scores[i] = Score{Index: i, Score: 0}
	}

	// Fill in scores from the API response.
	for _, result := range jinaResp.Results {
		if result.Index >= 0 && result.Index < len(scores) {
			scores[result.Index] = Score{
				Index: result.Index,
				Score: float32(result.RelevanceScore),
			}
		}
	}

	return scores, nil
}

// doWithRetry executes the Jina API request with retry logic for transient errors.
// Retries up to 3 times on HTTP 429 or 5xx with exponential backoff (1s, 2s, 4s).
// Honors the Retry-After header on 429 responses.
func (r *JinaReranker) doWithRetry(ctx context.Context, jsonBody []byte) ([]byte, error) {
	maxRetries := 3
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+r.apiKey)

		resp, err := r.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("request cancelled: %w", ctx.Err())
			}
			lastErr = fmt.Errorf("request failed: %w", err)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoffs[attempt]):
				}
			}
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoffs[attempt]):
				}
			}
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		// Retry on 429 (rate limited) or 5xx (server error).
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("jina API error (status %d): %s", resp.StatusCode, string(body))

			if attempt < maxRetries {
				delay := backoffs[attempt]

				// Honor Retry-After header on 429.
				if resp.StatusCode == http.StatusTooManyRequests {
					if ra := resp.Header.Get("Retry-After"); ra != "" {
						if seconds, parseErr := strconv.Atoi(ra); parseErr == nil && seconds > 0 {
							delay = time.Duration(seconds) * time.Second
							if delay > 30*time.Second {
								delay = 30 * time.Second
							}
						}
					}
				}

				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
			}
			continue
		}

		// Non-retryable error (e.g. 400, 401, 403).
		return nil, fmt.Errorf("jina API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil, fmt.Errorf("jina API request failed after %d retries: %w", maxRetries, lastErr)
}

// jinaRerankRequest is the request body for the Jina rerank API.
type jinaRerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

// jinaRerankResponse is the response body from the Jina rerank API.
type jinaRerankResponse struct {
	Results []jinaRerankResult `json:"results"`
}

// jinaRerankResult is a single document's relevance score from the Jina API.
type jinaRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}
