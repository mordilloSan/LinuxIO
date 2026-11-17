#!/usr/bin/env bash
# Disposable LinuxIO test VM on qemu:///system, driven by mk/vm.mk
# (docs/development.md, "Test VM"). It is hard-wired to the domain linuxio-e2e
# and its linuxio-e2e-* volumes in the default pool, and never lists, changes
# or deletes any other domain, volume or network. State (image cache, SSH key,
# login) lives in ~/.cache/linuxio-vm.
#
# LINUXIO_VM_RELEASE=noble selects a second VM on Ubuntu 24.04 for the root
# tests on an older kernel: the domain linuxio-e2e-noble, its
# linuxio-e2e-noble-* volumes and ~/.cache/linuxio-vm-noble, beside the first.
#
# LINUXIO_VM_INDEX=2..9 selects one more VM of the same release, so the browser
# specs run on several at once: the domain linuxio-e2e-2, its linuxio-e2e-2-*
# volumes and ~/.cache/linuxio-vm-2. It shares only the image cache with the
# first.
#
# Usage: linuxio-vm.sh create | ensure | prepare [BUILD_DIR] |
#        install BUILD_DIR | start | ssh [COMMAND] | ip | password | reboot |
#        power-cut | off-for SECONDS | throttle DISK BYTES_PER_SEC | stop |
#        reset | update | destroy | fit | room | count | test DIR [SUITE...] |
#        each COUNT COMMAND [ARGS]
set -euo pipefail

case "${LINUXIO_VM_RELEASE:-resolute}" in
resolute) readonly RELEASE=resolute VERSION=26.04 FIRST_DOMAIN=linuxio-e2e ;;
noble) readonly RELEASE=noble VERSION=24.04 FIRST_DOMAIN=linuxio-e2e-noble ;;
*)
	echo "linuxio-vm: unknown LINUXIO_VM_RELEASE '${LINUXIO_VM_RELEASE}' (resolute or noble)" >&2
	exit 1
	;;
esac
readonly INDEX="${LINUXIO_VM_INDEX:-1}"
if [[ ! $INDEX =~ ^[1-9]$ ]]; then
	echo "linuxio-vm: LINUXIO_VM_INDEX '${INDEX}' is not a number from 1 to 9" >&2
	exit 1
fi

# The domain of the VM numbered $1: the first has no number in its name.
domain_name() {
	if (($1 == 1)); then echo "$FIRST_DOMAIN"; else echo "${FIRST_DOMAIN}-$1"; fi
}

DOMAIN="$(domain_name "$INDEX")"
readonly DOMAIN
readonly VM_USER=linuxio-e2e
readonly VM_MEMORY_GIB=4 VM_VCPUS=4
# What the host keeps when VMs are counted against its free memory: the
# browsers of a run and whatever else grows while it lasts.
readonly HOST_RESERVE_MIB=1536
# The longest spec file takes a third of the time of all of them, so a fifth
# VM would only cost its disk and its first boot.
readonly MOST_VMS=4
readonly SNAPSHOT=clean
readonly POOL=default
readonly BASE_VOL="${DOMAIN}-base.qcow2"
readonly SYSTEM_VOL="${DOMAIN}-system.qcow2"
readonly DATA_VOLS=("${DOMAIN}-data1.qcow2" "${DOMAIN}-data2.qcow2" "${DOMAIN}-data3.qcow2" "${DOMAIN}-data4.qcow2")
readonly HOST_IP=192.168.122.1
# One port for each VM number, so several first boots can run at once.
readonly SEED_PORT=$((8398 + INDEX))
readonly SEED_URL="http://${HOST_IP}:${SEED_PORT}/"
readonly IMAGE_BASE_URL="https://cloud-images.ubuntu.com/releases/${RELEASE}/release"
readonly IMAGE_NAME="ubuntu-${VERSION}-server-cloudimg-amd64.img"
readonly STATE="${HOME}/.cache/linuxio-vm${DOMAIN#linuxio-e2e}"
readonly IMAGE_DIR="${HOME}/.cache/linuxio-vm${FIRST_DOMAIN#linuxio-e2e}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly REPO_ROOT
# Where `each` keeps the output of every VM, one file per VM and command.
readonly LOG_DIR="${LINUXIO_VM_LOG_DIR:-${REPO_ROOT}/.cache/test-logs}"
readonly SSH_OPTS=(
	-i "${STATE}/id_ed25519" -o IdentitiesOnly=yes -o BatchMode=yes
	-o UserKnownHostsFile="${STATE}/known_hosts" -o StrictHostKeyChecking=accept-new
	-o ConnectTimeout=5 -o ServerAliveInterval=15 -o LogLevel=ERROR
)
SEED_PID=""
IP=""

say() { echo "linuxio-vm: $*" >&2; }

# A step of this VM, for `each` to show while it runs: "NAME run EPOCH" when it
# begins, "NAME ok SECONDS" when it ends. Without `each` nothing is written.
step_note() { [[ -z ${LINUXIO_VM_STATUS:-} ]] || echo "$*" >>"$LINUXIO_VM_STATUS"; }

# Runs FUNCTION [ARGS] as the step NAME. A failure exits the script as before;
# `each` then shows the step that was running as failed.
step() {
	local name=$1 begun=$SECONDS
	shift
	step_note "$name run $EPOCHSECONDS"
	"$@"
	step_note "$name ok $((SECONDS - begun))"
}

