# Private release automation (encrypted with git-crypt)
# This file contains release workflow targets only available to maintainers

# -------- Release flow helpers (gh CLI) --------
DEFAULT_BASE_BRANCH := main
REPO ?=
current_rel_branch = $(shell git branch --show-current)

define _require_clean
	@if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "❌ Working tree not clean. Commit/stash changes first."; exit 1; \
	fi
endef

define _require_gh
	@if ! command -v gh >/dev/null 2>&1; then \
		echo "❌ GitHub CLI (gh) not found. Install: https://cli.github.com/"; exit 1; \
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
	  echo "❌ VERSION must look like v1.2.3 or v1.2.3-rc.1 (got '$$VERSION')"; \
	  exit 1; \
	fi; \
	REL_BRANCH="dev/$$VERSION"
endef

define _repo_flag
$(if $(REPO),--repo $(REPO),)
endef

# ==================== Release Targets ====================

start-dev:
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  $(call _read_and_validate_version); \
	  git fetch origin; \
	  git checkout $(DEFAULT_BASE_BRANCH); \
	  git pull --ff-only; \
	  if git show-ref --verify --quiet "refs/heads/$$REL_BRANCH"; then \
	    echo "ℹ️  Branch $$REL_BRANCH already exists, checking it out…"; \
	    git checkout "$$REL_BRANCH"; \
	  else \
	    echo "Creating branch $$REL_BRANCH from $(DEFAULT_BASE_BRANCH)…"; \
	    git checkout -b "$$REL_BRANCH" "$(DEFAULT_BASE_BRANCH)"; \
	    git push -u origin "$$REL_BRANCH"; \
	  fi; \
	  echo "✅ Ready on branch $$REL_BRANCH"; \
	}

changelog:
	@$(call _require_clean)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$'; then \
	    echo "❌ Not on a dev/v* release branch (got '$$BRANCH')."; \
	    echo "💡 Run 'make start-dev VERSION=v1.2.3' first."; \
	    exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  DATE="$$(date -u +%Y-%m-%d)"; \
	  REPO="$${GITHUB_REPOSITORY:-$$(git remote get-url origin 2>/dev/null | sed -E 's#.*github\.com[:/]##; s#\.git$$##')}"; \
	  echo "📝 Generating changelog for $$VERSION ($$DATE)..."; \
	  echo "📦 Repository: $$REPO"; \
	  echo ""; \
	  PREV_TAG="$$(git tag --list 'v*' --sort=-v:refname | grep -v "^$$VERSION$$" | head -n1 || echo "")"; \
	  if [ -n "$$PREV_TAG" ]; then \
	    echo "📍 Changes since $$PREV_TAG"; \
	    COMMIT_RANGE="$${PREV_TAG}..HEAD"; \
	  else \
	    echo "📍 All commits (no previous tag found)"; \
	    COMMIT_RANGE=""; \
	  fi; \
	  BODY_FILE="$$(mktemp)"; \
	  ./packaging/scripts/changelog-entry.sh "$$VERSION" "$$PREV_TAG" "$$COMMIT_RANGE" "$$REPO" > "$$BODY_FILE"; \
	  { \
	    echo ""; \
	    echo "## $$VERSION — $$DATE"; \
	    echo ""; \
	    cat "$$BODY_FILE"; \
	    echo ""; \
	  } > new_entry.md; \
	  if [ -f CHANGELOG.md ]; then \
	    if grep -q "^## $$VERSION —" CHANGELOG.md; then \
	      echo "⚠️  Version $$VERSION already exists in CHANGELOG.md, updating..."; \
	      awk -v ver="$$VERSION" ' \
	        /^## / { \
	          if ($$2 == ver) { in_section=1; next } \
	          else if (in_section) { in_section=0 } \
	        } \
	        !in_section { print } \
	      ' CHANGELOG.md > CHANGELOG.tmp; \
	      cat new_entry.md CHANGELOG.tmp > CHANGELOG.md; \
	      rm CHANGELOG.tmp; \
	    else \
	      cat new_entry.md CHANGELOG.md > CHANGELOG.tmp; \
	      mv CHANGELOG.tmp CHANGELOG.md; \
	    fi; \
	  else \
	    echo "# Changelog" > CHANGELOG.md; \
	    echo "" >> CHANGELOG.md; \
	    cat new_entry.md >> CHANGELOG.md; \
	  fi; \
	  rm -f new_entry.md "$$BODY_FILE"; \
	  echo ""; \
	  echo "✅ CHANGELOG.md updated for $$VERSION"; \
	  echo ""; \
	  echo "📄 Preview:"; \
	  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; \
	  head -n 30 CHANGELOG.md; \
	  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; \
	  echo ""; \
	  echo "📦 Committing changes..."; \
	  git add CHANGELOG.md; \
	  git commit -m "changelog"; \
	  git push; \
	  echo "✅ Changes committed"; \
	  echo ""; \
	}

