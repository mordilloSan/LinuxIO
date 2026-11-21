#!/usr/bin/env bash
# Lay out the file tree the DEB and the RPM are both built from: the nine
# programs from the build folder, and the units, configuration and scripts
# from packaging/, each at its installed path and mode under DESTDIR. It runs
# as the build user: ownership is the package tools' to set. What differs
# between the two families (the PAM file, the SELinux module, the needrestart
# override, the login banner's links, which the post-install step makes, the
# licences) is added by each package.
#
# Usage: package-root.sh --build-dir DIR --destdir DIR
set -euo pipefail
umask 022

packaging_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Programs run as the signed-in user, a dynamic user, or the administrator.
public_programs=(linuxio-webserver linuxio-bridge linuxio-claude-relay)
# Programs systemd runs as root: no other user may start them.
root_programs=(linuxio-auth linuxio-docker-update linuxio-indexer linuxio-monitoring linuxio-worker)
# Folders under usr/lib/systemd/system that get the run-record drop-in, as the
# installer's run_record_dropin_dirs.
run_record_dropin_dirs=(
	linuxio-schedule-.service.d
	linuxio-btrfs-snapshot.service.d
	linuxio-btrfs-replicate.service.d
	linuxio-docker-update.service.d
	apt-daily-upgrade.service.d
	dnf-automatic.service.d
	dnf5-automatic.service.d
	mintupdate-automation-upgrade.service.d
)
# "source under packaging/  path under DESTDIR  mode"
files=(
	"systemd/linuxio-tmpfiles.conf usr/lib/tmpfiles.d/linuxio.conf 0644"
	"scripts/update-issue usr/share/linuxio/issue/update-issue 0755"
	"etc/linuxio/disallowed-users etc/linuxio/disallowed-users 0644"
	"etc/linuxio/indexer/config.yaml etc/linuxio/indexer/config.yaml 0644"
	"etc/linuxio/monitoring/config.yaml etc/linuxio/monitoring/config.yaml 0644"
	"etc/avahi/services/linuxio.service etc/avahi/services/linuxio.service 0644"
	"libvirt/hooks/qemu.d/linuxio-bridge-netfilter etc/libvirt/hooks/qemu.d/linuxio-bridge-netfilter 0755"
	# The untouched copy the bridge restores the hook from (network.restore_bridge_hook).
	"libvirt/hooks/qemu.d/linuxio-bridge-netfilter usr/share/linuxio/libvirt/linuxio-bridge-netfilter 0644"
)

die() {
	echo "package-root: $*" >&2
	exit 1
}

# stage SRC DEST MODE: install SRC at DEST under the tree with MODE.
stage() {
	[[ -f "$1" ]] || die "missing ${1}"
	install -D -m "$3" "$1" "${destdir}/${2}"
}

build_dir=""
destdir=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--build-dir)
		build_dir="${2:-}"
		shift 2 || die "--build-dir needs a folder"
		;;
	--destdir)
		destdir="${2:-}"
		shift 2 || die "--destdir needs a folder"
		;;
	*) die "unknown argument ${1} (usage: package-root.sh --build-dir DIR --destdir DIR)" ;;
	esac
done

[[ -n "$build_dir" ]] || die "--build-dir is required"
[[ -d "$build_dir" ]] || die "build folder ${build_dir} does not exist"
[[ -n "$destdir" ]] || die "--destdir is required and must not be empty"
destdir="$(realpath -m -- "$destdir")" || die "cannot resolve ${destdir}"
[[ "$destdir" == /* ]] || die "${destdir} does not resolve to an absolute path"
[[ "$destdir" != / ]] || die "refusing to stage into /"
if [[ -e "$destdir" ]]; then
	[[ -d "$destdir" ]] || die "${destdir} is not a folder"
	[[ -z "$(find "$destdir" -mindepth 1 -maxdepth 1 -print -quit)" ]] || die "${destdir} is not empty"
fi

for name in linuxio "${public_programs[@]}" "${root_programs[@]}"; do
	[[ -f "${build_dir}/${name}" && -x "${build_dir}/${name}" ]] ||
		die "${build_dir}/${name} is missing or not executable (build it first: make build-binaries)"
done

mkdir -p "$destdir"

stage "${build_dir}/linuxio" usr/bin/linuxio 0755
for name in "${public_programs[@]}"; do
	stage "${build_dir}/${name}" "usr/libexec/linuxio/${name}" 0755
done
for name in "${root_programs[@]}"; do
	stage "${build_dir}/${name}" "usr/libexec/linuxio/${name}" 0700
done

units=()
for unit in "${packaging_dir}"/systemd/*.{service,socket,target,timer}; do
	if [[ -e "$unit" ]]; then
		units+=("$unit")
	fi
done
[[ ${#units[@]} -gt 0 ]] || die "no units in ${packaging_dir}/systemd"
for unit in "${units[@]}"; do
	stage "$unit" "usr/lib/systemd/system/${unit##*/}" 0644
done

for dir in "${run_record_dropin_dirs[@]}"; do
	stage "${packaging_dir}/systemd/linuxio-run-record.conf" "usr/lib/systemd/system/${dir}/10-linuxio-run.conf" 0644
done

for entry in "${files[@]}"; do
	read -r src dest mode <<<"$entry"
	stage "${packaging_dir}/${src}" "$dest" "$mode"
done

echo "Staged the package tree in ${destdir}"
