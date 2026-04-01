#!/bin/bash
# =============================================================================
# LinuxIO Local Build and Install Script
# Builds and installs LinuxIO from local source code
#  2025 Miguel Mariz (mordilloSan)
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

echo -e "${GREEN} All binaries verified!${NC}"

# ========== INSTALL ==========
echo ""
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo -e "${CYAN}  Step 2/2: Installing files${NC}"
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo ""

# Install binaries
echo -e "${YELLOW} Installing binaries to /usr/local/bin...${NC}"
install -o root -g root -m 0755 "$REPO_ROOT/linuxio" /usr/local/bin/linuxio
install -o root -g root -m 0755 "$REPO_ROOT/linuxio-webserver" /usr/local/bin/linuxio-webserver
install -o root -g root -m 0755 "$REPO_ROOT/linuxio-bridge" /usr/local/bin/linuxio-bridge
install -o root -g root -m 0755 "$REPO_ROOT/linuxio-auth" /usr/local/bin/linuxio-auth
echo -e "${GREEN}✓ Binaries installed${NC}"

# Install systemd files
echo -e "${YELLOW} Installing systemd service files...${NC}"
for file in linuxio.target linuxio-webserver.service linuxio-webserver.socket \
            linuxio-auth.socket linuxio-auth@.service \
            linuxio-bridge-socket-user.service \
            linuxio-issue.service linuxio-monitoring.service; do
    if [[ -f "$REPO_ROOT/packaging/systemd/$file" ]]; then
        install -m 0644 "$REPO_ROOT/packaging/systemd/$file" /etc/systemd/system/
        echo "  • Installed $file"
    else
        echo -e "${YELLOW}    Warning: $file not found in packaging/systemd/${NC}"
    fi
done
echo -e "${GREEN}✓ Systemd files installed${NC}"

# Install tmpfiles.d configuration
echo -e "${YELLOW} Installing tmpfiles.d configuration...${NC}"
mkdir -p /usr/lib/tmpfiles.d
if [[ -f "$REPO_ROOT/packaging/systemd/linuxio-tmpfiles.conf" ]]; then
    install -m 0644 "$REPO_ROOT/packaging/systemd/linuxio-tmpfiles.conf" /usr/lib/tmpfiles.d/linuxio.conf
    echo "  • Installed /usr/lib/tmpfiles.d/linuxio.conf"
    # Create the directories now (don't wait for reboot)
    systemd-tmpfiles --create /usr/lib/tmpfiles.d/linuxio.conf 2>/dev/null || true
    echo -e "${GREEN}✓ Tmpfiles.d configuration installed${NC}"
else
    echo -e "${YELLOW}    Warning: linuxio-tmpfiles.conf not found${NC}"
fi

# Install configuration files
echo -e "${YELLOW} Installing configuration files...${NC}"
if [[ -d "$REPO_ROOT/packaging/etc/linuxio" ]]; then
    while IFS= read -r file; do
        rel_path="${file#$REPO_ROOT/packaging/etc/linuxio/}"
        dest_path="/etc/linuxio/$rel_path"
        install -D -o root -g root -m 0644 "$file" "$dest_path"
        echo "  • Installed $dest_path"
    done < <(find "$REPO_ROOT/packaging/etc/linuxio" -type f | sort)
else
    echo -e "${YELLOW}    Warning: packaging/etc/linuxio directory not found${NC}"
fi

if [[ -d /etc/linuxio/docker/linuxio-monitoring ]]; then
    chown root:root /etc/linuxio/docker/linuxio-monitoring
    chmod 0755 /etc/linuxio/docker/linuxio-monitoring

    for file in /etc/linuxio/docker/linuxio-monitoring/docker-compose.yml /etc/linuxio/docker/linuxio-monitoring/prometheus.yml; do
        if [[ -f "$file" ]]; then
            chown root:root "$file"
            chmod 0644 "$file"
            echo "  • Enforced root:root 0644 on $file"
        fi
    done

    echo "  • Enforced root:root 0755 on /etc/linuxio/docker/linuxio-monitoring"
