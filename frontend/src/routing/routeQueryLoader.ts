/**
 * Framework-agnostic React Query loader primitive.
 *
 * Future route loaders call this after their access checks. It intentionally
 * receives a live update-state getter: UpdateContext owns that state and the
 * stream multiplexer does not.
 */

import type {
  QueryClient,
  QueryKey,
  UseQueryOptions,
} from "@tanstack/react-query";

import { ensureLoaderRequestReady, LinuxIOError } from "@/api";

type LoaderQueryOptions<
  TQueryFnData = unknown,
  TError = unknown,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
> = UseQueryOptions<TQueryFnData, TError, TData, TQueryKey>;

export interface EnsureRouteQueryDataOptions<
  TQueryFnData = unknown,
  TError = unknown,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
> {
  queryClient: QueryClient;
  /** The typed options built by endpoint.queryOptions(...). */
  queryOptions: LoaderQueryOptions<TQueryFnData, TError, TData, TQueryKey>;
  /** Live state supplied by the router's UpdateContext bridge; never read from the mux. */
  isUpdateBlocked: () => boolean;
  /** Intent preloads are best-effort and must not trigger the global error toast. */
  speculative?: boolean;
  timeoutMs?: number;
}

/**
 * Seed a route's query cache after the transport is ready.
 *
 * `ensureQueryData` gives a subsequently mounted endpoint.useQuery observer the
 * same cache entry, avoiding a second initial request. A speculative call still
 * rejects to its caller, but is explicitly tagged silent for QueryCache's toast
 * policy.
 */
export async function ensureRouteQueryData<
  TQueryFnData = unknown,
  TError = unknown,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
>(
  options: EnsureRouteQueryDataOptions<TQueryFnData, TError, TData, TQueryKey>,
): Promise<TQueryFnData> {
  const {
    isUpdateBlocked,
    queryClient,
    queryOptions,
    speculative = false,
    timeoutMs,
  } = options;

  if (isUpdateBlocked()) {
    throw new LinuxIOError(
      "Cannot load route data while an update is in progress",
      "update_in_progress",
    );
  }

  await ensureLoaderRequestReady(timeoutMs);

  if (isUpdateBlocked()) {
    throw new LinuxIOError(
      "Cannot load route data while an update is in progress",
      "update_in_progress",
    );
  }

  return queryClient.ensureQueryData({
    ...queryOptions,
    meta: speculative
      ? { ...queryOptions.meta, routeIntentPrefetch: true, silent: true }
      : queryOptions.meta,
  });
}
