import { linuxio } from "@/api";
import type { ProtectedRouteId } from "@/routing/protectedRouteCatalog";
import { ensureRouteQueryData } from "@/routing/routeQueryLoader";
import type { ProtectedRouteLoader } from "@/tanstack-router/router";

export const dashboardRouteLoader: ProtectedRouteLoader = async ({
  context,
  preload,
}) => {
  if (context.isUpdateBlocked()) return;

  await Promise.allSettled([
    ensureRouteQueryData({
      isUpdateBlocked: context.isUpdateBlocked,
      queryClient: context.queryClient,
      queryOptions: linuxio.system.get_health_summary.queryOptions(),
      speculative: preload,
    }),
    ensureRouteQueryData({
      isUpdateBlocked: context.isUpdateBlocked,
      queryClient: context.queryClient,
      queryOptions: linuxio.system.get_host_info.queryOptions(),
      speculative: preload,
    }),
    ensureRouteQueryData({
      isUpdateBlocked: context.isUpdateBlocked,
      queryClient: context.queryClient,
      queryOptions: linuxio.system.get_uptime.queryOptions(),
      speculative: preload,
    }),
    ensureRouteQueryData({
      isUpdateBlocked: context.isUpdateBlocked,
      queryClient: context.queryClient,
      queryOptions: linuxio.system.get_server_time.queryOptions(),
      speculative: preload,
    }),
  ]);
};

export const networkRouteLoader: ProtectedRouteLoader = async ({
  context,
  preload,
}) => {
  if (context.isUpdateBlocked()) return;

  await Promise.allSettled([
    ensureRouteQueryData({
      isUpdateBlocked: context.isUpdateBlocked,
      queryClient: context.queryClient,
      queryOptions: linuxio.network.get_network_info.queryOptions(),
      speculative: preload,
    }),
  ]);
};

export const updatesRouteLoader: ProtectedRouteLoader = async ({
  context,
  preload,
}) => {
  if (
    context.isUpdateBlocked() ||
    context.access.packageKitAvailable !== true
  ) {
    return;
  }

  await Promise.allSettled([
    ensureRouteQueryData({
      isUpdateBlocked: context.isUpdateBlocked,
      queryClient: context.queryClient,
      queryOptions: linuxio.updates.get_updates_basic.queryOptions(),
      speculative: preload,
    }),
  ]);
};

export const servicesRouteLoader: ProtectedRouteLoader = async ({
  context,
  preload,
}) => {
  if (context.isUpdateBlocked()) return;

  await Promise.allSettled([
    ensureRouteQueryData({
      isUpdateBlocked: context.isUpdateBlocked,
      queryClient: context.queryClient,
      queryOptions: linuxio.systemd.list_services.queryOptions(),
      speculative: preload,
    }),
  ]);
};

export const protectedRouteLoaders = {
  dashboard: dashboardRouteLoader,
  network: networkRouteLoader,
  services: servicesRouteLoader,
  updates: updatesRouteLoader,
} as const satisfies Partial<Record<ProtectedRouteId, ProtectedRouteLoader>>;
