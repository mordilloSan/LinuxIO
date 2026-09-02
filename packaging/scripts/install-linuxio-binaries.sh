#!/usr/bin/env bash
# =============================================================================
# LinuxIO Full Installer
# Downloads and installs LinuxIO binaries, systemd services, PAM, and config
#  2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
trap 'echo -e "\e[0m"; exit 1' INT

# ---------- Configuration ----------
readonly REPO_OWNER="mordilloSan"
readonly REPO_NAME="LinuxIO"
readonly BIN_DIR="/usr/local/bin"
readonly SYSTEMD_DIR="/etc/systemd/system"
readonly PAM_DIR="/etc/pam.d"
readonly CONFIG_DIR="/etc/linuxio"
readonly DATA_DIR="/var/lib/linuxio"
readonly DOC_DIR="/usr/share/doc/linuxio"
readonly STAGING="/tmp/linuxio-install-$$"
readonly INDEXER_TIMER_UNIT_NAME="linuxio-indexer-index.timer"
readonly MINIMUM_INDEXER_RELEASE="v0.27.0"
ENABLE_INDEXER_TIMER=0
# Recovery-asset policy: a versioned install downloads immutable release
# binaries, but intentionally fetches current-main configuration, PAM, MOTD,
# and systemd packaging assets. This lets a maintainer repair an installer or
# service-definition bug for an already-published release without rebuilding
# or republishing its binaries. Current-main assets must remain compatible
# with supported historical binaries.
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
	((major > 0 || (major == 0 && minor >= 27)))
}

require_release_version() {
	local requested="$1"
	if [[ -n "$requested" ]] && ! release_version_supported "$requested"; then
		Show 1 "LinuxIO releases before ${MINIMUM_INDEXER_RELEASE} are not supported by this installer"
	fi
}

release_binary_names() {
	printf '%s\n' \
		linuxio \
		linuxio-webserver \
		linuxio-bridge \
		linuxio-auth \
		linuxio-docker-update \
		linuxio-indexer
}

release_download_assets() {
	release_binary_names
	printf '%s\n' LICENSE THIRD_PARTY_NOTICES.md SHA256SUMS
}

release_systemd_units() {
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
		linuxio-indexer-index.timer
}

