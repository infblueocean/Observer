package brain

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/abelbrown/observer/internal/httpclient"
)

// Provider configurations

func ClaudeConfig() *ProviderConfig {
	return &ProviderConfig{
		Name:       "claude",
		Endpoint:   "https://api.anthropic.com/v1/messages",
		APIKey:     os.Getenv("ANTHROPIC_API_KEY"),
		Model:      getEnvOr("CLAUDE_MODEL", "claude-sonnet-4-5-20250929"),
		AuthHeader: "x-api-key",
		AuthPrefix: "",
		ExtraHeaders: map[string]string{
			"anthropic-version": "2023-06-01",
		},
		BuildBody:       buildClaudeBody,
		ParseResponse:   parseClaudeResponse,
		ParseStreamLine: parseClaudeStream,
	}
}

func OpenAIConfig() *ProviderConfig {
	return &ProviderConfig{
		Name:            "openai",
		Endpoint:        "https://api.openai.com/v1/chat/completions",
		APIKey:          os.Getenv("OPENAI_API_KEY"),
		Model:           getEnvOr("OPENAI_MODEL", "gpt-4o"),
		AuthHeader:      "Authorization",
		AuthPrefix:      "Bearer ",
		BuildBody:       buildOpenAIBody,
		ParseResponse:   parseOpenAIResponse,
		ParseStreamLine: parseOpenAIStream,
	}
}

func GeminiConfig() *ProviderConfig {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	model := getEnvOr("GEMINI_MODEL", "gemini-2.5-flash")

	return &ProviderConfig{
		Name:     "gemini",
		Endpoint: "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent",
		APIKey:   apiKey,
		Model:    model,
		// Use x-goog-api-key header instead of URL query param (security best practice)
		AuthHeader:      "x-goog-api-key",
		AuthPrefix:      "",
		BuildBody:       buildGeminiBody,
		ParseResponse:   parseGeminiResponse,
		ParseStreamLine: parseGeminiStream,
	}
}

func GrokConfig(useReasoning bool) *ProviderConfig {
	model := getEnvOr("GROK_MODEL", "grok-3-fast")
	name := "grok"
	if useReasoning {
		model = "grok-3"
		name = "grok-reasoning"
	}
	return &ProviderConfig{
		Name:            name,
		Endpoint:        "https://api.x.ai/v1/chat/completions",
		APIKey:          os.Getenv("XAI_API_KEY"),
		Model:           model,
		AuthHeader:      "Authorization",
		AuthPrefix:      "Bearer ",
		BuildBody:       buildOpenAIBody, // Grok uses OpenAI-compatible API
		ParseResponse:   parseOpenAIResponse,
		ParseStreamLine: parseOpenAIStream,
	}
}

func OllamaConfig() *ProviderConfig {
	endpoint := os.Getenv("OLLAMA_HOST")
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}

	// Auto-detect model if not specified
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = detectOllamaModel(endpoint)
	}

	return &ProviderConfig{
		Name:            "ollama",
		Endpoint:        endpoint + "/api/generate",
		Model:           model,
		AuthHeader:      "", // No auth needed
		BuildBody:       buildOllamaBody,
		ParseResponse:   parseOllamaResponse,
		ParseStreamLine: parseOllamaStream,
	}
}

// detectOllamaModel queries Ollama for available models and picks one
func detectOllamaModel(endpoint string) string {
	resp, err := httpclient.Default().Get(endpoint + "/api/tags")
	if err != nil {
		return "" // Will mark provider as unavailable
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

	if len(tags.Models) == 0 {
		return ""
	}

	// Prefer instruct models for better chat/analysis
	for _, m := range tags.Models {
		if strings.Contains(strings.ToLower(m.Name), "instruct") {
			return m.Name
		}
	}

	// Fall back to first available model
	return tags.Models[0].Name
}

// Body builders

func buildClaudeBody(cfg *ProviderConfig, req Request) map[string]any {
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": maxTokensOr(req.MaxTokens, 2048),
		"messages":   []map[string]string{{"role": "user", "content": req.UserPrompt}},
	}
	if req.SystemPrompt != "" {
		body["system"] = req.SystemPrompt
	}
	return body
}

func buildOpenAIBody(cfg *ProviderConfig, req Request) map[string]any {
	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.UserPrompt})

	return map[string]any{
		"model":                 cfg.Model,
		"max_completion_tokens": maxTokensOr(req.MaxTokens, 2048),
		"messages":              messages,
	}
}

func buildGeminiBody(cfg *ProviderConfig, req Request) map[string]any {
	contents := []map[string]any{
		{"role": "user", "parts": []map[string]string{{"text": req.UserPrompt}}},
	}

	body := map[string]any{
		"contents": contents,
		"generationConfig": map[string]any{
			"maxOutputTokens": maxTokensOr(req.MaxTokens, 2048),
		},
	}

	if req.SystemPrompt != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": req.SystemPrompt}},
		}
	}

	return body
}

