# Private release automation (encrypted with git-crypt)
# This file contains release workflow targets only available to maintainers

# -------- Release flow helpers (gh CLI) --------
DEFAULT_BASE_BRANCH := main
REPO ?=
current_rel_branch = $(shell git branch --show-current)

define _require_clean
	@if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo " Working tree not clean. Commit/stash changes first."; exit 1; \
	fi
endef

define _require_gh
	@if ! command -v gh >/dev/null 2>&1; then \
		echo " GitHub CLI (gh) not found. Install: https://cli.github.com/"; exit 1; \
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
	if ! echo "$$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9\.-]+)?$$'; then \
	  echo " VERSION must look like v1.2.3 or v1.2.3-rc.1 (got '$$VERSION')"; \
	  exit 1; \
	fi; \
	REL_BRANCH="dev/$$VERSION"
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
	    echo "  Branch $$REL_BRANCH already exists, checking it out…"; \
	    git checkout "$$REL_BRANCH"; \
	  else \
	    echo "Creating branch $$REL_BRANCH from $(DEFAULT_BASE_BRANCH)…"; \
	    git checkout -b "$$REL_BRANCH" "$(DEFAULT_BASE_BRANCH)"; \
	    git push -u origin "$$REL_BRANCH"; \
	  fi; \
	  echo " Ready on branch $$REL_BRANCH"; \
	}

