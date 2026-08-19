import type { AppTheme } from "@/theme";

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

export const getNetworkStateColor = (
  state: number,
  theme: AppTheme,
): string => {
  if (state === 100) return theme.palette.success.main;
  if (state >= 40 && state <= 90) return theme.palette.warning.main;
  if (state === 30 || state === 120) return theme.palette.error.main;
  return theme.palette.text.disabled;
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