rebuild-changelog:
	@echo "⚠️  WARNING: This will OVERWRITE your entire CHANGELOG.md file!"
	@echo "   Press Ctrl+C to cancel, or Enter to continue..."
	@read -r _
	@{ \
	  set -euo pipefail; \
	  REPO="$${GITHUB_REPOSITORY:-$$(git remote get-url origin 2>/dev/null | sed -E 's#.*github\.com[:/]##; s#\.git$$##')}"; \
	  echo "📝 Rebuilding entire changelog history..."; \
	  echo "📦 Repository: $$REPO"; \
	  echo ""; \
	  TAGS="$$(git tag --list 'v*' --sort=-v:refname)"; \
	  if [ -z "$$TAGS" ]; then \
	    echo "❌ No version tags found."; exit 1; \
	  fi; \
	  echo "# Changelog" > CHANGELOG.md; \
	  echo "" >> CHANGELOG.md; \
	  echo "$$TAGS" | while IFS= read -r VERSION; do \
	    [ -z "$$VERSION" ] && continue; \
	    echo "Processing $$VERSION..."; \
	    DATE="$$(git log -1 --format=%ai "$$VERSION" | cut -d' ' -f1)"; \
	    PREV_TAG="$$(git tag --list 'v*' --sort=-v:refname | grep -A1 "^$$VERSION$$" | tail -n1)"; \
	    if [ "$$PREV_TAG" = "$$VERSION" ]; then PREV_TAG=""; fi; \
	    if [ -n "$$PREV_TAG" ]; then \
	      COMMIT_RANGE="$${PREV_TAG}..$$VERSION"; \
	    else \
	      COMMIT_RANGE="$$VERSION"; \
	    fi; \
	    echo "" >> CHANGELOG.md; \
	    echo "## $$VERSION — $$DATE" >> CHANGELOG.md; \
	    echo "" >> CHANGELOG.md; \
	    ./packaging/scripts/changelog-entry.sh "$$VERSION" "$$PREV_TAG" "$$COMMIT_RANGE" "$$REPO" >> CHANGELOG.md; \
	  done; \
	  echo ""; \
	  echo "✅ Changelog rebuilt for all versions!"; \
	  echo ""; \
	  echo "📄 Preview:"; \
	  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; \
	  head -n 50 CHANGELOG.md; \
	  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; \
	  echo ""; \
	}

