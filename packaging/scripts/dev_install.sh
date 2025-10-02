#!/usr/bin/env bash
set -euo pipefail

# Locate repo root (two dirs up from this script)
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
SRC_DIR="$(cd -- "$SCRIPT_DIR/../.." &>/dev/null && pwd)"

# System-wide dev enclave (accessible to all users)
SECURE_DEV_DIR="/tmp/linuxio/dev"
DEV_ENV_FILE="$SRC_DIR/dev.env"

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

echo "==> Repo root: $SRC_DIR"
echo "==> Building as $BUILD_USER"

# Build artifacts as non-root with a sensible Go PATH
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
  make -C "$SRC_DIR" build-bridge build-auth-helper

# Ensure artifacts exist in repo root
for f in linuxio-bridge linuxio-auth-helper; do
  [[ -f "$SRC_DIR/$f" ]] || { echo "Missing after build: $SRC_DIR/$f"; exit 1; }
done

# We need root for system-wide install
SUDO_CMD=""
if [[ "${EUID}" -ne 0 ]]; then
  if ! command -v sudo >/dev/null 2>&1; then
    echo "This step needs root. Install sudo or run this script with sudo."; exit 1
  fi
  SUDO_CMD="sudo"
fi

# Warn if repo filesystem is mounted nosuid (SUID would be ignored if we were still using repo path)
if command -v findmnt >/dev/null 2>&1; then
  mp=$(findmnt -no TARGET "$SRC_DIR" || true)
  opts=$(findmnt -no OPTIONS "$SRC_DIR" || true)
  if [[ "$opts" == *nosuid* ]]; then
    echo "ℹ️  Note: Repo filesystem has 'nosuid', but we're installing to /opt (which should not have nosuid)."
  fi
fi

echo "==> Creating system-wide dev enclave: $SECURE_DEV_DIR"
$SUDO_CMD mkdir -p "$SECURE_DEV_DIR"
$SUDO_CMD chown root:root "$SECURE_DEV_DIR"
$SUDO_CMD chmod 0755 "$SECURE_DEV_DIR"

# Install bridge (root:root 0755) and helper (root:root 4755) to /opt
echo "==> Installing dev bridge + helper to system location"
$SUDO_CMD install -o root -g root -m 0755 "$SRC_DIR/linuxio-bridge"      "$SECURE_DEV_DIR/linuxio-bridge"
$SUDO_CMD install -o root -g root -m 4755 "$SRC_DIR/linuxio-auth-helper" "$SECURE_DEV_DIR/linuxio-auth-helper"

echo "==> System enclave contents:"
ls -l "$SECURE_DEV_DIR/linuxio-bridge" "$SECURE_DEV_DIR/linuxio-auth-helper"

# Write dev env file to point your dev process to the system enclave
mkdir -p "$(dirname "$DEV_ENV_FILE")"
cat > "$DEV_ENV_FILE" <<EOF
# Auto-generated on $(date -Iseconds)
# Source this in your dev shell (or have 'make dev' source it) to use dev binaries from /opt.

# Ensure we run in development mode
export LINUXIO_ENV="development"

# Point helper + bridge at system-wide dev enclave
export LINUXIO_PAM_HELPER="$SECURE_DEV_DIR/linuxio-auth-helper"
export LINUXIO_BRIDGE_BIN="$SECURE_DEV_DIR/linuxio-bridge"

# (Optional) turn on verbose logs
# export LINUXIO_VERBOSE=1
EOF
chmod 0644 "$DEV_ENV_FILE"

echo "==> Wrote dev env: $DEV_ENV_FILE"
echo "   Use it like:"
echo "     source dev.env"
echo "   or in your Makefile dev recipe before starting the backend."

# --- Cleanup build artifacts in repo root (keep /opt copies) ---
if [[ "${KEEP_ARTIFACTS:-0}" != "1" ]]; then
  echo "==> Removing repo-root build artifacts"
  for f in linuxio-bridge linuxio-auth-helper; do
    p="$SRC_DIR/$f"
    if [[ -f "$p" ]]; then
      rm -f -- "$p"
      echo "   removed $p"
    fi
  done
else
  echo "==> KEEP_ARTIFACTS=1 set; leaving repo-root binaries in place"
fi

echo "✅ Done. Dev binaries installed to: $SECURE_DEV_DIR"
echo "   All system users can now access these binaries."