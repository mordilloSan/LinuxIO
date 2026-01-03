# Include private release automation
-include release.mk

# Main flags
VITE_DEV_PORT = 3000
DEV_LOG_LINES ?= 25
VITE_DEV_LOG  ?= frontend/.vite-dev.log
VITE_DEV_PID  ?= frontend/.vite-dev.pid
VERBOSE      ?= true

# --- Go project root autodetection ---
BACKEND_DIR := $(shell \
  if [ -f backend/go.mod ]; then echo backend; \
  elif [ -f go.mod ]; then echo .; \
  else echo ""; fi )
ifeq ($(BACKEND_DIR),)
$(error Could not find go.mod in backend/ or project root)
endif

# Toolchain versions (sourced from repo files)
GO_VERSION ?= $(shell awk '/^go / {print $$2; exit}' "$(BACKEND_DIR)/go.mod")
NODE_VERSION ?= $(shell python3 -c "import json, pathlib; data=json.loads(pathlib.Path('frontend/package.json').read_text()); print((data.get('engines') or {}).get('node',''))" 2>/dev/null)
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

# ---- toolchain --------------------------------------------------------------
CC       ?= gcc
UNAME_S  := $(shell uname -s)

# ---- toggles (override on CLI: make build-auth LTO=0 STRIP=0 WERROR=1)
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

print-toolchain-versions:
	@set -euo pipefail; \
	if [ -z "$(GO_VERSION)" ]; then \
	  echo "ERROR: GO_VERSION is empty; check $(BACKEND_DIR)/go.mod" >&2; \
	  exit 1; \
	fi; \
	if [ -z "$(NODE_VERSION)" ]; then \
	  echo "ERROR: NODE_VERSION is empty; check frontend/package.json engines.node" >&2; \
	  exit 1; \
	fi; \
	echo "go=$(GO_VERSION)"; \
	echo "node=$(NODE_VERSION)"

ensure-node:
	@echo ""
	@echo "🔧 Ensuring Node.js $(NODE_VERSION) is available..."
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
	@echo "🔧 Ensuring Go $(GO_VERSION) is available..."
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
		else echo "⚠️  Go not found on expected paths; check PATH."; fi; \
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
	   [ "$$major" = "2" ] || { echo "❌ not a v2 golangci-lint"; exit 1; }; \
	   echo "$$out" | grep -Eq 'built with go1\.25(\.|$$)' || { echo "❌ golangci-lint not built with Go 1.25"; exit 1; }; \
	   echo "✔ golangci-lint v2 ready."; \
	}

setup:
	@echo ""
	@echo "📦 Installing frontend dependencies..."
	@bash -c 'cd frontend && npm install --silent;'
	@echo "✅ Frontend dependencies installed!"

# Separate lint/tsc targets that include all prerequisites (delegate to -only variants)
lint: ensure-node setup
	@$(MAKE) --no-print-directory lint-only

tsc: ensure-node setup
	@$(MAKE) --no-print-directory tsc-only

golint: ensure-golint
	@$(MAKE) --no-print-directory golint-only

# Optimized test target: runs setup ONCE, then parallelizes the actual checks
test: ensure-node ensure-go ensure-golint setup dev-prep
	@echo "🧪 Running checks (parallel)..."
	@{ \
	  $(MAKE) --no-print-directory lint-only & \
	  $(MAKE) --no-print-directory tsc-only & \
	  $(MAKE) --no-print-directory golint-only & \
	  wait; \
	} && $(MAKE) --no-print-directory test-backend

# Core lint implementations (used by both individual targets and parallel test)
lint-only:
	@echo "🔍 Running ESLint..."
	@bash -c 'cd frontend && npx eslint src --ext .js,.jsx,.ts,.tsx --fix --concurrency=auto && echo "✅ frontend Linting Ok!"'

tsc-only:
	@echo "🔍 Running TypeScript type checks..."
	@bash -c 'cd frontend && npx tsc && echo "✅ TypeScript Linting Ok!"'

