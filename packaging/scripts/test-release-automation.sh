#!/bin/bash
# Focused fixture tests for the release automation helpers.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEST_ROOT="$(mktemp -d /tmp/linuxio-release-tests.XXXXXX)"

cleanup() {
  case "$TEST_ROOT" in
    /tmp/linuxio-release-tests.*) rm -rf -- "$TEST_ROOT" ;;
  esac
}
trap cleanup EXIT

fail() {
  echo "release automation fixture failed: $*" >&2
  exit 1
}

assert_contains() {
  local expected=$1
  local actual=$2
  [[ "$actual" == *"$expected"* ]] || fail "expected output to contain: $expected"
}

new_git_fixture() {
  local fixture
  fixture="$(mktemp -d "$TEST_ROOT/repository.XXXXXX")"
  git -C "$fixture" init -q
  git -C "$fixture" config user.name Tester
  git -C "$fixture" config user.email tester@example.com
  printf '%s\n' 'SHELL := /bin/bash' '-include release.mk' >"$fixture/Makefile"
  cp "$REPO_ROOT/release.mk" "$fixture/release.mk"
  mkdir -p "$fixture/packaging/scripts"
  cp "$REPO_ROOT/packaging/scripts/changelog-entry.sh" \
    "$fixture/packaging/scripts/changelog-entry.sh"
  printf '%s\n' 'fixture' >"$fixture/fixture.txt"
  git -C "$fixture" add Makefile release.mk packaging/scripts/changelog-entry.sh fixture.txt
  git -C "$fixture" commit -qm 'chore: fixture setup'
  printf '%s' "$fixture"
}

test_changelog_delimiters() {
  local fixture output
  fixture="$(mktemp -d "$TEST_ROOT/changelog.XXXXXX")"
  git -C "$fixture" init -q
  git -C "$fixture" config user.name Tester
  git -C "$fixture" config user.email tester@example.com
  printf base >"$fixture/file"
  git -C "$fixture" add file
  git -C "$fixture" commit -qm 'chore: base'
  git -C "$fixture" tag v0.1.0
  printf one >>"$fixture/file"
  git -C "$fixture" add file
  git -C "$fixture" commit -qm 'fix: preserve | pipe and \backslash'
  printf two >>"$fixture/file"
  git -C "$fixture" add file
  git -C "$fixture" commit -qm 'feat: ordinary subject'

  output="$(cd "$fixture" && "$SCRIPT_DIR/changelog-entry.sh" \
    v0.2.0 v0.1.0 v0.1.0..HEAD owner/project)"
  assert_contains 'fix: preserve | pipe and \backslash' "$output"
  assert_contains 'feat: ordinary subject' "$output"
  assert_contains \
    '**Full Changelog**: https://github.com/owner/project/compare/v0.1.0...v0.2.0' \
    "$output"
}

