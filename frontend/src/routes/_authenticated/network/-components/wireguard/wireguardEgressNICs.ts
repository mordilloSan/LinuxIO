import type { NetworkInterface } from "@/api";

// Links that carry traffic only between the host and its containers or VMs:
// Docker's bridges and veth pairs, libvirt's NAT bridge and VM taps.
const internalPrefixes = ["veth", "docker", "br-", "virbr", "vnet"];

// The way out first: a card with an IPv4 gateway, then one with an address.
const rank = (nic: NetworkInterface) => {
  const hasIPv4 = Array.isArray(nic.ipv4) && nic.ipv4.length > 0;
  if (hasIPv4 && nic.gateway) return 0;
  return hasIPv4 ? 1 : 2;
};

/**
 * The cards a WireGuard interface's traffic can leave by, best first, so the
 * first is the default. An internal link is left out unless it holds a
 * gateway: a bridge the LAN card is a port of (`br-enp2s0`) is the way out,
 * and its port, which has no address of its own, is listed last.
 */
export const getEgressNICOptions = (
  data: NetworkInterface[] | undefined,
): { name: string; label: string }[] => {
  if (!Array.isArray(data)) return [];

  return data
    .filter(
      (nic) =>
        (nic.type === "ethernet" || nic.type === "wifi") &&
        nic.mac &&
        (nic.gateway ||
          !internalPrefixes.some((prefix) => nic.name.startsWith(prefix))),
    )
    .toSorted((a, b) => rank(a) - rank(b))
    .map((nic) => ({
      name: nic.name,
      label: `${nic.name} (${nic.ipv4?.[0] ?? "no IPv4 address"})`,
    }));
};