golint-only:
	@echo "📁 Linting Go module in: $(BACKEND_DIR)"
	@echo "🔍 Running gofmt..."
ifneq ($(CI),)
	@fmt_out="$$(cd "$(BACKEND_DIR)" && gofmt -s -l .)"; \
	if [ -n "$$fmt_out" ]; then echo "The following files are not gofmt'ed:"; echo "$$fmt_out"; exit 1; fi
else
	@( cd "$(BACKEND_DIR)" && gofmt -s -w . )
endif
	@echo "🔍 Ensuring go.mod is tidy..."
	@( cd "$(BACKEND_DIR)" && go mod tidy && go mod download )
	@echo "🔍 Running golangci-lint..."
	@( cd "$(BACKEND_DIR)" && "$(GOLANGCI_LINT)" run --fix ./... --timeout 3m $(GOLANGCI_LINT_OPTS) )
	@echo "✅ Go Linting Ok!"

test-backend:
	@echo "🧪 Running Go unit tests (backend)..."
	@cd "$(BACKEND_DIR)" && \
		out="$$(GOFLAGS="-buildvcs=false" go test ./... -count=1 -timeout 5m 2>&1)"; \
		status=$$?; \
		echo "$$out" | grep -v '\[no test files\]' || true; \
		exit $$status
	

build-vite:
	@echo ""
	@echo "🏗️  Building frontend..."
	@bash -c 'cd frontend && npx vite build && echo "✅ Frontend built successfully!"'

build-backend: ensure-go
	@echo ""
	@echo "🏗️  Building backend..."
	@echo "📦 Module: $(MODULE_PATH)"
	@echo "🔖 Version: $(GIT_VERSION)"
	@if [ -n "$(BRIDGE_SHA256)" ]; then \
		echo "🔐 Bridge SHA256: $(BRIDGE_SHA256)"; \
	else \
		echo "🔐 Bridge SHA256: (not embedded - development mode)"; \
	fi
	@cd "$(BACKEND_DIR)" && \
	GOFLAGS="-buildvcs=false -tags=nomsgpack" \
	go build \
	-ldflags "\
		-s -w \
		-X '$(MODULE_PATH)/common/config.Version=$(GIT_VERSION)' \
		-X '$(MODULE_PATH)/common/config.CommitSHA=$(GIT_COMMIT_SHORT)' \
		-X '$(MODULE_PATH)/common/config.BuildTime=$(BUILD_TIME)' \
		-X '$(MODULE_PATH)/common/config.BridgeSHA256=$(BRIDGE_SHA256)'" \
	-o ../linuxio-webserver ./webserver/ && \
	echo "✅ Backend built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: $(PWD)/linuxio-webserver" && \
	echo "🔖 Version: $(GIT_VERSION)" && \
	echo "📊 Size: $$(du -h ../linuxio-webserver | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 ../linuxio-webserver | awk '{ print $$1 }')"

build-bridge: ensure-go
	@echo ""
	@echo "🌉 Building bridge..."
	@echo "📦 Module: $(MODULE_PATH)"
	@echo "🔖 Version: $(GIT_VERSION)"
	@cd "$(BACKEND_DIR)" && \
	GOFLAGS="-buildvcs=false -tags=nomsgpack" \
	go build \
	-ldflags "\
		-s -w \
		-X '$(MODULE_PATH)/common/config.Version=$(GIT_VERSION)' \
		-X '$(MODULE_PATH)/common/config.CommitSHA=$(GIT_COMMIT_SHORT)' \
		-X '$(MODULE_PATH)/common/config.BuildTime=$(BUILD_TIME)'" \
	-o ../linuxio-bridge ./bridge && \
	echo "✅ Bridge built successfully!" && \
	echo "" && \
	echo "Summary:" && \
	echo "📄 Path: $(PWD)/linuxio-bridge" && \
	echo "🔖 Version: $(GIT_VERSION)" && \
	echo "📊 Size: $$(du -h ../linuxio-bridge | cut -f1)" && \
	echo "🔐 SHA256: $$(shasum -a 256 ../linuxio-bridge | awk '{ print $$1 }')"

