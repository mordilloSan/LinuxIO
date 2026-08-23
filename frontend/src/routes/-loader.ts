/**
 * TanStack Router and TanStack Query loader primitives.
 *
 * Route loaders call this after their access checks. It intentionally
 * receives a live update-state getter: UpdateContext owns that state and the
 * stream multiplexer does not.
 */

import {
  hashKey,
  type AnyUseQueryOptions,
  type QueryClient,
  type QueryKey,
} from "@tanstack/react-query";

import {
  CACHE_TTL_MS,
  ensureLoaderRequestReady,
  getStreamMux,
  linuxio,
  subscribeRequestAvailability,
  type UIConfig,
} from "@/api";
import { scopedConfigQueryKey } from "@/api/config-query";
import { subscribeLiveUpdateBlocked } from "@/contexts/UpdateContext";
import type { LinuxIORouterContext } from "@/routes/-auth";

/** Heterogeneous queryOptions accepted by a route-level batch. */
export type LoaderQueryOptions = AnyUseQueryOptions & { queryKey: QueryKey };

export const LOADER_FRESHNESS = {
  /** Return any cached value; fetch only when the cache entry is absent. */
  PRESENCE: "presence",
  /** Return cached data immediately and refresh stale data in the background. */
  BACKGROUND: "background",
  /** Await data that satisfies the query's staleTime contract. */
  BLOCKING: "blocking",
} as const;

export type LoaderFreshness =
  (typeof LOADER_FRESHNESS)[keyof typeof LOADER_FRESHNESS];

export interface RouteLoaderArgs {
  abortController: AbortController;
  context: LinuxIORouterContext;
  preload: boolean;
}

interface RouteQueryLoadOptions {
  context: LinuxIORouterContext;
  freshness: LoaderFreshness;
  preload: boolean;
  signal?: AbortSignal;
}

interface ActiveLoaderQuery {
  consumers: Set<symbol>;
  loaderStartedFetch: boolean;
}

const activeLoaderQueries = new WeakMap<
  QueryClient,
  Map<string, ActiveLoaderQuery>
>();

/**
 * Load the TanStack Query entries consumed by a route.
 *
 * The loader and the mounted query observers share the same cache entries.
 * Request failures propagate to TanStack Router's route error boundary.
 */
export async function loadRouteQueries(
  { abortController, context, preload }: RouteLoaderArgs,
  queryOptions: readonly LoaderQueryOptions[],
  freshness: LoaderFreshness = LOADER_FRESHNESS.PRESENCE,
): Promise<void> {
  const { signal } = abortController;
  await prepareRouteLoading(context.isUpdateBlocked, signal);

  await Promise.all(
    queryOptions.map((options) =>
      loadRouteQuery(context.queryClient, options, {
        freshness,
        preload,
        signal,
      }),
    ),
  );
}

/**
 * Start optional route work without making the route transition atomic.
 *
 * Call this only after loadRouteTransport/loadRouteQueries has completed its
 * readiness and update checks. Query prefetch intentionally swallows failures;
 * the mounted widget observer and its local error boundary own a later retry.
 */
export function startRouteQueryPrefetches(
  {
    context,
    preload,
    signal,
  }: Pick<RouteQueryLoadOptions, "context" | "preload" | "signal">,
  queryOptions: readonly LoaderQueryOptions[],
): void {
  throwIfAborted(signal);
  if (routeLoadingBlocked(context.isUpdateBlocked)) return;

  for (const options of queryOptions) {
    const prepared = {
      ...options,
      retry: options.retry ?? false,
      meta: {
        ...options.meta,
        deferredRoutePrefetch: true,
        routeIntentPrefetch: preload || undefined,
        silent: true,
      },
    };
    const release = registerLoaderQuery(
      context.queryClient,
      options.queryKey,
      signal,
    );
    try {
      throwIfAborted(signal);
      void context.queryClient.prefetchQuery(prepared).then(
        () => release(),
        () => release(),
      );
    } catch (error) {
      release();
      throw error;
    }
  }
}

