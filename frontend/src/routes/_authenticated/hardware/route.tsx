import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { CpuIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";
import {
  type LoaderQueryOptions,
  loadRouteTransport,
  loadRouteUIConfig,
  startRouteQueryPrefetches,
} from "@/routes/-loader";

import HardwarePage from "./-components/HardwarePage";
import { hardwareStableQueryOptions } from "./-components/hardwareQueryOptions";

const access = {
  requiredCapabilities: ["lmSensorsAvailable"],
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/hardware")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: async ({ abortController, context, preload }) => {
    await loadRouteTransport(context, abortController.signal);
    const ui = await loadRouteUIConfig(context, abortController.signal);
    const sections = ui.hardwareSections;

    const queries: LoaderQueryOptions[] = [];
    if (sections.sensors) queries.push(linuxio.monitoring.get_live);
    if (sections.pciDevices)
      queries.push({
        ...linuxio.system.get_pci_devices,
        ...hardwareStableQueryOptions,
      });
    if (sections.memoryModules)
      queries.push({
        ...linuxio.system.get_memory_modules,
        ...hardwareStableQueryOptions,
      });
    if (sections.systemInfo) {
      queries.push(
        {
          ...linuxio.system.get_motherboard_info,
          ...hardwareStableQueryOptions,
        },
        { ...linuxio.system.get_system_info, ...hardwareStableQueryOptions },
        { ...linuxio.system.get_cpu_info, ...hardwareStableQueryOptions },
        { ...linuxio.system.get_gpu_info, ...hardwareStableQueryOptions },
        linuxio.monitoring.get_live,
      );
    }

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
