#!/bin/bash
# =============================================================================
# LinuxIO Local Build and Install Script
# Builds and installs LinuxIO from local source code
# © 2025 Miguel Mariz (mordilloSan)
# =============================================================================

set -euo pipefail

# Colors
RED='\033[1;31m'
GREEN='\033[1;32m'
YELLOW='\033[1;33m'
BLUE='\033[1;34m'
CYAN='\033[1;36m'
NC='\033[0m' # No Color

# Configuration
# This script is in packaging/scripts/, so go up two levels to repo root
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PORT=8090

echo -e "${BLUE}════════════════════════════════════════════${NC}"
echo -e "${BLUE}  LinuxIO Local Build and Install${NC}"
echo -e "${BLUE}════════════════════════════════════════════${NC}"
echo ""

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}✗ This script must be run as root${NC}"
    echo "  Please run: sudo $0"
    exit 1
fi

# Check if we're in the LinuxIO repository
if [[ ! -f "$REPO_ROOT/makefile" && ! -f "$REPO_ROOT/Makefile" ]]; then
    echo -e "${RED}✗ Cannot find LinuxIO repository root${NC}"
    echo -e "${RED}  Expected at: $REPO_ROOT${NC}"
    exit 1
fi

cd "$REPO_ROOT"

# ========== VERIFY BUILD ==========
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo -e "${CYAN}  Step 1/2: Verifying built binaries${NC}"
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo ""

# Verify binaries exist (should be built by 'make build' already)
MISSING_BINARIES=0
for binary in linuxio linuxio-webserver linuxio-bridge linuxio-auth; do
    if [[ ! -f "$REPO_ROOT/$binary" ]]; then
        echo -e "${RED}✗ Binary not found: $binary${NC}"
        MISSING_BINARIES=1
    else
        echo -e "${GREEN}✓ Found $binary${NC}"
    fi
done

if [[ $MISSING_BINARIES -eq 1 ]]; then
    echo ""
    echo -e "${RED}✗ Missing binaries! Please run 'make build' first.${NC}"
    echo -e "${YELLOW}  Or use 'make localinstall' which builds automatically.${NC}"
    exit 1
fi

echo -e "${GREEN}✅ All binaries verified!${NC}"

# ========== INSTALL ==========
echo ""
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo -e "${CYAN}  Step 2/2: Installing files${NC}"
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo ""

# Install binaries
echo -e "${YELLOW}📦 Installing binaries to /usr/local/bin...${NC}"
install -o root -g root -m 0755 "$REPO_ROOT/linuxio" /usr/local/bin/linuxio
install -o root -g root -m 0755 "$REPO_ROOT/linuxio-webserver" /usr/local/bin/linuxio-webserver
install -o root -g root -m 0755 "$REPO_ROOT/linuxio-bridge" /usr/local/bin/linuxio-bridge
install -o root -g root -m 0755 "$REPO_ROOT/linuxio-auth" /usr/local/bin/linuxio-auth
echo -e "${GREEN}✓ Binaries installed${NC}"

# Install systemd files
echo -e "${YELLOW}📦 Installing systemd service files...${NC}"
for file in linuxio.target linuxio-webserver.service linuxio-webserver.socket \
            linuxio-auth.socket linuxio-auth@.service \
            linuxio-bridge-socket-user.service \
            linuxio-issue.service; do
    if [[ -f "$REPO_ROOT/packaging/systemd/$file" ]]; then
        install -m 0644 "$REPO_ROOT/packaging/systemd/$file" /etc/systemd/system/
        echo "  • Installed $file"
    else
        echo -e "${YELLOW}  ⚠  Warning: $file not found in packaging/systemd/${NC}"
    fi
done
echo -e "${GREEN}✓ Systemd files installed${NC}"

# Install tmpfiles.d configuration
echo -e "${YELLOW}📦 Installing tmpfiles.d configuration...${NC}"
mkdir -p /usr/lib/tmpfiles.d
if [[ -f "$REPO_ROOT/packaging/systemd/linuxio-tmpfiles.conf" ]]; then
    install -m 0644 "$REPO_ROOT/packaging/systemd/linuxio-tmpfiles.conf" /usr/lib/tmpfiles.d/linuxio.conf
    echo "  • Installed /usr/lib/tmpfiles.d/linuxio.conf"
    # Create the directories now (don't wait for reboot)
    systemd-tmpfiles --create /usr/lib/tmpfiles.d/linuxio.conf 2>/dev/null || true
    echo -e "${GREEN}✓ Tmpfiles.d configuration installed${NC}"
