import { Icon } from "@iconify/react";
import { motion } from "framer-motion";
import React, { RefObject, useState } from "react";

import type { WireGuardInterface } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
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

const InterfaceCard: React.FC<InterfaceCardProps> = ({
  iface,
  selectedInterface,
  selectedCardRef,
  handleSelectInterface,
  handleToggleInterface,
  handleToggleBootPersistence,
  handleDelete,
  handleAddPeer,
}) => {
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
        onClick={() => handleSelectInterface(iface)}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        ref={isSelected ? selectedCardRef : null}
        style={{
          cursor: "pointer",
          ...getAccentCardStyles(idleAccentColor),
          transition:
            "border 0.3s ease-in-out, box-shadow 0.3s ease-in-out, margin 0.3s ease-in-out, transform 0.2s",
          ...((isSelected || hovered) && hoverStyles),
        }}
      >
        <AppCardContent>
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
            }}
          >
            <AppTypography fontWeight={700} variant="subtitle1">
              {iface.name}
            </AppTypography>
            <div>
              <AppTooltip
                title={iface.isConnected === "Active" ? "Turn Off" : "Turn On"}
              >
                <AppIconButton
                  aria-label="Power"
                  onClick={(e) => {
                    e.stopPropagation();
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
                  aria-label="Boot Persistence"
                  onClick={(e) => {
                    e.stopPropagation();
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
                  onClick={(e) => {
                    e.stopPropagation();
                    handleAddPeer(iface.name, {});
                  }}
                >
                  <Icon height={22} icon="mdi:plus" width={22} />
                </AppIconButton>
              </AppTooltip>
              <AppTooltip title="Delete Interface">
                <AppIconButton
                  color="error"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDelete(iface.name);
                  }}
                >
                  <Icon height={22} icon="mdi:delete" width={22} />
                </AppIconButton>
              </AppTooltip>
            </div>
          </div>
          <div style={{ marginTop: 6 }}>
            <InfoRow label="Address" wrap>
              {iface.address}
            </InfoRow>
            <InfoRow label="Port">{iface.port}</InfoRow>
            <InfoRow label="Peers" noBorder>
              {iface.peerCount}
            </InfoRow>
          </div>
        </AppCardContent>
      </FrostedCard>
    </motion.div>
  );
};

export default InterfaceCard;
