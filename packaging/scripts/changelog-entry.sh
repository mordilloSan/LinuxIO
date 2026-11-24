#!/usr/bin/env bash
# The changelog entry of a LinuxIO package, from the release notes
# (docs/packaging.md, "Changelog"): the whole debian/changelog for
# package-deb.sh, or the text of the spec's %changelog for package-rpm.sh.
#
# NOTES is the Markdown body gh release-flow writes on the release PR. The
# packages carry it as plain text: headings become "Heading:", bold and links
# keep their text, and every list item is one line. Without NOTES, or with an
# empty file, the entry says "LinuxIO VERSION." alone, as a local build or a
# dry run has no notes.
#
# rpm drops a %changelog line whose first character other than a space is
# "#", as a comment, starts a new entry at a line beginning with "*", and
# reads a line beginning with "%" as a section or a conditional (a "%post"
# line would be a scriptlet), so the RPM's text has none of them: such a line
# becomes a list item.
#
# Usage: changelog-entry.sh deb|rpm --version VERSION [--notes FILE]
set -euo pipefail

die() {
	echo "changelog-entry: $*" >&2
	exit 1
}

usage="usage: changelog-entry.sh deb|rpm --version VERSION [--notes FILE]"
format="${1:-}"
[[ $format == deb || $format == rpm ]] || die "$usage"
shift
version=""
notes=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--version | --notes)
		[[ $# -ge 2 && -n $2 ]] || die "$1 needs a value"
		case "$1" in
		--version) version="$2" ;;
		--notes) notes="$2" ;;
		esac
		shift 2
		;;
	*) die "unknown argument ${1} (${usage})" ;;
	esac
done
[[ -n $version ]] || die "$usage"
[[ -z $notes || -f $notes ]] || die "release notes ${notes} do not exist"

readonly maintainer="LinuxIO packages <packages@engmariz.com>"

# The notes as plain text, list items as "- ", one empty line at most
# between paragraphs and none around them.
plain_notes() {
	sed -E \
		-e 's/\r$//' \
		-e 's/[[:space:]]+$//' \
		-e '/^[[:space:]]*<!--.*-->$/d' \
		-e 's/!\[([^]]*)\]\([^)]*\)/\1/g' \
		-e 's/\[([^]]*)\]\([^)]*\)/\1/g' \
		-e 's/\*\*([^*]+)\*\*/\1/g' \
		-e 's/__([^_]+)__/\1/g' \
		-e 's/^#{1,6}[[:space:]]+(.+)$/\1:/' \
		-e 's/^([[:space:]]*)[*+-][[:space:]]+/\1- /' \
		"$notes" |
		awk 'NF { if (seen && blank) print ""; print; seen = 1; blank = 0; next } { blank = 1 }'
}

text=""
[[ -z $notes ]] || text="$(plain_notes)"
[[ -n $text ]] || text="- LinuxIO ${version}."

case $format in
deb)
	echo "linuxio (${version}) unstable; urgency=medium"
	echo
	# Change lines start with two spaces, items with "* ".
	sed -E -e 's/^- /* /' -e 's/^(.)/  \1/' <<<"$text"
	echo
	echo " -- ${maintainer}  $(LC_ALL=C date -R)"
	;;
rpm)
	echo "* $(LC_ALL=C date '+%a %b %d %Y') ${maintainer} - ${version}-1"
	sed -E 's/^([[:space:]]*)([#*%])/\1- \2/' <<<"$text"
	;;
esac
