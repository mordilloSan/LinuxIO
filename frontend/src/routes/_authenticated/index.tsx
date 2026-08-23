import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
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
        linuxio.system.get_host_info,
        linuxio.system.get_uptime,
        linuxio.system.get_server_time,
      );
    }
    if (!hiddenCards.has("system"))
      queries.push(linuxio.system.get_health_summary);
    if (!hiddenCards.has("cpu")) queries.push(linuxio.system.get_cpu_info);
    if (!hiddenCards.has("memory"))
      queries.push(linuxio.system.get_memory_info);
    if (!hiddenCards.has("nic"))
      queries.push(linuxio.network.get_interface_stats);
    if (!hiddenCards.has("fs")) queries.push(linuxio.system.get_fs_info);
    if (!hiddenCards.has("mb"))
      queries.push(linuxio.system.get_motherboard_info);
    if (!hiddenCards.has("gpu")) queries.push(linuxio.system.get_gpu_info);
    if (!hiddenCards.has("drive")) {
      queries.push(
        linuxio.storage.get_drive_info,
        linuxio.system.get_disk_throughput,
      );
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
