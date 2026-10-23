#!/usr/bin/env bash
# Run a Make target with bounded output: the complete output goes to a log and
# only a compact summary is printed. The target itself is unchanged.
# Usage: quiet.sh TARGET LOG_DIR FAILURE_LINES SETSID_CMD   (MAKE from the environment)
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

target="$1" log_dir="$2" failure_lines="$3" setsid_cmd="$4"
make_cmd="${MAKE:-make}"
case "$failure_lines" in
'' | *[!0-9]*)
	echo "quiet_failure_lines must be a non-negative integer" >&2
	exit 2
	;;
esac

mkdir -p "$log_dir"
log="$log_dir/$target-$(date -u +%Y%m%dT%H%M%S)-$PPID-$RANDOM.log"
start_seconds="$(uptime_now)"

if [ "$target" = "test-updater" ]; then
	# Stays in the foreground so sudo retains a controlling terminal.
	if "$make_cmd" --no-print-directory "$target" >"$log" 2>&1; then rc=0; else rc=$?; fi
elif run_grouped "$setsid_cmd" "$make_cmd" --no-print-directory "$target" >"$log" 2>&1; then
	rc=0
else
	rc=$?
fi

elapsed="$(elapsed_since "$start_seconds")"
summary_re='Found[[:space:]]+[0-9]+[[:space:]]+warnings?[[:space:]]+and[[:space:]]+[0-9]+[[:space:]]+errors?'
if [ "$rc" -eq 0 ]; then
	printf '✓ %-32s %ss\n' "$target" "$elapsed"
	summaries="$(grep -E "$summary_re" "$log" | tail -n 5 || true)"
	if [ -n "$summaries" ]; then
		printf '%s\n' "$summaries"
	fi
	warnings="$(grep -E '⚠️|(^|[[:space:]])warning:' "$log" | grep -Eiv "$summary_re" | tail -n 10 || true)"
	if [ -n "$warnings" ]; then
		printf '  warnings:\n%s\n' "$warnings"
	fi
else
	printf '✗ %-32s %ss\n' "$target" "$elapsed" >&2
	if [ "$failure_lines" -gt 0 ]; then
		printf '  last %s lines:\n' "$failure_lines" >&2
		tail -n "$failure_lines" "$log" >&2
	fi
	printf '  full log: %s\n' "$log" >&2
	exit "$rc"
fi
