import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { HomeIcon } from "@/icons/svg";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";

import DashboardPage from "./-dashboard/DashboardPage";

export const Route = createFileRoute("/_authenticated/")({
  loader: ({ context, preload }) => {
    const queries: LoaderQueryOptions[] = [
      linuxio.system.get_health_summary.queryOptions(),
      linuxio.system.get_host_info.queryOptions(),
      linuxio.system.get_uptime.queryOptions(),
      linuxio.system.get_server_time.queryOptions(),
      linuxio.system.get_cpu_info.queryOptions(),
      linuxio.system.get_memory_info.queryOptions(),
      linuxio.system.get_fs_info.queryOptions(),
      linuxio.system.get_network_info.queryOptions(),
      linuxio.system.get_motherboard_info.queryOptions(),
      linuxio.system.get_gpu_info.queryOptions(),
      linuxio.storage.get_drive_info.queryOptions(),
      linuxio.system.get_disk_throughput.queryOptions(),
    ];

    if (context.access.dockerAvailable === true) {
      queries.push(
        linuxio.docker.list_containers.queryOptions(),
        linuxio.docker.list_images.queryOptions(),
        linuxio.docker.list_networks.queryOptions(),
        linuxio.docker.list_volumes.queryOptions(),
      );
    }

    return loadRouteQueries({ context, preload }, queries);
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
