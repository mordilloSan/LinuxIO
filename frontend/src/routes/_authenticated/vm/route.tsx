import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { VirtualMachineIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";
import { loadRouteQueries } from "@/routes/-loader";

import VMPage from "./-components/VMPage";

const access = {
  requiredCapabilities: ["libvirtAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/vm")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.virt.list.queryOptions(),
      linuxio.virt.preflight.queryOptions({}),
    ]),
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
