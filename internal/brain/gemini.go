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

// GenerateStream implements StreamingProvider for real-time token streaming
func (g *GeminiProvider) GenerateStream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if !g.Available() {
		return nil, fmt.Errorf("gemini provider not configured")
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}

	logging.Debug("Gemini streaming request starting", "model", g.model, "max_tokens", maxTokens)

	// Build the request body
	contents := []map[string]interface{}{}

	systemInstruction := map[string]interface{}{}
	if req.SystemPrompt != "" {
		systemInstruction = map[string]interface{}{
			"parts": []map[string]string{
				{"text": req.SystemPrompt},
			},
		}
	}

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
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Use streamGenerateContent endpoint for streaming
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", g.model, g.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

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

		modelName := g.model

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				chunks <- StreamChunk{Error: ctx.Err(), Done: true}
				return
			default:
			}

			line := scanner.Text()

			// Gemini SSE format: "data: <json>"
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			var event struct {
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

			if err := json.Unmarshal([]byte(data), &event); err != nil {
				logging.Debug("Gemini stream parse error", "error", err, "data", data)
				continue
			}

			if event.ModelVersion != "" {
				modelName = event.ModelVersion
			}

			if len(event.Candidates) > 0 {
				candidate := event.Candidates[0]
				if len(candidate.Content.Parts) > 0 {
					text := candidate.Content.Parts[0].Text
					if text != "" {
						chunks <- StreamChunk{
							Content: text,
							Model:   modelName,
						}
					}
				}
				if candidate.FinishReason == "STOP" || candidate.FinishReason == "MAX_TOKENS" {
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
