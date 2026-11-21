#!/usr/bin/env bash
# Download the DEB and the RPM of the newest published releases other than
# TAG, so the release workflow can rebuild the package repositories with the
# last KEEP versions: TAG's own packages and those of KEEP-1 releases before
# or after it. Only releases named vX.Y.Z that are neither drafts nor
# prereleases count, and a release without both packages (one from before
# LinuxIO was packaged) is passed over. Each file is checked against the
# SHA256SUMS of its own release. With no such release, the very first, it
# downloads nothing. The token is read from GH_TOKEN.
#
# Usage: previous-packages.sh --repo OWNER/NAME --tag vX.Y.Z --keep N --out-dir DIR
set -euo pipefail

die() {
	echo "previous-packages: $*" >&2
	exit 1
}

repo=""
tag=""
keep=""
out_dir=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--repo | --tag | --keep | --out-dir)
		[[ $# -ge 2 && -n $2 ]] || die "$1 needs a value"
		case "$1" in
		--repo) repo="$2" ;;
		--tag) tag="$2" ;;
		--keep) keep="$2" ;;
		--out-dir) out_dir="$2" ;;
		esac
		shift 2
		;;
	*) die "unknown argument ${1} (usage: previous-packages.sh --repo OWNER/NAME --tag vX.Y.Z --keep N --out-dir DIR)" ;;
	esac
done
[[ $repo =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "--repo must be OWNER/NAME"
[[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "--tag must be vX.Y.Z"
[[ $keep =~ ^[1-9][0-9]*$ ]] || die "--keep must be a positive number"
[[ -n $out_dir ]] || die "--out-dir is required"
[[ -n ${GH_TOKEN:-} ]] || die "GH_TOKEN is not set"
for tool in curl jq sha256sum; do
	command -v "$tool" >/dev/null || die "${tool} is required"
done

api() {
	curl -fsSL --retry 3 --max-time 300 \
		-H "Authorization: Bearer ${GH_TOKEN}" \
		-H "X-GitHub-Api-Version: 2022-11-28" "$@"
}

mkdir -p "$out_dir"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

releases="$(api -H "Accept: application/vnd.github+json" "https://api.github.com/repos/${repo}/releases?per_page=100")" ||
	die "cannot list the releases of ${repo}"
mapfile -t tags < <(jq -r --arg tag "$tag" '.[]
	| select((.draft | not) and (.prerelease | not))
	| .tag_name
	| select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+$") and . != $tag)' <<<"$releases" | sort -rV)

wanted=$((keep - 1))
found=0
for release in "${tags[@]}"; do
	((found < wanted)) || break
	assets="$(jq -c --arg t "$release" '.[] | select(.tag_name == $t) | .assets' <<<"$releases")"
	deb="$(jq -r '[.[] | select(.name | test("^linuxio_[^/]*_amd64\\.deb$"))][0].name // empty' <<<"$assets")"
	rpm="$(jq -r '[.[] | select(.name | test("^linuxio-[^/]*\\.x86_64\\.rpm$"))][0].name // empty' <<<"$assets")"
	sums="$(jq -r '[.[] | select(.name == "SHA256SUMS")][0].name // empty' <<<"$assets")"
	if [[ -z $deb || -z $rpm || -z $sums ]]; then
		echo "previous-packages: ${release} has no DEB, RPM and SHA256SUMS; passed over" >&2
		continue
	fi
	dir="${work}/${release}"
	mkdir -p "$dir"
	for name in "$deb" "$rpm" "$sums"; do
		url="$(jq -r --arg n "$name" '.[] | select(.name == $n) | .url' <<<"$assets")"
		api -H "Accept: application/octet-stream" -o "${dir}/${name}" "$url" ||
			die "cannot download ${name} of ${release}"
	done
	# The lines of SHA256SUMS that name the two packages, exactly.
	lines="$(awk -v a="$deb" -v b="$rpm" '{ n = $2; sub(/^\*/, "", n) } n == a || n == b' "${dir}/${sums}")"
	[[ $(wc -l <<<"$lines") -eq 2 ]] || die "${release}'s SHA256SUMS does not list ${deb} and ${rpm} once each"
	(cd "$dir" && sha256sum --check --strict --quiet <<<"$lines") ||
		die "${release}'s packages do not match its SHA256SUMS"
	mv -f "${dir}/${deb}" "${dir}/${rpm}" "$out_dir/"
	echo "previous-packages: ${release}: ${deb} ${rpm}" >&2
	found=$((found + 1))
done
echo "previous-packages: kept ${found} earlier release(s) beside ${tag}" >&2
