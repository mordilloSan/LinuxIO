#!/usr/bin/env bash
# Fixture checks for third-party-notices.sh, against a stand-in for `go` and a
# module cache in a temporary folder: the notices hold the hand-written part,
# Go's licence, every licence file of every module the programs report (a
# replaced module under its replacement, a capital letter in the cache's
# spelling, the licence of code in a subfolder), a repeated text once, and Vite's packages; and the script fails
# on a module with no licence file, on a module missing from the cache and on
# a frontend bundle without Vite's file.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NOTICES="${SCRIPT_DIR}/../third-party-notices.sh"

fail() {
	echo "❌ $*" >&2
	exit 1
}

pass() {
	printf '   \033[1;32m✓\033[0m %s\n' "$1"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "${work}/build" "${work}/goroot" "${work}/frontend/.vite" \
	"${work}/mod/example.com/one@v1.0.0" "${work}/mod/example.com/t!wo@v2.1.0" \
	"${work}/mod/example.com/fork@v0.3.0" "${work}/mod/example.com/bare@v0.0.1"
echo "GO LICENCE TEXT" >"${work}/goroot/LICENSE"
echo "ONE LICENCE TEXT" >"${work}/mod/example.com/one@v1.0.0/LICENSE"
echo "ONE NOTICE TEXT" >"${work}/mod/example.com/one@v1.0.0/NOTICE"
echo "not a licence" >"${work}/mod/example.com/one@v1.0.0/README.md"
echo "not a licence" >"${work}/mod/example.com/one@v1.0.0/license.go"
echo "not a licence" >"${work}/mod/example.com/one@v1.0.0/licensecheck.sh"
mkdir -p "${work}/mod/example.com/one@v1.0.0/internal/other" "${work}/mod/example.com/one@v1.0.0/testdata"
echo "OTHER LICENCE TEXT" >"${work}/mod/example.com/one@v1.0.0/internal/other/LICENSE.txt"
echo "not a licence" >"${work}/mod/example.com/one@v1.0.0/testdata/LICENSE"
echo "ONE LICENCE TEXT" >"${work}/mod/example.com/t!wo@v2.1.0/COPYING"
echo "FORK LICENCE TEXT" >"${work}/mod/example.com/fork@v0.3.0/LICENSE.md"
echo "not a licence" >"${work}/mod/example.com/bare@v0.0.1/README.md"
printf '# Licenses\n\nThe app bundles dependencies which contain the following licenses:\n\n## pkg - 1.0.0 (MIT)\n\nPKG LICENCE TEXT\n' \
	>"${work}/frontend/.vite/license.md"
touch "${work}/build/alpha" "${work}/build/beta" "${work}/frontend/index.html"

# The stand-in: `go env NAME` and `go version -m PROGRAM`, whose modules come
# from PROGRAM.modules beside it.
cat >"${work}/go" <<'GO'
#!/usr/bin/env bash
case "$1 $2" in
"env GOMODCACHE") echo "$FIXTURE/mod" ;;
"env GOROOT") echo "$FIXTURE/goroot" ;;
"version -m")
	[[ -f "$3.modules" ]] || exit 1
	printf '%s: go1.99.0\n\tpath\texample.com/linuxio\n' "$3"
	cat "$3.modules"
	;;
*) exit 2 ;;
esac
GO
chmod +x "${work}/go"
export FIXTURE=$work
printf '\tdep\texample.com/one\tv1.0.0\th1:x=\n\tdep\texample.com/tWo\tv2.1.0\th1:x=\n' >"${work}/build/alpha.modules"
printf '\tdep\texample.com/one\tv1.0.0\th1:x=\n\tdep\texample.com/orig\tv0.1.0\n\t=>\texample.com/fork\tv0.3.0\th1:x=\n' >"${work}/build/beta.modules"

notices() {
	bash "$NOTICES" --build-dir "${work}/build" --frontend-dir "${work}/frontend" --go "${work}/go" "$@"
}
out="${work}/build/THIRD_PARTY_NOTICES.md"
has() { grep -Fq -- "$1" "$out" || fail "the notices lack: $1"; }
count() { grep -Fc -- "$1" "$out" || true; }
refuses() {
	local what=$1 want=$2 err
	shift 2
	if err=$(notices "$@" 2>&1 >/dev/null); then
		fail "${what}: expected a failure"
	fi
	[[ $err == *"$want"* ]] || fail "${what}: expected '${want}' in: ${err}"
}

notices alpha beta
has "## FileBrowser Quantum"
has "## Icon sets"
has "GO LICENCE TEXT"
has "(alpha beta)"
has "go1.99.0"
pass "the hand-written part and Go's licence are in"
has "### example.com/one v1.0.0"
has "Source: https://pkg.go.dev/example.com/one@v1.0.0"
has "ONE NOTICE TEXT"
[[ $(count "### example.com/one v1.0.0") == 1 ]] || fail "a module two programs link is listed more than once"
[[ $(count "not a licence") == 0 ]] || fail "a file that is no licence was copied"
has "internal/other/LICENSE.txt:"
has "OTHER LICENCE TEXT"
pass "a module is listed once, with its LICENSE and NOTICE, the licence of code in a subfolder, and nothing else"
has "### example.com/tWo v2.1.0"
[[ $(count "ONE LICENCE TEXT") == 1 ]] || fail "the same licence text is written more than once"
has "COPYING: the same text as LICENSE under example.com/one above."
pass "a capital letter is found in the cache, and a repeated text is named, not repeated"
has "### example.com/fork v0.3.0"
has "FORK LICENCE TEXT"
[[ $(count "example.com/orig") == 0 ]] || fail "a replaced module is listed under its old name"
pass "a replaced module is listed as what replaced it"
has "## pkg - 1.0.0 (MIT)"
has "PKG LICENCE TEXT"
[[ $(count "# Licenses") == 0 ]] || fail "Vite's own title was copied"
pass "Vite's packages follow, without its title"
[[ $(stat -c %a "$out") == 644 ]] || fail "the notices are not 0644"

first=$(sha256sum <"$out")
notices alpha beta
[[ $(sha256sum <"$out") == "$first" ]] || fail "two runs on one build differ"
pass "two runs on one build write the same bytes"

printf '\tdep\texample.com/bare\tv0.0.1\th1:x=\n' >"${work}/build/gamma.modules"
touch "${work}/build/gamma"
refuses "a module without a licence file" "has no licence file" gamma
printf '\tdep\texample.com/absent\tv9.9.9\th1:x=\n' >"${work}/build/gamma.modules"
refuses "a module missing from the cache" "is not in the module cache" gamma
printf '\tdep\texample.com/orig\tv0.1.0\n\t=>\t../local\t(devel)\t\n' >"${work}/build/gamma.modules"
refuses "a module replaced by a folder" "replaced by a folder" gamma
refuses "a program that is not built" "build it first" delta
touch "${work}/build/delta"
refuses "a program go cannot read" "could not read the modules" delta
[[ $(sha256sum <"$out") == "$first" ]] || fail "a failed run changed the notices"
pass "a module without a licence, outside the cache or replaced by a folder fails, and leaves the file alone"

rm "${work}/frontend/.vite/license.md"
refuses "a bundle without Vite's file" "run make build-vite" alpha
rm "${work}/frontend/index.html"
notices alpha
has "This build holds no frontend bundle."
pass "a bundle without Vite's file fails; a build with no bundle says so"

printf '\033[1;32m%s\033[0m\n' "✅ third-party-notices fixtures passed"
