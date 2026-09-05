# bodger - task runner.
#
# These targets are the project's build contract: CI runs `make check-go`
# and `make check-web`, gated on which paths a commit touches - see
# .github/workflows/ci.yml - and nothing else. `make check` runs both
# unconditionally, for local use. If a command isn't in this file, it
# isn't part of the build - see docs/contributing.md.

SHELL       := /bin/bash
BINARY      := bodger
BIN_DIR     := bin
CMD         := ./cmd/bodger
GO_PKGS     := ./...
WEB_DIR     := web
# Where web/vite.config.ts's build.outDir writes to, and what
# internal/platform/webui go:embeds — see that package's doc comment.
# Only WEB_EMBED_DIR/.gitkeep is tracked (an empty marker, so a Go-only
# build still compiles before the web UI has ever been built); everything
# else `make build-web` puts there is generated and gitignored.
WEB_EMBED_DIR := internal/platform/webui/dist
COVERPROFILE:= coverage.out

# Web targets no-op until the web UI exists (milestone 2). Everything stays
# green from the first commit rather than being switched on later.
HAS_WEB     := $(wildcard $(WEB_DIR)/package.json)
# Go targets no-op until there is at least one package to act on (milestone 1).
# Excludes .claude/worktrees: agent-managed parallel worktrees live on disk
# under the main worktree but are separate git worktrees / Go modules, and
# must not make this worktree's own targets fire on their behalf.
HAS_GO      := $(shell find . -name '*.go' -not -path './$(WEB_DIR)/*' -not -path './.claude/worktrees/*' -print -quit 2>/dev/null)

export TZ := UTC

.DEFAULT_GOAL := help

## help: list available targets
.PHONY: help
help:
	@echo "bodger - available targets:"
	@echo
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /' | sort
	@echo

## setup: install toolchain dependencies
.PHONY: setup
setup:
	@command -v asdf >/dev/null 2>&1 && asdf install || echo "asdf not found - install the versions in .tool-versions manually"
	go mod download
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm ci
endif

## setup-hooks: activate the repo's git hooks (run once per clone)
.PHONY: setup-hooks
setup-hooks:
	git config core.hooksPath .githooks
	@echo "git hooks activated from .githooks/"

## fmt: format all code in place
.PHONY: fmt
fmt:
ifneq ($(HAS_GO),)
	gofmt -w -s .
	go run golang.org/x/tools/cmd/goimports@latest -w -local github.com/anirudhgray/bodger .
endif
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run fmt
endif

## fmt-check-go: fail if Go code is unformatted
.PHONY: fmt-check-go
fmt-check-go:
ifneq ($(HAS_GO),)
	@unformatted=$$(gofmt -l -s . | grep -v '^$(WEB_DIR)/' || true); \
	if [ -n "$$unformatted" ]; then \
	  echo "unformatted Go files (run 'make fmt'):"; echo "$$unformatted"; exit 1; \
	fi
endif

## fmt-check-web: fail if web code is unformatted
.PHONY: fmt-check-web
fmt-check-web:
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run fmt:check
endif

## fmt-check: fail if any code is unformatted (CI)
.PHONY: fmt-check
fmt-check: fmt-check-go fmt-check-web

## generate: regenerate generated files (openapi.json, then its frontend types)
.PHONY: generate
generate:
ifneq ($(HAS_GO),)
	go generate $(GO_PKGS)
endif
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run generate
endif

## vet: run go vet
.PHONY: vet
vet:
ifneq ($(HAS_GO),)
	go vet $(GO_PKGS)
endif

## lint-go: run Go linters
.PHONY: lint-go
lint-go:
ifneq ($(HAS_GO),)
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  golangci-lint run; \
	else \
	  echo "golangci-lint not installed - skipping (see docs/contributing.md)"; \
	fi
endif

## lint-web: run web linters
.PHONY: lint-web
lint-web:
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run lint
endif

## lint: run linters
.PHONY: lint
lint: lint-go lint-web

## test-go: run Go tests
.PHONY: test-go
test-go:
ifneq ($(HAS_GO),)
	go test -race -count=1 $(GO_PKGS)
endif

## test-web: run web tests (vitest, api-types freshness, e2e)
.PHONY: test-web
test-web:
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run test
endif

## test: run all tests
.PHONY: test
test: test-go test-web

## test-cover: run Go tests with a coverage profile
.PHONY: test-cover
test-cover:
ifneq ($(HAS_GO),)
	go test -race -count=1 -coverprofile=$(COVERPROFILE) $(GO_PKGS)
	go tool cover -func=$(COVERPROFILE) | tail -1
endif

## build: build the web UI and the bodger binary
.PHONY: build
build: build-web build-bin

## build-bin: build the bodger binary only, without the web UI
.PHONY: build-bin
build-bin:
ifneq ($(HAS_GO),)
	CGO_ENABLED=0 go build -trimpath -o $(BIN_DIR)/$(BINARY) $(CMD)
endif

## build-web: build the web UI into its embedded-assets directory
.PHONY: build-web
build-web:
ifneq ($(HAS_WEB),)
	find $(WEB_EMBED_DIR) -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
	cd $(WEB_DIR) && npm run build
endif

## analyze-web: build the web UI and open a bundle-size treemap
.PHONY: analyze-web
analyze-web:
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run analyze
endif

## run: run the server locally
.PHONY: run
run:
	go run $(CMD) serve

## seed-dev: seed a scratch dev DB with realistic accounts/categories/transactions via the real CLI (scripts/seed-dev.sh; requires BODGER_DB_PATH; pass script flags via ARGS, e.g. `make seed-dev ARGS=--force`)
.PHONY: seed-dev
seed-dev: build-bin
	@./scripts/seed-dev.sh $(ARGS)

## release-dry-run: build every release target locally, no tag or publish
.PHONY: release-dry-run
release-dry-run:
	goreleaser release --snapshot --clean --skip=publish

## check-go: everything CI runs for a Go change - format, vet, lint, test
.PHONY: check-go
check-go: fmt-check-go vet lint-go test-go

## check-web: everything CI runs for a web change - format, lint, test
.PHONY: check-web
check-web: fmt-check-web lint-web test-web

## check: everything CI runs - format, vet, lint, test
.PHONY: check
check: check-go check-web

## docker-build: build the container image
.PHONY: docker-build
docker-build:
	docker build -t $(BINARY):dev .

## docker-up: run the self-hosted stack locally
.PHONY: docker-up
docker-up:
	docker compose up --build

## clean: remove build artefacts
.PHONY: clean
clean:
	rm -rf $(BIN_DIR) dist $(COVERPROFILE)
ifneq ($(HAS_WEB),)
	rm -rf $(WEB_DIR)/node_modules/.vite
	find $(WEB_EMBED_DIR) -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
endif
