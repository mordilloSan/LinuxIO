#!/usr/bin/env bash
# Oxlint + Oxfmt for the frontend.
#   fix:   auto-fix in place (local workflow); records the warning count in
#          $FRONTEND_LINT_WARNINGS_FILE when set.
#   check: read-only (CI).
# Usage: lint-frontend.sh fix|check FRONTEND_DIR
set -uo pipefail

mode="$1"
cd "$2" || exit 1
oxlint_config=(-c config/.oxlintrc.json)
oxlint_paths=(src config scripts/compiler-coverage.mjs scripts/run-browser-fixture.mjs)
oxfmt_globs=("src/**/*.js" "src/**/*.jsx" "src/**/*.ts" "src/**/*.tsx" "src/test/browser/**/*.html"
	"!src/routeTree.gen.ts" "config/**/*.ts"
	scripts/compiler-coverage.mjs scripts/run-browser-fixture.mjs)
oxfmt=(./node_modules/.bin/oxfmt -c config/.oxfmtrc.json)

if [ "$mode" = "check" ]; then
	set -e
	echo "🔎 Running read-only frontend lint and formatting checks..."
	./node_modules/.bin/oxlint --type-aware "${oxlint_config[@]}" "${oxlint_paths[@]}"
	"${oxfmt[@]}" --check --no-error-on-unmatched-pattern "${oxfmt_globs[@]}"
	echo "✅ Frontend linting and formatting checks passed!"
	exit 0
fi

lint_output="$(mktemp)"
format_output="$(mktemp)"
trap 'rm -f "$lint_output" "$format_output"' EXIT

./node_modules/.bin/oxlint --type-aware --fix "${oxlint_config[@]}" "${oxlint_paths[@]}" >"$lint_output" 2>&1
status=$?
warning_count="$(awk '/^Found [0-9]+ warning/ { count = $2; found = 1 } /: warning / || /^[[:space:]]*⚠ / { fallback++ } END { print found ? count : fallback + 0 }' "$lint_output")"
[ "$status" -eq 0 ] || {
	printf "\033[1;31m   ✗\033[0m Oxlint failed\n"
	sed "s/^/      /" "$lint_output"
	exit "$status"
}
if ! grep -Eq 'Found[[:space:]]+[0-9]+[[:space:]]+warnings?[[:space:]]+and[[:space:]]+[0-9]+[[:space:]]+errors?' "$lint_output"; then
	printf 'Found %s warnings and 0 errors.\n' "$warning_count" >>"$lint_output"
fi
printf "\033[1;32m   ✓\033[0m Oxlint\n"
sed "s/^/      /" "$lint_output"
if [ "$warning_count" -gt 0 ]; then
	printf "\033[1;33m   ⚠\033[0m Oxlint completed with %s non-blocking warning(s).\n" "$warning_count"
	if [ -n "${FRONTEND_LINT_WARNINGS_FILE:-}" ]; then printf "%s\n" "$warning_count" >"$FRONTEND_LINT_WARNINGS_FILE"; fi
fi

"${oxfmt[@]}" --no-error-on-unmatched-pattern "${oxfmt_globs[@]}" >"$format_output" 2>&1
status=$?
[ "$status" -eq 0 ] || {
	printf "\033[1;31m   ✗\033[0m Oxfmt failed\n"
	sed "s/^/      /" "$format_output"
	exit "$status"
}
printf "\033[1;32m   ✓\033[0m Oxfmt\n"
sed "s/^/      /" "$format_output"
printf "\033[1;32m%s\033[0m\n" "✅ Frontend linting and formatting passed!"
