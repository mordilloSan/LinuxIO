#!/usr/bin/env bash
# Move the build pins to their newest and print a suggested commit message:
# the digests of the two release images in packaging/build/pins.env (the
# Fedora image also to the release Docker Hub calls latest; the compile image
# keeps its release, chosen for its C library), Node within the major the file
# names, the Go check tools (a release that needs a newer Go than
# backend/go.mod is left alone), and every GitHub Action under
# .github/workflows to the commit of its newest release, pinned by commit with
# the version in a comment. Every lookup is made before anything is written,
# so an unreachable registry changes nothing. Needs curl, jq and git; no sudo,
# no docker.
# Usage: update-build-pins.sh REPO_ROOT
set -euo pipefail

repo="$1"
pins="${repo}/packaging/build/pins.env"
workflows_dir="${repo}/.github/workflows"

die() {
	echo "❌ update-build-pins: $*" >&2
	exit 1
}

for tool in curl jq git; do
	command -v "$tool" >/dev/null || die "${tool} is required"
done
[[ -f $pins ]] || die "missing ${pins}"

fetch() { curl -fsSL --retry 2 --max-time 30 "$@"; }
# git ls-remote fails instead of asking for a login.
export GIT_TERMINAL_PROMPT=0

pin() { sed -n "s/^$1=//p" "$pins"; }

changes=()
pin_updates=()
action_updates=()

# ---- images --------------------------------------------------------------
manifest_types='application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json'

# hub_digest NAME TAG: the digest Docker Hub serves for the official image
# NAME:TAG (the multi-architecture index where there is one).
hub_digest() {
	local token digest
	token="$(fetch "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/$1:pull" | jq -er .token)" ||
		die "cannot reach Docker Hub (auth.docker.io) for $1"
	digest="$(fetch -I -H "Authorization: Bearer ${token}" -H "Accept: ${manifest_types}" \
		"https://registry-1.docker.io/v2/library/$1/manifests/$2" |
		tr -d '\r' | awk 'tolower($1) == "docker-content-digest:" { print $2 }')" ||
		die "cannot read $1:$2 from Docker Hub (registry-1.docker.io)"
	[[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || die "Docker Hub gave no digest for $1:$2"
	echo "$digest"
}

# update_image KEY FOLLOW_LATEST
update_image() {
	local key="$1" follow_latest="$2" current name tag digest new_tag new_digest latest next
	current="$(pin "$key")"
	[[ $current =~ ^([a-z0-9._-]+):([A-Za-z0-9._-]+)@(sha256:[0-9a-f]{64})$ ]] ||
		die "${key} in ${pins} is not NAME:RELEASE@sha256:DIGEST: '${current}'"
	name="${BASH_REMATCH[1]}" tag="${BASH_REMATCH[2]}" digest="${BASH_REMATCH[3]}"
	new_tag="$tag"
	new_digest="$(hub_digest "$name" "$tag")"
	if [[ $follow_latest == 1 && $tag =~ ^[0-9]+$ ]]; then
		latest="$(hub_digest "$name" latest)"
		if [[ $latest != "$new_digest" ]]; then
			# The next release may not be published yet: no tag is no move.
			next="$(hub_digest "$name" "$((tag + 1))" 2>/dev/null)" || next=""
			[[ $latest == "$next" ]] && new_tag="$((tag + 1))" new_digest="$latest"
		fi
	fi
	[[ $new_tag == "$tag" && $new_digest == "$digest" ]] && return 0
	pin_updates+=("${key}=${name}:${new_tag}@${new_digest}")
	if [[ $new_tag != "$tag" ]]; then
		changes+=("update ${name} image from ${tag} to ${new_tag}")
	else
		changes+=("update ${name}:${tag} image digest to ${new_digest:7:12}")
	fi
}

update_image COMPILE_IMAGE 0
update_image RPM_IMAGE 1

# ---- Node ----------------------------------------------------------------
node_current="$(pin NODE_VERSION)"
[[ $node_current =~ ^([0-9]+)\.[0-9]+\.[0-9]+$ ]] || die "NODE_VERSION in ${pins} is not a full release: '${node_current}'"
node_major="${BASH_REMATCH[1]}"
node_newest="$(fetch https://nodejs.org/dist/index.json |
	jq -er --arg m "v${node_major}." '[.[] | select(.version | startswith($m))][0].version')" ||
	die "cannot read the Node releases from nodejs.org"
