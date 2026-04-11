#!/bin/bash
# =============================================================================
# LinuxIO Local Build and Install Script
# Builds and installs LinuxIO from local source code
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

# ---------- Configuration ----------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PORT=8090

# ---------- Main ----------

Header "LinuxIO ${GREY}· Local Install${COLOUR_RESET}"

if [[ $EUID -ne 0 ]]; then
    Show 1 "This script must be run as root"
fi

if [[ ! -f "$REPO_ROOT/makefile" && ! -f "$REPO_ROOT/Makefile" ]]; then
    Show 1 "Cannot find LinuxIO repository root at ${REPO_ROOT}"
fi

cd "$REPO_ROOT"

# ========== VERIFY BUILD ==========
Header "Step 1/2 — Verify Binaries"

MISSING=0
for binary in linuxio linuxio-webserver linuxio-bridge linuxio-auth; do
    if [[ ! -f "$REPO_ROOT/$binary" ]]; then
        Show 1 "Binary not found: ${binary}. Run 'make build' first."
        MISSING=1
    else
        Show 0 "${binary}"
    fi
done

# ========== INSTALL ==========
Header "Step 2/2 — Install"

# Binaries
Show 2 "Installing binaries..."
for binary in linuxio linuxio-webserver linuxio-bridge linuxio-auth; do
    install -o root -g root -m 0755 "$REPO_ROOT/$binary" /usr/local/bin/
done
Show 0 "Binaries installed to /usr/local/bin"

# Systemd
Show 2 "Installing systemd service files..."
for file in linuxio.target linuxio-webserver.service linuxio-webserver.socket \
            linuxio-auth.socket linuxio-auth@.service \
            linuxio-bridge-socket-user.service \
            linuxio-issue.service; do
    if [[ -f "$REPO_ROOT/packaging/systemd/$file" ]]; then
        install -m 0644 "$REPO_ROOT/packaging/systemd/$file" /etc/systemd/system/
    else
        Show 3 "${file} not found in packaging/systemd/"
    fi
done
Show 0 "Systemd files installed"

# Tmpfiles
Show 2 "Installing tmpfiles.d configuration..."
mkdir -p /usr/lib/tmpfiles.d
if [[ -f "$REPO_ROOT/packaging/systemd/linuxio-tmpfiles.conf" ]]; then
    install -m 0644 "$REPO_ROOT/packaging/systemd/linuxio-tmpfiles.conf" /usr/lib/tmpfiles.d/linuxio.conf
    systemd-tmpfiles --create /usr/lib/tmpfiles.d/linuxio.conf 2>/dev/null || true
    Show 0 "tmpfiles.d configuration installed"
else
    Show 3 "linuxio-tmpfiles.conf not found"
fi

# Config files
Show 2 "Installing configuration files..."
if [[ -d "$REPO_ROOT/packaging/etc/linuxio" ]]; then
    while IFS= read -r file; do
        rel_path="${file#$REPO_ROOT/packaging/etc/linuxio/}"
        install -D -o root -g root -m 0644 "$file" "/etc/linuxio/$rel_path"
    done < <(find "$REPO_ROOT/packaging/etc/linuxio" -type f | sort)
    Show 0 "Configuration files installed"
else
    Show 3 "packaging/etc/linuxio directory not found"
fi

# PCP derived metrics and pmproxy override
Show 2 "Installing PCP derived metrics..."
if [[ -f "$REPO_ROOT/packaging/etc/linuxio/pcp-derived.conf" ]]; then
    install -D -o root -g root -m 0644 "$REPO_ROOT/packaging/etc/linuxio/pcp-derived.conf" /etc/linuxio/pcp-derived.conf
    Show 0 "PCP derived metrics installed"
else
    Show 3 "pcp-derived.conf not found in packaging/etc/linuxio/"
fi

if [[ -f "$REPO_ROOT/packaging/etc/linuxio/pmlogger-linuxio.config" ]]; then
    mkdir -p /etc/pcp/pmlogger/config.d
    install -m 0644 "$REPO_ROOT/packaging/etc/linuxio/pmlogger-linuxio.config" \
        /etc/pcp/pmlogger/config.d/linuxio.config
    Show 0 "pmlogger config installed (15s intervals)"
    if systemctl is-active --quiet pmlogger 2>/dev/null; then
        systemctl restart pmlogger
        Show 0 "pmlogger restarted with new config"
    fi
