import React, { useMemo } from "react";

import NetworkGraph from "./NetworkGraph";

import { linuxio, type NetworkInterface } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

const formatNetworkRate = (value: number): string => {
  if (!Number.isFinite(value) || value <= 0) {
    return "0.0 kB/s";
  }

  if (value >= 1024 * 1024) {
    return `${(value / (1024 * 1024)).toFixed(value >= 10 * 1024 * 1024 ? 0 : 1)} GB/s`;
  }

  if (value >= 1024) {
    return `${(value / 1024).toFixed(value >= 10 * 1024 ? 0 : 1)} MB/s`;
  }

  return `${value.toFixed(value >= 100 ? 0 : 1)} kB/s`;
};

const isPhysicalNetworkInterface = (iface: NetworkInterface): boolean =>
  (iface.type === "ethernet" ||
    iface.type === "wifi" ||
    iface.type === "unknown") &&
  !iface.name.startsWith("veth") &&
  !iface.name.startsWith("docker") &&
  !iface.name.startsWith("br") &&
  !iface.name.startsWith("virbr") &&
  !iface.name.startsWith("wg") &&
  !iface.name.startsWith("tun") &&
  !iface.name.startsWith("tap") &&
  iface.name !== "lo";

const summarizeInterfaces = (interfaces: NetworkInterface[]): string => {
  if (interfaces.length === 0) {
    return "None";
  }

  if (interfaces.length <= 2) {
    return interfaces.map((iface) => iface.name).join(", ");
  }

  return `${interfaces[0].name}, ${interfaces[1].name} +${interfaces.length - 2}`;
};

const NetworkInterfacesCard: React.FC = () => {
  const theme = useAppTheme();
  const { data: rawInterfaces = [], isPending: isLoading } =
    linuxio.dbus.get_network_info.useQuery({
      refetchInterval: 1000,
    });

  const physicalInterfaces = useMemo(
    () => rawInterfaces.filter(isPhysicalNetworkInterface),
    [rawInterfaces],
  );

  const connectedInterfaces = useMemo(
    () =>
      physicalInterfaces.filter(
        (iface) => iface.state === 100 || iface.ipv4.length > 0,
      ),
    [physicalInterfaces],
  );

  const totalRxKBs = useMemo(
    () =>
      physicalInterfaces.reduce(
        (sum, iface) =>
          sum + (Number.isFinite(iface.rx_speed) ? iface.rx_speed / 1024 : 0),
        0,
      ),
    [physicalInterfaces],
  );

  const totalTxKBs = useMemo(
    () =>
      physicalInterfaces.reduce(
        (sum, iface) =>
          sum + (Number.isFinite(iface.tx_speed) ? iface.tx_speed / 1024 : 0),
        0,
      ),
    [physicalInterfaces],
  );

  const totalIPv4Count = useMemo(
    () => physicalInterfaces.reduce((sum, iface) => sum + iface.ipv4.length, 0),
    [physicalInterfaces],
  );

  const content =
    physicalInterfaces.length > 0 ? (
      isLoading ? (
        <ComponentLoader />
      ) : (
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            alignSelf: "flex-start",
            width: "fit-content",
          }}
        >
          {[
            {
              label: "NICs",
              value: summarizeInterfaces(physicalInterfaces),
            },
            {
              label: "Connected",
              value: `${connectedInterfaces.length}/${physicalInterfaces.length}`,
            },
            { label: "IPv4", value: `${totalIPv4Count}` },
            { label: "RX/s", value: formatNetworkRate(totalRxKBs) },
            { label: "TX/s", value: formatNetworkRate(totalTxKBs) },
            {
              label: "Total",
              value: formatNetworkRate(totalRxKBs + totalTxKBs),
            },
          ].map(({ label, value }, index, rows) => (
            <div
              key={label}
              style={{
                display: "flex",
                alignItems: "baseline",
                justifyContent: "flex-start",
                paddingTop: theme.spacing(0.5),
                paddingBottom: theme.spacing(0.5),
                borderBottom:
                  index === rows.length - 1
                    ? "none"
                    : "1px solid var(--app-palette-divider)",
                gap: theme.spacing(1),
              }}
            >
              <AppTypography
                variant="caption"
                color="text.secondary"
                style={{
                  textTransform: "uppercase",
                  letterSpacing: "0.06em",
                  fontSize: "0.62rem",
                  flexShrink: 0,
                }}
              >
                {label}
              </AppTypography>
              <AppTypography variant="body2" fontWeight={500} noWrap>
                {value}
              </AppTypography>
            </div>
          ))}
        </div>
      )
    ) : (
      <AppTypography variant="body2">
        No physical interfaces detected.
      </AppTypography>
    );

  const content2 =
    physicalInterfaces.length > 0 ? (
      isLoading ? (
        <ComponentLoader />
      ) : (
        <div style={{ height: "90px", width: "100%", minWidth: 0 }}>
          <NetworkGraph rx={totalRxKBs} tx={totalTxKBs} />
        </div>
      )
    ) : (
      <AppTypography variant="body2">No graph data.</AppTypography>
    );

  return (
    <DashboardCard
      title="Network"
      avatarIcon="mdi:ethernet"
      stats={content}
      stats2={content2}
      connectionStatus={connectedInterfaces.length > 0 ? "online" : "offline"}
    />
  );
};

export default NetworkInterfacesCard;
