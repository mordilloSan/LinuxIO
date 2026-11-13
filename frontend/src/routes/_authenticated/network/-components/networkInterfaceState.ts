/**
 * NetworkManager-style device states, as reported by
 * `network.get_network_info`. Shared by the interface list card's status dot
 * and the detail card's, so the two cannot drift apart.
 */
export const getNetworkStateLabel = (state: number): string => {
  if (state === 100) return "Connected";
  if (state === 110) return "Deactivating";
  if (state >= 40 && state <= 90) return "Connecting";
  if (state === 30) return "Disconnected";
  if (state === 20) return "Unavailable";
  if (state === 120) return "Failed";
  if (state === 10) return "Unmanaged";
  return "Unknown";
};

export const getNetworkStateColor = (state: number): string => {
  if (state === 100) return "var(--app-palette-success-main)";
  if (state >= 40 && state <= 90) return "var(--app-palette-warning-main)";
  if (state === 30 || state === 120) return "var(--app-palette-error-main)";
  return "var(--app-palette-text-disabled)";
};

/**
 * Carrier is nil whenever the kernel refuses to answer — a down link, or a
 * virtual device with no such attribute — so "unknown" is a real third state.
 */
export const getNetworkCarrierLabel = (
  carrier: boolean | undefined,
): string => {
  if (carrier === undefined) return "unknown";
  return carrier ? "connected" : "down";
};

/**
 * The one-line summary behind the status dot. It carries the carrier and
 * operstate readings that the statistics card no longer lists.
 */
export const getNetworkStateSummary = (iface: {
  carrier?: boolean;
  operstate: string;
  state: number;
}): string =>
  [
    getNetworkStateLabel(iface.state),
    `carrier ${getNetworkCarrierLabel(iface.carrier)}`,
    `link ${iface.operstate}`,
  ].join(" · ");

/** The host the browser used to reach LinuxIO. */
export const getBrowserHostname = () => window.location.hostname;

const isLoopbackHost = (hostname: string) =>
  hostname === "localhost" ||
  hostname.endsWith(".localhost") ||
  hostname === "[::1]" ||
  /^127(\.\d{1,3}){3}$/.test(hostname);

const isIPv4Host = (hostname: string) =>
  /^(\d{1,3})(\.\d{1,3}){3}$/.test(hostname);

/**
 * Whether the browser may be reaching LinuxIO through this interface, so that
 * disabling it or re-addressing it can end the session. No route reports the
 * browser's own address, so this decides from what the page already has: the
 * interface's addresses and `window.location.hostname`.
 *
 * - An interface that is not connected, or has no address, is plainly not it.
 * - A loopback hostname never crosses an interface.
 * - An IPv4 hostname is matched against the interface's addresses.
 * - Any other hostname (a name, or an IPv6 literal the interface list does not
 *   carry) cannot be resolved here, so a connected interface with an address
 *   counts as possibly the one.
 */
export const mayCarryBrowserSession = (
  iface: { ipv4: string[]; state: number },
  hostname: string,
): boolean => {
  if (iface.state !== 100 || iface.ipv4.length === 0) return false;
  if (isLoopbackHost(hostname)) return false;
  if (!isIPv4Host(hostname)) return true;
  return iface.ipv4.some((cidr) => cidr.split("/")[0] === hostname);
};
