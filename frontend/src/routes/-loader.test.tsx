import {
  QueryCache,
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  ensureLoaderRequestReady: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    ensureLoaderRequestReady: apiMocks.ensureLoaderRequestReady,
  };
});

import { emptyCapabilityState } from "@/api/capabilities";
import linuxio from "@/api/generated/client";
import type { LinuxIORouterContext } from "@/routes/-context";
import { loadRouteQueries } from "@/routes/-loader";

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

describe("loadRouteQueries", () => {
  it("accepts typed queryOptions from generated endpoints", async () => {
    const context = createRouterContext(createClient(), () => true);

    await expect(
      loadRouteQueries({ context, preload: false }, [
        linuxio.system.get_host_info.queryOptions(),
      ]),
    ).rejects.toMatchObject({ code: "update_in_progress" });
  });

  it("does not run readiness or a query while updates block route loading", async () => {
    const queryFn = vi.fn();
    const queryOptions = { queryKey: ["blocked"], queryFn };
    const context = createRouterContext(createClient(), () => true);

    await expect(
      loadRouteQueries({ context, preload: false }, [queryOptions]),
    ).rejects.toMatchObject({ code: "update_in_progress" });

    expect(apiMocks.ensureLoaderRequestReady).not.toHaveBeenCalled();
    expect(queryFn).not.toHaveBeenCalled();
  });

  it("seeds the same cache entry for a component observer without another initial request", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const queryFn = vi.fn().mockResolvedValue("ready");
    const queryOptions = {
      queryKey: ["shared-query"],
      queryFn,
      staleTime: Infinity,
    };
    const queryClient = createClient();
    const context = createRouterContext(queryClient);

    await expect(
      loadRouteQueries({ context, preload: false }, [queryOptions]),
    ).resolves.toEqual(["ready"]);

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
    expect(queryFn).toHaveBeenCalledTimes(1);
  });

  it("deduplicates simultaneous route-loader callers for the shared cache key", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
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

    const first = loadRouteQueries({ context, preload: false }, [options]);
    const second = loadRouteQueries({ context, preload: false }, [options]);

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    resolveQuery("shared");
    await expect(Promise.all([first, second])).resolves.toEqual([
      ["shared"],
      ["shared"],
    ]);
    expect(queryFn).toHaveBeenCalledTimes(1);
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
      loadRouteQueries({ context, preload: true }, [queryOptions]),
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
      loadRouteQueries({ context, preload: false }, [
        { queryKey: ["update-race"], queryFn },
      ]),
    ).rejects.toMatchObject({ code: "update_in_progress" });

    expect(queryFn).not.toHaveBeenCalled();
  });
  it("returns route data in query declaration order", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const context = createRouterContext(createClient());

    await expect(
      loadRouteQueries({ context, preload: false }, [
        { queryKey: ["first"], queryFn: () => Promise.resolve("first") },
        { queryKey: ["second"], queryFn: () => Promise.resolve(2) },
      ]),
    ).resolves.toEqual(["first", 2]);
  });

  it("propagates query failures to the route error boundary", async () => {
    apiMocks.ensureLoaderRequestReady.mockResolvedValue(undefined);
    const context = createRouterContext(createClient());

    await expect(
      loadRouteQueries({ context, preload: false }, [
        {
          queryKey: ["failed-route-query"],
          queryFn: () => Promise.reject(new Error("route failed")),
        },
      ]),
    ).rejects.toThrow("route failed");
  });
});
