#!/usr/bin/env bash
# Disposable LinuxIO test VM on qemu:///system, driven by mk/vm.mk
# (docs/development.md, "Test VM"). It is hard-wired to the domain linuxio-e2e
# and its linuxio-e2e-* volumes in the default pool, and never changes or
# deletes any other domain, volume or network; it reads the names of the
# running domains only to find the test VMs among them (check_memory). State
# (image cache, SSH key, login) lives in ~/.cache/linuxio-vm.
#
# LINUXIO_VM_DISTRO=fedora selects a VM on Fedora instead of Ubuntu, the
# default: the domain linuxio-e2e-fedora, its linuxio-e2e-fedora-* volumes and
# ~/.cache/linuxio-vm-fedora, beside Ubuntu's. A profile below holds what
# differs between distributions.
#
# LINUXIO_VM_RELEASE=noble selects a second Ubuntu VM on Ubuntu 24.04 for the
# root tests on an older kernel: the domain linuxio-e2e-noble, its
# linuxio-e2e-noble-* volumes and ~/.cache/linuxio-vm-noble, beside the first.
#
# LINUXIO_VM_INDEX=2..9 selects one more VM of the same release, so the browser
# specs run on several at once: the domain linuxio-e2e-2, its volumes and
# ~/.cache/linuxio-vm-2 (linuxio-e2e-fedora-2 and ~/.cache/linuxio-vm-fedora-2
# on Fedora). It shares only the image cache with the first.
#
# Usage: linuxio-vm.sh create | ensure | prepare [BUILD_DIR] |
#        install BUILD_DIR [REF] | start | ssh [COMMAND] | ip | password |
#        reboot | power-cut | off-for SECONDS | throttle DISK BYTES_PER_SEC |
#        stop | reset | update | destroy | existing | locks N...|all |
#        lock N:FD... | pick WANT N:FD... | running |
#        test DIR [SUITE...] | each VMS COMMAND [ARGS]
set -euo pipefail

readonly DISTRO="${LINUXIO_VM_DISTRO:-ubuntu}"
# SEED_BASE gives each release ports of its own for the first-boot seed, VM N
# taking SEED_BASE+N, so first boots of any VMs can run side by side.
case "${DISTRO}/${LINUXIO_VM_RELEASE:-}" in
ubuntu/ | ubuntu/resolute) readonly RELEASE=resolute VERSION=26.04 FIRST_DOMAIN=linuxio-e2e SEED_BASE=8398 ;;
ubuntu/noble) readonly RELEASE=noble VERSION=24.04 FIRST_DOMAIN=linuxio-e2e-noble SEED_BASE=8408 ;;
# A Fedora release's Cloud Base images never change once it ships, so the
# version names the compose too; the first boot upgrades the packages.
fedora/) readonly RELEASE=45 VERSION=45-1.4 FIRST_DOMAIN=linuxio-e2e-fedora SEED_BASE=8418 ;;
*)
	echo "linuxio-vm: unknown distribution '${DISTRO}' release '${LINUXIO_VM_RELEASE:-}' (ubuntu, with resolute or noble, or fedora)" >&2
	exit 1
	;;
esac
# The profile: where the cloud image and its checksum file are published, the
# first-boot seed, the package manager and the packages the suites add.
# Neither checksum file's signature is checked: it comes over HTTPS from the
# distribution's own server, and Fedora's image from one of its mirrors.
case $DISTRO in
ubuntu)
	readonly IMAGE_NAME="ubuntu-${VERSION}-server-cloudimg-amd64.img"
	readonly IMAGE_URL="https://cloud-images.ubuntu.com/releases/${RELEASE}/release/${IMAGE_NAME}"
	readonly SUMS_URL="https://cloud-images.ubuntu.com/releases/${RELEASE}/release/SHA256SUMS"
	readonly SEED_TEMPLATE=user-data.yaml.in PKG=apt
	# zram-tools is LinuxIO's first choice where it exists.
	readonly ZRAM_DEFAULT=zram-tools GENERATOR_PACKAGE=systemd-zram-generator
	readonly COMPOSE_PACKAGE=docker-compose-v2 BUILDX_PACKAGE=docker-buildx
	;;
fedora)
	readonly IMAGE_NAME="Fedora-Cloud-Base-Generic-${VERSION}.x86_64.qcow2"
	readonly IMAGE_URL="https://download.fedoraproject.org/pub/fedora/linux/releases/${RELEASE}/Cloud/x86_64/images/${IMAGE_NAME}"
	readonly SUMS_URL="https://dl.fedoraproject.org/pub/fedora/linux/releases/${RELEASE}/Cloud/x86_64/images/Fedora-Cloud-${VERSION}-x86_64-CHECKSUM"
	readonly SEED_TEMPLATE=user-data-fedora.yaml.in PKG=dnf
	# Fedora has no zram-tools; the image comes with zram-generator.
	readonly ZRAM_DEFAULT=zram-generator GENERATOR_PACKAGE=zram-generator
	readonly COMPOSE_PACKAGE=docker-compose BUILDX_PACKAGE=docker-buildx
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
# What a run that plans its own VMs counts for each: two of the host's threads
# (nproc: hyperthreads, not cores). The suites and the specs wait on reboots,
# throttled disks and timers and hardly use the CPU, so a VM's 4 vCPUs, which
# its boot and an install use for a moment, share threads with another's.
readonly VM_HOST_THREADS=2
# The VMs of a release are numbered 1 to 9.
readonly MOST_VMS=9
readonly SNAPSHOT=clean
readonly POOL=default
readonly BASE_VOL="${DOMAIN}-base.qcow2"
readonly SYSTEM_VOL="${DOMAIN}-system.qcow2"
readonly DATA_VOLS=("${DOMAIN}-data1.qcow2" "${DOMAIN}-data2.qcow2" "${DOMAIN}-data3.qcow2" "${DOMAIN}-data4.qcow2")
readonly HOST_IP=192.168.122.1
readonly SEED_PORT=$((SEED_BASE + INDEX))
readonly SEED_URL="http://${HOST_IP}:${SEED_PORT}/"
# The state folder of the domain $1: SSH key, login, address, the ID of the
# target that last started it (run) and, while it holds a build install put
# there, the marker "installed".
state_of() { echo "${HOME}/.cache/linuxio-vm${1#linuxio-e2e}"; }
STATE="$(state_of "$DOMAIN")"
readonly STATE
readonly IMAGE_DIR="${HOME}/.cache/linuxio-vm${FIRST_DOMAIN#linuxio-e2e}"
# The locks of the test VMs, one for each domain, which mk/vm.mk opens and
# lock and pick take (see there), and two short ones: "taking", held while a
# target takes its VMs, and "start", taken by every start of a test VM, of
# any release, around its memory check (start_domain).
readonly LOCK_DIR="${HOME}/.cache/linuxio-vm/locks"
readonly START_LOCK="${LOCK_DIR}/start"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly REPO_ROOT
# Where `each` keeps the output of every VM, one file per VM and command.
readonly LOG_DIR="${LINUXIO_VM_LOG_DIR:-${REPO_ROOT}/.cache/test-logs}"
# Where `each` leaves the steps of the rows it drew, for the next `each` of the
# same make recipe to go on with them.
readonly ROWS="${LOG_DIR}/vm-rows"
# How wide a row may be: the terminal's width, read when `each` draws.
ROW_WIDTH=80
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

