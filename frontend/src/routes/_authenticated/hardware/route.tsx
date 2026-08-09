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
import { readConfigCache } from "@/utils/configCache";

import HardwarePage from "./-components/HardwarePage";
import {
  hardwareGpuQueryOptions,
  hardwareSensorQueryOptions,
  hardwareStableQueryOptions,
} from "./-components/hardwareQueryOptions";
import { resolvedHardwareSections } from "./-components/hardwareSections";

const access = {
  requiredCapabilities: ["lmSensorsAvailable"],
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/hardware")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: async ({ abortController, context, preload }) => {
    await loadRouteTransport(context, abortController.signal);

    const cachedConfig = readConfigCache(context.auth.user?.id);
    if (!cachedConfig) return;

    const sections = resolvedHardwareSections(
      cachedConfig.appSettings?.hardwareSections,
    );
    const queries: LoaderQueryOptions[] = [];
    if (sections.sensors) {
      queries.push({
        ...linuxio.system.get_sensor_info,
        ...hardwareSensorQueryOptions,
      });
    }
    if (sections.pciDevices) {
      queries.push({
        ...linuxio.system.get_pci_devices,
        ...hardwareStableQueryOptions,
      });
    }
    if (sections.memoryModules) {
      queries.push({
        ...linuxio.system.get_memory_modules,
        ...hardwareStableQueryOptions,
      });
    }
    if (sections.systemInfo) {
      queries.push(
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
