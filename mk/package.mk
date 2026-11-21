# The DEB and the RPM, and the file tree they share. The layout lives in
# packaging/scripts/package-root.sh, the packages in package-deb.sh and
# package-rpm.sh; these targets build no program, so run a build first.

# One version for both packages, from git (packaging/scripts/package-version.sh):
# X.Y.Z on a commit tagged vX.Y.Z, X.Y.Z+devN.gSHA after it. PACKAGE_VERSION=...
# on the command line gives another, such as a higher one to upgrade to.
ifeq ($(origin PACKAGE_VERSION),undefined)
PACKAGE_VERSION := $(shell "$(packaging_scripts_dir)/package-version.sh" "$(repo_root)" 2>/dev/null)
endif
packages_dir := $(if $(PACKAGES_DIR),$(PACKAGES_DIR),$(bin_dir)/packages)
PACKAGES_DIR ?= $(packages_dir)

##@ Build|yellow
package-root: ## Stage the package file tree from @BIN@ into DESTDIR=<empty folder>
	@if [ -z "$(DESTDIR)" ]; then \
		echo "❌ DESTDIR is required: make package-root DESTDIR=<empty folder>"; \
		exit 1; \
	fi
	@"$(packaging_scripts_dir)/package-root.sh" --build-dir "$(bin_dir)" --destdir "$(DESTDIR)"

package-deb: ## Assemble the DEB from @BIN@ into PACKAGES_DIR (needs debhelper)
	@"$(packaging_scripts_dir)/package-deb.sh" --build-dir "$(bin_dir)" --version "$(PACKAGE_VERSION)" --out-dir "$(packages_dir)"

package-rpm: ## Assemble the RPM from @BIN@ into PACKAGES_DIR (needs rpm-build)
	@"$(packaging_scripts_dir)/package-rpm.sh" --build-dir "$(bin_dir)" --version "$(PACKAGE_VERSION)" --out-dir "$(packages_dir)"
