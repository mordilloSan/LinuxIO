#!/usr/bin/env bash
# Fixture checks for changelog-entry.sh, the changelog the packages carry
# from the release notes: the Markdown of release-notes.md becomes plain
# text, the DEB's entry is one dpkg reads (with dpkg-parsechangelog where it
# is installed), the RPM's has no line rpm would drop or read as a new entry,
# and without notes each names the version alone. The packages' own copies
# go through repo-fixtures.sh and the test VMs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRY="${SCRIPT_DIR}/../changelog-entry.sh"
NOTES="${SCRIPT_DIR}/release-notes.md"

fail() {
	echo "❌ $*" >&2
	exit 1
}

pass() {
	printf '   \033[1;32m✓\033[0m %s\n' "$1"
}

skip() {
	printf '   \033[1;33m-\033[0m %s\n' "$1"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for args in "" "deb" "dpkg --version 1.0" "deb --version 1.0 --notes ${work}/none" "rpm --version 1.0 --sign"; do
	# shellcheck disable=SC2086 # each case is several words
	if bash "$ENTRY" $args >/dev/null 2>&1; then fail "changelog-entry.sh accepted: ${args}"; fi
done
pass "a missing format, version or notes file and an unknown argument are refused"

bash "$ENTRY" deb --version 1.0.2 --notes "$NOTES" >"${work}/deb"
[[ $(head -n 1 "${work}/deb") == "linuxio (1.0.2) unstable; urgency=medium" ]] || fail "the DEB entry does not open with its version"
grep -Eq '^ -- LinuxIO packages <packages@engmariz\.com>  [A-Z][a-z]{2}, ' "${work}/deb" || fail "the DEB entry has no trailer line"
for line in "  Features:" \
	"  * updates: show the package's changes on the Updates page (abc1234)" \
	"  * storage: stop a scan that never ends" \
	"  Full Changelog: https://github.com/mordilloSan/LinuxIO/compare/v0.0.1...v0.0.2"; do
	grep -qxF "$line" "${work}/deb" || fail "the DEB entry has no line '${line}'"
done
! grep -qE '\*\*|\]\(|^#' "${work}/deb" || fail "the DEB entry keeps Markdown"
! grep -qE '^[^ ].*$' <(sed '1d;$d' "${work}/deb") || fail "a DEB change line does not start with a space"
if command -v dpkg-parsechangelog >/dev/null; then
	[[ $(dpkg-parsechangelog -l "${work}/deb" -S Version) == 1.0.2 ]] || fail "dpkg-parsechangelog does not read version 1.0.2"
	grep -qF "updates: show the package's changes" <<<"$(dpkg-parsechangelog -l "${work}/deb" -S Changes)" ||
		fail "dpkg-parsechangelog does not read the notes as the changes"
	pass "the DEB entry carries the notes as text, and dpkg reads it"
else
	skip "no dpkg-parsechangelog: the DEB entry is checked as text only"
fi

bash "$ENTRY" rpm --version 1.0.2 --notes "$NOTES" >"${work}/rpm"
head -n 1 "${work}/rpm" | grep -Eqx '\* [A-Z][a-z]{2} [A-Z][a-z]{2} [0-9]{2} [0-9]{4} LinuxIO packages <packages@engmariz\.com> - 1\.0\.2-1' ||
	fail "the RPM entry does not open with an rpm changelog header"
! grep -qE '^[[:space:]]*[#*%]' <(tail -n +2 "${work}/rpm") || fail "an RPM line starts with #, * or %, which rpm drops or reads as an entry or a section"
for line in "- memory: keep 50% of %{name} free (def5678)" "- #42 stays as it was" "- *emphasis* stays text" "- %post stays text"; do
	grep -qxF -- "$line" "${work}/rpm" || fail "the RPM entry has no line '${line}'"
done
pass "the RPM entry carries the notes as text, with no line rpm would drop or split at"

grep -qxF "  * LinuxIO 1.0.2." <<<"$(bash "$ENTRY" deb --version 1.0.2)" || fail "the DEB entry without notes does not name the version"
grep -qxF -- "- LinuxIO 1.0.2." <<<"$(bash "$ENTRY" rpm --version 1.0.2)" || fail "the RPM entry without notes does not name the version"
printf '\n  \n' >"${work}/empty.md"
grep -qxF -- "- LinuxIO 1.0.2." <<<"$(bash "$ENTRY" rpm --version 1.0.2 --notes "${work}/empty.md")" ||
	fail "empty notes do not fall back to the version"
pass "without notes, or with empty ones, the entry names the version alone"