/**
 * Read the backend-owned UI snapshot for route decisions. The authenticated
 * provider consumes the same user-scoped cache entry, so the initial load is
 * one request and no frontend defaults are needed in a loader.
 */
export async function loadRouteUIConfig(
  context: LinuxIORouterContext,
  signal?: AbortSignal,
): Promise<UIConfig> {
  throwIfAborted(signal);
  const userId = context.auth.user?.id ?? "anonymous";
  const queryKey = scopedConfigQueryKey(linuxio.config.get_ui.queryKey, userId);
  const cached = context.queryClient.getQueryData<UIConfig>(queryKey);
  if (cached) return cached;

  return rejectOnAbort(
    context.queryClient.fetchQuery({
      ...linuxio.config.get_ui,
      queryKey,
      staleTime: CACHE_TTL_MS.NONE,
    }),
    signal,
  );
}

/** Prepare the RPC transport for stream-only routes such as Terminal. */
export async function loadRouteTransport(
  context: LinuxIORouterContext,
  signal?: AbortSignal,
): Promise<void> {
  await prepareRouteLoading(context.isUpdateBlocked, signal);
}

async function prepareRouteLoading(
  isUpdateBlocked: () => boolean,
  signal?: AbortSignal,
): Promise<void> {
  await waitForRouteLoadingAllowed(isUpdateBlocked, signal);
  await ensureLoaderRequestReady(undefined, signal);
  await waitForRouteLoadingAllowed(isUpdateBlocked, signal);
}

async function loadRouteQuery(
  queryClient: QueryClient,
  options: LoaderQueryOptions,
  {
    freshness,
    preload,
    signal,
  }: Pick<RouteQueryLoadOptions, "freshness" | "preload" | "signal">,
): Promise<void> {
  throwIfAborted(signal);
  const existing = queryClient.getQueryCache().find({
    exact: true,
    queryKey: options.queryKey,
  });
  const loaderOwnsError =
    existing?.state.data === undefined ||
    freshness === LOADER_FRESHNESS.BLOCKING;
  const prepared = {
    ...options,
    // The transport owns its one bounded reconnect retry. A route transition
    // must not multiply that policy with Query's observer retry defaults.
    retry: options.retry ?? false,
    meta: {
      ...options.meta,
      routeInitialLoad: loaderOwnsError || undefined,
      routeIntentPrefetch: preload || undefined,
      silent: preload || loaderOwnsError || options.meta?.silent,
    },
  };
  const release = registerLoaderQuery(queryClient, options.queryKey, signal);
  let backgroundRetainsConsumer = false;
  try {
    // Close the registration/start gap if a navigation was canceled while
    // this query joined an existing loader batch.
    throwIfAborted(signal);
    let request: Promise<unknown>;
    switch (freshness) {
      case LOADER_FRESHNESS.PRESENCE:
        request = queryClient.ensureQueryData(prepared);
        break;
      case LOADER_FRESHNESS.BACKGROUND:
        if (existing?.state.data !== undefined) {
          // prefetchQuery exposes the lifetime of stale revalidation while
          // preserving the policy's immediate cached-data return. Keep this
          // loader registered so a later navigation abort can still cancel an
          // orphaned refresh.
          const backgroundRequest = queryClient.prefetchQuery(prepared);
          backgroundRetainsConsumer = true;
          void backgroundRequest.then(
            () => release(),
            () => release(),
          );
          throwIfAborted(signal);
          return;
        }
        request = queryClient.fetchQuery(prepared);
        break;
      case LOADER_FRESHNESS.BLOCKING:
        request = queryClient.fetchQuery(prepared);
        break;
    }
    await rejectOnAbort(request, signal);
  } finally {
    if (!backgroundRetainsConsumer) release();
  }
}

