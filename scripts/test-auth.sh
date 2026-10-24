#!/usr/bin/env bash
# C authentication helper tests.
#   unit AUTH_DIR              compile and run linuxio-auth_test.c
#   frametool AUTH_DIR OUT     compile the cross-language protocol frame tool to OUT
#   pam AUTH_DIR CWRAP_LIBDIR  hermetic PAM/host-integration suite for the launcher
# Environment: CC, CFLAGS, LDFLAGS, AUTH_LIBS (from the Makefile).
#
# The pam suite runs the real handle_client() against a pam_wrapper/pam_matrix
# PAM stack, an nss_wrapper user database, uid_wrapper emulated-root privilege
# drops, a stub bridge exec'd through the fixed fd layout, and tmpfile
# accounting databases. It needs the cwrap libraries
# (apt install libpam-wrapper libnss-wrapper libuid-wrapper) and skips with a
# warning when they are missing. CWRAP_LIBDIR names a non-standard location.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

mode="$1" auth_dir="$2"
read -ra cc <<<"${CC:-cc}"
# shellcheck disable=SC2153  # CFLAGS/LDFLAGS are the Makefile's, not typos of the arrays
read -ra cflags <<<"$CFLAGS"
# shellcheck disable=SC2153
read -ra ldflags <<<"$LDFLAGS"
read -ra libs <<<"$AUTH_LIBS"

# compile_test OUT SRC: build SRC with the production flags, warnings fatal.
compile_test() {
	"${cc[@]}" "${cflags[@]}" -Werror '-DLINUXIO_VERSION="test"' -o "$1" "$2" "${ldflags[@]}" "${libs[@]}"
}

find_lib() {
	local f
	for f in ${cwrap_dir:+"$cwrap_dir/$1"} /usr/lib/*/"$1" /usr/lib/"$1" /usr/lib64/"$1" /usr/local/lib/"$1"; do
		if [ -e "$f" ]; then
			echo "$f"
			return 0
		fi
	done
	return 1
}

case "$mode" in
unit)
	test_dir="$(mktemp -d)"
	trap 'rm -rf "$test_dir"' EXIT
	compile_test "$test_dir/linuxio-auth-test" "$auth_dir/linuxio-auth_test.c"
	"$test_dir/linuxio-auth-test" | checkmarks
	printc "${COLOR_GREEN}✅ C authentication helper tests passed!${COLOR_RESET}"
	;;
frametool)
	out="$3"
	mkdir -p "$(dirname "$out")"
	compile_test "$out.tmp" "$auth_dir/linuxio-auth-frametool.c"
	mv "$out.tmp" "$out"
	;;
pam)
	cwrap_dir="${3:-}"
	pam_wrapper_lib="$(find_lib libpam_wrapper.so || true)"
	nss_wrapper_lib="$(find_lib libnss_wrapper.so || true)"
	uid_wrapper_lib="$(find_lib libuid_wrapper.so || true)"
	pam_matrix_lib="$(find_lib pam_wrapper/pam_matrix.so || true)"
	if [ -z "$pam_wrapper_lib" ] || [ -z "$nss_wrapper_lib" ] ||
		[ -z "$uid_wrapper_lib" ] || [ -z "$pam_matrix_lib" ]; then
		echo "⚠️  Skipping PAM integration tests: cwrap wrapper libraries not found."
		echo "   Install them with: apt install libpam-wrapper libnss-wrapper libuid-wrapper"
		echo "   (or pass LINUXIO_CWRAP_LIBDIR=<dir containing the .so files>)"
		exit 0
	fi
	test_dir="$(mktemp -d)"
	trap 'rm -rf "$test_dir"' EXIT
	"${cc[@]}" -shared -fPIC -O2 -Wall -Wextra -Werror \
		-o "$test_dir/pam_linuxio_probe.so" "$auth_dir/testdata/pam_linuxio_probe.c" -lpam
	"${cc[@]}" -O2 -Wall -Wextra -Werror \
		-o "$test_dir/bridge-stub" "$auth_dir/testdata/linuxio-test-bridge.c"
	compile_test "$test_dir/linuxio-auth-pam-test" "$auth_dir/linuxio-auth-pam_test.c"
	mkdir -p "$test_dir/pam.d"
	sed -e "s|@PROBE_MODULE@|$test_dir/pam_linuxio_probe.so|g" \
		-e "s|@PAM_MATRIX@|$pam_matrix_lib|g" \
		-e "s|@PASSDB@|$test_dir/passdb|g" \
		"$auth_dir/testdata/linuxio.pam.in" >"$test_dir/pam.d/linuxio"
	: >"$test_dir/passwd"
	: >"$test_dir/group"
	cd "$test_dir"
	env -u JOURNAL_STREAM \
		LD_PRELOAD="$pam_wrapper_lib:$nss_wrapper_lib:$uid_wrapper_lib" \
		PAM_WRAPPER=1 \
		PAM_WRAPPER_SERVICE_DIR="$test_dir/pam.d" \
		NSS_WRAPPER_PASSWD="$test_dir/passwd" \
		NSS_WRAPPER_GROUP="$test_dir/group" \
		UID_WRAPPER=1 UID_WRAPPER_ROOT=1 \
		LINUXIO_TEST_REAL_UID="$(id -u)" LINUXIO_TEST_REAL_GID="$(id -g)" \
		LANG=C.UTF-8 TERM=xterm \
		./linuxio-auth-pam-test | checkmarks
	printc "${COLOR_GREEN}✅ PAM integration tests passed!${COLOR_RESET}"
	;;
*)
	echo "usage: test-auth.sh unit|frametool|pam AUTH_DIR [ARG]" >&2
	exit 2
	;;
esac