build-auth:
	@echo ""
	@echo "🛡️  Building Session helper (C)..."
	@set -euo pipefail; \
	LIBS="-lpam"; \
	if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists libsystemd 2>/dev/null; then \
	  LIBS="$$LIBS $$(pkg-config --libs libsystemd)"; \
	  echo "📦 Linking with libsystemd for journald support (via pkg-config)"; \
	elif [ -f /usr/include/systemd/sd-journal.h ]; then \
	  LIBS="$$LIBS -lsystemd"; \
	  echo "📦 Linking with libsystemd for journald support"; \
	else \
	  echo "⚠️  libsystemd-dev not found - bridge logs will go to /dev/null"; \
	  echo "   Install with: sudo apt-get install libsystemd-dev"; \
	fi; \
	$(CC) $(CFLAGS) -DLINUXIO_VERSION=\"$(GIT_VERSION)\" -o linuxio-auth backend/auth/linuxio-auth.c $(LDFLAGS) $$LIBS; \
	if [ "$(STRIP)" = "1" ]; then strip --strip-unneeded linuxio-auth; fi; \
	echo "✅ Session helper built successfully!"; \
	echo "📄 Path: $$PWD/linuxio-auth"; \
	echo "📊 Size: $$(du -h linuxio-auth | cut -f1)"; \
	echo "🔐 SHA256: $$(shasum -a 256 linuxio-auth | awk '{ print $$1 }')"; \
	if command -v checksec >/dev/null 2>&1; then \
	  echo "🔎 checksec:"; checksec --file=linuxio-auth || true; \
	fi

build-cli: ensure-go
	@echo ""
	@echo "🖥️  Building CLI..."
	@cd "$(BACKEND_DIR)" && \
	GOFLAGS="-buildvcs=false" \
	go build \
	-ldflags "\
		-s -w \
		-X '$(MODULE_PATH)/common/config.Version=$(GIT_VERSION)' \
		-X '$(MODULE_PATH)/common/config.CommitSHA=$(GIT_COMMIT_SHORT)' \
		-X '$(MODULE_PATH)/common/config.BuildTime=$(BUILD_TIME)'" \
	-o ../linuxio ./ && \
	echo "✅ CLI built successfully!" && \
	echo "📄 Path: $(PWD)/linuxio" && \
	echo "📊 Size: $$(du -h ../linuxio | cut -f1)"

dev-prep:
	@mkdir -p "$(BACKEND_DIR)/webserver/web/frontend/assets"
	@mkdir -p "$(BACKEND_DIR)/webserver/web/frontend/.vite"
	@touch "$(BACKEND_DIR)/webserver/web/frontend/.vite/manifest.json"
	@touch "$(BACKEND_DIR)/webserver/web/frontend/manifest.json"
	@touch "$(BACKEND_DIR)/webserver/web/frontend/favicon-1.png"
	@touch "$(BACKEND_DIR)/webserver/web/frontend/assets/index-mock.js"