die() {
	say "$*"
	exit 1
}

v() { virsh -c qemu:///system "$@"; }

# Only virsh's own "failed to get domain" means there is none: any other error
# stops the script, since ensure deletes and creates again what it misses.
domain_exists() {
	local name=${1:-$DOMAIN} err
	err="$(v dominfo "$name" 2>&1 >/dev/null)" && return 0
	[[ $err == *"failed to get domain"* ]] && return 1
	die "cannot look up ${name}: ${err}"
}

domain_state() { v domstate "$DOMAIN" 2>/dev/null; }

vol_exists() { v vol-info --pool "$POOL" "$1" >/dev/null 2>&1; }

# shellcheck disable=SC2029 # the caller's command is meant for the VM's shell
vm_ssh() { ssh "${SSH_OPTS[@]}" "${VM_USER}@${IP}" "$@"; }

# The addresses the default network's DHCP server leased to the domain, the
# one that last answered first. A boot whose initrd also took a lease (seen
# after a power cut mid device replace) leaves a dead address listed.
lease_ips() {
	local last=""
	[[ -f "${STATE}/ip" ]] && last="$(<"${STATE}/ip")"
	v domifaddr "$DOMAIN" | awk -v last="$last" '$3 == "ipv4" {
		sub(/\/.*/, "", $4)
		if ($4 == last) print $4; else rest = rest $4 "\n"
	} END { printf "%s", rest }'
}

# Sets IP to the leased address that answers SSH.
wait_for_ssh() {
	local deadline=$((SECONDS + $1))
	while true; do
		for IP in $(lease_ips); do
			if vm_ssh true </dev/null 2>/dev/null; then
				echo "$IP" >"${STATE}/ip"
				return
			fi
		done
		((SECONDS < deadline)) || die "no SSH to ${DOMAIN} within $1 seconds"
		sleep 3
	done
}

wait_for_shut_off() {
	local deadline=$((SECONDS + $1))
	until [[ "$(domain_state)" == "shut off" ]]; do
		((SECONDS < deadline)) || die "${DOMAIN} did not shut down within $1 seconds"
		sleep 2
	done
}

# Start the domain if needed and wait until it answers SSH; sets IP.
connect() {
	domain_exists || die "${DOMAIN} does not exist; run make vm-create"
	if [[ "$(domain_state)" != running ]]; then
		say "starting ${DOMAIN}"
		v start "$DOMAIN" >/dev/null
	fi
	wait_for_ssh 300
}

stop_seed() {
	if [[ -n $SEED_PID ]]; then
		kill "$SEED_PID" 2>/dev/null || true
		wait "$SEED_PID" 2>/dev/null || true
		SEED_PID=""
	fi
}

# The cloud image, checked against the release's SHA256SUMS; a cached copy is
# reused while it still matches. The VMs of a release share the copy, so one
# create at a time looks at it.
fetch_image() {
	local sum image="${IMAGE_DIR}/${IMAGE_NAME}"
	mkdir -p "$IMAGE_DIR"
	chmod 700 "$IMAGE_DIR"
	exec 8>"${IMAGE_DIR}/image.lock"
	flock 8
	sum="$(curl -fsSL "${IMAGE_BASE_URL}/SHA256SUMS" | awk -v f="*${IMAGE_NAME}" '$2 == f { print $1 }')"
	[[ -n $sum ]] || die "${IMAGE_NAME} is not listed in ${IMAGE_BASE_URL}/SHA256SUMS"
	if echo "${sum}  ${image}" | sha256sum --check --status >/dev/null 2>&1; then
		exec 8>&-
		return
	fi
	say "downloading ${IMAGE_NAME}"
	curl -fL --progress-bar -o "${image}.part" "${IMAGE_BASE_URL}/${IMAGE_NAME}"
	echo "${sum}  ${image}.part" | sha256sum --check --status || die "${IMAGE_NAME} does not match SHA256SUMS"
	mv "${image}.part" "$image"
	exec 8>&-
}

write_seed() {
	local seed="${STATE}/seed" hash key
	hash="$(openssl passwd -6 -stdin <"${STATE}/password")"
	key="$(cat "${STATE}/id_ed25519.pub")"
	rm -rf "$seed"
	mkdir -p "$seed"
	printf 'instance-id: %s\nlocal-hostname: %s\n' "$DOMAIN" "$DOMAIN" >"${seed}/meta-data"
	sed -e "s|@PASSWORD_HASH@|${hash}|" -e "s|@SSH_KEY@|${key}|" -e "s|@SEED_URL@|${SEED_URL}|" \
		"${REPO_ROOT}/packaging/vm/user-data.yaml.in" >"${seed}/user-data"
	cp "${REPO_ROOT}/packaging/scripts/install-dependencies.sh" "${seed}/"
}

# cloud-init reads the seed over HTTP: the host has no ISO tools. The server
# runs only until the first boot finishes or this script exits.
start_seed() {
	python3 -m http.server --bind "$HOST_IP" --directory "${STATE}/seed" "$SEED_PORT" >"${STATE}/seed.log" 2>&1 &
	SEED_PID=$!
	local tries=0
	until curl -fsS -o /dev/null "${SEED_URL}meta-data" 2>/dev/null; do
		kill -0 "$SEED_PID" 2>/dev/null || die "seed server failed: $(cat "${STATE}/seed.log")"
		((++tries < 20)) || die "seed server did not answer on ${SEED_URL}"
		sleep 0.5
	done
}

