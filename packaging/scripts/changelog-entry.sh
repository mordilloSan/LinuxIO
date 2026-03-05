#!/bin/bash
# Generate a changelog entry for a version
# Usage: changelog-entry.sh <version> <prev_tag> <commit_range> <repo>
# Output: Markdown changelog body (categories + contributors)

set -euo pipefail

VERSION="$1"
PREV_TAG="${2:-}"
COMMIT_RANGE="$3"
REPO="$4"

# Get commits
if [ -n "$COMMIT_RANGE" ]; then
  COMMITS="$(git log $COMMIT_RANGE --pretty=format:'%s|%h|%an' --reverse)"
else
  COMMITS="$(git log --pretty=format:'%s|%h|%an' --reverse)"
fi

# Categorize commits
FEATURES="" FIXES="" DOCS="" STYLE="" REFACTOR="" PERF=""
TEST="" BUILD="" CI="" CHORE="" OTHER=""

while IFS='|' read -r message hash author; do
  [ -z "$message" ] && continue
  [[ "$author" == "github-actions[bot]" ]] && continue
  [[ "$message" =~ ^[Cc]hangelog$ ]] && continue

  ENTRY="* $message ([${hash:0:7}](https://github.com/$REPO/commit/$hash)) by @$author"

  if [[ "$message" =~ ^feat(\(.*\))?: ]]; then FEATURES="$FEATURES$ENTRY"$'\n'
  elif [[ "$message" =~ ^fix(\(.*\))?: ]]; then FIXES="$FIXES$ENTRY"$'\n'
  elif [[ "$message" =~ ^docs(\(.*\))?: ]]; then DOCS="$DOCS$ENTRY"$'\n'
  elif [[ "$message" =~ ^style(\(.*\))?: ]]; then STYLE="$STYLE$ENTRY"$'\n'
  elif [[ "$message" =~ ^refactor(\(.*\))?: ]]; then REFACTOR="$REFACTOR$ENTRY"$'\n'
  elif [[ "$message" =~ ^perf(\(.*\))?: ]]; then PERF="$PERF$ENTRY"$'\n'
  elif [[ "$message" =~ ^test(\(.*\))?: ]]; then TEST="$TEST$ENTRY"$'\n'
  elif [[ "$message" =~ ^build(\(.*\))?: ]]; then BUILD="$BUILD$ENTRY"$'\n'
  elif [[ "$message" =~ ^ci(\(.*\))?: ]]; then CI="$CI$ENTRY"$'\n'
  elif [[ "$message" =~ ^chore(\(.*\))?: ]]; then CHORE="$CHORE$ENTRY"$'\n'
  else OTHER="$OTHER$ENTRY"$'\n'
  fi
done <<< "$COMMITS"

# Output sections
[ -n "$FEATURES" ] && printf "###  Features\n\n%b\n" "$FEATURES"
[ -n "$FIXES" ] && printf "###  Bug Fixes\n\n%b\n" "$FIXES"
[ -n "$PERF" ] && printf "###  Performance\n\n%b\n" "$PERF"
[ -n "$REFACTOR" ] && printf "###  Refactoring\n\n%b\n" "$REFACTOR"
[ -n "$DOCS" ] && printf "###  Documentation\n\n%b\n" "$DOCS"
[ -n "$STYLE" ] && printf "###  Style\n\n%b\n" "$STYLE"
[ -n "$TEST" ] && printf "###  Tests\n\n%b\n" "$TEST"
[ -n "$BUILD" ] && printf "###  Build\n\n%b\n" "$BUILD"
[ -n "$CI" ] && printf "###  CI/CD\n\n%b\n" "$CI"
[ -n "$CHORE" ] && printf "###  Chores\n\n%b\n" "$CHORE"
[ -n "$OTHER" ] && printf "###  Other Changes\n\n%b\n" "$OTHER"

# Contributors
printf "###  Contributors\n\n"
if [ -n "$COMMIT_RANGE" ]; then
  git log $COMMIT_RANGE --pretty=format:'* @%an' | sort -u
else
  git log --pretty=format:'* @%an' | sort -u
fi

# Full changelog link
if [ -n "$PREV_TAG" ]; then
  printf "\n\n**Full Changelog**: https://github.com/$REPO/compare/$PREV_TAG...$VERSION\n"
else
  printf "\n\n**Full Changelog**: https://github.com/$REPO/releases/tag/$VERSION\n"
fi
