# Variables
BINARY_NAME   := get-rich-quick
BUILD_DIR     := ./bin
CMD_DIR       := ./cmd/server
DOCKER_IMAGE  := get-rich-quick
DOCKER_TAG    := latest
MIGRATE_DIR   := ./migrations
DB_URL        ?= postgres://postgres:postgres@localhost:5432/get_rich_quick?sslmode=disable

.DEFAULT_GOAL := help

.PHONY: help build test test-integration lint fmt migrate-up migrate-down docker-build dev clean

## help: list all available targets with descriptions
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | column -t -s ':'

## build: compile the Go binary into ./bin/
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

## test: run unit tests
test:
	go test ./... -short

## test-integration: run integration tests (requires a running PostgreSQL instance)
test-integration:
	go test ./... -run Integration

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## fmt: format Go source files with gofumpt
fmt:
	gofumpt -w .

## migrate-up: apply all pending database migrations
migrate-up:
	migrate -path $(MIGRATE_DIR) -database "$(DB_URL)" up

## migrate-down: roll back the last database migration
migrate-down:
	migrate -path $(MIGRATE_DIR) -database "$(DB_URL)" down 1

## docker-build: build the Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

## dev: start the development environment via docker compose
dev:
	docker compose up --build

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)