define_domain() {
	local disks="" path i
	local -r vols=("$SYSTEM_VOL" "${DATA_VOLS[@]}") devs=(vda vdb vdc vdd vde)
	for i in "${!vols[@]}"; do
		path="$(v vol-path --pool "$POOL" "${vols[i]}")"
		disks+="<disk type='file' device='disk'><driver name='qemu' type='qcow2' discard='unmap'/><source file='${path}'/><target dev='${devs[i]}' bus='virtio'/></disk>"
	done
	v define /dev/stdin >/dev/null <<EOF
<domain type='kvm'>
  <name>${DOMAIN}</name>
  <memory unit='GiB'>${VM_MEMORY_GIB}</memory>
  <vcpu>${VM_VCPUS}</vcpu>
  <sysinfo type='smbios'>
    <system><entry name='serial'>ds=nocloud;s=${SEED_URL}</entry></system>
  </sysinfo>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    <boot dev='hd'/>
    <smbios mode='sysinfo'/>
  </os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough'/>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    ${disks}
    <interface type='network'><source network='default'/><model type='virtio'/></interface>
    <serial type='pty'/>
    <console type='pty'/>
    <rng model='virtio'><backend model='random'>/dev/urandom</backend></rng>
  </devices>
</domain>
EOF
}

create() {
	if domain_exists; then die "${DOMAIN} already exists; run make vm-destroy first"; fi
	for vol in "$BASE_VOL" "$SYSTEM_VOL" "${DATA_VOLS[@]}"; do
		if vol_exists "$vol"; then die "volume ${vol} already exists; run make vm-destroy first"; fi
	done
	mkdir -p "$STATE"
	chmod 700 "$STATE"
	fetch_image
	[[ -f "${STATE}/id_ed25519" ]] || ssh-keygen -q -t ed25519 -N '' -C linuxio-e2e-rig -f "${STATE}/id_ed25519"
	(umask 077 && head -c 18 /dev/urandom | base64 >"${STATE}/password")
	rm -f "${STATE}/known_hosts"
	write_seed

	say "uploading the base image to the ${POOL} pool"
	v vol-create-as "$POOL" "$BASE_VOL" "$(stat -c %s "${IMAGE_DIR}/${IMAGE_NAME}")" --format qcow2 >/dev/null
	v vol-upload --pool "$POOL" "$BASE_VOL" "${IMAGE_DIR}/${IMAGE_NAME}"
	v vol-create-as "$POOL" "$SYSTEM_VOL" 25G --format qcow2 --backing-vol "$BASE_VOL" --backing-vol-format qcow2 >/dev/null
	for vol in "${DATA_VOLS[@]}"; do
		v vol-create-as "$POOL" "$vol" 8G --format qcow2 >/dev/null
	done
	define_domain

	trap stop_seed EXIT
	trap 'exit 130' INT TERM
	start_seed
	say "first boot: cloud-init installs the packages (several minutes)"
	v start "$DOMAIN" >/dev/null
	wait_for_ssh 600
	if ! vm_ssh cloud-init status --wait >/dev/null; then
		vm_ssh 'cloud-init status --long; sudo tail -n 40 /var/log/cloud-init-output.log' >&2 || true
		die "cloud-init failed; inspect with make vm-ssh, then make vm-destroy"
	fi
	stop_seed

	# Later boots need no seed; the snapshot is taken shut off, so a revert
	# boots fresh with a correct clock.
	vm_ssh 'sudo touch /etc/cloud/cloud-init.disabled && sync'
	v shutdown "$DOMAIN" >/dev/null
	wait_for_shut_off 180
	v snapshot-create-as "$DOMAIN" "$SNAPSHOT" --description "cloud-init done, LinuxIO not installed" >/dev/null
	say "${DOMAIN} is ready (snapshot ${SNAPSHOT}); next: make vm-install"
}

# A VM ready to use: nothing to do when it has its clean snapshot. Otherwise
# what is left of it (volumes without a domain, a first boot that was cut)
# goes, and it is created.
ensure() {
	if has_clean_snapshot; then return; fi
	if domain_exists; then say "${DOMAIN} has no ${SNAPSHOT} snapshot, so it is created again"; fi
	destroy
	create
}

has_clean_snapshot() {
	local snapshots
	domain_exists || return 1
	snapshots="$(v snapshot-list "$DOMAIN" --name)" || die "cannot list the snapshots of ${DOMAIN}"
	grep -qx "$SNAPSHOT" <<<"$snapshots"
}

# What make test-vm-browser and test-vm-backend do to each VM before their
# tests: create it when it is missing or its first boot was cut, revert it to
# clean and, given BUILD_DIR, install that build. test-vm-backend gives none:
# its suites bring their own test binaries.
prepare() {
	local build_dir=${1:-}
	[[ -z $build_dir || -x "${build_dir}/linuxio" ]] || die "no LinuxIO build in '${build_dir}'"
	if has_clean_snapshot; then
		step_note "ready ok 0"
	else
		step created ensure
	fi
	step reset reset
	[[ -z $build_dir ]] || step install install "$build_dir"
}

install() {
	local build_dir=${1:-}
	[[ -n $build_dir && -x "${build_dir}/linuxio" ]] || die "no LinuxIO build in '${build_dir}'"
	connect
	push_checkout "$build_dir"
	vm_ssh 'sudo ~/linuxio/packaging/scripts/install-linuxio-binaries.sh --local --build-dir ~/linuxio/build'
}

