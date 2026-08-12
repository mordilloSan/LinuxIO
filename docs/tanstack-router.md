# TanStack Router

This is the canonical guide for LinuxIO's **frontend routing** — how URLs map to
files, where loaders and guards go, and what to do when you add a page.

## Summary

- Routes are **file-based**: a URL segment is a directory under
  `frontend/src/routes/`, and `@tanstack/router-plugin` generates a typed
  `routeTree.gen.ts` from it.
- A route file declares everything about its own URL — guard, search validation,
  loader, component, navigation metadata. There is no central route table.
- Four shared modules under `src/routes/` supply the primitives every route
  uses: `-auth.ts` (guards), `-loader.ts` (loaders), `-search.ts` (search
  validation), `-components/` (error and not-found UI).
- Pending and error UI use **router-wide defaults** with no route overrides.
  Not-found UI also has a global default, plus deliberate boundaries in
  `__root.tsx` and `_authenticated.tsx`.
- The sidebar is *derived* from route `staticData`; it is not a second catalogue
  you have to update.
- Loaders exist to warm the shared TanStack Query cache and to gate the
  transition. Components read the same data with `useSuspenseQuery`.
- Page-level tabs are real child routes, so each one gets its own URL, loader,
  and code-split chunk.

## Route File Conventions

| Pattern | Produces | Example |
|---------|----------|---------|
| `<segment>/route.tsx` | A layout route at `/<segment>` that renders an `Outlet` | `network/route.tsx` → `/network` |
| `<segment>/index.tsx` | The index child of that layout | `docker/index.tsx` → `/docker` |
| `<segment>/<name>.tsx` | A named child route | `docker/volumes.tsx` → `/docker/volumes` |
| `<segment>/$<param>.tsx` | A dynamic path param | `vm/machines/$name.tsx` → `/vm/machines/$name` |
| `<segment>/$.tsx` | A splat, read as `params._splat` | `filebrowser/$.tsx` → `/filebrowser/$` |
| `_<name>.tsx` | A **pathless** layout — wraps children, adds no URL segment | `_authenticated.tsx` |
| `-<anything>` | **Ignored** by the generator | `-components/`, `-loader.ts` |

The `-` prefix is how route-owned code lives next to its route without becoming
a route. Page components sit in `-components/`, the dashboard widgets in
`-dashboard/`, and the shared route toolkit is `-auth.ts` / `-loader.ts` /
`-search.ts`.

Every route files declares a `component`.

### The generated tree

`src/routeTree.gen.ts` regenerates automatically whenever Vite runs — dev server, production build,
or `vitest`, because `config/vitest.config.ts` loads the same plugin. If any new
route is not showing up in autocomplete, run any test and re-check.


## Router Setup

Three files in `frontend/src/router/`:

| File | Role |
|------|------|
| `router.tsx` | Creates the singleton router, sets every global default, declares the two type augmentations. |
| `provider.tsx` | Injects live auth/capability/query context and keeps it fresh. |
| `query-client.tsx` | Owns the browser QueryClient singleton that loaders and components share. |

### Global defaults

`router/router.tsx` is the only place the global defaults are set. No route
overrides the error or pending defaults. `__root.tsx` and `_authenticated.tsx`
deliberately declare local not-found boundaries, described below.
`router.test.tsx` asserts this policy:

```ts
export const router = createRouter({
  context: {
    access: undefined!,
    auth: undefined!,
    isUpdateBlocked: undefined!,
    queryClient: undefined!,
  } satisfies LinuxIORouterContext,
  defaultErrorComponent: RouteError,
  defaultNotFoundComponent: NotFoundPage,
  defaultPendingComponent: PageLoader,
  defaultPreload: "intent",
  defaultPreloadDelay: 50,
  defaultPendingMs: 150,
  defaultPendingMinMs: 0,
  defaultPreloadStaleTime: 0,
  routeTree,
  search: { strict: true },
});
```

The `undefined!` context values are deliberate placeholders — they satisfy the
type while the real values arrive per render from `RouterProvider`.

`defaultPreloadStaleTime: 0` hands freshness decisions to TanStack Query rather
than the router cache.

### Type augmentations

Two interfaces are augmented in the same block. `Register` makes `Link`,
`navigate`, and `getRouteApi` typed against the real tree.
`StaticDataRouteOption` is what makes `staticData` typed:

```ts
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }

  interface StaticDataRouteOption {
    access?: AccessPolicy;
    navigation?: RouteNavigation;
  }
}
```

```ts
interface RouteNavigation {
  icon: ElementType | string; // component, or an Iconify name
  params?: { _splat: string }; // only needed by /filebrowser/$
  position: number; // sidebar sort key
  title: string; // sidebar label
}
```

`AccessPolicy` (`hooks/useCapabilities.ts`) is
`{ requiredCapabilities?: CapabilityKey[]; requiresPrivileged?: boolean }`.

### Live context

`router/provider.tsx` gates the router on auth bootstrap and re-runs guards when
context changes:

```tsx
export default function ApplicationRouterProvider() {
  const { isInitialized } = useAuth();
  return isInitialized ? <ActiveApplicationRouterProvider /> : null;
}
```

Inside, the context is memoized and any identity change triggers
`router.invalidate()`, so `beforeLoad` and loaders re-evaluate after sign-in,
sign-out, or a capability change. Because the router never mounts with unknown
auth, `RouterAuthSnapshot` can narrow `isInitialized: true`.

`isUpdateBlocked` is the module-level getter `isLiveUpdateBlocked` from
`contexts/UpdateContext.tsx`, not a hook — the router mounts *above*
`UpdateProvider`, so it cannot consume that context directly. That is why
`-loader.ts` takes a getter rather than a boolean.

Mount chain: `index.tsx` → `App.tsx` → `AuthProvider` > `AppQueryClientProvider`
> `ApplicationRouterProvider`.

### The shared QueryClient

