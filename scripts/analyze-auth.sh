#!/usr/bin/env bash
# Static analysis of the C authentication helper: cppcheck, the compiler's
# -fanalyzer, the clang static analyzer (optional) and clang-tidy.
# Usage: analyze-auth.sh AUTH_SOURCE CACHE_DIR
# Environment: CC, MAKE, SB_WARNFLAGS (warning flags for the scan-build build).
set -euo pipefail

file="$1" cache_dir="$2"
cc="${CC:-cc}"
make_cmd="${MAKE:-make}"
sb_warnflags="${SB_WARNFLAGS:?SB_WARNFLAGS is required}"

# libpam's conversation callback ABI requires void *appdata_ptr. Cppcheck's
# callback-specific const suggestion would require an incompatible function type.
cppcheck_defs=('-D__has_include(x)=0' '-DLINUXIO_VERSION="dev"')
cppcheck_suppress=(
	"--suppress=ctunullpointer:$file" "--suppress=variableScope:$file"
	"--suppress=constParameter:$file" "--suppress=constParameterCallback:$file"
	--suppress=normalCheckLevelMaxBranches
)
tidy_checks="-checks=-clang-analyzer-security.insecureAPI.DeprecatedOrUnsafeBufferHandling,-clang-diagnostic-format-nonliteral"
cc_db_dir="$cache_dir/clang"
cc_db="$cc_db_dir/compile_commands.json"

need() { # need COMMAND INSTALL_HINT
	command -v "$1" >/dev/null 2>&1 || {
		echo "❌ $1 not found (install: $2)"
		exit 1
	}
}

need cppcheck "sudo apt-get install cppcheck"
echo "   cppcheck"
cppcheck --enable=warning,style,performance,portability --inconclusive --std=c11 --force \
	"${cppcheck_suppress[@]}" "${cppcheck_defs[@]}" "$file"

command -v "$cc" >/dev/null 2>&1 || {
	echo "❌ compiler not found: $cc"
	exit 1
}
echo "   $cc -fanalyzer"
"$cc" -fanalyzer -Wall -Wextra -Wshadow -Wformat=2 -Wconversion -Wnull-dereference -Wvla -O2 -c "$file"
rm -f linuxio-auth.o

clang_bin=""
for c in clang clang-{30..14}; do
	if command -v "$c" >/dev/null 2>&1; then
		clang_bin="$c"
		break
	fi
done
if ! command -v scan-build >/dev/null 2>&1; then
	echo "  scan-build not found - skipping clang static analyzer"
elif [ -z "$clang_bin" ]; then
	echo "  clang not found - skipping clang static analyzer (install: sudo apt-get install clang)"
else
	echo "   scan-build (clang static analyzer, $clang_bin)"
	scan-build --use-cc="$clang_bin" "$make_cmd" --no-print-directory build-auth CC="$clang_bin" WARNFLAGS="$sb_warnflags"
fi

need bear "sudo apt-get install bear"
need clang-tidy "sudo apt-get install clang-tidy"
echo "   clang-tidy (compile_commands.json via bear)"
mkdir -p "$cc_db_dir"
rm -f "$cc_db"
bear --output "$cc_db" -- "$make_cmd" --no-print-directory build-auth
clang-tidy --quiet "$tidy_checks" --extra-arg=-Wno-unknown-warning-option -p "$cc_db_dir" "$file"
echo "✅ C analysis complete."
