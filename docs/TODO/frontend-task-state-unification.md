# Frontend Task State Unification

## Status

Proposed. This is the follow-on to the completed config-state migration, which
moved per-user configuration out of a provider `useState` copy and into the
TanStack Query cache with select-based slice hooks and an actions-only context.
The background-task system is the remaining frontend state that duplicates
server-pushed data in provider state, and it is the largest one.

This is an architectural consolidation, not a fix for a measured performance
problem: the current implementation is correct and already carries deliberate
re-render mitigations. Schedule it when task surfaces grow (a tasks page,
cross-tab task visibility, notification integration per the
[API Reliability Roadmap](./api-reliability-roadmap.md) Phases 6–8), or when a
change would otherwise add a fifth domain hook.

## Current state

`BackgroundTasksProvider` (`frontend/src/contexts/BackgroundTasksContext.tsx`)
owns five parallel domain hooks — `useDownloadTasks`, `useUploadTasks`,
`useTransferTasks`, `useIndexerTasks`, `useGenericBackgroundTasks` (plus
`useRecoveredTasks` for post-login recovery) — and fans their output into four
contexts split by volatility:

- `BackgroundTasksActionsContext` — 16 identity-stable action functions.
- `BackgroundTasksStateContext` — the task arrays; republished on essentially
  every WebSocket progress frame during an active transfer.
- `BackgroundTasksIndexerContext` / `BackgroundTasksIsIndexingContext` —
  narrower slices split out so their consumers do not re-render per tick.

Every progress frame flows `bindStreamHandlers` → domain-hook `useState`
setter → provider memo → new state-context value. The state never touches the
query cache even though it originates entirely from server-pushed Task streams
using the uniform progress envelope (roadmap Phase 5). Existing mitigations —
the four-way context split, the identity-stable `useBackgroundTaskRuntime` ref
bag, per-item reference stability in `useTransferTasks`, and memoized rows in
`NavbarNotificationsDropdown` — keep today's blast radius small (one or two
subscribed consumers per context).

## Target model

Apply the same ownership rule the config migration established: the query
cache is the single frontend copy of server-originated state; contexts carry
only identity-stable actions.

- Stream frames write per-task cache entries via `queryClient.setQueryData`
  (keyed by task id, scoped like other per-user entries), instead of per-kind
  `useState` arrays.
- Consumers read through select-based slice hooks: a task list hook per kind,
  a per-task progress hook for rows, and derived flags (`isIndexing`) as
  cheap selects. Subscription granularity replaces the hand-rolled context
  splitting.
- `BackgroundTasksActionsContext` stays: actions are already identity-stable
  and are exactly what a context is for.
- Toast history stays as it is (`useSyncExternalStore` over a module store):
  it is client-local notification state, not server state.

## Why bother

- Deletes the parallel state plumbing: five domain hooks shrink to cache
  writers, and the State/Indexer/IsIndexing context split (plus its
  re-render-defense memos) becomes unnecessary.
- One state system across the app: config and task state follow the same
  read/write model, so invalidation, devtools inspection, and testing use one
  mental model (the test cache-seeding helper already exists).
- Groundwork for task surfaces the roadmap plans: a persistent tasks page,
  alert/notification integration, and post-login recovery all read the same
  cache entries instead of resubscribing bespoke provider state.

## Constraints

- Per-frame `setQueryData` volume must stay bounded; the owner-wide stream is
  already coalesced (roadmap Phase 5 exit criteria), and per-item reference
  stability must be preserved so unchanged rows keep identity under
  structural sharing.
- Recovery (`useRecoveredTasks`) must seed the same cache entries it would
  find live, so a reconnect and a fresh login converge on one code path.
- Actions must not regain state dependencies: `useUploadChunkSizeGetter`
  already reads the config cache directly and must stay render-inert.
- Unit tests must not require a live mux: seeding task entries in the test
  query cache replaces hand-built context values, mirroring
  `seedConfigCache`.

## Phases

1. Define task cache keys and a writer in `useBackgroundTaskRuntime`; write
   frames to the cache alongside the existing state (dual-publish, no
   consumer change).
2. Migrate one domain end-to-end (downloads is the smallest), switching its
   consumers to slice hooks and deleting its `useState` path.
3. Migrate the remaining domains and recovery.
4. Collapse `TasksStateContext`, `IndexerContext`, and
   `BackgroundTasksIsIndexingContext` into selects; keep only the actions
   context.

Each phase ends green on `make check-frontend-quiet`; claims about re-render
behavior during live transfers additionally need
`make test-frontend-browser-quiet` or runtime observation, not unit tests
alone.

## Exit criteria

- [ ] No provider `useState` holds task or progress state; the query cache is
      the only frontend copy.
- [ ] A progress frame re-renders only components subscribed to that task or
      to a list that structurally changed.
- [ ] The actions context value never changes identity after mount.
- [ ] Recovery and live streams populate identical cache entries.
- [ ] The four task contexts are reduced to one actions context.
