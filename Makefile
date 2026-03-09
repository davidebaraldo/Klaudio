.PHONY: build run docker-build test clean tidy migrate frontend embed-docker

# Binary output
BINARY := klaudio
BUILD_DIR := bin

# Go parameters
GOFLAGS := -trimpath
LDFLAGS := -s -w

# Directories
FRONTEND_SRC := web
FRONTEND_BUILD := $(FRONTEND_SRC)/build
EMBED_FRONTEND := cmd/klaudio/frontend
EMBED_DOCKER := cmd/klaudio/docker

## Build the full binary (frontend + Docker context embedded)
build: frontend embed-docker
	@echo "Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/klaudio

## Build Go binary only (no frontend, faster for backend development)
build-backend: embed-docker
	@echo "Building $(BINARY) (backend only)..."
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/klaudio

## Build the SvelteKit frontend
frontend:
	@echo "Building frontend..."
	cd $(FRONTEND_SRC) && npm ci && npm run build
	@echo "Copying frontend build to embed directory..."
	@rm -rf $(EMBED_FRONTEND)
	@mkdir -p $(EMBED_FRONTEND)
	@cp -r $(FRONTEND_BUILD)/* $(EMBED_FRONTEND)/ 2>/dev/null || echo "Warning: frontend build output not found"

## Copy Docker files to embed directory
embed-docker:
	@rm -rf $(EMBED_DOCKER)
	@mkdir -p $(EMBED_DOCKER)
	@cp docker/Dockerfile.agent $(EMBED_DOCKER)/
	@cp docker/entrypoint.sh $(EMBED_DOCKER)/

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
	rm -rf $(EMBED_DOCKER)
	@mkdir -p $(EMBED_FRONTEND) $(EMBED_DOCKER)
	@touch $(EMBED_FRONTEND)/.gitkeep $(EMBED_DOCKER)/.gitkeep
	rm -rf data/klaudio.db

## Run go mod tidy
tidy:
	go mod tidy

migrate: build
	@echo "Migrations are applied automatically on server start."
	@echo "Start the server with: make run"
