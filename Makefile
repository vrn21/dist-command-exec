.PHONY: all build test proto clean docker lint local-up local-down

# Default target
all: proto build

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	cd proto && buf generate

# Build all binaries
build:
	@echo "Building binaries..."
	go build -o bin/master ./cmd/master
	go build -o bin/worker ./cmd/worker
	go build -o bin/distctl ./cmd/distctl

# Run tests
test:
	go test -v -race ./...

# Run tests with coverage
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Lint
lint:
	golangci-lint run

# Clean build artifacts
clean:
	rm -rf bin/ gen/ coverage.out coverage.html

# Install buf (if not installed)
install-buf:
	go install github.com/bufbuild/buf/cmd/buf@latest

# Docker images
docker: docker-master docker-worker

docker-master:
	docker build -f Dockerfile.master -t dist-exec-master:latest .

docker-worker:
	docker build -f Dockerfile.worker -t dist-exec-worker:latest .

# Local development with docker-compose
local-up:
	docker-compose up --build

local-down:
	docker-compose down

# Run master locally
run-master:
	MASTER_ADDRESS=:50051 LOG_LEVEL=debug go run ./cmd/master

# Run worker locally
run-worker:
	WORKER_ID=worker-1 WORKER_ADDRESS=:50052 MASTER_ADDRESS=localhost:50051 LOG_LEVEL=debug go run ./cmd/worker

# Tidy dependencies
tidy:
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Vet code
vet:
	go vet ./...

# Check all (pre-commit)
check: fmt vet test

# Help
help:
	@echo "Available targets:"
	@echo "  all          - Generate proto and build (default)"
	@echo "  proto        - Generate protobuf code"
	@echo "  build        - Build all binaries"
	@echo "  test         - Run tests"
	@echo "  lint         - Run linter"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker       - Build Docker images"
	@echo "  local-up     - Start local cluster"
	@echo "  local-down   - Stop local cluster"
	@echo "  run-master   - Run master locally"
	@echo "  run-worker   - Run worker locally"