open-pr:
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$'; then \
	    echo " Not on a dev/v* release branch (got '$$BRANCH')."; exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  OPEN_PR_STATUS=0; \
	  PUSH_REMOTE="$$(git config --get "branch.$$BRANCH.remote" 2>/dev/null || echo origin)"; \
	  REMOTE_REF="refs/remotes/$$PUSH_REMOTE/$$BRANCH"; \
	  if ! REMOTE_INFO="$$(git ls-remote --heads "$$PUSH_REMOTE" "refs/heads/$$BRANCH")"; then \
	    echo " Unable to query $$PUSH_REMOTE/$$BRANCH; refusing to use a stale tracking ref."; \
	    exit 1; \
	  fi; \
	  if [ -z "$$REMOTE_INFO" ]; then \
	    echo " $$PUSH_REMOTE/$$BRANCH does not exist yet - publishing…"; \
	    git push -u "$$PUSH_REMOTE" "HEAD:refs/heads/$$BRANCH"; \
	  else \
	    if ! git fetch --quiet "$$PUSH_REMOTE" "+refs/heads/$$BRANCH:$$REMOTE_REF"; then \
	      echo " Unable to fetch $$PUSH_REMOTE/$$BRANCH; refusing to use a stale tracking ref."; \
	      exit 1; \
	    fi; \
	    BEHIND="$$(git rev-list --count "HEAD..$$REMOTE_REF")"; \
	    AHEAD="$$(git rev-list --count "$$REMOTE_REF..HEAD")"; \
	    if [ "$$BEHIND" -gt 0 ] && [ "$$AHEAD" -gt 0 ]; then \
	      echo " $$BRANCH has diverged from $$PUSH_REMOTE/$$BRANCH ($$BEHIND behind, $$AHEAD ahead)."; \
	      echo " Reconcile deliberately (rebase, then git push --force-with-lease), then re-run."; \
	      exit 1; \
	    fi; \
	    if [ "$$BEHIND" -gt 0 ]; then \
	      echo " $$BRANCH is $$BEHIND commit(s) behind $$PUSH_REMOTE/$$BRANCH."; \
	      echo " Reconcile first (e.g. git pull --rebase), then re-run."; \
	      exit 1; \
	    fi; \
	    if [ "$$AHEAD" -gt 0 ]; then \
	      echo " Publishing $$AHEAD local commit(s) to $$PUSH_REMOTE/$$BRANCH…"; \
	      git push "$$PUSH_REMOTE" "HEAD:refs/heads/$$BRANCH"; \
	    else \
	      echo " $$BRANCH already in sync with $$PUSH_REMOTE/$$BRANCH - nothing to push."; \
	    fi; \
	  fi; \
	  BASE_BRANCH="$(DEFAULT_BASE_BRANCH)"; \
	  PRNUM="$$(gh pr list $(call _repo_flag) --base "$$BASE_BRANCH" --head "$$BRANCH" --state open --json number --jq '.[0].number' || true)"; \
	  CREATED=0; \
	  if [ -n "$$PRNUM" ] && [ "$$PRNUM" != "null" ]; then \
	    echo "  An open PR (#$$PRNUM) from $$BRANCH -> $$BASE_BRANCH already exists."; \
	  else \
	    echo " Opening PR: $$BRANCH -> $$BASE_BRANCH…"; \
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
	    } > "$$PR_BODY_FILE"; \
	    gh pr create $(call _repo_flag) \
	      --base "$$BASE_BRANCH" \
	      --head "$$BRANCH" \
	      --title "Release $$VERSION" \
	      --body-file "$$PR_BODY_FILE"; \
	    rm -f "$$PR_BODY_FILE"; \
	    PRNUM="$$(gh pr list $(call _repo_flag) --base "$$BASE_BRANCH" --head "$$BRANCH" --state open --json number --jq '.[0].number')"; \
	    CREATED=1; \
	  fi; \
	  echo ""; \
	  echo " Waiting for CI checks to register..."; \
	  sleep 3; \
	  for i in 1 2 3 4 5; do \
	    CHECK_OUTPUT="$$(gh pr checks $(call _repo_flag) "$$PRNUM" 2>&1 || true)"; \
	    if ! echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	      break; \
	    fi; \
	    if [ $$i -lt 5 ]; then \
	      echo "  Retrying in 2s... (attempt $$i/5)"; \
	      sleep 2; \
	    fi; \
	  done; \
	  if echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	    echo "  No CI checks detected after 15s. Skipping check wait."; \
	    echo " Checks might start later - monitor the PR manually."; \
	  else \
	    echo " Waiting for checks to complete on PR #$$PRNUM…"; \
	    echo "   (Press Ctrl+C to cancel)"; \
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
	      echo " All checks passed! (took $$(printf "%02d:%02d" $$((TOTAL_TIME/60)) $$((TOTAL_TIME%60))))"; \
	    else \
	      echo "  gh pr checks exited with code $$CHECK_STATUS"; \
	      echo "   Re-checking final status..."; \
	      gh pr checks $(call _repo_flag) "$$PRNUM" || true; \
	      echo ""; \
	      echo " Checks failed or monitoring was interrupted"; \
	      OPEN_PR_STATUS=1; \
	    fi; \
	  fi; \
	  echo ""; \
	  PR_URL="$$(gh pr view $(call _repo_flag) "$$PRNUM" --json url --jq '.url' 2>/dev/null || true)"; \
	  if gh pr view $(call _repo_flag) "$$PRNUM" --web >/dev/null 2>&1; then \
	    echo " Opened PR #$$PRNUM in your browser."; \
	  elif [ -n "$$PR_URL" ]; then \
	    echo " Couldn't open a browser. View PR #$$PRNUM here:"; \
	    echo "   $$PR_URL"; \
	  else \
	    echo " Couldn't open a browser. View it with: gh pr view $$PRNUM --web"; \
	  fi; \
	  exit "$$OPEN_PR_STATUS"; \
	}

