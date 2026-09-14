# Pinned, self-contained Linux and Windows release archives.

##@ Distribution
.PHONY: dist
dist: build-all ffmpeg ## Build and package pinned tools, licences, manuals and checksums; needs network
	$(SHELL) $(PROJECT_ROOT)/scripts/package-dist.sh '$(VERSION)' $(BIN_DIR) dist
