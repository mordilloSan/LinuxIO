# TanStack Router architecture

Status: TanStack Router is the application's only routing path as of
2026-07-23. The approved page-level child-route phase and its query,
error-boundary, and browser-regression follow-ups were completed on 2026-07-25.

## Route architecture

- Routes live in `frontend/src/routes/` and use `createFileRoute`.
- URL segments are real directories with a `route.tsx`, `index.tsx`, or
  `$.tsx` route file. Route-owned UI is co-located in `-components/` (and the
  dashboard in `-dashboard/`), using TanStack Router's native `-` ignore
  convention.
- `@tanstack/router-plugin` generates `routeTree.gen.ts` and automatically
  code-splits route components.
- `router.tsx` creates the typed application router from the generated tree.
- `router/provider.tsx` injects the live auth, capability, QueryClient, and
  update-blocking context into that router.
- `_authenticated.tsx` is the pathless authenticated layout. Its native
  `beforeLoad` guard runs before protected child loaders.
- Capability and privilege guards are route-local `beforeLoad` functions.
- Search parameters are validated by the route that owns them.
- Navigation and access metadata are route-local `staticData`; the sidebar
  reads that metadata from the router instead of maintaining another route
  catalog.
- File Browser uses the native `filebrowser/$` splat and `_splat` parameter.
- Unmatched authenticated URLs use the pathless layout's native
  `notFoundComponent`, preserving the authenticated shell. The root route owns
  the global not-found component; `router.tsx` configures the global default
  error component.

The former code-based router factory, component registry, protected-route
catalog, loader registry, and lazy-component wrappers have been removed.

## Preloading and code splitting

The router has one application-wide policy:

```ts
defaultPreload: "intent",
defaultPreloadDelay: 150,
defaultPreloadStaleTime: 0,
```

Links inherit this policy. There are no per-link or per-route preload delays,
and no `250 ms` override. `defaultPreloadStaleTime: 0` lets TanStack Query,
rather than the router cache, decide whether route data is fresh.

The Vite router plugin runs before the React plugin with
`autoCodeSplitting: true`. Route components, pending UI, error UI, and
not-found UI are lazy boundaries managed by TanStack Router. Critical route
configuration—search validation, guards, loaders, and static data—remains
available for matching and intent preloading.

## Route loader inventory

Every protected page that owns initial route data has a co-located loader:

| Route | Initial loader data |
| --- | --- |
| Dashboard | health, host, uptime, time, CPU, memory, filesystems, network, motherboard, GPU, drives, disk throughput, and Docker summaries when available |
| Network | network interfaces |
| Updates / History | available updates or update history, in the owning child route |
| Services / Timers / Sockets | the child route's unit list and selected-unit details |
| Logs | request-transport readiness; service filter data loads in background |
| Disks / LVM | disks, filesystems, and NFS mounts; or LVM physical volumes, volume groups, and logical volumes |
| Docker child routes | data for that page; auto-update state only for Containers |
| VMs | shared VM list and preflight status; Machines also loads the URL-selected VM detail |
| Users / Groups | the child route's account list; Users also loads selected-user details/login history |
| Shares / Mounts | NFS/Samba shares or NFS/CIFS mounts, in the owning child route |
| WireGuard | WireGuard interfaces |
| Hardware | sensors, PCI devices, memory modules, and hardware summaries; variable-range monitoring histories load progressively in their cards |
| Navigator | the resource for the current splat path |
| Terminal | request-transport readiness before the stream-only page mounts |

Search-dependent data is declared through `loaderDeps`, so a relevant search
change reruns the loader with a distinct dependency identity. Each query loader
returns the result of generated endpoint `queryOptions` loaded through the
router's shared QueryClient. Request failures propagate to the route error
boundary. The mounted query observer consumes and subscribes to those same
cache entries instead of starting an independent initial request.

Sign-in, not-found, and root routes own no backend data and therefore do not
contain artificial no-op loaders. Dialog-only and click-selected data stays
with its enabled query rather than being fetched speculatively for every route
visit.

## Update boundary

