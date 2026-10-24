#!/usr/bin/env bash
# Install the requested Go release into a versioned user-local directory and
# point the `current` symlink at it. Run as `bash -l` like the Makefile does.
# Usage: ensure-go.sh GO_VERSION GO_DIR GO_CURRENT TOOLS_DIR GOOS [GOARCH]
set -euo pipefail

desired="$1" go_dir="$2" go_current="$3" tools_dir="$4" target_goos="$5" target_goarch="${6:-}"

if [ -z "$target_goarch" ]; then
	case "$(uname -m)" in
	aarch64 | arm64) target_goarch=arm64 ;;
	*) target_goarch=amd64 ;;
	esac
fi
tarball="go${desired}.${target_goos}-${target_goarch}.tar.gz"
url="https://go.dev/dl/${tarball}"

read_go_version() {
	local root="$1"
	if [ -f "$root/VERSION" ]; then
		sed -n "1s/^go//p" "$root/VERSION" 2>/dev/null
	elif [ -x "$root/bin/go" ]; then
		"$root/bin/go" version 2>/dev/null | awk '{print $3}' | sed "s/^go//"
	fi
}

cur=""
if [ -x "$go_dir/bin/go" ]; then
	cur="$(read_go_version "$go_dir")"
fi
if [ "$cur" = "$desired" ]; then
	echo "✅ Go $cur already available at $go_dir"
else
	echo "📥 Downloading $url"
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT
	curl -fsSL "$url" -o "$tmp/$tarball"
	mkdir -p "$(dirname "$go_dir")"
	rm -rf "$go_dir"
	tar -C "$tmp" -xzf "$tmp/$tarball"
	mv "$tmp/go" "$go_dir"
	echo "✅ Installed Go $desired to $go_dir"
fi
mkdir -p "$(dirname "$go_current")" "$tools_dir/bin"
ln -sfnT "$go_dir" "$go_current"
if ! grep -Fq "$go_current/bin" "$HOME/.bashrc" 2>/dev/null; then
	# shellcheck disable=SC2016  # $PATH must stay literal in .bashrc
	echo "export PATH=$go_current/bin:$tools_dir/bin:\$PATH" >>"$HOME/.bashrc"
fi
final_version="$("$go_current/bin/go" version 2>/dev/null || true)"
if ! printf "%s\n" "$final_version" | grep -Fq "go$desired "; then
	echo "❌ Expected Go $desired, got: ${final_version:-not found}"
	exit 1
fi
echo "✅ Go is ready!"
