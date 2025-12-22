#!/usr/bin/env bash
# =============================================================================
# LinuxIO Installer
# Downloads and installs LinuxIO binaries, systemd services, and configuration
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

# ---------- Main Functions ----------

download_release() {
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

    # Download individual binaries and checksums
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

    # Download tarball for config files
    local tarball_name
    if [[ -n "$version" ]]; then
        tarball_name="linuxio-${version}-linux-amd64.tar.gz"
    else
        # For latest, we need to detect the version from the binary
        tarball_name="linuxio-linux-amd64.tar.gz"
    fi

    log_info "Downloading release tarball for config files..."
    # Try versioned tarball first, fall back to glob pattern
    if [[ -n "$version" ]]; then
        if ! curl -fsSL "${base_url}/${tarball_name}" -o "${STAGING}/release.tar.gz"; then
            log_warn "Could not download tarball, config files may need manual installation"
        fi
    else
        # For latest, try to find the tarball
        if ! curl -fsSL "${base_url}/linuxio-v0.0.0-linux-amd64.tar.gz" -o "${STAGING}/release.tar.gz" 2>/dev/null; then
            # Fallback: download config files from main branch
            log_info "Downloading config files from repository..."
            curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging/systemd/linuxio.service" -o "${STAGING}/linuxio.service" || true
            curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging/systemd/linuxio.socket" -o "${STAGING}/linuxio.socket" || true
            curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging/etc/pam.d/linuxio" -o "${STAGING}/linuxio.pam" || true
            curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging/etc/linuxio/disallowed-users" -o "${STAGING}/disallowed-users" || true
        fi
    fi

    # Extract tarball if downloaded
    if [[ -f "${STAGING}/release.tar.gz" ]]; then
        log_info "Extracting config files..."
        tar -xzf "${STAGING}/release.tar.gz" -C "${STAGING}" 2>/dev/null || true
    fi

    log_ok "All files downloaded"
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
    
    return 0
}

create_user_and_group() {
    log_info "Setting up linuxio user and group..."

    # Create linuxio group if it doesn't exist
    if ! getent group linuxio >/dev/null 2>&1; then
        groupadd --system linuxio
        log_ok "Created group: linuxio"
    else
        log_ok "Group linuxio already exists"
    fi

    # Create linuxio user if it doesn't exist
    if ! getent passwd linuxio >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin \
            --gid linuxio linuxio
        log_ok "Created user: linuxio"
    else
        log_ok "User linuxio already exists"
    fi

    # Add linuxio to docker group if docker is installed
    if getent group docker >/dev/null 2>&1; then
        usermod -aG docker linuxio 2>/dev/null || true
        log_ok "Added linuxio to docker group"
    fi

    return 0
}

install_config_files() {
    log_info "Installing configuration files..."

    # Find config files (either from tarball extraction or direct download)
    local service_file socket_file pam_file disallowed_file

    # Check tarball extraction path first
    if [[ -f "${STAGING}/release-config/systemd/linuxio.service" ]]; then
        service_file="${STAGING}/release-config/systemd/linuxio.service"
        socket_file="${STAGING}/release-config/systemd/linuxio.socket"
        pam_file="${STAGING}/release-config/pam.d/linuxio"
        disallowed_file="${STAGING}/release-config/linuxio/disallowed-users"
    else
        # Fallback to direct downloads
        service_file="${STAGING}/linuxio.service"
        socket_file="${STAGING}/linuxio.socket"
        pam_file="${STAGING}/linuxio.pam"
        disallowed_file="${STAGING}/disallowed-users"
    fi

    # Install systemd service file
    if [[ -f "$service_file" ]]; then
        cp "$service_file" "${SYSTEMD_DIR}/linuxio.service"
        chmod 644 "${SYSTEMD_DIR}/linuxio.service"
        log_ok "Installed linuxio.service"
    else
        log_warn "linuxio.service not found, skipping"
    fi

    # Install systemd socket file
    if [[ -f "$socket_file" ]]; then
        cp "$socket_file" "${SYSTEMD_DIR}/linuxio.socket"
        chmod 644 "${SYSTEMD_DIR}/linuxio.socket"
        log_ok "Installed linuxio.socket"
    else
        log_warn "linuxio.socket not found, skipping"
    fi

    # Install PAM configuration
    if [[ -f "$pam_file" ]]; then
        cp "$pam_file" "${PAM_DIR}/linuxio"
        chmod 644 "${PAM_DIR}/linuxio"
        log_ok "Installed PAM config"
    else
        log_warn "PAM config not found, skipping"
    fi

    # Install linuxio config directory
    mkdir -p "${CONFIG_DIR}"
    chmod 755 "${CONFIG_DIR}"

    if [[ -f "$disallowed_file" ]]; then
        cp "$disallowed_file" "${CONFIG_DIR}/disallowed-users"
        chmod 644 "${CONFIG_DIR}/disallowed-users"
        log_ok "Installed disallowed-users config"
    else
        # Create default empty file
        touch "${CONFIG_DIR}/disallowed-users"
        chmod 644 "${CONFIG_DIR}/disallowed-users"
        log_ok "Created empty disallowed-users config"
    fi

    return 0
}

enable_service() {
    log_info "Enabling LinuxIO service..."

    # Reload systemd to pick up new unit files
    systemctl daemon-reload

    # Enable socket activation
    if [[ -f "${SYSTEMD_DIR}/linuxio.socket" ]]; then
        systemctl enable linuxio.socket
        log_ok "Enabled linuxio.socket"
    fi

    # Enable the service
    if [[ -f "${SYSTEMD_DIR}/linuxio.service" ]]; then
        systemctl enable linuxio.service
        log_ok "Enabled linuxio.service"
    fi

    log_info "To start LinuxIO now, run: sudo systemctl start linuxio"
    return 0
}

# ---------- Main ----------

main() {
    local version="${1:-}"

    # Check we're running as root
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi

    log_info "Starting LinuxIO installation"
    [[ -n "$version" ]] && log_info "Target version: ${version}" || log_info "Target version: latest"

    if ! download_release "$version"; then
        log_error "Download failed"
        exit 1
    fi

    if ! verify_checksums; then
        log_error "Checksum verification failed"
        exit 1
    fi

    if ! install_binaries; then
        log_error "Binary installation failed"
        exit 1
    fi

    if ! create_user_and_group; then
        log_error "User/group creation failed"
        exit 1
    fi

    if ! install_config_files; then
        log_error "Config file installation failed"
        exit 1
    fi

    if ! enable_service; then
        log_error "Service enablement failed"
        exit 1
    fi

    verify_installation

    log_ok "Installation complete!"
    log_info ""
    log_info "LinuxIO has been installed and enabled."
    log_info "Start the service with: sudo systemctl start linuxio"
    log_info "Access the web interface at: https://your-server-ip:9090"
    exit 0
}

# ---------- Usage ----------

if [[ -z "${BASH_SOURCE+x}" || "${BASH_SOURCE[0]-}" == "$0" ]]; then
    if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
        cat <<EOF
Usage: $(basename "$0") [VERSION]

Downloads and installs LinuxIO from GitHub releases, including:
  - Binary files (linuxio, linuxio-bridge, linuxio-auth-helper)
  - Systemd service and socket files
  - PAM configuration
  - Default configuration files

Arguments:
  VERSION    Optional release tag (e.g., v0.5.5). If omitted, installs latest.

Examples:
  $(basename "$0")           # Install latest release
  $(basename "$0") v0.5.5    # Install specific version

This script must be run as root.
EOF
        exit 0
    fi

    main "$@"
fi