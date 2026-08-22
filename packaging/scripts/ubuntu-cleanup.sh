#!/bin/bash
# =============================================================================
# Ubuntu cleanup - remove MOTD noise, motd-news, and snap entirely
# Standalone host utility; not part of the LinuxIO install or uninstall flow.
# Idempotent: safe to re-run.
# Usage: sudo bash ubuntu-cleanup.sh
# =============================================================================
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    echo "This script must be run as root (sudo bash $0)" >&2
    exit 1
fi

say() { echo -e "\e[1;32m==>\e[0m $*"; }

# ========== 1. MOTD cleanup ==========
say "Removing Ubuntu MOTD noise scripts..."
rm -f /etc/update-motd.d/10-help-text \
      /etc/update-motd.d/50-motd-news \
      /etc/update-motd.d/50-landscape-sysinfo.wants 2>/dev/null || true
# dpkg does not restore deleted conffiles on upgrade, so removal is permanent.

say "Disabling and masking motd-news..."
if [[ -f /etc/default/motd-news ]]; then
    sed -i 's/^ENABLED=1/ENABLED=0/' /etc/default/motd-news
fi
systemctl disable --now motd-news.timer 2>/dev/null || true
# Mask so a future base-files upgrade cannot re-enable it.
systemctl mask motd-news.timer motd-news.service 2>/dev/null || true

# ========== 2. Snap removal ==========
if command -v snap >/dev/null 2>&1; then
    say "Removing installed snaps..."
    # Remove leaf snaps first; bases/snapd refuse removal while dependents
    # exist, so loop until nothing is removable, then take snapd itself.
    for _ in 1 2 3 4 5; do
        mapfile -t snaps < <(snap list 2>/dev/null | awk 'NR>1 {print $1}' | grep -vx snapd || true)
        [[ ${#snaps[@]} -eq 0 ]] && break
        for s in "${snaps[@]}"; do
            snap remove --purge "$s" 2>/dev/null || true
        done
    done
    snap remove --purge snapd 2>/dev/null || true
else
    say "snap command not present, skipping snap removal step"
fi

say "Stopping snapd services..."
systemctl disable --now snapd.socket snapd.service snapd.seeded.service 2>/dev/null || true

say "Purging snapd package..."
export DEBIAN_FRONTEND=noninteractive
apt-get purge -y snapd gnome-software-plugin-snap 2>/dev/null || apt-get purge -y snapd || true
apt-get autoremove -y --purge

say "Removing leftover snap directories..."
rm -rf /snap /var/snap /var/lib/snapd /var/cache/snapd
for home in /home/* /root; do
    rm -rf "$home/snap" 2>/dev/null || true
done

say "Blocking snapd from ever being reinstalled as a dependency..."
cat > /etc/apt/preferences.d/nosnap.pref <<'EOF'
# Prevent snapd from being installed (removed by ubuntu-cleanup.sh).
# Delete this file to allow snapd again.
Package: snapd
Pin: release *
Pin-Priority: -10
EOF

say "Done. MOTD noise removed, motd-news masked, snap fully purged."
echo "    (To ever restore snap: rm /etc/apt/preferences.d/nosnap.pref && apt install snapd)"
