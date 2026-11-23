# Validation lanes, backend lint and tests, dead-code scan and coverage reports.
# Lane scheduling lives in scripts/lanes.sh; the dead-code scan in scripts/deadcode.sh.
# Frontend lint/tsc/test targets are in frontend.mk, the C helper's in auth.mk.

GO_TEST_PKGS ?= ./...

##@ Quality checks|green
check-actions: ## Validate GitHub Actions workflows
	@command -v actionlint >/dev/null 2>&1 || { echo "❌ actionlint is required" >&2; exit 1; }
	@actionlint

check-docs: ## Verify the docs index, and that paths and links named in docs exist
	@bash "$(scripts_dir)/check-doc-links.sh"

check-systemd: ## Validate LinuxIO systemd units
	@command -v systemd-analyze >/dev/null 2>&1 || { echo "❌ systemd-analyze is required" >&2; exit 1; }
	@set -e; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp packaging/systemd/linuxio*.service packaging/systemd/linuxio*.socket packaging/systemd/linuxio*.target packaging/systemd/linuxio*.timer "$$tmp"; \
	sed -Ei 's|^ExecStart=-?[^ ]+|ExecStart=/bin/true|' "$$tmp"/*.service; \
	systemd-analyze verify --man=no "$$tmp"/linuxio*.service "$$tmp"/linuxio*.socket "$$tmp"/linuxio*.target "$$tmp"/linuxio*.timer

golint: ensure-golint ensure-modernize ensure-govulncheck dev-prep ## Run Go formatters + modernize + govulncheck + golangci-lint (backend)
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
	@( cd "$(backend_dir)" && $(GO_CMD_ENV) "$(golangci_lint)" run ./... --timeout 3m $(addprefix --build-tags=,$(nvml_go_tags)) $(GOLANGCI_LINT_OPTS) ) 2>&1 \
		| sed 's/^/      /'
	@$(PRINTC) "   $(COLOR_GREEN)✓$(COLOR_RESET) golangci-lint"
	@$(PRINTC) "$(COLOR_GREEN)✅ Go linting passed!$(COLOR_RESET)"

##@ Tests|green
# See scripts/lanes.sh for the lane layout and why it is scheduled this way.
test: setup dev-prep test-packaging-scripts check-docs ## Run lint + tsc + frontend tests + golint + backend tests + deadcode scan
	@MAKE="$(MAKE)" "$(scripts_dir)/lanes.sh" test "$(test_timer_start)"

##@ Quality checks|green
check-frontend: ensure-node setup-frontend ## Run frontend lint + typecheck + unit tests
	@MAKE="$(MAKE)" "$(scripts_dir)/lanes.sh" frontend

check-backend: ensure-go ensure-golint ensure-modernize ensure-govulncheck ensure-deadcode dev-prep ## Run backend lint + unit tests + deadcode scan
	@MAKE="$(MAKE)" "$(scripts_dir)/lanes.sh" backend

# Backend test entry point for CI (`make test` runs the same pieces through
# scripts/lanes.sh, with the C tests alongside golint). Race results
# are cached like normal test results, so incremental runs stay fast. Pass
# GO_TEST_FLAGS="-count=5" for a fresh sweep with more scheduling
# interleavings (races only surface on interleavings that actually happen).
test-backend: $(GO_BUILD_PREREQ) test-auth test-auth-protocol test-auth-pam ## Run Go + C backend tests (used by 'make test' + CI)
	@$(MAKE) --no-print-directory test-go

# Go unit tests only. Narrow with GO_TEST_PKGS and GO_TEST_FLAGS, e.g.
#   make test-go GO_TEST_PKGS=./bridge/handlers/filebrowser/... GO_TEST_FLAGS='-run TestExtract'
test-go: $(GO_BUILD_PREREQ) dev-prep ## Run Go unit tests only (GO_TEST_PKGS=./pkg/... GO_TEST_FLAGS='-run X' to narrow)
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running Go unit tests with race detector (backend)...$(COLOR_RESET)"
	@cd "$(backend_dir)" && \
		$(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=1 "$(GO_BIN)" test $(GO_TEST_PKGS) -race -tags "$(strip $(indexer_go_tags) $(nvml_go_tags))" $(GO_TEST_FLAGS) -timeout 10m 2>&1 \
		| grep --line-buffered -v '\[no test files\]' \
		| $(GOTEST_STATUS_SED); \
		exit "$${PIPESTATUS[0]}"

# Go benchmarks, without the race detector test-go runs under, which slows
# code several times over and so says nothing about speed. For example:
#   LINUXIO_INDEX_TREE=/usr make bench-go GO_TEST_PKGS=./indexer/internal/storage/ GO_BENCH=RescanOfTree
GO_BENCH ?= .
GO_BENCH_TIME ?= 1x
bench-go: $(GO_BUILD_PREREQ) dev-prep ## Run Go benchmarks without the race detector (GO_TEST_PKGS=./pkg/ GO_BENCH=Name GO_BENCH_TIME=1x)
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)⏱  Running Go benchmarks (backend)...$(COLOR_RESET)"
	@cd "$(backend_dir)" && \
		$(GO_CMD_ENV) GOFLAGS="-buildvcs=false" CGO_ENABLED=1 "$(GO_BIN)" test $(GO_TEST_PKGS) -run NONE -bench '$(GO_BENCH)' -benchtime $(GO_BENCH_TIME) -tags "$(strip $(indexer_go_tags) $(nvml_go_tags))" -timeout 30m

test-packaging-scripts: ## Run host-independent fixture tests of the packaging files and scripts
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running package file fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/tests/package-files-fixtures.sh"
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running package-root fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/tests/package-root-fixtures.sh"
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running package-version fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/tests/package-version-fixtures.sh"
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running third-party notices fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/tests/third-party-notices-fixtures.sh"
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running signed repository fixture tests...$(COLOR_RESET)"
	@bash "$(packaging_scripts_dir)/tests/repo-fixtures.sh"

test-indexer-systemd-integration: ## Run the opt-in disposable-host indexer smoke test
	@"$(packaging_scripts_dir)/tests/systemd-indexer-smoke.sh"

# The Docker update tests, here and in the test VM's docker suite (mk/vm.mk).
DOCKER_UPDATE_RUN := ^TestDockerUpdateCompose

test-docker-update-integration: ensure-go ## Run the opt-in real Docker/Compose update test
	@echo "🐳 Running native Docker update integration test..."
	@cd "$(backend_dir)" && \
	  LINUXIO_RUN_DOCKER_INTEGRATION=1 \
	  $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" \
	  "$(GO_BIN)" test ./common/dockerupdate -run '$(DOCKER_UPDATE_RUN)' -count=1 -v

test-assistant-agent-integration: ensure-go ## Run the opt-in real npm install and launch of the assistant agents (needs Node.js and network)
	@echo "🤖 Running assistant agent install integration test..."
	@cd "$(backend_dir)" && \
	  LINUXIO_RUN_ASSISTANT_AGENT_INTEGRATION=1 \
	  $(GO_CMD_ENV) GOFLAGS="-buildvcs=false" \
	  "$(GO_BIN)" test ./bridge/handlers/packages -run '^TestAssistantAgentIntegration$$' -count=1 -v -timeout 10m

# The root btrfs and partition kernel tests, run on this host by
# test-btrfs-integration and in the test VM by vm-test-btrfs-integration:
# common/btrfs (TestBtrfsDropoutIntegration needs the scsi_debug module), and
# the bridge storage rules that decide which disks a pool may take
# (TestBtrfsMemberIntegration) and destroy (TestBtrfsDestroyIntegration) and
# which members Format, Delete and Initialize erase (TestPartitionMemberIntegration).
# TestBtrfsRebuildProofIntegration and TestBtrfsAutoRebuildIntegration pull
# the VM's virtio data disks off the PCI bus, so they run only in the test VM
# and skip on a host.
BTRFS_INTEGRATION_RUN := ^Test(Btrfs(Degraded|Dropout|Unready|Resume|Replicate|Member|ArrayMember|AddWipe|Destroy|MountRecord|RebuildProof|AutoRebuild)?|Partition(Claim|Member)?)Integration$$
BTRFS_INTEGRATION_PKGS := ./common/btrfs ./bridge/handlers/storage

# -p 1: with two packages go test otherwise holds each one's output until it
# has finished, which is minutes of silence for common/btrfs. The summary
# script reads stdout only, so sudo's prompt and build errors are not held
# back by it.
test-btrfs-integration: ensure-go ## Run the root-only btrfs and partition kernel tests on loop devices
	@echo "🧪 Running btrfs and partition kernel integration tests on loop devices (needs sudo)..."
	@cd "$(backend_dir)" && \
	  sudo env \
	    PATH="$$(dirname "$(GO_BIN)"):$${PATH}" \
	    GOTOOLCHAIN="$(GO_TOOLCHAIN)" \
	    LINUXIO_RUN_BTRFS_INTEGRATION=1 \
	    "$(GO_BIN)" test $(BTRFS_INTEGRATION_PKGS) -run '$(BTRFS_INTEGRATION_RUN)' -p 1 -count=1 -v -timeout 20m \
	  | awk -f "$(scripts_dir)/gotest-summary.awk"; \
	  exit "$${PIPESTATUS[0]}"

# The file browser tests that hand entries to other users or groups, which only
# root can do; without root they skip or cover less.
FILEBROWSER_ROOT_RUN := AsRoot$$|^TestUpload(BufferNameIsNeverFollowed|BatchBufferNameIsNeverFollowed|BatchOverwriteOfASymlinkTakesNothingFromItsTarget|HandsOverOnlyAFreshFolder|KeepsTheGroupOfASetgidFolder)$$

test-filebrowser-root: ensure-go ## Run the root-only file browser ownership tests (needs sudo)
	@echo "🧪 Running file browser ownership tests as root (needs sudo)..."
	@cd "$(backend_dir)" && \
	  sudo env \
	    PATH="$$(dirname "$(GO_BIN)"):$${PATH}" \
	    GOTOOLCHAIN="$(GO_TOOLCHAIN)" \
	    "$(GO_BIN)" test ./bridge/handlers/filebrowser/... -run '$(FILEBROWSER_ROOT_RUN)' -count=1 -v

deadcode: ensure-deadcode dev-prep ## Report unreachable Go functions (informational)
	@$(MAKE) --no-print-directory deadcode-only

deadcode-only: ## Scan backend for dead code without tool setup
	@$(PRINTC) "$(COLOR_CYAN)🔎 Scanning backend for dead code (informational)...$(COLOR_RESET)"
	@$(GO_CMD_ENV) DEADCODE_CACHE="$(DEADCODE_CACHE)" DEADCODE_PARALLEL="$(DEADCODE_PARALLEL)" SHA256_CMD="$(sha256_cmd)" \
	  "$(scripts_dir)/deadcode.sh" "$(backend_dir)" "$(cache_dir)" "$(deadcode)" "$(GO_BIN)"

compiler-coverage: ## Report React Compiler memoization coverage (informational)
	@echo ""
	@echo "⚛️  Reporting React Compiler coverage..."
	@cd "$(frontend_dir)" && npm run compiler:coverage
