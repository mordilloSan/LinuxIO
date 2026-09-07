import { createFileRoute } from "@tanstack/react-router";

import { CACHE_TTL_MS, linuxio } from "@/api";
import { HomeIcon } from "@/icons/svg";
import {
  type LoaderQueryOptions,
  loadRouteTransport,
  loadRouteUIConfig,
  startRouteQueryPrefetches,
} from "@/routes/-loader";

import DashboardPage from "./-dashboard/DashboardPage";

export const Route = createFileRoute("/_authenticated/")({
  loader: async ({ abortController, context, preload }) => {
    await loadRouteTransport(context, abortController.signal);
    const ui = await loadRouteUIConfig(context, abortController.signal);
    const hiddenCards = new Set(ui.hiddenCards);

    const queries: LoaderQueryOptions[] = [];
    if (!hiddenCards.has("overview")) {
      queries.push(
        { ...linuxio.system.get_host_info, staleTime: CACHE_TTL_MS.ONE_DAY },
        linuxio.system.get_server_time,
      );
    }
    if (!hiddenCards.has("system"))
      queries.push(linuxio.system.get_health_summary);
    if (!hiddenCards.has("cpu")) {
      queries.push({
        ...linuxio.system.get_cpu_info,
        staleTime: CACHE_TTL_MS.ONE_DAY,
      });
    }
    if (
      ["cpu", "memory", "nic", "fs", "gpu", "drive", "overview", "mb"].some(
        (card) => !hiddenCards.has(card),
      )
    ) {
      queries.push(linuxio.monitoring.get_live);
    }
    if (!hiddenCards.has("nic")) queries.push(linuxio.network.get_network_info);
    if (!hiddenCards.has("mb"))
      queries.push({
        ...linuxio.system.get_motherboard_info,
        staleTime: CACHE_TTL_MS.ONE_DAY,
      });
    if (!hiddenCards.has("gpu"))
      queries.push({
        ...linuxio.system.get_gpu_info,
        staleTime: CACHE_TTL_MS.ONE_DAY,
      });
    if (!hiddenCards.has("drive")) {
      queries.push(linuxio.storage.get_drive_info);
    }
    if (context.access.dockerAvailable === true && !hiddenCards.has("docker")) {
      queries.push(
        linuxio.docker.list_containers,
        linuxio.docker.list_images,
        linuxio.docker.list_networks,
        linuxio.docker.list_volumes,
      );
    }

    startRouteQueryPrefetches(
      { context, preload, signal: abortController.signal },
      queries,
    );
  },
  component: DashboardPage,
  staticData: {
    navigation: {
      icon: HomeIcon,
      position: 0,
      title: "Dashboard",
    },
  },
});
