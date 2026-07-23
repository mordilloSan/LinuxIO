# TanStack Router Migration Baseline

Status: Phase 1 characterization baseline, recorded 2026-07-23. This is not
an implementation plan and does not change runtime behavior.

## Protected-navigation contracts

The following contracts have executable coverage and must remain true during
the router migration:

| Contract | Existing characterization |
| --- | --- |
| Guest protected deep link | `AuthGuard` redirects to `/sign-in?redirect=...` and encodes the complete path, search, and hash target. |
| Existing redirect target | `AuthGuard` retains an existing `redirect` parameter rather than nesting it. |
| Authenticated sign-in visit | `GuestGuard` follows the complete redirect target, including encoded file-browser paths, search, and hash; it otherwise goes to `/`. |
| Access-gated routes | `useAppRoutes` omits unavailable capability routes and privileged routes from the protected branch. |
| Tab URL state | `useTabUrlState` uses its default for an absent or empty parameter and preserves sibling search parameters while changing a tab. |
| File browser | `useFileBrowserNavigation` decodes wildcard URL segments, encodes each navigated segment, normalizes `/filebrowser` to `/`, and reports one path change per navigation. |
| Update navigation guard | `useUpdateNavigationGuard` restores the full pre-update pathname, search, and hash while an update is running, but allows navigation otherwise. |

Relevant tests:

- `frontend/src/components/guards/AuthGuard.test.tsx`
- `frontend/src/components/guards/GuestGuard.test.tsx`
- `frontend/src/routing/useAppRoutes.test.tsx`
- `frontend/src/hooks/useTabUrlState.test.tsx`
- `frontend/src/hooks/filebrowser/useFileBrowserNavigation.test.tsx`
- `frontend/src/contexts/UpdateContext.navigationGuard.test.tsx`

## Query/loading baseline

This section records source-backed behavior only. It deliberately does not
claim timings, request totals, or render counts that have not been measured.

| Area | Initial loading behavior | Current request/subscription evidence |
| --- | --- | --- |
| Dashboard | Individual cards query from components and show card-level loading placeholders when their query is pending. | Intent preload declares health summary, host info, uptime, and server time in `frontend/src/routes.tsx`; components also own their queries. Intervals vary by card (for example, System Overview: 30–60 seconds; Network card: 1 second). |
| Network | `NetworkInterfaceList` renders `PageLoader` while `get_network_info` is pending. | The page uses `get_network_info.useQuery({ refetchInterval: 1000 })`; route intent preload declares the same endpoint. |
| Docker dashboard | The parent Docker page and `DockerDashboard` each subscribe to `list_containers` while the dashboard tab is active. The dashboard also has pending state for images, networks, volumes, and daemon information. | Parent `list_containers` polls every 5 seconds while active; `DockerDashboard` independently subscribes to the same query at 5 seconds. Its other dashboard queries poll at 30 or 60 seconds. Route intent preload declares `list_containers`. |

### Measurements still required before/after migration

Automated characterization alone cannot establish actual browser scheduling or
render costs. Capture these with a real authenticated session before beginning
the loader phase, then repeat after it:

1. React DevTools Profiler commit count and duration from link intent through
   first usable Dashboard, Network, and Docker views.
2. Browser network/RPC trace for direct URL entry and sidebar intent preload:
   identify whether a query is served from cache, deduplicated, or requested
   more than once.
3. Steady-state profiler trace across at least three Network one-second polls
   and three Docker five-second polls, noting which components re-render.

Do not compare speculative preload requests as user-visible failures: current
route-intent preloads are intentionally silent and swallow individual
prefetch errors (`frontend/src/routing/routeIntentPreload.ts`).

## Phase boundary

This baseline must be retained until the TanStack Router migration is complete.
It does not authorize dependency changes, a second runtime router, loader
execution, or changes to React Query's polling semantics.


No testing needed from you yet. Phase 3A is complete and the new TanStack Router remains unmounted, so React Router still runs production.
Completed:
Typed router context and protected route tree.
Shared route/sidebar/access catalog.
Auth redirect, access guard, 404, and file-browser splat parity.
Compile-time rejection of invalid routes.
Sol review and corrections after Terra’s implementation.
Verification:
make check-frontend passed: 489 tests across 89 files.
git diff --check passed.
The next phase is the live router cutover. That’s when your manual smoke testing becomes valuable.