#!/usr/bin/env bash
# shellcheck disable=SC2016
# Fixture-only checks for package-root.sh, the layout the DEB and the RPM are
# built from: the staged tree is exactly the manifest below, with its modes,
# and agrees with the Makefile, packaging/systemd and the installer. Fake
# programs stand in for the build; nothing is installed on the host.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
PACKAGE_ROOT="${SCRIPT_DIR}/../package-root.sh"
INSTALLER="${SCRIPT_DIR}/../install-linuxio-binaries.sh"

fail() {
	echo "❌ $*" >&2
	exit 1
}

assert_eq() {
	local expected="$1"
	local actual="$2"
	local label="$3"

	[[ "$actual" == "$expected" ]] || fail "${label}: expected ${expected}, got ${actual}"
}

pass() {
	printf '   \033[1;32m✓\033[0m %s\n' "$1"
}

# What package-root.sh stages, apart from the units, which are every unit
# file in packaging/systemd (checked on their own below).
expected_tree="\
755 etc
755 etc/avahi
755 etc/avahi/services
644 etc/avahi/services/linuxio.service
755 etc/libvirt
755 etc/libvirt/hooks
755 etc/libvirt/hooks/qemu.d
755 etc/libvirt/hooks/qemu.d/linuxio-bridge-netfilter
755 etc/linuxio
644 etc/linuxio/disallowed-users
755 etc/linuxio/indexer
644 etc/linuxio/indexer/config.yaml
755 etc/linuxio/monitoring
644 etc/linuxio/monitoring/config.yaml
755 usr
755 usr/bin
755 usr/bin/linuxio
755 usr/lib
755 usr/lib/systemd
755 usr/lib/systemd/system
755 usr/lib/systemd/system/apt-daily-upgrade.service.d
644 usr/lib/systemd/system/apt-daily-upgrade.service.d/10-linuxio-run.conf
755 usr/lib/systemd/system/dnf-automatic.service.d
644 usr/lib/systemd/system/dnf-automatic.service.d/10-linuxio-run.conf
755 usr/lib/systemd/system/dnf5-automatic.service.d
644 usr/lib/systemd/system/dnf5-automatic.service.d/10-linuxio-run.conf
755 usr/lib/systemd/system/linuxio-btrfs-replicate.service.d
644 usr/lib/systemd/system/linuxio-btrfs-replicate.service.d/10-linuxio-run.conf
755 usr/lib/systemd/system/linuxio-btrfs-snapshot.service.d
644 usr/lib/systemd/system/linuxio-btrfs-snapshot.service.d/10-linuxio-run.conf
755 usr/lib/systemd/system/linuxio-docker-update.service.d
644 usr/lib/systemd/system/linuxio-docker-update.service.d/10-linuxio-run.conf
755 usr/lib/systemd/system/linuxio-schedule-.service.d
644 usr/lib/systemd/system/linuxio-schedule-.service.d/10-linuxio-run.conf
755 usr/lib/systemd/system/mintupdate-automation-upgrade.service.d
644 usr/lib/systemd/system/mintupdate-automation-upgrade.service.d/10-linuxio-run.conf
755 usr/lib/tmpfiles.d
644 usr/lib/tmpfiles.d/linuxio.conf
755 usr/libexec
755 usr/libexec/linuxio
700 usr/libexec/linuxio/linuxio-auth
755 usr/libexec/linuxio/linuxio-bridge
755 usr/libexec/linuxio/linuxio-claude-relay
700 usr/libexec/linuxio/linuxio-docker-update
700 usr/libexec/linuxio/linuxio-indexer
700 usr/libexec/linuxio/linuxio-monitoring
755 usr/libexec/linuxio/linuxio-webserver
700 usr/libexec/linuxio/linuxio-worker
755 usr/share
755 usr/share/linuxio
755 usr/share/linuxio/issue
755 usr/share/linuxio/issue/update-issue
755 usr/share/linuxio/libvirt
644 usr/share/linuxio/libvirt/linuxio-bridge-netfilter"

# The nine programs of a build.
programs="\
linuxio
linuxio-webserver
linuxio-bridge
linuxio-auth
linuxio-indexer
linuxio-monitoring
linuxio-worker
linuxio-docker-update
linuxio-claude-relay"

fixture_dir=$(mktemp -d)
trap 'rm -rf "$fixture_dir"' EXIT

# Fake programs: package-root.sh copies them and runs none.
bin="${fixture_dir}/build"
mkdir -p "$bin"
while IFS= read -r name; do
	printf '#!/bin/sh\nexit 0\n' >"${bin}/${name}"
