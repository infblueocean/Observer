# Observer Project

## Project Overview

**Observer** is an ambient news aggregation Terminal User Interface (TUI) application built with **Go 1.24**. It aggregates content from over 200 RSS/API sources (via the local `Clarion` library), embeds them using Jina AI, stores them in a local SQLite database, and presents them in a Bubble Tea interface.

**Core Philosophy:** Radical transparency. No hidden algorithms; all filters are visible and adjustable by the user.

## Architecture

The project follows a clean architecture with a clear separation of concerns:

*   **`cmd/observer/`**: Main entry point. Wires dependencies (store, embedder, etc.) and injects them as callbacks into the UI.
*   **`cmd/obs/`**: Unified CLI for maintenance and debugging (stats, search, backfill, events).
*   **`internal/ui/`**: Bubble Tea TUI components. **Crucially, the UI has no direct dependencies on services.** It interacts solely via `tea.Msg` and injected callbacks.
*   **`internal/coord/`**: Coordinator pattern. Manages background fetch loops (5-min interval) and embedding workers (2-sec interval).
*   **`internal/store/`**: Persistence layer using pure-Go SQLite (`modernc.org/sqlite`). Stores items and their vector embeddings (as BLOBs).
*   **`internal/embed/`**: Interfaces for embedding services. Supports Jina AI (production, batched) and Ollama (local/dev).
*   **`internal/fetch/`**: Wraps the `Clarion` library for fetching from RSS/API sources.
*   **`internal/otel/`**: Structured observability system (async JSONL logger, ring buffer).

## Key Workflows

1.  **Fetching:** The coordinator runs a fetch loop. Currently sequential (moving to parallel in v0.7). New items are saved to SQLite.
2.  **Embedding:** A background worker polls for items with `NULL` embeddings and processes them in batches (if supported, e.g., Jina).
3.  **UI Loading:** Two-stage load for perceived performance:
    *   **Stage 1:** Load recent (1h) unread items.
    *   **Stage 2:** Load full (24h) corpus.
4.  **Search:** Two-stage pipeline:
    *   **Fast:** Cosine similarity search on embeddings.
    *   **Precise:** Cross-encoder reranking (Jina) for top results.

## Build and Run

### Prerequisites
*   Go 1.24+
*   `JINA_API_KEY` environment variable (required for embedding/search).

### Commands

*   **Build TUI:**
    ```bash
    go build -o observer ./cmd/observer
    ```
*   **Build Debug CLI:**
    ```bash
    go build -o obs ./cmd/obs
    ```
*   **Run TUI:**
    ```bash
    ./observer
    ```
*   **Run Tests:**
    ```bash
    go test ./...
    go test -race ./...  # Recommended before pushing
    ```
*   **Debug/Maintenance:**
    ```bash
    ./obs stats --db        # Check DB health
    ./obs events --tail 20  # View recent logs
    ./obs search "query"    # Debug search pipeline
    ```

## Development Conventions

*   **Code Style:** Standard Go 1.24 formatting.
*   **Dependencies:** `Clarion` is a local module replacement (`replace` directive in `go.mod`). Ensure it exists at `/home/abelbrown/src/clarion`.
*   **Testing:**
    *   Tests coexist with code (`_test.go`).
    *   Naming: `TestComponent_Behavior` (e.g., `TestCoordinator_StartsWorker`).
    *   Use table-driven tests for logic.
*   **Commits:** Follow `area: summary` format (e.g., `feat: add parallel fetch`, `fix: ui rendering`).
*   **No "Magic" in UI:** The UI must remain a pure view/controller. All heavy lifting happens in `internal/coord` or via callbacks.

## Directory Structure Overview

*   `archive/v0.4/`: Legacy code. **Do not use.**
*   `cmd/`: Entry points.
*   `docs/`: detailed implementation plans and architectural reviews.
*   `internal/`: Application code.
*   `AGENTS.md` & `CLAUDE.md`: Context for AI agents.

## Future Plans (v0.7+)
*   Transitioning `fetchAll` to use `errgroup` for parallel source fetching.
*   Maintaining strict separation of UI and backend logic.
