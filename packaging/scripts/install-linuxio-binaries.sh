#!/usr/bin/env bash
# =============================================================================
# LinuxIO Full Installer
# Downloads and installs LinuxIO binaries, systemd services, PAM, and config.
# With --local it installs from a local checkout instead (binaries from the
# build directory, packaging assets from the repo); localinstall.sh wraps that.
# This file must stay self-contained: it is piped to bash from main and run by
# the bridge updater with --defer-restart.
#  2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
trap 'echo -e "\e[0m"; exit 1' INT

# ---------- Configuration ----------
readonly REPO_OWNER="mordilloSan"
readonly REPO_NAME="LinuxIO"
# The admin CLI is on PATH; every other program is LinuxIO's own and is
# started by systemd, linuxio-auth or the bridge (binary_dir).
readonly CLI_DIR="/usr/bin"
readonly LIBEXEC_DIR="/usr/libexec/linuxio"
# Where earlier releases installed every program; the install removes the
# copies they left there once the new units are in place (remove_old_binaries).
readonly OLD_BIN_DIR="/usr/local/bin"
readonly SYSTEMD_DIR="/etc/systemd/system"
readonly PAM_DIR="/etc/pam.d"
readonly CONFIG_DIR="/etc/linuxio"
readonly DATA_DIR="/var/lib/linuxio"
readonly NEEDRESTART_DIR="/etc/needrestart"
# semodule writes the loaded policy here; read-only in the in-app updater.
readonly SELINUX_DIR="/etc/selinux"
# In-app updates run with the installed bridge's writable-path allowlist.
# Keep mandatory release files under paths supported since v0.27.0.
readonly SHARE_DIR="/usr/share/linuxio"
readonly DOC_DIR="${SHARE_DIR}/doc"
# Downloads and copies are staged in a directory no other user can predict or
# write to (mktemp -d: a random name, mode 0700); the EXIT trap removes it.
STAGING="$(mktemp -d /tmp/linuxio-install.XXXXXXXXXX)" || {
	echo "Cannot create a staging directory under /tmp" >&2
	exit 1
}
readonly STAGING
readonly INDEXER_TIMER_UNIT_NAME="linuxio-indexer-index.timer"
readonly BTRFS_HEALTH_TIMER_UNIT_NAME="linuxio-btrfs-health.timer"
readonly BTRFS_DEGRADED_UNIT_NAME="linuxio-btrfs-degraded.service"
# The oldest release this installer installs: the units and drop-ins it
# fetches from current main start linuxio-worker, start the webserver,
# monitoring and the Docker updater with no command word, and give the
# indexer --config, which older binaries do not have or accept.
readonly MINIMUM_SUPPORTED_RELEASE="v0.55.0"
ENABLE_INDEXER_TIMER=0
# Install source. Release mode downloads into STAGING; --local reads the build
# directory and the checkout (set in main).
LOCAL_INSTALL=0
REPO_ROOT=""
BINARY_SRC_DIR="$STAGING"
LICENSE_SRC="${STAGING}/LICENSE"
NOTICES_SRC="${STAGING}/THIRD_PARTY_NOTICES.md"
# Recovery-asset policy: a versioned install downloads immutable release
# binaries, but intentionally fetches current-main configuration, PAM, MOTD,
# and systemd packaging assets. This lets a maintainer repair an installer or
# service-definition bug for an already-published release without rebuilding
# or republishing its binaries. Current-main assets must remain compatible
# with supported historical binaries. The needrestart override is fetched the
# same way; it is Perl that needrestart runs as root and SHA256SUMS does not
# cover it, so it is installed only when its content has the shipped form
# (needrestart_override_valid).
readonly CURRENT_MAIN_PACKAGING_BASE="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging"

release_architecture_supported() {
	case "${LINUXIO_RELEASE_ARCH_OVERRIDE:-$(uname -m)}" in
	x86_64 | amd64) return 0 ;;
	*) return 1 ;;
	esac
}

require_release_architecture() {
	if ! release_architecture_supported; then
		Show 1 "Release binaries support amd64 (x86_64) only; use localinstall.sh for a host-built install"
	fi
}

