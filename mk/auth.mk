# The C authentication helper (linuxio-auth): hardened build, tests, analysis.
# Test and analysis bodies live in scripts/test-auth.sh and scripts/analyze-auth.sh.

# ---- toolchain --------------------------------------------------------------
UNAME_S  := $(shell uname -s)

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

# Libraries every auth build links: PAM plus libsystemd (journald), found
# through pkg-config when available. Deferred so only auth recipes run it.
auth_libs = -lpam $(or $(shell command -v pkg-config >/dev/null 2>&1 && pkg-config --exists libsystemd 2>/dev/null && pkg-config --libs libsystemd),-lsystemd)

# Environment scripts/test-auth.sh compiles the test binaries with.
auth_test_env = CC="$(CC)" CFLAGS="$(CFLAGS)" LDFLAGS="$(LDFLAGS)" AUTH_LIBS="$(auth_libs)"

##@ Quality checks|green
check-c-build-deps: ## Check C authentication build dependencies
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

##@ Build|yellow
build-auth: ## Build the PAM authentication helper
	@echo ""
	@echo "🏗️  Building Session helper (C)..."
	@set -euo pipefail; \
	mkdir -p "$(bin_dir)"; \
	if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists libsystemd 2>/dev/null; then \
	  echo "   Linking with libsystemd for journald support (via pkg-config)"; \
	elif [ -f /usr/include/systemd/sd-journal.h ]; then \
	  echo "   Linking with libsystemd for journald support"; \
	else \
	  echo "❌ libsystemd-dev/systemd-devel is required to build linuxio-auth"; \
	  echo "   Install with: sudo apt-get install libsystemd-dev"; \
	  exit 1; \
	fi; \
	$(CC) $(CFLAGS) -DLINUXIO_VERSION=\"$(GIT_VERSION)\" -o "$(auth_binary)" "$(backend_auth_dir)/linuxio-auth.c" $(LDFLAGS) $(auth_libs); \
	if [ "$(STRIP)" = "1" ]; then strip --strip-unneeded "$(auth_binary)"; fi; \
	echo "✅ Session helper built successfully!"; \
	echo "   Path: $(auth_binary)"; \
	echo "   Size: $$(du -h "$(auth_binary)" | cut -f1)"; \
	echo "   SHA256: $(call sha256_of,$(auth_binary))"; \
	if command -v checksec >/dev/null 2>&1; then \
	  echo " checksec:"; checksec --file="$(auth_binary)" || true; \
	fi

##@ Quality checks|green
test-auth: check-c-build-deps ## Run C authentication helper tests
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running C authentication helper tests...$(COLOR_RESET)"
	@$(auth_test_env) "$(scripts_dir)/test-auth.sh" unit "$(backend_auth_dir)"

# Stable path + real file deps keep the frametool's mtime unchanged between
# runs, so Go's test cache (which stats it) can reuse a passing result. It
# lives inside the backend module because Go ignores files outside it when
# keying the test cache.
auth_frametool_bin := $(backend_dir)/.cache/auth-frametool/linuxio-auth-frametool

$(auth_frametool_bin): $(backend_auth_dir)/linuxio-auth-frametool.c $(backend_auth_dir)/linuxio-auth.c $(backend_auth_dir)/linuxio_protocol.h | check-c-build-deps
	@$(auth_test_env) "$(scripts_dir)/test-auth.sh" frametool "$(backend_auth_dir)" "$@"

test-auth-protocol: $(auth_frametool_bin) $(GO_BUILD_PREREQ) ## Run cross-language (C<->Go) auth protocol frame tests
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running cross-language auth protocol tests...$(COLOR_RESET)"
	@set -euo pipefail; \
	cd "$(backend_dir)" && LINUXIO_AUTH_FRAMETOOL="$(auth_frametool_bin)" $(GO_CMD_ENV) \
	  "$(GO_BIN)" test ./common/ipc/auth -run TestCrossLanguage $(GO_TEST_FLAGS) | $(GOTEST_STATUS_SED); \
	$(PRINTC) "$(COLOR_GREEN)✅ Cross-language auth protocol tests passed!$(COLOR_RESET)"

# Tier-1 hermetic PAM/host-integration suite for the C launcher. Needs the
# cwrap wrapper libraries (apt install libpam-wrapper libnss-wrapper
# libuid-wrapper) and skips with a warning when they are missing.
# Non-standard library location:
#   make test-auth-pam LINUXIO_CWRAP_LIBDIR=/path/containing/the/so/files
test-auth-pam: check-c-build-deps ## Run hermetic PAM integration tests (pam_wrapper)
	@echo ""
	@$(PRINTC) "$(COLOR_CYAN)🧪 Running hermetic PAM integration tests (pam_wrapper)...$(COLOR_RESET)"
	@$(auth_test_env) "$(scripts_dir)/test-auth.sh" pam "$(backend_auth_dir)" "$(LINUXIO_CWRAP_LIBDIR)"

# libpam's conversation callback ABI requires void *appdata_ptr; see the
# suppressions in scripts/analyze-auth.sh.
analyze-auth: ## Run C static analysis on linuxio-auth
	@echo ""
	@echo "🔬 Running C static analysis (linuxio-auth)..."
	@CC="$(CC)" MAKE="$(MAKE)" SB_WARNFLAGS="$(filter-out -Wduplicated-cond -Wlogical-op,$(WARNFLAGS)) -Wno-format-nonliteral" \
	  "$(scripts_dir)/analyze-auth.sh" "$(backend_auth_dir)/linuxio-auth.c" "$(cache_dir)"
