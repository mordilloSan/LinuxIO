# The staged file tree the DEB and the RPM are built from. The layout lives in
# packaging/scripts/package-root.sh; this target builds nothing.

##@ Build|yellow
package-root: ## Stage the package file tree from @BIN@ into DESTDIR=<empty folder>
	@if [ -z "$(DESTDIR)" ]; then \
		echo "❌ DESTDIR is required: make package-root DESTDIR=<empty folder>"; \
		exit 1; \
	fi
	@"$(packaging_scripts_dir)/package-root.sh" --build-dir "$(bin_dir)" --destdir "$(DESTDIR)"