fi

echo -e "${GREEN}✓ Configuration files installed${NC}"

# Install PAM configuration
echo -e "${YELLOW} Installing PAM configuration...${NC}"
if [[ -f "$REPO_ROOT/packaging/etc/pam.d/linuxio" ]]; then
    install -m 0644 "$REPO_ROOT/packaging/etc/pam.d/linuxio" /etc/pam.d/
    echo "  • Installed /etc/pam.d/linuxio"
    echo -e "${GREEN}✓ PAM configuration installed${NC}"
else
    echo -e "${RED}✗ PAM configuration not found at packaging/etc/pam.d/linuxio${NC}"
    exit 1
fi

# Install issue updater script
echo -e "${YELLOW} Installing issue updater...${NC}"
mkdir -p /usr/share/linuxio/issue
if [[ -f "$REPO_ROOT/packaging/scripts/update-issue" ]]; then
    install -m 0755 "$REPO_ROOT/packaging/scripts/update-issue" /usr/share/linuxio/issue/
    echo "  • Installed /usr/share/linuxio/issue/update-issue"
    echo -e "${GREEN}✓ Issue updater installed${NC}"
else
    echo -e "${YELLOW}  Warning: update-issue script not found${NC}"
fi

# Create symlink for SSH login banner
if [[ -d /etc/motd.d ]]; then
    ln -sf ../../run/linuxio/issue /etc/motd.d/linuxio 2>/dev/null || true
    echo "  • Created SSH login banner symlink"
fi

# Create global Watchtower data directory
echo -e "${YELLOW} Creating Watchtower data directory...${NC}"
mkdir -p /var/lib/linuxIO/watchtower
if getent group docker &>/dev/null; then
    chown root:docker /var/lib/linuxIO/watchtower
    chmod 775 /var/lib/linuxIO/watchtower
    echo -e "${GREEN}✓ Watchtower directory created (/var/lib/linuxIO/watchtower, group: docker)${NC}"
else
    chmod 755 /var/lib/linuxIO/watchtower
    echo -e "${YELLOW}  docker group not found — Watchtower directory created with mode 755${NC}"
fi

# ========== ENABLE AND RESTART ==========
echo ""
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo -e "${CYAN}  Enabling and restarting services${NC}"
echo -e "${CYAN}════════════════════════════════════════════${NC}"
echo ""

echo -e "${YELLOW} Reloading systemd...${NC}"
systemctl daemon-reload
echo -e "${GREEN}✓ Systemd reloaded${NC}"

echo -e "${YELLOW} Enabling services...${NC}"
systemctl enable linuxio.target
systemctl enable linuxio-monitoring.service
echo -e "${GREEN}✓ Services enabled${NC}"

echo -e "${YELLOW} Restarting LinuxIO...${NC}"
linuxio restart

# Wait a moment for services to settle
sleep 2

# Check if target is active
if systemctl is-active --quiet linuxio.target; then
    echo -e "${GREEN}✓ LinuxIO restarted successfully${NC}"
else
    echo -e "${YELLOW}  Warning: LinuxIO may not have restarted properly${NC}"
fi

# ========== SUMMARY ==========
echo ""
echo -e "${GREEN}════════════════════════════════════════════${NC}"
echo -e "${GREEN} Installation Complete!${NC}"
echo -e "${GREEN}════════════════════════════════════════════${NC}"
echo ""
echo "Installed components:"
echo "  • Binaries:        /usr/local/bin/{linuxio,linuxio-webserver,linuxio-bridge,linuxio-auth}"
echo "  • Systemd files:   /etc/systemd/system/linuxio*"
echo "  • Configuration:   /etc/linuxio/"
echo "  • Monitoring:      /etc/linuxio/docker/linuxio-monitoring/"
echo "  • PAM config:      /etc/pam.d/linuxio"
echo "  • Issue updater:   /usr/share/linuxio/issue/"
echo ""
echo -e "${CYAN} Access LinuxIO at: https://localhost:${PORT}${NC}"
echo ""
