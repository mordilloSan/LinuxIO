# TanStack Router baseline

Status: the code-based TanStack Router architecture is the only application
routing path as of 2026-07-23.

Docker was not available on this host. No Docker runtime, authenticated browser,
profiling, or integration validation was performed.

## Runtime architecture

- `App` mounts auth, React Query, and then `AppRouterProvider` after bootstrap.
- `AppRouterProvider` owns one router instance and refreshes its live auth,
  access, query-client, and update-blocking context.
- `appRouteRegistry.tsx` supplies route components and four named data loaders
  directly to the code-based router factory.
- `protectedRouteCatalog.ts` is the single source for protected-route paths,
  access policy, and sidebar presentation metadata.
- Authentication, capability, and privilege checks are native `beforeLoad`
  guards and run before protected child loaders.
- Route search is validated per route with global strict search enabled.
- File browser uses the native `filebrowser/$` splat and `_splat` parameter.
- Update navigation blocking uses the router's native blocker.

## Native route preloading

The installed `@tanstack/react-router` 1.170.18 and router core 1.171.15 were
inspected before implementation. `Link preload="intent"` calls
`router.preloadRoute()`, whose route-chunk phase invokes
`route.options.component.preload()`.

Every layout and page registered with the router is created by the official
`lazyRouteComponent` API. Protected page importers wait for both the icon
collection registration and page module, so the same native preload promise
covers both. The lazy component itself is assigned to `createRoute`; no element
or render-function wrapper hides its `preload()` method.

Every enabled sidebar item renders `Link preload="intent"`. The router defines
`defaultPreloadDelay: 150` once, so no route catalog entry or Link carries a
delay override. There is no manual event wiring, callback factory, or duplicate
deduplication layer.

### Update boundary

While an update is active, `Sidebar` passes `disabled` to every item.
`SidebarNavList` then renders an inert `span`, rather than a `Link`, so it
offers no new hover, focus, or touch intents. The native navigation blocker
continues to reject navigation, and every named data loader checks the live
update getter before starting query work. `ensureRouteQueryData` checks it
again after transport readiness, closing the data-request race.

The installed Link implementation does not cancel or recheck a delayed intent
timer when its Link is disabled or unmounted. Therefore a timer queued before
the update transition may still call `preloadRoute`, and a static JavaScript
chunk import already scheduled may finish during the update. That code-only
load performs no RPC and does not navigate. The router's `abortController`
cannot prevent it: core calls `loadRouteChunk` after `beforeLoad` without
testing the signal, and dynamic component imports receive no signal.

This baseline deliberately accepts that native behavior. It does not mutate
the route-global `preload` boolean, introduce a custom Link callback, or wrap
the official lazy component. In this release there is no supported per-call
preload predicate; a strict zero-chunk invariant would require disabling route
preload statically or adding custom global state.

## Data loader inventory

Exactly four protected routes declare data loaders:

| Route     |                    Query count | Query scope                                    |
| --------- | -----------------------------: | ---------------------------------------------- |
| Dashboard |                              4 | health summary, host info, uptime, server time |
| Network   |                              1 | network info                                   |
| Updates   | 1 with PackageKit, otherwise 0 | basic updates                                  |
| Services  |                              1 | service list                                   |

Data loaders exist only where a route has specific data worth warming. Native
`lazyRouteComponent` preloading already loads every protected route's JavaScript
chunk, so adding empty loaders to the other ten routes would add no behavior.

Each named loader calls `ensureRouteQueryData` with concrete generated
`queryOptions`, so page observers reuse the same React Query cache entries.
They wait for transport readiness, check live update blocking before and after
readiness, and settle query failures so page-level error UI remains authoritative.
Intent calls pass `preload: true`, which tags query work silent and speculative;
navigation calls pass `false`.

Logs and Storage have no data loader and issue zero route queries. Docker, VM,
Accounts, Shares, WireGuard, Hardware, File Browser, and Terminal also have no
route data loader yet; every one still has native component chunk preloading.

## Coverage

- `tanstack-router/router.test.tsx` covers topology, guards, typed search, deep
  links, exact loader registration, native chunk preload for Logs, Storage,
  and capability-enabled Docker, plus access denial before importer/loader.
- `routing/protectedRouteLoaders.test.ts` covers exact query counts and keys,
  PackageKit gating, navigation/speculative metadata, update early-return for
  all four named loaders, and failures.
- `routing/routeQueryLoader.test.tsx` covers shared cache, readiness races, and
  silent speculative metadata.
- `routing/useSidebarItems.test.tsx` covers access, order, and native Link
  intent props without per-Link delay overrides.
- Provider tests cover bootstrap, deep-link restoration, and live access changes.

## Source residue audit

The routing source has no:

- boolean loader metadata;
- route/data intent policy or manual sidebar preload callback;
- custom lazy-component preload wrapper;
- duplicate protected route table;
- alternate application routing dependency or adapter.

`routeQueryLoader.ts` remains intentionally: it owns transport readiness,
the live update recheck, React Query cache reuse, and silent speculative-query
metadata. It is data-loading infrastructure, not a route registry, intent
policy, or component-preload abstraction.

## Validation

The implementation is validated with:

- focused routing and loader tests;
- TypeScript checking and frontend lint/format checks;
- the complete frontend test suite;
- the production Vite build;
- source-residue and package-manifest audits;
- `git diff --check`.

No Docker runtime was available, so no Docker command or container validation
was run. No authenticated browser session or performance profiler was
available; runtime timing and network-waterfall claims are intentionally not
made.



só D/N/U/S têm loaders de dados...

Porque???

Não devem ter todos loaders?

E outra coisas. Porque temos 250 e 150 ms no preloadDelayMs?

Não devem ser todos iguais (150?) e não ser preciso definir em protectedRouteCatalog?

Um único defaultPreloadDelay: 150 está configurado em [router.tsx](/home/wdeadmin/LinuxIO/frontend/src/tanstack-router/router.tsx).