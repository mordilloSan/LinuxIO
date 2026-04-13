#!/usr/bin/env bash
# =============================================================================
# LinuxIO Dependencies Installer
# Installs mandatory runtime dependencies and optionally installs extras
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

# ---------- User Prompt ----------
ask_yes_no() {
    local prompt="$1"
    local default="${2:-y}"
    local yn

    if [[ "$default" == "y" ]]; then
        prompt="${BOLD}${prompt}${COLOUR_RESET} ${GREY}[Y/n]${COLOUR_RESET} "
    else
        prompt="${BOLD}${prompt}${COLOUR_RESET} ${GREY}[y/N]${COLOUR_RESET} "
    fi

    echo ""
    read -rp "$(echo -e " $prompt")" yn
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
# Check if a package is already installed
pkg_installed() {
    if is_debian; then
        dpkg -s "$1" &>/dev/null
    elif is_fedora; then
        rpm -q "$1" &>/dev/null
    fi
}

# Install packages quietly — stdout hidden, stderr captured for error reporting
pkg_install() {
    local err
    if is_debian; then
        err=$(apt-get install -y -qq "$@" 2>&1 >/dev/null) || { echo "$err" >&2; return 1; }
    elif is_fedora; then
        err=$(dnf install -y -q "$@" 2>&1 >/dev/null) || { echo "$err" >&2; return 1; }
    fi
}

# Install a named dependency: show "already installed" or install quietly
# Usage: install_pkg <display_name> <debian_pkg> <fedora_pkg>
install_pkg() {
    local name="$1" deb_pkg="$2" fed_pkg="$3"
    local pkg=""

    if is_debian; then pkg="$deb_pkg"
    elif is_fedora; then pkg="$fed_pkg"
    fi

    if pkg_installed "$pkg"; then
        Show 0 "${name} ${GREY}already installed${COLOUR_RESET}"
    else
        Show 2 "Installing ${name}..."
        if pkg_install "$pkg"; then
            Show 0 "${name} installed"
        else
            Show 1 "Failed to install ${name}"
        fi
    fi
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

    install_pkg "PAM libraries" "libpam0g" "pam"
    install_pkg "PolicyKit" "policykit-1" "polkit"
    install_pkg "PackageKit" "packagekit" "PackageKit"
}

# ---------- Optional Dependencies ----------
install_lm_sensors() {
    install_pkg "lm-sensors" "lm-sensors" "lm_sensors"
    if command -v sensors-detect &>/dev/null; then
        Show 2 "Detecting hardware sensors..."
        yes "" | sensors-detect --auto &>/dev/null || true
        Show 0 "Sensors configured"
    fi
}

install_smartmontools() {
    install_pkg "smartmontools" "smartmontools" "smartmontools"
}

install_nfs() {
    install_pkg "NFS utilities" "nfs-common" "nfs-utils"
}

install_docker() {
    if command -v docker &>/dev/null; then
        Show 0 "Docker ${GREY}already installed ($(docker --version 2>/dev/null | sed 's/Docker version //'))${COLOUR_RESET}"
        return 0
    fi

    Show 2 "Installing Docker..."
    if ! curl -fsSL https://get.docker.com 2>/dev/null | sh >/dev/null 2>&1; then
        Show 3 "Docker installation failed"
        return 1
    fi

    if command -v systemctl &>/dev/null; then
        systemctl enable docker >/dev/null 2>&1
        systemctl start docker >/dev/null 2>&1
    fi

    Show 0 "Docker installed"
}

install_indexer() {
    if command -v indexer &>/dev/null; then
        Show 0 "Indexer ${GREY}already installed${COLOUR_RESET}"
        return 0
    fi

    Show 2 "Installing Indexer..."
    if ! curl -fsSL https://github.com/mordilloSan/indexer/releases/latest/download/indexer-install.sh 2>/dev/null | bash >/dev/null 2>&1; then
        Show 3 "Indexer installation failed"
        return 1
    fi

    Show 0 "Indexer installed"
}

install_all_optional() {
    Header "Optional Dependencies"

    install_lm_sensors
    install_smartmontools
    install_nfs
    install_docker
    install_indexer
}

# ---------- Optional prompt ----------
prompt_optional() {
    echo ""
    echo -e " ${BOLD}Optional dependencies enable extra features:${COLOUR_RESET}"
    echo -e "${BULLET} lm-sensors       ${GREY}hardware temperature/voltage monitoring${COLOUR_RESET}"
    echo -e "${BULLET} smartmontools    ${GREY}disk SMART health data${COLOUR_RESET}"
    echo -e "${BULLET} NFS utilities    ${GREY}mount/browse NFS shares${COLOUR_RESET}"
    echo -e "${BULLET} Docker           ${GREY}container management${COLOUR_RESET}"
    echo -e "${BULLET} Indexer          ${GREY}file search and directory size indexing${COLOUR_RESET}"

    if ask_yes_no "Install all optional dependencies?"; then
        install_all_optional
    else
        Show 3 "Skipped optional dependencies — you can install them later"
    fi
}

# ---------- Main ----------
main() {
    local install_all=0

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -a|--all) install_all=1; shift ;;
            -h|--help) show_help; exit 0 ;;
            *) shift ;;
        esac
    done

    if [[ $EUID -ne 0 ]]; then
        Show 1 "This script must be run as root"
    fi

    Header "LinuxIO ${GREY}· Dependencies Installer${COLOUR_RESET}"

    detect_distro
    Show 2 "Detected distribution: ${BOLD}${DISTRO}${COLOUR_RESET}"

    install_mandatory

    if [[ $install_all -eq 1 ]]; then
        install_all_optional
    else
        prompt_optional
    fi

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
  -a, --all     Install everything (mandatory + optional) without prompting
  -h, --help    Show this help message

Mandatory (installed automatically):
  - PAM libraries    (authentication)
  - PolicyKit        (authorization)
  - PackageKit       (software updates)

Optional (prompted interactively, or use --all):
  - lm-sensors       (hardware temperature/voltage monitoring)
  - smartmontools    (disk SMART health data)
  - NFS utilities    (mount/browse NFS shares)
  - Docker           (container management)
  - Indexer          (file search and directory size indexing)

This script must be run as root.
EOF
}

main "$@"
