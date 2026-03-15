.PHONY: build run docker-build test clean tidy migrate frontend embed-migrations

# Binary output
BINARY := klaudio
BUILD_DIR := bin

# Version (from git tag, or "dev")
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Go parameters
GOFLAGS := -trimpath
LDFLAGS := -s -w -X main.version=$(VERSION)

# Directories
FRONTEND_SRC := web
FRONTEND_BUILD := $(FRONTEND_SRC)/build
EMBED_FRONTEND := cmd/klaudio/frontend
EMBED_MIGRATIONS := cmd/klaudio/migrations

## Build the full binary (frontend + migrations + Docker context embedded)
build: frontend embed-migrations
	@echo "Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/klaudio

## Build Go binary only (no frontend, faster for backend development)
build-backend: embed-migrations
	@echo "Building $(BINARY) (backend only)..."
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/klaudio

## Copy SQL migrations into embed directory
embed-migrations:
	@echo "Copying migrations to embed directory..."
	@rm -rf $(EMBED_MIGRATIONS)
	@mkdir -p $(EMBED_MIGRATIONS)
	@cp migrations/*.sql $(EMBED_MIGRATIONS)/ 2>/dev/null || echo "Warning: no migration files found"

## Build the SvelteKit frontend
frontend:
	@echo "Building frontend..."
	cd $(FRONTEND_SRC) && npm ci && npm run build
	@echo "Copying frontend build to embed directory..."
	@rm -rf $(EMBED_FRONTEND)
	@mkdir -p $(EMBED_FRONTEND)
	@cp -r $(FRONTEND_BUILD)/* $(EMBED_FRONTEND)/ 2>/dev/null || echo "Warning: frontend build output not found"

## Build and run the server
run: build
	@echo "Starting Klaudio server..."
	./$(BUILD_DIR)/$(BINARY)

## Run in development mode (backend only, frontend via npm run dev)
dev: build-backend
	@echo "Starting Klaudio server (dev mode)..."
	./$(BUILD_DIR)/$(BINARY)

## Build the klaudio-agent Docker image directly (without embedding)
docker-build:
	@echo "Building klaudio-agent Docker image..."
	docker build -t klaudio-agent -f docker/Dockerfile.agent docker/

## Run all tests
test:
	@echo "Running tests..."
	go test ./... -v -count=1

## Remove build artifacts and database
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -rf $(EMBED_FRONTEND)
	@mkdir -p $(EMBED_FRONTEND)
	@touch $(EMBED_FRONTEND)/.gitkeep
	rm -rf $(EMBED_MIGRATIONS)
	@mkdir -p $(EMBED_MIGRATIONS)
	@touch $(EMBED_MIGRATIONS)/.gitkeep
	rm -rf data/klaudio.db

## Run go mod tidy
tidy:
	go mod tidy

migrate: build
	@echo "Migrations are applied automatically on server start."
	@echo "Start the server with: make run"
