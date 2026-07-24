/**
 * TanStack Router and TanStack Query loader primitives.
 *
 * Route loaders call this after their access checks. It intentionally
 * receives a live update-state getter: UpdateContext owns that state and the
 * stream multiplexer does not.
 */

import type {
  AnyUseQueryOptions,
  UseQueryOptions,
} from "@tanstack/react-query";

import { ensureLoaderRequestReady, LinuxIOError } from "@/api";
import type { LinuxIORouterContext } from "@/routes/-auth";

/** Heterogeneous queryOptions accepted by a route-level batch. */
export type LoaderQueryOptions = AnyUseQueryOptions;

type LoaderQueryData<TOptions> =
  TOptions extends UseQueryOptions<
    infer TQueryFnData,
    infer _TError,
    infer _TData,
    infer _TQueryKey
  >
    ? TQueryFnData
    : never;

type LoaderQueryResults<TOptions extends readonly LoaderQueryOptions[]> = {
  -readonly [TIndex in keyof TOptions]: LoaderQueryData<TOptions[TIndex]>;
};

export interface LoadRouteQueriesOptions {
  context: LinuxIORouterContext;
  preload: boolean;
}

/**
 * Load and return the TanStack Query entries consumed by a route.
 *
 * The loader and the mounted query observers share the same cache entries.
 * Request failures propagate to TanStack Router's route error boundary.
 */
export async function loadRouteQueries<
  const TOptions extends readonly LoaderQueryOptions[],
>(
  { context, preload }: LoadRouteQueriesOptions,
  queryOptions: TOptions,
): Promise<LoaderQueryResults<TOptions>> {
  await prepareRouteLoading(context.isUpdateBlocked);

  const data = await Promise.all(
    queryOptions.map((options) =>
      context.queryClient.ensureQueryData({
        ...options,
        meta: preload
          ? { ...options.meta, routeIntentPrefetch: true, silent: true }
          : options.meta,
      }),
    ),
  );

  return data as LoaderQueryResults<TOptions>;
}

/** Prepare the RPC transport for stream-only routes such as Terminal. */
export async function loadRouteTransport(
  context: LinuxIORouterContext,
): Promise<void> {
  await prepareRouteLoading(context.isUpdateBlocked);
}

async function prepareRouteLoading(
  isUpdateBlocked: () => boolean,
): Promise<void> {
  assertRouteLoadingAllowed(isUpdateBlocked);
  await ensureLoaderRequestReady();
  assertRouteLoadingAllowed(isUpdateBlocked);
}

function assertRouteLoadingAllowed(isUpdateBlocked: () => boolean): void {
  if (!isUpdateBlocked()) return;

  throw new LinuxIOError(
    "Cannot load route data while an update is in progress",
    "update_in_progress",
  );
}
