# bodger - task runner.
#
# These targets are the project's build contract: CI runs `make check` and
# `make build`, and nothing else. If a command isn't in this file, it isn't
# part of the build - see docs/contributing.md.

SHELL       := /bin/bash
BINARY      := bodger
BIN_DIR     := bin
CMD         := ./cmd/bodger
GO_PKGS     := ./...
WEB_DIR     := web
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

## fmt-check: fail if any code is unformatted (CI)
.PHONY: fmt-check
fmt-check:
ifneq ($(HAS_GO),)
	@unformatted=$$(gofmt -l -s . | grep -v '^$(WEB_DIR)/' || true); \
	if [ -n "$$unformatted" ]; then \
	  echo "unformatted Go files (run 'make fmt'):"; echo "$$unformatted"; exit 1; \
	fi
endif
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run fmt:check
endif

## vet: run go vet
.PHONY: vet
vet:
ifneq ($(HAS_GO),)
	go vet $(GO_PKGS)
endif

## lint: run linters
.PHONY: lint
lint:
ifneq ($(HAS_GO),)
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  golangci-lint run; \
	else \
	  echo "golangci-lint not installed - skipping (see docs/contributing.md)"; \
	fi
endif
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run lint
endif

## test: run all tests
.PHONY: test
test:
ifneq ($(HAS_GO),)
	go test -race -count=1 $(GO_PKGS)
endif
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run test
endif

## test-cover: run Go tests with a coverage profile
.PHONY: test-cover
test-cover:
ifneq ($(HAS_GO),)
	go test -race -count=1 -coverprofile=$(COVERPROFILE) $(GO_PKGS)
	go tool cover -func=$(COVERPROFILE) | tail -1
endif

## build: build the web UI and the bodger binary
.PHONY: build
build: build-web
ifneq ($(HAS_GO),)
	CGO_ENABLED=0 go build -trimpath -o $(BIN_DIR)/$(BINARY) $(CMD)
endif

## build-web: build the web UI into its embedded-assets directory
.PHONY: build-web
build-web:
ifneq ($(HAS_WEB),)
	cd $(WEB_DIR) && npm run build
endif

## run: run the server locally
.PHONY: run
run:
	go run $(CMD) serve

## check: everything CI runs - format, vet, lint, test
.PHONY: check
check: fmt-check vet lint test

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
	rm -rf $(WEB_DIR)/dist $(WEB_DIR)/node_modules/.vite
endif
