# LinuxIO build interface. This file owns the public variables, validation,
# help, the *-quiet aliases and .PHONY. Targets live in mk/*.mk by concern;
# the long shell programs behind them live in scripts/.

# Default target
.DEFAULT_GOAL := help
default: help

# Include private release automation
-include release.mk

repo_root := $(if $(REPO_ROOT),$(REPO_ROOT),$(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST))))))
REPO_ROOT ?= $(repo_root)
mk_dir := $(repo_root)/mk
scripts_dir := $(repo_root)/scripts
frontend_dir := $(if $(FRONTEND_DIR),$(FRONTEND_DIR),$(repo_root)/frontend)
FRONTEND_DIR ?= $(frontend_dir)
cache_dir := $(if $(CACHE_DIR),$(CACHE_DIR),$(repo_root)/.cache)
CACHE_DIR ?= $(cache_dir)
bin_dir := $(if $(BIN_DIR),$(BIN_DIR),$(repo_root)/build)
BIN_DIR ?= $(bin_dir)
frontend_node_modules_dir := $(frontend_dir)/node_modules
frontend_install_stamp := $(frontend_node_modules_dir)/.package-lock.json
packaging_scripts_dir := $(repo_root)/packaging/scripts

# Quiet aliases capture complete target output in .cache/test-logs while
# printing only a compact success/failure summary. Keep this list limited to
# user-facing validation and analysis targets; implementation targets inherit
# quiet behavior when invoked through one of these aliases.
quiet_targets := \
	test \
	check-actions \
	check-docs \
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
	test-btrfs-integration \
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
GO_TOOLCHAIN ?= auto
SKIP_ENSURE_GO ?= 0

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

# Colors (exported so scripts/ can use them; plain text when run standalone)
COLOR_RESET  := \033[0m
COLOR_BLUE   := \033[1;34m
COLOR_GREEN  := \033[1;32m
COLOR_YELLOW := \033[1;33m
COLOR_CYAN   := \033[1;36m
COLOR_RED    := \033[1;31m
export COLOR_RESET COLOR_BLUE COLOR_GREEN COLOR_YELLOW COLOR_CYAN COLOR_RED

PRINTC := printf '%b\n'
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
# Extra arguments for the Playwright browser suite, e.g. a single spec path:
# make test-frontend-browser PLAYWRIGHT_ARGS=src/test/browser/router.spec.ts
PLAYWRIGHT_ARGS ?=
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

# For code counting (built-in find + wc + awk; no external `cloc` dependency)
LOC_INCLUDE_EXT := js,jsx,ts,tsx,css,scss,html,go

# --- Git metadata ---
GIT_BRANCH        := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
GIT_TAG           := $(shell git describe --tags --exact-match 2>/dev/null || true)
GIT_COMMIT        := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_COMMIT_SHORT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BRANCH_VERSION    := $(patsubst dev/%,%,$(GIT_BRANCH))

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

# ---- C helper toggles (override on CLI: make build-auth LTO=0 STRIP=0 WERROR=1)
LTO      ?= 1          # enable link-time optimization
STRIP    ?= 1          # strip unneeded symbols after build
WERROR   ?= 0          # treat warnings as errors (good in CI)

# ---- validation -------------------------------------------------------------
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

.ONESHELL:
SHELL := /bin/bash

# Order matters: toolchain.mk defines the Go variables the others use in
# prerequisite lists (GO_BUILD_PREREQ).
include $(mk_dir)/toolchain.mk
include $(mk_dir)/go.mk
include $(mk_dir)/frontend.mk
include $(mk_dir)/auth.mk
include $(mk_dir)/quality.mk
include $(mk_dir)/install.mk

# ---- help -------------------------------------------------------------------
# Generated from the `##` annotations in this file and mk/*.mk by
# scripts/help.awk. The section order is declared here.
##@ Toolchain setup|green
##@ Quality checks|green
##@ Development|yellow
##@ Build|yellow
##@ Install / Uninstall|red
##@ Run / Clean|red
help:
	@awk -v blue='$(COLOR_BLUE)' -v cyan='$(COLOR_CYAN)' -v green='$(COLOR_GREEN)' \
	  -v yellow='$(COLOR_YELLOW)' -v red='$(COLOR_RED)' -v reset='$(COLOR_RESET)' \
	  -v go='$(GO_VERSION)' -v node='$(NODE_VERSION)' -v bin='$(bin_dir)' \
	  -f "$(scripts_dir)/help.awk" $(MAKEFILE_LIST)

##@ Run / Clean|red
help-overrides: ## Show documented Make variables and overrides
	@$(PRINTC) "$(COLOR_BLUE)  Override reference: docs/development.md$(COLOR_RESET)"
	@$(PRINTC) "$(COLOR_CYAN)  Example: make check-backend-quiet quiet_failure_lines=80$(COLOR_RESET)"

# Run any public validation target with bounded output. The underlying target
# is unchanged, and its complete output remains available for diagnosis.
##@ Quality checks|green
$(quiet_aliases): ## Run a validation target with compact output and a saved full log
	@MAKE="$(MAKE)" "$(scripts_dir)/quiet.sh" "$(patsubst %-quiet,%,$@)" "$(quiet_log_dir)" "$(quiet_failure_lines)" "$(setsid_cmd)"

# Every public target, in one place. File targets (the frontend install stamp,
# placeholder assets, the auth frame tool) are deliberately not phony.
.PHONY: default help help-overrides $(quiet_aliases)
.PHONY: ensure-node ensure-go ensure-golint ensure-deadcode ensure-modernize ensure-govulncheck
.PHONY: setup update-deps update-go-deps ci-frontend-deps
.PHONY: lint lint-only lint-ci lint-ci-only tsc tsc-only tsc-ci golint golint-only
.PHONY: test check-actions check-docs check-systemd check-frontend check-backend
.PHONY: test-frontend test-frontend-only test-frontend-ci setup-frontend-browser test-frontend-browser update-frontend-screenshots
.PHONY: test-backend test-go test-auth test-auth-protocol test-auth-pam
.PHONY: test-installation-scripts test-indexer-systemd-integration test-updater test-docker-update-integration test-btrfs-integration
.PHONY: deadcode deadcode-only analyze analyze-auth bundle-metrics compiler-coverage check-c-build-deps
.PHONY: dev-prep dev generate
.PHONY: build build-nocheck fastbuild build-vite build-backend build-bridge build-cli build-docker-update build-indexer build-monitoring build-btrfs-timer build-auth build-leak-profile build-go-binaries build-binaries print-binaries print-bin-dir
.PHONY: localinstall reinstall uninstall clean cloc
