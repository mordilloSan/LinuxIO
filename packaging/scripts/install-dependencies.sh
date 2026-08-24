#!/usr/bin/env bash
# =============================================================================
# LinuxIO Dependencies Installer
# Installs mandatory runtime dependencies
#  2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
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
# Check if a package is already installed
pkg_installed() {
    if is_debian; then
        dpkg -s "$1" &>/dev/null
    elif is_fedora; then
        rpm -q "$1" &>/dev/null
    fi
}

# Install packages quietly — stdout hidden, stderr retained for error reporting
pkg_install() {
    local err
    if is_debian; then
        err=$(apt-get install -y -qq "$@" 2>&1 >/dev/null) || { echo "$err" >&2; return 1; }
    elif is_fedora; then
        err=$(dnf install -y -q "$@" 2>&1 >/dev/null) || { echo "$err" >&2; return 1; }
    fi
}

# Install a required dependency. Tries each candidate in order until one
# succeeds. When every candidate fails, aborts with the supplied consequence.
# Usage: install_pkg <display_name> <debian_pkg_candidates> <fedora_pkg_candidates> [consequence]
install_pkg() {
    local name="$1" deb_pkgs="$2" fed_pkgs="$3" consequence="${4:-required by LinuxIO}"
    local pkgs="" candidate=""

    if is_debian; then pkgs="$deb_pkgs"
    elif is_fedora; then pkgs="$fed_pkgs"
    fi

    for candidate in $pkgs; do
        if pkg_installed "$candidate"; then
            Show 0 "${name} ${GREY}already installed${COLOUR_RESET}"
            return 0
        fi
    done

    for candidate in $pkgs; do
        Show 2 "Installing ${name} (${candidate})..."
        if pkg_install "$candidate"; then
            Show 0 "${name} installed (${candidate})"
            return 0
        fi
    done

    Show 1 "${name}: installation failed — ${consequence}"
}

# ---------- Mandatory Dependencies ----------
install_mandatory() {
    Header "Mandatory Dependencies"

    if ! is_debian && ! is_fedora; then
        Show 1 "Unsupported distribution: ${DISTRO}"
    fi

    if is_debian; then
        Show 2 "Updating package lists..."
        if ! apt-get update -qq >/dev/null 2>&1; then
            Show 1 "Failed to update package lists"
        fi
        Show 0 "Package lists updated"
    fi

    install_pkg "PAM libraries" "libpam0g" "pam" \
        "PAM is required for LinuxIO authentication"
    install_pkg "PolicyKit" "polkitd policykit-1" "polkit" \
        "PolicyKit is required for LinuxIO authorization"
    install_pkg "PackageKit" "packagekit" "PackageKit" \
        "PackageKit is required for in-app capability installation"
}

# ---------- Main ----------
require_root() {
    if [[ $EUID -ne 0 ]]; then
        Show 1 "This script must be run as root"
    fi
}

main() {
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help) show_help; exit 0 ;;
            *) Show 1 "Unknown option: $1" ;;
        esac
    done

    require_root

    Header "LinuxIO ${GREY}· Dependencies Installer${COLOUR_RESET}"

    detect_distro
    Show 2 "Detected distribution: ${BOLD}${DISTRO}${COLOUR_RESET}"

    install_mandatory

    Show 2 "Optional capabilities are installed later in LinuxIO Capability Manager"

    echo ""
    echo -e "${LINE}"
    echo -e " ${GREEN}${BOLD}Installation complete!${COLOUR_RESET}"
    echo -e "${LINE}"
    echo ""
    echo -e " ${BOLD}Next step:${COLOUR_RESET} Install LinuxIO binaries with:"
    echo -e " ${GREY}curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh | sudo bash${COLOUR_RESET}"
    echo ""
}

# ---------- Usage ----------
show_help() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Installs dependencies required by LinuxIO.

Options:
  -h, --help    Show this help message

Mandatory (installed automatically; failures are fatal):
  - PAM libraries    (authentication)
  - PolicyKit        (authorization)
  - PackageKit       (software updates and in-app capability installation)

Other optional capabilities are installed from LinuxIO Capability Manager
after signing in. PackageKit is required for those in-app installations.

Docker is not installed by this script. Use Docker's separate installer when
container support is needed.

This script must be run as root.
EOF
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
