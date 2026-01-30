// Command obs is the unified CLI for Observer debugging and maintenance.
//
// Usage:
//
//	obs                     Show help
//	obs backfill            Batch embed items missing embeddings
//	obs stats               Pipeline statistics
//	obs stats --db          Pipeline statistics + DB health
//	obs search <query>      Two-stage search pipeline debug
//	obs rerank              Reranker validation (Ollama)
//	obs events              JSONL event log viewer
package main

import (
	"fmt"
	"os"
)

const usage = `obs — Observer debug & maintenance CLI

Usage:
  obs <command> [flags]

Commands:
  backfill    Batch embed items missing embeddings (requires JINA_API_KEY)
  stats       Pipeline statistics and source distribution
  search      Two-stage search pipeline debug (requires JINA_API_KEY)
  rerank      Reranker validation with test headlines (Ollama)
  events      JSONL event log viewer

Environment:
  JINA_API_KEY       Jina AI API key (required for backfill, search)
  JINA_EMBED_MODEL   Embedding model (default: jina-embeddings-v3)
  JINA_RERANK_MODEL  Reranking model (default: jina-reranker-v3)

Run 'obs <command> -h' for command-specific help.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	cmd := os.Args[1]
	// Strip the program name + subcommand so flag sets see only their flags
	os.Args = os.Args[1:]

	switch cmd {
	case "backfill":
		runBackfill()
	case "stats":
		runStats()
	case "search":
		runSearch()
	case "rerank":
		runRerank()
	case "events":
		runEvents()
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "obs: unknown command %q\n\n", cmd)
		fmt.Print(usage)
		os.Exit(1)
	}
}
