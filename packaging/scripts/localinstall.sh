#!/bin/bash
# =============================================================================
# LinuxIO Local Build and Install Script
# Builds and installs LinuxIO from local source code
#  2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

trap 'echo -e "\e[0m"; exit 1' INT

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

# ---------- Configuration ----------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly SYSTEMD_DIR="/etc/systemd/system"
readonly LINUXIO_SOCKET_NAME="linuxio-webserver.socket"
readonly INDEXER_TIMER_UNIT_NAME="linuxio-indexer-index.timer"
readonly LINUXIO_PORT_MIN=8090
readonly LINUXIO_PORT_MAX=8099
readonly DOC_DIR="/usr/share/linuxio/doc"
PORT=""

linuxio_binary_names() {
	printf '%s\n' \
		linuxio \
		linuxio-webserver \
		linuxio-bridge \
		linuxio-auth \
		linuxio-docker-update \
		linuxio-indexer \
		linuxio-monitoring
}

linuxio_systemd_units() {
	printf '%s\n' \
		linuxio.target \
		linuxio-webserver.service \
		linuxio-webserver.socket \
		linuxio-auth.socket \
		linuxio-auth@.service \
		linuxio-bridge-socket-user.service \
		linuxio-issue.service \
		linuxio-indexer.socket \
		linuxio-indexer.service \
		linuxio-indexer-index.service \
		linuxio-indexer-index.timer \
		linuxio-monitoring.service
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

# The socket port is deliberately bounded to the range supported by the
# release installer.  LINUXIO_EXISTING_SOCKET_FILE is a fixture hook and is
# otherwise unset on real hosts.
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

# ---------- Main ----------

main() {

	Header "LinuxIO ${GREY}· Local Install${COLOUR_RESET}"

	if [[ $EUID -ne 0 ]]; then
		Show 1 "This script must be run as root"
	fi

	if [[ ! -f "$REPO_ROOT/makefile" && ! -f "$REPO_ROOT/Makefile" ]]; then
		Show 1 "Cannot find LinuxIO repository root at ${REPO_ROOT}"
	fi

	cd "$REPO_ROOT"

	# ========== VERIFY BUILD ==========
	Header "Step 1/2 — Verify Binaries"

	local binaries
	mapfile -t binaries < <(linuxio_binary_names)
	for binary in "${binaries[@]}"; do
		if [[ ! -f "$REPO_ROOT/$binary" ]]; then
			Show 1 "Binary not found: ${binary}. Run 'make build' first."
		else
			Show 0 "${binary}"
		fi
	done

	# ========== INSTALL ==========
	Header "Step 2/2 — Install"
	Show 2 "Installing licenses..."
	if ! atomic_replace_file "$REPO_ROOT/LICENSE" "${DOC_DIR}/LICENSE" 0644 root:root; then
		Show 1 "Failed to install license"
	fi
	if ! atomic_replace_file "$REPO_ROOT/docs/THIRD_PARTY_NOTICES.md" "${DOC_DIR}/THIRD_PARTY_NOTICES.md" 0644 root:root; then
		Show 1 "Failed to install third-party notices"
	fi
	Show 0 "Licenses installed to ${DOC_DIR}"

	# Binaries
	Show 2 "Installing binaries..."
	for binary in "${binaries[@]}"; do
		if ! atomic_replace_file "$REPO_ROOT/$binary" "/usr/local/bin/$binary" 0755 root:root; then
			Show 1 "Failed to install ${binary}"
		fi
	done
	Show 0 "Binaries installed to /usr/local/bin"

	# Systemd
	Show 2 "Installing systemd service files..."
	existing_port=$(find_existing_linuxio_port || true)
	local enable_indexer_timer=0
	if [[ ! -e "${SYSTEMD_DIR}/${INDEXER_TIMER_UNIT_NAME}" &&
		! -L "${SYSTEMD_DIR}/${INDEXER_TIMER_UNIT_NAME}" ]]; then
		enable_indexer_timer=1
	fi
	local units
	mapfile -t units < <(linuxio_systemd_units)
	for file in "${units[@]}"; do
		if [[ -f "$REPO_ROOT/packaging/systemd/$file" ]]; then
			if ! atomic_replace_file "$REPO_ROOT/packaging/systemd/$file" "${SYSTEMD_DIR}/${file}" 0644 root:root; then
				Show 1 "Failed to install ${file}"
			fi
		else
			Show 3 "${file} not found in packaging/systemd/"
		fi
	done
	if ! PORT=$(find_available_port "$existing_port"); then
		Show 1 "No available LinuxIO port in supported range ${LINUXIO_PORT_MIN}-${LINUXIO_PORT_MAX}"
	fi
	if ! rewrite_linuxio_socket_port "${SYSTEMD_DIR}/${LINUXIO_SOCKET_NAME}" "$PORT"; then
		Show 1 "Could not apply selected port ${PORT} to ${SYSTEMD_DIR}/${LINUXIO_SOCKET_NAME}"
	fi
	if [[ -n "$existing_port" && "$PORT" == "$existing_port" ]]; then
		Show 0 "Preserving existing LinuxIO port ${PORT}"
	elif [[ -n "$existing_port" ]]; then
		Show 3 "Existing LinuxIO port ${existing_port} is unavailable; using ${BOLD}${PORT}${COLOUR_RESET}"
	else
		Show 0 "Selected LinuxIO port ${PORT}"
	fi
	Show 0 "Systemd files installed"

	# Tmpfiles
	Show 2 "Installing tmpfiles.d configuration..."
	mkdir -p /usr/lib/tmpfiles.d
	if [[ -f "$REPO_ROOT/packaging/systemd/linuxio-tmpfiles.conf" ]]; then
		install -m 0644 "$REPO_ROOT/packaging/systemd/linuxio-tmpfiles.conf" /usr/lib/tmpfiles.d/linuxio.conf
		systemd-tmpfiles --create /usr/lib/tmpfiles.d/linuxio.conf 2>/dev/null || true
		Show 0 "tmpfiles.d configuration installed"
	else
		Show 3 "linuxio-tmpfiles.conf not found"
	fi

	# Config files
	Show 2 "Installing configuration files..."
	if [[ -d "$REPO_ROOT/packaging/etc/linuxio" ]]; then
		while IFS= read -r file; do
			rel_path="${file#"$REPO_ROOT"/packaging/etc/linuxio/}"
			if [[ ("$rel_path" == "indexer/config.yaml" || "$rel_path" == "monitoring/config.yaml") &&
				-f "/etc/linuxio/$rel_path" ]]; then
				Show 0 "/etc/linuxio/$rel_path already exists (not overwriting)"
				continue
			fi
			if ! atomic_replace_file "$file" "/etc/linuxio/$rel_path" 0644 root:root; then
				Show 1 "Failed to install /etc/linuxio/$rel_path"
			fi
		done < <(find "$REPO_ROOT/packaging/etc/linuxio" -type f | sort)
		Show 0 "Configuration files installed"
	else
		Show 3 "packaging/etc/linuxio directory not found"
	fi

	# Avahi mDNS service file
	Show 2 "Installing Avahi service file..."
	if [[ -f "$REPO_ROOT/packaging/etc/avahi/services/linuxio.service" ]]; then
		install -D -m 0644 "$REPO_ROOT/packaging/etc/avahi/services/linuxio.service" \
			/etc/avahi/services/linuxio.service
		if pgrep -x avahi-daemon >/dev/null 2>&1; then
			Show 0 "mDNS advertisement enabled ${GREY}(reachable at <hostname>.local)${COLOUR_RESET}"
		else
			Show 3 "Avahi not running — file installed, will activate when avahi-daemon starts"
		fi
	else
		Show 3 "Avahi service file not found — mDNS advertisement skipped"
	fi

	# PAM
	Show 2 "Installing PAM configuration..."
	if [[ -f "$REPO_ROOT/packaging/etc/pam.d/linuxio" ]]; then
		install -m 0644 "$REPO_ROOT/packaging/etc/pam.d/linuxio" /etc/pam.d/
		Show 0 "PAM configuration installed"
	else
		Show 1 "PAM configuration not found at packaging/etc/pam.d/linuxio"
	fi

	# Issue updater
	Show 2 "Installing issue updater..."
	mkdir -p /usr/share/linuxio/issue
	if [[ -f "$REPO_ROOT/packaging/scripts/update-issue" ]]; then
		install -m 0755 "$REPO_ROOT/packaging/scripts/update-issue" /usr/share/linuxio/issue/
		Show 0 "Issue updater installed"
	else
		Show 3 "update-issue script not found"
	fi

	# Wire the banner into the distro's MOTD mechanism:
	# - Debian/Ubuntu/Mint render /etc/update-motd.d/* into /run/motd.dynamic
	# - Fedora/RHEL-style pam_motd reads /etc/motd.d directly
	if [[ -d /etc/update-motd.d ]]; then
		if [[ -f "$REPO_ROOT/packaging/etc/update-motd.d/60-linuxio" ]]; then
			install -m 0755 "$REPO_ROOT/packaging/etc/update-motd.d/60-linuxio" /etc/update-motd.d/
			Show 0 "SSH login banner configured (update-motd.d)"
		else
			Show 3 "60-linuxio motd script not found"
		fi
	elif [[ -d /etc/motd.d ]]; then
		ln -sf ../../run/linuxio/issue /etc/motd.d/linuxio 2>/dev/null || true
		Show 0 "SSH login banner configured (motd.d)"
	else
		Show 3 "No update-motd.d or motd.d directory found, skipping login banner setup"
	fi

	# Journal access
	if [[ -n "${SUDO_USER:-}" ]]; then
		if ! id -nG "$SUDO_USER" | tr ' ' '\n' | grep -qxE "systemd-journal|adm"; then
			Show 2 "Granting ${SUDO_USER} journal read access..."
			usermod -aG systemd-journal "$SUDO_USER"
			Show 0 "${SUDO_USER} added to systemd-journal group ${GREY}(re-login refreshes the shell session)${COLOUR_RESET}"
		else
			Show 0 "${SUDO_USER} already has journal read access"
		fi
	fi

	# ========== ENABLE AND RESTART ==========
	Show 2 "Reloading systemd..."
	systemctl daemon-reload
	Show 0 "Systemd reloaded"

	Show 2 "Enabling services..."
	systemctl enable linuxio.target >/dev/null 2>&1
	if [[ $enable_indexer_timer -eq 1 ]]; then
		systemctl enable "$INDEXER_TIMER_UNIT_NAME" >/dev/null 2>&1
	fi
	systemctl enable linuxio-monitoring.service >/dev/null 2>&1
	Show 0 "Services enabled"

	Show 2 "Restarting LinuxIO..."
	linuxio restart

	# linuxio restart covers the control plane only; restart the monitoring
	# daemon and regenerate the login banner explicitly so an updated daemon
	# and update-issue script take effect now.
	systemctl restart linuxio-monitoring.service >/dev/null 2>&1 || true
	systemctl restart linuxio-issue.service 2>/dev/null || true

	sleep 2

	if systemctl is-active --quiet linuxio.target; then
		Show 0 "LinuxIO restarted successfully"
	else
		Show 3 "LinuxIO may not have restarted properly"
	fi

	# ========== SUMMARY ==========
	lan_ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}') || true

	echo ""
	echo -e "${LINE}"
	echo -e " ${GREEN}${BOLD}Installation complete!${COLOUR_RESET}"
	echo -e "${LINE}"
	echo "Installed components:"
	echo "  • Binaries:        /usr/local/bin/{linuxio,linuxio-webserver,linuxio-bridge,linuxio-auth,linuxio-docker-update,linuxio-indexer,linuxio-monitoring}"
	echo "  • Systemd files:   /etc/systemd/system/linuxio*"
	echo "  • Configuration:   /etc/linuxio/indexer/config.yaml, /etc/linuxio/monitoring/config.yaml"
	echo "  • Licenses:        ${DOC_DIR}/"
	echo "  • PAM config:      /etc/pam.d/linuxio"
	echo "  • Issue updater:   /usr/share/linuxio/issue/"
	echo ""
	echo -e " ${BOLD}Dashboard:${COLOUR_RESET}"
	echo -e "${BULLET} https://localhost:${PORT}"
	if [[ -n "$lan_ip" ]]; then
		echo -e "${BULLET} https://${lan_ip}:${PORT}"
	fi
	if pgrep -x avahi-daemon >/dev/null 2>&1; then
		hn=$(hostname 2>/dev/null) || hn=""
		if [[ -n "$hn" ]]; then
			echo -e "${BULLET} https://${hn}.local:${PORT}  ${GREY}(via mDNS)${COLOUR_RESET}"
		fi
	fi
	echo ""

}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	main "$@"
fi
