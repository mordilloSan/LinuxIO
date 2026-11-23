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
  // ?iface= is the selected interface; ?qr= the peer of that interface whose
  // QR code is open. A QR code belongs to an interface, so qr needs iface.
  validateSearch: (search): { iface?: string; qr?: string } => {
    const selected = optionalString(search, "iface");
    return selected.iface
      ? { ...selected, ...optionalString(search, "qr") }
      : selected;
  },
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.wireguard.list_interfaces]),
  component: WireguardPage,
  staticData: { access },
});
