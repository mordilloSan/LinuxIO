# TanStack Router, React Query, and Start Loader Audit

Status: review complete; implementation reconciled through Phase 5.

Audit date: 2026-07-30

Implementation reconciliation: 2026-07-31

Repository snapshot: `215217bacdaeea0a0eb400a31b07bc7ee4ace813`

Upstream TanStack Router snapshot inspected:
`77ed6d5c3a9878adb1ac50bf2881b243e390812e`

This document preserves the original audit evidence below. Each finding now has
a resolution note, and the checklists reflect the implemented work rather than
the original handoff state.

Local package versions:

- `@tanstack/react-query`: `5.101.4`
- `@tanstack/react-router`: `1.170.18`
- `@tanstack/router-core`: `1.171.15`

## Executive conclusion

LinuxIO already uses the correct high-level TanStack architecture:

```text
Link intent
  -> beforeLoad access checks
  -> route loader
  -> transport/update readiness
  -> shared QueryClient cache
  -> useSuspenseQuery observes the same query key
```

TanStack Start does not replace this with a second loader system. Start uses
TanStack Router's lifecycle and adds request-scoped routers, SSR, streaming,
Query dehydration/hydration, server functions, and server cleanup.

Router-abort integration now preserves shared-query behavior through per-key
loader-consumer reference counting.

The most useful Start pattern to adopt is an explicit distinction between:

- navigation-critical queries that must finish before the route renders;
- stale-while-revalidate queries that may render cached data immediately;
- optional/deferred queries that should not make the entire route atomic.

LinuxIO's VM ownership and progressive Hardware history queries already followed
this principle well. Dashboard and Hardware now both use transport-only shell
gates, conditional deferred prefetch, and local widget boundaries.

## Scope inspected

All production route loaders and their React Query consumers were inspected.

- 26 loader files
- 24 Query-backed loaders
- 2 transport-only loaders: Logs and Terminal
- 46 unique Query endpoints
- Up to 62 query declarations across routes

The counts below describe the original snapshot. The current route inventory and
loader contract live in [`docs/tanstack-router.md`](docs/tanstack-router.md).

Large or notable loader groups:

- Dashboard: 12 base system/storage queries plus four conditional Docker
  queries.
- Hardware: seven route queries; history queries intentionally remain
  component-owned.
- Docker dashboard: five queries.
- Accounts, Services, Sockets, and Timers: a list query plus search-selected
  detail queries through `loaderDeps`.
- VM parent: `virt.list` and `virt.preflight`; the `$name` child adds only its
  detail query.
- Logs and Terminal: transport readiness only.

## Strong parts to preserve

### Shared cache ownership

The browser uses one QueryClient shared by Router loaders and components:

- `frontend/src/router/query-client.tsx`
- `frontend/src/router/provider.tsx`
- `frontend/src/routes/-loader.ts`

Loaders and observers use the same generated query keys, so requests deduplicate
and polling, invalidation, optimistic writes, and refetching all operate on one
entry.

### Generated endpoint options and keys

The endpoint abstraction is stronger than the minimal TanStack examples:

- Keys are consistently
  `["linuxio", handler, command, normalizedRequest]`.
- `queryOptions` centralizes each endpoint's key and query function.
- Prefix helpers support broad, declarative invalidation.

Relevant files:

- `frontend/src/api/query-keys.ts`
- `frontend/src/api/react-query.ts`
- `frontend/src/api/operation-query-invalidations.ts`

### Router ownership

The following decisions are intentional and should remain:

- One global intent-preload policy.
- Access checks run before speculative data work.
- Search values that affect query identity use `loaderDeps`.
- Path parameters rely on Router's automatic dependency tracking.
- Empty layout routes do not have meaningless loaders.
- Common parents own data shared by child tabs.
- Progressive, dialog-only, and interaction-only reads remain component-owned.
- Speculative preload failures are marked `silent`.
- Router retry resets React Query's error boundary.

Existing tests cover cache sharing, concurrent deduplication, quiet speculative
errors, update races, result ordering, error propagation, route topology, and
some query-ownership decisions.

## Comparison with TanStack Start

