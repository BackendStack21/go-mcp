.PHONY: all build test test-verbose vet fmt tidy clean install examples

# Default target
all: vet test build

# Build everything
build: examples
	go build ./...

# Build example binaries
examples:
	go build -o bin/greet ./examples/greet/
	go build -o bin/sys-monitor ./examples/sys-monitor/
	go build -o bin/fs-navigator ./examples/fs-navigator/

# Run tests (quiet)
test:
	go test ./gomcp/ -count=1

# Run tests with verbose output
test-verbose:
	go test ./gomcp/ -v -count=1

# Run tests with race detector
test-race:
	go test ./gomcp/ -race -count=1

# Run tests with coverage
test-cover:
	go test ./gomcp/ -coverprofile=coverage.out
	go tool cover -func=coverage.out

# Static analysis
vet:
	go vet ./...

# Format code
fmt:
	go fmt ./...

# Tidy module dependencies
tidy:
	go mod tidy

# Clean built artifacts
clean:
	rm -rf bin/
	rm -f coverage.out

# Install the package
install:
	go install ./...

# Shortcut: full CI pipeline
ci: fmt vet test build

# Docker E2E test — validates full MCP protocol
e2e-docker:
	bash test/e2e-docker.sh

# Full E2E pipeline (unit tests + Docker + OpenCode docs)
e2e:
	bash test/e2e-all.sh

# OpenCode integration setup
e2e-opencode:
	docker exec -i projects-dev bash -c "export PATH=\$$PATH:/usr/local/go/bin && cd /workspace/go-mcp && go build -o /tmp/greeter-mcp ./examples/greet/"
	@echo "MCP server built at /tmp/greeter-mcp (inside container)"
	@echo "Run: opencode mcp add  →  Name: greeter  →  Command: /tmp/greeter-mcp"
	@echo "Then: opencode run 'Use the greet tool to say hello to OpenCode'"

