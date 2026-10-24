# Go builds, the shipped-binary manifest, code generation and Go dependency updates.

backend_binary := $(bin_dir)/linuxio-webserver
bridge_binary := $(bin_dir)/linuxio-bridge
cli_binary := $(bin_dir)/linuxio
auth_binary := $(bin_dir)/linuxio-auth
docker_update_binary := $(bin_dir)/linuxio-docker-update
indexer_binary := $(bin_dir)/linuxio-indexer
monitoring_binary := $(bin_dir)/linuxio-monitoring
btrfs_timer_binary := $(bin_dir)/linuxio-btrfs-timer

# The shipped-binary manifest. Every consumer (release workflow, installers'
# fixture test) reads this list through `make print-binaries`; add a binary
# here, give it a build target, and add it to build-go-binaries/build-binaries.
shipped_binaries := linuxio linuxio-webserver linuxio-bridge linuxio-auth linuxio-docker-update linuxio-indexer linuxio-monitoring linuxio-btrfs-timer

# sha256_of FILE: hex digest of FILE, for use inside a recipe's shell.
sha256_of = $$($(sha256_cmd) "$(1)" | awk '{ print $$1 }')

# Linker flags shared by the binaries that report their version.
go_version_ldflags := -s -w -X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)'

##@ Toolchain setup|green
# Export module arguments as data rather than interpolating them into shell code.
update-go-deps: export GO_MODULES := $(GO_MODULES)
update-go-deps: ensure-go ## Update all Go dependencies (optional GO_MODULES=...)
	@set -euo pipefail; \
	read -r -a modules <<< "$${GO_MODULES}"; \
	for module in "$${modules[@]}"; do \
	  case "$$module" in -*) echo "Expected a Go module, not an option: $$module" >&2; exit 1 ;; esac; \
	done; \
	if [ "$${#modules[@]}" -eq 0 ]; then modules=(-u -t ./...); fi; \
	cd "$(backend_dir)"; \
	$(GO_CMD_ENV) "$(GO_BIN)" get "$${modules[@]}"; \
	$(GO_CMD_ENV) "$(GO_BIN)" mod tidy

##@ Development|yellow
generate: ensure-go ensure-node setup ## Regenerate frontend API contracts
	@cd "$(backend_dir)" && $(GO_CMD_ENV) "$(GO_BIN)" run ./bridge/internal/apigen

##@ Build|yellow
build: generate test build-vite build-binaries ## Full build (test + frontend + all binaries)

build-nocheck: generate build-vite build-binaries ## Full build without running tests

fastbuild: generate build-binaries ## Quick binary build (skip tests and frontend)

build-backend: $(GO_BUILD_PREREQ) ## Build Go backend binary
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
	-ldflags "$(go_version_ldflags) -X '$(MODULE_PATH)/common/version.BridgeSHA256=$(BRIDGE_SHA256)'" \
	$(GO_BUILD_TAGS_FLAG) \
	-o "$(backend_binary)" ./webserver/ && \
	echo "✅ Backend built successfully!" && \
	echo "   Path: $(backend_binary)" && \
	echo "   Version: $(GIT_VERSION)" && \
	echo "   Size: $$(du -h "$(backend_binary)" | cut -f1)" && \
	echo "   SHA256: $(call sha256_of,$(backend_binary))"

# One generic recipe builds the other Go binaries; the target-specific
# variables below describe each one.
go_binary_targets := build-bridge build-cli build-docker-update build-btrfs-timer build-indexer build-monitoring

build-bridge: go_binary_label := bridge
build-bridge: go_binary_package := ./bridge
build-bridge: go_binary_output := $(bridge_binary)
build-bridge: go_binary_ldflags := $(go_version_ldflags)
build-bridge: ## Build Go bridge binary

build-cli: go_binary_label := CLI
build-cli: go_binary_package := ./cli
build-cli: go_binary_output := $(cli_binary)
build-cli: go_binary_ldflags := $(go_version_ldflags)
build-cli: ## Build the CLI tool

