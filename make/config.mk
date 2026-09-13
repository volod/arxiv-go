# Shared Make variables.
GO        ?= go
BIN_DIR   := bin
PKG       := github.com/volod/arxiv-go
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X $(PKG)/internal/cli.version=$(VERSION)
PLATFORMS := linux/amd64 windows/amd64
# Recursive (=) so `go env` runs only for targets that use it, such as `make build`.
HOST_EXE   = $(if $(filter windows,$(shell $(GO) env GOOS 2>/dev/null)),.exe,)
