package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// OllamaReranker implements Reranker using Ollama with Qwen3-Reranker models.
// Uses the cross-encoder prompt format that Qwen3-Reranker expects.
type OllamaReranker struct {
	endpoint    string
	model       string
	client      *http.Client
	concurrency int // Max parallel requests to Ollama
}

// NewOllamaReranker creates a reranker using Ollama.
// Model should be a Qwen3-Reranker variant (e.g., "dengcao/Qwen3-Reranker-4B:Q5_K_M").
// If model is empty, attempts to auto-detect a reranker model.
func NewOllamaReranker(endpoint, model string) *OllamaReranker {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}

	r := &OllamaReranker{
		endpoint:    endpoint,
		model:       model,
		client:      &http.Client{Timeout: 120 * time.Second},
		concurrency: 32, // Let Ollama batch and GPU sort it out
	}

	// Auto-detect model if not specified
	if r.model == "" {
		r.model = r.detectModel()
	}

	return r
}

// Name returns the reranker identifier.
func (r *OllamaReranker) Name() string {
	if r.model == "" {
		return "ollama/none"
	}
	return fmt.Sprintf("ollama/%s", r.model)
}

// Available returns true if Ollama is running and has a suitable model.
func (r *OllamaReranker) Available() bool {
	if r.model == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint+"/api/tags", nil)
	if err != nil {
		return false
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// detectModel finds a Qwen3-Reranker model from available Ollama models.
func (r *OllamaReranker) detectModel() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint+"/api/tags", nil)
	if err != nil {
		return ""
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return ""
	}

	// Prefer Qwen3-Reranker models
	for _, m := range tags.Models {
		name := strings.ToLower(m.Name)
		if strings.Contains(name, "qwen3-reranker") {
			return m.Name
		}
	}

	// Fall back to any reranker model
	for _, m := range tags.Models {
		name := strings.ToLower(m.Name)
		if strings.Contains(name, "rerank") {
			return m.Name
		}
	}

	return ""
}

// Rerank scores documents against the query using Qwen3-Reranker.
// Runs scoring in parallel for efficiency.
func (r *OllamaReranker) Rerank(ctx context.Context, query string, documents []string) ([]Score, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	if !r.Available() {
		return nil, fmt.Errorf("reranker not available (model: %s)", r.model)
	}

	scores := make([]Score, len(documents))
	for i := range scores {
		scores[i] = Score{Index: i, Score: 0.5} // Default neutral score
	}

	// Semaphore for concurrency control
	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup
	var errCount int64

	for i, doc := range documents {
		wg.Add(1)
		go func(idx int, document string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			score, err := r.ScoreOne(ctx, query, document)
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				// Keep default 0.5 score
				return
			}
			scores[idx] = Score{Index: idx, Score: score}
		}(i, doc)
	}

	wg.Wait()

	// Return error only if all requests failed
	if errCount == int64(len(documents)) {
		return nil, fmt.Errorf("all %d rerank requests failed", len(documents))
	}

	return scores, nil
}

// ScoreOne scores a single query-document pair using Qwen3-Reranker.
// Uses the ChatML prompt format with raw mode that Qwen3-Reranker requires.
func (r *OllamaReranker) ScoreOne(ctx context.Context, query, doc string) (float32, error) {
	// Truncate very long documents to avoid context overflow
	if len(doc) > 500 {
		doc = doc[:500] + "..."
	}

	// Qwen3-Reranker ChatML format with raw mode
	// Uses topic-relevance instruct (not "passages that answer the query")
	// because we're matching news headlines to user interests, not doing QA.
	prompt := fmt.Sprintf(
		"<|im_start|>system\nJudge whether the Document meets the requirements based on the Query and the Instruct provided. Note that the answer can only be \"yes\" or \"no\".<|im_end|>\n<|im_start|>user\n<Instruct>: Given a topic of interest, determine if the news headline is relevant to the topic\n<Query>: %s\n<Document>: %s<|im_end|>\n<|im_start|>assistant\n",
		query, doc)

	body := map[string]any{
		"model":  r.model,
		"prompt": prompt,
		"raw":    true,
		"stream": false,
		"options": map[string]any{
			"temperature": 0.0,   // Deterministic
			"num_predict": 300,   // Qwen3 uses <think> blocks before answering
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ollama error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return r.parseResponse(respBody)
}

// ollamaGenerateResponse represents Ollama's /api/generate response.
type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// parseResponse extracts a relevance score from the Ollama response.
// Parses a numeric score (0.0-1.0) from the response text.
func (r *OllamaReranker) parseResponse(body []byte) (float32, error) {
	var resp ollamaGenerateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parse response: %w", err)
	}

	return r.scoreFromText(resp.Response), nil
}

// scoreFromText parses a relevance score from the response text.
// Handles numeric values (0.0-1.0) and text responses (yes/no/maybe).
func (r *OllamaReranker) scoreFromText(response string) float32 {
	response = strings.TrimSpace(response)

	// Strip <think>...</think> blocks (Qwen3 reasoning output)
	if idx := strings.Index(response, "</think>"); idx != -1 {
		response = strings.TrimSpace(response[idx+len("</think>"):])
	}

	// Handle empty response
	if response == "" {
		return 0.5
	}

	// Get the first word for text-based responses
	firstWord := strings.ToLower(strings.TrimRight(strings.Fields(response)[0], ".,!?"))

	// Check for yes/no text responses first
	switch firstWord {
	case "yes", "y", "true":
		return 1.0
	case "no", "n", "false":
		return 0.0
	case "maybe", "unclear", "uncertain":
		return 0.5
	}

	// Try to parse a float from the beginning of the response
	var score float64
	found := false
	if _, err := fmt.Sscanf(response, "%f", &score); err == nil {
		found = true
	} else {
		// Try to find a number anywhere in the response
		for _, word := range strings.Fields(response) {
			if _, err := fmt.Sscanf(word, "%f", &score); err == nil {
				found = true
				break
			}
		}
	}

	if !found {
		return 0.5
	}

	// Clamp to 0-1 range
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return float32(score)
}