node_newest="${node_newest#v}"
if [[ $node_newest != "$node_current" ]]; then
	pin_updates+=("NODE_VERSION=${node_newest}")
	changes+=("update Node from ${node_current} to ${node_newest}")
fi

# ---- Go check tools ------------------------------------------------------
go_version="$(awk '$1 == "go" { print $2; exit }' "${repo}/backend/go.mod")"
[[ -n $go_version ]] || die "no go directive in ${repo}/backend/go.mod"
notes=()

# update_tool KEY MODULE LABEL
update_tool() {
	local key="$1" module="$2" label="$3" current newest needs
	current="$(pin "$key")"
	newest="$(fetch "https://proxy.golang.org/${module}/@latest" | jq -er .Version)" ||
		die "cannot read ${module} from the Go module proxy (proxy.golang.org)"
	[[ $newest == "$current" ]] && return 0
	needs="$(fetch "https://proxy.golang.org/${module}/@v/${newest}.mod" | awk '$1 == "go" { print $2; exit }')" ||
		die "cannot read the go.mod of ${module}@${newest} from proxy.golang.org"
	if [[ -n $needs && $(printf '%s\n%s\n' "$needs" "$go_version" | sort -V | tail -n1) != "$go_version" ]]; then
		notes+=("${label} ${newest} needs Go ${needs}; kept at ${current} until backend/go.mod moves")
		return 0
	fi
	pin_updates+=("${key}=${newest}")
	changes+=("update ${label} from ${current} to ${newest}")
}

update_tool GOLANGCI_LINT_VERSION github.com/golangci/golangci-lint/v2 golangci-lint
update_tool MODERNIZE_VERSION golang.org/x/tools modernize
update_tool DEADCODE_VERSION golang.org/x/tools deadcode
update_tool GOVULNCHECK_VERSION golang.org/x/vuln govulncheck

