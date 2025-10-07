#!/usr/bin/env bash
set -euo pipefail

# Locate repo root (two dirs up from this script)
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
SRC_DIR="$(cd -- "$SCRIPT_DIR/../.." &>/dev/null && pwd)"

# System-wide dev enclave (accessible to all users)
SECURE_DEV_DIR="/tmp/linuxio/dev"
SUDOERS_FILE="/etc/sudoers.d/linuxio-dev"

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

echo "==> Creating system-wide dev enclave: $SECURE_DEV_DIR"
$SUDO_CMD mkdir -p "$SECURE_DEV_DIR"
$SUDO_CMD chown root:root "$SECURE_DEV_DIR"
$SUDO_CMD chmod 0755 "$SECURE_DEV_DIR"

# Install bridge (root:root 0755) and helper (root:root 4755) to /tmp/linuxio/dev
echo "==> Installing dev bridge + helper to system location"
$SUDO_CMD install -o root -g root -m 0755 "$SRC_DIR/linuxio-bridge"      "$SECURE_DEV_DIR/linuxio-bridge"
$SUDO_CMD install -o root -g root -m 4755 "$SRC_DIR/linuxio-auth-helper" "$SECURE_DEV_DIR/linuxio-auth-helper"

echo "==> System enclave contents:"
ls -l "$SECURE_DEV_DIR/linuxio-bridge" "$SECURE_DEV_DIR/linuxio-auth-helper"

# Cleanup build artifacts in repo root (keep /tmp/linuxio/dev copies)
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

# --- Configure passwordless sudo for future runs ---
SCRIPT_FULLPATH="$(readlink -f "${BASH_SOURCE[0]}")"

if [[ ! -f "$SUDOERS_FILE" ]]; then
  echo ""
  echo "==> Configuring passwordless sudo for dev workflow"
  echo "    This allows 'make dev' to run without password prompts"
  
  # Create temporary sudoers file
  TMP_SUDOERS="$(mktemp)"
  cat > "$TMP_SUDOERS" <<EOF
# linuxio development - allow passwordless reinstall of dev binaries
# Created: $(date -Iseconds)
# Script: $SCRIPT_FULLPATH
$BUILD_USER ALL=(ALL) NOPASSWD: $SCRIPT_FULLPATH
EOF

  # Validate syntax before installing
  if $SUDO_CMD visudo -c -f "$TMP_SUDOERS" >/dev/null 2>&1; then
    $SUDO_CMD install -o root -g root -m 0440 "$TMP_SUDOERS" "$SUDOERS_FILE"
    echo "✅ Passwordless sudo configured: $SUDOERS_FILE"
    echo "   Future 'make dev' runs won't require a password!"
  else
    echo "⚠️  Failed to validate sudoers syntax, skipping passwordless setup"
    echo "   You'll need to enter your password on each 'make devinstall'"
  fi
  
  rm -f "$TMP_SUDOERS"
else
  # Check if the sudoers file needs updating (script path changed)
  if ! grep -q "$SCRIPT_FULLPATH" "$SUDOERS_FILE" 2>/dev/null; then
    echo ""
    echo "==> Updating sudoers configuration (script path changed)"
    
    TMP_SUDOERS="$(mktemp)"
    cat > "$TMP_SUDOERS" <<EOF
# linuxio development - allow passwordless reinstall of dev binaries
# Updated: $(date -Iseconds)
# Script: $SCRIPT_FULLPATH
$BUILD_USER ALL=(ALL) NOPASSWD: $SCRIPT_FULLPATH
EOF

    if $SUDO_CMD visudo -c -f "$TMP_SUDOERS" >/dev/null 2>&1; then
      $SUDO_CMD install -o root -g root -m 0440 "$TMP_SUDOERS" "$SUDOERS_FILE"
      echo "✅ Sudoers configuration updated"
    fi
    
    rm -f "$TMP_SUDOERS"
  else
    echo "✅ Passwordless sudo already configured: $SUDOERS_FILE"
  fi
fi

echo ""
echo "✅ Done. Dev binaries installed to: $SECURE_DEV_DIR"
echo "   The Makefile 'dev' target will automatically use these binaries."