merge-release:
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$'; then \
	    echo " Current branch '$$BRANCH' is not a dev/v* release branch."; exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  PRNUM="$${PR:-$$(gh pr list $(call _repo_flag) --base "$(DEFAULT_BASE_BRANCH)" --head "$$BRANCH" --state open --json number --jq '.[0].number' || true)}"; \
	  if [ -z "$$PRNUM" ] || [ "$$PRNUM" = "null" ]; then echo " No open PR from $$BRANCH to main."; exit 1; fi; \
	  echo " Checking status of PR #$$PRNUM…"; \
	  CHECK_OUTPUT="$$(gh pr checks $(call _repo_flag) "$$PRNUM" 2>&1 || true)"; \
	  if echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	    echo " No CI checks are reported for PR #$$PRNUM; refusing to merge."; \
	    echo " Wait for checks to register or inspect the PR, then re-run."; \
	    exit 1; \
	  elif ! gh pr checks $(call _repo_flag) "$$PRNUM" > /dev/null 2>&1; then \
	    echo " Checks have not passed. Run 'make open-pr' to wait for checks."; \
	    exit 1; \
	  else \
	    echo " All checks passed."; \
	  fi; \
	  PUSH_REMOTE="$$(git config --get "branch.$$BRANCH.remote" 2>/dev/null || echo origin)"; \
	  if ! REMOTE_INFO="$$(git ls-remote --heads "$$PUSH_REMOTE" "refs/heads/$$BRANCH")"; then \
	    echo " Unable to query $$PUSH_REMOTE/$$BRANCH; refusing to merge an unsynchronized PR."; \
	    exit 1; \
	  fi; \
	  if [ -z "$$REMOTE_INFO" ]; then \
	    echo " $$PUSH_REMOTE/$$BRANCH does not exist; refusing to merge."; \
	    exit 1; \
	  fi; \
	  LOCAL_HEAD="$$(git rev-parse HEAD)"; \
	  REMOTE_HEAD="$$(printf '%s\n' "$$REMOTE_INFO" | cut -f1)"; \
	  if [ "$$LOCAL_HEAD" != "$$REMOTE_HEAD" ]; then \
	    echo " Local HEAD ($$LOCAL_HEAD) is not synchronized with $$PUSH_REMOTE/$$BRANCH ($$REMOTE_HEAD)."; \
	    echo " Push or reconcile the release branch, then re-run."; \
	    exit 1; \
	  fi; \
	  PR_HEAD_INFO="$$(gh pr view $(call _repo_flag) "$$PRNUM" --json baseRefName,headRefName,headRefOid --jq '[.baseRefName, .headRefName, .headRefOid] | @tsv')"; \
	  IFS=$$'\t' read -r PR_BASE_BRANCH PR_HEAD_BRANCH PR_HEAD_OID <<< "$$PR_HEAD_INFO"; \
	  if [ "$$PR_BASE_BRANCH" != "$(DEFAULT_BASE_BRANCH)" ] || [ "$$PR_HEAD_BRANCH" != "$$BRANCH" ] || [ "$$PR_HEAD_OID" != "$$LOCAL_HEAD" ]; then \
	    echo " PR #$$PRNUM is not synchronized with local $$BRANCH ($$LOCAL_HEAD)."; \
	    echo "   PR: $$PR_HEAD_BRANCH -> $$PR_BASE_BRANCH ($$PR_HEAD_OID)"; \
	    echo " Push or reconcile the release branch, then re-run."; \
	    exit 1; \
	  fi; \
	  TRIGGER_MARK=$$(( $$(date -u +%s) - 30 )); \
	  echo ""; \
	  echo " Merging PR #$$PRNUM…"; \
	  MERGE_SUCCESS=0; \
	  if ! gh pr merge --help 2>&1 | grep -q -- '--match-head-commit'; then \
	    echo " gh must support --match-head-commit for race-safe release merges."; \
	    echo " Update GitHub CLI, then re-run."; \
	    exit 1; \
	  fi; \
	  gh pr merge $(call _repo_flag) "$$PRNUM" --merge --match-head-commit "$$LOCAL_HEAD" && MERGE_SUCCESS=1; \
	  if [ $$MERGE_SUCCESS -eq 0 ]; then \
	    echo " Merge failed! Branch NOT deleted."; \
	    exit 1; \
	  fi; \
	  echo " Tag to be released: $$VERSION"; \
	  echo ""; \
	  echo " Checking for release workflow..."; \
	  sleep 2; \
	  WORKFLOW_TSV=""; \
	  for i in $$(seq 1 10); do \
	    WORKFLOW_TSV="$$(VER="$$VERSION" BR="$$BRANCH" T="$$TRIGGER_MARK" \
	      gh run list $(call _repo_flag) --workflow=release.yml --limit=20 \
	      --json databaseId,status,conclusion,name,createdAt,displayTitle,headBranch,event \
	      --jq '[ .[] \
	              | select((.createdAt|fromdateiso8601) >= ($$ENV.T|tonumber)) \
	              | select((.headBranch == "main") or (.headBranch == $$ENV.BR) or ((.displayTitle // .name) | contains($$ENV.VER))) \
	            ] \
	            | .[0] | $(_run_tsv)')" ; \
	    if [ -n "$$WORKFLOW_TSV" ]; then break; fi; \
	    echo "  Waiting for workflow to start... (attempt $$i/10)"; \
	    sleep 2; \
	  done; \
	  if [ -z "$$WORKFLOW_TSV" ]; then \
	    WORKFLOW_TSV="$$(T="$$TRIGGER_MARK" \
	      gh run list $(call _repo_flag) --workflow=release.yml --limit=20 \
	      --json databaseId,status,conclusion,name,createdAt,displayTitle,headBranch,event \
	      --jq '[ .[] | select((.createdAt|fromdateiso8601) >= ($$ENV.T|tonumber)) ] | .[0] | $(_run_tsv)')" ; \
	  fi; \
	  if [ -n "$$WORKFLOW_TSV" ]; then \
	    IFS=$$'\t' read -r RUN_ID STATUS CONCLUSION CREATED HBRANCH EVENT TITLE <<< "$$WORKFLOW_TSV" || true; \
	    echo " Release workflow found"; \
	    echo "   Run ID: #$$RUN_ID"; \
	    echo "   Title: $$TITLE"; \
	    echo "   Event: $$EVENT"; \
	    echo "   Branch: $$HBRANCH"; \
	    echo "   Status: $$STATUS"; \
	    echo "   Started: $$CREATED"; \
	    if [ "$$STATUS" = "in_progress" ] || [ "$$STATUS" = "queued" ] || [ "$$STATUS" = "waiting" ]; then \
	      echo ""; \
	      echo " Watching release workflow..."; \
	      echo "   (Press Ctrl+C to cancel)"; \
	      echo ""; \
	      SAVED_STTY=""; \
	      if [ -t 1 ]; then SAVED_STTY=$$(stty -g); stty -echo -icanon min 0 time 0; fi; \
	      cleanup_workflow() { \
	        [ -n "$$TIMER_PID" ] && kill $$TIMER_PID 2>/dev/null || true; \
	        [ -n "$$TIMER_PID" ] && wait $$TIMER_PID 2>/dev/null || true; \
	        [ -n "$$WATCH_PID" ] && kill $$WATCH_PID 2>/dev/null || true; \
	        [ -n "$$WATCH_PID" ] && wait $$WATCH_PID 2>/dev/null || true; \
	        if [ -n "$$SAVED_STTY" ]; then stty "$$SAVED_STTY" 2>/dev/null || true; fi; \
	        printf "\r\033[K"; \
	      }; \
	      trap 'cleanup_workflow; exit 130' INT TERM; \
	      START_TIME=$$(date +%s); \
	      TIMER_PID=""; WATCH_PID=""; \
	      ( \
	        while true; do \
	          ELAPSED=$$(($$(date +%s) - START_TIME)); \
	          CURRENT_STATUS="$$(gh run view $(call _repo_flag) "$$RUN_ID" --json status --jq '.status // "unknown"' 2>/dev/null || echo '')"; \
	          if [ -n "$$CURRENT_STATUS" ]; then \
	            printf "\r  Elapsed: %02d:%02d | Status: %-15s" $$((ELAPSED/60)) $$((ELAPSED%60)) "$$CURRENT_STATUS"; \
	          else \
	            printf "\r  Elapsed: %02d:%02d | Status: checking...      " $$((ELAPSED/60)) $$((ELAPSED%60)); \
	          fi; \
	          sleep 2; \
	        done \
	      ) & \
	      TIMER_PID=$$!; \
	      ( gh run watch $(call _repo_flag) "$$RUN_ID" ) & \
	      WATCH_PID=$$!; \
	      if wait $$WATCH_PID; then \
	        WATCH_STATUS=0; \
	      else \
	        WATCH_STATUS=$$?; \
	      fi; \
	      cleanup_workflow; \
	      trap - INT TERM; \
	      TOTAL_TIME=$$(($$(date +%s) - START_TIME)); \
	      if [ $$WATCH_STATUS -eq 0 ]; then \
	        echo " Release workflow completed! (took $$(printf "%02d:%02d" $$((TOTAL_TIME/60)) $$((TOTAL_TIME%60))))"; \
	        FINAL_CONCLUSION="$$(gh run view $(call _repo_flag) "$$RUN_ID" --json conclusion --jq '.conclusion // ""')"; \
	        WORKFLOW_SUCCESS=$$( [ "$$FINAL_CONCLUSION" = "success" ] && echo 1 || echo 0 ); \
	      else \
	        echo " Release workflow failed or was cancelled"; \
	        WORKFLOW_SUCCESS=0; \
	      fi; \
	      echo ""; \
	      gh run view $(call _repo_flag) "$$RUN_ID"; \
	    else \
	      echo "   Workflow already completed: $$CONCLUSION"; \
	      WORKFLOW_SUCCESS=$$( [ "$$CONCLUSION" = "success" ] && echo 1 || echo 0 ); \
	      gh run view $(call _repo_flag) "$$RUN_ID"; \
	    fi; \
	  else \
	    echo "  No release workflow found. The workflow may:"; \
	    echo "   • Not exist (no .github/workflows/release.yml)"; \
	    echo "   • Not be triggered by this merge"; \
	    echo "   • Take longer to start than expected"; \
	    echo " Check manually: gh run list --workflow=release.yml"; \
	    WORKFLOW_SUCCESS=0; \
	  fi; \
	  echo ""; \
	  if [ "$${WORKFLOW_SUCCESS:-0}" -eq 1 ]; then \
	    echo "  Cleaning up: deleting branch $$BRANCH..."; \
	    git checkout $(DEFAULT_BASE_BRANCH); \
	    git pull --ff-only; \
	    if ! git merge-base --is-ancestor "$$LOCAL_HEAD" "$(DEFAULT_BASE_BRANCH)"; then \
	      echo " $$BRANCH is not contained in $(DEFAULT_BASE_BRANCH); leaving both branch refs intact."; \
	      exit 1; \
	    fi; \
	    if ! REMOTE_CLEANUP_INFO="$$(git ls-remote --heads "$$PUSH_REMOTE" "refs/heads/$$BRANCH")"; then \
	      echo " Unable to query $$PUSH_REMOTE/$$BRANCH for safe cleanup."; \
	      exit 1; \
	    fi; \
	    if [ -n "$$REMOTE_CLEANUP_INFO" ] && [ "$$(printf '%s\n' "$$REMOTE_CLEANUP_INFO" | cut -f1)" != "$$LOCAL_HEAD" ]; then \
	      echo " $$PUSH_REMOTE/$$BRANCH advanced after merge; leaving both branch refs intact."; \
	      exit 1; \
	    fi; \
	    if ! git branch -d "$$BRANCH"; then \
	      echo " Unable to delete local branch $$BRANCH; remote branch was left intact."; \
	      exit 1; \
	    fi; \
	    if [ -n "$$REMOTE_CLEANUP_INFO" ] && ! git push \
	      --force-with-lease="refs/heads/$$BRANCH:$$LOCAL_HEAD" \
	      "$$PUSH_REMOTE" ":refs/heads/$$BRANCH"; then \
	      echo " Unable to delete remote branch $$PUSH_REMOTE/$$BRANCH."; \
	      exit 1; \
	    fi; \
	    echo " Branch cleanup complete"; \
	  else \
	    echo "  Workflow did not succeed - keeping branch $$BRANCH for debugging"; \
	    echo " After fixing issues, you can manually delete with:"; \
	    echo "   git branch -d $$BRANCH"; \
	    echo "   git push origin --delete $$BRANCH"; \
	    exit 1; \
	  fi; \
	}

.PHONY: start-dev open-pr merge-release test-release-automation

# Exercise release automation in isolated temporary Git repositories.
test-release-automation:
	@"$(packaging_scripts_dir)/test-release-automation.sh"