done <<<"$programs"
chmod 0755 "${bin}"/*

tree="${fixture_dir}/root"
bash "$PACKAGE_ROOT" --build-dir "$bin" --destdir "$tree" >/dev/null ||
	fail "package-root.sh failed on a complete build"

# 1. The tree is exactly the manifest: every path and mode, nothing else.
packaged_units=$(cd "${REPO_ROOT}/packaging/systemd" && printf '%s\n' * | grep -vxE 'linuxio-tmpfiles.conf|linuxio-run-record.conf')
expected=$({
	printf '%s\n' "$expected_tree"
	awk '{ print "644 usr/lib/systemd/system/" $0 }' <<<"$packaged_units"
} | LC_ALL=C sort)
actual=$(cd "$tree" && find . -mindepth 1 -printf '%m %P\n' | LC_ALL=C sort)
if [[ "$actual" != "$expected" ]]; then
	diff <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >&2 || true
	fail "staged tree differs from the manifest (< expected, > staged)"
fi
pass "staged tree matches the manifest, paths and modes"

# 2. The nine programs are the Makefile's, at the installer's modes.
staged_programs=$(cd "$tree" && find usr/bin usr/libexec/linuxio -type f -printf '%f\n' | LC_ALL=C sort)
make_binaries=$(make -s -C "$REPO_ROOT" print-binaries | LC_ALL=C sort)
assert_eq "$make_binaries" "$staged_programs" "staged programs vs make print-binaries"
if [[ -f "$INSTALLER" ]]; then
	installer_modes=$(bash -c "source '$INSTALLER'; for b in \$(binary_names); do printf '%s %s\n' \"\$(binary_mode \"\$b\")\" \"\$b\"; done" | LC_ALL=C sort -k2)
	staged_modes=$(cd "$tree" && find usr/bin usr/libexec/linuxio -type f -printf '0%m %f\n' | LC_ALL=C sort -k2)
	assert_eq "$installer_modes" "$staged_modes" "program modes vs the installer's binary_mode"
fi
pass "programs match make print-binaries and the installer's modes"

# 3. The units are packaging/systemd's, the drop-in folders the installer's.
staged_units=$(cd "${tree}/usr/lib/systemd/system" && find . -maxdepth 1 -type f -printf '%f\n' | LC_ALL=C sort)
assert_eq "$(printf '%s\n' "$packaged_units" | LC_ALL=C sort)" "$staged_units" "staged units vs packaging/systemd"
staged_dropin_dirs=$(cd "${tree}/usr/lib/systemd/system" && find . -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | LC_ALL=C sort)
if [[ -f "$INSTALLER" ]]; then
	installer_dropin_dirs=$(bash -c "source '$INSTALLER'; run_record_dropin_dirs" | LC_ALL=C sort)
	assert_eq "$installer_dropin_dirs" "$staged_dropin_dirs" "drop-in folders vs the installer's run_record_dropin_dirs"
fi
for dir in $staged_dropin_dirs; do
	cmp -s "${REPO_ROOT}/packaging/systemd/linuxio-run-record.conf" "${tree}/usr/lib/systemd/system/${dir}/10-linuxio-run.conf" ||
		fail "${dir}/10-linuxio-run.conf is not packaging/systemd/linuxio-run-record.conf"
done
pass "units match packaging/systemd, drop-in folders the installer's"

# 4. Every LinuxIO program a unit or drop-in starts is in the tree, and no
# staged file names the script installer's /usr/local/bin.
exec_paths=$(cd "${tree}/usr/lib/systemd/system" && find . -type f -exec grep -hE '^Exec[A-Za-z]*=' {} + |
	grep -oE '/usr/(libexec/linuxio/[^[:space:]]+|bin/linuxio[^[:space:]]*|share/linuxio/[^[:space:]]+)' | LC_ALL=C sort -u || true)
for path in $exec_paths; do
	[[ -f "${tree}${path}" ]] || fail "a staged unit starts ${path}, which is not in the tree"
done
old_paths=$(grep -rlF /usr/local/bin "$tree" | sed "s#^${tree}/##" || true)
[[ -z "$old_paths" ]] || fail "staged files still name /usr/local/bin: ${old_paths//$'\n'/ }"
pass "every program a unit starts is staged, and nothing names /usr/local/bin"

# 5. Refusals, each before anything is staged.
refuses() {
	local label="$1"
	local dest="$2"
	shift 2
	if bash "$PACKAGE_ROOT" --destdir "$dest" "$@" >/dev/null 2>&1; then
		fail "package-root.sh accepted ${label}"
	fi
}
refuses "an empty destdir" "" --build-dir "$bin"
refuses "/" / --build-dir "$bin"
refuses "a destdir that resolves to /" /. --build-dir "$bin"
refuses "a non-empty destdir" "$tree" --build-dir "$bin"
cp -a "$bin" "${fixture_dir}/partial"
rm "${fixture_dir}/partial/linuxio-worker"
refuses "a build without linuxio-worker" "${fixture_dir}/out-missing" --build-dir "${fixture_dir}/partial"
[[ ! -e "${fixture_dir}/out-missing" ]] || fail "a refused run left ${fixture_dir}/out-missing behind"
printf '#!/bin/sh\nexit 0\n' >"${fixture_dir}/partial/linuxio-worker"
chmod 0644 "${fixture_dir}/partial/linuxio-worker"
refuses "a linuxio-worker that is not executable" "${fixture_dir}/out-noexec" --build-dir "${fixture_dir}/partial"
[[ ! -e "${fixture_dir}/out-noexec" ]] || fail "a refused run left ${fixture_dir}/out-noexec behind"
pass "refuses an empty, root or non-empty destdir and an incomplete build"

printf '\033[1;32m%s\033[0m\n' "✅ package-root fixtures passed!"
