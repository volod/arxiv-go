# Tests, format, vet, coverage, CI, and planning checks.

##@ Quality
.PHONY: test test-race test-integration fmt fmt-check vet vet-windows coverage ci
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

vet-windows: ## Type-check all packages and tests for Windows (Windows runtime tests are not a gate)
	GOOS=windows GOARCH=amd64 $(GO) vet ./...

coverage: ## Write a diagnostic coverage report (never a gate)
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -n 1

ci: fmt-check vet vet-windows test build-all lint-spec-plan lint-doc-links ## Required checks before accepting a task

##@ Planning
.PHONY: lint-spec-plan lint-doc-links plan-status
lint-spec-plan: ## Check registry, plan and records agree
	$(GO) run ./tools/plancheck lint

lint-doc-links: ## Check relative Markdown links and anchors
	$(GO) run ./tools/plancheck links

plan-status: ## Report open tasks and the next eligible task
	@$(GO) run ./tools/plancheck status