# push_checkout BUILD_DIR puts what the installer reads in ~/linuxio and the
# build in ~/linuxio/build: --local reads packaging/, LICENSE and
# docs/THIRD_PARTY_NOTICES.md, and finds the checkout by its Makefile.
push_checkout() {
	tar -C "$REPO_ROOT" -cf - Makefile LICENSE docs/THIRD_PARTY_NOTICES.md packaging |
		vm_ssh 'rm -rf linuxio && mkdir -p linuxio/build && tar -C linuxio -xf -'
	tar -C "$1" -cf - . | vm_ssh 'tar -C linuxio/build -xf -'
}

# The root test suites of the vm-test-* targets and test-vm-backend, one
# suite_NAME function each; mk/vm.mk lists them. Make builds their test
# binaries on the host into DIR and passes the test name patterns it shares
# with the host targets; a suite copies its binaries into the VM and runs them
# as root. `test DIR SUITE...` runs the named suites in turn. `test DIR` takes
# them one at a time from DIR/queue, which make wrote longest first, until it
# is empty: the VMs of a run share the queue, so the one that is free takes
# the next suite. The first failure ends the VM's run, and the others take
# what is left.
#
# TestMemoryDrainIntegration's second run caps its reads from the disk at this
# many bytes a second, so the swapoff has to outlast the 90 seconds systemd
# gives a swap unit.
readonly DRAIN_READ_BPS=2097152

