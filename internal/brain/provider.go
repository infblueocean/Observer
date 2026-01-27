package brain

import (
	"context"
)

// Provider is the interface for AI providers
type Provider interface {
	// Name returns the provider name (e.g., "claude", "openai")
	Name() string

	// Available returns true if the provider is configured and ready
	Available() bool

	// Generate sends a prompt and returns the response
	Generate(ctx context.Context, req Request) (Response, error)
}

// Request is a prompt request to an AI provider
type Request struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
}

// Response is the AI provider's response
type Response struct {
	Content     string
	Model       string
	RawResponse string // The raw API response body for logging/debugging
	Error       error
}

// StreamChunk is an incremental piece of a streaming response
type StreamChunk struct {
	Content string // Incremental content (append to previous)
	Done    bool   // True when stream is complete
	Error   error  // Non-nil if stream encountered an error
	Model   string // Model name (set on first/last chunk)
}

// StreamingProvider extends Provider with streaming support
type StreamingProvider interface {
	Provider
	// GenerateStream returns a channel that yields content chunks
	// The channel is closed when generation is complete
	// Callers should check chunk.Done and chunk.Error
	GenerateStream(ctx context.Context, req Request) (<-chan StreamChunk, error)
}

// ProviderManager manages multiple AI providers with fallback
type ProviderManager struct {
	providers []Provider
	preferred string // Preferred provider name
}

// NewProviderManager creates a new provider manager
func NewProviderManager() *ProviderManager {
	return &ProviderManager{
		providers: make([]Provider, 0),
	}
}

// AddProvider adds a provider to the manager
func (pm *ProviderManager) AddProvider(p Provider) {
	pm.providers = append(pm.providers, p)
}

// SetPreferred sets the preferred provider by name
func (pm *ProviderManager) SetPreferred(name string) {
	pm.preferred = name
}

// GetAvailable returns the first available provider, preferring the preferred one
func (pm *ProviderManager) GetAvailable() Provider {
	// First try preferred
	if pm.preferred != "" {
		for _, p := range pm.providers {
			if p.Name() == pm.preferred && p.Available() {
				return p
			}
		}
	}

	// Fall back to first available
	for _, p := range pm.providers {
		if p.Available() {
			return p
		}
	}

	return nil
}

// GetByName returns a provider by name
func (pm *ProviderManager) GetByName(name string) Provider {
	for _, p := range pm.providers {
		if p.Name() == name && p.Available() {
			return p
		}
	}
	return nil
}

// ListAvailable returns names of all available providers
func (pm *ProviderManager) ListAvailable() []string {
	var names []string
	for _, p := range pm.providers {
		if p.Available() {
			names = append(names, p.Name())
		}
	}
	return names
}
