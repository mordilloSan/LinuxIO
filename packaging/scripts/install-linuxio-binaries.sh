#!/usr/bin/env bash
# =============================================================================
# LinuxIO Binary Installer (Debug Version)
# Downloads and installs LinuxIO binaries with proper permissions
# © 2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

# ---------- Configuration ----------
readonly REPO_OWNER="mordilloSan"
readonly REPO_NAME="LinuxIO"
readonly BIN_DIR="/usr/local/bin"
readonly STAGING="/tmp/linuxio-install-$$"

# ---------- Logging Functions ----------
log_info()  { echo "[INFO] $*"; }
log_ok()    { echo "[OK] $*"; }
log_error() { echo "[ERROR] $*" >&2; }
log_warn()  { echo "[WARN] $*"; }

cleanup() {
    if [[ -d "$STAGING" ]]; then
        rm -rf "$STAGING" 2>/dev/null || true
    fi
}

trap cleanup EXIT INT TERM

# ---------- Diagnostic Functions ----------

check_environment() {
    log_info "=== Environment Diagnostics ==="
    log_info "Script: $0"
    log_info "Running as: $(whoami) (UID: $(id -u))"
    log_info "Effective UID: $EUID"
    log_info "Real UID: $(id -ru)"
    log_info "Groups: $(groups)"
    log_info "Current directory: $(pwd)"
    
    if [[ $EUID -ne 0 ]]; then
        log_error "Not running as root! Current EUID: $EUID"
        log_info "Hint: Run with 'sudo bash $0' or ensure script has proper privileges"
        return 1
    fi
    
    log_ok "Running with root privileges"
    
    log_info "=== Filesystem Diagnostics ==="
    log_info "Mount points involving /usr:"
    mount | grep -E '(^/ | /usr)' || log_warn "No specific /usr mounts found"
    
    log_info "Filesystem for ${BIN_DIR}:"
    df -h "$BIN_DIR" 2>&1 || log_error "df command failed"
    
    log_info "Directory info for ${BIN_DIR}:"
    ls -ld "$BIN_DIR" 2>&1 || log_error "Cannot list ${BIN_DIR}"
    
    log_info "Parent directory info:"
    ls -ld "$(dirname "$BIN_DIR")" 2>&1 || log_error "Cannot list parent"
    
    # Check if directory is writable
    if [[ -w "$BIN_DIR" ]]; then
        log_ok "${BIN_DIR} is writable by current user"
    else
        log_error "${BIN_DIR} is NOT writable!"
    fi
    
    # Try a test write
    log_info "Attempting test write to ${BIN_DIR}..."
    local test_file="${BIN_DIR}/.linuxio-write-test-$$"
    if touch "$test_file" 2>/dev/null; then
        log_ok "Test file created successfully"
        rm -f "$test_file"
    else
        log_error "Cannot create test file in ${BIN_DIR}"
        log_info "Trying to see why..."
        
        # Check for immutable flag
        if command -v lsattr >/dev/null 2>&1; then
            log_info "Attributes: $(lsattr -d "$BIN_DIR" 2>&1)"
        fi
        
        # Check mount options
        log_info "Mount options: $(mount | grep "$(df "$BIN_DIR" | tail -1 | awk '{print $1}')")"
    fi
    
    # Check for existing binaries
    log_info "=== Existing LinuxIO binaries ==="
    for binary in linuxio linuxio-bridge linuxio-auth-helper; do
        local path="${BIN_DIR}/${binary}"
        if [[ -f "$path" ]]; then
            log_info "${binary}: $(ls -lh "$path")"
            if lsof "$path" 2>/dev/null | grep -q "$path"; then
                log_warn "${binary} is currently running:"
                lsof "$path" 2>&1 | tail -n +2 || true
            fi
        else
            log_info "${binary}: not found"
        fi
    done
    
    # Check /tmp filesystem
    log_info "=== /tmp Filesystem ==="
    log_info "/tmp mount: $(mount | grep ' /tmp ' || echo 'not separately mounted')"
    df -h /tmp 2>&1 || log_error "Cannot check /tmp disk space"
    
    return 0
}

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
    log_info "=== Installing binaries to ${BIN_DIR} ==="
    
    # Ensure target directory exists
    mkdir -p "$BIN_DIR" || {
        log_error "Failed to create ${BIN_DIR}"
        return 1
    }
    
    # Define binaries with their permissions
    # linuxio-auth-helper needs setuid (4755) to run as root
    local -A binaries=(
        ["linuxio"]="0755"
        ["linuxio-bridge"]="0755"
        ["linuxio-auth-helper"]="4755"
    )
    
    for binary in "${!binaries[@]}"; do
        log_info "--- Processing ${binary} ---"
        
        local src="${STAGING}/${binary}"
        local dst="${BIN_DIR}/${binary}"
        local mode="${binaries[$binary]}"
        
        if [[ ! -f "$src" ]]; then
            log_error "Source file not found: ${src}"
            return 1
        fi
        
        local src_size
        src_size=$(stat -c '%s' "$src" 2>/dev/null || stat -f '%z' "$src" 2>/dev/null || echo "unknown")
        log_info "Source: ${src} (${src_size} bytes)"
        log_info "Destination: ${dst}"
        log_info "Mode: ${mode}"
        
        # Check if target exists and is in use
        if [[ -f "$dst" ]]; then
            log_info "Target exists: $(ls -lh "$dst")"
            if lsof "$dst" >/dev/null 2>&1; then
                log_warn "Binary is currently in use!"
                lsof "$dst" 2>&1 | tail -n +2 || true
            fi
        else
            log_info "Target does not exist (fresh install)"
        fi
        
        # Use /tmp for staging
        local tmp="/tmp/linuxio-stage-${binary}-$$"
        log_info "Staging file: ${tmp}"
        
        # Copy to staging
        log_info "Copying to staging..."
        if ! cp "$src" "$tmp"; then
            log_error "Failed to copy to staging area"
            return 1
        fi
        
        # Set permissions
        log_info "Setting permissions (${mode})..."
        if ! chmod "$mode" "$tmp"; then
            log_error "Failed to chmod"
            rm -f "$tmp"
            return 1
        fi
        
        # Set ownership
        log_info "Setting ownership (root:root)..."
        if ! chown root:root "$tmp"; then
            log_error "Failed to chown"
            rm -f "$tmp"
            return 1
        fi
        
        log_info "Staged file ready: $(ls -lh "$tmp")"
        
        # If target exists, try to remove it first
        if [[ -f "$dst" ]]; then
            log_info "Removing existing target..."
            if rm -f "$dst" 2>/dev/null; then
                log_ok "Existing file removed"
            else
                log_error "Cannot remove ${dst}"
                log_info "Trying with explicit privileges..."
                if ! sudo rm -f "$dst" 2>/dev/null; then
                    log_error "Failed to remove even with sudo"
                    log_info "File details: $(ls -l "$dst" 2>&1)"
                    
                    # Check for special attributes
                    if command -v lsattr >/dev/null 2>&1; then
                        log_info "Attributes: $(lsattr "$dst" 2>&1)"
                    fi
                    
                    # Check what's locking it
                    if command -v fuser >/dev/null 2>&1; then
                        log_info "Processes using file: $(fuser "$dst" 2>&1 || echo 'none')"
                    fi
                    
                    rm -f "$tmp"
                    return 1
                fi
                log_ok "Removed with sudo"
            fi
        fi
        
        # Now move the staged file
        log_info "Moving staged file to destination..."
        if mv "$tmp" "$dst"; then
            log_ok "File moved successfully"
        else
            local mv_exit=$?
            log_error "Move failed with exit code: ${mv_exit}"
            log_info "Destination directory state:"
            ls -la "$BIN_DIR" | head -20
            log_info "Staging file still exists: $(ls -lh "$tmp" 2>&1 || echo 'no')"
            rm -f "$tmp"
            return 1
        fi
        
        # Verify final state
        log_info "Verifying installation..."
        if [[ -f "$dst" ]]; then
            log_ok "File exists: $(ls -lh "$dst")"
            
            # Re-apply permissions to be sure
            chmod "$mode" "$dst" || log_warn "Failed to re-apply chmod"
            
            # Verify it's executable
            if [[ -x "$dst" ]]; then
                log_ok "${binary} is executable"
            else
                log_error "${binary} is NOT executable!"
            fi
        else
            log_error "File does NOT exist after installation!"
            return 1
        fi
        
        log_ok "Successfully installed ${binary}"
    done
    
    log_ok "All binaries installed successfully"
    return 0
}

verify_installation() {
    log_info "=== Post-Installation Verification ==="
    
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
    
    log_info "========================================="
    log_info "LinuxIO Binary Installer (Debug Mode)"
    log_info "========================================="
    
    # Run diagnostics first
    if ! check_environment; then
        log_error "Environment check failed"
        exit 1
    fi
    
    log_info ""
    log_info "========================================="
    log_info "Starting Installation Process"
    log_info "========================================="
    
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
    
    log_info ""
    log_ok "========================================="
    log_ok "Installation Complete!"
    log_ok "========================================="
    exit 0
}

# ---------- Usage ----------

if [[ -z "${BASH_SOURCE+x}" || "${BASH_SOURCE[0]-}" == "$0" ]]; then
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