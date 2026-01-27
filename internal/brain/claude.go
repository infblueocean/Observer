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

// ClaudeProvider implements the Provider interface for Anthropic's Claude
type ClaudeProvider struct {
	apiKey    string
	model     string
	client    *http.Client
	webSearch bool // Enable web search tool
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(apiKey, model string) *ClaudeProvider {
	if model == "" {
		model = "claude-sonnet-4-5-20250929" // Claude Sonnet 4.5 - fast, balanced
	}
	return &ClaudeProvider{
		apiKey:    apiKey,
		model:     model,
		webSearch: false, // Disabled for now - tool format needs verification
		client: &http.Client{
			Timeout: 120 * time.Second, // Longer timeout for web search
		},
	}
}

// SetWebSearch enables or disables web search
func (c *ClaudeProvider) SetWebSearch(enabled bool) {
	c.webSearch = enabled
}

func (c *ClaudeProvider) Name() string {
	return "claude"
}

func (c *ClaudeProvider) Available() bool {
	return c.apiKey != ""
}

func (c *ClaudeProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if !c.Available() {
		logging.Warn("Claude provider not configured")
		return Response{}, fmt.Errorf("claude provider not configured")
	}

	logging.Debug("Claude API request starting", "model", c.model)

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048 // Reasonable default for analysis tasks
	}

	// Build the request body
	body := map[string]interface{}{
		"model":      c.model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": req.UserPrompt},
		},
	}

	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	// Enable web search tool for richer analysis
	if c.webSearch {
		body["tools"] = []map[string]interface{}{
			{
				"type": "web_search_20250305",
				"name": "web_search",
			},
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return Response{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logging.Error("Claude API error", "status", resp.StatusCode, "body", string(respBody))
		return Response{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response - handle both text and tool_use content blocks
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
			Name string `json:"name,omitempty"`
		} `json:"content"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return Response{}, fmt.Errorf("failed to parse response: %w", err)
	}

	// Log warning if response was truncated
	if result.StopReason == "max_tokens" {
		logging.Warn("Claude response truncated due to max tokens",
			"model", result.Model,
			"max_tokens", maxTokens)
	}

	logging.Debug("Claude API response parsed",
		"stop_reason", result.StopReason,
		"content_blocks", len(result.Content),
		"model", result.Model)

	// Extract text content from all text blocks
	var textParts []string
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			textParts = append(textParts, block.Text)
		}
	}
	content := ""
	if len(textParts) > 0 {
		content = textParts[0] // Use first text block as main content
		if len(textParts) > 1 {
			// If multiple text blocks, join them
			content = strings.Join(textParts, "\n\n")
		}
	}

	// Log the raw response for debugging and auditing
	logging.Info("Claude API raw response",
		"model", result.Model,
		"content_length", len(content),
		"raw_response", string(respBody))

	return Response{
		Content:     content,
		Model:       result.Model,
		RawResponse: string(respBody),
	}, nil
}

// GenerateStream implements StreamingProvider for real-time token streaming
func (c *ClaudeProvider) GenerateStream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if !c.Available() {
		return nil, fmt.Errorf("claude provider not configured")
	}

	logging.Info("STREAMING: Claude GenerateStream called", "model", c.model)

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}

	// Build the request body with streaming enabled
	body := map[string]interface{}{
		"model":      c.model,
		"max_tokens": maxTokens,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": req.UserPrompt},
		},
	}

	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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

			// Claude SSE format: "event: <type>" followed by "data: <json>"
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				if data == "" {
					continue
				}

				var event struct {
					Type    string `json:"type"`
					Message struct {
						Model string `json:"model"`
					} `json:"message"`
					Delta struct {
						Type       string `json:"type"`
						Text       string `json:"text"`
						StopReason string `json:"stop_reason"`
					} `json:"delta"`
					ContentBlock struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content_block"`
				}

				if err := json.Unmarshal([]byte(data), &event); err != nil {
					logging.Debug("Claude stream parse error", "error", err, "data", data)
					continue
				}

				switch event.Type {
				case "message_start":
					modelName = event.Message.Model
					logging.Debug("STREAMING: Claude message_start", "model", modelName)
				case "content_block_delta":
					if event.Delta.Text != "" {
						logging.Debug("STREAMING: Claude sending chunk", "len", len(event.Delta.Text))
						chunks <- StreamChunk{
							Content: event.Delta.Text,
							Model:   modelName,
						}
					}
				case "message_delta":
					if event.Delta.StopReason != "" {
						logging.Debug("STREAMING: Claude message_delta done", "reason", event.Delta.StopReason)
						chunks <- StreamChunk{
							Done:  true,
							Model: modelName,
						}
						return
					}
				case "message_stop":
					logging.Debug("STREAMING: Claude message_stop")
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