While a live application update blocks requests, sidebar entries are inert and
the navigation blocker rejects transitions. Route query loading checks the live
update state before transport readiness and again immediately before the
request. Intent-prefetched queries are tagged silent and speculative so an
unreliable hover preload does not create a global error toast.

## Regression coverage

- `router.test.tsx` checks the generated topology, the single
  global preload policy, loader coverage, route-local navigation metadata, and
  access metadata.
- `routes/-loader.test.tsx` checks shared-cache reuse, request
  readiness, update races, and speculative metadata.
- `routes/_authenticated/-components/sidebar/useSidebarItems.test.tsx` checks
  access filtering, static-data
  ordering, and the absence of per-link preload overrides.
- `router/provider.test.tsx` checks bootstrap gating and live
  router-context invalidation.
- `routes/_authenticated/-query-ownership.test.ts` prevents progressive
  Hardware history queries, dialog-only WireGuard network data, and
  filter-only Logs service data from moving back into eager page loading.
- `components/errors/ErrorBoundary.test.tsx` and
  `routes/-components/RouteError.test.tsx` verify recovery from failed
  Suspense queries at local and route boundaries.
- `src/test/browser/router.spec.ts` runs the routed-tab lifecycle in Chromium:
  direct links, refresh, back/forward navigation, pending and error UI, and
  first-navigation chunk loading. It also inspects the production manifest to
  keep all 21 page-level child-route components in distinct dynamic entries.

`routeTree.gen.ts` is generated code. It is committed for TypeScript consumers
but excluded from formatting.

## Approved simplification plan

Status: direction reviewed and approved on 2026-07-24; the child-route phase
and the independent query, boundary, and browser follow-ups were implemented on
2026-07-25. No Phase 3 is defined for this migration. Broader authenticated and
privileged end-to-end coverage belongs to the separate three-tier test plan.

