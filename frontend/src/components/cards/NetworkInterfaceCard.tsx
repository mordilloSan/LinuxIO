import { Icon } from "@iconify/react";
import { useId } from "react";

import { type NetworkInterface } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import NetworkInterfaceEditor from "@/components/network/NetworkInterfaceEditor";
import AppButton from "@/components/ui/AppButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

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
  expanded: boolean;
  iface: NetworkInterface;
  onClose: () => void;
  onToggle: () => void;
}

const NetworkInterfaceCard = ({
  iface,
  expanded,
  onToggle,
  onClose,
}: NetworkInterfaceCardProps) => {
  const theme = useAppTheme();
  const primaryColor = theme.palette.primary.main;
  const editorId = useId();

  return (
    <FrostedCard
      hoverLift={!expanded}
      style={{ padding: 8, position: "relative" }}
    >
      <AppTooltip arrow title={getStatusTooltip(iface.state)}>
        <span
          style={{
            position: "absolute",
            top: 16,
            right: 8,
            width: 10,
            height: 10,
            borderRadius: "50%",
            display: "inline-block",
            backgroundColor:
              iface.state === 100
                ? theme.palette.success.main
                : iface.state >= 40 && iface.state <= 90
                  ? theme.palette.warning.main
                  : iface.state === 30 || iface.state === 120
                    ? theme.palette.error.main
                    : theme.palette.text.disabled,
          }}
        />
      </AppTooltip>

      <AppButton
        aria-controls={editorId}
        aria-expanded={expanded}
        color="inherit"
        onClick={onToggle}
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
            color={primaryColor}
            height={36}
            icon={getInterfaceIcon(iface.type)}
            width={36}
          />
        </div>
        <div style={{ flexGrow: 1 }}>
          <AppTypography fontWeight={600} noWrap variant="subtitle1">
            {iface.name}
          </AppTypography>
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
      <div id={editorId}>
        <NetworkInterfaceEditor
          expanded={expanded}
          iface={iface}
          onClose={onClose}
        />
      </div>
    </FrostedCard>
  );
};

export default NetworkInterfaceCard;