func buildOllamaBody(cfg *ProviderConfig, req Request) map[string]any {
	prompt := req.UserPrompt
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n" + req.UserPrompt
	}
	return map[string]any{
		"model":  cfg.Model,
		"prompt": prompt,
		"stream": false, // Disable streaming for non-streaming requests
	}
}

// Response parsers

func parseClaudeResponse(body []byte) (string, string, error) {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", err
	}
	var texts []string
	for _, c := range resp.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n\n"), resp.Model, nil
}

func parseOpenAIResponse(body []byte) (string, string, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", err
	}
	if len(resp.Choices) > 0 {
		return resp.Choices[0].Message.Content, resp.Model, nil
	}
	return "", resp.Model, nil
}

func parseGeminiResponse(body []byte) (string, string, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		ModelVersion string `json:"modelVersion"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", err
	}
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		return resp.Candidates[0].Content.Parts[0].Text, resp.ModelVersion, nil
	}
	return "", resp.ModelVersion, nil
}

func parseOllamaResponse(body []byte) (string, string, error) {
	var resp struct {
		Response string `json:"response"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", err
	}
	return resp.Response, resp.Model, nil
}

// Stream parsers

func parseClaudeStream(line string, state *StreamState) (string, bool) {
	data, ok := parseSSEData(line)
	if !ok || data == "" {
		return "", false
	}

	var event struct {
		Type    string `json:"type"`
		Message struct {
			Model string `json:"model"`
		} `json:"message"`
		Delta struct {
			Text       string `json:"text"`
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return "", false
	}

	switch event.Type {
	case "message_start":
		state.Model = event.Message.Model
	case "content_block_delta":
		return event.Delta.Text, false
	case "message_delta":
		if event.Delta.StopReason != "" {
			return "", true
		}
	case "message_stop":
		return "", true
	}
	return "", false
}

func parseOpenAIStream(line string, state *StreamState) (string, bool) {
	data, ok := parseSSEData(line)
	if !ok {
		return "", false
	}
	if data == "[DONE]" {
		return "", true
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
		return "", false
	}

	if state.Model == "" && event.Model != "" {
		state.Model = event.Model
	}

	if len(event.Choices) > 0 {
		choice := event.Choices[0]
		if choice.FinishReason == "stop" || choice.FinishReason == "length" {
			return "", true
		}
		return choice.Delta.Content, false
	}
	return "", false
}

func parseGeminiStream(line string, state *StreamState) (string, bool) {
	// Gemini uses different streaming format
	data, ok := parseSSEData(line)
	if !ok || data == "" {
		return "", false
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
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return "", false
	}

	if len(event.Candidates) > 0 {
		cand := event.Candidates[0]
		if cand.FinishReason == "STOP" {
			return "", true
		}
		if len(cand.Content.Parts) > 0 {
			return cand.Content.Parts[0].Text, false
		}
	}
	return "", false
}

func parseOllamaStream(line string, state *StreamState) (string, bool) {
	// Ollama returns JSON objects, one per line (not SSE format)
	if line == "" {
		return "", false
	}

	var event struct {
		Model    string `json:"model"`
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return "", false
	}

	if state.Model == "" && event.Model != "" {
		state.Model = event.Model
	}

	if event.Done {
		return "", true
	}
	return event.Response, false
}

// Helpers

func getEnvOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func maxTokensOr(v, defaultVal int) int {
	if v > 0 {
		return v
	}
	return defaultVal
}

// CreateAllProviders creates all configured providers
func CreateAllProviders() []*HTTPProvider {
	return CreateAllProvidersWithLogging(nil)
}

// CreateAllProvidersWithLogging creates all configured providers with optional logging
func CreateAllProvidersWithLogging(log func(msg string, args ...any)) []*HTTPProvider {
	configs := []*ProviderConfig{
		ClaudeConfig(),
		OpenAIConfig(),
		GeminiConfig(),
		GrokConfig(false), // Non-reasoning by default
		OllamaConfig(),
	}

	var providers []*HTTPProvider
	for _, cfg := range configs {
		p := NewHTTPProvider(cfg)
		if p.Available() {
			providers = append(providers, p)
			if log != nil {
				log("Provider created", "name", cfg.Name, "model", cfg.Model)
			}
		} else {
			if log != nil {
				// Only log whether key exists, never log key content (security)
				log("Provider skipped - not available", "name", cfg.Name, "has_api_key", cfg.APIKey != "")
			}
		}
	}
	return providers
}

// CreateProviderByName creates a specific provider
func CreateProviderByName(name string) *HTTPProvider {
	var cfg *ProviderConfig
	switch name {
	case "claude":
		cfg = ClaudeConfig()
	case "openai":
		cfg = OpenAIConfig()
	case "gemini":
		cfg = GeminiConfig()
	case "grok":
		cfg = GrokConfig(false)
	case "grok-reasoning":
		cfg = GrokConfig(true)
	case "ollama":
		cfg = OllamaConfig()
	default:
		return nil
	}
	return NewHTTPProvider(cfg)
}
