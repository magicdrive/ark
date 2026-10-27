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
.PHONY: $(PLATFORMS)
$(PLATFORMS):
	@mkdir -p $(BUILD_DIR)
	GOOS=$(word 1,$(subst /, ,$@)) GOARCH=$(word 2,$(subst /, ,$@)) \
		 $(GO) build -o $(BUILD_DIR)/$(word 1,$(subst /, ,$@))-$(word 2,$(subst /, ,$@))/$(BINARY_NAME)\
		 -ldflags "$(LDFLAGS)" .
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

# Run go test with race detector (with cgo, which -race requires on Linux)
.PHONY: race
race:
	@GO111MODULE=on go test -race $(CURDIR)/...

# Run go vet
.PHONY: vet
vet:
	@$(GO) vet $(CURDIR)/...

# Run staticcheck as the lint job of ci.yml does: its version and its
# packages (TestMakefile_StaticcheckMatchesCI keeps them equal).
# staticcheck 2026.2.1 cannot read Go 1.27.2's export data.
STATICCHECK_VERSION = v0.7.0-0.dev.0.20261009230814-452d5bb86b45
STATICCHECK_PACKAGES = \
	./internal/cache/... \
	./internal/context/... \
	./internal/index/... \
	./internal/resolver/... \
	./internal/language/... \
	./internal/languages/... \
	./internal/mcp/... \
	./internal/symbol/... \
	./internal/reference/... \
	./internal/repomap/... \
	./internal/impact/... \
	./internal/search/... \
	./internal/fsroot/... \
	./internal/accesspolicy/... \
	./internal/safewalk/...

.PHONY: staticcheck
staticcheck:
	@cd $(CURDIR) && go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) $(STATICCHECK_PACKAGES)

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
	@$(GO) test -run '^$$' -fuzz='^FuzzExtractRust$$' -fuzztime=10s $(CURDIR)/internal/languages/rust/
	@$(GO) test -run '^$$' -fuzz='^FuzzExtractPython$$' -fuzztime=10s $(CURDIR)/internal/languages/python/
	@$(GO) test -run '^$$' -fuzz='^FuzzDecode$$' -fuzztime=10s $(CURDIR)/internal/textenc/
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

# Release tooling. Hephaestus builds, packages and publishes releases
# (hephaestus.yaml, .github/workflows/release.yml); the procedure is
# docs/development/releasing.md. HEPHAESTUS_VERSION is the version of the
# magicdrive/hephaestus action the workflows pin (release_workflow_test.go
# keeps them equal); Renovate updates both together.
# renovate: datasource=github-tags depName=magicdrive/hephaestus
HEPHAESTUS_VERSION ?= v1.3.0
HEPHAESTUS ?= go run github.com/magicdrive/hephaestus@$(HEPHAESTUS_VERSION)
# renovate: datasource=go depName=golang.org/x/vuln
GOVULNCHECK_VERSION = v1.9.0
# renovate: datasource=go depName=github.com/rhysd/actionlint
ACTIONLINT_VERSION = v1.7.12
# renovate: datasource=npm depName=renovate
RENOVATE_VERSION = 44.178.3

# gofmt over the tracked Go files, as the lint job of ci.yml (testdata holds
# deliberately malformed fixtures)
.PHONY: fmt
fmt:
	@git ls-files '*.go' | grep -v '/testdata/' | xargs gofmt -w

# (git and gofmt are checked: either one failing prints nothing, which must
# not read as "formatted"; xargs exits non-zero when gofmt does)
.PHONY: fmt-check
fmt-check:
	@list=$$(git ls-files '*.go') || exit 1; \
	files=$$(printf '%s\n' "$$list" | grep -v '/testdata/' | xargs gofmt -l) || { echo "fmt-check: gofmt failed"; exit 1; }; \
	if [ -n "$$files" ]; then echo "gofmt found unformatted files:"; echo "$$files"; exit 1; fi

