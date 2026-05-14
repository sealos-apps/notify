.PHONY: build run clean test lint docker-build docker-push

# Binary name
BINARY_NAME=sealos-notify

# Build variables
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"
IMAGE?=docker.io/sealosio/sealos-notify

# Build the binary
build:
	@echo "Building ${BINARY_NAME}..."
	go build ${LDFLAGS} -o ${BINARY_NAME} .

# Run the application
run: build
	./${BINARY_NAME}

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f ${BINARY_NAME}
	rm -rf bin/
	go clean

# Run tests
test:
	go test -v -race -coverprofile=coverage.out ./...

# Run linter
lint:
	golangci-lint run

# Build Docker image
docker-build:
	docker build -t ${IMAGE}:${VERSION} -t ${IMAGE}:latest .

docker-push:
	docker push ${IMAGE}:${VERSION}
	docker push ${IMAGE}:latest

# Install dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...
	goimports -w .
