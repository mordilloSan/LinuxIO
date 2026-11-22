#!/usr/bin/env bash
# Fixture checks for changelog-entry.sh, in a throwaway git repository with a
# stand-in for `gh`: an author GitHub knows is written as @login and listed
# once, also for a commit made with another email; a lookup that fails is
# tried again and given up after three tries; a commit with no author email
# does not end the script; and an author's text is never run.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHANGELOG="${SCRIPT_DIR}/../changelog-entry.sh"

fail() {
	echo "❌ $*" >&2
	exit 1
}

pass() {
	printf '   \033[1;32m✓\033[0m %s\n' "$1"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "${work}/bin" "${work}/repo"
export FIXTURE=$work

# The stand-in answers `gh api repos/R/commits/HASH --jq ...` by the commit's
# author email, and writes each call down.
cat >"${work}/bin/gh" <<'GH'
#!/usr/bin/env bash
email=$(git -C "$FIXTURE/repo" log -1 --format=%ae "${2##*/}")
echo "$email" >>"$FIXTURE/calls"
case $email in
alice@linked) echo alice ;;
flaky@example) [[ $(grep -cxF -- "$email" "$FIXTURE/calls") -ge 2 ]] && echo flaky-login || exit 1 ;;
down@example) exit 1 ;;
*) echo "" ;;
esac
GH
chmod +x "${work}/bin/gh"

g() { git -C "${work}/repo" -c commit.gpgsign=false "$@"; }
commit() {
	GIT_AUTHOR_NAME=$1 GIT_AUTHOR_EMAIL=$2 GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=fixture@example.invalid \
		g commit -q --allow-empty -m "$3"
}
g init -q
commit "Alice A" alice@linked "feat: one"
commit "Alice A" alice@machine "fix: two"
commit "Bob B" bob@plain "docs: three"
commit "Flaky F" flaky@example "fix: four"
commit "Flaky F" flaky@example "fix: five"
for n in 1 2 3 4 5; do commit "Down D" down@example "chore: down ${n}"; done
commit "Eve E" "x]\$(touch ${work}/ran)[" "fix: six"
commit "Eve E" "a[\$(touch ${work}/ran)]" "fix: seven"
# No author email: git commit refuses one, so the commit object is written.
no_mail=$(printf 'tree %s\nparent %s\nauthor No Mail <> 1700000000 +0000\ncommitter fixture <fixture@example.invalid> 1700000000 +0000\n\nfix: eight\n' \
	"$(g rev-parse 'HEAD^{tree}')" "$(g rev-parse HEAD)" | g hash-object -t commit -w --stdin)
g update-ref HEAD "$no_mail"
[[ -z $(g log -1 --format=%ae) ]] || fail "the fixture's last commit has an author email"

out="${work}/out.md"
(cd "${work}/repo" && PATH="${work}/bin:${PATH}" bash "$CHANGELOG" v9.9.9 "" "" owner/repo >"$out") ||
	fail "changelog-entry.sh failed"
has() { grep -Eq -- "$1" "$out" || fail "the entry lacks: $1"; }
calls() { grep -cxF -- "$1" "${work}/calls" || true; }
# Sorted here: the script's own order follows the locale.
contributors=$(sed -n '/^###  Contributors/,/^\*\*Full/p' "$out" | grep '^\* ' | LC_ALL=C sort | tr '\n' ' ')

has '^\* fix: eight .* by No Mail$'
pass "a commit with no author email is written by its name"
has '^\* feat: one .* by @alice$'
has '^\* docs: three .* by Bob B$'
[[ $(calls alice@linked) == 1 && $(calls bob@plain) == 1 ]] || fail "an answered lookup was made again"
pass "an author GitHub knows is @login, another the Git name, one lookup each"
has '^\* fix: four .* by Flaky F$'
has '^\* fix: five .* by @flaky-login$'
[[ $(calls down@example) == 3 ]] || fail "a failing lookup was made $(calls down@example) times, not three"
pass "a failed lookup is tried again on the next commit and given up after three"
[[ $contributors == "* @alice * @flaky-login * Bob B * Down D * Eve E * No Mail " ]] ||
	fail "contributors: ${contributors}"
pass "each author is listed once, by login where one commit gave it"
[[ ! -e "${work}/ran" ]] || fail "an author email was run as a command"
pass "an author's text is not run"

printf '\033[1;32m%s\033[0m\n' "✅ changelog-entry fixtures passed"