else
    echo -e "${YELLOW}  ⚠  Warning: linuxio-tmpfiles.conf not found${NC}"
fi

# Install configuration files
echo -e "${YELLOW}📦 Installing configuration files...${NC}"
mkdir -p /etc/linuxio

if [[ -f "$REPO_ROOT/packaging/etc/linuxio/disallowed-users" ]]; then
    install -m 0644 "$REPO_ROOT/packaging/etc/linuxio/disallowed-users" /etc/linuxio/
    echo "  • Installed /etc/linuxio/disallowed-users"
else
    echo -e "${YELLOW}  ⚠  Warning: disallowed-users file not found${NC}"
fi

# Install any other config files in packaging/etc/linuxio/
if [[ -d "$REPO_ROOT/packaging/etc/linuxio" ]]; then
    for file in "$REPO_ROOT/packaging/etc/linuxio"/*; do
        if [[ -f "$file" && "$(basename "$file")" != "disallowed-users" ]]; then
            install -m 0644 "$file" /etc/linuxio/
            echo "  • Installed /etc/linuxio/$(basename "$file")"
        fi
    done
fi

echo -e "${GREEN}✓ Configuration files installed${NC}"

# Install PAM configuration
echo -e "${YELLOW}📦 Installing PAM configuration...${NC}"
if [[ -f "$REPO_ROOT/packaging/etc/pam.d/linuxio" ]]; then
    install -m 0644 "$REPO_ROOT/packaging/etc/pam.d/linuxio" /etc/pam.d/
    echo "  • Installed /etc/pam.d/linuxio"
    echo -e "${GREEN}✓ PAM configuration installed${NC}"
else
    echo -e "${RED}✗ PAM configuration not found at packaging/etc/pam.d/linuxio${NC}"
    exit 1
fi

# Install issue updater script
echo -e "${YELLOW}📦 Installing issue updater...${NC}"
mkdir -p /usr/share/linuxio/issue
if [[ -f "$REPO_ROOT/packaging/scripts/update-issue" ]]; then
    install -m 0755 "$REPO_ROOT/packaging/scripts/update-issue" /usr/share/linuxio/issue/
    echo "  • Installed /usr/share/linuxio/issue/update-issue"
    echo -e "${GREEN}✓ Issue updater installed${NC}"
else
    echo -e "${YELLOW}⚠  Warning: update-issue script not found${NC}"
fi

# Create symlink for SSH login banner
if [[ -d /etc/motd.d ]]; then
    ln -sf ../../run/linuxio/issue /etc/motd.d/linuxio 2>/dev/null || true
    echo "  • Created SSH login banner symlink"
fi

# ========== ENABLE AND START ==========
echo ""
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo -e "${CYAN}  Enabling and starting services${NC}"
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo ""

echo -e "${YELLOW}🔄 Reloading systemd...${NC}"
systemctl daemon-reload
echo -e "${GREEN}✓ Systemd reloaded${NC}"

echo -e "${YELLOW}✅ Enabling services...${NC}"
systemctl enable linuxio.target
echo -e "${GREEN}✓ Services enabled${NC}"

echo -e "${YELLOW}🚀 Starting LinuxIO...${NC}"
systemctl start linuxio.target

# Wait a moment for service to start
sleep 2

# Check if service is running
if systemctl is-active --quiet linuxio-webserver.service; then
    echo -e "${GREEN}✓ LinuxIO service started successfully${NC}"
else
    echo -e "${YELLOW}⚠  Warning: Service may not have started properly${NC}"
fi

# ========== SUMMARY ==========
echo ""
echo -e "${GREEN}════════════════════════════════════════════${NC}"
echo -e "${GREEN}✅ Installation Complete!${NC}"
echo -e "${GREEN}════════════════════════════════════════════${NC}"
echo ""
echo "Installed components:"
echo "  • Binaries:        /usr/local/bin/{linuxio,linuxio-webserver,linuxio-bridge,linuxio-auth}"
echo "  • Systemd files:   /etc/systemd/system/linuxio*"
echo "  • Configuration:   /etc/linuxio/"
echo "  • PAM config:      /etc/pam.d/linuxio"
echo "  • Issue updater:   /usr/share/linuxio/issue/"
echo ""
echo -e "${CYAN}🌐 Access LinuxIO at: https://localhost:${PORT}${NC}"
echo ""
echo "Useful commands:"
echo "  • Check status:  linuxio status"
echo "  • View logs:     linuxio logs"
echo "  • Restart:       sudo linuxio restart"
echo "  • Stop:          sudo linuxio stop"
echo ""
