import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { HomeIcon } from "@/icons/svg";
import {
  type LoaderQueryOptions,
  loadRouteTransport,
  startRouteQueryPrefetches,
} from "@/routes/-loader";
import { readConfigCache } from "@/utils/configCache";

import DashboardPage from "./-dashboard/DashboardPage";

export const Route = createFileRoute("/_authenticated/")({
  loader: async ({ abortController, context, preload }) => {
    await loadRouteTransport(context, abortController.signal);

    // ConfigProvider mounts below the Router. Only warm widgets when its
    // per-user session cache already tells us which cards are visible; on a
    // first visit the mounted cards start their own locally-bounded queries.
    const cachedConfig = readConfigCache(context.auth.user?.id);
    if (!cachedConfig) return;

    const hiddenCards = new Set(cachedConfig.appSettings?.hiddenCards ?? []);
    const visible = (card: string) => !hiddenCards.has(card);
    const queries: LoaderQueryOptions[] = [];

    if (visible("overview")) {
      queries.push(
        linuxio.system.get_host_info,
        linuxio.system.get_uptime,
        linuxio.system.get_server_time,
      );
    }
    if (visible("system")) {
      queries.push(linuxio.system.get_health_summary);
    }
    if (visible("cpu")) {
      queries.push(linuxio.system.get_cpu_info);
    }
    if (visible("memory")) {
      queries.push(linuxio.system.get_memory_info);
    }
    if (visible("nic")) {
      queries.push(linuxio.system.get_network_info);
    }
    if (visible("fs")) {
      queries.push(linuxio.system.get_fs_info);
    }
    if (visible("mb")) {
      queries.push(linuxio.system.get_motherboard_info);
    }
    if (visible("gpu")) {
      queries.push(linuxio.system.get_gpu_info);
    }
    if (visible("drive")) {
      queries.push(
        linuxio.storage.get_drive_info,
        linuxio.system.get_disk_throughput,
      );
    }
    if (visible("docker") && context.access.dockerAvailable === true) {
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
