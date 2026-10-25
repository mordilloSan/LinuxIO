# Validation lanes, backend lint and tests, dead-code scan and coverage reports.
# Lane scheduling lives in scripts/lanes.sh; the dead-code scan in scripts/deadcode.sh.
# Frontend lint/tsc/test targets are in frontend.mk, the C helper's in auth.mk.

GO_TEST_PKGS ?= ./...

##@ Quality checks|green
check-actions: ## Validate GitHub Actions workflows
	@command -v actionlint >/dev/null 2>&1 || { echo "❌ actionlint is required" >&2; exit 1; }
	@actionlint

check-docs: ## Verify repo paths and links referenced by docs exist
	@bash "$(scripts_dir)/check-doc-links.sh"

check-systemd: ## Validate LinuxIO systemd units
	@command -v systemd-analyze >/dev/null 2>&1 || { echo "❌ systemd-analyze is required" >&2; exit 1; }
	@set -e; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp packaging/systemd/linuxio*.service packaging/systemd/linuxio*.socket packaging/systemd/linuxio*.target packaging/systemd/linuxio*.timer "$$tmp"; \
	sed -Ei 's|^ExecStart=-?[^ ]+|ExecStart=/bin/true|' "$$tmp"/*.service; \
	systemd-analyze verify --man=no "$$tmp"/linuxio*.service "$$tmp"/linuxio*.socket "$$tmp"/linuxio*.target "$$tmp"/linuxio*.timer

golint: ensure-golint ensure-modernize ensure-govulncheck ## Run Go formatters + modernize + govulncheck + golangci-lint (backend)
	@$(MAKE) --no-print-directory golint-only

golint-only: ## Run backend formatting, modernization, vulnerability, and lint checks
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

# See scripts/lanes.sh for the lane layout and why it is scheduled this way.
test: ensure-node ensure-go ensure-golint ensure-modernize ensure-govulncheck ensure-deadcode setup dev-prep test-installation-scripts check-docs ## Run lint + tsc + frontend tests + golint + backend tests + deadcode scan
	@MAKE="$(MAKE)" "$(scripts_dir)/lanes.sh" test "$(test_timer_start)"

check-frontend: ensure-node setup ## Run frontend lint + typecheck + unit tests
	@MAKE="$(MAKE)" "$(scripts_dir)/lanes.sh" frontend

check-backend: ensure-go ensure-golint ensure-modernize ensure-govulncheck ensure-deadcode ## Run backend lint + unit tests + deadcode scan
	@MAKE="$(MAKE)" "$(scripts_dir)/lanes.sh" backend

# Backend test entry point; this is what `make test` and CI run. Race results
# are cached like normal test results, so incremental runs stay fast. Pass
# GO_TEST_FLAGS="-count=5" for a fresh sweep with more scheduling
# interleavings (races only surface on interleavings that actually happen).
test-backend: $(GO_BUILD_PREREQ) test-auth test-auth-protocol test-auth-pam ## Run Go + C backend tests (used by 'make test' + CI)
	@$(MAKE) --no-print-directory test-go

# Go unit tests only. Narrow with GO_TEST_PKGS and GO_TEST_FLAGS, e.g.
#   make test-go GO_TEST_PKGS=./bridge/handlers/filebrowser/... GO_TEST_FLAGS='-run TestExtract'
test-go: $(GO_BUILD_PREREQ) ## Run Go unit tests only (GO_TEST_PKGS=./pkg/... GO_TEST_FLAGS='-run X' to narrow)
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running Go unit tests with race detector (backend)...$(COLOR_RESET)"
	@cd "$(backend_dir)" && \
		$(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=1 "$(GO_BIN)" test $(GO_TEST_PKGS) -race $(GO_TEST_FLAGS) -timeout 10m 2>&1 \
		| grep --line-buffered -v '\[no test files\]' \
		| $(GOTEST_STATUS_SED); \
		exit "$${PIPESTATUS[0]}"

test-installation-scripts: ## Run host-independent installer fixture tests
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running install-dependencies fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/tests/install-dependencies-fixtures.sh"
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running installer fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/tests/installer-fixtures.sh"

test-indexer-systemd-integration: ## Run the opt-in disposable-host indexer smoke test
	@"$(packaging_scripts_dir)/tests/systemd-indexer-smoke.sh"

test-updater: ensure-go ## Run the root-only updater systemd dry-run integration test
	@echo "🔎 Running updater systemd dry-run integration test..."
	@cd "$(backend_dir)" && \
	  sudo env \
	    PATH="$$(dirname "$(GO_BIN)"):$${PATH}" \
	    GOTOOLCHAIN="$(GO_TOOLCHAIN)" \
	    LINUXIO_RUN_SYSTEMD_INTEGRATION=1 \
	    "$(GO_BIN)" test ./bridge/handlers/appupdate -run TestInstallScriptDryRunWithSystemdSandbox -count=1 -v

test-docker-update-integration: ensure-go ## Run the opt-in real Docker/Compose update test
	@echo "🐳 Running native Docker update integration test..."
	@cd "$(backend_dir)" && \
	  LINUXIO_RUN_DOCKER_INTEGRATION=1 \
	  $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" \
	  "$(GO_BIN)" test ./common/dockerupdate -run '^TestDockerUpdateCompose' -count=1 -v

test-btrfs-integration: ensure-go ## Run the root-only btrfs kernel tests on loop devices
	@echo "🧪 Running btrfs kernel integration tests on loop devices (needs sudo)..."
	@cd "$(backend_dir)" && \
	  sudo env \
	    PATH="$$(dirname "$(GO_BIN)"):$${PATH}" \
	    GOTOOLCHAIN="$(GO_TOOLCHAIN)" \
	    LINUXIO_RUN_BTRFS_INTEGRATION=1 \
	    "$(GO_BIN)" test ./common/btrfs -run '^TestBtrfs(Degraded)?Integration$$' -count=1 -v -timeout 20m

deadcode: ensure-deadcode ## Report unreachable Go functions (informational)
	@$(MAKE) --no-print-directory deadcode-only

deadcode-only: ## Scan backend for dead code without tool setup
	@$(PRINTC) "$(COLOR_CYAN)🔎 Scanning backend for dead code (informational)...$(COLOR_RESET)"
	@$(GO_CMD_ENV) DEADCODE_CACHE="$(DEADCODE_CACHE)" DEADCODE_PARALLEL="$(DEADCODE_PARALLEL)" SHA256_CMD="$(sha256_cmd)" \
	  "$(scripts_dir)/deadcode.sh" "$(backend_dir)" "$(cache_dir)" "$(deadcode)" "$(GO_BIN)"

compiler-coverage: ## Report React Compiler memoization coverage (informational)
	@echo ""
	@echo "⚛️  Reporting React Compiler coverage..."
	@cd "$(frontend_dir)" && npm run compiler:coverage
