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
# The release notes the packages' changelogs carry (Markdown, the release PR's
# body); the release workflow passes them. Without them the changelog names
# the version alone.
RELEASE_NOTES ?=
package_notes = $(if $(strip $(RELEASE_NOTES)),--notes "$(RELEASE_NOTES)")

##@ Build|yellow
package-root: ## Stage the package file tree from @BIN@ into DESTDIR=<empty folder>
	@if [ -z "$(DESTDIR)" ]; then \
		echo "❌ DESTDIR is required: make package-root DESTDIR=<empty folder>"; \
		exit 1; \
	fi
	@"$(packaging_scripts_dir)/package-root.sh" --build-dir "$(bin_dir)" --destdir "$(DESTDIR)"

package-deb: ## Assemble the DEB from @BIN@ into PACKAGES_DIR (needs debhelper; RELEASE_NOTES for its changelog)
	@"$(packaging_scripts_dir)/package-deb.sh" --build-dir "$(bin_dir)" --version "$(PACKAGE_VERSION)" --out-dir "$(packages_dir)" $(package_notes)

package-rpm: ## Assemble the RPM from @BIN@ into PACKAGES_DIR (needs rpm-build; RELEASE_NOTES for its changelog)
	@"$(packaging_scripts_dir)/package-rpm.sh" --build-dir "$(bin_dir)" --version "$(PACKAGE_VERSION)" --out-dir "$(packages_dir)" $(package_notes)

# The signed repositories a release publishes (docs/releases.md,
# "Repositories"), from the packages in PACKAGES_DIR:
# REPO_DIR/apt and REPO_DIR/rpm, each kept across runs so a new version joins
# the ones already there. REPO_SIGNING_KEY names a key already in the GnuPG
# keyring (GNUPGHOME); the scripts never create or import one. REPO_BASE_URL is
# where REPO_DIR is published, without the apt/ or rpm/ that each adds.
REPO_DIR ?= $(bin_dir)/repo
REPO_SIGNING_KEY ?=
REPO_BASE_URL ?=
repo_check = $(if $(strip $(REPO_SIGNING_KEY)),,$(error REPO_SIGNING_KEY is required: a key ID in the GnuPG keyring))$(if $(strip $(REPO_BASE_URL)),,$(error REPO_BASE_URL is required: the published address of REPO_DIR))

repo-apt: ## Add the DEBs in PACKAGES_DIR to the signed APT repository REPO_DIR/apt (REPO_SIGNING_KEY, REPO_BASE_URL)
	@$(repo_check)
	@"$(packaging_scripts_dir)/repo-apt.sh" --packages-dir "$(packages_dir)" --key "$(REPO_SIGNING_KEY)" --base-url "$(REPO_BASE_URL)/apt" --out-dir "$(REPO_DIR)/apt"

repo-rpm: ## Add the RPMs in PACKAGES_DIR to the signed RPM repository REPO_DIR/rpm (REPO_SIGNING_KEY, REPO_BASE_URL)
	@$(repo_check)
	@"$(packaging_scripts_dir)/repo-rpm.sh" --packages-dir "$(packages_dir)" --key "$(REPO_SIGNING_KEY)" --base-url "$(REPO_BASE_URL)/rpm" --out-dir "$(REPO_DIR)/rpm"

##@ Tests|green
# Every DEB and RPM in PACKAGES_DIR against the build in BIN_DIR it was
# assembled from (package-contents.sh); reading an RPM needs rpm, a DEB
# dpkg-deb.
test-packages: ## Check the DEB and RPM in PACKAGES_DIR against the build they were assembled from
	@set -e; found=0; \
	for package in "$(packages_dir)"/linuxio_*.deb "$(packages_dir)"/linuxio-*.rpm; do \
		[ -e "$$package" ] || continue; \
		found=1; \
		bash "$(packaging_scripts_dir)/tests/package-contents.sh" "$$package" "$(bin_dir)"; \
	done; \
	[ "$$found" = 1 ] || { echo "❌ no linuxio package in $(packages_dir): run make package-deb or package-rpm" >&2; exit 1; }

# The quiet form, as the top-level Makefile makes its own (quiet_aliases).
quiet_aliases += test-packages-quiet

.PHONY: repo-apt repo-rpm test-packages
