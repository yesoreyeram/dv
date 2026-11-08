# dv — developer entry points. Run `make help` for a list.
SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG        := github.com/yesoreyeram/dv/internal/buildinfo
LDFLAGS    := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).BuildDate=$(BUILD_DATE)
ADDR       ?= 127.0.0.1:8080
FILE_ROOTS ?= samples=$(CURDIR)/deploy/samples

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---------- setup ----------
.PHONY: deps
deps: ## Install Go modules and pnpm dependencies
	go mod download
	pnpm install --frozen-lockfile

## ---------- build ----------
.PHONY: build build-go build-web
build: build-web build-go ## Build the SPA and the server binary (bin/dv)

build-go: ## Build the server binary
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/dv ./cmd/dv

build-web: ## Build the SPA into web/dist
	pnpm --filter web run build

## ---------- run ----------
.PHONY: run run-query dev
run: build ## Build and run the single binary serving API + UI on $(ADDR)
	DV_HTTP_ADDR=$(ADDR) DV_WEB_DIR=web/dist DV_LOG_FORMAT=text DV_FILE_ROOTS=$(FILE_ROOTS) ./bin/dv

run-query: build ## Like run, plus the loopback-only query API over deploy/samples (FR-016, deploy/query-demo.json)
	DV_CONFIG_FILE=deploy/query-demo.json DV_HTTP_ADDR=$(ADDR) DV_WEB_DIR=web/dist DV_LOG_FORMAT=text DV_FILE_ROOTS=$(FILE_ROOTS) ./bin/dv

dev: ## Run the API (go run) and the Vite dev server with HMR (proxying /api)
	@trap 'kill 0' EXIT; \
	DV_HTTP_ADDR=$(ADDR) DV_LOG_FORMAT=text DV_LOG_LEVEL=debug DV_FILE_ROOTS=$(FILE_ROOTS) go run ./cmd/dv & \
	DV_API_URL=http://$(ADDR) pnpm --filter web run dev

## ---------- quality ----------
.PHONY: lint lint-go lint-web fmt check-toolchain lint-workflows
lint: lint-go lint-web check-toolchain lint-workflows ## Run all linters

lint-workflows: ## CI supply-chain policy (NFR-SEC-015) + actionlint when installed
	go test -count=1 -run TestRepositoryWorkflows ./internal/cipolicy/
	@if command -v actionlint >/dev/null 2>&1; then actionlint; \
	else echo "actionlint not installed; CI runs it (see CONTRIBUTING.md)"; fi

check-toolchain: ## mise.toml, go.mod, package.json, Dockerfile and CI agree (NFR-MAINT-007)
	./scripts/check-toolchain.sh

lint-go: ## golangci-lint (includes gosec) + go vet
	go vet ./...
	golangci-lint run ./...

lint-web: ## ESLint, tsc --noEmit and Prettier check
	pnpm -r run lint
	pnpm -r run typecheck
	pnpm run format:check

fmt: ## Format Go and web sources
	gofmt -w cmd internal api test
	pnpm run format

## ---------- tests ----------
.PHONY: test test-go test-web bdd bdd-api bdd-ui e2e bench perf size
test: test-go test-web ## Unit tests (Go with -race, Vitest with coverage)

test-go: ## Go unit tests with race detector and coverage
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -n 1

test-web: ## Vitest unit tests with coverage thresholds
	pnpm --filter web run test

bench: ## Probe benchmarks on one core (NFR-PERF-001 AC3)
	go test -run '^$$' -bench . -benchmem -cpu 1 ./internal/server/

perf: ## Probe latency under 500 rps open-loop load (NFR-PERF-001 AC1/AC2)
	go test -tags perf -count=1 -v -run 'UnderLoad' ./internal/server/

size: build-web ## Initial JS/CSS gzip budget (NFR-PERF-004)
	pnpm --filter web run size

bdd: bdd-api bdd-ui ## All Gherkin suites

bdd-api: ## godog API scenarios (features/api)
	go test -count=1 ./test/bdd/...

bdd-ui: build ## playwright-bdd UI scenarios (features/ui) against bin/dv
	pnpm --filter e2e run test

e2e: bdd-ui ## Alias for bdd-ui

## ---------- security ----------
.PHONY: scan vulncheck audit
scan: vulncheck audit ## Security scans (gosec runs inside lint-go)

vulncheck: ## govulncheck against the Go module
	govulncheck ./...

audit: ## pnpm audit: prod deps fail on low+, all deps on moderate+ (NFR-SEC-010)
	pnpm audit --prod --audit-level low
	pnpm audit --audit-level moderate

## ---------- containers ----------
.PHONY: image compose-up
image: ## Build the OCI image
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t dv:$(VERSION) -f deploy/Dockerfile .

compose-up: ## Run with docker compose
	docker compose -f deploy/docker-compose.yml up --build

## ---------- misc ----------
.PHONY: ci clean
ci: lint test bdd ## Everything CI runs except security scans and image build

clean: ## Remove build outputs
	rm -rf bin web/dist coverage.out web/coverage e2e/.features-gen e2e/test-results e2e/playwright-report
