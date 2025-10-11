# Main flags
VITE_DEV_PORT = 3000
SERVER_PORT   = 18090
VERBOSE      ?= true

# Go and Node.js versions
GO_VERSION   = 1.25.0
NODE_VERSION = 24
CC ?= cc

# Helpers
VERBOSE_FLAG := $(if $(filter true 1 yes on,$(VERBOSE)),--verbose,)
GO_INSTALL_DIR := $(HOME)/.go
NVM_DIR ?= $(HOME)/.nvm
export PATH := $(GO_INSTALL_DIR)/bin:$(NVM_DIR)/versions/node/current/bin:$(PATH)
NVM_SETUP = export NVM_DIR="$(NVM_DIR)"; \
            [ -s "$$NVM_DIR/nvm.sh" ] && . "$$NVM_DIR/nvm.sh"

# Colors
COLOR_RESET  := \033[0m
COLOR_BLUE   := \033[1;34m
COLOR_GREEN  := \033[1;32m
COLOR_YELLOW := \033[1;33m
COLOR_CYAN   := \033[1;36m
COLOR_RED    := \033[1;31m

PRINTC := printf '%b\n'
GOLANGCI_LINT_OPTS ?= --modules-download-mode=mod

# --- Go project root autodetection ---
BACKEND_DIR := $(shell \
  if [ -f backend/go.mod ]; then echo backend; \
  elif [ -f go.mod ]; then echo .; \
  else echo ""; fi )
ifeq ($(BACKEND_DIR),)
$(error Could not find go.mod in backend/ or project root)
endif

MODULE_PATH = $(shell cd "$(BACKEND_DIR)" && go list -m 2>/dev/null || echo "github.com/mordilloSan/LinuxIO")

# --- Git metadata ---
GIT_BRANCH        := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
GIT_TAG           := $(shell git describe --tags --exact-match 2>/dev/null || true)
GIT_COMMIT        := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_COMMIT_SHORT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BRANCH_VERSION    := $(patsubst dev/%,%,$(GIT_BRANCH))
BUILD_TIME        := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Determine version: prioritize dev branch, then tag, then commit
ifneq ($(findstring dev/,$(GIT_BRANCH)),)
  # On dev/vX.Y.Z branch - always use dev prefix
  GIT_VERSION := dev-$(BRANCH_VERSION)
else ifeq ($(GIT_TAG),)
  # Not on dev branch and no tag - use commit
  GIT_VERSION := dev-$(GIT_COMMIT_SHORT)
else
  # Not on dev branch but has tag - use tag (release)
  GIT_VERSION := $(GIT_TAG)
endif

GO_BIN := $(if $(wildcard $(GO_INSTALL_DIR)/bin/go),$(GO_INSTALL_DIR)/bin/go,$(shell which go))
GOLANGCI_LINT_MODULE  := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= latest
GOLANGCI_LINT         := $(GO_INSTALL_DIR)/bin/golangci-lint

# -------- Release flow helpers (gh CLI) --------
DEFAULT_BASE_BRANCH := main
REPO ?=
current_rel_branch = $(shell git branch --show-current)

define _require_clean
	@if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo " Working tree not clean. Commit/stash changes first."; exit 1; \
	fi
endef

define _require_gh
	@if ! command -v gh >/dev/null 2>&1; then \
		echo " GitHub CLI (gh) not found. Install: https://cli.github.com/"; exit 1; \
	fi
endef

define _read_and_validate_version
	if [ -z "$(VERSION)" ]; then \
	  read -p "Enter version (e.g. v1.2.3): " VERSION_INPUT; \
	else \
	  VERSION_INPUT="$(VERSION)"; \
	fi; \
	VERSION="$${VERSION_INPUT:-}"; \
	VERSION="$$(printf '%s' "$$VERSION" | sed -E 's/^V/v/')"; \
	if ! echo "$$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9\.-]+)?$$'; then \
	  echo " VERSION must look like v1.2.3 or v1.2.3-rc.1 (got '$$VERSION')"; \
	  exit 1; \
	fi; \
	REL_BRANCH="dev/$$VERSION"
endef

# ---- toolchain --------------------------------------------------------------
CC       ?= gcc
UNAME_S  := $(shell uname -s)

# ---- toggles (override on CLI: make build-auth-helper LTO=0 STRIP=0 WERROR=1)
LTO      ?= 1          # enable link-time optimization
STRIP    ?= 1          # strip unneeded symbols after build
WERROR   ?= 0          # treat warnings as errors (good in CI)

# ---- warnings ---------------------------------------------------------------
WARNFLAGS := \
  -Wall -Wextra -Wformat=2 -Wformat-security -Wnull-dereference \
  -Wshadow -Wpointer-arith -Wcast-qual -Wvla \
  -Wstrict-overflow=2 -Winit-self -Wduplicated-cond -Wlogical-op

ifeq ($(WERROR),1)
  WARNFLAGS += -Werror
endif

# ---- codegen / security-friendly opts --------------------------------------
OPTFLAGS := -O2 -fno-plt -fno-strict-aliasing -pipe

# ---- hardening (compile-time) ----------------------------------------------
HARDEN_CFLAGS := -fstack-protector-strong -D_FORTIFY_SOURCE=3 -fPIE
ifeq ($(UNAME_S),Linux)
  HARDEN_CFLAGS += -fstack-clash-protection
endif

# ---- hardening (link-time) -------------------------------------------------
HARDEN_LDFLAGS := -Wl,-z,relro -Wl,-z,now -Wl,-z,noexecstack -pie
# Keep --as-needed to avoid pulling unused libs into a SUID binary:
HARDEN_LDFLAGS += -Wl,--as-needed

# ---- size hygiene -----------------------------------------------------------
SIZEFLAGS    := -ffunction-sections -fdata-sections
SIZELDFLAGS  := -Wl,--gc-sections

# ---- LTO (safe with PAM; disable for debug if needed) ----------------------
LTOFLAGS :=
ifeq ($(LTO),1)
  LTOFLAGS := -flto
endif

# ---- standard ---------------------------------------------------------------
CSTD := -std=gnu11

# ---- final flags ------------------------------------------------------------
CFLAGS  := $(CSTD) $(WARNFLAGS) $(OPTFLAGS) $(HARDEN_CFLAGS) $(SIZEFLAGS) $(LTOFLAGS)
LDFLAGS := $(HARDEN_LDFLAGS) $(SIZELDFLAGS) $(LTOFLAGS)

.ONESHELL:
SHELL := /bin/bash

default: help

ensure-node:
	@echo ""
	@echo " Ensuring Node.js $(NODE_VERSION) is available..."
	@if [ ! -d "$(NVM_DIR)" ]; then \
		curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.2/install.sh | bash; \
	fi
	@bash -lc '\
		$(NVM_SETUP); \
		nvm install $(NODE_VERSION) >/dev/null || true; \
		nvm alias default $(NODE_VERSION); \
		CURR="$$(nvm version $(NODE_VERSION))"; \
		mkdir -p "$(NVM_DIR)/versions/node"; \
		ln -snf "$(NVM_DIR)/versions/node/$$CURR" "$(NVM_DIR)/versions/node/current"; \
		hash -r; \
		echo "✔ Node path: $$(command -v node)"; \
		echo "✔ Node version: $$(node -v)"; \
		echo "✔ NPM version:  $$(npm -v)"; \
		echo "✔ NPX version:  $$(npx -v)"; \
	'
	@echo "✅ Node.js environment ready!"

ensure-go:
	@echo ""
	@echo " Ensuring Go $(GO_VERSION) is available..."
	@bash -lc '\
		set -euo pipefail; \
		DESIRED="$(GO_VERSION)"; \
		GO_DIR="$(GO_INSTALL_DIR)"; \
		ARCH="$$(uname -m)"; \
		case "$$ARCH" in \
		  x86_64|amd64) GOARCH=amd64 ;; \
		  aarch64|arm64) GOARCH=arm64 ;; \
		  *) GOARCH=amd64 ;; \
		esac; \
		TMP="$$(mktemp -d)"; \
		TARBALL="go$${DESIRED}.linux-$${GOARCH}.tar.gz"; \
		URL="https://go.dev/dl/$${TARBALL}"; \
		\
		CUR=""; \
		if [ -x "$${GO_DIR}/bin/go" ]; then \
		  CUR="$$( "$${GO_DIR}/bin/go" version 2>/dev/null | awk "{print \$$3}" | sed "s/^go//" )"; \
		fi; \
		if [ "$$CUR" = "$$DESIRED" ]; then \
		  echo "✔ Go $$CUR already active at $$GO_DIR"; \
		else \
		  echo "⬇ Downloading $$URL"; \
		  curl -fsSL "$$URL" -o "$$TMP/$$TARBALL"; \
		  if [ -w /usr/local ]; then \
		    rm -rf /usr/local/go; \
		    tar -C /usr/local -xzf "$$TMP/$$TARBALL"; \
		    echo "✔ Installed Go $$DESIRED to /usr/local/go"; \
		  else \
		    VERSIONS_DIR="$$HOME/.go-versions"; \
		    DEST_VER_DIR="$$VERSIONS_DIR/go$${DESIRED}"; \
		    mkdir -p "$$VERSIONS_DIR"; \
		    rm -rf "$$DEST_VER_DIR"; \
		    tar -C "$$TMP" -xzf "$$TMP/$$TARBALL"; \
		    mv "$$TMP/go" "$$DEST_VER_DIR"; \
		    ln -sfn "$$DEST_VER_DIR" "$$GO_DIR"; \
		    if ! grep -q "$$GO_DIR/bin" "$$HOME/.bashrc" 2>/dev/null; then \
		      echo "export PATH=$$GO_DIR/bin:\$$PATH" >> "$$HOME/.bashrc"; \
		    fi; \
		  fi; \
		fi; \
		if [ -x "$$GO_DIR/bin/go" ]; then "$$GO_DIR/bin/go" version || true; \
		elif [ -x /usr/local/go/bin/go ]; then /usr/local/go/bin/go version || true; \
		else echo "  Go not found on expected paths; check PATH."; fi; \
		rm -rf "$$TMP"; \
		echo "✅ Go is ready!"; \
	'

ensure-golint: ensure-go
	@{ set -euo pipefail; \
	   bin="$(GOLANGCI_LINT)"; need=1; \
	   if [ -x "$$bin" ]; then \
	     out="$$( "$$bin" version 2>/dev/null || true)"; \
	     ver="$$( printf '%s' "$$out" | sed -n 's/^golangci-lint has version[[:space:]]\([v0-9.]\+\).*/\1/p' )"; \
	     ver_no_v="$${ver#v}"; major="$${ver_no_v%%.*}"; \
	     built_ok="$$( printf '%s' "$$out" | grep -Eq 'built with go1\.25(\.|$$)' && echo yes || echo no )"; \
	     if [ "$$major" = "2" ] && [ "$$built_ok" = "yes" ]; then need=0; fi; \
	   fi; \
	   if [ $$need -eq 1 ]; then \
	     echo "⬇ Installing golangci-lint $(GOLANGCI_LINT_VERSION) (v2) with local Go ($(GO_BIN))..."; \
	     rm -f "$$bin" || true; \
	     PATH="$(GO_INSTALL_DIR)/bin:$$PATH" GOBIN="$(GO_INSTALL_DIR)/bin" GOTOOLCHAIN=local GOFLAGS="-buildvcs=false" \
	       "$(GO_BIN)" install "$(GOLANGCI_LINT_MODULE)@$(GOLANGCI_LINT_VERSION)"; \
	   fi; \
	   "$$bin" version | head -n1; \
	   out="$$( "$$bin" version )"; \
	   ver="$$( printf '%s' "$$out" | sed -n 's/^golangci-lint has version[[:space:]]\([v0-9.]\+\).*/\1/p' )"; \
	   ver_no_v="$${ver#v}"; major="$${ver_no_v%%.*}"; \
	   [ "$$major" = "2" ] || { echo " not a v2 golangci-lint"; exit 1; }; \
	   echo "$$out" | grep -Eq 'built with go1\.25(\.|$$)' || { echo " golangci-lint not built with Go 1.25"; exit 1; }; \
	   echo "✔ golangci-lint v2 ready."; \
	}

setup:
	@echo ""
	@echo " Installing frontend dependencies..."
	@bash -c 'cd frontend && npm install --silent;'
	@echo "✅ Frontend dependencies installed!"

lint: ensure-node setup
	@echo " Running ESLint..."
	@bash -c 'cd frontend && npx eslint src --ext .js,.jsx,.ts,.tsx --fix && echo "✅ frontend Linting Ok!"'

tsc: ensure-node setup
	@echo " Running TypeScript type checks..."
	@bash -c 'cd frontend && npx tsc && echo "✅ TypeScript Linting Ok!"'

golint: ensure-golint
	@set -euo pipefail
	@echo "📁 Linting Go module in: $(BACKEND_DIR)"
	@echo " Running gofmt..."
ifneq ($(CI),)
	@fmt_out="$$(cd "$(BACKEND_DIR)" && gofmt -s -l .)"; \
	if [ -n "$$fmt_out" ]; then echo "The following files are not gofmt'ed:"; echo "$$fmt_out"; exit 1; fi
else
	@( cd "$(BACKEND_DIR)" && gofmt -s -w . )
endif
	@echo " Ensuring go.mod is tidy..."
	@( cd "$(BACKEND_DIR)" && go mod tidy && go mod download )
	@echo " Running golangci-lint..."
	@( cd "$(BACKEND_DIR)" && "$(GOLANGCI_LINT)" run ./... --timeout 3m $(GOLANGCI_LINT_OPTS) )
	@echo "✅ Go Linting Ok!"

test: setup dev-prep
	@echo ""
	@echo " Running checks..."
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory tsc
	@$(MAKE) --no-print-directory golint
	@$(MAKE) --no-print-directory test-backend

test-backend:
	@echo "Running Go unit tests (backend)..."
	@cd "$(BACKEND_DIR)" && \
		GOFLAGS="-buildvcs=false" \
		go test ./... -count=1 -timeout 5m
	@echo "✅ Backend tests passed!"

build-vite: lint tsc
	@echo ""
	@echo " Building frontend..."
	@bash -c 'cd frontend && VITE_API_URL=/ npx vite build && echo "✅ Frontend built successfully!"'

build-backend: ensure-go
	@echo ""
	@echo " Building backend..."
	@echo "📦 Module: $(MODULE_PATH)"
	@echo "🔖 Version: $(GIT_VERSION)"
	@cd "$(BACKEND_DIR)" && \
	GOFLAGS="-buildvcs=false" \
	go build \
	-ldflags "\
		-X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' \
		-X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' \
		-X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)'" \
	-o ../linuxio ./ && \
	echo "✅ Backend built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: $(PWD)/linuxio" && \
	echo "🔖 Version: $(GIT_VERSION)" && \
	echo " Size: $$(du -h ../linuxio | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 ../linuxio | awk '{ print $$1 }')"

build-bridge: ensure-go
	@echo ""
	@echo "🌉 Building bridge..."
	@echo "📦 Module: $(MODULE_PATH)"
	@echo "🔖 Version: $(GIT_VERSION)"
	@cd "$(BACKEND_DIR)" && \
	GOFLAGS="-buildvcs=false" \
	go build \
	-ldflags "\
		-X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' \
		-X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' \
		-X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)'" \
	-o ../linuxio-bridge ./bridge && \
	echo "✅ Bridge built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: $(PWD)/linuxio-bridge" && \
	echo "🔖 Version: $(GIT_VERSION)" && \
	echo " Size: $$(du -h ../linuxio-bridge | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 ../linuxio-bridge | awk '{ print $$1 }')"

build-auth-helper:
	@echo ""
	@echo "🛡️  Building Session helper (C)..."
	@set -euo pipefail; \
	$(CC) $(CFLAGS) -o linuxio-auth-helper packaging/linuxio-auth-helper.c $(LDFLAGS) -lpam; \
	if [ "$(STRIP)" = "1" ]; then strip --strip-unneeded linuxio-auth-helper; fi; \
	echo "✅ Session helper built successfully!"; \
	echo "📄 Path: $$PWD/linuxio-auth-helper"; \
	echo " Size: $$(du -h linuxio-auth-helper | cut -f1)"; \
	echo "🔐 SHA256: $$(shasum -a 256 linuxio-auth-helper | awk '{ print $$1 }')"; \
	if command -v checksec >/dev/null 2>&1; then \
	  echo "🔎 checksec:"; checksec --file=linuxio-auth-helper || true; \
	fi

dev-prep:
	@mkdir -p "$(BACKEND_DIR)/server/web/frontend/assets"
	@mkdir -p "$(BACKEND_DIR)/server/web/frontend/.vite"
	@touch "$(BACKEND_DIR)/server/web/frontend/.vite/manifest.json"
	@touch "$(BACKEND_DIR)/server/web/frontend/manifest.json"
	@touch "$(BACKEND_DIR)/server/web/frontend/favicon-1.png"
	@touch "$(BACKEND_DIR)/server/web/frontend/assets/index-mock.js"

dev: setup dev-prep devinstall
	@echo ""
	@echo "🚀 Starting dev mode (frontend + backend)..."
	set -euo pipefail

	# TTY polish
	if [ -t 1 ]; then SAVED_STTY=$$(stty -g); stty -echoctl; fi

	# Backend with inline env vars (no dev.env needed)
	( \
	  cd "$(BACKEND_DIR)"; \
	  LINUXIO_ENV=development \
	  LINUXIO_PAM_HELPER=/tmp/linuxio/dev/linuxio-auth-helper \
	  LINUXIO_BRIDGE_BIN=/tmp/linuxio/dev/linuxio-bridge \
	  go run . run \
	    -env development \
	    -verbose=$(VERBOSE) \
	    -vite-port=$(VITE_DEV_PORT) \
	    -port=$(SERVER_PORT); \
	) &

	BACK_PID=$$!

	@timeout 60s bash -c 'until ss -ltn "sport = :$(SERVER_PORT)" | grep -q LISTEN; do sleep 0.2; done' \
	  || { echo " Backend port :$(SERVER_PORT) did not open in time"; cleanup; exit 1; }

	cleanup_done=0
	cleanup() {
	  [[ "$$cleanup_done" -eq 1 ]] && return
	  cleanup_done=1
	  kill -INT "$$BACK_PID" 2>/dev/null || true
	  ( sleep 10; kill -KILL "$$BACK_PID" 2>/dev/null || true ) &
	  WATCH_PID=$$!
	  wait "$$BACK_PID" 2>/dev/null || true
	  kill -TERM "$$WATCH_PID" 2>/dev/null || true
	  wait "$$WATCH_PID" 2>/dev/null || true
	}
	trap 'trap - INT TERM; cleanup; stty "$$SAVED_STTY" 2>/dev/null || true; exit 0' INT TERM

	# Frontend
	cd frontend
	VITE_API_URL="http://localhost:$(SERVER_PORT)" npx vite --port $(VITE_DEV_PORT)
	STATUS=$$?

	# Always clean up
	cleanup
	stty "$$SAVED_STTY" 2>/dev/null || true
	[[ "$$STATUS" -eq 130 ]] && STATUS=0
	exit "$$STATUS"

build: build-vite golint build-backend build-bridge build-auth-helper

localinstall:
	./packaging/scripts/local_install.sh

devinstall:
	@SECURE_DEV_DIR="/tmp/linuxio/dev"; \
	NEED_INSTALL=0; \
	\
	if [ ! -f "$$SECURE_DEV_DIR/linuxio-bridge" ] || [ ! -f "$$SECURE_DEV_DIR/linuxio-auth-helper" ]; then \
	  echo "⚠️  Dev binaries not found in $$SECURE_DEV_DIR"; \
	  NEED_INSTALL=1; \
	elif [ ! -u "$$SECURE_DEV_DIR/linuxio-auth-helper" ]; then \
	  echo "⚠️  Auth helper missing setuid bit"; \
	  NEED_INSTALL=1; \
	elif [ "packaging/linuxio-auth-helper.c" -nt "$$SECURE_DEV_DIR/linuxio-auth-helper" ]; then \
	  echo "⚠️  Auth helper source is newer than installed binary"; \
	  NEED_INSTALL=1; \
	elif [ -d "$(BACKEND_DIR)" ] && find "$(BACKEND_DIR)" -name "*.go" -newer "$$SECURE_DEV_DIR/linuxio-bridge" 2>/dev/null | grep -q .; then \
	  echo "⚠️  Go source files changed since bridge was built"; \
	  NEED_INSTALL=1; \
	elif [ "$(BACKEND_DIR)/go.mod" -nt "$$SECURE_DEV_DIR/linuxio-bridge" ] || [ "$(BACKEND_DIR)/go.sum" -nt "$$SECURE_DEV_DIR/linuxio-bridge" ]; then \
	  echo "⚠️  Go dependencies changed (go.mod/go.sum updated)"; \
	  NEED_INSTALL=1; \
	fi; \
	\
	if [ $$NEED_INSTALL -eq 1 ]; then \
	  echo "🔧 Running dev installation (requires sudo)..."; \
	  sudo ./packaging/scripts/dev_install.sh; \
	else \
	  echo "✅ Dev binaries are up-to-date in $$SECURE_DEV_DIR"; \
	fi

devinstall-force:
	@echo "🔧 Force reinstalling dev binaries..."
	@sudo ./packaging/scripts/dev_install.sh

generate:
	@cd "$(BACKEND_DIR)" && go generate ./bridge/userconfig/init.go

run:
	@./linuxio run \
	  -env production \
	  -verbose=$(VERBOSE) \
	  -vite-port=$(VITE_DEV_PORT) \
	  -port=$(SERVER_PORT)

clean:
	@rm -f ./linuxio || true
	@rm -f ./linuxio-bridge || true
	@rm -f ./linuxio-auth-helper || true
	@rm -rf frontend/node_modules || true
	@rm -f frontend/package-lock.json || true
	@rm -rf /tmp/linuxio/dev || true
	@find "$(BACKEND_DIR)/server/frontend" -mindepth 1 -exec rm -rf {} + 2>/dev/null || true
	@echo "🧹 Cleaned workspace."
	@echo "💡 Run 'make clean-dev' to also remove dev binaries and sudo config"

clean-dev:
	@if [ -d /tmp/linuxio/dev ]; then \
		echo "Removing dev binaries from /tmp/linuxio/dev..."; \
		sudo rm -rf /tmp/linuxio/dev; \
	fi
	@if [ -f /etc/sudoers.d/linuxio-dev ]; then \
		echo "Removing passwordless sudo configuration..."; \
		sudo rm -f /etc/sudoers.d/linuxio-dev; \
	fi
	@echo "🧹 Dev environment cleaned."

clean-all: clean clean-dev
	@echo "✨ Everything cleaned!"

start-dev:
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  $(call _read_and_validate_version); \
	  git fetch origin; \
	  git checkout $(DEFAULT_BASE_BRANCH); \
	  git pull --ff-only; \
	  if git show-ref --verify --quiet "refs/heads/$$REL_BRANCH"; then \
	    echo "ℹBranch $$REL_BRANCH already exists, checking it out…"; \
	    git checkout "$$REL_BRANCH"; \
	  else \
	    echo "Creating branch $$REL_BRANCH from $(DEFAULT_BASE_BRANCH)…"; \
	    git checkout -b "$$REL_BRANCH" "$(DEFAULT_BASE_BRANCH)"; \
	    git push -u origin "$$REL_BRANCH"; \
	  fi; \
	  echo "✅ Ready on branch $$REL_BRANCH"; \
	}

changelog:
	@$(call _require_clean)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$'; then \
	    echo "❌ Not on a dev/v* release branch (got '$$BRANCH')."; \
	    echo "💡 Run 'make start-dev VERSION=v1.2.3' first."; \
	    exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  DATE="$$(date -u +%Y-%m-%d)"; \
	  echo "📝 Generating changelog for $$VERSION ($$DATE)..."; \
	  echo ""; \
	  PREV_TAG="$$(git tag --list 'v*' --sort=-v:refname | grep -v "^$$VERSION$$" | head -n1 || echo "")"; \
	  if [ -n "$$PREV_TAG" ]; then \
	    echo "📍 Changes since $$PREV_TAG"; \
	    COMMITS="$$(git log $${PREV_TAG}..HEAD --pretty=format:'%s|%h|%an' --reverse)"; \
	  else \
	    echo "📍 All commits (no previous tag found)"; \
	    COMMITS="$$(git log --pretty=format:'%s|%h|%an' --reverse)"; \
	  fi; \
	  FEATURES=""; FIXES=""; DOCS=""; STYLE=""; REFACTOR=""; PERF=""; \
	  TEST=""; BUILD=""; CI=""; CHORE=""; OTHER=""; \
	  while IFS='|' read -r message hash author; do \
	    [ -z "$$message" ] && continue; \
	    [[ "$$author" == "github-actions[bot]" ]] && continue; \
	    ENTRY="* $$message ([$${hash:0:7}](https://github.com/$${GITHUB_REPOSITORY:-owner/repo}/commit/$$hash)) by @$$author"; \
	    if [[ "$$message" =~ ^feat(\(.*\))?: ]]; then FEATURES="$$FEATURES$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^fix(\(.*\))?: ]]; then FIXES="$$FIXES$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^docs(\(.*\))?: ]]; then DOCS="$$DOCS$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^style(\(.*\))?: ]]; then STYLE="$$STYLE$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^refactor(\(.*\))?: ]]; then REFACTOR="$$REFACTOR$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^perf(\(.*\))?: ]]; then PERF="$$PERF$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^test(\(.*\))?: ]]; then TEST="$$TEST$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^build(\(.*\))?: ]]; then BUILD="$$BUILD$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^ci(\(.*\))?: ]]; then CI="$$CI$$ENTRY"$$'\n'; \
	    elif [[ "$$message" =~ ^chore(\(.*\))?: ]]; then CHORE="$$CHORE$$ENTRY"$$'\n'; \
	    else OTHER="$$OTHER$$ENTRY"$$'\n'; fi; \
	  done <<< "$$COMMITS"; \
	  BODY_FILE="$$(mktemp)"; \
	  { \
	    [ -n "$$FEATURES" ] && printf "### 🚀 Features\n\n%b\n" "$$FEATURES"; \
	    [ -n "$$FIXES" ] && printf "### 🐛 Bug Fixes\n\n%b\n" "$$FIXES"; \
	    [ -n "$$PERF" ] && printf "### ⚡ Performance\n\n%b\n" "$$PERF"; \
	    [ -n "$$REFACTOR" ] && printf "### ♻️ Refactoring\n\n%b\n" "$$REFACTOR"; \
	    [ -n "$$DOCS" ] && printf "### 📚 Documentation\n\n%b\n" "$$DOCS"; \
	    [ -n "$$STYLE" ] && printf "### 💄 Style\n\n%b\n" "$$STYLE"; \
	    [ -n "$$TEST" ] && printf "### 🧪 Tests\n\n%b\n" "$$TEST"; \
	    [ -n "$$BUILD" ] && printf "### 🏗️ Build\n\n%b\n" "$$BUILD"; \
	    [ -n "$$CI" ] && printf "### 🤖 CI/CD\n\n%b\n" "$$CI"; \
	    [ -n "$$CHORE" ] && printf "### 🔧 Chores\n\n%b\n" "$$CHORE"; \
	    [ -n "$$OTHER" ] && printf "### 🔄 Other Changes\n\n%b\n" "$$OTHER"; \
	    printf "### 👥 Contributors\n\n"; \
	    if [ -n "$$PREV_TAG" ]; then \
	      git log $${PREV_TAG}..HEAD --pretty=format:'* @%an' | sort -u; \
	    else \
	      git log --pretty=format:'* @%an' | sort -u; \
	    fi; \
	    printf "\n\n**Full Changelog**: https://github.com/$${GITHUB_REPOSITORY:-owner/repo}/compare/$$PREV_TAG...$$VERSION\n"; \
	  } > "$$BODY_FILE"; \
	  HEADER="## $$VERSION — $$DATE"; \
	  { \
	    echo "$$HEADER"; \
	    echo ""; \
	    cat "$$BODY_FILE"; \
	    echo ""; \
	  } > new_entry.md; \
	  if [ -f CHANGELOG.md ]; then \
	    if grep -q "^## $$VERSION —" CHANGELOG.md; then \
	      echo "⚠️  Version $$VERSION already exists in CHANGELOG.md, updating..."; \
	      awk -v ver="$$VERSION" ' \
	        /^## / { \
	          if ($$2 == ver) { in_section=1; next } \
	          else if (in_section) { in_section=0 } \
	        } \
	        !in_section { print } \
	      ' CHANGELOG.md > CHANGELOG.tmp; \
	      cat new_entry.md CHANGELOG.tmp > CHANGELOG.md; \
	      rm CHANGELOG.tmp; \
	    else \
	      cat new_entry.md CHANGELOG.md > CHANGELOG.tmp; \
	      mv CHANGELOG.tmp CHANGELOG.md; \
	    fi; \
	  else \
	    echo "# Changelog" > CHANGELOG.md; \
	    echo "" >> CHANGELOG.md; \
	    cat new_entry.md >> CHANGELOG.md; \
	  fi; \
	  rm -f new_entry.md "$$BODY_FILE"; \
	  echo ""; \
	  echo "✅ CHANGELOG.md updated for $$VERSION"; \
	  echo ""; \
	  echo "📄 Preview:"; \
	  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; \
	  head -n 30 CHANGELOG.md; \
	  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; \
	  echo ""; \
	  echo "💡 Review the changes, then:"; \
	  echo "   git add CHANGELOG.md"; \
	  echo "   git commit -m 'docs: update changelog for $$VERSION'"; \
	  echo "   make open-pr"; \
	  git add CHANGELOG.md \
	  git commit -m 'docs: update changelog for v0.2.6' \
	}

open-pr: generate changelog
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$'; then \
	    echo "❌ Not on a dev/v* release branch (got '$$BRANCH')."; exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  BASE_BRANCH="$(DEFAULT_BASE_BRANCH)"; \
	  PRNUM="$$(gh pr list $(call _repo_flag) --base "$$BASE_BRANCH" --head "$$BRANCH" --state open --json number --jq '.[0].number' || true)"; \
	  if [ -n "$$PRNUM" ] && [ "$$PRNUM" != "null" ]; then \
	    echo "ℹ️  An open PR (#$$PRNUM) from $$BRANCH -> $$BASE_BRANCH already exists."; \
	    gh pr view $(call _repo_flag) "$$PRNUM" --web || true; \
	  else \
	    echo "🔁 Opening PR: $$BRANCH -> $$BASE_BRANCH…"; \
	    gh pr create $(call _repo_flag) \
	      --base "$$BASE_BRANCH" \
	      --head "$$BRANCH" \
	      --title "Release $$VERSION" \
	      --body-file CHANGELOG.md; \
	    PRNUM="$$(gh pr list $(call _repo_flag) --base "$$BASE_BRANCH" --head "$$BRANCH" --state open --json number --jq '.[0].number')"; \
	  fi; \
	  echo "🔍 Checking for CI checks on PR #$$PRNUM…"; \
	  CHECK_OUTPUT="$$(gh pr checks $(call _repo_flag) "$$PRNUM" 2>&1 || true)"; \
	  if echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	    echo "⚠️  No CI checks configured. Skipping check wait."; \
	    echo "💡 Set up GitHub Actions to run tests automatically."; \
	  else \
	    echo "⏳ Waiting for checks on PR #$$PRNUM…"; \
	    gh pr checks $(call _repo_flag) "$$PRNUM" --watch --interval 5; \
	    echo "✅ All checks passed!"; \
	  fi; \
	  gh pr view $(call _repo_flag) "$$PRNUM" --web || true; \
	}

merge-release:
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$'; then \
	    echo "❌ Current branch '$$BRANCH' is not a dev/v* release branch."; exit 1; \
	  fi; \
	  PRNUM="$${PR:-$$(gh pr list $(call _repo_flag) --base main --head "$$BRANCH" --state open --json number --jq '.[0].number' || true)}"; \
	  if [ -z "$$PRNUM" ] || [ "$$PRNUM" = "null" ]; then echo "❌ No open PR from $$BRANCH to main."; exit 1; fi; \
	  echo "🔍 Checking status of PR #$$PRNUM…"; \
	  CHECK_OUTPUT="$$(gh pr checks $(call _repo_flag) "$$PRNUM" 2>&1 || true)"; \
	  if echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	    echo "⚠️  No CI checks configured. Proceeding with merge."; \
	    echo "💡 Consider setting up GitHub Actions for automated testing."; \
	  elif ! gh pr checks $(call _repo_flag) "$$PRNUM" > /dev/null 2>&1; then \
	    echo "❌ Checks have not passed. Run 'make open-pr' to wait for checks."; \
	    exit 1; \
	  else \
	    echo "✅ All checks passed."; \
	  fi; \
	  echo "🔀 Merging PR #$$PRNUM…"; \
	  gh pr merge $(call _repo_flag) "$$PRNUM" --merge --delete-branch; \
	  VERSION="$${BRANCH#dev/}"; \
	  echo "🔖 Tag to be released: $$VERSION"; \
	}

version-debug:
	@echo "=== Version Debug Info ==="
	@echo "BACKEND_DIR:      $(BACKEND_DIR)"
	@echo "MODULE_PATH:      $(MODULE_PATH)"
	@echo "GIT_VERSION:      $(GIT_VERSION)"
	@echo "GIT_COMMIT_SHORT: $(GIT_COMMIT_SHORT)"
	@echo "BUILD_TIME:       $(BUILD_TIME)"
	@echo ""
	@echo "=== Testing go list -m ==="
	@cd "$(BACKEND_DIR)" && go list -m
	@echo ""
	@echo "=== Build command preview ==="
	@echo "go build -ldflags \\"
	@echo "  -X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' \\"
	@echo "  -X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' \\"
	@echo "  -X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)'"
	
help:
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_BLUE)🛠️  Available commands:$(COLOR_RESET)"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Toolchain setup$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-node      $(COLOR_RESET) Install/activate Node $(NODE_VERSION) via nvm"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-go        $(COLOR_RESET) Install Go $(GO_VERSION) (user-local, no sudo)"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-golint    $(COLOR_RESET) Install golangci-lint (built with local Go 1.25)"
	@$(PRINTC) "$(COLOR_GREEN)    make setup            $(COLOR_RESET) Install frontend dependencies (npm i)"
	@$(PRINTC) "$(COLOR_GREEN)    make devinstall       $(COLOR_RESET) Install dev binaries (only if needed)"
	@$(PRINTC) "$(COLOR_GREEN)    make devinstall-force $(COLOR_RESET) Force reinstall dev binaries"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Quality checks$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make lint             $(COLOR_RESET) Run ESLint (frontend)"
	@$(PRINTC) "$(COLOR_GREEN)    make tsc              $(COLOR_RESET) Type-check with TypeScript (frontend)"
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
	@$(PRINTC) "$(COLOR_YELLOW)    make build-auth-helper $(COLOR_RESET) Build the PAM authentication helper"
	@$(PRINTC) "$(COLOR_YELLOW)    make build            $(COLOR_RESET) Build frontend + backend + bridge"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Run / Clean$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make run              $(COLOR_RESET) Run production backend server"
	@$(PRINTC) "$(COLOR_RED)    make clean            $(COLOR_RESET) Remove binaries, node_modules, and generated assets"
	@$(PRINTC) "$(COLOR_RED)    make clean-dev        $(COLOR_RESET) Remove dev binaries and sudo config (sudo required)"
	@$(PRINTC) "$(COLOR_RED)    make clean-all        $(COLOR_RESET) Full cleanup: workspace + dev environment"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Release flow$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make start-dev        $(COLOR_RESET) Create and switch to dev/<version> from main (pushes upstream)"
	@$(PRINTC) "$(COLOR_GREEN)    make open-pr          $(COLOR_RESET) Open PR dev/<version> → main (uses gh)"
	@$(PRINTC) "$(COLOR_GREEN)    make merge-release    $(COLOR_RESET) Wait for checks, merge PR to main, delete branch"
	@$(PRINTC) ""

.PHONY: \
    default help clean clean-dev clean-all run \
    build build-vite build-backend build-bridge build-auth-helper \
	dev dev-prep setup test lint tsc golint \
	ensure-node ensure-go ensure-golint \
	generate devinstall devinstall-force \
	start-dev open-pr merge-release version-debug