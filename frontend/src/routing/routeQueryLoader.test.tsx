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

import linuxio from "@/api/generated/client";
import { ensureRouteQueryData } from "@/routing/routeQueryLoader";

function createClient(queryCache?: QueryCache) {
  return new QueryClient({
    queryCache,
    defaultOptions: { queries: { retry: false } },
  });
}

describe("ensureRouteQueryData", () => {
  it("accepts typed queryOptions from generated endpoints", async () => {
    await expect(
      ensureRouteQueryData({
        isUpdateBlocked: () => true,
        queryClient: createClient(),
        queryOptions: linuxio.system.get_host_info.queryOptions(),
      }),
    ).rejects.toMatchObject({ code: "update_in_progress" });
  });

  it("does not run readiness or a query while updates block route loading", async () => {
    const queryFn = vi.fn();
    const queryOptions = { queryKey: ["blocked"], queryFn };

    await expect(
      ensureRouteQueryData({
        isUpdateBlocked: () => true,
        queryClient: createClient(),
        queryOptions,
      }),
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

    await expect(
      ensureRouteQueryData({
        isUpdateBlocked: () => false,
        queryClient,
        queryOptions,
      }),
    ).resolves.toBe("ready");

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
    const options = {
      queryKey: ["shared-loader-query"],
      queryFn,
      staleTime: Infinity,
    };

    const first = ensureRouteQueryData({
      isUpdateBlocked: () => false,
      queryClient,
      queryOptions: options,
    });
    const second = ensureRouteQueryData({
      isUpdateBlocked: () => false,
      queryClient,
      queryOptions: options,
    });

    await Promise.resolve();
    resolveQuery("shared");
    await expect(Promise.all([first, second])).resolves.toEqual([
      "shared",
      "shared",
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

    await expect(
      ensureRouteQueryData({
        isUpdateBlocked: () => false,
        queryClient,
        queryOptions,
        speculative: true,
      }),
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

    await expect(
      ensureRouteQueryData({
        isUpdateBlocked: () => isUpdating,
        queryClient: createClient(),
        queryOptions: { queryKey: ["update-race"], queryFn },
      }),
    ).rejects.toMatchObject({ code: "update_in_progress" });

    expect(queryFn).not.toHaveBeenCalled();
  });
});