build-docker-update: go_binary_label := Docker update worker
build-docker-update: go_binary_package := ./docker-update
build-docker-update: go_binary_output := $(docker_update_binary)
build-docker-update: go_binary_ldflags := -s -w
build-docker-update: ## Build the scheduled Docker update worker

build-btrfs-timer: go_binary_label := btrfs timer worker
build-btrfs-timer: go_binary_package := ./btrfs-timer
build-btrfs-timer: go_binary_output := $(btrfs_timer_binary)
build-btrfs-timer: go_binary_ldflags := -s -w
build-btrfs-timer: ## Build the btrfs snapshot/health timer worker

build-indexer: go_binary_label := filesystem indexer
build-indexer: go_binary_package := ./indexer
build-indexer: go_binary_output := $(indexer_binary)
build-indexer: go_binary_ldflags := $(go_version_ldflags)
build-indexer: go_binary_extra_env := CGO_ENABLED=1
build-indexer: go_binary_tags := sqlite_fts5
build-indexer: ## Build the filesystem indexer

build-monitoring: go_binary_label := monitoring daemon
build-monitoring: go_binary_package := ./monitoring
build-monitoring: go_binary_output := $(monitoring_binary)
build-monitoring: go_binary_ldflags := $(go_version_ldflags)
build-monitoring: go_binary_extra_env := CGO_ENABLED=1
# NVML is compiled only for amd64; other architectures use the stub. Deferred
# expansion keeps `go env` out of every unrelated make invocation.
build-monitoring: go_binary_tags = $(if $(filter amd64,$(if $(GOARCH),$(GOARCH),$(GOARCH_HOST))),glibc,)
build-monitoring: ## Build the monitoring daemon

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

# Debug-only binaries for goroutine leak hunting. Serves
# net/http/pprof on localhost only (webserver :6060, bridge :6061). The leak
# report lives at /debug/pprof/goroutineleak. The endpoint has no auth (it is
# loopback-bound). Rebuild with `make build` afterwards.
build-leak-profile: ## Build DEBUG webserver+bridge with localhost pprof + goroutine leak profile
	@echo ""
	@echo "🕵️  Building DEBUG binaries with pprof + goroutine leak profile..."
	@$(MAKE) --no-print-directory build-bridge \
		GO_BUILD_TAGS="pprofdebug"
	@BRIDGE_HASH=$(call sha256_of,$(bridge_binary)); \
	echo "   Bridge hash: $$BRIDGE_HASH"; \
	$(MAKE) --no-print-directory build-backend BRIDGE_SHA256=$$BRIDGE_HASH SKIP_ENSURE_GO=1 \
		GO_BUILD_TAGS="pprofdebug"
	@echo ""
	@echo "   Webserver pprof: http://127.0.0.1:6060/debug/pprof/  (leaks: /debug/pprof/goroutineleak)"
	@echo "   Bridge pprof:    http://127.0.0.1:6061/debug/pprof/"
	@echo "   ⚠️  Debug binaries only — rebuild with 'make build' before packaging."

# Every Go binary, sequenced once: bridge first, then the webserver with the
# bridge hash embedded. Honours SKIP_ENSURE_GO=1 (CI); build-binaries adds the
# C auth helper.
build-go-binaries: $(GO_BUILD_PREREQ) ## Build every Go binary (no C auth helper)
	@$(MAKE) --no-print-directory build-bridge
	@echo ""
	@echo "🔑 Capturing bridge hash for backend build..."
	@BRIDGE_HASH=$(call sha256_of,$(bridge_binary)); \
	echo "   Hash: $$BRIDGE_HASH"; \
	$(MAKE) --no-print-directory build-backend BRIDGE_SHA256=$$BRIDGE_HASH SKIP_ENSURE_GO=1
	@$(MAKE) --no-print-directory build-cli build-docker-update build-indexer build-monitoring build-btrfs-timer SKIP_ENSURE_GO=1

build-binaries: check-c-build-deps build-go-binaries build-auth ## Build every shipped binary into @BIN@

print-binaries: ## Print the shipped-binary names, one per line
	@printf '%s\n' $(shipped_binaries)

print-bin-dir:
	@printf '%s\n' "$(bin_dir)"
