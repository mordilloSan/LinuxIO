#!/usr/bin/env bash
# =============================================================================
# LinuxIO Binary Installer
# Downloads and installs LinuxIO binaries with proper permissions
# © 2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

# ---------- Configuration ----------
readonly REPO_OWNER="mordilloSan"
readonly REPO_NAME="LinuxIO"
readonly BIN_DIR="/usr/local/bin"
readonly STAGING="/tmp/linuxio-install-$$"

# ---------- Colors ----------
readonly COLOUR_RESET='\e[0m'
readonly GREEN='\e[38;5;154m'
readonly RED='\e[91m'
readonly GREY='\e[90m'
readonly YELLOW='\e[33m'

log_info()  { echo -e "${GREY}[INFO]${COLOUR_RESET} $*"; }
log_ok()    { echo -e "${GREEN}[OK]${COLOUR_RESET} $*"; }
log_error() { echo -e "${RED}[ERROR]${COLOUR_RESET} $*" >&2; }
log_warn()  { echo -e "${YELLOW}[WARN]${COLOUR_RESET} $*"; }

cleanup() {
    if [[ -d "$STAGING" ]]; then
        rm -rf "$STAGING" 2>/dev/null || true
    fi
}

trap cleanup EXIT INT TERM

# ---------- Main Functions ----------

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
        
        # Atomic installation: write to temp file, then rename
        local tmp="${dst}.new"
        
        # Copy file
        if ! cp "$src" "$tmp"; then
            log_error "Failed to copy ${binary}"
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
        
        # Atomic rename
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

# ---------- Main ----------

main() {
    local version="${1:-}"
    
    # Check we're running as root
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi
    
    log_info "Starting LinuxIO binary installation"
    [[ -n "$version" ]] && log_info "Target version: ${version}" || log_info "Target version: latest"
    
    if ! download_binaries "$version"; then
        log_error "Download failed"
        exit 1
    fi
    
    if ! verify_checksums; then
        log_error "Checksum verification failed"
        exit 1
    fi
    
    if ! install_binaries; then
        log_error "Installation failed"
        exit 1
    fi
    
    verify_installation
    
    log_ok "Installation complete!"
    exit 0
}

# ---------- Usage ----------

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
        cat <<EOF
Usage: $(basename "$0") [VERSION]

Downloads and installs LinuxIO binaries from GitHub releases.

Arguments:
  VERSION    Optional release tag (e.g., v0.3.0). If omitted, installs latest.

Examples:
  $(basename "$0")           # Install latest release
  $(basename "$0") v0.3.0    # Install specific version

This script must be run as root.
EOF
        exit 0
    fi
    
    main "$@"
fi