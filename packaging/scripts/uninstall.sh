#!/bin/bash
# =============================================================================
# LinuxIO Complete Uninstall Script
# Removes all LinuxIO files, services, and configurations
#  2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

trap 'echo -e "\e[0m"; exit 1' INT

# ---------- Colors & Styling ----------
readonly COLOUR_RESET='\e[0m'
readonly GREEN='\e[38;5;154m'
readonly BOLD='\e[1m'
readonly GREY='\e[90m'
readonly RED='\e[91m'
readonly YELLOW='\e[33m'

readonly LINE=" ${GREEN}───────────────────────────────────────────────────────${COLOUR_RESET}"

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

# ---------- Main ----------

Header "LinuxIO ${GREY}· Uninstaller${COLOUR_RESET}"

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    Show 1 "This script must be run as root"
fi

# ========== STOP AND DISABLE SERVICES ==========
Header "Stopping Services"
Show 2 "Stopping and disabling LinuxIO services..."
systemctl stop linuxio.target 2>/dev/null || true
systemctl stop 'linuxio*' 2>/dev/null || true
systemctl disable 'linuxio*' 2>/dev/null || true
Show 0 "Services stopped and disabled"

# ========== REMOVE SYSTEMD FILES ==========
Header "Removing Files"
Show 2 "Removing systemd files..."
rm -f /etc/systemd/system/linuxio*
rm -f /lib/systemd/system/linuxio*
rm -f /etc/systemd/system/*.wants/linuxio*
Show 0 "Systemd files removed"

# ========== RELOAD SYSTEMD ==========
Show 2 "Reloading systemd..."
systemctl daemon-reload
systemctl reset-failed 2>/dev/null || true
Show 0 "Systemd reloaded"

# ========== REMOVE BINARIES ==========
Show 2 "Removing binaries..."
rm -f /usr/local/bin/linuxio*
Show 0 "Binaries removed"

# ========== REMOVE CONFIGURATION FILES ==========
Show 2 "Removing configuration files..."
rm -rf /etc/linuxio
rm -f /etc/pam.d/linuxio
Show 0 "Configuration files removed"

# ========== REMOVE PCP CONFIGURATION ==========
Show 2 "Removing PCP configuration..."
rm -f /etc/pcp/pmlogger/config.d/linuxio.config
if systemctl is-active --quiet pmlogger 2>/dev/null; then
    systemctl restart pmlogger 2>/dev/null || true
fi
Show 0 "pmlogger config removed"

Show 2 "Removing pmproxy override..."
rm -f /etc/systemd/system/pmproxy.service.d/linuxio.conf
rmdir /etc/systemd/system/pmproxy.service.d 2>/dev/null || true
if systemctl is-active --quiet pmproxy 2>/dev/null; then
    systemctl daemon-reload
    systemctl restart pmproxy 2>/dev/null || true
fi
Show 0 "pmproxy override removed"

# ========== REMOVE RUNTIME FILES ==========
Show 2 "Removing runtime and data files..."
rm -rf /run/linuxio
rm -rf /usr/share/linuxio
rm -rf /var/lib/linuxIO
rm -f /etc/motd.d/linuxio 2>/dev/null || true
Show 0 "Runtime files removed"

# ========== REMOVE DEV FILES ==========
Show 2 "Removing development files..."
rm -rf /tmp/linuxio
rm -f /etc/sudoers.d/linuxio-dev
Show 0 "Development files removed"

# ========== CLEAN BUILD ARTIFACTS ==========
Show 2 "Cleaning build artifacts from repo..."
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ -f "$REPO_ROOT/makefile" || -f "$REPO_ROOT/Makefile" ]]; then
    cd "$REPO_ROOT"
    rm -f linuxio linuxio-webserver linuxio-bridge linuxio-auth linuxio-pcp-api 2>/dev/null || true
    Show 0 "Build artifacts cleaned"
else
    Show 3 "Cannot find repo directory, skipping build artifact cleanup"
fi

# ========== SUMMARY ==========
echo ""
echo -e "${LINE}"
echo -e " ${GREEN}${BOLD}LinuxIO completely uninstalled!${COLOUR_RESET}"
echo -e "${LINE}"
echo ""
echo -e " ${BOLD}Removed:${COLOUR_RESET}"
echo -e " ${GREEN}-${COLOUR_RESET} All systemd services and sockets"
echo -e " ${GREEN}-${COLOUR_RESET} All binaries from /usr/local/bin"
echo -e " ${GREEN}-${COLOUR_RESET} Configuration files from /etc/linuxio"
echo -e " ${GREEN}-${COLOUR_RESET} PCP derived metrics and pmproxy override"
echo -e " ${GREEN}-${COLOUR_RESET} PAM configuration"
echo -e " ${GREEN}-${COLOUR_RESET} Runtime files from /run and /var/lib"
echo -e " ${GREEN}-${COLOUR_RESET} Development files from /tmp/linuxio"
echo ""
echo -e " ${BOLD}To reinstall:${COLOUR_RESET} ${GREY}make localinstall${COLOUR_RESET}"
echo ""
