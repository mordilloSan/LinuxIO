# Toolchain provisioning: Node through nvm, and the Go tool installers. Go
# itself comes from PATH (install it with linux-toolbox or from go.dev); the
# exact version go.mod pins is fetched by Go's own toolchain switching. Node
# and the Go tools are the versions packaging/build/pins.env names, which
# update-build-pins moves with the release images and the action commits.

go_tools_dir := $(if $(GO_TOOLS_DIR),$(GO_TOOLS_DIR),$(HOME)/.go)
GO_TOOLS_DIR ?= $(go_tools_dir)
NVM_DIR ?= $(HOME)/.nvm
export PATH := $(go_tools_dir)/bin:$(NVM_DIR)/versions/node/current/bin:$(PATH)

GO_BIN ?= $(if $(CODEQL_ACTION_GO_BINARY),$(CODEQL_ACTION_GO_BINARY),$(shell command -v go 2>/dev/null || printf 'go'))
GO_PATH_PREFIX := $(dir $(GO_BIN)):$(go_tools_dir)/bin
GO_CMD_ENV := PATH="$(GO_PATH_PREFIX):$$PATH" GOTOOLCHAIN=$(GO_TOOLCHAIN) GOOS="$(GOOS)" $(if $(GOARCH),GOARCH="$(GOARCH)",)
GOLANGCI_LINT_MODULE  := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOLANGCI_LINT_VERSION ?= $(call build_pin,GOLANGCI_LINT_VERSION)
golangci_lint := $(if $(GOLANGCI_LINT),$(GOLANGCI_LINT),$(go_tools_dir)/bin/golangci-lint)
GOLANGCI_LINT ?= $(golangci_lint)
MODERNIZE_MODULE      := golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize
MODERNIZE_VERSION    ?= $(call build_pin,MODERNIZE_VERSION)
modernize := $(if $(MODERNIZE),$(MODERNIZE),$(go_tools_dir)/bin/modernize)
MODERNIZE ?= $(modernize)
DEADCODE_MODULE       := golang.org/x/tools/cmd/deadcode
DEADCODE_VERSION     ?= $(call build_pin,DEADCODE_VERSION)
deadcode := $(if $(DEADCODE),$(DEADCODE),$(go_tools_dir)/bin/deadcode)
DEADCODE ?= $(deadcode)
GOVULNCHECK_MODULE    := golang.org/x/vuln/cmd/govulncheck
GOVULNCHECK_VERSION  ?= $(call build_pin,GOVULNCHECK_VERSION)
govulncheck := $(if $(GOVULNCHECK),$(GOVULNCHECK),$(go_tools_dir)/bin/govulncheck)
GOVULNCHECK ?= $(govulncheck)
GO_BUILD_PREREQ := ensure-go
ifeq ($(SKIP_ENSURE_GO),1)
GO_BUILD_PREREQ :=
endif

# Trailing arguments shared by every Go tool installer.
go_tool_args = "$(GO_BIN)" "$(GO_VERSION)" "$(go_tools_dir)"

##@ Toolchain setup|green
ensure-node: ## Install/activate Node @NODE@ via nvm
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🔍 Ensuring Node.js $(NODE_VERSION) is available...$(COLOR_RESET)"
	@bash -l "$(scripts_dir)/ensure-node.sh" "$(NODE_VERSION)" "$(nvm_version)" "$(NVM_DIR)"
	@$(PRINTC) "$(COLOR_GREEN)✅ Node.js environment ready!$(COLOR_RESET)"

ensure-go: ## Check that Go is on PATH and resolve go.mod's Go @GO@ (fetched by Go itself)
	@command -v "$(GO_BIN)" >/dev/null 2>&1 || { \
	  $(PRINTC) "$(COLOR_RED)Go is not installed. Install it with linux-toolbox or from https://go.dev/dl/$(COLOR_RESET)"; exit 1; }
	@version="$$($(GO_CMD_ENV) "$(GO_BIN)" env GOVERSION)" && \
	  $(PRINTC) "$(COLOR_GREEN)✅ Go $$version ready ($(GO_BIN))$(COLOR_RESET)"

ensure-golint: ensure-go ## Install golangci-lint (built with local Go @GO@)
	@$(GO_CMD_ENV) "$(scripts_dir)/ensure-go-tool.sh" golangci-lint "$(golangci_lint)" "$(GOLANGCI_LINT_MODULE)" "$(GOLANGCI_LINT_VERSION)" $(go_tool_args)

ensure-deadcode: ensure-go ## Install deadcode (built with local Go @GO@)
	@$(GO_CMD_ENV) "$(scripts_dir)/ensure-go-tool.sh" deadcode "$(deadcode)" "$(DEADCODE_MODULE)" "$(DEADCODE_VERSION)" $(go_tool_args)

ensure-modernize: ensure-go ## Install modernize (built with local Go @GO@)
	@$(GO_CMD_ENV) "$(scripts_dir)/ensure-go-tool.sh" modernize "$(modernize)" "$(MODERNIZE_MODULE)" "$(MODERNIZE_VERSION)" $(go_tool_args)

ensure-govulncheck: ensure-go ## Install govulncheck (built with local Go @GO@)
	@$(GO_CMD_ENV) "$(scripts_dir)/ensure-go-tool.sh" govulncheck "$(govulncheck)" "$(GOVULNCHECK_MODULE)" "$(GOVULNCHECK_VERSION)" $(go_tool_args)

# Everything a fresh checkout needs for make test and make build. Playwright
# Chromium (setup-frontend-browser) is a large download only browser tests use,
# and the C build packages are checked by check-c-build-deps, not installed.
setup: ensure-node ensure-go ensure-golint ensure-deadcode ensure-modernize ensure-govulncheck setup-frontend ## Install Node, the Go tools and frontend dependencies
	@$(PRINTC) "$(COLOR_GREEN)✅ Development toolchain ready!$(COLOR_RESET)"

# Needs only curl, jq and git: no sudo, no docker. Go modules and npm packages
# keep their own targets (update-go-deps, update-deps).
update-build-pins: ## Move the release images, Node, the Go tools and the action commits to their newest
	@"$(scripts_dir)/update-build-pins.sh" "$(repo_root)"

# Needs only curl and jq (gh, when installed, reads the release notes). The
# notes of every release in between go to .cache/agent-pins/release-notes.md.
update-agent-pins: ## Move the Assistant's agent pins to the versions the ACP registry lists
	@"$(scripts_dir)/update-agent-pins.sh" "$(repo_root)"

# Starts a real chat on the owner's Claude login, so the owner runs it, never
# an agent. Installs the pinned adapter in .cache/subagent-trace/agent; the
# chat runs on Sonnet unless MODEL= names another of the adapter's models.
record-subagent-trace: ## Record the Assistant's subagent chat again with the pinned Claude adapter (MODEL=sonnet)
	@cd "$(frontend_dir)" && MODEL="$(MODEL)" node scripts/record-subagent-trace.mjs
