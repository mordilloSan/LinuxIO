#!/usr/bin/env bash
# =============================================================================
# LinuxIO Dependencies Installer
# Installs all runtime dependencies required by LinuxIO
# © 2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

# ---------- Logging Functions ----------
log_info()  { printf "▸ %s\n" "$*"; }
log_ok()    { printf "✓ %s\n" "$*"; }
log_error() { printf "✗ %s\n" "$*" >&2; }
log_warn()  { printf "⚠ %s\n" "$*"; }

# ---------- Distro Detection ----------
detect_distro() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        echo "${ID:-unknown}"
    elif [[ -f /etc/debian_version ]]; then
        echo "debian"
    elif [[ -f /etc/redhat-release ]]; then
        echo "rhel"
    else
        echo "unknown"
    fi
}

# ---------- Package Installation ----------
install_debian_packages() {
    log_info "Updating package lists..."
    apt-get update -qq

    log_info "Installing dependencies..."
    apt-get install -y \
        lm-sensors \
        libpam0g \
        policykit-1 \
        smartmontools \
        curl

    log_ok "Debian/Ubuntu packages installed"
}

install_fedora_packages() {
    log_info "Installing dependencies..."
    dnf install -y \
        lm_sensors \
        pam \
        polkit \
        smartmontools \
        curl

    log_ok "Fedora/RHEL packages installed"
}

# ---------- Docker Installation ----------
install_docker() {
    if command -v docker &>/dev/null; then
        log_ok "Docker is already installed: $(docker --version)"
        return 0
    fi

    log_info "Installing Docker using official script..."

    if ! curl -fsSL https://get.docker.com | sh; then
        log_error "Docker installation failed"
        return 1
    fi

    # Enable and start Docker service
    if command -v systemctl &>/dev/null; then
        log_info "Enabling Docker service..."
        systemctl enable docker
        systemctl start docker
    fi

    log_ok "Docker installed: $(docker --version)"
}

# ---------- Post-Installation ----------
configure_sensors() {
    log_info "Detecting hardware sensors..."
    if command -v sensors-detect &>/dev/null; then
        # Run sensors-detect with auto-accept (safe defaults)
        yes "" | sensors-detect --auto &>/dev/null || true
        log_ok "Sensors configured"
    fi
}

# ---------- Main ----------
main() {
    # Check we're running as root
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi

    log_info "Starting LinuxIO dependencies installation"

    local distro
    distro=$(detect_distro)
    log_info "Detected distribution: ${distro}"

    case "$distro" in
        ubuntu|debian|linuxmint|pop)
            install_debian_packages
            ;;
        fedora|rhel|centos|rocky|almalinux)
            install_fedora_packages
            ;;
        *)
            log_error "Unsupported distribution: ${distro}"
            log_error "Please install dependencies manually:"
            log_error "  - Docker"
            log_error "  - lm-sensors"
            log_error "  - PAM libraries"
            log_error "  - PolicyKit"
            log_error "  - smartmontools"
            exit 1
            ;;
    esac

    install_docker
    configure_sensors

    log_ok "All dependencies installed successfully!"
    log_info ""
    log_info "Next step: Install LinuxIO binaries with:"
    log_info "  curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh | sudo bash"
}

# ---------- Usage ----------
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    cat <<EOF
Usage: $(basename "$0")

Installs all runtime dependencies required by LinuxIO:
  - Docker (via https://get.docker.com)
  - lm-sensors (hardware monitoring)
  - PAM libraries (authentication)
  - PolicyKit (authorization)
  - smartmontools (disk SMART data)

This script must be run as root.
EOF
    exit 0
fi

main "$@"
