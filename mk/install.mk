# Development server, install/uninstall glue, cleanup and housekeeping.
# The dev-server supervisor lives in scripts/dev.sh, the LOC counter in scripts/loc.sh.

##@ Development|yellow
dev: setup dev-prep ## Start frontend dev server (detached) + tail LinuxIO logs
	@echo ""
	@echo "🚀 Starting development servers..."
	@echo "   Backend must be running via: sudo systemctl start linuxio"
	@echo "   Vite proxies /ws, /auth, /api to port 8090"
	@echo "   Vite log: $(VITE_DEV_LOG)"
	@echo ""
	@"$(scripts_dir)/dev.sh" "$(frontend_dir)" "$(VITE_DEV_PID)" "$(VITE_DEV_LOG)" "$(VITE_DEV_PORT)" "$(DEV_LOG_LINES)"

##@ Install / Uninstall|red
localinstall: ## Install from local build
	@echo ""
	@echo "📦 Installing LinuxIO from local build..."
	@sudo "$(packaging_scripts_dir)/localinstall.sh" --build-dir "$(bin_dir)"

reinstall: fastbuild localinstall ## Fastbuild + install (no uninstall)
	@echo ""
	@echo "LinuxIO reinstalled successfully!"
	@echo "  WARNING: Quick & dirty build - no tests executed!"

uninstall: ## Remove LinuxIO installation
	@echo ""
	@echo "🗑️  Uninstalling LinuxIO..."
	@sudo "$(packaging_scripts_dir)/uninstall.sh"

##@ Run / Clean|red
clean: ## Remove binaries, node_modules, and generated assets
	@for bin in $(shipped_binaries); do rm -f "$(bin_dir)/$$bin"; done
	@rm -f "$(VITE_DEV_PID)" "$(VITE_DEV_LOG)" "$(frontend_dir)/tsconfig.tsbuildinfo" || true
	@rm -rf "$(cache_dir)" "$(frontend_node_modules_dir)" || true
	@find "$(backend_frontend_dir)" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
	@rmdir "$(bin_dir)" 2>/dev/null || true
	@echo "🧹 Cleaned workspace."

cloc: ## Count handwritten source lines
	@echo "====>   Handwritten LOC    <===="
	@"$(scripts_dir)/loc.sh" . "$(LOC_INCLUDE_EXT)" 1 "$(backend_frontend_rel_dir)"
