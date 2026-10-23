#!/usr/bin/env bash
# Install a Go tool with the managed toolchain unless a runnable copy built by
# that toolchain already exists. Run with the Makefile's GO_CMD_ENV (PATH,
# GOTOOLCHAIN, GOOS, GOARCH) in the environment.
# Usage: ensure-go-tool.sh NAME BIN MODULE VERSION GO_BIN GO_VERSION TOOLS_DIR
set -euo pipefail

name="$1" bin="$2" module="$3" version="$4" go_bin="$5" go_version="$6" tools_dir="$7"
go_version_re="${go_version//./\\.}"

# golangci-lint must be v2 and built with the managed Go release.
golangci_check() {
	local out ver major
	out="$("$bin" version 2>/dev/null || true)"
	ver="$(printf '%s' "$out" | sed -n 's/^golangci-lint has version[[:space:]]\([v0-9.]\+\).*/\1/p')"
	ver="${ver#v}"
	major="${ver%%.*}"
	[ "$major" = "2" ] || return 1
	printf '%s' "$out" | grep -Eq "built with go${go_version_re}([[:space:]]|\$)" || return 2
}

# Other tools must be runnable and built with the managed Go release.
generic_check() {
	local build_info tool_go_version
	build_info="$("$go_bin" version -m "$bin" 2>/dev/null || true)"
	tool_go_version="$(printf '%s\n' "$build_info" | sed -n '1s/.*: go\([^ ]*\).*/\1/p')"
	[ "$tool_go_version" = "$go_version" ] && "$bin" -h >/dev/null 2>&1
}

check() {
	if [ "$name" = "golangci-lint" ]; then golangci_check; else generic_check; fi
}

if [ ! -x "$bin" ] || ! check; then
	echo "📥 Installing $name $version with local Go ($go_bin)..."
	rm -f "$bin" || true
	GOBIN="$tools_dir/bin" GOFLAGS="-buildvcs=false" "$go_bin" install "$module@$version"
fi

if [ "$name" = "golangci-lint" ]; then
	"$bin" version | head -n1
	rc=0
	golangci_check || rc=$?
	[ "$rc" -ne 1 ] || {
		echo " not a v2 golangci-lint"
		exit 1
	}
	[ "$rc" -eq 0 ] || {
		echo " golangci-lint not built with Go $go_version"
		exit 1
	}
	echo "✅ golangci-lint v2 ready."
else
	"$bin" -h >/dev/null 2>&1 || {
		echo "❌ $name is installed but not runnable"
		exit 1
	}
	echo "✅ $name ready."
fi