dev: setup dev-prep
	@echo ""
	@echo "🚀 Starting frontend dev server (detached)..."
	@echo "   Backend must be running via: sudo systemctl start linuxio"
	@echo "   Vite proxies /ws and /auth to port 8090"
	@echo "   Vite log: $(VITE_DEV_LOG)"
	@echo ""
	@STARTED_VITE=0
	@cleanup() { \
	  if [ "$$STARTED_VITE" = "1" ]; then \
	    if [ -f "$(VITE_DEV_PID)" ]; then \
	      pid="$$(cat "$(VITE_DEV_PID)")"; \
	      if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
	        kill "$$pid" 2>/dev/null || true; \
	      fi; \
	      rm -f "$(VITE_DEV_PID)"; \
	    fi; \
	    rm -f "$(VITE_DEV_LOG)"; \
	  fi; \
	}
	@if [ -f "$(VITE_DEV_PID)" ] && kill -0 "$$(cat "$(VITE_DEV_PID)")" 2>/dev/null; then \
	  echo "⚠️  Vite already running (pid $$(cat "$(VITE_DEV_PID)"))"; \
	else \
	  rm -f "$(VITE_DEV_PID)"; \
	  nohup bash -c 'cd frontend && exec npx vite --port $(VITE_DEV_PORT)' > "$(VITE_DEV_LOG)" 2>&1 & \
	  echo $$! > "$(VITE_DEV_PID)"; \
	  STARTED_VITE=1; \
	fi
	@if [ -f "$(VITE_DEV_PID)" ]; then \
	  echo "✅ Vite started (pid $$(cat "$(VITE_DEV_PID)"))"; \
	  echo "   ➜  Local:   http://localhost:$(VITE_DEV_PORT)/"; \
	  echo "   Stop with: kill $$(cat "$(VITE_DEV_PID)")"; \
	else \
	  echo "❌ Failed to capture Vite PID. Check $(VITE_DEV_LOG) for details."; \
	fi
	@trap cleanup INT TERM EXIT
	@echo ""
	@echo "📜 Tailing LinuxIO logs (last $(DEV_LOG_LINES) lines)..."
	@linuxio logs $(DEV_LOG_LINES)

# Internal target: build backend + auth + cli (requires bridge already built)
_build-binaries:
	@echo ""
	@echo "🔐 Capturing bridge hash for backend build..."
	@BRIDGE_HASH=$$(shasum -a 256 linuxio-bridge | awk '{ print $$1 }'); \
	echo "   Hash: $$BRIDGE_HASH"; \
	$(MAKE) --no-print-directory build-backend BRIDGE_SHA256=$$BRIDGE_HASH
	@$(MAKE) --no-print-directory build-auth
	@$(MAKE) --no-print-directory build-cli

build: generate test build-vite build-bridge _build-binaries

fastbuild: generate build-bridge _build-binaries

generate:
	@cd "$(BACKEND_DIR)" && go generate ./bridge/handlers/config/init.go

clean:
	@rm -f ./linuxio || true
	@rm -f ./linuxio-webserver || true
	@rm -f ./linuxio-bridge || true
	@rm -f ./linuxio-auth || true
	@rm -rf frontend/node_modules || true
	@rm -f frontend/package-lock.json || true
	@find "$(BACKEND_DIR)/webserver/frontend" -mindepth 1 -exec rm -rf {} + 2>/dev/null || true
	@echo "🧹 Cleaned workspace."

# ========== Installation Targets ==========

uninstall:
	@echo ""
	@echo "🗑️  Uninstalling LinuxIO..."
	@sudo ./packaging/scripts/uninstall.sh

localinstall:
	@echo ""
	@echo "📦 Installing LinuxIO from local build..."
	@sudo ./packaging/scripts/localinstall.sh

reinstall: uninstall fastbuild localinstall
	@echo ""
	@echo "LinuxIO reinstalled successfully!"
	@echo "⚠️  WARNING: Quick & dirty build - no tests executed!"

fullinstall: uninstall
	@echo ""
	@echo "📦 Installing LinuxIO from GitHub repo..."
	@sudo ./packaging/scripts/install-linuxio-binaries.sh

