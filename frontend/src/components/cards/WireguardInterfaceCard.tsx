import { Icon } from "@iconify/react";
import { motion } from "framer-motion";
import React, { RefObject, useState } from "react";

import FrostedCard from "@/components/cards/RootCard";
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
import { WireGuardInterface } from "@/types/wireguard";

// Props type
interface InterfaceCardProps {
  iface: WireGuardInterface;
  selectedInterface: string | null;
  selectedCardRef: RefObject<HTMLDivElement> | null;
  primaryColor?: string;
  handleSelectInterface: (iface: WireGuardInterface) => void;
  handleToggleInterface: (name: string, status: "up" | "down") => void;
  handleToggleBootPersistence: (name: string, isEnabled: boolean) => void;
  handleDelete: (name: string) => void;
  handleAddPeer: (name: string, peerData: any) => void;
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
      initial={{ opacity: 0, y: -20 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 20 }}
      transition={{ duration: 0.3 }}
      layout
    >
      <FrostedCard
        ref={isSelected ? selectedCardRef : null}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          cursor: "pointer",
          ...getAccentCardStyles(idleAccentColor),
          transition:
            "border 0.3s ease-in-out, box-shadow 0.3s ease-in-out, margin 0.3s ease-in-out, transform 0.2s",
          ...((isSelected || hovered) && hoverStyles),
        }}
        onClick={() => handleSelectInterface(iface)}
      >
        <AppCardContent>
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
            }}
          >
            <AppTypography variant="subtitle1" fontWeight={700}>
              {iface.name}
            </AppTypography>
            <div>
              <AppTooltip
                title={iface.isConnected === "Active" ? "Turn Off" : "Turn On"}
              >
                <AppIconButton
                  style={{
                    color:
                      iface.isConnected === "Active"
                        ? theme.palette.primary.light
                        : theme.palette.text.disabled,
                  }}
                  aria-label="Power"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleToggleInterface(
                      iface.name,
                      iface.isConnected === "Active" ? "down" : "up",
                    );
                  }}
                >
                  <Icon icon="mdi:power" width={22} height={22} />
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
                  style={{
                    color: iface.isEnabled
                      ? theme.palette.success.main
                      : theme.palette.text.disabled,
                  }}
                  aria-label="Boot Persistence"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleToggleBootPersistence(iface.name, iface.isEnabled);
                  }}
                >
                  <Icon icon="mdi:restart" width={22} height={22} />
                </AppIconButton>
              </AppTooltip>
              <AppTooltip title="Add Peer">
                <AppIconButton
                  onClick={(e) => {
                    e.stopPropagation();
                    handleAddPeer(iface.name, {});
                  }}
                >
                  <Icon icon="mdi:plus" width={22} height={22} />
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
                  <Icon icon="mdi:delete" width={22} height={22} />
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
