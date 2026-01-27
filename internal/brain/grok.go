package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/abelbrown/observer/internal/logging"
)

// GrokProvider implements the Provider interface for xAI's Grok models
type GrokProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGrokProvider creates a new Grok provider
func NewGrokProvider(apiKey, model string) *GrokProvider {
	if model == "" {
		model = "grok-4-1-fast-non-reasoning" // Grok 4.1 Fast - instant responses
	}
	return &GrokProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (g *GrokProvider) Name() string {
	return "grok"
}

func (g *GrokProvider) Available() bool {
	return g.apiKey != ""
}

func (g *GrokProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if !g.Available() {
		logging.Warn("Grok provider not configured")
		return Response{}, fmt.Errorf("grok provider not configured")
	}

	logging.Debug("Grok API request starting", "model", g.model)

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048 // Reasonable default for analysis tasks
	}

	// Grok uses OpenAI-compatible API format
	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": req.UserPrompt,
	})

	body := map[string]interface{}{
		"model":      g.model,
		"max_tokens": maxTokens,
		"messages":   messages,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// xAI API endpoint
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.x.ai/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return Response{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logging.Error("Grok API error", "status", resp.StatusCode, "body", string(respBody))
		return Response{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Grok uses OpenAI-compatible response format
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Model string `json:"model"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return Response{}, fmt.Errorf("failed to parse response: %w", err)
	}

	content := ""
	finishReason := ""
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
		finishReason = result.Choices[0].FinishReason
	}

	// Log warning if response was truncated
	if finishReason == "length" {
		logging.Warn("Grok response truncated due to max tokens",
			"model", result.Model,
			"max_tokens", maxTokens,
			"content_length", len(content))
	}

	logging.Info("Grok API response",
		"model", result.Model,
		"content_length", len(content),
		"finish_reason", finishReason)

	return Response{
		Content:     content,
		Model:       result.Model,
		RawResponse: string(respBody),
	}, nil
}
