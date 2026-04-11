#!/usr/bin/env bash
# =============================================================================
# LinuxIO Full Installer
# Downloads and installs LinuxIO binaries, systemd services, PAM, and config
#  2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

# ---------- Configuration ----------
readonly REPO_OWNER="mordilloSan"
readonly REPO_NAME="LinuxIO"
readonly BIN_DIR="/usr/local/bin"
readonly SYSTEMD_DIR="/etc/systemd/system"
readonly PAM_DIR="/etc/pam.d"
readonly CONFIG_DIR="/etc/linuxio"
readonly STAGING="/tmp/linuxio-install-$$"
readonly RAW_BASE="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging"

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
        1) echo -e " ${GREY}[${RED}FAILED${GREY}]${COLOUR_RESET} $*"; exit 1 ;;
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

    local files=(
        "linuxio"
        "linuxio-webserver"
        "linuxio-bridge"
        "linuxio-auth"
        "SHA256SUMS"
    )

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
        read -r expected_hash filename <<< "$line"

        [[ "$filename" == *.tar.gz ]] && continue
        [[ ! -f "$filename" ]] && continue

        local actual_hash
        actual_hash=$(sha256sum "$filename" | awk '{print $1}')

        if [[ "$actual_hash" != "$expected_hash" ]]; then
            Show 1 "Checksum mismatch for ${filename}"
        fi

        Show 0 "Verified ${filename}"
    done < "$checksum_file"

    cd - >/dev/null || return 1
    Show 0 "All checksums verified"
    return 0
}

install_binaries() {
    Show 2 "Installing binaries to ${BOLD}${BIN_DIR}${COLOUR_RESET}"

    mkdir -p "$BIN_DIR"

    local -A binaries=(
        ["linuxio"]="0755"
        ["linuxio-webserver"]="0755"
        ["linuxio-bridge"]="0755"
        ["linuxio-auth"]="0755"
    )

    for binary in "${!binaries[@]}"; do
        local src="${STAGING}/${binary}"
        local dst="${BIN_DIR}/${binary}"
        local mode="${binaries[$binary]}"

        if [[ ! -f "$src" ]]; then
            Show 1 "Source file not found: ${src}"
        fi

        local tmp="/tmp/${binary}.new.$$"

        if ! cp "$src" "$tmp"; then
            Show 1 "Failed to copy ${binary} to temp location"
        fi

        if ! chmod "$mode" "$tmp"; then
            rm -f "$tmp"
            Show 1 "Failed to chmod ${binary}"
        fi

        if ! chown root:root "$tmp"; then
            rm -f "$tmp"
            Show 1 "Failed to chown ${binary}"
        fi

        if ! mv "$tmp" "$dst"; then
            rm -f "$tmp"
            Show 1 "Failed to install ${binary}"
        fi

        chmod "$mode" "$dst" || Show 3 "Failed to re-apply permissions to ${dst}"
        Show 0 "Installed ${binary}"
    done

    Show 2 "Verifying installations..."
    for binary in "${!binaries[@]}"; do
        local dst="${BIN_DIR}/${binary}"
        if [[ ! -x "$dst" ]]; then
            Show 1 "${binary} is not executable"
        fi
    done

    Show 0 "All binaries installed"
    return 0
}

# ---------- Configuration Functions ----------

install_config_files() {
    Show 2 "Installing configuration files..."

    if [[ ! -d "$CONFIG_DIR" ]]; then
        mkdir -p "$CONFIG_DIR"
        chown root:root "$CONFIG_DIR"
        chmod 0755 "$CONFIG_DIR"
    fi

    local disallowed_file="${CONFIG_DIR}/disallowed-users"
    if [[ ! -f "$disallowed_file" ]]; then
        Show 2 "Downloading disallowed-users..."
        if ! curl -fsSL "${RAW_BASE}/etc/linuxio/disallowed-users" -o "$disallowed_file"; then
            Show 1 "Failed to download disallowed-users"
        fi
        chown root:root "$disallowed_file"
        chmod 0644 "$disallowed_file"
        Show 0 "Created ${disallowed_file}"
    else
        Show 0 "${disallowed_file} already exists (not overwriting)"
    fi

    return 0
}

install_pam_config() {
    Show 2 "Installing PAM configuration..."

    local pam_file="${PAM_DIR}/linuxio"

    if ! curl -fsSL "${RAW_BASE}/etc/pam.d/linuxio" -o "$pam_file"; then
        Show 1 "Failed to download PAM configuration"
    fi

    chown root:root "$pam_file"
    chmod 0644 "$pam_file"
    Show 0 "PAM configuration installed"

    return 0
}

# ---------- Systemd Functions ----------

SELECTED_PORT=8090

