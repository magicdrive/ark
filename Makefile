# Makefile for ark

# Variables
BUILD_DIR = $(CURDIR)/build
CMD_DIRS = $(wildcard cmd/*)
BINARY_NAME = ark
VERSION = $(shell git describe --tags --always)
LDFLAGS += -X "main.version=$(VERSION)"

PLATFORMS := linux/amd64 darwin/amd64 windows/amd64
GO := GO111MODULE=on CGO_ENABLED=0 go

# Argments
tag =

# Default target
.PHONY: all
all: help

# Build all artifacts
.PHONY: build
build: clean
	@mkdir -p $(BUILD_DIR)
	@$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) -ldflags "$(LDFLAGS)"
	@chmod 755 $(BUILD_DIR)/$(BINARY_NAME)

# Build artifacts for all platforms and release
.PHONY: release-build
release-build: clean $(PLATFORMS)
	@echo "Release files are created in the $(BUILD_DIR) directory."

# Build each platform
$(PLATFORMS):
	@mkdir -p $(BUILD_DIR)
	GOOS=$(word 1,$(subst /, ,$@)) GOARCH=$(word 2,$(subst /, ,$@)) \
		 $(GO) build -o $(BUILD_DIR)/$(word 1,$(subst /, ,$@))-$(word 2,$(subst /, ,$@))/$(BINARY_NAME)\
		 -ldflags "$(LDFAGS)" .
	chmod 755 $(BUILD_DIR)/$(word 1,$(subst /, ,$@))-$(word 2,$(subst /, ,$@))/$(BINARY_NAME)

# Run go test for each directory
.PHONY: test
test:
	@$(GO) test $(CURDIR)/...

# Run go test with verbose output and clear test cache
.PHONY: test-verbose
test-verbose:
	@$(GO) clean -testcache
	@$(GO) test -v $(CURDIR)/...

# Install application. Use `go install`
.PHONY: install
install:
	@echo "Installing ark..."
	@$(GO) install -ldflags "$(LDFLAGS)"

# Clean build artifacts
.PHONY: clean
clean:
	@rm -rf $(BUILD_DIR)

# Install dev tools
.PHONY: dev-tools
dev-tools:
	go install "github.com/magicdrive/goreg@latest"
	go install "github.com/magicdrive/kirke@latest"

# Execute goreg -w to entire gofile.
.PHONY: goreg
goreg:
	git ls-files | grep -e '.go$$' | xargs -I GOFILE goreg -w GOFILE

# Publish to github.com
.PHONY: publish
publish: test-verbose
	@if [ -z "$(tag)" ]; then \
		echo "Error: version is not set. Please set it and try again."; \
		echo "ex) make publish tag=v0.0.1"; \
		echo ; \
		echo ; \
		exit 1; \
		fi
	git tag $(tag)
	git push origin $(tag)


# Show help
.PHONY: help
help:
	@echo "Makefile commands:"
	@echo "  make build             - Build all artifacts"
	@echo "  make release-build     - Build artifacts for multiple platforms with version info"
	@echo "  make install           - Install application. Use `go install`"
	@echo "  make test              - Run go test"
	@echo "  make test-verbose      - Run go test -v with go clean -testcache"
	@echo "  make clean             - Remove build artifacts"
	@echo "  make dev-tools         - Install dev tools"
	@echo "  make goreg             - Execute goreg -w to entire gofile"
	@echo "  make publish tag=<tag> - Publish to github.com"
	@echo "  make help              - Show this message"

