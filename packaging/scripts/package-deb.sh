#!/usr/bin/env bash
# Assemble the DEB of LinuxIO from programs already built: debhelper in a
# temporary folder, the file tree from package-root.sh, the metadata and
# maintainer scripts from packaging/debian. It reads only packaging/, LICENSE
# beside it and the build's THIRD_PARTY_NOTICES.md (the test VM has nothing
# else) and runs as the build user; dpkg-deb gives every file to root. The path of
# the package is the one line it prints on standard output.
#
# Usage: package-deb.sh --build-dir DIR --version VERSION --out-dir DIR
set -euo pipefail
umask 022

packaging_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_dir="$(dirname "$packaging_dir")"

die() {
	echo "package-deb: $*" >&2
	exit 1
}

build_dir=""
version=""
out_dir=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--build-dir | --version | --out-dir)
		[[ $# -ge 2 && -n $2 ]] || die "$1 needs a value"
		case "$1" in
		--build-dir) build_dir="$2" ;;
		--version) version="$2" ;;
		--out-dir) out_dir="$2" ;;
		esac
		shift 2
		;;
	*) die "unknown argument ${1} (usage: package-deb.sh --build-dir DIR --version VERSION --out-dir DIR)" ;;
	esac
done
[[ -n $build_dir && -n $version && -n $out_dir ]] ||
	die "usage: package-deb.sh --build-dir DIR --version VERSION --out-dir DIR"
[[ $version =~ ^[0-9][A-Za-z0-9.+~]*$ ]] ||
	die "version '${version}' is not a package version (digits first, then letters, digits, '.', '+' or '~')"
[[ -d $build_dir ]] || die "build folder ${build_dir} does not exist"
for tool in dpkg-buildpackage dh; do
	command -v "$tool" >/dev/null ||
		die "${tool} is missing: install debhelper (sudo apt-get install debhelper)"
done
[[ -f "${source_dir}/LICENSE" ]] || die "missing ${source_dir}/LICENSE"
# Written beside the programs by the build, from what they hold.
[[ -f "${build_dir}/THIRD_PARTY_NOTICES.md" ]] ||
	die "missing ${build_dir}/THIRD_PARTY_NOTICES.md: make build-binaries writes it (or make third-party-notices)"
build_dir="$(realpath -- "$build_dir")"
mkdir -p "$out_dir"
out_dir="$(realpath -- "$out_dir")"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
src="${work}/linuxio-${version}"
mkdir -p "$src"
cp -r "${packaging_dir}/debian" "${src}/debian"
cp "${source_dir}/LICENSE" "${build_dir}/THIRD_PARTY_NOTICES.md" "$src/"
cat >"${src}/debian/changelog" <<EOF
linuxio (${version}) unstable; urgency=medium

  * LinuxIO ${version}.

 -- LinuxIO packages <packages@engmariz.com>  $(date -R)
EOF

(
	cd "$src"
	LINUXIO_BUILD_DIR="$build_dir" LINUXIO_PACKAGING_DIR="$packaging_dir" \
		dpkg-buildpackage --build=binary --unsigned-source --unsigned-changes --no-pre-clean --no-check-builddeps >&2
) || die "dpkg-buildpackage failed"

shopt -s nullglob
debs=("${work}"/linuxio_"${version}"_*.deb)
[[ ${#debs[@]} -eq 1 ]] || die "dpkg-buildpackage made no linuxio_${version}_*.deb"
mv -f "${debs[0]}" "$out_dir/"
echo "${out_dir}/$(basename "${debs[0]}")"
