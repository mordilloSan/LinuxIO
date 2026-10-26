#!/usr/bin/env bash
# Validation lane scheduler behind `make test`, `make check-frontend` and
# `make check-backend`. Each lane step is a Make target run through $MAKE.
# Usage: lanes.sh test TIMER_START_UPTIME | frontend | backend
#
# `test` runs two independent lanes, serial inside each lane:
#
#   frontend: lint-only -> tsc-only + test-frontend-only
#   backend:  golint-only -> test-auth-protocol + test-go -> deadcode-only
#             test-auth + test-auth-pam (C only, no Go sources) alongside golint
#
# The lane heads must come first because they rewrite sources in place
# (oxlint --fix / oxfmt; golangci-lint fmt / modernize -fix / go mod tidy), so
# nothing may read those trees until they finish. The rest is scheduling, not
# dependency: every job here already saturates ~2 cores on its own, so running
# four of them at once on a 4-core box costs more in contention than it buys in
# overlap. Two lanes keeps demand near the core count and puts the long pole
# (frontend unit tests, ~58% of the suite's total CPU) in flight early, with the
# cheaper backend lane filling the other half of the machine underneath it.
# tsc-only and the C auth tests are single-threaded and build in temp dirs, so
# they ride alongside their lane instead of extending it (~5s off a warm run).
#
# Serializing test-go before deadcode-only also stops them from compiling
# the same 58 packages simultaneously and fighting over the Go build cache.
#
# The frontend lane is normally the critical path. Deprioritising the backend
# lane with nice(1) was measured and made no material difference.
#
# Execution order is fixed by the lane chains; the follow() order below is only
# how output is replayed, and does not constrain what runs when.
#
# `backend` is fully serial: test-backend and deadcode-only both compile the
# whole backend, so overlapping them duplicates the work and contends on the Go
# build cache.
set -uo pipefail
# shellcheck source-path=SCRIPTDIR source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

mode="$1"
make_cmd="${MAKE:-make}"
st=0
pids=""
FRONTEND_LINT_WARNINGS_FILE="$(mktemp)"
jobs_dir="$(mktemp -d)"
export FRONTEND_LINT_WARNINGS_FILE