`router/query-client.tsx` exports `getAppQueryClient()`, the browser singleton.
This is *the* reason a route loader and a mounted `useSuspenseQuery` hit the same
cache entry instead of firing two requests. `createQueryClient()` is the
isolated variant for tests and SSR.

It also owns the global error toast, and skips it for queries tagged `silent`:

```ts
onError: (error, query) => {
  if (query.meta?.silent) return;
  toast.error(getErrorMessage(error));
},
```

That is what makes speculative hover preloads quiet — see
[Loaders](#loaders).

The default Query retry handles one ordinary transient failure, but does not
retry `connection_unavailable` or `outcome_unknown` after the Call transport
has applied its route-owned bounded policy. Route loaders are stricter still
and default their Query-layer attempt to `retry: false`. A component may opt
into a local retry policy when its UX benefits from one, but that is an explicit
exception.

## The Shared Route Toolkit

Everything a route file needs comes from these four `-` prefixed modules:

| File | Exports | Use |
|------|---------|-----|
| `routes/-auth.ts` | `LinuxIORouterContext`, `requireAuthentication`, `requireGuest`, `requireAccess`, `sanitizeInternalRedirect` | `beforeLoad` guards and the router context type |
| `routes/-loader.ts` | `LOADER_FRESHNESS`, `loadRouteQueries`, `loadRouteTransport`, `startRouteQueryPrefetches`, `LoaderQueryOptions` | Critical and deferred route work |
| `routes/-search.ts` | `optionalString`, `optionalNumber`, `optionalBoolean` | `validateSearch` helpers |
| `routes/-components/` | `RouteError`, `ErrorPage`, `NotFoundPage` | Wired as router defaults; you rarely touch these |

## Anatomy Of A Route

A complete, guarded, data-owning route — `routes/_authenticated/wireguard/route.tsx`:

```tsx
const access = {
  requiredCapabilities: ["wireguardAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/wireguard")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.wireguard.list_interfaces,
    ]),
  component: WireguardPage,
  staticData: {
    access,
    navigation: {
      icon: WireguardIcon,
      position: 80,
      title: "Wireguard",
    },
  },
});
```

Note the single `access` const feeding both `beforeLoad` and `staticData`. That
co-location is what stops route access and sidebar visibility from drifting, and
`router.test.tsx` asserts it.

Execution order per navigation: `validateSearch` → `beforeLoad` (parents first)
→ `loaderDeps` → `loader` → `component`.

Which options you actually need:

| Option | When | Default if omitted |
|--------|------|--------------------|
| `component` | Always | — |
| `loader` | The page needs data before first paint | Inherits ancestors' data |
| `loaderDeps` | The loader depends on search params | Path params are already deps |
| `validateSearch` | The route owns search params | No search params accepted |
| `beforeLoad` | Auth, capability, or privilege gating | Inherits `_authenticated` |
| `staticData` | The route belongs in the sidebar, or is gated | Hidden from sidebar |
| `errorComponent` / `pendingComponent` | **Don't** | Router defaults |
| `notFoundComponent` | Only for a deliberate boundary | Router default with fuzzy fallback |

## Loaders

### Where they live

Inline in the route file, as the `loader` option. There is no loader registry
and no separate loader directory. To learn what a route loads, read that one
file.

### How they are written

Every loader goes through the helpers in `src/routes/-loader.ts`. Route code
never calls QueryClient loading primitives directly.

```ts
loadRouteQueries(loaderArgs, [
  /* generated Call descriptor */
]): Promise<void>
```

`PRESENCE` is the documented default. A route passes the optional third
argument only when it needs a different named freshness policy:

| Policy | Behavior |
|--------|----------|
| `PRESENCE` | Use any cached value; fetch only when the entry is absent |
| `BACKGROUND` | Return cached data immediately and revalidate it when stale |
| `BLOCKING` | Await `fetchQuery`, subject to the query's `staleTime` |

`loadRouteQueries` then does the following:

1. Throws `LinuxIOError(…, "update_in_progress")` if either the router-context
   getter or the live stream mux reports an update. The mux check covers the
   first-load window before `UpdateProvider` mounts.
2. Awaits abortable `ensureLoaderRequestReady()` — the RPC transport may need
   reconnecting. This happens before cache inspection even when every required
   Query entry already contains usable data: route transitions require a live
   backend and cached data is not an offline-navigation contract.
3. **Re-checks** the update state, closing the race where an update starts
   during the transport wait.
4. Runs the selected QueryClient operation in parallel, deduped by query key.
5. Tags initial and intent-preload failures `silent`, so the route boundary is
   their single visible error owner. A stale background failure remains eligible
   for the global toast because cached UI is still visible.
6. Defaults the Query-layer attempt to `retry: false`; the RPC transport retains
   its own bounded reconnect retry, so the two layers do not multiply attempts.

Call descriptor loaders pass Router's loader arguments directly. The helper derives
`abortController.signal`, so cancellation reaches transport readiness and the
endpoint request without a repeated per-route signal field. Loader consumers
are ref-counted per query key: an abandoned navigation cancels a loader-started
Query only when no other loader or mounted observer still needs it. Background
freshness keeps that registration until its revalidation settles, even though
the loader itself returns cached data immediately.

If one required query rejects, `Promise.all` fails the route immediately but
does not cancel its siblings. Those requests finish and warm the shared cache,
so a route retry need not repeat successful members. Router cancellation still
aborts an abandoned batch through the normal ref-counted path.

On the bridge, bounded Calls, including the synchronous `tasks.get`,
`tasks.list`, and `tasks.cancel` primitives, consume explicit stream-abort
frames. Long-lived Task watch/events/data Channels retain their separate
lifecycle.

```ts
loadRouteTransport(context, abortController.signal): Promise<void>
```

The transport-only variant is used by Logs and Terminal and as the critical
shell gate for Dashboard and Hardware.

Optional route work uses the non-blocking helper after that gate:

```ts
startRouteQueryPrefetches(
  { context, preload, signal: abortController.signal },
  visibleWidgetQueries,
): void
```

The helper first enforces the same update and abort preconditions synchronously,
so invoking it while loading is blocked throws into the route. Once the work has
started, individual prefetch failures are silent and do not fail the route; the
mounted widget owns Suspense, retry, and error UI.

### Four rules

- **Never call QueryClient loading methods from a route.** Use the shared
  helpers so readiness, update blocking, retry, error ownership, and
  cancellation stay intact.
- **Pass Router loader arguments directly.** `PRESENCE` is the default; pass a
  named third argument only for an intentional `BACKGROUND` or `BLOCKING`
  exception. A source guard in `-query-ownership.test.ts` checks this contract.
- **Never use `Route.useLoaderData()`.** It appears zero times in this codebase
  and should stay that way. A loader's job is to populate the shared cache and
  suspend the transition; the component then observes that cache with
  `useSuspenseQuery`. Keeping one read path means polling, invalidation, and
  optimistic updates all behave identically whether the data arrived from a
  loader or a refetch.
- **`routeIntentPrefetch` is a diagnostic marker only.** Only `silent` has a
  consumer. Do not build behaviour on it.

### The pairing pattern

The loader and the component name the *same* endpoint options. The loader
usually passes none, and the component adds polling — the query key is identical
either way, so the component reuses the entry the loader seeded and simply
attaches an observer:

```tsx
// wireguard/route.tsx
loader: (loaderArgs) =>
  loadRouteQueries(loaderArgs, [
    linuxio.wireguard.list_interfaces,
  ]),
```

```tsx
// wireguard/-components/WireguardPage.tsx
const { data: interfaces } = useSuspenseQuery({
  ...linuxio.wireguard.list_interfaces,
  refetchInterval: 10000,
});
```

A loader plus a mounted observer is not a hybrid architecture — they coordinate
through one QueryClient entry. Use `useSuspenseQueries` when a component reads
several.

When a parent route loads data that several children need, the children **each
observe the same options directly**. Do not pass query data down through props or
a React context: direct observers share one cache entry, and each component then
re-renders only for the data it actually reads. Observer options still matter:
when the parent owns polling, children set `refetchOnMount: false` so mounting a
tab does not add a stale-query refetch outside that cadence.

`/vm` is the worked example. Its loader warms `virt.list` and `virt.preflight`;
`VMPage` observes both (it owns the poll cadence for the section, since it stays
mounted throughout), and each child observes only what it needs — `VMImagesPage`
takes preflight alone, so the 5-second list poll does not re-render it:

```tsx
const { data: preflight } = useSuspenseQuery({
  ...linuxio.virt.preflight({}),
  refetchOnMount: false,
});
```

Because the always-mounted parent declares `refetchInterval`, children omit it
and disable mount refetches while still reading fresh data — one owner of
cadence, with no extra intervals or tab-mount requests to drift.

The loader is the prefetch-and-readiness layer. The observer is the subscription.
Both are load-bearing.

### Loader shapes

Single query — `network/route.tsx`:

```ts
loader: (loaderArgs) =>
  loadRouteQueries(loaderArgs, [
    linuxio.network.get_network_info,
  ]),
```

Deferred visible widgets — `_authenticated/index.tsx`:

```ts
loader: async ({ abortController, context, preload }) => {
  await loadRouteTransport(context, abortController.signal);
  const cachedConfig = readConfigCache(context.auth.user?.id);
  if (!cachedConfig) return;

  const hiddenCards = new Set(cachedConfig.appSettings?.hiddenCards ?? []);
  const queries: LoaderQueryOptions[] = [];
  if (!hiddenCards.has("overview")) {
    queries.push(linuxio.system.get_host_info);
  }
  // Add the other visible card queries, plus Docker queries when capable.
  startRouteQueryPrefetches(
    { context, preload, signal: abortController.signal },
    queries,
  );
},
```

The real route constructs the array inline and also checks the Docker
capability. It reads only an existing per-user config cache: it never guesses
which cards are hidden on a first visit. With no cache, mounted visible cards
start their locally bounded queries themselves. Hardware uses the same pattern
for expanded sections, and collapsed sections unmount their observers.

Annotate conditional arrays as `LoaderQueryOptions[]` so heterogeneous options
stay assignable.

Search-dependent, with a conditional detail query — `services/sockets.tsx`:

```ts
validateSearch: (search) => ({ ...optionalString(search, "socket") }),
loaderDeps: ({ search }) => ({ socket: search.socket }),
loader: (loaderArgs) => {
  const { deps } = loaderArgs;
  const queries: LoaderQueryOptions[] = [
    linuxio.systemd.list_sockets,
  ];
  if (deps.socket) {
    queries.push(
      linuxio.systemd.get_unit_info({ unitName: deps.socket }),
    );
  }
  return loadRouteQueries(loaderArgs, queries);
},
```

`loaderDeps` is what gives a search change a distinct dependency identity so the
loader re-runs. **Path params are already loader deps** — do not add
`loaderDeps` for them.

Path param — `vm/machines/$name.tsx`:

```ts
loader: (loaderArgs) =>
  loadRouteQueries(loaderArgs, [
    linuxio.virt.get({ name: loaderArgs.params.name }),
  ]),
```

Splat — `filebrowser/$.tsx`, the one loader that passes per-call query options:

```ts
loader: (loaderArgs) => {
  const { params } = loaderArgs;
  const path = params._splat ? `/${params._splat}` : "/";
  return loadRouteQueries(
    loaderArgs,
    [
      {
        ...linuxio.filebrowser.resource_get({ path }),
        ...fileBrowserListingQueryOptions,
      },
    ],
    LOADER_FRESHNESS.BACKGROUND,
  );
},
```

`fileBrowserListingQueryOptions` gives both loader and observer the same
two-second `staleTime`. That short grace keeps the freshly loaded listing fresh
while the observer mounts, eliminating the immediate duplicate request, while
the `BACKGROUND` policy still revalidates stale or invalidated listings on a
later navigation without hiding the cached directory.

Transport only — `logs/route.tsx` and `terminal/route.tsx`:

```ts
loader: ({ abortController, context }) =>
  loadRouteTransport(context, abortController.signal),
```

Capability early-return, for a page that degrades instead of 404ing —
`updates/index.tsx`:

```ts
loader: (loaderArgs) => {
  const { context } = loaderArgs;
  if (context.access.packageKitAvailable !== true) return;
  return loadRouteQueries(loaderArgs, [
    linuxio.updates.get_updates_basic,
  ]);
},
```

### Coverage rule

`router.test.tsx` enforces both halves, so a new route must satisfy them:

- Every protected **leaf** route has a loader on itself *or an ancestor*.
- Layout routes that own no data must have **no loader at all** — no empty no-op
  loaders. Currently that means `_authenticated`, `sign-in`, the six tab layouts,
  and `vm/machines`.

### Route data at a glance

Which helper each route uses, and what its loader depends on. `×N` is the number
of `queryOptions` declared; `+cond` means some are conditional. Unless labeled
otherwise, `loadRouteQueries` rows use `PRESENCE`. A `—` loader means the route
inherits its ancestor's.

| Route | File | Loader | Deps | Search | Guard |
|-------|------|--------|------|--------|-------|
| `/sign-in` | `sign-in/route.tsx` | — | — | `redirect` | `requireGuest` |
| *(pathless)* | `_authenticated.tsx` | — | — | — | `requireAuthentication` |
| `/` | `_authenticated/index.tsx` | transport + deferred ×16 +cond | — | — | — |
| `/accounts` | `accounts/route.tsx` | — | — | — | — |
| `/accounts/` | `accounts/index.tsx` | `loadRouteQueries` ×3 +cond | `user` | `user` +3 | — |
| `/accounts/groups` | `accounts/groups.tsx` | `loadRouteQueries` ×1 | — | — | — |
| `/docker` | `docker/route.tsx` | — | — | — | `requireAccess` docker |
| `/docker/` | `docker/index.tsx` | `loadRouteQueries` ×5 | — | — | — |
| `/docker/compose` | `docker/compose.tsx` | `loadRouteQueries` ×1 | — | — | — |
| `/docker/containers` | `docker/containers.tsx` | `loadRouteQueries` ×2 | — | `container` | — |
| `/docker/images` | `docker/images.tsx` | `loadRouteQueries` ×1 | — | — | — |
| `/docker/networks` | `docker/networks.tsx` | `loadRouteQueries` ×1 | — | — | — |
| `/docker/volumes` | `docker/volumes.tsx` | `loadRouteQueries` ×1 | — | — | — |
| `/filebrowser/$` | `filebrowser/$.tsx` | `BACKGROUND` ×1 | *params* | `enabled`, `redirect`, `tail` | — |
| `/hardware` | `hardware/route.tsx` | transport + deferred ×7 +cond | — | — | `requireAccess` lmSensors |
| `/logs` | `logs/route.tsx` | `loadRouteTransport` | — | — | — |
| `/network` | `network/route.tsx` | `loadRouteQueries` ×1 | — | `iface`, `sort`, `tab` | — |
| `/services` | `services/route.tsx` | — | — | — | — |
| `/services/` | `services/index.tsx` | `loadRouteQueries` ×2 +cond | `service` | `service` | — |
| `/services/sockets` | `services/sockets.tsx` | `loadRouteQueries` ×2 +cond | `socket` | `socket` | — |
| `/services/timers` | `services/timers.tsx` | `loadRouteQueries` ×2 +cond | `timer` | `timer` | — |
| `/shares` | `shares/route.tsx` | — | — | — | — |
| `/shares/` | `shares/index.tsx` | `loadRouteQueries` ×2 | — | — | — |
| `/shares/mounts` | `shares/mounts.tsx` | `loadRouteQueries` ×2 | — | — | — |
| `/storage` | `storage/route.tsx` | — | — | — | — |
| `/storage/` | `storage/index.tsx` | `loadRouteQueries` ×3 | — | `drive`, `fs` | — |
| `/storage/lvm` | `storage/lvm.tsx` | `loadRouteQueries` ×3 | — | — | — |
| `/terminal` | `terminal/route.tsx` | `loadRouteTransport` | — | — | — |
| `/updates` | `updates/route.tsx` | — | — | — | — |
| `/updates/` | `updates/index.tsx` | `loadRouteQueries` ×1 +cond | — | — | — |
| `/updates/history` | `updates/history.tsx` | `loadRouteQueries` ×1 +cond | — | — | — |
| `/vm` | `vm/route.tsx` | `loadRouteQueries` ×2 | — | — | `requireAccess` libvirt, privileged |
| `/vm/` | `vm/index.tsx` | — | — | — | — |
| `/vm/images` | `vm/images.tsx` | — | — | — | — |
| `/vm/networks` | `vm/networks.tsx` | — | — | — | — |
| `/vm/machines` | `vm/machines/route.tsx` | — | — | — | — |
| `/vm/machines/` | `vm/machines/index.tsx` | — | — | — | — |
| `/vm/machines/$name` | `vm/machines/$name.tsx` | `loadRouteQueries` ×1 | *params* | — | — |
| `/wireguard` | `wireguard/route.tsx` | `loadRouteQueries` ×1 | — | — | `requireAccess` wireguard, privileged |

Paths under `_authenticated/` are shown relative to it. The four `/vm*` rows with
no loader inherit `/vm`'s — that is the intended shape, not an omission.

### Who owns which data

| Data | Owner |
|------|-------|
| Critical to render the current URL | Closest route loader + `useSuspenseQuery` on the same options |
| Shared by sibling routes | Loader in the closest common parent; each consumer observes the same options itself |
| Specific to one child | That child's loader and observer |
| Dialog, expansion, optional selection | Conditionally mounted or `enabled` `useQuery` |
| Visible optional Dashboard/Hardware widget | Transport gate + deferred prefetch + local Suspense/ErrorBoundary |
| Polling, variable-range charts, progressive | `useQuery`; keep it out of the loader |
| Event-driven validation or path resolution | `useFetcher` / `useAction` |
| Logs and terminal streams | `loadRouteTransport` + the stream lifecycle hook |

Interaction-driven reads stay lazy on purpose: WireGuard peer data and QR codes,
expanded changelogs, remote NFS/CIFS browsing, dialog preflight, failed-login
panels, file-browser search and editor content, directory sizes, Docker icons.
`routes/_authenticated/-query-ownership.test.ts` actively preserves Dashboard
and Hardware's non-atomic shells, default freshness and direct loader-argument
cancellation, progressive Hardware histories, lazy WireGuard network info, and
the gated Logs service list.

Dashboard card boundaries use `DashboardCardSkeleton` fallbacks with the same
frosted frame and stats-only or split/chart geometry as the resolved cards.
`WidgetLoader` remains the generic fallback for non-card Hardware sections.

For the endpoint layer itself — Call descriptors, `useCallMutation`, remaining
`queryOptions`/`useFetcher`/`useAction`, `useTaskAction`, and the invalidation
manifest — see
[API Contract](./api-contract.md#frontend-shape). Do not add parallel
`queryOptions` factories, duplicate query-key constants, or `useSomethingQuery`
wrappers that only hide an observer.

## Guards And Access

All guarding goes through `routes/-auth.ts`. There are six `beforeLoad` hooks in
the whole tree:

| Route | Guard | Policy |
|-------|-------|--------|
| `_authenticated.tsx` | `requireAuthentication` | signed in |
| `sign-in/route.tsx` | `requireGuest` | signed **out** |
| `docker/route.tsx` | `requireAccess` | `dockerAvailable` |
| `hardware/route.tsx` | `requireAccess` | `lmSensorsAvailable` |
| `vm/route.tsx` | `requireAccess` | `libvirtAvailable` + privileged |
| `wireguard/route.tsx` | `requireAccess` | `wireguardAvailable` + privileged |

`requireAccess` throws `notFound()` — a 404, **deliberately not a 403**, so an
unavailable capability is not disclosed to an unauthorised caller. This is
intentional; do not "fix" it.

Because a gated route also puts its policy in `staticData.access`, the sidebar
hides what the guard would reject. See
[Capabilities](./capabilities.md#1-whole-route-gating-docker-hardware-vms-wireguard).

### Redirect after sign-in

Entirely router-driven — there is no manual `navigate()` in the login form:

1. `requireAuthentication` throws
   `redirect({ to: "/sign-in", search: { redirect: <full href> } })`.
2. `/sign-in` re-sanitizes that param in its own `validateSearch`.
3. Signing in mutates auth state → the provider's context identity changes →
   `router.invalidate()` → `/sign-in`'s `beforeLoad` re-runs → `requireGuest`
   throws a redirect back to the original URL.

`sanitizeInternalRedirect` is security-critical: it rejects non-strings, anything
not starting with `/`, and `//`-prefixed network-path references, then re-parses
against an opaque origin and re-checks the normalized result. Any new redirect
sink must go through it. `routes/-auth.test.ts` pins this, including
external-redirect rejection.

## Search Parameters

No schema library. Nine routes validate search using three generic helpers from
`routes/-search.ts`:

```ts
validateSearch: (search) => ({
  ...optionalBoolean(search, "enabled"),
  ...optionalString(search, "redirect"),
  ...optionalNumber(search, "tail"),
}),
```

Each helper returns `{ key: value }` when the value has the right type, and `{}`
otherwise. Spreading means an absent or invalid param yields **no key at all**
rather than an explicit `undefined` — which, combined with
`search: { strict: true }`, keeps URLs free of empty params.

Read and write search through the route's own API, using a functional update so
you never clobber sibling params:

```tsx
const search = Route.useSearch();
const navigate = Route.useNavigate();
const setSelected = useCallback(
  (service: string | null) =>
    navigate({
      search: (previous) => ({ ...previous, service: service ?? undefined }),
      to: "/services",
    }),
  [navigate],
);
```

From a component outside the route file, use
`getRouteApi("/_authenticated/network")` instead of importing the route.

**Search or path?** Use a path param when the value identifies *what the page is
showing* and deserves its own URL (`/vm/machines/$name`). Use search for
filters, sort order, and optional UI state layered on top of a page
(`/network?iface=eth0&tab=traffic`).

## Navigation Metadata And The Sidebar

Add `staticData.navigation` and the route appears in the sidebar. There is no
second list to update — `useSidebarItems.ts` is the whole implementation:

```ts
export function useSidebarItems(): SidebarItem[] {
  const access = useAccessContext();
  const router = useRouter();

  return Object.values(router.routesById)
    .filter((route) => {
      const { staticData } = route.options;
      return (
        staticData?.navigation && hasAccessPolicy(staticData.access, access)
      );
    })
    .sort(
      (a, b) =>
        a.options.staticData!.navigation!.position -
        b.options.staticData!.navigation!.position,
    )
    .map((route) => ({
      icon: route.options.staticData!.navigation!.icon,
      params: route.options.staticData!.navigation!.params,
      title: route.options.staticData!.navigation!.title,
      to: route.fullPath,
    }));
}
```

Current `position` ladder — pick a gap, and leave room:

| 0 | 10 | 20 | 30 | 35 | 40 | 50 | 55 | 60 | 70 | 80 | 90 | 100 | 110 |
|---|----|----|----|----|----|----|----|----|----|----|----|-----|-----|
| Dashboard | Network | Updates | Services | Logs | Storage | Docker | VMs | Accounts | Shares | Wireguard | Hardware | Navigator | Terminal |

## Page Tabs Are Child Routes

A tabbed page is a layout route plus one child route per tab. Each tab gets a
real URL, its own loader and search validation, and its own lazy chunk. Tab
controls are typed `Link`s that inherit the global intent-preload policy.

```text
docker/
  route.tsx        -> /docker          (layout: tabs + Outlet)
  index.tsx        -> /docker          (Dashboard tab)
  containers.tsx   -> /docker/containers
  compose.tsx      -> /docker/compose
  networks.tsx     -> /docker/networks
  volumes.tsx      -> /docker/volumes
  images.tsx       -> /docker/images
```

The pieces live in `components/tabbar/RoutedTabContainer.tsx`:

| Export | Use |
|--------|-----|
| `makeTabLayout(tabs, containerStyle?)` | Builds a layout component that renders the tab strip and an `Outlet`. Use it when the layout is *only* tabs. |
| `RoutedTabLayout` | The same layout as a component, for when the parent must wrap or render alongside the `Outlet`. |
| `RoutedTabActions` | Lets a child route portal toolbar buttons into the parent's tab strip. |
| `RoutedTab` | The tab manifest type. |

The tab selector is a memoized sibling of the routed panel. Polling or local
state updates inside the active child route therefore do not rebuild the tab
links, while each TanStack `Link` still observes location changes so real
navigation updates the selected pill. Portaled route actions can update inside
the persistent selector without invalidating its links.

Tab manifests live at `<group>/-components/<group>Tabs.ts`:

```ts
export const DOCKER_TABS = [
  { label: "Dashboard", to: "/docker" },
  { label: "Containers", to: "/docker/containers" },
  { label: "Stacks", to: "/docker/compose" },
  { label: "Networks", to: "/docker/networks" },
  { label: "Volumes", to: "/docker/volumes" },
  { label: "Images", to: "/docker/images" },
] as const satisfies readonly RoutedTab[];
```

`to` is checked against the generated tree, and routes needing path params are
excluded from the target type — a tab renders a bare `Link`, so it cannot supply
params.

Most layouts are then three lines (`docker/route.tsx`):

```tsx
const DockerLayout = makeTabLayout(DOCKER_TABS);

export const Route = createFileRoute("/_authenticated/docker")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  component: DockerLayout,
  staticData: { access, navigation: { ... } },
});
```

When the parent needs to wrap the tabs — a provider, a banner — use
`RoutedTabLayout` directly instead (`updates/route.tsx`, `vm/-components/VMPage.tsx`):

```tsx
function UpdatesLayout() {
  return (
    <PackageUpdateControllerProvider>
      <RoutedTabLayout containerStyle={{ paddingInline: 0 }} tabs={UPDATES_TABS}>
        <Outlet />
      </RoutedTabLayout>
    </PackageUpdateControllerProvider>
  );
}
```

A child route hoists its own actions into the persistent strip, so the strip
stays mounted while the child changes:

```tsx
return (
  <>
    <RoutedTabActions>
      <UnitViewToggle viewModeKey="services.list" />
    </RoutedTabActions>
    <ServicesTab … />
  </>
);
```

### Tabs that own detail routes

A tab may nest further. `/vm/machines` is a layout holding the machine list; the
selected machine is a child route, `/vm/machines/$name`, rendered in its outlet.
The list and its mutations stay mounted across selection changes.

Set `matchChildren` so the pill stays selected while a detail route is open —
without it the tab uses exact matching and would deselect:

```ts
{ label: "Virtual machines", matchChildren: true, to: "/vm/machines" },
```

### State ownership when the child unmounts

Changing child routes unmounts the previous child. That is the navigation
lifecycle, not a cache reset:

- Server state stays in the TanStack Query cache — siblings re-observe it, they
  do not receive it as props.
- Shareable selection and filters belong in validated search params, or in the
  path when they identify the page.
- Non-server state shared by siblings (drawer open, expanded rows) belongs in the
  closest common parent.
- Only genuinely transient UI state stays local to the child.

Do **not** keep every tab mounted and hidden — that keeps observers, polling,
effects, and heavy trees alive for pages nobody is looking at.

Tabs that are not pages stay local UI state: the settings dialog tabs, and the
detail tabs within a single selected disk.

## Pending, Error, And Not-Found UI

Error and pending UI come from the router defaults. **No route file in
`src/routes/` declares `errorComponent` or `pendingComponent`** — keep it that
way unless you have a reason you can write down. Not-found UI uses the global
default plus two deliberate local boundaries.

| Situation | Component | Behaviour |
|-----------|-----------|-----------|
| Loader in flight > 150 ms | `PageLoader` | Three dots, `role="status"` |
| Loader or render threw | `RouteError` | Error page with a working retry |
| No route matched | `NotFoundPage` | Fuzzy matching renders the default at the nearest matching route, preserving its parent layout |

The two local not-found boundaries solve cases that fuzzy unmatched-URL fallback
does not:

- `__root.tsx` renders `RootNotFound`, which adds `BootstrapLoaderReady` beside
  `NotFoundPage` so a top-level terminal 404 removes the initial HTML splash.
- `_authenticated.tsx` renders `NotFoundPage` for a manual `notFound()` thrown
  by an access gate, keeping that 404 inside the authenticated branch. This
  boundary is not what preserves the layout for an ordinary unknown URL; fuzzy
  fallback already renders that URL's default not-found UI beneath the nearest
  matched layout.

The pending values are an intentional low-latency policy: 50 ms intent delay,
150 ms before pending UI, zero minimum pending duration, and zero Router preload
staleness. `router.test.tsx` pins the exact values. Change them only with measured
navigation evidence, not to follow Router's larger defaults mechanically.

`RouteError` is the recovery contract, and it is short:

```tsx
function RouteError({ error }: ErrorComponentProps) {
  const router = useRouter();
  const queryErrorResetBoundary = useQueryErrorResetBoundary();

  useEffect(() => {
    queryErrorResetBoundary.reset();
  }, [queryErrorResetBoundary]);

  const handleRetry = () => {
    void router.invalidate();
  };

  return (
    <>
      <ErrorPage error={error} onRetry={handleRetry} />
      <BootstrapLoaderReady />
    </>
  );
}
```

Resetting on mount clears React Query's error latch so the reran loader can
refetch; `router.invalidate()` is what rerun means. Because the boundary sits at
the failing route, a failed child renders its error *inside* the parent layout —
the tab strip and sidebar survive.

Two tiers, and they are not interchangeable:

- **`RouteError`** recovers a whole failed route load.
- **`components/errors/ErrorBoundary.tsx`** isolates one optional widget — a
  dashboard card, a hardware sensor panel, a settings section. Use it only where
  partial failure isolation is deliberate. Its retry also resets the failed query
  before remounting.

Initial critical-query failures are tagged `routeInitialLoad` and `silent`:
`RouteError` is their one visible owner, without a duplicate global toast.
Failures refreshing already-visible cached data are not silent and retain the
global toast. Deferred Dashboard/Hardware prefetches are silent because each
widget's local boundary owns the retry and error UI after it mounts.

One wiring detail worth knowing: `PageLoader` renders `div.page-loader`, and
`BootstrapLoaderReady` removes the HTML splash and un-`inert`s `#root` once no
page loader remains. Authenticated pages inherit this through `MainLayout`; a new
*top-level* terminal route would need to render `BootstrapLoaderReady` itself.

## Preloading And Code Splitting

One application-wide policy, set once in `router/router.tsx`:
`defaultPreload: "intent"`, `defaultPreloadDelay: 50`,
`defaultPreloadStaleTime: 0`. Links inherit it. **Do not add per-link or
per-route preload options** — `router.test.tsx` fails if any route declares
`preload`, `preloadDelay`, or `preloadStaleTime`.

`autoCodeSplitting: true` makes the plugin split every route's component, pending
UI, error UI, and not-found UI into its own lazy chunk, while leaving search
validation, guards, loaders, and static data in the eager bundle so matching and
intent preloading still work. You never write `.lazy.tsx` files, and there are
none.

`src/test/browser/router.spec.ts` enforces the boundary against a real
production build: it discovers route files from disk, then asserts each has a
distinct `isDynamicEntry` chunk in the Vite manifest. Because the list is
derived, a new route is covered the moment you add it.

## The Update Boundary

While a live application update is running, two independent mechanisms stop
traffic — the doc-level distinction matters when debugging:

1. **Loaders hard-fail.** `loadRouteQueries` / `loadRouteTransport` throw
   `LinuxIOError(…, "update_in_progress")` before and after transport readiness.
2. **Mounted queries pause.** `UpdateContext` flips the stream multiplexer's
   updating flag; `isRequestAvailable()` goes false; `query-client.tsx` feeds that
   into `onlineManager`, so React Query treats the app as offline. The
   multiplexer itself does not reject calls.

This distinction is intentional. When the mux is unavailable, a mounted
observer may keep rendering usable cached data while React Query pauses new
network work, but a new route transition still waits for transport readiness
before inspecting that cache and may enter its error boundary. LinuxIO does not
offer offline route navigation.

On top of that, `useUpdateNavigationGuard` installs a `useBlocker` that rejects
transitions, and sidebar entries go inert via `useUpdateCanNavigate()`.
Intent-prefetched queries are tagged `silent`, so an unreliable hover preload
during an update does not produce a toast.

The loader gate checks both `context.isUpdateBlocked()` and the live
`getStreamMux()?.isUpdating` flag before and after readiness. The latter closes
the initial-load gap before `UpdateProvider` has published context state.

## Adding A Route — Checklist

Worked example: a new tabbed **Backups** page with a list tab and a detail child,
gated on a `resticAvailable` capability.

**1. Create the group.**

```text
routes/_authenticated/backups/
  route.tsx              -> /backups           (layout)
  index.tsx              -> /backups           (Overview tab)
  snapshots/
    route.tsx            -> /backups/snapshots (layout: list + Outlet)
    index.tsx            -> /backups/snapshots (no selection)
    $id.tsx              -> /backups/snapshots/$id
  -components/
    backupsTabs.ts
    BackupsOverview.tsx
    SnapshotsLayout.tsx
```

**2. Write the tab manifest** — `-components/backupsTabs.ts`:

```ts
import type { RoutedTab } from "@/components/tabbar";

export const BACKUPS_TABS = [
  { label: "Overview", to: "/backups" },
  { label: "Snapshots", matchChildren: true, to: "/backups/snapshots" },
] as const satisfies readonly RoutedTab[];
```

**3. Write the parent layout** — `backups/route.tsx`:

```tsx
const access = {
  requiredCapabilities: ["resticAvailable"],
} satisfies AccessPolicy;

const BackupsLayout = makeTabLayout(BACKUPS_TABS);

export const Route = createFileRoute("/_authenticated/backups")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.backups.list_repositories,
    ]),
  component: BackupsLayout,
  staticData: {
    access,
    navigation: { icon: BackupIcon, position: 45, title: "Backups" },
  },
});
```

One `access` const, used twice. Position 45 slots between Storage (40) and
Docker (50).

**4. Write a child that owns search state** — `snapshots/route.tsx`:

```tsx
export const Route = createFileRoute("/_authenticated/backups/snapshots")({
  validateSearch: (search) => ({ ...optionalString(search, "tag") }),
  loaderDeps: ({ search }) => ({ tag: search.tag }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.backups.list_snapshots({
        tag: loaderArgs.deps.tag,
      }),
    ]),
  component: SnapshotsLayout,
});
```

**5. Write the detail route** — `snapshots/$id.tsx`. No `loaderDeps`; path params
are already deps:

```tsx
export const Route = createFileRoute("/_authenticated/backups/snapshots/$id")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.backups.get_snapshot({ id: loaderArgs.params.id }),
    ]),
  component: SnapshotDetail,
});

function SnapshotDetail() {
  const { id } = Route.useParams();
  const { data: snapshot } = useSuspenseQuery(
    linuxio.backups.get_snapshot({ id }),
  );
  return <SnapshotPanel snapshot={snapshot} />;
}
```

**6. Hoist any child toolbar controls** with `<RoutedTabActions>`.

**7. Check the rules** before running anything:

- Does every leaf have a loader on itself or an ancestor?
- Do the layout routes that own no data have **no** loader?
- Does every Query loader pass `loaderArgs` directly, leaving the default
  `PRESENCE` policy implicit and naming only intentional exceptions?
- Did you avoid `errorComponent`, `pendingComponent`, and per-route `preload`?
- Is the capability declared in both places
  ([Capabilities](./capabilities.md#adding-a-capability--checklist))?

**8. Update `router.test.tsx`.** It asserts the full topology and the sidebar
order, so add your paths to the expected lists — and to the no-loader list if you
added a data-free layout. This is the one test that intentionally needs editing
per route; the chunk-boundary test does not.

**9. Verify.**

```bash
make check-frontend
```

## Conventions And Anti-Patterns

Settled decisions. Reopen them with a product or performance requirement, not by
preference.

| Don't | Why |
|-------|-----|
| Add a per-route `preload`, `errorComponent`, or `pendingComponent` | One global policy is the point; two tests enforce it |
| Add an empty no-op loader to a layout route | It costs a transition and hides which route owns data |
| Call `queryClient.ensureQueryData` in a route | Bypasses transport readiness and the update boundary |
| Use `Route.useLoaderData()` | Splits the read path; observe the cache instead |
| Add parallel `queryOptions` factories, duplicate query keys, or `useSomethingQuery` wrappers | The generated endpoint layer is the single definition — see [API Contract](./api-contract.md) |
| Pass query data to children through props or a React context | Direct observers share one cache entry and let each consumer track only the data it reads; a shared context value re-renders every consumer on every poll, even ones reading other fields |
| Wrap a route builder around `createFileRoute` | The plugin matches the literal callee name, so a wrapper silently disables code splitting for that route |
| Keep every page-level tab mounted and hidden | Keeps observers, polling, and effects alive for invisible pages |
| Move progressive, polled, or dialog-only queries into a route loader | Guarded by `-query-ownership.test.ts` |
| Suspend on an endpoint no loader in the route's branch declares | It is absent from both critical loading and deferred intent prefetch. Guarded by `-suspense-loader-coverage.test.ts` |
| Build a generic route builder, component registry, or route catalogue | All previously existed and were removed; file-based routing replaces them |
| Convert every lazy query to Suspense | Lazy is deliberate for interaction-driven reads |
| Introduce another global state store | Query cache + URL + closest common parent has covered every case so far |
| Return a 403 from a capability guard | 404 avoids disclosing host capabilities |

## Testing

### Rendering a route under test

`src/test/render.tsx` provides the harness — you do not need the real router:

| Helper | Use |
|--------|-----|
| `renderWithTanStackRouter(ui, options)` | Renders inside a throwaway `createRootRoute` + `createMemoryHistory` router |
| `createTanStackRouterWrapper(options)` | The wrapper alone, for `renderHook` |
| `createAuthContextValue(...)` | Fabricates auth and capability state for guard tests |
| `createTestQueryClient()` | An isolated QueryClient |

For a component that calls `getRouteApi(...)`, mock the module's `getRouteApi`
(and `useParams` if the component reads path params) — see
`vm/-components/VMPage.test.tsx`.

### Router regressions

| File | Covers |
|------|--------|
| `src/router/router.test.tsx` | Global defaults, full route topology, loader coverage, no per-route preload, sidebar order, access co-location |
| `src/router/provider.test.tsx` | Bootstrap gating, one `invalidate()` per context change |
| `src/router/query-client.test.tsx` | Browser singleton, isolated clients, and silent-vs-background error ownership |
| `src/routes/-auth.test.ts` | Redirect preservation, external-redirect rejection, `requireAccess` policies |
| `src/routes/-loader.test.tsx` | Freshness modes, `Promise<void>`, deferred work, rapid-navigation cancellation, shared consumers, retry/error policy, and update races |
| `src/routes/_authenticated/-query-ownership.test.ts` | Keeps lazy/progressive queries out of critical loading and enforces default freshness, explicit exceptions, and direct loader arguments |
| `src/routes/-suspense-loader-coverage.test.ts` | Walks the import graph and proves each suspense endpoint is declared as critical or deferred work in its loader branch |
| `src/routes/-components/RouteError.test.tsx` | Route-boundary recovery |
| `src/components/errors/ErrorBoundary.test.tsx` | Widget-boundary recovery |
| `src/components/tabbar/RoutedTabContainer.test.tsx` | Tab links, active child URL, `matchChildren`, mobile action slot |
| `sidebar/useSidebarItems.test.tsx` | Access filtering, `position` ordering, no preload overrides |
| `sidebar/useCloseMobileSidebarOnNavigate.test.tsx` | Mobile drawer close-on-navigate |
| `src/utils/navigation.test.ts`, `src/contexts/UpdateContext.navigationGuard.test.tsx` | The update navigation blocker |
| `src/test/browser/router.spec.ts` | Chromium: direct links, refresh, back/forward, pending and error UI, first-navigation chunk load, and the derived production chunk-boundary assertion |

### Scope of the browser suite

`router.spec.ts` runs against a self-contained fixture in
`src/test/browser/fixture/` — no credentials, no backend, no host mutation. It
protects router *mechanics* and production chunk boundaries.

It deliberately does **not** cover real authentication, the WebSocket bridge, or
backend data. Those belong to Tiers 2 and 3 of the
[three-tier test plan](./e2e-testing.md), which are **not yet implemented**:
Tier 2 (local Playwright against a real session, non-privileged queries) and
Tier 3 (VM Playwright for privileged queries and safe mutations) still have open
done-criteria. Until they land, an authenticated end-to-end regression is a
manual check — do not assume `make test-frontend-browser` passing means a routed
page works against a live bridge.

## Verification

```bash
make check-frontend          # lint + tsc in parallel, then the vitest suite
make test-frontend-only      # vitest alone
make setup-frontend-browser  # one-time: playwright install chromium
make test-frontend-browser   # production vite build, then Chromium suite
```

`make test-frontend-browser` builds first on purpose: the chunk-boundary
assertion reads the real Vite manifest, so it cannot run against a stale or
missing build.
