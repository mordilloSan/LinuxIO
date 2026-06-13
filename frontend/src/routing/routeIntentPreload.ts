/**
 * @file Route **intent preload** primitives.
 *
 * "Intent preload" makes the app eagerly loads that route's lazy code chunk and, optionally,
 * prefetches React Query data.
 *
 * This module owns the *declarative* and *factory* halves of that system:
 *
 * - {@link ROUTE_INTENT_PRELOAD} — the catalog of named policy presets a route
 *   picks from to describe *what* to preload and *how eagerly*.
 * - {@link routeQuery} — wraps a query endpoint/options into a
 *   {@link RoutePrefetchQuery} thunk tagged as a speculative, silent prefetch.
 * - {@link createRouteIntentPreload} — compiles a route's declared policy into a
 *   single `preload()` callback (or `undefined` when there is nothing to do).
 *
 * The runtime/DOM half lives elsewhere and consumes the output of this module:
 * - `@/routing/routeTypes` defines the policy and route shapes used here.
 * - `@/routing/useSidebarItems` calls {@link createRouteIntentPreload} per route
 *   and forwards `intentPreload.delayMs`.
 * - `@/components/sidebar/SidebarNavList` binds the result to pointer/focus
 *   events via `@/hooks/useIntentPreload`, which debounces and dedupes the call.
 */

import type {
  QueryClient,
  QueryKey,
  UseQueryOptions,
} from "@tanstack/react-query";

import type { AccessContext } from "@/hooks/useCapabilities";
import type {
  RouteIntentPreloadPolicy,
  RoutePrefetchQuery,
  RouteWithSidebar,
} from "@/routing/routeTypes";

/**
 * Catalog of reusable intent-preload presets. A route in `routes.tsx` references
 * one of these by name as its `intentPreload` policy instead of hand-writing a
 * {@link RouteIntentPreloadPolicy} object, so preload behavior stays consistent
 * across the app and is described in one place.
 *
 * Each preset is a {@link RouteIntentPreloadPolicy} — see that type for the
 * precise meaning of the `route`, `data`, and `delayMs` fields. Pick a preset by
 * answering two questions: *does this route need data prefetched up front?* and
 * *is its code chunk heavy enough that we should wait a beat before fetching?*
 */
export const ROUTE_INTENT_PRELOAD = {
  /**
   * Preload the lazy code chunk only, with a short 150ms hover debounce.
   * For light pages that fetch their own data on mount (e.g. Logs, Storage,
   * Shares, Hardware).
   */
  routeOnly: { route: true, data: false, delayMs: 150 },
  /**
   * Preload the lazy code chunk **and** prefetch the route's React Query data,
   * with a slightly longer 250ms debounce (data prefetch is more expensive, so
   * require a touch more intent before firing). For data-heavy dashboards
   * (e.g. Default/health, Network, Updates, Services, Docker, Accounts).
   */
  routeAndData: { route: true, data: true, delayMs: 250 },
  /**
   * Preload the lazy code chunk only, but with the longer 250ms debounce so a
   * large bundle isn't fetched on an incidental hover. For heavy routes whose
   * chunk is expensive to download/parse (e.g. Wireguard, FileBrowser,
   * Terminal).
   */
  heavyRouteOnly: { route: true, data: false, delayMs: 250 },
} satisfies Record<string, RouteIntentPreloadPolicy>;

/**
 * Query `meta` merged into every intent prefetch.
 * - `silent` — suppress the usual error toast: a speculative prefetch failing is
 *   not something the user asked for and must not surface as an error.
 * - `routeIntentPrefetch` — marks the request as an intent prefetch for any
 *   downstream meta-aware logic (logging, dev tooling, cache policy).
 */
const ROUTE_INTENT_QUERY_META = { silent: true, routeIntentPrefetch: true };

/**
 * Plain TanStack `UseQueryOptions` — the raw query config shape accepted by
 * {@link routeQuery} when you pass options directly rather than an endpoint.
 */
type RouteQueryOptions<
  TQueryFnData = unknown,
  TError = unknown,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
