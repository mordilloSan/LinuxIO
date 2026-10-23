# shellcheck shell=bash
# Helpers shared by scripts/*.sh. Source this file; it sets no shell options.
# The Makefile exports COLOR_*; standalone runs fall back to plain text.

COLOR_RESET="${COLOR_RESET:-}"
COLOR_BLUE="${COLOR_BLUE:-}"
COLOR_GREEN="${COLOR_GREEN:-}"
COLOR_YELLOW="${COLOR_YELLOW:-}"
COLOR_CYAN="${COLOR_CYAN:-}"
COLOR_RED="${COLOR_RED:-}"

printc() { printf '%b\n' "$*"; }

uptime_now() { awk '{ print $1 }' /proc/uptime; }

# elapsed_since START_UPTIME: seconds since START_UPTIME, one decimal. The
# monotonic uptime clock cannot jump when the host synchronizes its wall clock.
elapsed_since() { awk -v start="$1" -v end="$(uptime_now)" 'BEGIN { printf "%.1f", end - start }'; }

# checkmarks: colorize the check and cross marks printed by the C test binaries.
checkmarks() { sed 's/✓/\x1b[1;32m✓\x1b[0m/g;s/✗/\x1b[1;31m✗\x1b[0m/g'; }

RUN_GROUP_PID=""
_stop_group() {
	local signal="$1" rc="$2"
	trap - EXIT HUP INT TERM
	if [ -n "$RUN_GROUP_PID" ]; then
		kill -s "$signal" -- "-$RUN_GROUP_PID" 2>/dev/null || true
		wait "$RUN_GROUP_PID" 2>/dev/null || true
		kill -s KILL -- "-$RUN_GROUP_PID" 2>/dev/null || true
	fi
	exit "$rc"
}

# run_grouped SETSID_CMD CMD...: run CMD in its own session and, when this shell
# is hung up, interrupted, terminated or exits, stop the whole process group.
# Returns CMD's status.
run_grouped() {
	local setsid_cmd="$1" rc=0
	shift
	trap '_stop_group TERM 129' HUP
	trap '_stop_group TERM 130' INT
	trap '_stop_group TERM 143' TERM
	trap '_stop_group TERM $?' EXIT
	"$setsid_cmd" "$@" &
	RUN_GROUP_PID=$!
	wait "$RUN_GROUP_PID" || rc=$?
	trap - EXIT HUP INT TERM
	return "$rc"
}
