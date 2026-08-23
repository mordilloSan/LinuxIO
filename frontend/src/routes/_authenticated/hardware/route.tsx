import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { CpuIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";
import {
  type LoaderQueryOptions,
  loadRouteTransport,
  startRouteQueryPrefetches,
} from "@/routes/-loader";

import HardwarePage from "./-components/HardwarePage";
import {
  hardwareGpuQueryOptions,
  hardwareSensorQueryOptions,
  hardwareStableQueryOptions,
} from "./-components/hardwareQueryOptions";

const access = {
  requiredCapabilities: ["lmSensorsAvailable"],
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/hardware")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: async ({ abortController, context, preload }) => {
    await loadRouteTransport(context, abortController.signal);

    // ConfigProvider owns the authoritative UI load. Warm the full Suspense
    // query set without creating a second config/default path in the loader.
    const queries: LoaderQueryOptions[] = [
      {
        ...linuxio.system.get_sensor_info,
        ...hardwareSensorQueryOptions,
      },
      {
        ...linuxio.system.get_pci_devices,
        ...hardwareStableQueryOptions,
      },
      {
        ...linuxio.system.get_memory_modules,
        ...hardwareStableQueryOptions,
      },
      {
        ...linuxio.system.get_motherboard_info,
        ...hardwareStableQueryOptions,
      },
      {
        ...linuxio.system.get_system_info,
        ...hardwareStableQueryOptions,
      },
      { ...linuxio.system.get_cpu_info, ...hardwareStableQueryOptions },
      { ...linuxio.system.get_gpu_info, ...hardwareGpuQueryOptions },
    ];

    startRouteQueryPrefetches(
      { context, preload, signal: abortController.signal },
      queries,
    );
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
