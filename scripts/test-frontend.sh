#!/usr/bin/env bash
# Run the frontend unit tests (Vitest) in their own process group so an
# interrupted `make` terminates every worker.
# Usage: test-frontend.sh FRONTEND_DIR SETSID_CMD
# Environment: VITEST_MAX_WORKERS, VITEST_FILE, VITEST_TEST_NAME
set -uo pipefail

run_vitest() {
	cd "$1" || exit 1
	local args=(run --config config/vitest.config.ts --reporter=default "--maxWorkers=${VITEST_MAX_WORKERS:-8}")
	local file="${VITEST_FILE:-}" name="${VITEST_TEST_NAME:-}"
	if [ -n "$file" ]; then
		case "$file" in
		/* | ../* | */../* | */..)
			echo "VITEST_FILE must stay within the frontend directory" >&2
			exit 2
			;;
		*.test.js | *.test.jsx | *.test.ts | *.test.tsx | *.spec.js | *.spec.jsx | *.spec.ts | *.spec.tsx) ;;
		*)
			echo "VITEST_FILE must name a test or spec file" >&2
			exit 2
			;;
		esac
		[ -f "$file" ] || {
			echo "VITEST_FILE does not exist: $file" >&2
			exit 2
		}
		args+=("$file")
	fi
	if [ -n "$name" ]; then args+=(--testNamePattern "$name"); fi
	./node_modules/.bin/vitest "${args[@]}"
	local rc=$?
	if [ "$rc" -eq 0 ]; then printf "\033[1;32m%s\033[0m\n" "✅ Frontend unit tests passed!"; fi
	exit "$rc"
}

# The supervisor re-executes this script as the grouped child.
if [ "$1" = "--run" ]; then
	run_vitest "$2"
fi

# shellcheck source-path=SCRIPTDIR source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
rc=0
run_grouped "$2" bash -o pipefail "${BASH_SOURCE[0]}" --run "$1" || rc=$?
exit "$rc"