This plan follows the reusable query-options pattern described in
[The Better Way to Use React Query](https://www.youtube.com/watch?v=e2OC3aaiGhI).
The existing endpoint layer already implements the important parts of that
pattern:

- Query keys and query functions are exposed together through reusable,
  generated `endpoint.queryOptions(...)`.
- Components choose the appropriate observer (`useQuery`,
  `useSuspenseQuery`, or `useQueries`) instead of being forced through a
  query-specific custom hook.
- Route loaders reuse the same options with `ensureQueryData`.
- Mutations and cache invalidation reuse generated endpoint keys.
- Multiple components may observe the same cache entry. This is intentional
  and is not prop-drilled or wrapped in another abstraction.

The endpoint API, query-key scheme, loader helper, and mutation invalidation
layer remain the canonical central abstractions. Do not add parallel
`queryOptions` factories, duplicate query-key constants, or custom
`useSomethingQuery` wrappers that only hide a TanStack Query observer.

### Page-level tabs become child routes

Tabs that represent independently navigable pages use traditional child
routes:

- Accounts: Users and Groups
- Services: Services, Timers, and Sockets
- Storage: Disks and LVM
- Shares: Shares and Mounts
- Updates: Updates and History
- Docker: Dashboard, Containers, Compose, Networks, Volumes, and Images
- VMs: Dashboard, Networks, Images, and Machines

A route group has this shape:

```text
docker/
  route.tsx
  index.tsx
  containers.tsx
  compose.tsx
  networks.tsx
  volumes.tsx
  images.tsx
```

The parent route owns shared layout, navigation links, guards, and genuinely
shared critical data. It renders an `Outlet`. Each child route owns its URL,
search validation, loader, and route-specific component. Tab controls become
typed `Link` navigation and inherit the global intent-preload policy.

This removes the page-level dependency on `TabContainer`, `useTabUrlState`,
untyped `useSearch({ strict: false })`, manual tab-key unions, and loaders that
switch data requirements based on a tab string. It also gives every page-level
tab its own automatic code-split boundary.

Changing child routes still unmounts the previous child component. That is the
expected navigation lifecycle, not a cache reset:

- Server state remains in the TanStack Query cache.
- Shareable selection and filter state belongs in validated URL search
  parameters.
- State shared by sibling routes belongs in the closest common parent.
- Only genuinely transient UI state remains local to the child.

Do not keep every page-level tab mounted and hidden. Doing so would keep
observers, polling, effects, and heavy component trees active for inactive
pages.

Tabs that do not represent pages remain local UI state. This includes settings
dialog tabs and detail tabs within a selected disk or other single resource.

### Data ownership rules

| Data category | Owner and consumption pattern |
| --- | --- |
| Critical for rendering the current URL | Closest route loader uses `ensureQueryData`; component uses `useSuspenseQuery` with the same endpoint options |
| Shared by sibling routes | Loader and observer in the closest common parent route |
| Specific to one child route | Child loader and child `useSuspenseQuery` observer |
| Dialog, expansion, or optional selection | Conditionally mounted or enabled `useQuery` |
| Polling, variable-range charts, or progressive data | `useQuery`; no speculative route-loader requirement |
| Event-driven validation, backfill, or path resolution | Existing endpoint `useFetcher` or `useAction` surface |
| Logs and terminal streams | Transport-readiness loader plus the existing stream lifecycle hook |

The presence of both a loader and a mounted query observer is not a hybrid
architecture. They coordinate through the same QueryClient entry. The decision
is whether a query is critical to the route's first render or intentionally
lazy/progressive.

### Targeted query cleanup

- **Updates and Shares:** child routes replace tab-dependent switch loaders.
  Critical child data is consumed with `useSuspenseQuery`.
- **Hardware:** history charts are progressive, range-dependent, and polled.
  Their speculative one-hour queries are excluded from the route loader; the
  cards retain their existing `useQuery` behavior.
- **Docker:** place each observer at the lowest route that consumes it.
  Container auto-update state belongs to the Containers child unless a
  documented product requirement makes it common to the Docker parent.
- **VMs:** make Machines a child route. Store the selected VM in validated
  search state with `loaderDeps`, unless VM detail later becomes a standalone
  path route.
- **WireGuard:** the page owns one interface-list observer and passes that
  cached result to the dashboard and create action. Network information is
  enabled only while the create-interface dialog is open.
- **Logs:** the service-list observer is enabled only for status filters that
  require service membership; `no_unit` and unfiltered logs do not request it.
- **Services:** repeated observers for the selected unit are valid because they
  share generated query options and one cache entry. Do not replace them with
  prop drilling or another wrapper.
- **Dashboard:** do not complicate its loader to account for hidden widgets
  until profiling demonstrates a material cost.

Interaction-driven queries remain intentionally lazy: selected WireGuard peer
data, QR codes, expanded changelogs, remote NFS/CIFS browsing, dialog
preflight/options, failed-login panels, file-browser details/search/editor
data, directory sizes, Docker icons, and similar on-demand reads.

### Suspense and error boundaries

Critical route data should suspend and fail at the route boundary. Removing
page-level `TabPanel` wrappers also removes their legacy catch-all boundary
from routed content.

Local error boundaries remain appropriate only where partial failure isolation
is deliberate, such as an optional dashboard widget or hardware card. The
shared local boundary integrates `useQueryErrorResetBoundary`; its retry action
resets the failed query before remounting the widget. The only custom static
fallback is used by settings sections, which use non-Suspense queries.

The route error component resets `useQueryErrorResetBoundary` when it mounts.
Its retry action invalidates the router so the loader reruns and the route
boundary resets.

### Completion record

The child-route work is complete for Shares, Accounts, Services, Storage,
Updates, Docker, and VMs, including typed links, route-owned loaders/search,
and topology regressions. The follow-up sequence is also complete:

1. Hardware, WireGuard, and Logs use the targeted lazy/progressive query
   ownership described above.
2. Local and route error boundaries reset failed TanStack Query state and have
   recovery regressions.
3. Browser-level coverage verifies direct links, refresh, back/forward
   navigation, pending/error UI, and production chunk boundaries.

Future work should not redesign the generated API layer, add a generic route
builder, convert every lazy query to Suspense, or introduce a new global state
store without a separate product or performance requirement.
