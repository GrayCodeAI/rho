# Canonical GrayCodeAI Makefile for Go binary repos.
# Source of truth: .shared-templates/Makefile.binary.tmpl at the eco root.
# Placeholders rendered per repo: rho, ..

# ---------------------------------------------------------------------------
# Project metadata
# ---------------------------------------------------------------------------
NAME      := rho
MAIN_PKG  := ./cmd/rho

# ---------------------------------------------------------------------------
# Versioning — sourced from VERSION file; falls back to git describe.
# See https://github.com/GrayCodeAI/rho/blob/main/docs/versioning.md.
# ---------------------------------------------------------------------------
VERSION ?= $(shell v=$$(cat VERSION 2>/dev/null | head -n1 | tr -d '[:space:]'); if [ -n "$$v" ]; then echo "$$v"; else git describe --tags --always --dirty 2>/dev/null || echo "dev"; fi)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.BuildDate=$(DATE)

# ---------------------------------------------------------------------------
# Tooling — pinned, install if missing.
# ---------------------------------------------------------------------------
GOBIN_DIR    := $(shell go env GOPATH)/bin
GOLANGCI     := $(GOBIN_DIR)/golangci-lint
# Keep in sync with the pin in .github/workflows/ci.yml (lint job).
GOLANGCI_VERSION := v2.1.0
GOFUMPT      := $(GOBIN_DIR)/gofumpt
GOIMPORTS    := $(GOBIN_DIR)/goimports
GOVULNCHECK  := $(GOBIN_DIR)/govulncheck
GORELEASER   := $(GOBIN_DIR)/goreleaser

# ---------------------------------------------------------------------------
# Phony declarations (alphabetical).
# ---------------------------------------------------------------------------
.PHONY: all bench boundaries build check-replace ci clean ecosystem-guard feature-boundaries-guard flux-client-guard flux-engine-guard manifest-guard peer-guard internal-layers-guard package-boundaries-guard release-parity cover cover-new fmt help install lint lint-fix \
        release security setup smoke path sync test test-10x test-live test-new test-race tidy version vet api-docs api-validate workspace

check-replace: ## Fail if go.mod has local replace directives (run before tagging)
	@bash scripts/check-no-replace-directives.sh

manifest-guard: ## Validate canonical repository identities and module paths.
	@bash scripts/ecosystem-manifest.sh validate

all: lint test build ## Default — lint, test, build.

# ---------------------------------------------------------------------------
# Build / install / release.
# ---------------------------------------------------------------------------
build: ## Build the binary into bin/$(NAME).
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME) $(MAIN_PKG)

install: ## Install the binary to $GOBIN.
	CGO_ENABLED=0 go install -trimpath -ldflags="$(LDFLAGS)" $(MAIN_PKG)

release: ## Cut a release via goreleaser (requires a clean tree + tag).
	@command -v $(GORELEASER) >/dev/null 2>&1 || (echo "install: go install github.com/goreleaser/goreleaser/v2@latest" && exit 1)
	$(GORELEASER) release --clean

# ---------------------------------------------------------------------------
# Tests.
# ---------------------------------------------------------------------------
test: ## Run unit tests.
	go test ./... -count=1 -timeout=120s

test-race: ## Run unit tests with the race detector.
	go test ./... -race -count=1 -timeout=300s

test-10x: ## Run tests 10 times to surface flakes.
	go test ./... -race -count=10 -timeout=600s

test-new: ## Run only the Round 2 ecosystem packages (fast iteration).
	go test -race -count=1 -timeout=60s ./internal/safewrite/... ./internal/session/... ./internal/permissions/...

test-live: ## Run opt-in live integration tests (requires real LLM credentials).
	@echo "Running live integration tests — requires OPENCODEGO_API_KEY"
	OPENCODEGO_API_KEY=$${OPENCODEGO_API_KEY:-$$(grep -v '^#' .envrc 2>/dev/null | grep OPENCODEGO_API_KEY | head -1 | cut -d= -f2-)} go test -tags=live_test -count=1 -timeout=300s ./cmd/...

cover: ## Generate a coverage report (coverage.out + coverage.html).
	go test ./... -race -coverprofile=coverage.out -covermode=atomic -timeout=180s
	@go tool cover -func=coverage.out | grep "^total:"
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

cover-new: ## Coverage report for Round 2 ecosystem packages only.
	go test -cover -timeout=30s ./internal/safewrite/... ./internal/session/... ./internal/permissions/...

