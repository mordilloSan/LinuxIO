import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import {
  createRouteIntentPreload,
  routeQuery,
} from "@/routing/routeIntentPreload";
import type { RouteWithSidebar } from "@/routing/routeTypes";

const access = {
  ...emptyCapabilityState,
  privileged: false,
};

function route(overrides: Partial<RouteWithSidebar>): RouteWithSidebar {
  return {
    intentPreload: {
      data: false,
      delayMs: 150,
      route: false,
    },
    path: "test",
    ...overrides,
  };
}

describe("routeQuery", () => {
  it("prefetches raw query options as silent route-intent work", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const prefetchQuery = vi.spyOn(queryClient, "prefetchQuery");
    const queryFn = vi.fn(async () => "ready");

    await routeQuery({
      queryKey: ["route", "data"],
      queryFn,
      meta: { feature: "docker" },
    })(queryClient);

    expect(prefetchQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: ["route", "data"],
        meta: {
          feature: "docker",
          routeIntentPrefetch: true,
          silent: true,
        },
      }),
    );
    expect(queryFn).toHaveBeenCalledTimes(1);
  });

  it("accepts generated-style endpoint query option factories", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const endpoint = {
      queryOptions: vi.fn(() => ({
        queryKey: ["endpoint"],
        queryFn: async () => "endpoint-data",
      })),
    };

    await routeQuery(endpoint)(queryClient);

    expect(endpoint.queryOptions).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(["endpoint"])).toBe("endpoint-data");
  });
});

describe("createRouteIntentPreload", () => {
  it("returns undefined when route and data preloading are both inactive", () => {
    const preload = createRouteIntentPreload(
      route({
        intentPreload: {
          data: true,
          delayMs: 150,
          route: true,
        },
      }),
      new QueryClient(),
      access,
    );

    expect(preload).toBeUndefined();
  });

  it("runs route preload and data prefetch tasks when both are enabled", async () => {
    const queryClient = new QueryClient();
    const preloadRoute = vi.fn(async () => "chunk");
    const prefetch = vi.fn(async () => "data");
    const preload = createRouteIntentPreload(
      route({
        intentPreload: {
          data: true,
          delayMs: 250,
          route: true,
        },
        preload: preloadRoute,
        prefetchQueries: [prefetch],
      }),
      queryClient,
      access,
    );

    await preload?.();

    expect(preloadRoute).toHaveBeenCalledTimes(1);
    expect(prefetch).toHaveBeenCalledWith(queryClient);
  });

  it("does not reject when one speculative data prefetch fails", async () => {
    const queryClient = new QueryClient();
    const failingPrefetch = vi.fn(async () => {
      throw new Error("prefetch failed");
    });
    const successfulPrefetch = vi.fn(async () => "ok");
    const preload = createRouteIntentPreload(
      route({
        intentPreload: {
          data: true,
          delayMs: 250,
          route: false,
        },
        prefetchQueries: [failingPrefetch, successfulPrefetch],
      }),
      queryClient,
      access,
    );

    await expect(preload?.()).resolves.toBeUndefined();
    expect(failingPrefetch).toHaveBeenCalledTimes(1);
    expect(successfulPrefetch).toHaveBeenCalledTimes(1);
  });

  it("honors access-gated data prefetch policies", async () => {
    const queryClient = new QueryClient();
    const prefetch = vi.fn(async () => "data");
    const preload = createRouteIntentPreload(
      route({
        intentPreload: {
          data: true,
          delayMs: 250,
          route: false,
        },
        prefetchDataWhen: (ctx) => ctx.privileged,
        prefetchQueries: [prefetch],
      }),
      queryClient,
      { ...access, privileged: false },
    );

    expect(preload).toBeUndefined();
    expect(prefetch).not.toHaveBeenCalled();
  });
});
