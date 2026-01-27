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

// OllamaProvider implements the Provider interface for local Ollama
type OllamaProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewOllamaProvider creates a new Ollama provider
// If model is empty, it will auto-detect the first available model
func NewOllamaProvider(endpoint, model string) *OllamaProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	return &OllamaProvider{
		endpoint: endpoint,
		model:    model, // Empty is OK - will auto-detect
		client: &http.Client{
			Timeout: 120 * time.Second, // Longer timeout for local inference
		},
	}
}

// getModel returns the configured model or auto-detects one
// Prefers instruct models for structured output tasks
func (o *OllamaProvider) getModel() string {
	if o.model != "" {
		return o.model
	}

	// Auto-detect, preferring instruct models
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", o.endpoint+"/api/tags", nil)
	if err != nil {
		return ""
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	if len(result.Models) == 0 {
		return ""
	}

	// Prefer instruct models for better structured output
	// Priority: small instruct (LFM) > other instruct > fallback
	// Skip large models (qwen 32B etc) that may not fit in GPU
	var fallback, instruct string
	for _, m := range result.Models {
		name := m.Name
		nameLower := strings.ToLower(name)

		if fallback == "" {
			fallback = name
		}

		// Prefer instruct models, prioritize smaller/faster ones
		if strings.Contains(nameLower, "instruct") {
			// If we already have an instruct, prefer smaller LFM models
			if instruct == "" {
				instruct = name
			} else if strings.Contains(nameLower, "lfm") {
				instruct = name // LFM instruct takes priority
			}
		}
	}

	// Pick best available
	var model string
	switch {
	case instruct != "":
		model = instruct
	default:
		model = fallback
	}

	logging.Info("Ollama auto-detected model", "model", model)
	return model
}

func (o *OllamaProvider) Name() string {
	return "ollama"
}

func (o *OllamaProvider) Available() bool {
	// Check if Ollama is running and has at least one model
	model := o.getModel()
	if model == "" {
		logging.Debug("Ollama not available - no models found", "endpoint", o.endpoint)
		return false
	}
	return true
}

func (o *OllamaProvider) Generate(ctx context.Context, req Request) (Response, error) {
	model := o.getModel()
	if model == "" {
		return Response{}, fmt.Errorf("ollama not available at %s (no models)", o.endpoint)
	}

	logging.Debug("Ollama API request starting", "model", model, "endpoint", o.endpoint)

	// Build the request body for Ollama chat API
	messages := []map[string]string{
		{"role": "user", "content": req.UserPrompt},
	}

	body := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   false, // Don't stream for simplicity
	}

	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	// Ollama uses num_predict for max tokens
	if req.MaxTokens > 0 {
		body["options"] = map[string]interface{}{
			"num_predict": req.MaxTokens,
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.endpoint+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return Response{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

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
		logging.Error("Ollama API error", "status", resp.StatusCode, "body", string(respBody))
		return Response{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse Ollama response
	var result struct {
		Model   string `json:"model"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Done bool `json:"done"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return Response{}, fmt.Errorf("failed to parse response: %w", err)
	}

	logging.Debug("Ollama API response parsed",
		"model", result.Model,
		"content_length", len(result.Message.Content),
		"done", result.Done)

	logging.Info("Ollama API raw response",
		"model", result.Model,
		"content_length", len(result.Message.Content),
		"raw_response", string(respBody))

	return Response{
		Content:     result.Message.Content,
		Model:       result.Model,
		RawResponse: string(respBody),
	}, nil
}

// GenerateWithModel generates with a specific model (for two-stage pipelines)
func (o *OllamaProvider) GenerateWithModel(ctx context.Context, model string, req Request) (Response, error) {
	logging.Debug("Ollama API request starting", "model", model, "endpoint", o.endpoint)

	messages := []map[string]string{
		{"role": "user", "content": req.UserPrompt},
	}

	body := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}

	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	if req.MaxTokens > 0 {
		body["options"] = map[string]interface{}{
			"num_predict": req.MaxTokens,
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.endpoint+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return Response{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

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
		return Response{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Model   string `json:"model"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Done bool `json:"done"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return Response{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return Response{
		Content:     result.Message.Content,
		Model:       result.Model,
		RawResponse: string(respBody),
	}, nil
}

// GetTranscriptModel returns the transcript model if available
func (o *OllamaProvider) GetTranscriptModel() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", o.endpoint+"/api/tags", nil)
	if err != nil {
		return ""
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	for _, m := range result.Models {
		if strings.Contains(strings.ToLower(m.Name), "transcript") {
			return m.Name
		}
	}
	return ""
}

// GenerateStream implements StreamingProvider for real-time token streaming
func (o *OllamaProvider) GenerateStream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	model := o.getModel()
	if model == "" {
		return nil, fmt.Errorf("ollama not available at %s (no models)", o.endpoint)
	}

	logging.Debug("Ollama streaming request starting", "model", model, "endpoint", o.endpoint)

	// Build the request body with streaming enabled
	messages := []map[string]string{
		{"role": "user", "content": req.UserPrompt},
	}

	body := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true, // Enable streaming
	}

	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}

	if req.MaxTokens > 0 {
		body["options"] = map[string]interface{}{
			"num_predict": req.MaxTokens,
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.endpoint+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Use a client without timeout for streaming (context handles cancellation)
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

	// Create channel for streaming chunks
	chunks := make(chan StreamChunk, 10)

	// Start goroutine to read streaming response
	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer size for potentially large chunks
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				chunks <- StreamChunk{Error: ctx.Err(), Done: true}
				return
			default:
			}

			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var chunk struct {
				Model   string `json:"model"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done bool `json:"done"`
			}

			if err := json.Unmarshal(line, &chunk); err != nil {
				logging.Debug("Ollama stream parse error", "error", err, "line", string(line))
				continue
			}

			// Send the chunk
			chunks <- StreamChunk{
				Content: chunk.Message.Content,
				Done:    chunk.Done,
				Model:   chunk.Model,
			}

			if chunk.Done {
				logging.Debug("Ollama stream complete", "model", chunk.Model)
				return
			}
		}

		if err := scanner.Err(); err != nil {
			chunks <- StreamChunk{Error: err, Done: true}
		}
	}()

	return chunks, nil
}
