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
- Pending, error, and not-found UI are **router-wide defaults**. No route file
  overrides them.
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

`router/router.tsx` is the only place these are set. No route overrides any of
them, and `router.test.tsx` asserts that:

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

## The Shared Route Toolkit

Everything a route file needs comes from these four `-` prefixed modules:

| File | Exports | Use |
|------|---------|-----|
| `routes/-auth.ts` | `LinuxIORouterContext`, `requireAuthentication`, `requireGuest`, `requireAccess`, `sanitizeInternalRedirect` | `beforeLoad` guards and the router context type |
| `routes/-loader.ts` | `loadRouteQueries`, `loadRouteTransport`, `LoaderQueryOptions` | The only two loader entry points |
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
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.wireguard.list_interfaces.queryOptions(),
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
| `errorComponent` / `pendingComponent` / `notFoundComponent` | **Don't** | Router defaults |

## Loaders

### Where they live

Inline in the route file, as the `loader` option. There is no loader registry
and no separate loader directory. To learn what a route loads, read that one
file.

### How they are written

Every loader goes through one of two helpers from `src/routes/-loader.ts`. Route
code never calls `queryClient.ensureQueryData` directly.

```ts
loadRouteQueries(
  { context, preload },       // straight from the loader args
  [ /* endpoint.queryOptions(...) */ ],
): Promise<[...typed data tuple]>
```

`loadRouteQueries` does five things, in order:

1. Throws `LinuxIOError(…, "update_in_progress")` if `context.isUpdateBlocked()`.
2. Awaits `ensureLoaderRequestReady()` — the RPC transport may need reconnecting.
3. **Re-checks** the update state, closing the race where an update starts
   during the transport wait.
4. `Promise.all` over `queryClient.ensureQueryData` — parallel, deduped by query
   key, results in declaration order.
5. When `preload` is true (a hover-intent preload), tags each query
   `meta: { routeIntentPrefetch: true, silent: true }` so a speculative failure
   does not raise a global toast.

```ts
loadRouteTransport(context): Promise<void>
```

The stream-only variant for Logs and Terminal: same gating, no queries. Note the
asymmetric signature — it takes `context` directly, **not** `{ context, preload }`.

### Three rules

- **Never call `queryClient.ensureQueryData` from a route.** The gating in
  `loadRouteQueries` is not optional; bypassing it breaks the update boundary.
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
loader: ({ context, preload }) =>
  loadRouteQueries({ context, preload }, [
    linuxio.wireguard.list_interfaces.queryOptions(),
  ]),
```

```tsx
// wireguard/-components/WireguardPage.tsx
const { data: interfaces } = useSuspenseQuery(
  linuxio.wireguard.list_interfaces.queryOptions({ refetchInterval: 10000 }),
);
```

A loader plus a mounted observer is not a hybrid architecture — they coordinate
through one QueryClient entry. Use `useSuspenseQueries` when a component reads
several.

When a parent loads data for several children, the parent observes it once and
shares it through a small context — see `VMRouteDataContext` / `useVMRouteData()`
in `vm/-components/VMPage.tsx`.

### Loader shapes

Single query — `network/route.tsx`:

```ts
loader: ({ context, preload }) =>
  loadRouteQueries({ context, preload }, [
    linuxio.network.get_network_info.queryOptions(),
  ]),
```

Many queries, some capability-conditional — `_authenticated/index.tsx`:

```ts
loader: ({ context, preload }) => {
  const queries: LoaderQueryOptions[] = [
    linuxio.system.get_health_summary.queryOptions(),
    // …eleven more system/storage queries
  ];

  if (context.access.dockerAvailable === true) {
    queries.push(
      linuxio.docker.list_containers.queryOptions(),
      linuxio.docker.list_images.queryOptions(),
      linuxio.docker.list_networks.queryOptions(),
      linuxio.docker.list_volumes.queryOptions(),
    );
  }

  return loadRouteQueries({ context, preload }, queries);
},
```

Annotate a conditional array as `LoaderQueryOptions[]` so heterogeneous options
stay assignable.

Search-dependent, with a conditional detail query — `services/sockets.tsx`:

```ts
validateSearch: (search) => ({ ...optionalString(search, "socket") }),
loaderDeps: ({ search }) => ({ socket: search.socket }),
loader: ({ context, deps, preload }) => {
  const queries: LoaderQueryOptions[] = [
    linuxio.systemd.list_sockets.queryOptions(),
  ];
  if (deps.socket) {
    queries.push(linuxio.systemd.get_unit_info.queryOptions(deps.socket));
  }
  return loadRouteQueries({ context, preload }, queries);
},
```

`loaderDeps` is what gives a search change a distinct dependency identity so the
loader re-runs. **Path params are already loader deps** — do not add
`loaderDeps` for them.

Path param — `vm/machines/$name.tsx`:

```ts
loader: ({ context, params, preload }) =>
  loadRouteQueries({ context, preload }, [
    linuxio.virt.get.queryOptions(params.name),
  ]),
