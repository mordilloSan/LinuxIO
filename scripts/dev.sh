#!/usr/bin/env bash
# Start the Vite dev server detached (unless already running), tail the
# LinuxIO logs, and stop a Vite this run started when it exits.
# Usage: dev.sh FRONTEND_DIR PID_FILE LOG_FILE PORT LOG_LINES
set -u

frontend_dir="$1" pid_file="$2" log_file="$3" port="$4" log_lines="$5"
started_vite=0

cleanup() {
	if [ "$started_vite" = "1" ]; then
		if [ -f "$pid_file" ]; then
			pid="$(cat "$pid_file")"
			if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
				kill "$pid" 2>/dev/null || true
			fi
			rm -f "$pid_file"
		fi
		rm -f "$log_file"
	fi
}

if [ -f "$pid_file" ] && kill -0 "$(cat "$pid_file")" 2>/dev/null; then
	echo "  Vite already running (pid $(cat "$pid_file"))"
else
	rm -f "$pid_file"
	# shellcheck disable=SC2016  # $1/$2 belong to the inner shell
	nohup bash -c 'cd "$1" && exec ./node_modules/.bin/vite --config config/vite.config.ts --configLoader native --port "$2"' _ "$frontend_dir" "$port" >"$log_file" 2>&1 &
	echo $! >"$pid_file"
	started_vite=1
fi
if [ -f "$pid_file" ]; then
	echo "✅ Vite started (pid $(cat "$pid_file"))"
	echo "   ➜  Local:   http://localhost:$port/"
	echo "   Stop with: kill $(cat "$pid_file")"
else
	echo "❌ Failed to capture Vite PID. Check $log_file for details."
fi
trap cleanup INT TERM EXIT
echo ""
echo "📋 Tailing LinuxIO logs (last $log_lines lines)..."
linuxio logs "$log_lines"
