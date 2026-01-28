package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abelbrown/observer/internal/httpclient"
	"github.com/abelbrown/observer/internal/logging"
)

// OllamaReranker uses Ollama to score document relevance.
// Uses parallel single-pair requests - Ollama batches internally on GPU.
type OllamaReranker struct {
	endpoint    string
	model       string
	client      *http.Client
	concurrency int // Max parallel requests
}

// NewOllamaReranker creates a reranker using Ollama.
// If model is empty, tries to auto-detect a suitable model.
func NewOllamaReranker(endpoint, model string) *OllamaReranker {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}

	r := &OllamaReranker{
		endpoint:    endpoint,
		model:       model,
		client:      httpclient.Default(), // Shared client with connection pooling
		concurrency: 32,                   // Parallel requests - Ollama batches on GPU
	}

	// Auto-detect model if not specified
	if r.model == "" {
		r.model = r.detectModel()
	}

	return r
}

func (r *OllamaReranker) Name() string {
	return fmt.Sprintf("ollama/%s", r.model)
}

func (r *OllamaReranker) Available() bool {
	if r.model == "" {
		return false
	}

	// Quick ping to check Ollama is running
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", r.endpoint+"/api/tags", nil)
	resp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// detectModel finds a suitable model for reranking.
func (r *OllamaReranker) detectModel() string {
	resp, err := r.client.Get(r.endpoint + "/api/tags")
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

	// Prefer reranker models
	for _, m := range tags.Models {
		name := strings.ToLower(m.Name)
		if strings.Contains(name, "rerank") {
			logging.Info("Auto-detected reranker model", "model", m.Name)
			return m.Name
		}
	}

	// Fall back to instruct models (good at following scoring instructions)
	for _, m := range tags.Models {
		name := strings.ToLower(m.Name)
		if strings.Contains(name, "instruct") {
			logging.Info("Using instruct model for reranking", "model", m.Name)
			return m.Name
		}
	}

	// Fall back to any available model
	if len(tags.Models) > 0 {
		logging.Info("Using first available model for reranking", "model", tags.Models[0].Name)
		return tags.Models[0].Name
	}

	return ""
}

func (r *OllamaReranker) Rerank(ctx context.Context, query string, docs []string) ([]float64, error) {
	return r.RerankWithProgress(ctx, query, docs, nil)
}

func (r *OllamaReranker) RerankWithProgress(ctx context.Context, query string, docs []string, progress func(pct float64, msg string)) ([]float64, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	if !r.Available() {
		return nil, fmt.Errorf("ollama reranker not available (model: %s)", r.model)
	}

	logging.Info("Reranking documents (parallel)", "count", len(docs), "concurrency", r.concurrency, "model", r.model)

	scores := make([]float64, len(docs))
	for i := range scores {
		scores[i] = 0.5 // Default neutral score
	}

	// Semaphore for concurrency control
	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup
	var completed int64

	// Error collection (non-fatal - we continue with neutral scores)
	var errCount int64

	for i, doc := range docs {
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

			score, err := r.scoreOne(ctx, query, document)
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				// Keep default 0.5 score
			} else {
				scores[idx] = score
			}

			done := atomic.AddInt64(&completed, 1)
			if progress != nil && done%10 == 0 {
				progress(float64(done)/float64(len(docs)), fmt.Sprintf("%d of %d", done, len(docs)))
			}
		}(i, doc)
	}

	wg.Wait()

	if progress != nil {
		progress(1.0, fmt.Sprintf("%d of %d", len(docs), len(docs)))
	}

	logging.Info("Reranking complete", "count", len(docs), "errors", errCount)
	return scores, nil
}

// scoreOne scores a single document against the query.
// This is called in parallel - Ollama batches internally on GPU.
func (r *OllamaReranker) scoreOne(ctx context.Context, query, doc string) (float64, error) {
	// Truncate very long documents
	if len(doc) > 300 {
		doc = doc[:300] + "..."
	}

	// Simple prompt for relevance scoring
	prompt := fmt.Sprintf(`Rate the relevance of this headline to the topic.
Topic: %s

Headline: %s

Reply with ONLY a number from 0-10 (10 = highly relevant, 0 = not relevant).
Score:`, query, doc)

	body := map[string]any{
		"model":  r.model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]any{
			"temperature": 0.0, // Deterministic scoring
			"num_predict": 10,  // Just need a single number
		},
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", r.endpoint+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ollama error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("parse response failed: %w", err)
	}

	// Parse score from response
	return parseScore(result.Response), nil
}

// parseScore extracts a numeric score from LLM response.
func parseScore(response string) float64 {
	response = strings.TrimSpace(response)

	// Try to parse as plain number
	if score, err := strconv.ParseFloat(response, 64); err == nil {
		return normalizeScore(score)
	}

	// Try to extract first number from response
	var score float64
	for _, word := range strings.Fields(response) {
		// Remove common suffixes
		word = strings.TrimSuffix(word, "/10")
		word = strings.TrimSuffix(word, ".")
		word = strings.TrimSuffix(word, ",")

		if s, err := strconv.ParseFloat(word, 64); err == nil {
			score = s
			break
		}
	}

	return normalizeScore(score)
}

// normalizeScore converts score to 0-1 range
func normalizeScore(score float64) float64 {
	// Assume 0-10 scale if > 1
	if score > 1 {
		score = score / 10.0
	}
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}
	return score
}