# vm_install PACKAGE... installs packages in the VM with the profile's
# package manager.
vm_install() {
	case $PKG in
	apt) vm_ssh "sudo apt-get update -qq && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq $*" </dev/null ;;
	dnf) vm_ssh "sudo dnf install -y -q $*" </dev/null ;;
	esac
}

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

# Start the domain if needed and wait until it answers SSH; sets IP. With
# LINUXIO_VM_NO_START, which make vm-ssh VM_KEEP=1 sets when it found the VM
# running and so took no lock, it refuses to start one.
connect() {
	domain_exists || die "${DOMAIN} does not exist; run make vm-create"
	if [[ "$(domain_state)" != running ]]; then
		[[ -z ${LINUXIO_VM_NO_START:-} ]] || die "${DOMAIN} stopped, and this command holds no lock to start it; run it again"
		say "starting ${DOMAIN}"
		start_domain
	fi
	note_run
	wait_for_ssh 300
}

gib() { printf '%d.%d GiB' $(($1 / 1048576)) $(($1 % 1048576 * 10 / 1048576)); }

# MemAvailable, read from LINUXIO_VM_MEMINFO when it names a file in place of
# /proc/meminfo, to see a refusal without filling the host.
available_kib() {
	local meminfo=${LINUXIO_VM_MEMINFO:-/proc/meminfo} kib
	kib="$(awk '$1 == "MemAvailable:" { print $2 }' "$meminfo")"
	[[ $kib =~ ^[0-9]+$ ]] || die "no MemAvailable in ${meminfo}"
	echo "$kib"
}

# The running test VMs, of every distribution and release, a "NAME KIB" line
# each: what it was given and has not touched yet (its memory less its RSS),
# since it will, or all of it when libvirt does not say.
untouched() {
	local vm_kib=$((VM_MEMORY_GIB * 1048576)) list name stats state actual rss
	local -a names=()
	list="$(v list --name)" || die "cannot list the running domains"
	mapfile -t names <<<"$list"
	for name in "${names[@]}"; do
		[[ $name == linuxio-e2e || $name == linuxio-e2e-* ]] || continue
		if stats="$(v dommemstat "$name" 2>/dev/null)"; then
			read -r actual rss < <(awk '$1 == "actual" { a = $2 } $1 == "rss" { r = $2 } END { print a + 0, r + 0 }' <<<"$stats")
			((actual > 0)) || actual=$vm_kib
		else
			# Stopped or gone since the list, it holds nothing; still running
			# (libvirt busy), it counts whole.
			state="$(v domstate "$name" 2>&1)" || true
			[[ $state != "shut off" && $state != *"failed to get domain"* ]] || continue
			actual=$vm_kib rss=0
		fi
		echo "$name $((actual > rss ? actual - rss : 0))"
	done
}

# check_memory [RESTART]: before a test VM starts, the host needs its memory
# free and HOST_RESERVE_MIB more, or only its memory when RESTART is 1 (see
# start_domain). Free is MemAvailable less what the running test VMs have not
# touched yet (untouched).
check_memory() {
	local restart=${1:-0} vm_kib reserve_kib need_kib avail_kib given_kib=0 free_kib vms name kib why
	local -a running=()
	vm_kib=$((VM_MEMORY_GIB * 1048576))
	reserve_kib=$((restart ? 0 : HOST_RESERVE_MIB * 1024))
	need_kib=$((vm_kib + reserve_kib))
	avail_kib="$(available_kib)"
	vms="$(untouched)"
	while read -r name kib; do
		[[ -n $name ]] || continue
		running+=("$name")
		given_kib=$((given_kib + kib))
	done <<<"$vms"
	free_kib=$((avail_kib - given_kib))
	if ((free_kib < need_kib)); then
		why="${VM_MEMORY_GIB} GiB for the VM, $(gib "$reserve_kib") kept for the host"
		((restart == 0)) || why="${VM_MEMORY_GIB} GiB for the VM, which this target started before"
		die "not enough memory to start ${DOMAIN}: it needs $(gib "$need_kib") (${why}) and $(gib $((free_kib > 0 ? free_kib : 0))) is free ($(gib "$avail_kib") available less $(gib "$given_kib") that running test VMs were given and have not used yet); test VMs running: ${running[*]:-none}"
	fi
}

# mk/vm.mk gives each target an ID of its own, LINUXIO_VM_RUN, which the rig
# commands its specs run inherit. STATE/run keeps the ID of the target that
# last started the VM or found it running.
note_run() { [[ -z ${LINUXIO_VM_RUN:-} ]] || echo "$LINUXIO_VM_RUN" >"${STATE}/run"; }

# Starts the domain once check_memory finds room for it. The check and the
# start hold START_LOCK, which every start of a test VM takes, so two starts
# side by side (two targets, or the VMs of one each) never count the same
# memory: the second sees the first running with its memory untouched. A VM
# this target started before or found running (a power cut, an off-for, a
# boot after a spec powered it off, the reset before the release-update spec)
# needs only its own memory: the reserve went to the target's browsers, and
# the VM's memory was the target's already.
start_domain() {
	local restart=0
	if [[ -n ${LINUXIO_VM_RUN:-} && "$(cat "${STATE}/run" 2>/dev/null)" == "$LINUXIO_VM_RUN" ]]; then restart=1; fi
	mkdir -p "${START_LOCK%/*}"
	(
		flock -w 120 7 || die "another test VM took more than 120 seconds to start"
		check_memory "$restart"
		v start "$DOMAIN" >/dev/null
	) 7>"$START_LOCK"
	note_run
}

stop_seed() {
	if [[ -n $SEED_PID ]]; then
		kill "$SEED_PID" 2>/dev/null || true
		wait "$SEED_PID" 2>/dev/null || true
		SEED_PID=""
	fi
}