```

Splat — `filebrowser/$.tsx`, the one loader that passes per-call query options:

```ts
loader: ({ context, params, preload }) => {
  const path = params._splat ? `/${params._splat}` : "/";
  return loadRouteQueries({ context, preload }, [
    linuxio.filebrowser.resource_get.queryOptions(
      { path },
      { staleTime: CACHE_TTL_MS.NONE },
    ),
  ]);
},
```

Transport only — `logs/route.tsx` and `terminal/route.tsx`:

```ts
loader: ({ context }) => loadRouteTransport(context),
```

Capability early-return, for a page that degrades instead of 404ing —
`updates/index.tsx`:

```ts
loader: ({ context, preload }) => {
  if (context.access.packageKitAvailable !== true) return;
  return loadRouteQueries({ context, preload }, [
    linuxio.updates.get_updates_basic.queryOptions(),
  ]);
},
```

### Coverage rule

`router.test.tsx` enforces both halves, so a new route must satisfy them:

- Every protected **leaf** route has a loader on itself *or an ancestor*.
- Layout routes that own no data must have **no loader at all** — no empty no-op
  loaders. Currently that means `_authenticated`, `sign-in`, the six tab layouts,
  and `vm/machines`.

### Who owns which data

| Data | Owner |
|------|-------|
| Critical to render the current URL | Closest route loader + `useSuspenseQuery` on the same options |
| Shared by sibling routes | Loader and observer in the closest common parent |
| Specific to one child | That child's loader and observer |
| Dialog, expansion, optional selection | Conditionally mounted or `enabled` `useQuery` |
| Polling, variable-range charts, progressive | `useQuery`; keep it out of the loader |
| Event-driven validation or path resolution | `useFetcher` / `useAction` |
| Logs and terminal streams | `loadRouteTransport` + the stream lifecycle hook |

Interaction-driven reads stay lazy on purpose: WireGuard peer data and QR codes,
expanded changelogs, remote NFS/CIFS browsing, dialog preflight, failed-login
panels, file-browser search and editor content, directory sizes, Docker icons.
`routes/_authenticated/-query-ownership.test.ts` actively prevents Hardware
history charts, WireGuard network info, and the Logs service list from being
moved back into eager route loading.

For the endpoint layer itself — `queryOptions`, `queryKey`, `useFetcher`,
`useAction`, `useJobAction`, and the invalidation manifest — see
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

- Server state stays in the TanStack Query cache.
- Shareable selection and filters belong in validated search params, or in the
  path when they identify the page.
- State shared by siblings belongs in the closest common parent.
- Only genuinely transient UI state stays local to the child.

Do **not** keep every tab mounted and hidden — that keeps observers, polling,
effects, and heavy trees alive for pages nobody is looking at.

Tabs that are not pages stay local UI state: the settings dialog tabs, and the
detail tabs within a single selected disk.

## Pending, Error, And Not-Found UI

You get all of it from the router defaults. **No route file in `src/routes/`
declares `errorComponent` or `pendingComponent`** — keep it that way unless you
have a reason you can write down.

| Situation | Component | Behaviour |
|-----------|-----------|-----------|
| Loader in flight > 150 ms | `PageLoader` | Three dots, `role="status"` |
| Loader or render threw | `RouteError` | Error page with a working retry |
| No route matched | `NotFoundPage` | `__root__` and `_authenticated` also set it locally, so an unknown authenticated URL keeps the app shell |

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

On top of that, `useUpdateNavigationGuard` installs a `useBlocker` that rejects
transitions, and sidebar entries go inert via `useUpdateCanNavigate()`.
Intent-prefetched queries are tagged `silent`, so an unreliable hover preload
during an update does not produce a toast.

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
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.backups.list_repositories.queryOptions(),
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
  loader: ({ context, deps, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.backups.list_snapshots.queryOptions({ tag: deps.tag }),
    ]),
  component: SnapshotsLayout,
});
```

**5. Write the detail route** — `snapshots/$id.tsx`. No `loaderDeps`; path params
are already deps:

```tsx
export const Route = createFileRoute("/_authenticated/backups/snapshots/$id")({
  loader: ({ context, params, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.backups.get_snapshot.queryOptions(params.id),
    ]),
  component: SnapshotDetail,
});

function SnapshotDetail() {
  const { id } = Route.useParams();
  const { data: snapshot } = useSuspenseQuery(
    linuxio.backups.get_snapshot.queryOptions(id),
  );
  return <SnapshotPanel snapshot={snapshot} />;
}
```

**6. Hoist any child toolbar controls** with `<RoutedTabActions>`.

**7. Check the rules** before running anything:

- Does every leaf have a loader on itself or an ancestor?
- Do the layout routes that own no data have **no** loader?
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
| Keep every page-level tab mounted and hidden | Keeps observers, polling, and effects alive for invisible pages |
| Move progressive, polled, or dialog-only queries into a route loader | Guarded by `-query-ownership.test.ts` |
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
| `src/router/query-client.test.tsx` | Browser singleton vs. isolated clients |
| `src/routes/-auth.test.ts` | Redirect preservation, external-redirect rejection, `requireAccess` policies |
| `src/routes/-loader.test.tsx` | Shared-cache reuse, dedup, update races, declaration order, silent preload metadata, error propagation |
| `src/routes/_authenticated/-query-ownership.test.ts` | Keeps lazy/progressive queries out of loaders |
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