write_gh_open_pr_stub() {
  local bin_dir=$1
  local body_file=$2
  mkdir -p "$bin_dir"
  cat >"$bin_dir/gh" <<'EOF'
#!/bin/bash
set -euo pipefail
state_file=${GH_STATE_FILE:?}
body_file=${GH_BODY_FILE:?}
if [[ "$1 $2" == 'pr list' ]]; then
  if [[ -f "$state_file" ]]; then
    printf '17\n'
  fi
  exit 0
fi
if [[ "$1 $2" == 'pr create' ]]; then
  while (($#)); do
    if [[ "$1" == '--body-file' ]]; then
      shift
      cp "$1" "$body_file"
      break
    fi
    shift
  done
  : >"$state_file"
  exit 0
fi
if [[ "$1 $2" == 'pr checks' ]]; then
  printf 'no checks reported\n'
  exit 0
fi
if [[ "$1 $2" == 'pr view' ]]; then
  if [[ " $* " == *' --web '* ]]; then
    exit 1
  fi
  printf 'https://github.com/owner/project/pull/17\n'
  exit 0
fi
printf 'unexpected gh invocation: %s\n' "$*" >&2
exit 1
EOF
  chmod +x "$bin_dir/gh"
  cat >"$bin_dir/sleep" <<'EOF'
#!/bin/bash
exit 0
EOF
  chmod +x "$bin_dir/sleep"
}

test_open_pr_ignores_stale_tracking_ref_and_honors_repo() {
  local fixture origin bin_dir body output branch body_contents
  fixture="$(new_git_fixture)"
  origin="$fixture/origin.git"
  git init --bare -q "$origin"
  git -C "$fixture" branch -M main
  git -C "$fixture" remote add origin "$origin"
  git -C "$fixture" push -q -u origin main
  git -C "$fixture" tag v0.1.0
  printf 'change' >>"$fixture/fixture.txt"
  git -C "$fixture" add fixture.txt
  git -C "$fixture" commit -qm 'fix: release | body \\ preserved'
  branch=dev/v0.2.0
  git -C "$fixture" checkout -qb "$branch"
  git -C "$fixture" update-ref "refs/remotes/origin/$branch" HEAD
  bin_dir="$fixture/bin"
  body="$fixture/pr-body"
  write_gh_open_pr_stub "$bin_dir" "$body"

  output="$(cd "$fixture" && PATH="$bin_dir:$PATH" GH_STATE_FILE="$fixture/gh-state" \
    GH_BODY_FILE="$body" make --no-print-directory open-pr REPO=override/project)"
  assert_contains 'does not exist yet - publishing' "$output"
  [[ -s "$body" ]] || fail 'open-pr did not capture the generated PR body'
  body_contents="$(<"$body")"
  assert_contains 'https://github.com/override/project/commit/' "$body_contents"
  assert_contains 'fix: release | body \\ preserved' "$body_contents"
  git -C "$fixture" ls-remote --exit-code --heads origin "refs/heads/$branch" >/dev/null \
    || fail 'open-pr did not publish the missing remote branch'
}

test_open_pr_rejects_remote_query_failure() {
  local fixture bin_dir body output status branch
  fixture="$(new_git_fixture)"
  git -C "$fixture" branch -M main
  git -C "$fixture" remote add origin "$fixture/missing-origin.git"
  branch=dev/v0.2.0
  git -C "$fixture" checkout -qb "$branch"
  git -C "$fixture" update-ref "refs/remotes/origin/$branch" HEAD
  bin_dir="$fixture/bin"
  body="$fixture/pr-body"
  write_gh_open_pr_stub "$bin_dir" "$body"

  set +e
  output="$(cd "$fixture" && PATH="$bin_dir:$PATH" GH_STATE_FILE="$fixture/gh-state" \
    GH_BODY_FILE="$body" make --no-print-directory open-pr 2>&1)"
  status=$?
  set -e
  ((status != 0)) || fail 'open-pr unexpectedly trusted a stale ref after query failure'
  assert_contains 'refusing to use a stale tracking ref' "$output"
  [[ ! -e "$fixture/gh-state" ]] || fail 'open-pr queried GitHub after the remote query failed'
}

write_gh_merge_stub() {
  local bin_dir=$1
  local merge_args=$2
  mkdir -p "$bin_dir"
  cat >"$bin_dir/gh" <<'EOF'
#!/bin/bash
set -euo pipefail
merge_args=${GH_MERGE_ARGS:?}
if [[ "$1 $2" == 'pr checks' ]]; then
  printf 'checks passed\n'
  exit 0
fi
if [[ "$1 $2" == 'pr view' ]]; then
  if [[ " $* " == *'baseRefName,headRefName,headRefOid'* ]]; then
    printf 'main\tdev/v0.2.0\t%s\n' "$(git rev-parse HEAD)"
  fi
  exit 0
fi
if [[ "$1 $2 $3" == 'pr merge --help' ]]; then
  printf '%s\n' '--match-head-commit string'
  exit 0
fi
if [[ "$1 $2" == 'pr merge' ]]; then
  printf '%s\n' "$*" >"$merge_args"
  exit 0
fi
if [[ "$1 $2" == 'run list' ]]; then
  exit 0
fi
printf 'unexpected gh invocation: %s\n' "$*" >&2
exit 1
EOF
  chmod +x "$bin_dir/gh"
  cat >"$bin_dir/sleep" <<'EOF'
#!/bin/bash
exit 0
EOF
  chmod +x "$bin_dir/sleep"
}

test_merge_checks_pr_pair_and_expected_head() {
  local fixture origin bin_dir merge_args output status head
  fixture="$(new_git_fixture)"
  origin="$fixture/origin.git"
  git init --bare -q "$origin"
  git -C "$fixture" branch -M main
  git -C "$fixture" remote add origin "$origin"
  git -C "$fixture" push -q -u origin main
  git -C "$fixture" checkout -qb dev/v0.2.0
  git -C "$fixture" push -q -u origin dev/v0.2.0
  head="$(git -C "$fixture" rev-parse HEAD)"
  bin_dir="$fixture/bin"
  merge_args="$fixture/merge-args"
  write_gh_merge_stub "$bin_dir" "$merge_args"

  set +e
  output="$(cd "$fixture" && PATH="$bin_dir:$PATH" GH_MERGE_ARGS="$merge_args" \
    make --no-print-directory merge-release PR=17 REPO=owner/project 2>&1)"
  status=$?
  set -e
  ((status != 0)) || fail 'merge fixture unexpectedly succeeded without a release workflow'
  [[ -s "$merge_args" ]] || fail 'merge-release did not invoke gh pr merge'
  assert_contains '--match-head-commit' "$(<"$merge_args")"
  assert_contains "$head" "$(<"$merge_args")"
  assert_contains 'Workflow did not succeed' "$output"
}

test_start_dev_fails_on_fetch_error() {
  local fixture output status
  fixture="$(new_git_fixture)"
  git -C "$fixture" branch -M main
  git -C "$fixture" remote add origin "$fixture/missing-origin.git"
  mkdir -p "$fixture/bin"
  printf '#!/bin/bash\nexit 0\n' >"$fixture/bin/gh"
  chmod +x "$fixture/bin/gh"

  set +e
  output="$(cd "$fixture" && PATH="$fixture/bin:$PATH" \
    make --no-print-directory start-dev VERSION=v0.2.0 2>&1)"
  status=$?
  set -e
  ((status != 0)) || fail 'start-dev unexpectedly succeeded with a failed fetch'
  git -C "$fixture" show-ref --verify --quiet refs/heads/dev/v0.2.0 \
    && fail 'start-dev created a release branch after fetch failure'
  assert_contains 'fatal:' "$output"
}

test_changelog_delimiters
test_open_pr_ignores_stale_tracking_ref_and_honors_repo
test_open_pr_rejects_remote_query_failure
test_merge_checks_pr_pair_and_expected_head
test_start_dev_fails_on_fetch_error
echo 'release automation fixtures: ok'
