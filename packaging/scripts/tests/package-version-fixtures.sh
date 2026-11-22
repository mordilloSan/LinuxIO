#!/usr/bin/env bash
# Fixture checks for package-version.sh, the one version the DEB and the RPM
# carry: in a throwaway git repository, a release tag gives its number, a
# commit after it the number with +devN.gSHA, a tag that is not a release is
# passed over, and a repository with no release tag starts from 0.0.0. Where
# dpkg is present, the versions also sort as a release needs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_VERSION="${SCRIPT_DIR}/../package-version.sh"

fail() {
	echo "❌ $*" >&2
	exit 1
}

pass() {
	printf '   \033[1;32m✓\033[0m %s\n' "$1"
}

repo=$(mktemp -d)
trap 'rm -rf "$repo"' EXIT
g() { git -C "$repo" -c user.name=fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false -c tag.gpgsign=false "$@"; }
commit() { g commit -q --allow-empty -m "$1"; }
sha() { g rev-parse --short HEAD; }
version() { bash "$PACKAGE_VERSION" "$repo"; }
expect() {
	local got
	got=$(version)
	[[ $got == "$1" ]] || fail "$2: expected $1, got ${got}"
}

g init -q
commit one
expect "0.0.0+dev1.g$(sha)" "no release tag"
g tag v1.2.3
expect 1.2.3 "on the tag"
commit two
expect "1.2.3+dev1.g$(sha)" "a commit after the tag"
g tag v1.3.0-rc1
commit three
expect "1.2.3+dev2.g$(sha)" "past a tag that is not a release"
pass "release tags, commits after them and other tags"

if command -v dpkg >/dev/null; then
	for pair in "1.2.3 1.2.3+dev1.gabc1234" "1.2.3+dev1.gabc1234 1.2.3+dev10.gabc1234" "1.2.3+dev10.gabc1234 1.2.4"; do
		read -r lower higher <<<"$pair"
		dpkg --compare-versions "$lower" lt "$higher" || fail "dpkg sorts ${lower} after ${higher}"
	done
	pass "dpkg sorts a release before its dev versions and those before the next release"
fi

printf '\033[1;32m%s\033[0m\n' "✅ package-version fixtures passed!"
