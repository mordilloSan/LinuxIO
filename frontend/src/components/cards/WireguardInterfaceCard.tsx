import { Icon } from "@iconify/react";
import { motion } from "motion/react";
import type { RefObject } from "react";
import { type CSSProperties } from "react";

import type { WireGuardInterface } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";
import { getWireguardStatusColor } from "@/constants/statusColors";
import { CARD_PADDING_SM, GAP_SM } from "@/theme/constants";

export type WireguardInterfaceAction =
  | "add-peer"
  | "delete"
  | "disable"
  | "down"
  | "enable"
  | "up";

interface InterfaceCardProps {
  handleAddPeer: (name: string, peerData: any) => void;
  handleDelete: (name: string) => void;
  handleSelectInterface: (iface: WireGuardInterface) => void;
  handleToggleBootPersistence: (name: string, isEnabled: boolean) => void;
  handleToggleInterface: (name: string, status: "up" | "down") => void;
  iface: WireGuardInterface;
  pendingAction?: WireguardInterfaceAction;
  selectedCardRef: RefObject<HTMLDivElement> | null;
  selectedInterface: string | null;
}

const cardStyle: CSSProperties = {
  padding: CARD_PADDING_SM,
  display: "flex",
  flexDirection: "column",
  height: "100%",
  position: "relative",
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
  pendingAction,
}: InterfaceCardProps) => {
  const isActive = iface.isConnected === "Active";
  const statusColor = getWireguardStatusColor(iface.isConnected);
  const isSelected = iface.name === selectedInterface;
  const actionBusy = Boolean(pendingAction);
  const togglePending = pendingAction === "up" || pendingAction === "down";
  const bootPending = pendingAction === "enable" || pendingAction === "disable";
  const detailsId = `wg-card-${iface.name.replace(/[^a-zA-Z0-9_-]/g, "-")}`;

  return (
    <motion.div
      animate={{ opacity: 1, y: 0 }}
      initial={{ opacity: 0, y: -20 }}
      layout
      transition={{ duration: 0.3 }}
    >
      <FrostedCard
        accent
        hoverLift={!isSelected}
        ref={isSelected ? selectedCardRef : null}
        style={{
          ...cardStyle,
          /*
            The resting line comes from `accent` above, already the theme's
            primary colour at a hint. Selecting takes it to full strength
            rather than standing it down the way the user, unit, container and
            interface cards do: this dashboard keeps every card in the grid
            and opens the detail below it, so a selected interface is still
            wrapped in a SortableCard and can still be held. The line stays
            live because the affordance does.
          */
          ...(isSelected && { borderBottomColor: "var(--fc-accent)" }),
          transition:
            "transform var(--hover-lift-duration) var(--hover-lift-ease), box-shadow var(--hover-lift-duration) var(--hover-lift-ease), border-color var(--app-transition-duration-fast) var(--app-easing-standard)",
        }}
      >
        {/* Status chip top-right */}
        <div style={{ position: "absolute", top: 12, right: 12 }}>
          <Chip
            color={statusColor}
            label={isActive ? "Active" : "Inactive"}
            labelStyle={{ paddingInline: 6 }}
            size="xsmall"
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
        <div
          aria-busy={actionBusy}
          aria-label={`Actions for ${iface.name}`}
          role="group"
          style={{ display: "flex", gap: 2, marginTop: "auto" }}
        >
          <AppActionIconButton
            ariaLabel={
              togglePending
                ? `${pendingAction === "up" ? "Turning interface on" : "Turning interface off"} ${iface.name}`
                : isActive
                  ? "Turn interface off"
                  : "Turn interface on"
            }
            color={isActive ? statusColor : undefined}
            disabled={actionBusy}
            icon="mdi:power"
            iconSize={20}
            label={isActive ? "Turn Off" : "Turn On"}
            loading={togglePending}
            onClick={() => {
              handleToggleInterface(iface.name, isActive ? "down" : "up");
            }}
          />
          <AppActionIconButton
            ariaLabel={
              bootPending
                ? `${pendingAction === "enable" ? "Enabling boot persistence" : "Disabling boot persistence"} ${iface.name}`
                : iface.isEnabled
                  ? "Disable boot persistence"
                  : "Enable boot persistence"
            }
            color={
              iface.isEnabled ? "var(--app-palette-primary-main)" : undefined
            }
            disabled={actionBusy}
            icon="mdi:restart"
            iconSize={20}
            label={
              iface.isEnabled
                ? "Disable Boot Persistence"
                : "Enable Boot Persistence"
            }
            loading={bootPending}
            onClick={() => {
              handleToggleBootPersistence(iface.name, iface.isEnabled);
            }}
          />
          <AppActionIconButton
            ariaLabel={
              pendingAction === "add-peer"
                ? `Adding peer to ${iface.name}`
                : "Add peer"
            }
            disabled={actionBusy}
            icon="mdi:plus"
            iconSize={20}
            label="Add Peer"
            loading={pendingAction === "add-peer"}
            onClick={() => {
              handleAddPeer(iface.name, {});
            }}
          />
          <AppActionIconButton
            ariaLabel={
              pendingAction === "delete"
                ? `Deleting interface ${iface.name}`
                : "Delete interface"
            }
            color="var(--app-palette-error-main)"
            disabled={actionBusy}
            icon="mdi:delete"
            iconSize={20}
            label="Delete Interface"
            loading={pendingAction === "delete"}
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
