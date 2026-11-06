#!/usr/bin/env bash
# Disposable LinuxIO test VM on qemu:///system, driven by mk/vm.mk
# (docs/development.md, "Test VM"). It is hard-wired to the domain linuxio-e2e
# and its linuxio-e2e-* volumes in the default pool, and never lists, changes
# or deletes any other domain, volume or network. State (image cache, SSH key,
# login) lives in ~/.cache/linuxio-vm.
#
# Usage: linuxio-vm.sh create | install BUILD_DIR | ssh [COMMAND] | ip | reboot |
#        power-cut | throttle DISK BYTES_PER_SEC | reset | destroy
set -euo pipefail

readonly DOMAIN=linuxio-e2e
readonly VM_USER=linuxio-e2e
readonly SNAPSHOT=clean
readonly POOL=default
readonly BASE_VOL=linuxio-e2e-base.qcow2
readonly SYSTEM_VOL=linuxio-e2e-system.qcow2
readonly DATA_VOLS=(linuxio-e2e-data1.qcow2 linuxio-e2e-data2.qcow2 linuxio-e2e-data3.qcow2 linuxio-e2e-data4.qcow2)
readonly HOST_IP=192.168.122.1
readonly SEED_PORT=8399
readonly SEED_URL="http://${HOST_IP}:${SEED_PORT}/"
readonly IMAGE_BASE_URL=https://cloud-images.ubuntu.com/releases/resolute/release
readonly IMAGE_NAME=ubuntu-26.04-server-cloudimg-amd64.img
readonly STATE="${HOME}/.cache/linuxio-vm"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly REPO_ROOT
readonly SSH_OPTS=(
	-i "${STATE}/id_ed25519" -o IdentitiesOnly=yes -o BatchMode=yes
	-o UserKnownHostsFile="${STATE}/known_hosts" -o StrictHostKeyChecking=accept-new
	-o ConnectTimeout=5 -o ServerAliveInterval=15 -o LogLevel=ERROR
)
SEED_PID=""
IP=""

say() { echo "linuxio-vm: $*" >&2; }

die() {
	say "$*"
	exit 1
}

v() { virsh -c qemu:///system "$@"; }

domain_exists() { v dominfo "$DOMAIN" >/dev/null 2>&1; }

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
# reused while it still matches.
fetch_image() {
	local sum image="${STATE}/${IMAGE_NAME}"
	sum="$(curl -fsSL "${IMAGE_BASE_URL}/SHA256SUMS" | awk -v f="*${IMAGE_NAME}" '$2 == f { print $1 }')"
	[[ -n $sum ]] || die "${IMAGE_NAME} is not listed in ${IMAGE_BASE_URL}/SHA256SUMS"
	if echo "${sum}  ${image}" | sha256sum --check --status >/dev/null 2>&1; then
		return
	fi
	say "downloading ${IMAGE_NAME}"
	curl -fL --progress-bar -o "${image}.part" "${IMAGE_BASE_URL}/${IMAGE_NAME}"
	echo "${sum}  ${image}.part" | sha256sum --check --status || die "${IMAGE_NAME} does not match SHA256SUMS"
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
  <memory unit='GiB'>4</memory>
  <vcpu>4</vcpu>
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
	v vol-create-as "$POOL" "$BASE_VOL" "$(stat -c %s "${STATE}/${IMAGE_NAME}")" --format qcow2 >/dev/null
	v vol-upload --pool "$POOL" "$BASE_VOL" "${STATE}/${IMAGE_NAME}"
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

install() {
	local build_dir=${1:-}
	[[ -n $build_dir && -x "${build_dir}/linuxio" ]] || die "no LinuxIO build in '${build_dir}'"
	connect
	# --local reads packaging/, LICENSE and docs/THIRD_PARTY_NOTICES.md, and
	# finds the checkout by its Makefile.
	tar -C "$REPO_ROOT" -cf - Makefile LICENSE docs/THIRD_PARTY_NOTICES.md packaging |
		vm_ssh 'rm -rf linuxio && mkdir -p linuxio/build && tar -C linuxio -xf -'
	tar -C "$build_dir" -cf - . | vm_ssh 'tar -C linuxio/build -xf -'
	vm_ssh 'sudo ~/linuxio/packaging/scripts/install-linuxio-binaries.sh --local --build-dir ~/linuxio/build'
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

# Caps a data disk's I/O in bytes per second (0 lifts the cap), in the
# running domain and in its definition, so the cap outlasts a power cut. The
# clean snapshot holds no cap, so a reset lifts it too.
throttle() {
	local disk=${1:-} rate=${2:-}
	[[ $disk =~ ^vd[b-e]$ && $rate =~ ^[0-9]+$ ]] || die "usage: throttle vdb|vdc|vdd|vde BYTES_PER_SEC"
	connect
	v blkdeviotune "$DOMAIN" "$disk" --total-bytes-sec "$rate" --live --config >/dev/null
}

reset() {
	domain_exists || die "${DOMAIN} does not exist; run make vm-create"
	if [[ "$(domain_state)" != "shut off" ]]; then v destroy "$DOMAIN" >/dev/null; fi
	v snapshot-revert "$DOMAIN" "$SNAPSHOT"
	say "${DOMAIN} reverted to ${SNAPSHOT} and stopped; the next VM target starts it"
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
	say "${DOMAIN} and its volumes are gone; the image cache stays in ${STATE}"
}

[[ "$(v uri 2>/dev/null)" == qemu:///system ]] || die "virsh cannot reach qemu:///system (is this user in the libvirt group?)"

case "${1:-}" in
create) create ;;
install) install "${2:-}" ;;
ssh)
	connect
	shift
	vm_ssh "$@"
	;;
ip)
	connect
	echo "$IP"
	;;
reboot) reboot_vm ;;
power-cut) power_cut ;;
throttle) throttle "${2:-}" "${3:-}" ;;
reset) reset ;;
destroy) destroy ;;
*) die "usage: $(basename "$0") create | install BUILD_DIR | ssh [COMMAND] | ip | reboot | power-cut | throttle DISK BYTES_PER_SEC | reset | destroy" ;;
esac
