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

// GeminiProvider implements the Provider interface for Google's Gemini models
type GeminiProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	if model == "" {
		model = "gemini-3-flash-preview" // Gemini 3 Flash - fast frontier-class
	}
	return &GeminiProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (g *GeminiProvider) Name() string {
	return "gemini"
}

func (g *GeminiProvider) Available() bool {
	return g.apiKey != ""
}

func (g *GeminiProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if !g.Available() {
		logging.Warn("Gemini provider not configured")
		return Response{}, fmt.Errorf("gemini provider not configured")
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048 // Reasonable default for analysis tasks
	}

	logging.Debug("Gemini API request starting", "model", g.model, "max_tokens", maxTokens)

	// Build the request body for Gemini API
	// Gemini uses a different format than OpenAI
	contents := []map[string]interface{}{}

	// Add system instruction if provided
	systemInstruction := map[string]interface{}{}
	if req.SystemPrompt != "" {
		systemInstruction = map[string]interface{}{
			"parts": []map[string]string{
				{"text": req.SystemPrompt},
			},
		}
	}

	// Add user message
	contents = append(contents, map[string]interface{}{
		"role": "user",
		"parts": []map[string]string{
			{"text": req.UserPrompt},
		},
	})

	body := map[string]interface{}{
		"contents": contents,
		"generationConfig": map[string]interface{}{
			"maxOutputTokens": maxTokens,
		},
	}

	if len(systemInstruction) > 0 {
		body["systemInstruction"] = systemInstruction
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Gemini API URL format
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", g.model, g.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return Response{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

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
		logging.Error("Gemini API error", "status", resp.StatusCode, "body", string(respBody))
		return Response{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		ModelVersion string `json:"modelVersion"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return Response{}, fmt.Errorf("failed to parse response: %w", err)
	}

	content := ""
	finishReason := ""
	if len(result.Candidates) > 0 {
		if len(result.Candidates[0].Content.Parts) > 0 {
			content = result.Candidates[0].Content.Parts[0].Text
		}
		finishReason = result.Candidates[0].FinishReason
	}

	modelName := g.model
	if result.ModelVersion != "" {
		modelName = result.ModelVersion
	}

	// Log warning if response was truncated
	if finishReason == "MAX_TOKENS" {
		logging.Warn("Gemini response truncated due to max tokens",
			"model", modelName,
			"max_tokens", maxTokens,
			"content_length", len(content))
	}

	logging.Info("Gemini API response",
		"model", modelName,
		"content_length", len(content),
		"finish_reason", finishReason)

	return Response{
		Content:     content,
		Model:       modelName,
		RawResponse: string(respBody),
	}, nil
}
