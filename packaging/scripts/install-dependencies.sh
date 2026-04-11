#!/usr/bin/env bash
# =============================================================================
# LinuxIO Dependencies Installer
# Installs mandatory runtime dependencies and optionally installs extras
#  2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

# ---------- Logging Functions ----------
log_info()  { printf "▸ %s\n" "$*"; }
log_ok()    { printf "✓ %s\n" "$*"; }
log_error() { printf "✗ %s\n" "$*" >&2; }
log_warn()  { printf "⚠ %s\n" "$*"; }
log_skip()  { printf "– %s\n" "$*"; }

# ---------- User Prompt ----------
ask_yes_no() {
    local prompt="$1"
    local default="${2:-y}"
    local yn

    if [[ "$default" == "y" ]]; then
        prompt="$prompt [Y/n] "
    else
        prompt="$prompt [y/N] "
    fi

    read -rp "$prompt" yn
    yn="${yn:-$default}"
    [[ "${yn,,}" == "y" || "${yn,,}" == "yes" ]]
}

# ---------- Distro Detection ----------
DISTRO=""
detect_distro() {
    if [[ -f /etc/os-release ]]; then
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

is_debian() {
    case "$DISTRO" in
        ubuntu|debian|linuxmint|pop) return 0 ;;
        *) return 1 ;;
    esac
}

is_fedora() {
    case "$DISTRO" in
        fedora|rhel|centos|rocky|almalinux) return 0 ;;
        *) return 1 ;;
    esac
}

# ---------- Package helpers ----------
pkg_install() {
    if is_debian; then
        apt-get install -y "$@"
    elif is_fedora; then
        dnf install -y "$@"
    fi
}

# ---------- Mandatory Dependencies ----------
install_mandatory() {
    log_info "Installing mandatory dependencies..."

    if is_debian; then
        apt-get update -qq
        pkg_install libpam0g policykit-1 packagekit
    elif is_fedora; then
        pkg_install pam polkit PackageKit
    else
        log_error "Unsupported distribution: ${DISTRO}"
        log_error "Please install the following mandatory dependencies manually:"
        log_error "  - PAM libraries (authentication)"
        log_error "  - PolicyKit (authorization)"
        log_error "  - PackageKit (software updates)"
        exit 1
    fi

    log_ok "Mandatory dependencies installed"
}

# ---------- Optional Dependencies ----------
install_lm_sensors() {
    if is_debian; then
        pkg_install lm-sensors
    elif is_fedora; then
        pkg_install lm_sensors
    fi
    # Auto-detect sensors
    if command -v sensors-detect &>/dev/null; then
        log_info "Detecting hardware sensors..."
        yes "" | sensors-detect --auto &>/dev/null || true
    fi
    log_ok "lm-sensors installed and configured"
}

install_smartmontools() {
    pkg_install smartmontools
    log_ok "smartmontools installed"
}

install_pcp() {
    pkg_install pcp
    # Enable PCP services — LinuxIO reads archives via libpcp directly
    if command -v systemctl &>/dev/null; then
        systemctl enable --now pmcd pmlogger 2>/dev/null || true
    fi
    log_ok "PCP installed and services enabled (pmcd, pmlogger)"
}

install_nfs() {
    if is_debian; then
        pkg_install nfs-common
    elif is_fedora; then
        pkg_install nfs-utils
    fi
    log_ok "NFS utilities installed"
}

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

    if command -v systemctl &>/dev/null; then
        systemctl enable docker
        systemctl start docker
    fi

    log_ok "Docker installed: $(docker --version)"
}

# ---------- Optional dependencies ----------
prompt_optional() {
    echo ""
    log_info "Optional dependencies enable extra features in LinuxIO:"
    log_info "  - lm-sensors       (hardware temperature/voltage monitoring)"
    log_info "  - smartmontools    (disk SMART health data)"
    log_info "  - PCP              (CPU, memory, network, disk history charts)"
    log_info "  - NFS utilities    (mount/browse NFS shares)"
    log_info "  - Docker           (container management)"
    echo ""

    if ask_yes_no "Install all optional dependencies?"; then
        install_lm_sensors
        install_smartmontools
        install_pcp
        install_nfs
        install_docker
    else
        log_skip "Skipped optional dependencies — you can install them later"
    fi
}

# ---------- Main ----------
main() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi

    log_info "Starting LinuxIO dependencies installation"

    detect_distro
    log_info "Detected distribution: ${DISTRO}"

    install_mandatory
    prompt_optional

    echo ""
    log_ok "Installation complete!"
    log_info ""
    log_info "Next step: Install LinuxIO binaries with:"
    log_info "  curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh | sudo bash"
}

# ---------- Usage ----------
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    cat <<EOF
Usage: $(basename "$0")

Installs dependencies required by LinuxIO.

Mandatory (installed automatically):
  - PAM libraries    (authentication)
  - PolicyKit        (authorization)
  - PackageKit       (software updates)

Optional (you will be prompted):
  - lm-sensors       (hardware temperature/voltage monitoring)
  - smartmontools    (disk SMART health data)
  - PCP              (CPU, memory, network, disk history charts)
  - NFS utilities    (mount/browse NFS shares)
  - Docker           (container management)

This script must be run as root.
EOF
    exit 0
fi

main "$@"