release_version_supported() {
	local requested="$1"
	[[ "$requested" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || return 1

	local major=$((10#${BASH_REMATCH[1]}))
	local minor=$((10#${BASH_REMATCH[2]}))
	((major > 0 || (major == 0 && minor >= 55)))
}

# Whether the programs already in the folder $1 are of
# MINIMUM_SUPPORTED_RELEASE or newer, for an install that keeps them: v0.55.0
# is the first release whose worker answers --version.
installed_binaries_supported() {
	[[ -x "$1/linuxio-worker" ]] && "$1/linuxio-worker" --version >/dev/null 2>&1
}

require_release_version() {
	local requested="$1"
	if [[ -n "$requested" ]] && ! release_version_supported "$requested"; then
		Show 1 "LinuxIO releases before ${MINIMUM_SUPPORTED_RELEASE} are not supported by this installer"
	fi
}

# Must equal `make print-binaries`; tests/installer-fixtures.sh enforces it.
binary_names() {
	printf '%s\n' \
		linuxio \
		linuxio-webserver \
		linuxio-bridge \
		linuxio-auth \
		linuxio-docker-update \
		linuxio-indexer \
		linuxio-monitoring \
		linuxio-worker \
		linuxio-claude-relay
}

# Who may start each program. Only root starts the programs that systemd runs
# as root, so no other user can run a private word or flag of theirs. The
# rest must stay startable by the user they run as: linuxio is the admin
# tool, the bridge and the Claude relay run as the signed-in user, and the
# webserver as a dynamic user, whose ids no file can be owned by.
binary_mode() {
	case "$1" in
	linuxio-worker | linuxio-docker-update | linuxio-auth | linuxio-indexer | linuxio-monitoring)
		printf '0700\n'
		;;
	*)
		printf '0755\n'
		;;
	esac
}

# The folder a program of binary_names is installed in.
binary_dir() {
	if [[ "$1" == linuxio ]]; then
		printf '%s\n' "$CLI_DIR"
	else
		printf '%s\n' "$LIBEXEC_DIR"
	fi
}

# Remove from $1 the programs earlier releases installed there: each name of
# binary_names, and linuxio-btrfs-timer, the worker's name up to v0.53. Named
# one by one, never a pattern, so nothing else in the folder goes. A linuxio
# left there would come before the new one on PATH.
remove_old_binaries() {
	local dir="$1"
	local binary

	while IFS= read -r binary; do
		[[ -e "${dir}/${binary}" || -L "${dir}/${binary}" ]] || continue
		if rm -f -- "${dir}/${binary}"; then
			Show 0 "Removed ${dir}/${binary}"
		else
			Show 3 "Could not remove ${dir}/${binary}; remove it by hand"
		fi
	done < <(
		binary_names
		printf '%s\n' linuxio-btrfs-timer
	)
}

# The programs whose version line write_components_file records. Must name
# the binaries of version.Components (backend/common/version/components.go);
# tests/installer-fixtures.sh enforces it.
component_binaries() {
	printf '%s\n' \
		linuxio \
		linuxio-webserver \
		linuxio-bridge \
		linuxio-auth \
		linuxio-indexer \
		linuxio-monitoring \
		linuxio-worker \
		linuxio-docker-update \
		linuxio-claude-relay
}

# Record the version line of each program of component_binaries, linuxio
# found in $1 and the others in $2, into the file $3 (owner $4, when given).
# The CLI and the webserver read it and never run the programs, several of
# which only root may start.
write_components_file() {
	local cli_dir="$1"
	local libexec_dir="$2"
	local dst="$3"
	local owner="${4:-}"
	local staged="${STAGING}/components"
	local binary line

	# Readable by the webserver's user whatever umask root runs under.
	mkdir -p "$(dirname "$dst")" && chmod 0755 "$(dirname "$dst")" || return 1
	: >"$staged" || return 1
	while IFS= read -r binary; do
		if [[ "$binary" == linuxio ]]; then
			line="$("${cli_dir}/${binary}" version --self 2>/dev/null | head -n1)" || line=""
		else
			# Without the variable that names Claude Code, which the relay
			# would hand --version to.
			line="$(env -u LINUXIO_CLAUDE_BIN "${libexec_dir}/${binary}" --version 2>/dev/null | head -n1)" || line=""
		fi
		if [[ -n "$line" ]]; then
			printf '%s\n' "$line" >>"$staged"
		else
			Show 3 "${binary} did not report its version"
		fi
	done < <(component_binaries)
	atomic_replace_file "$staged" "$dst" 0644 "$owner"
}

release_download_assets() {
	binary_names
	printf '%s\n' LICENSE THIRD_PARTY_NOTICES.md SHA256SUMS
}

# Must equal the units in packaging/systemd (linuxio-tmpfiles.conf and
# linuxio-run-record.conf are not units); tests/installer-fixtures.sh enforces it.
systemd_units() {
	printf '%s\n' \
		linuxio.target \
		linuxio-webserver.socket \
		linuxio-webserver.service \
		linuxio-auth.socket \
		linuxio-auth@.service \
		linuxio-bridge-socket-user.service \
		linuxio-issue.service \
		linuxio-indexer.socket \
		linuxio-indexer.service \
		linuxio-indexer-index.service \
		linuxio-indexer-index.timer \
		linuxio-btrfs-health.service \
		linuxio-btrfs-health.timer \
		linuxio-btrfs-degraded.service \
		linuxio-btrfs-resync@.service \
		linuxio-btrfs-rebuild@.service \
		linuxio-jobs@.service \
		linuxio-jobs@.socket \
		linuxio-monitoring.service
}

# Drop-in directories, relative to ${SYSTEMD_DIR}, that each receive
# packaging/systemd/linuxio-run-record.conf as 10-linuxio-run.conf: it hooks a
# unit so every run writes its job record. The first name ends in "-.service.d"
# on purpose: systemd applies it to every unit named linuxio-schedule-*. The
# last four are the distributions' automatic update services (APT, DNF 4 and
# 5, Linux Mint), so a system update shows in the history; a host has one of
# them at most, and systemd ignores a drop-in of a unit that does not exist.
# uninstall.sh removes the drop-in from them by its name.
run_record_dropin_dirs() {
	printf '%s\n' \
		linuxio-schedule-.service.d \
		linuxio-btrfs-snapshot.service.d \
		linuxio-btrfs-replicate.service.d \
		linuxio-docker-update.service.d \
		apt-daily-upgrade.service.d \
		dnf-automatic.service.d \
		dnf5-automatic.service.d \
		mintupdate-automation-upgrade.service.d
}

# Units and drop-ins, relative to ${SYSTEMD_DIR}, that LinuxIO wrote when a
# setting was saved. They hold a command line as text, and only saving the
# setting again writes them anew.
saved_unit_files() {
	printf '%s\n' \
		linuxio-btrfs-snapshot.service \
		linuxio-btrfs-replicate.service \
		linuxio-docker-update.service \
		linuxio-webserver.service.d/verbose.conf \
		linuxio-indexer.service.d/verbose.conf \
		linuxio-monitoring.service.d/verbose.conf \
		apt-daily.service.d/linuxio-alert.conf \
		apt-daily-upgrade.service.d/linuxio-alert.conf \
		dnf-automatic.service.d/linuxio-alert.conf \
		dnf5-automatic.service.d/linuxio-alert.conf \
		mintupdate-automation-upgrade.service.d/linuxio-alert.conf
}

# Rewrite the command lines of earlier releases in the files of
# saved_unit_files inside $1. Up to v0.53 the worker was linuxio-btrfs-timer
# (and before that the linuxio words btrfs-snapshot and btrfs-health) and its
# jobs took "run", the hooks were words of linuxio, and the webserver,
# monitoring and the Docker updater took "run". Up to v0.54 the indexer's
# flag was --config-file, which it no longer takes, and the webserver's was
# written -verbose. Earlier releases installed every program in
# /usr/local/bin, which the install empties of them; the last rule runs
# after the others, so a hook that was a word of linuxio ends up naming the
# worker in /usr/libexec/linuxio, and no saved file names the CLI.
# Without this, snapshots would stop and the page and the indexer would not
# start under verbose logging until each setting was saved again. Running it
# twice changes nothing.
rewrite_saved_unit_commands() {
	local systemd_dir="$1"
	local file path

	while IFS= read -r file; do
		path="${systemd_dir}/${file}"
		[[ -f "$path" && ! -L "$path" ]] || continue
		grep -Eq '/linuxio-btrfs-timer |/linuxio (alert|scheduled-run|btrfs-snapshot|btrfs-health) |/linuxio-(webserver|monitoring|docker-update) run( |$)|/linuxio-webserver -verbose( |$)|/linuxio-indexer --config-file |/usr/local/bin/linuxio-' "$path" || continue
		if sed -i -E \
			-e 's#/linuxio btrfs-(snapshot|health) #/linuxio-btrfs-timer \1 #' \
			-e 's#/linuxio-btrfs-timer (snapshot|replicate|health|degraded) run( |$)#/linuxio-worker \1\2#' \
			-e 's#/linuxio-btrfs-timer #/linuxio-worker #' \
			-e 's#/linuxio (alert|scheduled-run) #/linuxio-worker \1 #' \
			-e 's#/(linuxio-webserver|linuxio-monitoring|linuxio-docker-update) run( |$)#/\1\2#' \
			-e 's#/linuxio-webserver -verbose( |$)#/linuxio-webserver --verbose\1#' \
			-e 's#/linuxio-indexer --config-file #/linuxio-indexer --config #' \
			-e 's#/usr/local/bin/linuxio-#/usr/libexec/linuxio/linuxio-#g' \
			"$path"; then
			Show 0 "Updated the commands in ${file}"
		else
			Show 3 "Could not update the commands in ${file}"
		fi
	done < <(saved_unit_files)
}

# Install the run-record drop-in under each directory of
# run_record_dropin_dirs inside $1 (owner $2, when given).
install_run_record_dropins() {
	local systemd_dir="$1"
	local owner="${2:-}"
	local staged="${STAGING}/linuxio-run-record.conf"
	local dir

	Show 2 "Fetching linuxio-run-record.conf..."
	if ! fetch_packaging_asset "systemd/linuxio-run-record.conf" "$staged"; then
		Show 1 "Failed to fetch linuxio-run-record.conf"
	fi
	while IFS= read -r dir; do
		if ! atomic_replace_file "$staged" "${systemd_dir}/${dir}/10-linuxio-run.conf" 0644 "$owner"; then
			Show 1 "Failed to install ${dir}/10-linuxio-run.conf"
		fi
		Show 0 "Installed ${dir}/10-linuxio-run.conf"
	done < <(run_record_dropin_dirs)
}

# Administrator-owned configuration, relative to ${CONFIG_DIR}: created when
# missing, never overwritten by any install mode.
admin_owned_config_files() {
	printf '%s\n' \
		disallowed-users \
		indexer/config.yaml \
		monitoring/config.yaml
}

# Copy (--local) or download (release) one asset under packaging/ to a path.
# Release installs deliberately take current-main packaging; see the policy above.
fetch_packaging_asset() {
	local rel="$1"
	local dest="$2"

	if [[ $LOCAL_INSTALL -eq 1 ]]; then
		cp "${REPO_ROOT}/packaging/${rel}" "$dest"
	else
		curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/${rel}" -o "$dest"
	fi
}

atomic_replace_file() {
	local src="$1"
	local dst="$2"
	local mode="$3"
	local owner="${4:-}"
	local tmp

	if ! mkdir -p "$(dirname "$dst")"; then
		return 1
	fi
	tmp=$(mktemp "${dst}.new.XXXXXX") || return 1
	if ! cp "$src" "$tmp" || ! chmod "$mode" "$tmp"; then
		rm -f "$tmp"
		return 1
	fi
	if [[ -n "$owner" ]] && ! chown "$owner" "$tmp"; then
		rm -f "$tmp"
		return 1
	fi
	if ! mv "$tmp" "$dst"; then
		rm -f "$tmp"
		return 1
	fi
}

# ---------- Colors & Styling ----------
readonly COLOUR_RESET='\e[0m'
readonly GREEN='\e[38;5;154m'
readonly BOLD='\e[1m'
readonly GREY='\e[90m'
readonly RED='\e[91m'
readonly YELLOW='\e[33m'

readonly LINE=" ${GREEN}───────────────────────────────────────────────────────${COLOUR_RESET}"
readonly BULLET=" ${GREEN}-${COLOUR_RESET}"

Show() {
	local status="$1"
	shift
	case "$status" in
	0) echo -e " ${GREY}[${GREEN}  OK  ${GREY}]${COLOUR_RESET} $*" ;;
	1)
		echo -e " ${GREY}[${RED}FAILED${GREY}]${COLOUR_RESET} $*"
		exit 1
		;;
	2) echo -e " ${GREY}[${BOLD} INFO ${GREY}]${COLOUR_RESET} $*" ;;
	3) echo -e " ${GREY}[${YELLOW}NOTICE${GREY}]${COLOUR_RESET} $*" ;;
	esac
}

