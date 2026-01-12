#!/usr/bin/env bash
# =============================================================================
# LinuxIO Full Installer
# Downloads and installs LinuxIO binaries, systemd services, PAM, and config
# © 2025 Miguel Mariz (mordilloSan)
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

# ---------- Logging Functions ----------
log_info()  { printf "▸ %s\n" "$*"; }
log_ok()    { printf "✓ %s\n" "$*"; }
log_error() { printf "✗ %s\n" "$*" >&2; }
log_warn()  { printf "⚠ %s\n" "$*"; }

# Track if services were stopped (to ensure they're restarted on exit)
SERVICES_STOPPED=0

cleanup() {
    local exit_code=$?

    # Always try to start services if we stopped them
    # This ensures services come back up even if script fails
    if [[ $SERVICES_STOPPED -eq 1 ]]; then
        log_info "Ensuring LinuxIO services are started..."
        if command -v linuxio &>/dev/null; then
            linuxio start 2>/dev/null || systemctl start linuxio.target 2>/dev/null || true
        else
            systemctl start linuxio.target 2>/dev/null || true
        fi
    fi

    # Clean up staging directory
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
        log_info "Downloading version ${version}"
    else
        base_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download"
        log_info "Downloading latest release"
    fi

    local files=(
        "linuxio"
        "linuxio-webserver"
        "linuxio-bridge"
        "linuxio-auth"
        "SHA256SUMS"
    )

    for file in "${files[@]}"; do
        log_info "Downloading ${file}..."
        if ! curl -fsSL "${base_url}/${file}" -o "${STAGING}/${file}"; then
            log_error "Failed to download ${file}"
            return 1
        fi
    done

    log_ok "All binaries downloaded"
    return 0
}

verify_checksums() {
    log_info "Verifying checksums..."

    local checksum_file="${STAGING}/SHA256SUMS"
    if [[ ! -f "$checksum_file" ]]; then
        log_error "SHA256SUMS file not found"
        return 1
    fi

    cd "$STAGING" || return 1

    # Parse checksums and verify each binary
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue

        local expected_hash filename
        read -r expected_hash filename <<< "$line"

        # Skip tarball entries
        [[ "$filename" == *.tar.gz ]] && continue

        # Skip if file doesn't exist (like tarball)
        [[ ! -f "$filename" ]] && continue

        log_info "Verifying ${filename}..."
        local actual_hash
        actual_hash=$(sha256sum "$filename" | awk '{print $1}')

        if [[ "$actual_hash" != "$expected_hash" ]]; then
            log_error "Checksum mismatch for ${filename}"
            log_error "Expected: ${expected_hash}"
            log_error "Got:      ${actual_hash}"
            return 1
        fi

        log_ok "Verified ${filename}"
    done < "$checksum_file"

    cd - >/dev/null || return 1
    log_ok "All checksums verified"
    return 0
}

install_binaries() {
    log_info "Installing binaries to ${BIN_DIR}..."

    # Ensure target directory exists
    mkdir -p "$BIN_DIR"

    # Define binaries with their permissions
    # linuxio-auth runs via systemd socket activation (no setuid needed)
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
            log_error "Source file not found: ${src}"
            return 1
        fi

        log_info "Installing ${binary} with mode ${mode}..."

        # Use /tmp for temp file instead of /usr/local/bin
        local tmp="/tmp/${binary}.new.$$"

        # Copy file to /tmp
        if ! cp "$src" "$tmp"; then
            log_error "Failed to copy ${binary} to temp location"
            return 1
        fi

        # Set permissions
        if ! chmod "$mode" "$tmp"; then
            log_error "Failed to chmod ${binary}"
            rm -f "$tmp"
            return 1
        fi

        # Set ownership to root
        if ! chown root:root "$tmp"; then
            log_error "Failed to chown ${binary}"
            rm -f "$tmp"
            return 1
        fi

        # Atomic move (works even if $dst is running)
        if ! mv "$tmp" "$dst"; then
            log_error "Failed to install ${binary}"
            rm -f "$tmp"
            return 1
        fi

        # Double-check final permissions
        chmod "$mode" "$dst" || log_warn "Failed to re-apply permissions to ${dst}"

        log_ok "Installed ${binary} (mode: ${mode})"
    done

    # Verify installations
    log_info "Verifying installations..."
    for binary in "${!binaries[@]}"; do
        local dst="${BIN_DIR}/${binary}"
        if [[ ! -x "$dst" ]]; then
            log_error "${binary} is not executable"
            return 1
        fi
    done

    log_ok "All binaries installed successfully"
    return 0
}

