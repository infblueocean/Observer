package rerank

import (
	"math"
	"testing"
)

func TestScoreFromText(t *testing.T) {
	r := &OllamaReranker{}

	tests := []struct {
		response string
		want     float32
	}{
		{"yes", 1.0},
		{"Yes", 1.0},
		{"YES", 1.0},
		{"yes.", 1.0},
		{"yes, the document is relevant", 1.0},
		{"y", 1.0},
		{"true", 1.0},
		{"1", 1.0},
		{"no", 0.0},
		{"No", 0.0},
		{"NO", 0.0},
		{"no.", 0.0},
		{"no, the document is not relevant", 0.0},
		{"n", 0.0},
		{"false", 0.0},
		{"0", 0.0},
		{"maybe", 0.5},
		{"", 0.5},
		{"  ", 0.5},
		{"unclear", 0.5},
		// Numeric scores - the critical bug case
		{"0.0", 0.0},
		{"0.1", 0.1},
		{"0.5", 0.5},
		{"0.85", 0.85},
		{"1.0", 1.0},
		{" 0.0", 0.0},
		{"0.0\n\nThe document is not relevant", 0.0},
		{"0.9 - highly relevant", 0.9},
		{"The relevance score is 0.7", 0.7},
		// Think-wrapped responses (Qwen3-Reranker format)
		{"<think>\nSome reasoning here.\n</think>\n\nyes", 1.0},
		{"<think>\nNot relevant.\n</think>\n\nNo.", 0.0},
		{"<think>\n\n</think>\nyes", 1.0},
		{"<think>\nThe document discusses the Super Bowl which matches the query.\n</think>\n\nyes", 1.0},
		{"<think>\nThis is about cooking, not sports.\n</think>\n\nno", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.response, func(t *testing.T) {
			got := r.scoreFromText(tt.response)
			if math.Abs(float64(got-tt.want)) > 0.01 {
				t.Errorf("scoreFromText(%q) = %v, want %v", tt.response, got, tt.want)
			}
		})
	}
}

func TestNewOllamaReranker_Defaults(t *testing.T) {
	r := NewOllamaReranker("", "test-model")

	if r.endpoint != "http://localhost:11434" {
		t.Errorf("endpoint = %s, want http://localhost:11434", r.endpoint)
	}
	if r.model != "test-model" {
		t.Errorf("model = %s, want test-model", r.model)
	}
	if r.concurrency != 32 {
		t.Errorf("concurrency = %d, want 32", r.concurrency)
	}
}

func TestOllamaReranker_Name(t *testing.T) {
	r := NewOllamaReranker("", "qwen3-reranker:4b")
	if r.Name() != "ollama/qwen3-reranker:4b" {
		t.Errorf("Name() = %s, want ollama/qwen3-reranker:4b", r.Name())
	}

	r2 := &OllamaReranker{}
	if r2.Name() != "ollama/none" {
		t.Errorf("Name() = %s, want ollama/none", r2.Name())
	}
}
