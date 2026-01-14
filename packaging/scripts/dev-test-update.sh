#!/bin/bash
# =============================================================================
# LinuxIO Dev Mode Test Update Script
# Simulates update by copying current binaries onto themselves and restarting
# © 2025 Miguel Mariz (mordilloSan)
# =============================================================================

set -euo pipefail

UPDATE_STATUS_FILE="/run/linuxio/update-status.json"
RUN_ID="${1:-dev-test-$(date +%s)}"

# Create run directory
mkdir -p /run/linuxio

# Write initial status
started=$(date +%s)
printf '{"id":"%s","status":"running","started_at":%s}\n' "$RUN_ID" "$started" > "$UPDATE_STATUS_FILE"

# Output similar to production installer
echo "▸ Starting LinuxIO test update (dev mode)"
echo "▸ Target version: dev-v0.6.12"
sleep 0.5

echo "▸ === Step 1/5: Downloading binaries ==="
sleep 0.3
echo "▸ Downloading linuxio..."
sleep 0.2
echo "▸ Downloading linuxio-webserver..."
sleep 0.2
echo "▸ Downloading linuxio-bridge..."
sleep 0.2
echo "▸ Downloading linuxio-auth..."
sleep 0.2
echo "▸ Downloading SHA256SUMS..."
sleep 0.2
echo "✓ All binaries downloaded"

echo "▸ === Step 2/5: Verifying checksums ==="
sleep 0.3
echo "▸ Verifying checksums..."
echo "▸ Verifying linuxio..."
echo "✓ Verified linuxio"
echo "▸ Verifying linuxio-webserver..."
echo "✓ Verified linuxio-webserver"
echo "▸ Verifying linuxio-bridge..."
echo "✓ Verified linuxio-bridge"
echo "▸ Verifying linuxio-auth..."
echo "✓ Verified linuxio-auth"
echo "✓ All checksums verified"

echo "▸ === Step 3/5: Installing binaries ==="
sleep 0.3
echo "▸ Installing binaries to /usr/local/bin..."

# Copy each binary onto itself (simulates update)
for binary in linuxio linuxio-webserver linuxio-bridge linuxio-auth; do
    echo "▸ Installing $binary with mode 0755..."
    if [[ -f "/usr/local/bin/$binary" ]]; then
        # Create temp copy and move it back (atomic operation)
        cp -f "/usr/local/bin/$binary" "/tmp/${binary}.tmp"
        install -o root -g root -m 0755 "/tmp/${binary}.tmp" "/usr/local/bin/$binary"
        rm -f "/tmp/${binary}.tmp"
        echo "✓ Installed $binary (mode: 0755)"
    else
        echo "⚠ Binary $binary not found, skipping"
    fi
    sleep 0.1
done

echo "▸ Verifying installations..."
echo "✓ All binaries installed successfully"

echo "▸ === Step 4/5: Installing configuration ==="
sleep 0.3
echo "▸ Installing configuration files..."
if [[ -f "/etc/linuxio/disallowed-users" ]]; then
    echo "✓ /etc/linuxio/disallowed-users already exists (not overwriting)"
else
    echo "⚠ Configuration directory exists"
fi
echo "▸ Installing PAM configuration..."
if [[ -f "/etc/pam.d/linuxio" ]]; then
    echo "✓ PAM configuration already exists (not overwriting)"
fi

echo "▸ === Step 5/5: Installing systemd services ==="
sleep 0.3
echo "▸ Installing systemd service files..."
for service in linuxio.target linuxio-webserver.socket linuxio-webserver.service \
               linuxio-auth.socket linuxio-auth@.service linuxio-bridge-socket-user.service \
               linuxio-issue.service; do
    if [[ -f "/etc/systemd/system/$service" ]]; then
        echo "✓ $service already installed"
    fi
    sleep 0.1
done

echo "▸ Installing SSH login banner support..."
echo "✓ SSH login banner configured"

echo "▸ Reloading systemd daemon..."
systemctl daemon-reload
echo "✓ Systemd daemon reloaded"

echo "▸ Enabling systemd services..."
systemctl enable linuxio.target 2>/dev/null || true
echo "✓ Enabled linuxio.target"

echo "▸ === Verification ==="
sleep 0.2
echo "▸ Running post-installation checks..."
echo "✓ linuxio CLI: working"
echo "✓ LinuxIO Web Server"
echo "✓ linuxio-bridge: executable"
echo "✓ linuxio-auth: executable"
echo "✓ linuxio.target is enabled"
echo "✓ PAM configuration installed"
echo "✓ Configuration directory exists at /etc/linuxio"

# Write success status
finished=$(date +%s)
printf '{"id":"%s","status":"ok","exit_code":0,"started_at":%s,"finished_at":%s}\n' \
    "$RUN_ID" "$started" "$finished" > "$UPDATE_STATUS_FILE"

echo ""
echo "✅ Installation complete!"
echo ""
echo "Note: Services NOT restarted in dev mode (server stays up for testing)"
echo "      In production, services restart automatically."

exit 0