help:
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_BLUE)🛠️  Available commands:$(COLOR_RESET)"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Toolchain setup$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-node      $(COLOR_RESET) Install/activate Node $(NODE_VERSION) via nvm"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-go        $(COLOR_RESET) Install Go $(GO_VERSION) (user-local, no sudo)"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-golint    $(COLOR_RESET) Install golangci-lint (built with local Go 1.25)"
	@$(PRINTC) "$(COLOR_GREEN)    make setup            $(COLOR_RESET) Install frontend dependencies (npm i)"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Quality checks$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make lint             $(COLOR_RESET) Run ESLint (frontend)"
	@$(PRINTC) "$(COLOR_GREEN)    make tsc              $(COLOR_RESET) Type-check with TypeScript (frontend)"
	@$(PRINTC) "$(COLOR_GREEN)    make golint           $(COLOR_RESET) Run gofmt + golangci-lint (backend)"
	@$(PRINTC) "$(COLOR_GREEN)    make test             $(COLOR_RESET) Run lint + tsc + golint + backend tests (optimized)"
	@$(PRINTC) "$(COLOR_GREEN)    make test-backend     $(COLOR_RESET) Run Go unit tests only"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Development$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make dev-prep         $(COLOR_RESET) Create placeholder frontend assets for dev server"
	@$(PRINTC) "$(COLOR_YELLOW)    make dev              $(COLOR_RESET) Start frontend dev server (detached) + tail LinuxIO logs"
	@$(PRINTC) "$(COLOR_YELLOW)    make generate         $(COLOR_RESET) Run go generate on config handlers"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Build$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build            $(COLOR_RESET) Full build (test + frontend + all binaries)"
	@$(PRINTC) "$(COLOR_YELLOW)    make fastbuild        $(COLOR_RESET) Quick build (skip tests)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-vite       $(COLOR_RESET) Build frontend static assets (Vite)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-backend    $(COLOR_RESET) Build Go backend binary"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-bridge     $(COLOR_RESET) Build Go bridge binary"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-auth       $(COLOR_RESET) Build the PAM authentication helper"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-cli        $(COLOR_RESET) Build the CLI tool"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Install / Uninstall$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_RED)    make localinstall     $(COLOR_RESET) Install from local build"
	@$(PRINTC) "$(COLOR_RED)    make reinstall        $(COLOR_RESET) Uninstall + fastbuild + install"
	@$(PRINTC) "$(COLOR_RED)    make fullinstall      $(COLOR_RESET) Uninstall + fastbuild + install from GitHub"
	@$(PRINTC) "$(COLOR_RED)    make uninstall        $(COLOR_RESET) Remove LinuxIO installation"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Run / Clean$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_RED)    make run              $(COLOR_RESET) Run production backend server"
	@$(PRINTC) "$(COLOR_RED)    make clean            $(COLOR_RESET) Remove binaries, node_modules, and generated assets"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Modules$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-module MODULE=<name>       $(COLOR_RESET) Build module (production)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-module-dev MODULE=<name>   $(COLOR_RESET) Build module (development)"
	@$(PRINTC) "$(COLOR_YELLOW)    make watch-module MODULE=<name>       $(COLOR_RESET) Watch mode (auto-rebuild)"
	@$(PRINTC) "$(COLOR_YELLOW)    make deploy-module MODULE=<name>      $(COLOR_RESET) Build + deploy module"
	@$(PRINTC) "$(COLOR_YELLOW)    make list-modules                     $(COLOR_RESET) List all modules"
	@$(PRINTC) ""

# ============================================================================
# Module Build System
# ============================================================================

MODULE ?=
MODULE_DIR := $(CURDIR)/modules/$(MODULE)
INSTALL_DIR := /etc/linuxio/modules/$(MODULE)
BUILD_SCRIPT := $(CURDIR)/packaging/scripts/build-module.sh

# Build module in production mode (optimized)
build-module:
	@if [ -z "$(MODULE)" ]; then \
		echo "Error: MODULE parameter required"; \
		echo "Usage: make build-module MODULE=<name>"; \
		echo ""; \
		echo "To create a new module from template:"; \
		echo "  cp -r module-template modules/my-module"; \
		echo "  make build-module MODULE=my-module"; \
		exit 1; \
	fi
	@if [ ! -d "$(MODULE_DIR)" ]; then \
		echo "Error: Module directory not found: $(MODULE_DIR)"; \
		echo ""; \
		echo "To create a new module from template:"; \
		echo "  cp -r module-template modules/$(MODULE)"; \
		exit 1; \
	fi
	@echo "Building $(MODULE) in production mode..."
	@$(BUILD_SCRIPT) $(MODULE)

# Build module in development mode (source maps, no minification)
build-module-dev:
	@if [ ! -d "$(MODULE_DIR)" ]; then \
		echo "Error: Module directory not found: $(MODULE_DIR)"; \
		exit 1; \
	fi
	@echo "Building $(MODULE) in development mode..."
	@$(BUILD_SCRIPT) $(MODULE) --dev