linuxio_services_active() {
    systemctl is-active linuxio.target >/dev/null 2>&1 || \
        systemctl is-active linuxio-webserver.service >/dev/null 2>&1 || \
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

is_port_in_use() {
    local port="$1"

    local existing_socket="/lib/systemd/system/linuxio-webserver.socket"
    if [[ -f "$existing_socket" ]]; then
        if grep -qE "ListenStream=.*:${port}\$" "$existing_socket" 2>/dev/null; then
            return 1
        fi
    fi

    local proc
    proc=$(ss -tlnpH "sport = :${port}" 2>/dev/null)

    [[ -z "$proc" ]] && return 1

    if echo "$proc" | grep -qE 'linuxio|systemd'; then
        if systemctl is-active --quiet linuxio-webserver.socket 2>/dev/null; then
            return 1
        fi
    fi

    return 0
}

find_available_port() {
    local port=8090
    local max_port=8099

    while [[ $port -le $max_port ]]; do
        if ! is_port_in_use "$port"; then
            echo "$port"
            return 0
        fi
        ((port++))
    done

    echo "8090"
    return 1
}

install_systemd_files() {
    Show 2 "Installing systemd service files..."

    for file in linuxio.target linuxio-webserver.socket linuxio-webserver.service \
        linuxio-auth.socket linuxio-auth@.service \
        linuxio-bridge-socket-user.service \
        linuxio-issue.service; do
        Show 2 "Downloading ${file}..."
        if ! curl -fsSL "${RAW_BASE}/systemd/${file}" -o "${SYSTEMD_DIR}/${file}"; then
            Show 1 "Failed to download ${file}"
        fi
        chmod 0644 "${SYSTEMD_DIR}/${file}"
        Show 0 "Installed ${file}"
    done

    SELECTED_PORT=$(find_available_port)
    if [[ "$SELECTED_PORT" != "8090" ]]; then
        Show 3 "Port 8090 is in use, using port ${BOLD}${SELECTED_PORT}${COLOUR_RESET} instead"
        sed -i "s/ListenStream=0.0.0.0:8090/ListenStream=0.0.0.0:${SELECTED_PORT}/" "${SYSTEMD_DIR}/linuxio-webserver.socket"
    fi

    Show 2 "Installing SSH login banner support..."
    mkdir -p /usr/share/linuxio/issue
    if ! curl -fsSL "${RAW_BASE}/scripts/update-issue" -o /usr/share/linuxio/issue/update-issue; then
        Show 3 "Failed to download issue script (non-critical)"
    else
        chmod 0755 /usr/share/linuxio/issue/update-issue
        if [[ -d /etc/motd.d ]]; then
            ln -sf ../../run/linuxio/issue /etc/motd.d/linuxio 2>/dev/null || true
            Show 0 "SSH login banner configured"
        else
            Show 2 "No /etc/motd.d found, skipping login banner setup"
        fi
    fi

    Show 2 "Installing tmpfiles.d configuration..."
    mkdir -p /usr/lib/tmpfiles.d
    if ! curl -fsSL "${RAW_BASE}/systemd/linuxio-tmpfiles.conf" -o /usr/lib/tmpfiles.d/linuxio.conf; then
        Show 3 "Failed to download tmpfiles.d config (non-critical)"
    else
        chmod 0644 /usr/lib/tmpfiles.d/linuxio.conf
        systemd-tmpfiles --create /usr/lib/tmpfiles.d/linuxio.conf 2>/dev/null || true
        Show 0 "tmpfiles.d configuration installed"
    fi

    Show 2 "Creating Watchtower data directory..."
    mkdir -p /var/lib/linuxIO/watchtower
    if getent group docker &>/dev/null; then
        chown root:docker /var/lib/linuxIO/watchtower
        chmod 775 /var/lib/linuxIO/watchtower
        Show 0 "Watchtower directory created ${GREY}(group: docker)${COLOUR_RESET}"
    else
        chmod 755 /var/lib/linuxIO/watchtower
        Show 3 "docker group not found — Watchtower directory created with mode 755"
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
        "/var/lib/linuxIO"
    )

    for target in "${targets[@]}"; do
        if [[ ! -d "$target" ]]; then
            Show 3 "${target} does not exist, skipping"
            continue
        fi

        local probe="${target}/.linuxio-dry-run-$$"
        if : > "$probe"; then
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
    local version="${1:-}"
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
            -h|--help)
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

    # Step 5: Systemd
    Header "Step 5/5 — Systemd Services"
    if ! install_systemd_files; then
        Show 1 "Systemd installation failed"
    fi
    if ! enable_services; then
        Show 3 "Some services may not be enabled"
    fi

    # Verification
    Header "Verification"
    verify_installation
    sleep 2

    if [[ $defer_restart -eq 1 ]]; then
        Show 2 "Deferring service restart to the caller"
    else
        restart_or_start_services || true
    fi

    echo ""
    echo -e "${LINE}"
    echo -e " ${GREEN}${BOLD}Installation complete!${COLOUR_RESET}"
    echo -e "${LINE}"
    echo ""
    echo -e " ${BOLD}Dashboard:${COLOUR_RESET}  https://localhost:${SELECTED_PORT}"
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
  VERSION           Optional release tag (e.g., v0.3.0). If omitted, installs latest.

Options:
  --dry-run         Validate writable install targets and exit
  --defer-restart   Do not restart services; caller will do it after the script exits
  --skip-binaries   Skip downloading and installing binaries (config only)
  -h, --help        Show this help message

What gets installed:
  - Binaries:     /usr/local/bin/linuxio, linuxio-webserver, linuxio-bridge, linuxio-auth
  - Systemd:      /etc/systemd/system/linuxio*.service, linuxio*.socket, linuxio.target
  - Tmpfiles:     /usr/lib/tmpfiles.d/linuxio.conf (creates /run/linuxio/icons)
  - PAM:          /etc/pam.d/linuxio
  - Config:       /etc/linuxio/disallowed-users

Examples:
  $(basename "$0")                 # Install latest release
  $(basename "$0") v0.3.0          # Install specific version
  $(basename "$0") --dry-run       # Validate updater write access without installing
  $(basename "$0") --skip-binaries # Only install config/systemd/pam

This script must be run as root.
EOF
}

# ---------- Entry Point ----------

if [[ -z "${BASH_SOURCE+x}" || "${BASH_SOURCE[0]-}" == "$0" ]]; then
    main "$@"
fi
