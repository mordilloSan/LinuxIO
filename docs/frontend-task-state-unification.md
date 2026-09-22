# Frontend Task State Unification

## Status

Completed on 2026-09-21. Background tasks now use the same ownership model as
configuration: TanStack Query holds task data, and the provider publishes only
stable actions. No task or progress arrays remain in provider state.

## Implementation

`api/background-task-cache.ts` defines per-user keys and the cache writer owned
by `useBackgroundTaskRuntime`. Downloads, uploads, transfers, indexers, and
recovered generic tasks all write through it. Task entries use the bridge task
id once available; uploads start under a temporary id and move to the bridge id
without changing the row identity. Stream-owned entries do not expire while
unobserved. Provider unmount removes the user's task entries and disables that
writer so late callbacks cannot repopulate them.

The list entry contains membership and completion metadata, not live progress.
A progress flush writes only changed task entries. Existing animation-frame
coalescing remains in downloads, transfers, and live indexers. Unchanged task
objects retain their identity through cache structural sharing.

Consumers use `useBackgroundTaskList`, `useBackgroundTasks(kind)`, and
`useBackgroundTask(id)`. `useIsIndexing` selects a boolean from membership.
Notification rows subscribe to their own task. The notification peek has a
separate subscription to the active tasks because it displays the task with the
lowest progress. The notification shell reads membership and completion
metadata, preserving its completed-transfer history without subscribing to
progress frames.

`useBackgroundTaskIndexer` reads the indexer entries and the cached indexer
summary (dialog visibility, last result, and last error). The State, Indexer,
and IsIndexing contexts were removed. `BackgroundTasksActionsContext` retains
its 16 actions in an object initialized once per provider mount. Upload actions
still read chunk size through the render-inert config getter. Toast history
continues to use its existing external store.

Recovery writes the same per-user task keys and domain entries as live work.
Transfers share their existing watcher and progress reducer. Recovered indexers
seed their initial progress from the task snapshot. Recovered uploads and
browser archive downloads retain their generic task presentation because the
new session does not own the original browser data transfer.

`seedTaskCache` supplies entries to tests without a live mux or task-state
contexts.

## Validation

- `make check-frontend-quiet`: lint, types, and frontend unit tests pass with
  no lint warnings. Cache and stream-lifecycle tests cover user isolation,
  bounded writes, structural sharing, upload rekeying, stable actions,
  live/recovered transfers, indexer recovery, cancellation, terminal cleanup,
  and late writes after disposal.
- `make test-frontend-browser-quiet PLAYWRIGHT_ARGS=src/test/browser/background-tasks.spec.ts`:
  the Chromium fixture verifies that progress changes commit the subscribed
  task row without committing its sibling, the membership list, or the
  indexing flag; removal updates membership. This exercises browser rendering
  with controlled cache writes, not a live bridge transfer.
- `make compiler-coverage`: no fatal failures, actionable recoverable bailouts,
  or manual memo fallback callbacks after removing the domain and notification
  memo wrappers.

## Exit criteria

- [x] No provider `useState` holds task or progress state; the query cache is
      the only frontend copy.
- [x] Progress updates notify the relevant task subscribers; membership-only
      subscribers remain unchanged (verified in the browser fixture).
- [x] The actions context value never changes identity after mount.
- [x] Recovery and live streams populate the same per-user task entries.
- [x] The four task contexts are reduced to one actions context.