| Concern | LinuxIO | TanStack Start |
| --- | --- | --- |
| Router lifecycle | TanStack Router SPA | The same TanStack Router lifecycle |
| QueryClient | One browser singleton | New server client/router per request, then a browser instance |
| Loader priming | Batched `ensureQueryData` | Usually `ensureQueryData` for critical data |
| Optional work | Deferred through `startRouteQueryPrefetches` | Often started with `prefetchQuery` and rendered progressively |
| Loader return | `Promise<void>` when Query owns the data | Usually `void` when Query owns the data |
| Rendering | Client-only `createRoot` | SSR, streaming, dehydration, and hydration |
| Data transport | Authenticated browser WebSocket/RPC mux | Commonly server functions or isomorphic requests |

LinuxIO should not adopt Start's SSR machinery merely to improve loaders. Its
current browser-only architecture is appropriate for its authenticated
WebSocket transport. If SSR becomes a goal, the router, QueryClient, auth
context, and transport all need a separate request-scoped design.

## Findings

### 1. Correctness: socket and timer lists are not invalidated

**Resolved.** Unit mutations now invalidate Service, Socket, and Timer lists,
with exact manifest coverage.

`UNIT_KEYS` in the current uncommitted refactor invalidates only:

- `systemd.list_services`
- `systemd.get_unit_info`

File: `frontend/src/api/operation-query-invalidations.ts`, around lines 33-36.

The same start, stop, restart, reload, enable, disable, mask, and reset actions
are used by Service, Socket, and Timer cards. Socket and Timer list observers
stop polling in card mode:

- `frontend/src/routes/_authenticated/services/-components/SocketsTab.tsx`
- `frontend/src/routes/_authenticated/services/-components/TimersTab.tsx`
- `frontend/src/routes/_authenticated/services/-components/UnitViews.tsx`

As a result, list-derived card state can remain stale indefinitely after a
successful action. Invalidating `get_unit_info` does not update the base Socket
or Timer object used to construct the card.

Proposed fix:

- Add `systemd.list_sockets` to `UNIT_KEYS`.
- Add `systemd.list_timers` to `UNIT_KEYS`.
- Add an exact manifest test for both prefixes.

Related lower-priority invalidation candidates to review:

- Docker prune does not invalidate `docker.get_docker_info`.
- Compose operations may affect images, networks, or volumes while currently
  invalidating only projects and containers.
- Account modification invalidates `list_users`, but not selected
  `get_user_details`.

### 2. Performance: File Browser performs an immediate second request

**Resolved.** Loader and observer share
`fileBrowserListingQueryOptions` with a two-second `staleTime`. The route uses
background freshness, so a cold result remains fresh through observer mount and
stale or invalidated listings revalidate on revisit.

Before the fix, both the File Browser loader and mounted observer specified
`staleTime: 0`:

- `frontend/src/routes/_authenticated/filebrowser/$.tsx`, lines 20-27
- `frontend/src/hooks/filebrowser/useFileQueries.ts`, lines 23-30

On a cold navigation:

1. The loader fetches the resource.
2. The result is stale immediately.
3. `useSuspenseQuery` mounts.
4. React Query's default stale-on-mount behavior starts another request.

The loader test that proves no second request uses `staleTime: Infinity`, so it
does not cover this production behavior.

Possible fixes:

- Prefer the application's normal two-second stale grace.
- Alternatively, set `refetchOnMount: false` for this observer if the loader
  result is the intended navigation snapshot.

Replacing `ensureQueryData` with `fetchQuery` alone would not prevent the
mount-time refetch.

### 3. Semantics: `ensureQueryData` ensures presence, not freshness

**Resolved.** `PRESENCE` is the documented default for critical query loaders;
File Browser explicitly selects `BACKGROUND`, and `BLOCKING` remains available
when navigation must await fresh data. Query loaders pass Router loader
arguments directly so the shared helper derives the abort signal. Deferred work
has its own non-blocking helper.

`loadRouteQueries` calls `ensureQueryData` without
`revalidateIfStale`:

- `frontend/src/routes/-loader.ts`, lines 45-64
- installed Query source:
  `frontend/node_modules/@tanstack/query-core/src/queryClient.ts`, lines 138-162

