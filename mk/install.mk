# Development server, install/uninstall glue, cleanup and housekeeping.
# The dev-server supervisor lives in scripts/dev.sh, the LOC counter in scripts/loc.sh.

##@ Development|yellow
dev: setup-frontend dev-prep ## Start frontend dev server (detached) + tail LinuxIO logs
	@echo ""
	@echo "🚀 Starting development servers..."
	@echo "   Backend must be running via: sudo systemctl start linuxio"
	@echo "   Vite proxies /ws, /auth, /api to port 8090"
	@echo "   Vite log: $(VITE_DEV_LOG)"
	@echo ""
	@"$(scripts_dir)/dev.sh" "$(frontend_dir)" "$(VITE_DEV_PID)" "$(VITE_DEV_LOG)" "$(VITE_DEV_PORT)" "$(DEV_LOG_LINES)"

dev-stop: ## Stop the Vite dev server; every running make dev then exits
	@if [ -f "$(VITE_DEV_PID)" ] && kill "$$(cat "$(VITE_DEV_PID)")" 2>/dev/null; then \
		echo "✅ Vite stopped"; \
	else \
		echo "  Vite not running"; \
	fi
	@rm -f "$(VITE_DEV_PID)" "$(VITE_DEV_LOG)"

dev-status-install: ## Install the VS Code dev-server status bar extension (symlink)
	@version="$$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "$(dev_status_dir)/package.json")"; \
	test -n "$$version" || { echo "❌ No version in $(dev_status_dir)/package.json"; exit 1; }; \
	mkdir -p "$(VSCODE_EXTENSIONS_DIR)"; \
	find "$(VSCODE_EXTENSIONS_DIR)" -maxdepth 1 -type l -name 'linuxio.linuxio-dev-status-*' -delete; \
	ln -s "$(dev_status_dir)" "$(VSCODE_EXTENSIONS_DIR)/linuxio.linuxio-dev-status-$$version"; \
	echo "✅ Installed into $(VSCODE_EXTENSIONS_DIR); run 'Developer: Reload Window' in VS Code"

dev-status-uninstall: ## Remove the VS Code dev-server status bar extension
	@if [ -n "$$(find "$(VSCODE_EXTENSIONS_DIR)" -maxdepth 1 -type l -name 'linuxio.linuxio-dev-status-*' 2>/dev/null)" ]; then \
		find "$(VSCODE_EXTENSIONS_DIR)" -maxdepth 1 -type l -name 'linuxio.linuxio-dev-status-*' -delete; \
		echo "✅ Removed from $(VSCODE_EXTENSIONS_DIR); run 'Developer: Reload Window' in VS Code"; \
	else \
		echo "  Not installed in $(VSCODE_EXTENSIONS_DIR)"; \
	fi

# sudo wrapper for localinstall; the VS Code install command overrides it with "sudo -A".
SUDO ?= sudo

##@ Install / Uninstall|red
# The package of this host's family (/etc/os-release), assembled as the build
# user from the fastbuild programs; only apt or dnf and the start run through
# SUDO. The package restarts LinuxIO if it ran, so the start only brings up
# one that was stopped.
localinstall: fastbuild ## Build, package as a DEB or RPM for this host, and install it with apt or dnf
	@set -e
	. /etc/os-release
	case " $${ID:-} $${ID_LIKE:-} " in
	*" debian "* | *" ubuntu "*) family=deb ;;
	*" fedora "* | *" rhel "*) family=rpm ;;
	*) echo "❌ localinstall knows the Debian and RHEL families; /etc/os-release says $${ID:-nothing}" >&2; exit 1 ;;
	esac
	echo ""
	echo "📦 Packaging LinuxIO $(PACKAGE_VERSION) as a $$family..."
	file="$$("$(packaging_scripts_dir)/package-$$family.sh" --build-dir "$(bin_dir)" --version "$(PACKAGE_VERSION)" --out-dir "$(packages_dir)")"
	echo "📦 Installing $$file..."
	if [ "$$family" = deb ]; then
	  $(SUDO) apt-get install --reinstall --allow-downgrades -y "$$file"
	elif [ "$$(rpm -q --qf '%{VERSION}' linuxio 2>/dev/null)" = "$(PACKAGE_VERSION)" ]; then
	  $(SUDO) dnf reinstall -y "$$file"
	else
	  $(SUDO) dnf install -y "$$file"
	fi
	$(SUDO) systemctl start linuxio.target
	echo "✅ LinuxIO $(PACKAGE_VERSION) installed"

# For a LinuxIO the script installer put in, which the package refuses to
# install over.
uninstall: ## Remove a LinuxIO installed by the install script
	@echo ""
	@echo "🗑️  Uninstalling LinuxIO..."
	@sudo "$(packaging_scripts_dir)/uninstall.sh"

##@ Run / Clean|red
clean: ## Remove binaries, node_modules, and generated assets
	@for bin in $(shipped_binaries); do rm -f "$(bin_dir)/$$bin"; done
	@rm -f "$(packages_dir)"/linuxio_*.deb "$(packages_dir)"/linuxio-*.rpm
	@rmdir "$(packages_dir)" 2>/dev/null || true
	@rm -f "$(VITE_DEV_PID)" "$(VITE_DEV_LOG)" "$(frontend_dir)/tsconfig.tsbuildinfo" || true
	@rm -rf "$(cache_dir)" "$(frontend_node_modules_dir)" || true
	@find "$(backend_frontend_dir)" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
	@rmdir "$(bin_dir)" 2>/dev/null || true
	@echo "🧹 Cleaned workspace."

cloc: ## Count handwritten source lines
	@echo "====>   Handwritten LOC    <===="
	@"$(scripts_dir)/loc.sh" . "$(LOC_INCLUDE_EXT)" 1 "$(backend_frontend_rel_dir)"