# Watch mode (auto-rebuild on changes)
watch-module:
	@if [ ! -d "$(MODULE_DIR)" ]; then \
		echo "Error: Module directory not found: $(MODULE_DIR)"; \
		exit 1; \
	fi
	@echo "Starting watch mode for $(MODULE)..."
	@$(BUILD_SCRIPT) $(MODULE) --watch

# Build and deploy module to system
deploy-module: build-module install-module
	@echo ""
	@echo "✅ $(MODULE) deployed successfully!"
	@echo "   Restart LinuxIO: sudo systemctl restart linuxio.target"
	@echo "   View module at: https://localhost:8090/$(MODULE)"

# Install built module to system (requires sudo)
install-module:
	@if [ ! -f "$(MODULE_DIR)/dist/component.js" ]; then \
		echo "Error: Build output not found. Run 'make build-module MODULE=$(MODULE)' first."; \
		exit 1; \
	fi
	@echo "Installing $(MODULE) to $(INSTALL_DIR)..."
	@sudo mkdir -p "$(INSTALL_DIR)/ui"
	@sudo cp "$(MODULE_DIR)/module.yaml" "$(INSTALL_DIR)/"
	@sudo cp "$(MODULE_DIR)/dist/component.js" "$(INSTALL_DIR)/ui/"
	@sudo chmod -R 755 "$(INSTALL_DIR)"
	@echo "✅ Module installed to $(INSTALL_DIR)"

# Uninstall module from system (requires sudo)
uninstall-module:
	@echo "Removing $(MODULE) from system..."
	@sudo rm -rf "$(INSTALL_DIR)"
	@echo "✅ Module uninstalled"
	@echo "   Restart LinuxIO: sudo systemctl restart linuxio.target"

# Clean module build artifacts
clean-module:
	@if [ -d "$(MODULE_DIR)" ]; then \
		echo "Cleaning build artifacts for $(MODULE)..."; \
		rm -rf "$(MODULE_DIR)/dist"; \
		echo "✅ Build artifacts removed"; \
	fi

# List all available modules
list-modules:
	@echo "📦 Installed modules:"
	@module_count=0; \
	for dir in modules/*/; do \
		if [ -f "$$dir/module.yaml" ]; then \
			name=$$(grep "^name:" "$$dir/module.yaml" | awk '{print $$2}'); \
			version=$$(grep "^version:" "$$dir/module.yaml" | awk '{print $$2}'); \
			title=$$(grep "^title:" "$$dir/module.yaml" | sed 's/^title: //'); \
			echo "  • $$name (v$$version) - $$title"; \
			module_count=$$((module_count + 1)); \
		fi \
	done; \
	if [ $$module_count -eq 0 ]; then \
		echo "  (none)"; \
	fi
	@echo ""
	@echo "📋 Module template:"
	@if [ -f "module-template/module.yaml" ]; then \
		name=$$(grep "^name:" "module-template/module.yaml" | awk '{print $$2}'); \
		version=$$(grep "^version:" "module-template/module.yaml" | awk '{print $$2}'); \
		title=$$(grep "^title:" "module-template/module.yaml" | sed 's/^title: //'); \
		echo "  • $$name (v$$version) - $$title"; \
	else \
		echo "  (not found)"; \
	fi
	@echo ""
	@echo "To create a new module from template:"
	@echo "  cp -r module-template modules/my-module"
	@echo "  make build-module MODULE=my-module"

.PHONY: \
  default help clean run \
  build fastbuild _build-binaries build-vite build-backend build-bridge build-auth build-cli \
  dev dev-prep setup test test-backend lint tsc golint lint-only tsc-only golint-only \
  ensure-node ensure-go ensure-golint \
  generate localinstall reinstall fullinstall uninstall print-toolchain-versions \
  build-module build-module-dev watch-module deploy-module install-module uninstall-module clean-module list-modules
