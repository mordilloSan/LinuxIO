# Toolchain provisioning: user-local Go, Node through nvm, and the Go tool
# installers. The bodies live in scripts/ensure-*.sh.

go_tools_dir := $(if $(GO_TOOLS_DIR),$(GO_TOOLS_DIR),$(HOME)/.go)
GO_TOOLS_DIR ?= $(go_tools_dir)
go_toolchain_versions_dir := $(if $(GO_TOOLCHAIN_VERSIONS_DIR),$(GO_TOOLCHAIN_VERSIONS_DIR),$(HOME)/.go-versions)
GO_TOOLCHAIN_VERSIONS_DIR ?= $(go_toolchain_versions_dir)
GO_TOOLCHAIN_DIR := $(go_toolchain_versions_dir)/go$(go_version)
GO_TOOLCHAIN_CURRENT := $(go_toolchain_versions_dir)/current
NVM_DIR ?= $(HOME)/.nvm
export PATH := $(GO_TOOLCHAIN_CURRENT)/bin:$(go_tools_dir)/bin:$(NVM_DIR)/versions/node/current/bin:$(PATH)

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

# Trailing arguments shared by every Go tool installer.
go_tool_args = "$(GO_BIN)" "$(GO_VERSION)" "$(go_tools_dir)"

##@ Toolchain setup|green
ensure-node: ## Install/activate Node @NODE@ via nvm
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🔍 Ensuring Node.js $(NODE_VERSION) is available...$(COLOR_RESET)"
	@bash -l "$(scripts_dir)/ensure-node.sh" "$(NODE_VERSION)" "$(nvm_version)" "$(NVM_DIR)"
	@$(PRINTC) "$(COLOR_GREEN)✅ Node.js environment ready!$(COLOR_RESET)"

ensure-go: ## Install Go @GO@ (user-local, no sudo)
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🔍 Ensuring Go $(GO_VERSION) is available...$(COLOR_RESET)"
	@bash -l "$(scripts_dir)/ensure-go.sh" "$(GO_VERSION)" "$(GO_TOOLCHAIN_DIR)" "$(GO_TOOLCHAIN_CURRENT)" "$(go_tools_dir)" "$(GOOS)" "$(GOARCH)"

ensure-golint: ensure-go ## Install golangci-lint (built with local Go @GO@)
	@$(GO_CMD_ENV) "$(scripts_dir)/ensure-go-tool.sh" golangci-lint "$(golangci_lint)" "$(GOLANGCI_LINT_MODULE)" "$(GOLANGCI_LINT_VERSION)" $(go_tool_args)

ensure-deadcode: ensure-go ## Install deadcode (built with local Go @GO@)
	@$(GO_CMD_ENV) "$(scripts_dir)/ensure-go-tool.sh" deadcode "$(deadcode)" "$(DEADCODE_MODULE)" "$(DEADCODE_VERSION)" $(go_tool_args)

ensure-modernize: ensure-go ## Install modernize (built with local Go @GO@)
	@$(GO_CMD_ENV) "$(scripts_dir)/ensure-go-tool.sh" modernize "$(modernize)" "$(MODERNIZE_MODULE)" "$(MODERNIZE_VERSION)" $(go_tool_args)

ensure-govulncheck: ensure-go ## Install govulncheck (built with local Go @GO@)
	@$(GO_CMD_ENV) "$(scripts_dir)/ensure-go-tool.sh" govulncheck "$(govulncheck)" "$(GOVULNCHECK_MODULE)" "$(GOVULNCHECK_VERSION)" $(go_tool_args)
