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

export const ROUTE_INTENT_PRELOAD = {
  routeOnly: { route: true, data: false, delayMs: 150 },
  routeAndData: { route: true, data: true, delayMs: 250 },
  heavyRouteOnly: { route: true, data: false, delayMs: 250 },
} satisfies Record<string, RouteIntentPreloadPolicy>;

const ROUTE_INTENT_QUERY_META = { silent: true, routeIntentPrefetch: true };

type RouteQueryOptions<
  TQueryFnData = unknown,
  TError = unknown,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
> = UseQueryOptions<TQueryFnData, TError, TData, TQueryKey>;

type RouteQueryEndpoint<
  TQueryFnData = unknown,
  TError = unknown,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
> = {
  queryOptions: () => RouteQueryOptions<TQueryFnData, TError, TData, TQueryKey>;
};

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
