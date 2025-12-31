#!/bin/bash
# =============================================================================
# LinuxIO Complete Uninstall Script
# Removes all LinuxIO files, services, and configurations
# © 2025 Miguel Mariz (mordilloSan)
# =============================================================================

set -euo pipefail

# Colors
RED='\033[1;31m'
GREEN='\033[1;32m'
YELLOW='\033[1;33m'
BLUE='\033[1;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}════════════════════════════════════════════${NC}"
echo -e "${BLUE}  LinuxIO Complete Uninstall${NC}"
echo -e "${BLUE}════════════════════════════════════════════${NC}"
echo ""

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}✗ This script must be run as root${NC}"
    echo "  Please run: sudo $0"
    exit 1
fi

# ========== STOP SERVICES ==========
echo -e "${YELLOW}🛑 Stopping LinuxIO services...${NC}"
systemctl stop linuxio.service 2>/dev/null || true
systemctl stop linuxio.socket 2>/dev/null || true
systemctl stop linuxio-auth.socket 2>/dev/null || true
systemctl stop linuxio-auth@*.service 2>/dev/null || true
systemctl stop linuxio-bridge-socket-user.service 2>/dev/null || true
systemctl stop linuxio-issue.service 2>/dev/null || true
echo -e "${GREEN}✓ Services stopped${NC}"

# ========== DISABLE SERVICES ==========
echo -e "${YELLOW}🛑 Disabling LinuxIO services...${NC}"
systemctl disable linuxio.service 2>/dev/null || true
systemctl disable linuxio.socket 2>/dev/null || true
systemctl disable linuxio-auth.socket 2>/dev/null || true
echo -e "${GREEN}✓ Services disabled${NC}"

# ========== REMOVE SYSTEMD FILES ==========
echo -e "${YELLOW}🗑️  Removing systemd files...${NC}"
rm -f /etc/systemd/system/linuxio.service
rm -f /etc/systemd/system/linuxio.socket
rm -f /etc/systemd/system/linuxio-auth.socket
rm -f /etc/systemd/system/linuxio-auth@.service
rm -f /etc/systemd/system/linuxio-bridge-socket-user.service
rm -f /etc/systemd/system/linuxio-issue.service

# Also check /lib/systemd/system in case they were installed there
rm -f /lib/systemd/system/linuxio.service
rm -f /lib/systemd/system/linuxio.socket
rm -f /lib/systemd/system/linuxio-auth.socket
rm -f /lib/systemd/system/linuxio-auth@.service
rm -f /lib/systemd/system/linuxio-bridge-socket-user.service
rm -f /lib/systemd/system/linuxio-issue.service

echo -e "${GREEN}✓ Systemd files removed${NC}"

# ========== RELOAD SYSTEMD ==========
echo -e "${YELLOW}🔄 Reloading systemd...${NC}"
systemctl daemon-reload
systemctl reset-failed 2>/dev/null || true
echo -e "${GREEN}✓ Systemd reloaded${NC}"

# ========== REMOVE BINARIES ==========
echo -e "${YELLOW}🗑️  Removing binaries...${NC}"
rm -f /usr/local/bin/linuxio
rm -f /usr/local/bin/linuxio-bridge
rm -f /usr/local/bin/linuxio-auth
echo -e "${GREEN}✓ Binaries removed${NC}"

# ========== REMOVE CONFIGURATION FILES ==========
echo -e "${YELLOW}🗑️  Removing configuration files...${NC}"
rm -rf /etc/linuxio
rm -f /etc/pam.d/linuxio
echo -e "${GREEN}✓ Configuration files removed${NC}"

# ========== REMOVE RUNTIME FILES ==========
echo -e "${YELLOW}🗑️  Removing runtime and data files...${NC}"
rm -rf /run/linuxio
rm -rf /usr/share/linuxio
rm -rf /var/lib/linuxIO

# Remove motd symlink if it exists
rm -f /etc/motd.d/linuxio 2>/dev/null || true

echo -e "${GREEN}✓ Runtime files removed${NC}"

# ========== REMOVE OLD STATIC GROUP/USER (IF EXISTS) ==========
echo -e "${YELLOW}🗑️  Removing old static accounts (if they exist)...${NC}"

# Remove static linuxio user if it exists
if getent passwd linuxio >/dev/null 2>&1; then
    userdel linuxio 2>/dev/null || true
    echo -e "${GREEN}✓ Removed static 'linuxio' user${NC}"
fi

# Remove static linuxio group if it exists
if getent group linuxio >/dev/null 2>&1; then
    groupdel linuxio 2>/dev/null || true
    echo -e "${GREEN}✓ Removed static 'linuxio' group${NC}"
fi

# Remove user from linuxio group if they were added (for current non-root user)
if [[ -n "${SUDO_USER:-}" ]]; then
    if id -nG "$SUDO_USER" 2>/dev/null | tr ' ' '\n' | grep -qx linuxio; then
        gpasswd -d "$SUDO_USER" linuxio 2>/dev/null || true
        echo -e "${GREEN}✓ Removed $SUDO_USER from 'linuxio' group${NC}"
    fi
fi

# ========== REMOVE DEV FILES ==========
echo -e "${YELLOW}🗑️  Removing development files...${NC}"
rm -rf /tmp/linuxio
rm -f /etc/sudoers.d/linuxio-dev
echo -e "${GREEN}✓ Development files removed${NC}"

# ========== CLEAN BUILD ARTIFACTS ==========
echo -e "${YELLOW}🗑️  Cleaning build artifacts from repo...${NC}"
# This script is in packaging/scripts/, so go up two levels to repo root
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ -f "$REPO_ROOT/makefile" || -f "$REPO_ROOT/Makefile" ]]; then
    cd "$REPO_ROOT"
    rm -f linuxio linuxio-bridge linuxio-auth 2>/dev/null || true
    echo -e "${GREEN}✓ Build artifacts cleaned${NC}"
else
    echo -e "${YELLOW}⚠  Cannot find repo directory, skipping build artifact cleanup${NC}"
fi

# ========== SUMMARY ==========
echo ""
echo -e "${GREEN}════════════════════════════════════════════${NC}"
echo -e "${GREEN}✅ LinuxIO completely uninstalled!${NC}"
echo -e "${GREEN}════════════════════════════════════════════${NC}"
echo ""
echo "Removed:"
echo "  • All systemd services and sockets"
echo "  • All binaries from /usr/local/bin"
echo "  • Configuration files from /etc/linuxio"
echo "  • PAM configuration"
echo "  • Runtime files from /run and /var/lib"
echo "  • Development files from /tmp/linuxio"
echo "  • Static user/group accounts (if any)"
echo ""
echo "To reinstall LinuxIO, run: make localinstall"
echo "  or directly: sudo ./packaging/scripts/localinstall.sh"
echo ""