function registerLoaderQuery(
  queryClient: QueryClient,
  queryKey: QueryKey,
  signal?: AbortSignal,
): () => void {
  let clientQueries = activeLoaderQueries.get(queryClient);
  if (!clientQueries) {
    clientQueries = new Map();
    activeLoaderQueries.set(queryClient, clientQueries);
  }

  const queryHash = hashKey(queryKey);
  let active = clientQueries.get(queryHash);
  if (!active) {
    const query = queryClient.getQueryCache().find({ exact: true, queryKey });
    active = {
      consumers: new Set(),
      loaderStartedFetch: query?.state.fetchStatus !== "fetching",
    };
    clientQueries.set(queryHash, active);
  }

  const consumer = Symbol(queryHash);
  active.consumers.add(consumer);
  let released = false;

  const release = (aborted = false) => {
    if (released) return;
    released = true;
    signal?.removeEventListener("abort", handleAbort);
    active?.consumers.delete(consumer);
    if (!active || active.consumers.size > 0) return;

    clientQueries?.delete(queryHash);
    if (clientQueries?.size === 0) activeLoaderQueries.delete(queryClient);
    if (!aborted || !active.loaderStartedFetch) return;

    const query = queryClient.getQueryCache().find({ exact: true, queryKey });
    if (query?.getObserversCount() !== 0) return;
    void queryClient.cancelQueries({ exact: true, queryKey });
  };
  const handleAbort = () => release(true);

  if (signal?.aborted) handleAbort();
  else signal?.addEventListener("abort", handleAbort, { once: true });

  return release;
}

function rejectOnAbort<T>(
  request: Promise<T>,
  signal?: AbortSignal,
): Promise<T> {
  if (!signal) return request;
  if (signal.aborted) return Promise.reject(abortSignalError(signal));

  return new Promise<T>((resolve, reject) => {
    const cleanup = () => signal.removeEventListener("abort", handleAbort);
    const handleAbort = () => {
      cleanup();
      reject(abortSignalError(signal));
    };
    signal.addEventListener("abort", handleAbort, { once: true });
    request.then(
      (value) => {
        cleanup();
        resolve(value);
      },
      (error) => {
        cleanup();
        reject(error);
      },
    );
  });
}

function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) throw abortSignalError(signal);
}

function abortSignalError(signal: AbortSignal): Error {
  if (signal.reason instanceof Error) return signal.reason;
  const error = new Error("Navigation cancelled");
  error.name = "AbortError";
  return error;
}

function routeLoadingBlocked(isUpdateBlocked: () => boolean): boolean {
  // UpdateProvider mounts below the Router, so the live mux flag closes the
  // first-navigation window before the context getter has observed the event.
  return isUpdateBlocked() || getStreamMux()?.isUpdating === true;
}

function waitForRouteLoadingAllowed(
  isUpdateBlocked: () => boolean,
  signal?: AbortSignal,
): Promise<void> {
  throwIfAborted(signal);
  if (!routeLoadingBlocked(isUpdateBlocked)) return Promise.resolve();

  return new Promise((resolve, reject) => {
    let settled = false;
    let unsubscribeLiveUpdate: () => void = () => undefined;
    let unsubscribeRequestAvailability: () => void = () => undefined;

    const cleanup = () => {
      signal?.removeEventListener("abort", handleAbort);
      unsubscribeLiveUpdate();
      unsubscribeRequestAvailability();
    };
    const settle = (callback: () => void) => {
      if (settled) return;
      settled = true;
      cleanup();
      callback();
    };
    const handleAbort = () => {
      if (signal) settle(() => reject(abortSignalError(signal)));
    };
    const resolveIfAllowed = () => {
      if (!routeLoadingBlocked(isUpdateBlocked)) settle(resolve);
    };

    unsubscribeLiveUpdate = subscribeLiveUpdateBlocked(resolveIfAllowed);
    unsubscribeRequestAvailability =
      subscribeRequestAvailability(resolveIfAllowed);

    // Recheck after subscribing so a release between the initial check and
    // listener registration cannot leave the navigation waiting indefinitely.
    if (signal?.aborted) {
      handleAbort();
    } else {
      signal?.addEventListener("abort", handleAbort, { once: true });
      resolveIfAllowed();
    }
  });
}