# ---------- Configuration Functions ----------

install_config_files() {
    log_info "Installing configuration files..."

    # Create config directory
    if [[ ! -d "$CONFIG_DIR" ]]; then
        log_info "Creating ${CONFIG_DIR}..."
        mkdir -p "$CONFIG_DIR"
        chown root:root "$CONFIG_DIR"
        chmod 0755 "$CONFIG_DIR"
    fi

    # Install disallowed-users file
    local disallowed_file="${CONFIG_DIR}/disallowed-users"
    if [[ ! -f "$disallowed_file" ]]; then
        log_info "Downloading disallowed-users..."
        if ! curl -fsSL "${RAW_BASE}/etc/linuxio/disallowed-users" -o "$disallowed_file"; then
            log_error "Failed to download disallowed-users"
            return 1
        fi
        chown root:root "$disallowed_file"
        chmod 0644 "$disallowed_file"
        log_ok "Created ${disallowed_file}"
    else
        log_ok "${disallowed_file} already exists (not overwriting)"
    fi

    return 0
}

install_pam_config() {
    log_info "Installing PAM configuration..."

    local pam_file="${PAM_DIR}/linuxio"

    # Backup existing PAM config if it exists
    if [[ -f "$pam_file" ]]; then
        log_info "Backing up existing PAM config..."
        cp "$pam_file" "${pam_file}.bak.$(date +%Y%m%d%H%M%S)"
    fi

    log_info "Downloading PAM configuration..."
    if ! curl -fsSL "${RAW_BASE}/etc/pam.d/linuxio" -o "$pam_file"; then
        log_error "Failed to download PAM configuration"
        return 1
    fi

    chown root:root "$pam_file"
    chmod 0644 "$pam_file"
    log_ok "Installed PAM configuration"

    return 0
}

# ---------- Systemd Functions ----------

# Global variable to store the selected port
SELECTED_PORT=8090

stop_existing_services() {
    # Stop existing linuxio services before starting new version
    # Called at the END of installation (not the beginning) to ensure:
    # 1. UI remains connected during download/install and shows full output
    # 2. Port is freed just before new version starts
    if systemctl is-active linuxio.target >/dev/null 2>&1 || \
       systemctl is-active linuxio-webserver.service >/dev/null 2>&1 || \
       systemctl is-active linuxio-webserver.socket >/dev/null 2>&1; then
        log_info "Stopping existing LinuxIO services..."
        SERVICES_STOPPED=1  # Mark that we stopped services (cleanup trap will ensure restart)
        systemctl stop linuxio.target 2>/dev/null || true
        log_ok "Existing services stopped"
    fi
}

is_port_in_use() {
    local port="$1"
    # Check if port is in use by a process OTHER than linuxio
    # If linuxio is already using the port, that's fine (we're reinstalling/upgrading)

    # First check: if existing linuxio socket is configured for this port, it's ours
    local existing_socket="/lib/systemd/system/linuxio-webserver.socket"
    if [[ -f "$existing_socket" ]]; then
        if grep -qE "ListenStream=.*:${port}\$" "$existing_socket" 2>/dev/null; then
            return 1  # linuxio socket owns it, consider it available
        fi
    fi

    local proc
    proc=$(ss -tlnpH "sport = :${port}" 2>/dev/null)

    # Port not in use at all
    [[ -z "$proc" ]] && return 1

    # Port in use - check if it's owned by linuxio or systemd (socket activation)
    if echo "$proc" | grep -qE 'linuxio|systemd'; then
        # Could be socket-activated, check if linuxio socket unit is active for this port
        if systemctl is-active --quiet linuxio-webserver.socket 2>/dev/null; then
            return 1  # linuxio socket owns it
        fi
    fi

    return 0  # Some other process owns it
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

    # Fallback to default if all ports checked
    echo "8090"
    return 1
}