api-docs: ## Generate HTML API reference from OpenAPI spec.
	@command -v redoc-cli >/dev/null 2>&1 || (echo "install: npm install -g redoc-cli" && exit 1)
	redoc-cli bundle api/openapi.yaml -o api/reference.html
	@echo "API reference generated: api/reference.html"

api-validate: ## Validate the OpenAPI spec.
	@command -v redocly >/dev/null 2>&1 || npm install -g @redocly/cli
	redocly lint api/openapi.yaml

bench: ## Run benchmarks.
	go test ./... -bench=. -benchmem -count=3 -timeout=300s

update-golden: ## Regenerate golden test fixtures.
	go test ./cmd/ -run TestGoldenHelp -update-golden -count=1

# ---------------------------------------------------------------------------
# Quality gates.
# ---------------------------------------------------------------------------
fmt: ## Format source files (gofumpt + goimports).
	@command -v $(GOFUMPT)   >/dev/null 2>&1 || (echo "install: go install mvdan.cc/gofumpt@latest"   && exit 1)
	@command -v $(GOIMPORTS) >/dev/null 2>&1 || (echo "install: go install golang.org/x/tools/cmd/goimports@latest" && exit 1)
	@git ls-files -- '*.go' | xargs $(GOFUMPT) -w
	@git ls-files -- '*.go' | xargs $(GOIMPORTS) -w

fmt-check: ## Verify formatting without rewriting files (CI-safe).
	@command -v $(GOFUMPT)   >/dev/null 2>&1 || (echo "install: go install mvdan.cc/gofumpt@latest"   && exit 1)
	@command -v $(GOIMPORTS) >/dev/null 2>&1 || (echo "install: go install golang.org/x/tools/cmd/goimports@latest" && exit 1)
	@out=$$(git ls-files -- '*.go' | xargs $(GOFUMPT) -l); if [ -n "$$out" ]; then echo "gofumpt found unformatted files:"; echo "$$out"; exit 1; fi
	@out=$$(git ls-files -- '*.go' | xargs $(GOIMPORTS) -l); if [ -n "$$out" ]; then echo "goimports found unformatted files:"; echo "$$out"; exit 1; fi

vet: ## Run go vet.
	go vet ./...

ecosystem-guard: ## Fail if external ecosystem repos import rho/internal.
	bash ./scripts/check-ecosystem-boundaries.sh

flux-client-guard: ## Fail on any production flux/client import.
	bash ./scripts/check-flux-client-imports.sh

flux-engine-guard: ## Require all production Flux imports to use the stable engine facade.
	bash ./scripts/check-flux-engine-boundary.sh

peer-guard: ## Fail if support engines import each other instead of depending only on Rho contracts.
	bash ./scripts/check-support-repo-coupling.sh

internal-layers-guard: ## Enforce one-way dependencies across stable Rho internal layers.
	bash ./scripts/check-internal-layer-imports.sh

package-boundaries-guard: ## Enforce AST/package-graph boundaries with file/line diagnostics.
	bash ./scripts/check-package-boundaries.sh

feature-boundaries-guard: ## Prevent feature packages from importing delivery or sibling features.
	bash ./scripts/check-feature-boundaries.sh

boundaries: manifest-guard check-replace ecosystem-guard feature-boundaries-guard flux-client-guard flux-engine-guard peer-guard internal-layers-guard package-boundaries-guard ## Alias for all boundary guards (matches `make boundaries` in engine repos).

release-parity: ## Verify every go.mod ecosystem version resolves to a reachable remote commit.
	bash ./scripts/check-module-release-parity.sh

lint: ## Run golangci-lint.
	@command -v $(GOLANGCI) >/dev/null 2>&1 || (echo "install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)" && exit 1)
	$(GOLANGCI) run ./... --timeout=5m

lint-fix: ## Run golangci-lint with --fix.
	@command -v $(GOLANGCI) >/dev/null 2>&1 || (echo "install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)" && exit 1)
	$(GOLANGCI) run ./... --fix --timeout=5m

security: ## Run govulncheck.
	@command -v $(GOVULNCHECK) >/dev/null 2>&1 || (echo "install: go install golang.org/x/vuln/cmd/govulncheck@latest" && exit 1)
	$(GOVULNCHECK) ./...

tidy: ## Sync workspace modules and verify checksums.
	go work sync
	go mod verify

# ---------------------------------------------------------------------------
# Composite gate used by CI and pre-push.
# ---------------------------------------------------------------------------
ci: tidy fmt-check vet boundaries lint test-race security api-validate ## Run everything CI runs.
	@echo "All CI checks passed."

smoke: ## Quick build + doctor + ecosystem verification.
	./scripts/smoke-rho.sh

