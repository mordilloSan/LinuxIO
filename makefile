GO_VERSION      = 1.24
NODE_VERSION    = 22
GO_INSTALL_DIR := $(HOME)/.go
# Use NVM if present; otherwise fall back to whatever Node is on PATH (CI).
NVM_SETUP = export NVM_DIR="$$HOME/.nvm"; \
            if [ -s "$$NVM_DIR/nvm.sh" ]; then \
              . "$$NVM_DIR/nvm.sh"; \
              nvm use $(NODE_VERSION) >/dev/null 2>&1 || true; \
            fi
GO_BIN := $(shell which go)
GOLANGCI_LINT := $(shell command -v golangci-lint || echo $(GO_INSTALL_DIR)/bin/golangci-lint)

# Flags to pass into the Go binaries
VERBOSE ?= true
VERBOSE_FLAG := $(if $(filter true 1 yes on,$(VERBOSE)),--verbose,)
VITE_DEV_PORT = 3000
SERVER_PORT = 8080

# Colors
COLOR_RESET  := \033[0m
COLOR_BLUE   := \033[1;34m
COLOR_GREEN  := \033[1;32m
COLOR_YELLOW := \033[1;33m
COLOR_CYAN   := \033[1;36m
COLOR_RED    := \033[1;31m

# Reusable color printer (interprets \033 escapes)
PRINTC := printf '%b\n'

# Version auto-detection (from git tags)
GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
GIT_COMMIT  := $(shell git rev-parse --short HEAD)
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Centralize extra flags
GOLANGCI_LINT_OPTS ?= --modules-download-mode=mod

# Allow skipping lint during release builds
SKIP_LINT ?= 0
ifeq ($(filter 1 true yes on,$(SKIP_LINT)),)
PREBUILD_LINT := golint
endif

# -------- Release flow helpers (gh CLI) --------
DEFAULT_BASE_BRANCH := main
REPO ?=              # optional owner/name; gh will infer from git remote if empty

define _require_clean
	@if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "❌ Working tree not clean. Commit/stash changes first."; exit 1; \
	fi
endef

define _require_gh
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "❌ GitHub CLI (gh) not found. Install: https://cli.github.com/"; exit 1; \
	fi
endef

# Replace _read_version + _load_version + _branch_name with these:

define _read_and_validate_version
	# Read VERSION (from env or prompt), normalize V->v, validate, and set REL_BRANCH
	if [ -z "$(VERSION)" ]; then \
	  read -p "Enter version (e.g. v1.2.3): " VERSION_INPUT; \
	else \
	  VERSION_INPUT="$(VERSION)"; \
	fi; \
	VERSION="$${VERSION_INPUT:-}"; \
	# normalize leading 'V' to 'v'
	VERSION="$$(printf '%s' "$$VERSION" | sed -E 's/^V/v/')"; \
	if ! echo "$$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9\.-]+)?$$'; then \
	  echo "❌ VERSION must look like v1.2.3 or v1.2.3-rc.1 (got '$$VERSION')"; \
	  exit 1; \
	fi; \
	REL_BRANCH="dev/$$VERSION"
endef

define _repo_flag
	$(if $(REPO),--repo "$(REPO)",)
endef

# ------------------------------------------------

.ONESHELL:
SHELL := /bin/bash

default: help

ensure-node:
	@echo ""
	@echo "📦 Ensuring Node.js $(NODE_VERSION) is available..."
	@if [ ! -d "$$HOME/.nvm" ]; then \
		curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.2/install.sh | bash; \
	fi
	@bash -c '\
	$(NVM_SETUP); \
		nvm install $(NODE_VERSION) > /dev/null || true; \
		nvm use $(NODE_VERSION) > /dev/null || true; \
		echo "✔ Node version: $$(node -v)"; \
		echo "✔ NPM version: $$(npm -v)"; \
		echo "✔ NPX version: $$(npx -v)"; \
	'
	@echo "✅ Node.js environment ready!"

ensure-go:
	@echo ""
	@echo "📦 Ensuring Go is available..."
	@if ! command -v go >/dev/null 2>&1; then \
		echo "⬇ Installing Go (no sudo)..."; \
		curl -LO https://go.dev/dl/go$(GO_VERSION).linux-amd64.tar.gz; \
		rm -rf $(GO_INSTALL_DIR); \
		mkdir -p $(GO_INSTALL_DIR); \
		tar -C $(GO_INSTALL_DIR) -xzf go$(GO_VERSION).linux-amd64.tar.gz --strip-components=1; \
		rm go$(GO_VERSION).linux-amd64.tar.gz; \
		if ! grep -q 'export PATH=$(GO_INSTALL_DIR)/bin' $$HOME/.bashrc; then \
			echo 'export PATH=$(GO_INSTALL_DIR)/bin:$$PATH' >> $$HOME/.bashrc; \
		fi; \
		echo "✔ Go installed at $(GO_INSTALL_DIR)"; \
		echo "💡 Please run 'source ~/.bashrc' or restart your terminal to use Go globally."; \
	fi
	@bash -c 'export PATH=$(GO_INSTALL_DIR)/bin:$$PATH && go version'
	@echo "✅ Go is ready!"

ensure-golint:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "⬇ Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GO_INSTALL_DIR)/bin v1.58.2; \
	fi

setup:
	@echo ""
	@echo "📦 Installing frontend dependencies..."
	@bash -c '\
	$(NVM_SETUP); \
		cd frontend && npm install --silent; \
	'
	@echo "✅ Frontend dependencies installed!"

lint: setup
	@echo "🔍 Running ESLint..."
	@bash -c '$(NVM_SETUP); \
		cd frontend && \
		npx eslint src --ext .js,.jsx,.ts,.tsx --fix && echo "✅ frontend Linting Ok!" \
	'

tsc: setup
	@echo "🔍 Running TypeScript type checks..."
	@bash -c '$(NVM_SETUP); \
		cd frontend && \
		npx tsc && echo "✅ TypeScript Linting Ok!" \
	'

golint: ensure-golint
	@echo "🔍 Running gofmt..."
ifneq ($(CI),)
	@fmt_out="$$(cd backend && gofmt -s -l .)"; \
	if [ -n "$$fmt_out" ]; then \
		echo "The following files are not gofmt'ed:"; echo "$$fmt_out"; exit 1; \
	fi
else
	@(cd backend && gofmt -s -w .)
endif
	@echo "🔍 Ensuring go.mod is tidy..."
	@( cd backend && go mod tidy && go mod download )
	@echo "🔍 Running golangci-lint..."
	@( cd backend && $(GOLANGCI_LINT) run ./... --timeout 3m $(GOLANGCI_LINT_OPTS) )
	@echo "✅ Go Linting Ok!"

test: setup dev-prep
	@echo ""
	@echo "📦 Running checks..."
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory tsc
	@$(MAKE) --no-print-directory golint

build-vite: lint tsc
	@echo ""
	@echo "📦 Building frontend..."
	@bash -c '\
	$(NVM_SETUP); \
		cd frontend && \
		VITE_API_URL=/ npx vite build && \
		echo "✅ Frontend built successfully!" \
	'

build-backend: $(PREBUILD_LINT)
	@echo ""
	@echo "📦 Building backend..."
	@cd backend && \
	go build \
	-ldflags "\
		-X 'backend/version.Version=$(GIT_VERSION)' \
		-X 'backend/version.CommitSHA=$(GIT_COMMIT)' \
		-X 'backend/version.BuildTime=$(BUILD_TIME)' \
		-X 'backend/version.Env=production'" \
	-o ../linuxio-webserver ./cmd/server && \
	echo "✅ Backend built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: $(PWD)/linuxio-webserver" && \
	echo "🔖 Version: $(GIT_VERSION)" && \
	echo "🔐 Commit: $(GIT_COMMIT)" && \
	echo "⏱ Build Time: $(BUILD_TIME)" && \
	echo "📦 Size: $$(du -h ../linuxio-webserver | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 ../linuxio-webserver | awk '{ print $$1 }')"

build-bridge: $(PREBUILD_LINT)
	@echo ""
	@echo "🔌 Building bridge..."
	@cd backend && \
	go build \
	-ldflags "\
		-X 'backend/version.Version=$(GIT_VERSION)' \
		-X 'backend/version.CommitSHA=$(GIT_COMMIT)' \
		-X 'backend/version.BuildTime=$(BUILD_TIME)' \
		-X 'backend/version.Env=production'" \
	-o ../linuxio-bridge ./cmd/bridge && \
	echo "✅ Bridge built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: $(PWD)/linuxio-bridge" && \
	echo "🔖 Version: $(GIT_VERSION)" && \
	echo "🔐 Commit: $(GIT_COMMIT)" && \
	echo "⏱ Build Time: $(BUILD_TIME)" && \
	echo "📦 Size: $$(du -h ../linuxio-bridge | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 ../linuxio-bridge | awk '{ print $$1 }')"

dev-prep:
	@mkdir -p backend/cmd/server/frontend/assets
	@mkdir -p backend/cmd/server/frontend/.vite
	@touch backend/cmd/server/frontend/.vite/manifest.json
	@touch backend/cmd/server/frontend/manifest.json
	@touch backend/cmd/server/frontend/favicon-1.png
	@touch backend/cmd/server/frontend/assets/index-mock.js

dev: setup dev-prep build-bridge
	@echo ""
	@echo "🚀 Starting dev mode (frontend + backend)..."
	set -euo pipefail

	# TTY polish: hide '^C' echo and restore it later
	if [ -t 1 ]; then SAVED_STTY=$$(stty -g); stty -echoctl; fi

	# Start backend (flags, not env) in background and remember PID
	( cd backend/cmd/server && \
	  go run . \
	    --env=development \
	    $(VERBOSE_FLAG) \
	    --port=$(SERVER_PORT) \
	    --vite-port=$(VITE_DEV_PORT) \
	) &
	BACK_PID=$$!

	# Wait (briefly) until backend listens on $(SERVER_PORT)
	for _ in 1 2 3 4 5 6 7 8 9 10; do
	  ss -ltn 'sport = :$(SERVER_PORT)' | grep -q LISTEN && break
	  sleep 0.2
	done

	cleanup_done=0
	cleanup() {
	  [[ "$$cleanup_done" -eq 1 ]] && return
	  cleanup_done=1
	  kill -INT "$$BACK_PID" 2>/dev/null || true

	  # Watchdog: SIGKILL after 10s if still alive (canceled if it exits earlier)
	  ( sleep 10; kill -KILL "$$BACK_PID" 2>/dev/null || true ) &
	  WATCH_PID=$$!

	  # Block until backend exits
	  wait "$$BACK_PID" 2>/dev/null || true

	  # Cancel watchdog
	  kill -TERM "$$WATCH_PID" 2>/dev/null || true
	  wait "$$WATCH_PID" 2>/dev/null || true
	}

	# On Ctrl-C/TERM: cleanup, restore TTY, exit success (no "Error 130")
	trap 'trap - INT TERM; cleanup; stty "$$SAVED_STTY" 2>/dev/null || true; exit 0' INT TERM

	# Frontend (foreground)
	export NVM_DIR="$$HOME/.nvm"
	. "$$NVM_DIR/nvm.sh" || true
	cd frontend
	VITE_API_URL="http://localhost:$(SERVER_PORT)" npx vite --port $(VITE_DEV_PORT)
	STATUS=$$?

	# Always clean up (if not already done via the trap)
	cleanup

	# Restore TTY settings
	stty "$$SAVED_STTY" 2>/dev/null || true

	# Normalize Ctrl-C from Vite so make doesn't show "Error 130"
	[[ "$$STATUS" -eq 130 ]] && STATUS=0
	exit "$$STATUS"

build: build-vite golint build-backend build-bridge

generate:
	@go generate ./backend/cmd/server/config/init.go

run:
	@./linuxio-webserver \
	  --verbose=$(VERBOSE) \
	  --port=$(SERVER_PORT)

clean:
	@rm -f ./linuxio-webserver || true
	@rm -f ./linuxio-bridge || true
	@rm -rf frontend/node_modules || true
	@rm -f frontend/package-lock.json || true
	@find backend/cmd/server/frontend -mindepth 1 -exec rm -rf {} + 2>/dev/null || true
	@echo "🧹 Cleaned workspace."

# ----- Release flow targets -----
start-dev: ## Create dev/<version> from main and push (requires clean tree & gh)
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  $(call _read_and_validate_version); \
	  git fetch origin; \
	  git checkout $(DEFAULT_BASE_BRANCH); \
	  git pull --ff-only; \
	  if git show-ref --verify --quiet "refs/heads/$$REL_BRANCH"; then \
	    echo "ℹ️  Branch $$REL_BRANCH already exists, checking it out…"; \
	    git checkout "$$REL_BRANCH"; \
	  else \
	    echo "🌱 Creating branch $$REL_BRANCH from $(DEFAULT_BASE_BRANCH)…"; \
	    git checkout -b "$$REL_BRANCH" "$(DEFAULT_BASE_BRANCH)"; \
	    git push -u origin "$$REL_BRANCH"; \
	  fi; \
	  echo "✅ Ready on branch $$REL_BRANCH"; \
	}

## Open PR dev/<version> -> main (requires gh)
open-pr: generate
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  $(call _read_and_validate_version); \
	  echo "🔁 Opening PR: $$REL_BRANCH -> $(DEFAULT_BASE_BRANCH)…"; \
	  gh pr create $(call _repo_flag) \
	    --base $(DEFAULT_BASE_BRANCH) \
	    --head "$$REL_BRANCH" \
	    --title "Release $$VERSION" \
	    --body "Automated release PR for $$VERSION."; \
	  gh pr view $(call _repo_flag) --web; \
	}

promote-release:
	@if [ -z "$(VERSION)" ]; then \
		read -p "Enter version (e.g. v1.2.3): " VERSION; \
		if [ -z "$$VERSION" ]; then echo "No version given. Exiting."; exit 1; fi; \
	fi; \
	git checkout main && \
	git pull && \
	git merge dev && \
	git push && \
	git tag -a $${VERSION:-$(VERSION)} -m "Release $${VERSION:-$(VERSION)}" && \
	git push origin $${VERSION:-$(VERSION)} && \
	echo "✅ Merged dev into main and released $${VERSION:-$(VERSION)}!"

help:
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_BLUE)🛠️  Available commands:$(COLOR_RESET)"
	@$(PRINTC) ""

	@$(PRINTC) "$(COLOR_CYAN)  Toolchain setup$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-node      $(COLOR_RESET) Install/activate Node $(NODE_VERSION) via nvm"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-go        $(COLOR_RESET) Install Go $(GO_VERSION) (user-local, no sudo)"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-golint    $(COLOR_RESET) Install golangci-lint"
	@$(PRINTC) "$(COLOR_GREEN)    make setup            $(COLOR_RESET) Install frontend dependencies (npm i)"
	@$(PRINTC) ""

	@$(PRINTC) "$(COLOR_CYAN)  Quality checks$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make lint             $(COLOR_RESET) Run ESLint (frontend)"
	@$(PRINTC) "$(COLOR_GREEN)    make tsc              $(COLOR_RESET) Type-check with TypeScript"
	@$(PRINTC) "$(COLOR_GREEN)    make golint           $(COLOR_RESET) Run gofmt + golangci-lint (backend)"
	@$(PRINTC) "$(COLOR_GREEN)    make test             $(COLOR_RESET) Run lint + tsc + golint"
	@$(PRINTC) ""

	@$(PRINTC) "$(COLOR_CYAN)  Development$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make dev-prep         $(COLOR_RESET) Create placeholder frontend assets for dev server"
	@$(PRINTC) "$(COLOR_YELLOW)    make dev              $(COLOR_RESET) Start backend (Go) + frontend (Vite) with live reload"
	@$(PRINTC) ""

	@$(PRINTC) "$(COLOR_CYAN)  Build$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-vite       $(COLOR_RESET) Build frontend static assets (Vite)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-backend    $(COLOR_RESET) Build Go backend binary"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-bridge     $(COLOR_RESET) Build Go bridge binary"
	@$(PRINTC) "$(COLOR_YELLOW)    make build            $(COLOR_RESET) Build frontend + backend + bridge"
	@$(PRINTC) ""

	@$(PRINTC) "$(COLOR_CYAN)  Run / Clean$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make run              $(COLOR_RESET) Run production backend server"
	@$(PRINTC) "$(COLOR_RED)    make clean            $(COLOR_RESET) Remove binaries, node_modules, and generated assets"
	@$(PRINTC) ""

	@$(PRINTC) "$(COLOR_CYAN)  Release flow$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make start-dev        $(COLOR_RESET) Create and switch to dev/<version> off main (pushes upstream)"
	@$(PRINTC) "$(COLOR_GREEN)    make open-pr          $(COLOR_RESET) Open PR from dev/<version> into main (uses gh)"
	@$(PRINTC) "$(COLOR_GREEN)    make promote-release  $(COLOR_RESET) (Legacy) Merge dev→main, tag, and push release"
	@$(PRINTC) ""
	@$(PRINTC) "  💡 Tip: Use a workflow to tag on merge of dev/v* → main so your 'Release' workflow (on tags v*) triggers automatically."
	@$(PRINTC) ""

.PHONY: default help clean run build build-vite build-backend build-bridge \
        dev dev-prep setup test lint tsc golint ensure-node ensure-go ensure-golint \
        promote-release start-dev open-pr
