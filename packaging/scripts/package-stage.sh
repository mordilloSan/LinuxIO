#!/usr/bin/env bash
# =============================================================================
# LinuxIO Package Staging Helper
# Populates a filesystem root for .deb/.rpm packaging
# =============================================================================
set -euo pipefail

DEST_ROOT="${1:-}"
if [[ -z "$DEST_ROOT" ]]; then
  echo "Usage: $(basename "$0") <dest-root>" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

bins=(linuxio linuxio-webserver linuxio-bridge linuxio-auth)
for bin in "${bins[@]}"; do
  if [[ ! -f "${REPO_ROOT}/${bin}" ]]; then
    echo "Missing binary: ${bin}. Run 'make build' first." >&2
    exit 1
  fi
done

# Binaries
install -d "${DEST_ROOT}/usr/local/bin"
for bin in "${bins[@]}"; do
  install -m 0755 "${REPO_ROOT}/${bin}" "${DEST_ROOT}/usr/local/bin/${bin}"
done

# Systemd units
install -d "${DEST_ROOT}/usr/lib/systemd/system"
systemd_files=(
  linuxio.target
  linuxio-webserver.socket
  linuxio-webserver.service
  linuxio-auth.socket
  linuxio-auth@.service
  linuxio-bridge-socket-user.service
  linuxio-issue.service
)
for unit in "${systemd_files[@]}"; do
  src="${REPO_ROOT}/packaging/systemd/${unit}"
  if [[ ! -f "$src" ]]; then
    echo "Missing systemd unit: ${src}" >&2
    exit 1
  fi
  install -m 0644 "$src" "${DEST_ROOT}/usr/lib/systemd/system/${unit}"
done

# tmpfiles.d configuration
install -d "${DEST_ROOT}/usr/lib/tmpfiles.d"
tmpfiles_src="${REPO_ROOT}/packaging/systemd/linuxio-tmpfiles.conf"
if [[ -f "$tmpfiles_src" ]]; then
  install -m 0644 "$tmpfiles_src" "${DEST_ROOT}/usr/lib/tmpfiles.d/linuxio.conf"
else
  echo "Missing tmpfiles config: ${tmpfiles_src}" >&2
  exit 1
fi

# Config files
install -d "${DEST_ROOT}/etc/linuxio"
if [[ -f "${REPO_ROOT}/packaging/etc/linuxio/disallowed-users" ]]; then
  install -m 0644 "${REPO_ROOT}/packaging/etc/linuxio/disallowed-users" \
    "${DEST_ROOT}/etc/linuxio/disallowed-users"
else
  echo "Missing config file: packaging/etc/linuxio/disallowed-users" >&2
  exit 1
fi

# Copy any additional config files in packaging/etc/linuxio/
if [[ -d "${REPO_ROOT}/packaging/etc/linuxio" ]]; then
  for file in "${REPO_ROOT}/packaging/etc/linuxio/"*; do
    if [[ -f "$file" && "$(basename "$file")" != "disallowed-users" ]]; then
      install -m 0644 "$file" "${DEST_ROOT}/etc/linuxio/$(basename "$file")"
    fi
  done
fi

# PAM configuration
install -d "${DEST_ROOT}/etc/pam.d"
pam_src="${REPO_ROOT}/packaging/etc/pam.d/linuxio"
if [[ -f "$pam_src" ]]; then
  install -m 0644 "$pam_src" "${DEST_ROOT}/etc/pam.d/linuxio"
else
  echo "Missing PAM config: packaging/etc/pam.d/linuxio" >&2
  exit 1
fi

# Issue updater
install -d "${DEST_ROOT}/usr/share/linuxio/issue"
issue_src="${REPO_ROOT}/packaging/scripts/update-issue"
if [[ -f "$issue_src" ]]; then
  install -m 0755 "$issue_src" "${DEST_ROOT}/usr/share/linuxio/issue/update-issue"
else
  echo "Missing issue updater: packaging/scripts/update-issue" >&2
  exit 1
fi

# License
install -d "${DEST_ROOT}/usr/share/doc/linuxio"
install -m 0644 "${REPO_ROOT}/LICENSE" "${DEST_ROOT}/usr/share/doc/linuxio/LICENSE"
