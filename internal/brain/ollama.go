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
func (o *OllamaProvider) getModel() string {
	if o.model != "" {
		return o.model
	}

	// Auto-detect first available model
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

	if len(result.Models) > 0 {
		model := result.Models[0].Name
		logging.Info("Ollama auto-detected model", "model", model)
		return model
	}

	return ""
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
