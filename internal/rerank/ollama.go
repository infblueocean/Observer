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
	"time"

	"github.com/abelbrown/observer/internal/logging"
)

// OllamaReranker uses Ollama to score document relevance.
// Works with any instruction-following model but optimized for reranker models.
type OllamaReranker struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewOllamaReranker creates a reranker using Ollama.
// If model is empty, tries to auto-detect a suitable model.
func NewOllamaReranker(endpoint, model string) *OllamaReranker {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}

	r := &OllamaReranker{
		endpoint: endpoint,
		model:    model,
		client:   &http.Client{Timeout: 120 * time.Second},
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

	logging.Info("Reranking documents", "count", len(docs), "model", r.model)

	// Batch documents to reduce API calls
	// Score in batches of 10 for efficiency
	batchSize := 10
	scores := make([]float64, len(docs))

	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		if progress != nil {
			pct := float64(i) / float64(len(docs))
			progress(pct, fmt.Sprintf("%d of %d", i, len(docs)))
		}

		batchScores, err := r.scoreBatch(ctx, query, batch)
		if err != nil {
			logging.Error("Batch scoring failed", "batch_start", i, "error", err)
			// Fill with neutral scores on error
			for j := i; j < end; j++ {
				scores[j] = 0.5
			}
			continue
		}

		copy(scores[i:end], batchScores)
	}

	if progress != nil {
		progress(1.0, fmt.Sprintf("%d of %d", len(docs), len(docs)))
	}

	logging.Info("Reranking complete", "count", len(docs))
	return scores, nil
}

// scoreBatch scores a batch of documents against the query.
func (r *OllamaReranker) scoreBatch(ctx context.Context, query string, docs []string) ([]float64, error) {
	// Build prompt asking for relevance scores
	var sb strings.Builder
	sb.WriteString("Rate the relevance of each headline to this topic on a scale of 0-10.\n\n")
	sb.WriteString("Topic: ")
	sb.WriteString(query)
	sb.WriteString("\n\nHeadlines:\n")

	for i, doc := range docs {
		// Truncate very long headlines
		headline := doc
		if len(headline) > 200 {
			headline = headline[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, headline))
	}

	sb.WriteString("\nRespond with ONLY the scores, one per line, format: 'N. score'")
	sb.WriteString("\nExample: '1. 8' means headline 1 has relevance score 8")

	prompt := sb.String()

	// Call Ollama
	body := map[string]any{
		"model":  r.model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]any{
			"temperature": 0.1, // Low temperature for consistent scoring
			"num_predict": 100, // Short response expected
		},
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", r.endpoint+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response failed: %w", err)
	}

	// Parse scores from response
	return parseScores(result.Response, len(docs)), nil
}

// parseScores extracts numeric scores from LLM response.
// Handles formats like "1. 8", "1: 8", "1 - 8", etc.
func parseScores(response string, expected int) []float64 {
	scores := make([]float64, expected)
	for i := range scores {
		scores[i] = 0.5 // Default neutral score
	}

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try to parse "N. score" or "N: score" or "N score" format
		var idx int
		var score float64

		// Try different separators
		for _, sep := range []string{".", ":", "-", " "} {
			parts := strings.SplitN(line, sep, 2)
			if len(parts) == 2 {
				if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
					idx = n
					// Extract number from second part
					scoreStr := strings.TrimSpace(parts[1])
					// Handle "8/10" format
					if slashIdx := strings.Index(scoreStr, "/"); slashIdx > 0 {
						scoreStr = scoreStr[:slashIdx]
					}
					if s, err := strconv.ParseFloat(strings.TrimSpace(scoreStr), 64); err == nil {
						score = s
						break
					}
				}
			}
		}

		if idx >= 1 && idx <= expected {
			// Normalize to 0-1 range
			if score > 1 {
				score = score / 10.0
			}
			if score > 1 {
				score = 1
			}
			if score < 0 {
				score = 0
			}
			scores[idx-1] = score
		}
	}

	return scores
}
