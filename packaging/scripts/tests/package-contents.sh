#!/usr/bin/env bash
# Check a LinuxIO DEB or RPM against the build it was assembled from, before
# it is installed (the test VM runs it right after assembling one):
#
# - its files and links are exactly the tree package-root.sh stages from the
#   build, at the same modes, plus what that family adds (below), all owned by
#   root;
# - every file under /etc is a configuration file (a conffile in the DEB,
#   %config(noreplace) in the RPM);
# - nothing is under /usr/local;
# - the nine programs are byte for byte the build's (SHA-256): the webserver
#   checks the bridge's hash recorded at build time.
#
# The shared tree is not written down again here: package-root.sh stages it,
# and package-root-fixtures.sh holds it to its manifest.
#
# Usage: package-contents.sh PACKAGE.deb|PACKAGE.rpm BUILD_DIR
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_ROOT="${SCRIPT_DIR}/../package-root.sh"

# What each family adds to the shared tree: "TYPE MODE PATH", TYPE f for a
# file and l for a link (whose mode is not compared).
deb_additions="\
f 644 etc/pam.d/linuxio
f 644 etc/needrestart/conf.d/linuxio.conf
f 644 usr/share/doc/linuxio/copyright
f 644 usr/share/doc/linuxio/changelog.gz
f 644 usr/share/doc/linuxio/THIRD_PARTY_NOTICES.md
f 644 usr/share/man/man8/linuxio.8.gz"
rpm_additions="\
f 644 etc/pam.d/linuxio
f 644 usr/share/selinux/packages/targeted/linuxio.cil
f 644 usr/share/licenses/linuxio/LICENSE
f 644 usr/share/doc/linuxio/THIRD_PARTY_NOTICES.md
f 644 usr/share/man/man8/linuxio.8.gz"

fail() {
	echo "❌ package-contents: $*" >&2
	exit 1
}

pass() {
	printf '   \033[1;32m✓\033[0m %s\n' "$1"
}

# perm_octal rwxr-x---: the octal mode of a permission string from tar -tv.
perm_octal() {
	local perms=$1 mode=0 i
	[[ $perms =~ ^[r-][w-][x-][r-][w-][x-][r-][w-][x-]$ ]] || {
		echo "?"
		return
	}
	for ((i = 0; i < 9; i++)); do
		mode=$((mode * 2))
		[[ ${perms:i:1} == - ]] || mode=$((mode + 1))
	done
	printf '%o\n' "$mode"
}