tests() {
	local dir=${1:-} suite
	[[ -n $dir ]] || die "usage: test DIR [SUITE...]"
	shift
	if (($# > 0)); then
		for suite in "$@"; do
			run_suite "$dir" "$suite"
		done
		return
	fi
	[[ -f "${dir}/queue" ]] || die "no suite queue in ${dir}"
	while true; do
		suite="$(take_suite "${dir}/queue")"
		[[ -n $suite ]] || break
		run_suite "$dir" "$suite"
	done
	[[ -n $IP ]] || say "the queue was empty before ${DOMAIN} took a suite"
}

# take_suite QUEUE prints the first suite in QUEUE and removes it, under a lock
# so two VMs never take the same one; nothing once the queue is empty.
take_suite() {
	(
		flock 8
		head -n 1 "$1"
		sed -i 1d "$1"
	) 8>>"$1.lock"
}

# run_suite DIR SUITE boots the VM before its first suite, so a VM the queue
# has nothing for never starts.
run_suite() {
	local kernel
	declare -F "suite_$2" >/dev/null || die "unknown suite '$2'"
	if [[ -z $IP ]]; then
		step start connect
		kernel="$(vm_ssh uname -r </dev/null)"
		echo "VM kernel: ${kernel}"
	fi
	step "$2" "suite_$2" "$1"
}

# push_test DIR NAME copies DIR/NAME.test into the VM user's home.
push_test() {
	[[ -f "$1/$2.test" ]] || die "no test binary $1/$2.test"
	vm_ssh "cat > $2.test && chmod +x $2.test" <"$1/$2.test"
}

# run_test NAME PATTERN TIMEOUT [VAR=VALUE...] runs the tests of ./NAME.test
# that PATTERN matches, as root and with those variables.
run_test() {
	local name=$1 pattern=$2 timeout=$3 vars=""
	shift 3
	(($# == 0)) || vars="$(printf '%q ' "$@")"
	vm_ssh "sudo env ${vars}./${name}.test -test.run $(printf %q "$pattern") -test.count=1 -test.v -test.timeout=${timeout}" </dev/null
}

# The tests of test-btrfs-integration, on the VM's kernel.
suite_btrfs() {
	local name
	for name in btrfs storage; do
		push_test "$1" "$name"
		run_test "$name" "${LINUXIO_VM_BTRFS_RUN:?mk/vm.mk sets it}" 20m LINUXIO_RUN_BTRFS_INTEGRATION=1
	done
}

# TestMemoryIntegration (backend/bridge/handlers/memory) switches the VM to
# zram and back through systemd and the kernel. It needs a zram tool and a
# swap file in fstab: the first run installs one and adds a 1 GiB /swap.img,
# and make vm-reset undoes both. TestMemoryDrainIntegration then parks data
# on the data disk vdb, used whole as swap, twice: with 2 GiB, then with 320
# MiB read back at DRAIN_READ_BPS. vdb's fstab entry is noauto, so it swaps
# only for the test (an active swap partition rules the zram mode out), and
# pri=10 puts it ahead of /swap.img. A vdb that holds anything but swap is
# another check's and is refused. vdb stops swapping when the suite ends,
# passed, failed or interrupted.
suite_memory() {
	local -r drain=(LINUXIO_RUN_MEMORY_INTEGRATION=1 LINUXIO_MEMORY_DRAIN_DEVICE=/dev/vdb)
	[[ ${LINUXIO_VM_ZRAM_PROVIDER:-zram-tools} =~ ^zram-(tools|generator)$ ]] ||
		die "ZRAM_PROVIDER must be zram-tools or zram-generator"
	zram_setup
	vm_ssh sudo bash -s <<'SWAPFILE'
set -euo pipefail
if ! grep -qs '^/swap.img[[:space:]]' /etc/fstab; then
	fallocate -l 1G /swap.img
	chmod 600 /swap.img
	mkswap /swap.img >/dev/null
	echo '/swap.img none swap sw 0 0' >>/etc/fstab
	systemctl daemon-reload
	systemctl start swap.img.swap
fi
SWAPFILE
	push_test "$1" memory
	run_test memory '^TestMemoryIntegration$' 30m LINUXIO_RUN_MEMORY_INTEGRATION=1
	trap vdb_swap_off EXIT
	drain_setup
	run_test memory '^TestMemoryDrainIntegration$' 30m "${drain[@]}" LINUXIO_MEMORY_DRAIN_MIB=2048
	run_test memory '^TestMemoryDrainIntegration$' 30m "${drain[@]}" LINUXIO_MEMORY_DRAIN_MIB=320 \
		LINUXIO_MEMORY_DRAIN_READ_BPS="$DRAIN_READ_BPS" LINUXIO_MEMORY_DRAIN_MIN_SECONDS=90
	trap - EXIT
	vdb_swap_off
}

# After an interrupt the drain test can still be running in the VM, since ssh
# gives it no terminal to hang up: it and the processes it parked on vdb are
# ended first, so the swapoff does not pull their pages back under them.
vdb_swap_off() {
	vm_ssh sudo bash -s <<'SWAPOFF' || say "/dev/vdb still swaps in ${DOMAIN}: run make vm-reset"
set -euo pipefail
pkill -x memory.test || true
for _ in {1..30}; do
	pgrep -x memory.test >/dev/null || break
	sleep 1
done
systemctl stop dev-vdb.swap
SWAPOFF
}

# zram-tools by default. LINUXIO_VM_ZRAM_PROVIDER=zram-generator uses systemd's
# zram-generator instead (the Fedora and RHEL path, from Ubuntu's
# systemd-zram-generator package); LinuxIO uses it only when zram-tools is
# absent, so that needs a VM without zram-tools.
zram_setup() {
	if [[ ${LINUXIO_VM_ZRAM_PROVIDER:-zram-tools} == zram-generator ]]; then
		vm_ssh sudo bash -s <<'GENERATOR'
set -euo pipefail
if test -x /usr/sbin/zramswap; then
	echo "zram-tools is installed and LinuxIO prefers it: run make vm-reset first" >&2
	exit 1
fi
if ! test -x /usr/lib/systemd/system-generators/zram-generator; then
	apt-get update -qq
	DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd-zram-generator
fi
if grep -q '^/dev/zram' /proc/swaps; then systemctl stop dev-zram0.swap; fi
GENERATOR
	else
		# The package starts zramswap.service, which the test does itself.
		vm_ssh sudo bash -s <<'TOOLS'
set -euo pipefail
if ! test -x /usr/sbin/zramswap; then
	apt-get update -qq
	DEBIAN_FRONTEND=noninteractive apt-get install -y -qq zram-tools
	systemctl disable --now zramswap.service
fi
TOOLS
	fi
}

drain_setup() {
	vm_ssh sudo bash -s <<'DRAIN'
set -euo pipefail
kind=$(blkid -o value -s TYPE /dev/vdb || true)
if [[ -n $kind && $kind != swap ]]; then
	echo "/dev/vdb holds ${kind}, which another check put there: run make vm-reset first" >&2
	exit 1
fi
if [[ -z $kind ]]; then mkswap /dev/vdb >/dev/null; fi
if ! grep -qs '^/dev/vdb[[:space:]]' /etc/fstab; then
	echo '/dev/vdb none swap sw,noauto,pri=10 0 0' >>/etc/fstab
	systemctl daemon-reload
fi
systemctl start dev-vdb.swap
DRAIN
}

# The file browser's root-only ownership tests of test-filebrowser-root.
suite_filebrowser() {
	local name
	for name in filebrowser filebrowser-services; do
		push_test "$1" "$name"
		run_test "$name" "${LINUXIO_VM_FILEBROWSER_RUN:?mk/vm.mk sets it}" 10m
	done
}

# The bridge-to-job-host relay tests (backend/bridge/cmd/jobs_relay_test.go).
# They serve a host in-process, so as root its owner and privileged rules are
# those of the root host; the relay dials it without starting a socket unit.
suite_relay() {
	push_test "$1" bridge-cmd
	run_test bridge-cmd "${LINUXIO_VM_RELAY_RUN:?mk/vm.mk sets it}" 10m
}

# The updater's install script, run with --dry-run by systemd-run in the
# sandbox the updater gives it (test-updater). The test reads the script from
# the checkout, which the VM does not have, so it gets a copy.
suite_updater() {
	push_test "$1" appupdate
	vm_ssh 'cat > install-linuxio-binaries.sh' <"${REPO_ROOT}/packaging/scripts/install-linuxio-binaries.sh"
	run_test appupdate "${LINUXIO_VM_UPDATER_RUN:?mk/vm.mk sets it}" 10m \
		LINUXIO_RUN_SYSTEMD_INTEGRATION=1 LINUXIO_INSTALL_SCRIPT="/home/${VM_USER}/install-linuxio-binaries.sh"
}

# The Docker update's real Compose runs (test-docker-update-integration),
# which pull alpine images from Docker Hub. Ubuntu's docker.io comes without
# the Compose plugin, so the first run installs docker-compose-v2.
suite_docker() {
	vm_ssh sudo bash -s <<'COMPOSE'
set -euo pipefail
if ! docker compose version >/dev/null 2>&1; then
	apt-get update -qq
	DEBIAN_FRONTEND=noninteractive apt-get install -y -qq docker-compose-v2
fi
COMPOSE
	push_test "$1" dockerupdate
	run_test dockerupdate "${LINUXIO_VM_DOCKER_RUN:?mk/vm.mk sets it}" 10m LINUXIO_RUN_DOCKER_INTEGRATION=1
}

# A macvlan link moved into another network namespace, still found from its
# parent (bridge/handlers/network/internal/hostbridge). The test adds dummy
# and macvlan links and a namespace, which belong on a VM, not a workstation.
suite_network() {
	push_test "$1" hostbridge
	run_test hostbridge '^TestVirtualChildrenByParentSeesOtherNamespaces$' 10m
}

# Config files a root bridge creates for another user's IDs
# (bridge/internal/userconfig).
suite_userconfig() {
	push_test "$1" userconfig
	run_test userconfig '^TestOwnedConfigArtifactsCanTargetDifferentIDsWhenPrivileged$' 10m
}

# The indexer's socket activation, idle exit and index timer under systemd
# (packaging/scripts/tests/systemd-indexer-smoke.sh). The script installs the
# LinuxIO build make made and uninstalls it again, and refuses a VM that
# already has LinuxIO.
suite_indexer() {
	local build=${LINUXIO_VM_BUILD_DIR:?mk/vm.mk sets it}
	[[ -x "${build}/linuxio" ]] || die "no LinuxIO build in '${build}'"
	push_checkout "$build"
	vm_ssh "sudo env LINUXIO_SYSTEMD_SMOKE=1 LINUXIO_SYSTEMD_SMOKE_CONFIRM=disposable-linuxio-host LINUXIO_SMOKE_BUILD_DIR=/home/${VM_USER}/linuxio/build bash linuxio/packaging/scripts/tests/systemd-indexer-smoke.sh" </dev/null
}

boot_id() { vm_ssh cat /proc/sys/kernel/random/boot_id </dev/null; }

# Waits until the VM answers SSH from a boot other than $1.
wait_for_boot() {
	local deadline=$((SECONDS + 300)) boot=""
	until boot="$(boot_id 2>/dev/null)" && [[ -n $boot && $boot != "$1" ]]; do
		((SECONDS < deadline)) || die "${DOMAIN} did not boot again within 300 seconds"
		sleep 3
	done
}

# An orderly reboot: the guest's systemd stops its services first.
reboot_vm() {
	connect
	local boot
	boot="$(boot_id)"
	v reboot "$DOMAIN" >/dev/null
	wait_for_boot "$boot"
}

# A power loss: the domain stops at once, then boots again.
power_cut() {
	connect
	local boot
	boot="$(boot_id)"
	v destroy "$DOMAIN" >/dev/null
	v start "$DOMAIN" >/dev/null
	wait_for_boot "$boot"
}

# An orderly shutdown, SECONDS with the domain off, then a boot: what a timer
# does with the time that passed while the host was down.
off_for() {
	local seconds=${1:-}
	[[ $seconds =~ ^[0-9]+$ ]] || die "usage: off-for SECONDS"
	connect
	local boot
	boot="$(boot_id)"
	v shutdown "$DOMAIN" >/dev/null
	wait_for_shut_off 180
	sleep "$seconds"
	v start "$DOMAIN" >/dev/null
	wait_for_boot "$boot"
}

# Caps a data disk's I/O in bytes per second (0 lifts the cap), in the
# running domain and in its definition, so the cap outlasts a power cut. The
# clean snapshot holds no cap, so a reset lifts it too.
throttle() {
	local disk=${1:-} rate=${2:-}
	[[ $disk =~ ^vd[b-e]$ && $rate =~ ^[0-9]+$ ]] || die "usage: throttle vdb|vdc|vdd|vde BYTES_PER_SEC"
	connect
	v blkdeviotune "$DOMAIN" "$disk" --total-bytes-sec "$rate" --live --config >/dev/null
}

# Shut the domain down in order when it runs, keeping its disks as they are:
# the test targets end with this, so no VM runs unused.
stop() {
	domain_exists || return 0
	[[ "$(domain_state)" != "shut off" ]] || return 0
	v shutdown "$DOMAIN" >/dev/null
	wait_for_shut_off 180
	say "${DOMAIN} is stopped; the next VM target starts it"
}

reset() {
	domain_exists || die "${DOMAIN} does not exist; run make vm-create"
	if [[ "$(domain_state)" != "shut off" ]]; then v destroy "$DOMAIN" >/dev/null; fi
	v snapshot-revert "$DOMAIN" "$SNAPSHOT"
	say "${DOMAIN} reverted to ${SNAPSHOT} and stopped; the next VM target starts it"
}

# Bring the clean snapshot up to date: revert to it, upgrade every package,
# switch the unattended apt timers off (the seed does that for a new VM; one
# created before it did still has them on) and take the snapshot again. An
# unattended upgrade that a power-cut check interrupts leaves dpkg broken, and
# a long list of waiting updates slows the Updates page the specs open.
update() {
	reset
	connect
	say "upgrading the packages of ${DOMAIN} (several minutes)"
	vm_ssh sudo bash -s <<'UPGRADE'
set -euo pipefail
systemctl disable --now apt-daily.timer apt-daily-upgrade.timer
# A run the boot started holds the dpkg lock; let it end by itself.
while systemctl is-active apt-daily.service apt-daily-upgrade.service | grep -Eqx 'active|activating'; do
	sleep 2
done
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=600 -qq update
apt-get -o DPkg::Lock::Timeout=600 -o Dpkg::Options::=--force-confold -y -qq dist-upgrade
sync
UPGRADE
	v shutdown "$DOMAIN" >/dev/null
	wait_for_shut_off 180
	# Between these two the VM has no clean snapshot: if the second fails,
	# make vm-destroy and make vm-create bring one back.
	v snapshot-delete "$DOMAIN" "$SNAPSHOT" >/dev/null
	v snapshot-create-as "$DOMAIN" "$SNAPSHOT" --description "cloud-init done, packages upgraded $(date -u +%F), LinuxIO not installed" >/dev/null
	say "${DOMAIN} is upgraded and stopped (snapshot ${SNAPSHOT} taken again); next: make vm-install"
}

destroy() {
	if domain_exists; then
		if [[ "$(domain_state)" != "shut off" ]]; then v destroy "$DOMAIN" >/dev/null; fi
		v undefine "$DOMAIN" --snapshots-metadata >/dev/null
	fi
	for vol in "$BASE_VOL" "$SYSTEM_VOL" "${DATA_VOLS[@]}"; do
		if vol_exists "$vol"; then v vol-delete --pool "$POOL" "$vol" >/dev/null; fi
	done
	rm -f "${STATE}/known_hosts"
	say "${DOMAIN} and its volumes are gone; the image cache stays in ${IMAGE_DIR}"
}

# How many VMs the host can run now, MOST_VMS at most: each needs its memory
# free and its CPUs, and HOST_RESERVE_MIB stays with the host. What a running
# VM of this release holds counts as free, since the target that asks is about
# to use or stop it.
fit() {
	local free_kib n by_memory by_cpu
	free_kib="$(free_for_vms_kib)"
	by_memory=$(((free_kib / 1024 - HOST_RESERVE_MIB) / (VM_MEMORY_GIB * 1024)))
	by_cpu=$(($(nproc) / VM_VCPUS))
	n=$((by_memory < by_cpu ? by_memory : by_cpu))
	((n <= MOST_VMS)) || n=$MOST_VMS
	((n >= 1)) || n=1
	echo "$n"
}

free_for_vms_kib() {
	local free_kib n rss
	free_kib="$(awk '$1 == "MemAvailable:" { print $2 }' /proc/meminfo)"
	for n in {1..9}; do
		[[ "$(v domstate "$(domain_name "$n")" 2>/dev/null)" == running ]] || continue
		rss="$(v dommemstat "$(domain_name "$n")" 2>/dev/null | awk '$1 == "rss" { print $2 }')"
		free_kib=$((free_kib + ${rss:-0}))
	done
	echo "$free_kib"
}

# The numbers behind fit, for the line test-vm-browser and test-vm-backend
# start with.
room() {
	local free_kib
	free_kib="$(free_for_vms_kib)"
	printf '%d.%d GiB free and %d CPUs (a VM takes %d GiB and %d CPUs, %d at most)\n' \
		$((free_kib / 1048576)) $((free_kib % 1048576 * 10 / 1048576)) "$(nproc)" \
		"$VM_MEMORY_GIB" "$VM_VCPUS" "$MOST_VMS"
}

# How many VMs of this release exist, counted from the first with no gap; 1
# when there is none, so the command that follows says to create it.
count() {
	local n=0
	while ((n < 9)) && domain_exists "$(domain_name $((n + 1)))"; do
		n=$((n + 1))
	done
	echo $((n > 0 ? n : 1))
}

# Runs a command of this script for the VMs 1..COUNT side by side. Each VM's
# output goes to LOG_DIR/vm-N-COMMAND.log. On a terminal one row per VM shows
# its steps as they end and the one running; elsewhere a line per step that
# ends. A VM that fails has the end of its log shown, and each fails with it.
# A shell without job control makes a background command ignore Ctrl-C, which
# would leave these running after the target was stopped: each takes it back.
each() {
	local count=${1:-} n status_dir failed=0 begun=$EPOCHSECONDS alive tty=0 drawn=0
	local -a codes=() ended=() logs=() shown=()
	[[ $count =~ ^[1-9]$ && $# -ge 2 ]] || die "usage: each COUNT COMMAND [ARGS]"
	shift
	[[ -t 2 ]] && tty=1
	mkdir -p "$LOG_DIR"
	status_dir="$(mktemp -d)"
	# shellcheck disable=SC2064 # the directory is fixed now
	trap "rm -f -- '${status_dir}'/[1-9] '${status_dir}'/[1-9].code; rmdir -- '${status_dir}'" EXIT
	for n in $(seq "$count"); do
		logs[n]="${LOG_DIR}/vm-${n}-${1}.log"
		: >"${status_dir}/${n}"
		# The exit code lands in N.code, so the loop below never asks after a
		# process ID that may since belong to another program.
		(
			trap - INT QUIT
			code=0
			LINUXIO_VM_INDEX=$n LINUXIO_VM_STATUS="${status_dir}/${n}" "$0" "$@" </dev/null >"${logs[n]}" 2>&1 || code=$?
			echo "$code" >"${status_dir}/${n}.code"
		) &
		shown[n]=0
	done
	while true; do
		alive=0
		for n in $(seq "$count"); do
			if [[ -z ${codes[n]:-} && -s "${status_dir}/${n}.code" ]]; then
				codes[n]=$(<"${status_dir}/${n}.code")
				ended[n]=$EPOCHSECONDS
			fi
			[[ -n ${codes[n]:-} ]] || alive=1
		done
		if ((tty)); then
			((drawn == 0)) || printf '\e[%dA' "$count" >&2
			for n in $(seq "$count"); do
				printf '\e[2K%s\n' "$(vm_row "$n" "$1" "${status_dir}/${n}" "${codes[n]:-}" "$begun" "${ended[n]:-}")" >&2
			done
			drawn=1
		else
			for n in $(seq "$count"); do
				shown[n]=$(vm_new_lines "$n" "$1" "${status_dir}/${n}" "${codes[n]:-}" "$begun" "${ended[n]:-}" "${shown[n]}")
			done
		fi
		((alive)) || break
		sleep 1
	done
	wait
	for n in $(seq "$count"); do
		((codes[n] == 0)) && continue
		failed=1
		say "$(vm_tag "$n") failed; the end of ${logs[n]#"$REPO_ROOT"/}:"
		tail -n 25 "${logs[n]}" | sed 's/^/    /' >&2
	done
	return "$failed"
}

color() { [[ -t 2 && -z ${NO_COLOR:-} ]]; }
paint() { if color; then printf '\e[%sm%s\e[0m' "$1" "$2"; else printf '%s' "$2"; fi; }
# Teal, amber, violet, blue: the colours the VM reporter gives the same VMs.
vm_tag() {
	local -r colors=(80 215 141 75)
	paint "1;38;5;${colors[($1 - 1) % 4]}" "vm$1"
}
duration() { if (($1 < 60)); then echo "${1}s"; else printf '%dm %02ds' $(($1 / 60)) $(($1 % 60)); fi; }

# The steps of VM N as "STATE NAME SECONDS" lines, in order. A command that
# reports no steps (stop, reset, install) is one step of its own name. A step
# that was running when the VM failed is failed.
vm_steps() {
	local command=$2 file=$3 code=$4 begun=$5 end_epoch=$6 name state value
	local -a names=()
	local -A states=() values=()
	while read -r name state value; do
		[[ -n ${states[$name]:-} ]] || names+=("$name")
		states[$name]=$state
		values[$name]=$value
	done <"$file"
	if ((${#names[@]} == 0)); then
		names=("$command")
		if [[ -z $code ]]; then
			states[$command]=run values[$command]=$begun
		else
			states[$command]=$([[ $code == 0 ]] && echo ok || echo fail) values[$command]=$((end_epoch - begun))
		fi
	fi
	for name in "${names[@]}"; do
		state=${states[$name]} value=${values[$name]}
		if [[ $state == run ]]; then
			if [[ -z $code ]]; then
				value=$((EPOCHSECONDS - value))
			else
				state=fail value=$((end_epoch - value))
			fi
		fi
		echo "$state $name $value"
	done
}

vm_step_text() {
	local state=$1 name=$2 value=$3 label
	case $name in
	ready) label="ready" ;;
	*) label="$name $(duration "$value")" ;;
	esac
	case $state in
	ok) printf '%s %s' "$(paint 32 ✓)" "$label" ;;
	fail) printf '%s %s' "$(paint 31 ✘)" "$(paint 31 "$label")" ;;
	run) printf '%s %s' "$(paint 1 ▶)" "$label" ;;
	esac
}

vm_row() {
	local n=$1 row state name value
	row="$(vm_tag "$n")  $(printf '%-15s' "$(domain_name "$n")")"
	while read -r state name value; do
		row+="  $(vm_step_text "$state" "$name" "$value")"
	done < <(vm_steps "$@")
	echo "$row"
}

# For a log or a pipe: a line for each step of VM N that ended since the
# first PRINTED ones, then the number of steps that have ended.
vm_new_lines() {
	local n=$1 printed=$7 i=0 state name value
	while read -r state name value; do
		[[ $state != run ]] || continue
		i=$((i + 1))
		((i > printed)) || continue
		printf '%s  %-15s  %s\n' "$(vm_tag "$n")" "$(domain_name "$n")" "$(vm_step_text "$state" "$name" "$value")" >&2
	done < <(vm_steps "${@:1:6}")
	echo "$i"
}

[[ "$(v uri 2>/dev/null)" == qemu:///system ]] || die "virsh cannot reach qemu:///system (is this user in the libvirt group?)"

case "${1:-}" in
create) create ;;
ensure) ensure ;;
install) install "${2:-}" ;;
ssh)
	connect
	shift
	vm_ssh "$@"
	;;
start) connect ;;
ip)
	connect
	echo "$IP"
	;;
password) cat "${STATE}/password" ;;
reboot) reboot_vm ;;
power-cut) power_cut ;;
off-for) off_for "${2:-}" ;;
throttle) throttle "${2:-}" "${3:-}" ;;
stop) stop ;;
reset) reset ;;
update) update ;;
destroy) destroy ;;
fit) fit ;;
room) room ;;
count) count ;;
prepare) prepare "${2:-}" ;;
test)
	shift
	tests "$@"
	;;
each)
	shift
	each "$@"
	;;
*) die "usage: $(basename "$0") create | ensure | prepare [BUILD_DIR] | install BUILD_DIR | start | ssh [COMMAND] | ip | password | reboot | power-cut | off-for SECONDS | throttle DISK BYTES_PER_SEC | stop | reset | update | destroy | fit | room | count | test DIR [SUITE...] | each COUNT COMMAND [ARGS]" ;;
esac