When cached data exists, `ensureQueryData` returns it immediately regardless of
whether it is stale or invalidated. It only starts a background refresh when
`revalidateIfStale: true`.

Consequences:

- `defaultPreloadStaleTime: 0` correctly makes Router invoke the loader.
- A second hover over a stale route does not normally refresh its Query entry.
- Navigation may render cached data and let the mounted observer refetch.
- `router.invalidate()` reruns loaders but does not itself guarantee fresh
  Query data.

This is valid stale-while-revalidate behavior, not a TanStack misuse. The
documentation should avoid describing every loader as a fresh-data gate.

Recommended explicit policies:

| Desired behavior | QueryClient operation |
| --- | --- |
| Data merely needs to exist | `ensureQueryData` |
| Return cache immediately and refresh stale data | `ensureQueryData({ revalidateIfStale: true })` |
| Navigation must await data within its `staleTime` contract | `fetchQuery` |
| Optional work must not block or fail the route | `prefetchQuery` without awaiting it as critical work |

Do not globally replace `ensureQueryData` with `fetchQuery`. Blocking freshness
should be selected only for routes where stale data is unsafe.

### 4. Resilience: Dashboard optional widgets make the route atomic

**Resolved.** Dashboard has no data-critical shell query. It gates only on
transport readiness, prefetches known-visible widgets from the per-user config
cache, and lets local Suspense/ErrorBoundary pairs own each card. Hardware uses
the same model for expanded sections and unmounts collapsed query observers.
Dashboard fallbacks preserve the resolved card frame with `FrostedCard` and
`AppSkeleton`, using stats-only or split/chart geometry instead of a local
`PageLoader` animation.

`loadRouteQueries` uses `Promise.all` and propagates any member failure to
Router's error boundary.

The Dashboard always loads every available card's data, up to 16 queries, even
when the user has hidden cards. Each widget has a local ErrorBoundary, but those
boundaries cannot help during a cold route load because a loader failure occurs
before any widget renders.

Relevant files:

- `frontend/src/routes/_authenticated/index.tsx`
- `frontend/src/routes/_authenticated/-dashboard/DashboardPage.tsx`
- `frontend/src/routes/-loader.ts`

Recommended direction:

1. Classify the minimum data needed to construct the Dashboard shell.
2. Keep that set navigation-critical.
3. Start visible optional widget queries as deferred prefetches.
4. Let each widget's Suspense and ErrorBoundary own its result.
5. Avoid fetching hidden-card data unless it is intentionally being warmed.

Do not generalize this finding to every `Promise.all`. Small pages may
legitimately require their complete batches.

### 5. Cancellation does not reach the transport

**Resolved.** Router signals now flow through loader readiness, Query functions,
RPC timeout composition, stream abort frames, and query handler contexts.
Loader cancellation is ref-counted so another loader or mounted observer keeps a
shared query alive. `jobs.get`, `jobs.list`, and `jobs.cancel` now use the same
abort context instead of bypassing it; streaming job primitives remain detached
by design.

TanStack Router supplies an `abortController` to loaders, but LinuxIO routes
discard it. The generated endpoint query function also ignores React Query's
`QueryFunctionContext.signal`, and the RPC layer accepts only timeout/retry
settings.

Relevant files:

- `frontend/src/routes/-loader.ts`
- `frontend/src/api/react-query.ts`, around lines 538-559
- `frontend/src/api/linuxio-core.ts`

Consequences:

- Started intent preloads continue after the navigation becomes irrelevant.
- Rapid param/search changes leave old-key RPCs running.
- `EndpointCache.cancel()` can cancel Query state but cannot stop backend work.
- Sibling requests keep running after one member makes a batch fail.

Recommended phased implementation:

1. Add caller `AbortSignal` support to the RPC request layer.
2. Compose the caller signal with the existing timeout signal.
3. Consume Query's signal inside every endpoint `queryFn`.
4. Make `ensureLoaderRequestReady` abortable.
5. Add Router-abort integration only after considering deduplicated consumers.

Do not unconditionally call `cancelQueries` for every loader abort: a shared
query may also have an active observer or another loader awaiting it.