Header() {
	echo ""
	echo -e "${LINE}"
	echo -e " ${BOLD} $*${COLOUR_RESET}"
	echo -e "${LINE}"
	echo ""
}

# Track if services may need a recovery start on exit
SERVICES_STOPPED=0

cleanup() {
	local exit_code=$?

	# Always try to start services if we stopped them
	if [[ $SERVICES_STOPPED -eq 1 ]]; then
		Show 2 "Ensuring LinuxIO services are started..."
		if command -v linuxio &>/dev/null; then
			linuxio start 2>/dev/null || systemctl start linuxio.target 2>/dev/null || true
		else
			systemctl start linuxio.target 2>/dev/null || true
		fi
	fi

	if [[ -d "$STAGING" ]]; then
		rm -rf "$STAGING" 2>/dev/null || true
	fi

	exit $exit_code
}

trap cleanup EXIT INT TERM

# ---------- Binary Functions ----------

download_binaries() {
	local version="$1"
	local base_url

	if [[ -n "$version" ]]; then
		base_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}"
		Show 2 "Downloading version ${BOLD}${version}${COLOUR_RESET}"
	else
		base_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download"
		Show 2 "Downloading ${BOLD}latest${COLOUR_RESET} release"
	fi

	local files
	mapfile -t files < <(release_download_assets)

	for file in "${files[@]}"; do
		Show 2 "Downloading ${file}..."
		if ! curl -fsSL "${base_url}/${file}" -o "${STAGING}/${file}"; then
			Show 1 "Failed to download ${file}"
		fi
	done

	Show 0 "All binaries downloaded"
	return 0
}

verify_checksums() {
	Show 2 "Verifying checksums..."

	local checksum_file="${STAGING}/SHA256SUMS"
	if [[ ! -f "$checksum_file" ]]; then
		Show 1 "SHA256SUMS file not found"
	fi

	cd "$STAGING" || return 1

	while IFS= read -r line; do
		[[ -z "$line" ]] && continue

		local expected_hash filename
		read -r expected_hash filename <<<"$line"

		[[ "$filename" == *.tar.gz ]] && continue
		[[ ! -f "$filename" ]] && continue

		local actual_hash
		actual_hash=$(sha256sum "$filename" | awk '{print $1}')

		if [[ "$actual_hash" != "$expected_hash" ]]; then
			Show 1 "Checksum mismatch for ${filename}"
		fi

		Show 0 "Verified ${filename}"
	done <"$checksum_file"

	cd - >/dev/null || return 1
	Show 0 "All checksums verified"
	return 0
}

verify_local_binaries() {
	local binary
	while IFS= read -r binary; do
		[[ -f "${BINARY_SRC_DIR}/${binary}" ]] || Show 1 "Binary not found: ${BINARY_SRC_DIR}/${binary}. Run 'make build-binaries' first."
		Show 0 "${binary}"
	done < <(binary_names)
}

install_binaries() {
	Show 2 "Installing binaries to ${BOLD}${CLI_DIR}${COLOUR_RESET} and ${BOLD}${LIBEXEC_DIR}${COLOUR_RESET}"

	# linuxio-auth checks that root owns this folder and only root may write
	# it before it starts the bridge from it.
	if ! mkdir -p "$LIBEXEC_DIR" || ! chown root:root "$LIBEXEC_DIR" || ! chmod 0755 "$LIBEXEC_DIR"; then
		Show 1 "Failed to create ${LIBEXEC_DIR}"
	fi

	local binaries
	mapfile -t binaries < <(binary_names)

	for binary in "${binaries[@]}"; do
		local src="${BINARY_SRC_DIR}/${binary}"
		local dst
		dst="$(binary_dir "$binary")/${binary}"
		local mode
		mode="$(binary_mode "$binary")"

		if [[ ! -f "$src" ]]; then
			Show 1 "Source file not found: ${src}"
		fi

		if ! atomic_replace_file "$src" "$dst" "$mode" root:root; then
			Show 1 "Failed to install ${binary}"
		fi

		chmod "$mode" "$dst" || Show 3 "Failed to re-apply permissions to ${dst}"
		Show 0 "Installed ${binary}"
	done

	Show 2 "Verifying installations..."
	for binary in "${binaries[@]}"; do
		local dst
		dst="$(binary_dir "$binary")/${binary}"
		if [[ ! -x "$dst" ]]; then
			Show 1 "${binary} is not executable"
		fi
	done

	Show 0 "All binaries installed"
	return 0
}

install_license_files() {
	Show 2 "Installing licenses to ${BOLD}${DOC_DIR}${COLOUR_RESET}"
	if ! atomic_replace_file "$LICENSE_SRC" "${DOC_DIR}/LICENSE" 0644 root:root; then
		Show 1 "Failed to install license"
	fi
	if ! atomic_replace_file "$NOTICES_SRC" "${DOC_DIR}/THIRD_PARTY_NOTICES.md" 0644 root:root; then
		Show 1 "Failed to install third-party notices"
	fi
	Show 0 "Licenses installed"
}

# ---------- Configuration Functions ----------

install_config_files() {
	Show 2 "Installing configuration files..."

	if [[ ! -d "$CONFIG_DIR" ]]; then
		mkdir -p "$CONFIG_DIR"
		chown root:root "$CONFIG_DIR"
		chmod 0755 "$CONFIG_DIR"
	fi

	local rel dest staged
	while IFS= read -r rel; do
		dest="${CONFIG_DIR}/${rel}"
		if [[ -f "$dest" ]]; then
			Show 0 "${dest} already exists (not overwriting)"
			continue
		fi
		Show 2 "Installing ${rel}..."
		staged="${STAGING}/config-${rel//\//-}"
		if ! fetch_packaging_asset "etc/linuxio/${rel}" "$staged"; then
			Show 1 "Failed to fetch ${rel}"
		fi
		if ! atomic_replace_file "$staged" "$dest" 0644 root:root; then
			Show 1 "Failed to install ${dest}"
		fi
		Show 0 "Created ${dest}"
	done < <(admin_owned_config_files)

	return 0
}

# The distribution family, as install-dependencies.sh decides it: this script
# runs alone (curl | bash, the updater), so it carries a copy of those
# helpers, which the installer fixtures keep identical.
DISTRO=""
detect_distro() {
	if [[ -f /etc/os-release ]]; then
		# shellcheck source=/dev/null
		. /etc/os-release
		DISTRO="${ID:-unknown}"
	elif [[ -f /etc/debian_version ]]; then
		DISTRO="debian"
	elif [[ -f /etc/redhat-release ]]; then
		DISTRO="rhel"
	else
		DISTRO="unknown"
	fi
}

