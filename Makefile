.PHONY: build clean install test run help completions manpages gendocs docs fmt format-check lint lint-version-check tidy-check vet race vulncheck verify-template corrections-base verify check ci

BINARY_NAME=intuneme
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.builtBy=make -s -w"

# The Go release this module is built with, from go.mod's go line: the only Go
# pin (mise reads the same line). golangci-lint must be built with a Go at
# least this new, or its embedded gofmt and typechecker disagree with the
# toolchain.
GO_VERSION := $(strip $(shell sed -n 's/^go \(.*\)/\1/p' go.mod))
# Pinned golangci-lint release, read from mise.toml — mise.toml is the only pin:
# `mise install` provisions it locally and in CI (jdx/mise-action), verified
# against mise.lock. Bump it there, never here.
GOLANGCI_LINT_VERSION := $(strip $(shell sed -n 's/^golangci-lint = "\(.*\)"/\1/p' mise.toml))
# The major version of the Node on PATH (mise.toml pins it), checked by
# make verify-template so the template gate needs no package install.
NODE_MAJOR := $(shell node -p "process.versions.node.split('.')[0]" 2>/dev/null)

help: ## Display this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build the binary
	@echo "Building $(BINARY_NAME) version $(VERSION)..."
	@go build $(LDFLAGS) -o $(BINARY_NAME) .
	@echo "Build complete: ./$(BINARY_NAME)"

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@go clean

install: build ## Install to /usr/local/bin
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	@sudo cp $(BINARY_NAME) /usr/local/bin/
	@echo "Installed successfully"

test:
	@go test -v ./...


fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...

format-check: ## Fail and print the files when gofmt reports unformatted source
	@files="$$(gofmt -l .)" || exit $$?; \
	if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

# Fail unless the installed golangci-lint is the mise.toml pin and was built
# with a Go no older than go.mod's go line. Only major.minor is compared:
# golangci-lint releases are built with the Go patch current at their release
# (2.15.0 with go1.27.0), while go.mod's go line is a patch-level release
# (1.27.4), and a patch difference does not change gofmt's output.
lint-version-check: ## Fail unless golangci-lint is the mise.toml pin, built with a Go no older than go.mod's
	@test -n "$(GOLANGCI_LINT_VERSION)" || { echo "mise.toml pins no golangci-lint"; exit 1; }
	@installed="$$(golangci-lint version --short 2>/dev/null)" || { \
		echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required (not installed; run: mise install)"; exit 1; }; \
	if [ "$$installed" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "expected golangci-lint $(GOLANGCI_LINT_VERSION), found $$installed (run: mise install)"; \
		exit 1; \
	fi; \
	built="$$(golangci-lint version 2>/dev/null | sed -n 's/.*built with go\([0-9.]*\).*/\1/p')"; \
	built_minor="$$(printf '%s' "$$built" | cut -d. -f1,2)"; \
	module_minor="$$(printf '%s' "$(GO_VERSION)" | cut -d. -f1,2)"; \
	if [ -n "$$built" ] && [ "$$(printf '%s\n%s\n' "$$module_minor" "$$built_minor" | sort -V | head -1)" != "$$module_minor" ]; then \
		echo "golangci-lint $(GOLANGCI_LINT_VERSION) was built with go$$built, older than go.mod's go$(GO_VERSION): bump golangci-lint first"; \
		exit 1; \
	fi

lint: ## Run linter
	@if ! command -v golangci-lint >/dev/null 2>&1; then echo "golangci-lint not found on PATH; run 'mise install'" >&2; exit 1; fi
	@$(MAKE) --no-print-directory lint-version-check
	golangci-lint run

tidy-check: ## Fail unless go.mod and go.sum are tidy
	go mod tidy -diff

vet: ## Run go vet
	go vet ./...

race: ## Run the test suite with the race detector
	go test -v -race ./...

vulncheck: ## Run govulncheck over the module
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

# Check the agent surfaces, docs index, risk paths, corrections and vendored
# copies (needs Node.js 22 or later, from mise.toml)
verify-template: ## Check agent surfaces, docs index, risk paths, corrections and vendored copies (needs Node 22+)
	@if ! command -v node >/dev/null 2>&1; then echo "FAIL: verify-template requires Node.js 22 or later (run: mise install)"; exit 1; fi
	@if [ "$(NODE_MAJOR)" = "" ] || [ "$(NODE_MAJOR)" -lt 22 ]; then echo "FAIL: verify-template requires Node.js 22 or later (found: $$(node --version 2>/dev/null || echo missing)); run: mise install"; exit 1; fi
	node scripts/check-template.mjs
	node --test test/*.test.mjs

# The corrections check compares the record files in .memory/corrections/ with
# BASE: no record is removed, renamed or changed, except that an empty
# promoted_to may be filled in once. The old single file must not come back.
# The contract starts with the gate: a BASE that does not hold
# scripts/check-corrections.mjs had no such rule, so the comparison is skipped
# with a notice. Every BASE after the adoption holds the script.
#
# A BASE that does not resolve to a commit is an error, never an old base: a
# typo or an unfetched ref must not turn the comparison into a silent pass.
corrections-base: ## Check the corrections records against BASE
	@if [ -z "$(BASE)" ]; then exit 0; fi; \
	if ! git rev-parse --verify --quiet "$(BASE)^{commit}" >/dev/null; then \
		echo "FAIL: BASE $(BASE) does not resolve to a commit (fetch it first)"; \
		exit 1; \
	fi; \
	if git cat-file -e "$(BASE):scripts/check-corrections.mjs" 2>/dev/null; then \
		node scripts/check-corrections.mjs --base "$(BASE)"; \
	else \
		echo "==> corrections: BASE $(BASE) predates the corrections gate; append-only comparison skipped"; \
	fi

# The non-mutating, credential-free gate (core ADR-0043): it changes no file,
# so `git status --porcelain` is unchanged before and after a run.
verify: tidy-check vet format-check lint verify-template race ## Run the non-mutating, credential-free gate

# Developer gate: format, then verify
check: fmt verify ## Format, then run the verify gate

# ci.yml runs each prerequisite of this target as its own `make <target>` step
# (core ADR-0073), so a new prerequisite needs a job step in the same change.
ci: verify corrections-base vulncheck build ## Run every CI gate that needs no credentials

run: build ## Build and run
	@./$(BINARY_NAME)

completions: build ## Generate shell completions
	@echo "Generating completions..."
	@mkdir -p completions
	@for sh in bash zsh fish; do \
		./$(BINARY_NAME) completion $$sh > completions/$(BINARY_NAME).$$sh; \
	done

manpages: build ## Generate man pages
	@echo "Generating man pages..."
	@mkdir -p manpages
	@./$(BINARY_NAME) man > manpages/$(BINARY_NAME).1

gendocs: build ## Generate CLI reference markdown for MkDocs
	@echo "Generating CLI reference..."
	@mkdir -p site/reference/cli
	@./$(BINARY_NAME) gendocs site/reference/cli/

docs: completions manpages gendocs ## Generate all documentation

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

bump: ## generate a new version with svu
	@$(MAKE) build
	@$(MAKE) test
	@$(MAKE) fmt
	$(MAKE) lint
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Working directory is not clean. Please commit or stash changes before bumping version."; \
		exit 1; \
	fi
	@echo "Creating new tag..."
	@version=$$(svu next); \
		git tag -a $$version -m "Version $$version"; \
		echo "Tagged version $$version"; \
		echo "Pushing tag $$version to origin..."; \
		git push origin $$version


.DEFAULT_GOAL := help