# Known vulnerabilities in the code paths Ark uses (Go modules and stdlib)
.PHONY: vulncheck
vulncheck:
	@cd $(CURDIR) && go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Build ark and run it: the CLI starts and answers --version and --help
.PHONY: smoke
smoke: build
	@$(BUILD_DIR)/$(BINARY_NAME) --version
	@$(BUILD_DIR)/$(BINARY_NAME) --help >/dev/null
	@echo "ok: $(BUILD_DIR)/$(BINARY_NAME) --version, --help"

# Validate hephaestus.yaml (nothing is built or published)
.PHONY: hephaestus-check
hephaestus-check:
	@$(HEPHAESTUS) check

# The release archives of every target, as a snapshot (version
# 0.0.0-SNAPSHOT, nothing is published), verified as the release workflow
# verifies them
.PHONY: snapshot
snapshot:
	@$(HEPHAESTUS) run --snapshot --stages build,archive,checksum --fresh
	@$(CURDIR)/.github/release/verify-dist.sh $(CURDIR)/dist --snapshot

# The release formula on the snapshot archives with the machine's Homebrew:
# style, audit --strict, install, test, uninstall (run make snapshot first;
# installs into the Homebrew prefix and removes it again)
.PHONY: homebrew-e2e
homebrew-e2e:
	@HEPHAESTUS="$(HEPHAESTUS)" $(CURDIR)/.github/release/homebrew-e2e.sh $(CURDIR)/dist

# Everything about a release that can be checked locally, without a tag
.PHONY: release-check
release-check: hephaestus-check snapshot

# Read-only release safety checks before pushing a tag (needs GITHUB_TOKEN
# or GH_TOKEN): make preflight tag=vX.Y.Z
.PHONY: preflight
preflight:
	@if [ -z "$(tag)" ]; then echo "Error: make preflight tag=vX.Y.Z"; exit 1; fi
	@$(HEPHAESTUS) preflight --tag $(tag) --strict

# Lint the GitHub Actions workflows
.PHONY: actionlint
actionlint:
	@cd $(CURDIR) && go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

# Validate renovate.json (needs Node.js and npm)
.PHONY: renovate-check
renovate-check:
	@cd $(CURDIR) && npx --yes --package renovate@$(RENOVATE_VERSION) -- renovate-config-validator --strict renovate.json

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

# Release Ark with `hephaestus publish` (docs/development/releasing.md,
# https://github.com/magicdrive/hephaestus/blob/main/docs/publish.md):
#   make publish v1.2.3          (or VERSION=v1.2.3, or tag=v1.2.3)
#   make publish-check v1.2.3    preflight only
#   make publish-status v1.2.3   what exists for a version
#   make publish-watch v1.2.3    follow an already pushed tag
#   make publish-help
# publish checks, asks, pushes one annotated tag (the release workflow is the
# only publisher), follows the release run and verifies the release. The
# version given as a goal is a no-op goal (.DEFAULT). It reaches hephaestus
# through the environment, never through a shell command line, and hephaestus
# validates it. YES=1 on the command line skips the prompt.
PUBLISH ?= $(HEPHAESTUS) publish
PUBLISH_TARGETS = publish publish-check publish-status publish-watch publish-help

