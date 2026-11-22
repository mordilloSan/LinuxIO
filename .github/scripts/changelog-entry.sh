#!/usr/bin/env bash
# Generate a changelog entry for a version
# Usage: changelog-entry.sh <version> <prev_tag> <commit_range> <repo>
# Output: Markdown changelog body (categories + contributors)
#
# An author is written as @login only when GitHub names the account behind one
# of their commits (needs gh and GH_TOKEN, and the commits pushed); otherwise as
# the plain Git author name. A Git author name is not a login: "@Miguel Mariz"
# mentions whoever owns the account "Miguel".

set -euo pipefail

VERSION="$1"
PREV_TAG="${2:-}"
COMMIT_RANGE="$3"
REPO="$4"

# Categorize commits
FEATURES="" FIXES="" DOCS="" STYLE="" REFACTOR="" PERF=""
TEST="" BUILD="" CI="" CHORE="" OTHER=""

# NUL-delimited fields preserve pipes and backslashes without interpreting
# commit text as shell or printf syntax. `%s` is Git's single-line subject.
LOG_ARGS=(--reverse --no-merges --pretty=format:'%s%x00%H%x00%an%x00%ae%x00')
if [ -n "$COMMIT_RANGE" ]; then
	LOG_ARGS+=("$COMMIT_RANGE")
fi

# One answer per author email (per name where a commit has no email), kept from
# the first lookup GitHub answers. A lookup that fails (network, rate limit) is
# tried again on the author's next commits, three times at most, so one failure
# does not write the author's name for the whole run. The keys are never empty,
# which bash refuses as a subscript, and are never used in arithmetic, which
# would expand an author's text.
declare -A MENTIONS=() FAILED=() AUTHORS=() AUTHOR_MENTIONS=()

mention_for() {
	local hash=$1 name=$2 email=$3 login=""
	local key="${email:-name:$name}"
	if [[ -z ${MENTIONS[$key]+set} ]]; then
		if login="$(gh api "repos/$REPO/commits/$hash" --jq '.author.login // ""' 2>/dev/null)"; then
			if [[ $login =~ ^[A-Za-z0-9-]+$ ]]; then
				MENTIONS[$key]="@$login"
			else
				MENTIONS[$key]=""
			fi
		else
			FAILED[$key]="${FAILED[$key]:-}x"
			[[ ${FAILED[$key]} != xxx ]] || MENTIONS[$key]=""
		fi
	fi
	MENTION="${MENTIONS[$key]:-$name}"
	# For the Contributors list: an author seen once with a login is listed by
	# it, not a second time by name for a commit made with another email.
	AUTHORS["name:$name"]=1
	[[ $MENTION != @* ]] || AUTHOR_MENTIONS["name:$name"]="$MENTION"
}

while IFS= read -r -d '' message &&
	IFS= read -r -d '' hash &&
	IFS= read -r -d '' author &&
	IFS= read -r -d '' email; do
	# `format:` separates commits with one newline; the fields themselves are
	# NUL-delimited, so discard only that known record separator.
	message="${message#$'\n'}"
	[ -z "$message" ] && continue
	[[ "$author" == "github-actions[bot]" ]] && continue
	[[ "$message" =~ ^[Cc]hangelog$ ]] && continue

	mention_for "$hash" "$author" "$email"
	ENTRY="* $message ([${hash:0:7}](https://github.com/$REPO/commit/$hash)) by $MENTION"

	if [[ "$message" =~ ^feat(\(.*\))?: ]]; then
		FEATURES="$FEATURES$ENTRY"$'\n'
	elif [[ "$message" =~ ^fix(\(.*\))?: ]]; then
		FIXES="$FIXES$ENTRY"$'\n'
	elif [[ "$message" =~ ^docs(\(.*\))?: ]]; then
		DOCS="$DOCS$ENTRY"$'\n'
	elif [[ "$message" =~ ^style(\(.*\))?: ]]; then
		STYLE="$STYLE$ENTRY"$'\n'
	elif [[ "$message" =~ ^refactor(\(.*\))?: ]]; then
		REFACTOR="$REFACTOR$ENTRY"$'\n'
	elif [[ "$message" =~ ^perf(\(.*\))?: ]]; then
		PERF="$PERF$ENTRY"$'\n'
	elif [[ "$message" =~ ^test(\(.*\))?: ]]; then
		TEST="$TEST$ENTRY"$'\n'
	elif [[ "$message" =~ ^build(\(.*\))?: ]]; then
		BUILD="$BUILD$ENTRY"$'\n'
	elif [[ "$message" =~ ^ci(\(.*\))?: ]]; then
		CI="$CI$ENTRY"$'\n'
	elif [[ "$message" =~ ^chore(\(.*\))?: ]]; then
		CHORE="$CHORE$ENTRY"$'\n'
	else
		OTHER="$OTHER$ENTRY"$'\n'
	fi
done < <(git log "${LOG_ARGS[@]}")

# Output sections
[ -n "$FEATURES" ] && printf "###  Features\n\n%s\n" "$FEATURES"
[ -n "$FIXES" ] && printf "###  Bug Fixes\n\n%s\n" "$FIXES"
[ -n "$PERF" ] && printf "###  Performance\n\n%s\n" "$PERF"
[ -n "$REFACTOR" ] && printf "###  Refactoring\n\n%s\n" "$REFACTOR"
[ -n "$DOCS" ] && printf "###  Documentation\n\n%s\n" "$DOCS"
[ -n "$STYLE" ] && printf "###  Style\n\n%s\n" "$STYLE"
[ -n "$TEST" ] && printf "###  Tests\n\n%s\n" "$TEST"
[ -n "$BUILD" ] && printf "###  Build\n\n%s\n" "$BUILD"
[ -n "$CI" ] && printf "###  CI/CD\n\n%s\n" "$CI"
[ -n "$CHORE" ] && printf "###  Chores\n\n%s\n" "$CHORE"
[ -n "$OTHER" ] && printf "###  Other Changes\n\n%s\n" "$OTHER"

# Contributors
printf "###  Contributors\n\n"
for author in "${!AUTHORS[@]}"; do
	printf '* %s\n' "${AUTHOR_MENTIONS[$author]:-${author#name:}}"
done | sort -u

# Full changelog link
if [ -n "$PREV_TAG" ]; then
	printf '\n\n**Full Changelog**: https://github.com/%s/compare/%s...%s\n' "$REPO" "$PREV_TAG" "$VERSION"
else
	printf '\n\n**Full Changelog**: https://github.com/%s/releases/tag/%s\n' "$REPO" "$VERSION"
fi
