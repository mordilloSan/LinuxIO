import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { VirtualMachineIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-context";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import VMPage from "./-components/VMPage";

const access = {
  requiredCapabilities: ["libvirtAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/vm")({
  validateSearch: (search) => ({
    ...optionalString(search, "vmTab"),
  }),
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: async ({ context, preload }) => {
    const [virtualMachines, preflight] = await loadRouteQueries(
      { context, preload },
      [
        linuxio.virt.list.queryOptions(),
        linuxio.virt.preflight.queryOptions({}),
      ],
    );

    const firstVirtualMachine = virtualMachines?.[0];
    const [initialVirtualMachine] = firstVirtualMachine
      ? await loadRouteQueries({ context, preload }, [
          linuxio.virt.get.queryOptions(firstVirtualMachine.name),
        ])
      : [undefined];

    return { initialVirtualMachine, preflight, virtualMachines };
  },
  component: VMPage,
  staticData: {
    access,
    navigation: {
      icon: VirtualMachineIcon,
      position: 55,
      title: "VMs",
    },
  },
});
