import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { HomeIcon } from "@/icons/svg";
import {
  type LoaderQueryOptions,
  loadRouteTransport,
  startRouteQueryPrefetches,
} from "@/routes/-loader";

import DashboardPage from "./-dashboard/DashboardPage";

export const Route = createFileRoute("/_authenticated/")({
  loader: async ({ abortController, context, preload }) => {
    await loadRouteTransport(context, abortController.signal);

    // ConfigProvider owns the authoritative UI load. Route loaders do not
    // create a second config path, so Suspense coverage warms every dashboard
    // query that may mount.
    const queries: LoaderQueryOptions[] = [
      linuxio.system.get_host_info,
      linuxio.system.get_uptime,
      linuxio.system.get_server_time,
      linuxio.system.get_health_summary,
      linuxio.system.get_cpu_info,
      linuxio.system.get_memory_info,
      linuxio.network.get_interface_stats,
      linuxio.system.get_fs_info,
      linuxio.system.get_motherboard_info,
      linuxio.system.get_gpu_info,
      linuxio.storage.get_drive_info,
      linuxio.system.get_disk_throughput,
    ];
    if (context.access.dockerAvailable === true) {
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
