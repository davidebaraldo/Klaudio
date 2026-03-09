.PHONY: build run docker-build test clean tidy migrate

# Binary output
BINARY := klaudio
BUILD_DIR := bin

# Go parameters
GOFLAGS := -trimpath
LDFLAGS := -s -w

build:
	@echo "Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/klaudio

run: build
	@echo "Starting Klaudio server..."
	./$(BUILD_DIR)/$(BINARY)

docker-build:
	@echo "Building klaudio-agent Docker image..."
	docker build -t klaudio-agent -f docker/Dockerfile.agent docker/

test:
	@echo "Running tests..."
	go test ./... -v -count=1

clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -rf data/klaudio.db

tidy:
	go mod tidy

migrate: build
	@echo "Migrations are applied automatically on server start."
	@echo "Start the server with: make run"
