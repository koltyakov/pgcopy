# pgcopy Makefile

# Variables
BINARY_NAME=pgcopy
VERSION?=1.0.0
COMMIT=$(shell git rev-parse --short HEAD)
DATE=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X github.com/koltyakov/pgcopy/cmd.version=$(VERSION) -X github.com/koltyakov/pgcopy/cmd.commit=$(COMMIT) -X github.com/koltyakov/pgcopy/cmd.date=$(DATE)"

# Default target
.PHONY: all
all: clean build

# Build the binary
.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) .

# Build for multiple platforms
.PHONY: build-all
build-all: clean
	@echo "Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 .
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe .

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	rm -rf bin/

# Run tests
.PHONY: test
test:
	go test -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Format code
.PHONY: fmt
fmt:
	go fmt ./...

# Run linter
.PHONY: lint
lint:
	golangci-lint run

# Install dependencies
.PHONY: deps
deps:
	go mod download
	go mod tidy

# Install the binary
.PHONY: install
install: build
	cp bin/$(BINARY_NAME) /usr/local/bin/

# Uninstall the binary
.PHONY: uninstall
uninstall:
	rm -f /usr/local/bin/$(BINARY_NAME)

# Run the application in development mode
.PHONY: run
run:
	go run . $(ARGS)

# Release targets (requires goreleaser)
.PHONY: release-local
release-local:
	@echo "Building local release..."
	goreleaser build --snapshot --clean

.PHONY: release-test
release-test:
	@echo "Testing release configuration..."
	goreleaser check

.PHONY: release
release:
	@echo "Creating release..."
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required. Usage: make release VERSION=v1.0.0"; exit 1; fi
	./scripts/release.sh $(VERSION)

# Docker targets
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	docker build -t pgcopy:latest .

.PHONY: docker-run
docker-run: docker-build
	@echo "Running Docker container..."
	docker run --rm -it pgcopy:latest

# Security scanning
.PHONY: security-scan
security-scan:
	@echo "Running security scan..."
	gosec ./...

# Full CI check (runs all checks locally)
.PHONY: ci
ci: deps fmt lint test security-scan
	@echo "All CI checks passed!"

# Show help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build           - Build the binary"
	@echo "  build-all       - Build for multiple platforms"
	@echo "  clean           - Clean build artifacts"
	@echo "  test            - Run tests"
	@echo "  test-coverage   - Run tests with coverage"
	@echo "  fmt             - Format code"
	@echo "  lint            - Run linter"
	@echo "  deps            - Install dependencies"
	@echo "  install         - Install the binary to /usr/local/bin"
	@echo "  uninstall       - Remove the binary from /usr/local/bin"
	@echo "  run             - Run the application (use ARGS to pass arguments)"
	@echo "  release-local   - Build local release with goreleaser"
	@echo "  release-test    - Test release configuration"
	@echo "  release         - Create and push release tag (requires VERSION)"
	@echo "  docker-build    - Build Docker image"
	@echo "  docker-run      - Build and run Docker container"
	@echo "  security-scan   - Run security scan with gosec"
	@echo "  ci              - Run all CI checks locally"
	@echo "  help            - Show this help"