path: ## Verify developer path (setup, security, milestone tests).
	./scripts/verify-developer-path.sh

# ---------------------------------------------------------------------------
# Misc.
# ---------------------------------------------------------------------------
version: ## Print the version that will be embedded.
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Date:    $(DATE)"

clean: ## Remove build artefacts.
	rm -rf bin/ dist/ coverage.out coverage.html
	go clean -testcache

# ---------------------------------------------------------------------------
# Setup — bootstrap local development environment.
# ---------------------------------------------------------------------------
workspace: ## Regenerate the ecosystem root go.work from ecosystem.yaml.
	@bash ./scripts/generate-workspace.sh

setup: workspace ## Set up local development environment and development tools.
	@echo "=== Setting up rho development environment ==="
	@echo "✓ go.work generated and synced from ecosystem.yaml"
	@echo ""
	@echo "=== Environment check ==="
	@echo "Go version: $$(go version)"
	@echo "GOPATH:     $$(go env GOPATH)"
	@echo "GOBIN:      $$(go env GOPATH)/bin"
	@echo ""
	@echo "=== Installing development tools ==="
	@command -v $(GOFUMPT)   >/dev/null 2>&1 || go install mvdan.cc/gofumpt@latest || echo "  ⚠ Could not install gofumpt"
	@command -v $(GOIMPORTS) >/dev/null 2>&1 || go install golang.org/x/tools/cmd/goimports@latest || echo "  ⚠ Could not install goimports"
	@command -v $(GOLANGCI)  >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) || echo "  ⚠ Could not install golangci-lint"
	@command -v $(GOVULNCHECK) >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest || echo "  ⚠ Could not install govulncheck"
	@command -v lefthook >/dev/null 2>&1 || go install github.com/evilmartians/lefthook@latest || echo "  ⚠ Could not install lefthook"
	@echo "✓ All tools installed"
	@echo ""
	@echo "=== Installing git hooks ==="
	@lefthook install || echo "  ⚠ lefthook install failed (run 'make hooks' manually)"
	@echo ""
	@echo "=== Setup complete! ==="
	@echo "Run 'make ci' to verify everything works."

help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Compatibility matrix (rho-specific extension to the canonical template).
# Validates compatibility-matrix.json and reports the resolved versions for
# a chosen matrix entry. Wired into the compatibility-test workflow.
# ---------------------------------------------------------------------------
.PHONY: compat-test compat-check compat-drift

compat-test: ## Validate testdata/compatibility-matrix.json and report the 'next' matrix.
	@go run ./cmd/compat-test -matrix=next -file=testdata/compatibility-matrix.json

compat-check: ## Strict validation — non-zero exit if any component lacks a version.
	@go run ./cmd/compat-test -matrix=next -strict -file=testdata/compatibility-matrix.json

compat-drift: ## Advisory: report pin drift between Rho and sibling repositories. Never fails.
	@go run ./cmd/compat-test -check-external -file=testdata/compatibility-matrix.json

.PHONY: hooks sync
hooks: ## Install git hooks via lefthook (formatting, linting, conventional commits).
	@command -v lefthook >/dev/null 2>&1 || (echo "install: go install github.com/evilmartians/lefthook@latest" && exit 1)
	lefthook install

sync: ## Sync the workspace go.work and verify sibling release parity.
	@go work sync
	@bash ./scripts/check-module-release-parity.sh
# === Cross-platform binary targets (add after existing 'build' target) ===

.PHONY: build-all build-static size-check

build-all: ## Build for all platforms (darwin/linux/windows × amd64/arm64)
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME)-darwin-amd64 $(MAIN_PKG)
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME)-darwin-arm64 $(MAIN_PKG)
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME)-linux-amd64 $(MAIN_PKG)
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME)-linux-arm64 $(MAIN_PKG)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME)-windows-amd64.exe $(MAIN_PKG)
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME)-windows-arm64.exe $(MAIN_PKG)

build-static: ## Build fully static binaries for Linux (musl-compatible)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME)-linux-amd64-static $(MAIN_PKG)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(NAME)-linux-arm64-static $(MAIN_PKG)

size-check: build ## Report binary size and warn if over threshold (80MB, matching CI).
	@SIZE=$$(stat -f%z bin/$(NAME) 2>/dev/null || stat -c%s bin/$(NAME) 2>/dev/null); \
	MB=$$(echo "scale=1; $$SIZE / 1048576" | bc); \
	echo "Binary size: $${MB} MB"; \
	if [ $$SIZE -gt 83886080 ]; then echo "::warning::Binary size $${MB} MB exceeds 80 MB threshold (CI gate)"; fi
