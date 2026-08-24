# Release flow targets for maintainers (gh CLI): start-dev, open-pr, merge-release.

DEFAULT_BASE_BRANCH := main
REPO ?=
CONFIRM ?= 1

# One version pattern shared by VERSION validation and dev/v* branch checks,
# matching what .github/workflows/release.yml accepts (any dev/v* prerelease).
release_version_re := v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?

define _require_clean
	@if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "ERROR: Working tree not clean. Commit/stash changes first."; exit 1; \
	fi
endef

define _require_gh
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "ERROR: GitHub CLI (gh) not found. Install: https://cli.github.com/"; exit 1; \
	fi
endef

define _read_and_validate_version
	if [ -z "$(VERSION)" ]; then \
	  read -p "Enter version (e.g. v1.2.3): " VERSION_INPUT; \
	else \
	  VERSION_INPUT="$(VERSION)"; \
	fi; \
	VERSION="$${VERSION_INPUT:-}"; \
	VERSION="$$(printf '%s' "$$VERSION" | sed -E 's/^V/v/')"; \
	if ! echo "$$VERSION" | grep -Eq '^$(release_version_re)$$'; then \
	  echo "ERROR: VERSION must look like v1.2.3 or v1.2.3-rc.1 (got '$$VERSION')"; \
	  exit 1; \
	fi; \
	REL_BRANCH="dev/$$VERSION"
endef

# Regenerate the changelog-based PR body into a fresh temp file named by
# $$PR_BODY_FILE. Requires $$VERSION; the caller removes the temp file.
define _generate_pr_body
	REPO_NAME="$(if $(REPO),$(REPO),$${GITHUB_REPOSITORY:-$$(git remote get-url origin 2>/dev/null | sed -E 's#.*github\.com[:/]##; s#\.git$$##')})"; \
	PREV_TAG="$$(git tag --list 'v*' --sort=-v:refname | grep -v "^$$VERSION$$" | head -n1 || echo "")"; \
	if [ -n "$$PREV_TAG" ]; then \
	  COMMIT_RANGE="$${PREV_TAG}..HEAD"; \
	else \
	  COMMIT_RANGE=""; \
	fi; \
	PR_BODY_FILE="$$(mktemp)"; \
	{ \
	  echo "## $$VERSION — $$(date -u +%Y-%m-%d)"; \
	  echo ""; \
	  ./packaging/scripts/changelog-entry.sh "$$VERSION" "$$PREV_TAG" "$$COMMIT_RANGE" "$$REPO_NAME"; \
	} > "$$PR_BODY_FILE"
endef

define _repo_flag
$(if $(REPO),--repo $(REPO),)
endef

