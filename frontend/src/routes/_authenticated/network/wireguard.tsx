import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { requireAccess } from "@/routes/-auth";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import WireguardPage from "./-components/wireguard/WireguardPage";

const access = {
  requiredCapabilities: ["wireguardAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/network/wireguard")({
  validateSearch: (search) => optionalString(search, "iface"),
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.wireguard.list_interfaces]),
  component: WireguardPage,
  staticData: { access },
});
