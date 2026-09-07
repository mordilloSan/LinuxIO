import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";

import { linuxio, type LiveInterface, type NetworkInterface } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import { CARD_PADDING_LG, GAP_SM } from "@/theme/constants";
import { formatFileSize } from "@/utils/formaters";

import { selectNetworkInterface } from "./networkSelectors";

interface StatRow {
  label: string;
  /** Rendered in the warning colour: a counter that should normally read 0. */
  warn?: boolean;
  value: string;
}

const formatLink = (iface: NetworkInterface): string => {
  if (iface.speed === "unknown" || iface.speed.startsWith("-1")) {
    return "No carrier";
  }
  return iface.duplex === "unknown"
    ? iface.speed
    : `${iface.speed} (${iface.duplex})`;
};

const formatPackets = (bytes: number, packets: number): string =>
  `${formatFileSize(bytes, 1, "0 Bytes")} · ${packets.toLocaleString()} pkt`;

/**
 * The kernel's own view of the link and its lifetime counters. Carrier and
 * operstate are deliberately absent: they are what the status dot beside the
 * interface name reports. Counters run from boot but reset with the device, so
 * a link that has been down and back up counts from there — the totals answer
 * "since this link came up", not "since install".
 */
export const networkInterfaceStatRows = (
  iface: NetworkInterface,
  live?: LiveInterface,
): StatRow[] => [
  { label: "Link", value: formatLink(iface) },
  { label: "MTU", value: `${iface.mtu}` },
  { label: "Driver", value: iface.driver || "—" },
  { label: "Managed by", value: iface.config_backend || "Unmanaged" },
  {
    label: "Sent",
    value: formatPackets(live?.tx_bytes_total ?? 0, live?.tx_packets ?? 0),
  },
  {
    label: "Received",
    value: formatPackets(live?.rx_bytes_total ?? 0, live?.rx_packets ?? 0),
  },
  {
    label: "Errors (tx/rx)",
    value: `${live?.tx_errors ?? 0} / ${live?.rx_errors ?? 0}`,
    warn: (live?.tx_errors ?? 0) > 0 || (live?.rx_errors ?? 0) > 0,
  },
  {
    label: "Dropped (tx/rx)",
    value: `${live?.tx_dropped ?? 0} / ${live?.rx_dropped ?? 0}`,
    warn: (live?.tx_dropped ?? 0) > 0 || (live?.rx_dropped ?? 0) > 0,
  },
];

const NetworkInterfaceStatsCard = ({ name }: { name: string }) => {
  const { data: iface } = useQuery({
    ...linuxio.network.get_network_info,
    refetchOnMount: false,
    select: selectNetworkInterface(name),
  });
  const { data: liveIface } = useQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: (live) => (live.interfaces ?? {})[name],
  });

  if (!iface) return null;

  return (
    <FrostedCard
      style={{
        boxSizing: "border-box",
        display: "flex",
        flexDirection: "column",
        height: "100%",
        padding: CARD_PADDING_LG,
      }}
    >
      <CardIconHeader
        icon={
          <Icon
            color="var(--app-palette-primary-main)"
            height={22}
            icon="mdi:counter"
            width={22}
          />
        }
        style={{ marginBottom: 4 }}
        title="Statistics"
      />
      <div style={{ display: "flex", flexDirection: "column" }}>
        {networkInterfaceStatRows(iface, liveIface).map((row) => (
          <div
            key={row.label}
            style={{
              alignItems: "baseline",
              borderTop: "1px solid var(--app-palette-divider)",
              display: "flex",
              gap: GAP_SM,
              justifyContent: "space-between",
              padding: "2px 0",
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              {row.label}
            </AppTypography>
            <AppTypography
              noWrap
              style={
                row.warn
                  ? { color: "var(--app-palette-warning-main)" }
                  : undefined
              }
              variant="body2"
            >
              {row.value}
            </AppTypography>
          </div>
        ))}
      </div>
    </FrostedCard>
  );
};

export default NetworkInterfaceStatsCard;
