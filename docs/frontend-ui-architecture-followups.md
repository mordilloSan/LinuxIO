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

## 2. Dialog-owned job streams → `useJobStreamAction` + `attach()`

`components/docker/ComposeOperationDialog.tsx` and
`components/docker/DockerIndexerDialog.tsx` each hand-roll an ~80-line state
machine on top of `useJobStreamAction`: 5–7 refs (`streamRef`,
`abortControllerRef`, `jobIdRef`, `startRequestedRef`, `closedByUserRef`,
…), a mutation started from a `useEffect` (non-idiomatic React Query), and
manual closed-by-user vs error-from-progress bookkeeping.

Since the migration, the mutation returned by `useJobStreamAction` also
exposes **`attach(job, variables)`**, which adopts an already-running job
into the same declarative config (progress, toasts, invalidation, pending
state). `pages/main/storage/DiskOverview/index.tsx` is the reference:
one config object drives fresh starts (`mutate`) and page-reload recovery
(`attach` via `useActiveJobRecovery`), with `options.onSettled` doing the
cleanup that used to need refs.

**Fix shape:** per dialog, move the lifecycle into the action config
(`onOpen`/`onProgress`/`onClose`/`signal`/`closeOnAbort` +
`options.onSettled`), replace the effect-started mutation with an explicit
start on open, and use `attach()` for the "dialog reopened while the job is
still running" path. The ref machines should collapse to at most a
started-guard.

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

## 4. Legacy stream consumers on the layering allowlist

`apiLayering.test.ts` fences the byte/mux-level primitives behind a
shrink-only allowlist. Three page-level entries are marked as debt:

| File | Primitive | Move to |
|------|-----------|---------|
| `pages/main/terminal/Terminal.tsx` | `bindStreamHandlers` | `useLiveStream` (it exists for exactly this) or `useXtermStreamTerminal` |
| `pages/main/logs/GeneralLogsPage.tsx` | `decodeString` | `useLogStream` (which already decodes frames for the other log pages) |
| `pages/main/vm/ConsoleDialog.tsx` | `createStreamMessageChannel` | a small `useStreamMessageChannel` lifecycle hook (owns open/close/cleanup) |

Each move deletes its allowlist entry; the staleness guard fails if an entry
is left behind. The stream *openers* (`openTerminalStream`, …) stay public —
the factory-prop idiom (`createStream={(tail) => openDockerLogsStream(id, tail)}`)
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

Items 2 and 4 are small and independent — good warm-ups. Items 1
and ToDo #12 should be planned together (same files, same context-identity
concerns); do them last and as one design.