open-pr: generate
	@$(call _require_clean)
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$'; then \
	    echo "❌ Not on a dev/v* release branch (got '$$BRANCH')."; exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  BASE_BRANCH="$(DEFAULT_BASE_BRANCH)"; \
	  PRNUM="$$(gh pr list $(call _repo_flag) --base "$$BASE_BRANCH" --head "$$BRANCH" --state open --json number --jq '.[0].number' || true)"; \
	  CREATED=0; \
	  if [ -n "$$PRNUM" ] && [ "$$PRNUM" != "null" ]; then \
	    echo "ℹ️  An open PR (#$$PRNUM) from $$BRANCH -> $$BASE_BRANCH already exists."; \
	  else \
	    echo "🔁 Opening PR: $$BRANCH -> $$BASE_BRANCH…"; \
	    PR_BODY_FILE="$$(mktemp)"; \
	    awk -v ver="$$VERSION" ' \
	      /^## / { \
	        if ($$2 == ver) { in_section=1; print; next } \
	        else if (in_section) { exit } \
	      } \
	      in_section { print } \
	    ' CHANGELOG.md > "$$PR_BODY_FILE"; \
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
	  echo "🔍 Waiting for CI checks to register..."; \
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
	    echo "⚠️  No CI checks detected after 15s. Skipping check wait."; \
	    echo "💡 Checks might start later - monitor the PR manually."; \
	  else \
	    echo "⏳ Waiting for checks to complete on PR #$$PRNUM…"; \
	    echo "   (Press Ctrl+C to cancel)"; \
	    echo ""; \
	    START_TIME=$$(date +%s); \
	    gh pr checks $(call _repo_flag) "$$PRNUM" --watch --interval 5; \
	    CHECK_STATUS=$$?; \
	    TOTAL_TIME=$$(( $$(date +%s) - $$START_TIME )); \
	    echo ""; \
	    if [ $$CHECK_STATUS -eq 0 ]; then \
	      echo "✅ All checks passed! (took $$(printf "%02d:%02d" $$((TOTAL_TIME/60)) $$((TOTAL_TIME%60))))"; \
	    else \
	      echo "⚠️  gh pr checks exited with code $$CHECK_STATUS"; \
	      echo "   Re-checking final status..."; \
	      gh pr checks $(call _repo_flag) "$$PRNUM" || true; \
	      echo ""; \
	      echo "❌ Checks failed or monitoring was interrupted"; \
	    fi; \
	  fi; \
	  echo ""; \
	  gh pr view $(call _repo_flag) "$$PRNUM" --web || true; \
	}

