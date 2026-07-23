import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const loaderMocks = vi.hoisted(() => ({
  ensureRouteQueryData: vi.fn(),
}));

vi.mock("@/routing/routeQueryLoader", () => ({
  ensureRouteQueryData: loaderMocks.ensureRouteQueryData,
}));

import { linuxio } from "@/api";
import { emptyCapabilityState } from "@/api/capabilities";
import {
  dashboardRouteLoader,
  networkRouteLoader,
  servicesRouteLoader,
  updatesRouteLoader,
} from "@/routing/protectedRouteLoaders";
import type { LinuxIORouterContext } from "@/tanstack-router/router";

function routerContext(
  overrides: Partial<LinuxIORouterContext> = {},
): LinuxIORouterContext {
  return {
    access: { ...emptyCapabilityState, privileged: true },
    auth: {
      isAuthenticated: true,
      isInitialized: true,
      user: { id: "root", name: "root" },
    },
    isUpdateBlocked: () => false,
    queryClient: new QueryClient(),
    ...overrides,
  };
}

const loaderCases = [
  [
    "Dashboard",
    dashboardRouteLoader,
    [
      linuxio.system.get_health_summary.queryOptions().queryKey,
      linuxio.system.get_host_info.queryOptions().queryKey,
      linuxio.system.get_uptime.queryOptions().queryKey,
      linuxio.system.get_server_time.queryOptions().queryKey,
    ],
  ],
  [
    "Network",
    networkRouteLoader,
    [linuxio.network.get_network_info.queryOptions().queryKey],
  ],
  [
    "Updates",
    updatesRouteLoader,
    [linuxio.updates.get_updates_basic.queryOptions().queryKey],
  ],
  [
    "Services",
    servicesRouteLoader,
    [linuxio.systemd.list_services.queryOptions().queryKey],
  ],
] as const;

describe("protected route data loaders", () => {
  beforeEach(() => {
    loaderMocks.ensureRouteQueryData.mockReset();
    loaderMocks.ensureRouteQueryData.mockResolvedValue(undefined);
  });

  it.each(loaderCases)(
    "%s ensures exactly its declared queries on navigation",
    async (_label, loader, expectedQueryKeys) => {
      await loader({
        context: routerContext({
          access: {
            ...emptyCapabilityState,
            packageKitAvailable: true,
            privileged: true,
          },
        }),
        preload: false,
      });

      expect(loaderMocks.ensureRouteQueryData).toHaveBeenCalledTimes(
        expectedQueryKeys.length,
      );
      expect(
        loaderMocks.ensureRouteQueryData.mock.calls.map(
          ([options]) => options.queryOptions.queryKey,
        ),
      ).toEqual(expectedQueryKeys);
      expect(
        loaderMocks.ensureRouteQueryData.mock.calls.every(
          ([options]) => options.speculative === false,
        ),
      ).toBe(true);
    },
  );

  it("gates Updates data on PackageKit availability", async () => {
    await updatesRouteLoader({
      context: routerContext({
        access: {
          ...emptyCapabilityState,
          packageKitAvailable: false,
          privileged: true,
        },
      }),
      preload: false,
    });
    expect(loaderMocks.ensureRouteQueryData).not.toHaveBeenCalled();

    await updatesRouteLoader({
      context: routerContext({
        access: {
          ...emptyCapabilityState,
          packageKitAvailable: true,
          privileged: true,
        },
      }),
      preload: false,
    });
    expect(loaderMocks.ensureRouteQueryData).toHaveBeenCalledTimes(1);
  });

  it("marks intent work speculative and settles every failure", async () => {
    loaderMocks.ensureRouteQueryData.mockRejectedValue(new Error("offline"));
    await expect(
      dashboardRouteLoader({ context: routerContext(), preload: true }),
    ).resolves.toBeUndefined();
    expect(loaderMocks.ensureRouteQueryData).toHaveBeenCalledTimes(4);
    expect(
      loaderMocks.ensureRouteQueryData.mock.calls.every(
        ([options]) => options.speculative === true,
      ),
    ).toBe(true);
  });

  it("keeps navigation failures nonfatal and non-speculative", async () => {
    loaderMocks.ensureRouteQueryData.mockRejectedValue(new Error("offline"));
    await expect(
      networkRouteLoader({ context: routerContext(), preload: false }),
    ).resolves.toBeUndefined();
    expect(loaderMocks.ensureRouteQueryData).toHaveBeenCalledWith(
      expect.objectContaining({ speculative: false }),
    );
  });

  it.each(loaderCases)(
    "%s does no query work while a live update blocks loading",
    async (_label, loader) => {
      await loader({
        context: routerContext({
          access: {
            ...emptyCapabilityState,
            packageKitAvailable: true,
            privileged: true,
          },
          isUpdateBlocked: () => true,
        }),
        preload: true,
      });

      expect(loaderMocks.ensureRouteQueryData).not.toHaveBeenCalled();
    },
  );
});
