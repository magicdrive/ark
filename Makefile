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

# Run go test with race detector
.PHONY: race
race:
	@$(GO) test -race $(CURDIR)/...

# Run go vet
.PHONY: vet
vet:
	@$(GO) vet $(CURDIR)/...

# Run staticcheck (install with: go install honnef.co/go/tools/cmd/staticcheck@latest)
.PHONY: staticcheck
staticcheck:
	@staticcheck $(CURDIR)/...

# Run all quality gates: vet + test + race + staticcheck
.PHONY: lint
lint: vet test race staticcheck

# Run fuzz smoke tests (short, for local use)
.PHONY: fuzz
fuzz:
	@$(GO) test -run '^$$' -fuzz='^FuzzResolveToolPath$$' -fuzztime=10s $(CURDIR)/internal/mcp/
	@$(GO) test -run '^$$' -fuzz='^FuzzResolveToolPath_PhysicalRoot$$' -fuzztime=10s $(CURDIR)/internal/mcp/
	@$(GO) test -fuzz=FuzzCacheDecoding   -fuzztime=10s $(CURDIR)/internal/cache/
	@$(GO) test -fuzz=FuzzExtract         -fuzztime=10s $(CURDIR)/internal/languages/golang/
	@$(GO) test -run '^$$' -fuzz=FuzzExtractTerraform -fuzztime=10s $(CURDIR)/internal/languages/terraform/
	@$(GO) test -run '^$$' -fuzz='^FuzzTypeArgParser$$' -fuzztime=10s $(CURDIR)/internal/languages/typescript/
	@$(GO) test -run '^$$' -fuzz='^FuzzMatchSymbols$$' -fuzztime=10s $(CURDIR)/internal/search/
	@$(GO) test -run '^$$' -fuzz='^FuzzDeclarationIdentity$$' -fuzztime=10s $(CURDIR)/internal/index/
	@$(GO) test -run '^$$' -fuzz='^FuzzParentIdentity$$' -fuzztime=10s $(CURDIR)/internal/index/
	@$(GO) test -run '^$$' -fuzz='^FuzzIdentityCollision$$' -fuzztime=10s $(CURDIR)/internal/index/

# Run the TypeScript compiler differential against the pinned compiler
# (needs Node.js and npm; CI runs it in its own job)
.PHONY: ts-oracle
ts-oracle:
	@$(CURDIR)/.github/ts-oracle/run.sh

# Run intelligence benchmarks
.PHONY: bench
bench:
	@$(GO) test -bench=. -benchmem $(CURDIR)/internal/index/ $(CURDIR)/internal/context/ $(CURDIR)/internal/resolver/

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
	git ls-files '*.go' | grep -v '/testdata/' | xargs -I GOFILE goreg -w GOFILE

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
	@echo "  make race              - Run go test -race"
	@echo "  make vet               - Run go vet"
	@echo "  make staticcheck       - Run staticcheck"
	@echo "  make lint              - Run all quality gates (vet+test+race+staticcheck)"
	@echo "  make fuzz              - Run fuzz smoke tests (10s each)"
	@echo "  make ts-oracle         - Run the TypeScript compiler differential (Node.js)"
	@echo "  make bench             - Run intelligence benchmarks"
	@echo "  make clean             - Remove build artifacts"
	@echo "  make dev-tools         - Install dev tools"
	@echo "  make goreg             - Execute goreg -w to entire gofile"
	@echo "  make publish tag=<tag> - Publish to github.com"
	@echo "  make help              - Show this message"

