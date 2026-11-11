---
name: finish-check
description: Run the LinuxIO quality gate that matches the files changed in the working tree, report the exact Make targets and results, then give the suggested commit message.
disable-model-invocation: true
allowed-tools: Bash(scripts/docs-for.sh *)
---

Changed files (tracked and untracked):

!`git status --porcelain --untracked-files=all`

Make processes already running on this machine:

!`pgrep -af '(^|/)make( |$)' || echo none`

Docs covering the changed files, from the Code column of `docs/README.md`:

!`scripts/docs-for.sh --changed || echo "scripts/docs-for.sh did not run; run it from the repository root"`

## Steps

1. If a Make process is listed above, stop and tell the user. Never run Make concurrently in the same worktree.
2. Classify the changed paths and pick one gate:
   - only `frontend/` → `make check-frontend-quiet`
   - only `backend/` → `make check-backend-quiet`
   - anything else, or both → `make test-quiet`
   - if `backend/bridge/apischema/` or `backend/bridge/internal/apigen/` changed → `make generate` first, then `make test-quiet`
3. Run directly, not through Make: `shellcheck` and `shfmt -d` on changed `.sh` files; `actionlint` if `.github/workflows/` changed.
4. On a failure, read the log under `.cache/test-logs/` before rerunning. Separate failures this change introduced from pre-existing or environmental ones.
5. If anything under `backend/common/btrfs` that issues ioctls or writes disks changed: ask the user to run `make test-btrfs-integration` in their own terminal and say kernel-level verification is pending until they paste a PASS.
6. Go through the docs listed above. For each one marked "not changed", update it if this change altered what it describes, otherwise say it is still accurate. No doc describes the directories listed as covered only by general docs or by no doc: general docs such as Handler Patterns set rules for that code without describing it. If the change added a subsystem, host integration, protocol or decision there, write the doc in `docs/` and add its row to `docs/README.md`, then run `make check-docs-quiet`.
7. Report the exact targets run and their results, and name any target that could not run. Do not claim browser or runtime coverage from unit tests.
8. End with the suggested commit message in Conventional Commits form: `type(scope): subject`, imperative, at most 72 characters; blank line; body paragraphs each on one unbroken line explaining what and why. No `Co-Authored-By` or any Claude attribution. Do not create the commit.
