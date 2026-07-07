# Frontend Rerender Performance Plan

## Summary

Profiler-confirmed (2026-07-07): while any background transfer runs, every job
progress frame (~4Hz per job, `jobs.progressMinIntervalMs: 250`) rerenders the
entire filebrowser subtree. Root cause is the background-jobs *actions* context
changing identity on every progress tick because the cancel callbacks close
over their state arrays. The state/actions context split and the `React.memo`
walls in the filebrowser are correct designs; they are being defeated upstream
by callback identity churn.

Bundle weight is ruled out: `make analyze` budgets pass (initial shell
137 KiB gzip vs 190 budget, dashboard cold 282 KiB vs 330), and the largest
chunks are lazy feature routes (FileEditor/ace 142 KiB, xterm 86 KiB,
noVNC 70 KiB gzip).

No Zustand (or any store) migration: React Query owns server state, contexts
own low-frequency app state, live charts feed smoothie through the module-level
series store. Fix the identity bugs instead. Revisit a subscribable store for
jobs state only if its consumer count grows beyond the navbar.

## Profiler Evidence

React DevTools Profiler, dev build, one recording during a transfer with the
filebrowser open:

- 61 commits, each "What caused this update? BackgroundJobsProvider".
- 71.4ms render per commit; `BackgroundJobsProvider` self time 0.5ms.
- `FileBrowser` subtree: 68.9ms (~97% of the commit).
- `DirectoryListing`/`VirtualDirectoryItems`: ~51ms — the file listing fully
  rerenders even though nothing transfer-related is displayed there.
- `FileBrowserContent (Memo)` and `VirtualDirectoryItems (Memo)` render anyway:
  the controller rerender rebuilds `contentProps`, so memo bailouts fail.

Dev numbers overstate prod (React Compiler runs only on build), but the commit
count reproduces identically in prod: the compiler cannot memoize away a
context whose value identity changes.

## Root Cause Chain

1. Progress frame → `setTransfers`/`setDownloads`/… in a job hook.
2. Cancel callbacks (`cancelTransfer` in `useTransferJobs.ts`,
   `cancelDownload`, `cancelUpload`, `cancelJob`) list their state arrays as
   deps → new identity per frame.
3. `actionsValue` memo in `BackgroundJobsContext.tsx` invalidates →
   `BackgroundJobsActionsContext` consumers rerender: `useFileBrowserController`
   (via `useBackgroundJobActions` + `useFileMutations`), `FileBrowserHeader`,
   `IndexerDialog`, `NavbarNotificationsDropdown`.
4. Controller rerender → `contentProps`/`dialogsProps` identity changes →
   memoized filebrowser subtree renders.

## Fix Plan (ranked by measured cost)

1. **Stable cancel actions** (~97% of the measured commit cost)
   - Add a `useLatestRef` helper; cancels read the current item through the ref
     and drop the state arrays from their deps.
   - Files: `hooks/backgroundJobs/useTransferJobs.ts`, `useDownloadJobs.ts`,
     `useUploadJobs.ts`, `useGenericBackgroundJobs.ts`.
   - Post-fix invariant: `BackgroundJobsActionsContext` value identity never
     changes after mount.
2. **Toast history localStorage churn** — `ToastContext.tsx` re-parses
   localStorage on every sonner change and re-persists in an effect. Parse once
   (lazy init), keep the merged history in memory, debounce the persist.
   `clearHistory` keeps its immediate write.
3. **Log stream buffer cap** — `useLogStream.ts` accumulates
   `prev + text` unbounded (O(n²), one rerender per frame forever). Cap the
   buffer (e.g. 512 KiB, trim to the next newline).
4. **Download double progress path** — `useDownloadJobs.startDownload` attaches
   both the job-attach stream and the data stream; both call `updateDownload`.
   Suppress attach-stream updates once data-stream progress flows (shared local
   flag); attach keeps covering preparing/compressing/waiting_for_client.
5. **Decouple BackgroundJobsProvider from ConfigContext** —
   `useUploadChunkSize` subscribes the provider to all config changes. Expose a
   stable getter (accessor context with a ref-backed `getConfig`), read
   `chunkSizeMB` at upload start. `ComposeStacksPage` keeps the reactive hook
   (it already subscribes via `useConfig`).
6. **`useFileQueries` variable-length dep array** —
   `[multipleDetailTargets, ...multipleResourceData]` violates the hooks
   contract and recomputes anyway (fresh arrays per render). Replace with a
   plain guarded derivation; `useFileMultipleDirectoryDetails` keys queries by
   path strings, so map identity churn cannot cause refetch loops.
7. **Drop global `refetchIntervalInBackground: true`** —
   `ReactQueryContext.tsx`. Hidden tabs keep polling (dashboard ~3.5 queries/s
   when mounted). Live charts already handle gaps via stale reset + history
   backfill (`useLiveSeries`). Lowest priority: browser timer throttling
   already mitigates most of the cost.

## Verification

- Repeat the profiler recording (copy or download running, filebrowser open):
  `FileBrowser` and below must show "did not render"; commit duration drops
  from ~71ms to low single digits; the only subtree rendering per frame should
  be `NavbarNotificationsDropdown` (it displays the progress).
- `make lint-only` + tsc, existing hook/context tests
  (`contextHooks.test.tsx`, `useBackgroundJobRuntime.test.ts`, filebrowser hook
  tests).
- Exercise cancel for each job kind (download, upload, compress/extract,
  copy/move, generic) — the cancel path is the code being changed.

## Non-Goals

- No store/Zustand migration; no changes to the live-chart path (smoothie
  module store already bypasses React).
- No React Compiler in dev builds (tracked separately in ToDo #2).
- Navbar progress rerenders per frame stay: that component displays the
  progress and its subtree is small.

Related: ToDo #6 (total review of jobs code) — the cancel-identity fix and the
double-progress fix fall inside that review's scope.
