# Frontend UI-Architecture Follow-ups (post API migration)

Status: open. ToDo #13. (#14, `filebrowser.chmod_batch`, is done — see §5.)

The 2026-07 API migration is complete: feature code talks to the backend
through the typed endpoint surface only (decision table in
`docs/api-contract.md`), invalidation is manifest-driven, the four transfer
jobs run on one descriptor-based engine (`hooks/backgroundJobs/useTransferJobs.ts`),
stream routes carry Go-generated request contracts (`LinuxIOStreamSchema`),
and `frontend/src/constants/apiLayering.test.ts` enforces all of it
mechanically.

What remains is **not** API spaghetti — none of these items mix data-access
paradigms or violate the guards. They are UI-architecture debt (how
components and hooks are composed) plus one backend surface gap. They are
listed here so each can be picked up as its own small, reviewable change.

---

## 1. Filebrowser controller prop-drilling

`hooks/filebrowser/useFileBrowserController.tsx` (~700 lines) composes ~20
domain hooks by threading raw `useState` setters between them —
`useFileBrowserItemActions` alone takes ~21 parameters, 8 of which are
`Dispatch<SetStateAction<...>>` — followed by ~380 lines of `useMemo`
prop-bundle assembly.

The domain hooks themselves are fine (they add real semantics: indexer
gating, cache policy, path-scoped invalidation). The problem is the wiring:
any state change is reached through 3–4 layers of parameter passing, and the
controller is the file every filebrowser change has to touch.

**Fix shape:** group the browser's UI state into a small number of cohesive
slices (selection, dialogs, editor, view) owned by a reducer or a
`FileBrowserStateContext`, and let the domain hooks consume the slice they
need instead of receiving setters as arguments. Constraints to respect:

- `AppDataTable` cells render via `flexRender`, so column definitions must
  stay stable — per-row volatile state goes through context, never through
  rebuilt column arrays.
- This overlaps ToDo #12 (rerender plan): stable callback identities are the
  fix for the per-progress-frame rerenders, so do #12's "stable cancels"
  first or together — both changes fight over the same context identities.

## 2. Dialog-owned job streams → `useJobStreamAction` + `attach()` — DONE

Done 2026-07-08. Both dialogs now run one `useJobStreamAction` config with
`closeOnAbort: "close"` (aborting the run's controller detaches the attach
stream and rejects `AbortError`, which the `error` callback filters), so the
ref machines collapsed to a started-guard + abort handle (plus `jobIdRef` in
the indexer, whose close cancels the job). `isRunning` is derived
(`!success && !error`). The effect-started mutation was replaced by a
recovery-scan-driven start: `useActiveJobRecovery` gained an `onMiss`
callback, so one scan per dialog open either `attach()`es a still-running
job (reopen mid-run, page reload) or `mutate()`s a fresh one — the two paths
cannot race. Compose failures now toast once, from the terminal error
callback; the progress `error` frame only updates the in-dialog display.

## 3. Container auto-update dual writer

`pages/main/docker/ContainerAutoUpdateDialog.tsx` (draft state + explicit
Save → `cache.set`) and `pages/main/docker/useContainerAutoUpdateControls.ts`
(optimistic autosave with a hand-rolled debounce/coalescing queue) both write
`docker.get_container_auto_update` state with two different consistency
models, and duplicate `DEFAULT_OPTIONS` / `normalizeOptions` / `optionKey`
verbatim. If both are mounted (dialog opened from the containers tab), two
writers race on the same cache entry.

**Fix shape:** extract the shared normalize/key helpers into one module
under `pages/main/docker/`, then pick a single write strategy — either the
dialog reuses the controls hook, or both feed one small
`useContainerAutoUpdateState` hook that owns the cache writes. The
hand-rolled autosave queue is also the only call site that would benefit
from a debounced-optimistic-write helper; do not generalize it into the API
layer unless a second consumer appears.

## 4. Legacy stream consumers on the layering allowlist — DONE

Done 2026-07-08. No page-level file imports stream primitives anymore:

- `Terminal.tsx` → `useLiveStream`, which gained `detachStream()` (unbind
  handlers without closing — the PTY stream persists for reconnection).
  Reattach goes through `openStream`'s `open` callback
  (`getStream("terminal.open") ?? openTerminalStream(...)`).
- `GeneralLogsPage.tsx` → `useLiveStream`'s new `onText` handler (the hook
  decodes frames); `useLogStream` switched to `onText` too and came off the
  allowlist with it. (`useLogStream` itself didn't fit this page: it
  accumulates a text blob, the page builds parsed rows.)
- `ConsoleDialog.tsx` → new `hooks/useStreamMessageChannel.ts` lifecycle
  hook (allowlisted in the pages' place — the one sanctioned add).

Net: allowlist entries went from four consumer files to one lifecycle hook.
The stream *openers* (`openTerminalStream`, …) stay public — the
factory-prop idiom (`createStream={(tail) => openDockerLogsStream(id, tail)}`)
is the blessed way pages consume streams.

## 5. Backend: `filebrowser.chmod_batch` (ToDo #14) — DONE

Done 2026-07-08. `filebrowser.chmod_batch` replaced `filebrowser.chmod`
outright, mirroring how `delete_batch` replaced the single-item delete: one
job over the whole selection (`{ paths, mode, owner, group, recursive }`),
server-side loop in `backend/bridge/handlers/filebrowser/batch_operations.go`
with owner/group resolved once per job, aggregate result
(`{ total, succeeded, failed: [{path, error}] }`), and an indeterminate
running entry count for progress. `handleConfirmPermissions` is a single
`useJobStreamAction` call; partial failures surface as one aggregate error
toast.

---

### Ordering suggestion

Items 2 and 4 are done (2026-07-08), as is ToDo #12 (rerender plan — see
`docs/frontend-rerender-plan.md`). Remaining: items 1 and 3. Item 1
(controller prop-drilling) was deliberately sequenced after #12's
stable-cancels fix, which has landed — it is unblocked.
