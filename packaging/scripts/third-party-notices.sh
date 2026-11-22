#!/usr/bin/env bash
# Write the third-party notices of a build, the file both packages install as
# /usr/share/doc/linuxio/THIRD_PARTY_NOTICES.md, from what the build holds:
#
# - docs/THIRD_PARTY_NOTICES.md, written by hand: the code LinuxIO adapted,
#   the icon sets, and where the source of the MPL-2.0 parts is;
# - Go's own licence, for the standard library and runtime in every Go
#   program;
# - the licence files of every module the Go programs report as linked
#   (`go version -m`), read from the module cache the build filled, those of
#   code a module carries in a subfolder included;
# - the licences of the packages in the frontend bundle, which Vite wrote
#   (`build.license` in frontend/config/vite.config.ts).
#
# It fails on a linked module without a licence file, and on a frontend
# bundle without Vite's file, so neither ships unnamed. A licence file with
# the same bytes as one already written is named, not repeated.
#
# Usage: third-party-notices.sh --build-dir DIR --frontend-dir DIR [--go GO]
#        [--out FILE] PROGRAM...
# PROGRAM is the name of a Go program in the build folder; FILE defaults to
# THIRD_PARTY_NOTICES.md there.
set -euo pipefail
umask 022
# One order of modules and files on every host.
export LC_ALL=C

packaging_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
written="$(dirname "$packaging_dir")/docs/THIRD_PARTY_NOTICES.md"
usage="usage: third-party-notices.sh --build-dir DIR --frontend-dir DIR [--go GO] [--out FILE] PROGRAM..."

die() {
	echo "third-party-notices: $*" >&2
	exit 1
}

build_dir=""
frontend_dir=""
go="go"
out=""
programs=()
while [[ $# -gt 0 ]]; do
	case "$1" in
	--build-dir | --frontend-dir | --go | --out)
		[[ $# -ge 2 && -n $2 ]] || die "$1 needs a value"
		case "$1" in
		--build-dir) build_dir="$2" ;;
		--frontend-dir) frontend_dir="$2" ;;
		--go) go="$2" ;;
		--out) out="$2" ;;
		esac
		shift 2
		;;
	-*) die "unknown argument ${1} (${usage})" ;;
	*)
		programs+=("$1")
		shift
		;;
	esac
done
[[ -n $build_dir && -n $frontend_dir && ${#programs[@]} -gt 0 ]] || die "$usage"
[[ -d $build_dir ]] || die "build folder ${build_dir} does not exist"
[[ -f $written ]] || die "missing ${written}"
out="${out:-${build_dir}/THIRD_PARTY_NOTICES.md}"

modcache="$("$go" env GOMODCACHE)"
goroot="$("$go" env GOROOT)"
[[ -f "${goroot}/LICENSE" ]] || die "no LICENSE in the Go root ${goroot}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# What the programs report: their Go versions and "MODULE VERSION" lines. A
# "=>" line names what replaced the module on the line before it.
for program in "${programs[@]}"; do
	[[ -f "${build_dir}/${program}" ]] || die "no program ${build_dir}/${program}: build it first"
	"$go" version -m "${build_dir}/${program}" >"${work}/info" ||
		die "could not read the modules of ${build_dir}/${program}"
	sed -n '1s/^.*: //p' "${work}/info" >>"${work}/go-versions"
	awk '$1 == "dep" { if (m) print m; m = $2 " " $3 }
		$1 == "=>" { m = $2 " " $3 }
		END { if (m) print m }' "${work}/info" >>"${work}/modules"
done
sort -u -o "${work}/modules" "${work}/modules"
go_versions="$(sort -u "${work}/go-versions" | paste -sd, - | sed 's/,/, /g')"

# cache_name MODULE@VERSION: its folder name in the module cache, which writes
# a capital letter as "!" and the small one.
cache_name() {
	local name=$1 out="" char i
	for ((i = 0; i < ${#name}; i++)); do
		char=${name:i:1}
		[[ $char == [[:upper:]] ]] && char="!${char,,}"
		out+=$char
	done
	printf '%s\n' "$out"
}

declare -A seen=()
{
	cat "$written"
	printf '\n## Go standard library and runtime\n\n'
	printf 'Every Go program of LinuxIO (%s) holds the Go standard library and\nruntime (%s). Source: https://go.dev/dl/\n\n' \
		"${programs[*]}" "$go_versions"
	cat "${goroot}/LICENSE"
	printf '\n## Go modules\n\n'
	printf 'The modules linked into the Go programs, each at the version named, as the\nprograms themselves report them. The source of each is at the link under\nits name.\n'
	while read -r module version; do
		[[ -n $version && $version != "(devel)" && $module != [./]* ]] ||
			die "module ${module} is replaced by a folder, which has no version: add its licence to docs/THIRD_PARTY_NOTICES.md and teach this script about it"
		dir="${modcache}/$(cache_name "${module}@${version}")"
		[[ -d $dir ]] || die "module ${module} ${version} is not in the module cache (${dir}): build the programs first"
		printf '\n### %s %s\n\nSource: https://pkg.go.dev/%s@%s\n' "$module" "$version" "$module" "$version"
		found=0
		# Subfolders too: a module can carry another project's code with its
		# own licence. Not a source file that happens to be named licence.go.
		while read -r file; do
			name="${file##*/}"
			[[ ${name^^} =~ ^(LICEN[SC]E|COPYING|NOTICE|UNLICENSE)([-._].*)?$ ]] || continue
			[[ ${name,,} != *.@(go|sh|py|js|c|h) ]] || continue
			name="${file#"$dir"/}"
			found=1
			sum="$(sha256sum <"$file")"
			if [[ -n ${seen[$sum]:-} ]]; then
				printf '\n%s: the same text as %s under %s above.\n' "$name" "${seen[$sum]#* }" "${seen[$sum]%% *}"
				continue
			fi
			seen[$sum]="${module} ${name}"
			printf '\n%s:\n\n' "$name"
			cat "$file"
		done < <(find "$dir" -type f -not -path '*/testdata/*' | sort)
		((found)) || die "module ${module} ${version} has no licence file in ${dir}: it cannot ship without one"
	done <"${work}/modules"

	printf '\n## Frontend packages\n\n'
	if [[ -f "${frontend_dir}/.vite/license.md" ]]; then
		[[ "$(head -n 1 "${frontend_dir}/.vite/license.md")" == "# Licenses" ]] ||
			die "${frontend_dir}/.vite/license.md does not start as Vite's licence file does"
		printf 'The packages in the page that linuxio-webserver holds and sends to the\nbrowser, each at the version named, as Vite lists them. They fill the rest\nof this file.\n\n'
		sed '1,/^## /{/^## /!d}' "${frontend_dir}/.vite/license.md"
	elif [[ -f "${frontend_dir}/index.html" ]]; then
		# The placeholder files of make dev-prep have no page.
		die "the frontend in ${frontend_dir} was built without its licence file: run make build-vite"
	else
		printf 'This build holds no frontend bundle.\n'
	fi
} >"${work}/notices"

mkdir -p "$(dirname "$out")"
mv "${work}/notices" "$out"
chmod 644 "$out"
