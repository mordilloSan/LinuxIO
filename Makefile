# Default target
.DEFAULT_GOAL := help
default: help

# Include private release automation
-include release.mk

repo_root := $(if $(REPO_ROOT),$(REPO_ROOT),$(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST))))))
REPO_ROOT ?= $(repo_root)
frontend_dir := $(if $(FRONTEND_DIR),$(FRONTEND_DIR),$(repo_root)/frontend)
FRONTEND_DIR ?= $(frontend_dir)
cache_dir := $(if $(CACHE_DIR),$(CACHE_DIR),$(repo_root)/.cache)
CACHE_DIR ?= $(cache_dir)
bin_dir := $(if $(BIN_DIR),$(BIN_DIR),$(repo_root))
BIN_DIR ?= $(bin_dir)
frontend_node_modules_dir := $(frontend_dir)/node_modules
frontend_install_stamp := $(frontend_node_modules_dir)/.package-lock.json
packaging_scripts_dir := $(repo_root)/packaging/scripts
backend_binary := $(bin_dir)/linuxio-webserver
bridge_binary := $(bin_dir)/linuxio-bridge
cli_binary := $(bin_dir)/linuxio
auth_binary := $(bin_dir)/linuxio-auth
docker_update_binary := $(bin_dir)/linuxio-docker-update
indexer_binary := $(bin_dir)/linuxio-indexer
monitoring_binary := $(bin_dir)/linuxio-monitoring

# Quiet aliases capture complete target output in .cache/test-logs while
# printing only a compact success/failure summary. Keep this list limited to
# user-facing validation and analysis targets; implementation targets inherit
# quiet behavior when invoked through one of these aliases.
quiet_targets := \
	test \
	check-actions \
	check-systemd \
	check-frontend \
	check-backend \
	lint \
	lint-ci \
	tsc \
	golint \
	deadcode \
	test-frontend \
	setup-frontend-browser \
	test-frontend-browser \
	test-backend \
	test-go \
	test-auth \
	test-auth-protocol \
	test-auth-pam \
	test-installation-scripts \
	test-indexer-systemd-integration \
	test-updater \
	test-docker-update-integration \
	analyze \
	bundle-metrics \
	compiler-coverage \
	analyze-auth
quiet_aliases := $(addsuffix -quiet,$(quiet_targets))
quiet_log_dir ?= $(cache_dir)/test-logs
quiet_failure_lines ?= 40

# Main flags
VITE_DEV_PORT ?= 3000
DEV_LOG_LINES ?= 25
VITE_DEV_LOG  ?= $(frontend_dir)/.vite-dev.log
VITE_DEV_PID  ?= $(frontend_dir)/.vite-dev.pid
VERBOSE      ?= true
nvm_version := $(if $(NVM_VERSION),$(NVM_VERSION),0.40.2)
NVM_VERSION  ?= $(nvm_version)
GOOS         ?= linux
GOARCH       ?=
GOAMD64      ?= v3
# Host architecture, used to decide amd64-only build tags for a native build.
GOARCH_HOST  ?= $(shell go env GOARCH 2>/dev/null || echo amd64)

# --- Go project root autodetection ---
backend_dir := $(if $(BACKEND_DIR),$(BACKEND_DIR),$(shell \
  if [ -f "$(repo_root)/backend/go.mod" ]; then echo "$(repo_root)/backend"; \
  elif [ -f "$(repo_root)/go.mod" ]; then echo "$(repo_root)"; \
  else echo ""; fi ))
BACKEND_DIR ?= $(backend_dir)
ifeq ($(backend_dir),)
$(error Could not find go.mod in backend/ or project root)
endif
backend_auth_dir := $(backend_dir)/auth
backend_frontend_dir := $(backend_dir)/webserver/web/frontend
backend_frontend_rel_dir := $(patsubst $(repo_root)/%,%,$(backend_frontend_dir))
ifeq ($(wildcard $(frontend_dir)/package.json),)
$(error Could not find frontend package.json under $(frontend_dir))
endif

# Toolchain versions (sourced from repo files)
go_version := $(if $(GO_VERSION),$(GO_VERSION),$(shell awk '/^go / {print $$2; exit}' "$(backend_dir)/go.mod"))
GO_VERSION ?= $(go_version)
node_version := $(if $(NODE_VERSION),$(NODE_VERSION),$(shell python3 -c "import json, pathlib; data=json.loads(pathlib.Path('$(frontend_dir)/package.json').read_text()); print((data.get('engines') or {}).get('node',''))" 2>/dev/null))
NODE_VERSION ?= $(node_version)
CC ?= cc

# Helpers
VERBOSE_FLAG := $(if $(filter true 1 yes on,$(VERBOSE)),--verbose,)
go_tools_dir := $(if $(GO_TOOLS_DIR),$(GO_TOOLS_DIR),$(HOME)/.go)
GO_TOOLS_DIR ?= $(go_tools_dir)
go_toolchain_versions_dir := $(if $(GO_TOOLCHAIN_VERSIONS_DIR),$(GO_TOOLCHAIN_VERSIONS_DIR),$(HOME)/.go-versions)
GO_TOOLCHAIN_VERSIONS_DIR ?= $(go_toolchain_versions_dir)
GO_TOOLCHAIN_DIR := $(go_toolchain_versions_dir)/go$(go_version)
GO_TOOLCHAIN_CURRENT := $(go_toolchain_versions_dir)/current
NVM_DIR ?= $(HOME)/.nvm
export PATH := $(GO_TOOLCHAIN_CURRENT)/bin:$(go_tools_dir)/bin:$(NVM_DIR)/versions/node/current/bin:$(PATH)
NVM_SETUP := export NVM_DIR="$(NVM_DIR)"; \
            [ -s "$$NVM_DIR/nvm.sh" ] && . "$$NVM_DIR/nvm.sh"

