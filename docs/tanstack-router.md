# TanStack Router architecture

Status: TanStack Router is the application's only routing path as of
2026-07-23.

## Route architecture

- Routes live in `frontend/src/routes/` and use `createFileRoute`.
- URL segments are real directories with a `route.tsx`, `index.tsx`, or
  `$.tsx` route file. Route-owned UI is co-located in `-components/` (and the
  dashboard in `-dashboard/`), using TanStack Router's native `-` ignore
  convention.
- `@tanstack/router-plugin` generates `routeTree.gen.ts` and automatically
  code-splits route components.
- `router.tsx` creates the typed application router from the generated tree.
- `routes/-provider.tsx` injects the live auth, capability, QueryClient, and
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
  the global not-found and error components.

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
| Authenticated shell | application version shown in the persistent footer |
| Dashboard | health, host, uptime, time, CPU, memory, filesystems, network, motherboard, GPU, drives, disk throughput, and Docker summaries when available |
| Network | network interfaces |
| Updates | available updates and history when the history tab is active |
| Services | active unit list for the selected section and selected-unit details |
| Logs | request-transport readiness; service filter data loads in background |
| Storage | disks/filesystems/NFS mounts, or LVM physical volumes, volume groups, and logical volumes |
| Docker | data for the active tab; auto-update state only for Containers |
| VMs | VM list, preflight status, and the initially selected VM detail |
| Accounts | active users/groups list and selected-user details/login history |
| Shares | NFS/Samba shares or NFS/CIFS mounts, according to the active tab |
| WireGuard | WireGuard interfaces |
| Hardware | sensors, PCI devices, memory modules, hardware summaries, and monitoring histories when available |
| Navigator | the resource for the current splat path |
| Terminal | request-transport readiness before the stream-only page mounts |

Search-dependent data is declared through `loaderDeps`, so a relevant search
change reruns the loader with a distinct dependency identity. Each query loader
returns the result of generated endpoint `queryOptions` loaded through the
router's shared QueryClient. Request failures propagate to the route error
boundary. The mounted `useQuery` observer consumes and subscribes to those same
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
- `routes/-provider.test.tsx` checks bootstrap gating and live
  router-context invalidation.

`routeTree.gen.ts` is generated code. It is committed for TypeScript consumers
but excluded from formatting.