# The cloud image, checked against the SHA-256 in the release's checksum file
# (Ubuntu's SHA256SUMS lines read "SUM *NAME", Fedora's CHECKSUM ones
# "SHA256 (NAME) = SUM"); a cached copy is reused while it still matches. The
# VMs of a release share the copy, so one create at a time looks at it, and
# keeps it locked on fd 8 until it has uploaded it: a create beside it that
# found a newer image would otherwise replace the file under the upload.
fetch_image() {
	local sum image="${IMAGE_DIR}/${IMAGE_NAME}"
	mkdir -p "$IMAGE_DIR"
	chmod 700 "$IMAGE_DIR"
	exec 8>"${IMAGE_DIR}/image.lock"
	flock 8
	sum="$(curl -fsSL "$SUMS_URL" | awk -v f="$IMAGE_NAME" '
		$2 == "*" f { print $1 }
		$1 == "SHA256" && $2 == "(" f ")" && $3 == "=" { print $4 }')"
	[[ $sum =~ ^[0-9a-f]{64}$ ]] || die "${IMAGE_NAME} is not listed once in ${SUMS_URL}"
	if echo "${sum}  ${image}" | sha256sum --check --status >/dev/null 2>&1; then return; fi
	say "downloading ${IMAGE_NAME}"
	curl -fL --progress-bar -o "${image}.part" "$IMAGE_URL"
	echo "${sum}  ${image}.part" | sha256sum --check --status || die "${IMAGE_NAME} does not match ${SUMS_URL##*/}"
	mv "${image}.part" "$image"
}