is_fedora() {
	case "$DISTRO" in
	fedora | rhel | centos | rocky | almalinux) return 0 ;;
	*) return 1 ;;
	esac
}

# install_pam_config PAM_DIR [OWNER] installs the family's PAM file as
# PAM_DIR/linuxio. It goes through a staged copy, so a download cut half-way
# leaves the file there as it was.
install_pam_config() {
	local pam_dir="$1"
	local owner="${2:-}"
	local staged="${STAGING}/pam-linuxio"
	Show 2 "Installing PAM configuration..."

	# The RHEL family has password-auth and postlogin where the Debian family
	# has common-auth and the rest. The subshell keeps /etc/os-release's
	# variables out of the installer's.
	local asset="etc/pam.d/linuxio"
	if (detect_distro && is_fedora); then
		asset="etc/pam.d/linuxio.rhel"
	fi

	if ! fetch_packaging_asset "$asset" "$staged"; then
		Show 1 "Failed to fetch PAM configuration"
	fi
	if ! atomic_replace_file "$staged" "${pam_dir}/linuxio" 0644 "$owner"; then
		Show 1 "Failed to install ${pam_dir}/linuxio"
	fi
	Show 0 "PAM configuration installed"

	return 0
}

# selinux_module_valid FILE: whether FILE has the form of the module LinuxIO
# ships, packaging/selinux/linuxio.cil. The file comes from main with no
# checksum and is loaded as root into the policy store, where it stays until
# removed, so every line must be one the module uses, about LinuxIO's own
# types and paths only: its two types, their roles and attributes, rules whose
# source is linuxio_auth_t, the one type transition, the constraint exactly as
# shipped, the two file contexts, and the optional block around the banner's.
selinux_module_valid() {
	local file="$1" line constrained=0
	local own='(linuxio_auth_t|linuxio_auth_exec_t)'
	local name='[a-z][a-z0-9_]*'
	local constraint='(constrain (process (transition)) (or (neq t1 linuxio_auth_t) (or (eq u1 u2) (eq u2 unconfined_u))))'

	if LC_ALL=C grep -qa '[^[:print:][:blank:]]' "$file"; then
		return 1
	fi
	while IFS= read -r line || [[ -n "$line" ]]; do
		line="${line#"${line%%[![:blank:]]*}"}"
		if [[ $line == "$constraint" ]]; then
			constrained=1
		elif [[ $line =~ ^(;.*)?$ ||
			$line =~ ^\(type\ ${own}\)$ ||
			$line =~ ^\(roletype\ ${name}\ ${own}\)$ ||
			$line =~ ^\(typeattributeset\ ${name}\ \(${own}\)\)$ ||
			$line =~ ^\(allow\ linuxio_auth_t\ ${name}\ \(${name}\ \(${name}(\ ${name})*\)\)\)$ ||
			$line == '(typetransition initrc_domain linuxio_auth_exec_t process linuxio_auth_t)' ||
			$line == "(filecon \"${LIBEXEC_DIR}/linuxio-auth\" file (system_u object_r linuxio_auth_exec_t ((s0) (s0))))" ||
			$line == '(filecon "/run/linuxio/issue" file (system_u object_r motd_var_run_t ((s0) (s0))))' ||
			$line == '(optional linuxio_banner' ||
			$line == ')' ]]; then
			continue
		else
			return 1
		fi
	done <"$file"
	((constrained == 1))
}

# selinux_module_load_command prints the root shell command that loads the
# module this install fetched, for a notice when it could not load it.
selinux_module_load_command() {
	if [[ $LOCAL_INSTALL -eq 1 ]]; then
		printf 'semodule -i %s\n' "${REPO_ROOT}/packaging/selinux/linuxio.cil"
	else
		printf 'curl -fsSL %s -o linuxio.cil && semodule -i linuxio.cil\n' "${CURRENT_MAIN_PACKAGING_BASE}/selinux/linuxio.cil"
	fi
}

# load_selinux_module SELINUX_DIR fetches the module, checks its form, and
# loads it unless the store already holds the same file. A load takes about
# 5 s and writes the policy under SELINUX_DIR, which the in-app updater's
# sandbox has read-only (ProtectSystem=full): there a new or changed module is
# not tried, since semodule would fail and leave the store as it was. Whatever
# goes wrong, the module loaded before stays, and without one sign-in is
# refused where SELinux enforces, never more allowed than before.
load_selinux_module() {
	local selinux_dir="$1"
	local staged="${STAGING}/linuxio.cil"
	local loaded="${STAGING}/selinux-loaded"
	local keeps="A module loaded before stays; without one, sign-in is refused while SELinux enforces"

	Show 2 "Installing the SELinux module for linuxio-auth..."
	if ! fetch_packaging_asset "selinux/linuxio.cil" "$staged"; then
		Show 3 "Failed to fetch the SELinux module. ${keeps}"
		return 0
	fi
	if ! selinux_module_valid "$staged"; then
		Show 3 "The SELinux module was not loaded: the fetched file is not what LinuxIO ships. ${keeps}"
		return 0
	fi
	if mkdir -p "$loaded" && (cd "$loaded" && semodule -E linuxio >/dev/null 2>&1) &&
		cmp -s "$staged" "${loaded}/linuxio.cil"; then
		Show 0 "SELinux module linuxio is up to date"
		return 0
	fi
	if [[ ! -w $selinux_dir ]]; then
		Show 3 "The SELinux module linuxio is new or changed, and ${selinux_dir} is read-only here (an in-app update), so it was not loaded. ${keeps}. To load it, run as root: $(selinux_module_load_command)"
		return 0
	fi
	if semodule -i "$staged"; then
		Show 0 "SELinux module linuxio loaded"
	else
		Show 3 "The SELinux module linuxio did not load (semodule's message is above). ${keeps}. To try again, run as root: $(selinux_module_load_command)"
	fi
}

# install_selinux_module SELINUX_DIR gives linuxio-auth its SELinux domain,
# packaging/selinux/linuxio.cil, and relabels the auth binary
# (docs/process-systemd-architecture.md, "SELinux"). Without the domain the
# policy refuses linuxio-auth the start of the session bridge in the context
# pam_selinux sets, so nobody signs in where SELinux is enforcing.
#
# The RHEL family only, whose policy has the names the module uses. The module
# part runs where semodule lists the modules of a policy store; a host with
# SELinux disabled passes too, since semodule works on its store, and the
# relabel that switching SELinux on asks for gives the binary its type. When
# SELinux is on and semodule cannot read the store (another program holding
# its lock), the module is left as it is and the notice says so.
#
# The relabel runs whenever restorecon exists, whatever happened to the
# module: the installer replaces each binary through a new file, which is
# bin_t again, and every sign-in is refused until restorecon gives it its type.
install_selinux_module() {
	local selinux_dir="$1"

	(detect_distro && is_fedora) || return 0
	if command -v semodule >/dev/null 2>&1; then
		if semodule -l >/dev/null 2>&1; then
			load_selinux_module "$selinux_dir"
		elif command -v selinuxenabled >/dev/null 2>&1 && selinuxenabled; then
			Show 3 "The SELinux module was not checked: semodule could not read the policy store. Run this installer again once 'semodule -l' works"
		fi
	fi
	if command -v restorecon >/dev/null 2>&1; then
		if restorecon "${LIBEXEC_DIR}/linuxio-auth"; then
			Show 0 "${LIBEXEC_DIR}/linuxio-auth labelled"
		else
			Show 3 "Failed to label ${LIBEXEC_DIR}/linuxio-auth, so sign-in is refused while SELinux enforces; run restorecon ${LIBEXEC_DIR}/linuxio-auth"
		fi
	fi
	return 0
}

