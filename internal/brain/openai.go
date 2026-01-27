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

// OpenAIProvider implements the Provider interface for OpenAI's GPT models
type OpenAIProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	if model == "" {
		model = "gpt-5.2" // GPT-5.2 - latest flagship model
	}
	return &OpenAIProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (o *OpenAIProvider) Name() string {
	return "openai"
}

func (o *OpenAIProvider) Available() bool {
	return o.apiKey != ""
}

func (o *OpenAIProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if !o.Available() {
		logging.Warn("OpenAI provider not configured")
		return Response{}, fmt.Errorf("openai provider not configured")
	}

	logging.Debug("OpenAI API request starting", "model", o.model)

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 256
	}

	// Build messages array
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
		"model":      o.model,
		"max_tokens": maxTokens,
		"messages":   messages,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return Response{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logging.Error("OpenAI API error", "status", resp.StatusCode, "body", string(respBody))
		return Response{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Model string `json:"model"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return Response{}, fmt.Errorf("failed to parse response: %w", err)
	}

	content := ""
	if len(result.Choices) > 0 {
		content = result.Choices[0].Message.Content
	}

	logging.Info("OpenAI API response",
		"model", result.Model,
		"content_length", len(content))

	return Response{
		Content:     content,
		Model:       result.Model,
		RawResponse: string(respBody),
	}, nil
}