ifneq ($(filter $(PUBLISH_TARGETS),$(MAKECMDGOALS)),)
PUBLISH_GOAL := $(firstword $(MAKECMDGOALS))
PUBLISH_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
ifeq ($(filter $(PUBLISH_TARGETS),$(PUBLISH_GOAL)),)
$(error publish targets come first and alone: make publish v1.2.3)
endif
ifneq ($(filter $(PUBLISH_TARGETS),$(PUBLISH_ARGS)),)
$(error give one publish target at a time)
endif
ifneq ($(word 2,$(PUBLISH_ARGS)),)
$(error one version is expected, got: $(PUBLISH_ARGS))
endif
ifeq ($(PUBLISH_GOAL),publish-help)
ifneq ($(PUBLISH_ARGS),)
$(error publish-help takes no version)
endif
endif
export HEPHAESTUS_PUBLISH_ARGS := $(PUBLISH_ARGS)
export HEPHAESTUS_PUBLISH_VERSION := $(if $(filter command line,$(origin VERSION)),$(VERSION))
export HEPHAESTUS_PUBLISH_TAG := $(if $(filter command line,$(origin tag)),$(tag))
PUBLISH_YES := $(if $(and $(filter command line,$(origin YES)),$(filter 1,$(YES))),-yes)
ifneq ($(PUBLISH_ARGS),)
ifeq ($(filter v0% v1% v2% v3% v4% v5% v6% v7% v8% v9% 0% 1% 2% 3% 4% 5% 6% 7% 8% 9%,$(PUBLISH_ARGS)),)
$(error "$(PUBLISH_ARGS)" is not a version: make $(PUBLISH_GOAL) v1.2.3)
endif
# The version goal has no rule of its own: it is the publish target's
# argument. (No other target starts with v<digit>, so none is shadowed.)
.DEFAULT:
	@:
endif
endif

# Check, tag, push and follow a release
.PHONY: publish
publish:
	@$(PUBLISH) publish -from-make $(PUBLISH_YES)

# Release preflight only (no tag, no push, no GitHub writes)
.PHONY: publish-check
publish-check:
	@$(PUBLISH) check -from-make

# Show what exists for a version (read-only)
.PHONY: publish-status
publish-status:
	@$(PUBLISH) status -from-make

# Follow the release run of an already pushed tag and verify the release
.PHONY: publish-watch
publish-watch:
	@$(PUBLISH) watch -from-make

# Show how releasing works and its safety rules
.PHONY: publish-help
publish-help:
	@$(PUBLISH) help


# Show help
.PHONY: help
help:
	@echo "Makefile commands:"
	@echo "  make build             - Build all artifacts"
	@echo "  make release-build     - Build artifacts for multiple platforms with version info"
	@echo "  make install           - Install application (go install)"
	@echo "  make test              - Run go test"
	@echo "  make test-verbose      - Run go test -v with go clean -testcache"
	@echo "  make race              - Run go test -race"
	@echo "  make vet               - Run go vet"
	@echo "  make staticcheck       - Run staticcheck (the version and packages of CI)"
	@echo "  make lint              - Run all quality gates (vet+test+race+staticcheck)"
	@echo "  make fuzz              - Run fuzz smoke tests (10s each)"
	@echo "  make ts-oracle         - Run the TypeScript compiler differential (Node.js)"
	@echo "  make bench             - Run intelligence benchmarks"
	@echo "  make clean             - Remove build artifacts"
	@echo "  make dev-tools         - Install dev tools"
	@echo "  make goreg             - Execute goreg -w to entire gofile"
	@echo "  make fmt               - Run gofmt -w over the tracked Go files"
	@echo "  make fmt-check         - Fail on unformatted Go files (as CI)"
	@echo "  make vulncheck         - Run govulncheck"
	@echo "  make smoke             - Build ark and run --version and --help"
	@echo "  make hephaestus-check  - Validate hephaestus.yaml"
	@echo "  make snapshot          - Build and verify the release archives (snapshot)"
	@echo "  make homebrew-e2e      - Test the formula on the snapshot with Homebrew"
	@echo "  make release-check     - hephaestus-check + snapshot"
	@echo "  make preflight tag=<tag> - Read-only release checks (strict)"
	@echo "  make actionlint        - Lint the GitHub Actions workflows"
	@echo "  make renovate-check    - Validate renovate.json"
	@echo "  make publish <tag>     - Check, push the tag, follow and verify the release (also tag=<tag>, VERSION=<tag>; YES=1 skips the prompt)"
	@echo "  make publish-check <tag>  - Release preflight only (read-only)"
	@echo "  make publish-status <tag> - What exists for a version (read-only)"
	@echo "  make publish-watch <tag>  - Follow the release run of a pushed tag, then verify"
	@echo "  make publish-help      - How publishing works and its safety rules"
	@echo "  make help              - Show this message"

