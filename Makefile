# arxiv-go developer entrypoints. Run `make help` for the target list.
SHELL := /bin/bash
.DEFAULT_GOAL := help

GO        ?= go
BIN_DIR   := bin
PKG       := github.com/volod/arxiv-go
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X $(PKG)/internal/cli.version=$(VERSION)
PLATFORMS := linux/amd64 windows/amd64
HOST_EXE  := $(if $(filter windows,$(shell $(GO) env GOOS)),.exe,)

##@ General
.PHONY: help
help: ## List available targets
	@awk 'BEGIN {FS = ":.*## "} /^##@/ {printf "\n%s\n", substr($$0, 5)} /^[a-zA-Z_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

##@ Build
.PHONY: build build-all ffmpeg clean
build: ## Build arxgo for the host into bin/
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/arxgo$(HOST_EXE) ./cmd/arxgo

build-all: ## Cross-compile static arxgo for Linux and Windows amd64
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=$$([ "$$os" = windows ] && echo .exe || true); \
		out=$(BIN_DIR)/arxgo-$$os-$$arch$$ext; echo "build $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $$out ./cmd/arxgo; \
	done

ffmpeg: ## Download pinned static ffmpeg/ffprobe (linux/windows amd64) into bin/; needs network
	$(SHELL) tools/fetch-ffmpeg.sh $(BIN_DIR) $(PLATFORMS)

clean: ## Remove build and coverage outputs
	rm -rf $(BIN_DIR) dist coverage.out coverage.html

##@ Quality
.PHONY: test test-race test-integration fmt fmt-check vet coverage ci
test: ## Run unit tests
	$(GO) test ./...

test-race: ## Run unit tests with the race detector (requires cgo on the host)
	CGO_ENABLED=1 $(GO) test -race ./...

test-integration: ## Run end-to-end tests (build tag integration)
	$(GO) test -tags integration ./test/integration/...

fmt: ## Format Go sources
	gofmt -w $$($(GO) list -f '{{.Dir}}' ./...)

fmt-check: ## Fail when Go sources are not gofmt-formatted
	@out=$$(gofmt -l $$($(GO) list -f '{{.Dir}}' ./...)); \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

vet: ## Run go vet
	$(GO) vet ./...

coverage: ## Write a diagnostic coverage report (never a gate)
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -n 1

ci: fmt-check vet test build-all lint-spec-plan lint-doc-links ## Required checks before accepting a task

##@ Planning
.PHONY: lint-spec-plan lint-doc-links plan-status
lint-spec-plan: ## Check registry, plan and records agree
	$(GO) run ./tools/plancheck lint

lint-doc-links: ## Check relative Markdown links and anchors
	$(GO) run ./tools/plancheck links

plan-status: ## Report open tasks and the next eligible task
	@$(GO) run ./tools/plancheck status