cleanup() {
	local rc=$? pid
	trap - EXIT INT TERM
	for pid in $pids; do kill "$pid" 2>/dev/null || true; done
	for pid in $pids; do wait "$pid" 2>/dev/null || true; done
	rm -f "$FRONTEND_LINT_WARNINGS_FILE"
	rm -rf "$jobs_dir"
	exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mk() { "$make_cmd" --no-print-directory "$@"; }

# follow FILE PID: replay FILE as PID writes it, then return PID's status.
follow() {
	tail -n +1 -f -s 0.1 --pid="$2" "$1"
	wait "$2"
}

# note_lint_warnings TEMPLATE: print TEMPLATE (@N@ = count) if lint warned.
note_lint_warnings() {
	[ -s "$FRONTEND_LINT_WARNINGS_FILE" ] || return 0
	local count
	count="$(tail -n 1 "$FRONTEND_LINT_WARNINGS_FILE")"
	printc "\n${COLOR_YELLOW}⚠️  ${1//@N@/$count}${COLOR_RESET}"
	printc "${COLOR_YELLOW}   Warnings are non-blocking; review the Oxlint output above or run 'make lint'.${COLOR_RESET}"
}

# await STATUS_FILE PID: wait for STATUS_FILE; fail if PID died without it.
await() {
	while [ ! -f "$1" ]; do
		kill -0 "$2" 2>/dev/null || return 1
		sleep 0.1
	done
}

# stage NAME CMD...: run CMD with its output captured in the jobs dir and its
# exit status recorded in NAME.rc.
stage() {
	local name="$1" command_pid rc
	shift
	"$@" >"$jobs_dir/$name" 2>&1 &
	command_pid=$!
	trap 'kill "$command_pid" 2>/dev/null || true; wait "$command_pid" 2>/dev/null || true; exit 130' INT
	trap 'kill "$command_pid" 2>/dev/null || true; wait "$command_pid" 2>/dev/null || true; exit 143' TERM
	wait "$command_pid"
	rc=$?
	trap - INT TERM
	printf '%s\n' "$rc" >"$jobs_dir/$name.rc"
	return "$rc"
}

lane_test() {
	local timer_start="$1" elapsed
	touch "$jobs_dir"/{lint,golint,cauth,tsc,fe,be,dead}
	stage lint mk lint-only &
	PID_LINT=$!
	stage golint mk golint-only &
	PID_GOLINT=$!
	stage cauth mk test-auth test-auth-pam &
	PID_CAUTH=$!
	(
		await "$jobs_dir/lint.rc" "$PID_LINT" || exit $?
		stage tsc mk tsc-only
	) &
	PID_TSC=$!
	(
		await "$jobs_dir/lint.rc" "$PID_LINT" || exit $?
		stage fe mk test-frontend-only
	) &
	PID_FE=$!
	(
		await "$jobs_dir/golint.rc" "$PID_GOLINT" || exit $?
		stage be mk test-auth-protocol test-go SKIP_ENSURE_GO=1
	) &
	PID_BE=$!
	(
		await "$jobs_dir/be.rc" "$PID_BE" || exit $?
		await "$jobs_dir/cauth.rc" "$PID_CAUTH" || exit $?
		stage dead mk deadcode-only
	) &
	PID_DEAD=$!
	pids="$PID_LINT $PID_GOLINT $PID_CAUTH $PID_TSC $PID_FE $PID_BE $PID_DEAD"

	follow "$jobs_dir/lint" "$PID_LINT" || st=1
	follow "$jobs_dir/golint" "$PID_GOLINT" || st=1
	printc ""
	follow "$jobs_dir/cauth" "$PID_CAUTH" || st=1
	printc ""
	follow "$jobs_dir/tsc" "$PID_TSC" || st=1
	printc ""
	follow "$jobs_dir/be" "$PID_BE" || st=1
	printc ""
	follow "$jobs_dir/dead" "$PID_DEAD" || true
	printc ""
	follow "$jobs_dir/fe" "$PID_FE" || st=1
	note_lint_warnings "All checks completed with @N@ frontend lint warning(s)."
	elapsed="$(elapsed_since "$timer_start")"
	if [ "$st" -ne 0 ]; then
		printc "\n${COLOR_RED}❌ Some checks failed.${COLOR_RESET} ${COLOR_CYAN}(⏱️  ${elapsed}s)${COLOR_RESET}"
		exit 1
	fi
	printc "\n${COLOR_GREEN}✅ All checks passed!${COLOR_RESET} ${COLOR_CYAN}(⏱️  ${elapsed}s)${COLOR_RESET}"
}

lane_frontend() {
	mk lint-only || st=1
	printc ""
	touch "$jobs_dir"/{tsc,fe}
	mk tsc-only >"$jobs_dir/tsc" 2>&1 &
	PID_TSC=$!
	mk test-frontend-only >"$jobs_dir/fe" 2>&1 &
	PID_FE=$!
	pids="$PID_TSC $PID_FE"
	follow "$jobs_dir/tsc" "$PID_TSC" || st=1
	printc ""
	follow "$jobs_dir/fe" "$PID_FE" || st=1
	note_lint_warnings "Frontend checks completed with @N@ lint warning(s)."
	if [ "$st" -ne 0 ]; then
		printc "\n${COLOR_RED}❌ Frontend checks failed.${COLOR_RESET}"
		exit 1
	fi
	printc "\n${COLOR_GREEN}✅ Frontend checks passed!${COLOR_RESET}"
}

lane_backend() {
	mk golint-only || st=1
	printc ""
	mk test-backend SKIP_ENSURE_GO=1 || st=1
	printc ""
	mk deadcode-only || true
	if [ "$st" -ne 0 ]; then
		printc "\n${COLOR_RED}❌ Backend checks failed.${COLOR_RESET}"
		exit 1
	fi
	printc "\n${COLOR_GREEN}✅ Backend checks passed!${COLOR_RESET}"
}

case "$mode" in
test) lane_test "${2:?TIMER_START_UPTIME required}" ;;
frontend) lane_frontend ;;
backend) lane_backend ;;
*)
	echo "usage: lanes.sh test TIMER_START_UPTIME | frontend | backend" >&2
	exit 2
	;;
esac