else
    Show 3 "pmlogger-linuxio.config not found in packaging/etc/linuxio/"
fi

if [[ -f "$REPO_ROOT/packaging/systemd/linuxio-pmproxy-override.conf" ]]; then
    mkdir -p /etc/systemd/system/pmproxy.service.d
    install -m 0644 "$REPO_ROOT/packaging/systemd/linuxio-pmproxy-override.conf" \
        /etc/systemd/system/pmproxy.service.d/linuxio.conf
    Show 0 "pmproxy override installed"
    if systemctl is-active --quiet pmproxy 2>/dev/null; then
        systemctl daemon-reload
        systemctl restart pmproxy
        Show 0 "pmproxy restarted with derived metrics"
    fi
else
    Show 3 "linuxio-pmproxy-override.conf not found in packaging/systemd/"
fi

# PAM
Show 2 "Installing PAM configuration..."
if [[ -f "$REPO_ROOT/packaging/etc/pam.d/linuxio" ]]; then
    install -m 0644 "$REPO_ROOT/packaging/etc/pam.d/linuxio" /etc/pam.d/
    Show 0 "PAM configuration installed"
else
    Show 1 "PAM configuration not found at packaging/etc/pam.d/linuxio"
fi

# Issue updater
Show 2 "Installing issue updater..."
mkdir -p /usr/share/linuxio/issue
if [[ -f "$REPO_ROOT/packaging/scripts/update-issue" ]]; then
    install -m 0755 "$REPO_ROOT/packaging/scripts/update-issue" /usr/share/linuxio/issue/
    Show 0 "Issue updater installed"
else
    Show 3 "update-issue script not found"
fi

if [[ -d /etc/motd.d ]]; then
    ln -sf ../../run/linuxio/issue /etc/motd.d/linuxio 2>/dev/null || true
    Show 0 "SSH login banner configured"
fi

# Watchtower
Show 2 "Creating Watchtower data directory..."
mkdir -p /var/lib/linuxIO/watchtower
if getent group docker &>/dev/null; then
    chown root:docker /var/lib/linuxIO/watchtower
    chmod 775 /var/lib/linuxIO/watchtower
    Show 0 "Watchtower directory created ${GREY}(group: docker)${COLOUR_RESET}"
else
    chmod 755 /var/lib/linuxIO/watchtower
    Show 3 "docker group not found — Watchtower directory created with mode 755"
fi

# ========== ENABLE AND RESTART ==========
Show 2 "Reloading systemd..."
systemctl daemon-reload
Show 0 "Systemd reloaded"

Show 2 "Enabling services..."
systemctl enable linuxio.target >/dev/null 2>&1
Show 0 "Services enabled"

Show 2 "Restarting LinuxIO..."
linuxio restart

sleep 2

if systemctl is-active --quiet linuxio.target; then
    Show 0 "LinuxIO restarted successfully"
else
    Show 3 "LinuxIO may not have restarted properly"
fi

# ========== SUMMARY ==========
lan_ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}') || true

echo ""
echo -e "${LINE}"
echo -e " ${GREEN}${BOLD}Installation complete!${COLOUR_RESET}"
echo -e "${LINE}"
echo "Installed components:"
echo "  • Binaries:        /usr/local/bin/{linuxio,linuxio-webserver,linuxio-bridge,linuxio-auth}"
echo "  • Systemd files:   /etc/systemd/system/linuxio*"
echo "  • Configuration:   /etc/linuxio/"
echo "  • PCP derived:     /etc/linuxio/pcp-derived.conf"
echo "  • PCP logging:    /etc/pcp/pmlogger/config.d/linuxio.config"
echo "  • pmproxy drop-in: /etc/systemd/system/pmproxy.service.d/linuxio.conf"
echo "  • PAM config:      /etc/pam.d/linuxio"
echo "  • Issue updater:   /usr/share/linuxio/issue/"
echo ""
echo -e " ${BOLD}Dashboard:${COLOUR_RESET}"
echo -e "${BULLET} https://localhost:${PORT}"
if [[ -n "$lan_ip" ]]; then
    echo -e "${BULLET} https://${lan_ip}:${PORT}"
fi
echo ""