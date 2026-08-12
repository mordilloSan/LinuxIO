import {
  QueryCache,
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRouteWithContext,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  ensureLoaderRequestReady: vi.fn(),
  getStreamMux: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    ensureLoaderRequestReady: apiMocks.ensureLoaderRequestReady,
    getStreamMux: apiMocks.getStreamMux,
  };
});

import { emptyCapabilityState } from "@/api/capabilities";
import linuxio from "@/api/generated/client";
import type { ExtendedFileInfo } from "@/api/generated/linuxio-types";
import { fileBrowserListingQueryOptions } from "@/hooks/filebrowser/fileBrowserListingQueryOptions";
import type { LinuxIORouterContext } from "@/routes/-auth";
import {
  LOADER_FRESHNESS,
  loadRouteQueries,
  startRouteQueryPrefetches,
} from "@/routes/-loader";

function createClient(queryCache?: QueryCache) {
  return new QueryClient({
    queryCache,
    defaultOptions: { queries: { retry: false } },
  });
}

function createRouterContext(
  queryClient: QueryClient,
  isUpdateBlocked = () => false,
): LinuxIORouterContext {
  return {
    access: { ...emptyCapabilityState, privileged: false },
    auth: {
      isAuthenticated: true,
      isInitialized: true,
      user: { id: "root", name: "root" },
    },
    isUpdateBlocked,
    queryClient,
  };
}

function createLoaderArgs(
  context: LinuxIORouterContext,
  {
    controller = new AbortController(),
    preload = false,
  }: { controller?: AbortController; preload?: boolean } = {},
) {
  return { abortController: controller, context, preload };
}

const fileBrowserListing = {
  name: "projects",
  size: 0,
  modified: "2026-01-01T00:00:00Z",
  type: "directory",
  hidden: false,
  hasPreview: false,
  symlink: false,
  files: [],
  folders: [],
  path: "/srv/projects",
} satisfies ExtendedFileInfo;

function fileBrowserListingOptions(queryFn: () => Promise<ExtendedFileInfo>) {
  return {
    ...linuxio.filebrowser.resource_get({ path: fileBrowserListing.path }),
    ...fileBrowserListingQueryOptions,
    queryFn,
  };
}

