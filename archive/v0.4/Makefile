.PHONY: build test test-race lint clean

# Build the main binary
build:
	go build -o observer ./cmd/observer

# Run tests
test:
	go test ./...

# Run tests with race detection
test-race:
	CGO_ENABLED=1 go test -race ./...

# Run linter (if golangci-lint is installed)
lint:
	golangci-lint run || echo "golangci-lint not installed, skipping"

# Clean build artifacts
clean:
	rm -f observer
	go clean ./...

# Run all checks (test + race + lint)
check: test-race lint

# Default target
all: build
