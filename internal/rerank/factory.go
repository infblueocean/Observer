package rerank

import (
	"os"

	"github.com/abelbrown/observer/internal/logging"
)

// NewReranker creates a reranker based on available backends.
// Tries Ollama first, returns nil if no backend available.
func NewReranker() Reranker {
	// Try Ollama
	endpoint := os.Getenv("OLLAMA_HOST")
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}

	model := os.Getenv("RERANKER_MODEL")
	// If no specific reranker model, let OllamaReranker auto-detect

	ollama := NewOllamaReranker(endpoint, model)
	if ollama.Available() {
		logging.Info("Reranker initialized", "backend", "ollama", "model", ollama.model)
		return ollama
	}

	logging.Warn("No reranker backend available")
	return nil
}

// NewRerankerWithConfig creates a reranker with explicit configuration.
func NewRerankerWithConfig(backend, endpoint, model string) Reranker {
	switch backend {
	case "ollama", "":
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		return NewOllamaReranker(endpoint, model)
	default:
		logging.Warn("Unknown reranker backend", "backend", backend)
		return nil
	}
}
