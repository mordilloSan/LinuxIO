#!/usr/bin/env bash
# =============================================================================
# LinuxIO Debian Package Builder
# Builds a .deb from prebuilt binaries and packaging assets
# =============================================================================
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

RAW_VERSION="${GIT_VERSION:-}"
if [[ -z "$RAW_VERSION" ]]; then
  if git -C "$REPO_ROOT" describe --tags --exact-match >/dev/null 2>&1; then
    RAW_VERSION="$(git -C "$REPO_ROOT" describe --tags --exact-match)"
  else
    RAW_VERSION="dev-$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
  fi
fi

COMMIT_SHORT="${GIT_COMMIT_SHORT:-}"
if [[ -z "$COMMIT_SHORT" ]]; then
  COMMIT_SHORT="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
fi

base_version="${RAW_VERSION#dev-}"
base_version="${base_version#v}"
if [[ -z "$base_version" ]]; then
  echo "Unable to determine package version from GIT_VERSION=${RAW_VERSION}" >&2
  exit 1
fi

if [[ ! "$base_version" =~ ^[0-9] ]]; then
  base_version="0.0.0"
fi

if [[ "$RAW_VERSION" == dev-* ]]; then
  DEB_VERSION="${base_version}~dev"
else
  DEB_VERSION="${base_version}"
fi

DEB_ARCH="amd64"

OUT_DIR="${PKG_OUTPUT_DIR:-$REPO_ROOT/dist}"
WORK_DIR="$(mktemp -d)"
DEB_ROOT="${WORK_DIR}/linuxio_${DEB_VERSION}_${DEB_ARCH}"

cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

mkdir -p "$DEB_ROOT"

"$SCRIPT_DIR/package-stage.sh" "${DEB_ROOT}"

mkdir -p "${DEB_ROOT}/DEBIAN"

control_template="${REPO_ROOT}/packaging/debian/control.in"
if [[ ! -f "$control_template" ]]; then
  echo "Missing control template: ${control_template}" >&2
  exit 1
fi

sed \
  -e "s/@VERSION@/${DEB_VERSION}/g" \
  -e "s/@ARCH@/${DEB_ARCH}/g" \
  "$control_template" > "${DEB_ROOT}/DEBIAN/control"

if [[ -f "${REPO_ROOT}/packaging/debian/conffiles" ]]; then
  cp "${REPO_ROOT}/packaging/debian/conffiles" "${DEB_ROOT}/DEBIAN/conffiles"
fi

for script in postinst prerm postrm; do
  src="${REPO_ROOT}/packaging/debian/${script}"
  if [[ -f "$src" ]]; then
    install -m 0755 "$src" "${DEB_ROOT}/DEBIAN/${script}"
  fi
done

mkdir -p "$OUT_DIR"
OUT_FILE="${OUT_DIR}/linuxio_${DEB_VERSION}.deb"

if command -v dpkg-deb >/dev/null 2>&1; then
  dpkg-deb --root-owner-group --build "$DEB_ROOT" "$OUT_FILE"
else
  echo "dpkg-deb not found. Please install dpkg-dev." >&2
  exit 1
fi

echo "Built: ${OUT_FILE}"
