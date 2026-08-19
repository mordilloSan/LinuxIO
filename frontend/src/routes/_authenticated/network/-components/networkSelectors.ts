import type { NetworkInterface } from "@/api";

/**
 * Picks one interface out of the shared `network.get_network_info` cache. The
 * detail cards all observe that one query — the list owns its polling — so
 * every card selects rather than fetching for itself.
 */
export const selectNetworkInterface =
  (name: string) => (interfaces: NetworkInterface[]) =>
    interfaces.find((iface) => iface.name === name);

/**
 * The systemd unit whose journal covers this interface, resolved by the bridge
 * from the detected config backend. Selecting the string (not the interface)
 * keeps a 1 Hz poll from re-rendering the log view.
 */
export const selectNetworkInterfaceLogUnit =
  (name: string) => (interfaces: NetworkInterface[]) =>
    interfaces.find((iface) => iface.name === name)?.log_unit ?? "";
