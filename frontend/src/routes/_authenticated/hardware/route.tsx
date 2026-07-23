import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { CpuIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-context";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";

import HardwarePage from "./-components/HardwarePage";

const access = {
  requiredCapabilities: ["lmSensorsAvailable"],
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/hardware")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: ({ context, preload }) => {
    const queries: LoaderQueryOptions[] = [
      linuxio.system.get_sensor_info.queryOptions(),
      linuxio.system.get_pci_devices.queryOptions(),
      linuxio.system.get_memory_modules.queryOptions(),
      linuxio.system.get_motherboard_info.queryOptions(),
      linuxio.system.get_system_info.queryOptions(),
      linuxio.system.get_cpu_info.queryOptions(),
      linuxio.system.get_gpu_info.queryOptions(),
    ];

    if (context.access.monitoringAvailable === true) {
      const historyRequest = { limit: 400, resolution: "1m" } as const;
      queries.push(
        linuxio.monitoring.get_cpu_history.queryOptions(historyRequest),
        linuxio.monitoring.get_memory_history.queryOptions(historyRequest),
        linuxio.monitoring.get_diskio_history.queryOptions(historyRequest),
        linuxio.monitoring.get_network_history.queryOptions(historyRequest),
      );
    }

    return loadRouteQueries({ context, preload }, queries);
  },
  component: HardwarePage,
  staticData: {
    access,
    navigation: {
      icon: CpuIcon,
      position: 90,
      title: "Hardware",
    },
  },
});
