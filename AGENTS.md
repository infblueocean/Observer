# Repository Guidelines

## Project Structure & Module Organization
- `cmd/observer/`: main TUI entry point.
- `cmd/obs/`: maintenance/debug CLI (stats, search, backfill, events).
- `internal/`: core packages (coord, embed, fetch, filter, otel, rerank, store, ui).
- `docs/`: design plans and reviews.
- `archive/v0.4/`: legacy codebase; do not build on it unless explicitly instructed.

## Architecture Overview
- `cmd/observer` wires dependencies and injects callbacks into the UI via `ui.AppConfig`.
- The UI only emits `tea.Msg` messages; it never calls external services directly.
- `internal/coord/` runs fetch + embedding loops and notifies the UI on completion.
- Storage is SQLite with embeddings stored as BLOBs; observability events go to `~/.observer/`.

## Build, Test, and Development Commands
- `go build -o observer ./cmd/observer`: build the TUI binary.
- `go build -o obs ./cmd/obs`: build the CLI helper.
- `go test ./...`: run the full test suite.
- `go test -race ./...`: race detector pass (slower, use before large changes).
- `./observer`: run the TUI (requires `JINA_API_KEY`).
- `./obs stats --db`: database health + pipeline counts.

## Coding Style & Naming Conventions
- Go 1.24 project; follow `gofmt` defaults (tabs for indentation).
- Package names are short, lowercase (`coord`, `embed`, `otel`).
- Prefer clear, descriptive identifiers; keep exported names in `UpperCamelCase`.
- Keep UI logic in `internal/ui/`; background work belongs in `internal/coord/`.

## Testing Guidelines
- Tests are Go `_test.go` files alongside packages in `internal/`.
- Use table-driven tests where it improves clarity.
- Name tests by behavior, e.g., `TestCoordinator_StartsWorker`.
- Run targeted tests with `go test -run TestName ./path/to/package`.

## Commit & Pull Request Guidelines
- Commit messages follow a short `area: summary` style (e.g., `docs: update plan`, `feat: add rerank cache`).
- Keep commits focused and scoped to a single change.
- PRs should include: a concise description, linked issues (if any), and test results.
- Include screenshots or terminal captures for TUI/UI changes.

## Configuration & Environment
- `JINA_API_KEY` is required for embeddings and reranking.
- Runtime data lives in `~/.observer/` (SQLite DB, logs, events).
- Clarion is a local module via `go.mod` replace; ensure it is available at `/home/abelbrown/src/clarion`.