> = UseQueryOptions<TQueryFnData, TError, TData, TQueryKey>;

/**
 * The "endpoint" shape accepted by {@link routeQuery}: any object exposing a
 * `queryOptions()` factory. This matches the generated `linuxio.*` API endpoints
 * (e.g. `linuxio.system.get_host_info`), letting routes prefetch with the same
 * descriptor they use to read the data.
 */
type RouteQueryEndpoint<
  TQueryFnData = unknown,
  TError = unknown,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
> = {
  queryOptions: () => RouteQueryOptions<TQueryFnData, TError, TData, TQueryKey>;
};

/**
 * Normalize a query source into a {@link RoutePrefetchQuery} thunk for use in a
 * route's `prefetchQueries`. Accepts either a {@link RouteQueryEndpoint} (an
 * object with `queryOptions()`, e.g. a generated `linuxio.*` endpoint) or raw
 * {@link RouteQueryOptions}. The returned thunk runs `queryClient.prefetchQuery`
 * with {@link ROUTE_INTENT_QUERY_META} merged in so the prefetch is silent and
 * tagged as intent-driven.
 *
 * @param source Query endpoint (with `queryOptions()`) or raw query options.
 * @returns A prefetch thunk bound to a `QueryClient`.
 */
export function routeQuery<
  TQueryFnData,
  TError,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
>(
  source:
    | RouteQueryEndpoint<TQueryFnData, TError, TData, TQueryKey>
    | RouteQueryOptions<TQueryFnData, TError, TData, TQueryKey>,
): RoutePrefetchQuery {
  const options = "queryOptions" in source ? source.queryOptions() : source;

  return (queryClient) =>
    queryClient.prefetchQuery({
      ...options,
      meta: {
        ...options.meta,
        ...ROUTE_INTENT_QUERY_META,
      },
    });
}

/**
 * Compile a route's declared intent-preload policy into a single `preload()`
 * callback that `useIntentPreload` can fire on hover/focus/press.
 *
 * The route's {@link RouteIntentPreloadPolicy} (`route.intentPreload`) gates two
 * independent actions:
 * - **Route preload** — runs when `intentPreload.route` is set *and* the route
 *   actually has a lazy `preload()` loader.
 * - **Data prefetch** — runs when `intentPreload.data` is set, the route has at
 *   least one `prefetchQueries` entry, *and* the optional `prefetchDataWhen`
 *   access gate passes. The gate defaults to `true` when not provided, so a
 *   route only needs `prefetchDataWhen` when prefetching depends on capabilities
 *   (e.g. only prefetch updates when PackageKit is available).
 *
 * When neither action applies, returns `undefined` so the caller can skip wiring
 * a no-op preload. Data prefetch uses `Promise.allSettled` so one failed query
 * never rejects the combined preload, while the outer `Promise.all` still lets
 * callers await completion of every kicked-off task.
 *
 * @param route The route whose policy and prefetch descriptors drive preloading.
 * @param queryClient Client used to run any data prefetches.
 * @param access Capability/access context evaluated by `prefetchDataWhen`.
 * @returns A `preload()` callback, or `undefined` if nothing should preload.
 */
export function createRouteIntentPreload(
  route: RouteWithSidebar,
  queryClient: QueryClient,
  access: AccessContext,
): (() => Promise<unknown>) | undefined {
  const { intentPreload } = route;
  const shouldPreloadRoute = intentPreload.route && route.preload;
  const shouldPrefetchData =
    intentPreload.data &&
    route.prefetchQueries?.length &&
    (route.prefetchDataWhen?.(access) ?? true);

  if (!shouldPreloadRoute && !shouldPrefetchData) return undefined;

  return async () => {
    const tasks: Promise<unknown>[] = [];

    if (shouldPreloadRoute) {
      tasks.push(shouldPreloadRoute());
    }

    if (shouldPrefetchData && route.prefetchQueries) {
      tasks.push(
        Promise.allSettled(
          route.prefetchQueries.map((query) => query(queryClient)),
        ),
      );
    }

    await Promise.all(tasks);
  };
}
