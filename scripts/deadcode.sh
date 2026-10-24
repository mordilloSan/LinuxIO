#!/usr/bin/env bash
# Scan the backend for dead code. Informational: never fails the build.
#
# Scan with tests for wholly unreachable code, then without tests to surface
# production APIs kept alive only by tests. Intentional cross-package test
# helpers are excluded from the production scan only; the test scan still
# checks them. Successful results are content-addressed under CACHE_DIR/deadcode.
#
# Usage: deadcode.sh BACKEND_DIR CACHE_DIR DEADCODE_BIN GO_BIN
# Environment: DEADCODE_CACHE (1|0), DEADCODE_PARALLEL (auto|0|1), SHA256_CMD,
# plus the Go environment (PATH, GOTOOLCHAIN, GOOS, GOARCH) from the Makefile.
# shellcheck source-path=SCRIPTDIR source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

backend_dir="$1" cache_root="$2/deadcode" deadcode_bin="$3" go_bin="$4"
cache_enabled="${DEADCODE_CACHE:-1}" parallel_mode="${DEADCODE_PARALLEL:-auto}"
read -ra sha256 <<<"${SHA256_CMD:-sha256sum}"
self="$(realpath "${BASH_SOURCE[0]}")"

cd "$backend_dir" || exit 1
scan_dir="$(mktemp -d)"
trap 'rm -rf "$scan_dir"' EXIT
cache_hit=0

if [ "$cache_enabled" = "1" ]; then
	mkdir -p "$cache_root"
	exec 9>"$cache_root/lock"
	flock 9
	# The script itself is part of the key: it holds the scan arguments and filters.
	cache_key="$(
		{
			printf '%s\n' 'linuxio-deadcode-cache-v1' '-test ./...' './...' \
				'exclude cross-package test helpers'
			"${sha256[@]}" <"$self"
			"${sha256[@]}" <"$deadcode_bin"
			"${sha256[@]}" <"$go_bin"
			"$go_bin" version
			"$go_bin" env -json \
				GOOS GOARCH GOAMD64 GOARM GOARM64 GO386 GOMIPS GOMIPS64 GOWASM \
				CGO_ENABLED CGO_CFLAGS CGO_CPPFLAGS CGO_CXXFLAGS CGO_FFLAGS CGO_LDFLAGS \
				CC CXX GCCGO PKG_CONFIG GOEXPERIMENT GOFIPS140 GOFLAGS GOTOOLCHAIN \
				GO_EXTLINK_ENABLED GOWORK GOPACKAGESDRIVER GOPROXY GONOPROXY GOPRIVATE GONOSUMDB
			find . -type f -print0 | sort -z | xargs -0 "${sha256[@]}"
		} | "${sha256[@]}" | awk '{ print $1 }'
	)"
	cache_entry="$cache_root/$cache_key"
	if [ -f "$cache_entry/test.out" ] && [ -f "$cache_entry/production.out" ]; then
		cp "$cache_entry/test.out" "$scan_dir/test.out"
		cp "$cache_entry/production.out" "$scan_dir/production.out"
		printf '0\n' >"$scan_dir/test.status"
		printf '0\n' >"$scan_dir/production.status"
		cache_hit=1
		echo "   Reusing unchanged dead-code results..."
	fi
fi

run_scan() {
	local name="$1"
	shift
	"$deadcode_bin" "$@" >"$scan_dir/$name.out" 2>&1
	printf '%s\n' "$?" >"$scan_dir/$name.status"
}

if [ "$cache_hit" -eq 0 ]; then
	# Two deadcode processes need about 3 GiB combined; auto keeps small hosts serial.
	cores="$(nproc)"
	available_kb="$(awk '/^MemAvailable:/ { print $2; exit }' /proc/meminfo 2>/dev/null || true)"
	parallel=0
	if [ "$parallel_mode" = "1" ] ||
		{ [ "$parallel_mode" = "auto" ] && [ "$cores" -ge 8 ] && [ "${available_kb:-0}" -ge 6291456 ]; }; then
		parallel=1
	fi
	if [ "$parallel" -eq 1 ]; then
		echo "   Running test and production scans concurrently..."
		run_scan test -test ./... &
		test_pid=$!
		run_scan production ./... &
		production_pid=$!
		wait "$test_pid"
		wait "$production_pid"
	else
		run_scan test -test ./...
		run_scan production ./...
	fi
	if [ "$cache_enabled" = "1" ] &&
		[ "$(cat "$scan_dir/test.status")" -eq 0 ] &&
		[ "$(cat "$scan_dir/production.status")" -eq 0 ]; then
		publish_dir="$scan_dir/cache-entry"
		mkdir "$publish_dir"
		cp "$scan_dir/test.out" "$publish_dir/test.out"
		cp "$scan_dir/production.out" "$publish_dir/production.out"
		mv -T "$publish_dir" "$cache_entry"
	fi
fi

test_out="$(cat "$scan_dir/test.out")"
test_status="$(cat "$scan_dir/test.status")"
production_out="$(cat "$scan_dir/production.out")"
production_status="$(cat "$scan_dir/production.status")"
if [ "$production_status" -eq 0 ]; then
	production_out="$(printf '%s\n' "$production_out" | grep -Ev \
		-e '^common/dbusclient/testdbus/' \
		-e '^common/logguard/' \
		-e '^common/peercred/peercred\.go:[0-9]+:[0-9]+: unreachable func: WithCredForTest$' \
		-e '^bridge/internal/acpagents/acpagents\.go:[0-9]+:[0-9]+: unreachable func: Override$' \
		-e '^common/dockerupdate/journal\.go:[0-9]+:[0-9]+: unreachable func: NewJournal$' \
		-e '^monitoring/internal/store/storetest/sample\.go:[0-9]+:[0-9]+: unreachable func: SampleCombinedData$' || true)"
fi
if [ "$test_status" -ne 0 ] || [ "$production_status" -ne 0 ]; then
	printc "${COLOR_YELLOW}⚠️  deadcode scan could not complete (informational, not failing):${COLOR_RESET}"
	if [ "$test_status" -ne 0 ]; then printf '%s\n' "$test_out"; fi
	if [ "$production_status" -ne 0 ]; then printf '%s\n' "$production_out"; fi
elif [ -n "$test_out" ]; then
	printc "${COLOR_YELLOW}⚠️  deadcode found unreachable functions (informational, not failing):${COLOR_RESET}"
	printf '%s\n' "$test_out"
elif [ -n "$production_out" ]; then
	printc "${COLOR_YELLOW}⚠️  deadcode found functions reachable only from tests (informational, not failing):${COLOR_RESET}"
	printf '%s\n' "$production_out"
else
	printc "${COLOR_GREEN}✅ No dead code found!${COLOR_RESET}"
fi
