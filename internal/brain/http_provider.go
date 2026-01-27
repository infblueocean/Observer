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

// Compile-time interface satisfaction checks
var (
	_ Provider          = (*HTTPProvider)(nil)
	_ StreamingProvider = (*HTTPProvider)(nil)
)

// ProviderConfig defines how to communicate with an LLM API
type ProviderConfig struct {
	Name         string
	Endpoint     string
	APIKey       string // Actual API key (resolved from env)
	Model        string
	AuthHeader   string            // "x-api-key" or "Authorization"
	AuthPrefix   string            // "" or "Bearer "
	ExtraHeaders map[string]string // Additional headers (e.g., anthropic-version)

	// Request building
	BuildBody func(cfg *ProviderConfig, req Request) map[string]any

	// Response parsing
	ParseResponse func(body []byte) (content, model string, err error)

	// Stream parsing (returns content delta, done flag)
	ParseStreamLine func(line string, state *StreamState) (content string, done bool)
}

// StreamState holds state during stream parsing
type StreamState struct {
	Model string
}

// HTTPProvider is a generic HTTP-based LLM provider
type HTTPProvider struct {
	config *ProviderConfig
	client *http.Client
}

// NewHTTPProvider creates a provider from config
func NewHTTPProvider(cfg *ProviderConfig) *HTTPProvider {
	return &HTTPProvider{
		config: cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *HTTPProvider) Name() string {
	return p.config.Name
}

func (p *HTTPProvider) Available() bool {
	// Ollama doesn't need an API key
	if p.config.Name == "ollama" {
		return true
	}
	return p.config.APIKey != ""
}

func (p *HTTPProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if !p.Available() {
		return Response{}, fmt.Errorf("%s provider not configured", p.config.Name)
	}

	logging.Debug("HTTP provider request", "provider", p.config.Name, "model", p.config.Model)

	body := p.config.BuildBody(p.config, req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logging.Error("API error", "provider", p.config.Name, "status", resp.StatusCode, "body", string(respBody))
		return Response{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	content, model, err := p.config.ParseResponse(respBody)
	if err != nil {
		return Response{}, fmt.Errorf("parse response: %w", err)
	}

	logging.Debug("API response", "provider", p.config.Name, "model", model, "content_len", len(content))

	return Response{
		Content:     content,
		Model:       model,
		RawResponse: string(respBody),
	}, nil
}

func (p *HTTPProvider) GenerateStream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if !p.Available() {
		return nil, fmt.Errorf("%s provider not configured", p.config.Name)
	}

	logging.Debug("HTTP provider stream", "provider", p.config.Name, "model", p.config.Model)

	body := p.config.BuildBody(p.config, req)
	body["stream"] = true
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	p.setHeaders(httpReq)

	// No timeout for streaming
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

		state := &StreamState{}

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				chunks <- StreamChunk{Error: ctx.Err(), Done: true}
				return
			default:
			}

			line := scanner.Text()
			if line == "" {
				continue
			}

			content, done := p.config.ParseStreamLine(line, state)
			if content != "" {
				chunks <- StreamChunk{Content: content, Model: state.Model}
			}
			if done {
				chunks <- StreamChunk{Done: true, Model: state.Model}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			chunks <- StreamChunk{Error: err, Done: true}
		}
	}()

	return chunks, nil
}

func (p *HTTPProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")

	if p.config.AuthHeader != "" && p.config.APIKey != "" {
		req.Header.Set(p.config.AuthHeader, p.config.AuthPrefix+p.config.APIKey)
	}

	for k, v := range p.config.ExtraHeaders {
		req.Header.Set(k, v)
	}
}

// Helper for parsing SSE data lines
func parseSSEData(line string) (string, bool) {
	if strings.HasPrefix(line, "data: ") {
		return strings.TrimPrefix(line, "data: "), true
	}
	return "", false
}
