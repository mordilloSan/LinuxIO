#!/usr/bin/env bash
# =============================================================================
# LinuxIO RPM Package Builder
# Builds an .rpm from prebuilt binaries and packaging assets
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

RPM_VERSION="${base_version}"
if [[ "$RAW_VERSION" == dev-* ]]; then
  RPM_RELEASE="0.dev.${COMMIT_SHORT}"
else
  RPM_RELEASE="1"
fi

OUT_DIR="${PKG_OUTPUT_DIR:-$REPO_ROOT/dist}"
WORK_DIR="$(mktemp -d)"
TOPDIR="${WORK_DIR}/rpmbuild"
STAGE_DIR="${WORK_DIR}/stage/linuxio-${RPM_VERSION}"

cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

mkdir -p "$TOPDIR/BUILD" "$TOPDIR/RPMS" "$TOPDIR/SOURCES" "$TOPDIR/SPECS" "$TOPDIR/SRPMS"
mkdir -p "$STAGE_DIR"

"$SCRIPT_DIR/package-stage.sh" "${STAGE_DIR}"

tar -czf "${TOPDIR}/SOURCES/linuxio-${RPM_VERSION}.tar.gz" -C "${WORK_DIR}/stage" "linuxio-${RPM_VERSION}"

spec_template="${REPO_ROOT}/packaging/rpm/linuxio.spec.in"
if [[ ! -f "$spec_template" ]]; then
  echo "Missing spec template: ${spec_template}" >&2
  exit 1
fi

sed \
  -e "s/@VERSION@/${RPM_VERSION}/g" \
  -e "s/@RELEASE@/${RPM_RELEASE}/g" \
  "$spec_template" > "${TOPDIR}/SPECS/linuxio.spec"

if ! command -v rpmbuild >/dev/null 2>&1; then
  echo "rpmbuild not found. Please install rpm-build." >&2
  exit 1
fi

rpmbuild --define "_topdir ${TOPDIR}" -bb "${TOPDIR}/SPECS/linuxio.spec"

mkdir -p "$OUT_DIR"
find "${TOPDIR}/RPMS" -name "linuxio-*.rpm" -exec cp -f {} "$OUT_DIR/" \;

echo "Built RPM(s) in: ${OUT_DIR}"
