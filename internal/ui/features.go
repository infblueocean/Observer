package ui

// Features gates optional functionality. All default to false.
// Note: F2 (Ollama rerank) is not feature-gated — it's controlled by
// AutoReranks and rerankerAvailable() based on the wired backend.
type Features struct {
	MLT           bool // Feature 1: "More Like This"
	FTS5          bool // Feature 7: Full-text search
	SearchHistory bool // Feature 5+9: Search history + pinned views
	ScoreColumn   bool // Feature 6: Score transparency
	JinaReader    bool // Feature 10: Article reader
}
