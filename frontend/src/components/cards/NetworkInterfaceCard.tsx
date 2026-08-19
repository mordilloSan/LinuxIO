import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { memo, useCallback } from "react";

import { linuxio, type NetworkInterface } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useAppTheme } from "@/theme";
import { CARD_PADDING_SM } from "@/theme/constants";

const getStatusTooltip = (state: number) => {
  if (state === 100) return "Connected";
  if (state === 110) return "Deactivating";
  if (state >= 40 && state <= 90) return "Connecting";
  if (state === 30) return "Disconnected";
  if (state === 20) return "Unavailable";
  if (state === 120) return "Failed";
  if (state === 10) return "Unmanaged";
  return "Unknown";
};

const getInterfaceIcon = (type?: string) => {
  if (type === "wifi") return "mdi:wifi";
  if (type === "ethernet") return "mdi:ethernet";
  if (type === "loopback") return "mdi:lan-connect";
  return "mdi:network";
};

const formatBps = (bps?: number) =>
  typeof bps === "number" ? `${(bps / 1024).toFixed(1)} kB/s` : "N/A";

export interface NetworkInterfaceCardProps {
  name: string;
  onToggle: (name: string) => void;
  type: string;
}

const selectNetworkInterface =
  (name: string) => (interfaces: NetworkInterface[]) =>
    interfaces.find((iface) => iface.name === name);

const NetworkInterfaceIcon = memo(function NetworkInterfaceIcon({
  color,
  type,
}: {
  color: string;
  type: string;
}) {
  return (
    <div
      style={{
        width: 44,
        height: 44,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        marginRight: 6,
      }}
    >
      <Icon
        color={color}
        height={36}
        icon={getInterfaceIcon(type)}
        width={36}
      />
    </div>
  );
});

const NetworkInterfaceTitle = memo(function NetworkInterfaceTitle({
  name,
}: {
  name: string;
}) {
  return (
    <AppTypography fontWeight={600} noWrap variant="subtitle1">
      {name}
    </AppTypography>
  );
});

const NetworkInterfaceCardContent = ({
  name,
  onToggle,
  type,
}: NetworkInterfaceCardProps) => {
  const theme = useAppTheme();
  const handleToggle = useCallback(() => onToggle(name), [name, onToggle]);
  const { data: rawInterface } = useQuery({
    ...linuxio.network.get_network_info,
    refetchOnMount: false,
    select: selectNetworkInterface(name),
  });

  if (!rawInterface) return null;

  const iface = { ...rawInterface, type };
  const primaryColor = theme.palette.primary.main;

  return (
    <>
      <StatusDot
        absolute
        color={
          iface.state === 100
            ? theme.palette.success.main
            : iface.state >= 40 && iface.state <= 90
              ? theme.palette.warning.main
              : iface.state === 30 || iface.state === 120
                ? theme.palette.error.main
                : theme.palette.text.disabled
        }
        size={10}
        style={{ top: 16, right: 8 }}
        tooltip={getStatusTooltip(iface.state)}
      />

      <AppButton
        color="inherit"
        onClick={handleToggle}
        style={{
          appearance: "none",
          background: "none",
          border: 0,
          color: "inherit",
          cursor: "pointer",
          display: "flex",
          font: "inherit",
          padding: 0,
          textAlign: "left",
          alignItems: "flex-start",
          width: "100%",
        }}
      >
        <NetworkInterfaceIcon color={primaryColor} type={type} />
        <div style={{ flexGrow: 1 }}>
          <NetworkInterfaceTitle name={name} />
          <AppTypography color="text.secondary" noWrap variant="body2">
            IPv4: {Array.isArray(iface.ipv4) ? iface.ipv4.join(", ") : "N/A"}
          </AppTypography>
          <AppTypography color="text.secondary" noWrap variant="body2">
            MAC: {iface.mac}
          </AppTypography>
          <AppTypography color="text.secondary" noWrap variant="body2">
            {iface.speed === "unknown" || iface.speed.startsWith("-1")
              ? "No Carrier"
              : `Link Speed: ${iface.speed}${iface.duplex !== "unknown" ? ` (${iface.duplex})` : ""}`}
          </AppTypography>
          <AppTypography color="text.secondary" noWrap variant="body2">
            RX/s: {formatBps(iface.rx_speed)} | TX/s:{" "}
            {formatBps(iface.tx_speed)}
          </AppTypography>
        </div>
      </AppButton>
    </>
  );
};

const NetworkInterfaceCard = ({
  name,
  onToggle,
  type,
}: NetworkInterfaceCardProps) => {
  return (
    <FrostedCard
      accent
      hoverLift
      style={{
        padding: CARD_PADDING_SM,
        position: "relative",
      }}
    >
      <NetworkInterfaceCardContent
        name={name}
        onToggle={onToggle}
        type={type}
      />
    </FrostedCard>
  );
};

export default NetworkInterfaceCard;
