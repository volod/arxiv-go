# Setup, host and cross builds, ffmpeg fetch, clean.

##@ Setup
.PHONY: setup env check-go
setup: check-go ## One-time setup: build arxgo, download ffmpeg/ffprobe, create bin/.env, print usage
	@$(MAKE) --no-print-directory build-all
	@$(MAKE) --no-print-directory ffmpeg
	@$(MAKE) --no-print-directory env
	@exe=$(BIN_DIR)/arxgo$(HOST_EXE); \
	printf '%s\n' \
	  '' \
	  'arxgo is ready in $(BIN_DIR)/:' \
	  '  arxgo (Linux amd64), arxgo.exe (Windows amd64),' \
	  "  ffmpeg and ffprobe for this host, and $(BIN_DIR)/.env (optional settings)" \
	  '' \
	  'Configure (optional; flags and environment variables override the file):' \
	  '  1. Edit $(BIN_DIR)/.env and uncomment the settings you need, for example' \
	  '       ARXGO_ARCHIVE=/data/archive' \
	  '       ARXGO_VIDEO_ARCHIVE=/mnt/nas/video' \
	  '     Windows paths: use single quotes or no quotes, e.g. ARXGO_ARCHIVE='"'"'D:\archive'"'"'' \
	  '  2. Keep the file private: it may hold credentials and is git-ignored.' \
	  '' \
	  'Run:' \
	  "  $$exe help                     # operations and flags" \
	  "  $$exe version" \
	  "  $$exe --archive /data/archive  # scan (reads ARXGO_* from $(BIN_DIR)/.env when set)" \
	  "  $$exe split --archive /data/archive --video-archive /mnt/nas/video --dry-run" \
	  '' \
	  'Re-run `make setup` at any time; it never overwrites $(BIN_DIR)/.env.'

check-go:
	@command -v $(GO) >/dev/null 2>&1 || { \
	  echo "error: '$(GO)' not found. Install Go 1.27+ (https://go.dev/dl/) and add its bin directory to PATH," >&2; \
	  echo "       for example: export PATH=/usr/local/go/bin:\$$PATH" >&2; exit 1; }

env: ## Create bin/.env from .env.example unless it already exists (never overwrites)
	@mkdir -p $(BIN_DIR)
	@if [ -e $(BIN_DIR)/.env ]; then \
	  echo "keep existing $(BIN_DIR)/.env (compare with .env.example for new settings)"; \
	else \
	  cp .env.example $(BIN_DIR)/.env && chmod 600 $(BIN_DIR)/.env && \
	  echo "created $(BIN_DIR)/.env from .env.example"; \
	fi

##@ Build
.PHONY: build build-all ffmpeg clean
build: ## Build static Linux amd64 arxgo into bin/
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/arxgo ./cmd/arxgo

build-all: build ## Build static Linux and Windows amd64 arxgo into bin/
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/arxgo.exe ./cmd/arxgo

ffmpeg: ## Download pinned static ffmpeg/ffprobe (linux/windows amd64) into bin/; needs network
	$(SHELL) $(PROJECT_ROOT)/scripts/fetch-ffmpeg.sh $(BIN_DIR) $(PLATFORMS)

clean: ## Remove build and coverage outputs; keeps bin/.env
	rm -rf dist coverage.out coverage.html
	@if [ -d $(BIN_DIR) ]; then find $(BIN_DIR) -mindepth 1 -maxdepth 1 ! -name .env -exec rm -rf {} +; fi