# Colors
COLOR_RESET  := \033[0m
COLOR_BLUE   := \033[1;34m
COLOR_GREEN  := \033[1;32m
COLOR_YELLOW := \033[1;33m
COLOR_CYAN   := \033[1;36m
COLOR_RED    := \033[1;31m

PRINTC := printf '%b\n'
CHECKMARK_SED := sed 's/✓/\x1b[1;32m✓\x1b[0m/g;s/✗/\x1b[1;31m✗\x1b[0m/g'
GOTEST_STATUS_SED := sed -E 's/^ok\s+/   \x1b[1;32m✓\x1b[0m /;s/^FAIL\s+/   \x1b[1;31m✗\x1b[0m /'
sha256_cmd ?= sha256sum
setsid_cmd ?= setsid
GOLANGCI_LINT_OPTS ?= --modules-download-mode=mod
# Two deadcode processes need about 3 GiB combined; auto keeps small hosts serial.
# Override with DEADCODE_PARALLEL=0 or 1 when resource limits are known.
DEADCODE_PARALLEL ?= auto
# Successful deadcode results are content-addressed under the ignored .cache tree.
# Set DEADCODE_CACHE=0 to force a fresh scan.
DEADCODE_CACHE ?= 1
# Go's test cache re-runs only packages whose inputs changed. Pass
# GO_TEST_FLAGS=-count=1 to force a full fresh run (or: go clean -testcache).
GO_TEST_FLAGS ?= 
VITEST_MAX_WORKERS ?= 8
VITEST_FILE ?=
VITEST_TEST_NAME ?=
export VITEST_FILE VITEST_MAX_WORKERS VITEST_TEST_NAME
# Extra env vars / build tags injected into build-backend and build-bridge.
# Normally empty; build-leak-profile sets them for pprof debug binaries.
GO_BUILD_EXTRA_ENV ?=
GO_BUILD_TAGS      ?=
GO_BUILD_TAGS_FLAG := $(if $(GO_BUILD_TAGS),-tags $(GO_BUILD_TAGS))
# Linux's monotonic uptime clock cannot jump when the host synchronizes its
# wall clock. Capture it while parsing so `make test` includes prerequisites.
test_timer_start := $(shell awk '{ print $$1 }' /proc/uptime)

MODULE_PATH := $(shell awk '/^module / {print $$2; exit}' "$(backend_dir)/go.mod" 2>/dev/null)
ifeq ($(strip $(MODULE_PATH)),)
$(error Could not determine the Go module path from $(backend_dir)/go.mod)
endif

# --- Git metadata ---
GIT_BRANCH        := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
GIT_TAG           := $(shell git describe --tags --exact-match 2>/dev/null || true)
GIT_COMMIT        := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_COMMIT_SHORT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BRANCH_VERSION    := $(patsubst dev/%,%,$(GIT_BRANCH))
BUILD_TIME        := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# For code counting (built-in find + wc + awk; no external `cloc` dependency)
LOC_INCLUDE_EXT := js,jsx,ts,tsx,css,scss,html,go

define LOC_COUNT_SCRIPT
loc_count() {
  local root="$$1"; local exts_csv="$$2"; local exclude_gen="$$3"
  if [ ! -d "$$root" ]; then echo "(skipped: $$root does not exist)"; return 0; fi
  local -a find_ext_args=()
  local first=1
  local IFS=','
  for ext in $$exts_csv; do
    if [ $$first -eq 1 ]; then first=0; else find_ext_args+=( -o ); fi
    find_ext_args+=( -name "*.$$ext" )
  done
  unset IFS
  find "$$root" \
    \( -type d \( -name node_modules \
                  -o -path "*/$(backend_frontend_rel_dir)" \
                  -o -path "*/$(backend_frontend_rel_dir)/*" \) -prune \) \
    -o -type f \( "$${find_ext_args[@]}" \) -print0 \
    | { if [ "$$exclude_gen" = "1" ]; then \
          grep -zvE '(routeTree\.gen\.ts|\.min\.js)$$'; \
        else cat; fi; } \
    | xargs -0 -r wc -l 2>/dev/null \
    | awk '
        function basename_ext(p,   n, parts) {
          n = split(p, parts, ".");
          return (n > 1 ? parts[n] : "");
        }
        $$1 ~ /^[0-9]+$$/ {
          count = $$1;
          match($$0, /^[ \t]*[0-9]+[ \t]+/);
          path = substr($$0, RSTART+RLENGTH);
          if (path == "total") next;
          ext = basename_ext(path);
          if (ext == "") next;
          lines[ext] += count; files[ext] += 1;
          total += count; total_files += 1;
        }
        END {
          if (total_files == 0) { print "(no files matched)"; exit }
          printf "%-10s %10s %10s\n", "Language", "Files", "Lines";
          printf "%-10s %10s %10s\n", "--------", "-----", "-----";
          for (e in lines) printf "%-10s %10d %10d\n", e, files[e], lines[e];
          printf "%-10s %10s %10s\n", "--------", "-----", "-----";
          printf "%-10s %10d %10d\n", "TOTAL", total_files, total;
        }'
}
endef
export LOC_COUNT_SCRIPT

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

GO_TOOLCHAIN ?= auto
SKIP_ENSURE_GO ?= 0
ifeq ($(SKIP_ENSURE_GO),1)
GO_BIN ?= $(if $(CODEQL_ACTION_GO_BINARY),$(CODEQL_ACTION_GO_BINARY),$(shell command -v go 2>/dev/null || printf 'go'))
GO_BIN_DIR := $(if $(CODEQL_ACTION_GO_BINARY),$(dir $(CODEQL_ACTION_GO_BINARY)),$(dir $(GO_BIN)))
GO_PATH_PREFIX := $(GO_BIN_DIR):$(go_tools_dir)/bin
else
GO_BIN ?= $(GO_TOOLCHAIN_CURRENT)/bin/go
GO_PATH_PREFIX := $(GO_TOOLCHAIN_CURRENT)/bin:$(go_tools_dir)/bin
endif
GO_CMD_ENV := PATH="$(GO_PATH_PREFIX):$$PATH" GOTOOLCHAIN=$(GO_TOOLCHAIN) GOOS="$(GOOS)" $(if $(GOARCH),GOARCH="$(GOARCH)",)
GOLANGCI_LINT_MODULE  := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= latest
golangci_lint := $(if $(GOLANGCI_LINT),$(GOLANGCI_LINT),$(go_tools_dir)/bin/golangci-lint)
GOLANGCI_LINT ?= $(golangci_lint)
MODERNIZE_MODULE      := golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize
MODERNIZE_VERSION     ?= latest
modernize := $(if $(MODERNIZE),$(MODERNIZE),$(go_tools_dir)/bin/modernize)
MODERNIZE ?= $(modernize)
DEADCODE_MODULE       := golang.org/x/tools/cmd/deadcode
DEADCODE_VERSION      ?= latest
deadcode := $(if $(DEADCODE),$(DEADCODE),$(go_tools_dir)/bin/deadcode)
DEADCODE ?= $(deadcode)
GOVULNCHECK_MODULE    := golang.org/x/vuln/cmd/govulncheck
GOVULNCHECK_VERSION   ?= latest
govulncheck := $(if $(GOVULNCHECK),$(GOVULNCHECK),$(go_tools_dir)/bin/govulncheck)
GOVULNCHECK ?= $(govulncheck)
GO_BUILD_PREREQ := ensure-go
ifeq ($(SKIP_ENSURE_GO),1)
GO_BUILD_PREREQ :=
endif

# ---- toolchain --------------------------------------------------------------
UNAME_S  := $(shell uname -s)

# ---- toggles (override on CLI: make build-auth LTO=0 STRIP=0 WERROR=1)
LTO      ?= 1          # enable link-time optimization
STRIP    ?= 1          # strip unneeded symbols after build
WERROR   ?= 0          # treat warnings as errors (good in CI)

ifneq ($(filter-out 0 1,$(LTO) $(STRIP) $(WERROR) $(SKIP_ENSURE_GO) $(DEADCODE_CACHE)),)
$(error LTO, STRIP, WERROR, SKIP_ENSURE_GO and DEADCODE_CACHE must be 0 or 1)
endif
ifneq ($(filter-out auto 0 1,$(DEADCODE_PARALLEL)),)
$(error DEADCODE_PARALLEL must be auto, 0 or 1)
endif
ifneq ($(filter-out true false 0 1 yes no on off,$(VERBOSE)),)
$(error VERBOSE must be true, false, 0, 1, yes, no, on or off)
endif
ifneq ($(filter-out linux,$(GOOS)),)
$(error GOOS must be linux for the LinuxIO backend)
endif
ifneq ($(filter-out amd64 arm64,$(GOARCH)),)
$(error GOARCH must be empty, amd64 or arm64)
endif

ifneq ($(shell if test "$(DEV_LOG_LINES)" -ge 0 2>/dev/null; then printf valid; else printf invalid; fi),valid)
$(error DEV_LOG_LINES must be a non-negative integer)
endif
ifneq ($(shell if test "$(quiet_failure_lines)" -ge 0 2>/dev/null; then printf valid; else printf invalid; fi),valid)
$(error quiet_failure_lines must be a non-negative integer)
endif
ifneq ($(shell if test "$(VITEST_MAX_WORKERS)" -ge 1 2>/dev/null; then printf valid; else printf invalid; fi),valid)
$(error VITEST_MAX_WORKERS must be a positive integer)
endif
ifneq ($(shell if test "$(VITE_DEV_PORT)" -ge 1 2>/dev/null && test "$(VITE_DEV_PORT)" -le 65535 2>/dev/null; then printf valid; else printf invalid; fi),valid)
$(error VITE_DEV_PORT must be an integer from 1 to 65535)
endif

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
HARDEN_LDFLAGS := -Wl,-z,relro -Wl,-z,now -Wl,-z,noexecstack -Wl,-z,separate-code -Wl,-z,nodlopen -pie
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

.PHONY: ensure-node ensure-go ensure-golint ensure-deadcode ensure-modernize ensure-govulncheck
ensure-node:
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🔍 Ensuring Node.js $(NODE_VERSION) is available...$(COLOR_RESET)"
	@if [ ! -d "$(NVM_DIR)" ]; then \
		curl -o- "https://raw.githubusercontent.com/nvm-sh/nvm/v$(nvm_version)/install.sh" | bash; \
	fi
	@bash -lc '\
		$(NVM_SETUP); \
		nvm install $(NODE_VERSION) >/dev/null || true; \
		nvm alias default $(NODE_VERSION); \
		CURR="$$(nvm version $(NODE_VERSION))"; \
		mkdir -p "$(NVM_DIR)/versions/node"; \
		ln -snf "$(NVM_DIR)/versions/node/$$CURR" "$(NVM_DIR)/versions/node/current"; \
		hash -r; \
		echo "   Node path: $$(command -v node)"; \
		echo "   Node version: $$(node -v)"; \
		echo "   NPM version:  $$(npm -v)"; \
		echo "   NPX version:  $$(npx -v)"; \
	'
	@$(PRINTC) "$(COLOR_GREEN)✅ Node.js environment ready!$(COLOR_RESET)"

ensure-go:
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🔍 Ensuring Go $(GO_VERSION) is available...$(COLOR_RESET)"
	@bash -lc '\
		set -euo pipefail; \
		DESIRED="$(GO_VERSION)"; \
		GO_DIR="$(GO_TOOLCHAIN_DIR)"; \
		GO_CURRENT="$(GO_TOOLCHAIN_CURRENT)"; \
		TOOLS_DIR="$(go_tools_dir)"; \
		ARCH="$$(uname -m)"; \
		TARGET_GOOS="$(GOOS)"; \
		if [ -n "$(GOARCH)" ]; then TARGET_GOARCH="$(GOARCH)"; \
		elif [ "$$ARCH" = "x86_64" ] || [ "$$ARCH" = "amd64" ]; then TARGET_GOARCH=amd64; \
		elif [ "$$ARCH" = "aarch64" ] || [ "$$ARCH" = "arm64" ]; then TARGET_GOARCH=arm64; \
		else TARGET_GOARCH=amd64; fi; \
		TARBALL="go$${DESIRED}.$${TARGET_GOOS}-$${TARGET_GOARCH}.tar.gz"; \
		URL="https://go.dev/dl/$${TARBALL}"; \
		\
		read_go_version() { \
		  root="$$1"; \
		  if [ -f "$$root/VERSION" ]; then \
		    sed -n "1s/^go//p" "$$root/VERSION" 2>/dev/null; \
		  elif [ -x "$$root/bin/go" ]; then \
		    "$$root/bin/go" version 2>/dev/null | awk "{print \$$3}" | sed "s/^go//"; \
		  fi; \
		}; \
		CUR=""; \
		if [ -x "$$GO_DIR/bin/go" ]; then \
		  CUR="$$(read_go_version "$$GO_DIR")"; \
		fi; \
		if [ "$$CUR" = "$$DESIRED" ]; then \
		  echo "✅ Go $$CUR already available at $$GO_DIR"; \
		else \
		  echo "📥 Downloading $$URL"; \
		  TMP="$$(mktemp -d)"; \
		  trap "rm -rf \"$$TMP\"" EXIT; \
		  curl -fsSL "$$URL" -o "$$TMP/$$TARBALL"; \
		  mkdir -p "$$(dirname "$$GO_DIR")"; \
		  rm -rf "$$GO_DIR"; \
		  tar -C "$$TMP" -xzf "$$TMP/$$TARBALL"; \
		  mv "$$TMP/go" "$$GO_DIR"; \
		  echo "✅ Installed Go $$DESIRED to $$GO_DIR"; \
		fi; \
		mkdir -p "$$(dirname "$$GO_CURRENT")" "$$TOOLS_DIR/bin"; \
		ln -sfnT "$$GO_DIR" "$$GO_CURRENT"; \
		if ! grep -Fq "$$GO_CURRENT/bin" "$$HOME/.bashrc" 2>/dev/null; then \
		  echo "export PATH=$$GO_CURRENT/bin:$$TOOLS_DIR/bin:\$$PATH" >> "$$HOME/.bashrc"; \
		fi; \
		FINAL_VERSION="$$( "$$GO_CURRENT/bin/go" version 2>/dev/null || true )"; \
		if ! printf "%s\n" "$$FINAL_VERSION" | grep -Fq "go$$DESIRED "; then \
		  echo "❌ Expected Go $$DESIRED, got: $${FINAL_VERSION:-not found}"; \
		  exit 1; \
		fi; \
		echo "✅ Go is ready!"; \
	'

ensure-golint: ensure-go
	@{ set -euo pipefail; \
	   bin="$(golangci_lint)"; need=1; \
	   if [ -x "$$bin" ]; then \
	     out="$$( "$$bin" version 2>/dev/null || true)"; \
	     ver="$$( printf '%s' "$$out" | sed -n 's/^golangci-lint has version[[:space:]]\([v0-9.]\+\).*/\1/p' )"; \
	     ver_no_v="$${ver#v}"; major="$${ver_no_v%%.*}"; \
	     built_ok="$$( printf '%s' "$$out" | grep -Eq 'built with go$(subst .,\.,$(GO_VERSION))([[:space:]]|$$)' && echo yes || echo no )"; \
	     if [ "$$major" = "2" ] && [ "$$built_ok" = "yes" ]; then need=0; fi; \
	   fi; \
	   if [ $$need -eq 1 ]; then \
	     echo "📥 Installing golangci-lint $(GOLANGCI_LINT_VERSION) (v2) with local Go ($(GO_BIN))..."; \
	     rm -f "$$bin" || true; \
	     $(GO_CMD_ENV) GOBIN="$(go_tools_dir)/bin" GOFLAGS="-buildvcs=false" \
	       "$(GO_BIN)" install "$(GOLANGCI_LINT_MODULE)@$(GOLANGCI_LINT_VERSION)"; \
	   fi; \
	   "$$bin" version | head -n1; \
	   out="$$( "$$bin" version )"; \
	   ver="$$( printf '%s' "$$out" | sed -n 's/^golangci-lint has version[[:space:]]\([v0-9.]\+\).*/\1/p' )"; \
	   ver_no_v="$${ver#v}"; major="$${ver_no_v%%.*}"; \
	   [ "$$major" = "2" ] || { echo " not a v2 golangci-lint"; exit 1; }; \
	   echo "$$out" | grep -Eq 'built with go$(subst .,\.,$(GO_VERSION))([[:space:]]|$$)' || { echo " golangci-lint not built with Go $(GO_VERSION)"; exit 1; }; \
	   echo "✅ golangci-lint v2 ready."; \
	}

ensure-deadcode: ensure-go
	@{ set -euo pipefail; \
	   bin="$(deadcode)"; need=1; \
	   if [ -x "$$bin" ]; then \
	     build_info="$$( $(GO_CMD_ENV) "$(GO_BIN)" version -m "$$bin" 2>/dev/null || true )"; \
	     tool_go_version="$$(printf '%s\n' "$$build_info" | sed -n '1s/.*: go\([^ ]*\).*/\1/p')"; \
	     if [ "$$tool_go_version" = "$(GO_VERSION)" ] && "$$bin" -h >/dev/null 2>&1; then need=0; fi; \
	   fi; \
	   if [ $$need -eq 1 ]; then \
	     echo "📥 Installing deadcode $(DEADCODE_VERSION) with local Go ($(GO_BIN))..."; \
	     rm -f "$$bin" || true; \
	     $(GO_CMD_ENV) GOBIN="$(go_tools_dir)/bin" GOFLAGS="-buildvcs=false" \
	       "$(GO_BIN)" install "$(DEADCODE_MODULE)@$(DEADCODE_VERSION)"; \
	   fi; \
	   "$$bin" -h >/dev/null 2>&1 || { echo "❌ deadcode is installed but not runnable"; exit 1; }; \
	   echo "✅ deadcode ready."; \
	}

ensure-modernize: ensure-go
	@{ set -euo pipefail; \
	   bin="$(modernize)"; need=1; \
	   if [ -x "$$bin" ]; then \
	     build_info="$$( $(GO_CMD_ENV) "$(GO_BIN)" version -m "$$bin" 2>/dev/null || true )"; \
	     tool_go_version="$$(printf '%s\n' "$$build_info" | sed -n '1s/.*: go\([^ ]*\).*/\1/p')"; \
	     if [ "$$tool_go_version" = "$(GO_VERSION)" ] && "$$bin" -h >/dev/null 2>&1; then need=0; fi; \
	   fi; \
	   if [ $$need -eq 1 ]; then \
	     echo "📥 Installing modernize $(MODERNIZE_VERSION) with local Go ($(GO_BIN))..."; \
	     rm -f "$$bin" || true; \
	     $(GO_CMD_ENV) GOBIN="$(go_tools_dir)/bin" GOFLAGS="-buildvcs=false" \
	       "$(GO_BIN)" install "$(MODERNIZE_MODULE)@$(MODERNIZE_VERSION)"; \
	   fi; \
	   "$$bin" -h >/dev/null 2>&1 || { echo "❌ modernize is installed but not runnable"; exit 1; }; \
	   echo "✅ modernize ready."; \
	}

ensure-govulncheck: ensure-go
	@{ set -euo pipefail; \
	   bin="$(govulncheck)"; need=1; \
	   if [ -x "$$bin" ]; then \
	     build_info="$$( $(GO_CMD_ENV) "$(GO_BIN)" version -m "$$bin" 2>/dev/null || true )"; \
	     tool_go_version="$$(printf '%s\n' "$$build_info" | sed -n '1s/.*: go\([^ ]*\).*/\1/p')"; \
	     if [ "$$tool_go_version" = "$(GO_VERSION)" ] && "$$bin" -h >/dev/null 2>&1; then need=0; fi; \
	   fi; \
	   if [ $$need -eq 1 ]; then \
	     echo "📥 Installing govulncheck $(GOVULNCHECK_VERSION) with local Go ($(GO_BIN))..."; \
	     rm -f "$$bin" || true; \
	     $(GO_CMD_ENV) GOBIN="$(go_tools_dir)/bin" GOFLAGS="-buildvcs=false" \
	       "$(GO_BIN)" install "$(GOVULNCHECK_MODULE)@$(GOVULNCHECK_VERSION)"; \
	   fi; \
	   "$$bin" -h >/dev/null 2>&1 || { echo "❌ govulncheck is installed but not runnable"; exit 1; }; \
	   echo "✅ govulncheck ready."; \
	}

.PHONY: setup update-deps
setup: $(frontend_install_stamp)
	@:

$(frontend_install_stamp): $(frontend_dir)/package.json $(frontend_dir)/package-lock.json
	@echo ""
	@echo "📦 Installing frontend dependencies..."
	@cd "$(frontend_dir)" && npm install --silent
	@test -f "$@"
	@echo "✅ Frontend dependencies installed!"

update-deps: ensure-node ensure-go
	@echo ""
	@echo "📦 Frontend dependency update (npm → latest)"
	@bash -c '\
	  set -euo pipefail; \
	  cd "$(frontend_dir)"; \
	  echo ""; \
	  echo "🔎 Current outdated packages:"; \
	  npm outdated || true; \
	  echo ""; \
	  echo "⬆️  Bumping package.json to latest with npm-check-updates..."; \
	  npx --yes npm-check-updates -u; \
	  echo ""; \
	  echo "🔄 Refreshing lockfile + node_modules (npm install)..."; \
	  npm install --no-audit --no-fund; \
	  echo ""; \
	  echo "🛡️  Applying available npm audit fixes..."; \
	  if ! npm audit fix --no-fund; then \
	    echo "⚠️  npm audit fix did not complete; checking for remaining vulnerabilities." >&2; \
	  fi; \
	  echo ""; \
	  if ! npm audit --audit-level=low >/dev/null 2>&1; then \
	    echo "⚠️  npm vulnerabilities remain after npm audit fix; review the audit report above." >&2; \
	    npm audit || true; \
	  fi; \
	  echo ""; \
	  echo "🔎 Remaining outdated after update:"; \
	  npm outdated || true; \
	  echo ""; \
	  echo "✅ Frontend dependencies updated to latest!"; \
	'
	@echo ""
	@echo "📦 Go dependency update (go get -u -t ./...)"
	@cd "$(backend_dir)" && $(GO_CMD_ENV) "$(GO_BIN)" get -u -t ./...
	@cd "$(backend_dir)" && $(GO_CMD_ENV) "$(GO_BIN)" mod tidy
	@echo "✅ Go dependencies updated to latest!"

# Separate lint/tsc targets that include all prerequisites (delegate to -only variants)
.PHONY: lint tsc lint-ci golint test check-actions check-systemd check-frontend check-backend test-frontend test-frontend-ci setup-frontend-browser test-frontend-browser test-frontend-only test-auth test-auth-protocol test-auth-pam test-installation-scripts test-indexer-systemd-integration test-updater test-docker-update-integration lint-only lint-ci-only tsc-only tsc-ci golint-only test-backend test-go deadcode deadcode-only ci-frontend-deps update-frontend-screenshots
check-actions:
	@command -v actionlint >/dev/null 2>&1 || { echo "❌ actionlint is required" >&2; exit 1; }
	@actionlint

check-systemd:
	@command -v systemd-analyze >/dev/null 2>&1 || { echo "❌ systemd-analyze is required" >&2; exit 1; }
	@set -e; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp packaging/systemd/linuxio*.service packaging/systemd/linuxio*.socket packaging/systemd/linuxio*.target packaging/systemd/linuxio*.timer "$$tmp"; \
	sed -Ei 's|^ExecStart=-?[^ ]+|ExecStart=/bin/true|' "$$tmp"/*.service; \
	systemd-analyze verify --man=no "$$tmp"/linuxio*.service "$$tmp"/linuxio*.socket "$$tmp"/linuxio*.target "$$tmp"/linuxio*.timer

lint: ensure-node setup
	@$(MAKE) --no-print-directory lint-only

# CI installs are immutable and linting is read-only. Keep `setup`/`lint` as
# the developer workflow: those targets intentionally use npm install and
# auto-fixing lint tools so local changes can be repaired in place.
ci-frontend-deps:
	@command -v npm >/dev/null 2>&1 || { echo "❌ npm is required (use actions/setup-node in CI)." >&2; exit 1; }
	@echo "📦 Installing frontend dependencies from the lockfile..."
	@cd "$(frontend_dir)" && npm ci --no-audit --no-fund

lint-ci: ci-frontend-deps
	@$(MAKE) --no-print-directory lint-ci-only

lint-ci-only:
	@set -euo pipefail
	@echo "🔎 Running read-only frontend lint and formatting checks..."
	@cd "$(frontend_dir)" && \
	  ./node_modules/.bin/oxlint --type-aware -c config/.oxlintrc.json \
	    src config scripts/compiler-coverage.mjs scripts/run-browser-fixture.mjs
	@cd "$(frontend_dir)" && \
	  ./node_modules/.bin/oxfmt -c config/.oxfmtrc.json --check \
	    --no-error-on-unmatched-pattern "src/**/*.js" "src/**/*.jsx" \
	    "src/**/*.ts" "src/**/*.tsx" "src/test/browser/**/*.html" \
	    "!src/routeTree.gen.ts" "config/**/*.ts" \
	    "scripts/compiler-coverage.mjs" "scripts/run-browser-fixture.mjs"
	@echo "✅ Frontend linting and formatting checks passed!"

tsc: ensure-node setup
	@$(MAKE) --no-print-directory tsc-only

tsc-ci: ci-frontend-deps
	@$(MAKE) --no-print-directory tsc-only

golint: ensure-golint ensure-modernize ensure-govulncheck
	@$(MAKE) --no-print-directory golint-only

# Two independent lanes, serial inside each lane:
#
#   frontend: lint-only ─→ tsc-only ─→ test-frontend-only
#   backend:  golint-only ─→ test-backend ─→ deadcode-only
#
# The lane heads must come first because they rewrite sources in place
# (oxlint --fix / oxfmt; golangci-lint fmt / modernize -fix / go mod tidy), so
# nothing may read those trees until they finish. The rest is scheduling, not
# dependency: every job here already saturates ~2 cores on its own, so running
# four of them at once on a 4-core box costs more in contention than it buys in
# overlap. Two lanes keeps demand near the core count and puts the long pole
# (frontend unit tests, ~58% of the suite's total CPU) in flight early, with the
# cheaper backend lane filling the other half of the machine underneath it.
#
# Serializing test-backend before deadcode-only also stops them from compiling
# the same 58 packages simultaneously and fighting over the Go build cache.
#
# The frontend lane is normally the critical path. Deprioritising the backend
# lane with nice(1) was measured and made no material difference.
#
# Execution order is fixed by the lane chains; the follow() order below is only
# how output is replayed, and does not constrain what runs when.
test: ensure-node ensure-go ensure-golint ensure-modernize ensure-govulncheck ensure-deadcode setup dev-prep test-installation-scripts
	@set -uo pipefail; \
	ST=0; \
	FRONTEND_LINT_WARNINGS_FILE="$$(mktemp)"; \
	TMPDIR_JOBS="$$(mktemp -d)"; \
	PIDS=""; \
	cleanup() { rc=$$?; trap - EXIT INT TERM; \
	  for pid in $$PIDS; do kill "$$pid" 2>/dev/null || true; done; \
	  for pid in $$PIDS; do wait "$$pid" 2>/dev/null || true; done; \
	  rm -f "$$FRONTEND_LINT_WARNINGS_FILE"; rm -rf "$$TMPDIR_JOBS"; \
	  exit "$$rc"; \
	}; \
	trap cleanup EXIT; \
	trap 'exit 130' INT; \
	trap 'exit 143' TERM; \
	export FRONTEND_LINT_WARNINGS_FILE; \
	follow() { tail -n +1 -f -s 0.1 --pid="$$2" "$$1"; wait "$$2"; }; \
	await() { status_file="$$1"; producer_pid="$$2"; \
	  while [ ! -f "$$status_file" ]; do \
	    kill -0 "$$producer_pid" 2>/dev/null || return 1; \
	    sleep 0.1; \
	  done; \
	}; \
	stage() { name="$$1"; shift; \
	  "$$@" > "$$TMPDIR_JOBS/$$name" 2>&1 & command_pid=$$!; \
	  trap 'kill "$$command_pid" 2>/dev/null || true; wait "$$command_pid" 2>/dev/null || true; exit 130' INT; \
	  trap 'kill "$$command_pid" 2>/dev/null || true; wait "$$command_pid" 2>/dev/null || true; exit 143' TERM; \
	  wait "$$command_pid"; rc=$$?; trap - INT TERM; \
	  printf '%s\n' "$$rc" > "$$TMPDIR_JOBS/$$name.rc"; return "$$rc"; \
	}; \
	touch "$$TMPDIR_JOBS/lint" "$$TMPDIR_JOBS/golint" "$$TMPDIR_JOBS/tsc" \
	      "$$TMPDIR_JOBS/fe" "$$TMPDIR_JOBS/be" "$$TMPDIR_JOBS/dead"; \
	stage lint   $(MAKE) --no-print-directory lint-only   & PID_LINT=$$!; PIDS="$$PIDS $$PID_LINT"; \
	stage golint $(MAKE) --no-print-directory golint-only & PID_GOLINT=$$!; PIDS="$$PIDS $$PID_GOLINT"; \
	( await "$$TMPDIR_JOBS/lint.rc" "$$PID_LINT" || exit $$?; stage tsc  $(MAKE) --no-print-directory tsc-only ) & PID_TSC=$$!; PIDS="$$PIDS $$PID_TSC"; \
	( await "$$TMPDIR_JOBS/tsc.rc" "$$PID_TSC" || exit $$?; stage fe   $(MAKE) --no-print-directory test-frontend-only ) & PID_FE=$$!; PIDS="$$PIDS $$PID_FE"; \
	( await "$$TMPDIR_JOBS/golint.rc" "$$PID_GOLINT" || exit $$?; stage be   $(MAKE) --no-print-directory test-backend SKIP_ENSURE_GO=1 ) & PID_BE=$$!; PIDS="$$PIDS $$PID_BE"; \
	( await "$$TMPDIR_JOBS/be.rc" "$$PID_BE" || exit $$?; stage dead $(MAKE) --no-print-directory deadcode-only ) & PID_DEAD=$$!; PIDS="$$PIDS $$PID_DEAD"; \
	follow "$$TMPDIR_JOBS/lint"   $$PID_LINT   || ST=1; \
	follow "$$TMPDIR_JOBS/golint" $$PID_GOLINT || ST=1; \
	$(PRINTC) ""; \
	follow "$$TMPDIR_JOBS/tsc"    $$PID_TSC    || ST=1; \
	$(PRINTC) ""; \
	follow "$$TMPDIR_JOBS/be"     $$PID_BE     || ST=1; \
	$(PRINTC) ""; \
	follow "$$TMPDIR_JOBS/dead"   $$PID_DEAD   || true; \
	$(PRINTC) ""; \
	follow "$$TMPDIR_JOBS/fe"     $$PID_FE     || ST=1; \
	if [ -s "$$FRONTEND_LINT_WARNINGS_FILE" ]; then \
	  FRONTEND_LINT_WARNINGS="$$(tail -n 1 "$$FRONTEND_LINT_WARNINGS_FILE")"; \
	  $(PRINTC) "\n$(COLOR_YELLOW)⚠️  All checks completed with $$FRONTEND_LINT_WARNINGS frontend lint warning(s).$(COLOR_RESET)"; \
	  $(PRINTC) "$(COLOR_YELLOW)   Warnings are non-blocking; review the Oxlint output above or run 'make lint'.$(COLOR_RESET)"; \
	fi; \
	ELAPSED="$$(awk -v start="$(test_timer_start)" -v end="$$(awk '{ print $$1 }' /proc/uptime)" 'BEGIN { printf "%.1f", end - start }')"; \
	if [ $$ST -ne 0 ]; then \
	  $(PRINTC) "\n$(COLOR_RED)❌ Some checks failed.$(COLOR_RESET) $(COLOR_CYAN)(⏱️  $${ELAPSED}s)$(COLOR_RESET)"; \
	  exit 1; \
	fi; \
	$(PRINTC) "\n$(COLOR_GREEN)✅ All checks passed!$(COLOR_RESET) $(COLOR_CYAN)(⏱️  $${ELAPSED}s)$(COLOR_RESET)"

check-frontend: ensure-node setup
	@set -uo pipefail; \
	ST=0; \
	FRONTEND_LINT_WARNINGS_FILE="$$(mktemp)"; \
	TMPDIR_JOBS="$$(mktemp -d)"; \
	trap 'rm -f "$$FRONTEND_LINT_WARNINGS_FILE"; rm -rf "$$TMPDIR_JOBS"' EXIT; \
	export FRONTEND_LINT_WARNINGS_FILE; \
	follow() { tail -n +1 -f -s 0.1 --pid="$$2" "$$1"; wait "$$2"; }; \
	$(MAKE) --no-print-directory lint-only || ST=1; \
	$(PRINTC) ""; \
	touch "$$TMPDIR_JOBS/tsc" "$$TMPDIR_JOBS/fe"; \
	$(MAKE) --no-print-directory tsc-only           > "$$TMPDIR_JOBS/tsc" 2>&1 & PID_TSC=$$!; \
	$(MAKE) --no-print-directory test-frontend-only > "$$TMPDIR_JOBS/fe"  2>&1 & PID_FE=$$!; \
	follow "$$TMPDIR_JOBS/tsc" $$PID_TSC || ST=1; \
	$(PRINTC) ""; \
	follow "$$TMPDIR_JOBS/fe"  $$PID_FE  || ST=1; \
	if [ -s "$$FRONTEND_LINT_WARNINGS_FILE" ]; then \
	  FRONTEND_LINT_WARNINGS="$$(tail -n 1 "$$FRONTEND_LINT_WARNINGS_FILE")"; \
	  $(PRINTC) "\n$(COLOR_YELLOW)⚠️  Frontend checks completed with $$FRONTEND_LINT_WARNINGS lint warning(s).$(COLOR_RESET)"; \
	  $(PRINTC) "$(COLOR_YELLOW)   Warnings are non-blocking; review the Oxlint output above or run 'make lint'.$(COLOR_RESET)"; \
	fi; \
	if [ $$ST -ne 0 ]; then \
	  $(PRINTC) "\n$(COLOR_RED)❌ Frontend checks failed.$(COLOR_RESET)"; \
	  exit 1; \
	fi; \
	$(PRINTC) "\n$(COLOR_GREEN)✅ Frontend checks passed!$(COLOR_RESET)"

# Fully serial: test-backend and deadcode-only both compile the whole backend,
# so overlapping them duplicates the work and contends on the Go build cache.
# Each already uses ~2 cores on its own, so there is nothing to gain by pairing.
check-backend: ensure-go ensure-golint ensure-modernize ensure-govulncheck ensure-deadcode
	@set -uo pipefail; \
	ST=0; \
	$(MAKE) --no-print-directory golint-only || ST=1; \
	$(PRINTC) ""; \
	$(MAKE) --no-print-directory test-backend SKIP_ENSURE_GO=1 || ST=1; \
	$(PRINTC) ""; \
	$(MAKE) --no-print-directory deadcode-only || true; \
	if [ $$ST -ne 0 ]; then \
	  $(PRINTC) "\n$(COLOR_RED)❌ Backend checks failed.$(COLOR_RESET)"; \
	  exit 1; \
	fi; \
	$(PRINTC) "\n$(COLOR_GREEN)✅ Backend checks passed!$(COLOR_RESET)"

test-frontend: ensure-node setup
	@$(MAKE) --no-print-directory test-frontend-only

test-frontend-ci: ci-frontend-deps
	@$(MAKE) --no-print-directory test-frontend-only

setup-frontend-browser: ensure-node setup
	@echo "🌐 Installing Playwright Chromium..."
	@cd "$(frontend_dir)" && ./node_modules/.bin/playwright install chromium
	@echo "✅ Playwright Chromium installed!"

test-frontend-browser: ensure-node setup
	@set -e
	@echo "🏗️  Building the production frontend for chunk-boundary checks..."
	@cd "$(frontend_dir)" && ./node_modules/.bin/vite build --config config/vite.config.ts --configLoader native
	@echo "🌐 Running frontend browser tests..."
	@cd "$(frontend_dir)" && ./node_modules/.bin/playwright test --config config/playwright.config.ts
	@echo "✅ Frontend browser tests passed!"

# Rewrites the screenshot baselines src/test/browser/styling-gallery.spec.ts
# compares against. Run after a deliberate visual change, then review the PNG
# diff in git; the gallery does not need the production build.
update-frontend-screenshots: ensure-node setup
	@echo "📸 Rewriting frontend screenshot baselines..."
	@cd "$(frontend_dir)" && ./node_modules/.bin/playwright test --config config/playwright.config.ts --update-snapshots=all src/test/browser/styling-gallery.spec.ts
	@echo "✅ Screenshot baselines updated!"

test-frontend-only:
	@set -uo pipefail; \
	$(PRINTC) "$(COLOR_CYAN)🧪 Running frontend unit tests...$(COLOR_RESET)"; \
	runner_pid=""; \
	stop_runner() { signal="$$1"; rc="$$2"; trap - EXIT HUP INT TERM; \
	  if [ -n "$${runner_pid:-}" ]; then \
	    kill -s "$$signal" -- "-$$runner_pid" 2>/dev/null || true; \
	    wait "$$runner_pid" 2>/dev/null || true; \
	    kill -s KILL -- "-$$runner_pid" 2>/dev/null || true; \
	  fi; \
	  exit "$$rc"; \
	}; \
	"$(setsid_cmd)" bash -o pipefail -c ' \
	  set -uo pipefail; \
	  cd "$$1"; \
	  args=(run --config config/vitest.config.ts --reporter=default "--maxWorkers=$$VITEST_MAX_WORKERS"); \
	  if [ -n "$$VITEST_FILE" ]; then \
	    case "$$VITEST_FILE" in \
	      /*|../*|*/../*|*/..) echo "VITEST_FILE must stay within the frontend directory" >&2; exit 2 ;; \
	      *.test.js|*.test.jsx|*.test.ts|*.test.tsx|*.spec.js|*.spec.jsx|*.spec.ts|*.spec.tsx) ;; \
	      *) echo "VITEST_FILE must name a test or spec file" >&2; exit 2 ;; \
	    esac; \
	    [ -f "$$VITEST_FILE" ] || { echo "VITEST_FILE does not exist: $$VITEST_FILE" >&2; exit 2; }; \
	    args+=("$$VITEST_FILE"); \
	  fi; \
	  if [ -n "$$VITEST_TEST_NAME" ]; then args+=(--testNamePattern "$$VITEST_TEST_NAME"); fi; \
	  ./node_modules/.bin/vitest "$${args[@]}"; \
	  rc=$$?; \
	  if [ "$$rc" -eq 0 ]; then printf "\\033[1;32m%s\\033[0m\\n" "✅ Frontend unit tests passed!"; fi; \
	  exit "$$rc" \
	' _ "$(frontend_dir)" & runner_pid=$$!; \
	trap 'stop_runner TERM 129' HUP; \
	trap 'stop_runner TERM 130' INT; \
	trap 'stop_runner TERM 143' TERM; \
	trap 'stop_runner TERM $$?' EXIT; \
	if wait "$$runner_pid"; then rc=0; else rc=$$?; fi; \
	trap - EXIT HUP INT TERM; \
	exit "$$rc"

test-auth: check-c-build-deps
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running C authentication helper tests...$(COLOR_RESET)"
	@set -euo pipefail; \
	TEST_DIR="$$(mktemp -d)"; \
	trap 'rm -rf "$$TEST_DIR"' EXIT; \
	TEST_BIN="$$TEST_DIR/linuxio-auth-test"; \
	LIBS="-lpam"; \
	if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists libsystemd 2>/dev/null; then \
	  LIBS="$$LIBS $$(pkg-config --libs libsystemd)"; \
	else \
	  LIBS="$$LIBS -lsystemd"; \
	fi; \
	$(CC) $(CFLAGS) -Werror -DLINUXIO_VERSION=\"test\" \
	  -o "$$TEST_BIN" "$(backend_auth_dir)/linuxio-auth_test.c" $(LDFLAGS) $$LIBS; \
	"$$TEST_BIN" | $(CHECKMARK_SED); \
	$(PRINTC) "$(COLOR_GREEN)✅ C authentication helper tests passed!$(COLOR_RESET)"

test-auth-protocol: check-c-build-deps $(GO_BUILD_PREREQ)
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running cross-language auth protocol tests...$(COLOR_RESET)"
	@set -euo pipefail; \
	TEST_DIR="$$(mktemp -d)"; \
	trap 'rm -rf "$$TEST_DIR"' EXIT; \
	TEST_BIN="$$TEST_DIR/linuxio-auth-frametool"; \
	LIBS="-lpam"; \
	if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists libsystemd 2>/dev/null; then \
	  LIBS="$$LIBS $$(pkg-config --libs libsystemd)"; \
	else \
	  LIBS="$$LIBS -lsystemd"; \
	fi; \
	$(CC) $(CFLAGS) -Werror -DLINUXIO_VERSION=\"test\" \
	  -o "$$TEST_BIN" "$(backend_auth_dir)/linuxio-auth-frametool.c" $(LDFLAGS) $$LIBS; \
	cd "$(backend_dir)" && LINUXIO_AUTH_FRAMETOOL="$$TEST_BIN" $(GO_CMD_ENV) \
	  "$(GO_BIN)" test ./common/ipc/auth -run TestCrossLanguage -count=1 | $(GOTEST_STATUS_SED); \
	$(PRINTC) "$(COLOR_GREEN)✅ Cross-language auth protocol tests passed!$(COLOR_RESET)"

# Tier-1 hermetic PAM/host-integration suite for the C launcher. Runs the
# real handle_client() against a pam_wrapper/pam_matrix PAM stack, an
# nss_wrapper user database, uid_wrapper emulated-root privilege drops, a
# stub bridge exec'd through the fixed fd layout, and tmpfile accounting
# databases. Requires the cwrap wrapper libraries
# (apt install libpam-wrapper libnss-wrapper libuid-wrapper); skips with a
# warning when they are missing. Non-standard library location:
#   make test-auth-pam LINUXIO_CWRAP_LIBDIR=/path/containing/the/so/files
test-auth-pam: check-c-build-deps
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running hermetic PAM integration tests (pam_wrapper)...$(COLOR_RESET)"
	@set -euo pipefail; \
	CWRAP_DIR="$(LINUXIO_CWRAP_LIBDIR)"; \
	find_lib() { \
	  for f in $${CWRAP_DIR:+"$$CWRAP_DIR/$$1"} /usr/lib/*/"$$1" /usr/lib/"$$1" /usr/lib64/"$$1" /usr/local/lib/"$$1"; do \
	    if [ -e "$$f" ]; then echo "$$f"; return 0; fi; \
	  done; return 1; \
	}; \
	PAM_WRAPPER_LIB="$$(find_lib libpam_wrapper.so || true)"; \
	NSS_WRAPPER_LIB="$$(find_lib libnss_wrapper.so || true)"; \
	UID_WRAPPER_LIB="$$(find_lib libuid_wrapper.so || true)"; \
	PAM_MATRIX_LIB="$$(find_lib pam_wrapper/pam_matrix.so || true)"; \
	if [ -z "$$PAM_WRAPPER_LIB" ] || [ -z "$$NSS_WRAPPER_LIB" ] || \
	   [ -z "$$UID_WRAPPER_LIB" ] || [ -z "$$PAM_MATRIX_LIB" ]; then \
	  echo "⚠️  Skipping PAM integration tests: cwrap wrapper libraries not found."; \
	  echo "   Install them with: apt install libpam-wrapper libnss-wrapper libuid-wrapper"; \
	  echo "   (or pass LINUXIO_CWRAP_LIBDIR=<dir containing the .so files>)"; \
	  exit 0; \
	fi; \
	TEST_DIR="$$(mktemp -d)"; \
	trap 'rm -rf "$$TEST_DIR"' EXIT; \
	LIBS="-lpam"; \
	if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists libsystemd 2>/dev/null; then \
	  LIBS="$$LIBS $$(pkg-config --libs libsystemd)"; \
	else \
	  LIBS="$$LIBS -lsystemd"; \
	fi; \
	$(CC) -shared -fPIC -O2 -Wall -Wextra -Werror \
	  -o "$$TEST_DIR/pam_linuxio_probe.so" "$(backend_auth_dir)/testdata/pam_linuxio_probe.c" -lpam; \
	$(CC) -O2 -Wall -Wextra -Werror \
	  -o "$$TEST_DIR/bridge-stub" "$(backend_auth_dir)/testdata/linuxio-test-bridge.c"; \
	$(CC) $(CFLAGS) -Werror -DLINUXIO_VERSION=\"test\" \
	  -o "$$TEST_DIR/linuxio-auth-pam-test" "$(backend_auth_dir)/linuxio-auth-pam_test.c" $(LDFLAGS) $$LIBS; \
	mkdir -p "$$TEST_DIR/pam.d"; \
	sed -e "s|@PROBE_MODULE@|$$TEST_DIR/pam_linuxio_probe.so|g" \
	    -e "s|@PAM_MATRIX@|$$PAM_MATRIX_LIB|g" \
	    -e "s|@PASSDB@|$$TEST_DIR/passdb|g" \
	    "$(backend_auth_dir)/testdata/linuxio.pam.in" > "$$TEST_DIR/pam.d/linuxio"; \
	: > "$$TEST_DIR/passwd"; : > "$$TEST_DIR/group"; \
	cd "$$TEST_DIR" && env -u JOURNAL_STREAM \
	  LD_PRELOAD="$$PAM_WRAPPER_LIB:$$NSS_WRAPPER_LIB:$$UID_WRAPPER_LIB" \
	  PAM_WRAPPER=1 \
	  PAM_WRAPPER_SERVICE_DIR="$$TEST_DIR/pam.d" \
	  NSS_WRAPPER_PASSWD="$$TEST_DIR/passwd" \
	  NSS_WRAPPER_GROUP="$$TEST_DIR/group" \
	  UID_WRAPPER=1 UID_WRAPPER_ROOT=1 \
	  LINUXIO_TEST_REAL_UID="$$(id -u)" LINUXIO_TEST_REAL_GID="$$(id -g)" \
	  LANG=C.UTF-8 TERM=xterm \
	  ./linuxio-auth-pam-test | $(CHECKMARK_SED); \
	$(PRINTC) "$(COLOR_GREEN)✅ PAM integration tests passed!$(COLOR_RESET)"

test-updater: ensure-go
	@echo "🔎 Running updater systemd dry-run integration test..."
	@cd "$(backend_dir)" && \
	  sudo env \
	    PATH="$$(dirname "$(GO_BIN)"):$${PATH}" \
	    GOTOOLCHAIN="$(GO_TOOLCHAIN)" \
	    LINUXIO_RUN_SYSTEMD_INTEGRATION=1 \
	    "$(GO_BIN)" test ./bridge/handlers/appupdate -run TestInstallScriptDryRunWithSystemdSandbox -count=1 -v

test-installation-scripts:
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running install-dependencies fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/test-install-dependencies.sh"
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running installer port and recovery-asset fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/tests/installer-port-fixtures.sh"

test-indexer-systemd-integration:
	@"$(packaging_scripts_dir)/tests/systemd-indexer-smoke.sh"

test-docker-update-integration: ensure-go
	@echo "🐳 Running native Docker update integration test..."
	@cd "$(backend_dir)" && \
	  LINUXIO_RUN_DOCKER_INTEGRATION=1 \
	  $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" \
	  "$(GO_BIN)" test ./bridge/handlers/docker -run '^TestDockerUpdateCompose' -count=1 -v

# Core lint implementations (used by both individual targets and parallel test)
lint-only:
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🔎 Running Oxlint + Oxfmt (auto-fix)...$(COLOR_RESET)"
	@bash -c ' \
	  cd "$(frontend_dir)"; \
	  lint_output="$$(mktemp)"; format_output="$$(mktemp)"; \
	  trap "rm -f \"$$lint_output\" \"$$format_output\"" EXIT; \
	  ./node_modules/.bin/oxlint --type-aware --fix -c config/.oxlintrc.json src config scripts/compiler-coverage.mjs scripts/run-browser-fixture.mjs > "$$lint_output" 2>&1; \
	  status=$$?; \
	  warning_count="$$(awk '\''/^Found [0-9]+ warning/ { count = $$2; found = 1 } /: warning / || /^[[:space:]]*⚠ / { fallback++ } END { print found ? count : fallback + 0 }'\'' "$$lint_output")"; \
	  [ "$$status" -eq 0 ] || { \
	    printf "\\033[1;31m   ✗\\033[0m Oxlint failed\\n"; \
	    sed "s/^/      /" "$$lint_output"; \
	    exit "$$status"; \
	  }; \
	  if ! grep -Eq '"'"'Found[[:space:]]+[0-9]+[[:space:]]+warnings?[[:space:]]+and[[:space:]]+[0-9]+[[:space:]]+errors?'"'"' "$$lint_output"; then \
	    printf '"'"'Found %s warnings and 0 errors.\n'"'"' "$$warning_count" >> "$$lint_output"; \
	  fi; \
	  printf "\\033[1;32m   ✓\\033[0m Oxlint\\n"; \
	  sed "s/^/      /" "$$lint_output"; \
	  if [ "$$warning_count" -gt 0 ]; then \
	    printf "\\033[1;33m   ⚠\\033[0m Oxlint completed with %s non-blocking warning(s).\\n" "$$warning_count"; \
	    if [ -n "$${FRONTEND_LINT_WARNINGS_FILE:-}" ]; then printf "%s\\n" "$$warning_count" > "$$FRONTEND_LINT_WARNINGS_FILE"; fi; \
	  fi; \
	  ./node_modules/.bin/oxfmt -c config/.oxfmtrc.json --no-error-on-unmatched-pattern "src/**/*.js" "src/**/*.jsx" "src/**/*.ts" "src/**/*.tsx" "src/test/browser/**/*.html" "!src/routeTree.gen.ts" "config/**/*.ts" "scripts/compiler-coverage.mjs" "scripts/run-browser-fixture.mjs" > "$$format_output" 2>&1; \
	  status=$$?; \
	  [ "$$status" -eq 0 ] || { \
	    printf "\\033[1;31m   ✗\\033[0m Oxfmt failed\\n"; \
	    sed "s/^/      /" "$$format_output"; \
	    exit "$$status"; \
	  }; \
	  printf "\\033[1;32m   ✓\\033[0m Oxfmt\\n"; \
	  sed "s/^/      /" "$$format_output"; \
	  printf "\\033[1;32m%s\\033[0m\\n" "✅ Frontend linting and formatting passed!" \
	'

tsc-only:
	@$(PRINTC) "$(COLOR_CYAN)🔎 Running TypeScript type checks...$(COLOR_RESET)"
	@cd "$(frontend_dir)" && ./node_modules/.bin/tsc && $(PRINTC) "$(COLOR_GREEN)✅ TypeScript checks passed!$(COLOR_RESET)"

golint-only:
	@set -euo pipefail
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🔎 Running backend lint and static analysis...$(COLOR_RESET)"
	@echo "   Module: $(backend_dir)"
	@( cd "$(backend_dir)" && $(GO_CMD_ENV) "$(golangci_lint)" fmt )
	@$(PRINTC) "   $(COLOR_GREEN)✓$(COLOR_RESET) Go formatters"
	@( cd "$(backend_dir)" && $(GO_CMD_ENV) "$(GO_BIN)" mod tidy && $(GO_CMD_ENV) "$(GO_BIN)" mod download )
	@$(PRINTC) "   $(COLOR_GREEN)✓$(COLOR_RESET) go.mod is tidy"
	@( cd "$(backend_dir)" && $(GO_CMD_ENV) "$(modernize)" -fix ./... )
	@$(PRINTC) "   $(COLOR_GREEN)✓$(COLOR_RESET) modernize"
	@cd "$(backend_dir)" && \
		if ! vuln_out="$$( $(GO_CMD_ENV) "$(govulncheck)" ./... 2>&1 )"; then \
			$(PRINTC) "$(COLOR_RED)❌ govulncheck found vulnerabilities reachable from this code:$(COLOR_RESET)"; \
			printf '%s\n' "$$vuln_out"; \
			exit 1; \
		fi
	@$(PRINTC) "   $(COLOR_GREEN)✓$(COLOR_RESET) govulncheck"
	@( cd "$(backend_dir)" && $(GO_CMD_ENV) "$(golangci_lint)" run ./... --timeout 3m $(GOLANGCI_LINT_OPTS) ) 2>&1 \
		| sed 's/^/      /'
	@$(PRINTC) "   $(COLOR_GREEN)✓$(COLOR_RESET) golangci-lint"
	@$(PRINTC) "$(COLOR_GREEN)✅ Go linting passed!$(COLOR_RESET)"

# Backend test entry point; this is what `make test` and CI run. Race results
# are cached like normal test results, so incremental runs stay fast. Pass
# GO_TEST_FLAGS="-count=5" for a fresh sweep with more scheduling
# interleavings (races only surface on interleavings that actually happen).
test-backend: $(GO_BUILD_PREREQ) test-auth test-auth-protocol test-auth-pam
	@$(MAKE) --no-print-directory test-go

# Go unit tests only. Narrow with GO_TEST_PKGS and GO_TEST_FLAGS, e.g.
#   make test-go GO_TEST_PKGS=./bridge/handlers/filebrowser/... GO_TEST_FLAGS='-run TestExtract'
GO_TEST_PKGS ?= ./...
test-go: $(GO_BUILD_PREREQ)
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running Go unit tests with race detector (backend)...$(COLOR_RESET)"
	@cd "$(backend_dir)" && \
		$(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=1 "$(GO_BIN)" test $(GO_TEST_PKGS) -race $(GO_TEST_FLAGS) -timeout 10m 2>&1 \
		| grep --line-buffered -v '\[no test files\]' \
		| $(GOTEST_STATUS_SED); \
		exit "$${PIPESTATUS[0]}"

deadcode: ensure-deadcode
	@$(MAKE) --no-print-directory deadcode-only

# Scan with tests for wholly unreachable code, then without tests to surface
# production APIs kept alive only by tests. testdbus is deliberately test-only
# cross-package infrastructure and is the sole production-scan exclusion.
deadcode-only:
	@$(PRINTC) "$(COLOR_CYAN)🔎 Scanning backend for dead code (informational)...$(COLOR_RESET)"
	@cd "$(backend_dir)" && \
		scan_dir="$$(mktemp -d)"; \
		trap 'rm -rf "$$scan_dir"' EXIT; \
		cache_root="$(cache_dir)/deadcode"; \
		cache_hit=0; \
		if [ "$(DEADCODE_CACHE)" = "1" ]; then \
			mkdir -p "$$cache_root"; \
			exec 9> "$$cache_root/lock"; \
			flock 9; \
			cache_key="$$( \
				{ \
					printf '%s\n' 'linuxio-deadcode-cache-v1' '-test ./...' './...' \
						'exclude bridge/internal/dbusclient/testdbus/'; \
					$(sha256_cmd) < "$(repo_root)/Makefile"; \
					$(sha256_cmd) < "$(deadcode)"; \
					$(sha256_cmd) < "$(GO_BIN)"; \
					$(GO_CMD_ENV) "$(GO_BIN)" version; \
					$(GO_CMD_ENV) "$(GO_BIN)" env -json \
						GOOS GOARCH GOAMD64 GOARM GOARM64 GO386 GOMIPS GOMIPS64 GOWASM \
						CGO_ENABLED CGO_CFLAGS CGO_CPPFLAGS CGO_CXXFLAGS CGO_FFLAGS CGO_LDFLAGS \
						CC CXX GCCGO PKG_CONFIG GOEXPERIMENT GOFIPS140 GOFLAGS GOTOOLCHAIN \
						GO_EXTLINK_ENABLED GOWORK GOPACKAGESDRIVER GOPROXY GONOPROXY GOPRIVATE GONOSUMDB; \
					find . -type f -print0 | sort -z | xargs -0 $(sha256_cmd); \
				} | $(sha256_cmd) | awk '{ print $$1 }' \
			)"; \
			cache_entry="$$cache_root/$$cache_key"; \
			if [ -f "$$cache_entry/test.out" ] && [ -f "$$cache_entry/production.out" ]; then \
				cp "$$cache_entry/test.out" "$$scan_dir/test.out"; \
				cp "$$cache_entry/production.out" "$$scan_dir/production.out"; \
				printf '0\n' > "$$scan_dir/test.status"; \
				printf '0\n' > "$$scan_dir/production.status"; \
				cache_hit=1; \
				echo "   Reusing unchanged dead-code results..."; \
			fi; \
		fi; \
		run_scan() { \
			name="$$1"; shift; \
			$(GO_CMD_ENV) "$(deadcode)" "$$@" > "$$scan_dir/$$name.out" 2>&1; \
			printf '%s\n' "$$?" > "$$scan_dir/$$name.status"; \
		}; \
		if [ $$cache_hit -eq 0 ]; then \
			cores="$$(nproc)"; \
			available_kb="$$(awk '/^MemAvailable:/ { print $$2; exit }' /proc/meminfo 2>/dev/null || true)"; \
			parallel=0; \
			if [ "$(DEADCODE_PARALLEL)" = "1" ] || \
			   { [ "$(DEADCODE_PARALLEL)" = "auto" ] && [ "$$cores" -ge 8 ] && [ "$${available_kb:-0}" -ge 6291456 ]; }; then \
				parallel=1; \
			fi; \
			if [ $$parallel -eq 1 ]; then \
				echo "   Running test and production scans concurrently..."; \
				run_scan test -test ./... & test_pid=$$!; \
				run_scan production ./... & production_pid=$$!; \
				wait "$$test_pid"; wait "$$production_pid"; \
			else \
				run_scan test -test ./...; \
				run_scan production ./...; \
			fi; \
			if [ "$(DEADCODE_CACHE)" = "1" ] && \
			   [ "$$(cat "$$scan_dir/test.status")" -eq 0 ] && \
			   [ "$$(cat "$$scan_dir/production.status")" -eq 0 ]; then \
				publish_dir="$$scan_dir/cache-entry"; \
				mkdir "$$publish_dir"; \
				cp "$$scan_dir/test.out" "$$publish_dir/test.out"; \
				cp "$$scan_dir/production.out" "$$publish_dir/production.out"; \
				mv -T "$$publish_dir" "$$cache_entry"; \
			fi; \
		fi; \
		test_out="$$(cat "$$scan_dir/test.out")"; \
		test_status="$$(cat "$$scan_dir/test.status")"; \
		production_out="$$(cat "$$scan_dir/production.out")"; \
		production_status="$$(cat "$$scan_dir/production.status")"; \
		if [ $$production_status -eq 0 ]; then \
			production_out="$$(printf '%s\n' "$$production_out" | grep -v '^bridge/internal/dbusclient/testdbus/' || true)"; \
		fi; \
		if [ $$test_status -ne 0 ] || [ $$production_status -ne 0 ]; then \
			$(PRINTC) "$(COLOR_YELLOW)⚠️  deadcode scan could not complete (informational, not failing):$(COLOR_RESET)"; \
			if [ $$test_status -ne 0 ]; then printf '%s\n' "$$test_out"; fi; \
			if [ $$production_status -ne 0 ]; then printf '%s\n' "$$production_out"; fi; \
		elif [ -n "$$test_out" ]; then \
			$(PRINTC) "$(COLOR_YELLOW)⚠️  deadcode found unreachable functions (informational, not failing):$(COLOR_RESET)"; \
			printf '%s\n' "$$test_out"; \
		elif [ -n "$$production_out" ]; then \
			$(PRINTC) "$(COLOR_YELLOW)⚠️  deadcode found functions reachable only from tests (informational, not failing):$(COLOR_RESET)"; \
			printf '%s\n' "$$production_out"; \
		else \
			$(PRINTC) "$(COLOR_GREEN)✅ No dead code found!$(COLOR_RESET)"; \
		fi

# libpam's conversation callback ABI requires void *appdata_ptr. Cppcheck's
# callback-specific const suggestion would require an incompatible function type.
.PHONY: analyze-auth
analyze-auth:
	@echo ""
	@echo "🔬 Running C static analysis (linuxio-auth)..."
	@set -euo pipefail; \
	FILE="$(backend_auth_dir)/linuxio-auth.c"; \
	CPPCHK_DEFS='-D__has_include(x)=0 -DLINUXIO_VERSION="dev"'; \
	CPPCHK_SUPPRESS="--suppress=ctunullpointer:$$FILE --suppress=variableScope:$$FILE --suppress=constParameter:$$FILE --suppress=constParameterCallback:$$FILE --suppress=normalCheckLevelMaxBranches"; \
	SB_WARNFLAGS="$(filter-out -Wduplicated-cond -Wlogical-op,$(WARNFLAGS)) -Wno-format-nonliteral"; \
	CLANG_TIDY_OPTS='--quiet -checks=-clang-analyzer-security.insecureAPI.DeprecatedOrUnsafeBufferHandling,-clang-diagnostic-format-nonliteral --extra-arg=-Wno-unknown-warning-option'; \
	CC_DB_DIR="$(cache_dir)/clang"; \
	CC_DB="$$CC_DB_DIR/compile_commands.json"; \
	if ! command -v cppcheck >/dev/null 2>&1; then \
	  echo "❌ cppcheck not found (install: sudo apt-get install cppcheck)"; \
	  exit 1; \
	fi; \
	echo "   cppcheck"; \
	cppcheck --enable=warning,style,performance,portability --inconclusive --std=c11 --force \
	  $$CPPCHK_SUPPRESS $$CPPCHK_DEFS "$$FILE"; \
	if ! command -v "$(CC)" >/dev/null 2>&1; then \
	  echo "❌ compiler not found: $(CC)"; \
	  exit 1; \
	fi; \
	echo "   $(CC) -fanalyzer"; \
	"$(CC)" -fanalyzer -Wall -Wextra -Wshadow -Wformat=2 -Wconversion -Wnull-dereference -Wvla -O2 -c "$$FILE"; \
	rm -f linuxio-auth.o; \
	CLANG_BIN=""; \
	for c in clang clang-{30..14}; do \
	  if command -v "$$c" >/dev/null 2>&1; then CLANG_BIN="$$c"; break; fi; \
	done; \
	if ! command -v scan-build >/dev/null 2>&1; then \
	  echo "  scan-build not found - skipping clang static analyzer"; \
	elif [ -z "$$CLANG_BIN" ]; then \
	  echo "  clang not found - skipping clang static analyzer (install: sudo apt-get install clang)"; \
	else \
	  echo "   scan-build (clang static analyzer, $$CLANG_BIN)"; \
	  scan-build --use-cc="$$CLANG_BIN" $(MAKE) --no-print-directory build-auth CC="$$CLANG_BIN" WARNFLAGS="$$SB_WARNFLAGS"; \
	fi; \
	if ! command -v bear >/dev/null 2>&1; then \
	  echo "❌ bear not found (install: sudo apt-get install bear)"; \
	  exit 1; \
	fi; \
	if ! command -v clang-tidy >/dev/null 2>&1; then \
	  echo "❌ clang-tidy not found (install: sudo apt-get install clang-tidy)"; \
	  exit 1; \
	fi; \
	echo "   clang-tidy (compile_commands.json via bear)"; \
	mkdir -p "$$CC_DB_DIR"; \
	rm -f "$$CC_DB"; \
	bear --output "$$CC_DB" -- $(MAKE) --no-print-directory build-auth; \
	clang-tidy $$CLANG_TIDY_OPTS -p "$$CC_DB_DIR" "$$FILE"; \
	echo "✅ C analysis complete."

# Run any public validation target with bounded output. The underlying target
# is unchanged, and its complete output remains available for diagnosis.
# test-updater stays in the foreground so sudo retains a controlling terminal.
$(quiet_aliases):
	@set -euo pipefail; \
	target="$(patsubst %-quiet,%,$@)"; \
	case "$(quiet_failure_lines)" in \
		''|*[!0-9]*) echo "quiet_failure_lines must be a non-negative integer" >&2; exit 2 ;; \
	esac; \
	mkdir -p "$(quiet_log_dir)"; \
	run_id="$$(date -u +%Y%m%dT%H%M%S)-$$PPID-$$RANDOM"; \
	log="$(quiet_log_dir)/$$target-$$run_id.log"; \
	start_seconds="$$(awk '{ print $$1 }' /proc/uptime)"; \
	target_pid=""; \
	stop_target() { signal="$$1"; rc="$$2"; trap - EXIT HUP INT TERM; \
		if [ -n "$${target_pid:-}" ]; then \
			kill -s "$$signal" -- "-$$target_pid" 2>/dev/null || true; \
			wait "$$target_pid" 2>/dev/null || true; \
			kill -s KILL -- "-$$target_pid" 2>/dev/null || true; \
		fi; \
		exit "$$rc"; \
	}; \
	trap 'stop_target TERM 129' HUP; \
	trap 'stop_target TERM 130' INT; \
	trap 'stop_target TERM 143' TERM; \
	trap 'stop_target TERM $$?' EXIT; \
	if [ "$$target" = "test-updater" ]; then \
		if $(MAKE) --no-print-directory "$$target" >"$$log" 2>&1; then rc=0; else rc=$$?; fi; \
	else \
		"$(setsid_cmd)" $(MAKE) --no-print-directory "$$target" >"$$log" 2>&1 & target_pid=$$!; \
		if wait "$$target_pid"; then rc=0; else rc=$$?; fi; \
	fi; \
	trap - EXIT HUP INT TERM; \
	if [ "$$rc" -eq 0 ]; then \
		elapsed="$$(awk -v start="$$start_seconds" -v end="$$(awk '{ print $$1 }' /proc/uptime)" 'BEGIN { printf "%.1f", end - start }')"; \
		printf '✓ %-32s %ss\n' "$$target" "$$elapsed"; \
		summaries="$$(grep -E 'Found[[:space:]]+[0-9]+[[:space:]]+warnings?[[:space:]]+and[[:space:]]+[0-9]+[[:space:]]+errors?' "$$log" | tail -n 5 || true)"; \
		if [ -n "$$summaries" ]; then \
			printf '%s\n' "$$summaries"; \
		fi; \
		warnings="$$(grep -E '⚠️|(^|[[:space:]])warning:' "$$log" | grep -Eiv 'Found[[:space:]]+[0-9]+[[:space:]]+warnings?[[:space:]]+and[[:space:]]+[0-9]+[[:space:]]+errors?' | tail -n 10 || true)"; \
		if [ -n "$$warnings" ]; then \
			printf '  warnings:\n%s\n' "$$warnings"; \
		fi; \
	else \
		elapsed="$$(awk -v start="$$start_seconds" -v end="$$(awk '{ print $$1 }' /proc/uptime)" 'BEGIN { printf "%.1f", end - start }')"; \
		printf '✗ %-32s %ss\n' "$$target" "$$elapsed" >&2; \
		if [ "$(quiet_failure_lines)" -gt 0 ]; then \
			printf '  last %s lines:\n' "$(quiet_failure_lines)" >&2; \
			tail -n "$(quiet_failure_lines)" "$$log" >&2; \
		fi; \
		printf '  full log: %s\n' "$$log" >&2; \
		exit "$$rc"; \
	fi

.PHONY: build-vite bundle-metrics compiler-coverage analyze build-leak-profile build-backend build-bridge check-c-build-deps build-auth build-cli build-docker-update build-indexer build-monitoring
build-vite:
	@echo ""
	@echo "🏗️  Building frontend..."
	@cd "$(frontend_dir)" && npm run build && echo "✅ Frontend built successfully!"

bundle-metrics:
	@echo ""
	@echo "📊 Reporting frontend bundle metrics..."
	@cd "$(frontend_dir)" && npm run bundle:metrics

compiler-coverage:
	@echo ""
	@echo "⚛️  Reporting React Compiler coverage..."
	@cd "$(frontend_dir)" && npm run compiler:coverage

analyze: ensure-node setup
	@echo ""
	@echo "🔬 Building frontend bundle analysis..."
	@cd "$(frontend_dir)" && npm run analyze && echo "✅ Frontend analysis built successfully!"

# Debug-only binaries for goroutine leak hunting. Serves
# net/http/pprof on localhost only (webserver :6060, bridge :6061). The leak
# report lives at /debug/pprof/goroutineleak. The endpoint has no auth (it is
# loopback-bound). Rebuild with `make build` afterwards.
build-leak-profile:
	@echo ""
	@echo "🕵️  Building DEBUG binaries with pprof + goroutine leak profile..."
	@$(MAKE) --no-print-directory build-bridge \
		GO_BUILD_TAGS="pprofdebug"
	@BRIDGE_HASH=$$($(sha256_cmd) "$(bridge_binary)" | awk '{ print $$1 }'); \
	echo "   Bridge hash: $$BRIDGE_HASH"; \
	$(MAKE) --no-print-directory build-backend BRIDGE_SHA256=$$BRIDGE_HASH SKIP_ENSURE_GO=1 \
		GO_BUILD_TAGS="pprofdebug"
	@echo ""
	@echo "   Webserver pprof: http://127.0.0.1:6060/debug/pprof/  (leaks: /debug/pprof/goroutineleak)"
	@echo "   Bridge pprof:    http://127.0.0.1:6061/debug/pprof/"
	@echo "   ⚠️  Debug binaries only — rebuild with 'make build' before packaging."

build-backend: $(GO_BUILD_PREREQ)
	@echo ""
	@echo "🏗️  Building backend..."
	@mkdir -p "$(bin_dir)"
	@echo "   Module: $(MODULE_PATH)"
	@echo "   Version: $(GIT_VERSION)"
	@if [ -n "$(BRIDGE_SHA256)" ]; then \
		echo "   Bridge SHA256: $(BRIDGE_SHA256)"; \
	else \
		echo "   Bridge SHA256: (not embedded - development mode)"; \
	fi
	@cd "$(backend_dir)" && \
	$(GO_CMD_ENV) GOAMD64=$(GOAMD64) GOFLAGS="-buildvcs=false" $(GO_BUILD_EXTRA_ENV) \
	"$(GO_BIN)" build -trimpath \
	-ldflags "\
		-s -w \
		-X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' \
		-X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' \
		-X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)' \
		-X '$(MODULE_PATH)/common/version.BridgeSHA256=$(BRIDGE_SHA256)'" \
	$(GO_BUILD_TAGS_FLAG) \
	-o "$(backend_binary)" ./webserver/ && \
	echo "✅ Backend built successfully!" && \
	echo "   Path: $(backend_binary)" && \
	echo "   Version: $(GIT_VERSION)" && \
	echo "   Size: $$(du -h "$(backend_binary)" | cut -f1)" && \
	echo "   SHA256: $$($(sha256_cmd) "$(backend_binary)" | awk '{ print $$1 }')"

check-c-build-deps:
	@missing=""; pkgs=""; \
	if ! command -v $(CC) >/dev/null 2>&1; then \
	  missing="$$missing\n  - C compiler ($(CC)) not found"; \
	  pkgs="$$pkgs build-essential"; \
	fi; \
	if ! (command -v pkg-config >/dev/null 2>&1 && pkg-config --exists libsystemd 2>/dev/null) && \
	   ! [ -f /usr/include/systemd/sd-journal.h ]; then \
	  missing="$$missing\n  - libsystemd-dev"; \
	  pkgs="$$pkgs libsystemd-dev"; \
	fi; \
	if ! [ -f /usr/include/security/pam_appl.h ]; then \
	  missing="$$missing\n  - libpam-dev"; \
	  pkgs="$$pkgs libpam-dev"; \
	fi; \
	if [ -n "$$missing" ]; then \
	  echo ""; \
	  echo "❌ Missing build dependencies for linuxio-auth:"; \
	  printf '%b\n' "$$missing"; \
	  echo ""; \
	  echo "   Install with: sudo apt-get install$$pkgs"; \
	  echo ""; \
	  exit 1; \
	fi

build-auth:
	@echo ""
	@echo "🏗️  Building Session helper (C)..."
	@set -euo pipefail; \
	mkdir -p "$(bin_dir)"; \
	LIBS="-lpam"; \
	if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists libsystemd 2>/dev/null; then \
	  LIBS="$$LIBS $$(pkg-config --libs libsystemd)"; \
	  echo "   Linking with libsystemd for journald support (via pkg-config)"; \
	elif [ -f /usr/include/systemd/sd-journal.h ]; then \
	  LIBS="$$LIBS -lsystemd"; \
	  echo "   Linking with libsystemd for journald support"; \
	else \
	  echo "❌ libsystemd-dev/systemd-devel is required to build linuxio-auth"; \
	  echo "   Install with: sudo apt-get install libsystemd-dev"; \
	  exit 1; \
	fi; \
	$(CC) $(CFLAGS) -DLINUXIO_VERSION=\"$(GIT_VERSION)\" -o "$(auth_binary)" "$(backend_auth_dir)/linuxio-auth.c" $(LDFLAGS) $$LIBS; \
	if [ "$(STRIP)" = "1" ]; then strip --strip-unneeded "$(auth_binary)"; fi; \
	echo "✅ Session helper built successfully!"; \
	echo "   Path: $(auth_binary)"; \
	echo "   Size: $$(du -h "$(auth_binary)" | cut -f1)"; \
	echo "   SHA256: $$($(sha256_cmd) "$(auth_binary)" | awk '{ print $$1 }')"; \
	if command -v checksec >/dev/null 2>&1; then \
	  echo " checksec:"; checksec --file="$(auth_binary)" || true; \
	fi

go_binary_targets := build-bridge build-cli build-docker-update build-indexer build-monitoring

build-bridge: go_binary_label := bridge
build-bridge: go_binary_package := ./bridge
build-bridge: go_binary_output := $(bridge_binary)
build-bridge: go_binary_ldflags := -s -w -X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' -X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' -X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)'

build-cli: go_binary_label := CLI
build-cli: go_binary_package := ./cli
build-cli: go_binary_output := $(cli_binary)
build-cli: go_binary_ldflags := -s -w -X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' -X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' -X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)'

build-docker-update: go_binary_label := Docker update worker
build-docker-update: go_binary_package := ./docker-update
build-docker-update: go_binary_output := $(docker_update_binary)
build-docker-update: go_binary_ldflags := -s -w

build-indexer: go_binary_label := filesystem indexer
build-indexer: go_binary_package := ./indexer
build-indexer: go_binary_output := $(indexer_binary)
build-indexer: go_binary_ldflags := -s -w -X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' -X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' -X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)'
build-indexer: go_binary_extra_env := CGO_ENABLED=1
build-indexer: go_binary_tags := sqlite_fts5

build-monitoring: go_binary_label := monitoring daemon
build-monitoring: go_binary_package := ./monitoring
build-monitoring: go_binary_output := $(monitoring_binary)
build-monitoring: go_binary_ldflags := -s -w -X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' -X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' -X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)'
build-monitoring: go_binary_extra_env := CGO_ENABLED=1
# NVML is compiled only for amd64; other architectures use the stub. Deferred
# expansion keeps `go env` out of every unrelated make invocation.
build-monitoring: go_binary_tags = $(if $(filter amd64,$(if $(GOARCH),$(GOARCH),$(GOARCH_HOST))),glibc,)

$(go_binary_targets): $(GO_BUILD_PREREQ)

$(go_binary_targets):
	@echo ""
	@echo "🏗️  Building $(go_binary_label)..."
	@mkdir -p "$(bin_dir)"
	@cd "$(backend_dir)" && \
	$(GO_CMD_ENV) GOAMD64=$(GOAMD64) GOFLAGS="-buildvcs=false" $(GO_BUILD_EXTRA_ENV) $(go_binary_extra_env) \
	"$(GO_BIN)" build -trimpath \
	-ldflags "$(go_binary_ldflags)" \
	$(if $(strip $(GO_BUILD_TAGS) $(go_binary_tags)),-tags "$(strip $(GO_BUILD_TAGS) $(go_binary_tags))") \
	-o "$(go_binary_output)" "$(go_binary_package)" && \
	echo "✅ $(go_binary_label) built successfully!" && \
	echo "   Path: $(go_binary_output)" && \
	echo "   Size: $$(du -h "$(go_binary_output)" | cut -f1)"


.PHONY: dev-prep dev
frontend_placeholder_files := \
	$(backend_frontend_dir)/.vite/manifest.json \
	$(backend_frontend_dir)/manifest.json \
	$(backend_frontend_dir)/favicon-1.png \
	$(backend_frontend_dir)/assets/index-mock.js

dev-prep: $(frontend_placeholder_files)

$(frontend_placeholder_files):
	@mkdir -p "$(@D)"
	@touch "$@"

dev: setup dev-prep
	@echo ""
	@echo "🚀 Starting development servers..."
	@echo "   Backend must be running via: sudo systemctl start linuxio"
	@echo "   Vite proxies /ws, /auth, /api to port 8090"
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
	  echo "  Vite already running (pid $$(cat "$(VITE_DEV_PID)"))"; \
	else \
	  rm -f "$(VITE_DEV_PID)"; \
	  nohup bash -c 'cd "$(frontend_dir)" && exec ./node_modules/.bin/vite --config config/vite.config.ts --configLoader native --port $(VITE_DEV_PORT)' > "$(VITE_DEV_LOG)" 2>&1 & \
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
	@echo "📋 Tailing LinuxIO logs (last $(DEV_LOG_LINES) lines)..."
	@linuxio logs $(DEV_LOG_LINES)

# Internal target: build backend + auth + command binaries (requires bridge already built)
.PHONY: _build-binaries build build-nocheck fastbuild generate clean
_build-binaries: ensure-go check-c-build-deps
	@echo ""
	@echo "🔑 Capturing bridge hash for backend build..."
	@BRIDGE_HASH=$$($(sha256_cmd) "$(bridge_binary)" | awk '{ print $$1 }'); \
	echo "   Hash: $$BRIDGE_HASH"; \
	$(MAKE) --no-print-directory build-backend BRIDGE_SHA256=$$BRIDGE_HASH SKIP_ENSURE_GO=1
	@$(MAKE) --no-print-directory build-auth
	@$(MAKE) --no-print-directory build-cli SKIP_ENSURE_GO=1
	@$(MAKE) --no-print-directory build-docker-update SKIP_ENSURE_GO=1
	@$(MAKE) --no-print-directory build-indexer SKIP_ENSURE_GO=1
	@$(MAKE) --no-print-directory build-monitoring SKIP_ENSURE_GO=1

build: generate test build-vite build-bridge _build-binaries

build-nocheck: generate build-vite build-bridge _build-binaries

fastbuild: generate build-bridge _build-binaries

generate: ensure-go ensure-node setup
	@cd "$(backend_dir)" && $(GO_CMD_ENV) "$(GO_BIN)" run ./common/tools/linuxio-api-gen

clean:
	@rm -f "$(cli_binary)" "$(backend_binary)" "$(bridge_binary)" "$(auth_binary)" "$(docker_update_binary)" "$(indexer_binary)" "$(monitoring_binary)" || true
	@rm -f "$(VITE_DEV_PID)" "$(VITE_DEV_LOG)" "$(frontend_dir)/tsconfig.tsbuildinfo" || true
	@rm -rf "$(cache_dir)" "$(frontend_node_modules_dir)" || true
	@find "$(backend_frontend_dir)" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
	@echo "🧹 Cleaned workspace."

# ========== Installation Targets ==========

.PHONY: uninstall localinstall reinstall
uninstall:
	@echo ""
	@echo "🗑️  Uninstalling LinuxIO..."
	@sudo "$(packaging_scripts_dir)/uninstall.sh"

localinstall:
	@echo ""
	@echo "📦 Installing LinuxIO from local build..."
	@sudo "$(packaging_scripts_dir)/localinstall.sh"

reinstall: uninstall fastbuild localinstall
	@echo ""
	@echo "LinuxIO reinstalled successfully!"
	@echo "  WARNING: Quick & dirty build - no tests executed!"

.PHONY: default help help-overrides
help:
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_BLUE)  Available commands:$(COLOR_RESET)"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Toolchain setup$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-node      $(COLOR_RESET) Install/activate Node $(NODE_VERSION) via nvm"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-go        $(COLOR_RESET) Install Go $(GO_VERSION) (user-local, no sudo)"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-golint    $(COLOR_RESET) Install golangci-lint (built with local Go $(GO_VERSION))"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-deadcode  $(COLOR_RESET) Install deadcode (built with local Go $(GO_VERSION))"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-modernize $(COLOR_RESET) Install modernize (built with local Go $(GO_VERSION))"
	@$(PRINTC) "$(COLOR_GREEN)    make ensure-govulncheck $(COLOR_RESET) Install govulncheck (built with local Go $(GO_VERSION))"
	@$(PRINTC) "$(COLOR_GREEN)    make setup            $(COLOR_RESET) Install frontend dependencies (npm i)"
	@$(PRINTC) "$(COLOR_GREEN)    make update-deps      $(COLOR_RESET) Update frontend and Go dependencies"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Quality checks$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_GREEN)    make lint             $(COLOR_RESET) Run ESLint + Oxfmt (frontend)"
	@$(PRINTC) "$(COLOR_GREEN)    make lint-only        $(COLOR_RESET) Run frontend lint without setup prerequisites"
	@$(PRINTC) "$(COLOR_GREEN)    make lint-ci          $(COLOR_RESET) Install locked frontend deps and run read-only lint"
	@$(PRINTC) "$(COLOR_GREEN)    make tsc              $(COLOR_RESET) Type-check with TypeScript (frontend)"
	@$(PRINTC) "$(COLOR_GREEN)    make tsc-only         $(COLOR_RESET) Run TypeScript checks without setup prerequisites"
	@$(PRINTC) "$(COLOR_GREEN)    make tsc-ci           $(COLOR_RESET) Install locked frontend deps and run TypeScript checks"
	@$(PRINTC) "$(COLOR_GREEN)    make golint           $(COLOR_RESET) Run Go formatters + modernize + govulncheck + golangci-lint (backend)"
	@$(PRINTC) "$(COLOR_GREEN)    make golint-only      $(COLOR_RESET) Run backend formatting, modernization, vulnerability, and lint checks"
	@$(PRINTC) "$(COLOR_GREEN)    make deadcode         $(COLOR_RESET) Report unreachable Go functions (informational)"
	@$(PRINTC) "$(COLOR_GREEN)    make deadcode-only    $(COLOR_RESET) Scan backend for dead code without tool setup"
	@$(PRINTC) "$(COLOR_GREEN)    make test             $(COLOR_RESET) Run lint + tsc + frontend tests + golint + backend tests + deadcode scan"
	@$(PRINTC) "$(COLOR_GREEN)    make check-actions    $(COLOR_RESET) Validate GitHub Actions workflows"
	@$(PRINTC) "$(COLOR_GREEN)    make check-systemd    $(COLOR_RESET) Validate LinuxIO systemd units"
	@$(PRINTC) "$(COLOR_GREEN)    make check-frontend   $(COLOR_RESET) Run frontend lint + typecheck + unit tests"
	@$(PRINTC) "$(COLOR_GREEN)    make check-backend    $(COLOR_RESET) Run backend lint + unit tests + deadcode scan"
	@$(PRINTC) "$(COLOR_GREEN)    make test-frontend    $(COLOR_RESET) Run frontend unit tests only"
	@$(PRINTC) "$(COLOR_GREEN)    make test-frontend-only$(COLOR_RESET) Run frontend unit tests without setup prerequisites"
	@$(PRINTC) "$(COLOR_GREEN)    make test-frontend-ci $(COLOR_RESET) Install locked frontend deps and run frontend tests"
	@$(PRINTC) "$(COLOR_GREEN)    make setup-frontend-browser$(COLOR_RESET) Install Playwright Chromium"
	@$(PRINTC) "$(COLOR_GREEN)    make test-frontend-browser$(COLOR_RESET) Build frontend + run router browser tests"
	@$(PRINTC) "$(COLOR_GREEN)    make test-backend$(COLOR_RESET) Run Go + C backend tests (used by 'make test' + CI)"
	@$(PRINTC) "$(COLOR_GREEN)    make test-go$(COLOR_RESET)      Run Go unit tests only (GO_TEST_PKGS=./pkg/... GO_TEST_FLAGS='-run X' to narrow)"
	@$(PRINTC) "$(COLOR_GREEN)    make test-auth        $(COLOR_RESET) Run C authentication helper tests"
	@$(PRINTC) "$(COLOR_GREEN)    make test-auth-protocol$(COLOR_RESET) Run cross-language (C<->Go) auth protocol frame tests"
	@$(PRINTC) "$(COLOR_GREEN)    make test-auth-pam    $(COLOR_RESET) Run hermetic PAM integration tests (pam_wrapper)"
	@$(PRINTC) "$(COLOR_GREEN)    make test-installation-scripts$(COLOR_RESET) Run host-independent installer fixture tests"
	@$(PRINTC) "$(COLOR_GREEN)    make test-indexer-systemd-integration$(COLOR_RESET) Run the opt-in disposable-host indexer smoke test"
	@$(PRINTC) "$(COLOR_GREEN)    make test-updater     $(COLOR_RESET) Run the root-only updater systemd dry-run integration test"
	@$(PRINTC) "$(COLOR_GREEN)    make test-docker-update-integration$(COLOR_RESET) Run the opt-in real Docker/Compose update test"
	@$(PRINTC) "$(COLOR_GREEN)    make <target>-quiet   $(COLOR_RESET) Run a validation target with compact output and a saved full log"
	@$(PRINTC) "$(COLOR_GREEN)    make bundle-metrics   $(COLOR_RESET) Report frontend bundle sizes after a Vite build (informational)"
	@$(PRINTC) "$(COLOR_GREEN)    make compiler-coverage$(COLOR_RESET) Report React Compiler memoization coverage (informational)"
	@$(PRINTC) "$(COLOR_GREEN)    make analyze          $(COLOR_RESET) Build frontend with bundle analysis enabled"
	@$(PRINTC) "$(COLOR_GREEN)    make analyze-auth     $(COLOR_RESET) Run C static analysis on linuxio-auth"
	@$(PRINTC) "$(COLOR_GREEN)    make check-c-build-deps$(COLOR_RESET) Check C authentication build dependencies"
	@$(PRINTC) "$(COLOR_GREEN)    make test-release-automation$(COLOR_RESET) Smoke-test release automation fixture"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Development$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make dev-prep         $(COLOR_RESET) Create placeholder frontend assets for dev server"
	@$(PRINTC) "$(COLOR_YELLOW)    make dev              $(COLOR_RESET) Start frontend dev server (detached) + tail LinuxIO logs"
	@$(PRINTC) "$(COLOR_YELLOW)    make generate         $(COLOR_RESET) Regenerate frontend API contracts"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Build$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build            $(COLOR_RESET) Full build (test + frontend + all binaries)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-nocheck    $(COLOR_RESET) Full build without running tests"
	@$(PRINTC) "$(COLOR_YELLOW)    make fastbuild        $(COLOR_RESET) Quick binary build (skip tests and frontend)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-vite       $(COLOR_RESET) Build frontend static assets (Vite)"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-backend    $(COLOR_RESET) Build Go backend binary"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-bridge     $(COLOR_RESET) Build Go bridge binary"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-indexer    $(COLOR_RESET) Build the filesystem indexer"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-monitoring $(COLOR_RESET) Build the monitoring daemon"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-leak-profile$(COLOR_RESET) Build DEBUG webserver+bridge with localhost pprof + goroutine leak profile"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-auth       $(COLOR_RESET) Build the PAM authentication helper"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-cli        $(COLOR_RESET) Build the CLI tool"
	@$(PRINTC) "$(COLOR_YELLOW)    make build-docker-update$(COLOR_RESET) Build the scheduled Docker update worker"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Install / Uninstall$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_RED)    make localinstall     $(COLOR_RESET) Install from local build"
	@$(PRINTC) "$(COLOR_RED)    make reinstall        $(COLOR_RESET) Uninstall + fastbuild + install"
	@$(PRINTC) "$(COLOR_RED)    make uninstall        $(COLOR_RESET) Remove LinuxIO installation"
	@$(PRINTC) ""
	@$(PRINTC) "$(COLOR_CYAN)  Run / Clean$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_RED)    make clean            $(COLOR_RESET) Remove binaries, node_modules, and generated assets"
	@$(PRINTC) "$(COLOR_RED)    make cloc             $(COLOR_RESET) Count handwritten source lines"
	@$(PRINTC) "$(COLOR_RED)    make help-overrides   $(COLOR_RESET) Show documented Make variables and overrides"
	@$(PRINTC) ""

help-overrides:
	@$(PRINTC) "$(COLOR_BLUE)  Override reference: docs/development.md$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_CYAN)  Example: make check-backend-quiet quiet_failure_lines=80$(COLOR_RESET)"

.PHONY: cloc
cloc:
	@echo "====>   Handwritten LOC    <===="
	@bash -c 'eval "$$LOC_COUNT_SCRIPT"; loc_count . "$(LOC_INCLUDE_EXT)" 1'

.PHONY: $(quiet_aliases)
