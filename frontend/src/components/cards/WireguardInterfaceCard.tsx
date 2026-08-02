import { Icon } from "@iconify/react";
import { motion } from "motion/react";
import { RefObject, useState } from "react";

import type { WireGuardInterface } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import { useAppTheme } from "@/theme";
import {
  getAccentCardHoverStyles,
  getAccentCardStyles,
} from "@/theme/surfaces";

// Props type
interface InterfaceCardProps {
  handleAddPeer: (name: string, peerData: any) => void;
  handleDelete: (name: string) => void;
  handleSelectInterface: (iface: WireGuardInterface) => void;
  handleToggleBootPersistence: (name: string, isEnabled: boolean) => void;
  handleToggleInterface: (name: string, status: "up" | "down") => void;
  iface: WireGuardInterface;
  primaryColor?: string;
  selectedCardRef: RefObject<HTMLDivElement> | null;
  selectedInterface: string | null;
}

const InterfaceCard = ({
  iface,
  selectedInterface,
  selectedCardRef,
  handleSelectInterface,
  handleToggleInterface,
  handleToggleBootPersistence,
  handleDelete,
  handleAddPeer,
}: InterfaceCardProps) => {
  const theme = useAppTheme();
  const color = "primary";
  const activeAccentColor =
    theme.palette[color]?.main || theme.palette.primary.main;
  const idleAccentColor =
    theme.palette[color]?.dark || theme.palette.primary.dark;

  const hoverStyles = getAccentCardHoverStyles(theme, activeAccentColor);
  const isSelected = iface.name === selectedInterface;
  const [hovered, setHovered] = useState(false);

  return (
    <motion.div
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 20 }}
      initial={{ opacity: 0, y: -20 }}
      layout
      transition={{ duration: 0.3 }}
    >
      <FrostedCard
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        ref={isSelected ? selectedCardRef : null}
        style={{
          ...getAccentCardStyles(idleAccentColor),
          transition:
            "border 0.3s ease-in-out, box-shadow 0.3s ease-in-out, margin 0.3s ease-in-out, transform 0.2s",
          ...((isSelected || hovered) && hoverStyles),
        }}
      >
        <AppCardContent>
          <div style={{ display: "flex", alignItems: "flex-start" }}>
            <AppButton
              aria-pressed={isSelected}
              color="inherit"
              onClick={() => handleSelectInterface(iface)}
              style={{
                appearance: "none",
                background: "none",
                border: 0,
                color: "inherit",
                cursor: "pointer",
                display: "block",
                flex: 1,
                font: "inherit",
                padding: 0,
                textAlign: "left",
              }}
            >
              <AppTypography fontWeight={700} variant="subtitle1">
                {iface.name}
              </AppTypography>
              <div style={{ marginTop: 6 }}>
                <InfoRow label="Address" wrap>
                  {iface.address}
                </InfoRow>
                <InfoRow label="Port">{iface.port}</InfoRow>
                <InfoRow label="Peers" noBorder>
                  {iface.peerCount}
                </InfoRow>
              </div>
            </AppButton>
            <div style={{ marginLeft: 8 }}>
              <AppTooltip
                title={iface.isConnected === "Active" ? "Turn Off" : "Turn On"}
              >
                <AppIconButton
                  aria-label={
                    iface.isConnected === "Active"
                      ? "Turn interface off"
                      : "Turn interface on"
                  }
                  onClick={() => {
                    handleToggleInterface(
                      iface.name,
                      iface.isConnected === "Active" ? "down" : "up",
                    );
                  }}
                  style={{
                    color:
                      iface.isConnected === "Active"
                        ? theme.palette.primary.light
                        : theme.palette.text.disabled,
                  }}
                >
                  <Icon height={22} icon="mdi:power" width={22} />
                </AppIconButton>
              </AppTooltip>
              <AppTooltip
                title={
                  iface.isEnabled
                    ? "Disable Boot Persistence"
                    : "Enable Boot Persistence"
                }
              >
                <AppIconButton
                  aria-label={
                    iface.isEnabled
                      ? "Disable boot persistence"
                      : "Enable boot persistence"
                  }
                  onClick={() => {
                    handleToggleBootPersistence(iface.name, iface.isEnabled);
                  }}
                  style={{
                    color: iface.isEnabled
                      ? theme.palette.success.main
                      : theme.palette.text.disabled,
                  }}
                >
                  <Icon height={22} icon="mdi:restart" width={22} />
                </AppIconButton>
              </AppTooltip>
              <AppTooltip title="Add Peer">
                <AppIconButton
                  aria-label="Add peer"
                  onClick={() => {
                    handleAddPeer(iface.name, {});
                  }}
                >
                  <Icon height={22} icon="mdi:plus" width={22} />
                </AppIconButton>
              </AppTooltip>
              <AppTooltip title="Delete Interface">
                <AppIconButton
                  aria-label="Delete interface"
                  color="error"
                  onClick={() => {
                    handleDelete(iface.name);
                  }}
                >
                  <Icon height={22} icon="mdi:delete" width={22} />
                </AppIconButton>
              </AppTooltip>
            </div>
          </div>
        </AppCardContent>
      </FrostedCard>
    </motion.div>
  );
};

export default InterfaceCard;