### 6. Transport readiness runs before cache inspection

**Decided and documented.** LinuxIO continues to require a live backend for
every route transition, including cache hits. This is intentional for a live
administration UI; cached Query data is not an offline-navigation contract.

Every Query loader awaits transport readiness before calling
`ensureQueryData`, even when all required entries are already cached.

Consequences:

- Cached UI cannot satisfy the loader while the mux reconnects.
- A disconnected transport can move a previously successful route into its
  error boundary even when Query still has usable data.
- This partially conflicts with the custom Query `onlineManager`, which
  otherwise pauses unavailable network work.

This may be intentional for a live administration application. Decide
explicitly whether LinuxIO permits cached route rendering during an unavailable
transport or update.

Possible implementation directions:

- Preserve the pre/post update assertions but initialize transport only when at
  least one required cache entry is missing.
- Move lazy transport readiness into the actual endpoint request path.
- Keep the current behavior and document that every navigation requires a live
  backend, even on cache hits.

### 7. Query results are returned as unused Router loader data

**Resolved.** `loadRouteQueries` returns `Promise<void>` and Query remains the
only data owner.

No production component uses `useLoaderData`, yet `loadRouteQueries` returns an
array containing every Query result.

Router therefore retains an unnecessary loader-data owner in addition to the
Query cache. This is not a deep in-memory copy, but it adds an extra array and
can retain the referenced objects for the route match's lifetime.

Recommended change:

- Make `loadRouteQueries` return `Promise<void>`.
- Await `Promise.all` internally without returning its results.
- Return small, explicit loader metadata only where a route needs it for
  document head data, breadcrumbs, redirects, or other Router consumers.

This also produces a cleaner future SSR contract.

### 8. Route failures have two visible error owners

**Resolved.** Initial route queries are tagged `routeInitialLoad` and `silent`,
so `RouteError` is their one visible owner. Stale background failures remain
toast-visible. Deferred widget work is silent and locally bounded.

Normal loader query failures:

1. Trigger the global QueryCache error toast.
2. Reject the loader.
3. Render the route-level ErrorPage.

Intent preload failures are correctly silent, but direct navigation failures
can show both UI mechanisms. Multiple failed batch members can also produce
multiple toasts.

Relevant files:

- `frontend/src/router/query-client.tsx`
- `frontend/src/routes/-loader.ts`
- `frontend/src/routes/-components/RouteError.tsx`

Suggested policy:

- Let the route error boundary own initial navigation failures.
- Reserve global query toasts for background failures where cached data remains
  visible.
- Alternatively, tag route-owned initial queries and suppress their global
  toast.

### 9. Smaller observations

**Resolved or preserved intentionally.** Route-owned Query attempts default to
no Query retry, leaving the bounded connection retry in the transport. Exact
50/150/0/0 preload and pending timings are pinned in tests. Loaders also consult
the live mux update flag, closing the first-load provider gap.

- Query retries once globally, while retryable reads may internally retry a
  closed connection. One logical read can therefore produce up to four
  transport attempts.
- Pending UI begins after 150 ms with no minimum duration. TanStack Router's
  defaults are 1000 ms and 500 ms. The LinuxIO values may produce short flashes;
  measure before changing them.
- The live-update blocker is published by a provider rendered after initial
  authenticated child loaders. It protects navigation during a mounted update,
  but the first loader after a full reload does not yet have a published update
  state.
- The preload-policy test asserts that timing values are numbers, not their
  intended exact values.
- Route coverage tests prove that a protected leaf has a loader somewhere in
  its branch, but not that the loader keys match the mounted observer keys.

## Proposed implementation order

### Phase 1: definite fixes

- [x] Add Socket and Timer list invalidation.
- [x] Add exact invalidation tests.
- [x] Remove the File Browser's immediate duplicate request.
- [x] Add a test demonstrating the `staleTime: 0` mount refetch.

### Phase 2: make loader behavior explicit

- [x] Make `loadRouteQueries` return `Promise<void>`.
- [x] Introduce named presence, background-revalidation, and blocking-freshness
  policies.
- [x] Make presence the loader default and reserve explicit policy arguments for
  non-default behavior.
