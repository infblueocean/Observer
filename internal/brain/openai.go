package brain

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		maxTokens = 2048 // Reasonable default for analysis tasks
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
		"model":                o.model,
		"max_completion_tokens": maxTokens,
		"messages":             messages,
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
		logging.Warn("OpenAI response truncated due to max tokens",
			"model", result.Model,
			"max_tokens", maxTokens,
			"content_length", len(content))
	}

	logging.Info("OpenAI API response",
		"model", result.Model,
		"content_length", len(content),
		"finish_reason", finishReason)

	return Response{
		Content:     content,
		Model:       result.Model,
		RawResponse: string(respBody),
	}, nil
}

// GenerateStream implements StreamingProvider for real-time token streaming
func (o *OpenAIProvider) GenerateStream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if !o.Available() {
		return nil, fmt.Errorf("openai provider not configured")
	}

	logging.Debug("OpenAI streaming request starting", "model", o.model)

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
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
		"model":                 o.model,
		"max_completion_tokens": maxTokens,
		"messages":              messages,
		"stream":                true,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	// Use a client without timeout for streaming
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	chunks := make(chan StreamChunk, 10)

	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		var modelName string

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				chunks <- StreamChunk{Error: ctx.Err(), Done: true}
				return
			default:
			}

			line := scanner.Text()

			// OpenAI SSE format: "data: <json>" or "data: [DONE]"
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				chunks <- StreamChunk{
					Done:  true,
					Model: modelName,
				}
				return
			}

			var event struct {
				Model   string `json:"model"`
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}

			if err := json.Unmarshal([]byte(data), &event); err != nil {
				logging.Debug("OpenAI stream parse error", "error", err, "data", data)
				continue
			}

			if modelName == "" && event.Model != "" {
				modelName = event.Model
			}

			if len(event.Choices) > 0 {
				choice := event.Choices[0]
				if choice.Delta.Content != "" {
					chunks <- StreamChunk{
						Content: choice.Delta.Content,
						Model:   modelName,
					}
				}
				if choice.FinishReason == "stop" || choice.FinishReason == "length" {
					chunks <- StreamChunk{
						Done:  true,
						Model: modelName,
					}
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			chunks <- StreamChunk{Error: err, Done: true}
		}
	}()

	return chunks, nil
}