write_seed() {
	local seed="${STATE}/seed" hash key
	hash="$(openssl passwd -6 -stdin <"${STATE}/password")"
	key="$(cat "${STATE}/id_ed25519.pub")"
	rm -rf "$seed"
	mkdir -p "$seed"
	printf 'instance-id: %s\nlocal-hostname: %s\n' "$DOMAIN" "$DOMAIN" >"${seed}/meta-data"
	sed -e "s|@PASSWORD_HASH@|${hash}|" -e "s|@SSH_KEY@|${key}|" -e "s|@SEED_URL@|${SEED_URL}|" \
		"${REPO_ROOT}/packaging/vm/${SEED_TEMPLATE}" >"${seed}/user-data"
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

# The data disks carry a serial, as real disks do, so udev gives each one an
# identity (ID_SERIAL, /dev/disk/by-id/virtio-linuxio-vdb): LinuxIO's hot spare
# and its automatic rebuild recognise a disk by it (docs/btrfs.md). It names
# the slot, not the volume, and is at most 20 bytes, virtio's limit.
serial_of() { echo "linuxio-$1"; }

define_domain() {
	local disks="" path i serial
	local -r vols=("$SYSTEM_VOL" "${DATA_VOLS[@]}") devs=(vda vdb vdc vdd vde)
	for i in "${!vols[@]}"; do
		path="$(v vol-path --pool "$POOL" "${vols[i]}")"
		serial=""
		((i == 0)) || serial="<serial>$(serial_of "${devs[i]}")</serial>"
		disks+="<disk type='file' device='disk'><driver name='qemu' type='qcow2' discard='unmap'/><source file='${path}'/><target dev='${devs[i]}' bus='virtio'/>${serial}</disk>"
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
	# Before the download, so a host without room is told at once; the first
	# boot checks again (start_domain).
	check_memory
	mkdir -p "$STATE"
	chmod 700 "$STATE"
	fetch_image
	[[ -f "${STATE}/id_ed25519" ]] || ssh-keygen -q -t ed25519 -N '' -C linuxio-e2e-rig -f "${STATE}/id_ed25519"
	(umask 077 && head -c 18 /dev/urandom | base64 >"${STATE}/password")
	rm -f "${STATE}/known_hosts" "${STATE}/installed"
	write_seed

	say "uploading the base image to the ${POOL} pool"
	v vol-create-as "$POOL" "$BASE_VOL" "$(stat -c %s "${IMAGE_DIR}/${IMAGE_NAME}")" --format qcow2 >/dev/null
	v vol-upload --pool "$POOL" "$BASE_VOL" "${IMAGE_DIR}/${IMAGE_NAME}"
	exec 8>&-
	v vol-create-as "$POOL" "$SYSTEM_VOL" 25G --format qcow2 --backing-vol "$BASE_VOL" --backing-vol-format qcow2 >/dev/null
	for vol in "${DATA_VOLS[@]}"; do
		v vol-create-as "$POOL" "$vol" 8G --format qcow2 >/dev/null
	done
	define_domain

	trap stop_seed EXIT
	trap 'exit 130' INT TERM
	start_seed
	say "first boot: cloud-init installs the packages (several minutes)"
	start_domain
	wait_for_ssh 600
	# As root: Fedora's cloud-init keeps its state from other users, and its
	# status then waits for ever. The key that lets SSH in and the sudo rule
	# are written together.
	if ! vm_ssh sudo cloud-init status --wait >/dev/null; then
		vm_ssh 'sudo cloud-init status --long; sudo tail -n 40 /var/log/cloud-init-output.log' >&2 || true
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

# install BUILD_DIR [REF] installs the build in BUILD_DIR with the installer
# and packaging of this checkout, or of the git REF when given: a release's
# binaries need that release's packaging (its units start the programs and
# words it has), so jobs-update.spec.ts installs v0.53.0 with REF v0.53.0.
install() {
	local build_dir=${1:-} ref=${2:-}
	[[ -n $build_dir && -x "${build_dir}/linuxio" ]] || die "no LinuxIO build in '${build_dir}'"
	connect
	push_checkout "$build_dir" "$ref"
	vm_ssh 'sudo ~/linuxio/packaging/scripts/install-linuxio-binaries.sh --local --build-dir ~/linuxio/build'
	: >"${STATE}/installed"
}

# push_checkout BUILD_DIR [REF] puts what the installer reads in ~/linuxio,
# from the checkout or from the git REF, and the build in ~/linuxio/build:
# --local reads packaging/, LICENSE and docs/THIRD_PARTY_NOTICES.md, and finds
# the checkout by its Makefile.
push_checkout() {
	local -a files=(Makefile LICENSE docs/THIRD_PARTY_NOTICES.md packaging)
	if [[ -n ${2:-} ]]; then
		git -C "$REPO_ROOT" archive "$2" -- "${files[@]}"
	else
		tar -C "$REPO_ROOT" -cf - "${files[@]}"
	fi | vm_ssh 'rm -rf linuxio && mkdir -p linuxio/build && tar -C linuxio -xf -'
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
# and make vm-reset undoes both. Where the zram tool comes with a default zram
# (Fedora's), TestMemoryDefaultZramIntegration first starts from it, without
# and then with the swap file. TestMemoryStatusWithoutRootIntegration then
# reads the status as a user without sudo with TuneD running.
# TestMemoryDrainIntegration parks data on the data disk vdb, used whole as
# swap, twice: with 2 GiB, then with 320 MiB read back at DRAIN_READ_BPS.
# vdb's fstab entry is noauto, so it swaps only for the test (an active swap
# partition rules the zram mode out), and pri=10 puts it ahead of /swap.img. A
# vdb that holds anything but swap is another check's and is refused. Then
# fstab names vdb by UUID= for TestMemoryDrainAliasIntegration. vdb stops
# swapping when its tests end, passed, failed or interrupted. Last, the
# reboots: TestMemoryRebootIntegration switches to zram, reboots and switches
# back, once with fstab naming /swap.img and once through a link to it.
suite_memory() {
	local -r run=(LINUXIO_RUN_MEMORY_INTEGRATION=1)
	local -r drain=("${run[@]}" LINUXIO_MEMORY_DRAIN_DEVICE=/dev/vdb)
	zram_install
	push_test "$1" memory
	link_swap_file off
	default_zram
	run_test memory '^TestMemoryDefaultZramIntegration$' 10m "${run[@]}"
	swap_file
	run_test memory '^TestMemoryDefaultZramIntegration$' 10m "${run[@]}"
	zram_default_off
	run_test memory '^TestMemoryIntegration$' 30m "${run[@]}"
	status_without_root
	trap vdb_swap_off EXIT
	drain_setup
	run_test memory '^TestMemoryDrainIntegration$' 30m "${drain[@]}" LINUXIO_MEMORY_DRAIN_MIB=2048
	run_test memory '^TestMemoryDrainIntegration$' 30m "${drain[@]}" LINUXIO_MEMORY_DRAIN_MIB=320 \
		LINUXIO_MEMORY_DRAIN_READ_BPS="$DRAIN_READ_BPS" LINUXIO_MEMORY_DRAIN_MIN_SECONDS=90
	vdb_by_uuid
	run_test memory '^TestMemoryDrainAliasIntegration$' 10m "${drain[@]}"
	vdb_by_path
	trap - EXIT
	vdb_swap_off
	reboot_check
	link_swap_file on
	reboot_check
	link_swap_file off
}

# A 1 GiB /swap.img in fstab, swapping. On a btrfs root (Fedora's) the swap
# file is made by btrfs, which leaves it without copy-on-write as swap needs.
# Unlike mkswap, btrfs does not label it for SELinux, so it is labelled
# swapfile_t, the type swapon is allowed to use. btrfs refuses a file that
# exists, so a /swap.img without its fstab line, as a cut run or default_zram
# leaves it, is removed first; one that swaps is refused.
swap_file() {
	vm_ssh sudo bash -s <<'SWAPFILE'
set -euo pipefail
if ! grep -qs '^/swap.img[[:space:]]' /etc/fstab; then
	if grep -q '^/swap.img[[:space:]]' /proc/swaps; then
		echo "/swap.img swaps without an fstab line: run make vm-reset first" >&2
		exit 1
	fi
	rm -f /swap.img
	if [[ $(stat -f -c %T /) == btrfs ]]; then
		btrfs filesystem mkswapfile --size 1g /swap.img >/dev/null
	else
		fallocate -l 1G /swap.img
		chmod 600 /swap.img
		mkswap /swap.img >/dev/null
	fi
	if command -v selinuxenabled >/dev/null && selinuxenabled; then chcon -t swapfile_t /swap.img; fi
	echo '/swap.img none swap sw 0 0' >>/etc/fstab
	systemctl daemon-reload
	systemctl start swap.img.swap
fi
SWAPFILE
}

# Fedora's default zram as a fresh image has it, where the zram tool comes
# with one (/usr/lib/systemd/zram-generator.conf): no swap file, no
# zram-generator.conf in /etc, and zram0 swapping at the size the vendor file
# sets. An earlier run changed all three, so /swap.img is taken away and out
# of fstab (swap_file makes it again) and its unit let go (LinuxIO's
# hold-off drop-in removed, a mask an older run left removed), the file in
# /etc and LinuxIO's copy of it are removed, and zram0 is stopped, reset and
# started again from the vendor file.
default_zram() {
	vm_ssh sudo bash -s <<'DEFAULT'
set -euo pipefail
[[ -e /usr/lib/systemd/zram-generator.conf ]] || exit 0
if grep -q '^/swap.img[[:space:]]' /proc/swaps; then swapoff /swap.img; fi
sed -i '\|^/swap.img[[:space:]]|d' /etc/fstab
systemctl unmask swap.img.swap
rm -rf /etc/systemd/system/swap.img.swap.d
rm -f /etc/systemd/zram-generator.conf /etc/systemd/zram-generator.conf.before-linuxio
systemctl daemon-reload
if grep -q '^/dev/zram0[[:space:]]' /proc/swaps; then systemctl stop dev-zram0.swap; fi
systemctl stop systemd-zram-setup@zram0.service
# The reset fails while udev still holds the device after the swapoff.
for _ in {1..20}; do
	[[ -e /sys/block/zram0/disksize && $(</sys/block/zram0/disksize) != 0 ]] || break
	echo 1 >/sys/block/zram0/reset 2>/dev/null || sleep 0.5
done
systemctl start dev-zram0.swap
for _ in {1..60}; do
	if grep -q '^/dev/zram0[[:space:]]' /proc/swaps; then exit 0; fi
	sleep 1
done
echo "zram0 did not start from /usr/lib/systemd/zram-generator.conf" >&2
exit 1
DEFAULT
}

# TestMemoryRebootIntegration: a switch to zram, a reboot, which must keep the
# swap file off, and a switch back.
reboot_check() {
	run_test memory '^TestMemoryRebootIntegration$' 10m LINUXIO_RUN_MEMORY_INTEGRATION=1 LINUXIO_MEMORY_REBOOT_PHASE=before
	reboot_vm
	run_test memory '^TestMemoryRebootIntegration$' 10m LINUXIO_RUN_MEMORY_INTEGRATION=1 LINUXIO_MEMORY_REBOOT_PHASE=after
}

# link_swap_file on|off has fstab name the swap file through the link
# /swap-link, in / like the file, or by its own path again. Off also undoes a
# cut run: what holds the link's unit off at boot, LinuxIO's drop-in or the
# mask an older run left, moves to the file's unit.
link_swap_file() {
	vm_ssh sudo bash -s -- "$1" <<'LINK'
set -euo pipefail
if [[ $1 == on ]]; then
	ln -sfn /swap.img /swap-link
	sed -i 's|^/swap.img\([[:space:]]\)|/swap-link\1|' /etc/fstab
	systemctl daemon-reload
	exit 0
fi
link=/etc/systemd/system/swap\\x2dlink.swap
if grep -q '^/swap-link[[:space:]]' /etc/fstab; then
	masked=$(systemctl is-enabled 'swap\x2dlink.swap' 2>/dev/null || true)
	systemctl unmask 'swap\x2dlink.swap'
	sed -i 's|^/swap-link\([[:space:]]\)|/swap.img\1|' /etc/fstab
	if [[ $masked == masked ]]; then systemctl mask swap.img.swap; fi
	if [[ -e $link.d/90-linuxio-memory.conf ]]; then
		mkdir -p /etc/systemd/system/swap.img.swap.d
		mv "$link.d/90-linuxio-memory.conf" /etc/systemd/system/swap.img.swap.d/
	fi
	systemctl daemon-reload
fi
rm -rf "$link.d"
rm -f /swap-link
LINK
}

# TestMemoryStatusWithoutRootIntegration reads the status as a user without
# sudo, as the bridge of such a session does, with no sbin folder in PATH and
# TuneD running, while a switch to zram, made as root with the job host's
# umask (TestMemorySwitchIntegration), holds the swap file off; the host goes
# back to disk after it. TuneD is installed and started for this alone and
# stopped and disabled again, since its profile sets swappiness; the user and
# the copy of the test binary it runs are removed.
status_without_root() {
	vm_ssh test -x /usr/sbin/tuned-adm </dev/null || vm_install tuned
	run_test memory '^TestMemorySwitchIntegration$' 10m LINUXIO_RUN_MEMORY_INTEGRATION=1 LINUXIO_MEMORY_SWITCH_TO=zram
	vm_ssh sudo bash -s -- "${LINUXIO_VM_ZRAM_PROVIDER:-$ZRAM_DEFAULT}" "$VM_USER" <<'STATUS'
set -euo pipefail
user=linuxio-e2e-nosudo test=/var/tmp/linuxio-memory-status.test
trap 'systemctl disable --now tuned.service >/dev/null 2>&1 || true; userdel "$user" 2>/dev/null || true; rm -f "$test"' EXIT
id "$user" >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin "$user"
install -m 0755 "/home/$2/memory.test" "$test"
systemctl start tuned.service
profile=""
for _ in {1..30}; do
	profile=$(tuned-adm active 2>/dev/null | sed -n 's/^Current active profile: //p')
	if [[ -n $profile ]]; then break; fi
	sleep 1
done
cd /
sudo -u "$user" env -i PATH=/usr/bin:/bin LINUXIO_RUN_MEMORY_STATUS_WITHOUT_ROOT=1 LINUXIO_MEMORY_EXPECT_HELD_OFF=1 \
	LINUXIO_MEMORY_TUNED_PROFILE="$profile" LINUXIO_MEMORY_ZRAM_PROVIDER="$1" \
	"$test" -test.run '^TestMemoryStatusWithoutRootIntegration$' -test.count=1 -test.v -test.timeout=2m
STATUS
	run_test memory '^TestMemorySwitchIntegration$' 10m LINUXIO_RUN_MEMORY_INTEGRATION=1 LINUXIO_MEMORY_SWITCH_TO=disk
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

# The profile's zram tool unless LINUXIO_VM_ZRAM_PROVIDER names one: zram-tools
# on Ubuntu, zram-generator on Fedora, which has no zram-tools. On Ubuntu
# zram-generator is the systemd-zram-generator package, the Fedora and RHEL
# path; LinuxIO uses it only when zram-tools is absent, so that needs a VM
# without zram-tools.
zram_install() {
	case ${LINUXIO_VM_ZRAM_PROVIDER:-$ZRAM_DEFAULT} in
	zram-generator)
		if vm_ssh test -x /usr/sbin/zramswap </dev/null; then
			die "zram-tools is installed and LinuxIO prefers it: run make vm-reset first"
		fi
		vm_ssh test -x /usr/lib/systemd/system-generators/zram-generator </dev/null || vm_install "$GENERATOR_PACKAGE"
		;;
	zram-tools)
		[[ $PKG == apt ]] || die "zram-tools is a Debian package: on ${DISTRO} use ZRAM_PROVIDER=zram-generator"
		if ! vm_ssh test -x /usr/sbin/zramswap </dev/null; then
			vm_install zram-tools
			# The package starts zramswap.service, which the test does itself.
			vm_ssh sudo systemctl disable --now zramswap.service </dev/null
		fi
		;;
	*) die "ZRAM_PROVIDER must be zram-tools or zram-generator" ;;
	esac
}

# TestMemoryIntegration starts from disk, so with zram-generator a zram device
# the generator swaps on from boot (Fedora's default zram0) is switched off. A
# stop alone does not hold: while a configuration names the device, systemd
# starts it again as soon as it is reset. So it is switched off the way Fedora
# documents, which is also where LinuxIO's disk mode leaves it: an empty
# /etc/systemd/zram-generator.conf, a reload, then the stop. The swap stops
# before its setup service: with no device configured nothing orders the two,
# and a reset while zram still swaps fails (EBUSY) and leaves the device set
# up, which the next zram start cannot configure. A device left that way, set
# up and not swapping, with its setup service stopped, is reset through sysfs
# as the generator's stop would have.
zram_default_off() {
	[[ ${LINUXIO_VM_ZRAM_PROVIDER:-$ZRAM_DEFAULT} == zram-generator ]] || return 0
	vm_ssh sudo bash -s <<'ZRAMOFF'
set -euo pipefail
zram0_set_up() { [[ -e /sys/block/zram0/disksize && $(</sys/block/zram0/disksize) != 0 ]]; }
if grep -q '^/dev/zram' /proc/swaps || zram0_set_up; then
	: >/etc/systemd/zram-generator.conf
	systemctl daemon-reload
	# With no device configured the swap unit is loaded only while it swaps.
	if grep -q '^/dev/zram0[[:space:]]' /proc/swaps; then systemctl stop dev-zram0.swap; fi
	systemctl stop systemd-zram-setup@zram0.service
	if zram0_set_up; then echo 1 >/sys/block/zram0/reset; fi
	sleep 2
	if grep -q '^/dev/zram' /proc/swaps || zram0_set_up; then
		echo "zram0 is still set up after it was switched off:" >&2
		cat /proc/swaps >&2
		exit 1
	fi
fi
ZRAMOFF
}

drain_setup() {
	vdb_by_path
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

# vdb_by_uuid has fstab name vdb by UUID=, whose unit,
# dev-disk-by\x2duuid-UUID.swap, is an alias of dev-vdb.swap
# (TestMemoryDrainAliasIntegration); vdb_by_path names it /dev/vdb again, also
# after a cut run.
vdb_by_uuid() { vdb_fstab_source uuid; }
vdb_by_path() { vdb_fstab_source path; }

vdb_fstab_source() {
	vm_ssh sudo bash -s -- "$1" <<'SOURCE'
set -euo pipefail
uuid=$(blkid -o value -s UUID /dev/vdb || true)
[[ -n $uuid ]] || exit 0
if [[ $1 == uuid ]]; then
	sed -i "s|^/dev/vdb\([[:space:]]\)|UUID=${uuid}\1|" /etc/fstab
elif grep -q "^UUID=${uuid}[[:space:]]" /etc/fstab; then
	sed -i "s|^UUID=${uuid}\([[:space:]]\)|/dev/vdb\1|" /etc/fstab
else
	exit 0
fi
systemctl daemon-reload
SOURCE
}

# TestInstallByNameRefreshesAColdCacheIntegration (bridge/handlers/packages)
# installs PACKAGEKIT_PACKAGE by name through PackageKit while its cache is
# cold, as on a host where nothing refreshed it, and expects one refresh of
# the package lists. GNU hello is small, in both distributions' main
# repositories, and nothing else needs it. The package is removed before and
# after, also after a cut run. The cache is made cold with PackageKit
# stopped, each way the test VM showed it to find nothing. On Ubuntu its apt
# backend reads apt's lists and binary caches, which are removed (a later
# apt-get update makes them again). On Fedora its dnf backend keeps a cache
# of its own under /var/cache/PackageKit and fetches one when there is none,
# but after a dnf transaction made outside it, here installing and removing
# the package, it finds nothing until a refresh.
readonly PACKAGEKIT_PACKAGE=hello

suite_packagekit() {
	push_test "$1" packages
	packagekit_remove
	vm_ssh sudo bash -s -- "$PKG" "$PACKAGEKIT_PACKAGE" <<'COLD'
set -euo pipefail
systemctl stop packagekit.service
if [[ $1 == apt ]]; then
	find /var/lib/apt/lists -mindepth 1 -maxdepth 1 ! -name lock ! -name partial -delete
	rm -f /var/cache/apt/pkgcache.bin /var/cache/apt/srcpkgcache.bin
else
	find /var/cache/PackageKit -mindepth 1 -maxdepth 1 -exec rm -rf {} +
	dnf install -y -q "$2" >/dev/null
	dnf remove -y -q "$2" >/dev/null
fi
COLD
	trap packagekit_remove EXIT
	run_test packages '^TestInstallByNameRefreshesAColdCacheIntegration$' 10m \
		LINUXIO_RUN_PACKAGEKIT_INTEGRATION=1 LINUXIO_PACKAGEKIT_TEST_PACKAGE="$PACKAGEKIT_PACKAGE"
	trap - EXIT
	packagekit_remove
}

# packagekit_remove takes PACKAGEKIT_PACKAGE out of the VM, if it is there.
packagekit_remove() {
	case $PKG in
	apt) vm_ssh "! dpkg -s $PACKAGEKIT_PACKAGE >/dev/null 2>&1 || sudo env DEBIAN_FRONTEND=noninteractive apt-get remove -y -qq $PACKAGEKIT_PACKAGE >/dev/null" </dev/null ;;
	dnf) vm_ssh "! rpm -q $PACKAGEKIT_PACKAGE >/dev/null 2>&1 || sudo dnf remove -y -q $PACKAGEKIT_PACKAGE >/dev/null" </dev/null ;;
	esac
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
# which pull alpine images from Docker Hub. Ubuntu's docker.io and Fedora's
# moby-engine come without the Compose and buildx plugins, which the seed
# installs (docker-compose-v2 or docker-compose, and docker-buildx); a VM
# created before the seed did gets them here, so every VM builds through
# Bake, as Compose does on a host that has buildx.
suite_docker() {
	vm_ssh sudo docker compose version >/dev/null 2>&1 </dev/null || vm_install "$COMPOSE_PACKAGE"
	vm_ssh sudo docker buildx version >/dev/null 2>&1 </dev/null || vm_install "$BUILDX_PACKAGE"
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

# The seconds until this boot is 40 s old. libvirt's dnsmasq pings an address
# for 3 s before it offers it, longer than the initrd's DHCP client lives, but
# not again within 30 s of the last ping. A boot sooner than that after the
# last one (seen with a reboot 9 s and one 27 s into a boot) lets the initrd
# take the VM's address, the system gets the next one, and a spec's browser is
# left on an address nothing answers.
boot_age_left() {
	local up
	up="$(vm_ssh cut -d. -f1 /proc/uptime </dev/null)" || die "cannot read the uptime of ${DOMAIN}"
	echo $((up < 40 ? 40 - up : 0))
}

# An orderly reboot: the guest's systemd stops its services first. A boot
# less than 40 s old is waited for first (boot_age_left).
reboot_vm() {
	connect
	local boot left
	left="$(boot_age_left)"
	sleep "$left"
	boot="$(boot_id)"
	v reboot "$DOMAIN" >/dev/null
	wait_for_boot "$boot"
}

# A power loss: the domain stops at once, then boots again, staying off for
# what a boot less than 40 s old lacks (boot_age_left).
power_cut() {
	connect
	local boot left
	boot="$(boot_id)"
	left="$(boot_age_left)"
	v destroy "$DOMAIN" >/dev/null
	sleep "$left"
	start_domain
	wait_for_boot "$boot"
}

# An orderly shutdown, SECONDS with the domain off, then a boot: what a timer
# does with the time that passed while the host was down. The domain stays
# off longer when a boot less than 40 s old lacks more (boot_age_left).
off_for() {
	local seconds=${1:-}
	[[ $seconds =~ ^[0-9]+$ ]] || die "usage: off-for SECONDS"
	connect
	local boot left
	boot="$(boot_id)"
	left="$(boot_age_left)"
	v shutdown "$DOMAIN" >/dev/null
	wait_for_shut_off 180
	sleep "$((seconds > left ? seconds : left))"
	start_domain
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
	disk_serials
	rm -f "${STATE}/installed"
	say "${DOMAIN} reverted to ${SNAPSHOT} and stopped; the next VM target starts it"
}

# Gives the data disks of the stopped domain their serials (serial_of) when
# its definition has none: a VM created before they existed, whose clean
# snapshot keeps the definition a revert brings back. make vm-update takes
# the snapshot again with them.
disk_serials() {
	local xml dev
	xml="$(v dumpxml --inactive "$DOMAIN")" || die "cannot read the definition of ${DOMAIN}"
	[[ $xml != *"<serial>$(serial_of vdb)</serial>"* ]] || return 0
	for dev in vdb vdc vdd vde; do
		xml="${xml/"<target dev='${dev}' bus='virtio'/>"/"<target dev='${dev}' bus='virtio'/><serial>$(serial_of "$dev")</serial>"}"
	done
	v define /dev/stdin >/dev/null <<<"$xml" || die "cannot give the data disks of ${DOMAIN} their serials"
	say "${DOMAIN}: the data disks have serials now"
}

# Bring the clean snapshot up to date: revert to it, upgrade every package,
# switch the unattended apt timers off (the seed does that for a new VM; one
# created before it did still has them on) and take the snapshot again. An
# unattended upgrade that a power-cut check interrupts leaves dpkg broken, and
# a long list of waiting updates slows the Updates page the specs open. Fedora
# upgrades nothing by itself; its seed switches dnf-makecache.timer off.
update() {
	reset
	connect
	say "upgrading the packages of ${DOMAIN} (several minutes)"
	case $PKG in
	apt)
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
		;;
	dnf) vm_ssh 'sudo dnf upgrade -y -q --refresh && sync' </dev/null ;;
	esac
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
	rm -f "${STATE}/known_hosts" "${STATE}/installed"
	say "${DOMAIN} and its volumes are gone; the image cache stays in ${IMAGE_DIR}"
}

