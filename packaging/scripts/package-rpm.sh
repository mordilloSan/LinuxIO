#!/usr/bin/env bash
# Assemble the RPM of LinuxIO from programs already built: rpmbuild in a
# temporary top folder, the file tree from package-root.sh, the metadata and
# scriptlets from packaging/rpm/linuxio.spec. It reads only packaging/,
# LICENSE beside it and the build's THIRD_PARTY_NOTICES.md (the test VM has
# nothing else) and runs as the build user. The path of the package is the one line it
# prints on standard output.
#
# NOTES is the release notes file of a release (make RELEASE_NOTES=...), which
# the package's changelog carries (changelog-entry.sh); without it the
# changelog names the version alone.
#
# Usage: package-rpm.sh --build-dir DIR --version VERSION --out-dir DIR [--notes NOTES]
set -euo pipefail
umask 022

packaging_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_dir="$(dirname "$packaging_dir")"

die() {
	echo "package-rpm: $*" >&2
	exit 1
}

build_dir=""
version=""
out_dir=""
notes=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--build-dir | --version | --out-dir | --notes)
		[[ $# -ge 2 && -n $2 ]] || die "$1 needs a value"
		case "$1" in
		--build-dir) build_dir="$2" ;;
		--version) version="$2" ;;
		--out-dir) out_dir="$2" ;;
		--notes) notes="$2" ;;
		esac
		shift 2
		;;
	*) die "unknown argument ${1} (usage: package-rpm.sh --build-dir DIR --version VERSION --out-dir DIR [--notes NOTES])" ;;
	esac
done
[[ -n $build_dir && -n $version && -n $out_dir ]] ||
	die "usage: package-rpm.sh --build-dir DIR --version VERSION --out-dir DIR [--notes NOTES]"
[[ $version =~ ^[0-9][A-Za-z0-9.+~]*$ ]] ||
	die "version '${version}' is not a package version (digits first, then letters, digits, '.', '+' or '~')"
[[ -d $build_dir ]] || die "build folder ${build_dir} does not exist"
[[ -z $notes || -f $notes ]] || die "release notes ${notes} do not exist"
command -v rpmbuild >/dev/null || die "rpmbuild is missing: install rpm-build (sudo dnf install rpm-build)"
# The spec's scriptlets are these macros; rpmbuild would leave an undefined
# one in them as text.
for macro in tmpfiles_create:systemd-rpm-macros selinux_modules_install:selinux-policy; do
	[[ "$(rpm --eval "%{?${macro%%:*}:yes}")" == yes ]] ||
		die "the rpm macro %${macro%%:*} is undefined: install ${macro#*:}"
done
[[ -f "${source_dir}/LICENSE" ]] || die "missing ${source_dir}/LICENSE"
# Written beside the programs by the build, from what they hold.
[[ -f "${build_dir}/THIRD_PARTY_NOTICES.md" ]] ||
	die "missing ${build_dir}/THIRD_PARTY_NOTICES.md: make build-binaries writes it (or make third-party-notices)"
build_dir="$(realpath -- "$build_dir")"
mkdir -p "$out_dir"
out_dir="$(realpath -- "$out_dir")"

top="$(mktemp -d)"
trap 'rm -rf "$top"' EXIT
"${packaging_dir}/scripts/changelog-entry.sh" rpm --version "$version" ${notes:+--notes "$notes"} >"${top}/changelog"
rpmbuild -bb \
	--define "_topdir ${top}" \
	--define "linuxio_version ${version}" \
	--define "linuxio_changelog ${top}/changelog" \
	--define "linuxio_build_dir ${build_dir}" \
	--define "linuxio_packaging_dir ${packaging_dir}" \
	--define "linuxio_source_dir ${source_dir}" \
	"${packaging_dir}/rpm/linuxio.spec" >&2 || die "rpmbuild failed"

shopt -s nullglob
rpms=("${top}"/RPMS/*/linuxio-"${version}"-*.rpm)
[[ ${#rpms[@]} -eq 1 ]] || die "rpmbuild made no linuxio-${version}-*.rpm"
mv -f "${rpms[0]}" "$out_dir/"
echo "${out_dir}/$(basename "${rpms[0]}")"
