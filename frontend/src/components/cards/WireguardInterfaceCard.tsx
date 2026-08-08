import { Icon } from "@iconify/react";
import { motion } from "motion/react";
import { RefObject, type CSSProperties } from "react";

import type { WireGuardInterface } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";
import { getWireguardStatusColor } from "@/constants/statusColors";
import { useAppTheme } from "@/theme";
import { GAP_SM, TRANSITION_SLOW_CSS } from "@/theme/constants";

interface InterfaceCardProps {
  handleAddPeer: (name: string, peerData: any) => void;
  handleDelete: (name: string) => void;
  handleSelectInterface: (iface: WireGuardInterface) => void;
  handleToggleBootPersistence: (name: string, isEnabled: boolean) => void;
  handleToggleInterface: (name: string, status: "up" | "down") => void;
  iface: WireGuardInterface;
  selectedCardRef: RefObject<HTMLDivElement> | null;
  selectedInterface: string | null;
}

const cardStyle: CSSProperties = {
  padding: 8,
  display: "flex",
  flexDirection: "column",
  height: "100%",
  position: "relative",
  borderBottomWidth: 2,
  borderBottomStyle: "solid",
};

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
  const isActive = iface.isConnected === "Active";
  const statusColor = getWireguardStatusColor(iface.isConnected);
  const isSelected = iface.name === selectedInterface;
  const detailsId = `wg-card-${iface.name.replace(/[^a-zA-Z0-9_-]/g, "-")}`;

  return (
    <motion.div
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: 20 }}
      initial={{ opacity: 0, y: -20 }}
      layout
      transition={{ duration: 0.3 }}
    >
      <FrostedCard
        hoverLift={!isSelected}
        ref={isSelected ? selectedCardRef : null}
        style={{
          ...cardStyle,
          borderBottomColor: isSelected
            ? statusColor
            : `color-mix(in srgb, ${statusColor}, transparent 70%)`,
          transition: `transform 0.2s, box-shadow 0.2s, border ${TRANSITION_SLOW_CSS}`,
        }}
      >
        {/* Status chip top-right */}
        <div style={{ position: "absolute", top: 12, right: 12 }}>
          <Chip
            color={statusColor}
            label={isActive ? "Active" : "Inactive"}
            labelStyle={{ paddingInline: 6 }}
            size="small"
            style={{ fontSize: "0.65rem" }}
            variant="soft"
          />
        </div>

        <AppButton
          aria-controls={detailsId}
          aria-expanded={isSelected}
          aria-label={`${isSelected ? "Collapse" : "Expand"} ${iface.name}`}
          color="inherit"
          fullWidth
          onClick={() => handleSelectInterface(iface)}
          style={{
            alignItems: "stretch",
            color: "inherit",
            flexDirection: "column",
            justifyContent: "flex-start",
            padding: 0,
            textAlign: "left",
          }}
        >
          {/* Icon + name */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: GAP_SM,
              paddingRight: 72,
            }}
          >
            <Icon
              color={statusColor}
              height={32}
              icon="mdi:shield-lock-outline"
              width={32}
            />
            <AppTypography
              fontWeight={600}
              noWrap
              title={iface.name}
              variant="subtitle1"
            >
              {iface.name}
            </AppTypography>
          </div>

          {/* Address + stats */}
          <div id={detailsId} style={{ marginTop: GAP_SM }}>
            <AppTypography
              color="text.secondary"
              noWrap
              style={{
                display: "block",
                fontFamily: "var(--app-font-mono)",
                fontSize: "0.8rem",
              }}
              variant="body2"
            >
              {iface.address}
            </AppTypography>
            <div style={{ display: "flex", gap: 8, marginTop: 2 }}>
              <AppTypography color="text.secondary" variant="body2">
                Port {iface.port}
              </AppTypography>
              <AppTypography color="text.secondary" variant="body2">
                {iface.peerCount} peer{iface.peerCount === 1 ? "" : "s"}
              </AppTypography>
            </div>
          </div>
        </AppButton>

        <AppDivider style={{ marginBlock: 12 }} />

        {/* Actions */}
        <div style={{ display: "flex", gap: 2, marginTop: "auto" }}>
          <AppActionIconButton
            ariaLabel={isActive ? "Turn interface off" : "Turn interface on"}
            color={isActive ? statusColor : undefined}
            icon="mdi:power"
            iconSize={20}
            label={isActive ? "Turn Off" : "Turn On"}
            onClick={() => {
              handleToggleInterface(iface.name, isActive ? "down" : "up");
            }}
          />
          <AppActionIconButton
            ariaLabel={
              iface.isEnabled
                ? "Disable boot persistence"
                : "Enable boot persistence"
            }
            color={iface.isEnabled ? theme.palette.primary.main : undefined}
            icon="mdi:restart"
            iconSize={20}
            label={
              iface.isEnabled
                ? "Disable Boot Persistence"
                : "Enable Boot Persistence"
            }
            onClick={() => {
              handleToggleBootPersistence(iface.name, iface.isEnabled);
            }}
          />
          <AppActionIconButton
            ariaLabel="Add peer"
            icon="mdi:plus"
            iconSize={20}
            label="Add Peer"
            onClick={() => {
              handleAddPeer(iface.name, {});
            }}
          />
          <AppActionIconButton
            ariaLabel="Delete interface"
            color="var(--app-palette-error-main)"
            icon="mdi:delete"
            iconSize={20}
            label="Delete Interface"
            onClick={() => {
              handleDelete(iface.name);
            }}
          />
        </div>
      </FrostedCard>
    </motion.div>
  );
};

export default InterfaceCard;