install_systemd_files() {
    log_info "Installing systemd service files..."

    for file in linuxio.target linuxio-webserver.socket linuxio-webserver.service \
        linuxio-auth.socket linuxio-auth@.service \
        linuxio-bridge-socket-user.service \
        linuxio-issue.service; do
        log_info "Downloading ${file}..."
        if ! curl -fsSL "${RAW_BASE}/systemd/${file}" -o "${SYSTEMD_DIR}/${file}"; then
            log_error "Failed to download ${file}"
            return 1
        fi
        chmod 0644 "${SYSTEMD_DIR}/${file}"
        log_ok "Installed ${file}"
    done

    # Check if default port is in use and find alternative
    SELECTED_PORT=$(find_available_port)
    if [[ "$SELECTED_PORT" != "8090" ]]; then
        log_warn "Port 8090 is in use, using port ${SELECTED_PORT} instead"
        sed -i "s/ListenStream=0.0.0.0:8090/ListenStream=0.0.0.0:${SELECTED_PORT}/" "${SYSTEMD_DIR}/linuxio-webserver.socket"
        log_ok "Updated socket to use port ${SELECTED_PORT}"
    fi

    # Install SSH login banner support
    log_info "Installing SSH login banner support..."
    mkdir -p /usr/share/linuxio/issue
    if ! curl -fsSL "${RAW_BASE}/scripts/update-issue" -o /usr/share/linuxio/issue/update-issue; then
        log_warn "Failed to download issue script (non-critical)"
    else
        chmod 0755 /usr/share/linuxio/issue/update-issue
        # Create symlink for SSH login banner (motd.d)
        if [[ -d /etc/motd.d ]]; then
            ln -sf ../../run/linuxio/issue /etc/motd.d/linuxio 2>/dev/null || true
            log_ok "SSH login banner configured"
        else
            log_info "No /etc/motd.d found, skipping login banner setup"
        fi
    fi

    # Reload systemd
    log_info "Reloading systemd daemon..."
    systemctl daemon-reload
    log_ok "Systemd daemon reloaded"

    return 0
}

enable_services() {
    log_info "Enabling systemd services..."

    # Enable the target (pulls in all sockets and services)
    if systemctl enable linuxio.target >/dev/null 2>&1; then
        log_ok "Enabled linuxio.target"
    else
        log_warn "Failed to enable linuxio.target"
    fi

    return 0
}

# ---------- Verification Functions ----------

verify_installation() {
    log_info "Running post-installation checks..."

    # Check that binaries can execute
    if "${BIN_DIR}/linuxio" help >/dev/null 2>&1; then
        log_ok "linuxio CLI: working"
    else
        log_warn "linuxio CLI did not run successfully"
    fi

    if "${BIN_DIR}/linuxio-webserver" >/dev/null 2>&1; then
        local version
        version=$("${BIN_DIR}/linuxio-webserver" 2>&1 | head -n1 || echo "unknown")
        log_ok "${version}"
    else
        log_warn "linuxio-webserver did not run successfully (may be arch mismatch)"
    fi

    # Check bridge is executable (it's session-based, only runs when user logs in)
    if [[ -x "${BIN_DIR}/linuxio-bridge" ]]; then
        log_ok "linuxio-bridge: executable"
    else
        log_warn "linuxio-bridge: not executable"
    fi

    # Check auth helper is executable
    if [[ -x "${BIN_DIR}/linuxio-auth" ]]; then
        log_ok "linuxio-auth: executable"
    else
        log_warn "linuxio-auth: not executable"
    fi

    # Check systemd target
    if systemctl is-enabled linuxio.target >/dev/null 2>&1; then
        log_ok "linuxio.target is enabled"
    else
        log_warn "linuxio.target is not enabled"
    fi

    # Check PAM config
    if [[ -f "${PAM_DIR}/linuxio" ]]; then
        log_ok "PAM configuration installed"
    else
        log_warn "PAM configuration not found"
    fi

    # Check config directory
    if [[ -d "$CONFIG_DIR" ]]; then
        log_ok "Configuration directory exists at ${CONFIG_DIR}"
    else
        log_warn "Configuration directory not found at ${CONFIG_DIR}"
    fi

    return 0
}

