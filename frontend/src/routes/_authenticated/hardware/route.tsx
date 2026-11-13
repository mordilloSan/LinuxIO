import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { createIsSectionOpen } from "@/hooks/useSectionToggles";
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
import { HARDWARE_SECTIONS } from "./-components/hardwareSections";

const access = {
  requiredCapabilities: ["lmSensorsAvailable"],
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/hardware")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: async ({ abortController, context, preload }) => {
    await loadRouteTransport(context, abortController.signal);
    const ui = await loadRouteUIConfig(context, abortController.signal);
    const isOpen = createIsSectionOpen(ui.collapsedSections, HARDWARE_SECTIONS);

    const queries: LoaderQueryOptions[] = [];
    if (isOpen("sensors")) queries.push(linuxio.monitoring.get_live);
    if (isOpen("pciDevices"))
      queries.push({
        ...linuxio.system.get_pci_devices,
        ...hardwareStableQueryOptions,
      });
    if (isOpen("memoryModules"))
      queries.push({
        ...linuxio.system.get_memory_modules,
        ...hardwareStableQueryOptions,
      });
    if (isOpen("systemInfo")) {
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
