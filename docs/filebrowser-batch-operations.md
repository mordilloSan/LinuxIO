# Filebrowser Batch Operations — Design & Constraints

## Background

Filebrowser transfers are bridge **jobs** whose progress streams to the browser
over the single multiplexed WebSocket (`/ws`). Job admission is governed by
per-route policies in `backend/common/ipc/bridge/router.go`:

- `ActionDefault` — copy / move / delete / compress / extract / chmod
- `StreamDefault` — upload / download / archive

Two production problems drove this work:

1. **Folder uploads failed** with "rate limit exceeded" — the per-minute
   start-rate cap rejected uploads once a folder had more than ~30 files, and
   the failure *persisted* because broken upload streams leaked job slots.
2. **Bulk copy/move/delete** fired one job per file all at once (`Promise.all`),
   so multi-selections larger than the queue were rejected, and copy/move
   resolved on job *start* (cut/paste cleared the clipboard before the move
   actually landed).

## What changed

- **Rate caps removed** (`StartRatePerMinuteOwner: 0`) on both `StreamDefault`
  and `ActionDefault`. These are user-initiated transfers; concurrency caps
  still bound them.
- **Upload slot leak fixed** — the frontend cancels an abandoned upload job, and
  the bridge self-cancels any transfer idle past `transferIdleTimeout` (5 min)
  in `backend/bridge/handlers/filebrowser/transfer_operations.go`.
- **Copy/move resolve on completion**, not on start (`useCopyMoveJobs.ts`), so
  cut/paste clears the clipboard only after the move lands.
- **Copy/move/delete are now one batch job** — a multi-selection runs as a
  single server-side job that loops over the items and reports one aggregate
  progress bar (one navbar entry). See:
  - `backend/bridge/handlers/filebrowser/batch_operations.go` —
    `runCopyBatchJob`, `runMoveBatchJob`, `runDeleteBatchJob`
  - routes `filebrowser.copy_batch` / `move_batch` / `delete_batch`
    (`background_operations.go`), request types `BatchTransferRequest` /
    `BatchPathRequest` (`apischema/contracts.go`)
  - frontend: `useCopyMoveJobs.ts`, `useFileMutations.ts`, recovery in
    `useRecoveredJobs.ts`

## Constraints that remain (by design)

- **Concurrency caps stay.** `ActionDefault` is still 1-active/user. Irrelevant
  within one batch (it's a single job), but two *separate* paste operations
  serialize — the second queues (up to 16). Intentional: avoids disk thrash from
  parallel writes.
- **Best-effort, no auto-rename.** Batch copy/move/delete skip bad items
  (missing source, existing destination without overwrite, type mismatch) and
  report them in the job result's `failed[]`; the toast surfaces the count. A
  same-name copy into the same folder is reported as a failure, **not**
  auto-`(copy)` — this mirrors the old single-copy `409` behavior.
- **Aggregate progress needs a pre-walk.** Copy/move sum each source's size up
  front (via `ComputeCopySize`, since a minimal install has no indexer), so on a
  huge tree there is upfront walk cost before bytes move. Delete reports a
  running item count (indeterminate, no percentage).

## Deferred work (actionable)

- **Batch uploads.** We batched copy/move/delete but **not** upload. A
  2000-file folder upload still runs 2000 sequential jobs (works now — rate cap
  gone, leak fixed — but slower than a single stream). Batching uploads needs a
  multi-file stream framing protocol (one job, file headers + bytes over one
  stream), which was deliberately skipped as too large for the bug fix. This is
  the main remaining upgrade.
- **Post-reload listing refresh.** A batch reloaded mid-flight re-attaches as
  one navbar entry, but batch job types are not in `INVALIDATIONS_BY_JOB_TYPE`
  (`constants/backgroundJobQueryInvalidations.ts`) — so a batch that finishes
  *after* a page reload won't auto-refresh the listing. (Fresh batches refresh
  via `onComplete`.) The pre-existing single copy/move/delete have the same gap;
  fixing both together would close it.
- **Remove dead single routes.** `filebrowser.copy` / `move` / `resource_delete`
  remain registered (tested endpoints + legacy in-flight job recovery) but the
  frontend no longer calls them. Removing the routes + `runCopyJob` /
  `runMoveJob` / `resourceDelete` handlers + their recovery cases is a separate
  cleanup.
- **Optional: auto-rename on collision** for batch copy (`name (copy)`), if the
  best-effort "skip + report" behavior proves annoying in practice.
