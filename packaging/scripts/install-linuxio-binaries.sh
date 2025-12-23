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
readonly SERVICE_GROUP="linuxio"
readonly RAW_BASE="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging"

# ---------- Logging Functions ----------
log_info()  { printf "▸ %s\n" "$*"; }
log_ok()    { printf "✓ %s\n" "$*"; }
log_error() { printf "✗ %s\n" "$*" >&2; }
log_warn()  { printf "⚠ %s\n" "$*"; }

cleanup() {
    if [[ -d "$STAGING" ]]; then
        rm -rf "$STAGING" 2>/dev/null || true
    fi
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
        "linuxio-bridge"
        "linuxio-auth-helper"
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
    # linuxio-auth-helper needs setuid (4755) to run as root
    local -A binaries=(
        ["linuxio"]="0755"
        ["linuxio-bridge"]="0755"
        ["linuxio-auth-helper"]="4755"
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

        # Double-check final permissions (important for setuid)
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

# ---------- Account Functions ----------

create_service_account() {
    log_info "Setting up service account..."

    # Create group if it doesn't exist
    if ! getent group "$SERVICE_GROUP" >/dev/null 2>&1; then
        log_info "Creating group '${SERVICE_GROUP}'..."
        groupadd -r "$SERVICE_GROUP"
        log_ok "Created group '${SERVICE_GROUP}'"
    else
        log_ok "Group '${SERVICE_GROUP}' already exists"
    fi

    # Create user if it doesn't exist
    if ! getent passwd "$SERVICE_GROUP" >/dev/null 2>&1; then
        log_info "Creating user '${SERVICE_GROUP}'..."
        useradd -r -g "$SERVICE_GROUP" -s /usr/sbin/nologin -M "$SERVICE_GROUP"
        log_ok "Created user '${SERVICE_GROUP}'"
    else
        log_ok "User '${SERVICE_GROUP}' already exists"
    fi

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

is_port_in_use() {
    local port="$1"
    # Check if port is in use by any process
    ss -tlnH "sport = :${port}" 2>/dev/null | grep -q .
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

    for file in linuxio.socket linuxio.service; do
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
        sed -i "s/ListenStream=0.0.0.0:8090/ListenStream=0.0.0.0:${SELECTED_PORT}/" "${SYSTEMD_DIR}/linuxio.socket"
        log_ok "Updated socket to use port ${SELECTED_PORT}"
    fi

    # Reload systemd
    log_info "Reloading systemd daemon..."
    systemctl daemon-reload
    log_ok "Systemd daemon reloaded"

    return 0
}

enable_services() {
    log_info "Enabling systemd services..."

    # Enable socket activation
    if systemctl enable linuxio.socket >/dev/null 2>&1; then
        log_ok "Enabled linuxio.socket"
    else
        log_warn "Failed to enable linuxio.socket"
    fi

    # Enable the service
    if systemctl enable linuxio.service >/dev/null 2>&1; then
        log_ok "Enabled linuxio.service"
    else
        log_warn "Failed to enable linuxio.service"
    fi

    return 0
}

# ---------- Verification Functions ----------

verify_installation() {
    log_info "Running post-installation checks..."

    # Check that binaries can execute
    if "${BIN_DIR}/linuxio" --version >/dev/null 2>&1; then
        local version
        version=$("${BIN_DIR}/linuxio" --version 2>&1 || echo "unknown")
        log_ok "linuxio: ${version}"
    else
        log_warn "linuxio did not run successfully (may be arch mismatch)"
    fi

    if "${BIN_DIR}/linuxio-bridge" --version >/dev/null 2>&1; then
        local version
        version=$("${BIN_DIR}/linuxio-bridge" --version 2>&1 || echo "unknown")
        log_ok "linuxio-bridge: ${version}"
    else
        log_warn "linuxio-bridge did not run successfully"
    fi

    # Check setuid bit on auth-helper
    local auth_helper="${BIN_DIR}/linuxio-auth-helper"
    if [[ -u "$auth_helper" ]]; then
        log_ok "linuxio-auth-helper: setuid bit is set"
    else
        log_warn "linuxio-auth-helper: setuid bit NOT set (may affect authentication)"
    fi

    # Check systemd services
    if systemctl is-enabled linuxio.socket >/dev/null 2>&1; then
        log_ok "linuxio.socket is enabled"
    else
        log_warn "linuxio.socket is not enabled"
    fi

    if systemctl is-enabled linuxio.service >/dev/null 2>&1; then
        log_ok "linuxio.service is enabled"
    else
        log_warn "linuxio.service is not enabled"
    fi

    # Check group
    if getent group "$SERVICE_GROUP" >/dev/null 2>&1; then
        log_ok "Service group '${SERVICE_GROUP}' exists"
    else
        log_warn "Service group '${SERVICE_GROUP}' not found"
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
    [[ -n "$version" ]] && log_info "Target version: ${version}" || log_info "Target version: latest"
    echo ""

    # Step 1: Create service account
    log_info "=== Step 1/6: Creating service account ==="
    if ! create_service_account; then
        log_error "Failed to create service account"
        exit 1
    fi
    echo ""

    # Step 2: Download binaries (unless skipped)
    if [[ $skip_binaries -eq 0 ]]; then
        log_info "=== Step 2/6: Downloading binaries ==="
        if ! download_binaries "$version"; then
            log_error "Download failed"
            exit 1
        fi
        echo ""

        # Step 3: Verify checksums
        log_info "=== Step 3/6: Verifying checksums ==="
        if ! verify_checksums; then
            log_error "Checksum verification failed"
            exit 1
        fi
        echo ""

        # Step 4: Install binaries
        log_info "=== Step 4/6: Installing binaries ==="
        if ! install_binaries; then
            log_error "Binary installation failed"
            exit 1
        fi
    else
        log_info "=== Steps 2-4: Skipping binary installation ==="
    fi
    echo ""

    # Step 5: Install configuration files
    log_info "=== Step 5/6: Installing configuration ==="
    if ! install_config_files; then
        log_error "Config installation failed"
        exit 1
    fi

    if ! install_pam_config; then
        log_error "PAM configuration failed"
        exit 1
    fi
    echo ""

    # Step 6: Install systemd files
    log_info "=== Step 6/6: Installing systemd services ==="
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

    # Start the service
    log_info "Starting LinuxIO service..."
    if systemctl start linuxio.socket linuxio.service; then
        log_ok "LinuxIO service started"
    else
        log_warn "Failed to start LinuxIO service"
    fi

    log_ok "Installation complete!"
    echo ""
    echo "Access the dashboard at: http://localhost:${SELECTED_PORT}"
    echo ""
    echo "Useful commands:"
    echo "  • Check status:  systemctl status linuxio.service"
    echo "  • View logs:     journalctl -u linuxio.service -f"
    echo "  • Restart:       systemctl restart linuxio.service"
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
  • Binaries:     /usr/local/bin/linuxio, linuxio-bridge, linuxio-auth-helper
  • Systemd:      /etc/systemd/system/linuxio.service, linuxio.socket
  • PAM:          /etc/pam.d/linuxio
  • Config:       /etc/linuxio/disallowed-users
  • Account:      linuxio system user and group (for service and auth-helper)

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
