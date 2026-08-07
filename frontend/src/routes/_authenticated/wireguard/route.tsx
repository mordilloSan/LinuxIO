import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { WireguardIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";
import { loadRouteQueries } from "@/routes/-loader";

import WireguardPage from "./-components/WireguardPage";

const access = {
  requiredCapabilities: ["wireguardAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/wireguard")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.wireguard.list_interfaces.queryOptions(),
    ]),
  component: WireguardPage,
  staticData: {
    access,
    navigation: {
      icon: WireguardIcon,
      position: 80,
      title: "Wireguard",
    },
  },
});