# Drop the Avahi service file so LinuxIO advertises itself on the LAN as
# <hostname>.local when Avahi is installed.
install_avahi_service() {
	Show 2 "Installing Avahi service file..."

	local avahi_dir="/etc/avahi/services"
	local avahi_file="${avahi_dir}/linuxio.service"

	if [[ ! -d "$avahi_dir" ]]; then
		Show 3 "Avahi not installed — mDNS advertisement skipped"
		return 0
	fi

	if ! fetch_packaging_asset "etc/avahi/services/linuxio.service" "$avahi_file"; then
		Show 3 "Failed to fetch Avahi service file — mDNS advertisement skipped"
		return 0
	fi

	chown root:root "$avahi_file"
	chmod 0644 "$avahi_file"

	if pgrep -x avahi-daemon >/dev/null 2>&1; then
		Show 0 "mDNS advertisement enabled ${GREY}(reachable at <hostname>.local)${COLOUR_RESET}"
	else
		Show 3 "Avahi not running — file installed, will activate when avahi-daemon starts"
	fi

	return 0
}

# The needrestart override is Perl that needrestart evaluates as root on every
# apt run, and a release install fetches it from main without a checksum. A
# file needrestart cannot load stops all its restarts, and a pattern that is
# too wide exempts more than LinuxIO's work, so accept a file only in the form
# the shipped one has: printable ASCII; blank lines and comments; at least one
# line that sets an override_rc entry to 0 whose pattern is, token by token,
# what the six shipped patterns use. That is "^linuxio-", lowercase words
# joined by "-", then at most one "@" or "-" followed by "\d+" or ".+", then
# "\.service$". A pattern for an always-on daemon (linuxio-webserver,
# linuxio-monitoring, linuxio-indexer, linuxio-auth), which the installer
# restarts itself, is refused. Where Perl is present, the file must also
# compile under strict, as needrestart loads it (needrestart_override_loads).
needrestart_override_valid() {
	local file="$1" line words entries=0
	local word='[abcdefghijklmnopqrstuvwxyz]+'
	# shellcheck disable=SC2016 # a literal Perl variable, not a shell expansion
	local entry='^\$nrconf\{override_rc\}->\{qr\(\^linuxio-('"$word"'(-'"$word"')*)((@|-)(\\d\+|\.\+))?\\\.service\$\)\} = 0;$'

	if LC_ALL=C grep -qa '[^[:print:][:blank:]]' "$file"; then
		return 1
	fi
	while IFS= read -r line || [[ -n "$line" ]]; do
		if [[ $line =~ $entry ]]; then
			words="${BASH_REMATCH[1]}"
			case "$words" in
			webserver | webserver-* | monitoring | monitoring-* | indexer | indexer-* | auth | auth-*) return 1 ;;
			esac
			entries=$((entries + 1))
		elif [[ ! $line =~ ^[[:blank:]]*(#.*)?$ ]]; then
			return 1
		fi
	done <"$file"
	((entries > 0)) || return 1
	if command -v perl >/dev/null 2>&1; then
		needrestart_override_loads "$file" || return 1
	fi
}

# Whether Perl compiles file $1 under strict with %nrconf declared, as
# needrestart's configuration evaluates it. perl -c compiles without running
# the file's statements; needrestart_override_valid lets no BEGIN block or
# other code through to it.
needrestart_override_loads() {
	# shellcheck disable=SC2016 # Perl source, not a shell expansion
	{ printf '%s\n' 'use strict; our %nrconf;' && cat "$1"; } | perl -c - >/dev/null 2>&1
}

# On Ubuntu, apt runs needrestart after every dpkg run, and it restarts each
# service still running a replaced binary: the job hosts an update leaves
# running would lose their jobs. Install the override that exempts them (and
# the timer-run work) into $1/conf.d, owner $2 when given, only where
# needrestart's directory $1 exists: a system without needrestart gets nothing.
# Never fatal, and a file that fails needrestart_override_valid leaves any
# installed override as it is. An in-app update runs this installer in a unit
# whose writable paths come from the bridge installed before the update; one
# without /etc/needrestart/conf.d among them (every build before this file
# existed) leaves it read-only.
install_needrestart_override() {
	local needrestart_dir="$1"
	local owner="${2:-}"
	local staged="${STAGING}/needrestart-linuxio.conf"
	local dest="${needrestart_dir}/conf.d/linuxio.conf"

	if [[ ! -d "$needrestart_dir" ]]; then
		Show 2 "needrestart not installed — no override needed"
		return 0
	fi
	Show 2 "Installing needrestart override..."
	if ! fetch_packaging_asset "etc/needrestart/conf.d/linuxio.conf" "$staged"; then
		Show 3 "Failed to fetch the needrestart override — apt runs may restart running jobs"
		return 0
	fi
	if ! needrestart_override_valid "$staged"; then
		Show 3 "The needrestart override was not installed: the fetched file is not what LinuxIO ships"
		return 0
	fi
	if ! atomic_replace_file "$staged" "$dest" 0644 "$owner"; then
		Show 3 "Could not write ${dest}, so needrestart may restart running LinuxIO work after an update; running this installer from a terminal installs it (the usual cause is an in-app update started by an older LinuxIO, which cannot write there)"
		return 0
	fi
	Show 0 "Installed ${dest}"
}

# ---------- Systemd Functions ----------

readonly LINUXIO_SOCKET_NAME="linuxio-webserver.socket"
readonly LINUXIO_PORT_MIN=8090
readonly LINUXIO_PORT_MAX=8099
SELECTED_PORT=""

linuxio_services_active() {
	systemctl is-active linuxio.target >/dev/null 2>&1 ||
		systemctl is-active linuxio-webserver.service >/dev/null 2>&1 ||
		systemctl is-active linuxio-webserver.socket >/dev/null 2>&1
}

restart_or_start_services() {
	SERVICES_STOPPED=1

	if linuxio_services_active; then
		Show 2 "Restarting LinuxIO services..."
		if systemctl restart linuxio.target; then
			Show 0 "LinuxIO services restarted"
			SERVICES_STOPPED=0
			return 0
		fi

		Show 3 "Failed to restart — cleanup will retry"
		return 1
	fi

	Show 2 "Starting LinuxIO service..."
	if systemctl start linuxio.target; then
		Show 0 "LinuxIO service started"
		SERVICES_STOPPED=0
		return 0
	fi

	Show 3 "Failed to start — cleanup will retry"
	return 1
}

linuxio_socket_candidates() {
	if [[ -n "${LINUXIO_EXISTING_SOCKET_FILE:-}" ]]; then
		printf '%s\n' "$LINUXIO_EXISTING_SOCKET_FILE"
		return 0
	fi

	printf '%s\n' \
		"${SYSTEMD_DIR}/${LINUXIO_SOCKET_NAME}" \
		"/run/systemd/system/${LINUXIO_SOCKET_NAME}" \
		"/usr/lib/systemd/system/${LINUXIO_SOCKET_NAME}" \
		"/lib/systemd/system/${LINUXIO_SOCKET_NAME}"
}

extract_linuxio_socket_port() {
	local socket_file="$1"
	local line value port

	[[ -f "$socket_file" ]] || return 1

	while IFS= read -r line; do
		[[ "$line" =~ ^[[:space:]]*ListenStream[[:space:]]*=[[:space:]]*(.*)$ ]] || continue
		value="${BASH_REMATCH[1]}"
		value="${value#"${value%%[![:space:]]*}"}"
		value="${value%%[[:space:]]*}"

		if [[ "$value" =~ ^[0-9]+$ ]]; then
			port="$value"
		elif [[ "$value" =~ :([0-9]+)$ ]]; then
			port="${BASH_REMATCH[1]}"
		else
			continue
		fi

		if [[ "$port" =~ ^[0-9]+$ ]] &&
			((10#$port >= LINUXIO_PORT_MIN && 10#$port <= LINUXIO_PORT_MAX)); then
			echo "$((10#$port))"
			return 0
		fi
	done <"$socket_file"

	return 1
}

find_existing_linuxio_port() {
	local socket_file port

	while IFS= read -r socket_file; do
		port=$(extract_linuxio_socket_port "$socket_file") || continue
		echo "$port"
		return 0
	done < <(linuxio_socket_candidates)

	return 1
}

is_port_in_use() {
	local port="$1"
	local proc

	proc=$(ss -tlnpH "sport = :${port}" 2>/dev/null || true)
	[[ -n "$proc" ]]
}

linuxio_socket_active() {
	systemctl is-active --quiet "$LINUXIO_SOCKET_NAME" 2>/dev/null
}

linuxio_socket_owns_port() {
	local port="$1"
	local listeners endpoint listener_port

	[[ "$port" =~ ^[0-9]+$ ]] || return 1
	linuxio_socket_active || return 1
	listeners=$(systemctl show --property=Listen --value "$LINUXIO_SOCKET_NAME" 2>/dev/null) || return 1

	while read -r endpoint _; do
		if [[ "$endpoint" =~ ^[0-9]+$ ]]; then
			listener_port="$endpoint"
		elif [[ "$endpoint" =~ :([0-9]+)$ ]]; then
			listener_port="${BASH_REMATCH[1]}"
		else
			continue
		fi
		if ((10#$listener_port == 10#$port)); then
			return 0
		fi
	done <<<"$listeners"

	return 1
}

find_available_port() {
	local preferred_port="${1:-}"
	local port

	if [[ "$preferred_port" =~ ^[0-9]+$ ]] &&
		((10#$preferred_port >= LINUXIO_PORT_MIN && 10#$preferred_port <= LINUXIO_PORT_MAX)); then
		if ! is_port_in_use "$preferred_port" || linuxio_socket_owns_port "$preferred_port"; then
			echo "$((10#$preferred_port))"
			return 0
		fi
	fi

	for ((port = LINUXIO_PORT_MIN; port <= LINUXIO_PORT_MAX; port++)); do
		if ! is_port_in_use "$port"; then
			echo "$port"
			return 0
		fi
	done

	return 1
}

rewrite_linuxio_socket_port() {
	local socket_file="$1"
	local port="$2"

	[[ -f "$socket_file" ]] || return 1
	[[ "$port" =~ ^[0-9]+$ ]] || return 1
	grep -Eq '^[[:space:]]*ListenStream[[:space:]]*=[[:space:]]*([^[:space:]]*:)?[0-9]+' "$socket_file" || return 1

	sed -Ei \
		"s|^([[:space:]]*ListenStream[[:space:]]*=[[:space:]]*)([^[:space:]]*:)?[0-9]+([[:space:]]*(#.*)?)$|\1\2${port}\3|" \
		"$socket_file"
}

install_systemd_files() {
	local existing_port

	Show 2 "Installing systemd service files..."
	# Capture this before downloading current-main assets overwrites the
	# installed socket. A valid existing port is part of the reinstall
	# compatibility contract and is preserved whenever possible.
	existing_port=$(find_existing_linuxio_port || true)
	if [[ ! -e "${SYSTEMD_DIR}/${INDEXER_TIMER_UNIT_NAME}" &&
		! -L "${SYSTEMD_DIR}/${INDEXER_TIMER_UNIT_NAME}" ]]; then
		ENABLE_INDEXER_TIMER=1
	fi

	local units
	mapfile -t units < <(systemd_units)
	for file in "${units[@]}"; do
		Show 2 "Fetching ${file}..."
		local staged_unit="${STAGING}/${file}"
		if ! fetch_packaging_asset "systemd/${file}" "$staged_unit"; then
			Show 1 "Failed to fetch ${file}"
		fi
		if ! atomic_replace_file "$staged_unit" "${SYSTEMD_DIR}/${file}" 0644 root:root; then
			Show 1 "Failed to install ${file}"
		fi
		Show 0 "Installed ${file}"
	done
	install_run_record_dropins "$SYSTEMD_DIR" root:root

	if ! SELECTED_PORT=$(find_available_port "$existing_port"); then
		Show 1 "No available LinuxIO port in supported range ${LINUXIO_PORT_MIN}-${LINUXIO_PORT_MAX}"
	fi
	if ! rewrite_linuxio_socket_port "${SYSTEMD_DIR}/${LINUXIO_SOCKET_NAME}" "$SELECTED_PORT"; then
		Show 1 "Could not apply selected port ${SELECTED_PORT} to ${SYSTEMD_DIR}/${LINUXIO_SOCKET_NAME}"
	fi
	if [[ -n "$existing_port" && "$SELECTED_PORT" == "$existing_port" ]]; then
		Show 0 "Preserving existing LinuxIO port ${SELECTED_PORT}"
	elif [[ -n "$existing_port" ]]; then
		Show 3 "Existing LinuxIO port ${existing_port} is unavailable; using ${BOLD}${SELECTED_PORT}${COLOUR_RESET}"
	elif [[ "$SELECTED_PORT" != "$LINUXIO_PORT_MIN" ]]; then
		Show 3 "Port ${LINUXIO_PORT_MIN} is in use, using port ${BOLD}${SELECTED_PORT}${COLOUR_RESET} instead"
	else
		Show 0 "Selected LinuxIO port ${SELECTED_PORT}"
	fi

	Show 2 "Installing SSH login banner support..."
	mkdir -p /usr/share/linuxio/issue
	if ! fetch_packaging_asset "scripts/update-issue" /usr/share/linuxio/issue/update-issue; then
		Show 3 "Failed to fetch issue script (non-critical)"
	else
		chmod 0755 /usr/share/linuxio/issue/update-issue
		# Wire the banner into the distro's MOTD mechanism:
		# - Debian/Ubuntu/Mint render /etc/update-motd.d/* into /run/motd.dynamic
		# - Fedora/RHEL-style pam_motd reads /etc/motd.d directly
		if [[ -d /etc/update-motd.d ]]; then
			if fetch_packaging_asset "etc/update-motd.d/60-linuxio" /etc/update-motd.d/60-linuxio; then
				chmod 0755 /etc/update-motd.d/60-linuxio
				Show 0 "SSH login banner configured (update-motd.d)"
			else
				Show 3 "Failed to fetch motd script (non-critical)"
			fi
		elif [[ -d /etc/motd.d ]]; then
			ln -sf ../../run/linuxio/issue /etc/motd.d/linuxio 2>/dev/null || true
			Show 0 "SSH login banner configured (motd.d)"
		else
			Show 2 "No update-motd.d or motd.d directory found, skipping login banner setup"
		fi
	fi

	Show 2 "Installing tmpfiles.d configuration..."
	mkdir -p /usr/lib/tmpfiles.d
	if ! fetch_packaging_asset "systemd/linuxio-tmpfiles.conf" /usr/lib/tmpfiles.d/linuxio.conf; then
		Show 3 "Failed to fetch tmpfiles.d config (non-critical)"
	else
		chmod 0644 /usr/lib/tmpfiles.d/linuxio.conf
		systemd-tmpfiles --create /usr/lib/tmpfiles.d/linuxio.conf 2>/dev/null || true
		Show 0 "tmpfiles.d configuration installed"
	fi
	# Pre-release builds started job hosts through a world-connectable
	# launcher socket; linuxio-auth starts them at login now. Retire it.
	if [[ -e "${SYSTEMD_DIR}/linuxio-jobs-launcher.socket" ]]; then
		systemctl disable --now linuxio-jobs-launcher.socket >/dev/null 2>&1 || true
		rm -f "${SYSTEMD_DIR}/linuxio-jobs-launcher.socket" "${SYSTEMD_DIR}/linuxio-jobs-launcher@.service"
	fi
	# The boot reconcile moved into the job host, which marks its own
	# interrupted jobs when it starts. Retire its unit.
	if [[ -e "${SYSTEMD_DIR}/linuxio-jobs-reconcile.service" ]]; then
		systemctl disable --now linuxio-jobs-reconcile.service >/dev/null 2>&1 || true
		rm -f "${SYSTEMD_DIR}/linuxio-jobs-reconcile.service"
	fi
	# The job store replaced the durable-operations store; drop its orphaned data.
	if [[ -d "${DATA_DIR}/durable-operations" ]]; then
		rm -rf "${DATA_DIR}/durable-operations"
	fi

	# libvirt hook: lets bridged VM traffic through Docker's FORWARD DROP policy
	Show 2 "Installing libvirt bridge hook..."
	mkdir -p /etc/libvirt/hooks/qemu.d
	if ! fetch_packaging_asset "libvirt/hooks/qemu.d/linuxio-bridge-netfilter" /etc/libvirt/hooks/qemu.d/linuxio-bridge-netfilter; then
		Show 3 "Failed to fetch libvirt bridge hook (non-critical)"
	else
		chmod 0755 /etc/libvirt/hooks/qemu.d/linuxio-bridge-netfilter
		# libvirt only discovers hook scripts when its daemon starts.
		systemctl try-restart libvirtd.service virtqemud.service 2>/dev/null || true
		Show 0 "libvirt bridge hook installed"
	fi

	Show 2 "Reloading systemd daemon..."
	systemctl daemon-reload
	Show 0 "Systemd daemon reloaded"

	return 0
}

enable_services() {
	Show 2 "Enabling systemd services..."

	if systemctl enable linuxio.target >/dev/null 2>&1; then
		Show 0 "Enabled linuxio.target"
	else
		Show 3 "Failed to enable linuxio.target"
	fi
	if [[ $ENABLE_INDEXER_TIMER -eq 1 ]]; then
		if systemctl enable "$INDEXER_TIMER_UNIT_NAME" >/dev/null 2>&1; then
			Show 0 "Enabled ${INDEXER_TIMER_UNIT_NAME}"
		else
			Show 3 "Failed to enable ${INDEXER_TIMER_UNIT_NAME}"
		fi
	fi
	if systemctl enable linuxio-monitoring.service >/dev/null 2>&1; then
		Show 0 "Enabled linuxio-monitoring.service"
	else
		Show 3 "Failed to enable linuxio-monitoring.service"
	fi
	# Outside linuxio.target so that updates never stop a running job. The
	# root host starts at boot to mark, alert and resume the jobs a reboot
	# interrupted; a user's host starts through the socket linuxio-auth
	# starts at their login.
	if systemctl enable linuxio-jobs@0.service >/dev/null 2>&1; then
		Show 0 "Enabled linuxio-jobs@0.service"
	else
		Show 3 "Failed to enable linuxio-jobs@0.service"
	fi
	# Pools created later get the timer when an admin lists btrfs filesystems.
	if lsblk -nro FSTYPE 2>/dev/null | grep -qx btrfs; then
		for unit in "$BTRFS_HEALTH_TIMER_UNIT_NAME" "$BTRFS_DEGRADED_UNIT_NAME"; do
			if systemctl enable "$unit" >/dev/null 2>&1; then
				Show 0 "Enabled ${unit}"
			else
				Show 3 "Failed to enable ${unit}"
			fi
		done
	fi
	# The control-plane restart (here or deferred to the caller) does not cover
	# the monitoring daemon or the login banner, so move them onto the
	# installed binary and update-issue script now.
	systemctl restart linuxio-monitoring.service >/dev/null 2>&1 || true
	systemctl restart linuxio-issue.service >/dev/null 2>&1 || true

	return 0
}

grant_journal_access() {
	[[ -z "${SUDO_USER:-}" ]] && return 0

	if ! id -nG "$SUDO_USER" | tr ' ' '\n' | grep -qxE "systemd-journal|adm"; then
		Show 2 "Granting ${SUDO_USER} journal read access..."
		usermod -aG systemd-journal "$SUDO_USER"
		Show 0 "${SUDO_USER} added to systemd-journal group ${GREY}(re-login refreshes the shell session)${COLOUR_RESET}"
	else
		Show 0 "${SUDO_USER} already has journal read access"
	fi

	return 0
}

# ---------- Verification Functions ----------

verify_installation() {
	local binary path
	while IFS= read -r binary; do
		path="$(binary_dir "$binary")/${binary}"
		case "$binary" in
		linuxio)
			if "$path" help >/dev/null 2>&1; then
				Show 0 "${binary}: working"
			else
				Show 3 "${binary} did not run successfully"
			fi
			;;
		linuxio-webserver)
			# With no argument it starts the server, so ask for its version.
			if "$path" --version >/dev/null 2>&1; then
				Show 0 "$("$path" --version 2>&1 | head -n1 || echo "unknown")"
			else
				Show 3 "${binary} did not run successfully (may be arch mismatch)"
			fi
			;;
		linuxio-indexer | linuxio-monitoring | linuxio-docker-update | linuxio-worker)
			if "$path" --version >/dev/null 2>&1; then
				Show 0 "${binary}: working"
			else
				Show 3 "${binary} did not run successfully"
			fi
			;;
		*)
			if [[ -x "$path" ]]; then
				Show 0 "${binary}: executable"
			else
				Show 3 "${binary}: not executable"
			fi
			;;
		esac
	done < <(binary_names)

	if systemctl is-enabled linuxio.target >/dev/null 2>&1; then
		Show 0 "linuxio.target is enabled"
	else
		Show 3 "linuxio.target is not enabled"
	fi

	if [[ -f "${PAM_DIR}/linuxio" ]]; then
		Show 0 "PAM configuration installed"
	else
		Show 3 "PAM configuration not found"
	fi

	if [[ -d "$CONFIG_DIR" ]]; then
		Show 0 "Configuration directory exists"
	else
		Show 3 "Configuration directory not found"
	fi
	local rel config_file
	while IFS= read -r rel; do
		config_file="${CONFIG_DIR}/${rel}"
		if [[ -f "$config_file" ]]; then
			Show 0 "${config_file} installed"
		else
			Show 3 "${config_file} not found"
		fi
	done < <(admin_owned_config_files)

	return 0
}

verify_dry_run_targets() {
	Show 2 "Dry run: validating writable install targets..."

	local targets=(
		"${CLI_DIR}"
		"${LIBEXEC_DIR}"
		"${CONFIG_DIR}"
		"${PAM_DIR}"
		"${SYSTEMD_DIR}"
		"/usr/lib/tmpfiles.d"
		"/usr/share/linuxio"
		"${DOC_DIR}"
		"${DATA_DIR}"
	)

	for target in "${targets[@]}"; do
		if [[ ! -d "$target" ]]; then
			Show 3 "${target} does not exist, skipping"
			continue
		fi

		local probe="${target}/.linuxio-dry-run-$$"
		if : >"$probe"; then
			rm -f "$probe"
			Show 0 "Writable: ${target}"
		else
			Show 1 "Not writable: ${target}"
		fi
	done

	Show 0 "Dry run completed successfully"
	return 0
}

# ---------- Main ----------

main() {
	local version=""
	local skip_binaries=0
	local dry_run=0
	local defer_restart=0
	local build_dir=""

	while [[ $# -gt 0 ]]; do
		case "$1" in
		--dry-run)
			dry_run=1
			shift
			;;
		--skip-binaries)
			skip_binaries=1
			shift
			;;
		--defer-restart)
			defer_restart=1
			shift
			;;
		--local)
			LOCAL_INSTALL=1
			shift
			;;
		--build-dir)
			[[ $# -ge 2 ]] || Show 1 "--build-dir requires a directory"
			LOCAL_INSTALL=1
			build_dir="$2"
			shift 2
			;;
		-h | --help)
			show_help
			exit 0
			;;
		v*)
			version="$1"
			shift
			;;
		*)
			shift
			;;
		esac
	done

	if [[ $EUID -ne 0 ]]; then
		Show 1 "This script must be run as root"
	fi
	if [[ $LOCAL_INSTALL -eq 1 ]]; then
		[[ -z "$version" ]] || Show 1 "A version cannot be combined with --local"
		REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
		[[ -f "${REPO_ROOT}/Makefile" ]] || Show 1 "Cannot find LinuxIO repository root at ${REPO_ROOT}"
		BINARY_SRC_DIR="${build_dir:-${REPO_ROOT}/build}"
		LICENSE_SRC="${REPO_ROOT}/LICENSE"
		NOTICES_SRC="${REPO_ROOT}/docs/THIRD_PARTY_NOTICES.md"
	else
		require_release_architecture
		require_release_version "$version"
	fi

	if [[ $dry_run -eq 1 ]]; then
		if ! verify_dry_run_targets; then
			Show 1 "Dry run failed"
		fi
		exit 0
	fi

	if [[ $LOCAL_INSTALL -eq 1 ]]; then
		Header "LinuxIO ${GREY}· Local Install${COLOUR_RESET}"
		Show 2 "Installing from ${BOLD}${REPO_ROOT}${COLOUR_RESET}"
	else
		Header "LinuxIO ${GREY}· Binary Installer${COLOUR_RESET}"
		Show 2 "Target version: ${BOLD}${version:-latest}${COLOUR_RESET}"
	fi

	# Step 1-2: Binaries, fetched and verified
	if [[ $skip_binaries -eq 0 ]]; then
		if [[ $LOCAL_INSTALL -eq 1 ]]; then
			Header "Steps 1-2 — Verify Local Build"
			verify_local_binaries
		else
			Header "Step 1/5 — Download Binaries"
			if ! download_binaries "$version"; then
				Show 1 "Download failed"
			fi

			Header "Step 2/5 — Verify Checksums"
			if ! verify_checksums; then
				Show 1 "Checksum verification failed"
			fi
		fi
	else
		Header "Steps 1-3 — Skipping binary installation"
		# The units installed below start the programs with command lines
		# that binaries before MINIMUM_SUPPORTED_RELEASE do not take.
		if ! installed_binaries_supported "$LIBEXEC_DIR"; then
			Show 1 "The binaries in ${LIBEXEC_DIR} are missing or older than ${MINIMUM_SUPPORTED_RELEASE}; run without --skip-binaries"
		fi
	fi

	# After the release assets are verified, so that a failed verification
	# installs nothing, and before any binary is replaced, so that an apt run
	# during the install already finds it.
	install_needrestart_override "$NEEDRESTART_DIR" root:root

	# Step 3: Release files
	if [[ $skip_binaries -eq 0 ]]; then
		Header "Step 3/5 — Install Release Files"
		if ! install_license_files; then
			Show 1 "License file installation failed"
		fi
		if ! install_binaries; then
			Show 1 "Binary installation failed"
		fi
	fi
	# Right after the binaries, on both paths, before any step that can end
	# the install: a replaced linuxio-auth is bin_t until relabelled, and every
	# sign-in is refused meanwhile where SELinux enforces. It is also before the
	# restart, which recreates the auth socket in the binary's domain. What
	# remains: a binary that fails inside install_binaries ends the install
	# first, and a linuxio-auth it already replaced stays bin_t until the next
	# install.
	install_selinux_module "$SELINUX_DIR"
	if ! write_components_file "$CLI_DIR" "$LIBEXEC_DIR" "${SHARE_DIR}/components" root:root; then
		Show 1 "Failed to record the installed versions"
	fi
	# Right after the binaries, so a later step that fails leaves no saved unit
	# calling a program or a word the new binaries do not have.
	rewrite_saved_unit_commands "$SYSTEMD_DIR"
	systemctl daemon-reload 2>/dev/null || true

	# Step 4: Configuration
	Header "Step 4/5 — Configuration"
	if ! install_config_files; then
		Show 1 "Config installation failed"
	fi
	if ! install_pam_config "$PAM_DIR" root:root; then
		Show 1 "PAM configuration failed"
	fi
	install_avahi_service

	# Step 5: Systemd
	Header "Step 5/5 — Systemd Services"
	if ! install_systemd_files; then
		Show 1 "Systemd installation failed"
	fi
	# Only now: until the units above replaced those of an earlier release,
	# systemd started the programs from there, so an install that ended before
	# this line leaves the old ones in place and working.
	remove_old_binaries "$OLD_BIN_DIR"
	if ! enable_services; then
		Show 3 "Some services may not be enabled"
	fi
	grant_journal_access

	# Verification
	Header "Verification"
	verify_installation
	sleep 2

	if [[ $defer_restart -eq 1 ]]; then
		Show 2 "Deferring service restart to the caller"
	else
		restart_or_start_services || true
	fi

	# Detect LAN IP for the completion banner
	local lan_ip=""
	lan_ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}')

	echo ""
	echo -e "${LINE}"
	echo -e " ${GREEN}${BOLD}Installation complete!${COLOUR_RESET}"
	echo -e "${LINE}"
	echo ""
	echo -e " ${BOLD}Dashboard:${COLOUR_RESET}"
	echo -e "${BULLET} https://localhost:${SELECTED_PORT}"
	if [[ -n "$lan_ip" ]]; then
		echo -e "${BULLET} https://${lan_ip}:${SELECTED_PORT}"
	fi
	if pgrep -x avahi-daemon >/dev/null 2>&1; then
		hn=$(hostname 2>/dev/null) || hn=""
		if [[ -n "$hn" ]]; then
			echo -e "${BULLET} https://${hn}.local:${SELECTED_PORT}  ${GREY}(via mDNS)${COLOUR_RESET}"
		fi
	fi
	echo ""
	echo -e " ${BOLD}Useful commands:${COLOUR_RESET}"
	echo -e "${BULLET} Check status:  ${GREY}linuxio status${COLOUR_RESET}"
	echo -e "${BULLET} View logs:     ${GREY}linuxio logs${COLOUR_RESET}"
	echo -e "${BULLET} All commands:  ${GREY}linuxio${COLOUR_RESET}"
	echo ""

	exit 0
}