atomic_replace_file() {
	local src="$1"
	local dst="$2"
	local mode="$3"
	local owner="${4:-}"
	local tmp

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

	mkdir -p "$STAGING"

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

install_binaries() {
	Show 2 "Installing binaries to ${BOLD}${BIN_DIR}${COLOUR_RESET}"

	mkdir -p "$BIN_DIR"

	local binaries
	mapfile -t binaries < <(release_binary_names)

	for binary in "${binaries[@]}"; do
		local src="${STAGING}/${binary}"
		local dst="${BIN_DIR}/${binary}"
		local mode=0755

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
		local dst="${BIN_DIR}/${binary}"
		if [[ ! -x "$dst" ]]; then
			Show 1 "${binary} is not executable"
		fi
	done

	Show 0 "All binaries installed"
	return 0
}

install_license_files() {
	Show 2 "Installing licenses to ${BOLD}${DOC_DIR}${COLOUR_RESET}"
	mkdir -p "$DOC_DIR"
	if ! atomic_replace_file "${STAGING}/LICENSE" "${DOC_DIR}/LICENSE" 0644 root:root; then
		Show 1 "Failed to install license"
	fi
	if ! atomic_replace_file "${STAGING}/THIRD_PARTY_NOTICES.md" "${DOC_DIR}/THIRD_PARTY_NOTICES.md" 0644 root:root; then
		Show 1 "Failed to install third-party notices"
	fi
	Show 0 "Licenses installed"
}

# ---------- Configuration Functions ----------

install_config_files() {
	Show 2 "Installing configuration files..."
	mkdir -p "$STAGING"

	if [[ ! -d "$CONFIG_DIR" ]]; then
		mkdir -p "$CONFIG_DIR"
		chown root:root "$CONFIG_DIR"
		chmod 0755 "$CONFIG_DIR"
	fi

	local disallowed_file="${CONFIG_DIR}/disallowed-users"
	if [[ ! -f "$disallowed_file" ]]; then
		Show 2 "Downloading disallowed-users..."
		if ! curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/etc/linuxio/disallowed-users" -o "$disallowed_file"; then
			Show 1 "Failed to download disallowed-users"
		fi
		chown root:root "$disallowed_file"
		chmod 0644 "$disallowed_file"
		Show 0 "Created ${disallowed_file}"
	else
		Show 0 "${disallowed_file} already exists (not overwriting)"
	fi

	local indexer_dir="${CONFIG_DIR}/indexer"
	if [[ ! -d "$indexer_dir" ]]; then
		mkdir -p "$indexer_dir"
		chown root:root "$indexer_dir"
		chmod 0755 "$indexer_dir"
	fi
	local indexer_file="${indexer_dir}/config.yaml"
	if [[ ! -f "$indexer_file" ]]; then
		Show 2 "Downloading indexer configuration..."
		local indexer_staged="${STAGING}/indexer-config.yaml"
		if ! curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/etc/linuxio/indexer/config.yaml" -o "$indexer_staged"; then
			Show 1 "Failed to download indexer configuration"
		fi
		if ! atomic_replace_file "$indexer_staged" "$indexer_file" 0644 root:root; then
			Show 1 "Failed to install indexer configuration"
		fi
		Show 0 "Created ${indexer_file}"
	else
		Show 0 "${indexer_file} already exists (not overwriting)"
	fi

	return 0
}

install_pam_config() {
	Show 2 "Installing PAM configuration..."

	local pam_file="${PAM_DIR}/linuxio"

	if ! curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/etc/pam.d/linuxio" -o "$pam_file"; then
		Show 1 "Failed to download PAM configuration"
	fi

	chown root:root "$pam_file"
	chmod 0644 "$pam_file"
	Show 0 "PAM configuration installed"

	return 0
}

# Drop the Avahi service file so LinuxIO advertises itself on the LAN as
# <hostname>.local once avahi-daemon is running. The file is harmless when
# Avahi isn't installed — it just sits in /etc/avahi/services/ until it is.
install_avahi_service() {
	Show 2 "Installing Avahi service file..."

	local avahi_dir="/etc/avahi/services"
	local avahi_file="${avahi_dir}/linuxio.service"

	mkdir -p "$avahi_dir"

	if ! curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/etc/avahi/services/linuxio.service" -o "$avahi_file"; then
		Show 3 "Failed to download Avahi service file — mDNS advertisement skipped"
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
	mapfile -t units < <(release_systemd_units)
	for file in "${units[@]}"; do
		Show 2 "Downloading ${file}..."
		local staged_unit="${STAGING}/${file}"
		if ! curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/systemd/${file}" -o "$staged_unit"; then
			Show 1 "Failed to download ${file}"
		fi
		if ! atomic_replace_file "$staged_unit" "${SYSTEMD_DIR}/${file}" 0644 root:root; then
			Show 1 "Failed to install ${file}"
		fi
		Show 0 "Installed ${file}"
	done

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
	if ! curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/scripts/update-issue" -o /usr/share/linuxio/issue/update-issue; then
		Show 3 "Failed to download issue script (non-critical)"
	else
		chmod 0755 /usr/share/linuxio/issue/update-issue
		# Wire the banner into the distro's MOTD mechanism:
		# - Debian/Ubuntu/Mint render /etc/update-motd.d/* into /run/motd.dynamic
		# - Fedora/RHEL-style pam_motd reads /etc/motd.d directly
		if [[ -d /etc/update-motd.d ]]; then
			if curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/etc/update-motd.d/60-linuxio" -o /etc/update-motd.d/60-linuxio; then
				chmod 0755 /etc/update-motd.d/60-linuxio
				Show 0 "SSH login banner configured (update-motd.d)"
			else
				Show 3 "Failed to download motd script (non-critical)"
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
	if ! curl -fsSL "${CURRENT_MAIN_PACKAGING_BASE}/systemd/linuxio-tmpfiles.conf" -o /usr/lib/tmpfiles.d/linuxio.conf; then
		Show 3 "Failed to download tmpfiles.d config (non-critical)"
	else
		chmod 0644 /usr/lib/tmpfiles.d/linuxio.conf
		systemd-tmpfiles --create /usr/lib/tmpfiles.d/linuxio.conf 2>/dev/null || true
		Show 0 "tmpfiles.d configuration installed"
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
	if "${BIN_DIR}/linuxio" help >/dev/null 2>&1; then
		Show 0 "linuxio CLI: working"
	else
		Show 3 "linuxio CLI did not run successfully"
	fi

	if "${BIN_DIR}/linuxio-webserver" >/dev/null 2>&1; then
		local version
		version=$("${BIN_DIR}/linuxio-webserver" 2>&1 | head -n1 || echo "unknown")
		Show 0 "${version}"
	else
		Show 3 "linuxio-webserver did not run successfully (may be arch mismatch)"
	fi

	if [[ -x "${BIN_DIR}/linuxio-bridge" ]]; then
		Show 0 "linuxio-bridge: executable"
	else
		Show 3 "linuxio-bridge: not executable"
	fi

	if [[ -x "${BIN_DIR}/linuxio-auth" ]]; then
		Show 0 "linuxio-auth: executable"
	else
		Show 3 "linuxio-auth: not executable"
	fi

	if "${BIN_DIR}/linuxio-docker-update" help >/dev/null 2>&1; then
		Show 0 "linuxio-docker-update: working"
	else
		Show 3 "linuxio-docker-update did not run successfully"
	fi

	if "${BIN_DIR}/linuxio-indexer" --version >/dev/null 2>&1; then
		Show 0 "linuxio-indexer: working"
	else
		Show 3 "linuxio-indexer did not run successfully"
	fi

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
	for config_file in "${CONFIG_DIR}/config.yaml" "${CONFIG_DIR}/indexer/config.yaml"; do
		if [[ -f "$config_file" ]]; then
			Show 0 "${config_file} installed"
		else
			Show 3 "${config_file} not found"
		fi
	done

	return 0
}

verify_dry_run_targets() {
	Show 2 "Dry run: validating writable install targets..."

	local targets=(
		"${BIN_DIR}"
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
	require_release_architecture
	require_release_version "$version"

	if [[ $dry_run -eq 1 ]]; then
		if ! verify_dry_run_targets; then
			Show 1 "Dry run failed"
		fi
		exit 0
	fi

	Header "LinuxIO ${GREY}· Binary Installer${COLOUR_RESET}"

	if [[ -n "$version" ]]; then
		Show 2 "Target version: ${BOLD}${version}${COLOUR_RESET}"
	else
		Show 2 "Target version: ${BOLD}latest${COLOUR_RESET}"
	fi

	# Step 1-3: Binaries
	if [[ $skip_binaries -eq 0 ]]; then
		Header "Step 1/5 — Download Binaries"
		if ! download_binaries "$version"; then
			Show 1 "Download failed"
		fi

		Header "Step 2/5 — Verify Checksums"
		if ! verify_checksums; then
			Show 1 "Checksum verification failed"
		fi

		Header "Step 3/5 — Install Binaries"
		if ! install_binaries; then
			Show 1 "Binary installation failed"
		fi
		if ! install_license_files; then
			Show 1 "License file installation failed"
		fi
	else
		Header "Steps 1-3 — Skipping binary installation"
	fi

	# Step 4: Configuration
	Header "Step 4/5 — Configuration"
	if ! install_config_files; then
		Show 1 "Config installation failed"
	fi
	if ! install_pam_config; then
		Show 1 "PAM configuration failed"
	fi
	install_avahi_service

	# Step 5: Systemd
	Header "Step 5/5 — Systemd Services"
	if ! install_systemd_files; then
		Show 1 "Systemd installation failed"
	fi
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

Downloads and installs LinuxIO with all required system configuration.

Arguments:
  VERSION           Optional release tag (v0.27.0 or newer). If omitted, installs latest.

Options:
  --dry-run         Validate writable install targets and exit
  --defer-restart   Do not restart services; caller will do it after the script exits
  --skip-binaries   Skip downloading and installing binaries (config only)
  -h, --help        Show this help message

What gets installed:
  - Binaries:     /usr/local/bin/linuxio, linuxio-webserver, linuxio-bridge, linuxio-auth, linuxio-docker-update, linuxio-indexer
  - Systemd:      /etc/systemd/system/linuxio*
  - Tmpfiles:     /usr/lib/tmpfiles.d/linuxio.conf (creates ${DATA_DIR} and /run/linuxio/icons)
  - PAM:          /etc/pam.d/linuxio
  - Config:       /etc/linuxio/indexer/config.yaml, /etc/linuxio/disallowed-users
  - Licenses:     ${DOC_DIR}/
  - Avahi mDNS:   /etc/avahi/services/linuxio.service (advertises <hostname>.local)

Examples:
  $(basename "$0")                 # Install latest release
  $(basename "$0") v0.27.0         # Install specific version
  $(basename "$0") --dry-run       # Validate updater write access without installing
  $(basename "$0") --skip-binaries # Only install config/systemd/pam

This script must be run as root.
EOF
}

# ---------- Entry Point ----------

if [[ -z "${BASH_SOURCE+x}" || "${BASH_SOURCE[0]-}" == "$0" ]]; then
	main "$@"
fi
