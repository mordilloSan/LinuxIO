#!/usr/bin/env bash
set -euo pipefail

# Locate repo root (two dirs up from this script)
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
SRC_DIR="$(cd -- "$SCRIPT_DIR/../.." &>/dev/null && pwd)"
DEST_DIR="/usr/local/bin"

# Figure out the real (non-root) user to build as
if [[ -n "${SUDO_USER:-}" && "${EUID}" -eq 0 ]]; then
  BUILD_USER="$SUDO_USER"
else
  BUILD_USER="$(id -un)"
fi

# Resolve that user's HOME to extend PATH for Go installed at ~/.go
BUILD_HOME="$(getent passwd "$BUILD_USER" | cut -d: -f6 || true)"
if [[ -z "${BUILD_HOME:-}" ]]; then
  echo "Could not resolve home for user '$BUILD_USER'"; exit 1
fi

echo "==> Building as $BUILD_USER in $SRC_DIR"
sudo -u "$BUILD_USER" env -i \
  HOME="$BUILD_HOME" \
  USER="$BUILD_USER" \
  LOGNAME="$BUILD_USER" \
  SHELL="/bin/bash" \
  PATH="$BUILD_HOME/.go/bin:/usr/local/go/bin:/usr/bin:/bin" \
  XDG_CACHE_HOME="$BUILD_HOME/.cache" \
  GOCACHE="$BUILD_HOME/.cache/go-build" \
  GOPATH="$BUILD_HOME/go" \
  GOFLAGS="-buildvcs=false" \
  make -C "$SRC_DIR" build-backend build-bridge build-auth-helper

# Ensure artifacts exist
for f in linuxio linuxio-bridge linuxio-auth-helper; do
  [[ -f "$SRC_DIR/$f" ]] || { echo "Missing after build: $SRC_DIR/$f"; exit 1; }
done

# Use sudo for install actions if not root
SUDO_CMD=""
if [[ "${EUID}" -ne 0 ]]; then
  if ! command -v sudo >/dev/null 2>&1; then
    echo "This step needs root. Please install sudo or run the script with sudo."; exit 1
  fi
  SUDO_CMD="sudo"
fi

echo "==> Installing to $DEST_DIR (requires root)"
$SUDO_CMD rm -f \
  "$DEST_DIR/linuxio" \
  "$DEST_DIR/linuxio-bridge" \
  "$DEST_DIR/linuxio-auth-helper"

$SUDO_CMD install -m 0755 "$SRC_DIR/linuxio"        "$DEST_DIR/linuxio"
$SUDO_CMD install -m 0755 "$SRC_DIR/linuxio-bridge" "$DEST_DIR/linuxio-bridge"
$SUDO_CMD install -o root -g root -m 4755 "$SRC_DIR/linuxio-auth-helper" "$DEST_DIR/linuxio-auth-helper"

# Refresh shell command cache for the current shell (best effort)
hash -r 2>/dev/null || true

echo "==> Done. Installed:"
ls -l "$DEST_DIR/linuxio" "$DEST_DIR/linuxio-bridge" "$DEST_DIR/linuxio-auth-helper"

# Helper to remove built binaries from the repo (not source code)
remove_repo_artifacts() {
  echo "==> Removing build artifacts from repo:"
  for f in linuxio linuxio-bridge linuxio-auth-helper; do
    if [[ -f "$SRC_DIR/$f" ]]; then
      rm -f -- "$SRC_DIR/$f"
      echo "   removed $SRC_DIR/$f"
    fi
  done
}

# Restart service if present (robust check)
if $SUDO_CMD systemctl show -p LoadState --value linuxio.service 2>/dev/null | grep -qx loaded; then
  echo "==> Restarting linuxio.service"
  if $SUDO_CMD systemctl restart linuxio.service; then
    echo "✅ linuxio.service restarted successfully"
    echo "==> Current status:"
    $SUDO_CMD systemctl --no-pager --full --lines=20 status linuxio.service || true
    # Only clean artifacts after a successful restart
    remove_repo_artifacts
  else
    rc="$?"
    echo "❌ linuxio.service restart failed (exit $rc)"
    echo "==> Status after failure:"
    $SUDO_CMD systemctl --no-pager --full --lines=50 status linuxio.service || true
    echo "==> Recent logs:"
    $SUDO_CMD journalctl -u linuxio.service -n 100 --no-pager || true
    exit "$rc"
  fi
else
  echo "Note: linuxio.service not loaded; skipping restart."
  # No service to restart; install succeeded, so clean now.
  remove_repo_artifacts
fi