package=${1:-}
build_dir=${2:-}
[[ -n $package && -n $build_dir ]] || fail "usage: package-contents.sh PACKAGE.deb|PACKAGE.rpm BUILD_DIR"
[[ -f $package ]] || fail "no package ${package}"
[[ -d $build_dir ]] || fail "no build folder ${build_dir}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The package as lines "TYPE MODE OWNER CONF PATH": TYPE f, d or l, CONF c for
# a configuration file and - otherwise; and the nine programs' hashes as
# "SHA256  PATH".
case $package in
*.deb)
	command -v dpkg-deb >/dev/null || fail "dpkg-deb is needed to read ${package}"
	additions=$deb_additions
	dpkg-deb --ctrl-tarfile "$package" | tar -xOf - ./conffiles 2>/dev/null | sed 's#^/##' >"${work}/conffiles" || true
	dpkg-deb --fsys-tarfile "$package" | tar -tv --numeric-owner -f - |
		while read -r perms owner _ _ _ path _; do
			path=${path#./}
			[[ -n $path ]] || continue
			path=${path%/}
			type=${perms:0:1}
			[[ $type == - ]] && type=f
			conf=-
			grep -qxF "$path" "${work}/conffiles" && conf=c
			echo "${type} $(perm_octal "${perms:1}") ${owner} ${conf} ${path}"
		done >"${work}/package"
	mkdir "${work}/unpacked"
	dpkg-deb -x "$package" "${work}/unpacked"
	;;
*.rpm)
	command -v rpm >/dev/null || fail "rpm is needed to read ${package}"
	additions=$rpm_additions
	[[ "$(rpm -qp --qf '%{FILEDIGESTALGO}' "$package" 2>/dev/null)" == 8 ]] ||
		fail "${package} does not record SHA-256 file digests"
	rpm -qp --qf '[%{FILEMODES:octal} %{FILEUSERNAME}:%{FILEGROUPNAME} =%{FILEFLAGS:fflags} %{FILENAMES}\n]' "$package" 2>/dev/null |
		while read -r mode owner flags path; do
			flags=${flags#=}
			case $((8#$mode & 8#170000)) in
			$((8#100000))) type=f ;;
			$((8#40000))) type=d ;;
			$((8#120000))) type=l ;;
			*) type=other ;;
			esac
			conf=-
			[[ $flags == *c* && $flags == *n* ]] && conf=c
			[[ $owner == root:root ]] && owner=0/0
			printf '%s %o %s %s %s\n' "$type" $((8#$mode & 8#7777)) "$owner" "$conf" "${path#/}"
		done >"${work}/package"
	rpm -qp --qf '[%{FILEDIGESTS}  %{FILENAMES}\n]' "$package" 2>/dev/null >"${work}/digests"
	;;
*) fail "${package} is neither a .deb nor a .rpm" ;;
esac
[[ -s "${work}/package" ]] || fail "could not list ${package}"

# 1. Files and links: the staged tree plus the family's additions, same modes.
bash "$PACKAGE_ROOT" --build-dir "$build_dir" --destdir "${work}/tree" >/dev/null ||
	fail "package-root.sh could not stage ${build_dir}"
expected=$({
	(cd "${work}/tree" && find . -mindepth 1 -type f -printf 'f %m %P\n')
	awk '$1 == "f" { print } $1 == "l" { print "l - " $3 }' <<<"$additions"
} | LC_ALL=C sort)
actual=$(awk '$1 == "f" { print "f", $2, $5 } $1 == "l" { print "l -", $5 }' "${work}/package" | LC_ALL=C sort)
if [[ $actual != "$expected" ]]; then
	diff <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >&2 || true
	fail "the files of ${package##*/} differ from the build's tree (< expected, > packaged)"
fi
pass "files and links are the staged tree and the family's additions, at their modes"
root_only=$(awk '$1 == "f" && $2 == "700" { sub(".*/", "", $5); print $5 }' "${work}/package" | LC_ALL=C sort | tr '\n' ' ')
pass "0700: ${root_only% }"

# 2. Owned by root.
others=$(awk '$3 != "0/0" { print $3, $5 }' "${work}/package")
[[ -z $others ]] || fail "not owned by root: ${others//$'\n'/, }"
pass "every entry is owned by root"

# 3. Configuration: every file and link under /etc.
not_conf=$(awk '$1 != "d" && $5 ~ /^etc\// && $4 != "c" { print $5 }' "${work}/package")
[[ -z $not_conf ]] || fail "under /etc but not a configuration file: ${not_conf//$'\n'/ }"
pass "everything under /etc is a configuration file"

# 4. Nothing under /usr/local.
local_paths=$(awk '$5 ~ /^usr\/local(\/|$)/ { print $5 }' "${work}/package")
[[ -z $local_paths ]] || fail "under /usr/local: ${local_paths//$'\n'/ }"
pass "nothing under /usr/local"

# 5. The programs, byte for byte.
programs=$(cd "${work}/tree" && find usr/bin usr/libexec/linuxio -type f -printf '%P %p\n' | LC_ALL=C sort)
count=0
while read -r name path; do
	built=$(sha256sum <"${build_dir}/${name}" | cut -d' ' -f1)
	if [[ $package == *.deb ]]; then
		packaged=$(sha256sum <"${work}/unpacked/${path}" | cut -d' ' -f1)
	else
		packaged=$(awk -v p="/${path}" '$2 == p { print $1 }' "${work}/digests")
	fi
	[[ $packaged == "$built" ]] || fail "${path} is not the build's ${name} (SHA-256 ${packaged:-missing}, built ${built})"
	count=$((count + 1))
done <<<"$programs"
[[ $count -eq 9 ]] || fail "expected nine programs, found ${count}"
pass "the nine programs have the build's SHA-256"

printf '\033[1;32m%s\033[0m\n' "✅ ${package##*/} matches ${build_dir}"