# Tail of a `gh run list --jq` filter: collapse the selected run into one TSV row,
# or emit nothing when no run matched. Keep the free-text title last so a tab
# inside it cannot shift the earlier fields when the shell splits the row.
# gh embeds its own jq (gojq), so none of this needs the jq binary installed.
_run_tsv = if . == null then empty else [ (.databaseId|tostring), .status, (.conclusion // "n/a"), .createdAt, (.headBranch // "n/a"), (.event // "n/a"), (.displayTitle // .name) ] | @tsv end

# ==================== Release Targets ====================

start-dev:
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  $(call _read_and_validate_version); \
	  git fetch origin; \
	  git checkout $(DEFAULT_BASE_BRANCH); \
	  git pull --ff-only; \
	  if git show-ref --verify --quiet "refs/heads/$$REL_BRANCH"; then \
	    echo "==> Branch $$REL_BRANCH already exists, checking it out…"; \
	    git checkout "$$REL_BRANCH"; \
	  else \
	    echo "==> Creating branch $$REL_BRANCH from $(DEFAULT_BASE_BRANCH)…"; \
	    git checkout -b "$$REL_BRANCH" "$(DEFAULT_BASE_BRANCH)"; \
	    git push -u origin "$$REL_BRANCH"; \
	  fi; \
	  echo "OK: Ready on branch $$REL_BRANCH"; \
	}

open-pr:
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/$(release_version_re)$$'; then \
	    echo "ERROR: Not on a dev/v* release branch (got '$$BRANCH')."; exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  trap 'rm -f "$${PR_BODY_FILE:-}"' EXIT; \
	  OPEN_PR_STATUS=0; \
	  PUSHED=0; \
	  PUSH_REMOTE="$$(git config --get "branch.$$BRANCH.remote" 2>/dev/null || echo origin)"; \
	  REMOTE_REF="refs/remotes/$$PUSH_REMOTE/$$BRANCH"; \
	  if ! REMOTE_INFO="$$(git ls-remote --heads "$$PUSH_REMOTE" "refs/heads/$$BRANCH")"; then \
	    echo "ERROR: Unable to query $$PUSH_REMOTE/$$BRANCH; refusing to use a stale tracking ref."; \
	    exit 1; \
	  fi; \
	  if [ -z "$$REMOTE_INFO" ]; then \
	    echo "==> $$PUSH_REMOTE/$$BRANCH does not exist yet - publishing…"; \
	    git push -u "$$PUSH_REMOTE" "HEAD:refs/heads/$$BRANCH"; \
	    PUSHED=1; \
	  else \
	    if ! git fetch --quiet "$$PUSH_REMOTE" "+refs/heads/$$BRANCH:$$REMOTE_REF"; then \
	      echo "ERROR: Unable to fetch $$PUSH_REMOTE/$$BRANCH; refusing to use a stale tracking ref."; \
	      exit 1; \
	    fi; \
	    BEHIND="$$(git rev-list --count "HEAD..$$REMOTE_REF")"; \
	    AHEAD="$$(git rev-list --count "$$REMOTE_REF..HEAD")"; \
	    if [ "$$BEHIND" -gt 0 ] && [ "$$AHEAD" -gt 0 ]; then \
	      echo "ERROR: $$BRANCH has diverged from $$PUSH_REMOTE/$$BRANCH ($$BEHIND behind, $$AHEAD ahead)."; \
	      echo "    Reconcile deliberately (rebase, then git push --force-with-lease), then re-run."; \
	      exit 1; \
	    fi; \
	    if [ "$$BEHIND" -gt 0 ]; then \
	      echo "ERROR: $$BRANCH is $$BEHIND commit(s) behind $$PUSH_REMOTE/$$BRANCH."; \
	      echo "    Reconcile first (e.g. git pull --rebase), then re-run."; \
	      exit 1; \
	    fi; \
	    if [ "$$AHEAD" -gt 0 ]; then \
	      echo "==> Publishing $$AHEAD local commit(s) to $$PUSH_REMOTE/$$BRANCH…"; \
	      git push "$$PUSH_REMOTE" "HEAD:refs/heads/$$BRANCH"; \
	      PUSHED=1; \
	    else \
	      echo "==> $$BRANCH already in sync with $$PUSH_REMOTE/$$BRANCH - nothing to push."; \
	    fi; \
	  fi; \
	  BASE_BRANCH="$(DEFAULT_BASE_BRANCH)"; \
	  if ! PRNUM="$$(gh pr list $(call _repo_flag) --base "$$BASE_BRANCH" --head "$$BRANCH" --state open --json number --jq '.[0].number')"; then \
	    echo "ERROR: Unable to query open PRs for $$BRANCH."; \
	    exit 1; \
	  fi; \
	  if [ -n "$$PRNUM" ] && [ "$$PRNUM" != "null" ]; then \
	    echo "==> An open PR (#$$PRNUM) from $$BRANCH -> $$BASE_BRANCH already exists."; \
	    if [ "$$PUSHED" -eq 1 ]; then \
	      echo "==> Refreshing PR body with the latest changelog…"; \
	      $(call _generate_pr_body); \
	      gh pr edit $(call _repo_flag) "$$PRNUM" --body-file "$$PR_BODY_FILE"; \
	      echo "OK: PR body updated."; \
	    fi; \
	  else \
	    echo "==> Opening PR: $$BRANCH -> $$BASE_BRANCH…"; \
	    $(call _generate_pr_body); \
	    gh pr create $(call _repo_flag) \
	      --base "$$BASE_BRANCH" \
	      --head "$$BRANCH" \
	      --title "Release $$VERSION" \
	      --body-file "$$PR_BODY_FILE"; \
	    PRNUM="$$(gh pr list $(call _repo_flag) --base "$$BASE_BRANCH" --head "$$BRANCH" --state open --json number --jq '.[0].number')"; \
	  fi; \
	  echo ""; \
	  echo "==> Waiting for CI checks to register..."; \
	  sleep 3; \
	  for i in 1 2 3 4 5; do \
	    CHECK_OUTPUT="$$(gh pr checks $(call _repo_flag) "$$PRNUM" 2>&1 || true)"; \
	    if ! echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	      break; \
	    fi; \
	    if [ $$i -lt 5 ]; then \
	      echo "==> Retrying in 2s... (attempt $$i/5)"; \
	      sleep 2; \
	    fi; \
	  done; \
	  if echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	    echo "WARN: No CI checks detected after 15s. Skipping check wait."; \
	    echo "    Checks might start later - monitor the PR manually."; \
	  else \
	    echo "==> Waiting for checks to complete on PR #$$PRNUM…"; \
	    echo "    (Ctrl+C stops watching; re-run 'make open-pr' to resume)"; \
	    echo ""; \
	    START_TIME=$$(date +%s); \
	    if gh pr checks $(call _repo_flag) "$$PRNUM" --watch --interval 5; then \
	      CHECK_STATUS=0; \
	    else \
	      CHECK_STATUS=$$?; \
	    fi; \
	    TOTAL_TIME=$$(( $$(date +%s) - $$START_TIME )); \
	    echo ""; \
	    if [ $$CHECK_STATUS -eq 0 ]; then \
	      echo "OK: All checks passed! (took $$(printf "%02d:%02d" $$((TOTAL_TIME/60)) $$((TOTAL_TIME%60))))"; \
	    else \
	      RECHECK_STATUS=0; \
	      gh pr checks $(call _repo_flag) "$$PRNUM" >/dev/null 2>&1 || RECHECK_STATUS=$$?; \
	      if [ $$RECHECK_STATUS -eq 0 ]; then \
	        echo "OK: All checks passed! (took $$(printf "%02d:%02d" $$((TOTAL_TIME/60)) $$((TOTAL_TIME%60))))"; \
	      elif [ $$CHECK_STATUS -eq 130 ] || [ $$RECHECK_STATUS -eq 8 ]; then \
	        echo "WARN: Check monitoring stopped while checks are still pending."; \
	        echo "    Re-run 'make open-pr' to resume watching."; \
	        OPEN_PR_STATUS=1; \
	      else \
	        gh pr checks $(call _repo_flag) "$$PRNUM" || true; \
	        echo ""; \
	        echo "ERROR: Checks failed on PR #$$PRNUM."; \
	        OPEN_PR_STATUS=1; \
	      fi; \
	    fi; \
	  fi; \
	  echo ""; \
	  PR_URL="$$(gh pr view $(call _repo_flag) "$$PRNUM" --json url --jq '.url' 2>/dev/null || true)"; \
	  if [ -n "$$PR_URL" ]; then \
	    echo "==> PR #$$PRNUM: $$PR_URL"; \
	  else \
	    echo "==> View PR #$$PRNUM with: gh pr view $$PRNUM"; \
	  fi; \
	  exit "$$OPEN_PR_STATUS"; \
	}

merge-release:
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/$(release_version_re)$$'; then \
	    echo "ERROR: Current branch '$$BRANCH' is not a dev/v* release branch."; exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  PUSH_REMOTE="$$(git config --get "branch.$$BRANCH.remote" 2>/dev/null || echo origin)"; \
	  PRNUM=""; \
	  PR_STATE=""; \
	  if [ -n "$(PR)" ]; then \
	    PRNUM="$(PR)"; \
	    if ! PR_STATE="$$(gh pr view $(call _repo_flag) "$$PRNUM" --json state --jq '.state')"; then \
	      echo "ERROR: Unable to query PR #$$PRNUM."; exit 1; \
	    fi; \
	  else \
	    if ! PRNUM="$$(gh pr list $(call _repo_flag) --base "$(DEFAULT_BASE_BRANCH)" --head "$$BRANCH" --state open --json number --jq '.[0].number')"; then \
	      echo "ERROR: Unable to query open PRs for $$BRANCH."; exit 1; \
	    fi; \
	    if [ -n "$$PRNUM" ] && [ "$$PRNUM" != "null" ]; then \
	      PR_STATE="OPEN"; \
	    else \
	      if ! PRNUM="$$(gh pr list $(call _repo_flag) --base "$(DEFAULT_BASE_BRANCH)" --head "$$BRANCH" --state merged --json number --jq '.[0].number')"; then \
	        echo "ERROR: Unable to query merged PRs for $$BRANCH."; exit 1; \
	      fi; \
	      if [ -n "$$PRNUM" ] && [ "$$PRNUM" != "null" ]; then \
	        PR_STATE="MERGED"; \
	      else \
	        PRNUM=""; \
	      fi; \
	    fi; \
	  fi; \
	  if [ -z "$$PRNUM" ]; then \
	    echo "ERROR: No open or merged PR from $$BRANCH to $(DEFAULT_BASE_BRANCH)."; exit 1; \
	  fi; \
	  if [ "$$PR_STATE" = "MERGED" ]; then \
	    echo "==> PR #$$PRNUM from $$BRANCH is already merged - resuming release watch and cleanup."; \
	    RESUME_INFO="$$(gh pr view $(call _repo_flag) "$$PRNUM" --json baseRefName,headRefName,headRefOid --jq '[.baseRefName, .headRefName, .headRefOid] | @tsv')"; \
	    IFS=$$'\t' read -r PR_BASE_BRANCH PR_HEAD_BRANCH RELEASE_HEAD <<< "$$RESUME_INFO" || true; \
	    if [ "$${PR_BASE_BRANCH:-}" != "$(DEFAULT_BASE_BRANCH)" ] || [ "$${PR_HEAD_BRANCH:-}" != "$$BRANCH" ] || [ -z "$${RELEASE_HEAD:-}" ]; then \
	      echo "ERROR: PR #$$PRNUM is not a merged $$BRANCH -> $(DEFAULT_BASE_BRANCH) release PR."; \
	      echo "    PR: $${PR_HEAD_BRANCH:-?} -> $${PR_BASE_BRANCH:-?}"; \
	      exit 1; \
	    fi; \
	  elif [ "$$PR_STATE" = "OPEN" ]; then \
	    echo "==> Checking status of PR #$$PRNUM…"; \
	    CHECK_OUTPUT="$$(gh pr checks $(call _repo_flag) "$$PRNUM" 2>&1 || true)"; \
	    if echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	      echo "ERROR: No CI checks are reported for PR #$$PRNUM; refusing to merge."; \
	      echo "    Wait for checks to register or inspect the PR, then re-run."; \
	      exit 1; \
	    elif ! gh pr checks $(call _repo_flag) "$$PRNUM" > /dev/null 2>&1; then \
	      echo "ERROR: Checks have not passed. Run 'make open-pr' to wait for checks."; \
	      exit 1; \
	    else \
	      echo "OK: All checks passed."; \
	    fi; \
	    if ! REMOTE_INFO="$$(git ls-remote --heads "$$PUSH_REMOTE" "refs/heads/$$BRANCH")"; then \
	      echo "ERROR: Unable to query $$PUSH_REMOTE/$$BRANCH; refusing to merge an unsynchronized PR."; \
	      exit 1; \
	    fi; \
	    if [ -z "$$REMOTE_INFO" ]; then \
	      echo "ERROR: $$PUSH_REMOTE/$$BRANCH does not exist; refusing to merge."; \
	      exit 1; \
	    fi; \
	    RELEASE_HEAD="$$(git rev-parse HEAD)"; \
	    REMOTE_HEAD="$$(printf '%s\n' "$$REMOTE_INFO" | cut -f1)"; \
	    if [ "$$RELEASE_HEAD" != "$$REMOTE_HEAD" ]; then \
	      echo "ERROR: Local HEAD ($$RELEASE_HEAD) is not synchronized with $$PUSH_REMOTE/$$BRANCH ($$REMOTE_HEAD)."; \
	      echo "    Push or reconcile the release branch, then re-run."; \
	      exit 1; \
	    fi; \
	    PR_HEAD_INFO="$$(gh pr view $(call _repo_flag) "$$PRNUM" --json baseRefName,headRefName,headRefOid --jq '[.baseRefName, .headRefName, .headRefOid] | @tsv')"; \
	    IFS=$$'\t' read -r PR_BASE_BRANCH PR_HEAD_BRANCH PR_HEAD_OID <<< "$$PR_HEAD_INFO"; \
	    if [ "$$PR_BASE_BRANCH" != "$(DEFAULT_BASE_BRANCH)" ] || [ "$$PR_HEAD_BRANCH" != "$$BRANCH" ] || [ "$$PR_HEAD_OID" != "$$RELEASE_HEAD" ]; then \
	      echo "ERROR: PR #$$PRNUM is not synchronized with local $$BRANCH ($$RELEASE_HEAD)."; \
	      echo "    PR: $$PR_HEAD_BRANCH -> $$PR_BASE_BRANCH ($$PR_HEAD_OID)"; \
	      echo "    Push or reconcile the release branch, then re-run."; \
	      exit 1; \
	    fi; \
	    if ! gh pr merge --help 2>&1 | grep -q -- '--match-head-commit'; then \
	      echo "ERROR: gh must support --match-head-commit for race-safe release merges."; \
	      echo "    Update GitHub CLI, then re-run."; \
	      exit 1; \
	    fi; \
	    PREV_TAG="$$(git tag --list 'v*' --sort=-v:refname | grep -v "^$$VERSION$$" | head -n1 || echo "")"; \
	    if [ -n "$$PREV_TAG" ]; then \
	      COMMIT_COUNT="$$(git rev-list --count "$$PREV_TAG..HEAD" 2>/dev/null || echo '?')"; \
	      COMMIT_SCOPE="since $$PREV_TAG"; \
	    else \
	      COMMIT_COUNT="$$(git rev-list --count HEAD)"; \
	      COMMIT_SCOPE="total"; \
	    fi; \
	    echo ""; \
	    echo "==> Ready to merge release $$VERSION:"; \
	    echo "    PR:      #$$PRNUM ($$BRANCH -> $(DEFAULT_BASE_BRANCH))"; \
	    echo "    Commits: $$COMMIT_COUNT $$COMMIT_SCOPE"; \
	    echo "    Head:    $$RELEASE_HEAD"; \
	    if [ "$(CONFIRM)" = "0" ]; then \
	      echo "==> CONFIRM=0 set - skipping confirmation prompt."; \
	    else \
	      printf '    Merge PR #%s into $(DEFAULT_BASE_BRANCH)? [y/N] ' "$$PRNUM"; \
	      read -r CONFIRM_ANSWER || CONFIRM_ANSWER=""; \
	      case "$$CONFIRM_ANSWER" in \
	        y|Y|yes|YES) ;; \
	        *) echo "==> Merge aborted."; exit 1;; \
	      esac; \
	    fi; \
	    echo ""; \
	    echo "==> Merging PR #$$PRNUM…"; \
	    if ! gh pr merge $(call _repo_flag) "$$PRNUM" --merge --match-head-commit "$$RELEASE_HEAD"; then \
	      echo "ERROR: Merge failed! Branch NOT deleted."; \
	      exit 1; \
	    fi; \
	  else \
	    echo "ERROR: PR #$$PRNUM state is '$$PR_STATE' (expected OPEN or MERGED); refusing to continue."; \
	    exit 1; \
	  fi; \
	  echo "==> Tag to be released: $$VERSION"; \
	  echo ""; \
	  echo "==> Checking for release workflow..."; \
	  sleep 2; \
	  WORKFLOW_TSV=""; \
	  for i in $$(seq 1 10); do \
	    WORKFLOW_TSV="$$(gh run list $(call _repo_flag) --workflow=release.yml \
	      --event pull_request --commit "$$RELEASE_HEAD" --limit 1 \
	      --json databaseId,status,conclusion,name,createdAt,displayTitle,headBranch,event \
	      --jq '.[0] | $(_run_tsv)')" ; \
	    if [ -n "$$WORKFLOW_TSV" ]; then break; fi; \
	    echo "==> Waiting for workflow to start... (attempt $$i/10)"; \
	    sleep 2; \
	  done; \
	  if [ -z "$$WORKFLOW_TSV" ]; then \
	    echo "ERROR: Could not identify the release workflow run for $$VERSION."; \
	    echo "    Check manually: gh run list --workflow=release.yml"; \
	    echo "    Then re-run 'make merge-release' to resume watching and cleanup."; \
	    exit 1; \
	  fi; \
	  IFS=$$'\t' read -r RUN_ID STATUS CONCLUSION CREATED HBRANCH EVENT TITLE <<< "$$WORKFLOW_TSV" || true; \
	  echo "==> Release workflow found"; \
	  echo "    Run ID: #$$RUN_ID"; \
	  echo "    Title: $$TITLE"; \
	  echo "    Event: $$EVENT"; \
	  echo "    Branch: $$HBRANCH"; \
	  echo "    Status: $$STATUS"; \
	  echo "    Started: $$CREATED"; \
	  WORKFLOW_SUCCESS=0; \
	  if [ "$$STATUS" != "completed" ]; then \
	    echo ""; \
	    echo "==> Watching release workflow #$$RUN_ID..."; \
	    echo "    (Ctrl+C stops watching; re-run 'make merge-release' to resume)"; \
	    echo ""; \
	    START_TIME=$$(date +%s); \
	    if gh run watch $(call _repo_flag) "$$RUN_ID" --exit-status; then \
	      WATCH_STATUS=0; \
	    else \
	      WATCH_STATUS=$$?; \
	    fi; \
	    TOTAL_TIME=$$(($$(date +%s) - START_TIME)); \
	    if [ $$WATCH_STATUS -eq 0 ]; then \
	      echo "OK: Release workflow completed! (took $$(printf "%02d:%02d" $$((TOTAL_TIME/60)) $$((TOTAL_TIME%60))))"; \
	      WORKFLOW_SUCCESS=1; \
	    elif [ $$WATCH_STATUS -eq 130 ]; then \
	      echo ""; \
	      echo "WARN: Monitoring cancelled - the workflow may still be running."; \
	      echo "    Re-run 'make merge-release' to resume."; \
	      exit 1; \
	    else \
	      FINAL_CONCLUSION="$$(gh run view $(call _repo_flag) "$$RUN_ID" --json conclusion --jq '.conclusion // ""' 2>/dev/null || true)"; \
	      if [ "$$FINAL_CONCLUSION" = "success" ]; then \
	        echo "OK: Release workflow completed! (took $$(printf "%02d:%02d" $$((TOTAL_TIME/60)) $$((TOTAL_TIME%60))))"; \
	        WORKFLOW_SUCCESS=1; \
	      else \
	        echo "ERROR: Release workflow failed (conclusion: $${FINAL_CONCLUSION:-unknown})"; \
	        gh run view $(call _repo_flag) "$$RUN_ID" || true; \
	      fi; \
	    fi; \
	  else \
	    echo "==> Workflow already completed: $$CONCLUSION"; \
	    WORKFLOW_SUCCESS=$$( [ "$$CONCLUSION" = "success" ] && echo 1 || echo 0 ); \
	    gh run view $(call _repo_flag) "$$RUN_ID" || true; \
	  fi; \
	  echo ""; \
	  if [ "$${WORKFLOW_SUCCESS:-0}" -eq 1 ]; then \
	    echo "==> Cleaning up: deleting branch $$BRANCH..."; \
	    git checkout $(DEFAULT_BASE_BRANCH); \
	    git pull --ff-only; \
	    if ! git merge-base --is-ancestor "$$RELEASE_HEAD" "$(DEFAULT_BASE_BRANCH)"; then \
	      echo "ERROR: $$BRANCH is not contained in $(DEFAULT_BASE_BRANCH); leaving both branch refs intact."; \
	      exit 1; \
	    fi; \
	    if ! REMOTE_CLEANUP_INFO="$$(git ls-remote --heads "$$PUSH_REMOTE" "refs/heads/$$BRANCH")"; then \
	      echo "ERROR: Unable to query $$PUSH_REMOTE/$$BRANCH for safe cleanup."; \
	      exit 1; \
	    fi; \
	    if [ -n "$$REMOTE_CLEANUP_INFO" ] && [ "$$(printf '%s\n' "$$REMOTE_CLEANUP_INFO" | cut -f1)" != "$$RELEASE_HEAD" ]; then \
	      echo "ERROR: $$PUSH_REMOTE/$$BRANCH advanced after merge; leaving both branch refs intact."; \
	      exit 1; \
	    fi; \
	    if [ -n "$$REMOTE_CLEANUP_INFO" ] && ! git push \
	      --force-with-lease="refs/heads/$$BRANCH:$$RELEASE_HEAD" \
	      "$$PUSH_REMOTE" ":refs/heads/$$BRANCH"; then \
	      echo "ERROR: Unable to delete remote branch $$PUSH_REMOTE/$$BRANCH; local branch kept for retry."; \
	      exit 1; \
	    fi; \
	    if ! git branch -d "$$BRANCH"; then \
	      echo "ERROR: Unable to delete local branch $$BRANCH (remote branch already deleted)."; \
	      exit 1; \
	    fi; \
	    echo "OK: Branch cleanup complete"; \
	    RELEASE_URL="$$(gh release view $(call _repo_flag) "$$VERSION" --json url --jq '.url' 2>/dev/null || true)"; \
	    echo ""; \
	    if [ -n "$$RELEASE_URL" ]; then \
	      echo "OK: Release $$VERSION published: $$RELEASE_URL"; \
	    else \
	      echo "OK: Release $$VERSION complete. View it with: gh release view $$VERSION"; \
	    fi; \
	  else \
	    echo "WARN: Workflow did not succeed - keeping branch $$BRANCH for debugging"; \
	    echo "    Inspect the failed run, then re-run the workflow and 'make merge-release' to resume,"; \
	    echo "    or clean up manually:"; \
	    echo "      git branch -d $$BRANCH"; \
	    echo "      git push origin --delete $$BRANCH"; \
	    exit 1; \
	  fi; \
	}

.PHONY: start-dev open-pr merge-release test-release-automation

# Exercise release automation in isolated temporary Git repositories.
test-release-automation:
	@"$(packaging_scripts_dir)/test-release-automation.sh"