describe("loadRouteQueries", () => {
  beforeEach(() => {
    apiMocks.ensureLoaderRequestReady.mockReset();
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    apiMocks.getStreamMux.mockReset();
    apiMocks.getStreamMux.mockReturnValue(null);
  });

  it("accepts typed Call descriptors from generated endpoints", async () => {
    const context = createRouterContext(createClient(), () => true);

    await expect(
      loadRouteQueries(createLoaderArgs(context), [
        linuxio.system.get_host_info,
      ]),
    ).rejects.toMatchObject({ code: "update_in_progress" });
  });

  it("does not run readiness or a query while updates block route loading", async () => {
    const queryFn = vi.fn();
    const queryOptions = { queryKey: ["blocked"], queryFn };
    const context = createRouterContext(createClient(), () => true);

    await expect(
      loadRouteQueries(createLoaderArgs(context), [queryOptions]),
    ).rejects.toMatchObject({ code: "update_in_progress" });

    expect(apiMocks.ensureLoaderRequestReady).not.toHaveBeenCalled();
    expect(queryFn).not.toHaveBeenCalled();
  });

  it("refetches a loader result on mount when staleTime is zero", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const queryFn = vi.fn().mockResolvedValue("ready");
    const queryOptions = {
      queryKey: ["zero-stale-query"],
      queryFn,
      staleTime: 0,
    };
    const queryClient = createClient();
    const context = createRouterContext(queryClient);

    await expect(
      loadRouteQueries(createLoaderArgs(context), [queryOptions]),
    ).resolves.toBeUndefined();

    function Observer() {
      const query = useQuery(queryOptions);
      return <span>{query.data}</span>;
    }

    const view = render(
      <QueryClientProvider client={queryClient}>
        <Observer />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(view.getByText("ready")).toBeInTheDocument());
    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));
  });

  it("uses the File Browser listing options without a duplicate cold-navigation request", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const queryFn = vi.fn().mockResolvedValue(fileBrowserListing);
    const queryOptions = fileBrowserListingOptions(queryFn);
    const queryClient = createClient();
    const context = createRouterContext(queryClient);

    await expect(
      loadRouteQueries(
        createLoaderArgs(context),
        [queryOptions],
        LOADER_FRESHNESS.BACKGROUND,
      ),
    ).resolves.toBeUndefined();

    function Observer() {
      const query = useQuery(queryOptions);
      return <span>{query.data?.name}</span>;
    }

    const view = render(
      <QueryClientProvider client={queryClient}>
        <Observer />
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(view.getByText(fileBrowserListing.name)).toBeInTheDocument(),
    );
    expect(queryFn).toHaveBeenCalledTimes(1);
  });

  it("refetches an invalidated File Browser listing when revisiting the route", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const queryFn = vi.fn().mockResolvedValue(fileBrowserListing);
    const queryOptions = fileBrowserListingOptions(queryFn);
    const queryClient = createClient();
    const context = createRouterContext(queryClient);

    await expect(
      loadRouteQueries(
        createLoaderArgs(context),
        [queryOptions],
        LOADER_FRESHNESS.BACKGROUND,
      ),
    ).resolves.toBeUndefined();

    function Observer() {
      const query = useQuery(queryOptions);
      return <span>{query.data?.name}</span>;
    }

    const firstVisit = render(
      <QueryClientProvider client={queryClient}>
        <Observer />
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(firstVisit.getByText(fileBrowserListing.name)).toBeInTheDocument(),
    );
    expect(queryFn).toHaveBeenCalledTimes(1);
    firstVisit.unmount();

    await queryClient.invalidateQueries({ queryKey: queryOptions.queryKey });
    await expect(
      loadRouteQueries(
        createLoaderArgs(context),
        [queryOptions],
        LOADER_FRESHNESS.BACKGROUND,
      ),
    ).resolves.toBeUndefined();
    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));

    const revisit = render(
      <QueryClientProvider client={queryClient}>
        <Observer />
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(revisit.getByText(fileBrowserListing.name)).toBeInTheDocument(),
    );
    expect(queryFn).toHaveBeenCalledTimes(2);
  });

  it("deduplicates simultaneous route-loader callers for the shared cache key", async () => {
    let resolveQuery!: (value: string) => void;
    const queryFn = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          resolveQuery = resolve;
        }),
    );
    const queryClient = createClient();
    const context = createRouterContext(queryClient);
    const options = {
      queryKey: ["shared-loader-query"],
      queryFn,
      staleTime: Infinity,
    };

    const first = loadRouteQueries(createLoaderArgs(context), [options]);
    const second = loadRouteQueries(createLoaderArgs(context), [options]);

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    resolveQuery("shared");
    await expect(Promise.all([first, second])).resolves.toEqual([
      undefined,
      undefined,
    ]);
    expect(queryFn).toHaveBeenCalledTimes(1);
  });

  it("defaults to presence freshness for stale cached data", async () => {
    const queryClient = createClient();
    const context = createRouterContext(queryClient);
    const queryFn = vi.fn().mockResolvedValue("fresh");
    const queryOptions = {
      queryKey: ["presence-policy"],
      queryFn,
      staleTime: 0,
    };
    queryClient.setQueryData(queryOptions.queryKey, "cached");
    await queryClient.invalidateQueries({ queryKey: queryOptions.queryKey });

    await expect(
      loadRouteQueries(createLoaderArgs(context), [queryOptions]),
    ).resolves.toBeUndefined();

    expect(queryFn).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(queryOptions.queryKey)).toBe("cached");
  });

  it("requires transport readiness before serving a cached route query", async () => {
    let resolveReadiness!: () => void;
    apiMocks.ensureLoaderRequestReady.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveReadiness = resolve;
        }),
    );
    const queryFn = vi.fn().mockResolvedValue("fresh");
    const queryOptions = {
      queryKey: ["cached-live-backend"],
      queryFn,
      staleTime: Infinity,
    };
    const queryClient = createClient();
    const context = createRouterContext(queryClient);
    queryClient.setQueryData(queryOptions.queryKey, "cached");

    const loading = loadRouteQueries(createLoaderArgs(context), [queryOptions]);

    await waitFor(() =>
      expect(apiMocks.ensureLoaderRequestReady).toHaveBeenCalledTimes(1),
    );
    expect(queryFn).not.toHaveBeenCalled();

    resolveReadiness();
    await expect(loading).resolves.toBeUndefined();
    expect(queryClient.getQueryData(queryOptions.queryKey)).toBe("cached");
  });

  it("returns stale cached data and revalidates it under the background policy", async () => {
    const queryClient = createClient();
    const context = createRouterContext(queryClient);
    const queryFn = vi.fn().mockResolvedValue("fresh");
    const queryOptions = {
      queryKey: ["background-policy"],
      queryFn,
      staleTime: 0,
    };
    queryClient.setQueryData(queryOptions.queryKey, "cached");

    await expect(
      loadRouteQueries(
        createLoaderArgs(context),
        [queryOptions],
        LOADER_FRESHNESS.BACKGROUND,
      ),
    ).resolves.toBeUndefined();

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(queryClient.getQueryData(queryOptions.queryKey)).toBe("fresh"),
    );
  });

  it("cancels orphaned background revalidation after the loader resolves", async () => {
    let querySignal: AbortSignal | undefined;
    const queryFn = vi.fn(
      ({ signal }: { signal: AbortSignal }) =>
        new Promise<string>((_resolve, reject) => {
          querySignal = signal;
          signal.addEventListener(
            "abort",
            () => reject(signal.reason ?? new Error("aborted")),
            { once: true },
          );
        }),
    );
    const queryOptions = {
      queryKey: ["background-abort"],
      queryFn,
      staleTime: 0,
    };
    const queryClient = createClient();
    const context = createRouterContext(queryClient);
    const controller = new AbortController();
    queryClient.setQueryData(queryOptions.queryKey, "cached");

    await expect(
      loadRouteQueries(
        createLoaderArgs(context, { controller }),
        [queryOptions],
        LOADER_FRESHNESS.BACKGROUND,
      ),
    ).resolves.toBeUndefined();
    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    expect(querySignal?.aborted).toBe(false);

    controller.abort(new DOMException("superseded", "AbortError"));

    await waitFor(() => expect(querySignal?.aborted).toBe(true));
  });

  it("starts deferred prefetch work without awaiting it", async () => {
    let resolveQuery!: (value: string) => void;
    const queryFn = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          resolveQuery = resolve;
        }),
    );
    const queryClient = createClient();
    const context = createRouterContext(queryClient);

    expect(
      startRouteQueryPrefetches({ context, preload: false }, [
        { queryKey: ["deferred-prefetch"], queryFn },
      ]),
    ).toBeUndefined();

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    expect(
      queryClient.getQueryCache().find({ queryKey: ["deferred-prefetch"] })
        ?.meta,
    ).toMatchObject({ deferredRoutePrefetch: true, silent: true });
    resolveQuery("ready");
    await waitFor(() =>
      expect(queryClient.getQueryData(["deferred-prefetch"])).toBe("ready"),
    );
  });

  it("keeps deferred prefetch failures local to the mounted observer", async () => {
    const queryClient = createClient();
    const context = createRouterContext(queryClient);

    expect(() =>
      startRouteQueryPrefetches({ context, preload: false }, [
        {
          queryKey: ["failed-deferred-prefetch"],
          queryFn: () => Promise.reject(new Error("optional widget failed")),
        },
      ]),
    ).not.toThrow();

    await waitFor(() =>
      expect(
        queryClient.getQueryState(["failed-deferred-prefetch"])?.status,
      ).toBe("error"),
    );
  });

  it("awaits stale data under the blocking policy", async () => {
    let resolveQuery!: (value: string) => void;
    const queryFn = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          resolveQuery = resolve;
        }),
    );
    const queryOptions = {
      queryKey: ["blocking-policy"],
      queryFn,
      staleTime: 0,
    };
    const queryClient = createClient();
    const context = createRouterContext(queryClient);
    queryClient.setQueryData(queryOptions.queryKey, "cached");

    const loading = loadRouteQueries(
      createLoaderArgs(context),
      [queryOptions],
      LOADER_FRESHNESS.BLOCKING,
    );

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    let settled = false;
    void loading.finally(() => {
      settled = true;
    });
    await Promise.resolve();
    expect(settled).toBe(false);

    resolveQuery("fresh");
    await expect(loading).resolves.toBeUndefined();
    expect(queryClient.getQueryData(queryOptions.queryKey)).toBe("fresh");
  });

  it("aborts an orphaned loader query through the Query signal", async () => {
    let querySignal: AbortSignal | undefined;
    const queryFn = vi.fn(
      ({ signal }: { signal: AbortSignal }) =>
        new Promise<string>((_resolve, reject) => {
          querySignal = signal;
          signal.addEventListener(
            "abort",
            () => reject(signal.reason ?? new Error("aborted")),
            { once: true },
          );
        }),
    );
    const controller = new AbortController();
    const context = createRouterContext(createClient());
    const loading = loadRouteQueries(
      createLoaderArgs(context, { controller }),
      [{ queryKey: ["orphaned-loader"], queryFn }],
    );

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    controller.abort(new DOMException("superseded", "AbortError"));

    await expect(loading).rejects.toMatchObject({ name: "AbortError" });
    await waitFor(() => expect(querySignal?.aborted).toBe(true));
  });

  it("aborts superseded route work during rapid Router navigation", async () => {
    let slowQuerySignal: AbortSignal | undefined;
    const slowQuery = vi.fn(
      ({ signal }: { signal: AbortSignal }) =>
        new Promise<string>((_resolve, reject) => {
          slowQuerySignal = signal;
          signal.addEventListener(
            "abort",
            () => reject(signal.reason ?? new Error("aborted")),
            { once: true },
          );
        }),
    );
    const fastQuery = vi.fn().mockResolvedValue("fast");
    const queryClient = createClient();
    const context = createRouterContext(queryClient);
    const rootRoute = createRootRouteWithContext<LinuxIORouterContext>()({});
    const indexRoute = createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
    });
    const slowRoute = createRoute({
      getParentRoute: () => rootRoute,
      loader: (loaderArgs) =>
        loadRouteQueries(loaderArgs, [
          { queryKey: ["rapid-navigation", "slow"], queryFn: slowQuery },
        ]),
      path: "slow",
    });
    const fastRoute = createRoute({
      getParentRoute: () => rootRoute,
      loader: (loaderArgs) =>
        loadRouteQueries(loaderArgs, [
          { queryKey: ["rapid-navigation", "fast"], queryFn: fastQuery },
        ]),
      path: "fast",
    });
    const testRouter = createRouter({
      context,
      history: createMemoryHistory({ initialEntries: ["/"] }),
      routeTree: rootRoute.addChildren([indexRoute, slowRoute, fastRoute]),
    });
    await testRouter.load();

    const slowNavigation = testRouter.navigate({ to: "/slow" as never });
    void slowNavigation.catch(() => undefined);
    await waitFor(() => expect(slowQuery).toHaveBeenCalledTimes(1));

    await testRouter.navigate({ to: "/fast" as never });

    await waitFor(() => expect(slowQuerySignal?.aborted).toBe(true));
    expect(fastQuery).toHaveBeenCalledTimes(1);
    expect(testRouter.state.location.pathname).toBe("/fast");
    expect(queryClient.getQueryData(["rapid-navigation", "fast"])).toBe("fast");
  });

  it("keeps a shared query alive when only one loader consumer aborts", async () => {
    let resolveQuery!: (value: string) => void;
    let querySignal: AbortSignal | undefined;
    const queryFn = vi.fn(
      ({ signal }: { signal: AbortSignal }) =>
        new Promise<string>((resolve) => {
          querySignal = signal;
          resolveQuery = resolve;
        }),
    );
    const queryOptions = {
      queryKey: ["shared-abort"],
      queryFn,
      staleTime: Infinity,
    };
    const queryClient = createClient();
    const context = createRouterContext(queryClient);
    const firstController = new AbortController();
    const secondController = new AbortController();
    const first = loadRouteQueries(
      createLoaderArgs(context, { controller: firstController }),
      [queryOptions],
    );
    const second = loadRouteQueries(
      createLoaderArgs(context, { controller: secondController }),
      [queryOptions],
    );

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    await Promise.resolve();
    firstController.abort(new DOMException("superseded", "AbortError"));

    await expect(first).rejects.toMatchObject({ name: "AbortError" });
    expect(querySignal?.aborted).toBe(false);
    resolveQuery("shared");
    await expect(second).resolves.toBeUndefined();
    expect(querySignal?.aborted).toBe(false);
  });

  it("aborts while waiting for loader transport readiness", async () => {
    apiMocks.ensureLoaderRequestReady.mockImplementation(
      (_timeout: number | undefined, signal: AbortSignal | undefined) =>
        new Promise<void>((_resolve, reject) => {
          signal?.addEventListener(
            "abort",
            () => reject(signal.reason ?? new Error("aborted")),
            { once: true },
          );
        }),
    );
    const queryFn = vi.fn();
    const controller = new AbortController();
    const context = createRouterContext(createClient());
    const loading = loadRouteQueries(
      createLoaderArgs(context, { controller }),
      [{ queryKey: ["readiness-abort"], queryFn }],
    );

    await waitFor(() =>
      expect(apiMocks.ensureLoaderRequestReady).toHaveBeenCalledWith(
        undefined,
        controller.signal,
      ),
    );
    controller.abort(new DOMException("superseded", "AbortError"));

    await expect(loading).rejects.toMatchObject({ name: "AbortError" });
    expect(queryFn).not.toHaveBeenCalled();
  });

  it("marks failed speculative work silent for QueryCache toast policy", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const onError = vi.fn();
    const queryClient = createClient(new QueryCache({ onError }));
    const queryOptions = {
      queryKey: ["speculative"],
      queryFn: () => Promise.reject(new Error("offline")),
    };
    const context = createRouterContext(queryClient);

    await expect(
      loadRouteQueries(createLoaderArgs(context, { preload: true }), [
        queryOptions,
      ]),
    ).rejects.toThrow("offline");

    const query = queryClient.getQueryCache().find({
      queryKey: ["speculative"],
    });
    expect(query?.meta).toMatchObject({
      routeIntentPrefetch: true,
      silent: true,
    });
    expect(onError).toHaveBeenCalledWith(expect.any(Error), query);
  });

  it("rechecks update state after waiting for request readiness", async () => {
    let isUpdating = false;
    apiMocks.ensureLoaderRequestReady.mockImplementation(async () => {
      isUpdating = true;
    });
    const queryFn = vi.fn();
    const context = createRouterContext(createClient(), () => isUpdating);

    await expect(
      loadRouteQueries(createLoaderArgs(context), [
        { queryKey: ["update-race"], queryFn },
      ]),
    ).rejects.toMatchObject({ code: "update_in_progress" });

    expect(queryFn).not.toHaveBeenCalled();
  });

  it("blocks the first route load from the live mux update flag", async () => {
    apiMocks.getStreamMux.mockReturnValue({ isUpdating: true });
    const queryFn = vi.fn();
    const context = createRouterContext(createClient(), () => false);

    await expect(
      loadRouteQueries(createLoaderArgs(context), [
        { queryKey: ["first-load-update"], queryFn },
      ]),
    ).rejects.toMatchObject({ code: "update_in_progress" });

    expect(apiMocks.ensureLoaderRequestReady).not.toHaveBeenCalled();
    expect(queryFn).not.toHaveBeenCalled();
  });

  it("does not multiply transport retries with Query retries", async () => {
    const queryFn = vi.fn().mockRejectedValue(new Error("offline"));
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: 2, retryDelay: 0 } },
    });
    const context = createRouterContext(queryClient);

    await expect(
      loadRouteQueries(createLoaderArgs(context), [
        { queryKey: ["route-retry-policy"], queryFn },
      ]),
    ).rejects.toThrow("offline");

    expect(queryFn).toHaveBeenCalledTimes(1);
  });

  it("lets sibling requests finish and warm the cache after a batch failure", async () => {
    let resolveSibling!: (value: string) => void;
    const siblingQuery = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          resolveSibling = resolve;
        }),
    );
    const queryClient = createClient();
    const context = createRouterContext(queryClient);

    const loading = loadRouteQueries(createLoaderArgs(context), [
      {
        queryKey: ["failed-batch-member"],
        queryFn: () => Promise.reject(new Error("route failed")),
      },
      { queryKey: ["warming-batch-member"], queryFn: siblingQuery },
    ]);

    await expect(loading).rejects.toThrow("route failed");
    expect(siblingQuery).toHaveBeenCalledTimes(1);

    resolveSibling("warm");
    await waitFor(() =>
      expect(queryClient.getQueryData(["warming-batch-member"])).toBe("warm"),
    );
  });

  it("resolves void after every declared query completes", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const context = createRouterContext(createClient());

    await expect(
      loadRouteQueries(createLoaderArgs(context), [
        { queryKey: ["first"], queryFn: () => Promise.resolve("first") },
        { queryKey: ["second"], queryFn: () => Promise.resolve(2) },
      ]),
    ).resolves.toBeUndefined();
  });

  it("propagates query failures to the route error boundary", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const context = createRouterContext(createClient());

    await expect(
      loadRouteQueries(createLoaderArgs(context), [
        {
          queryKey: ["failed-route-query"],
          queryFn: () => Promise.reject(new Error("route failed")),
        },
      ]),
    ).rejects.toThrow("route failed");

    const query = context.queryClient.getQueryCache().find({
      queryKey: ["failed-route-query"],
    });
    expect(query?.meta).toMatchObject({
      routeInitialLoad: true,
      silent: true,
    });
  });
});