- [x] Move endpoint/domain `staleTime` choices into shared options factories
  where loaders and observers must agree.
- [x] Keep observer-only options such as `refetchInterval` in components.

### Phase 3: critical versus deferred

- [x] Classify Dashboard data by rendering criticality.
- [x] Do not block the route on hidden or optional widgets.
- [x] Add widget-level Suspense/loading/error ownership before deferring.
- [x] Use layout-matched Dashboard card skeletons instead of spinner-per-slot
  fallbacks.
- [x] Apply the same review to Hardware, but do not assume every one of its
  seven queries is optional.

### Phase 4: cancellation and transport

- [x] Thread Query signals through endpoint query functions.
- [x] Add caller-signal support to the RPC transport.
- [x] Compose request and timeout signals safely.
- [x] Make readiness waits abortable.
- [x] Pass Router loader arguments directly and derive abort signals in the
  shared helper.
- [x] Add abandoned-loader, shared-consumer, and rapid-navigation tests.
- [x] Apply abort contexts to `jobs.get`, `jobs.list`, and `jobs.cancel`.
- [x] Preserve and document the live-backend requirement for cache hits.

### Phase 5: errors and UX

- [x] Give initial route failures one visible error owner.
- [x] Keep background-refetch errors observable.
- [x] Remove retry multiplication during transport outages.
- [x] Review and preserve the 150/0 pending timing; pin exact values in tests.
- [x] Add a first-load update-blocker test using the live mux flag.

## Suggested test additions

- [x] Stale cached intent preload with revalidation disabled.
- [x] Stale cached intent preload with background revalidation enabled.
- [x] Invalidated File Browser query followed by route revisit.
- [x] Blocking-fresh query behavior.
- [x] File Browser cold navigation request count.
- [x] Aborted route work and rapid Router navigation.
- [x] Post-resolution cancellation of orphaned background revalidation.
- [x] Two consumers sharing a query when one route load is aborted.
- [x] Deferred optional-work failure ownership.
- [x] Dashboard and Hardware conditional query ownership.
- [x] Exact Service, Socket, and Timer action invalidations.
- [x] Loader-to-observer suspense endpoint coverage.
- [x] Exact global preload and pending timing values.

Frontend-only implementation work should be handed off only after:

```text
make check-frontend
```

## Primary upstream references

- [TanStack Start overview](https://tanstack.com/start/latest/docs/framework/react/overview)
- [Router preloading guide](https://tanstack.com/router/latest/docs/guide/preloading)
- [Router loader cancellation guide](https://tanstack.com/router/v1/docs/guide/data-loading#using-the-abort-signal)
- [QueryClient `fetchQuery`, `prefetchQuery`, and `ensureQueryData`](https://tanstack.com/query/v5/docs/reference/QueryClient)
- [Start Query router factory](https://github.com/TanStack/router/blob/77ed6d5c3a9878adb1ac50bf2881b243e390812e/examples/react/start-basic-react-query/src/router.tsx#L8-L23)
- [Start critical-query loader](https://github.com/TanStack/router/blob/77ed6d5c3a9878adb1ac50bf2881b243e390812e/examples/react/start-basic-react-query/src/routes/posts.route.tsx#L5-L17)
- [Start deferred-query route](https://github.com/TanStack/router/blob/77ed6d5c3a9878adb1ac50bf2881b243e390812e/examples/react/start-basic-react-query/src/routes/deferred.tsx#L18-L44)
- [Start Query SSR integration](https://github.com/TanStack/router/blob/77ed6d5c3a9878adb1ac50bf2881b243e390812e/packages/router-ssr-query-core/src/index.ts#L35-L236)
- [Router loader execution source](https://github.com/TanStack/router/blob/77ed6d5c3a9878adb1ac50bf2881b243e390812e/packages/router-core/src/load-matches.ts#L641-L865)
- [Query `ensureQueryData` source](https://github.com/TanStack/query/blob/9d24c455453b965511472a8251d68e2ae02c96e0/packages/query-core/src/queryClient.ts#L138-L162)

## Handoff note

The audit began read-only. Its implementation phases are now complete and the
canonical current behavior is documented in `docs/tanstack-router.md`.