# ---- GitHub Actions ------------------------------------------------------
shopt -s nullglob
workflows=("$workflows_dir"/*.yml "$workflows_dir"/*.yaml)
[[ ${#workflows[@]} -gt 0 ]] || die "no workflows in ${workflows_dir}"

# Every remote action as OWNER/REPO with the version its lines name now: the
# comment after a commit pin, or the tag itself on a line not pinned yet.
declare -A action_version=()
while IFS=$'\t' read -r action ref comment; do
	# A reference that follows a branch is its author's choice and has no
	# release to pin to: left alone.
	if [[ ! $ref =~ ^[0-9a-f]{40}$ && ! $ref =~ ^v[0-9]+(\.[0-9]+){0,2}$ ]]; then
		notes+=("${action}@${ref} follows a branch, not a release; left as it is")
		continue
	fi
	version="$ref"
	[[ $ref =~ ^[0-9a-f]{40}$ ]] && version="$comment"
	[[ -n $version ]] || die "${action}@${ref} has no version comment: add '# vX.Y.Z' after the commit"
	# Lines at different versions count as the highest; all move together.
	if [[ -n ${action_version[$action]:-} ]]; then
		version="$(printf '%s\n%s\n' "${action_version[$action]}" "$version" | sort -V | tail -n1)"
	fi
	action_version[$action]="$version"
done < <(sed -nE 's/^[[:space:]-]*uses:[[:space:]]*([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)(\/[^@[:space:]]*)?@([^[:space:]#]+)[[:space:]]*(#[[:space:]]*([^[:space:]]+))?.*$/\1\t\3\t\5/p' "${workflows[@]}" | sort -u)

for action in $(printf '%s\n' "${!action_version[@]}" | sort); do
	current="${action_version[$action]}"
	refs="$(git ls-remote --tags "https://github.com/${action}.git")" ||
		die "cannot reach github.com for ${action}"
	newest="$(awk '{ sub("refs/tags/", "", $2); sub(/\^\{\}$/, "", $2); print $2 }' <<<"$refs" |
		grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -uV | tail -n1)" || true
	[[ -n $newest ]] || die "${action} has no vX.Y.Z release tag"
	# An annotated tag's commit is on its ^{} line; a lightweight tag has one line.
	sha="$(awk -v t="refs/tags/${newest}" '$2 == t || $2 == t "^{}" { s = $1 } END { print s }' <<<"$refs")"
	[[ $sha =~ ^[0-9a-f]{40}$ ]] || die "cannot resolve ${action} ${newest} to a commit"
	# Every line of the action as "REF VERSION": nothing to do when all of
	# them are already the newest release's commit and version.
	lines="$(sed -nE "s#^[[:space:]-]*uses:[[:space:]]*${action//./\\.}(/[^@[:space:]]*)?@([^[:space:]#]+)[[:space:]]*(\#[[:space:]]*([^[:space:]]+))?.*#\2 \4#p" "${workflows[@]}" | sort -u)"
	[[ $lines == "${sha} ${newest}" ]] && continue
	action_updates+=("${action}"$'\t'"${sha}"$'\t'"${newest}")
	if [[ ${current%%.*} != "${newest%%.*}" ]]; then
		changes+=("update ${action} from ${current} to ${newest} (major: read its release notes)")
	elif [[ $newest == "$current" ]]; then
		changes+=("pin ${action} ${newest} by commit")
	else
		changes+=("update ${action} from ${current} to ${newest}")
	fi
done

# ---- write ---------------------------------------------------------------
for note in "${notes[@]}"; do
	echo "ℹ️  ${note}"
done
if [[ ${#changes[@]} -eq 0 ]]; then
	echo "✅ Build pins are current."
	exit 0
fi

# A copy of each file before the change, for showing only this run's edits.
before="$(mktemp -d)"
trap 'rm -rf "$before"' EXIT
files=("$pins" "${workflows[@]}")
for file in "${files[@]}"; do
	mkdir -p "${before}/$(dirname "${file#"$repo"/}")"
	cp "$file" "${before}/${file#"$repo"/}"
done

for update in "${pin_updates[@]}"; do
	key="${update%%=*}"
	value="${update#*=}"
	sed -i "s|^${key}=.*|${key}=${value}|" "$pins"
done
for update in "${action_updates[@]}"; do
	IFS=$'\t' read -r action sha version <<<"$update"
	sed -Ei "s|^([[:space:]-]*uses:[[:space:]]*${action//./\\.})(/[^@[:space:]]*)?@[^[:space:]#]+.*$|\1\2@${sha} # ${version}|" "${workflows[@]}"
done

echo ""
for file in "${files[@]}"; do
	rel="${file#"$repo"/}"
	diff -u --label "a/${rel}" --label "b/${rel}" "${before}/${rel}" "$file" || true
done
echo ""
echo "✅ Build pins moved. Run make setup and make test-quiet before committing;"
echo "   the release workflow first uses the new images and actions at the next release."
echo ""
echo "📝 Suggested commit message:"
echo ""
count="${#changes[@]}"
single="chore(build): $(sed -E 's/ from [^ ]+ to / to /; s/ \(major: .*\)$//' <<<"${changes[0]}")"
if [[ $count -eq 1 && ${#single} -le 72 ]]; then
	echo "$single"
else
	echo "chore(build): update ${count} build pins"
	echo ""
	printf -- '- %s\n' "${changes[@]}"
fi
