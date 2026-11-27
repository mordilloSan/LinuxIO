#!/usr/bin/env bash
# Move the Assistant's agent pins (the Package and Version of each agent in
# backend/bridge/internal/acpagents/acpagents.go) to the version the ACP
# registry lists for that npm package, save the release notes of every
# release in between to .cache/agent-pins/release-notes.md, and print a
# suggested commit message. Every lookup is made before anything is written,
# so an unreachable registry changes nothing; missing release notes are
# named and do not stop the move. Needs curl and jq; the notes are read with
# gh when it is installed (its sign-in lifts GitHub's limit of 60 reads an hour
# without one), otherwise with curl.
# Usage: update-agent-pins.sh REPO_ROOT
set -euo pipefail

repo="$1"
agents_go="${repo}/backend/bridge/internal/acpagents/acpagents.go"
notes_dir="${repo}/.cache/agent-pins"
notes_file="${notes_dir}/release-notes.md"
acp_registry="https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"

die() {
	echo "❌ update-agent-pins: $*" >&2
	exit 1
}

for tool in curl jq; do
	command -v "$tool" >/dev/null || die "${tool} is required"
done
[[ -f $agents_go ]] || die "missing ${agents_go}"

fetch() { curl -fsSL --retry 2 --max-time 30 "$@"; }

# release_body OWNER/REPO TAG: the body of that GitHub release, empty when
# there is none or GitHub cannot be read.
release_body() {
	if command -v gh >/dev/null; then
		gh api "repos/$1/releases/tags/$2" --jq '.body // empty' 2>/dev/null || true
	else
		fetch "https://api.github.com/repos/$1/releases/tags/$2" 2>/dev/null | jq -r '.body // empty' 2>/dev/null || true
	fi
}

# newer A B: A is a later release than B.
newer() { [[ $1 != "$2" && $(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n1) == "$1" ]]; }

# Every agent as "PACKAGE<tab>VERSION", in the order the table lists them.
pins() {
	awk -F'"' '
		/^[[:space:]]*Package:[[:space:]]*"/ { pkg = $2 }
		/^[[:space:]]*Version:[[:space:]]*"/ && pkg != "" { print pkg "\t" $2; pkg = "" }
	' "$agents_go"
}

