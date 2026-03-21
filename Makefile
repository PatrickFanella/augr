# ─── Variables ──────────────────────────────────────────────────────────────
BINARY_NAME   := tradingagent
BUILD_DIR     := ./bin
CMD_DIR       := ./cmd/tradingagent
DOCKER_IMAGE  := tradingagent
DOCKER_TAG    := latest
MIGRATE_DIR   := ./migrations
DB_URL        ?= postgres://tradingagent:tradingagent@localhost:5432/tradingagent?sslmode=disable
COVERAGE_DIR  := ./coverage
GO_MODULE     := github.com/PatrickFanella/get-rich-quick
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS       := -ldflags "-s -w -X main.version=$(VERSION)"

.DEFAULT_GOAL := help

.PHONY: help build run test test-race test-integration test-cover lint fmt vet \
        migrate-up migrate-down migrate-create migrate-status \
        docker-build docker-run dev dev-down dev-logs \
        clean deps tidy check ci \
        vulncheck audit

# ─── Help ───────────────────────────────────────────────────────────────────
## help: list all available targets with descriptions
help:
	@echo ""
	@echo "Trading Agent - Development Commands"
	@echo "─────────────────────────────────────"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | column -t -s ':'
	@echo ""

# ─── Build ──────────────────────────────────────────────────────────────────
## build: compile the Go binary into ./bin/
build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Built $(BUILD_DIR)/$(BINARY_NAME) ($(VERSION))"

## run: build and run the binary
run: build
	$(BUILD_DIR)/$(BINARY_NAME)

# ─── Testing ────────────────────────────────────────────────────────────────
## test: run unit tests (short mode, no integration)
test:
	go test -short -count=1 ./...

## test-race: run unit tests with race detector
test-race:
	go test -short -race -count=1 ./...

## test-integration: run integration tests (requires PostgreSQL)
test-integration:
	go test -count=1 -run Integration ./...

## test-cover: run tests with coverage report
test-cover:
	@mkdir -p $(COVERAGE_DIR)
	go test -short -race -count=1 -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	go tool cover -func=$(COVERAGE_DIR)/coverage.out | tail -1
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "Coverage report: $(COVERAGE_DIR)/coverage.html"

# ─── Code Quality ──────────────────────────────────────────────────────────
## lint: run golangci-lint
lint:
	golangci-lint run ./...

## fmt: format Go source files with gofumpt
fmt:
	gofumpt -w .

## vet: run go vet
vet:
	go vet ./...

## vulncheck: scan for known vulnerabilities in dependencies
vulncheck:
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

## audit: run all code quality checks (fmt check + vet + lint + vulncheck)
audit: vet lint vulncheck
	@gofumpt -d . | head -20 | grep -q . && echo "FAIL: files need formatting (run 'make fmt')" && exit 1 || echo "Formatting: OK"

## check: build + test + lint (quick pre-push verification)
check: build test-race lint
	@echo "All checks passed"

## ci: full CI pipeline locally (build + test + lint + vulncheck)
ci: build test-race lint vulncheck
	@echo "CI pipeline passed"

# ─── Database ──────────────────────────────────────────────────────────────
## migrate-up: apply all pending database migrations
migrate-up:
	@command -v migrate >/dev/null 2>&1 || go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	migrate -path $(MIGRATE_DIR) -database "$(DB_URL)" up

## migrate-down: roll back the last database migration
migrate-down:
	@command -v migrate >/dev/null 2>&1 || go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	migrate -path $(MIGRATE_DIR) -database "$(DB_URL)" down 1

## migrate-create: create a new migration (usage: make migrate-create NAME=add_users_table)
migrate-create:
	@test -n "$(NAME)" || (echo "Usage: make migrate-create NAME=description_here" && exit 1)
	@command -v migrate >/dev/null 2>&1 || go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	migrate create -ext sql -dir $(MIGRATE_DIR) -seq $(NAME)

## migrate-status: show current migration version
migrate-status:
	@command -v migrate >/dev/null 2>&1 || go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	migrate -path $(MIGRATE_DIR) -database "$(DB_URL)" version

# ─── Docker ────────────────────────────────────────────────────────────────
## docker-build: build the Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

## docker-run: build and run in Docker
docker-run: docker-build
	docker run --rm -it --env-file .env $(DOCKER_IMAGE):$(DOCKER_TAG)

## dev: start the full dev environment (app + PostgreSQL + Redis)
dev:
	docker compose up --build -d
	@echo "Services starting... run 'make dev-logs' to follow output"

## dev-down: stop the dev environment
dev-down:
	docker compose down

## dev-logs: follow dev environment logs
dev-logs:
	docker compose logs -f

## dev-restart: restart app container only (keeps DB/Redis running)
dev-restart:
	docker compose restart app

## dev-psql: open a psql shell to the dev database
dev-psql:
	docker compose exec postgres psql -U tradingagent -d tradingagent

# ─── Dependencies ──────────────────────────────────────────────────────────
## deps: download Go module dependencies
deps:
	go mod download

## tidy: tidy Go modules (add missing, remove unused)
tidy:
	go mod tidy

## tools: install development tools
tools:
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "All tools installed"

# ─── Cleanup ───────────────────────────────────────────────────────────────
## clean: remove build artifacts and coverage reports
clean:
	rm -rf $(BUILD_DIR) $(COVERAGE_DIR)
	go clean -cache -testcache
	@echo "Cleaned"
