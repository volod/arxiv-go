# arxiv-go developer entrypoints. Run `make help` for the target list.
SHELL := /bin/bash
PROJECT_ROOT := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

include $(PROJECT_ROOT)/make/config.mk
include $(PROJECT_ROOT)/make/build.mk
include $(PROJECT_ROOT)/make/quality.mk

.DEFAULT_GOAL := help

##@ General
.PHONY: help
help: ## List available targets
	@awk -f "$(PROJECT_ROOT)/make/help.awk" $(MAKEFILE_LIST)
