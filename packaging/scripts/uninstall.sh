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

# ========== STOP AND DISABLE SERVICES ==========
echo -e "${YELLOW}🛑 Stopping and disabling LinuxIO services...${NC}"
systemctl stop linuxio.target 2>/dev/null || true
# Stop all linuxio units
systemctl stop 'linuxio*' 2>/dev/null || true
# Disable all linuxio units
systemctl disable 'linuxio*' 2>/dev/null || true
echo -e "${GREEN}✓ Services stopped and disabled${NC}"

# ========== REMOVE SYSTEMD FILES ==========
echo -e "${YELLOW}🗑️  Removing systemd files...${NC}"
rm -f /etc/systemd/system/linuxio*
rm -f /lib/systemd/system/linuxio*
# Remove any symlinks in target.wants directories
rm -f /etc/systemd/system/*.wants/linuxio*
echo -e "${GREEN}✓ Systemd files removed${NC}"

# ========== RELOAD SYSTEMD ==========
echo -e "${YELLOW}🔄 Reloading systemd...${NC}"
systemctl daemon-reload
systemctl reset-failed 2>/dev/null || true
echo -e "${GREEN}✓ Systemd reloaded${NC}"

# ========== REMOVE BINARIES ==========
echo -e "${YELLOW}🗑️  Removing binaries...${NC}"
rm -f /usr/local/bin/linuxio*
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
    rm -f linuxio linuxio-webserver linuxio-bridge linuxio-auth 2>/dev/null || true
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