# ---------- Main ----------

main() {
    local version="${1:-}"
    local skip_binaries=0

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --skip-binaries)
                skip_binaries=1
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

    # Check we're running as root
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi

    log_info "Starting LinuxIO full installation"
    if [[ -n "$version" ]]; then
        log_info "Target version: ${version}"
    else
        log_info "Target version: latest"
    fi
    echo ""

    # NOTE: We intentionally do NOT stop services here during updates.
    # All installation steps (download, verify, install binaries/config/systemd)
    # can safely run while the old version is still serving requests.
    # Services are only stopped/restarted at the very end, ensuring the UI
    # remains connected and can show the full installation output.

    # Step 1: Download binaries (unless skipped)
    if [[ $skip_binaries -eq 0 ]]; then
        log_info "=== Step 1/5: Downloading binaries ==="
        if ! download_binaries "$version"; then
            log_error "Download failed"
            exit 1
        fi
        echo ""

        # Step 2: Verify checksums
        log_info "=== Step 2/5: Verifying checksums ==="
        if ! verify_checksums; then
            log_error "Checksum verification failed"
            exit 1
        fi
        echo ""

        # Step 3: Install binaries
        log_info "=== Step 3/5: Installing binaries ==="
        if ! install_binaries; then
            log_error "Binary installation failed"
            exit 1
        fi
    else
        log_info "=== Steps 1-3: Skipping binary installation ==="
    fi
    echo ""

    # Step 4: Install configuration files
    log_info "=== Step 4/5: Installing configuration ==="
    if ! install_config_files; then
        log_error "Config installation failed"
        exit 1
    fi

    if ! install_pam_config; then
        log_error "PAM configuration failed"
        exit 1
    fi
    echo ""

    # Step 5: Install systemd files
    log_info "=== Step 5/5: Installing systemd services ==="
    if ! install_systemd_files; then
        log_error "Systemd installation failed"
        exit 1
    fi

    if ! enable_services; then
        log_warn "Some services may not be enabled"
    fi
    echo ""

    # Verify everything
    log_info "=== Verification ==="
    verify_installation
    echo ""

    # Brief pause to let journalctl stream all output to the UI
    # Without this, the verification results may not be visible before disconnect
    sleep 2

    # Stop existing services if running (this is the only point where we stop)
    # This ensures all installation output was visible before disconnection
    stop_existing_services

    # Start the service
    log_info "Starting LinuxIO service..."
    if systemctl start linuxio.target; then
        log_ok "LinuxIO service started"
        SERVICES_STOPPED=0  # Clear flag - services are running, cleanup doesn't need to restart
    else
        log_warn "Failed to start LinuxIO service - cleanup will retry"
        # Leave SERVICES_STOPPED=1 so cleanup trap will try again
    fi

    log_ok "Installation complete!"
    echo ""
    echo "Access the dashboard at: https://localhost:${SELECTED_PORT}"
    echo ""
    echo "Useful commands:"
    echo "  • Check status:   linuxio status"
    echo "  • View logs:      linuxio logs"
    echo "  • All commands:   linuxio"
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
  --skip-binaries   Skip downloading and installing binaries (config only)
  -h, --help        Show this help message

What gets installed:
  • Binaries:     /usr/local/bin/linuxio, linuxio-webserver, linuxio-bridge, linuxio-auth
  • Systemd:      /etc/systemd/system/linuxio-webserver.service, linuxio-webserver.socket
  • PAM:          /etc/pam.d/linuxio
  • Config:       /etc/linuxio/disallowed-users

Note: LinuxIO uses systemd DynamicUser, no static accounts are created.

Examples:
  $(basename "$0")                 # Install latest release
  $(basename "$0") v0.3.0          # Install specific version
  $(basename "$0") --skip-binaries # Only install config/systemd/pam

This script must be run as root.
EOF
}

# ---------- Entry Point ----------

if [[ -z "${BASH_SOURCE+x}" || "${BASH_SOURCE[0]-}" == "$0" ]]; then
    main "$@"
fi
