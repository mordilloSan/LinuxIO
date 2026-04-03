import { Icon } from "@iconify/react";
import React from "react";

import { type NetworkInterface } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import NetworkInterfaceEditor from "@/pages/main/network/NetworkInterfaceEditor";
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
  iface: NetworkInterface;
  expanded: boolean;
  editForm: Record<string, any>;
  setEditForm: (form: Record<string, any>) => void;
  onToggle: () => void;
  onClose: () => void;
  onSave: (iface: NetworkInterface) => void;
}

const NetworkInterfaceCard: React.FC<NetworkInterfaceCardProps> = ({
  iface,
  expanded,
  editForm,
  setEditForm,
  onToggle,
  onClose,
  onSave,
}) => {
  const theme = useAppTheme();
  const primaryColor = theme.palette.primary.main;

  return (
    <FrostedCard
      hoverLift={!expanded}
      style={{ padding: 8, position: "relative", cursor: "pointer" }}
    >
      <AppTooltip title={getStatusTooltip(iface.state)} arrow>
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

      <div
        style={{ display: "flex", alignItems: "flex-start" }}
        onClick={onToggle}
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
            icon={getInterfaceIcon(iface.type)}
            width={36}
            height={36}
            color={primaryColor}
          />
        </div>
        <div style={{ flexGrow: 1 }}>
          <AppTypography variant="subtitle1" fontWeight={600} noWrap>
            {iface.name}
          </AppTypography>
          <AppTypography variant="body2" color="text.secondary" noWrap>
            IPv4: {Array.isArray(iface.ipv4) ? iface.ipv4.join(", ") : "N/A"}
          </AppTypography>
          <AppTypography variant="body2" color="text.secondary" noWrap>
            MAC: {iface.mac}
          </AppTypography>
          <AppTypography variant="body2" color="text.secondary" noWrap>
            {iface.speed === "unknown" || iface.speed.startsWith("-1")
              ? "No Carrier"
              : `Link Speed: ${iface.speed}${iface.duplex !== "unknown" ? ` (${iface.duplex})` : ""}`}
          </AppTypography>
          <AppTypography variant="body2" color="text.secondary" noWrap>
            RX/s: {formatBps(iface.rx_speed)} | TX/s:{" "}
            {formatBps(iface.tx_speed)}
          </AppTypography>
        </div>
      </div>
      <NetworkInterfaceEditor
        iface={iface}
        expanded={expanded}
        editForm={editForm}
        setEditForm={setEditForm}
        onClose={onClose}
        onSave={onSave}
      />
    </FrostedCard>
  );
};

export default NetworkInterfaceCard;
