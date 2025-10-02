# Makefile for Astrolabe

# Variables
BINARY_NAME=astrolabe
DOCKER_IMAGE=astrolabe
DOCKER_TAG=latest
GO=go
GOFLAGS=-v

.PHONY: all build test clean docker-build docker-push deploy undeploy run

all: build

# Build the binary
build:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) ./cmd/astrolabe

# Run tests
test:
	$(GO) test -v ./...

# Clean build artifacts
clean:
	rm -rf bin/
	$(GO) clean

# Build Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

# Push Docker image
docker-push: docker-build
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

# Deploy to Kubernetes
deploy:
	kubectl apply -f deploy/deployment.yaml

# Remove from Kubernetes
undeploy:
	kubectl delete -f deploy/deployment.yaml

# Run locally (requires kubeconfig)
run: build
	./bin/$(BINARY_NAME) --in-cluster=false --v=2

# Run with custom label selector
run-all: build
	./bin/$(BINARY_NAME) --in-cluster=false --label-selector="" --v=2

# Run with persistence (requires Redis)
run-persistent: build
	./bin/$(BINARY_NAME) --in-cluster=false --enable-persistence=true --redis-addr=localhost:6379 --v=2

# Start with Docker Compose (includes Redis)
up:
	docker-compose up -d

# Stop Docker Compose
down:
	docker-compose down

# View Docker Compose logs
logs:
	docker-compose logs -f astrolabe

# Restart Docker Compose
restart:
	docker-compose restart astrolabe

# Format code
fmt:
	$(GO) fmt ./...

# Lint code
lint:
	golangci-lint run

# Generate code (if needed)
generate:
	$(GO) generate ./...

# Install dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Help
help:
	@echo "Available targets:"
	@echo "  build           - Build the binary"
	@echo "  test            - Run tests"
	@echo "  clean           - Clean build artifacts"
	@echo "  docker-build    - Build Docker image"
	@echo "  docker-push     - Push Docker image"
	@echo "  deploy          - Deploy to Kubernetes"
	@echo "  undeploy        - Remove from Kubernetes"
	@echo "  run             - Run locally with Helm filter"
	@echo "  run-all         - Run locally without filters"
	@echo "  run-persistent  - Run locally with Redis persistence"
	@echo "  up              - Start with Docker Compose (Redis + Astrolabe)"
	@echo "  down            - Stop Docker Compose"
	@echo "  logs            - View Docker Compose logs"
	@echo "  restart         - Restart Docker Compose"
	@echo "  fmt             - Format code"
	@echo "  lint            - Lint code"
	@echo "  deps            - Download and tidy dependencies"