merge-release:
	@$(call _require_gh)
	@{ \
	  set -euo pipefail; \
	  BRANCH="$$(git rev-parse --abbrev-ref HEAD)"; \
	  if ! echo "$$BRANCH" | grep -qE '^dev/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$$'; then \
	    echo "❌ Current branch '$$BRANCH' is not a dev/v* release branch."; exit 1; \
	  fi; \
	  VERSION="$${BRANCH#dev/}"; \
	  PRNUM="$${PR:-$$(gh pr list $(call _repo_flag) --base main --head "$$BRANCH" --state open --json number --jq '.[0].number' || true)}"; \
	  if [ -z "$$PRNUM" ] || [ "$$PRNUM" = "null" ]; then echo "❌ No open PR from $$BRANCH to main."; exit 1; fi; \
	  echo "🔍 Checking status of PR #$$PRNUM…"; \
	  CHECK_OUTPUT="$$(gh pr checks $(call _repo_flag) "$$PRNUM" 2>&1 || true)"; \
	  if echo "$$CHECK_OUTPUT" | grep -q "no checks reported"; then \
	    echo "⚠️  No CI checks configured. Proceeding with merge."; \
	    echo "💡 Consider setting up GitHub Actions for automated testing."; \
	  elif ! gh pr checks $(call _repo_flag) "$$PRNUM" > /dev/null 2>&1; then \
	    echo "❌ Checks have not passed. Run 'make open-pr' to wait for checks."; \
	    exit 1; \
	  else \
	    echo "✅ All checks passed."; \
	  fi; \
	  echo ""; \
	  echo "📝 Add custom release notes? (opens editor)"; \
	  read -p "   [y/N]: " ADD_NOTES; \
	  MERGE_BODY=""; \
	  if [ "$$ADD_NOTES" = "y" ] || [ "$$ADD_NOTES" = "Y" ]; then \
	    NOTES_FILE="$$(mktemp)"; \
	    echo "# Add your release notes below (lines starting with # are ignored)" > "$$NOTES_FILE"; \
	    echo "# Save and close the editor to continue" >> "$$NOTES_FILE"; \
	    echo "" >> "$$NOTES_FILE"; \
	    $${EDITOR:-nano} "$$NOTES_FILE"; \
	    MERGE_BODY="$$(grep -v '^#' "$$NOTES_FILE" | sed '/^[[:space:]]*$$/d')"; \
	    rm -f "$$NOTES_FILE"; \
	    if [ -n "$$MERGE_BODY" ]; then \
	      echo "✅ Release notes added"; \
	    else \
	      echo "ℹ️  No notes added (empty)"; \
	    fi; \
	  fi; \
	  TRIGGER_MARK=$$(( $$(date -u +%s) - 30 )); \
	  echo ""; \
	  echo "🔀 Merging PR #$$PRNUM…"; \
	  MERGE_SUCCESS=0; \
	  if [ -n "$$MERGE_BODY" ]; then \
	    gh pr merge $(call _repo_flag) "$$PRNUM" --merge --body "$$MERGE_BODY" && MERGE_SUCCESS=1; \
	  else \
	    gh pr merge $(call _repo_flag) "$$PRNUM" --merge && MERGE_SUCCESS=1; \
	  fi; \
	  if [ $$MERGE_SUCCESS -eq 0 ]; then \
	    echo "❌ Merge failed! Branch NOT deleted."; \
	    exit 1; \
	  fi; \
	  echo "🔖 Tag to be released: $$VERSION"; \
	  echo ""; \
	  echo "🔍 Checking for release workflow..."; \
	  sleep 2; \
	  WORKFLOW_RUN=""; \
	  for i in $$(seq 1 10); do \
	    WORKFLOW_RUN="$$(gh run list $(call _repo_flag) --workflow=release.yml --limit=20 \
	      --json databaseId,status,conclusion,name,createdAt,displayTitle,headBranch,event \
	      | jq -c --arg ver "$$VERSION" --arg main "main" --arg branch "$$BRANCH" --argjson t $$TRIGGER_MARK \
	        '[ .[] \
	           | select((.createdAt|fromdateiso8601) >= $$t) \
	           | select((.headBranch == $$main) or (.headBranch == $$branch) or ((.displayTitle // .name) | test($$ver))) \
	         ] \
	         | .[0]')" ; \
	    if [ -n "$$WORKFLOW_RUN" ] && [ "$$WORKFLOW_RUN" != "null" ]; then break; fi; \
	    echo "  Waiting for workflow to start... (attempt $$i/10)"; \
	    sleep 2; \
	  done; \
	  if [ -z "$$WORKFLOW_RUN" ] || [ "$$WORKFLOW_RUN" = "null" ]; then \
	    WORKFLOW_RUN="$$(gh run list $(call _repo_flag) --workflow=release.yml --limit=20 \
	      --json databaseId,status,conclusion,name,createdAt,displayTitle,headBranch,event \
	      | jq -c --argjson t $$TRIGGER_MARK \
	        '[ .[] | select((.createdAt|fromdateiso8601) >= $$t) ] | .[0]')" ; \
	  fi; \
	  if [ -n "$$WORKFLOW_RUN" ] && [ "$$WORKFLOW_RUN" != "null" ]; then \
	    RUN_ID="$$(echo "$$WORKFLOW_RUN" | jq -r '.databaseId')"; \
	    STATUS="$$(echo "$$WORKFLOW_RUN" | jq -r '.status')"; \
	    CONCLUSION="$$(echo "$$WORKFLOW_RUN" | jq -r '.conclusion // "n/a"')"; \
	    CREATED="$$(echo "$$WORKFLOW_RUN" | jq -r '.createdAt')"; \
	    TITLE="$$(echo "$$WORKFLOW_RUN" | jq -r '.displayTitle // .name')"; \
	    HBRANCH="$$(echo "$$WORKFLOW_RUN" | jq -r '.headBranch // "n/a"')"; \
	    EVENT="$$(echo "$$WORKFLOW_RUN" | jq -r '.event // "n/a"')"; \
	    echo "📊 Release workflow found"; \
	    echo "   Run ID: #$$RUN_ID"; \
	    echo "   Title: $$TITLE"; \
	    echo "   Event: $$EVENT"; \
	    echo "   Branch: $$HBRANCH"; \
	    echo "   Status: $$STATUS"; \
	    echo "   Started: $$CREATED"; \
	    if [ "$$STATUS" = "in_progress" ] || [ "$$STATUS" = "queued" ] || [ "$$STATUS" = "waiting" ]; then \
	      echo ""; \
	      echo "⏳ Watching release workflow..."; \
	      echo "   (Press Ctrl+C to cancel)"; \
	      echo ""; \
	      if [ -t 1 ]; then SAVED_STTY=$$(stty -g); stty -echo -icanon min 0 time 0; fi; \
	      cleanup_workflow() { \
	        [ -n "$$TIMER_PID" ] && kill $$TIMER_PID 2>/dev/null || true; \
	        [ -n "$$TIMER_PID" ] && wait $$TIMER_PID 2>/dev/null || true; \
	        [ -n "$$WATCH_PID" ] && kill $$WATCH_PID 2>/dev/null || true; \
	        [ -n "$$WATCH_PID" ] && wait $$WATCH_PID 2>/dev/null || true; \
	        stty "$$SAVED_STTY" 2>/dev/null || true; \
	        printf "\r\033[K"; \
	      }; \
	      trap 'cleanup_workflow; exit 130' INT TERM; \
	      START_TIME=$$(date +%s); \
	      TIMER_PID=""; WATCH_PID=""; \
	      ( \
	        while true; do \
	          ELAPSED=$$(($$(date +%s) - START_TIME)); \
	          RUN_INFO="$$(gh run view $(call _repo_flag) "$$RUN_ID" --json status,conclusion 2>/dev/null || echo '')"; \
	          if [ -n "$$RUN_INFO" ]; then \
	            CURRENT_STATUS="$$(echo "$$RUN_INFO" | jq -r '.status // "unknown"')"; \
	            printf "\r⏱️  Elapsed: %02d:%02d | Status: %-15s" $$((ELAPSED/60)) $$((ELAPSED%60)) "$$CURRENT_STATUS"; \
	          else \
	            printf "\r⏱️  Elapsed: %02d:%02d | Status: checking...      " $$((ELAPSED/60)) $$((ELAPSED%60)); \
	          fi; \
	          sleep 2; \
	        done \
	      ) & \
	      TIMER_PID=$$!; \
	      ( gh run watch $(call _repo_flag) "$$RUN_ID" ) & \
	      WATCH_PID=$$!; \
	      wait $$WATCH_PID; \
	      WATCH_STATUS=$$?; \
	      cleanup_workflow; \
	      trap - INT TERM; \
	      TOTAL_TIME=$$(($$(date +%s) - START_TIME)); \
	      if [ $$WATCH_STATUS -eq 0 ]; then \
	        echo "✅ Release workflow completed! (took $$(printf "%02d:%02d" $$((TOTAL_TIME/60)) $$((TOTAL_TIME%60))))"; \
	      else \
	        echo "❌ Release workflow failed or was cancelled"; \
	      fi; \
	      echo ""; \
	      gh run view $(call _repo_flag) "$$RUN_ID"; \
	    else \
	      echo "   Workflow already completed: $$CONCLUSION"; \
	      gh run view $(call _repo_flag) "$$RUN_ID"; \
	    fi; \
	  else \
	    echo "⚠️  No release workflow found. The workflow may:"; \
	    echo "   • Not exist (no .github/workflows/release.yml)"; \
	    echo "   • Not be triggered by this merge"; \
	    echo "   • Take longer to start than expected"; \
	    echo "💡 Check manually: gh run list --workflow=release.yml"; \
	  fi; \
	  echo ""; \
	  echo "🗑️  Cleaning up: deleting branch $$BRANCH..."; \
	  git checkout $(DEFAULT_BASE_BRANCH) 2>/dev/null || git checkout main; \
	  git pull --ff-only; \
	  git branch -d "$$BRANCH" 2>/dev/null || true; \
	  git push origin --delete "$$BRANCH" 2>/dev/null || echo "   (remote branch already deleted)"; \
	  echo "✅ Branch cleanup complete"; \
	}

.PHONY: start-dev changelog rebuild-changelog open-pr merge-release