mapfile -t current_pins < <(pins)
[[ ${#current_pins[@]} -gt 0 ]] || die "no Package and Version pairs in ${agents_go}"

registry="$(fetch "$acp_registry")" || die "cannot reach the ACP registry (cdn.agentclientprotocol.com)"

changes=()
pin_updates=()
notes=()
release_notes=""

for line in "${current_pins[@]}"; do
	IFS=$'\t' read -r pkg current <<<"$line"
	entry="$(jq -c --arg p "${pkg}@" '[.agents[] | select((.distribution.npx.package // "") | startswith($p))][0] // empty' <<<"$registry")"
	[[ -n $entry ]] || die "${pkg} is not in the ACP registry"
	listed="$(jq -r '.distribution.npx.package' <<<"$entry")"
	newest="${listed#"${pkg}@"}"
	[[ $newest =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "the ACP registry lists ${listed}, not a release"
	[[ $newest == "$current" ]] && continue
	if ! newer "$newest" "$current"; then
		notes+=("the ACP registry lists ${pkg} ${newest}, older than the pin ${current}; kept")
		continue
	fi

	# Every release npm has between the pin and the newest, oldest first.
	npm_doc="$(fetch "https://registry.npmjs.org/${pkg/\//%2F}")" || die "cannot read ${pkg} from npm (registry.npmjs.org)"
	mapfile -t between < <(jq -r '.time | to_entries[] | select(.key | test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) | "\(.key)\t\(.value[0:10])"' <<<"$npm_doc" | sort -V)

	repo_url="$(jq -r '.repository // empty' <<<"$entry")"
	github=""
	[[ $repo_url =~ ^https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/?$ ]] && github="${BASH_REMATCH[1]%.git}"

	section="## ${pkg} ${current} → ${newest}"$'\n\n'
	breaking=0
	for release in "${between[@]}"; do
		IFS=$'\t' read -r version date <<<"$release"
		newer "$version" "$current" || continue
		newer "$version" "$newest" && continue
		body=""
		if [[ -n $github ]]; then
			body="$(release_body "$github" "v${version}")"
			[[ -n $body ]] || body="$(release_body "$github" "$version")"
		fi
		if [[ -z $body ]]; then
			notes+=("no release notes for ${pkg} ${version} on GitHub")
			body="(no release notes found)"
		fi
		grep -q 'BREAKING' <<<"$body" && breaking=1
		section+="### ${version} (${date})"$'\n\n'"${body}"$'\n\n'
	done
	release_notes+="$section"

	pin_updates+=("${pkg}"$'\t'"${newest}")
	if [[ $breaking == 1 ]]; then
		changes+=("update ${pkg} from ${current} to ${newest} (breaking: read its release notes)")
	else
		changes+=("update ${pkg} from ${current} to ${newest}")
	fi
done

# ---- write ---------------------------------------------------------------
for note in "${notes[@]}"; do
	echo "ℹ️  ${note}"
done

# A file that names an agent package at another version than its pin was
# checked against that version: say so, moved or not.
stale_mentions() {
	local line pkg pin name
	for line in "${current_pins[@]}"; do
		IFS=$'\t' read -r pkg pin <<<"$line"
		name="${pkg##*/}"
		grep -rEno --include='*.md' --include='*.ts' --include='*.tsx' --include='*.go' \
			"${name//./\\.}\`? [0-9]+\.[0-9]+\.[0-9]+" "${repo}/docs" "${repo}/frontend/src" "${repo}/backend" 2>/dev/null |
			awk -v pin="$pin" -v root="${repo}/" '{ v = $NF; if (v != pin) { sub(root, "", $0); print } }' || true
	done | sort -u
}

warn_stale_mentions() {
	local mentions mention
	mentions="$(stale_mentions)"
	[[ -n $mentions ]] || return 0
	echo ""
	echo "⚠️  These name an agent at another version than its pin; what they"
	echo "   describe was checked against that version:"
	while IFS= read -r mention; do
		echo "   ${mention}"
	done <<<"$mentions"
}

if [[ ${#changes[@]} -eq 0 ]]; then
	echo "✅ Agent pins are current."
	warn_stale_mentions
	exit 0
fi

before="$(mktemp)"
trap 'rm -f "$before"' EXIT
cp "$agents_go" "$before"

for update in "${pin_updates[@]}"; do
	IFS=$'\t' read -r pkg version <<<"$update"
	sed -Ei "\%Package:[[:space:]]*\"${pkg//./\\.}\"%,/Version:/ s/(Version:[[:space:]]*\")[^\"]*\"/\1${version}\"/" "$agents_go"
done
mapfile -t moved_pins < <(pins)
for update in "${pin_updates[@]}"; do
	printf '%s\n' "${moved_pins[@]}" | grep -qxF "$update" || {
		cp "$before" "$agents_go"
		die "could not rewrite the pin of ${update%%$'\t'*}; ${agents_go} is unchanged"
	}
done

mkdir -p "$notes_dir"
printf '# Agent release notes\n\n%s' "$release_notes" >"$notes_file"
current_pins=("${moved_pins[@]}")

echo ""
rel="${agents_go#"$repo"/}"
diff -u --label "a/${rel}" --label "b/${rel}" "$before" "$agents_go" || true
echo ""
echo "📄 Release notes of every release in between: ${notes_file#"$repo"/}"
warn_stale_mentions
echo ""
echo "✅ Agent pins moved. Read the release notes, run make check-backend-quiet and"
echo "   try a chat with each moved agent before committing; users reinstall the"
echo "   agent after the release that carries the new pin."
echo ""
echo "📝 Suggested commit message:"
echo ""
count="${#changes[@]}"
if [[ $count -eq 1 ]]; then
	# The subject names the package without its npm scope.
	echo "chore(assistant): $(sed -E 's/ from [^ ]+ to / to /; s/ \(breaking: .*\)$//; s/ @[^/ ]+\// /' <<<"${changes[0]}")"
	echo ""
	echo "${changes[0]^}."
else
	echo "chore(assistant): update ${count} agent pins"
	echo ""
	printf -- '- %s\n' "${changes[@]}"
fi