show_help() {
	cat <<EOF
Usage: $(basename "$0") [OPTIONS] [VERSION]

Downloads and installs LinuxIO with all required system configuration, or
installs a local checkout with --local.

Arguments:
  VERSION           Optional release tag (v0.55.0 or newer). If omitted, installs latest.

Options:
  --dry-run         Validate writable install targets and exit
  --defer-restart   Do not restart services; caller will do it after the script exits
  --skip-binaries   Skip downloading and installing binaries (config only)
  --local           Install from this checkout: binaries from build/, assets from packaging/
  --build-dir DIR   Like --local, with binaries from DIR (matches make BIN_DIR)
  -h, --help        Show this help message

What gets installed:
  - CLI:          ${CLI_DIR}/linuxio
  - Programs:     ${LIBEXEC_DIR}/: linuxio-webserver, linuxio-bridge, linuxio-auth, linuxio-docker-update, linuxio-indexer, linuxio-monitoring, linuxio-worker, linuxio-claude-relay
  - Removed:      the same programs in ${OLD_BIN_DIR}, where earlier releases installed them
  - Systemd:      /etc/systemd/system/linuxio*
  - Tmpfiles:     /usr/lib/tmpfiles.d/linuxio.conf (creates ${DATA_DIR} and ${DATA_DIR}/icons)
  - PAM:          /etc/pam.d/linuxio
  - Config:       /etc/linuxio/indexer/config.yaml, /etc/linuxio/monitoring/config.yaml, /etc/linuxio/disallowed-users (created if missing, never overwritten)
  - Licenses:     ${DOC_DIR}/
  - Avahi mDNS:   /etc/avahi/services/linuxio.service (advertises <hostname>.local)
  - needrestart:  ${NEEDRESTART_DIR}/conf.d/linuxio.conf (when needrestart is installed; keeps it from restarting running jobs)

Examples:
  $(basename "$0")                 # Install latest release
  $(basename "$0") v0.55.0         # Install specific version
  $(basename "$0") --dry-run       # Validate updater write access without installing
  $(basename "$0") --skip-binaries # Only install config/systemd/pam

This script must be run as root.
EOF
}

# ---------- Entry Point ----------

if [[ -z "${BASH_SOURCE+x}" || "${BASH_SOURCE[0]-}" == "$0" ]]; then
	main "$@"
fi
