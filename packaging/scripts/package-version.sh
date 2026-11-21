#!/usr/bin/env bash
# Print the version the DEB and the RPM of this checkout carry, one string
# valid in both formats: X.Y.Z on a commit tagged vX.Y.Z, otherwise
# X.Y.Z+devN.gSHA, where vX.Y.Z is the last release tag the commit reaches and
# N the commits since it. That sorts after the tag and before the next
# release in dpkg and in rpm. A checkout no release tag reaches gives
# 0.0.0+devN.gSHA. mk/package.mk reads it as PACKAGE_VERSION.
#
# Usage: package-version.sh [REPO]
set -euo pipefail

repo="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

describe="$(git -C "$repo" describe --tags --long --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' 2>/dev/null)" || describe=""
if [[ $describe =~ ^v([0-9]+\.[0-9]+\.[0-9]+)-([0-9]+)-g([0-9a-f]+)$ ]]; then
	if [[ ${BASH_REMATCH[2]} == 0 ]]; then
		echo "${BASH_REMATCH[1]}"
	else
		echo "${BASH_REMATCH[1]}+dev${BASH_REMATCH[2]}.g${BASH_REMATCH[3]}"
	fi
	exit 0
fi
count="$(git -C "$repo" rev-list --count HEAD)"
sha="$(git -C "$repo" rev-parse --short HEAD)"
echo "0.0.0+dev${count}.g${sha}"