# The numbers of the VMs of this release that exist, 1 to 9: what the targets
# that act on every VM act on. 1 when there is none, so the command that
# follows says to create it.
existing() {
	local n
	local -a found=()
	for n in {1..9}; do
		if domain_exists "$(domain_name "$n")"; then found+=("$n"); fi
	done
	echo "${found[*]:-1}"
}

# The locks of a target's VMs. mk/vm.mk opens the lock file of each VM it may
# take, from `locks N...` (`locks all` for pick: VMs 1 to MOST_VMS), and passes N:FD for
# each to lock or pick, which take the locks on those descriptors, so the
# locks stay with make's shell for the whole recipe. Both take them without
# waiting and under the "taking" lock, so that no other target finds part of
# a set held, and print the numbers they took.
locks() {
	local n
	(($# > 0)) || die "no VM to lock"
	[[ $* != all ]] || set -- $(seq "$MOST_VMS")
	mkdir -p "$LOCK_DIR"
	for n in "$@"; do
		[[ $n =~ ^[1-9]$ ]] || die "VM '${n}' is not a number from 1 to 9"
		echo "$n ${LOCK_DIR}/$(domain_name "$n")"
	done
}

take_guard() {
	mkdir -p "$LOCK_DIR"
	exec 6>"${LOCK_DIR}/taking"
	flock -w 30 6 || die "another VM target held ${LOCK_DIR}/taking for 30 seconds"
}

# lock N:FD...: the VMs a target names, all or none.
lock_vms() {
	local pair taken
	local -a got=()
	take_guard
	for pair in "$@"; do
		if ! flock -n "${pair#*:}"; then
			for taken in "${got[@]}"; do flock -u "${taken#*:}"; done
			die "$(domain_name "${pair%%:*}") is in use by another VM target; wait for it to end"
		fi
		got+=("$pair")
	done
	echo "${got[@]%%:*}"
}

# pick WANT N:FD...: the VMs of a test run that names none. Under the taking
# lock it first tries the lock of every VM from 1 to MOST_VMS. What other
# targets hold counts as spent memory: what a running one has not touched yet
# (untouched), and 4 GiB for one that is off or still being created, since
# its target will start it. A VM that runs with no target holding it was left
# up to look at (VM_KEEP=1), so it is left alone. Then, while the memory has
# room for one more (4 GiB for each VM of the run and HOST_RESERVE_MIB for the
# host), it takes the free VMs that hold no build install put there, then
# those that do, which their reset loses, then numbers that do not exist yet,
# which prepare creates; each in order. WANT is a number, or plan for as many
# as the host's threads allow (plan:MAX for MAX at most): VM_HOST_THREADS for
# each, less one VM for each test VM that runs or that another target holds,
# and one at least. It says on stderr what it took, why no more and the
# threads and memory it counted, and fails when it took none.
pick() {
	local want=${1:-} threads others=0 room vm_kib reserve_kib avail_kib others_kib=0 vms pair n fd name kib left_kib short="" line memory cpus=""
	local -a busy=() plain=() built=() missing=() got=() new=() reverted=()
	local -A spent=() fds=()
	shift
	[[ $want =~ ^([0-9]+|plan(:[0-9]+)?)$ ]] || die "usage: pick WANT|plan[:MAX] N:FD..."
	vm_kib=$((VM_MEMORY_GIB * 1048576)) reserve_kib=$((HOST_RESERVE_MIB * 1024))
	take_guard
	avail_kib="$(available_kib)"
	vms="$(untouched)"
	while read -r name kib; do
		[[ -n $name ]] || continue
		spent[$name]=$kib
		others_kib=$((others_kib + kib))
		others=$((others + 1))
	done <<<"$vms"
	for pair in "$@"; do
		n=${pair%%:*} fd=${pair#*:}
		name="$(domain_name "$n")"
		if ! flock -n "$fd"; then
			busy+=("$name")
			[[ -n ${spent[$name]+x} ]] || others_kib=$((others_kib + vm_kib)) others=$((others + 1))
		elif [[ -n ${spent[$name]+x} ]]; then
			flock -u "$fd"
			busy+=("${name} (running with no target: VM_KEEP=1?)")
		elif ! domain_exists "$name"; then
			fds[$n]=$fd missing+=("$n")
		elif [[ -e "$(state_of "$name")/installed" ]]; then
			fds[$n]=$fd built+=("$n")
		else
			fds[$n]=$fd plain+=("$n")
		fi
	done
	if [[ $want == plan* ]]; then
		threads="$(nproc)"
		room=$((threads / VM_HOST_THREADS - others))
		cpus="; CPUs: ${threads} threads, ${VM_HOST_THREADS} for each VM, ${others} other test VMs"
		[[ $want != plan:* ]] || room=$((${want#plan:} < room ? ${want#plan:} : room))
		want=$room
	fi
	((want >= 1)) || want=1
	for n in "${plain[@]}" "${built[@]}" "${missing[@]}"; do
		((${#got[@]} < want)) || break
		left_kib=$((avail_kib - others_kib - reserve_kib - ${#got[@]} * vm_kib))
		if ((left_kib < vm_kib)); then
			short="another VM needs $(gib "$vm_kib") and $(gib $((left_kib > 0 ? left_kib : 0))) is left"
			break
		fi
		got+=("$n")
		[[ " ${missing[*]} " != *" $n "* ]] || new+=("$n")
		[[ " ${built[*]} " != *" $n "* ]] || reverted+=("$n")
	done
	for n in "${!fds[@]}"; do
		[[ " ${got[*]} " == *" $n "* ]] || flock -u "${fds[$n]}"
	done
	exec 6>&-
	memory="$(gib "$avail_kib") available, less $(gib "$others_kib") for other test VMs and $(gib "$reserve_kib") kept for the host"
	((${#got[@]} > 0)) || die "no VM for this run:${busy[*]:+ in use: ${busy[*]};}${short:+ ${short}} (${memory})"
	line="VM ${got[*]} for this run${new[*]:+ (created first: ${new[*]})}${reverted[*]:+ (VM ${reverted[*]} held an installed build, which the revert removes)}"
	((${#got[@]} == want)) || line+=", ${#got[@]} of ${want} wanted${short:+ (${short})}"
	((${#busy[@]} == 0)) || line+="; in use: ${busy[*]}"
	line+="${cpus}; memory: ${memory}"
	if ((${#got[@]} == 1)); then line+="; VM=${got[0]} names it again"; else line+="; VM='${got[*]}' names them again"; fi
	say "$line"
	echo "${got[*]}"
}

# Runs a command of this script for the VMs VMS ("2 3") side by side. Each VM's
# output goes to LOG_DIR/vm-N-COMMAND.log. On a terminal one row per VM shows
# its steps as they end and the one running; elsewhere a line per step that
# ends. A VM that fails has the end of its log shown, and each fails with it.
# A shell without job control makes a background command ignore Ctrl-C, which
# would leave these running after the target was stopped: each takes it back.
#
# On a terminal the rows go on from one each to the next within a make recipe
# (the same parent shell), so a VM keeps one row from prepare to stop: each
# that ends with every VM passed leaves its rows' steps in ROWS, and the next
# draws over those rows with the steps added. A failure prints below the rows
# and a command run outside each may print too, so either ends them. The
# cursor is hidden while the rows are redrawn.
each() {
	local n status_dir failed=0 begun=$EPOCHSECONDS alive tty=0 drawn=0 cursor=: columns
	local -a vms=() codes=() ended=() logs=() shown=()
	read -ra vms <<<"${1:-}"
	[[ ${#vms[@]} -gt 0 && $# -ge 2 ]] || die "usage: each VMS COMMAND [ARGS]"
	for n in "${vms[@]}"; do [[ $n =~ ^[1-9]$ ]] || die "VM '${n}' is not a number from 1 to 9"; done
	shift
	[[ -t 2 ]] && tty=1
	((tty == 0)) || cursor="printf '\\e[?25h' >&2"
	mkdir -p "$LOG_DIR"
	status_dir="$(mktemp -d)"
	# shellcheck disable=SC2064 # the directory is fixed now
	trap "rm -f -- '${status_dir}'/[1-9] '${status_dir}'/[1-9].code '${status_dir}'/[1-9].before; rmdir -- '${status_dir}'; ${cursor}" EXIT
	if ((tty)); then
		printf '\e[?25l' >&2
		read -r _ columns < <(stty size </dev/tty 2>/dev/null) || true
		ROW_WIDTH=$((${columns:-0} > 0 ? columns - 1 : 80))
		rows_take "${vms[*]}" "$status_dir" && drawn=1
	fi
	for n in "${vms[@]}"; do
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
		for n in "${vms[@]}"; do
			if [[ -z ${codes[n]:-} && -s "${status_dir}/${n}.code" ]]; then
				codes[n]=$(<"${status_dir}/${n}.code")
				ended[n]=$EPOCHSECONDS
			fi
			[[ -n ${codes[n]:-} ]] || alive=1
		done
		if ((tty)); then
			((drawn == 0)) || printf '\e[%dA' "${#vms[@]}" >&2
			for n in "${vms[@]}"; do
				printf '\e[2K%s\n' "$(vm_row "$n" "$1" "${status_dir}/${n}" "${codes[n]:-}" "$begun" "${ended[n]:-}")" >&2
			done
			drawn=1
		else
			for n in "${vms[@]}"; do
				shown[n]=$(vm_new_lines "$n" "$1" "${status_dir}/${n}" "${codes[n]:-}" "$begun" "${ended[n]:-}" "${shown[n]}")
			done
		fi
		((alive)) || break
		sleep 1
	done
	wait
	for n in "${vms[@]}"; do
		((codes[n] == 0)) && continue
		failed=1
		say "$(vm_tag "$n") failed; the end of ${logs[n]#"$REPO_ROOT"/}:"
		tail -n 25 "${logs[n]}" | sed 's/^/    /' >&2
	done
	# Another target's command in this checkout may remove ROWS meanwhile:
	# the next each then starts new rows, and this one has not failed.
	if ((tty && failed == 0)); then
		{
			mkdir -p "$ROWS" &&
				for n in "${vms[@]}"; do
					vm_steps "$n" "$1" "${status_dir}/${n}" "${codes[n]}" "$begun" "${ended[n]}" >"${ROWS}/${n}"
				done &&
				echo "$PPID ${vms[*]}" >"${ROWS}/owner"
		} 2>/dev/null || true
	fi
	return "$failed"
}

# rows_take VMS DIR: when the each before this one in the same recipe left
# rows for VMS, gives each VM their steps in DIR/N.before and says so. They are
# taken once: an each that is stopped part-way leaves none for the next.
rows_take() {
	local owner n
	owner="$(cat "${ROWS}/owner" 2>/dev/null)" || return 1
	rm -f "${ROWS}/owner"
	[[ $owner == "$PPID $1" ]] || return 1
	for n in $1; do
		cp "${ROWS}/${n}" "$2/${n}.before"
	done
}

color() { [[ -t 2 && -z ${NO_COLOR:-} ]]; }
paint() { if color; then printf '\e[%sm%s\e[0m' "$1" "$2"; else printf '%s' "$2"; fi; }
# Teal, amber, violet, blue: the colours the VM reporter gives the same VMs.
vm_tag() {
	local -r colors=(80 215 141 75)
	paint "1;38;5;${colors[($1 - 1) % 4]}" "vm$1"
}
duration() { if (($1 < 60)); then echo "${1}s"; else printf '%dm %02ds' $(($1 / 60)) $(($1 % 60)); fi; }

# The steps of VM N as "STATE NAME SECONDS" lines, in order, after those of the
# each before (FILE.before). A command that reports no steps (stop, reset,
# install) is one step of its own name. A step that was running when the VM
# failed is failed.
vm_steps() {
	local command=$2 file=$3 code=$4 begun=$5 end_epoch=$6 name state value
	local -a names=()
	local -A states=() values=()
	[[ ! -f "${file}.before" ]] || cat -- "${file}.before"
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

# A row stays on one line of the terminal, since one that wrapped would throw
# the redraw off: when it is wider than ROW_WIDTH its oldest steps give way to
# an ellipsis, and the last one always shows.
vm_row() {
	local n=$1 head state name value plain
	local -a steps=() widths=()
	local -i total i=0
	head="$(vm_tag "$n")  $(printf '%-15s' "$(domain_name "$n")")"
	plain="vm${n}  $(printf '%-15s' "$(domain_name "$n")")"
	total=${#plain}
	while read -r state name value; do
		steps+=("  $(vm_step_text "$state" "$name" "$value")")
		plain="  $(NO_COLOR=1 vm_step_text "$state" "$name" "$value")"
		widths+=("${#plain}")
		total+=${#plain}
	done < <(vm_steps "$@")
	while ((total > ROW_WIDTH && i < ${#steps[@]} - 1)); do
		# The first step to go makes room for "  …" as well.
		((total -= widths[i] - (i == 0 ? 3 : 0), i += 1))
	done
	((i == 0)) || head+="  …"
	printf '%s' "$head" "${steps[@]:i}"
	echo
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

# A command run outside each may print below the rows each drew, so it ends
# them (see each).
[[ ${1:-} == each || -n ${LINUXIO_VM_STATUS:-} ]] || rm -rf -- "$ROWS"

case "${1:-}" in
create) create ;;
ensure) ensure ;;
install) install "${2:-}" "${3:-}" ;;
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
existing) existing ;;
locks)
	shift
	locks "$@"
	;;
lock)
	shift
	lock_vms "$@"
	;;
pick)
	shift
	pick "$@"
	;;
# Exits 0 when the VM is running: make vm-ssh VM_KEEP=1 locks one that is not.
running) [[ "$(domain_state)" == running ]] ;;
prepare) prepare "${2:-}" ;;
test)
	shift
	tests "$@"
	;;
each)
	shift
	each "$@"
	;;
*) die "usage: $(basename "$0") create | ensure | prepare [BUILD_DIR] | install BUILD_DIR [REF] | start | ssh [COMMAND] | ip | password | reboot | power-cut | off-for SECONDS | throttle DISK BYTES_PER_SEC | stop | reset | update | destroy | existing | locks N...|all | lock N:FD... | pick WANT N:FD... | running | test DIR [SUITE...] | each VMS COMMAND [ARGS]" ;;
esac
