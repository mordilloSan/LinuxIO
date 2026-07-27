import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { CpuIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";
import { loadRouteQueries } from "@/routes/-loader";

import HardwarePage from "./-components/HardwarePage";

const access = {
  requiredCapabilities: ["lmSensorsAvailable"],
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/hardware")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.system.get_sensor_info.queryOptions(),
      linuxio.system.get_pci_devices.queryOptions(),
      linuxio.system.get_memory_modules.queryOptions(),
      linuxio.system.get_motherboard_info.queryOptions(),
      linuxio.system.get_system_info.queryOptions(),
      linuxio.system.get_cpu_info.queryOptions(),
      linuxio.system.get_gpu_info.queryOptions(),
    ]),
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
